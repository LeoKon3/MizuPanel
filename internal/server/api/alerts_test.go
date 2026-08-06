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

func testAlertRouter(t *testing.T) (*http.ServeMux, *store.AlertStore) {
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
	alerts := store.NewAlertStore(database)
	mux := NewRouter(nodes, metrics, alerts)
	return mux, alerts
}

func TestCreateAlertRule(t *testing.T) {
	router, _ := testAlertRouter(t)

	rule := map[string]interface{}{
		"name":             "CPU Usage High",
		"enabled":          true,
		"metric_field":     "cpu_usage",
		"operator":         ">",
		"threshold":        80.0,
		"duration_seconds": 300,
		"scope_type":       "all",
		"notification_channels": []map[string]interface{}{
			{"type": "webhook", "webhook_url": "http://example.com/webhook"},
		},
	}
	body, _ := json.Marshal(rule)
	req := httptest.NewRequest("POST", "/api/alerts/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}

	var response store.AlertRule
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID == 0 {
		t.Fatal("response.ID = 0, want non-zero")
	}
	if response.Name != "CPU Usage High" {
		t.Fatalf("response.Name = %q, want CPU Usage High", response.Name)
	}
	if !response.Enabled {
		t.Fatal("response.Enabled = false, want true")
	}
}

func TestListAlertRules(t *testing.T) {
	router, s := testAlertRouter(t)

	// Create test rules
	rule1 := &store.AlertRule{
		Name:            "Rule 1",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 300,
		ScopeType:       "all",
		NotificationChannels: []store.NotificationChannel{
			{Type: "webhook", WebhookURL: "http://example.com/webhook"},
		},
	}
	if err := s.CreateAlertRule(rule1); err != nil {
		t.Fatalf("create rule1: %v", err)
	}

	rule2 := &store.AlertRule{
		Name:            "Rule 2",
		Enabled:         false,
		MetricField:     "memory_usage",
		Operator:        ">",
		Threshold:       90.0,
		DurationSeconds: 180,
		ScopeType:       "nodes",
		ScopeNodeIDs:    []string{"node-1", "node-2"},
		NotificationChannels: []store.NotificationChannel{
			{Type: "dingtalk", WebhookURL: "http://dingtalk.com/webhook", Secret: "secret"},
		},
	}
	if err := s.CreateAlertRule(rule2); err != nil {
		t.Fatalf("create rule2: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/alerts/rules", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var response struct {
		Rules []store.AlertRule `json:"rules"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(response.Rules))
	}
	if response.Rules[0].Name != "Rule 1" || response.Rules[1].Name != "Rule 2" {
		t.Fatalf("rules = %+v", response.Rules)
	}
}

func TestGetAlertRule(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "Test Rule",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 300,
		ScopeType:       "all",
		NotificationChannels: []store.NotificationChannel{
			{Type: "webhook", WebhookURL: "http://example.com/webhook"},
		},
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/alerts/rules/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var response store.AlertRule
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != rule.ID {
		t.Fatalf("response.ID = %d, want %d", response.ID, rule.ID)
	}
	if response.Name != "Test Rule" {
		t.Fatalf("response.Name = %q, want Test Rule", response.Name)
	}
}

func TestUpdateAlertRule(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "Original Rule",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 300,
		ScopeType:       "all",
		NotificationChannels: []store.NotificationChannel{
			{Type: "webhook", WebhookURL: "http://example.com/webhook"},
		},
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	update := map[string]interface{}{
		"name":             "Updated Rule",
		"enabled":          false,
		"metric_field":     "memory_usage",
		"operator":         ">=",
		"threshold":        90.0,
		"duration_seconds": 600,
		"scope_type":       "nodes",
		"scope_node_ids":   []string{"node-1"},
		"notification_channels": []map[string]interface{}{
			{"type": "dingtalk", "webhook_url": "http://dingtalk.com/webhook"},
		},
	}
	body, _ := json.Marshal(update)
	req := httptest.NewRequest("PUT", "/api/alerts/rules/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var response store.AlertRule
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Name != "Updated Rule" || response.Enabled || response.Threshold != 90.0 {
		t.Fatalf("response = %+v", response)
	}
}

func TestDeleteAlertRule(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "Rule to Delete",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 300,
		ScopeType:       "all",
		NotificationChannels: []store.NotificationChannel{
			{Type: "webhook", WebhookURL: "http://example.com/webhook"},
		},
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/api/alerts/rules/1", nil)
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}

	// Verify rule is deleted
	deletedRule, err := s.GetAlertRule(rule.ID)
	if err != nil {
		t.Fatalf("get deleted rule: %v", err)
	}
	if deletedRule != nil {
		t.Fatal("rule still exists after delete")
	}
}

func TestToggleAlertRule(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "Toggle Rule",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 300,
		ScopeType:       "all",
		NotificationChannels: []store.NotificationChannel{
			{Type: "webhook", WebhookURL: "http://example.com/webhook"},
		},
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	// Toggle to disabled
	toggle := map[string]interface{}{"enabled": false}
	body, _ := json.Marshal(toggle)
	req := httptest.NewRequest("PATCH", "/api/alerts/rules/1/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var response store.AlertRule
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Enabled {
		t.Fatal("response.Enabled = true, want false after toggle")
	}
}

func TestToggleAlertRuleResolvesActiveAlertsWhenDisabled(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "Toggle Rule",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 0,
		ScopeType:       "all",
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := s.CreateAlertHistory(&store.AlertHistory{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		NodeID:      "node-1",
		NodeName:    "Node 1",
		MetricField: "cpu_usage",
		MetricValue: 95,
		Threshold:   80,
		TriggeredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create active history: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{"enabled": false})
	req := httptest.NewRequest("PATCH", "/api/alerts/rules/1/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	active, err := s.GetActiveAlertHistory()
	if err != nil {
		t.Fatalf("get active alerts: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active alerts = %d, want 0 after disabling rule", len(active))
	}
	history, err := s.GetAlertHistory("node-1", 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 1 || history[0].ResolvedAt == nil {
		t.Fatalf("history = %+v, want disabled rule alert resolved", history)
	}
}

func TestUpdateAlertRuleRenamesActiveHistory(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "Old Rule",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 0,
		ScopeType:       "all",
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := s.CreateAlertHistory(&store.AlertHistory{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		NodeID:      "node-1",
		NodeName:    "Node 1",
		MetricField: "cpu_usage",
		MetricValue: 95,
		Threshold:   80,
		TriggeredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create active history: %v", err)
	}

	update := map[string]interface{}{
		"name":                  "New Rule",
		"enabled":               true,
		"metric_field":          "cpu_usage",
		"operator":              ">",
		"threshold":             80.0,
		"duration_seconds":      0,
		"scope_type":            "all",
		"notification_channels": []map[string]interface{}{},
	}
	body, _ := json.Marshal(update)
	req := httptest.NewRequest("PUT", "/api/alerts/rules/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	history, err := s.GetAlertHistory("node-1", 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 1 || history[0].RuleName != "New Rule" {
		t.Fatalf("history = %+v, want active history rule name updated", history)
	}
}

func TestUpdateAlertRuleRenamesResolvedHistory(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "Old Rule",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 0,
		ScopeType:       "all",
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	resolvedAt := time.Now().UTC()
	if err := s.CreateAlertHistory(&store.AlertHistory{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		NodeID:      "node-1",
		NodeName:    "Node 1",
		MetricField: "cpu_usage",
		MetricValue: 95,
		Threshold:   80,
		TriggeredAt: resolvedAt.Add(-time.Minute),
		ResolvedAt:  &resolvedAt,
	}); err != nil {
		t.Fatalf("create resolved history: %v", err)
	}

	update := map[string]interface{}{
		"name":                  "New Rule",
		"enabled":               true,
		"metric_field":          "cpu_usage",
		"operator":              ">",
		"threshold":             80.0,
		"duration_seconds":      0,
		"scope_type":            "all",
		"notification_channels": []map[string]interface{}{},
	}
	body, _ := json.Marshal(update)
	req := httptest.NewRequest("PUT", "/api/alerts/rules/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	history, err := s.GetAlertHistory("node-1", 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 1 || history[0].RuleName != "New Rule" {
		t.Fatalf("history = %+v, want resolved history rule name updated", history)
	}
}

func TestResolveAlertHistory(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "CPU High",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 0,
		ScopeType:       "all",
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	history := &store.AlertHistory{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		NodeID:      "node-1",
		NodeName:    "Node 1",
		MetricField: "cpu_usage",
		MetricValue: 95,
		Threshold:   80,
		TriggeredAt: time.Now().UTC(),
	}
	if err := s.CreateAlertHistory(history); err != nil {
		t.Fatalf("create active history: %v", err)
	}

	req := httptest.NewRequest("PATCH", "/api/alerts/history/1/resolve", nil)
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var response store.AlertHistory
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != history.ID || response.ResolvedAt == nil {
		t.Fatalf("response = %+v, want resolved history %d", response, history.ID)
	}

	active, err := s.GetActiveAlertHistory()
	if err != nil {
		t.Fatalf("get active alerts: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active alerts = %d, want 0 after manual resolve", len(active))
	}
}

func TestDeleteResolvedAlertHistory(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "CPU High",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 0,
		ScopeType:       "all",
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	resolvedAt := time.Now().UTC()
	if err := s.CreateAlertHistory(&store.AlertHistory{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		NodeID:      "node-1",
		NodeName:    "Node 1",
		MetricField: "cpu_usage",
		MetricValue: 95,
		Threshold:   80,
		TriggeredAt: resolvedAt.Add(-time.Minute),
		ResolvedAt:  &resolvedAt,
	}); err != nil {
		t.Fatalf("create resolved history: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/api/alerts/history/1", nil)
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	history, err := s.GetAlertHistory("node-1", 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("history = %+v, want deleted resolved alert removed", history)
	}
}

func TestDeleteAlertHistoryRejectsActiveAlert(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "CPU High",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 0,
		ScopeType:       "all",
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := s.CreateAlertHistory(&store.AlertHistory{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		NodeID:      "node-1",
		NodeName:    "Node 1",
		MetricField: "cpu_usage",
		MetricValue: 95,
		Threshold:   80,
		TriggeredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create active history: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/api/alerts/history/1", nil)
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	history, err := s.GetAlertHistory("node-1", 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 1 || history[0].ResolvedAt != nil {
		t.Fatalf("history = %+v, want active alert kept", history)
	}
}

func TestDeleteResolvedAlertHistoryBatch(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "CPU High",
		Enabled:         true,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 0,
		ScopeType:       "all",
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	resolvedAt := time.Now().UTC()
	for index := 0; index < 2; index++ {
		if err := s.CreateAlertHistory(&store.AlertHistory{
			RuleID:      rule.ID,
			RuleName:    rule.Name,
			NodeID:      "node-1",
			NodeName:    "Node 1",
			MetricField: "cpu_usage",
			MetricValue: 95 + float64(index),
			Threshold:   80,
			TriggeredAt: resolvedAt.Add(time.Duration(-index-1) * time.Minute),
			ResolvedAt:  &resolvedAt,
		}); err != nil {
			t.Fatalf("create resolved history %d: %v", index, err)
		}
	}

	body, _ := json.Marshal(map[string]interface{}{"ids": []int64{1, 2}})
	req := httptest.NewRequest("DELETE", "/api/alerts/history", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var response struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", response.Deleted)
	}
	history, err := s.GetAlertHistory("node-1", 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("history = %+v, want all selected resolved alerts removed", history)
	}
}

func TestListAlertHistoryResolvesAlertsForDisabledRules(t *testing.T) {
	router, s := testAlertRouter(t)

	rule := &store.AlertRule{
		Name:            "Disabled CPU High",
		Enabled:         false,
		MetricField:     "cpu_usage",
		Operator:        ">",
		Threshold:       80.0,
		DurationSeconds: 0,
		ScopeType:       "all",
	}
	if err := s.CreateAlertRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := s.CreateAlertHistory(&store.AlertHistory{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		NodeID:      "node-1",
		NodeName:    "Node 1",
		MetricField: "cpu_usage",
		MetricValue: 95,
		Threshold:   80,
		TriggeredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create active history: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/alerts/history?node_id=node-1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var response struct {
		History []store.AlertHistory `json:"history"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.History) != 1 || response.History[0].ResolvedAt == nil {
		t.Fatalf("history = %+v, want disabled rule alert resolved during listing", response.History)
	}
}

func TestListAlertHistoryReturnsEmptyArrayWhenNoHistoryExists(t *testing.T) {
	router, _ := testAlertRouter(t)

	req := httptest.NewRequest("GET", "/api/alerts/history?node_id=node-without-history", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var response struct {
		History []store.AlertHistory `json:"history"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.History == nil {
		t.Fatal("history = null, want an empty array")
	}
	if len(response.History) != 0 {
		t.Fatalf("history = %+v, want empty history", response.History)
	}
}

func TestCreateAlertRuleRejectsInvalidMetricField(t *testing.T) {
	router, _ := testAlertRouter(t)

	rule := map[string]interface{}{
		"name":             "Invalid Rule",
		"enabled":          true,
		"metric_field":     "invalid_field",
		"operator":         ">",
		"threshold":        80.0,
		"duration_seconds": 300,
		"scope_type":       "all",
		"notification_channels": []map[string]interface{}{
			{"type": "webhook", "webhook_url": "http://example.com/webhook"},
		},
	}
	body, _ := json.Marshal(rule)
	req := httptest.NewRequest("POST", "/api/alerts/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestCreateAlertRuleRejectsInvalidOperator(t *testing.T) {
	router, _ := testAlertRouter(t)

	rule := map[string]interface{}{
		"name":             "Invalid Rule",
		"enabled":          true,
		"metric_field":     "cpu_usage",
		"operator":         "invalid",
		"threshold":        80.0,
		"duration_seconds": 300,
		"scope_type":       "all",
		"notification_channels": []map[string]interface{}{
			{"type": "webhook", "webhook_url": "http://example.com/webhook"},
		},
	}
	body, _ := json.Marshal(rule)
	req := httptest.NewRequest("POST", "/api/alerts/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
