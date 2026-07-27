package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

const (
	DefaultTaskTimeoutSeconds = protocol.ScriptExecutionDefaultTimeoutSeconds
	MaxTaskTimeoutSeconds     = protocol.ScriptExecutionMaxTimeoutSeconds
	MaxAutomationNameRunes    = 128
	MaxScriptDescriptionBytes = 2048
	MaxScriptContentBytes     = protocol.ScriptExecutionMaxScriptBytes
	MaxCronExpressionBytes    = 128
	MaxTaskTimezoneBytes      = 128
	MaxTaskNodes              = 100
	MaxTaskNodeIDBytes        = 191
	MaxTaskNodeNameBytes      = 255
	MaxTaskOutputBytes        = protocol.ScriptExecutionMaxOutputBytes
	MaxTaskErrorBytes         = 1024
	MaxNotificationJSONBytes  = 64 << 10
	DefaultRunPageLimit       = 50
	MaxRunPageLimit           = 100
	MaxDueTaskLimit           = 100

	NotificationPolicyNever   = "never"
	NotificationPolicyFailure = "failure"
	NotificationPolicyAlways  = "always"

	RunTriggerManual    = "manual"
	RunTriggerScheduled = "scheduled"

	RunStatusQueued      = "queued"
	RunStatusRunning     = "running"
	RunStatusSuccess     = "success"
	RunStatusPartial     = "partial"
	RunStatusFailed      = "failed"
	RunStatusSkipped     = "skipped"
	RunStatusInterrupted = "interrupted"

	TargetStatusQueued      = "queued"
	TargetStatusRunning     = "running"
	TargetStatusSuccess     = "success"
	TargetStatusFailed      = "failed"
	TargetStatusTimedOut    = "timed_out"
	TargetStatusBusy        = "busy"
	TargetStatusCancelled   = "cancelled"
	TargetStatusOffline     = "offline"
	TargetStatusUnsupported = "unsupported"
	TargetStatusSkipped     = "skipped"
	TargetStatusInterrupted = "interrupted"
)

var (
	ErrInvalid   = errors.New("invalid automation value")
	ErrNotFound  = errors.New("automation resource not found")
	ErrConflict  = errors.New("automation resource conflict")
	ErrClaimLost = errors.New("automation claim lost")
)

type TaskStore struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

func NewTaskStore(database *sql.DB) *TaskStore {
	return NewTaskStoreWithDialect(database, serverdb.DialectSQLite)
}

func NewTaskStoreWithDialect(database *sql.DB, dialect serverdb.Dialect) *TaskStore {
	return &TaskStore{db: database, dialect: dialect}
}

type AutomationScript struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	NormalizedName string    `json:"-"`
	Description    string    `json:"description"`
	Content        string    `json:"content"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Revision       int       `json:"revision"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ScheduledTask struct {
	ID                   int64                 `json:"id"`
	Name                 string                `json:"name"`
	NormalizedName       string                `json:"-"`
	ScriptID             int64                 `json:"script_id"`
	ScriptName           string                `json:"script_name"`
	ScriptRevision       int                   `json:"script_revision"`
	CronExpression       string                `json:"cron_expression"`
	Timezone             string                `json:"timezone"`
	Enabled              bool                  `json:"enabled"`
	TimeoutSeconds       int                   `json:"timeout_seconds"`
	NotificationPolicy   string                `json:"notification_policy"`
	NotificationChannels []NotificationChannel `json:"notification_channels"`
	NodeIDs              []string              `json:"node_ids"`
	NextRunAt            *time.Time            `json:"next_run_at"`
	LastScheduledAt      *time.Time            `json:"last_scheduled_at"`
	LatestRunStatus      *string               `json:"latest_run_status"`
	LatestRunAt          *time.Time            `json:"latest_run_at"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

type TaskRun struct {
	ID                      int64                 `json:"id"`
	TaskID                  *int64                `json:"task_id,omitempty"`
	ScriptID                *int64                `json:"script_id,omitempty"`
	TaskName                string                `json:"task_name"`
	ScriptName              string                `json:"script_name"`
	ScriptRevision          int                   `json:"script_revision"`
	ScriptContent           string                `json:"-"`
	TimeoutSeconds          int                   `json:"timeout_seconds"`
	NotificationPolicy      string                `json:"notification_policy"`
	NotificationChannels    []NotificationChannel `json:"-"`
	Trigger                 string                `json:"trigger"`
	ScheduledFor            *time.Time            `json:"scheduled_for"`
	Status                  string                `json:"status"`
	TotalTargets            int                   `json:"total_targets"`
	CompletedTargets        int                   `json:"completed_targets"`
	SuccessTargets          int                   `json:"success_targets"`
	FailedTargets           int                   `json:"failed_targets"`
	Error                   string                `json:"error,omitempty"`
	NotificationSent        bool                  `json:"notification_sent"`
	NotificationError       string                `json:"notification_error,omitempty"`
	NotificationAttemptedAt *time.Time            `json:"notification_attempted_at"`
	StartedAt               *time.Time            `json:"started_at"`
	CompletedAt             *time.Time            `json:"completed_at"`
	CreatedAt               time.Time             `json:"created_at"`
	UpdatedAt               time.Time             `json:"updated_at"`
}

type TaskRunTarget struct {
	ID              int64      `json:"id"`
	RunID           int64      `json:"run_id"`
	NodeID          string     `json:"node_id"`
	NodeName        string     `json:"node_name"`
	Status          string     `json:"status"`
	ExitCode        *int       `json:"exit_code"`
	Output          string     `json:"output"`
	OutputTruncated bool       `json:"output_truncated"`
	Error           string     `json:"error,omitempty"`
	DurationMS      int64      `json:"duration_ms"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type TaskRunDetail struct {
	TaskRun
	Targets []TaskRunTarget `json:"targets"`
}

type RunTargetSnapshot struct {
	NodeID   string
	NodeName string
}

type RunTargetResult struct {
	Status          string
	ExitCode        *int
	Output          string
	OutputTruncated bool
	Error           string
	DurationMS      int64
	StartedAt       *time.Time
	CompletedAt     time.Time
}

type RunFilter struct {
	BeforeID int64
	Limit    int
	From     *time.Time
	To       *time.Time
	Status   string
	TaskID   int64
	ScriptID int64
	NodeID   string
	Trigger  string
}

type RunPage struct {
	Runs         []TaskRun `json:"runs"`
	NextBeforeID *int64    `json:"next_before_id"`
}

const automationScriptColumns = `id, name, normalized_name, description, content,
	timeout_seconds, revision, created_at, updated_at`

const scheduledTaskColumns = `t.id, t.name, t.normalized_name, t.script_id, s.name,
	s.revision, t.cron_expression, t.timezone, t.enabled, t.timeout_seconds,
	t.notification_policy, t.notification_channels, t.next_run_at,
	t.last_scheduled_at, latest_run.status, latest_run.created_at,
	t.created_at, t.updated_at`

// Run history uses descending IDs for its keyset cursor. Reusing that ordering
// here makes the latest projection deterministic even when timestamps tie and
// lets both SQLite and MySQL use the existing (task_id, id) index.
const scheduledTaskLatestRunJoin = `LEFT JOIN task_runs latest_run ON latest_run.id = (
	SELECT MAX(candidate.id) FROM task_runs candidate WHERE candidate.task_id = t.id
)`

const taskRunColumns = `id, task_id, script_id, task_name, script_name, script_revision,
	script_content, timeout_seconds, notification_policy, notification_channels,
	trigger_type, scheduled_for, status, total_targets, completed_targets,
	success_targets, failed_targets, error, notification_sent, notification_error,
	notification_attempted_at, started_at, completed_at, created_at, updated_at`

const taskRunTargetColumns = `id, run_id, node_id, node_name, status, exit_code,
	output, output_truncated, error, duration_ms, started_at, completed_at,
	created_at, updated_at`

func ValidateScriptInput(script *AutomationScript) error {
	if script == nil {
		return invalidError("script")
	}
	copy := *script
	return prepareScript(&copy)
}

func ValidateTaskInput(task *ScheduledTask) error {
	if task == nil {
		return invalidError("task")
	}
	copy := *task
	return prepareScheduledTask(&copy, false)
}

func (s *TaskStore) CreateScript(ctx context.Context, script *AutomationScript) error {
	if script == nil {
		return invalidError("script")
	}
	if err := prepareScript(script); err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO automation_scripts (
		name, normalized_name, description, content, timeout_seconds, revision, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`, script.Name, script.NormalizedName,
		script.Description, script.Content, script.TimeoutSeconds, formatTaskTime(now), formatTaskTime(now))
	if err != nil {
		return mapAutomationWriteError(err, "script name")
	}
	if script.ID, err = result.LastInsertId(); err != nil {
		return err
	}
	created, err := s.GetScript(ctx, script.ID)
	if err != nil {
		return err
	}
	*script = *created
	return nil
}

func (s *TaskStore) GetScript(ctx context.Context, id int64) (*AutomationScript, error) {
	if id <= 0 {
		return nil, invalidError("script id")
	}
	script, err := scanAutomationScript(s.db.QueryRowContext(ctx,
		`SELECT `+automationScriptColumns+` FROM automation_scripts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundError("script")
	}
	if err != nil {
		return nil, err
	}
	return &script, nil
}

func (s *TaskStore) ListScripts(ctx context.Context) ([]AutomationScript, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+automationScriptColumns+` FROM automation_scripts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scripts := make([]AutomationScript, 0)
	for rows.Next() {
		script, err := scanAutomationScript(rows)
		if err != nil {
			return nil, err
		}
		scripts = append(scripts, script)
	}
	return scripts, rows.Err()
}

func (s *TaskStore) UpdateScript(ctx context.Context, script *AutomationScript) error {
	if script == nil || script.ID <= 0 {
		return invalidError("script")
	}
	if err := prepareScript(script); err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE automation_scripts SET
		name = ?, normalized_name = ?, description = ?, content = ?, timeout_seconds = ?,
		revision = revision + 1, updated_at = ? WHERE id = ?`, script.Name,
		script.NormalizedName, script.Description, script.Content, script.TimeoutSeconds,
		formatTaskTime(now), script.ID)
	if err != nil {
		return mapAutomationWriteError(err, "script name")
	}
	if err := requireAutomationRow(result, "script"); err != nil {
		return err
	}
	updated, err := s.GetScript(ctx, script.ID)
	if err != nil {
		return err
	}
	*script = *updated
	return nil
}

