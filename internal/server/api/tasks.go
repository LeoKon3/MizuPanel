package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	"github.com/mizupanel/mizupanel/internal/server/store"
	servertasks "github.com/mizupanel/mizupanel/internal/server/taskrunner"
)

const maxAutomationRequestBodyBytes = 256 * 1024

type AutomationRunner interface {
	RunManualScript(ctx context.Context, scriptID int64, nodeIDs []string) (store.TaskRun, error)
	RunManualTask(ctx context.Context, taskID int64) (store.TaskRun, error)
}

type AutomationConfig struct {
	Store  *store.TaskStore
	Runner AutomationRunner
}

type automationScriptRequest struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Content        string `json:"content"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (request automationScriptRequest) script(id int64) store.AutomationScript {
	return store.AutomationScript{
		ID: id, Name: request.Name, Description: request.Description,
		Content: request.Content, TimeoutSeconds: request.TimeoutSeconds,
	}
}

type scheduledTaskRequest struct {
	Name                 string                      `json:"name"`
	ScriptID             int64                       `json:"script_id"`
	NodeIDs              []string                    `json:"node_ids"`
	CronExpression       string                      `json:"cron_expression"`
	Timezone             string                      `json:"timezone"`
	TimeoutSeconds       int                         `json:"timeout_seconds"`
	Enabled              *bool                       `json:"enabled"`
	NotificationPolicy   string                      `json:"notification_policy"`
	NotificationChannels []store.NotificationChannel `json:"notification_channels"`
}

func (request scheduledTaskRequest) task(id int64, enabledDefault bool) store.ScheduledTask {
	enabled := enabledDefault
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	return store.ScheduledTask{
		ID: id, Name: request.Name, ScriptID: request.ScriptID,
		NodeIDs: request.NodeIDs, CronExpression: request.CronExpression,
		Timezone: request.Timezone, TimeoutSeconds: request.TimeoutSeconds,
		Enabled: enabled, NotificationPolicy: request.NotificationPolicy,
		NotificationChannels: request.NotificationChannels,
	}
}

type scheduledTaskResponse struct {
	ID                   int64                       `json:"id"`
	Name                 string                      `json:"name"`
	ScriptID             int64                       `json:"script_id"`
	ScriptName           string                      `json:"script_name"`
	ScriptRevision       int                         `json:"script_revision"`
	NodeIDs              []string                    `json:"node_ids"`
	CronExpression       string                      `json:"cron_expression"`
	Timezone             string                      `json:"timezone"`
	Enabled              bool                        `json:"enabled"`
	TimeoutSeconds       int                         `json:"timeout_seconds"`
	NotificationPolicy   string                      `json:"notification_policy"`
	NotificationChannels []store.NotificationChannel `json:"notification_channels"`
	NextRunAt            *time.Time                  `json:"next_run_at"`
	LastScheduledAt      *time.Time                  `json:"last_scheduled_at"`
	LatestRunStatus      *string                     `json:"latest_run_status"`
	LatestRunAt          *time.Time                  `json:"latest_run_at"`
	CreatedAt            time.Time                   `json:"created_at"`
	UpdatedAt            time.Time                   `json:"updated_at"`
}

func projectScheduledTask(task store.ScheduledTask) scheduledTaskResponse {
	return scheduledTaskResponse{
		ID: task.ID, Name: task.Name, ScriptID: task.ScriptID,
		ScriptName: task.ScriptName, ScriptRevision: task.ScriptRevision,
		NodeIDs: task.NodeIDs, CronExpression: task.CronExpression, Timezone: task.Timezone,
		Enabled: task.Enabled, TimeoutSeconds: task.TimeoutSeconds,
		NotificationPolicy: task.NotificationPolicy, NotificationChannels: task.NotificationChannels,
		NextRunAt: task.NextRunAt, LastScheduledAt: task.LastScheduledAt,
		LatestRunStatus: task.LatestRunStatus, LatestRunAt: task.LatestRunAt,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

func projectScheduledTasks(tasks []store.ScheduledTask) []scheduledTaskResponse {
	projected := make([]scheduledTaskResponse, len(tasks))
	for index, task := range tasks {
		projected[index] = projectScheduledTask(task)
	}
	return projected
}

type automationRunResponse struct {
	ID                      int64      `json:"id"`
	TaskID                  *int64     `json:"task_id,omitempty"`
	TaskName                string     `json:"task_name"`
	ScriptID                *int64     `json:"script_id,omitempty"`
	ScriptName              string     `json:"script_name"`
	ScriptRevision          int        `json:"script_revision"`
	Trigger                 string     `json:"trigger"`
	ScheduledFor            *time.Time `json:"scheduled_for,omitempty"`
	Status                  string     `json:"status"`
	TotalTargets            int        `json:"total_targets"`
	CompletedTargets        int        `json:"completed_targets"`
	SuccessTargets          int        `json:"success_targets"`
	FailedTargets           int        `json:"failed_targets"`
	NotificationSent        bool       `json:"notification_sent"`
	NotificationError       string     `json:"notification_error,omitempty"`
	NotificationAttemptedAt *time.Time `json:"notification_attempted_at,omitempty"`
	Error                   string     `json:"error,omitempty"`
	StartedAt               *time.Time `json:"started_at,omitempty"`
	CompletedAt             *time.Time `json:"completed_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}

