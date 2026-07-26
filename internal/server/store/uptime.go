package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

const (
	MaxUptimeResults   = 200
	MaxUptimeIncidents = 100

	UptimeStatusPending = "pending"
	UptimeStatusUp      = "up"
	UptimeStatusWarning = "warning"
	UptimeStatusDown    = "down"

	UptimeIncidentAvailability = "availability"
	UptimeIncidentCertificate  = "certificate"
)

type UptimeStore struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

func NewUptimeStore(db *sql.DB) *UptimeStore {
	return NewUptimeStoreWithDialect(db, serverdb.DialectSQLite)
}

func NewUptimeStoreWithDialect(db *sql.DB, dialect serverdb.Dialect) *UptimeStore {
	return &UptimeStore{db: db, dialect: dialect}
}

type UptimeMonitor struct {
	ID                     int64                 `json:"id"`
	Name                   string                `json:"name"`
	Type                   string                `json:"type"`
	Target                 string                `json:"target"`
	Enabled                bool                  `json:"enabled"`
	IntervalSeconds        int                   `json:"interval_seconds"`
	TimeoutSeconds         int                   `json:"timeout_seconds"`
	FailureThreshold       int                   `json:"failure_threshold"`
	ExpectedStatusMin      int                   `json:"expected_status_min"`
	ExpectedStatusMax      int                   `json:"expected_status_max"`
	TLSExpiryThresholdDays int                   `json:"tls_expiry_threshold_days"`
	NotificationChannels   []NotificationChannel `json:"notification_channels"`
	Status                 string                `json:"status"`
	ConsecutiveFailures    int                   `json:"consecutive_failures"`
	LastLatencyMS          int64                 `json:"last_latency_ms"`
	LastStatusCode         int                   `json:"last_status_code"`
	LastError              string                `json:"last_error,omitempty"`
	LastCheckedAt          *time.Time            `json:"last_checked_at,omitempty"`
	TLSExpiresAt           *time.Time            `json:"tls_expires_at,omitempty"`
	TLSRemainingDays       *int                  `json:"tls_remaining_days"`
	CreatedAt              time.Time             `json:"created_at"`
	UpdatedAt              time.Time             `json:"updated_at"`
}

type UptimeResult struct {
	ID           int64      `json:"id"`
	MonitorID    int64      `json:"monitor_id"`
	Success      bool       `json:"success"`
	LatencyMS    int64      `json:"latency_ms"`
	StatusCode   int        `json:"status_code,omitempty"`
	Error        string     `json:"error,omitempty"`
	TLSExpiresAt *time.Time `json:"tls_expires_at,omitempty"`
	CheckedAt    time.Time  `json:"checked_at"`
}

type UptimeIncident struct {
	ID                              int64      `json:"id"`
	MonitorID                       int64      `json:"monitor_id"`
	Kind                            string     `json:"kind"`
	Message                         string     `json:"message"`
	StartedAt                       time.Time  `json:"started_at"`
	ResolvedAt                      *time.Time `json:"resolved_at,omitempty"`
	NotificationSent                bool       `json:"notification_sent"`
	NotificationError               string     `json:"notification_error,omitempty"`
	NotificationAttemptedAt         *time.Time `json:"notification_attempted_at,omitempty"`
	RecoveryNotificationSent        bool       `json:"recovery_notification_sent"`
	RecoveryNotificationError       string     `json:"recovery_notification_error,omitempty"`
	RecoveryNotificationAttemptedAt *time.Time `json:"recovery_notification_attempted_at,omitempty"`
	CreatedAt                       time.Time  `json:"created_at"`
}

type UptimeProbeResult struct {
	Success      bool
	LatencyMS    int64
	StatusCode   int
	Error        string
	TLSExpiresAt *time.Time
	TLSChecked   bool
	TLSExpiring  bool
	CheckedAt    time.Time
}

type UptimeTransition struct {
	Monitor   UptimeMonitor
	Result    UptimeResult
	Triggered []UptimeIncident
	Resolved  []UptimeIncident
}

