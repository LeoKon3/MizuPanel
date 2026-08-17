package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type apiAutomationRunner struct {
	store *store.TaskStore
	nodes *store.NodeStore
}

type recordingAutomationRunner struct {
	scriptErr   error
	taskErr     error
	scriptCalls int
	taskCalls   int
	nodeIDs     []string
}

func (runner *recordingAutomationRunner) RunManualScript(_ context.Context, scriptID int64, nodeIDs []string) (store.TaskRun, error) {
	runner.scriptCalls++
	runner.nodeIDs = append([]string(nil), nodeIDs...)
	if runner.scriptErr != nil {
		return store.TaskRun{}, runner.scriptErr
	}
	now := time.Now().UTC()
	return store.TaskRun{
		ID: 101, ScriptID: &scriptID, ScriptName: "Script", ScriptRevision: 1,
		Trigger: store.RunTriggerManual, Status: store.RunStatusQueued,
		TotalTargets: len(nodeIDs), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (runner *recordingAutomationRunner) RunManualTask(_ context.Context, taskID int64) (store.TaskRun, error) {
	runner.taskCalls++
	if runner.taskErr != nil {
		return store.TaskRun{}, runner.taskErr
	}
	now := time.Now().UTC()
	return store.TaskRun{
		ID: 102, TaskID: &taskID, TaskName: "Task", ScriptName: "Script",
		ScriptRevision: 1, Trigger: store.RunTriggerManual, Status: store.RunStatusQueued,
		TotalTargets: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (runner apiAutomationRunner) RunManualScript(ctx context.Context, scriptID int64, nodeIDs []string) (store.TaskRun, error) {
	targets := make([]store.RunTargetSnapshot, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, err := runner.nodes.Get(ctx, nodeID)
		if err != nil {
			return store.TaskRun{}, err
		}
		targets = append(targets, store.RunTargetSnapshot{NodeID: node.ID, NodeName: node.Name})
	}
	detail, err := runner.store.CreateManualScriptRun(ctx, scriptID, targets, time.Now().UTC())
	return detail.TaskRun, err
}

func (runner apiAutomationRunner) RunManualTask(ctx context.Context, taskID int64) (store.TaskRun, error) {
	detail, err := runner.store.CreateManualTaskRun(ctx, taskID, time.Now().UTC())
	return detail.TaskRun, err
}

type automationAPIFixture struct {
	handler http.Handler
	tasks   *store.TaskStore
	nodes   *store.NodeStore
	audit   *serveraudit.Store
	db      *sql.DB
}

func newAutomationAPIFixture(t *testing.T, auth AuthConfig, runnerOverride ...AutomationRunner) automationAPIFixture {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	nodes := store.NewNodeStore(database)
	tasks := store.NewTaskStore(database)
	auditStore := serveraudit.NewStore(database, serverdb.DialectSQLite)
	runner := AutomationRunner(apiAutomationRunner{store: tasks, nodes: nodes})
	if len(runnerOverride) > 0 {
		runner = runnerOverride[0]
	}
	router := NewRouter(nodes, store.NewMetricStore(database), AutomationConfig{
		Store: tasks, Runner: runner,
	}, auth, auditStore)
	return automationAPIFixture{
		handler: serveraudit.Middleware(auditStore, router), tasks: tasks,
		nodes: nodes, audit: auditStore, db: database,
	}
}

func automationRequest(t *testing.T, handler http.Handler, method, path string, body any, mutation bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, "http://panel.test"+path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if mutation {
		request.Header.Set("Origin", "http://panel.test")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeAutomationResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.NewDecoder(recorder.Body).Decode(&value); err != nil {
		t.Fatalf("decode response %d %q: %v", recorder.Code, recorder.Body.String(), err)
	}
	return value
}

func TestAutomationAPIEmptyCollectionsCRUDExecutionHistoryAndAuditSecrets(t *testing.T) {
	fixture := newAutomationAPIFixture(t, AuthConfig{})
	now := time.Now().UTC()
	if err := fixture.nodes.Upsert(t.Context(), store.Node{ID: "node-a", Name: "Alpha", Status: "online", LastSeenAt: now}); err != nil {
		t.Fatalf("insert node: %v", err)
	}

	emptyScripts := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/scripts", nil, false)
	if emptyScripts.Code != http.StatusOK || !strings.Contains(emptyScripts.Body.String(), `"scripts":[]`) {
		t.Fatalf("empty scripts = %d %s", emptyScripts.Code, emptyScripts.Body.String())
	}
	emptyTasks := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/tasks", nil, false)
	if emptyTasks.Code != http.StatusOK || !strings.Contains(emptyTasks.Body.String(), `"tasks":[]`) {
		t.Fatalf("empty tasks = %d %s", emptyTasks.Code, emptyTasks.Body.String())
	}
	emptyRuns := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/runs", nil, false)
	if emptyRuns.Code != http.StatusOK || !strings.Contains(emptyRuns.Body.String(), `"runs":[]`) {
		t.Fatalf("empty runs = %d %s", emptyRuns.Code, emptyRuns.Body.String())
	}

	const scriptMarker = "script-secret-marker"
	createdRecorder := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/scripts", map[string]any{
		"name": "Cleanup", "description": "remove old files", "content": "echo " + scriptMarker, "timeout_seconds": 30,
	}, true)
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create script = %d %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	script := decodeAutomationResponse[store.AutomationScript](t, createdRecorder)
	if script.ID == 0 || script.Revision != 1 || script.Content != "echo "+scriptMarker {
		t.Fatalf("created script = %+v", script)
	}

	updatedRecorder := automationRequest(t, fixture.handler, http.MethodPut, "/api/automation/scripts/"+strconv.FormatInt(script.ID, 10), map[string]any{
		"name": "Cleanup logs", "description": "updated", "content": "printf updated", "timeout_seconds": 45,
	}, true)
	if updatedRecorder.Code != http.StatusOK {
		t.Fatalf("update script = %d %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	script = decodeAutomationResponse[store.AutomationScript](t, updatedRecorder)
	if script.Revision != 2 || script.Name != "Cleanup logs" {
		t.Fatalf("updated script = %+v", script)
	}

	const webhookMarker = "https://notify.invalid/task-secret-url-marker"
	const signingMarker = "notification-signing-secret-marker"
	createdTaskRecorder := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/tasks", map[string]any{
		"name": "Hourly cleanup", "script_id": script.ID, "node_ids": []string{"node-a"},
		"cron_expression": "0 * * * *", "timezone": "UTC", "timeout_seconds": 45,
		"enabled": true, "notification_policy": "always",
		"notification_channels": []map[string]any{{"type": "webhook", "webhook_url": webhookMarker, "secret": signingMarker}},
	}, true)
	if createdTaskRecorder.Code != http.StatusCreated {
		t.Fatalf("create task = %d %s", createdTaskRecorder.Code, createdTaskRecorder.Body.String())
	}
	task := decodeAutomationResponse[store.ScheduledTask](t, createdTaskRecorder)
	if task.ID == 0 || task.NextRunAt == nil || !task.Enabled || len(task.NodeIDs) != 1 {
		t.Fatalf("created task = %+v", task)
	}
	updatedTaskRecorder := automationRequest(t, fixture.handler, http.MethodPut, "/api/automation/tasks/"+strconv.FormatInt(task.ID, 10), map[string]any{
		"name": "Nightly cleanup", "script_id": script.ID, "node_ids": []string{"node-a"},
		"cron_expression": "30 2 * * *", "timezone": "UTC", "timeout_seconds": 60,
		"enabled": true, "notification_policy": "failure",
		"notification_channels": []map[string]any{{"type": "webhook", "webhook_url": webhookMarker, "secret": signingMarker}},
	}, true)
	if updatedTaskRecorder.Code != http.StatusOK {
		t.Fatalf("update task = %d %s", updatedTaskRecorder.Code, updatedTaskRecorder.Body.String())
	}
	task = decodeAutomationResponse[store.ScheduledTask](t, updatedTaskRecorder)
	if task.Name != "Nightly cleanup" || task.CronExpression != "30 2 * * *" || task.TimeoutSeconds != 60 || task.NextRunAt == nil {
		t.Fatalf("updated task = %+v", task)
	}
	duplicateTask := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/tasks", map[string]any{
		"name": " nightly cleanup ", "script_id": script.ID, "node_ids": []string{"node-a"},
		"cron_expression": "0 3 * * *", "timezone": "UTC", "timeout_seconds": 60,
		"enabled": true, "notification_policy": "never", "notification_channels": []any{},
	}, true)
	if duplicateTask.Code != http.StatusConflict {
		t.Fatalf("duplicate task = %d %s", duplicateTask.Code, duplicateTask.Body.String())
	}

	conflict := automationRequest(t, fixture.handler, http.MethodDelete, "/api/automation/scripts/"+strconv.FormatInt(script.ID, 10), nil, true)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("delete referenced script = %d %s", conflict.Code, conflict.Body.String())
	}

	paused := automationRequest(t, fixture.handler, http.MethodPatch, "/api/automation/tasks/"+strconv.FormatInt(task.ID, 10)+"/toggle", map[string]any{"enabled": false}, true)
	if paused.Code != http.StatusOK {
		t.Fatalf("pause task = %d %s", paused.Code, paused.Body.String())
	}
	pausedTask := decodeAutomationResponse[store.ScheduledTask](t, paused)
	if pausedTask.Enabled || pausedTask.NextRunAt != nil {
		t.Fatalf("paused task = %+v", pausedTask)
	}
	resumed := automationRequest(t, fixture.handler, http.MethodPatch, "/api/automation/tasks/"+strconv.FormatInt(task.ID, 10)+"/toggle", map[string]any{"enabled": true}, true)
	if resumed.Code != http.StatusOK || decodeAutomationResponse[store.ScheduledTask](t, resumed).NextRunAt == nil {
		t.Fatalf("resume task = %d %s", resumed.Code, resumed.Body.String())
	}

	scriptRunRecorder := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/scripts/"+strconv.FormatInt(script.ID, 10)+"/runs", map[string]any{"node_ids": []string{"node-a"}}, true)
	if scriptRunRecorder.Code != http.StatusAccepted {
		t.Fatalf("run script = %d %s", scriptRunRecorder.Code, scriptRunRecorder.Body.String())
	}
	scriptRun := decodeAutomationResponse[automationRunResponse](t, scriptRunRecorder)
	if scriptRun.ID == 0 || scriptRun.Status != store.RunStatusQueued || scriptRun.TotalTargets != 1 {
		t.Fatalf("script run = %+v", scriptRun)
	}
	taskRunRecorder := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/tasks/"+strconv.FormatInt(task.ID, 10)+"/runs", nil, true)
	if taskRunRecorder.Code != http.StatusAccepted {
		t.Fatalf("run task = %d %s", taskRunRecorder.Code, taskRunRecorder.Body.String())
	}
	taskRun := decodeAutomationResponse[automationRunResponse](t, taskRunRecorder)

	history := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/runs?limit=1&trigger=manual&node_id=node-a", nil, false)
	if history.Code != http.StatusOK {
		t.Fatalf("list history = %d %s", history.Code, history.Body.String())
	}
	var historyPage struct {
		Runs         []automationRunResponse `json:"runs"`
		NextBeforeID *int64                  `json:"next_before_id"`
	}
	historyPage = decodeAutomationResponse[struct {
		Runs         []automationRunResponse `json:"runs"`
		NextBeforeID *int64                  `json:"next_before_id"`
	}](t, history)
	if len(historyPage.Runs) != 1 || historyPage.NextBeforeID == nil {
		t.Fatalf("history page = %+v", historyPage)
	}
	detail := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/runs/"+strconv.FormatInt(taskRun.ID, 10), nil, false)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"targets":[`) {
		t.Fatalf("run detail = %d %s", detail.Code, detail.Body.String())
	}
	for _, forbidden := range []string{scriptMarker, webhookMarker, signingMarker, "notification_channels", "script_content", "updated_at"} {
		if strings.Contains(history.Body.String(), forbidden) || strings.Contains(detail.Body.String(), forbidden) {
			t.Fatalf("run API leaked %q: history=%s detail=%s", forbidden, history.Body.String(), detail.Body.String())
		}
	}

	runDetail, err := fixture.tasks.GetRun(t.Context(), taskRun.ID)
	if err != nil {
		t.Fatalf("get task run: %v", err)
	}
	exitCode := 0
	if _, err := fixture.tasks.CompleteRunTarget(t.Context(), runDetail.Targets[0].ID, store.RunTargetResult{
		Status: store.TargetStatusSuccess, ExitCode: &exitCode, CompletedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("complete task run: %v", err)
	}
	deleteTask := automationRequest(t, fixture.handler, http.MethodDelete, "/api/automation/tasks/"+strconv.FormatInt(task.ID, 10), nil, true)
	if deleteTask.Code != http.StatusNoContent {
		t.Fatalf("delete task = %d %s", deleteTask.Code, deleteTask.Body.String())
	}
	deleteScript := automationRequest(t, fixture.handler, http.MethodDelete, "/api/automation/scripts/"+strconv.FormatInt(script.ID, 10), nil, true)
	if deleteScript.Code != http.StatusNoContent {
		t.Fatalf("delete script = %d %s", deleteScript.Code, deleteScript.Body.String())
	}

	auditPage, err := fixture.audit.List(t.Context(), serveraudit.Filter{Module: "automation", Limit: 100})
	if err != nil {
		t.Fatalf("list automation audit: %v", err)
	}
	if len(auditPage.Events) < 10 {
		t.Fatalf("automation audit count = %d, want mutating operations", len(auditPage.Events))
	}
	acceptedActions := map[string]bool{"script_run": false, "task_run": false}
	for _, event := range auditPage.Events {
		if _, tracked := acceptedActions[event.Action]; tracked && event.Result == serveraudit.ResultAccepted {
			acceptedActions[event.Action] = true
		}
	}
	for action, found := range acceptedActions {
		if !found {
			t.Fatalf("missing accepted %s audit in %+v", action, auditPage.Events)
		}
	}
	auditJSON, err := json.Marshal(auditPage.Events)
	if err != nil {
		t.Fatalf("marshal audit: %v", err)
	}
	for _, forbidden := range []string{scriptMarker, webhookMarker, signingMarker, "printf updated"} {
		if strings.Contains(string(auditJSON), forbidden) {
			t.Fatalf("audit leaked %q: %s", forbidden, auditJSON)
		}
	}
}

