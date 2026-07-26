package db

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrateSQLiteCreatesAlertTables(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Verify alert_rules table exists
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='alert_rules'").Scan(&tableName)
	if err != nil {
		t.Fatalf("alert_rules table not found: %v", err)
	}
	if tableName != "alert_rules" {
		t.Fatalf("table name = %q, want alert_rules", tableName)
	}

	// Verify alert_history table exists
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='alert_history'").Scan(&tableName)
	if err != nil {
		t.Fatalf("alert_history table not found: %v", err)
	}
	if tableName != "alert_history" {
		t.Fatalf("table name = %q, want alert_history", tableName)
	}

	// Verify indexes exist
	var indexCount int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name LIKE 'idx_alert_history_%'").Scan(&indexCount)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	if indexCount != 3 {
		t.Fatalf("alert_history indexes count = %d, want 3", indexCount)
	}
}

func TestMigrateSQLiteCreatesReplaySafeAuditSchema(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := Migrate(database); err != nil {
		t.Fatalf("replay Migrate: %v", err)
	}

	var tableSQL string
	if err := database.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'audit_events'`).Scan(&tableSQL); err != nil {
		t.Fatalf("audit_events table not found: %v", err)
	}
	for _, column := range []string{
		"request_id", "created_at", "actor_type", "actor_name", "source_ip",
		"module", "action", "target_type", "target_id", "target_name", "node_id",
		"result", "duration_ms", "summary", "metadata_json",
	} {
		if !strings.Contains(tableSQL, column) {
			t.Errorf("audit_events schema missing %s: %s", column, tableSQL)
		}
	}

	var indexCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name LIKE 'idx_audit_events_%'`).Scan(&indexCount); err != nil {
		t.Fatalf("query audit indexes: %v", err)
	}
	if indexCount != 5 {
		t.Fatalf("audit index count = %d, want 5", indexCount)
	}
}

