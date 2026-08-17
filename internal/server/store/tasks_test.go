package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

func testTaskStore(t *testing.T) (*TaskStore, *sql.DB) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewTaskStore(database), database
}

func createTestScript(t *testing.T, taskStore *TaskStore, name string, content string) AutomationScript {
	t.Helper()
	script := AutomationScript{Name: name, Description: "test script", Content: content, TimeoutSeconds: 30}
	if err := taskStore.CreateScript(t.Context(), &script); err != nil {
		t.Fatalf("create script: %v", err)
	}
	return script
}

func createTestTask(t *testing.T, taskStore *TaskStore, scriptID int64, dueAt time.Time, nodes ...string) ScheduledTask {
	t.Helper()
	task := ScheduledTask{
		Name: "Nightly cleanup", ScriptID: scriptID, CronExpression: "0 2 * * *",
		Timezone: "UTC", Enabled: true, TimeoutSeconds: 45,
		NotificationPolicy:   NotificationPolicyFailure,
		NotificationChannels: []NotificationChannel{}, NodeIDs: nodes, NextRunAt: &dueAt,
	}
	if err := taskStore.CreateScheduledTask(t.Context(), &task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func insertTestNode(t *testing.T, database *sql.DB, id string, name string, at time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO nodes (id, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, "online", formatTime(at), formatTime(at)); err != nil {
		t.Fatalf("insert node %s: %v", id, err)
	}
}

func TestTaskStoreScriptAndTaskCRUDConflictsAndTypedEmptyCollections(t *testing.T) {
	ctx := context.Background()
	taskStore, _ := testTaskStore(t)
	scripts, err := taskStore.ListScripts(ctx)
	if err != nil {
		t.Fatalf("list scripts: %v", err)
	}
	tasks, err := taskStore.ListScheduledTasks(ctx)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	page, err := taskStore.ListRuns(ctx, RunFilter{})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if scripts == nil || tasks == nil || page.Runs == nil {
		t.Fatalf("empty collections must be non-nil: scripts=%#v tasks=%#v runs=%#v", scripts, tasks, page.Runs)
	}

	script := createTestScript(t, taskStore, "  Cleanup   Logs ", "echo first")
	if script.Name != "Cleanup Logs" || script.NormalizedName != "cleanup logs" || script.Revision != 1 {
		t.Fatalf("created script = %+v", script)
	}
	duplicate := AutomationScript{Name: "cleanup logs", Content: "echo duplicate", TimeoutSeconds: 30}
	if err := taskStore.CreateScript(ctx, &duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate script error = %v, want ErrConflict", err)
	}
	script.Content = "echo second"
	if err := taskStore.UpdateScript(ctx, &script); err != nil {
		t.Fatalf("update script: %v", err)
	}
	if script.Revision != 2 {
		t.Fatalf("script revision = %d, want 2", script.Revision)
	}

	dueAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	task := createTestTask(t, taskStore, script.ID, dueAt, "node-b", "node-a")
	if len(task.NodeIDs) != 2 || task.NodeIDs[0] != "node-a" || task.ScriptName != script.Name || task.ScriptRevision != 2 {
		t.Fatalf("created task = %+v", task)
	}
	paused, err := taskStore.SetScheduledTaskEnabled(ctx, task.ID, false, &dueAt)
	if err != nil {
		t.Fatalf("pause task: %v", err)
	}
	if paused.Enabled || paused.NextRunAt != nil {
		t.Fatalf("paused task = %+v", paused)
	}
	if _, err := taskStore.SetScheduledTaskEnabled(ctx, task.ID, true, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("enable without next run error = %v, want ErrInvalid", err)
	}
	resumeAt := dueAt.Add(time.Hour)
	resumed, err := taskStore.SetScheduledTaskEnabled(ctx, task.ID, true, &resumeAt)
	if err != nil {
		t.Fatalf("resume task: %v", err)
	}
	task = *resumed
	if err := taskStore.DeleteScript(ctx, script.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete referenced script error = %v, want ErrConflict", err)
	}
	other := createTestScript(t, taskStore, "Other", "echo other")
	task.ScriptID = other.ID
	task.NodeIDs = []string{"node-c"}
	if err := taskStore.UpdateScheduledTask(ctx, &task); err != nil {
		t.Fatalf("update task: %v", err)
	}
	if len(task.NodeIDs) != 1 || task.NodeIDs[0] != "node-c" || task.ScriptName != other.Name {
		t.Fatalf("updated task = %+v", task)
	}
	if err := taskStore.DeleteScript(ctx, script.ID); err != nil {
		t.Fatalf("delete unreferenced script: %v", err)
	}
	if err := taskStore.DeleteScheduledTask(ctx, task.ID); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	if err := taskStore.DeleteScript(ctx, other.ID); err != nil {
		t.Fatalf("delete task script: %v", err)
	}
	if _, err := taskStore.GetScheduledTask(ctx, task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing task error = %v, want ErrNotFound", err)
	}
}

func TestTaskStoreScheduledTaskLatestRunProjectionUsesDescendingID(t *testing.T) {
	taskStore, _ := testTaskStore(t)
	base := time.Date(2026, 7, 26, 10, 0, 0, 500_000_000, time.UTC)
	script := createTestScript(t, taskStore, "Latest projection", "echo latest")
	task := createTestTask(t, taskStore, script.ID, base.Add(24*time.Hour), "node-a")

	assertNoLatestRun := func(label string, projected ScheduledTask) {
		t.Helper()
		if projected.LatestRunStatus != nil || projected.LatestRunAt != nil {
			t.Fatalf("%s latest run = status %v at %v, want nil fields", label, projected.LatestRunStatus, projected.LatestRunAt)
		}
	}
	loaded, err := taskStore.GetScheduledTask(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get task without runs: %v", err)
	}
	assertNoLatestRun("detail", *loaded)
	tasks, err := taskStore.ListScheduledTasks(t.Context())
	if err != nil {
		t.Fatalf("list tasks without runs: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks without runs = %+v", tasks)
	}
	assertNoLatestRun("list", tasks[0])

	firstCreatedAt := base.Add(2 * time.Hour)
	first, err := taskStore.CreateManualTaskRun(t.Context(), task.ID, firstCreatedAt)
	if err != nil {
		t.Fatalf("create first task run: %v", err)
	}
	exitZero := 0
	if _, err := taskStore.CompleteRunTarget(t.Context(), first.Targets[0].ID, RunTargetResult{
		Status: TargetStatusSuccess, ExitCode: &exitZero, CompletedAt: firstCreatedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("complete first task run: %v", err)
	}

	// The second row has the greater ID but an earlier timestamp. This proves the
	// projection follows the run-history cursor rather than timestamp ordering.
	latestCreatedAt := base
	latest, err := taskStore.CreateManualTaskRun(t.Context(), task.ID, latestCreatedAt)
	if err != nil {
		t.Fatalf("create latest task run: %v", err)
	}
	exitOne := 1
	if _, err := taskStore.CompleteRunTarget(t.Context(), latest.Targets[0].ID, RunTargetResult{
		Status: TargetStatusFailed, ExitCode: &exitOne, CompletedAt: latestCreatedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("complete latest task run: %v", err)
	}
	if latest.ID <= first.ID {
		t.Fatalf("run IDs are not increasing: first=%d latest=%d", first.ID, latest.ID)
	}

	assertLatestRun := func(label string, projected ScheduledTask) {
		t.Helper()
		if projected.LatestRunStatus == nil || *projected.LatestRunStatus != RunStatusFailed {
			t.Fatalf("%s latest status = %v, want %q", label, projected.LatestRunStatus, RunStatusFailed)
		}
		if projected.LatestRunAt == nil || !projected.LatestRunAt.Equal(latestCreatedAt) {
			t.Fatalf("%s latest time = %v, want %v", label, projected.LatestRunAt, latestCreatedAt)
		}
	}
	loaded, err = taskStore.GetScheduledTask(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get task with runs: %v", err)
	}
	assertLatestRun("detail", *loaded)
	tasks, err = taskStore.ListScheduledTasks(t.Context())
	if err != nil {
		t.Fatalf("list tasks with runs: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks with runs = %+v", tasks)
	}
	assertLatestRun("list", tasks[0])
}

func TestTaskStoreRejectsInvalidAndDuplicateTargets(t *testing.T) {
	taskStore, _ := testTaskStore(t)
	script := createTestScript(t, taskStore, "Script", "echo ok")
	dueAt := time.Now().UTC().Add(time.Hour)
	for _, task := range []ScheduledTask{
		{Name: "No target", ScriptID: script.ID, CronExpression: "* * * * *", Timezone: "UTC", Enabled: true, NodeIDs: []string{}, NextRunAt: &dueAt},
		{Name: "Duplicate", ScriptID: script.ID, CronExpression: "* * * * *", Timezone: "UTC", Enabled: true, NodeIDs: []string{"node-1", "node-1"}, NextRunAt: &dueAt},
		{Name: "No next", ScriptID: script.ID, CronExpression: "* * * * *", Timezone: "UTC", Enabled: true, NodeIDs: []string{"node-1"}},
	} {
		value := task
		if err := taskStore.CreateScheduledTask(t.Context(), &value); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CreateScheduledTask(%q) error = %v, want ErrInvalid", task.Name, err)
		}
	}
	if err := taskStore.CreateScript(t.Context(), &AutomationScript{Name: "too big", Content: strings.Repeat("x", MaxScriptContentBytes+1)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized script error = %v, want ErrInvalid", err)
	}
}

func TestTaskStoreDueClaimAdvancesOnceAndKeepsImmutableSnapshots(t *testing.T) {
	taskStore, database := testTaskStore(t)
	base := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	insertTestNode(t, database, "node-a", "Alpha", base)
	insertTestNode(t, database, "node-b", "Beta", base)
	script := createTestScript(t, taskStore, "Cleanup", "echo original")
	task := createTestTask(t, taskStore, script.ID, base, "node-a", "node-b")
	next := base.Add(time.Hour)
	detail, err := taskStore.ClaimDueTask(t.Context(), task.ID, base, next, base.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim due task: %v", err)
	}
	if detail.Status != RunStatusQueued || detail.ScheduledFor == nil || !detail.ScheduledFor.Equal(base) || len(detail.Targets) != 2 {
		t.Fatalf("claimed run = %+v", detail)
	}
	if detail.ScriptContent != "echo original" || detail.Targets[0].NodeName != "Alpha" {
		t.Fatalf("run snapshots = %+v", detail)
	}
	if _, err := taskStore.ClaimDueTask(t.Context(), task.ID, base, next.Add(time.Hour), base.Add(2*time.Minute)); !errors.Is(err, ErrClaimLost) {
		t.Fatalf("second claim error = %v, want ErrClaimLost", err)
	}
	loadedTask, err := taskStore.GetScheduledTask(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get advanced task: %v", err)
	}
	if loadedTask.NextRunAt == nil || !loadedTask.NextRunAt.Equal(next) || loadedTask.LastScheduledAt == nil || !loadedTask.LastScheduledAt.Equal(base) {
		t.Fatalf("advanced task = %+v", loadedTask)
	}
	script.Content = "echo changed"
	if err := taskStore.UpdateScript(t.Context(), &script); err != nil {
		t.Fatalf("update script after claim: %v", err)
	}
	loadedRun, err := taskStore.GetRun(t.Context(), detail.ID)
	if err != nil {
		t.Fatalf("get claimed run: %v", err)
	}
	if loadedRun.ScriptContent != "echo original" || loadedRun.ScriptRevision == script.Revision {
		t.Fatalf("run snapshot changed with script: run=%+v script=%+v", loadedRun.TaskRun, script)
	}
}

func TestTaskStoreOnceClaimDisablesScheduleAndCannotReplay(t *testing.T) {
	taskStore, database := testTaskStore(t)
	runAt := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	insertTestNode(t, database, "node-once", "Once Node", runAt)
	script := createTestScript(t, taskStore, "Run once", "echo once")
	task := ScheduledTask{
		Name: "One-time task", ScriptID: script.ID, ScheduleType: ScheduleTypeOnce,
		RunAt: &runAt, Timezone: "UTC", Enabled: true, TimeoutSeconds: 30,
		NotificationPolicy: NotificationPolicyNever, NotificationChannels: []NotificationChannel{},
		NodeIDs: []string{"node-once"}, NextRunAt: &runAt,
	}
	if err := taskStore.CreateScheduledTask(t.Context(), &task); err != nil {
		t.Fatalf("create once task: %v", err)
	}
	detail, err := taskStore.ClaimDueTask(t.Context(), task.ID, runAt, time.Time{}, runAt.Add(time.Second))
	if err != nil {
		t.Fatalf("claim once task: %v", err)
	}
	if detail.ScheduledFor == nil || !detail.ScheduledFor.Equal(runAt) || detail.Status != RunStatusQueued {
		t.Fatalf("once run = %+v", detail.TaskRun)
	}
	loaded, err := taskStore.GetScheduledTask(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get once task: %v", err)
	}
	if loaded.Enabled || loaded.NextRunAt != nil || loaded.LastScheduledAt == nil || !loaded.LastScheduledAt.Equal(runAt) {
		t.Fatalf("claimed once task = %+v", loaded)
	}
	if _, err := taskStore.ClaimDueTask(t.Context(), task.ID, runAt, time.Time{}, runAt.Add(2*time.Second)); !errors.Is(err, ErrClaimLost) {
		t.Fatalf("replayed once claim error = %v, want ErrClaimLost", err)
	}
}

func TestTaskStoreScheduledOverlapIsPersistedAsSkipped(t *testing.T) {
	taskStore, _ := testTaskStore(t)
	base := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	script := createTestScript(t, taskStore, "Overlap script", "sleep 10")
	task := createTestTask(t, taskStore, script.ID, base, "node-a")
	active, err := taskStore.CreateManualTaskRun(t.Context(), task.ID, base.Add(-time.Minute))
	if err != nil {
		t.Fatalf("create active manual task run: %v", err)
	}
	if active.Status != RunStatusQueued {
		t.Fatalf("active status = %q", active.Status)
	}
	if _, err := taskStore.SetScheduledTaskEnabled(t.Context(), task.ID, false, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("toggle active task error = %v, want ErrConflict", err)
	}
	skipped, err := taskStore.ClaimDueTask(t.Context(), task.ID, base, base.Add(time.Hour), base)
	if err != nil {
		t.Fatalf("claim overlapping occurrence: %v", err)
	}
	if skipped.Status != RunStatusSkipped || skipped.Error != "overlap" ||
		len(skipped.Targets) != 1 || skipped.Targets[0].Status != TargetStatusSkipped {
		t.Fatalf("skipped run = %+v", skipped)
	}
}

func TestTaskStoreTargetUpdatesAggregateAndReboundOutput(t *testing.T) {
	taskStore, _ := testTaskStore(t)
	base := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	script := createTestScript(t, taskStore, "Batch", "echo batch")
	detail, err := taskStore.CreateManualScriptRun(t.Context(), script.ID, []RunTargetSnapshot{
		{NodeID: "node-a", NodeName: "Alpha"},
		{NodeID: "node-b", NodeName: "Beta"},
	}, base)
	if err != nil {
		t.Fatalf("create manual run: %v", err)
	}
	if err := taskStore.MarkRunTargetRunning(t.Context(), detail.Targets[0].ID, base.Add(time.Second)); err != nil {
		t.Fatalf("mark target running: %v", err)
	}
	exitZero := 0
	run, err := taskStore.CompleteRunTarget(t.Context(), detail.Targets[0].ID, RunTargetResult{
		Status: TargetStatusSuccess, ExitCode: &exitZero,
		Output: strings.Repeat("界", MaxTaskOutputBytes), DurationMS: 25,
		CompletedAt: base.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("complete success target: %v", err)
	}
	if run.Status != RunStatusRunning || run.CompletedTargets != 1 || run.SuccessTargets != 1 {
		t.Fatalf("partially completed run = %+v", run)
	}
	exitOne := 1
	run, err = taskStore.CompleteRunTarget(t.Context(), detail.Targets[1].ID, RunTargetResult{
		Status: TargetStatusFailed, ExitCode: &exitOne, Error: strings.Repeat("e", MaxTaskErrorBytes+100),
		DurationMS: 10, CompletedAt: base.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("complete failed target: %v", err)
	}
	if run.Status != RunStatusPartial || run.CompletedTargets != 2 || run.SuccessTargets != 1 || run.FailedTargets != 1 {
		t.Fatalf("aggregated run = %+v", run)
	}
	finalized, err := taskStore.AggregateRun(t.Context(), detail.ID, base.Add(10*time.Second))
	if err != nil {
		t.Fatalf("repeat aggregate: %v", err)
	}
	if finalized.CompletedAt == nil || !finalized.CompletedAt.Equal(base.Add(3*time.Second)) {
		t.Fatalf("repeat aggregate changed completion time: %v", finalized.CompletedAt)
	}
	loaded, err := taskStore.GetRun(t.Context(), detail.ID)
	if err != nil {
		t.Fatalf("get completed run: %v", err)
	}
	if len(loaded.Targets[0].Output) > MaxTaskOutputBytes || !loaded.Targets[0].OutputTruncated ||
		!utf8.ValidString(loaded.Targets[0].Output) {
		t.Fatalf("bounded output len=%d truncated=%v", len(loaded.Targets[0].Output), loaded.Targets[0].OutputTruncated)
	}
	if len(loaded.Targets[1].Error) != MaxTaskErrorBytes {
		t.Fatalf("bounded error len = %d, want %d", len(loaded.Targets[1].Error), MaxTaskErrorBytes)
	}
	if _, err := taskStore.CompleteRunTarget(t.Context(), detail.Targets[1].ID, RunTargetResult{Status: TargetStatusFailed}); !errors.Is(err, ErrClaimLost) {
		t.Fatalf("duplicate target completion error = %v, want ErrClaimLost", err)
	}
}

func TestTaskStoreRunHistoryKeysetFiltersAndDetails(t *testing.T) {
	taskStore, _ := testTaskStore(t)
	base := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	scriptA := createTestScript(t, taskStore, "A", "echo a")
	scriptB := createTestScript(t, taskStore, "B", "echo b")
	first, err := taskStore.CreateManualScriptRun(t.Context(), scriptA.ID,
		[]RunTargetSnapshot{{NodeID: "node-a", NodeName: "Alpha"}}, base)
	if err != nil {
		t.Fatalf("create first run: %v", err)
	}
	second, err := taskStore.CreateManualScriptRun(t.Context(), scriptB.ID,
		[]RunTargetSnapshot{{NodeID: "node-b", NodeName: "Beta"}}, base.Add(time.Minute))
	if err != nil {
		t.Fatalf("create second run: %v", err)
	}
	third, err := taskStore.CreateManualScriptRun(t.Context(), scriptA.ID,
		[]RunTargetSnapshot{{NodeID: "node-a", NodeName: "Alpha"}}, base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("create third run: %v", err)
	}

	page, err := taskStore.ListRuns(t.Context(), RunFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(page.Runs) != 2 || page.Runs[0].ID != third.ID || page.NextBeforeID == nil {
		t.Fatalf("first page = %+v", page)
	}
	next, err := taskStore.ListRuns(t.Context(), RunFilter{BeforeID: *page.NextBeforeID, Limit: 2})
	if err != nil {
		t.Fatalf("list next page: %v", err)
	}
	if len(next.Runs) != 1 || next.Runs[0].ID != first.ID || next.NextBeforeID != nil {
		t.Fatalf("next page = %+v", next)
	}
	filtered, err := taskStore.ListRuns(t.Context(), RunFilter{ScriptID: scriptA.ID, NodeID: "node-a", Trigger: RunTriggerManual})
	if err != nil {
		t.Fatalf("list filtered runs: %v", err)
	}
	if len(filtered.Runs) != 2 || filtered.Runs[0].ID != third.ID {
		t.Fatalf("filtered runs = %+v", filtered.Runs)
	}
	from := base.Add(30 * time.Second).In(time.FixedZone("test", 8*60*60))
	to := base.Add(90 * time.Second)
	timeFiltered, err := taskStore.ListRuns(t.Context(), RunFilter{From: &from, To: &to})
	if err != nil {
		t.Fatalf("list time filtered runs: %v", err)
	}
	if len(timeFiltered.Runs) != 1 || timeFiltered.Runs[0].ID != second.ID {
		t.Fatalf("time filtered runs = %+v", timeFiltered.Runs)
	}
	detail, err := taskStore.GetRun(t.Context(), second.ID)
	if err != nil {
		t.Fatalf("get run detail: %v", err)
	}
	if detail.Targets == nil || len(detail.Targets) != 1 || detail.Targets[0].NodeID != "node-b" {
		t.Fatalf("run detail = %+v", detail)
	}
	if _, err := taskStore.ListRuns(t.Context(), RunFilter{Limit: MaxRunPageLimit + 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid page limit error = %v, want ErrInvalid", err)
	}
	invalidFrom := base.Add(time.Hour)
	invalidTo := base
	if _, err := taskStore.ListRuns(t.Context(), RunFilter{From: &invalidFrom, To: &invalidTo}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid time range error = %v, want ErrInvalid", err)
	}
}

func TestTaskStoreStartupRecoveryAndRetention(t *testing.T) {
	taskStore, database := testTaskStore(t)
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	script := createTestScript(t, taskStore, "Recovery", "sleep 30")
	oldRun, err := taskStore.CreateManualScriptRun(t.Context(), script.ID,
		[]RunTargetSnapshot{{NodeID: "node-old", NodeName: "Old"}}, base)
	if err != nil {
		t.Fatalf("create old run: %v", err)
	}
	newRun, err := taskStore.CreateManualScriptRun(t.Context(), script.ID,
		[]RunTargetSnapshot{{NodeID: "node-new", NodeName: "New"}}, base.Add(40*24*time.Hour))
	if err != nil {
		t.Fatalf("create new run: %v", err)
	}
	if err := taskStore.MarkRunTargetRunning(t.Context(), oldRun.Targets[0].ID, base.Add(time.Minute)); err != nil {
		t.Fatalf("mark old run running: %v", err)
	}
	recovered, err := taskStore.RecoverInterruptedRuns(t.Context(), base.Add(41*24*time.Hour))
	if err != nil {
		t.Fatalf("recover interrupted runs: %v", err)
	}
	if recovered != 2 {
		t.Fatalf("recovered runs = %d, want 2", recovered)
	}
	for _, runID := range []int64{oldRun.ID, newRun.ID} {
		detail, err := taskStore.GetRun(t.Context(), runID)
		if err != nil {
			t.Fatalf("get recovered run %d: %v", runID, err)
		}
		if detail.Status != RunStatusInterrupted || detail.Targets[0].Status != TargetStatusInterrupted || detail.CompletedAt == nil {
			t.Fatalf("recovered run = %+v", detail)
		}
	}
	deleted, err := taskStore.DeleteRunsOlderThan(t.Context(), base.Add(30*24*time.Hour))
	if err != nil {
		t.Fatalf("delete old runs: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted runs = %d, want 1", deleted)
	}
	if _, err := taskStore.GetRun(t.Context(), oldRun.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old run lookup error = %v, want ErrNotFound", err)
	}
	var targets int
	if err := database.QueryRow(`SELECT COUNT(*) FROM task_run_targets WHERE run_id = ?`, oldRun.ID).Scan(&targets); err != nil {
		t.Fatalf("count deleted targets: %v", err)
	}
	if targets != 0 {
		t.Fatalf("deleted run targets = %d, want 0", targets)
	}
}

func TestTaskRunJSONDoesNotExposeExecutionOrNotificationSecrets(t *testing.T) {
	run := TaskRun{
		ID: 1, ScriptContent: "script-secret-marker",
		NotificationChannels: []NotificationChannel{{
			Type: "webhook", WebhookURL: "https://secret.invalid/hook", Secret: "signing-secret-marker",
		}},
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("marshal run: %v", err)
	}
	for _, secret := range []string{"script-secret-marker", "secret.invalid", "signing-secret-marker"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("run JSON leaked %q: %s", secret, encoded)
		}
	}
}

func TestTaskStoreUsesDialectAwareTaskLock(t *testing.T) {
	if query := scheduledTaskLockSQL(serverdb.DialectSQLite); strings.Contains(query, "FOR UPDATE") {
		t.Fatalf("SQLite lock query contains MySQL syntax: %s", query)
	}
	if query := scheduledTaskLockSQL(serverdb.DialectMySQL); !strings.Contains(query, "FOR UPDATE") {
		t.Fatalf("MySQL lock query is not locking: %s", query)
	}
}

func TestTaskStoreTimeFiltersPreserveSubsecondOrdering(t *testing.T) {
	taskStore, _ := testTaskStore(t)
	base := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	createdAt := base.Add(500 * time.Millisecond)
	if !(formatTaskTime(base) < formatTaskTime(createdAt) && formatTaskTime(createdAt) < formatTaskTime(base.Add(time.Second))) {
		t.Fatalf("fixed task timestamps are not lexically ordered: %q %q %q",
			formatTaskTime(base), formatTaskTime(createdAt), formatTaskTime(base.Add(time.Second)))
	}
	script := createTestScript(t, taskStore, "Subsecond", "echo ok")
	run, err := taskStore.CreateManualScriptRun(t.Context(), script.ID,
		[]RunTargetSnapshot{{NodeID: "node-1", NodeName: "One"}}, createdAt)
	if err != nil {
		t.Fatalf("create subsecond run: %v", err)
	}
	to := base.Add(750 * time.Millisecond)
	page, err := taskStore.ListRuns(t.Context(), RunFilter{From: &base, To: &to})
	if err != nil {
		t.Fatalf("filter subsecond run: %v", err)
	}
	if len(page.Runs) != 1 || page.Runs[0].ID != run.ID {
		t.Fatalf("subsecond filtered runs = %+v", page.Runs)
	}
}