func (s *TaskStore) DeleteScript(ctx context.Context, id int64) error {
	if id <= 0 {
		return invalidError("script id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var references int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_tasks WHERE script_id = ?`, id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return conflictError("script is referenced by a scheduled task")
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM automation_scripts WHERE id = ?`, id)
	if err != nil {
		return mapAutomationWriteError(err, "script is referenced by a scheduled task")
	}
	if err := requireAutomationRow(result, "script"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TaskStore) CreateScheduledTask(ctx context.Context, task *ScheduledTask) error {
	if task == nil {
		return invalidError("task")
	}
	if err := prepareScheduledTask(task, true); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	script, err := getAutomationScriptTx(ctx, tx, task.ScriptID)
	if err != nil {
		return err
	}
	if task.TimeoutSeconds == 0 {
		task.TimeoutSeconds = script.TimeoutSeconds
	}
	if err := validateTimeout(task.TimeoutSeconds); err != nil {
		return err
	}
	channelsJSON, err := marshalTaskChannels(task.NotificationChannels)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO scheduled_tasks (
		name, normalized_name, script_id, cron_expression, timezone, enabled,
		timeout_seconds, notification_policy, notification_channels, next_run_at,
		last_scheduled_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.Name, task.NormalizedName,
		task.ScriptID, task.CronExpression, task.Timezone, task.Enabled, task.TimeoutSeconds,
		task.NotificationPolicy, channelsJSON, nullableTaskTime(task.NextRunAt),
		nullableTaskTime(task.LastScheduledAt), formatTaskTime(now), formatTaskTime(now))
	if err != nil {
		return mapAutomationWriteError(err, "task name")
	}
	if task.ID, err = result.LastInsertId(); err != nil {
		return err
	}
	if err := replaceScheduledTaskNodesTx(ctx, tx, task.ID, task.NodeIDs, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	created, err := s.GetScheduledTask(ctx, task.ID)
	if err != nil {
		return err
	}
	*task = *created
	return nil
}

func (s *TaskStore) GetScheduledTask(ctx context.Context, id int64) (*ScheduledTask, error) {
	if id <= 0 {
		return nil, invalidError("task id")
	}
	task, err := scanScheduledTask(s.db.QueryRowContext(ctx, `SELECT `+scheduledTaskColumns+`
		FROM scheduled_tasks t JOIN automation_scripts s ON s.id = t.script_id
		`+scheduledTaskLatestRunJoin+` WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundError("task")
	}
	if err != nil {
		return nil, err
	}
	task.NodeIDs, err = loadScheduledTaskNodeIDs(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *TaskStore) ListScheduledTasks(ctx context.Context) ([]ScheduledTask, error) {
	return s.listScheduledTasks(ctx, "", nil)
}

func (s *TaskStore) ListDueScheduledTasks(ctx context.Context, dueAt time.Time, limit int) ([]ScheduledTask, error) {
	if dueAt.IsZero() {
		return nil, invalidError("due time")
	}
	if limit == 0 {
		limit = MaxDueTaskLimit
	}
	if limit < 1 || limit > MaxDueTaskLimit {
		return nil, invalidError("due task limit")
	}
	return s.listScheduledTasks(ctx,
		`WHERE t.enabled = 1 AND t.next_run_at IS NOT NULL AND t.next_run_at <= ? ORDER BY t.next_run_at, t.id LIMIT ?`,
		[]any{formatTaskTime(dueAt.UTC()), limit})
}

func (s *TaskStore) listScheduledTasks(ctx context.Context, suffix string, args []any) ([]ScheduledTask, error) {
	query := `SELECT ` + scheduledTaskColumns + ` FROM scheduled_tasks t
		JOIN automation_scripts s ON s.id = t.script_id
		` + scheduledTaskLatestRunJoin + ` `
	if suffix == "" {
		query += `ORDER BY t.id`
	} else {
		query += suffix
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	tasks := make([]ScheduledTask, 0)
	for rows.Next() {
		task, err := scanScheduledTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range tasks {
		tasks[index].NodeIDs, err = loadScheduledTaskNodeIDs(ctx, s.db, tasks[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

func (s *TaskStore) UpdateScheduledTask(ctx context.Context, task *ScheduledTask) error {
	if task == nil || task.ID <= 0 {
		return invalidError("task")
	}
	if err := prepareScheduledTask(task, true); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockScheduledTaskTx(ctx, tx, task.ID); err != nil {
		return err
	}
	active, err := hasActiveTaskRunTx(ctx, tx, task.ID)
	if err != nil {
		return err
	}
	if active {
		return conflictError("task has an active run")
	}
	script, err := getAutomationScriptTx(ctx, tx, task.ScriptID)
	if err != nil {
		return err
	}
	if task.TimeoutSeconds == 0 {
		task.TimeoutSeconds = script.TimeoutSeconds
	}
	if err := validateTimeout(task.TimeoutSeconds); err != nil {
		return err
	}
	channelsJSON, err := marshalTaskChannels(task.NotificationChannels)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE scheduled_tasks SET
		name = ?, normalized_name = ?, script_id = ?, cron_expression = ?, timezone = ?,
		enabled = ?, timeout_seconds = ?, notification_policy = ?, notification_channels = ?,
		next_run_at = ?, updated_at = ? WHERE id = ?`, task.Name, task.NormalizedName,
		task.ScriptID, task.CronExpression, task.Timezone, task.Enabled, task.TimeoutSeconds,
		task.NotificationPolicy, channelsJSON, nullableTaskTime(task.NextRunAt),
		formatTaskTime(now), task.ID)
	if err != nil {
		return mapAutomationWriteError(err, "task name")
	}
	if err := requireAutomationRow(result, "task"); err != nil {
		return err
	}
	if err := replaceScheduledTaskNodesTx(ctx, tx, task.ID, task.NodeIDs, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	updated, err := s.GetScheduledTask(ctx, task.ID)
	if err != nil {
		return err
	}
	*task = *updated
	return nil
}

func (s *TaskStore) SetScheduledTaskEnabled(ctx context.Context, id int64, enabled bool, nextRunAt *time.Time) (*ScheduledTask, error) {
	if id <= 0 {
		return nil, invalidError("task id")
	}
	if enabled && (nextRunAt == nil || nextRunAt.IsZero()) {
		return nil, invalidError("next run time")
	}
	if !enabled {
		nextRunAt = nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.lockScheduledTaskTx(ctx, tx, id); err != nil {
		return nil, err
	}
	active, err := hasActiveTaskRunTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if active {
		return nil, conflictError("task has an active run")
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE scheduled_tasks SET enabled = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		enabled, nullableTaskTime(nextRunAt), formatTaskTime(now), id)
	if err != nil {
		return nil, err
	}
	if err := requireAutomationRow(result, "task"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetScheduledTask(ctx, id)
}

func (s *TaskStore) DeleteScheduledTask(ctx context.Context, id int64) error {
	if id <= 0 {
		return invalidError("task id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockScheduledTaskTx(ctx, tx, id); err != nil {
		return err
	}
	active, err := hasActiveTaskRunTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if active {
		return conflictError("task has an active run")
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if err := requireAutomationRow(result, "task"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TaskStore) HasActiveTaskRun(ctx context.Context, taskID int64) (bool, error) {
	if taskID <= 0 {
		return false, invalidError("task id")
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_runs
		WHERE task_id = ? AND status IN (?, ?)`, taskID, RunStatusQueued, RunStatusRunning).Scan(&count)
	return count > 0, err
}

func (s *TaskStore) ClaimDueTask(ctx context.Context, taskID int64, expectedDueAt time.Time, nextRunAt time.Time, claimedAt time.Time) (TaskRunDetail, error) {
	if taskID <= 0 || expectedDueAt.IsZero() || nextRunAt.IsZero() {
		return TaskRunDetail{}, invalidError("scheduled claim")
	}
	if claimedAt.IsZero() {
		claimedAt = time.Now().UTC()
	} else {
		claimedAt = claimedAt.UTC()
	}
	expectedDueAt = expectedDueAt.UTC()
	nextRunAt = nextRunAt.UTC()
	if !nextRunAt.After(claimedAt) || !nextRunAt.After(expectedDueAt) {
		return TaskRunDetail{}, invalidError("next run time")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRunDetail{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE scheduled_tasks SET next_run_at = ?,
		last_scheduled_at = ?, updated_at = ?
		WHERE id = ? AND enabled = 1 AND next_run_at = ?`, formatTaskTime(nextRunAt),
		formatTaskTime(expectedDueAt), formatTaskTime(claimedAt), taskID, formatTaskTime(expectedDueAt))
	if err != nil {
		return TaskRunDetail{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return TaskRunDetail{}, err
	}
	if rows != 1 {
		return TaskRunDetail{}, claimLostError("scheduled occurrence")
	}
	task, err := getScheduledTaskTx(ctx, tx, taskID)
	if err != nil {
		return TaskRunDetail{}, claimLostError("scheduled task")
	}
	targets, err := loadRunTargetSnapshotsTx(ctx, tx, taskID)
	if err != nil {
		return TaskRunDetail{}, err
	}
	if len(targets) == 0 {
		return TaskRunDetail{}, invalidError("task targets")
	}
	active, err := hasActiveTaskRunTx(ctx, tx, taskID)
	if err != nil {
		return TaskRunDetail{}, err
	}
	run := TaskRun{
		TaskID:               int64Pointer(task.ID),
		ScriptID:             int64Pointer(task.ScriptID),
		TaskName:             task.Name,
		ScriptName:           task.ScriptName,
		ScriptRevision:       task.ScriptRevision,
		TimeoutSeconds:       task.TimeoutSeconds,
		NotificationPolicy:   task.NotificationPolicy,
		NotificationChannels: nonNilNotificationChannels(task.NotificationChannels),
		Trigger:              RunTriggerScheduled,
		ScheduledFor:         timePointer(expectedDueAt),
		Status:               RunStatusQueued,
	}
	script, err := getAutomationScriptTx(ctx, tx, task.ScriptID)
	if err != nil {
		return TaskRunDetail{}, err
	}
	run.ScriptContent = script.Content
	targetStatus := TargetStatusQueued
	targetError := ""
	if active {
		run.Status = RunStatusSkipped
		run.Error = "overlap"
		targetStatus = TargetStatusSkipped
		targetError = "overlap"
	}
	detail, err := createTaskRunTx(ctx, tx, run, targets, targetStatus, targetError, claimedAt)
	if err != nil {
		if isDuplicateWriteError(err) {
			return TaskRunDetail{}, claimLostError("scheduled occurrence")
		}
		return TaskRunDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskRunDetail{}, err
	}
	return detail, nil
}

func (s *TaskStore) CreateManualScriptRun(ctx context.Context, scriptID int64, targets []RunTargetSnapshot, createdAt time.Time) (TaskRunDetail, error) {
	if scriptID <= 0 {
		return TaskRunDetail{}, invalidError("script id")
	}
	targets, err := normalizeRunTargets(targets)
	if err != nil {
		return TaskRunDetail{}, err
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRunDetail{}, err
	}
	defer tx.Rollback()
	script, err := getAutomationScriptTx(ctx, tx, scriptID)
	if err != nil {
		return TaskRunDetail{}, err
	}
	run := TaskRun{
		ScriptID:             int64Pointer(script.ID),
		ScriptName:           script.Name,
		ScriptRevision:       script.Revision,
		ScriptContent:        script.Content,
		TimeoutSeconds:       script.TimeoutSeconds,
		NotificationPolicy:   NotificationPolicyNever,
		NotificationChannels: make([]NotificationChannel, 0),
		Trigger:              RunTriggerManual,
		Status:               RunStatusQueued,
	}
	detail, err := createTaskRunTx(ctx, tx, run, targets, TargetStatusQueued, "", createdAt)
	if err != nil {
		return TaskRunDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskRunDetail{}, err
	}
	return detail, nil
}

func (s *TaskStore) CreateManualTaskRun(ctx context.Context, taskID int64, createdAt time.Time) (TaskRunDetail, error) {
	if taskID <= 0 {
		return TaskRunDetail{}, invalidError("task id")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRunDetail{}, err
	}
	defer tx.Rollback()
	if err := s.lockScheduledTaskTx(ctx, tx, taskID); err != nil {
		return TaskRunDetail{}, err
	}
	task, err := getScheduledTaskTx(ctx, tx, taskID)
	if err != nil {
		return TaskRunDetail{}, err
	}
	active, err := hasActiveTaskRunTx(ctx, tx, taskID)
	if err != nil {
		return TaskRunDetail{}, err
	}
	if active {
		return TaskRunDetail{}, conflictError("task has an active run")
	}
	targets, err := loadRunTargetSnapshotsTx(ctx, tx, taskID)
	if err != nil {
		return TaskRunDetail{}, err
	}
	if len(targets) == 0 {
		return TaskRunDetail{}, invalidError("task targets")
	}
	script, err := getAutomationScriptTx(ctx, tx, task.ScriptID)
	if err != nil {
		return TaskRunDetail{}, err
	}
	run := TaskRun{
		TaskID:               int64Pointer(task.ID),
		ScriptID:             int64Pointer(task.ScriptID),
		TaskName:             task.Name,
		ScriptName:           task.ScriptName,
		ScriptRevision:       task.ScriptRevision,
		ScriptContent:        script.Content,
		TimeoutSeconds:       task.TimeoutSeconds,
		NotificationPolicy:   task.NotificationPolicy,
		NotificationChannels: nonNilNotificationChannels(task.NotificationChannels),
		Trigger:              RunTriggerManual,
		Status:               RunStatusQueued,
	}
	detail, err := createTaskRunTx(ctx, tx, run, targets, TargetStatusQueued, "", createdAt)
	if err != nil {
		return TaskRunDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskRunDetail{}, err
	}
	return detail, nil
}

func (s *TaskStore) MarkRunTargetRunning(ctx context.Context, targetID int64, startedAt time.Time) error {
	if targetID <= 0 {
		return invalidError("run target id")
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	} else {
		startedAt = startedAt.UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID int64
	if err := tx.QueryRowContext(ctx, `SELECT run_id FROM task_run_targets WHERE id = ?`, targetID).Scan(&runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFoundError("run target")
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE task_run_targets SET status = ?, started_at = ?, updated_at = ?
		WHERE id = ? AND status = ?`, TargetStatusRunning, formatTaskTime(startedAt),
		formatTaskTime(startedAt), targetID, TargetStatusQueued)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return claimLostError("run target")
	}
	result, err = tx.ExecContext(ctx, `UPDATE task_runs SET status = ?,
		started_at = COALESCE(started_at, ?), updated_at = ? WHERE id = ? AND status IN (?, ?)`,
		RunStatusRunning, formatTaskTime(startedAt), formatTaskTime(startedAt), runID,
		RunStatusQueued, RunStatusRunning)
	if err != nil {
		return err
	}
	rows, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return claimLostError("run")
	}
	return tx.Commit()
}

func (s *TaskStore) CompleteRunTarget(ctx context.Context, targetID int64, result RunTargetResult) (*TaskRun, error) {
	if targetID <= 0 {
		return nil, invalidError("run target id")
	}
	if !isTerminalTargetStatus(result.Status) {
		return nil, invalidError("run target status")
	}
	if result.DurationMS < 0 {
		return nil, invalidError("run target duration")
	}
	completedAt := result.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	} else {
		completedAt = completedAt.UTC()
	}
	var startedAt any
	if result.StartedAt != nil && !result.StartedAt.IsZero() {
		value := result.StartedAt.UTC()
		startedAt = formatTaskTime(value)
	}
	output, truncated := boundTaskText(result.Output, MaxTaskOutputBytes)
	result.OutputTruncated = result.OutputTruncated || truncated
	safeError, _ := boundTaskText(result.Error, MaxTaskErrorBytes)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var runID int64
	if err := tx.QueryRowContext(ctx, `SELECT run_id FROM task_run_targets WHERE id = ?`, targetID).Scan(&runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, notFoundError("run target")
		}
		return nil, err
	}
	update, err := tx.ExecContext(ctx, `UPDATE task_run_targets SET status = ?, exit_code = ?,
		output = ?, output_truncated = ?, error = ?, duration_ms = ?,
		started_at = COALESCE(started_at, ?), completed_at = ?, updated_at = ?
		WHERE id = ? AND status IN (?, ?)`, result.Status, nullableInt(result.ExitCode),
		output, result.OutputTruncated, safeError, result.DurationMS, startedAt,
		formatTaskTime(completedAt), formatTaskTime(completedAt), targetID,
		TargetStatusQueued, TargetStatusRunning)
	if err != nil {
		return nil, err
	}
	rows, err := update.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows != 1 {
		return nil, claimLostError("run target")
	}
	if _, err := aggregateRunTx(ctx, tx, runID, completedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getRunSummary(ctx, runID)
}

func (s *TaskStore) AggregateRun(ctx context.Context, runID int64, at time.Time) (*TaskRun, error) {
	if runID <= 0 {
		return nil, invalidError("run id")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := aggregateRunTx(ctx, tx, runID, at); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getRunSummary(ctx, runID)
}

func (s *TaskStore) UpdateRunNotification(ctx context.Context, runID int64, sent bool, notificationError string, attemptedAt time.Time) error {
	if runID <= 0 {
		return invalidError("run id")
	}
	if attemptedAt.IsZero() {
		attemptedAt = time.Now().UTC()
	} else {
		attemptedAt = attemptedAt.UTC()
	}
	notificationError, _ = boundTaskText(notificationError, MaxTaskErrorBytes)
	result, err := s.db.ExecContext(ctx, `UPDATE task_runs SET notification_sent = ?,
		notification_error = ?, notification_attempted_at = ?, updated_at = ? WHERE id = ?`,
		sent, notificationError, formatTaskTime(attemptedAt), formatTaskTime(attemptedAt), runID)
	if err != nil {
		return err
	}
	return requireAutomationRow(result, "run")
}

func (s *TaskStore) GetRun(ctx context.Context, runID int64) (*TaskRunDetail, error) {
	if runID <= 0 {
		return nil, invalidError("run id")
	}
	run, err := s.getRunSummary(ctx, runID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskRunTargetColumns+`
		FROM task_run_targets WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make([]TaskRunTarget, 0)
	for rows.Next() {
		target, err := scanTaskRunTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &TaskRunDetail{TaskRun: *run, Targets: targets}, nil
}

func (s *TaskStore) ListRuns(ctx context.Context, filter RunFilter) (RunPage, error) {
	filter, err := normalizeRunFilter(filter)
	if err != nil {
		return RunPage{}, err
	}
	conditions := make([]string, 0, 6)
	args := make([]any, 0, 8)
	if filter.BeforeID > 0 {
		conditions = append(conditions, `id < ?`)
		args = append(args, filter.BeforeID)
	}
	if filter.From != nil {
		conditions = append(conditions, `created_at >= ?`)
		args = append(args, formatTaskTime(filter.From.UTC()))
	}
	if filter.To != nil {
		conditions = append(conditions, `created_at <= ?`)
		args = append(args, formatTaskTime(filter.To.UTC()))
	}
	if filter.Status != "" {
		conditions = append(conditions, `status = ?`)
		args = append(args, filter.Status)
	}
	if filter.TaskID > 0 {
		conditions = append(conditions, `task_id = ?`)
		args = append(args, filter.TaskID)
	}
	if filter.ScriptID > 0 {
		conditions = append(conditions, `script_id = ?`)
		args = append(args, filter.ScriptID)
	}
	if filter.Trigger != "" {
		conditions = append(conditions, `trigger_type = ?`)
		args = append(args, filter.Trigger)
	}
	if filter.NodeID != "" {
		conditions = append(conditions, `EXISTS (SELECT 1 FROM task_run_targets target WHERE target.run_id = task_runs.id AND target.node_id = ?)`)
		args = append(args, filter.NodeID)
	}
	query := `SELECT ` + taskRunColumns + ` FROM task_runs`
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, filter.Limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return RunPage{}, err
	}
	defer rows.Close()
	runs := make([]TaskRun, 0, filter.Limit+1)
	for rows.Next() {
		run, err := scanTaskRun(rows)
		if err != nil {
			return RunPage{}, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return RunPage{}, err
	}
	page := RunPage{Runs: runs}
	if len(page.Runs) > filter.Limit {
		page.Runs = page.Runs[:filter.Limit]
		cursor := page.Runs[len(page.Runs)-1].ID
		page.NextBeforeID = &cursor
	}
	return page, nil
}

func (s *TaskStore) RecoverInterruptedRuns(ctx context.Context, interruptedAt time.Time) (int64, error) {
	if interruptedAt.IsZero() {
		interruptedAt = time.Now().UTC()
	} else {
		interruptedAt = interruptedAt.UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE task_run_targets SET status = ?,
		error = ?, completed_at = ?, updated_at = ? WHERE status IN (?, ?)
		AND run_id IN (SELECT id FROM task_runs WHERE status IN (?, ?))`,
		TargetStatusInterrupted, "server restarted during execution", formatTaskTime(interruptedAt),
		formatTaskTime(interruptedAt), TargetStatusQueued, TargetStatusRunning,
		RunStatusQueued, RunStatusRunning); err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM task_runs WHERE status IN (?, ?) ORDER BY id`, RunStatusQueued, RunStatusRunning)
	if err != nil {
		return 0, err
	}
	runIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		runIDs = append(runIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, runID := range runIDs {
		if _, err := aggregateRunTx(ctx, tx, runID, interruptedAt); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE task_runs SET error = ?, status = ?,
			completed_at = ?, updated_at = ? WHERE id = ?`, "server restarted during execution",
			RunStatusInterrupted, formatTaskTime(interruptedAt), formatTaskTime(interruptedAt), runID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(runIDs)), nil
}

func (s *TaskStore) DeleteRunsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, invalidError("retention cutoff")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM task_runs WHERE created_at < ?`, formatTaskTime(cutoff.UTC()))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func createTaskRunTx(ctx context.Context, tx *sql.Tx, run TaskRun, targets []RunTargetSnapshot, targetStatus string, targetError string, createdAt time.Time) (TaskRunDetail, error) {
	channelsJSON, err := marshalTaskChannels(run.NotificationChannels)
	if err != nil {
		return TaskRunDetail{}, err
	}
	total := len(targets)
	completed := 0
	succeeded := 0
	failed := 0
	var startedAt, completedAt any
	if isTerminalRunStatus(run.Status) {
		completed = total
		failed = total
		startedAt = formatTaskTime(createdAt)
		completedAt = formatTaskTime(createdAt)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO task_runs (
		task_id, script_id, task_name, script_name, script_revision, script_content,
		timeout_seconds, notification_policy, notification_channels, trigger_type,
		scheduled_for, status, total_targets, completed_targets, success_targets,
		failed_targets, error, started_at, completed_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableInt64(run.TaskID), nullableInt64(run.ScriptID), run.TaskName, run.ScriptName,
		run.ScriptRevision, run.ScriptContent, run.TimeoutSeconds, run.NotificationPolicy,
		channelsJSON, run.Trigger, nullableTaskTime(run.ScheduledFor), run.Status, total,
		completed, succeeded, failed, run.Error, startedAt, completedAt,
		formatTaskTime(createdAt), formatTaskTime(createdAt))
	if err != nil {
		return TaskRunDetail{}, err
	}
	run.ID, err = result.LastInsertId()
	if err != nil {
		return TaskRunDetail{}, err
	}
	run.TotalTargets = total
	run.CompletedTargets = completed
	run.SuccessTargets = succeeded
	run.FailedTargets = failed
	run.CreatedAt = createdAt
	run.UpdatedAt = createdAt
	if startedAt != nil {
		run.StartedAt = timePointer(createdAt)
		run.CompletedAt = timePointer(createdAt)
	}
	detail := TaskRunDetail{TaskRun: run, Targets: make([]TaskRunTarget, 0, len(targets))}
	for _, target := range targets {
		insert, err := tx.ExecContext(ctx, `INSERT INTO task_run_targets (
			run_id, node_id, node_name, status, output, error, completed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, run.ID, target.NodeID, target.NodeName,
			targetStatus, "", targetError, completedAt, formatTaskTime(createdAt), formatTaskTime(createdAt))
		if err != nil {
			return TaskRunDetail{}, err
		}
		targetID, err := insert.LastInsertId()
		if err != nil {
			return TaskRunDetail{}, err
		}
		row := TaskRunTarget{
			ID: targetID, RunID: run.ID, NodeID: target.NodeID, NodeName: target.NodeName,
			Status: targetStatus, Error: targetError, Output: "", CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}
		if completedAt != nil {
			row.CompletedAt = timePointer(createdAt)
		}
		detail.Targets = append(detail.Targets, row)
	}
	return detail, nil
}

func aggregateRunTx(ctx context.Context, tx *sql.Tx, runID int64, at time.Time) (string, error) {
	var total, completed, succeeded, skipped, interrupted, running int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status NOT IN (?, ?) THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		FROM task_run_targets WHERE run_id = ?`, TargetStatusQueued, TargetStatusRunning,
		TargetStatusSuccess, TargetStatusSkipped, TargetStatusInterrupted,
		TargetStatusRunning, runID).Scan(&total, &completed, &succeeded, &skipped, &interrupted, &running)
	if err != nil {
		return "", err
	}
	if total == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_runs WHERE id = ?`, runID).Scan(&exists); err != nil {
			return "", err
		}
		if exists == 0 {
			return "", notFoundError("run")
		}
	}
	failed := completed - succeeded
	status := RunStatusQueued
	if completed < total {
		if running > 0 || completed > 0 {
			status = RunStatusRunning
		}
	} else {
		switch {
		case total > 0 && succeeded == total:
			status = RunStatusSuccess
		case total > 0 && skipped == total:
			status = RunStatusSkipped
		case total > 0 && interrupted == total:
			status = RunStatusInterrupted
		case succeeded > 0:
			status = RunStatusPartial
		default:
			status = RunStatusFailed
		}
	}
	var completedAt any
	if isTerminalRunStatus(status) {
		completedAt = formatTaskTime(at)
	}
	_, err = tx.ExecContext(ctx, `UPDATE task_runs SET status = ?, total_targets = ?,
		completed_targets = ?, success_targets = ?, failed_targets = ?,
		started_at = CASE WHEN ? = ? THEN started_at ELSE COALESCE(started_at, ?) END,
		completed_at = CASE WHEN ? THEN COALESCE(completed_at, ?) ELSE NULL END,
		updated_at = ? WHERE id = ?`, status, total, completed,
		succeeded, failed, status, RunStatusQueued, formatTaskTime(at), isTerminalRunStatus(status),
		completedAt, formatTaskTime(at), runID)
	if err != nil {
		return "", err
	}
	return status, nil
}

func (s *TaskStore) getRunSummary(ctx context.Context, runID int64) (*TaskRun, error) {
	run, err := scanTaskRun(s.db.QueryRowContext(ctx, `SELECT `+taskRunColumns+` FROM task_runs WHERE id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundError("run")
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func getAutomationScriptTx(ctx context.Context, tx *sql.Tx, id int64) (AutomationScript, error) {
	script, err := scanAutomationScript(tx.QueryRowContext(ctx,
		`SELECT `+automationScriptColumns+` FROM automation_scripts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AutomationScript{}, notFoundError("script")
	}
	return script, err
}

func getScheduledTaskTx(ctx context.Context, tx *sql.Tx, id int64) (ScheduledTask, error) {
	task, err := scanScheduledTask(tx.QueryRowContext(ctx, `SELECT `+scheduledTaskColumns+`
		FROM scheduled_tasks t JOIN automation_scripts s ON s.id = t.script_id
		`+scheduledTaskLatestRunJoin+` WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduledTask{}, notFoundError("task")
	}
	if err != nil {
		return ScheduledTask{}, err
	}
	return task, nil
}

func hasActiveTaskRunTx(ctx context.Context, tx *sql.Tx, taskID int64) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_runs
		WHERE task_id = ? AND status IN (?, ?)`, taskID, RunStatusQueued, RunStatusRunning).Scan(&count)
	return count > 0, err
}

func (s *TaskStore) lockScheduledTaskTx(ctx context.Context, tx *sql.Tx, taskID int64) error {
	query := scheduledTaskLockSQL(s.dialect)
	var id int64
	if err := tx.QueryRowContext(ctx, query, taskID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFoundError("task")
		}
		return err
	}
	return nil
}

func scheduledTaskLockSQL(dialect serverdb.Dialect) string {
	query := `SELECT id FROM scheduled_tasks WHERE id = ?`
	if dialect == serverdb.DialectMySQL {
		return query + ` FOR UPDATE`
	}
	return query
}

func replaceScheduledTaskNodesTx(ctx context.Context, tx *sql.Tx, taskID int64, nodeIDs []string, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM scheduled_task_nodes WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, nodeID := range nodeIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO scheduled_task_nodes (task_id, node_id, created_at) VALUES (?, ?, ?)`,
			taskID, nodeID, formatTaskTime(at)); err != nil {
			return err
		}
	}
	return nil
}

type scheduledTaskNodeQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadScheduledTaskNodeIDs(ctx context.Context, querier scheduledTaskNodeQuerier, taskID int64) ([]string, error) {
	rows, err := querier.QueryContext(ctx, `SELECT node_id FROM scheduled_task_nodes WHERE task_id = ? ORDER BY node_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodeIDs := make([]string, 0)
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, err
		}
		nodeIDs = append(nodeIDs, nodeID)
	}
	return nodeIDs, rows.Err()
}

