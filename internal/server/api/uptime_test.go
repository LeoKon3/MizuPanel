package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
	serveruptime "github.com/mizupanel/mizupanel/internal/server/uptime"
)

type apiUptimeChecker struct {
	monitors    *store.UptimeStore
	probe       store.UptimeProbeResult
	err         error
	mutationErr error
}

func (checker *apiUptimeChecker) CheckNow(ctx context.Context, monitorID int64) (store.UptimeMonitor, error) {
	if checker.err != nil {
		return store.UptimeMonitor{}, checker.err
	}
	monitor, err := checker.monitors.GetMonitor(ctx, monitorID)
	if err != nil {
		return store.UptimeMonitor{}, err
	}
	if monitor == nil {
		return store.UptimeMonitor{}, serveruptime.ErrMonitorNotFound
	}
	if !monitor.Enabled {
		return store.UptimeMonitor{}, serveruptime.ErrMonitorDisabled
	}
	probe := checker.probe
	if probe.CheckedAt.IsZero() {
		probe.CheckedAt = time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	}
	transition, err := checker.monitors.ApplyProbe(ctx, monitorID, probe)
	return transition.Monitor, err
}

func (checker *apiUptimeChecker) BeginMonitorMutation(_ int64) (func(), error) {
	if checker.mutationErr != nil {
		return nil, checker.mutationErr
	}
	return func() {}, nil
}

func testUptimeRouter(t *testing.T, auth AuthConfig) (*http.ServeMux, *store.UptimeStore, *apiUptimeChecker, *sql.DB) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	nodes := store.NewNodeStore(database)
	metrics := store.NewMetricStore(database)
	monitors := store.NewUptimeStore(database)
	checker := &apiUptimeChecker{monitors: monitors, probe: store.UptimeProbeResult{Success: true, StatusCode: 200, LatencyMS: 12}}
	router := NewRouter(nodes, metrics, UptimeConfig{Store: monitors, Checker: checker}, auth)
	return router, monitors, checker, database
}