const uptimeMonitorColumns = `id, name, type, target, enabled, interval_seconds, timeout_seconds,
	failure_threshold, expected_status_min, expected_status_max, tls_expiry_threshold_days,
	notification_channels, status, consecutive_failures, last_latency_ms, last_status_code,
	last_error, last_checked_at, tls_expires_at, created_at, updated_at`

func (s *UptimeStore) CreateMonitor(ctx context.Context, monitor *UptimeMonitor) error {
	channels, err := json.Marshal(nonNilNotificationChannels(monitor.NotificationChannels))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO uptime_monitors (
		name, type, target, enabled, interval_seconds, timeout_seconds, failure_threshold,
		expected_status_min, expected_status_max, tls_expiry_threshold_days,
		notification_channels, status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		monitor.Name, monitor.Type, monitor.Target, monitor.Enabled, monitor.IntervalSeconds,
		monitor.TimeoutSeconds, monitor.FailureThreshold, monitor.ExpectedStatusMin,
		monitor.ExpectedStatusMax, monitor.TLSExpiryThresholdDays, string(channels),
		UptimeStatusPending, formatTime(now), formatTime(now))
	if err != nil {
		return err
	}
	monitor.ID, err = result.LastInsertId()
	if err != nil {
		return err
	}
	created, err := s.GetMonitor(ctx, monitor.ID)
	if err != nil {
		return err
	}
	if created == nil {
		return sql.ErrNoRows
	}
	*monitor = *created
	return nil
}

func (s *UptimeStore) GetMonitor(ctx context.Context, id int64) (*UptimeMonitor, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+uptimeMonitorColumns+` FROM uptime_monitors WHERE id = ?`, id)
	monitor, err := scanUptimeMonitor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &monitor, nil
}

func (s *UptimeStore) ListMonitors(ctx context.Context) ([]UptimeMonitor, error) {
	return s.listMonitors(ctx, false)
}

func (s *UptimeStore) ListEnabledMonitors(ctx context.Context) ([]UptimeMonitor, error) {
	return s.listMonitors(ctx, true)
}

func (s *UptimeStore) listMonitors(ctx context.Context, enabledOnly bool) ([]UptimeMonitor, error) {
	query := `SELECT ` + uptimeMonitorColumns + ` FROM uptime_monitors`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	monitors := make([]UptimeMonitor, 0)
	for rows.Next() {
		monitor, err := scanUptimeMonitor(rows)
		if err != nil {
			return nil, err
		}
		monitors = append(monitors, monitor)
	}
	return monitors, rows.Err()
}

func (s *UptimeStore) UpdateMonitor(ctx context.Context, monitor *UptimeMonitor) (bool, error) {
	existing, err := s.GetMonitor(ctx, monitor.ID)
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, sql.ErrNoRows
	}
	materialChange := uptimeMateriallyChanged(*existing, *monitor)
	channels, err := json.Marshal(nonNilNotificationChannels(monitor.NotificationChannels))
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE uptime_monitors SET
		name = ?, type = ?, target = ?, interval_seconds = ?, timeout_seconds = ?,
		failure_threshold = ?, expected_status_min = ?, expected_status_max = ?,
		tls_expiry_threshold_days = ?, notification_channels = ?, updated_at = ?
		WHERE id = ?`, monitor.Name, monitor.Type, monitor.Target, monitor.IntervalSeconds,
		monitor.TimeoutSeconds, monitor.FailureThreshold, monitor.ExpectedStatusMin,
		monitor.ExpectedStatusMax, monitor.TLSExpiryThresholdDays, string(channels),
		formatTime(now), monitor.ID); err != nil {
		return false, err
	}
	if materialChange {
		if _, err := tx.ExecContext(ctx, `UPDATE uptime_monitors SET
			status = ?, consecutive_failures = 0, last_latency_ms = 0,
			last_status_code = 0, last_error = '', last_checked_at = NULL,
			tls_expires_at = NULL WHERE id = ?`, UptimeStatusPending, monitor.ID); err != nil {
			return false, err
		}
		if err := resolveActiveUptimeIncidentsTx(ctx, tx, monitor.ID, now); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	updated, err := s.GetMonitor(ctx, monitor.ID)
	if err != nil {
		return false, err
	}
	if updated == nil {
		return false, sql.ErrNoRows
	}
	*monitor = *updated
	return materialChange, nil
}

func (s *UptimeStore) SetMonitorEnabled(ctx context.Context, id int64, enabled bool) (*UptimeMonitor, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE uptime_monitors SET enabled = ?, updated_at = ? WHERE id = ?`, enabled, formatTime(now), id)
	if err != nil {
		return nil, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, sql.ErrNoRows
	}
	if enabled {
		if _, err := tx.ExecContext(ctx, `UPDATE uptime_monitors SET status = ?, consecutive_failures = 0, last_checked_at = NULL WHERE id = ?`, UptimeStatusPending, id); err != nil {
			return nil, err
		}
	} else if err := resolveActiveUptimeIncidentsTx(ctx, tx, id, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMonitor(ctx, id)
}

