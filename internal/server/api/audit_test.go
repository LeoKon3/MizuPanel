package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

func testAuditRouter(t *testing.T, auth AuthConfig) (http.Handler, *serveraudit.Store, *sql.DB) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	auditStore := serveraudit.NewStore(database, serverdb.DialectSQLite)
	router := NewRouter(store.NewNodeStore(database), store.NewMetricStore(database), auditStore, auth)
	return serveraudit.Middleware(auditStore, router), auditStore, database
}

func seedAuditEvent(t *testing.T, auditStore *serveraudit.Store, requestID, module, result string, createdAt time.Time) serveraudit.Event {
	t.Helper()
	event := serveraudit.Event{
		RequestID:  requestID,
		CreatedAt:  createdAt,
		ActorType:  serveraudit.ActorAdmin,
		ActorName:  "admin",
		SourceIP:   "192.0.2.10",
		Module:     module,
		Action:     "update",
		TargetType: "node",
		TargetID:   "node-1",
		TargetName: "Web Node",
		NodeID:     "node-1",
		Result:     result,
		DurationMS: 12,
		Summary:    "completed",
		Metadata:   map[string]string{"enabled": "true"},
	}
	if err := auditStore.Create(context.Background(), &event); err != nil {
		t.Fatalf("seed audit event: %v", err)
	}
	return event
}

func TestAuditAPIAuthenticationMethodAndTypedEmptyResponse(t *testing.T) {
	handler, _, _ := testAuditRouter(t, AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour})

	unauthenticated := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/audit/events", nil)
	request.Header.Set("X-Request-ID", "caller-controlled")
	handler.ServeHTTP(unauthenticated, request)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}
	if requestID := unauthenticated.Header().Get("X-Request-ID"); requestID == "" || requestID == "caller-controlled" {
		t.Fatalf("server request ID = %q", requestID)
	}

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d cookies=%v body=%s", login.Code, login.Result().Cookies(), login.Body.String())
	}

	list := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/audit/events?module=docker", nil)
	listRequest.AddCookie(login.Result().Cookies()[0])
	handler.ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var response struct {
		Events       []serveraudit.Event `json:"events"`
		NextBeforeID *int64              `json:"next_before_id"`
	}
	if err := json.NewDecoder(list.Body).Decode(&response); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if response.Events == nil || len(response.Events) != 0 || response.NextBeforeID != nil {
		t.Fatalf("empty response = %#v", response)
	}

	method := httptest.NewRecorder()
	methodRequest := httptest.NewRequest(http.MethodPost, "/api/audit/events", nil)
	methodRequest.AddCookie(login.Result().Cookies()[0])
	handler.ServeHTTP(method, methodRequest)
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != "GET" {
		t.Fatalf("method response=%d allow=%q", method.Code, method.Header().Get("Allow"))
	}
}

