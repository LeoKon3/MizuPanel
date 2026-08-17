package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

const (
	MetricsRetentionMin     = 6 * time.Hour
	MetricsRetentionMax     = 7 * 24 * time.Hour
	metricsRetentionKey     = "metrics_retention"
	MaxAIControlActions     = 32
	MaxAIControlNodes       = 100
	MaxAIControlActionBytes = 96
	MaxAIControlNodeBytes   = 191
)

type AIControlMode string

const (
	AIControlConfirmAll  AIControlMode = "confirm_all"
	AIControlLowRiskAuto AIControlMode = "low_risk_auto"
	AIControlPaused      AIControlMode = "paused"

	AIControlActionDockerContainerStart   = "docker.container.start"
	AIControlActionDockerContainerRestart = "docker.container.restart"
	AIControlActionComposeServiceStart    = "compose.service.start"
	AIControlActionComposeServiceRestart  = "compose.service.restart"
	AIControlActionSystemdServiceStart    = "systemd.service.start"
	AIControlActionSystemdServiceRestart  = "systemd.service.restart"
)

// AIControlPolicy is the persisted, server-owned authorization boundary for
// autonomous AI mutations. An empty NodeScope is intentionally never global.
type AIControlPolicy struct {
	Mode           AIControlMode `json:"mode"`
	AllowedActions []string      `json:"allowed_actions"`
	NodeScope      []string      `json:"node_scope"`
	Revision       int64         `json:"revision"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func DefaultAIControlPolicy() AIControlPolicy {
	return AIControlPolicy{Mode: AIControlConfirmAll, AllowedActions: []string{}, NodeScope: []string{}, Revision: 1}
}

func ValidateAIControlPolicy(policy AIControlPolicy) error {
	if policy.Mode != AIControlConfirmAll && policy.Mode != AIControlLowRiskAuto && policy.Mode != AIControlPaused {
		return fmt.Errorf("invalid AI control mode")
	}
	if len(policy.AllowedActions) > MaxAIControlActions || len(policy.NodeScope) > MaxAIControlNodes {
		return fmt.Errorf("AI control scope is too large")
	}
	seen := make(map[string]struct{}, len(policy.AllowedActions))
	for _, action := range policy.AllowedActions {
		if action == "" || len(action) > MaxAIControlActionBytes || strings.TrimSpace(action) != action || !validAIControlAction(action) {
			return fmt.Errorf("invalid AI control action")
		}
		if _, ok := seen[action]; ok {
			return fmt.Errorf("duplicate AI control action")
		}
		seen[action] = struct{}{}
	}
	seen = make(map[string]struct{}, len(policy.NodeScope))
	for _, nodeID := range policy.NodeScope {
		if nodeID == "" || len(nodeID) > MaxAIControlNodeBytes || strings.TrimSpace(nodeID) != nodeID {
			return fmt.Errorf("invalid AI control node scope")
		}
		if _, ok := seen[nodeID]; ok {
			return fmt.Errorf("duplicate AI control node scope")
		}
		seen[nodeID] = struct{}{}
	}
	if policy.Revision < 1 {
		return fmt.Errorf("invalid AI control revision")
	}
	return nil
}

func validAIControlAction(action string) bool {
	switch action {
	case AIControlActionDockerContainerStart, AIControlActionDockerContainerRestart,
		AIControlActionComposeServiceStart, AIControlActionComposeServiceRestart,
		AIControlActionSystemdServiceStart, AIControlActionSystemdServiceRestart:
		return true
	default:
		return false
	}
}

type SettingsStore struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

func NewSettingsStore(db *sql.DB) *SettingsStore {
	return NewSettingsStoreWithDialect(db, serverdb.DialectSQLite)
}

func NewSettingsStoreWithDialect(db *sql.DB, dialect serverdb.Dialect) *SettingsStore {
	return &SettingsStore{db: db, dialect: dialect}
}

func (s *SettingsStore) MetricsRetention(ctx context.Context, fallback time.Duration) (time.Duration, error) {
	value, err := s.MetricsRetentionValue(ctx, fallback)
	if err != nil {
		return 0, err
	}
	return ParseMetricsRetention(value)
}

func (s *SettingsStore) MetricsRetentionValue(ctx context.Context, fallback time.Duration) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE `+"`key`"+` = ?`, metricsRetentionKey).Scan(&value)
	if err == sql.ErrNoRows {
		return FormatMetricsRetention(fallback), nil
	}
	if err != nil {
		return "", err
	}
	retention, err := ParseMetricsRetention(value)
	if err != nil {
		return "", err
	}
	return FormatMetricsRetention(retention), nil
}

func (s *SettingsStore) SetMetricsRetention(ctx context.Context, value string) error {
	retention, err := ParseMetricsRetention(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, settingsUpsertSQL(s.dialect), metricsRetentionKey, FormatMetricsRetention(retention), formatTime(time.Now().UTC()))
	return err
}