func (s *UptimeStore) DeleteMonitor(ctx context.Context, id int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM uptime_monitors WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func (s *UptimeStore) ApplyProbe(ctx context.Context, monitorID int64, probe UptimeProbeResult) (UptimeTransition, error) {
	if probe.CheckedAt.IsZero() {
		probe.CheckedAt = time.Now().UTC()
	} else {
		probe.CheckedAt = probe.CheckedAt.UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UptimeTransition{}, err
	}
	defer tx.Rollback()
	monitor, err := scanUptimeMonitor(tx.QueryRowContext(ctx, `SELECT `+uptimeMonitorColumns+` FROM uptime_monitors WHERE id = ?`, monitorID))
	if err != nil {
		return UptimeTransition{}, err
	}
	status := UptimeStatusUp
	failures := 0
	if !probe.Success {
		status = UptimeStatusDown
		failures = monitor.ConsecutiveFailures + 1
	} else if probe.TLSExpiring {
		status = UptimeStatusWarning
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO uptime_results (
		monitor_id, success, latency_ms, status_code, error, tls_expires_at, checked_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`, monitorID, probe.Success, probe.LatencyMS,
		probe.StatusCode, probe.Error, nullableTimeString(probe.TLSExpiresAt), formatTime(probe.CheckedAt))
	if err != nil {
		return UptimeTransition{}, err
	}
	resultID, err := result.LastInsertId()
	if err != nil {
		return UptimeTransition{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE uptime_monitors SET
		status = ?, consecutive_failures = ?, last_latency_ms = ?, last_status_code = ?,
		last_error = ?, last_checked_at = ?, tls_expires_at = ?, updated_at = ? WHERE id = ?`,
		status, failures, probe.LatencyMS, probe.StatusCode, probe.Error,
		formatTime(probe.CheckedAt), nullableTimeString(probe.TLSExpiresAt),
		formatTime(probe.CheckedAt), monitorID); err != nil {
		return UptimeTransition{}, err
	}
	transition := UptimeTransition{
		Result: UptimeResult{
			ID: resultID, MonitorID: monitorID, Success: probe.Success, LatencyMS: probe.LatencyMS,
			StatusCode: probe.StatusCode, Error: probe.Error, TLSExpiresAt: probe.TLSExpiresAt,
			CheckedAt: probe.CheckedAt,
		},
		Triggered: make([]UptimeIncident, 0),
		Resolved:  make([]UptimeIncident, 0),
	}
	if err := applyAvailabilityTransitionTx(ctx, tx, monitor, probe, failures, &transition); err != nil {
		return UptimeTransition{}, err
	}
	if probe.Success && probe.TLSChecked {
		if err := applyCertificateTransitionTx(ctx, tx, monitor, probe, &transition); err != nil {
			return UptimeTransition{}, err
		}
	}
	if err := pruneUptimeResultsTx(ctx, tx, monitorID); err != nil {
		return UptimeTransition{}, err
	}
	if err := tx.Commit(); err != nil {
		return UptimeTransition{}, err
	}
	updated, err := s.GetMonitor(ctx, monitorID)
	if err != nil {
		return UptimeTransition{}, err
	}
	if updated == nil {
		return UptimeTransition{}, sql.ErrNoRows
	}
	transition.Monitor = *updated
	return transition, nil
}

func (s *UptimeStore) ListResults(ctx context.Context, monitorID int64, limit int) ([]UptimeResult, error) {
	if limit <= 0 || limit > MaxUptimeResults {
		limit = MaxUptimeResults
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, monitor_id, success, latency_ms, status_code, error, tls_expires_at, checked_at
		FROM uptime_results WHERE monitor_id = ? ORDER BY id DESC LIMIT ?`, monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]UptimeResult, 0)
	for rows.Next() {
		result, err := scanUptimeResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (s *UptimeStore) ListIncidents(ctx context.Context, monitorID int64, limit int) ([]UptimeIncident, error) {
	if limit <= 0 || limit > MaxUptimeIncidents {
		limit = MaxUptimeIncidents
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, monitor_id, kind, message, started_at, resolved_at,
		notification_sent, notification_error, notification_attempted_at,
		recovery_notification_sent, recovery_notification_error, recovery_notification_attempted_at, created_at
		FROM uptime_incidents WHERE monitor_id = ? ORDER BY id DESC LIMIT ?`, monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	incidents := make([]UptimeIncident, 0)
	for rows.Next() {
		incident, err := scanUptimeIncident(rows)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, incident)
	}
	return incidents, rows.Err()
}

func (s *UptimeStore) UpdateIncidentNotification(ctx context.Context, id int64, recovery bool, sent bool, message string, attemptedAt time.Time) error {
	if recovery {
		_, err := s.db.ExecContext(ctx, `UPDATE uptime_incidents SET recovery_notification_sent = ?, recovery_notification_error = ?, recovery_notification_attempted_at = ? WHERE id = ?`, sent, message, formatTime(attemptedAt.UTC()), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE uptime_incidents SET notification_sent = ?, notification_error = ?, notification_attempted_at = ? WHERE id = ?`, sent, message, formatTime(attemptedAt.UTC()), id)
	return err
}

func uptimeMateriallyChanged(existing UptimeMonitor, next UptimeMonitor) bool {
	return existing.Type != next.Type || existing.Target != next.Target ||
		existing.IntervalSeconds != next.IntervalSeconds || existing.TimeoutSeconds != next.TimeoutSeconds ||
		existing.FailureThreshold != next.FailureThreshold || existing.ExpectedStatusMin != next.ExpectedStatusMin ||
		existing.ExpectedStatusMax != next.ExpectedStatusMax || existing.TLSExpiryThresholdDays != next.TLSExpiryThresholdDays
}

func applyAvailabilityTransitionTx(ctx context.Context, tx *sql.Tx, monitor UptimeMonitor, probe UptimeProbeResult, failures int, transition *UptimeTransition) error {
	active, err := activeUptimeIncidentTx(ctx, tx, monitor.ID, UptimeIncidentAvailability)
	if err != nil {
		return err
	}
	if probe.Success {
		if active != nil {
			resolved, err := resolveUptimeIncidentTx(ctx, tx, *active, probe.CheckedAt)
			if err != nil {
				return err
			}
			transition.Resolved = append(transition.Resolved, resolved)
		}
		return nil
	}
	if active != nil || failures < monitor.FailureThreshold {
		return nil
	}
	message := fmt.Sprintf("连续 %d 次检测失败", failures)
	if probe.Error != "" {
		message += ": " + probe.Error
	}
	incident, err := createUptimeIncidentTx(ctx, tx, monitor.ID, UptimeIncidentAvailability, message, probe.CheckedAt)
	if err != nil {
		return err
	}
	transition.Triggered = append(transition.Triggered, incident)
	return nil
}

func applyCertificateTransitionTx(ctx context.Context, tx *sql.Tx, monitor UptimeMonitor, probe UptimeProbeResult, transition *UptimeTransition) error {
	active, err := activeUptimeIncidentTx(ctx, tx, monitor.ID, UptimeIncidentCertificate)
	if err != nil {
		return err
	}
	if !probe.TLSExpiring {
		if active != nil {
			resolved, err := resolveUptimeIncidentTx(ctx, tx, *active, probe.CheckedAt)
			if err != nil {
				return err
			}
			transition.Resolved = append(transition.Resolved, resolved)
		}
		return nil
	}
	if active != nil {
		return nil
	}
	days := 0
	if probe.TLSExpiresAt != nil {
		days = int(probe.TLSExpiresAt.Sub(probe.CheckedAt).Hours() / 24)
	}
	message := fmt.Sprintf("HTTPS 证书将在 %d 天内到期", days)
	incident, err := createUptimeIncidentTx(ctx, tx, monitor.ID, UptimeIncidentCertificate, message, probe.CheckedAt)
	if err != nil {
		return err
	}
	transition.Triggered = append(transition.Triggered, incident)
	return nil
}

func activeUptimeIncidentTx(ctx context.Context, tx *sql.Tx, monitorID int64, kind string) (*UptimeIncident, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, monitor_id, kind, message, started_at, resolved_at,
		notification_sent, notification_error, notification_attempted_at,
		recovery_notification_sent, recovery_notification_error, recovery_notification_attempted_at, created_at
		FROM uptime_incidents WHERE monitor_id = ? AND kind = ? AND active_marker = 1`, monitorID, kind)
	incident, err := scanUptimeIncident(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &incident, nil
}

func createUptimeIncidentTx(ctx context.Context, tx *sql.Tx, monitorID int64, kind string, message string, startedAt time.Time) (UptimeIncident, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO uptime_incidents (monitor_id, kind, message, started_at, active_marker, created_at) VALUES (?, ?, ?, ?, 1, ?)`, monitorID, kind, message, formatTime(startedAt.UTC()), formatTime(startedAt.UTC()))
	if err != nil {
		return UptimeIncident{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return UptimeIncident{}, err
	}
	return UptimeIncident{ID: id, MonitorID: monitorID, Kind: kind, Message: message, StartedAt: startedAt.UTC(), CreatedAt: startedAt.UTC()}, nil
}

func resolveUptimeIncidentTx(ctx context.Context, tx *sql.Tx, incident UptimeIncident, resolvedAt time.Time) (UptimeIncident, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE uptime_incidents SET resolved_at = ?, active_marker = NULL WHERE id = ? AND active_marker = 1`, formatTime(resolvedAt.UTC()), incident.ID); err != nil {
		return UptimeIncident{}, err
	}
	value := resolvedAt.UTC()
	incident.ResolvedAt = &value
	return incident, nil
}

func resolveActiveUptimeIncidentsTx(ctx context.Context, tx *sql.Tx, monitorID int64, resolvedAt time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE uptime_incidents SET resolved_at = ?, active_marker = NULL WHERE monitor_id = ? AND active_marker = 1`, formatTime(resolvedAt.UTC()), monitorID)
	return err
}

func pruneUptimeResultsTx(ctx context.Context, tx *sql.Tx, monitorID int64) error {
	var boundaryID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM uptime_results WHERE monitor_id = ? ORDER BY id DESC LIMIT 1 OFFSET ?`, monitorID, MaxUptimeResults-1).Scan(&boundaryID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM uptime_results WHERE monitor_id = ? AND id < ?`, monitorID, boundaryID)
	return err
}

type uptimeScanner interface {
	Scan(dest ...any) error
}

func scanUptimeMonitor(scanner uptimeScanner) (UptimeMonitor, error) {
	var monitor UptimeMonitor
	var channelsJSON string
	var lastChecked, tlsExpires sql.NullString
	var createdAt, updatedAt string
	err := scanner.Scan(&monitor.ID, &monitor.Name, &monitor.Type, &monitor.Target, &monitor.Enabled,
		&monitor.IntervalSeconds, &monitor.TimeoutSeconds, &monitor.FailureThreshold,
		&monitor.ExpectedStatusMin, &monitor.ExpectedStatusMax, &monitor.TLSExpiryThresholdDays,
		&channelsJSON, &monitor.Status, &monitor.ConsecutiveFailures, &monitor.LastLatencyMS,
		&monitor.LastStatusCode, &monitor.LastError, &lastChecked, &tlsExpires, &createdAt, &updatedAt)
	if err != nil {
		return UptimeMonitor{}, err
	}
	if err := json.Unmarshal([]byte(channelsJSON), &monitor.NotificationChannels); err != nil {
		return UptimeMonitor{}, err
	}
	monitor.NotificationChannels = nonNilNotificationChannels(monitor.NotificationChannels)
	if monitor.LastCheckedAt, err = parseNullableTime(lastChecked); err != nil {
		return UptimeMonitor{}, err
	}
	if monitor.TLSExpiresAt, err = parseNullableTime(tlsExpires); err != nil {
		return UptimeMonitor{}, err
	}
	monitor.TLSRemainingDays = uptimeTLSRemainingDays(monitor.LastCheckedAt, monitor.TLSExpiresAt)
	if monitor.CreatedAt, err = parseTime(createdAt); err != nil {
		return UptimeMonitor{}, err
	}
	if monitor.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return UptimeMonitor{}, err
	}
	return monitor, nil
}

func uptimeTLSRemainingDays(checkedAt *time.Time, expiresAt *time.Time) *int {
	if checkedAt == nil || expiresAt == nil {
		return nil
	}
	remaining := expiresAt.Sub(*checkedAt)
	days := int(remaining / (24 * time.Hour))
	if remaining > 0 && remaining%(24*time.Hour) != 0 {
		days++
	}
	if days < 0 {
		days = 0
	}
	return &days
}

func scanUptimeResult(scanner uptimeScanner) (UptimeResult, error) {
	var result UptimeResult
	var tlsExpires sql.NullString
	var checkedAt string
	err := scanner.Scan(&result.ID, &result.MonitorID, &result.Success, &result.LatencyMS,
		&result.StatusCode, &result.Error, &tlsExpires, &checkedAt)
	if err != nil {
		return UptimeResult{}, err
	}
	if result.TLSExpiresAt, err = parseNullableTime(tlsExpires); err != nil {
		return UptimeResult{}, err
	}
	if result.CheckedAt, err = parseTime(checkedAt); err != nil {
		return UptimeResult{}, err
	}
	return result, nil
}

func scanUptimeIncident(scanner uptimeScanner) (UptimeIncident, error) {
	var incident UptimeIncident
	var startedAt, createdAt string
	var resolvedAt, notificationAttemptedAt, recoveryAttemptedAt sql.NullString
	err := scanner.Scan(&incident.ID, &incident.MonitorID, &incident.Kind, &incident.Message,
		&startedAt, &resolvedAt, &incident.NotificationSent, &incident.NotificationError,
		&notificationAttemptedAt, &incident.RecoveryNotificationSent,
		&incident.RecoveryNotificationError, &recoveryAttemptedAt, &createdAt)
	if err != nil {
		return UptimeIncident{}, err
	}
	if incident.StartedAt, err = parseTime(startedAt); err != nil {
		return UptimeIncident{}, err
	}
	if incident.ResolvedAt, err = parseNullableTime(resolvedAt); err != nil {
		return UptimeIncident{}, err
	}
	if incident.NotificationAttemptedAt, err = parseNullableTime(notificationAttemptedAt); err != nil {
		return UptimeIncident{}, err
	}
	if incident.RecoveryNotificationAttemptedAt, err = parseNullableTime(recoveryAttemptedAt); err != nil {
		return UptimeIncident{}, err
	}
	if incident.CreatedAt, err = parseTime(createdAt); err != nil {
		return UptimeIncident{}, err
	}
	return incident, nil
}

func nullableTimeString(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(value.UTC())
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func nonNilNotificationChannels(channels []NotificationChannel) []NotificationChannel {
	if channels == nil {
		return make([]NotificationChannel, 0)
	}
	return channels
}
