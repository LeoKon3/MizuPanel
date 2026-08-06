package taskrunner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/alerting"
	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

var ErrUnavailable = errors.New("automation execution service is unavailable")

const (
	DefaultSweepInterval     = 10 * time.Second
	DefaultGlobalConcurrency = 8
	taskNotificationTimeout  = 30 * time.Second
)

type RunStore interface {
	ListDueScheduledTasks(ctx context.Context, dueAt time.Time, limit int) ([]store.ScheduledTask, error)
	ClaimDueTask(ctx context.Context, taskID int64, expectedDueAt time.Time, nextRunAt time.Time, claimedAt time.Time) (store.TaskRunDetail, error)
	CreateManualScriptRun(ctx context.Context, scriptID int64, targets []store.RunTargetSnapshot, createdAt time.Time) (store.TaskRunDetail, error)
	CreateManualTaskRun(ctx context.Context, taskID int64, createdAt time.Time) (store.TaskRunDetail, error)
	MarkRunTargetRunning(ctx context.Context, targetID int64, startedAt time.Time) error
	CompleteRunTarget(ctx context.Context, targetID int64, result store.RunTargetResult) (*store.TaskRun, error)
	AggregateRun(ctx context.Context, runID int64, at time.Time) (*store.TaskRun, error)
	UpdateRunNotification(ctx context.Context, runID int64, sent bool, notificationError string, attemptedAt time.Time) error
	GetRun(ctx context.Context, runID int64) (*store.TaskRunDetail, error)
	RecoverInterruptedRuns(ctx context.Context, interruptedAt time.Time) (int64, error)
}

type NodeProvider interface {
	Get(ctx context.Context, id string) (store.Node, error)
}

type ScriptExecutor interface {
	TaskRunnerSupported(nodeID string) bool
	RunScript(ctx context.Context, nodeID string, request protocol.ScriptExecutionRequest) (protocol.ScriptExecutionResponse, error)
}

type TaskNotifier interface {
	DeliverTask(ctx context.Context, channels []store.NotificationChannel, payload alerting.TaskPayload) alerting.TaskDeliveryResult
}

type EngineOptions struct {
	SweepInterval     time.Duration
	GlobalConcurrency int
	Now               func() time.Time
}

type Engine struct {
	store    RunStore
	nodes    NodeProvider
	executor ScriptExecutor
	notifier TaskNotifier
	audit    serveraudit.Recorder

	now           func() time.Time
	sweepInterval time.Duration
	globalSlots   chan struct{}

	scheduleMu   sync.Mutex
	schedules    map[int64]struct{}
	nodeMu       sync.Mutex
	nodeSlots    map[string]chan struct{}
	contextMu    sync.RWMutex
	runContext   context.Context
	initializeMu sync.Mutex
	initialized  bool
}

func NewEngine(taskStore *store.TaskStore, nodes *store.NodeStore, executor ScriptExecutor, audit serveraudit.Recorder) *Engine {
	return NewEngineWithDependencies(taskStore, nodes, executor, alerting.NewNotifier(), audit, EngineOptions{})
}

func NewEngineWithDependencies(taskStore RunStore, nodes NodeProvider, executor ScriptExecutor, notifier TaskNotifier, audit serveraudit.Recorder, options EngineOptions) *Engine {
	if options.SweepInterval <= 0 {
		options.SweepInterval = DefaultSweepInterval
	}
	if options.GlobalConcurrency <= 0 {
		options.GlobalConcurrency = DefaultGlobalConcurrency
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Engine{
		store: taskStore, nodes: nodes, executor: executor, notifier: notifier, audit: audit,
		now: options.Now, sweepInterval: options.SweepInterval,
		globalSlots: make(chan struct{}, options.GlobalConcurrency),
		schedules:   make(map[int64]struct{}),
		nodeSlots:   make(map[string]chan struct{}),
	}
}

// Initialize recovers runs left by an earlier Server process exactly once.
// Execution entry points call it before creating or claiming new work so an
// asynchronously started Engine cannot interrupt a newly accepted run.
func (e *Engine) Initialize(ctx context.Context) error {
	if e == nil || e.store == nil {
		return ErrUnavailable
	}
	e.initializeMu.Lock()
	defer e.initializeMu.Unlock()
	if e.initialized {
		return nil
	}
	recovered, err := e.store.RecoverInterruptedRuns(ctx, e.now().UTC())
	if err != nil {
		return err
	}
	e.initialized = true
	if recovered > 0 {
		log.Printf("automation recovered %d interrupted run(s)", recovered)
	}
	return nil
}

// Run recovers evidence from an unclean shutdown, performs an immediate due
// sweep, and then keeps scheduling until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	if e == nil || e.store == nil {
		return
	}
	e.setRunContext(ctx)
	now := e.now().UTC()
	if err := e.Initialize(ctx); err != nil {
		log.Printf("automation recovery failed: %v", err)
		return
	}
	if err := e.Sweep(ctx, now); err != nil {
		log.Printf("automation due sweep failed: %v", err)
	}

	ticker := time.NewTicker(e.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := e.Sweep(ctx, now.UTC()); err != nil {
				log.Printf("automation due sweep failed: %v", err)
			}
		}
	}
}

