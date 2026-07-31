package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/logbuffer"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

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
