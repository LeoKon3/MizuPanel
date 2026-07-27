package db

import (
	"database/sql"
	"strings"
)

func Migrate(db *sql.DB) error {
	return MigrateDialect(db, DialectSQLite)
}

func MigrateDialect(db *sql.DB, dialect Dialect) error {
	if dialect == DialectMySQL {
		return migrateStatements(db, DialectMySQL, mysqlMigrationStatements())
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	return migrateStatements(db, DialectSQLite, sqliteMigrationStatements())
}

func sqliteMigrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS nodes (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					group_id TEXT,
					hostname TEXT,
					ip TEXT,
					os TEXT,
					arch TEXT,
					kernel TEXT,
					agent_version TEXT,
					agent_mode TEXT NOT NULL DEFAULT 'normal',
					agent_user TEXT NOT NULL DEFAULT '',
					status TEXT NOT NULL DEFAULT 'offline',
					last_seen_at DATETIME,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS node_groups (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					normalized_name TEXT NOT NULL UNIQUE,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS node_tags (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					normalized_name TEXT NOT NULL UNIQUE,
					color TEXT NOT NULL,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS node_tag_links (
					node_id TEXT NOT NULL,
					tag_id TEXT NOT NULL,
					created_at DATETIME NOT NULL,
					PRIMARY KEY (node_id, tag_id),
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
					FOREIGN KEY (tag_id) REFERENCES node_tags(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX IF NOT EXISTS idx_node_tag_links_node ON node_tag_links(node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_node_tag_links_tag ON node_tag_links(tag_id);`,
		`CREATE TABLE IF NOT EXISTS node_metrics (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					node_id TEXT NOT NULL,
					cpu_usage REAL,
					cpu_cores INTEGER,
					memory_total INTEGER,
					memory_used INTEGER,
					memory_usage REAL,
					disk_total INTEGER,
					disk_used INTEGER,
					disk_usage REAL,
					uptime INTEGER DEFAULT 0,
					disk_read_speed INTEGER DEFAULT 0,
					disk_write_speed INTEGER DEFAULT 0,
					rx_speed INTEGER,
					tx_speed INTEGER,
					rx_total INTEGER,
					tx_total INTEGER,
					load1 REAL,
					load5 REAL,
					load15 REAL,
					created_at DATETIME NOT NULL
				);`,
		`CREATE INDEX IF NOT EXISTS idx_node_metrics_node_created ON node_metrics(node_id, created_at);`,
		`CREATE TABLE IF NOT EXISTS node_process_snapshots (
					node_id TEXT PRIMARY KEY,
					collected_at INTEGER NOT NULL,
					processes_json TEXT NOT NULL,
					error TEXT NOT NULL DEFAULT '',
					updated_at DATETIME NOT NULL,
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS node_docker_snapshots (
					node_id TEXT PRIMARY KEY,
					collected_at INTEGER NOT NULL,
					available INTEGER NOT NULL,
					version TEXT NOT NULL DEFAULT '',
					containers_json TEXT NOT NULL,
					error TEXT NOT NULL DEFAULT '',
					updated_at DATETIME NOT NULL,
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS install_tokens (
					token TEXT PRIMARY KEY,
					used_at DATETIME,
					created_at DATETIME NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS deleted_nodes (
					id TEXT PRIMARY KEY,
					deleted_at DATETIME NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS node_tokens (
					node_id TEXT PRIMARY KEY,
					token TEXT NOT NULL UNIQUE,
					created_at DATETIME NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS node_connection_events (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					node_id TEXT NOT NULL,
					event_type TEXT NOT NULL,
					reason TEXT NOT NULL DEFAULT '',
					agent_version TEXT NOT NULL DEFAULT '',
					protocol_version INTEGER NOT NULL DEFAULT 0,
					identity_source TEXT NOT NULL DEFAULT '',
					hostname TEXT NOT NULL DEFAULT '',
					remote_addr TEXT NOT NULL DEFAULT '',
					created_at DATETIME NOT NULL,
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX IF NOT EXISTS idx_node_connection_events_node_created ON node_connection_events(node_id, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS settings (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL,
					updated_at DATETIME NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS alert_rules (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL,
					enabled INTEGER NOT NULL DEFAULT 1,
					metric_field TEXT NOT NULL,
					operator TEXT NOT NULL,
					threshold REAL NOT NULL,
					duration_seconds INTEGER NOT NULL,
					scope_type TEXT NOT NULL,
					scope_node_ids TEXT,
					notification_channels TEXT NOT NULL,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				);`,
		`CREATE TABLE IF NOT EXISTS alert_history (
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
					notification_attempted_at DATETIME,
					recovery_notification_sent INTEGER NOT NULL DEFAULT 0,
					recovery_notification_error TEXT,
					recovery_notification_attempted_at DATETIME,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (rule_id) REFERENCES alert_rules(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_node ON alert_history(node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_triggered ON alert_history(triggered_at);`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_resolved ON alert_history(resolved_at);`,
		`CREATE TABLE IF NOT EXISTS k8s_clusters (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					node_id TEXT NOT NULL,
					kubeconfig_path TEXT NOT NULL,
					kubeconfig_content TEXT NOT NULL DEFAULT '',
					context TEXT,
					status TEXT NOT NULL DEFAULT 'online',
					last_seen_at DATETIME,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL,
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX IF NOT EXISTS idx_k8s_clusters_node ON k8s_clusters(node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_k8s_clusters_status ON k8s_clusters(status);`,
		`CREATE TABLE IF NOT EXISTS uptime_monitors (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL,
					type TEXT NOT NULL,
					target TEXT NOT NULL,
					enabled INTEGER NOT NULL DEFAULT 1,
					interval_seconds INTEGER NOT NULL,
					timeout_seconds INTEGER NOT NULL,
					failure_threshold INTEGER NOT NULL,
					expected_status_min INTEGER NOT NULL,
					expected_status_max INTEGER NOT NULL,
					tls_expiry_threshold_days INTEGER NOT NULL,
					notification_channels TEXT NOT NULL DEFAULT '[]',
					status TEXT NOT NULL DEFAULT 'pending',
					consecutive_failures INTEGER NOT NULL DEFAULT 0,
					last_latency_ms INTEGER NOT NULL DEFAULT 0,
					last_status_code INTEGER NOT NULL DEFAULT 0,
					last_error TEXT NOT NULL DEFAULT '',
					last_checked_at DATETIME,
					tls_expires_at DATETIME,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL
				);`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_monitors_enabled ON uptime_monitors(enabled);`,
		`CREATE TABLE IF NOT EXISTS uptime_results (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					monitor_id INTEGER NOT NULL,
					success INTEGER NOT NULL,
					latency_ms INTEGER NOT NULL,
					status_code INTEGER NOT NULL DEFAULT 0,
					error TEXT NOT NULL DEFAULT '',
					tls_expires_at DATETIME,
					checked_at DATETIME NOT NULL,
					FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_results_monitor_checked ON uptime_results(monitor_id, checked_at DESC);`,
		`CREATE TABLE IF NOT EXISTS uptime_incidents (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					monitor_id INTEGER NOT NULL,
					kind TEXT NOT NULL,
					message TEXT NOT NULL DEFAULT '',
					started_at DATETIME NOT NULL,
					resolved_at DATETIME,
					active_marker INTEGER DEFAULT 1,
					notification_sent INTEGER NOT NULL DEFAULT 0,
					notification_error TEXT NOT NULL DEFAULT '',
					notification_attempted_at DATETIME,
					recovery_notification_sent INTEGER NOT NULL DEFAULT 0,
					recovery_notification_error TEXT NOT NULL DEFAULT '',
					recovery_notification_attempted_at DATETIME,
					created_at DATETIME NOT NULL,
					FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_incidents_monitor_started ON uptime_incidents(monitor_id, started_at DESC);`,
		`CREATE TABLE IF NOT EXISTS audit_events (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					request_id TEXT NOT NULL,
					created_at TEXT NOT NULL,
					actor_type TEXT NOT NULL,
					actor_name TEXT NOT NULL DEFAULT '',
					source_ip TEXT NOT NULL DEFAULT '',
					module TEXT NOT NULL,
					action TEXT NOT NULL,
					target_type TEXT NOT NULL DEFAULT '',
					target_id TEXT NOT NULL DEFAULT '',
					target_name TEXT NOT NULL DEFAULT '',
					node_id TEXT NOT NULL DEFAULT '',
					result TEXT NOT NULL,
					duration_ms INTEGER NOT NULL DEFAULT 0,
					summary TEXT NOT NULL DEFAULT '',
					metadata_json TEXT NOT NULL DEFAULT '{}'
				);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_created_id ON audit_events(created_at DESC, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_module_id ON audit_events(module, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_node_id ON audit_events(node_id, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_result_id ON audit_events(result, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_actor_id ON audit_events(actor_type, actor_name, id DESC);`,
		`CREATE TABLE IF NOT EXISTS automation_scripts (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL,
					normalized_name TEXT NOT NULL UNIQUE,
					description TEXT NOT NULL DEFAULT '',
					content TEXT NOT NULL,
					timeout_seconds INTEGER NOT NULL,
					revision INTEGER NOT NULL DEFAULT 1,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL
				);`,
		`CREATE INDEX IF NOT EXISTS idx_automation_scripts_updated ON automation_scripts(updated_at DESC, id DESC);`,
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL,
					normalized_name TEXT NOT NULL UNIQUE,
					script_id INTEGER NOT NULL,
					cron_expression TEXT NOT NULL,
					timezone TEXT NOT NULL,
					enabled INTEGER NOT NULL DEFAULT 1,
					timeout_seconds INTEGER NOT NULL,
					notification_policy TEXT NOT NULL DEFAULT 'failure',
					notification_channels TEXT NOT NULL DEFAULT '[]',
					next_run_at DATETIME,
					last_scheduled_at DATETIME,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL,
					FOREIGN KEY (script_id) REFERENCES automation_scripts(id) ON DELETE RESTRICT
				);`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_due ON scheduled_tasks(enabled, next_run_at, id);`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_script ON scheduled_tasks(script_id);`,
		`CREATE TABLE IF NOT EXISTS scheduled_task_nodes (
					task_id INTEGER NOT NULL,
					node_id TEXT NOT NULL,
					created_at DATETIME NOT NULL,
					PRIMARY KEY (task_id, node_id),
					FOREIGN KEY (task_id) REFERENCES scheduled_tasks(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_task_nodes_node ON scheduled_task_nodes(node_id, task_id);`,
		`CREATE TABLE IF NOT EXISTS task_runs (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					task_id INTEGER,
					script_id INTEGER,
					task_name TEXT NOT NULL DEFAULT '',
					script_name TEXT NOT NULL,
					script_revision INTEGER NOT NULL,
					script_content TEXT NOT NULL,
					timeout_seconds INTEGER NOT NULL,
					notification_policy TEXT NOT NULL DEFAULT 'failure',
					notification_channels TEXT NOT NULL DEFAULT '[]',
					trigger_type TEXT NOT NULL,
					scheduled_for DATETIME,
					status TEXT NOT NULL DEFAULT 'queued',
					total_targets INTEGER NOT NULL DEFAULT 0,
					completed_targets INTEGER NOT NULL DEFAULT 0,
					success_targets INTEGER NOT NULL DEFAULT 0,
					failed_targets INTEGER NOT NULL DEFAULT 0,
					error TEXT NOT NULL DEFAULT '',
					notification_sent INTEGER NOT NULL DEFAULT 0,
					notification_error TEXT NOT NULL DEFAULT '',
					notification_attempted_at DATETIME,
					started_at DATETIME,
					completed_at DATETIME,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL,
					UNIQUE (task_id, scheduled_for),
					FOREIGN KEY (task_id) REFERENCES scheduled_tasks(id) ON DELETE SET NULL,
					FOREIGN KEY (script_id) REFERENCES automation_scripts(id) ON DELETE SET NULL
				);`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_task_id ON task_runs(task_id, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_script_id ON task_runs(script_id, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_status_id ON task_runs(status, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_trigger_id ON task_runs(trigger_type, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_created_id ON task_runs(created_at DESC, id DESC);`,
		`CREATE TABLE IF NOT EXISTS task_run_targets (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					run_id INTEGER NOT NULL,
					node_id TEXT NOT NULL,
					node_name TEXT NOT NULL DEFAULT '',
					status TEXT NOT NULL DEFAULT 'queued',
					exit_code INTEGER,
					output TEXT NOT NULL DEFAULT '',
					output_truncated INTEGER NOT NULL DEFAULT 0,
					error TEXT NOT NULL DEFAULT '',
					duration_ms INTEGER NOT NULL DEFAULT 0,
					started_at DATETIME,
					completed_at DATETIME,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL,
					UNIQUE (run_id, node_id),
					FOREIGN KEY (run_id) REFERENCES task_runs(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX IF NOT EXISTS idx_task_run_targets_run_id ON task_run_targets(run_id, id);`,
		`CREATE INDEX IF NOT EXISTS idx_task_run_targets_node_run ON task_run_targets(node_id, run_id DESC);`,
	}
}

func mysqlMigrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS nodes (
					id VARCHAR(191) PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					group_id VARCHAR(64),
					hostname VARCHAR(255),
					ip VARCHAR(64),
					os VARCHAR(64),
					arch VARCHAR(64),
					kernel VARCHAR(128),
					agent_version VARCHAR(64),
					agent_mode VARCHAR(32) NOT NULL DEFAULT 'normal',
					agent_user VARCHAR(255) NOT NULL DEFAULT '',
					status VARCHAR(32) NOT NULL DEFAULT 'offline',
					last_seen_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					INDEX idx_nodes_group (group_id)
				);`,
		`CREATE TABLE IF NOT EXISTS node_groups (
					id VARCHAR(64) PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					normalized_name VARCHAR(255) NOT NULL,
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					UNIQUE KEY uq_node_groups_normalized_name (normalized_name)
				);`,
		`CREATE TABLE IF NOT EXISTS node_tags (
					id VARCHAR(64) PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					normalized_name VARCHAR(255) NOT NULL,
					color VARCHAR(32) NOT NULL,
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					UNIQUE KEY uq_node_tags_normalized_name (normalized_name)
				);`,
		`CREATE TABLE IF NOT EXISTS node_tag_links (
					node_id VARCHAR(191) NOT NULL,
					tag_id VARCHAR(64) NOT NULL,
					created_at VARCHAR(64) NOT NULL,
					PRIMARY KEY (node_id, tag_id),
					INDEX idx_node_tag_links_node (node_id),
					INDEX idx_node_tag_links_tag (tag_id),
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
					FOREIGN KEY (tag_id) REFERENCES node_tags(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS node_metrics (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					node_id VARCHAR(191) NOT NULL,
					cpu_usage DOUBLE,
					cpu_cores INT,
					memory_total BIGINT,
					memory_used BIGINT,
					memory_usage DOUBLE,
					disk_total BIGINT,
					disk_used BIGINT,
					disk_usage DOUBLE,
					uptime BIGINT DEFAULT 0,
					disk_read_speed BIGINT DEFAULT 0,
					disk_write_speed BIGINT DEFAULT 0,
					rx_speed BIGINT,
					tx_speed BIGINT,
					rx_total BIGINT,
					tx_total BIGINT,
					load1 DOUBLE,
					load5 DOUBLE,
					load15 DOUBLE,
					created_at VARCHAR(64) NOT NULL
				);`,
		`CREATE INDEX idx_node_metrics_node_created ON node_metrics(node_id, created_at);`,
		`CREATE TABLE IF NOT EXISTS node_process_snapshots (
					node_id VARCHAR(191) PRIMARY KEY,
					collected_at BIGINT NOT NULL,
					processes_json LONGTEXT NOT NULL,
					error VARCHAR(1024) NOT NULL DEFAULT '',
					updated_at VARCHAR(64) NOT NULL,
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS node_docker_snapshots (
					node_id VARCHAR(191) PRIMARY KEY,
					collected_at BIGINT NOT NULL,
					available BOOLEAN NOT NULL,
					version VARCHAR(128) NOT NULL DEFAULT '',
					containers_json LONGTEXT NOT NULL,
					error VARCHAR(1024) NOT NULL DEFAULT '',
					updated_at VARCHAR(64) NOT NULL,
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS install_tokens (
					token VARCHAR(255) PRIMARY KEY,
					used_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS deleted_nodes (
					id VARCHAR(191) PRIMARY KEY,
					deleted_at VARCHAR(64) NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS node_tokens (
					node_id VARCHAR(191) PRIMARY KEY,
					token VARCHAR(255) NOT NULL UNIQUE,
					created_at VARCHAR(64) NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS node_connection_events (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					node_id VARCHAR(191) NOT NULL,
					event_type VARCHAR(64) NOT NULL,
					reason VARCHAR(1024) NOT NULL DEFAULT '',
					agent_version VARCHAR(64) NOT NULL DEFAULT '',
					protocol_version INT NOT NULL DEFAULT 0,
					identity_source VARCHAR(32) NOT NULL DEFAULT '',
					hostname VARCHAR(255) NOT NULL DEFAULT '',
					remote_addr VARCHAR(255) NOT NULL DEFAULT '',
					created_at VARCHAR(64) NOT NULL,
					INDEX idx_node_connection_events_node_created (node_id, created_at),
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS settings (
					` + "`key`" + ` VARCHAR(191) PRIMARY KEY,
					value VARCHAR(255) NOT NULL,
					updated_at VARCHAR(64) NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS alert_rules (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					enabled BOOLEAN NOT NULL DEFAULT 1,
					metric_field VARCHAR(64) NOT NULL,
					operator VARCHAR(8) NOT NULL,
					threshold DOUBLE NOT NULL,
					duration_seconds INT NOT NULL,
					scope_type VARCHAR(32) NOT NULL,
					scope_node_ids TEXT,
					notification_channels LONGTEXT NOT NULL,
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL
				);`,
		`CREATE TABLE IF NOT EXISTS alert_history (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					rule_id BIGINT NOT NULL,
					rule_name VARCHAR(255) NOT NULL,
					node_id VARCHAR(191) NOT NULL,
					node_name VARCHAR(255) NOT NULL,
					metric_field VARCHAR(64) NOT NULL,
					metric_value DOUBLE NOT NULL,
					threshold DOUBLE NOT NULL,
					triggered_at VARCHAR(64) NOT NULL,
					resolved_at VARCHAR(64),
					notification_sent BOOLEAN NOT NULL DEFAULT 0,
					notification_error TEXT,
					notification_attempted_at VARCHAR(64),
					recovery_notification_sent BOOLEAN NOT NULL DEFAULT 0,
					recovery_notification_error TEXT,
					recovery_notification_attempted_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL,
					FOREIGN KEY (rule_id) REFERENCES alert_rules(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX idx_alert_history_node ON alert_history(node_id);`,
		`CREATE INDEX idx_alert_history_triggered ON alert_history(triggered_at);`,
		`CREATE INDEX idx_alert_history_resolved ON alert_history(resolved_at);`,
		`CREATE TABLE IF NOT EXISTS k8s_clusters (
					id VARCHAR(191) PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					node_id VARCHAR(191) NOT NULL,
					kubeconfig_path VARCHAR(512) NOT NULL,
					kubeconfig_content LONGTEXT NOT NULL,
					context VARCHAR(255),
					status VARCHAR(32) NOT NULL DEFAULT 'online',
					last_seen_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
				);`,
		`CREATE INDEX idx_k8s_clusters_node ON k8s_clusters(node_id);`,
		`CREATE INDEX idx_k8s_clusters_status ON k8s_clusters(status);`,
		`CREATE TABLE IF NOT EXISTS uptime_monitors (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					type VARCHAR(16) NOT NULL,
					target VARCHAR(2048) NOT NULL,
					enabled BOOLEAN NOT NULL DEFAULT 1,
					interval_seconds INT NOT NULL,
					timeout_seconds INT NOT NULL,
					failure_threshold INT NOT NULL,
					expected_status_min INT NOT NULL,
					expected_status_max INT NOT NULL,
					tls_expiry_threshold_days INT NOT NULL,
					notification_channels LONGTEXT NOT NULL,
					status VARCHAR(16) NOT NULL DEFAULT 'pending',
					consecutive_failures INT NOT NULL DEFAULT 0,
					last_latency_ms BIGINT NOT NULL DEFAULT 0,
					last_status_code INT NOT NULL DEFAULT 0,
					last_error VARCHAR(1024) NOT NULL DEFAULT '',
					last_checked_at VARCHAR(64),
					tls_expires_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					INDEX idx_uptime_monitors_enabled (enabled)
				);`,
		`CREATE TABLE IF NOT EXISTS uptime_results (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					monitor_id BIGINT NOT NULL,
					success BOOLEAN NOT NULL,
					latency_ms BIGINT NOT NULL,
					status_code INT NOT NULL DEFAULT 0,
					error VARCHAR(1024) NOT NULL DEFAULT '',
					tls_expires_at VARCHAR(64),
					checked_at VARCHAR(64) NOT NULL,
					INDEX idx_uptime_results_monitor_checked (monitor_id, checked_at),
					FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS uptime_incidents (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					monitor_id BIGINT NOT NULL,
					kind VARCHAR(32) NOT NULL,
					message VARCHAR(1024) NOT NULL DEFAULT '',
					started_at VARCHAR(64) NOT NULL,
					resolved_at VARCHAR(64),
					active_marker BOOLEAN DEFAULT 1,
					notification_sent BOOLEAN NOT NULL DEFAULT 0,
					notification_error VARCHAR(1024) NOT NULL DEFAULT '',
					notification_attempted_at VARCHAR(64),
					recovery_notification_sent BOOLEAN NOT NULL DEFAULT 0,
					recovery_notification_error VARCHAR(1024) NOT NULL DEFAULT '',
					recovery_notification_attempted_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL,
					INDEX idx_uptime_incidents_monitor_started (monitor_id, started_at),
					FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS audit_events (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					request_id VARCHAR(64) NOT NULL,
					created_at VARCHAR(64) NOT NULL,
					actor_type VARCHAR(32) NOT NULL,
					actor_name VARCHAR(128) NOT NULL DEFAULT '',
					source_ip VARCHAR(64) NOT NULL DEFAULT '',
					module VARCHAR(64) NOT NULL,
					action VARCHAR(64) NOT NULL,
					target_type VARCHAR(64) NOT NULL DEFAULT '',
					target_id VARCHAR(1024) NOT NULL DEFAULT '',
					target_name VARCHAR(256) NOT NULL DEFAULT '',
					node_id VARCHAR(191) NOT NULL DEFAULT '',
					result VARCHAR(16) NOT NULL,
					duration_ms BIGINT NOT NULL DEFAULT 0,
					summary VARCHAR(64) NOT NULL DEFAULT '',
					metadata_json TEXT NOT NULL,
					INDEX idx_audit_events_created_id (created_at, id),
					INDEX idx_audit_events_module_id (module, id),
					INDEX idx_audit_events_node_id (node_id, id),
					INDEX idx_audit_events_result_id (result, id),
					INDEX idx_audit_events_actor_id (actor_type, actor_name, id)
				);`,
		`CREATE TABLE IF NOT EXISTS automation_scripts (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					normalized_name VARCHAR(255) NOT NULL,
					description VARCHAR(2048) NOT NULL DEFAULT '',
					content LONGTEXT NOT NULL,
					timeout_seconds INT NOT NULL,
					revision INT NOT NULL DEFAULT 1,
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					UNIQUE KEY uq_automation_scripts_normalized_name (normalized_name),
					INDEX idx_automation_scripts_updated (updated_at, id)
				);`,
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					normalized_name VARCHAR(255) NOT NULL,
					script_id BIGINT NOT NULL,
					cron_expression VARCHAR(128) NOT NULL,
					timezone VARCHAR(128) NOT NULL,
					enabled BOOLEAN NOT NULL DEFAULT 1,
					timeout_seconds INT NOT NULL,
					notification_policy VARCHAR(16) NOT NULL DEFAULT 'failure',
					notification_channels LONGTEXT NOT NULL,
					next_run_at VARCHAR(64),
					last_scheduled_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					UNIQUE KEY uq_scheduled_tasks_normalized_name (normalized_name),
					INDEX idx_scheduled_tasks_due (enabled, next_run_at, id),
					INDEX idx_scheduled_tasks_script (script_id),
					FOREIGN KEY (script_id) REFERENCES automation_scripts(id) ON DELETE RESTRICT
				);`,
		`CREATE TABLE IF NOT EXISTS scheduled_task_nodes (
					task_id BIGINT NOT NULL,
					node_id VARCHAR(191) NOT NULL,
					created_at VARCHAR(64) NOT NULL,
					PRIMARY KEY (task_id, node_id),
					INDEX idx_scheduled_task_nodes_node (node_id, task_id),
					FOREIGN KEY (task_id) REFERENCES scheduled_tasks(id) ON DELETE CASCADE
				);`,
		`CREATE TABLE IF NOT EXISTS task_runs (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					task_id BIGINT,
					script_id BIGINT,
					task_name VARCHAR(255) NOT NULL DEFAULT '',
					script_name VARCHAR(255) NOT NULL,
					script_revision INT NOT NULL,
					script_content LONGTEXT NOT NULL,
					timeout_seconds INT NOT NULL,
					notification_policy VARCHAR(16) NOT NULL DEFAULT 'failure',
					notification_channels LONGTEXT NOT NULL,
					trigger_type VARCHAR(16) NOT NULL,
					scheduled_for VARCHAR(64),
					status VARCHAR(16) NOT NULL DEFAULT 'queued',
					total_targets INT NOT NULL DEFAULT 0,
					completed_targets INT NOT NULL DEFAULT 0,
					success_targets INT NOT NULL DEFAULT 0,
					failed_targets INT NOT NULL DEFAULT 0,
					error VARCHAR(1024) NOT NULL DEFAULT '',
					notification_sent BOOLEAN NOT NULL DEFAULT 0,
					notification_error VARCHAR(1024) NOT NULL DEFAULT '',
					notification_attempted_at VARCHAR(64),
					started_at VARCHAR(64),
					completed_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					UNIQUE KEY uq_task_runs_scheduled_occurrence (task_id, scheduled_for),
					INDEX idx_task_runs_task_id (task_id, id),
					INDEX idx_task_runs_script_id (script_id, id),
					INDEX idx_task_runs_status_id (status, id),
					INDEX idx_task_runs_trigger_id (trigger_type, id),
					INDEX idx_task_runs_created_id (created_at, id),
					FOREIGN KEY (task_id) REFERENCES scheduled_tasks(id) ON DELETE SET NULL,
					FOREIGN KEY (script_id) REFERENCES automation_scripts(id) ON DELETE SET NULL
				);`,
		`CREATE TABLE IF NOT EXISTS task_run_targets (
					id BIGINT AUTO_INCREMENT PRIMARY KEY,
					run_id BIGINT NOT NULL,
					node_id VARCHAR(191) NOT NULL,
					node_name VARCHAR(255) NOT NULL DEFAULT '',
					status VARCHAR(16) NOT NULL DEFAULT 'queued',
					exit_code INT,
					output LONGTEXT NOT NULL,
					output_truncated BOOLEAN NOT NULL DEFAULT 0,
					error VARCHAR(1024) NOT NULL DEFAULT '',
					duration_ms BIGINT NOT NULL DEFAULT 0,
					started_at VARCHAR(64),
					completed_at VARCHAR(64),
					created_at VARCHAR(64) NOT NULL,
					updated_at VARCHAR(64) NOT NULL,
					UNIQUE KEY uq_task_run_targets_run_node (run_id, node_id),
					INDEX idx_task_run_targets_run_id (run_id, id),
					INDEX idx_task_run_targets_node_run (node_id, run_id),
					FOREIGN KEY (run_id) REFERENCES task_runs(id) ON DELETE CASCADE
				);`,
	}
}

func migrateStatements(db *sql.DB, dialect Dialect, statements []string) error {
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			if isIgnorableMigrationError(err) {
				continue
			}
			return err
		}
	}
	for _, statement := range nodeCompatibilityColumnStatements(dialect) {
		if err := addColumnIfMissing(db, statement); err != nil {
			return err
		}
	}
	for _, statement := range organizationIndexStatements(dialect) {
		if _, err := db.Exec(statement); err != nil && !isIgnorableMigrationError(err) {
			return err
		}
	}
	for _, statement := range metricCompatibilityColumnStatements(dialect) {
		if err := addColumnIfMissing(db, statement); err != nil {
			return err
		}
	}
	for _, statement := range alertHistoryCompatibilityColumnStatements(dialect) {
		if err := addColumnIfMissing(db, statement); err != nil {
			return err
		}
	}
	for _, statement := range k8sClusterCompatibilityColumnStatements(dialect) {
		if err := addColumnIfMissing(db, statement); err != nil {
			return err
		}
	}
	for _, statement := range uptimeIncidentCompatibilityColumnStatements(dialect) {
		if err := addColumnIfMissing(db, statement); err != nil {
			return err
		}
	}
	for _, statement := range uptimeIncidentInvariantStatements(dialect) {
		if _, err := db.Exec(statement); err != nil && !isIgnorableMigrationError(err) {
			return err
		}
	}
	_, err := db.Exec(`UPDATE nodes SET agent_mode = COALESCE(NULLIF(agent_mode, ''), 'normal'), agent_user = COALESCE(agent_user, '')`)
	return err
}

func uptimeIncidentCompatibilityColumnStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{`ALTER TABLE uptime_incidents ADD COLUMN active_marker BOOLEAN DEFAULT 1`}
	}
	return []string{`ALTER TABLE uptime_incidents ADD COLUMN active_marker INTEGER DEFAULT 1`}
}

func uptimeIncidentInvariantStatements(dialect Dialect) []string {
	resolveDuplicates := `UPDATE uptime_incidents
		SET resolved_at = started_at
		WHERE resolved_at IS NULL
			AND EXISTS (
				SELECT 1 FROM uptime_incidents AS newer
				WHERE newer.monitor_id = uptime_incidents.monitor_id
					AND newer.kind = uptime_incidents.kind
					AND newer.resolved_at IS NULL
					AND newer.id > uptime_incidents.id
			)`
	uniqueIndex := `CREATE UNIQUE INDEX IF NOT EXISTS uq_uptime_incidents_active ON uptime_incidents(monitor_id, kind, active_marker)`
	if dialect == DialectMySQL {
		resolveDuplicates = `UPDATE uptime_incidents AS stale
			JOIN uptime_incidents AS newer
				ON newer.monitor_id = stale.monitor_id
				AND newer.kind = stale.kind
				AND newer.resolved_at IS NULL
				AND newer.id > stale.id
			SET stale.resolved_at = stale.started_at
			WHERE stale.resolved_at IS NULL`
		uniqueIndex = `CREATE UNIQUE INDEX uq_uptime_incidents_active ON uptime_incidents(monitor_id, kind, active_marker)`
	}
	return []string{
		resolveDuplicates,
		`UPDATE uptime_incidents SET active_marker = NULL WHERE resolved_at IS NOT NULL`,
		`UPDATE uptime_incidents SET active_marker = 1 WHERE resolved_at IS NULL`,
		uniqueIndex,
	}
}

func alertHistoryCompatibilityColumnStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{
			`ALTER TABLE alert_history ADD COLUMN notification_attempted_at VARCHAR(64)`,
			`ALTER TABLE alert_history ADD COLUMN recovery_notification_sent BOOLEAN NOT NULL DEFAULT 0`,
			`ALTER TABLE alert_history ADD COLUMN recovery_notification_error TEXT`,
			`ALTER TABLE alert_history ADD COLUMN recovery_notification_attempted_at VARCHAR(64)`,
		}
	}
	return []string{
		`ALTER TABLE alert_history ADD COLUMN notification_attempted_at DATETIME`,
		`ALTER TABLE alert_history ADD COLUMN recovery_notification_sent INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE alert_history ADD COLUMN recovery_notification_error TEXT`,
		`ALTER TABLE alert_history ADD COLUMN recovery_notification_attempted_at DATETIME`,
	}
}

func nodeCompatibilityColumnStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{
			`ALTER TABLE nodes ADD COLUMN group_id VARCHAR(64)`,
			`ALTER TABLE nodes ADD COLUMN agent_mode VARCHAR(32) NOT NULL DEFAULT 'normal'`,
			`ALTER TABLE nodes ADD COLUMN agent_user VARCHAR(255) NOT NULL DEFAULT ''`,
			`ALTER TABLE nodes ADD COLUMN terminal_enabled BOOLEAN NOT NULL DEFAULT 0`,
		}
	}
	return []string{
		`ALTER TABLE nodes ADD COLUMN group_id TEXT`,
		`ALTER TABLE nodes ADD COLUMN agent_mode TEXT NOT NULL DEFAULT 'normal'`,
		`ALTER TABLE nodes ADD COLUMN agent_user TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE nodes ADD COLUMN terminal_enabled INTEGER NOT NULL DEFAULT 0`,
	}
}

func organizationIndexStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{`CREATE INDEX idx_nodes_group ON nodes(group_id)`}
	}
	return []string{`CREATE INDEX IF NOT EXISTS idx_nodes_group ON nodes(group_id)`}
}

func metricCompatibilityColumnStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{
			`ALTER TABLE node_metrics ADD COLUMN uptime BIGINT DEFAULT 0`,
			`ALTER TABLE node_metrics ADD COLUMN disk_read_speed BIGINT DEFAULT 0`,
			`ALTER TABLE node_metrics ADD COLUMN disk_write_speed BIGINT DEFAULT 0`,
		}
	}
	return []string{
		`ALTER TABLE node_metrics ADD COLUMN uptime INTEGER DEFAULT 0`,
		`ALTER TABLE node_metrics ADD COLUMN disk_read_speed INTEGER DEFAULT 0`,
		`ALTER TABLE node_metrics ADD COLUMN disk_write_speed INTEGER DEFAULT 0`,
	}
}

func k8sClusterCompatibilityColumnStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{
			`ALTER TABLE k8s_clusters ADD COLUMN version VARCHAR(64) DEFAULT ''`,
			`ALTER TABLE k8s_clusters ADD COLUMN node_count INT DEFAULT 0`,
			`ALTER TABLE k8s_clusters ADD COLUMN namespace_count INT DEFAULT 0`,
			`ALTER TABLE k8s_clusters ADD COLUMN kubeconfig_content LONGTEXT NOT NULL`,
		}
	}
	return []string{
		`ALTER TABLE k8s_clusters ADD COLUMN version TEXT DEFAULT ''`,
		`ALTER TABLE k8s_clusters ADD COLUMN node_count INTEGER DEFAULT 0`,
		`ALTER TABLE k8s_clusters ADD COLUMN namespace_count INTEGER DEFAULT 0`,
		`ALTER TABLE k8s_clusters ADD COLUMN kubeconfig_content TEXT NOT NULL DEFAULT ''`,
	}
}

func addColumnIfMissing(db *sql.DB, statement string) error {
	_, err := db.Exec(statement)
	if err != nil && isIgnorableMigrationError(err) {
		return nil
	}
	return err
}

func isIgnorableMigrationError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate column") || strings.Contains(message, "duplicate key name")
}
