package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/logbuffer"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type settingsAuditRecorder struct {
	events []serveraudit.Event
}

func (r *settingsAuditRecorder) Create(_ context.Context, event *serveraudit.Event) error {
	copy := *event
	copy.Metadata = make(map[string]string, len(event.Metadata))
	for key, value := range event.Metadata {
		copy.Metadata[key] = value
	}
	r.events = append(r.events, copy)
	return nil
}

func testSettingsStore(t *testing.T) *store.SettingsStore {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.NewSettingsStore(database)
}

func testSettingsRouter(t *testing.T, defaultRetention time.Duration, extras ...any) (*http.ServeMux, *store.SettingsStore) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	nodes := store.NewNodeStore(database)
	metrics := store.NewMetricStore(database)
	settings := store.NewSettingsStore(database)
	dependencies := []any{SettingsConfig{Store: settings, DefaultMetricsRetention: defaultRetention}}
	dependencies = append(dependencies, extras...)
	return NewRouter(nodes, metrics, dependencies...), settings
}

func testAIControlSettingsRouter(t *testing.T, defaultRetention time.Duration) (*http.ServeMux, *store.SettingsStore, *store.NodeStore) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	nodes := store.NewNodeStore(database)
	metrics := store.NewMetricStore(database)
	settings := store.NewSettingsStore(database)
	return NewRouter(nodes, metrics, SettingsConfig{Store: settings, DefaultMetricsRetention: defaultRetention}), settings, nodes
}

func TestSystemLogsAPIReturnsBoundedCurrentProcessLogs(t *testing.T) {
	buffer := logbuffer.New(10, 1024)
	_, _ = buffer.Write([]byte("first\nsecond\nthird\n"))
	mux, _ := testSettingsRouter(t, 6*time.Hour, buffer)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/system/logs?lines=2", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Content       string    `json:"content"`
		Lines         int       `json:"lines"`
		ReturnedLines int       `json:"returned_lines"`
		CollectedAt   time.Time `json:"collected_at"`
		StartedAt     time.Time `json:"started_at"`
		Truncated     bool      `json:"truncated"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode logs response: %v", err)
	}
	if response.Content != "first\nsecond\nthird\n" || response.Lines != 20 || response.ReturnedLines != 3 {
		t.Fatalf("response = %#v", response)
	}
	if response.CollectedAt.IsZero() || response.StartedAt.IsZero() {
		t.Fatalf("timestamps = %#v", response)
	}
}

func TestSystemLogsAPIRejectsInvalidRequestsAndUnavailableSource(t *testing.T) {
	mux, _ := testSettingsRouter(t, 6*time.Hour)

	unavailable := httptest.NewRecorder()
	mux.ServeHTTP(unavailable, httptest.NewRequest(http.MethodGet, "/api/system/logs", nil))
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d", unavailable.Code)
	}

	buffer := logbuffer.New(10, 1024)
	mux, _ = testSettingsRouter(t, 6*time.Hour, buffer)
	invalid := httptest.NewRecorder()
	mux.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/system/logs?lines=wat", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d", invalid.Code)
	}

	method := httptest.NewRecorder()
	mux.ServeHTTP(method, httptest.NewRequest(http.MethodPost, "/api/system/logs", nil))
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method status/allow = %d/%q", method.Code, method.Header().Get("Allow"))
	}
}

func TestSettingsAPIReadsAndUpdatesMetricsRetention(t *testing.T) {
	mux, _ := testSettingsRouter(t, 6*time.Hour)

	getRecorder := httptest.NewRecorder()
	mux.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d", getRecorder.Code)
	}
	var initial struct {
		MetricsRetention string `json:"metrics_retention"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial settings: %v", err)
	}
	if initial.MetricsRetention != "6h" {
		t.Fatalf("initial retention = %q, want 6h", initial.MetricsRetention)
	}

	body := bytes.NewBufferString(`{"metrics_retention":"24h"}`)
	putRecorder := httptest.NewRecorder()
	putRequest := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	putRequest.Host = "panel.example"
	putRequest.Header.Set("Origin", "http://panel.example")
	putRequest.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(putRecorder, putRequest)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", putRecorder.Code, putRecorder.Body.String())
	}
	var updated struct {
		MetricsRetention string `json:"metrics_retention"`
	}
	if err := json.Unmarshal(putRecorder.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated settings: %v", err)
	}
	if updated.MetricsRetention != "24h" {
		t.Fatalf("updated retention = %q, want 24h", updated.MetricsRetention)
	}
}