type automationRunDetailResponse struct {
	automationRunResponse
	Targets []automationRunTargetResponse `json:"targets"`
}

type automationRunTargetResponse struct {
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
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

func projectAutomationRun(run store.TaskRun) automationRunResponse {
	return automationRunResponse{
		ID: run.ID, TaskID: run.TaskID, TaskName: run.TaskName,
		ScriptID: run.ScriptID, ScriptName: run.ScriptName, ScriptRevision: run.ScriptRevision,
		Trigger: run.Trigger, ScheduledFor: run.ScheduledFor, Status: run.Status,
		TotalTargets: run.TotalTargets, CompletedTargets: run.CompletedTargets,
		SuccessTargets: run.SuccessTargets, FailedTargets: run.FailedTargets,
		NotificationSent: run.NotificationSent, NotificationError: run.NotificationError,
		NotificationAttemptedAt: run.NotificationAttemptedAt, Error: run.Error,
		StartedAt: run.StartedAt, CompletedAt: run.CompletedAt,
		CreatedAt: run.CreatedAt,
	}
}

func projectAutomationRunTargets(targets []store.TaskRunTarget) []automationRunTargetResponse {
	projected := make([]automationRunTargetResponse, len(targets))
	for index, target := range targets {
		projected[index] = automationRunTargetResponse{
			ID: target.ID, RunID: target.RunID, NodeID: target.NodeID, NodeName: target.NodeName,
			Status: target.Status, ExitCode: target.ExitCode, Output: target.Output,
			OutputTruncated: target.OutputTruncated, Error: target.Error, DurationMS: target.DurationMS,
			StartedAt: target.StartedAt, CompletedAt: target.CompletedAt, CreatedAt: target.CreatedAt,
		}
	}
	return projected
}

func (s *Server) handleAutomationScripts(w http.ResponseWriter, r *http.Request) {
	if s.automation == nil {
		writeError(w, http.StatusServiceUnavailable, "automation service unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		scripts, err := s.automation.ListScripts(r.Context())
		if err != nil {
			writeAutomationInternalError(w, err, "list scripts")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"scripts": scripts})
	case http.MethodPost:
		markAudit(r, "automation", "script_create", "automation_script", "", "")
		if !authorizeAutomationMutation(w, r, true) {
			return
		}
		var request automationScriptRequest
		if !decodeAutomationRequest(w, r, &request) {
			return
		}
		script := request.script(0)
		setAuditTarget(r, "automation_script", "", script.Name)
		if err := servertasks.ValidateScript(&script); err != nil {
			writeAutomationError(w, err)
			return
		}
		if err := s.automation.CreateScript(r.Context(), &script); err != nil {
			writeAutomationError(w, err)
			return
		}
		setAuditTarget(r, "automation_script", strconv.FormatInt(script.ID, 10), script.Name)
		setAuditMetadata(r, "revision", strconv.Itoa(script.Revision))
		writeJSON(w, http.StatusCreated, script)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleAutomationScriptRoutes(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseAutomationResourcePath(r.URL.Path, "/api/automation/scripts/")
	if !ok {
		writeError(w, http.StatusNotFound, "script not found")
		return
	}
	if action == "runs" {
		s.handleAutomationScriptRun(w, r, id)
		return
	}
	if action != "" {
		writeError(w, http.StatusNotFound, "script not found")
		return
	}
	s.handleAutomationScript(w, r, id)
}

func (s *Server) handleAutomationScript(w http.ResponseWriter, r *http.Request, scriptID int64) {
	if s.automation == nil {
		writeError(w, http.StatusServiceUnavailable, "automation service unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		script, err := s.automation.GetScript(r.Context(), scriptID)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, script)
	case http.MethodPut:
		markAudit(r, "automation", "script_update", "automation_script", strconv.FormatInt(scriptID, 10), "")
		if !authorizeAutomationMutation(w, r, true) {
			return
		}
		var request automationScriptRequest
		if !decodeAutomationRequest(w, r, &request) {
			return
		}
		script := request.script(scriptID)
		setAuditTarget(r, "automation_script", strconv.FormatInt(scriptID, 10), script.Name)
		if err := servertasks.ValidateScript(&script); err != nil {
			writeAutomationError(w, err)
			return
		}
		if err := s.automation.UpdateScript(r.Context(), &script); err != nil {
			writeAutomationError(w, err)
			return
		}
		setAuditMetadata(r, "revision", strconv.Itoa(script.Revision))
		writeJSON(w, http.StatusOK, script)
	case http.MethodDelete:
		markAudit(r, "automation", "script_delete", "automation_script", strconv.FormatInt(scriptID, 10), "")
		if !authorizeAutomationMutation(w, r, false) {
			return
		}
		if !rejectAutomationRequestBody(w, r) {
			return
		}
		script, err := s.automation.GetScript(r.Context(), scriptID)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		setAuditTarget(r, "automation_script", strconv.FormatInt(scriptID, 10), script.Name)
		if err := s.automation.DeleteScript(r.Context(), scriptID); err != nil {
			writeAutomationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (s *Server) handleAutomationScriptRun(w http.ResponseWriter, r *http.Request, scriptID int64) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	markAudit(r, "automation", "script_run", "automation_script", strconv.FormatInt(scriptID, 10), "")
	if !authorizeAutomationMutation(w, r, true) {
		return
	}
	if s.automationRunner == nil {
		writeError(w, http.StatusServiceUnavailable, "automation execution service unavailable")
		return
	}
	var request struct {
		NodeIDs []string `json:"node_ids"`
	}
	if !decodeAutomationRequest(w, r, &request) {
		return
	}
	nodeIDs, err := normalizeAutomationNodeIDs(request.NodeIDs)
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	run, err := s.automationRunner.RunManualScript(r.Context(), scriptID, nodeIDs)
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	setAuditTarget(r, "task_run", strconv.FormatInt(run.ID, 10), run.ScriptName)
	setAuditMetadata(r, "script_id", strconv.FormatInt(scriptID, 10))
	setAuditMetadata(r, "revision", strconv.Itoa(run.ScriptRevision))
	setAuditMetadata(r, "node_count", strconv.Itoa(run.TotalTargets))
	serveraudit.SetResult(r, serveraudit.ResultAccepted, "accepted")
	writeJSON(w, http.StatusAccepted, projectAutomationRun(run))
}

func (s *Server) handleAutomationTasks(w http.ResponseWriter, r *http.Request) {
	if s.automation == nil {
		writeError(w, http.StatusServiceUnavailable, "automation service unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		tasks, err := s.automation.ListScheduledTasks(r.Context())
		if err != nil {
			writeAutomationInternalError(w, err, "list scheduled tasks")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": projectScheduledTasks(tasks)})
	case http.MethodPost:
		markAudit(r, "automation", "task_create", "scheduled_task", "", "")
		if !authorizeAutomationMutation(w, r, true) {
			return
		}
		var request scheduledTaskRequest
		if !decodeAutomationRequest(w, r, &request) {
			return
		}
		task := request.task(0, true)
		setAuditTarget(r, "scheduled_task", "", task.Name)
		if err := servertasks.SetNextRun(&task, time.Now().UTC()); err != nil {
			writeAutomationError(w, err)
			return
		}
		if err := s.automation.CreateScheduledTask(r.Context(), &task); err != nil {
			writeAutomationError(w, err)
			return
		}
		setAuditTarget(r, "scheduled_task", strconv.FormatInt(task.ID, 10), task.Name)
		setTaskAuditMetadata(r, task)
		writeJSON(w, http.StatusCreated, projectScheduledTask(task))
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleAutomationTaskRoutes(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseAutomationResourcePath(r.URL.Path, "/api/automation/tasks/")
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	switch action {
	case "":
		s.handleAutomationTask(w, r, id)
	case "toggle":
		s.handleAutomationTaskToggle(w, r, id)
	case "runs":
		s.handleAutomationTaskRun(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "task not found")
	}
}

func (s *Server) handleAutomationTask(w http.ResponseWriter, r *http.Request, taskID int64) {
	if s.automation == nil {
		writeError(w, http.StatusServiceUnavailable, "automation service unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		task, err := s.automation.GetScheduledTask(r.Context(), taskID)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, projectScheduledTask(*task))
	case http.MethodPut:
		markAudit(r, "automation", "task_update", "scheduled_task", strconv.FormatInt(taskID, 10), "")
		if !authorizeAutomationMutation(w, r, true) {
			return
		}
		existing, err := s.automation.GetScheduledTask(r.Context(), taskID)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		var request scheduledTaskRequest
		if !decodeAutomationRequest(w, r, &request) {
			return
		}
		task := request.task(taskID, existing.Enabled)
		setAuditTarget(r, "scheduled_task", strconv.FormatInt(taskID, 10), task.Name)
		if err := servertasks.SetNextRun(&task, time.Now().UTC()); err != nil {
			writeAutomationError(w, err)
			return
		}
		if err := s.automation.UpdateScheduledTask(r.Context(), &task); err != nil {
			writeAutomationError(w, err)
			return
		}
		setTaskAuditMetadata(r, task)
		writeJSON(w, http.StatusOK, projectScheduledTask(task))
	case http.MethodDelete:
		markAudit(r, "automation", "task_delete", "scheduled_task", strconv.FormatInt(taskID, 10), "")
		if !authorizeAutomationMutation(w, r, false) {
			return
		}
		if !rejectAutomationRequestBody(w, r) {
			return
		}
		task, err := s.automation.GetScheduledTask(r.Context(), taskID)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		setAuditTarget(r, "scheduled_task", strconv.FormatInt(taskID, 10), task.Name)
		if err := s.automation.DeleteScheduledTask(r.Context(), taskID); err != nil {
			writeAutomationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (s *Server) handleAutomationTaskToggle(w http.ResponseWriter, r *http.Request, taskID int64) {
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w, http.MethodPatch)
		return
	}
	markAudit(r, "automation", "task_toggle", "scheduled_task", strconv.FormatInt(taskID, 10), "")
	if !authorizeAutomationMutation(w, r, true) {
		return
	}
	if s.automation == nil {
		writeError(w, http.StatusServiceUnavailable, "automation service unavailable")
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeAutomationRequest(w, r, &request) {
		return
	}
	if request.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid automation request")
		return
	}
	task, err := s.automation.GetScheduledTask(r.Context(), taskID)
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	setAuditTarget(r, "scheduled_task", strconv.FormatInt(taskID, 10), task.Name)
	nextRunAt := (*time.Time)(nil)
	if *request.Enabled {
		next, nextErr := servertasks.NextRun(task.CronExpression, task.Timezone, time.Now().UTC())
		if nextErr != nil {
			writeAutomationError(w, nextErr)
			return
		}
		nextRunAt = &next
		setAuditAction(r, "task_enable")
	} else {
		setAuditAction(r, "task_pause")
	}
	task, err = s.automation.SetScheduledTaskEnabled(r.Context(), taskID, *request.Enabled, nextRunAt)
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	setTaskAuditMetadata(r, *task)
	writeJSON(w, http.StatusOK, projectScheduledTask(*task))
}

func (s *Server) handleAutomationTaskRun(w http.ResponseWriter, r *http.Request, taskID int64) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	markAudit(r, "automation", "task_run", "scheduled_task", strconv.FormatInt(taskID, 10), "")
	if !authorizeAutomationMutation(w, r, false) {
		return
	}
	if !rejectAutomationRequestBody(w, r) {
		return
	}
	if s.automationRunner == nil {
		writeError(w, http.StatusServiceUnavailable, "automation execution service unavailable")
		return
	}
	run, err := s.automationRunner.RunManualTask(r.Context(), taskID)
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	setAuditTarget(r, "task_run", strconv.FormatInt(run.ID, 10), run.TaskName)
	setAuditMetadata(r, "task_id", strconv.FormatInt(taskID, 10))
	setAuditMetadata(r, "script_id", formatAutomationID(run.ScriptID))
	setAuditMetadata(r, "node_count", strconv.Itoa(run.TotalTargets))
	serveraudit.SetResult(r, serveraudit.ResultAccepted, "accepted")
	writeJSON(w, http.StatusAccepted, projectAutomationRun(run))
}

func (s *Server) handleAutomationRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.automation == nil {
		writeError(w, http.StatusServiceUnavailable, "automation service unavailable")
		return
	}
	filter, ok := automationRunFilter(w, r)
	if !ok {
		return
	}
	page, err := s.automation.ListRuns(r.Context(), filter)
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	runs := make([]automationRunResponse, len(page.Runs))
	for index, run := range page.Runs {
		runs[index] = projectAutomationRun(run)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "next_before_id": page.NextBeforeID})
}

func (s *Server) handleAutomationRunRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	runID, action, ok := parseAutomationResourcePath(r.URL.Path, "/api/automation/runs/")
	if !ok || action != "" {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if s.automation == nil {
		writeError(w, http.StatusServiceUnavailable, "automation service unavailable")
		return
	}
	detail, err := s.automation.GetRun(r.Context(), runID)
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, automationRunDetailResponse{
		automationRunResponse: projectAutomationRun(detail.TaskRun), Targets: projectAutomationRunTargets(detail.Targets),
	})
}

func automationRunFilter(w http.ResponseWriter, r *http.Request) (store.RunFilter, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid automation run filter")
		return store.RunFilter{}, false
	}
	allowed := map[string]bool{
		"before_id": true, "limit": true, "status": true, "task_id": true,
		"script_id": true, "node_id": true, "trigger": true, "from": true, "to": true,
	}
	for key := range query {
		if !allowed[key] || len(query[key]) != 1 {
			writeError(w, http.StatusBadRequest, "invalid automation run filter")
			return store.RunFilter{}, false
		}
	}
	var filter store.RunFilter
	var ok bool
	if filter.BeforeID, ok = parseOptionalPositiveInt64(query.Get("before_id"), false); !ok {
		writeError(w, http.StatusBadRequest, "invalid automation run filter")
		return store.RunFilter{}, false
	}
	limitValue := strings.TrimSpace(query.Get("limit"))
	limit, err := parseOptionalInt(limitValue)
	if err != nil || limitValue != "" && (limit < 1 || limit > store.MaxRunPageLimit) {
		writeError(w, http.StatusBadRequest, "invalid automation run filter")
		return store.RunFilter{}, false
	}
	filter.Limit = limit
	if filter.TaskID, ok = parseOptionalPositiveInt64(query.Get("task_id"), false); !ok {
		writeError(w, http.StatusBadRequest, "invalid automation run filter")
		return store.RunFilter{}, false
	}
	if filter.ScriptID, ok = parseOptionalPositiveInt64(query.Get("script_id"), false); !ok {
		writeError(w, http.StatusBadRequest, "invalid automation run filter")
		return store.RunFilter{}, false
	}
	filter.Status = query.Get("status")
	filter.Trigger = query.Get("trigger")
	filter.NodeID = query.Get("node_id")
	if value := strings.TrimSpace(query.Get("from")); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid automation run filter")
			return store.RunFilter{}, false
		}
		filter.From = &parsed
	}
	if value := strings.TrimSpace(query.Get("to")); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid automation run filter")
			return store.RunFilter{}, false
		}
		filter.To = &parsed
	}
	return filter, true
}

func parseAutomationResourcePath(path, prefix string) (int64, string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return 0, "", false
	}
	remainder := strings.TrimPrefix(path, prefix)
	parts := strings.Split(remainder, "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" || len(parts) == 2 && parts[1] == "" {
		return 0, "", false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	if len(parts) == 1 {
		return id, "", true
	}
	return id, parts[1], true
}

func parseOptionalPositiveInt64(value string, allowZero bool) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, true
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || !allowZero && parsed == 0 {
		return 0, false
	}
	return parsed, true
}

func parseOptionalInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func decodeAutomationRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAutomationRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request body")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		}
		return false
	}
	return true
}

func rejectAutomationRequestBody(w http.ResponseWriter, r *http.Request) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAutomationRequestBodyBytes))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request body")
		}
		return false
	}
	if len(bytes.TrimSpace(body)) != 0 {
		writeError(w, http.StatusBadRequest, "request body must be empty")
		return false
	}
	return true
}

