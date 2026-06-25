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

func testSettingsRouter(t *testing.T, defaultRetention time.Duration) (*http.ServeMux, *store.SettingsStore) {
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
	return NewRouter(nodes, metrics, SettingsConfig{Store: settings, DefaultMetricsRetention: defaultRetention}), settings
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
	if response.Version != "0.1.0" {
		t.Fatalf("version = %q, want 0.1.0", response.Version)
	}
	if response.GitHubURL != "https://github.com/LeoKon3/MizuPanel" {
		t.Fatalf("github_url = %q", response.GitHubURL)
	}
}