func loadRunTargetSnapshotsTx(ctx context.Context, tx *sql.Tx, taskID int64) ([]RunTargetSnapshot, error) {
	rows, err := tx.QueryContext(ctx, `SELECT target.node_id, COALESCE(node.name, '')
		FROM scheduled_task_nodes target LEFT JOIN nodes node ON node.id = target.node_id
		WHERE target.task_id = ? ORDER BY target.node_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make([]RunTargetSnapshot, 0)
	for rows.Next() {
		var target RunTargetSnapshot
		if err := rows.Scan(&target.NodeID, &target.NodeName); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

type taskScanner interface {
	Scan(...any) error
}

func scanAutomationScript(scanner taskScanner) (AutomationScript, error) {
	var script AutomationScript
	var createdAt, updatedAt string
	err := scanner.Scan(&script.ID, &script.Name, &script.NormalizedName,
		&script.Description, &script.Content, &script.TimeoutSeconds,
		&script.Revision, &createdAt, &updatedAt)
	if err != nil {
		return AutomationScript{}, err
	}
	if script.CreatedAt, err = parseTime(createdAt); err != nil {
		return AutomationScript{}, err
	}
	if script.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return AutomationScript{}, err
	}
	return script, nil
}

func scanScheduledTask(scanner taskScanner) (ScheduledTask, error) {
	var task ScheduledTask
	var channelsJSON, createdAt, updatedAt string
	var nextRunAt, lastScheduledAt, latestRunStatus, latestRunAt sql.NullString
	err := scanner.Scan(&task.ID, &task.Name, &task.NormalizedName, &task.ScriptID,
		&task.ScriptName, &task.ScriptRevision, &task.CronExpression, &task.Timezone,
		&task.Enabled, &task.TimeoutSeconds, &task.NotificationPolicy, &channelsJSON,
		&nextRunAt, &lastScheduledAt, &latestRunStatus, &latestRunAt, &createdAt, &updatedAt)
	if err != nil {
		return ScheduledTask{}, err
	}
	if err := json.Unmarshal([]byte(channelsJSON), &task.NotificationChannels); err != nil {
		return ScheduledTask{}, err
	}
	task.NotificationChannels = nonNilNotificationChannels(task.NotificationChannels)
	task.NodeIDs = make([]string, 0)
	if task.NextRunAt, err = parseNullableTime(nextRunAt); err != nil {
		return ScheduledTask{}, err
	}
	if task.LastScheduledAt, err = parseNullableTime(lastScheduledAt); err != nil {
		return ScheduledTask{}, err
	}
	if latestRunStatus.Valid {
		status := latestRunStatus.String
		task.LatestRunStatus = &status
	}
	if task.LatestRunAt, err = parseNullableTime(latestRunAt); err != nil {
		return ScheduledTask{}, err
	}
	if task.CreatedAt, err = parseTime(createdAt); err != nil {
		return ScheduledTask{}, err
	}
	if task.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ScheduledTask{}, err
	}
	return task, nil
}

func scanTaskRun(scanner taskScanner) (TaskRun, error) {
	var run TaskRun
	var taskID, scriptID sql.NullInt64
	var channelsJSON, createdAt, updatedAt string
	var scheduledFor, notificationAttemptedAt, startedAt, completedAt sql.NullString
	err := scanner.Scan(&run.ID, &taskID, &scriptID, &run.TaskName, &run.ScriptName,
		&run.ScriptRevision, &run.ScriptContent, &run.TimeoutSeconds,
		&run.NotificationPolicy, &channelsJSON, &run.Trigger, &scheduledFor, &run.Status,
		&run.TotalTargets, &run.CompletedTargets, &run.SuccessTargets, &run.FailedTargets,
		&run.Error, &run.NotificationSent, &run.NotificationError,
		&notificationAttemptedAt, &startedAt, &completedAt, &createdAt, &updatedAt)
	if err != nil {
		return TaskRun{}, err
	}
	if taskID.Valid {
		run.TaskID = int64Pointer(taskID.Int64)
	}
	if scriptID.Valid {
		run.ScriptID = int64Pointer(scriptID.Int64)
	}
	if err := json.Unmarshal([]byte(channelsJSON), &run.NotificationChannels); err != nil {
		return TaskRun{}, err
	}
	run.NotificationChannels = nonNilNotificationChannels(run.NotificationChannels)
	if run.ScheduledFor, err = parseNullableTime(scheduledFor); err != nil {
		return TaskRun{}, err
	}
	if run.NotificationAttemptedAt, err = parseNullableTime(notificationAttemptedAt); err != nil {
		return TaskRun{}, err
	}
	if run.StartedAt, err = parseNullableTime(startedAt); err != nil {
		return TaskRun{}, err
	}
	if run.CompletedAt, err = parseNullableTime(completedAt); err != nil {
		return TaskRun{}, err
	}
	if run.CreatedAt, err = parseTime(createdAt); err != nil {
		return TaskRun{}, err
	}
	if run.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return TaskRun{}, err
	}
	return run, nil
}

func scanTaskRunTarget(scanner taskScanner) (TaskRunTarget, error) {
	var target TaskRunTarget
	var exitCode sql.NullInt64
	var startedAt, completedAt sql.NullString
	var createdAt, updatedAt string
	err := scanner.Scan(&target.ID, &target.RunID, &target.NodeID, &target.NodeName,
		&target.Status, &exitCode, &target.Output, &target.OutputTruncated, &target.Error,
		&target.DurationMS, &startedAt, &completedAt, &createdAt, &updatedAt)
	if err != nil {
		return TaskRunTarget{}, err
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		target.ExitCode = &value
	}
	if target.StartedAt, err = parseNullableTime(startedAt); err != nil {
		return TaskRunTarget{}, err
	}
	if target.CompletedAt, err = parseNullableTime(completedAt); err != nil {
		return TaskRunTarget{}, err
	}
	if target.CreatedAt, err = parseTime(createdAt); err != nil {
		return TaskRunTarget{}, err
	}
	if target.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return TaskRunTarget{}, err
	}
	return target, nil
}

func prepareScript(script *AutomationScript) error {
	name, normalized, err := normalizeAutomationName(script.Name)
	if err != nil {
		return err
	}
	script.Name = name
	script.NormalizedName = normalized
	script.Description = strings.TrimSpace(script.Description)
	if !utf8.ValidString(script.Description) || len(script.Description) > MaxScriptDescriptionBytes {
		return invalidError("script description")
	}
	if !utf8.ValidString(script.Content) || len(script.Content) == 0 || len(script.Content) > MaxScriptContentBytes {
		return invalidError("script content")
	}
	if script.TimeoutSeconds == 0 {
		script.TimeoutSeconds = DefaultTaskTimeoutSeconds
	}
	return validateTimeout(script.TimeoutSeconds)
}

func prepareScheduledTask(task *ScheduledTask, requireNext bool) error {
	name, normalized, err := normalizeAutomationName(task.Name)
	if err != nil {
		return err
	}
	task.Name = name
	task.NormalizedName = normalized
	if task.ScriptID <= 0 {
		return invalidError("task script")
	}
	task.CronExpression = strings.TrimSpace(task.CronExpression)
	if !utf8.ValidString(task.CronExpression) || len(task.CronExpression) == 0 || len(task.CronExpression) > MaxCronExpressionBytes {
		return invalidError("cron expression")
	}
	task.Timezone = strings.TrimSpace(task.Timezone)
	if !utf8.ValidString(task.Timezone) || len(task.Timezone) == 0 || len(task.Timezone) > MaxTaskTimezoneBytes {
		return invalidError("timezone")
	}
	if task.TimeoutSeconds != 0 {
		if err := validateTimeout(task.TimeoutSeconds); err != nil {
			return err
		}
	}
	if task.NotificationPolicy == "" {
		task.NotificationPolicy = NotificationPolicyFailure
	}
	if !isNotificationPolicy(task.NotificationPolicy) {
		return invalidError("notification policy")
	}
	if _, err := marshalTaskChannels(task.NotificationChannels); err != nil {
		return err
	}
	nodeIDs, err := normalizeNodeIDs(task.NodeIDs)
	if err != nil {
		return err
	}
	task.NodeIDs = nodeIDs
	if requireNext && task.Enabled && (task.NextRunAt == nil || task.NextRunAt.IsZero()) {
		return invalidError("next run time")
	}
	if !task.Enabled {
		task.NextRunAt = nil
	}
	if task.NextRunAt != nil && !task.NextRunAt.IsZero() {
		value := task.NextRunAt.UTC()
		task.NextRunAt = &value
	}
	if task.LastScheduledAt != nil && !task.LastScheduledAt.IsZero() {
		value := task.LastScheduledAt.UTC()
		task.LastScheduledAt = &value
	}
	return nil
}

func normalizeAutomationName(value string) (string, string, error) {
	if !utf8.ValidString(value) {
		return "", "", invalidError("name")
	}
	display := strings.Join(strings.Fields(value), " ")
	if display == "" || utf8.RuneCountInString(display) > MaxAutomationNameRunes {
		return "", "", invalidError("name")
	}
	return display, strings.ToLower(display), nil
}

func normalizeNodeIDs(nodeIDs []string) ([]string, error) {
	if len(nodeIDs) == 0 || len(nodeIDs) > MaxTaskNodes {
		return nil, invalidError("task targets")
	}
	seen := make(map[string]struct{}, len(nodeIDs))
	normalized := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" || !utf8.ValidString(nodeID) || len(nodeID) > MaxTaskNodeIDBytes {
			return nil, invalidError("node id")
		}
		if _, exists := seen[nodeID]; exists {
			return nil, invalidError("duplicate node id")
		}
		seen[nodeID] = struct{}{}
		normalized = append(normalized, nodeID)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func normalizeRunTargets(targets []RunTargetSnapshot) ([]RunTargetSnapshot, error) {
	if len(targets) == 0 || len(targets) > MaxTaskNodes {
		return nil, invalidError("run targets")
	}
	seen := make(map[string]struct{}, len(targets))
	normalized := make([]RunTargetSnapshot, 0, len(targets))
	for _, target := range targets {
		target.NodeID = strings.TrimSpace(target.NodeID)
		target.NodeName = strings.TrimSpace(target.NodeName)
		if target.NodeID == "" || !utf8.ValidString(target.NodeID) || len(target.NodeID) > MaxTaskNodeIDBytes {
			return nil, invalidError("node id")
		}
		if !utf8.ValidString(target.NodeName) || len(target.NodeName) > MaxTaskNodeNameBytes {
			return nil, invalidError("node name")
		}
		if _, exists := seen[target.NodeID]; exists {
			return nil, invalidError("duplicate node id")
		}
		seen[target.NodeID] = struct{}{}
		normalized = append(normalized, target)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].NodeID < normalized[j].NodeID })
	return normalized, nil
}

func normalizeRunFilter(filter RunFilter) (RunFilter, error) {
	if filter.BeforeID < 0 || filter.TaskID < 0 || filter.ScriptID < 0 {
		return RunFilter{}, invalidError("run filter")
	}
	if filter.Limit == 0 {
		filter.Limit = DefaultRunPageLimit
	}
	if filter.Limit < 1 || filter.Limit > MaxRunPageLimit {
		return RunFilter{}, invalidError("run limit")
	}
	if filter.From != nil {
		if filter.From.IsZero() {
			return RunFilter{}, invalidError("run time range")
		}
		value := filter.From.UTC()
		filter.From = &value
	}
	if filter.To != nil {
		if filter.To.IsZero() {
			return RunFilter{}, invalidError("run time range")
		}
		value := filter.To.UTC()
		filter.To = &value
	}
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return RunFilter{}, invalidError("run time range")
	}
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Status != "" && !isRunStatus(filter.Status) {
		return RunFilter{}, invalidError("run status")
	}
	filter.Trigger = strings.TrimSpace(filter.Trigger)
	if filter.Trigger != "" && filter.Trigger != RunTriggerManual && filter.Trigger != RunTriggerScheduled {
		return RunFilter{}, invalidError("run trigger")
	}
	filter.NodeID = strings.TrimSpace(filter.NodeID)
	if !utf8.ValidString(filter.NodeID) || len(filter.NodeID) > MaxTaskNodeIDBytes {
		return RunFilter{}, invalidError("node id")
	}
	return filter, nil
}

func validateTimeout(value int) error {
	if value < 1 || value > MaxTaskTimeoutSeconds {
		return invalidError("timeout")
	}
	return nil
}

func marshalTaskChannels(channels []NotificationChannel) (string, error) {
	channels = nonNilNotificationChannels(channels)
	value, err := json.Marshal(channels)
	if err != nil || len(value) > MaxNotificationJSONBytes {
		return "", invalidError("notification channels")
	}
	return string(value), nil
}

func isNotificationPolicy(value string) bool {
	return value == NotificationPolicyNever || value == NotificationPolicyFailure || value == NotificationPolicyAlways
}

func isRunStatus(value string) bool {
	switch value {
	case RunStatusQueued, RunStatusRunning, RunStatusSuccess, RunStatusPartial,
		RunStatusFailed, RunStatusSkipped, RunStatusInterrupted:
		return true
	default:
		return false
	}
}

func isTerminalRunStatus(value string) bool {
	return value == RunStatusSuccess || value == RunStatusPartial || value == RunStatusFailed ||
		value == RunStatusSkipped || value == RunStatusInterrupted
}

func isTerminalTargetStatus(value string) bool {
	switch value {
	case TargetStatusSuccess, TargetStatusFailed, TargetStatusTimedOut, TargetStatusBusy,
		TargetStatusCancelled, TargetStatusOffline, TargetStatusUnsupported,
		TargetStatusSkipped, TargetStatusInterrupted:
		return true
	default:
		return false
	}
}

func boundTaskText(value string, limit int) (string, bool) {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= limit {
		return value, false
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value, true
}

func nullableTaskTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTaskTime(value.UTC())
}

func formatTaskTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func int64Pointer(value int64) *int64 {
	return &value
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func requireAutomationRow(result sql.Result, resource string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return notFoundError(resource)
	}
	return nil
}

func mapAutomationWriteError(err error, conflict string) error {
	if isDuplicateWriteError(err) || isForeignKeyWriteError(err) {
		return conflictError(conflict)
	}
	return err
}

func isDuplicateWriteError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "constraint failed") && strings.Contains(message, "unique")
}

func isForeignKeyWriteError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "foreign key constraint")
}

func invalidError(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalid, field)
}

func notFoundError(resource string) error {
	return fmt.Errorf("%w: %s", ErrNotFound, resource)
}

func conflictError(reason string) error {
	return fmt.Errorf("%w: %s", ErrConflict, reason)
}

func claimLostError(resource string) error {
	return fmt.Errorf("%w: %s", ErrClaimLost, resource)
}
