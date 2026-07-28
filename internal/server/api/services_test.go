package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type serviceAPIFixture struct {
	handler http.Handler
	db      *sql.DB
}

func newServiceAPIFixture(t *testing.T) serviceAPIFixture {
	return newServiceAPIFixtureWithAuth(t, AuthConfig{})
}

func newServiceAPIFixtureWithAuth(t *testing.T, auth AuthConfig) serviceAPIFixture {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	nodes := store.NewNodeStore(database)
	serviceStore := servicecenter.NewStore(database, serverdb.DialectSQLite)
	auditStore := serveraudit.NewStore(database, serverdb.DialectSQLite)
	router := NewRouter(
		nodes,
		store.NewMetricStore(database),
		ServiceCenterConfig{Facade: servicecenter.NewFacade(serviceStore, nil, nil)},
		auth,
		auditStore,
	)
	return serviceAPIFixture{handler: serveraudit.Middleware(auditStore, router), db: database}
}

func performServiceRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, "http://panel.test"+path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete || method == http.MethodPatch {
		request.Header.Set("Origin", "http://panel.test")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeServiceResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var response T
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response %d %q: %v", recorder.Code, recorder.Body.String(), err)
	}
	return response
}

func TestServicesAPIEmptyCRUDValidationAuditAndNonDestructiveDelete(t *testing.T) {
	fixture := newServiceAPIFixture(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := fixture.db.Exec(`INSERT INTO nodes (id, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "node-1", "Node One", "online", now, now); err != nil {
		t.Fatalf("insert node: %v", err)
	}

	empty := performServiceRequest(t, fixture.handler, http.MethodGet, "/api/services", nil)
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Fatalf("empty services = %d %q", empty.Code, empty.Body.String())
	}

	const secretMarker = "service-request-secret-marker"
	input := servicecenter.ServiceInput{
		Name:        "MizuPanel",
		Description: secretMarker,
		Resources:   []servicecenter.Resource{{ResourceType: servicecenter.ResourceNode, ResourceKey: "node-1", DisplayName: "Node One"}},
	}
	createdRecorder := performServiceRequest(t, fixture.handler, http.MethodPost, "/api/services", input)
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create service = %d %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	created := decodeServiceResponse[servicecenter.ServiceDetail](t, createdRecorder)
	if created.ID == "" || created.Health != servicecenter.HealthHealthy || created.ResourceCount != 1 || created.RecentAlerts == nil || created.RecentTasks == nil || created.RecentAudit == nil {
		t.Fatalf("created service = %#v", created)
	}

	listRecorder := performServiceRequest(t, fixture.handler, http.MethodGet, "/api/services", nil)
	services := decodeServiceResponse[[]servicecenter.ServiceSummary](t, listRecorder)
	if listRecorder.Code != http.StatusOK || len(services) != 1 || services[0].ID != created.ID {
		t.Fatalf("services list = %d %#v", listRecorder.Code, services)
	}
	detailRecorder := performServiceRequest(t, fixture.handler, http.MethodGet, "/api/services/"+created.ID, nil)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("get service = %d %s", detailRecorder.Code, detailRecorder.Body.String())
	}

	duplicateName := performServiceRequest(t, fixture.handler, http.MethodPost, "/api/services", servicecenter.ServiceInput{Name: " mizupanel "})
	if duplicateName.Code != http.StatusConflict || !strings.Contains(duplicateName.Body.String(), "名称") {
		t.Fatalf("duplicate name = %d %s", duplicateName.Code, duplicateName.Body.String())
	}
	emptyName := performServiceRequest(t, fixture.handler, http.MethodPost, "/api/services", servicecenter.ServiceInput{Name: " "})
	if emptyName.Code != http.StatusBadRequest || !strings.Contains(emptyName.Body.String(), "不能为空") {
		t.Fatalf("empty name = %d %s", emptyName.Code, emptyName.Body.String())
	}
	unknownResource := performServiceRequest(t, fixture.handler, http.MethodPost, "/api/services", servicecenter.ServiceInput{Name: "Unknown", Resources: []servicecenter.Resource{{ResourceType: "unknown", ResourceKey: "one"}}})
	if unknownResource.Code != http.StatusBadRequest || !strings.Contains(unknownResource.Body.String(), "不支持") {
		t.Fatalf("unknown resource = %d %s", unknownResource.Code, unknownResource.Body.String())
	}
	duplicateResource := performServiceRequest(t, fixture.handler, http.MethodPost, "/api/services", servicecenter.ServiceInput{Name: "Duplicate resource", Resources: []servicecenter.Resource{
		{ResourceType: servicecenter.ResourceNode, ResourceKey: "node-1"},
		{ResourceType: servicecenter.ResourceNode, ResourceKey: "node-1"},
	}})
	if duplicateResource.Code != http.StatusConflict {
		t.Fatalf("duplicate resource = %d %s", duplicateResource.Code, duplicateResource.Body.String())
	}
	invalidK8sKind := performServiceRequest(t, fixture.handler, http.MethodPost, "/api/services", servicecenter.ServiceInput{Name: "Invalid K8s", Resources: []servicecenter.Resource{{ResourceType: servicecenter.ResourceK8sWorkload, ScopeID: "cluster-1", ResourceKind: "job", Namespace: "default", ResourceKey: "worker"}}})
	if invalidK8sKind.Code != http.StatusBadRequest || !strings.Contains(invalidK8sKind.Body.String(), "Kubernetes") {
		t.Fatalf("invalid Kubernetes kind = %d %s", invalidK8sKind.Code, invalidK8sKind.Body.String())
	}

	input.Name = "MizuPanel Core"
	updatedRecorder := performServiceRequest(t, fixture.handler, http.MethodPut, "/api/services/"+created.ID, input)
	if updatedRecorder.Code != http.StatusOK {
		t.Fatalf("update service = %d %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	updated := decodeServiceResponse[servicecenter.ServiceDetail](t, updatedRecorder)
	if updated.Name != "MizuPanel Core" || updated.ResourceCount != 1 {
		t.Fatalf("updated service = %#v", updated)
	}

	deleteRecorder := performServiceRequest(t, fixture.handler, http.MethodDelete, "/api/services/"+created.ID, nil)
	if deleteRecorder.Code != http.StatusNoContent || deleteRecorder.Body.Len() != 0 {
		t.Fatalf("delete service = %d %q", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	missing := performServiceRequest(t, fixture.handler, http.MethodGet, "/api/services/"+created.ID, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get deleted service = %d %s", missing.Code, missing.Body.String())
	}
	var nodeCount int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = 'node-1'`).Scan(&nodeCount); err != nil {
		t.Fatalf("count original node: %v", err)
	}
	if nodeCount != 1 {
		t.Fatalf("original node count = %d, want 1", nodeCount)
	}

	rows, err := fixture.db.Query(`SELECT action, target_id, target_name, result, summary, metadata_json FROM audit_events WHERE module = 'service' AND target_id = ? ORDER BY id`, created.ID)
	if err != nil {
		t.Fatalf("query service audit: %v", err)
	}
	defer rows.Close()
	actions := []string{}
	for rows.Next() {
		var action, targetID, targetName, result, summary, metadata string
		if err := rows.Scan(&action, &targetID, &targetName, &result, &summary, &metadata); err != nil {
			t.Fatalf("scan service audit: %v", err)
		}
		serialized := strings.Join([]string{action, targetID, targetName, result, summary, metadata}, " ")
		if strings.Contains(serialized, secretMarker) || !strings.Contains(metadata, `"resource_count":"1"`) || result != serveraudit.ResultSuccess {
			t.Fatalf("unsafe or incomplete audit event: %s", serialized)
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate service audit: %v", err)
	}
	if strings.Join(actions, ",") != "create,update,delete" {
		t.Fatalf("successful service audit actions = %#v", actions)
	}

	failedRows, err := fixture.db.Query(`SELECT target_name, summary, metadata_json FROM audit_events WHERE module = 'service' AND result = 'failure'`)
	if err != nil {
		t.Fatalf("query failed service audits: %v", err)
	}
	defer failedRows.Close()
	failedCount := 0
	for failedRows.Next() {
		var targetName, summary, metadata string
		if err := failedRows.Scan(&targetName, &summary, &metadata); err != nil {
			t.Fatalf("scan failed service audit: %v", err)
		}
		if strings.Contains(strings.Join([]string{targetName, summary, metadata}, " "), secretMarker) {
			t.Fatal("failed service audit leaked request content")
		}
		failedCount++
	}
	if err := failedRows.Err(); err != nil {
		t.Fatalf("iterate failed service audits: %v", err)
	}
	if failedCount == 0 {
		t.Fatal("expected failed service operations to be audited")
	}
}

func TestServicesAPIMethodsAndMissingRoutes(t *testing.T) {
	fixture := newServiceAPIFixture(t)
	collection := performServiceRequest(t, fixture.handler, http.MethodPatch, "/api/services", nil)
	if collection.Code != http.StatusMethodNotAllowed || collection.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("collection method = %d Allow=%q", collection.Code, collection.Header().Get("Allow"))
	}
	detail := performServiceRequest(t, fixture.handler, http.MethodPatch, "/api/services/missing", nil)
	if detail.Code != http.StatusMethodNotAllowed || detail.Header().Get("Allow") != "GET, PUT, DELETE" {
		t.Fatalf("detail method = %d Allow=%q", detail.Code, detail.Header().Get("Allow"))
	}
	invalidPath := performServiceRequest(t, fixture.handler, http.MethodGet, "/api/services/a/b", nil)
	if invalidPath.Code != http.StatusNotFound {
		t.Fatalf("invalid nested path = %d %s", invalidPath.Code, invalidPath.Body.String())
	}
	missingUpdate := performServiceRequest(t, fixture.handler, http.MethodPut, "/api/services/missing", servicecenter.ServiceInput{Name: "Missing"})
	if missingUpdate.Code != http.StatusNotFound {
		t.Fatalf("missing update = %d %s", missingUpdate.Code, missingUpdate.Body.String())
	}
	missingDelete := performServiceRequest(t, fixture.handler, http.MethodDelete, "/api/services/missing", nil)
	if missingDelete.Code != http.StatusNotFound {
		t.Fatalf("missing delete = %d %s", missingDelete.Code, missingDelete.Body.String())
	}
}

func TestServicesAPIAuthenticationBodyBoundsAndStrictJSON(t *testing.T) {
	authFixture := newServiceAPIFixtureWithAuth(t, AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour})
	unauthorized := performServiceRequest(t, authFixture.handler, http.MethodGet, "/api/services", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized services = %d %s", unauthorized.Code, unauthorized.Body.String())
	}

	fixture := newServiceAPIFixture(t)
	strictRequest := httptest.NewRequest(http.MethodPost, "http://panel.test/api/services", strings.NewReader(`{"name":"Strict","unknown":true}`))
	strictRequest.Header.Set("Content-Type", "application/json")
	strictRequest.Header.Set("Origin", "http://panel.test")
	strictRecorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(strictRecorder, strictRequest)
	if strictRecorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field = %d %s", strictRecorder.Code, strictRecorder.Body.String())
	}

	largeBody := `{"name":"Large","description":"` + strings.Repeat("x", maxAutomationRequestBodyBytes) + `"}`
	largeRequest := httptest.NewRequest(http.MethodPost, "http://panel.test/api/services", strings.NewReader(largeBody))
	largeRequest.Header.Set("Content-Type", "application/json")
	largeRequest.Header.Set("Origin", "http://panel.test")
	largeRecorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(largeRecorder, largeRequest)
	if largeRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized service body = %d %s", largeRecorder.Code, largeRecorder.Body.String())
	}
}