func TestAutomationAPIOneTimeTaskRoundTrip(t *testing.T) {
	fixture := newAutomationAPIFixture(t, AuthConfig{})
	runAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	if err := fixture.nodes.Upsert(t.Context(), store.Node{ID: "once-node", Name: "Once Node", Status: "online", LastSeenAt: runAt}); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	script := store.AutomationScript{Name: "One-time script", Content: "echo once", TimeoutSeconds: 30}
	if err := fixture.tasks.CreateScript(t.Context(), &script); err != nil {
		t.Fatalf("create script: %v", err)
	}
	created := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/tasks", map[string]any{
		"name": "One-time task", "script_id": script.ID, "node_ids": []string{"once-node"},
		"schedule_type": "once", "run_at": runAt, "timezone": "Asia/Shanghai", "cron_expression": "",
		"enabled": true, "timeout_seconds": 30, "notification_policy": "never", "notification_channels": []any{},
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create one-time task = %d %s", created.Code, created.Body.String())
	}
	var projected store.ScheduledTask
	if err := json.NewDecoder(created.Body).Decode(&projected); err != nil {
		t.Fatalf("decode one-time task: %v", err)
	}
	if projected.ScheduleType != store.ScheduleTypeOnce || projected.RunAt == nil || !projected.RunAt.Equal(runAt) || projected.CronExpression != "" || projected.NextRunAt == nil || !projected.NextRunAt.Equal(runAt) {
		t.Fatalf("created one-time projection = %+v", projected)
	}
	listed := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/tasks", nil, false)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"schedule_type":"once"`) || !strings.Contains(listed.Body.String(), runAt.Format(time.RFC3339)) {
		t.Fatalf("listed one-time task = %d %s", listed.Code, listed.Body.String())
	}
}