func uptimeJSONRequest(t *testing.T, method string, path string, value any) *http.Request {
	t.Helper()
	var body bytes.Buffer
	if value != nil {
		if err := json.NewEncoder(&body).Encode(value); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Origin", "http://"+request.Host)
	if value != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func createUptimeMonitorThroughAPI(t *testing.T, router http.Handler, overrides map[string]any) store.UptimeMonitor {
	t.Helper()
	payload := map[string]any{
		"name":                      "Website",
		"type":                      "http",
		"target":                    "https://example.com/health",
		"failure_threshold":         1,
		"notification_channels":     []any{},
		"tls_expiry_threshold_days": 30,
	}
	for key, value := range overrides {
		payload[key] = value
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, uptimeJSONRequest(t, http.MethodPost, "/api/uptime/monitors", payload))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var monitor store.UptimeMonitor
	if err := json.NewDecoder(recorder.Body).Decode(&monitor); err != nil {
		t.Fatalf("decode monitor: %v", err)
	}
	return monitor
}

func TestUptimeAPIEmptyCollectionsAreTypedArrays(t *testing.T) {
	router, _, _, _ := testUptimeRouter(t, AuthConfig{})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/uptime/monitors", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Monitors []store.UptimeMonitor `json:"monitors"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if response.Monitors == nil || len(response.Monitors) != 0 {
		t.Fatalf("monitors = %#v, want non-nil empty array", response.Monitors)
	}
}

func TestUptimeAPICRUDToggleCheckAndHistory(t *testing.T) {
	router, _, checker, _ := testUptimeRouter(t, AuthConfig{})
	monitor := createUptimeMonitorThroughAPI(t, router, nil)
	if monitor.ID == 0 || monitor.Status != store.UptimeStatusPending || !monitor.Enabled || monitor.IntervalSeconds != 60 || monitor.TimeoutSeconds != 5 {
		t.Fatalf("created monitor = %+v", monitor)
	}

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/uptime/monitors/"+strconvFormat(monitor.ID), nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}

	update := map[string]any{
		"name": "Updated Website", "type": "http", "target": "https://example.net/health",
		"interval_seconds": 120, "timeout_seconds": 10, "failure_threshold": 1,
		"expected_status_min": 200, "expected_status_max": 299, "tls_expiry_threshold_days": 14,
		"notification_channels": []any{},
	}
	updateRecorder := httptest.NewRecorder()
	router.ServeHTTP(updateRecorder, uptimeJSONRequest(t, http.MethodPut, "/api/uptime/monitors/"+strconvFormat(monitor.ID), update))
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updated store.UptimeMonitor
	if err := json.NewDecoder(updateRecorder.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Name != "Updated Website" || updated.Target != "https://example.net/health" || updated.Status != store.UptimeStatusPending {
		t.Fatalf("updated monitor = %+v", updated)
	}

	toggleRecorder := httptest.NewRecorder()
	router.ServeHTTP(toggleRecorder, uptimeJSONRequest(t, http.MethodPatch, "/api/uptime/monitors/"+strconvFormat(monitor.ID)+"/toggle", map[string]any{"enabled": false}))
	if toggleRecorder.Code != http.StatusOK {
		t.Fatalf("toggle status=%d body=%s", toggleRecorder.Code, toggleRecorder.Body.String())
	}
	var toggled store.UptimeMonitor
	if err := json.NewDecoder(toggleRecorder.Body).Decode(&toggled); err != nil || toggled.Enabled {
		t.Fatalf("toggled=%+v err=%v", toggled, err)
	}

	checker.probe = store.UptimeProbeResult{Error: "连接超时", LatencyMS: 5000, CheckedAt: time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)}
	disabledCheckRecorder := httptest.NewRecorder()
	router.ServeHTTP(disabledCheckRecorder, uptimeJSONRequest(t, http.MethodPost, "/api/uptime/monitors/"+strconvFormat(monitor.ID)+"/check", nil))
	if disabledCheckRecorder.Code != http.StatusConflict {
		t.Fatalf("disabled check status=%d body=%s", disabledCheckRecorder.Code, disabledCheckRecorder.Body.String())
	}

	reenableRecorder := httptest.NewRecorder()
	router.ServeHTTP(reenableRecorder, uptimeJSONRequest(t, http.MethodPatch, "/api/uptime/monitors/"+strconvFormat(monitor.ID)+"/toggle", map[string]any{"enabled": true}))
	if reenableRecorder.Code != http.StatusOK {
		t.Fatalf("re-enable status=%d body=%s", reenableRecorder.Code, reenableRecorder.Body.String())
	}
	checkRecorder := httptest.NewRecorder()
	router.ServeHTTP(checkRecorder, uptimeJSONRequest(t, http.MethodPost, "/api/uptime/monitors/"+strconvFormat(monitor.ID)+"/check", nil))
	if checkRecorder.Code != http.StatusOK {
		t.Fatalf("check status=%d body=%s", checkRecorder.Code, checkRecorder.Body.String())
	}
	var checked store.UptimeMonitor
	if err := json.NewDecoder(checkRecorder.Body).Decode(&checked); err != nil || checked.Status != store.UptimeStatusDown {
		t.Fatalf("checked=%+v err=%v", checked, err)
	}

	for _, history := range []struct {
		path string
		key  string
	}{
		{path: "results?limit=10", key: "results"},
		{path: "incidents?limit=10", key: "incidents"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/uptime/monitors/"+strconvFormat(monitor.ID)+"/"+history.path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", history.path, recorder.Code, recorder.Body.String())
		}
		var body map[string][]json.RawMessage
		if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil || len(body[history.key]) != 1 {
			t.Fatalf("%s body=%v err=%v", history.path, body, err)
		}
	}

	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, uptimeJSONRequest(t, http.MethodDelete, "/api/uptime/monitors/"+strconvFormat(monitor.ID), nil))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	missingRecorder := httptest.NewRecorder()
	router.ServeHTTP(missingRecorder, httptest.NewRequest(http.MethodGet, "/api/uptime/monitors/"+strconvFormat(monitor.ID), nil))
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
}