func (s *SettingsStore) AIControlPolicy(ctx context.Context) (AIControlPolicy, error) {
	var policy AIControlPolicy
	var allowedActionsJSON, nodeScopeJSON, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT mode, allowed_actions_json, node_scope_json, revision, updated_at
		FROM ai_control_policy WHERE id = 1`).Scan(&policy.Mode, &allowedActionsJSON, &nodeScopeJSON, &policy.Revision, &updatedAt)
	if err == sql.ErrNoRows {
		return DefaultAIControlPolicy(), nil
	}
	if err != nil {
		return AIControlPolicy{}, err
	}
	if err := json.Unmarshal([]byte(allowedActionsJSON), &policy.AllowedActions); err != nil {
		return AIControlPolicy{}, fmt.Errorf("invalid AI control policy")
	}
	if err := json.Unmarshal([]byte(nodeScopeJSON), &policy.NodeScope); err != nil {
		return AIControlPolicy{}, fmt.Errorf("invalid AI control policy")
	}
	policy.AllowedActions = append([]string{}, policy.AllowedActions...)
	policy.NodeScope = append([]string{}, policy.NodeScope...)
	policy.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return AIControlPolicy{}, fmt.Errorf("invalid AI control policy")
	}
	if err := ValidateAIControlPolicy(policy); err != nil {
		return AIControlPolicy{}, err
	}
	return policy, nil
}

func (s *SettingsStore) SetAIControlPolicy(ctx context.Context, policy AIControlPolicy) (AIControlPolicy, error) {
	policy.Revision = 1
	policy.UpdatedAt = time.Time{}
	policy.AllowedActions = append([]string{}, policy.AllowedActions...)
	policy.NodeScope = append([]string{}, policy.NodeScope...)
	if err := ValidateAIControlPolicy(policy); err != nil {
		return AIControlPolicy{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIControlPolicy{}, err
	}
	defer tx.Rollback()
	var revision int64
	revisionQuery := `SELECT revision FROM ai_control_policy WHERE id = 1`
	if s.dialect == serverdb.DialectMySQL {
		revisionQuery += ` FOR UPDATE`
	}
	err = tx.QueryRowContext(ctx, revisionQuery).Scan(&revision)
	if err == sql.ErrNoRows {
		revision = DefaultAIControlPolicy().Revision
	} else if err != nil {
		return AIControlPolicy{}, err
	}
	policy.Revision = revision + 1
	policy.UpdatedAt = time.Now().UTC()
	allowedActionsJSON, err := json.Marshal(policy.AllowedActions)
	if err != nil {
		return AIControlPolicy{}, err
	}
	nodeScopeJSON, err := json.Marshal(policy.NodeScope)
	if err != nil {
		return AIControlPolicy{}, err
	}
	query := `INSERT INTO ai_control_policy (id, mode, allowed_actions_json, node_scope_json, revision, updated_at)
		VALUES (1, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET mode = excluded.mode,
		allowed_actions_json = excluded.allowed_actions_json, node_scope_json = excluded.node_scope_json,
		revision = excluded.revision, updated_at = excluded.updated_at`
	if s.dialect == serverdb.DialectMySQL {
		query = `INSERT INTO ai_control_policy (id, mode, allowed_actions_json, node_scope_json, revision, updated_at)
			VALUES (1, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE mode = VALUES(mode),
			allowed_actions_json = VALUES(allowed_actions_json), node_scope_json = VALUES(node_scope_json),
			revision = VALUES(revision), updated_at = VALUES(updated_at)`
	}
	if _, err := tx.ExecContext(ctx, query, policy.Mode, string(allowedActionsJSON), string(nodeScopeJSON), policy.Revision, formatTime(policy.UpdatedAt)); err != nil {
		return AIControlPolicy{}, err
	}
	if err := tx.Commit(); err != nil {
		return AIControlPolicy{}, err
	}
	return policy, nil
}

func settingsUpsertSQL(dialect serverdb.Dialect) string {
	if dialect == serverdb.DialectMySQL {
		return `INSERT INTO settings (` + "`key`" + `, value, updated_at) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`
	}
	return `INSERT INTO settings (` + "`key`" + `, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(` + "`key`" + `) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`
}

func ParseMetricsRetention(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if strings.HasSuffix(trimmed, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(trimmed, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid metrics retention")
		}
		return validateMetricsRetention(time.Duration(days) * 24 * time.Hour)
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid metrics retention")
	}
	return validateMetricsRetention(parsed)
}

func FormatMetricsRetention(retention time.Duration) string {
	if retention == 24*time.Hour {
		return "24h"
	}
	if retention%(24*time.Hour) == 0 && retention >= 48*time.Hour {
		return fmt.Sprintf("%dd", int(retention/(24*time.Hour)))
	}
	if retention%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(retention/time.Hour))
	}
	return retention.String()
}

func validateMetricsRetention(retention time.Duration) (time.Duration, error) {
	for _, allowed := range []time.Duration{6 * time.Hour, 24 * time.Hour, 3 * 24 * time.Hour, MetricsRetentionMax} {
		if retention == allowed {
			return retention, nil
		}
	}
	return 0, fmt.Errorf("metrics retention must be one of 6h, 24h, 3d, or 7d")
}