func TestAutomationAPIScheduledTaskLatestRunProjection(t *testing.T) {
	fixture := newAutomationAPIFixture(t, AuthConfig{})
	base := time.Date(2026, 7, 26, 10, 0, 0, 500_000_000, time.UTC)
	if err := fixture.nodes.Upsert(t.Context(), store.Node{
		ID: "node-a", Name: "Alpha", Status: "online", LastSeenAt: base,
	}); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	script := store.AutomationScript{
		Name: "Latest projection", Content: "echo latest", TimeoutSeconds: 30,
	}
	if err := fixture.tasks.CreateScript(t.Context(), &script); err != nil {
		t.Fatalf("create script: %v", err)
	}
	nextRunAt := base.Add(24 * time.Hour)
	task := store.ScheduledTask{
		Name: "Latest projection task", ScriptID: script.ID, NodeIDs: []string{"node-a"},
		CronExpression: "0 2 * * *", Timezone: "UTC", Enabled: true,
		TimeoutSeconds: 30, NotificationPolicy: store.NotificationPolicyFailure,
		NotificationChannels: []store.NotificationChannel{}, NextRunAt: &nextRunAt,
	}
	if err := fixture.tasks.CreateScheduledTask(t.Context(), &task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	loadProjections := func() (map[string]any, map[string]any) {
		t.Helper()
		listRecorder := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/tasks", nil, false)
		if listRecorder.Code != http.StatusOK {
			t.Fatalf("list tasks = %d %s", listRecorder.Code, listRecorder.Body.String())
		}
		listPage := decodeAutomationResponse[struct {
			Tasks []map[string]any `json:"tasks"`
		}](t, listRecorder)
		if len(listPage.Tasks) != 1 {
			t.Fatalf("task list = %+v", listPage.Tasks)
		}

		detailRecorder := automationRequest(t, fixture.handler, http.MethodGet,
			"/api/automation/tasks/"+strconv.FormatInt(task.ID, 10), nil, false)
		if detailRecorder.Code != http.StatusOK {
			t.Fatalf("get task = %d %s", detailRecorder.Code, detailRecorder.Body.String())
		}
		return listPage.Tasks[0], decodeAutomationResponse[map[string]any](t, detailRecorder)
	}
	assertProjection := func(label string, projected map[string]any, expectedStatus any, expectedAt *time.Time) {
		t.Helper()
		status, hasStatus := projected["latest_run_status"]
		at, hasAt := projected["latest_run_at"]
		if !hasStatus || !hasAt {
			t.Fatalf("%s omitted latest run keys: %+v", label, projected)
		}
		if status != expectedStatus {
			t.Fatalf("%s latest status = %#v, want %#v", label, status, expectedStatus)
		}
		if expectedAt == nil {
			if at != nil {
				t.Fatalf("%s latest time = %#v, want null", label, at)
			}
		} else {
			encodedAt, ok := at.(string)
			if !ok {
				t.Fatalf("%s latest time = %#v, want timestamp", label, at)
			}
			parsedAt, err := time.Parse(time.RFC3339Nano, encodedAt)
			if err != nil || !parsedAt.Equal(*expectedAt) {
				t.Fatalf("%s latest time = %q (%v), want %v", label, encodedAt, err, *expectedAt)
			}
		}
		for _, forbidden := range []string{"latest_run_id", "script_content", "output", "error", "targets"} {
			if _, exists := projected[forbidden]; exists {
				t.Fatalf("%s leaked %q: %+v", label, forbidden, projected)
			}
		}
	}

	listed, detailed := loadProjections()
	assertProjection("list without runs", listed, nil, nil)
	assertProjection("detail without runs", detailed, nil, nil)

	firstCreatedAt := base.Add(2 * time.Hour)
	first, err := fixture.tasks.CreateManualTaskRun(t.Context(), task.ID, firstCreatedAt)
	if err != nil {
		t.Fatalf("create first run: %v", err)
	}
	exitZero := 0
	if _, err := fixture.tasks.CompleteRunTarget(t.Context(), first.Targets[0].ID, store.RunTargetResult{
		Status: store.TargetStatusSuccess, ExitCode: &exitZero, Output: "first-run-output-marker",
		CompletedAt: firstCreatedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("complete first run: %v", err)
	}

	latestCreatedAt := base
	latest, err := fixture.tasks.CreateManualTaskRun(t.Context(), task.ID, latestCreatedAt)
	if err != nil {
		t.Fatalf("create latest run: %v", err)
	}
	exitOne := 1
	if _, err := fixture.tasks.CompleteRunTarget(t.Context(), latest.Targets[0].ID, store.RunTargetResult{
		Status: store.TargetStatusFailed, ExitCode: &exitOne, Output: "latest-run-output-marker",
		Error: "latest-run-error-marker", CompletedAt: latestCreatedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("complete latest run: %v", err)
	}
	if latest.ID <= first.ID {
		t.Fatalf("run IDs are not increasing: first=%d latest=%d", first.ID, latest.ID)
	}

	listed, detailed = loadProjections()
	assertProjection("list with runs", listed, store.RunStatusFailed, &latestCreatedAt)
	assertProjection("detail with runs", detailed, store.RunStatusFailed, &latestCreatedAt)
}

func TestAutomationAPIValidationBodyBoundsOriginMethodsAndAuth(t *testing.T) {
	fixture := newAutomationAPIFixture(t, AuthConfig{})

	missingOrigin := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/scripts", map[string]any{
		"name": "No origin", "content": "true", "timeout_seconds": 10,
	}, false)
	if missingOrigin.Code != http.StatusForbidden {
		t.Fatalf("missing origin = %d %s", missingOrigin.Code, missingOrigin.Body.String())
	}

	const rejectedSecret = "early-audit-secret-marker"
	unknown := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/scripts", map[string]any{
		"name": "Unknown", "content": "true", "timeout_seconds": 10, "environment": rejectedSecret,
	}, true)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d %s", unknown.Code, unknown.Body.String())
	}

	oversizedRequest := httptest.NewRequest(http.MethodPost, "http://panel.test/api/automation/scripts", strings.NewReader(`{"name":"large","content":"`+strings.Repeat("x", maxAutomationRequestBodyBytes)+`"}`))
	oversizedRequest.Header.Set("Origin", "http://panel.test")
	oversizedRequest.Header.Set("Content-Type", "application/json")
	oversized := httptest.NewRecorder()
	fixture.handler.ServeHTTP(oversized, oversizedRequest)
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d %s", oversized.Code, oversized.Body.String())
	}

	invalidCron := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/tasks", map[string]any{
		"name": "Invalid cron", "script_id": 1, "node_ids": []string{"node-a"},
		"cron_expression": "@daily", "timezone": "Local", "enabled": true,
		"notification_policy": "failure", "notification_channels": []any{},
	}, true)
	if invalidCron.Code != http.StatusBadRequest {
		t.Fatalf("invalid cron = %d %s", invalidCron.Code, invalidCron.Body.String())
	}

	invalidFilter := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/runs?limit=101&unknown=value", nil, false)
	if invalidFilter.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter = %d %s", invalidFilter.Code, invalidFilter.Body.String())
	}
	invalidRange := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/runs?from=2026-07-27T00:00:00Z&to=2026-07-26T00:00:00Z", nil, false)
	if invalidRange.Code != http.StatusBadRequest {
		t.Fatalf("invalid range = %d %s", invalidRange.Code, invalidRange.Body.String())
	}

	method := automationRequest(t, fixture.handler, http.MethodPatch, "/api/automation/scripts", nil, false)
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("method contract = %d allow=%q", method.Code, method.Header().Get("Allow"))
	}

	auditPage, err := fixture.audit.List(t.Context(), serveraudit.Filter{Module: "automation", Action: "script_create", Limit: 100})
	if err != nil {
		t.Fatalf("list rejected automation audits: %v", err)
	}
	if len(auditPage.Events) < 2 {
		t.Fatalf("rejected automation audits = %+v", auditPage.Events)
	}
	for _, event := range auditPage.Events {
		if event.Result != serveraudit.ResultFailure {
			t.Fatalf("rejected automation audit result = %+v", event)
		}
	}
	auditJSON, err := json.Marshal(auditPage.Events)
	if err != nil {
		t.Fatalf("marshal rejected automation audits: %v", err)
	}
	if strings.Contains(string(auditJSON), rejectedSecret) {
		t.Fatalf("rejected request secret leaked to audit: %s", auditJSON)
	}

	authFixture := newAutomationAPIFixture(t, AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour})
	unauthorized := automationRequest(t, authFixture.handler, http.MethodGet, "/api/automation/scripts", nil, false)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated automation = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
}

