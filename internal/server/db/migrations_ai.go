package db

func aiPlanIndexStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{`CREATE INDEX idx_ai_tool_calls_turn_step ON ai_tool_calls(turn_id, step_index)`}
	}
	return []string{`CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_tool_calls_turn_step ON ai_tool_calls(turn_id, step_index) WHERE step_index >= 0`}
}

func aiCompatibilityColumnStatements(dialect Dialect) []string {
	if dialect == DialectMySQL {
		return []string{
			`ALTER TABLE ai_providers ADD COLUMN enabled BOOLEAN NOT NULL DEFAULT 1`,
			`ALTER TABLE ai_providers ADD COLUMN discovery_status VARCHAR(32) NOT NULL DEFAULT 'unknown'`,
			`ALTER TABLE ai_providers ADD COLUMN discovery_latency_ms INT NOT NULL DEFAULT 0`,
			`ALTER TABLE ai_providers ADD COLUMN discovered_at VARCHAR(64)`,
			`ALTER TABLE ai_providers ADD COLUMN discovery_error VARCHAR(512) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_conversations ADD COLUMN model_id VARCHAR(36) NULL`,
			`ALTER TABLE ai_turns ADD COLUMN model_id VARCHAR(36) NULL`,
			`ALTER TABLE ai_turns ADD COLUMN requested_provider_id VARCHAR(36) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_turns ADD COLUMN requested_provider_name VARCHAR(191) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_turns ADD COLUMN requested_model_id VARCHAR(36) NULL`,
			`ALTER TABLE ai_turns ADD COLUMN requested_model VARCHAR(255) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_turns ADD COLUMN fallback_used BOOLEAN NOT NULL DEFAULT 0`,
			`ALTER TABLE ai_tool_calls ADD COLUMN operation_id VARCHAR(191) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_tool_calls ADD COLUMN step_index INT NOT NULL DEFAULT -1`,
			`ALTER TABLE ai_tool_calls ADD COLUMN policy_decision VARCHAR(32) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_tool_calls ADD COLUMN policy_reason VARCHAR(64) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_tool_calls ADD COLUMN policy_revision BIGINT NOT NULL DEFAULT 0`,
			`ALTER TABLE ai_tool_calls ADD COLUMN verification_status VARCHAR(16) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_tool_calls ADD COLUMN impact_json LONGTEXT NULL`,
		}
	}
	return []string{
		`ALTER TABLE ai_providers ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE ai_providers ADD COLUMN discovery_status TEXT NOT NULL DEFAULT 'unknown'`,
		`ALTER TABLE ai_providers ADD COLUMN discovery_latency_ms INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE ai_providers ADD COLUMN discovered_at TEXT`,
		`ALTER TABLE ai_providers ADD COLUMN discovery_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_conversations ADD COLUMN model_id TEXT REFERENCES ai_provider_models(id) ON DELETE SET NULL`,
		`ALTER TABLE ai_turns ADD COLUMN model_id TEXT REFERENCES ai_provider_models(id) ON DELETE SET NULL`,
		`ALTER TABLE ai_turns ADD COLUMN requested_provider_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_turns ADD COLUMN requested_provider_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_turns ADD COLUMN requested_model_id TEXT`,
		`ALTER TABLE ai_turns ADD COLUMN requested_model TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_turns ADD COLUMN fallback_used INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE ai_tool_calls ADD COLUMN operation_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_tool_calls ADD COLUMN step_index INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE ai_tool_calls ADD COLUMN policy_decision TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_tool_calls ADD COLUMN policy_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_tool_calls ADD COLUMN policy_revision INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE ai_tool_calls ADD COLUMN verification_status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_tool_calls ADD COLUMN impact_json TEXT`,
	}
}

