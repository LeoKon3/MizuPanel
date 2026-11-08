package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
	serveruptime "github.com/mizupanel/mizupanel/internal/server/uptime"
)

type serviceCheckFacade struct {
	ServiceCenter
	definition servicecenter.Service
	detail     servicecenter.ServiceDetail
}

func (f serviceCheckFacade) Definition(context.Context, string) (servicecenter.Service, error) {
	return f.definition, nil
}

func (f serviceCheckFacade) Get(context.Context, string) (servicecenter.ServiceDetail, error) {
	return f.detail, nil
}

type serviceCheckProber struct {
	mu     sync.Mutex
	ids    []int64
	status string
	err    error
	stale  bool
}

func (p *serviceCheckProber) BeginMonitorMutation(int64) (func(), error) { return func() {}, nil }

func (p *serviceCheckProber) CheckNow(_ context.Context, id int64) (store.UptimeMonitor, error) {
	p.mu.Lock()
	p.ids = append(p.ids, id)
	p.mu.Unlock()
	checked := time.Now().UTC()
	if p.stale {
		checked = checked.Add(-time.Hour)
	}
	return store.UptimeMonitor{ID: id, Status: p.status, LastCheckedAt: &checked}, p.err
}

func serviceCheckDefinition(count int) serviceCheckFacade {
	f := serviceCheckFacade{definition: servicecenter.Service{ID: "service-1", Name: "Panel"}}
	f.detail.ServiceSummary = servicecenter.ServiceSummary{ID: "service-1", Health: servicecenter.HealthHealthy}
	for i := 1; i <= count; i++ {
		resource := servicecenter.Resource{ID: fmt.Sprint(i), ResourceType: servicecenter.ResourceUptimeMonitor, ResourceKey: fmt.Sprint(i), DisplayName: "Health"}
		f.definition.Resources = append(f.definition.Resources, resource)
		f.detail.Resources = append(f.detail.Resources, servicecenter.ResourceProjection{Resource: resource, Health: servicecenter.HealthHealthy, State: "available"})
	}
	return f
}

func TestServiceHealthCheckRequiresFreshCompleteEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, probeStatus, expected string
		err                         error
		stale                       bool
		resources                   int
		health                      servicecenter.HealthStatus
	}{
		{name: "fresh success", probeStatus: "up", resources: 2, expected: "healthy"},
		{name: "failed probe", probeStatus: "down", resources: 1, expected: "issues"},
		{name: "certificate warning", probeStatus: "warning", resources: 1, expected: "issues"},
		{name: "busy", err: serveruptime.ErrCheckInProgress, resources: 1, expected: "inconclusive"},
		{name: "disabled", err: serveruptime.ErrMonitorDisabled, resources: 1, expected: "inconclusive"},
		{name: "unavailable", err: fmt.Errorf("secret-token https://private.example"), resources: 1, expected: "inconclusive"},
		{name: "stale", probeStatus: "up", stale: true, resources: 1, expected: "inconclusive"},
		{name: "no evidence", resources: 0, expected: "inconclusive"},
		{name: "partial resources", probeStatus: "up", resources: 1, health: servicecenter.HealthUnknown, expected: "inconclusive"},
		{name: "resource still unhealthy", probeStatus: "up", resources: 1, health: servicecenter.HealthUnhealthy, expected: "issues"},
		{name: "over budget", probeStatus: "up", resources: 21, expected: "inconclusive"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			facade := serviceCheckDefinition(tt.resources)
			if tt.health != "" {
				facade.detail.Health = tt.health
			}
			prober := &serviceCheckProber{status: tt.probeStatus, err: tt.err, stale: tt.stale}
			server := &Server{serviceCenter: facade, uptimeChecker: prober}
			request := httptest.NewRequest(http.MethodPost, "http://panel.test/api/services/service-1/check", nil)
			request.Header.Set("Origin", "http://panel.test")
			recorder := httptest.NewRecorder()
			server.handleServiceHealthCheck(recorder, request, "service-1")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
			}
			result := decodeServiceResponse[serviceHealthCheck](t, recorder)
			if result.Status != tt.expected || result.CheckedAt.Before(result.StartedAt) {
				t.Fatalf("result = %+v, want %s", result, tt.expected)
			}
			if len(prober.ids) > maxServiceCheckMonitors || strings.Contains(recorder.Body.String(), "secret-token") || strings.Contains(recorder.Body.String(), "private.example") {
				t.Fatal("probe budget or error sanitization failed")
			}
		})
	}
}

func TestServiceHealthCheckRoutesRequireAuthOriginAndExistingService(t *testing.T) {
	fixture := newServiceAPIFixture(t)
	created := performServiceRequest(t, fixture.handler, http.MethodPost, "/api/services", servicecenter.ServiceInput{Name: "Empty"})
	detail := decodeServiceResponse[servicecenter.ServiceDetail](t, created)
	path := "/api/services/" + detail.ID + "/check"
	checked := performServiceRequest(t, fixture.handler, http.MethodPost, path, nil)
	if checked.Code != http.StatusOK || decodeServiceResponse[serviceHealthCheck](t, checked).Status != "inconclusive" {
		t.Fatalf("empty check = %d %s", checked.Code, checked.Body.String())
	}
	for _, tt := range []struct {
		method, path, origin string
		status               int
	}{
		{http.MethodPost, path, "https://other.test", http.StatusForbidden},
		{http.MethodGet, path, "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/services/missing/check", "http://panel.test", http.StatusNotFound},
		{http.MethodPost, "/api/services/a/b/check", "http://panel.test", http.StatusNotFound},
	} {
		req := httptest.NewRequest(tt.method, "http://panel.test"+tt.path, nil)
		req.Header.Set("Origin", tt.origin)
		rec := httptest.NewRecorder()
		fixture.handler.ServeHTTP(rec, req)
		if rec.Code != tt.status {
			t.Errorf("%s %s = %d, want %d", tt.method, tt.path, rec.Code, tt.status)
		}
	}
	protected := newServiceAPIFixtureWithAuth(t, AuthConfig{Enabled: true, Username: "admin", Password: "test"})
	if rec := performServiceRequest(t, protected.handler, http.MethodPost, path, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated check = %d", rec.Code)
	}
}

func TestServiceHealthCheckDetectsConcurrentAssociationChange(t *testing.T) {
	before := serviceCheckDefinition(1)
	after := append([]servicecenter.ResourceProjection{}, before.detail.Resources...)
	after[0].ResourceKey = "999"
	if sameServiceCheckResources(before.definition.Resources, after) {
		t.Fatal("changed target treated as checked")
	}
}