func TestAutomationAPIMethodAndAuthenticationContracts(t *testing.T) {
	fixture := newAutomationAPIFixture(t, AuthConfig{})
	methodTests := []struct {
		method string
		path   string
		allow  string
	}{
		{http.MethodPatch, "/api/automation/scripts", "GET, POST"},
		{http.MethodPatch, "/api/automation/scripts/1", "GET, PUT, DELETE"},
		{http.MethodGet, "/api/automation/scripts/1/runs", "POST"},
		{http.MethodPatch, "/api/automation/tasks", "GET, POST"},
		{http.MethodPatch, "/api/automation/tasks/1", "GET, PUT, DELETE"},
		{http.MethodPost, "/api/automation/tasks/1/toggle", "PATCH"},
		{http.MethodGet, "/api/automation/tasks/1/runs", "POST"},
		{http.MethodPost, "/api/automation/runs", "GET"},
		{http.MethodPost, "/api/automation/runs/1", "GET"},
	}
	for _, test := range methodTests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			recorder := automationRequest(t, fixture.handler, test.method, test.path, nil, false)
			if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != test.allow {
				t.Fatalf("method contract = %d allow=%q body=%s", recorder.Code, recorder.Header().Get("Allow"), recorder.Body.String())
			}
		})
	}

	for _, path := range []string{
		"/api/automation/scripts/not-an-id",
		"/api/automation/scripts/1/unknown",
		"/api/automation/tasks/not-an-id",
		"/api/automation/tasks/1/unknown",
		"/api/automation/runs/not-an-id",
	} {
		recorder := automationRequest(t, fixture.handler, http.MethodGet, path, nil, false)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("invalid resource path %q = %d %s", path, recorder.Code, recorder.Body.String())
		}
	}

	authFixture := newAutomationAPIFixture(t, AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour})
	authTests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/automation/scripts"},
		{http.MethodPost, "/api/automation/scripts"},
		{http.MethodGet, "/api/automation/scripts/1"},
		{http.MethodPut, "/api/automation/scripts/1"},
		{http.MethodDelete, "/api/automation/scripts/1"},
		{http.MethodPost, "/api/automation/scripts/1/runs"},
		{http.MethodGet, "/api/automation/tasks"},
		{http.MethodPost, "/api/automation/tasks"},
		{http.MethodGet, "/api/automation/tasks/1"},
		{http.MethodPut, "/api/automation/tasks/1"},
		{http.MethodDelete, "/api/automation/tasks/1"},
		{http.MethodPatch, "/api/automation/tasks/1/toggle"},
		{http.MethodPost, "/api/automation/tasks/1/runs"},
		{http.MethodGet, "/api/automation/runs"},
		{http.MethodGet, "/api/automation/runs/1"},
	}
	for _, test := range authTests {
		recorder := automationRequest(t, authFixture.handler, test.method, test.path, nil, false)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s %s = %d %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}
	auditPage, err := authFixture.audit.List(t.Context(), serveraudit.Filter{Module: "automation", Limit: 100})
	if err != nil {
		t.Fatalf("list unauthenticated automation audits: %v", err)
	}
	if len(auditPage.Events) != 0 {
		t.Fatalf("unauthenticated requests reached automation handlers: %+v", auditPage.Events)
	}
}