// Sweep atomically claims every currently due occurrence. A stale occurrence
// is advanced to the first Cron time after now, collapsing missed periods into
// one catch-up run.
func (e *Engine) Sweep(ctx context.Context, now time.Time) error {
	if e == nil || e.store == nil || e.nodes == nil || e.executor == nil {
		return ErrUnavailable
	}
	if err := e.Initialize(ctx); err != nil {
		return err
	}
	if now.IsZero() {
		now = e.now()
	}
	now = now.UTC()
	tasks, err := e.store.ListDueScheduledTasks(ctx, now, store.MaxDueTaskLimit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, task := range tasks {
		if !e.reserveSchedule(task.ID) {
			continue
		}
		func() {
			defer e.releaseSchedule(task.ID)
			if task.NextRunAt == nil || task.NextRunAt.IsZero() {
				return
			}
			nextRun, nextErr := NextRun(task.CronExpression, task.Timezone, now)
			if nextErr != nil {
				if firstErr == nil {
					firstErr = nextErr
				}
				return
			}
			detail, claimErr := e.store.ClaimDueTask(ctx, task.ID, task.NextRunAt.UTC(), nextRun, now)
			if errors.Is(claimErr, store.ErrClaimLost) {
				return
			}
			if claimErr != nil {
				if firstErr == nil {
					firstErr = claimErr
				}
				return
			}
			if detail.Status == store.RunStatusSkipped {
				go e.finalizeRun(e.executionContext(ctx), detail.ID)
				return
			}
			e.dispatch(e.executionContext(ctx), detail)
		}()
	}
	return firstErr
}

func (e *Engine) RunManualScript(ctx context.Context, scriptID int64, nodeIDs []string) (store.TaskRun, error) {
	if e == nil || e.store == nil || e.nodes == nil || e.executor == nil {
		return store.TaskRun{}, ErrUnavailable
	}
	if err := e.Initialize(ctx); err != nil {
		return store.TaskRun{}, err
	}
	targets := make([]store.RunTargetSnapshot, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, err := e.nodes.Get(ctx, nodeID)
		if errors.Is(err, sql.ErrNoRows) {
			return store.TaskRun{}, fmt.Errorf("%w: node", store.ErrNotFound)
		}
		if err != nil {
			return store.TaskRun{}, err
		}
		targets = append(targets, store.RunTargetSnapshot{NodeID: node.ID, NodeName: node.Name})
	}
	detail, err := e.store.CreateManualScriptRun(ctx, scriptID, targets, e.now().UTC())
	if err != nil {
		return store.TaskRun{}, err
	}
	e.dispatch(e.executionContext(ctx), detail)
	return detail.TaskRun, nil
}

func (e *Engine) GetRun(ctx context.Context, runID int64) (*store.TaskRunDetail, error) {
	if e == nil || e.store == nil {
		return nil, ErrUnavailable
	}
	return e.store.GetRun(ctx, runID)
}

func (e *Engine) RunManualTask(ctx context.Context, taskID int64) (store.TaskRun, error) {
	if e == nil || e.store == nil || e.nodes == nil || e.executor == nil {
		return store.TaskRun{}, ErrUnavailable
	}
	if err := e.Initialize(ctx); err != nil {
		return store.TaskRun{}, err
	}
	detail, err := e.store.CreateManualTaskRun(ctx, taskID, e.now().UTC())
	if err != nil {
		return store.TaskRun{}, err
	}
	e.dispatch(e.executionContext(ctx), detail)
	return detail.TaskRun, nil
}

func (e *Engine) dispatch(ctx context.Context, detail store.TaskRunDetail) {
	go func() {
		var waitGroup sync.WaitGroup
		for _, target := range detail.Targets {
			target := target
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				e.executeTarget(ctx, detail.TaskRun, target)
			}()
		}
		waitGroup.Wait()
		e.finalizeRun(ctx, detail.ID)
	}()
}

