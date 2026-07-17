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