func TestSettingsAPIRejectsRetentionOverSevenDays(t *testing.T) {
	mux, _ := testSettingsRouter(t, 6*time.Hour)
	body := bytes.NewBufferString(`{"metrics_retention":"8d"}`)
	request := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestSettingsAPIPersistsScopedAIControlPolicy(t *testing.T) {
	mux, settings, nodes := testAIControlSettingsRouter(t, 6*time.Hour)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-online", Name: "online", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert online node: %v", err)
	}

	body := bytes.NewBufferString(`{"ai_control":{"mode":"low_risk_auto","allowed_actions":["docker.container.restart"],"node_scope":["node-online"]}}`)
	request := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		AIControl struct {
			Mode             store.AIControlMode `json:"mode"`
			AllowedActions   []string            `json:"allowed_actions"`
			NodeScope        []string            `json:"node_scope"`
			Revision         int64               `json:"revision"`
			ScopedNodeCount  int                 `json:"scoped_node_count"`
			EmergencyStopped bool                `json:"emergency_stopped"`
		} `json:"ai_control"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.AIControl.Mode != store.AIControlLowRiskAuto || response.AIControl.Revision != 2 ||
		response.AIControl.ScopedNodeCount != 1 || response.AIControl.EmergencyStopped {
		t.Fatalf("ai_control = %+v", response.AIControl)
	}

	reloaded, err := settings.AIControlPolicy(t.Context())
	if err != nil || reloaded.Mode != store.AIControlLowRiskAuto || len(reloaded.NodeScope) != 1 {
		t.Fatalf("reloaded policy = %+v, %v", reloaded, err)
	}
}

func TestSettingsAPIAuditsAIControlPolicyWithoutScopesOrActions(t *testing.T) {
	mux, _ := testSettingsRouter(t, 6*time.Hour)
	auditRecorder := &settingsAuditRecorder{}
	handler := serveraudit.Middleware(auditRecorder, mux)
	body := bytes.NewBufferString(`{"ai_control":{"mode":"paused","allowed_actions":["docker.container.restart","systemd.service.start"],"node_scope":["node-secret-one","node-secret-two"]}}`)
	request := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(auditRecorder.events) != 1 {
		t.Fatalf("audit events = %+v", auditRecorder.events)
	}
	event := auditRecorder.events[0]
	expected := map[string]string{
		"ai_control_mode":      "paused",
		"allowed_action_count": "2",
		"scoped_node_count":    "2",
		"policy_revision":      "2",
	}
	if event.Module != "settings" || event.Action != "update" || event.Result != serveraudit.ResultSuccess || len(event.Metadata) != len(expected) {
		t.Fatalf("audit event = %+v", event)
	}
	for key, value := range expected {
		if event.Metadata[key] != value {
			t.Errorf("metadata[%q] = %q, want %q", key, event.Metadata[key], value)
		}
	}
	serialized, err := json.Marshal(event.Metadata)
	if err != nil {
		t.Fatalf("marshal audit metadata: %v", err)
	}
	for _, forbidden := range []string{"docker.container.restart", "systemd.service.start", "node-secret-one", "node-secret-two"} {
		if bytes.Contains(serialized, []byte(forbidden)) {
			t.Errorf("audit metadata leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestSettingsAPIRejectsOfflineScopeAndStrictJSONButAlwaysAllowsPause(t *testing.T) {
	mux, _, nodes := testAIControlSettingsRouter(t, 6*time.Hour)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-offline", Name: "offline", Status: "offline", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert offline node: %v", err)
	}

	for _, body := range []string{
		`{"ai_control":{"mode":"low_risk_auto","allowed_actions":["docker.container.start"],"node_scope":["node-offline"]}}`,
		`{"metrics_retention":"24h","ai_control":{"mode":"confirm_all","allowed_actions":[],"node_scope":[]}}`,
		`{"ai_control":{"mode":"confirm_all"}}`,
		`{"ai_control":{"mode":"confirm_all","allowed_actions":null,"node_scope":[]}}`,
		`{"ai_control":{"mode":"confirm_all","allowed_actions":[],"node_scope":null}}`,
		`{"ai_control":{"mode":"confirm_all","allowed_actions":[],"node_scope":[],"unknown":true}}`,
		`{"ai_control":{"mode":"confirm_all","allowed_actions":[],"node_scope":[]}} {}`,
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
		request.Host = "panel.example"
		request.Header.Set("Origin", "http://panel.example")
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400", body, recorder.Code)
		}
	}

	pause := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(
		`{"ai_control":{"mode":"paused","allowed_actions":["docker.container.start"],"node_scope":["node-offline","node-deleted"]}}`,
	))
	pause.Host = "panel.example"
	pause.Header.Set("Origin", "http://panel.example")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, pause)
	if recorder.Code != http.StatusOK {
		t.Fatalf("pause status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestSystemAboutAPIReturnsVersionAndRepository(t *testing.T) {
	mux, _ := testSettingsRouter(t, 6*time.Hour)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/system/about", nil)

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Version   string `json:"version"`
		GitHubURL string `json:"github_url"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode about response: %v", err)
	}
	if response.Version != readVersion() {
		t.Fatalf("version = %q, want %q", response.Version, readVersion())
	}
	if response.GitHubURL != "https://github.com/LeoKon3/MizuPanel" {
		t.Fatalf("github_url = %q", response.GitHubURL)
	}
}