func (e *Engine) executeTarget(ctx context.Context, run store.TaskRun, target store.TaskRunTarget) {
	nodeSlot := e.nodeSlot(target.NodeID)
	if !acquire(ctx, nodeSlot) {
		e.completeCancelled(target.ID, "server stopped before execution")
		return
	}
	defer release(nodeSlot)
	if !acquire(ctx, e.globalSlots) {
		e.completeCancelled(target.ID, "server stopped before execution")
		return
	}
	defer release(e.globalSlots)

	node, err := e.nodes.Get(ctx, target.NodeID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && node.Status != "online" {
		_, completeErr := e.store.CompleteRunTarget(context.WithoutCancel(ctx), target.ID, store.RunTargetResult{
			Status: store.TargetStatusOffline, Error: "Agent is offline", CompletedAt: e.now().UTC(),
		})
		if completeErr != nil {
			log.Printf("automation target %d offline completion failed: %v", target.ID, completeErr)
		}
		return
	}
	if err != nil {
		result := targetResultForError(err, "node lookup failed", e.now().UTC())
		e.completeTarget(ctx, target.ID, result, "node lookup failure")
		return
	}
	if !e.executor.TaskRunnerSupported(target.NodeID) {
		_, completeErr := e.store.CompleteRunTarget(context.WithoutCancel(ctx), target.ID, store.RunTargetResult{
			Status: store.TargetStatusUnsupported, Error: "Agent does not support task execution; upgrade the Agent", CompletedAt: e.now().UTC(),
		})
		if completeErr != nil {
			log.Printf("automation target %d unsupported completion failed: %v", target.ID, completeErr)
		}
		return
	}

	startedAt := e.now().UTC()
	if err := e.store.MarkRunTargetRunning(ctx, target.ID, startedAt); err != nil {
		log.Printf("automation target %d start failed: %v", target.ID, err)
		result := targetResultForError(err, "script execution could not start", e.now().UTC())
		e.completeTarget(ctx, target.ID, result, "start failure")
		return
	}
	response, err := e.executor.RunScript(ctx, target.NodeID, protocol.ScriptExecutionRequest{
		ExecutionID: target.ID, Script: run.ScriptContent, TimeoutSeconds: run.TimeoutSeconds,
	})
	completedAt := e.now().UTC()
	if err != nil {
		result := targetResultForError(err, "script execution service failed", completedAt)
		result.StartedAt = &startedAt
		result.DurationMS = max(completedAt.Sub(startedAt).Milliseconds(), 0)
		e.completeTarget(ctx, target.ID, result, "execution failure")
		return
	}
	duration := response.DurationMS
	if duration <= 0 {
		duration = max(completedAt.Sub(startedAt).Milliseconds(), 0)
	}
	_, completeErr := e.store.CompleteRunTarget(context.WithoutCancel(ctx), target.ID, store.RunTargetResult{
		Status: mapTargetStatus(response.Status), ExitCode: response.ExitCode,
		Output: response.Output, OutputTruncated: response.OutputTruncated,
		Error: response.Error, DurationMS: duration, StartedAt: &startedAt, CompletedAt: completedAt,
	})
	if completeErr != nil {
		log.Printf("automation target %d completion failed: %v", target.ID, completeErr)
	}
}

func (e *Engine) completeCancelled(targetID int64, message string) {
	e.completeTarget(context.Background(), targetID, store.RunTargetResult{
		Status: store.TargetStatusCancelled, Error: message, CompletedAt: e.now().UTC(),
	}, "cancellation")
}

func (e *Engine) completeTarget(ctx context.Context, targetID int64, result store.RunTargetResult, operation string) {
	_, err := e.store.CompleteRunTarget(context.WithoutCancel(ctx), targetID, result)
	if err != nil {
		log.Printf("automation target %d %s completion failed: %v", targetID, operation, err)
	}
}

func targetResultForError(err error, fallbackMessage string, completedAt time.Time) store.RunTargetResult {
	result := store.RunTargetResult{
		Status: store.TargetStatusFailed, Error: fallbackMessage, CompletedAt: completedAt,
	}
	switch {
	case errors.Is(err, context.Canceled):
		result.Status = store.TargetStatusCancelled
		result.Error = "script execution was cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		result.Status = store.TargetStatusTimedOut
		result.Error = "script execution timed out"
	}
	return result
}

func (e *Engine) finalizeRun(ctx context.Context, runID int64) {
	run, err := e.store.AggregateRun(context.WithoutCancel(ctx), runID, e.now().UTC())
	if err != nil {
		log.Printf("automation run %d aggregation failed: %v", runID, err)
		return
	}
	detail, err := e.store.GetRun(context.WithoutCancel(ctx), runID)
	if err != nil {
		log.Printf("automation run %d detail load failed: %v", runID, err)
		return
	}
	if shouldNotify(*run) && e.notifier != nil {
		notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), taskNotificationTimeout)
		result := e.notifier.DeliverTask(notifyCtx, run.NotificationChannels, taskPayload(*detail))
		cancel()
		if err := e.store.UpdateRunNotification(context.WithoutCancel(ctx), run.ID, result.Sent, result.Error, result.AttemptedAt); err != nil {
			log.Printf("automation run %d notification result persistence failed: %v", run.ID, err)
		}
	}
	if run.Trigger == store.RunTriggerScheduled {
		result := serveraudit.ResultSuccess
		if run.Status != store.RunStatusSuccess {
			result = serveraudit.ResultFailure
		}
		serveraudit.RecordSystem(e.audit, serveraudit.RecordOptions{
			Module: "automation", Action: "scheduled_run", TargetType: "task_run",
			TargetID: strconv.FormatInt(run.ID, 10), TargetName: run.TaskName,
			Result: result, Summary: run.Status, Duration: runDuration(*run),
			Metadata: map[string]string{
				"task_id": formatOptionalID(run.TaskID), "script_id": formatOptionalID(run.ScriptID),
				"target_count": strconv.Itoa(run.TotalTargets),
			},
		})
	}
}

