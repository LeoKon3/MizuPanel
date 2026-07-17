package store

import (
	"database/sql"
	"encoding/json"
	"time"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

type AlertStore struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

func NewAlertStore(db *sql.DB) *AlertStore {
	return &AlertStore{
		db:      db,
		dialect: serverdb.DialectSQLite,
	}
}

type AlertRule struct {
	ID                   int64                 `json:"id"`
	Name                 string                `json:"name"`
	Enabled              bool                  `json:"enabled"`
	MetricField          string                `json:"metric_field"`
	Operator             string                `json:"operator"`
	Threshold            float64               `json:"threshold"`
	DurationSeconds      int                   `json:"duration_seconds"`
	ScopeType            string                `json:"scope_type"`
	ScopeNodeIDs         []string              `json:"scope_node_ids,omitempty"`
	NotificationChannels []NotificationChannel `json:"notification_channels"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

type NotificationChannel struct {
	Type       string            `json:"type"`
	WebhookURL string            `json:"webhook_url,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Secret     string            `json:"secret,omitempty"`
}

type AlertHistory struct {
	ID                              int64      `json:"id"`
	RuleID                          int64      `json:"rule_id"`
	RuleName                        string     `json:"rule_name"`
	NodeID                          string     `json:"node_id"`
	NodeName                        string     `json:"node_name"`
	MetricField                     string     `json:"metric_field"`
	MetricValue                     float64    `json:"metric_value"`
	Threshold                       float64    `json:"threshold"`
	TriggeredAt                     time.Time  `json:"triggered_at"`
	ResolvedAt                      *time.Time `json:"resolved_at,omitempty"`
	NotificationSent                bool       `json:"notification_sent"`
	NotificationError               string     `json:"notification_error,omitempty"`
	NotificationAttemptedAt         *time.Time `json:"notification_attempted_at,omitempty"`
	RecoveryNotificationSent        bool       `json:"recovery_notification_sent"`
	RecoveryNotificationError       string     `json:"recovery_notification_error,omitempty"`
	RecoveryNotificationAttemptedAt *time.Time `json:"recovery_notification_attempted_at,omitempty"`
	CreatedAt                       time.Time  `json:"created_at"`
}

func (s *AlertStore) CreateAlertRule(rule *AlertRule) error {
	scopeNodeIDsJSON, err := json.Marshal(rule.ScopeNodeIDs)
	if err != nil {
		return err
	}
	notificationChannelsJSON, err := json.Marshal(rule.NotificationChannels)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	result, err := s.db.Exec(`INSERT INTO alert_rules (name, enabled, metric_field, operator, threshold, duration_seconds, scope_type, scope_node_ids, notification_channels, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.Name, rule.Enabled, rule.MetricField, rule.Operator, rule.Threshold, rule.DurationSeconds, rule.ScopeType, string(scopeNodeIDsJSON), string(notificationChannelsJSON), now, now)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	rule.ID = id
	rule.CreatedAt = now
	rule.UpdatedAt = now
	return nil
}

func (s *AlertStore) GetAlertRules() ([]AlertRule, error) {
	rows, err := s.db.Query(`SELECT id, name, enabled, metric_field, operator, threshold, duration_seconds, scope_type, scope_node_ids, notification_channels, created_at, updated_at FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []AlertRule
	for rows.Next() {
		var rule AlertRule
		var scopeNodeIDsJSON, notificationChannelsJSON string
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Enabled, &rule.MetricField, &rule.Operator, &rule.Threshold, &rule.DurationSeconds, &rule.ScopeType, &scopeNodeIDsJSON, &notificationChannelsJSON, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(scopeNodeIDsJSON), &rule.ScopeNodeIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(notificationChannelsJSON), &rule.NotificationChannels); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *AlertStore) CreateAlertHistory(history *AlertHistory) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(`INSERT INTO alert_history (rule_id, rule_name, node_id, node_name, metric_field, metric_value, threshold, triggered_at, resolved_at, notification_sent, notification_error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		history.RuleID, history.RuleName, history.NodeID, history.NodeName, history.MetricField, history.MetricValue, history.Threshold, history.TriggeredAt, history.ResolvedAt, history.NotificationSent, history.NotificationError, now)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	history.ID = id
	history.CreatedAt = now
	return nil
}

func (s *AlertStore) GetAlertHistory(nodeID string, limit int) ([]AlertHistory, error) {
	query := `SELECT h.id, h.rule_id, COALESCE(r.name, h.rule_name), h.node_id, h.node_name, h.metric_field, h.metric_value, h.threshold, h.triggered_at, h.resolved_at, h.notification_sent, COALESCE(h.notification_error, ''), h.notification_attempted_at, h.recovery_notification_sent, COALESCE(h.recovery_notification_error, ''), h.recovery_notification_attempted_at, h.created_at
		FROM alert_history h
		LEFT JOIN alert_rules r ON r.id = h.rule_id
		WHERE h.node_id = ?
		ORDER BY h.triggered_at DESC
		LIMIT ?`
	rows, err := s.db.Query(query, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []AlertHistory
	for rows.Next() {
		var h AlertHistory
		if err := rows.Scan(&h.ID, &h.RuleID, &h.RuleName, &h.NodeID, &h.NodeName, &h.MetricField, &h.MetricValue, &h.Threshold, &h.TriggeredAt, &h.ResolvedAt, &h.NotificationSent, &h.NotificationError, &h.NotificationAttemptedAt, &h.RecoveryNotificationSent, &h.RecoveryNotificationError, &h.RecoveryNotificationAttemptedAt, &h.CreatedAt); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}

func (s *AlertStore) GetAlertHistoryByID(id int64) (*AlertHistory, error) {
	var h AlertHistory
	err := s.db.QueryRow(`SELECT h.id, h.rule_id, COALESCE(r.name, h.rule_name), h.node_id, h.node_name, h.metric_field, h.metric_value, h.threshold, h.triggered_at, h.resolved_at, h.notification_sent, COALESCE(h.notification_error, ''), h.notification_attempted_at, h.recovery_notification_sent, COALESCE(h.recovery_notification_error, ''), h.recovery_notification_attempted_at, h.created_at
		FROM alert_history h
		LEFT JOIN alert_rules r ON r.id = h.rule_id
		WHERE h.id = ?`, id).
		Scan(&h.ID, &h.RuleID, &h.RuleName, &h.NodeID, &h.NodeName, &h.MetricField, &h.MetricValue, &h.Threshold, &h.TriggeredAt, &h.ResolvedAt, &h.NotificationSent, &h.NotificationError, &h.NotificationAttemptedAt, &h.RecoveryNotificationSent, &h.RecoveryNotificationError, &h.RecoveryNotificationAttemptedAt, &h.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *AlertStore) UpdateAlertHistoryResolved(id int64, resolvedAt time.Time) error {
	_, err := s.db.Exec(`UPDATE alert_history SET resolved_at = ? WHERE id = ?`, resolvedAt, id)
	return err
}

func (s *AlertStore) UpdateAlertHistoryNotificationResult(id int64, sent bool, notificationError string, attemptedAt time.Time) error {
	_, err := s.db.Exec(`UPDATE alert_history SET notification_sent = ?, notification_error = ?, notification_attempted_at = ? WHERE id = ?`, sent, notificationError, attemptedAt, id)
	return err
}

func (s *AlertStore) UpdateAlertHistoryRecoveryNotificationResult(id int64, sent bool, notificationError string, attemptedAt time.Time) error {
	_, err := s.db.Exec(`UPDATE alert_history SET recovery_notification_sent = ?, recovery_notification_error = ?, recovery_notification_attempted_at = ? WHERE id = ?`, sent, notificationError, attemptedAt, id)
	return err
}

func (s *AlertStore) ResolveAlertHistory(id int64, resolvedAt time.Time) (*AlertHistory, error) {
	history, err := s.GetAlertHistoryByID(id)
	if err != nil || history == nil {
		return history, err
	}
	if history.ResolvedAt == nil {
		if _, err := s.db.Exec(`UPDATE alert_history SET resolved_at = ? WHERE id = ? AND resolved_at IS NULL`, resolvedAt, id); err != nil {
			return nil, err
		}
		return s.GetAlertHistoryByID(id)
	}
	return history, nil
}

func (s *AlertStore) DeleteResolvedAlertHistory(id int64) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM alert_history WHERE id = ? AND resolved_at IS NOT NULL`, id)
	if err != nil {
		return false, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (s *AlertStore) DeleteResolvedAlertHistories(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var deleted int64
	for _, id := range ids {
		result, err := tx.Exec(`DELETE FROM alert_history WHERE id = ? AND resolved_at IS NOT NULL`, id)
		if err != nil {
			return 0, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		deleted += rows
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (s *AlertStore) ResolveActiveAlertHistoryByRuleID(ruleID int64, resolvedAt time.Time) (int64, error) {
	result, err := s.db.Exec(`UPDATE alert_history SET resolved_at = ? WHERE rule_id = ? AND resolved_at IS NULL`, resolvedAt, ruleID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *AlertStore) ResolveActiveAlertHistoryForDisabledRules(resolvedAt time.Time) (int64, error) {
	result, err := s.db.Exec(`UPDATE alert_history
		SET resolved_at = ?
		WHERE resolved_at IS NULL
			AND rule_id IN (SELECT id FROM alert_rules WHERE enabled = 0)`, resolvedAt)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *AlertStore) UpdateAlertHistoryRuleNameByRuleID(ruleID int64, ruleName string) (int64, error) {
	result, err := s.db.Exec(`UPDATE alert_history SET rule_name = ? WHERE rule_id = ?`, ruleName, ruleID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// UpdateAlertHistoryMetricValue updates the metric_value for an active alert
func (s *AlertStore) UpdateAlertHistoryMetricValue(id int64, metricValue float64) error {
	_, err := s.db.Exec(`UPDATE alert_history SET metric_value = ? WHERE id = ?`, metricValue, id)
	return err
}

// GetActiveAlertHistory returns all unresolved alerts (resolved_at IS NULL)
func (s *AlertStore) GetActiveAlertHistory() ([]AlertHistory, error) {
	query := `SELECT h.id, h.rule_id, COALESCE(r.name, h.rule_name), h.node_id, h.node_name, h.metric_field, h.metric_value, h.threshold, h.triggered_at, h.resolved_at, h.notification_sent, COALESCE(h.notification_error, ''), h.notification_attempted_at, h.recovery_notification_sent, COALESCE(h.recovery_notification_error, ''), h.recovery_notification_attempted_at, h.created_at
		FROM alert_history h
		LEFT JOIN alert_rules r ON r.id = h.rule_id
		WHERE h.resolved_at IS NULL
		ORDER BY h.triggered_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []AlertHistory
	for rows.Next() {
		var h AlertHistory
		if err := rows.Scan(&h.ID, &h.RuleID, &h.RuleName, &h.NodeID, &h.NodeName, &h.MetricField, &h.MetricValue, &h.Threshold, &h.TriggeredAt, &h.ResolvedAt, &h.NotificationSent, &h.NotificationError, &h.NotificationAttemptedAt, &h.RecoveryNotificationSent, &h.RecoveryNotificationError, &h.RecoveryNotificationAttemptedAt, &h.CreatedAt); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}

func (s *AlertStore) GetAlertRule(id int64) (*AlertRule, error) {
	var rule AlertRule
	var scopeNodeIDsJSON, notificationChannelsJSON string
	err := s.db.QueryRow(`SELECT id, name, enabled, metric_field, operator, threshold, duration_seconds, scope_type, scope_node_ids, notification_channels, created_at, updated_at FROM alert_rules WHERE id = ?`, id).
		Scan(&rule.ID, &rule.Name, &rule.Enabled, &rule.MetricField, &rule.Operator, &rule.Threshold, &rule.DurationSeconds, &rule.ScopeType, &scopeNodeIDsJSON, &notificationChannelsJSON, &rule.CreatedAt, &rule.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(scopeNodeIDsJSON), &rule.ScopeNodeIDs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(notificationChannelsJSON), &rule.NotificationChannels); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *AlertStore) UpdateAlertRule(rule *AlertRule) error {
	scopeNodeIDsJSON, err := json.Marshal(rule.ScopeNodeIDs)
	if err != nil {
		return err
	}
	notificationChannelsJSON, err := json.Marshal(rule.NotificationChannels)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	_, err = s.db.Exec(`UPDATE alert_rules SET name = ?, enabled = ?, metric_field = ?, operator = ?, threshold = ?, duration_seconds = ?, scope_type = ?, scope_node_ids = ?, notification_channels = ?, updated_at = ? WHERE id = ?`,
		rule.Name, rule.Enabled, rule.MetricField, rule.Operator, rule.Threshold, rule.DurationSeconds, rule.ScopeType, string(scopeNodeIDsJSON), string(notificationChannelsJSON), now, rule.ID)
	if err != nil {
		return err
	}
	rule.UpdatedAt = now
	return nil
}

func (s *AlertStore) DeleteAlertRule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM alert_rules WHERE id = ?`, id)
	return err
}

func (s *AlertStore) GetEnabledAlertRules() ([]AlertRule, error) {
	rows, err := s.db.Query(`SELECT id, name, enabled, metric_field, operator, threshold, duration_seconds, scope_type, scope_node_ids, notification_channels, created_at, updated_at FROM alert_rules WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []AlertRule
	for rows.Next() {
		var rule AlertRule
		var scopeNodeIDsJSON, notificationChannelsJSON string
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Enabled, &rule.MetricField, &rule.Operator, &rule.Threshold, &rule.DurationSeconds, &rule.ScopeType, &scopeNodeIDsJSON, &notificationChannelsJSON, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(scopeNodeIDsJSON), &rule.ScopeNodeIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(notificationChannelsJSON), &rule.NotificationChannels); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}