func aiUpgradeStatements(dialect Dialect) []string {
	markerMissing := `NOT EXISTS (SELECT 1 FROM settings WHERE key = 'migration.ai_provider_models_v1')`
	insertModels := `INSERT INTO ai_provider_models
		(id, provider_id, model_id, display_name, enabled, chat_capable, tools_capable,
		 probe_status, probe_latency_ms, probed_at, probe_error, is_default, is_fallback,
		 created_at, updated_at)
		SELECT p.id, p.id, p.model, '', 1, p.chat_capable, p.tools_capable,
		 p.probe_status, 0, p.probed_at, p.probe_error, p.is_default, 0,
		 p.created_at, p.updated_at
		FROM ai_providers p
		WHERE TRIM(p.model) <> '' AND ` + markerMissing + `
		 AND NOT EXISTS (SELECT 1 FROM ai_provider_models m WHERE m.provider_id = p.id AND m.model_id = p.model)`
	backfillConversations := `UPDATE ai_conversations SET model_id =
		(SELECT id FROM ai_provider_models WHERE is_default = 1 LIMIT 1)
		WHERE model_id IS NULL AND ` + markerMissing
	backfillTurns := `UPDATE ai_turns SET
		model_id = COALESCE(model_id, (SELECT m.id FROM ai_provider_models m WHERE m.provider_id = ai_turns.provider_id AND m.model_id = ai_turns.model LIMIT 1)),
		requested_provider_id = CASE WHEN requested_provider_id = '' THEN provider_id ELSE requested_provider_id END,
		requested_provider_name = CASE WHEN requested_provider_name = '' THEN provider_name ELSE requested_provider_name END,
		requested_model_id = COALESCE(requested_model_id, (SELECT m.id FROM ai_provider_models m WHERE m.provider_id = ai_turns.provider_id AND m.model_id = ai_turns.model LIMIT 1)),
		requested_model = CASE WHEN requested_model = '' THEN model ELSE requested_model END`
	markComplete := `INSERT INTO settings (key, value, updated_at)
		SELECT 'migration.ai_provider_models_v1', 'complete', CURRENT_TIMESTAMP
		WHERE ` + markerMissing
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_ai_conversations_model ON ai_conversations(model_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_turns_model ON ai_turns(model_id)`,
	}
	if dialect == DialectMySQL {
		markerMissing = "NOT EXISTS (SELECT 1 FROM settings WHERE `key` = 'migration.ai_provider_models_v1')"
		insertModels = `INSERT INTO ai_provider_models
			(id, provider_id, model_id, display_name, enabled, chat_capable, tools_capable,
			 probe_status, probe_latency_ms, probed_at, probe_error, is_default, is_fallback,
			 default_marker, fallback_marker, created_at, updated_at)
			SELECT p.id, p.id, p.model, '', 1, p.chat_capable, p.tools_capable,
			 p.probe_status, 0, p.probed_at, p.probe_error, p.is_default, 0,
			 CASE WHEN p.is_default = 1 THEN '1' ELSE NULL END, NULL, p.created_at, p.updated_at
			FROM ai_providers p
			WHERE TRIM(p.model) <> '' AND ` + markerMissing + `
			 AND NOT EXISTS (SELECT 1 FROM ai_provider_models m WHERE m.provider_id = p.id AND m.model_id = p.model)`
		backfillConversations = `UPDATE ai_conversations SET model_id =
			(SELECT id FROM ai_provider_models WHERE is_default = 1 LIMIT 1)
			WHERE model_id IS NULL AND ` + markerMissing
		markComplete = "INSERT INTO settings (`key`, value, updated_at) SELECT 'migration.ai_provider_models_v1', 'complete', CURRENT_TIMESTAMP WHERE " + markerMissing
		indexes = []string{
			`CREATE INDEX idx_ai_conversations_model ON ai_conversations(model_id)`,
			`CREATE INDEX idx_ai_turns_model ON ai_turns(model_id)`,
			`ALTER TABLE ai_conversations ADD CONSTRAINT fk_ai_conversations_model FOREIGN KEY (model_id) REFERENCES ai_provider_models(id) ON DELETE SET NULL`,
			`ALTER TABLE ai_turns ADD CONSTRAINT fk_ai_turns_model FOREIGN KEY (model_id) REFERENCES ai_provider_models(id) ON DELETE SET NULL`,
		}
	}
	return append([]string{insertModels, backfillConversations, backfillTurns, markComplete}, indexes...)
}