func TestAutomationAPIManualScriptNodeValidationRunsBeforeRunner(t *testing.T) {
	runner := &recordingAutomationRunner{}
	fixture := newAutomationAPIFixture(t, AuthConfig{}, runner)
	tooMany := make([]string, store.MaxTaskNodes+1)
	for index := range tooMany {
		tooMany[index] = "node-" + strconv.Itoa(index)
	}
	invalidNodeLists := [][]string{
		nil,
		tooMany,
		{"node-a", " node-a "},
		{""},
		{strings.Repeat("n", store.MaxTaskNodeIDBytes+1)},
	}
	for _, nodeIDs := range invalidNodeLists {
		recorder := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/scripts/1/runs", map[string]any{"node_ids": nodeIDs}, true)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid node IDs %#v = %d %s", nodeIDs, recorder.Code, recorder.Body.String())
		}
	}
	if runner.scriptCalls != 0 {
		t.Fatalf("runner called %d times for invalid node lists", runner.scriptCalls)
	}

	recorder := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/scripts/1/runs", map[string]any{
		"node_ids": []string{" node-a ", "node-b"},
	}, true)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("valid node IDs = %d %s", recorder.Code, recorder.Body.String())
	}
	if runner.scriptCalls != 1 || !slices.Equal(runner.nodeIDs, []string{"node-a", "node-b"}) {
		t.Fatalf("runner calls=%d node IDs=%v", runner.scriptCalls, runner.nodeIDs)
	}
}