func normalizeAutomationNodeIDs(nodeIDs []string) ([]string, error) {
	if len(nodeIDs) == 0 || len(nodeIDs) > store.MaxTaskNodes {
		return nil, store.ErrInvalid
	}
	normalized := make([]string, 0, len(nodeIDs))
	seen := make(map[string]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" || !utf8.ValidString(nodeID) || len(nodeID) > store.MaxTaskNodeIDBytes {
			return nil, store.ErrInvalid
		}
		if _, exists := seen[nodeID]; exists {
			return nil, store.ErrInvalid
		}
		seen[nodeID] = struct{}{}
		normalized = append(normalized, nodeID)
	}
	return normalized, nil
}

func authorizeAutomationMutation(w http.ResponseWriter, r *http.Request, requireJSON bool) bool {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if !requireJSON {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return false
	}
	return true
}

func writeAutomationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid automation request")
	case errors.Is(err, store.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "automation resource not found")
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrClaimLost):
		writeError(w, http.StatusConflict, "automation resource conflict")
	case errors.Is(err, servertasks.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "automation execution service unavailable")
	default:
		writeAutomationInternalError(w, err, "automation operation")
	}
}

func writeAutomationInternalError(w http.ResponseWriter, err error, operation string) {
	log.Printf("%s failed (%T)", operation, err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func setTaskAuditMetadata(r *http.Request, task store.ScheduledTask) {
	setAuditMetadata(r, "script_id", strconv.FormatInt(task.ScriptID, 10))
	setAuditMetadata(r, "node_count", strconv.Itoa(len(task.NodeIDs)))
	setAuditMetadata(r, "enabled", strconv.FormatBool(task.Enabled))
}

func formatAutomationID(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