func TestMigrateSQLiteCreatesUptimeSchema(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, table := range []string{"uptime_monitors", "uptime_results", "uptime_incidents"} {
		var name string
		if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
	var indexCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND (name LIKE 'idx_uptime_%' OR name = 'uq_uptime_incidents_active')`).Scan(&indexCount); err != nil {
		t.Fatalf("query uptime indexes: %v", err)
	}
	if indexCount != 4 {
		t.Fatalf("uptime index count = %d, want 4", indexCount)
	}

	result, err := database.Exec(`INSERT INTO uptime_monitors (
		name, type, target, interval_seconds, timeout_seconds, failure_threshold,
		expected_status_min, expected_status_max, tls_expiry_threshold_days,
		notification_channels, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Website", "http", "https://example.com", 60, 5, 3, 200, 399, 30, "[]", "2026-07-25T00:00:00Z", "2026-07-25T00:00:00Z")
	if err != nil {
		t.Fatalf("insert monitor: %v", err)
	}
	monitorID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("monitor id: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO uptime_results (monitor_id, success, latency_ms, checked_at) VALUES (?, ?, ?, ?)`, monitorID, 1, 25, "2026-07-25T00:01:00Z"); err != nil {
		t.Fatalf("insert result: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO uptime_incidents (monitor_id, kind, message, started_at, created_at) VALUES (?, ?, ?, ?, ?)`, monitorID, "availability", "down", "2026-07-25T00:01:00Z", "2026-07-25T00:01:00Z"); err != nil {
		t.Fatalf("insert incident: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO uptime_incidents (monitor_id, kind, message, started_at, created_at) VALUES (?, ?, ?, ?, ?)`, monitorID, "availability", "still down", "2026-07-25T00:02:00Z", "2026-07-25T00:02:00Z"); err == nil {
		t.Fatal("inserted a second unresolved availability incident")
	}
	if _, err := database.Exec(`UPDATE uptime_incidents SET resolved_at = ?, active_marker = NULL WHERE monitor_id = ? AND kind = ?`, "2026-07-25T00:03:00Z", monitorID, "availability"); err != nil {
		t.Fatalf("resolve incident: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO uptime_incidents (monitor_id, kind, message, started_at, created_at) VALUES (?, ?, ?, ?, ?)`, monitorID, "availability", "down again", "2026-07-25T00:04:00Z", "2026-07-25T00:04:00Z"); err != nil {
		t.Fatalf("insert incident after resolution: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM uptime_monitors WHERE id = ?`, monitorID); err != nil {
		t.Fatalf("delete monitor: %v", err)
	}
	for _, table := range []string{"uptime_results", "uptime_incidents"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows = %d, want cascade cleanup", table, count)
		}
	}
}

func TestMigrateSQLiteUpgradesCurrentUptimeIncidentSchema(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`CREATE TABLE uptime_incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		monitor_id INTEGER NOT NULL,
		kind TEXT NOT NULL,
		message TEXT NOT NULL DEFAULT '',
		started_at DATETIME NOT NULL,
		resolved_at DATETIME,
		notification_sent INTEGER NOT NULL DEFAULT 0,
		notification_error TEXT NOT NULL DEFAULT '',
		notification_attempted_at DATETIME,
		recovery_notification_sent INTEGER NOT NULL DEFAULT 0,
		recovery_notification_error TEXT NOT NULL DEFAULT '',
		recovery_notification_attempted_at DATETIME,
		created_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatalf("create current uptime_incidents schema: %v", err)
	}
	for _, values := range []struct {
		kind       string
		startedAt  string
		resolvedAt any
	}{
		{kind: "availability", startedAt: "2026-07-25T00:01:00Z"},
		{kind: "availability", startedAt: "2026-07-25T00:02:00Z"},
		{kind: "availability", startedAt: "2026-07-25T00:03:00Z", resolvedAt: "2026-07-25T00:04:00Z"},
		{kind: "certificate", startedAt: "2026-07-25T00:05:00Z"},
	} {
		if _, err := database.Exec(`INSERT INTO uptime_incidents (monitor_id, kind, started_at, resolved_at, created_at) VALUES (?, ?, ?, ?, ?)`, 7, values.kind, values.startedAt, values.resolvedAt, values.startedAt); err != nil {
			t.Fatalf("seed %s incident: %v", values.kind, err)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate current uptime schema: %v", err)
	}
	if err := Migrate(database); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}

	rows, err := database.Query(`SELECT id, resolved_at, active_marker FROM uptime_incidents ORDER BY id`)
	if err != nil {
		t.Fatalf("query migrated incidents: %v", err)
	}
	defer rows.Close()
	type incidentState struct {
		resolved sql.NullString
		active   sql.NullInt64
	}
	states := make([]incidentState, 0, 4)
	for rows.Next() {
		var id int64
		var state incidentState
		if err := rows.Scan(&id, &state.resolved, &state.active); err != nil {
			t.Fatalf("scan migrated incident: %v", err)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migrated incidents: %v", err)
	}
	if len(states) != 4 {
		t.Fatalf("migrated incidents = %d, want 4", len(states))
	}
	if !states[0].resolved.Valid || states[0].active.Valid {
		t.Fatalf("older duplicate state = %+v, want resolved and inactive", states[0])
	}
	if states[1].resolved.Valid || !states[1].active.Valid || states[1].active.Int64 != 1 {
		t.Fatalf("newest availability state = %+v, want unresolved and active", states[1])
	}
	if !states[2].resolved.Valid || states[2].active.Valid {
		t.Fatalf("resolved incident state = %+v, want inactive", states[2])
	}
	if states[3].resolved.Valid || !states[3].active.Valid || states[3].active.Int64 != 1 {
		t.Fatalf("certificate state = %+v, want unresolved and active", states[3])
	}

	var indexSQL string
	if err := database.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'uq_uptime_incidents_active'`).Scan(&indexSQL); err != nil {
		t.Fatalf("query active incident index: %v", err)
	}
	if !strings.Contains(strings.ToUpper(indexSQL), "CREATE UNIQUE INDEX") {
		t.Fatalf("active incident index is not unique: %s", indexSQL)
	}
	if _, err := database.Exec(`INSERT INTO uptime_incidents (monitor_id, kind, started_at, created_at) VALUES (?, ?, ?, ?)`, 7, "availability", "2026-07-25T00:06:00Z", "2026-07-25T00:06:00Z"); err == nil {
		t.Fatal("migrated schema allowed a duplicate unresolved incident")
	}
}

func TestMySQLMigrationIncludesUptimeSchema(t *testing.T) {
	statements := strings.Join(mysqlMigrationStatements(), "\n")
	for _, fragment := range []string{
		"uptime_monitors",
		"uptime_results",
		"uptime_incidents",
		"idx_uptime_monitors_enabled",
		"idx_uptime_results_monitor_checked",
		"active_marker BOOLEAN DEFAULT 1",
		"notification_channels LONGTEXT",
		"notification_error VARCHAR(1024) NOT NULL DEFAULT ''",
	} {
		if !strings.Contains(statements, fragment) {
			t.Fatalf("MySQL uptime migration missing %s", fragment)
		}
	}
	compatibility := strings.Join(uptimeIncidentCompatibilityColumnStatements(DialectMySQL), "\n")
	if !strings.Contains(compatibility, "ADD COLUMN active_marker BOOLEAN DEFAULT 1") {
		t.Fatalf("MySQL uptime compatibility migration missing active marker: %s", compatibility)
	}
	invariant := strings.Join(uptimeIncidentInvariantStatements(DialectMySQL), "\n")
	for _, fragment := range []string{
		"JOIN uptime_incidents AS newer",
		"active_marker = NULL WHERE resolved_at IS NOT NULL",
		"active_marker = 1 WHERE resolved_at IS NULL",
		"CREATE UNIQUE INDEX uq_uptime_incidents_active ON uptime_incidents(monitor_id, kind, active_marker)",
	} {
		if !strings.Contains(invariant, fragment) {
			t.Fatalf("MySQL uptime invariant migration missing %s", fragment)
		}
	}
}

func TestMySQLMigrationIncludesAuditSchema(t *testing.T) {
	statements := strings.Join(mysqlMigrationStatements(), "\n")
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS audit_events",
		"id BIGINT AUTO_INCREMENT PRIMARY KEY",
		"request_id VARCHAR(64) NOT NULL",
		"target_id VARCHAR(1024) NOT NULL DEFAULT ''",
		"metadata_json TEXT NOT NULL",
		"INDEX idx_audit_events_created_id (created_at, id)",
		"INDEX idx_audit_events_module_id (module, id)",
		"INDEX idx_audit_events_node_id (node_id, id)",
		"INDEX idx_audit_events_result_id (result, id)",
		"INDEX idx_audit_events_actor_id (actor_type, actor_name, id)",
	} {
		if !strings.Contains(statements, fragment) {
			t.Errorf("MySQL audit migration missing %q", fragment)
		}
	}
}

func TestMigrateSQLiteAlertRulesSchema(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Insert a test rule
	_, err = db.Exec(`INSERT INTO alert_rules (name, enabled, metric_field, operator, threshold, duration_seconds, scope_type, notification_channels)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"Test Rule", 1, "cpu_usage", ">", 80.0, 300, "all", `[{"type":"webhook","url":"http://example.com"}]`)
	if err != nil {
		t.Fatalf("insert test rule: %v", err)
	}

	// Verify the rule was inserted
	var name string
	var enabled int
	var metricField, operator string
	var threshold float64
	err = db.QueryRow("SELECT name, enabled, metric_field, operator, threshold FROM alert_rules WHERE id = 1").
		Scan(&name, &enabled, &metricField, &operator, &threshold)
	if err != nil {
		t.Fatalf("query test rule: %v", err)
	}
	if name != "Test Rule" || enabled != 1 || metricField != "cpu_usage" || operator != ">" || threshold != 80.0 {
		t.Fatalf("rule data mismatch: name=%q enabled=%d metric=%q op=%q threshold=%f", name, enabled, metricField, operator, threshold)
	}
}

func TestMigrateSQLiteAlertHistorySchema(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Insert a test rule first
	_, err = db.Exec(`INSERT INTO alert_rules (name, enabled, metric_field, operator, threshold, duration_seconds, scope_type, notification_channels)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"Test Rule", 1, "cpu_usage", ">", 80.0, 300, "all", `[{"type":"webhook","url":"http://example.com"}]`)
	if err != nil {
		t.Fatalf("insert test rule: %v", err)
	}

	// Insert a test history record
	_, err = db.Exec(`INSERT INTO alert_history (rule_id, rule_name, node_id, node_name, metric_field, metric_value, threshold, triggered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "Test Rule", "node-1", "Test Node", "cpu_usage", 85.5, 80.0, "2026-06-14T10:00:00Z")
	if err != nil {
		t.Fatalf("insert test history: %v", err)
	}

	// Verify the history record was inserted
	var ruleName, nodeID string
	var metricValue, threshold float64
	err = db.QueryRow("SELECT rule_name, node_id, metric_value, threshold FROM alert_history WHERE id = 1").
		Scan(&ruleName, &nodeID, &metricValue, &threshold)
	if err != nil {
		t.Fatalf("query test history: %v", err)
	}
	if ruleName != "Test Rule" || nodeID != "node-1" || metricValue != 85.5 || threshold != 80.0 {
		t.Fatalf("history data mismatch: rule=%q node=%q value=%f threshold=%f", ruleName, nodeID, metricValue, threshold)
	}
}

func TestMigrateSQLiteAddsAlertHistoryNotificationColumns(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`CREATE TABLE alert_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_id INTEGER NOT NULL,
		rule_name TEXT NOT NULL,
		node_id TEXT NOT NULL,
		node_name TEXT NOT NULL,
		metric_field TEXT NOT NULL,
		metric_value REAL NOT NULL,
		threshold REAL NOT NULL,
		triggered_at DATETIME NOT NULL,
		resolved_at DATETIME,
		notification_sent INTEGER NOT NULL DEFAULT 0,
		notification_error TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create legacy alert_history: %v", err)
	}
	if err := Migrate(database); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}

	rows, err := database.Query(`PRAGMA table_info(alert_history)`)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[name] = true
	}
	for _, name := range []string{"notification_attempted_at", "recovery_notification_sent", "recovery_notification_error", "recovery_notification_attempted_at"} {
		if !columns[name] {
			t.Fatal(fmt.Sprintf("missing migrated column %s", name))
		}
	}
}

func TestMySQLMigrationIncludesAlertNotificationColumns(t *testing.T) {
	statements := strings.Join(mysqlMigrationStatements(), "\n")
	compatibility := strings.Join(alertHistoryCompatibilityColumnStatements(DialectMySQL), "\n")
	for _, name := range []string{"notification_attempted_at", "recovery_notification_sent", "recovery_notification_error", "recovery_notification_attempted_at"} {
		if !strings.Contains(statements, name) {
			t.Fatalf("MySQL create migration missing %s", name)
		}
		if !strings.Contains(compatibility, name) {
			t.Fatalf("MySQL compatibility migration missing %s", name)
		}
	}
}

func TestMigrateSQLiteCreatesNodeOrganizationSchema(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, table := range []string{"node_groups", "node_tags", "node_tag_links"} {
		var name string
		if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
	var groupIDColumn int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('nodes') WHERE name = 'group_id'`).Scan(&groupIDColumn); err != nil {
		t.Fatalf("query group_id column: %v", err)
	}
	if groupIDColumn != 1 {
		t.Fatalf("group_id column count = %d, want 1", groupIDColumn)
	}
	var indexCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ('idx_nodes_group', 'idx_node_tag_links_node', 'idx_node_tag_links_tag')`).Scan(&indexCount); err != nil {
		t.Fatalf("query organization indexes: %v", err)
	}
	if indexCount != 3 {
		t.Fatalf("organization index count = %d, want 3", indexCount)
	}
}

func TestMigrateSQLiteAddsNodeGroupIDToLegacyNodes(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE nodes (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		hostname TEXT,
		ip TEXT,
		os TEXT,
		arch TEXT,
		kernel TEXT,
		agent_version TEXT,
		status TEXT NOT NULL DEFAULT 'offline',
		last_seen_at DATETIME,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy nodes: %v", err)
	}
	if err := Migrate(database); err != nil {
		t.Fatalf("migrate legacy nodes: %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('nodes') WHERE name = 'group_id'`).Scan(&count); err != nil {
		t.Fatalf("query group_id: %v", err)
	}
	if count != 1 {
		t.Fatalf("group_id column count = %d, want 1", count)
	}
}

func TestMySQLMigrationIncludesNodeOrganizationSchema(t *testing.T) {
	statements := strings.Join(mysqlMigrationStatements(), "\n")
	compatibility := strings.Join(nodeCompatibilityColumnStatements(DialectMySQL), "\n")
	for _, fragment := range []string{"node_groups", "node_tags", "node_tag_links", "group_id", "uq_node_groups_normalized_name", "uq_node_tags_normalized_name"} {
		if !strings.Contains(statements, fragment) {
			t.Fatalf("MySQL create migration missing %s", fragment)
		}
	}
	if !strings.Contains(compatibility, "group_id") {
		t.Fatal("MySQL compatibility migration missing group_id")
	}
}