func TestAuditAPIFiltersAndKeysetPagination(t *testing.T) {
	handler, auditStore, _ := testAuditRouter(t, AuthConfig{})
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	first := seedAuditEvent(t, auditStore, "request-1", "docker", serveraudit.ResultSuccess, base)
	seedAuditEvent(t, auditStore, "request-2", "uptime", serveraudit.ResultFailure, base.Add(time.Minute))
	third := seedAuditEvent(t, auditStore, "request-3", "docker", serveraudit.ResultAccepted, base.Add(2*time.Minute))

	query := url.Values{
		"from":       []string{base.Add(-time.Minute).Format(time.RFC3339)},
		"to":         []string{base.Add(3 * time.Minute).Format(time.RFC3339)},
		"actor_type": []string{serveraudit.ActorAdmin},
		"actor_name": []string{"admin"},
		"module":     []string{"docker"},
		"action":     []string{"update"},
		"node_id":    []string{"node-1"},
		"result":     []string{serveraudit.ResultAccepted},
		"q":          []string{"Web Node"},
		"limit":      []string{"1"},
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/audit/events?"+query.Encode(), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("filtered status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var filtered struct {
		Events       []serveraudit.Event `json:"events"`
		NextBeforeID *int64              `json:"next_before_id"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&filtered); err != nil {
		t.Fatalf("decode filtered response: %v", err)
	}
	if len(filtered.Events) != 1 || filtered.Events[0].ID != third.ID || filtered.Events[0].Metadata == nil {
		t.Fatalf("filtered response = %+v", filtered)
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/api/audit/events?limit=2", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("page status=%d body=%s", page.Code, page.Body.String())
	}
	var firstPage struct {
		Events       []serveraudit.Event `json:"events"`
		NextBeforeID *int64              `json:"next_before_id"`
	}
	if err := json.NewDecoder(page.Body).Decode(&firstPage); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(firstPage.Events) != 2 || firstPage.NextBeforeID == nil {
		t.Fatalf("first page = %+v", firstPage)
	}

	next := httptest.NewRecorder()
	nextPath := "/api/audit/events?limit=2&before_id=" + url.QueryEscape(strconv.FormatInt(*firstPage.NextBeforeID, 10))
	handler.ServeHTTP(next, httptest.NewRequest(http.MethodGet, nextPath, nil))
	if next.Code != http.StatusOK {
		t.Fatalf("next status=%d body=%s", next.Code, next.Body.String())
	}
	var nextPage struct {
		Events       []serveraudit.Event `json:"events"`
		NextBeforeID *int64              `json:"next_before_id"`
	}
	if err := json.NewDecoder(next.Body).Decode(&nextPage); err != nil {
		t.Fatalf("decode next page: %v", err)
	}
	if len(nextPage.Events) != 1 || nextPage.Events[0].ID != first.ID || nextPage.NextBeforeID != nil {
		t.Fatalf("next page = %+v", nextPage)
	}
}

func TestAuditAPIRejectsInvalidFiltersAndHidesStorageErrors(t *testing.T) {
	handler, _, database := testAuditRouter(t, AuthConfig{})
	for _, path := range []string{
		"/api/audit/events?before_id=0",
		"/api/audit/events?limit=101",
		"/api/audit/events?from=not-a-time",
		"/api/audit/events?from=2026-07-27T00%3A00%3A00Z&to=2026-07-26T00%3A00%3A00Z",
		"/api/audit/events?actor_type=root",
		"/api/audit/events?module=Docker",
		"/api/audit/events?result=pending",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/audit/events", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("storage failure status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(strings.ToLower(recorder.Body.String()), "sql") || strings.Contains(recorder.Body.String(), "database is closed") {
		t.Fatalf("storage error leaked details: %s", recorder.Body.String())
	}
}

func TestAuditAuthenticationActors(t *testing.T) {
	handler, auditStore, _ := testAuditRouter(t, AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour})
	failedLogin := auditRequest(t, handler, nil, http.MethodPost, "/api/auth/login", map[string]any{"username": "attempted-user", "password": "wrong"})
	if failedLogin.Code != http.StatusUnauthorized {
		t.Fatalf("failed login status=%d body=%s", failedLogin.Code, failedLogin.Body.String())
	}
	successfulLogin := auditRequest(t, handler, nil, http.MethodPost, "/api/auth/login", map[string]any{"username": "admin", "password": "secret"})
	if successfulLogin.Code != http.StatusOK || len(successfulLogin.Result().Cookies()) == 0 {
		t.Fatalf("successful login status=%d cookies=%v body=%s", successfulLogin.Code, successfulLogin.Result().Cookies(), successfulLogin.Body.String())
	}
	logout := auditRequest(t, handler, successfulLogin.Result().Cookies()[0], http.MethodPost, "/api/auth/logout", nil)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}

	page, err := auditStore.List(t.Context(), serveraudit.Filter{Module: "auth", Limit: 10})
	if err != nil {
		t.Fatalf("list auth events: %v", err)
	}
	if len(page.Events) != 3 {
		t.Fatalf("auth events=%+v", page.Events)
	}
	var failedActor, successfulActor, logoutActor bool
	for _, event := range page.Events {
		switch {
		case event.Action == "login" && event.Result == serveraudit.ResultFailure:
			failedActor = event.ActorType == serveraudit.ActorUnauthenticated && event.ActorName == "attempted-user"
		case event.Action == "login" && event.Result == serveraudit.ResultSuccess:
			successfulActor = event.ActorType == serveraudit.ActorAdmin && event.ActorName == "admin"
		case event.Action == "logout" && event.Result == serveraudit.ResultSuccess:
			logoutActor = event.ActorType == serveraudit.ActorAdmin && event.ActorName == "admin"
		}
	}
	if !failedActor || !successfulActor || !logoutActor {
		t.Fatalf("auth actor attribution failed: %+v", page.Events)
	}

	localHandler, localAuditStore, _ := testAuditRouter(t, AuthConfig{})
	localLogin := auditRequest(t, localHandler, nil, http.MethodPost, "/api/auth/login", nil)
	localLogout := auditRequest(t, localHandler, nil, http.MethodPost, "/api/auth/logout", nil)
	if localLogin.Code != http.StatusOK || localLogout.Code != http.StatusOK {
		t.Fatalf("local auth statuses login=%d logout=%d", localLogin.Code, localLogout.Code)
	}
	localPage, err := localAuditStore.List(t.Context(), serveraudit.Filter{Module: "auth", Limit: 10})
	if err != nil {
		t.Fatalf("list local auth events: %v", err)
	}
	if len(localPage.Events) != 2 {
		t.Fatalf("local auth events=%+v", localPage.Events)
	}
	localActions := make(map[string]bool)
	for _, event := range localPage.Events {
		if event.ActorType != serveraudit.ActorLocalAdmin || event.ActorName != "local" || event.Result != serveraudit.ResultSuccess {
			t.Errorf("local auth event=%+v", event)
		}
		localActions[event.Action] = true
	}
	if !localActions["login"] || !localActions["logout"] {
		t.Errorf("local auth actions=%v", localActions)
	}
}

type auditK8sHub struct{}

func (auditK8sHub) IsNodeOnline(string) bool { return true }

func (auditK8sHub) SendToNodeWithTimeout(_ string, message interface{}, _ time.Duration) (json.RawMessage, error) {
	var response any
	switch message.(type) {
	case protocol.K8sClusterConnectRequest:
		response = protocol.K8sClusterConnectResult{
			Type:    protocol.MessageTypeK8sClusterConnectResult,
			Success: true,
			ClusterInfo: &protocol.K8sClusterInfo{
				Version:        "v1.30.0",
				NodeCount:      1,
				NamespaceCount: 3,
			},
		}
	case protocol.K8sApplyManifestRequest:
		response = protocol.K8sApplyManifestResult{Type: protocol.MessageTypeK8sApplyManifestResult, Success: true, Message: "applied"}
	case protocol.K8sResourceActionRequest:
		response = protocol.K8sResourceActionResult{Type: protocol.MessageTypeK8sResourceActionResult, Success: true, Message: "restarted"}
	default:
		response = map[string]any{"success": true}
	}
	encoded, err := json.Marshal(response)
	return encoded, err
}

func TestAuditOperationCoverageAndSecretBoundary(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "Oracle", OS: "linux", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	metrics := store.NewMetricStore(database)
	settings := store.NewSettingsStore(database)
	alerts := store.NewAlertStore(database)
	uptimeStore := store.NewUptimeStore(database)
	auditStore := serveraudit.NewStore(database, serverdb.DialectSQLite)
	k8sService := k8s.NewService(k8s.NewStore(database), auditK8sHub{})
	ops := &fakeNodeOperations{}
	auth := AuthConfig{Enabled: true, Username: "admin", Password: "admin-password-secret-marker", SessionTTL: time.Hour}
	router := NewRouter(
		nodes,
		metrics,
		ops,
		alerts,
		k8sService,
		SettingsConfig{Store: settings, DefaultMetricsRetention: 6 * time.Hour},
		UptimeConfig{Store: uptimeStore},
		TerminalConfig{Enabled: true},
		auditStore,
		auth,
	)
	handler := serveraudit.Middleware(auditStore, router)

	failedLogin := auditRequest(t, handler, nil, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "intruder-user",
		"password": "login-password-secret-marker",
	})
	if failedLogin.Code != http.StatusUnauthorized {
		t.Fatalf("failed login status=%d body=%s", failedLogin.Code, failedLogin.Body.String())
	}
	successfulLogin := auditRequest(t, handler, nil, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "admin",
		"password": "admin-password-secret-marker",
	})
	if successfulLogin.Code != http.StatusOK || len(successfulLogin.Result().Cookies()) == 0 {
		t.Fatalf("successful login status=%d cookies=%v body=%s", successfulLogin.Code, successfulLogin.Result().Cookies(), successfulLogin.Body.String())
	}
	cookie := successfulLogin.Result().Cookies()[0]

	mutations := []struct {
		method string
		path   string
		body   any
		status int
	}{
		{http.MethodPut, "/api/settings", map[string]any{"metrics_retention": "6h"}, http.StatusOK},
		{http.MethodPost, "/api/node-groups", map[string]any{"name": "Operations"}, http.StatusCreated},
		{http.MethodPost, "/api/nodes/node-1/reboot", nil, http.StatusOK},
		{http.MethodPut, "/api/nodes/node-1/files/content", map[string]any{"path": "/srv/app/config.json", "content": "file-content-secret-marker"}, http.StatusOK},
		{http.MethodPost, "/api/nodes/node-1/agent/restart", nil, http.StatusOK},
		{http.MethodPost, "/api/nodes/node-1/docker/exec", map[string]any{"command": "echo command-secret-marker"}, http.StatusOK},
		{http.MethodPost, "/api/nodes/node-1/containers/container-1/restart", nil, http.StatusOK},
		{http.MethodPost, "/api/nodes/node-1/docker/compose/action", map[string]any{"project_name": "demo", "service_name": "web", "action": "restart"}, http.StatusOK},
		{http.MethodPost, "/api/nodes/node-1/docker/compose/deployment", map[string]any{
			"action":             "apply",
			"project_id":         "11111111-1111-4111-8111-111111111111",
			"display_name":       "Demo",
			"compose_yaml":       "services:\n  web:\n    image: compose-yaml-secret-marker",
			"env_file":           "PASSWORD=compose-env-secret-marker",
			"confirmation_token": "compose-token-secret-marker",
		}, http.StatusOK},
		{http.MethodPost, "/api/nodes/node-1/docker/resources/action", map[string]any{"resource_type": "volume", "resource_id": "data", "action": "remove"}, http.StatusOK},
		{http.MethodPost, "/api/nodes/node-1/services/systemd/action", map[string]any{"service_name": "nginx.service", "action": "restart"}, http.StatusOK},
		{http.MethodPost, "/api/alerts/rules", map[string]any{
			"name": "CPU High", "enabled": true, "metric_field": "cpu_usage", "operator": ">", "threshold": 80,
			"duration_seconds": 60, "scope_type": "all", "notification_channels": []any{map[string]any{
				"type": "webhook", "webhook_url": "https://hooks.example/alert-webhook-secret-marker", "secret": "alert-signing-secret-marker",
			}},
		}, http.StatusCreated},
		{http.MethodPost, "/api/uptime/monitors", map[string]any{
			"name": "Website", "type": "http", "target": "https://uptime-target-secret-marker.example/health", "enabled": true,
			"interval_seconds": 60, "timeout_seconds": 5, "failure_threshold": 3, "expected_status_min": 200, "expected_status_max": 399,
			"tls_expiry_threshold_days": 30, "notification_channels": []any{map[string]any{"type": "webhook", "webhook_url": "https://hooks.example/uptime-webhook-secret-marker"}},
		}, http.StatusCreated},
	}
	for _, mutation := range mutations {
		recorder := auditRequest(t, handler, cookie, mutation.method, mutation.path, mutation.body)
		if recorder.Code != mutation.status {
			t.Fatalf("%s %s status=%d want=%d body=%s", mutation.method, mutation.path, recorder.Code, mutation.status, recorder.Body.String())
		}
	}

	connect := auditRequest(t, handler, cookie, http.MethodPost, "/api/k8s/clusters", map[string]any{
		"name": "Production", "node_id": "node-1", "context": "default", "kubeconfig_content": "kubeconfig-secret-marker",
	})
	if connect.Code != http.StatusOK {
		t.Fatalf("connect k8s status=%d body=%s", connect.Code, connect.Body.String())
	}
	var connectResponse struct {
		Cluster struct {
			ID string `json:"id"`
		} `json:"cluster"`
	}
	if err := json.NewDecoder(connect.Body).Decode(&connectResponse); err != nil || connectResponse.Cluster.ID == "" {
		t.Fatalf("decode connect response: id=%q err=%v", connectResponse.Cluster.ID, err)
	}
	apply := auditRequest(t, handler, cookie, http.MethodPost, "/api/k8s/clusters/"+connectResponse.Cluster.ID+"/resources/apply", map[string]any{
		"yaml": "apiVersion: v1\nkind: Secret\nstringData:\n  password: k8s-secret-data-marker",
	})
	if apply.Code != http.StatusOK {
		t.Fatalf("apply k8s status=%d body=%s", apply.Code, apply.Body.String())
	}
	resourceAction := auditRequest(t, handler, cookie, http.MethodPost, "/api/k8s/clusters/"+connectResponse.Cluster.ID+"/resources/deployment/default/web/actions", map[string]any{
		"action": "restart",
	})
	if resourceAction.Code != http.StatusOK {
		t.Fatalf("k8s resource action status=%d body=%s", resourceAction.Code, resourceAction.Body.String())
	}
	deleteCluster := auditRequest(t, handler, cookie, http.MethodDelete, "/api/k8s/clusters/"+connectResponse.Cluster.ID, nil)
	if deleteCluster.Code != http.StatusOK {
		t.Fatalf("delete k8s cluster status=%d body=%s", deleteCluster.Code, deleteCluster.Body.String())
	}
	invalidCompose := auditRequest(t, handler, cookie, http.MethodPost, "/api/nodes/node-1/docker/compose/action", map[string]any{"project_name": "demo", "action": "compose-action-secret-marker"})
	incompleteCompose := auditRequest(t, handler, cookie, http.MethodPost, "/api/nodes/node-1/docker/compose/action", map[string]any{"project_name": "demo"})
	malformedComposeRequest := httptest.NewRequest(http.MethodPost, "/api/nodes/node-1/docker/compose/action", strings.NewReader(`{"project_name":"demo","action":`))
	malformedComposeRequest.Host = "panel.example"
	malformedComposeRequest.Header.Set("Origin", "http://panel.example")
	malformedComposeRequest.Header.Set("Content-Type", "application/json")
	malformedComposeRequest.AddCookie(cookie)
	malformedCompose := httptest.NewRecorder()
	handler.ServeHTTP(malformedCompose, malformedComposeRequest)
	invalidSystemd := auditRequest(t, handler, cookie, http.MethodPost, "/api/nodes/node-1/services/systemd/action", map[string]any{"service_name": "nginx.service", "action": "systemd-action-secret-marker"})
	invalidSettings := auditRequest(t, handler, cookie, http.MethodPut, "/api/settings", map[string]any{"metrics_retention": "settings-secret-marker"})
	if invalidCompose.Code != http.StatusBadRequest || incompleteCompose.Code != http.StatusBadRequest || malformedCompose.Code != http.StatusBadRequest || invalidSystemd.Code != http.StatusBadRequest || invalidSettings.Code != http.StatusBadRequest {
		t.Fatalf("invalid request statuses compose=%d incomplete_compose=%d malformed_compose=%d systemd=%d settings=%d", invalidCompose.Code, incompleteCompose.Code, malformedCompose.Code, invalidSystemd.Code, invalidSettings.Code)
	}

	beforeTokens, err := auditStore.List(t.Context(), serveraudit.Filter{Limit: serveraudit.MaxPageLimit})
	if err != nil {
		t.Fatalf("list before tokens: %v", err)
	}
	terminalToken := auditRequest(t, handler, cookie, http.MethodPost, "/api/nodes/node-1/terminal/session", nil)
	containerToken := auditRequest(t, handler, cookie, http.MethodPost, "/api/nodes/node-1/containers/container-1/exec/session", nil)
	composeLogs := auditRequest(t, handler, cookie, http.MethodPost, "/api/nodes/node-1/docker/compose/action", map[string]any{"project_name": "demo", "service_name": "web", "action": "logs"})
	systemdLogs := auditRequest(t, handler, cookie, http.MethodPost, "/api/nodes/node-1/services/systemd/action", map[string]any{"service_name": "nginx.service", "action": "logs"})
	composePreview := auditRequest(t, handler, cookie, http.MethodPost, "/api/nodes/node-1/docker/compose/deployment", map[string]any{"action": "preview", "display_name": "Preview", "compose_yaml": "services:\n  web:\n    image: preview-yaml-secret-marker"})
	if terminalToken.Code != http.StatusOK || containerToken.Code != http.StatusOK || composeLogs.Code != http.StatusOK || systemdLogs.Code != http.StatusOK || composePreview.Code != http.StatusOK {
		t.Fatalf("read/session statuses terminal=%d container=%d compose_logs=%d systemd_logs=%d preview=%d", terminalToken.Code, containerToken.Code, composeLogs.Code, systemdLogs.Code, composePreview.Code)
	}
	afterTokens, err := auditStore.List(t.Context(), serveraudit.Filter{Limit: serveraudit.MaxPageLimit})
	if err != nil {
		t.Fatalf("list after tokens: %v", err)
	}
	if len(afterTokens.Events) != len(beforeTokens.Events) {
		t.Fatalf("token creation or Compose logs created audit events: before=%d after=%d", len(beforeTokens.Events), len(afterTokens.Events))
	}

	modules := make(map[string]bool)
	results := make(map[string]bool)
	operationResults := make(map[string]map[string]bool)
	genericComposeFailures := 0
	k8sNodeActions := map[string]bool{
		"manifest_apply": false,
		"restart":        false,
		"cluster_delete": false,
	}
	for _, event := range afterTokens.Events {
		modules[event.Module] = true
		results[event.Result] = true
		operationKey := event.Module + "/" + event.Action
		if operationResults[operationKey] == nil {
			operationResults[operationKey] = make(map[string]bool)
		}
		operationResults[operationKey][event.Result] = true
		if event.Module == "compose" && event.Action == "action" && event.Result == serveraudit.ResultFailure && event.Summary == "invalid_request" {
			genericComposeFailures++
		}
		if _, tracked := k8sNodeActions[event.Action]; event.Module == "kubernetes" && tracked {
			if event.NodeID != "node-1" {
				t.Errorf("kubernetes %s event node_id=%q, want node-1: %+v", event.Action, event.NodeID, event)
			}
			k8sNodeActions[event.Action] = true
		}
	}
	if genericComposeFailures != 3 {
		t.Errorf("generic Compose failure events=%d, want 3", genericComposeFailures)
	}
	for action, found := range k8sNodeActions {
		if !found {
			t.Errorf("missing Kubernetes %s event with node attribution", action)
		}
	}
	for _, expected := range []struct {
		operation string
		result    string
	}{
		{"auth/login", serveraudit.ResultSuccess},
		{"auth/login", serveraudit.ResultFailure},
		{"settings/update", serveraudit.ResultSuccess},
		{"settings/update", serveraudit.ResultFailure},
		{"organization/group_create", serveraudit.ResultSuccess},
		{"node/reboot", serveraudit.ResultAccepted},
		{"file/write", serveraudit.ResultSuccess},
		{"agent/restart", serveraudit.ResultAccepted},
		{"docker/exec", serveraudit.ResultSuccess},
		{"docker/container_restart", serveraudit.ResultSuccess},
		{"compose/restart", serveraudit.ResultSuccess},
		{"compose/apply", serveraudit.ResultSuccess},
		{"compose/action", serveraudit.ResultFailure},
		{"docker_resource/remove", serveraudit.ResultSuccess},
		{"systemd/restart", serveraudit.ResultSuccess},
		{"alert/rule_create", serveraudit.ResultSuccess},
		{"uptime/monitor_create", serveraudit.ResultSuccess},
		{"kubernetes/cluster_connect", serveraudit.ResultSuccess},
		{"kubernetes/manifest_apply", serveraudit.ResultSuccess},
		{"kubernetes/restart", serveraudit.ResultSuccess},
		{"kubernetes/cluster_delete", serveraudit.ResultSuccess},
	} {
		if !operationResults[expected.operation][expected.result] {
			t.Errorf("missing representative audit operation %s result=%s; operations=%v", expected.operation, expected.result, operationResults)
		}
	}
	for _, module := range []string{"auth", "settings", "organization", "node", "file", "agent", "docker", "compose", "docker_resource", "systemd", "kubernetes", "alert", "uptime"} {
		if !modules[module] {
			t.Errorf("missing representative audit module %q; modules=%v", module, modules)
		}
	}
	for _, result := range []string{serveraudit.ResultSuccess, serveraudit.ResultFailure, serveraudit.ResultAccepted} {
		if !results[result] {
			t.Errorf("missing representative result %q; results=%v", result, results)
		}
	}

	apiResponse := auditRequest(t, handler, cookie, http.MethodGet, "/api/audit/events?limit=100", nil)
	if apiResponse.Code != http.StatusOK {
		t.Fatalf("audit query status=%d body=%s", apiResponse.Code, apiResponse.Body.String())
	}
	serialized := apiResponse.Body.String()
	for _, secret := range []string{
		"login-password-secret-marker", "admin-password-secret-marker", "file-content-secret-marker", "command-secret-marker",
		"compose-yaml-secret-marker", "compose-env-secret-marker", "compose-token-secret-marker", "alert-webhook-secret-marker",
		"alert-signing-secret-marker", "uptime-target-secret-marker", "uptime-webhook-secret-marker", "kubeconfig-secret-marker", "k8s-secret-data-marker",
		"compose-action-secret-marker", "systemd-action-secret-marker", "settings-secret-marker", "preview-yaml-secret-marker",
		cookie.Value,
	} {
		if strings.Contains(serialized, secret) {
			t.Errorf("audit API leaked secret marker %q: %s", secret, serialized)
		}
	}
}

func TestAuditWebSocketCloseOutcome(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantResult  string
		wantSummary string
	}{
		{name: "nil", wantResult: serveraudit.ResultSuccess, wantSummary: "closed"},
		{name: "normal closure", err: &websocket.CloseError{Code: websocket.CloseNormalClosure, Text: "done"}, wantResult: serveraudit.ResultSuccess, wantSummary: "closed"},
		{name: "going away", err: &websocket.CloseError{Code: websocket.CloseGoingAway, Text: "leaving"}, wantResult: serveraudit.ResultSuccess, wantSummary: "closed"},
		{name: "abnormal closure", err: &websocket.CloseError{Code: websocket.CloseAbnormalClosure, Text: "disconnected"}, wantResult: serveraudit.ResultFailure, wantSummary: "remote_operation_failed"},
		{name: "generic error", err: errors.New("attach failed"), wantResult: serveraudit.ResultFailure, wantSummary: "remote_operation_failed"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result, summary := auditWebSocketCloseOutcome(testCase.err)
			if result != testCase.wantResult || summary != testCase.wantSummary {
				t.Fatalf("outcome=(%q, %q), want (%q, %q)", result, summary, testCase.wantResult, testCase.wantSummary)
			}
		})
	}
}

func TestAuditRecordsRealTerminalAndContainerExecWebSocketLifetimes(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "Oracle", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	auditStore := serveraudit.NewStore(database, serverdb.DialectSQLite)
	operations := &fakeNodeOperations{}
	router := NewRouter(nodes, store.NewMetricStore(database), operations, TerminalConfig{Enabled: true}, auditStore)
	testServer := newAuditHTTPTestServer(t, serveraudit.Middleware(auditStore, router))
	t.Cleanup(testServer.Close)

	type websocketCase struct {
		tokenPath string
		wsPath    string
		open      string
		close     string
	}
	cases := []websocketCase{
		{tokenPath: "/api/nodes/node-1/terminal/session", wsPath: "/api/nodes/node-1/terminal/ws", open: "session_open", close: "session_close"},
		{tokenPath: "/api/nodes/node-1/containers/container-1/exec/session", wsPath: "/api/nodes/node-1/containers/container-1/exec/ws", open: "container_exec_open", close: "container_exec_close"},
	}
	tokens := make([]string, 0, len(cases))
	for _, testCase := range cases {
		token := requestWebSocketToken(t, testServer.URL, testCase.tokenPath)
		tokens = append(tokens, token)
		before, err := auditStore.List(t.Context(), serveraudit.Filter{Module: "terminal"})
		if err != nil {
			t.Fatalf("list before websocket: %v", err)
		}
		if len(before.Events) != 0 && testCase.open == "session_open" {
			t.Fatalf("token creation recorded terminal event: %+v", before.Events)
		}

		header := http.Header{"Origin": []string{testServer.URL}}
		websocketURL := strings.Replace(testServer.URL, "http://", "ws://", 1) + testCase.wsPath + "?token=" + url.QueryEscape(token)
		connection, response, err := websocket.DefaultDialer.Dial(websocketURL, header)
		if err != nil {
			if response != nil {
				t.Fatalf("dial websocket status=%d err=%v", response.StatusCode, err)
			}
			t.Fatalf("dial websocket: %v", err)
		}
		_ = connection.Close()
	}

	var events []serveraudit.Event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		page, err := auditStore.List(t.Context(), serveraudit.Filter{Module: "terminal", Limit: 10})
		if err != nil {
			t.Fatalf("list terminal events: %v", err)
		}
		events = page.Events
		if len(events) == 4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(events) != 4 {
		t.Fatalf("terminal events = %+v", events)
	}

	actionsByRequest := make(map[string]map[string]bool)
	serialized, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal terminal events: %v", err)
	}
	for _, event := range events {
		if actionsByRequest[event.RequestID] == nil {
			actionsByRequest[event.RequestID] = make(map[string]bool)
		}
		actionsByRequest[event.RequestID][event.Action] = true
		if event.NodeID != "node-1" || event.Result != serveraudit.ResultSuccess {
			t.Errorf("terminal event = %+v", event)
		}
	}
	if len(actionsByRequest) != 2 {
		t.Fatalf("request groups = %v", actionsByRequest)
	}
	for _, testCase := range cases {
		foundPair := false
		for _, actions := range actionsByRequest {
			if actions[testCase.open] && actions[testCase.close] {
				foundPair = true
			}
		}
		if !foundPair {
			t.Errorf("missing WebSocket pair %s/%s in %v", testCase.open, testCase.close, actionsByRequest)
		}
	}
	for _, token := range tokens {
		if bytes.Contains(serialized, []byte(token)) {
			t.Errorf("terminal audit events leaked query token %q", token)
		}
	}
}

func requestWebSocketToken(t *testing.T, serverURL, path string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, serverURL+path, nil)
	if err != nil {
		t.Fatalf("create token request: %v", err)
	}
	request.Header.Set("Origin", serverURL)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request WebSocket token: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("token status=%d", response.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Token == "" {
		t.Fatalf("decode token: token=%q err=%v", body.Token, err)
	}
	return body.Token
}

func newAuditHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("sandbox does not permit loopback listeners: %v", err)
		}
		t.Fatalf("listen for WebSocket test: %v", err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	server.Start()
	return server
}

func auditRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("encode %s %s body: %v", method, path, err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