func TestUptimeAPIMutationsConflictWithActiveCheck(t *testing.T) {
	router, _, checker, _ := testUptimeRouter(t, AuthConfig{})
	monitor := createUptimeMonitorThroughAPI(t, router, nil)
	checker.mutationErr = serveruptime.ErrCheckInProgress

	tests := []struct {
		name    string
		request *http.Request
	}{
		{
			name: "update",
			request: uptimeJSONRequest(t, http.MethodPut, "/api/uptime/monitors/"+strconvFormat(monitor.ID), map[string]any{
				"name": "Updated", "type": "http", "target": "https://example.com/updated",
				"interval_seconds": 60, "timeout_seconds": 5, "failure_threshold": 1,
				"expected_status_min": 200, "expected_status_max": 399, "tls_expiry_threshold_days": 30,
				"notification_channels": []any{},
			}),
		},
		{name: "toggle", request: uptimeJSONRequest(t, http.MethodPatch, "/api/uptime/monitors/"+strconvFormat(monitor.ID)+"/toggle", map[string]any{"enabled": false})},
		{name: "delete", request: uptimeJSONRequest(t, http.MethodDelete, "/api/uptime/monitors/"+strconvFormat(monitor.ID), nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, test.request)
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestUptimeAPIValidationBounds(t *testing.T) {
	router, _, _, _ := testUptimeRouter(t, AuthConfig{})
	tests := []struct {
		name  string
		field string
		value any
	}{
		{name: "name", field: "name", value: strings.Repeat("界", serveruptime.MaxMonitorNameRunes+1)},
		{name: "target", field: "target", value: "https://example.com/" + strings.Repeat("a", serveruptime.MaxMonitorTargetBytes)},
		{name: "interval low", field: "interval_seconds", value: 29},
		{name: "interval high", field: "interval_seconds", value: 86401},
		{name: "timeout low", field: "timeout_seconds", value: -1},
		{name: "timeout high", field: "timeout_seconds", value: 31},
		{name: "failure low", field: "failure_threshold", value: -1},
		{name: "failure high", field: "failure_threshold", value: 11},
		{name: "status low", field: "expected_status_min", value: 99},
		{name: "status high", field: "expected_status_max", value: 600},
		{name: "TLS low", field: "tls_expiry_threshold_days", value: -1},
		{name: "TLS high", field: "tls_expiry_threshold_days", value: 366},
		{name: "type", field: "type", value: "icmp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := map[string]any{
				"name": "Website", "type": "http", "target": "https://example.com", "interval_seconds": 60,
				"timeout_seconds": 5, "failure_threshold": 3, "expected_status_min": 200,
				"expected_status_max": 399, "tls_expiry_threshold_days": 30, "notification_channels": []any{},
			}
			payload[test.field] = test.value
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, uptimeJSONRequest(t, http.MethodPost, "/api/uptime/monitors", payload))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestUptimeAPIMutationGuardsAndMethodContract(t *testing.T) {
	router, _, _, _ := testUptimeRouter(t, AuthConfig{})
	validBody := `{"name":"Website","type":"http","target":"https://example.com"}`
	tests := []struct {
		name       string
		request    *http.Request
		wantStatus int
	}{
		{name: "missing origin", request: httptest.NewRequest(http.MethodPost, "/api/uptime/monitors", strings.NewReader(validBody)), wantStatus: http.StatusForbidden},
		{name: "cross origin", request: func() *http.Request {
			request := httptest.NewRequest(http.MethodPost, "/api/uptime/monitors", strings.NewReader(validBody))
			request.Header.Set("Origin", "http://evil.example")
			request.Header.Set("Content-Type", "application/json")
			return request
		}(), wantStatus: http.StatusForbidden},
		{name: "wrong content type", request: func() *http.Request {
			request := httptest.NewRequest(http.MethodPost, "/api/uptime/monitors", strings.NewReader(validBody))
			request.Header.Set("Origin", "http://"+request.Host)
			request.Header.Set("Content-Type", "text/plain")
			return request
		}(), wantStatus: http.StatusForbidden},
		{name: "unknown field", request: uptimeJSONRequest(t, http.MethodPost, "/api/uptime/monitors", map[string]any{"name": "Website", "type": "http", "target": "https://example.com", "unknown": true}), wantStatus: http.StatusBadRequest},
		{name: "trailing JSON", request: func() *http.Request {
			request := httptest.NewRequest(http.MethodPost, "/api/uptime/monitors", strings.NewReader(validBody+` {}`))
			request.Header.Set("Origin", "http://"+request.Host)
			request.Header.Set("Content-Type", "application/json")
			return request
		}(), wantStatus: http.StatusBadRequest},
		{name: "body too large", request: func() *http.Request {
			request := httptest.NewRequest(http.MethodPost, "/api/uptime/monitors", strings.NewReader(`{"name":"`+strings.Repeat("a", maxUptimeRequestBodyBytes)+`"}`))
			request.Header.Set("Origin", "http://"+request.Host)
			request.Header.Set("Content-Type", "application/json")
			return request
		}(), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, test.request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s want=%d", recorder.Code, recorder.Body.String(), test.wantStatus)
			}
		})
	}

	methodRecorder := httptest.NewRecorder()
	router.ServeHTTP(methodRecorder, httptest.NewRequest(http.MethodPatch, "/api/uptime/monitors", nil))
	if methodRecorder.Code != http.StatusMethodNotAllowed || methodRecorder.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("method status=%d allow=%q", methodRecorder.Code, methodRecorder.Header().Get("Allow"))
	}
}

func TestUptimeAPIMissingConflictHistoryLimitAndAuth(t *testing.T) {
	router, _, checker, _ := testUptimeRouter(t, AuthConfig{})
	monitor := createUptimeMonitorThroughAPI(t, router, nil)
	checker.err = serveruptime.ErrCheckInProgress
	conflict := httptest.NewRecorder()
	router.ServeHTTP(conflict, uptimeJSONRequest(t, http.MethodPost, "/api/uptime/monitors/"+strconvFormat(monitor.ID)+"/check", nil))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	for _, path := range []string{
		"/api/uptime/monitors/999",
		"/api/uptime/monitors/999/results",
		"/api/uptime/monitors/999/incidents",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
	invalidLimit := httptest.NewRecorder()
	router.ServeHTTP(invalidLimit, httptest.NewRequest(http.MethodGet, "/api/uptime/monitors/"+strconvFormat(monitor.ID)+"/results?limit=201", nil))
	if invalidLimit.Code != http.StatusBadRequest {
		t.Fatalf("limit status=%d body=%s", invalidLimit.Code, invalidLimit.Body.String())
	}

	authRouter, _, _, _ := testUptimeRouter(t, AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour})
	unauthorized := httptest.NewRecorder()
	authRouter.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/uptime/monitors", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
}

func TestUptimeAPIUnexpectedStoreErrorIsGeneric(t *testing.T) {
	router, _, _, database := testUptimeRouter(t, AuthConfig{})
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/uptime/monitors", nil))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(strings.ToLower(recorder.Body.String()), "sql") || strings.Contains(strings.ToLower(recorder.Body.String()), "closed") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil || response["error"] != "internal server error" {
		t.Fatalf("response=%v err=%v", response, err)
	}
}

func strconvFormat(value int64) string {
	return strconv.FormatInt(value, 10)
}
