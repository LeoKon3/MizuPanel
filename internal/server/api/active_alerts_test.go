package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestActiveAlertsAreCompleteAndReadOnly(t *testing.T) {
	router, s := testAlertRouter(t)
	enabled := &store.AlertRule{Name: "CPU high", Enabled: true, MetricField: "cpu_usage", Operator: ">", Threshold: 80, ScopeType: "all"}
	disabled := &store.AlertRule{Name: "disabled", Enabled: false, MetricField: "cpu_usage", Operator: ">", Threshold: 80, ScopeType: "all"}
	for _, rule := range []*store.AlertRule{enabled, disabled} {
		if err := s.CreateAlertRule(rule); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().UTC().Add(-72 * time.Hour)
	add := func(rule *store.AlertRule, node string, at time.Time, resolved *time.Time) int64 {
		t.Helper()
		h := &store.AlertHistory{RuleID: rule.ID, RuleName: rule.Name, NodeID: node, NodeName: node, MetricField: "cpu_usage", MetricValue: 95, Threshold: 80, TriggeredAt: at, ResolvedAt: resolved}
		if err := s.CreateAlertHistory(h); err != nil {
			t.Fatal(err)
		}
		return h.ID
	}
	oldID := add(enabled, "node-old", old, nil)
	for i := 0; i < 12; i++ {
		add(enabled, fmt.Sprintf("node-%d", i), old.Add(time.Duration(i+1)*time.Minute), nil)
	}
	now := time.Now().UTC()
	for i := 0; i < 110; i++ {
		add(enabled, "node-old", now, &now)
	}
	disabledID := add(disabled, "node-disabled", now, nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/alerts/active", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		History []store.AlertHistory `json:"history"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.History) != 13 {
		t.Fatalf("got %d, want all 13 active alerts", len(result.History))
	}
	if result.History[len(result.History)-1].ID != oldID {
		t.Fatal("old unresolved alert hidden behind recent history")
	}
	for _, h := range result.History {
		if h.ResolvedAt != nil || h.RuleID == disabled.ID {
			t.Fatalf("unexpected alert: %+v", h)
		}
	}
	h, err := s.GetAlertHistoryByID(disabledID)
	if err != nil || h.ResolvedAt != nil {
		t.Fatalf("GET mutated disabled history: %+v %v", h, err)
	}
}

func TestActiveAlertsEmptyAndMethodBoundary(t *testing.T) {
	router, _ := testAlertRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/alerts/active", nil))
	if w.Code != http.StatusOK || w.Body.String() != "{\"history\":[]}\n" {
		t.Fatalf("empty response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/alerts/active", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d", w.Code)
	}
}

func TestActiveAlertsRequireSessionWhenAuthIsEnabled(t *testing.T) {
	router, _ := testAlertRouter(t, AuthConfig{Enabled: true, Username: "admin", Password: "test-only", SessionTTL: time.Hour})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/alerts/active", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status %d", w.Code)
	}
	login := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"test-only"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(login, request)
	if login.Code != http.StatusOK {
		t.Fatalf("login status %d", login.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/alerts/active", nil)
	for _, cookie := range login.Result().Cookies() {
		request.AddCookie(cookie)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated GET status %d", w.Code)
	}
}