func TestAutomationAPIStrictJSONAndRunFilterBoundaries(t *testing.T) {
	fixture := newAutomationAPIFixture(t, AuthConfig{})
	sendRaw := func(contentType, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "http://panel.test/api/automation/scripts", strings.NewReader(body))
		request.Header.Set("Origin", "http://panel.test")
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		recorder := httptest.NewRecorder()
		fixture.handler.ServeHTTP(recorder, request)
		return recorder
	}

	validObject := `{"name":"Strict","description":"","content":"true","timeout_seconds":10}`
	if recorder := sendRaw("text/plain", validObject); recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unsupported content type = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := sendRaw("application/json", validObject+` {}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("multiple JSON values = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := sendRaw("application/json", validObject+strings.Repeat(" ", maxAutomationRequestBodyBytes)); recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("trailing oversized body = %d %s", recorder.Code, recorder.Body.String())
	}
	sendNoBodyMutation := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://panel.test"+path, strings.NewReader(body))
		request.Header.Set("Origin", "http://panel.test")
		recorder := httptest.NewRecorder()
		fixture.handler.ServeHTTP(recorder, request)
		return recorder
	}
	if recorder := sendNoBodyMutation(http.MethodPost, "/api/automation/tasks/1/runs", `{}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("task run body = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := sendNoBodyMutation(http.MethodDelete, "/api/automation/scripts/1", `{}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("delete script body = %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := sendNoBodyMutation(http.MethodPost, "/api/automation/tasks/1/runs", strings.Repeat(" ", maxAutomationRequestBodyBytes+1)); recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized task run body = %d %s", recorder.Code, recorder.Body.String())
	}

	invalidFilters := []string{
		"?limit=1&limit=2",
		"?limit=1;status=success",
		"?limit=0",
		"?limit=-1",
		"?before_id=0",
		"?task_id=-1",
		"?script_id=invalid",
		"?status=unknown",
		"?trigger=unknown",
		"?from=invalid",
		"?node_id=" + strings.Repeat("n", store.MaxTaskNodeIDBytes+1),
	}
	for _, query := range invalidFilters {
		recorder := automationRequest(t, fixture.handler, http.MethodGet, "/api/automation/runs"+query, nil, false)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid filter %q = %d %s", query, recorder.Code, recorder.Body.String())
		}
	}
	validFilter := "/api/automation/runs?before_id=999&limit=1&status=success&task_id=1&script_id=1&node_id=node-a&trigger=manual&from=2026-07-25T00:00:00Z&to=2026-07-27T00:00:00Z"
	recorder := automationRequest(t, fixture.handler, http.MethodGet, validFilter, nil, false)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"runs":[]`) {
		t.Fatalf("valid full filter = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomationAPIScriptCRUDAndExecutionErrorMapping(t *testing.T) {
	fixture := newAutomationAPIFixture(t, AuthConfig{})
	create := func(name string) *httptest.ResponseRecorder {
		return automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/scripts", map[string]any{
			"name": name, "description": "", "content": "true", "timeout_seconds": 10,
		}, true)
	}
	first := create("Duplicate")
	if first.Code != http.StatusCreated {
		t.Fatalf("create first script = %d %s", first.Code, first.Body.String())
	}
	firstScript := decodeAutomationResponse[store.AutomationScript](t, first)
	second := create(" duplicate ")
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate script = %d %s", second.Code, second.Body.String())
	}
	other := create("Other")
	if other.Code != http.StatusCreated {
		t.Fatalf("create other script = %d %s", other.Code, other.Body.String())
	}
	otherScript := decodeAutomationResponse[store.AutomationScript](t, other)
	updateConflict := automationRequest(t, fixture.handler, http.MethodPut, "/api/automation/scripts/"+strconv.FormatInt(otherScript.ID, 10), map[string]any{
		"name": firstScript.Name, "description": "", "content": "true", "timeout_seconds": 10,
	}, true)
	if updateConflict.Code != http.StatusConflict {
		t.Fatalf("duplicate script update = %d %s", updateConflict.Code, updateConflict.Body.String())
	}

	overLimit := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/scripts", map[string]any{
		"name": "Too large", "description": "", "content": strings.Repeat("x", store.MaxScriptContentBytes+1), "timeout_seconds": 10,
	}, true)
	if overLimit.Code != http.StatusBadRequest {
		t.Fatalf("over-limit script content = %d %s", overLimit.Code, overLimit.Body.String())
	}

	missingID := strconv.FormatInt(otherScript.ID+1000, 10)
	missingRequests := []struct {
		method string
		body   any
	}{
		{http.MethodGet, nil},
		{http.MethodPut, map[string]any{"name": "Missing", "description": "", "content": "true", "timeout_seconds": 10}},
		{http.MethodDelete, nil},
	}
	for _, request := range missingRequests {
		recorder := automationRequest(t, fixture.handler, request.method, "/api/automation/scripts/"+missingID, request.body, request.method != http.MethodGet)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("missing script %s = %d %s", request.method, recorder.Code, recorder.Body.String())
		}
	}
	missingTaskID := "9999"
	missingTaskRequests := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/automation/tasks/" + missingTaskID, nil},
		{http.MethodPut, "/api/automation/tasks/" + missingTaskID, map[string]any{"name": "Missing", "script_id": firstScript.ID, "node_ids": []string{"node-a"}, "cron_expression": "0 * * * *", "timezone": "UTC", "timeout_seconds": 10, "notification_policy": "failure", "notification_channels": []any{}}},
		{http.MethodDelete, "/api/automation/tasks/" + missingTaskID, nil},
		{http.MethodPatch, "/api/automation/tasks/" + missingTaskID + "/toggle", map[string]any{"enabled": false}},
		{http.MethodPost, "/api/automation/tasks/" + missingTaskID + "/runs", nil},
	}
	for _, request := range missingTaskRequests {
		recorder := automationRequest(t, fixture.handler, request.method, request.path, request.body, true)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("missing task %s %s = %d %s", request.method, request.path, recorder.Code, recorder.Body.String())
		}
	}
	missingScriptTask := automationRequest(t, fixture.handler, http.MethodPost, "/api/automation/tasks", map[string]any{
		"name": "Missing script", "script_id": 9999, "node_ids": []string{"node-a"},
		"cron_expression": "0 * * * *", "timezone": "UTC", "timeout_seconds": 10,
		"notification_policy": "failure", "notification_channels": []any{},
	}, true)
	if missingScriptTask.Code != http.StatusNotFound {
		t.Fatalf("task with missing script = %d %s", missingScriptTask.Code, missingScriptTask.Body.String())
	}

	unavailableFixture := newAutomationAPIFixture(t, AuthConfig{}, nil)
	unavailable := automationRequest(t, unavailableFixture.handler, http.MethodPost, "/api/automation/scripts/1/runs", map[string]any{
		"node_ids": []string{"node-a"},
	}, true)
	if unavailable.Code != http.StatusServiceUnavailable || strings.Contains(unavailable.Body.String(), "nil") {
		t.Fatalf("unavailable runner = %d %s", unavailable.Code, unavailable.Body.String())
	}

	const runnerSecret = "raw-runner-secret-marker"
	runner := &recordingAutomationRunner{scriptErr: errors.New(runnerSecret)}
	failingFixture := newAutomationAPIFixture(t, AuthConfig{}, runner)
	var logOutput bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logOutput)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })
	failure := automationRequest(t, failingFixture.handler, http.MethodPost, "/api/automation/scripts/1/runs", map[string]any{
		"node_ids": []string{"node-a"},
	}, true)
	if failure.Code != http.StatusInternalServerError || !strings.Contains(failure.Body.String(), "internal server error") {
		t.Fatalf("runner failure = %d %s", failure.Code, failure.Body.String())
	}
	if strings.Contains(failure.Body.String(), runnerSecret) || strings.Contains(logOutput.String(), runnerSecret) {
		t.Fatalf("runner error leaked: response=%s log=%s", failure.Body.String(), logOutput.String())
	}
	auditPage, err := failingFixture.audit.List(t.Context(), serveraudit.Filter{Module: "automation", Action: "script_run", Limit: 10})
	if err != nil {
		t.Fatalf("list failed runner audit: %v", err)
	}
	if len(auditPage.Events) != 1 || auditPage.Events[0].Result != serveraudit.ResultFailure {
		t.Fatalf("failed runner audit = %+v", auditPage.Events)
	}
	auditJSON, err := json.Marshal(auditPage.Events)
	if err != nil {
		t.Fatalf("marshal failed runner audit: %v", err)
	}
	if strings.Contains(string(auditJSON), runnerSecret) {
		t.Fatalf("runner error leaked to audit: %s", auditJSON)
	}
}