func shouldNotify(run store.TaskRun) bool {
	switch run.NotificationPolicy {
	case store.NotificationPolicyAlways:
		return true
	case store.NotificationPolicyFailure:
		return run.Status != store.RunStatusSuccess
	default:
		return false
	}
}

func taskPayload(detail store.TaskRunDetail) alerting.TaskPayload {
	failures := make([]alerting.TaskTargetSummary, 0)
	skipped := 0
	for _, target := range detail.Targets {
		if target.Status == store.TargetStatusSkipped {
			skipped++
		}
		if target.Status == store.TargetStatusSuccess {
			continue
		}
		name := target.NodeName
		if name == "" {
			name = target.NodeID
		}
		failures = append(failures, alerting.TaskTargetSummary{NodeName: name, Status: target.Status, ExitCode: target.ExitCode})
	}
	finishedAt := detail.UpdatedAt.UTC()
	if detail.CompletedAt != nil {
		finishedAt = detail.CompletedAt.UTC()
	}
	return alerting.TaskPayload{
		RunID: detail.ID, TaskName: detail.TaskName, ScriptName: detail.ScriptName,
		Trigger: detail.Trigger, Status: detail.Status, TotalTargets: detail.TotalTargets,
		SuccessfulTargets: detail.SuccessTargets, FailedTargets: detail.FailedTargets,
		SkippedTargets: skipped, DurationMS: runDuration(detail.TaskRun).Milliseconds(),
		Failures: failures, FinishedAt: finishedAt,
	}
}

func runDuration(run store.TaskRun) time.Duration {
	if run.CompletedAt == nil {
		return 0
	}
	started := run.CreatedAt
	if run.StartedAt != nil {
		started = *run.StartedAt
	}
	if run.CompletedAt.Before(started) {
		return 0
	}
	return run.CompletedAt.Sub(started)
}

func mapTargetStatus(status string) string {
	switch status {
	case protocol.ScriptExecutionStatusSuccess:
		return store.TargetStatusSuccess
	case protocol.ScriptExecutionStatusTimedOut:
		return store.TargetStatusTimedOut
	case protocol.ScriptExecutionStatusBusy:
		return store.TargetStatusBusy
	case protocol.ScriptExecutionStatusCancelled:
		return store.TargetStatusCancelled
	case protocol.ScriptExecutionStatusUnsupported:
		return store.TargetStatusUnsupported
	default:
		return store.TargetStatusFailed
	}
}

func (e *Engine) reserveSchedule(taskID int64) bool {
	e.scheduleMu.Lock()
	defer e.scheduleMu.Unlock()
	if _, exists := e.schedules[taskID]; exists {
		return false
	}
	e.schedules[taskID] = struct{}{}
	return true
}

func (e *Engine) releaseSchedule(taskID int64) {
	e.scheduleMu.Lock()
	delete(e.schedules, taskID)
	e.scheduleMu.Unlock()
}

func (e *Engine) nodeSlot(nodeID string) chan struct{} {
	e.nodeMu.Lock()
	defer e.nodeMu.Unlock()
	if slot := e.nodeSlots[nodeID]; slot != nil {
		return slot
	}
	slot := make(chan struct{}, 1)
	e.nodeSlots[nodeID] = slot
	return slot
}

func acquire(ctx context.Context, slot chan struct{}) bool {
	select {
	case slot <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func release(slot chan struct{}) {
	<-slot
}

func (e *Engine) setRunContext(ctx context.Context) {
	e.contextMu.Lock()
	e.runContext = ctx
	e.contextMu.Unlock()
}

func (e *Engine) executionContext(fallback context.Context) context.Context {
	e.contextMu.RLock()
	ctx := e.runContext
	e.contextMu.RUnlock()
	if ctx != nil {
		return ctx
	}
	return context.WithoutCancel(fallback)
}

func formatOptionalID(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
