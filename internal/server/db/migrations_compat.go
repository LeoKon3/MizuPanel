package db

func scheduledTaskCompatibilityColumnStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{
			`ALTER TABLE scheduled_tasks ADD COLUMN schedule_type VARCHAR(16) NOT NULL DEFAULT 'cron'`,
			`ALTER TABLE scheduled_tasks ADD COLUMN run_at VARCHAR(64)`,
		}
	}
	return []string{
		`ALTER TABLE scheduled_tasks ADD COLUMN schedule_type TEXT NOT NULL DEFAULT 'cron'`,
		`ALTER TABLE scheduled_tasks ADD COLUMN run_at DATETIME`,
	}
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
			`ALTER TABLE k8s_clusters ADD COLUMN health_reason VARCHAR(64) NOT NULL DEFAULT 'checking'`,
			`ALTER TABLE k8s_clusters ADD COLUMN health_checked_at VARCHAR(64)`,
		}
	}
	return []string{
		`ALTER TABLE k8s_clusters ADD COLUMN version TEXT DEFAULT ''`,
		`ALTER TABLE k8s_clusters ADD COLUMN node_count INTEGER DEFAULT 0`,
		`ALTER TABLE k8s_clusters ADD COLUMN namespace_count INTEGER DEFAULT 0`,
		`ALTER TABLE k8s_clusters ADD COLUMN kubeconfig_content TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE k8s_clusters ADD COLUMN health_reason TEXT NOT NULL DEFAULT 'checking'`,
		`ALTER TABLE k8s_clusters ADD COLUMN health_checked_at DATETIME`,
	}
}
