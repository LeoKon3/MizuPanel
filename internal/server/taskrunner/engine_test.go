package taskrunner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/alerting"
	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

const engineTestTimeout = 5 * time.Second

type engineTestExecutionCall struct {
	NodeID  string
	Request protocol.ScriptExecutionRequest
}

type engineTestExecutor struct {
	mu sync.Mutex

	supported map[string]bool
	responses map[string]protocol.ScriptExecutionResponse
	errors    map[string]error
	gate      <-chan struct{}
	entered   chan string

	calls       []engineTestExecutionCall
	active      int
	maxActive   int
	activeNodes map[string]int
	maxByNode   map[string]int
}

func newEngineTestExecutor() *engineTestExecutor {
	return &engineTestExecutor{
		supported:   make(map[string]bool),
		responses:   make(map[string]protocol.ScriptExecutionResponse),
		errors:      make(map[string]error),
		entered:     make(chan string, 256),
		activeNodes: make(map[string]int),
		maxByNode:   make(map[string]int),
	}
}

func (e *engineTestExecutor) TaskRunnerSupported(nodeID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	supported, configured := e.supported[nodeID]
	return !configured || supported
}

func (e *engineTestExecutor) RunScript(ctx context.Context, nodeID string, request protocol.ScriptExecutionRequest) (protocol.ScriptExecutionResponse, error) {
	e.mu.Lock()
	e.active++
	if e.active > e.maxActive {
		e.maxActive = e.active
	}
	e.activeNodes[nodeID]++
	if e.activeNodes[nodeID] > e.maxByNode[nodeID] {
		e.maxByNode[nodeID] = e.activeNodes[nodeID]
	}
	e.calls = append(e.calls, engineTestExecutionCall{NodeID: nodeID, Request: request})
	response := e.responses[nodeID]
	runErr := e.errors[nodeID]
	e.mu.Unlock()

	e.entered <- nodeID
	defer func() {
		e.mu.Lock()
		e.active--
		e.activeNodes[nodeID]--
		e.mu.Unlock()
	}()

	if e.gate != nil {
		select {
		case <-ctx.Done():
			return protocol.ScriptExecutionResponse{}, ctx.Err()
		case <-e.gate:
		}
	}
	if runErr != nil {
		return protocol.ScriptExecutionResponse{}, runErr
	}
	if response.Status == "" {
		response.Status = protocol.ScriptExecutionStatusSuccess
	}
	return response, nil
}

func (e *engineTestExecutor) snapshot() ([]engineTestExecutionCall, int, map[string]int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	calls := append([]engineTestExecutionCall(nil), e.calls...)
	maxByNode := make(map[string]int, len(e.maxByNode))
	for nodeID, count := range e.maxByNode {
		maxByNode[nodeID] = count
	}
	return calls, e.maxActive, maxByNode
}

type engineTestNotification struct {
	Channels []store.NotificationChannel
	Payload  alerting.TaskPayload
}

type engineTestNotifier struct {
	deliveries chan engineTestNotification
	result     alerting.TaskDeliveryResult
}

func (n *engineTestNotifier) DeliverTask(_ context.Context, channels []store.NotificationChannel, payload alerting.TaskPayload) alerting.TaskDeliveryResult {
	channelsCopy := append([]store.NotificationChannel(nil), channels...)
	payload.Failures = append([]alerting.TaskTargetSummary(nil), payload.Failures...)
	n.deliveries <- engineTestNotification{Channels: channelsCopy, Payload: payload}
	return n.result
}

type engineTestAuditRecorder struct {
	events chan serveraudit.Event
}

func (r *engineTestAuditRecorder) Create(_ context.Context, event *serveraudit.Event) error {
	copy := *event
	copy.Metadata = make(map[string]string, len(event.Metadata))
	for key, value := range event.Metadata {
		copy.Metadata[key] = value
	}
	r.events <- copy
	return nil
}

type engineTestClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *engineTestClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *engineTestClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type engineTestListBarrierStore struct {
	RunStore

	mu      sync.Mutex
	parties int
	arrived int
	release chan struct{}
}

func (s *engineTestListBarrierStore) ListDueScheduledTasks(ctx context.Context, dueAt time.Time, limit int) ([]store.ScheduledTask, error) {
	tasks, err := s.RunStore.ListDueScheduledTasks(ctx, dueAt, limit)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.arrived++
	if s.arrived == s.parties {
		close(s.release)
	}
	release := s.release
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-release:
		return tasks, nil
	}
}

type engineTestRecoveryStore struct {
	RunStore
	recovered chan int64
}

func (s *engineTestRecoveryStore) RecoverInterruptedRuns(ctx context.Context, interruptedAt time.Time) (int64, error) {
	count, err := s.RunStore.RecoverInterruptedRuns(ctx, interruptedAt)
	if err == nil {
		s.recovered <- count
	}
	return count, err
}

type engineTestBlockingRecoveryStore struct {
	RunStore

	started chan struct{}
	release <-chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func (s *engineTestBlockingRecoveryStore) RecoverInterruptedRuns(ctx context.Context, interruptedAt time.Time) (int64, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-s.release:
		return s.RunStore.RecoverInterruptedRuns(ctx, interruptedAt)
	}
}

func (s *engineTestBlockingRecoveryStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type engineTestMarkFailureStore struct {
	RunStore

	err  error
	mu   sync.Mutex
	seen int
}

func (s *engineTestMarkFailureStore) MarkRunTargetRunning(context.Context, int64, time.Time) error {
	s.mu.Lock()
	s.seen++
	s.mu.Unlock()
	return s.err
}

func (s *engineTestMarkFailureStore) attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen
}

type engineTestExecutionNodeProvider struct {
	NodeProvider

	err   error
	mu    sync.Mutex
	calls int
}

func (p *engineTestExecutionNodeProvider) Get(ctx context.Context, nodeID string) (store.Node, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call > 1 {
		return store.Node{}, p.err
	}
	return p.NodeProvider.Get(ctx, nodeID)
}

func TestEngineRunManualScriptPersistsIndependentTargetOutcomes(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	script := createEngineTestScript(t, taskStore, "Outcome script", "printf outcome-marker", 27)
	for _, node := range []struct {
		id     string
		status string
	}{
		{id: "node-success", status: "online"},
		{id: "node-failure", status: "online"},
		{id: "node-offline", status: "offline"},
		{id: "node-legacy", status: "online"},
	} {
		createEngineTestNode(t, nodeStore, node.id, node.status)
	}

	exitZero := 0
	exitSeven := 7
	executor := newEngineTestExecutor()
	executor.supported["node-legacy"] = false
	executor.responses["node-success"] = protocol.ScriptExecutionResponse{
		Status: protocol.ScriptExecutionStatusSuccess, ExitCode: &exitZero,
		Output: "success output", DurationMS: 12,
	}
	executor.responses["node-failure"] = protocol.ScriptExecutionResponse{
		Status: protocol.ScriptExecutionStatusFailed, ExitCode: &exitSeven,
		Output: "failure output", Error: "exit status 7", DurationMS: 19,
	}
	clock := &engineTestClock{now: time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)}
	engine := NewEngineWithDependencies(taskStore, nodeStore, executor, nil, nil, EngineOptions{Now: clock.Now})

	run, err := engine.RunManualScript(context.Background(), script.ID, []string{
		"node-success", "node-failure", "node-offline", "node-legacy",
	})
	if err != nil {
		t.Fatalf("run manual script: %v", err)
	}
	detail := waitEngineTestRunStatus(t, taskStore, run.ID, store.RunStatusPartial)
	if detail.TotalTargets != 4 || detail.CompletedTargets != 4 || detail.SuccessTargets != 1 || detail.FailedTargets != 3 {
		t.Fatalf("aggregate counts = total:%d completed:%d success:%d failed:%d", detail.TotalTargets, detail.CompletedTargets, detail.SuccessTargets, detail.FailedTargets)
	}
	targets := engineTestTargetsByNode(detail.Targets)
	assertEngineTestTarget(t, targets["node-success"], store.TargetStatusSuccess, "success output", "", 0)
	assertEngineTestTarget(t, targets["node-failure"], store.TargetStatusFailed, "failure output", "exit status 7", 7)
	assertEngineTestTarget(t, targets["node-offline"], store.TargetStatusOffline, "", "Agent is offline", -1)
	assertEngineTestTarget(t, targets["node-legacy"], store.TargetStatusUnsupported, "", "Agent does not support task execution; upgrade the Agent", -1)

	calls, _, _ := executor.snapshot()
	if len(calls) != 2 {
		t.Fatalf("executor calls = %d, want 2 for online capable nodes", len(calls))
	}
	for _, call := range calls {
		if call.Request.Script != script.Content || call.Request.TimeoutSeconds != script.TimeoutSeconds || call.Request.ExecutionID <= 0 {
			t.Fatalf("executor request for %s = %+v", call.NodeID, call.Request)
		}
	}
}

func TestEngineInitializationSerializesRecoveryBeforeManualCreation(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	script := createEngineTestScript(t, taskStore, "Initialization barrier", "exit 0", 30)
	createEngineTestNode(t, nodeStore, "initialization-node", "online")
	releaseRecovery := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRecovery) }) }
	t.Cleanup(release)
	blockingStore := &engineTestBlockingRecoveryStore{
		RunStore: taskStore, started: make(chan struct{}), release: releaseRecovery,
	}
	executor := newEngineTestExecutor()
	engine := NewEngineWithDependencies(blockingStore, nodeStore, executor, nil, nil, EngineOptions{SweepInterval: time.Hour})
	serverContext, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()
	engineDone := make(chan struct{})
	go func() {
		engine.Run(serverContext)
		close(engineDone)
	}()

	select {
	case <-blockingStore.started:
	case <-time.After(engineTestTimeout):
		t.Fatal("timed out waiting for startup recovery to begin")
	}
	type manualResult struct {
		run store.TaskRun
		err error
	}
	manualDone := make(chan manualResult, 1)
	go func() {
		run, err := engine.RunManualScript(context.Background(), script.ID, []string{"initialization-node"})
		manualDone <- manualResult{run: run, err: err}
	}()
	select {
	case result := <-manualDone:
		t.Fatalf("manual run escaped recovery barrier: run=%+v error=%v", result.run, result.err)
	case <-time.After(150 * time.Millisecond):
	}
	page, err := taskStore.ListRuns(context.Background(), store.RunFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list runs during recovery: %v", err)
	}
	if len(page.Runs) != 0 {
		t.Fatalf("runs created before recovery completed: %+v", page.Runs)
	}

	release()
	var result manualResult
	select {
	case result = <-manualDone:
	case <-time.After(engineTestTimeout):
		t.Fatal("manual run did not continue after recovery")
	}
	if result.err != nil {
		t.Fatalf("manual run after recovery: %v", result.err)
	}
	waitEngineTestRunStatus(t, taskStore, result.run.ID, store.RunStatusSuccess)
	if err := engine.Initialize(context.Background()); err != nil {
		t.Fatalf("repeat initialize: %v", err)
	}
	if calls := blockingStore.callCount(); calls != 1 {
		t.Fatalf("recovery calls = %d, want exactly one", calls)
	}

	cancelServer()
	select {
	case <-engineDone:
	case <-time.After(engineTestTimeout):
		t.Fatal("engine did not stop after initialization test")
	}
}

func TestEngineActiveExecutorCancellationPersistsCancelled(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	script := createEngineTestScript(t, taskStore, "Cancellation script", "sleep 30", 30)
	createEngineTestNode(t, nodeStore, "cancel-node", "online")
	gate := make(chan struct{})
	defer close(gate)
	executor := newEngineTestExecutor()
	executor.gate = gate
	clock := &engineTestClock{now: time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)}
	recoveryStore := &engineTestRecoveryStore{RunStore: taskStore, recovered: make(chan int64, 1)}
	engine := NewEngineWithDependencies(recoveryStore, nodeStore, executor, nil, nil, EngineOptions{Now: clock.Now, SweepInterval: time.Hour})
	serverContext, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()
	engineDone := make(chan struct{})
	go func() {
		engine.Run(serverContext)
		close(engineDone)
	}()
	select {
	case recovered := <-recoveryStore.recovered:
		if recovered != 0 {
			t.Fatalf("unexpected recovered runs = %d", recovered)
		}
	case <-time.After(engineTestTimeout):
		t.Fatal("timed out waiting for engine initialization")
	}

	run, err := engine.RunManualScript(context.Background(), script.ID, []string{"cancel-node"})
	if err != nil {
		t.Fatalf("run cancellable script: %v", err)
	}
	waitEngineTestEntry(t, executor.entered)
	clock.Set(clock.Now().Add(2 * time.Second))
	cancelServer()
	select {
	case <-engineDone:
	case <-time.After(engineTestTimeout):
		t.Fatal("engine did not stop after cancellation")
	}
	detail := waitEngineTestRunStatus(t, taskStore, run.ID, store.RunStatusFailed)
	if len(detail.Targets) != 1 || detail.Targets[0].Status != store.TargetStatusCancelled ||
		detail.Targets[0].Error != "script execution was cancelled" || detail.Targets[0].StartedAt == nil ||
		detail.Targets[0].DurationMS != 2000 {
		t.Fatalf("cancelled target = %+v", detail.Targets)
	}
	recovered, err := taskStore.RecoverInterruptedRuns(context.Background(), clock.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("recover after graceful cancellation: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("gracefully cancelled run recovered as interrupted: %d", recovered)
	}
	persisted, err := taskStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get cancelled run: %v", err)
	}
	if persisted.Targets[0].Status != store.TargetStatusCancelled {
		t.Fatalf("recovery changed cancelled target: %+v", persisted.Targets[0])
	}
}

func TestEngineExecutorDeadlinePersistsTimedOut(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	script := createEngineTestScript(t, taskStore, "Deadline script", "sleep 30", 30)
	createEngineTestNode(t, nodeStore, "deadline-node", "online")
	executor := newEngineTestExecutor()
	executor.errors["deadline-node"] = fmt.Errorf("rpc deadline: %w", context.DeadlineExceeded)
	engine := NewEngineWithDependencies(taskStore, nodeStore, executor, nil, nil, EngineOptions{})

	run, err := engine.RunManualScript(context.Background(), script.ID, []string{"deadline-node"})
	if err != nil {
		t.Fatalf("run deadline script: %v", err)
	}
	detail := waitEngineTestRunStatus(t, taskStore, run.ID, store.RunStatusFailed)
	if len(detail.Targets) != 1 || detail.Targets[0].Status != store.TargetStatusTimedOut || detail.Targets[0].Error != "script execution timed out" {
		t.Fatalf("timed out target = %+v", detail.Targets)
	}
}

func TestEngineMarkRunningFailureCompletesQueuedTarget(t *testing.T) {
	tests := []struct {
		name       string
		markError  error
		wantStatus string
		wantError  string
	}{
		{name: "storage failure", markError: errors.New("mark-running-storage-marker"), wantStatus: store.TargetStatusFailed, wantError: "script execution could not start"},
		{name: "cancelled", markError: fmt.Errorf("mark cancelled: %w", context.Canceled), wantStatus: store.TargetStatusCancelled, wantError: "script execution was cancelled"},
		{name: "deadline", markError: fmt.Errorf("mark deadline: %w", context.DeadlineExceeded), wantStatus: store.TargetStatusTimedOut, wantError: "script execution timed out"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			taskStore, nodeStore := newEngineTestStores(t)
			script := createEngineTestScript(t, taskStore, "Mark failure "+test.name, "exit 0", 30)
			createEngineTestNode(t, nodeStore, "mark-failure-node", "online")
			faultStore := &engineTestMarkFailureStore{RunStore: taskStore, err: test.markError}
			executor := newEngineTestExecutor()
			engine := NewEngineWithDependencies(faultStore, nodeStore, executor, nil, nil, EngineOptions{})

			run, err := engine.RunManualScript(context.Background(), script.ID, []string{"mark-failure-node"})
			if err != nil {
				t.Fatalf("run script: %v", err)
			}
			detail := waitEngineTestRunStatus(t, taskStore, run.ID, store.RunStatusFailed)
			if len(detail.Targets) != 1 || detail.Targets[0].Status != test.wantStatus || detail.Targets[0].Error != test.wantError ||
				detail.Targets[0].StartedAt != nil || detail.Targets[0].CompletedAt == nil {
				t.Fatalf("terminal target after mark failure = %+v", detail.Targets)
			}
			if strings.Contains(detail.Targets[0].Error, "mark-running-storage-marker") {
				t.Fatalf("target error leaked storage failure: %q", detail.Targets[0].Error)
			}
			if attempts := faultStore.attempts(); attempts != 1 {
				t.Fatalf("mark attempts = %d, want 1", attempts)
			}
			calls, _, _ := executor.snapshot()
			if len(calls) != 0 {
				t.Fatalf("executor called after mark failure: %+v", calls)
			}
		})
	}
}

func TestEngineNodeLookupCancellationPersistsCancelled(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	script := createEngineTestScript(t, taskStore, "Lookup cancellation", "exit 0", 30)
	createEngineTestNode(t, nodeStore, "lookup-node", "online")
	nodes := &engineTestExecutionNodeProvider{NodeProvider: nodeStore, err: fmt.Errorf("lookup cancelled: %w", context.Canceled)}
	executor := newEngineTestExecutor()
	engine := NewEngineWithDependencies(taskStore, nodes, executor, nil, nil, EngineOptions{})

	run, err := engine.RunManualScript(context.Background(), script.ID, []string{"lookup-node"})
	if err != nil {
		t.Fatalf("run script: %v", err)
	}
	detail := waitEngineTestRunStatus(t, taskStore, run.ID, store.RunStatusFailed)
	if len(detail.Targets) != 1 || detail.Targets[0].Status != store.TargetStatusCancelled || detail.Targets[0].Error != "script execution was cancelled" {
		t.Fatalf("cancelled lookup target = %+v", detail.Targets)
	}
	calls, _, _ := executor.snapshot()
	if len(calls) != 0 {
		t.Fatalf("executor called after cancelled node lookup: %+v", calls)
	}
}

func TestEngineRejectsSchedulingWithoutNodeProvider(t *testing.T) {
	taskStore, _ := newEngineTestStores(t)
	executor := newEngineTestExecutor()
	engine := NewEngineWithDependencies(taskStore, nil, executor, nil, nil, EngineOptions{})
	if _, err := engine.RunManualTask(context.Background(), 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RunManualTask error = %v, want ErrUnavailable", err)
	}
	if err := engine.Sweep(context.Background(), time.Now().UTC()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Sweep error = %v, want ErrUnavailable", err)
	}
	page, err := taskStore.ListRuns(context.Background(), store.RunFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(page.Runs) != 0 {
		t.Fatalf("unavailable engine created runs: %+v", page.Runs)
	}
}

func TestEngineEnforcesGlobalAndPerNodeConcurrency(t *testing.T) {
	t.Run("global limit", func(t *testing.T) {
		taskStore, nodeStore := newEngineTestStores(t)
		script := createEngineTestScript(t, taskStore, "Global concurrency", "exit 0", 30)
		nodeIDs := make([]string, 0, 12)
		for index := 0; index < 12; index++ {
			nodeID := fmt.Sprintf("global-node-%02d", index)
			nodeIDs = append(nodeIDs, nodeID)
			createEngineTestNode(t, nodeStore, nodeID, "online")
		}
		gate := make(chan struct{})
		var releaseOnce sync.Once
		releaseAll := func() { releaseOnce.Do(func() { close(gate) }) }
		t.Cleanup(releaseAll)
		executor := newEngineTestExecutor()
		executor.gate = gate
		engine := NewEngineWithDependencies(taskStore, nodeStore, executor, nil, nil, EngineOptions{GlobalConcurrency: 8})

		run, err := engine.RunManualScript(context.Background(), script.ID, nodeIDs)
		if err != nil {
			t.Fatalf("run manual script: %v", err)
		}
		for index := 0; index < 8; index++ {
			waitEngineTestEntry(t, executor.entered)
		}
		select {
		case nodeID := <-executor.entered:
			t.Fatalf("ninth executor entered before a slot was released: %s", nodeID)
		case <-time.After(150 * time.Millisecond):
		}
		_, maxActive, _ := executor.snapshot()
		if maxActive != 8 {
			t.Fatalf("maximum global concurrency = %d, want 8", maxActive)
		}

		releaseAll()
		waitEngineTestRunStatus(t, taskStore, run.ID, store.RunStatusSuccess)
		calls, maxActive, _ := executor.snapshot()
		if len(calls) != len(nodeIDs) || maxActive > 8 {
			t.Fatalf("calls = %d, max concurrency = %d, want calls=%d and max<=8", len(calls), maxActive, len(nodeIDs))
		}
	})

	t.Run("one active execution per node", func(t *testing.T) {
		taskStore, nodeStore := newEngineTestStores(t)
		script := createEngineTestScript(t, taskStore, "Node serialization", "exit 0", 30)
		createEngineTestNode(t, nodeStore, "shared-node", "online")
		gate := make(chan struct{})
		defer close(gate)
		executor := newEngineTestExecutor()
		executor.gate = gate
		engine := NewEngineWithDependencies(taskStore, nodeStore, executor, nil, nil, EngineOptions{GlobalConcurrency: 8})

		first, err := engine.RunManualScript(context.Background(), script.ID, []string{"shared-node"})
		if err != nil {
			t.Fatalf("run first script: %v", err)
		}
		second, err := engine.RunManualScript(context.Background(), script.ID, []string{"shared-node"})
		if err != nil {
			t.Fatalf("run second script: %v", err)
		}
		waitEngineTestEntry(t, executor.entered)
		select {
		case nodeID := <-executor.entered:
			t.Fatalf("second execution entered for serialized node: %s", nodeID)
		case <-time.After(150 * time.Millisecond):
		}

		gate <- struct{}{}
		waitEngineTestEntry(t, executor.entered)
		_, _, maxByNode := executor.snapshot()
		if maxByNode["shared-node"] != 1 {
			t.Fatalf("maximum concurrency for shared-node = %d, want 1", maxByNode["shared-node"])
		}
		gate <- struct{}{}
		waitEngineTestRunStatus(t, taskStore, first.ID, store.RunStatusSuccess)
		waitEngineTestRunStatus(t, taskStore, second.ID, store.RunStatusSuccess)
		calls, _, maxByNode := executor.snapshot()
		if len(calls) != 2 || maxByNode["shared-node"] > 1 {
			t.Fatalf("calls = %d, max per-node concurrency = %d", len(calls), maxByNode["shared-node"])
		}
	})
}

func TestEngineSweepCollapsesMissedOccurrencesAndClaimsOnce(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	dueAt := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	claimedAt := time.Date(2026, 7, 26, 12, 37, 0, 0, time.UTC)
	nextExpected := time.Date(2026, 7, 26, 12, 40, 0, 0, time.UTC)
	createEngineTestNode(t, nodeStore, "scheduled-node", "online")
	script := createEngineTestScript(t, taskStore, "Catch-up script", "printf sensitive-script-marker", 30)
	channels := []store.NotificationChannel{{
		Type: "webhook", WebhookURL: "https://notify.invalid/task", Secret: "channel-secret-marker",
	}}
	task := createEngineTestTask(t, taskStore, script.ID, "Catch-up task", "*/5 * * * *", dueAt, store.NotificationPolicyAlways, channels, "scheduled-node")

	executor := newEngineTestExecutor()
	executor.responses["scheduled-node"] = protocol.ScriptExecutionResponse{
		Status: protocol.ScriptExecutionStatusSuccess, Output: "sensitive-output-marker", DurationMS: 23,
	}
	notificationTime := claimedAt.Add(time.Second)
	notifier := &engineTestNotifier{
		deliveries: make(chan engineTestNotification, 4),
		result:     alerting.TaskDeliveryResult{Sent: true, AttemptedAt: notificationTime},
	}
	auditRecorder := &engineTestAuditRecorder{events: make(chan serveraudit.Event, 4)}
	barrierStore := &engineTestListBarrierStore{
		RunStore: taskStore, parties: 2, release: make(chan struct{}),
	}
	clock := &engineTestClock{now: claimedAt}
	firstEngine := NewEngineWithDependencies(barrierStore, nodeStore, executor, notifier, auditRecorder, EngineOptions{Now: clock.Now})
	secondEngine := NewEngineWithDependencies(barrierStore, nodeStore, executor, notifier, auditRecorder, EngineOptions{Now: clock.Now})

	sweepContext, cancelSweeps := context.WithTimeout(context.Background(), engineTestTimeout)
	defer cancelSweeps()
	errors := make(chan error, 2)
	go func() { errors <- firstEngine.Sweep(sweepContext, claimedAt) }()
	go func() { errors <- secondEngine.Sweep(sweepContext, claimedAt) }()
	for index := 0; index < 2; index++ {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent sweep: %v", err)
		}
	}

	page := waitEngineTestRunCount(t, taskStore, 1)
	if len(page.Runs) != 1 {
		t.Fatalf("scheduled runs = %d, want exactly one", len(page.Runs))
	}
	detail := waitEngineTestRunStatus(t, taskStore, page.Runs[0].ID, store.RunStatusSuccess)
	if detail.ScheduledFor == nil || !detail.ScheduledFor.Equal(dueAt) {
		t.Fatalf("scheduled_for = %v, want missed occurrence %s", detail.ScheduledFor, dueAt)
	}
	loadedTask, err := taskStore.GetScheduledTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("get scheduled task: %v", err)
	}
	if loadedTask.NextRunAt == nil || !loadedTask.NextRunAt.Equal(nextExpected) {
		t.Fatalf("next_run_at = %v, want first occurrence after sweep %s", loadedTask.NextRunAt, nextExpected)
	}
	if loadedTask.LastScheduledAt == nil || !loadedTask.LastScheduledAt.Equal(dueAt) {
		t.Fatalf("last_scheduled_at = %v, want %s", loadedTask.LastScheduledAt, dueAt)
	}

	delivery := waitEngineTestNotification(t, notifier.deliveries)
	event := waitEngineTestAudit(t, auditRecorder.events)
	if delivery.Payload.RunID != detail.ID || delivery.Payload.Status != store.RunStatusSuccess || delivery.Payload.SuccessfulTargets != 1 || len(delivery.Channels) != 1 {
		t.Fatalf("notification = %+v", delivery)
	}
	if event.ActorType != serveraudit.ActorSystem || event.Module != "automation" || event.Action != "scheduled_run" ||
		event.TargetID != strconv.FormatInt(detail.ID, 10) || event.Result != serveraudit.ResultSuccess || event.Summary != store.RunStatusSuccess {
		t.Fatalf("system audit event = %+v", event)
	}
	if event.Metadata["task_id"] != strconv.FormatInt(task.ID, 10) || event.Metadata["script_id"] != strconv.FormatInt(script.ID, 10) || event.Metadata["target_count"] != "1" {
		t.Fatalf("system audit metadata = %#v", event.Metadata)
	}
	assertEngineTestSecretsAbsent(t, delivery.Payload, event, "sensitive-script-marker", "sensitive-output-marker", "channel-secret-marker")

	persisted, err := taskStore.GetRun(context.Background(), detail.ID)
	if err != nil {
		t.Fatalf("get notified run: %v", err)
	}
	if !persisted.NotificationSent || persisted.NotificationAttemptedAt == nil || !persisted.NotificationAttemptedAt.Equal(notificationTime) {
		t.Fatalf("persisted notification result = sent:%t attempted:%v error:%q", persisted.NotificationSent, persisted.NotificationAttemptedAt, persisted.NotificationError)
	}
	calls, _, _ := executor.snapshot()
	if len(calls) != 1 {
		t.Fatalf("executor calls = %d, want one claimed occurrence", len(calls))
	}
}

func TestEngineConcurrentSweepClaimsOneTimeTaskExactlyOnce(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	runAt := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	createEngineTestNode(t, nodeStore, "once-node", "online")
	script := createEngineTestScript(t, taskStore, "Once script", "exit 0", 30)
	task := store.ScheduledTask{
		Name: "Once task", ScriptID: script.ID, ScheduleType: store.ScheduleTypeOnce,
		RunAt: &runAt, Timezone: "UTC", Enabled: true, TimeoutSeconds: 30,
		NotificationPolicy: store.NotificationPolicyNever, NotificationChannels: []store.NotificationChannel{},
		NodeIDs: []string{"once-node"}, NextRunAt: &runAt,
	}
	if err := taskStore.CreateScheduledTask(context.Background(), &task); err != nil {
		t.Fatalf("create one-time task: %v", err)
	}
	executor := newEngineTestExecutor()
	barrierStore := &engineTestListBarrierStore{RunStore: taskStore, parties: 2, release: make(chan struct{})}
	first := NewEngineWithDependencies(barrierStore, nodeStore, executor, nil, nil, EngineOptions{Now: func() time.Time { return runAt }})
	second := NewEngineWithDependencies(barrierStore, nodeStore, executor, nil, nil, EngineOptions{Now: func() time.Time { return runAt }})

	errors := make(chan error, 2)
	go func() { errors <- first.Sweep(context.Background(), runAt) }()
	go func() { errors <- second.Sweep(context.Background(), runAt) }()
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent one-time sweep: %v", err)
		}
	}
	page := waitEngineTestRunCount(t, taskStore, 1)
	waitEngineTestRunStatus(t, taskStore, page.Runs[0].ID, store.RunStatusSuccess)
	loaded, err := taskStore.GetScheduledTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("get one-time task: %v", err)
	}
	if loaded.Enabled || loaded.NextRunAt != nil || loaded.LastScheduledAt == nil || !loaded.LastScheduledAt.Equal(runAt) {
		t.Fatalf("claimed one-time task = %+v", loaded)
	}
	calls, _, _ := executor.snapshot()
	if len(calls) != 1 {
		t.Fatalf("one-time executor calls = %d, want 1", len(calls))
	}
}

func TestEngineRestartRecoveryDoesNotReplayClaimedOneTimeTask(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	runAt := time.Date(2026, 8, 14, 11, 0, 0, 0, time.UTC)
	createEngineTestNode(t, nodeStore, "restart-once-node", "online")
	script := createEngineTestScript(t, taskStore, "Restart once script", "exit 0", 30)
	task := store.ScheduledTask{
		Name: "Restart once task", ScriptID: script.ID, ScheduleType: store.ScheduleTypeOnce,
		RunAt: &runAt, Timezone: "UTC", Enabled: true, TimeoutSeconds: 30,
		NotificationPolicy: store.NotificationPolicyNever, NotificationChannels: []store.NotificationChannel{},
		NodeIDs: []string{"restart-once-node"}, NextRunAt: &runAt,
	}
	if err := taskStore.CreateScheduledTask(context.Background(), &task); err != nil {
		t.Fatalf("create restart one-time task: %v", err)
	}
	claimed, err := taskStore.ClaimDueTask(context.Background(), task.ID, runAt, time.Time{}, runAt)
	if err != nil {
		t.Fatalf("claim one-time task before restart: %v", err)
	}
	executor := newEngineTestExecutor()
	restarted := NewEngineWithDependencies(taskStore, nodeStore, executor, nil, nil, EngineOptions{Now: func() time.Time { return runAt.Add(time.Minute) }})
	if err := restarted.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize restarted engine: %v", err)
	}
	if err := restarted.Sweep(context.Background(), runAt.Add(time.Minute)); err != nil {
		t.Fatalf("sweep restarted engine: %v", err)
	}
	detail := waitEngineTestRunStatus(t, taskStore, claimed.ID, store.RunStatusInterrupted)
	if detail.Status != store.RunStatusInterrupted {
		t.Fatalf("recovered run = %+v", detail.TaskRun)
	}
	page := waitEngineTestRunCount(t, taskStore, 1)
	if len(page.Runs) != 1 {
		t.Fatalf("restart replayed one-time task: %+v", page.Runs)
	}
	calls, _, _ := executor.snapshot()
	if len(calls) != 0 {
		t.Fatalf("restart executed claimed one-time task %d time(s)", len(calls))
	}
}

func TestEngineNotificationFailureDoesNotChangeCompletedRunStatus(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	now := time.Date(2026, 7, 26, 13, 0, 0, 0, time.UTC)
	createEngineTestNode(t, nodeStore, "notify-node", "online")
	script := createEngineTestScript(t, taskStore, "Notify script", "exit 0", 30)
	task := createEngineTestTask(t, taskStore, script.ID, "Notify task", "0 * * * *", now.Add(time.Hour), store.NotificationPolicyAlways,
		[]store.NotificationChannel{{Type: "webhook", WebhookURL: "https://notify.invalid/task"}}, "notify-node")

	notificationTime := now.Add(time.Second)
	notifier := &engineTestNotifier{
		deliveries: make(chan engineTestNotification, 1),
		result: alerting.TaskDeliveryResult{
			Sent: false, Error: "notification delivery failed", AttemptedAt: notificationTime,
		},
	}
	clock := &engineTestClock{now: now}
	engine := NewEngineWithDependencies(taskStore, nodeStore, newEngineTestExecutor(), notifier, nil, EngineOptions{Now: clock.Now})

	run, err := engine.RunManualTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("run manual task: %v", err)
	}
	completed := waitEngineTestRunStatus(t, taskStore, run.ID, store.RunStatusSuccess)
	delivery := waitEngineTestNotification(t, notifier.deliveries)
	if delivery.Payload.RunID != run.ID || delivery.Payload.Status != store.RunStatusSuccess {
		t.Fatalf("notification delivery = %+v", delivery.Payload)
	}

	persisted, err := taskStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run after notification failure: %v", err)
	}
	if persisted.Status != completed.Status || persisted.Status != store.RunStatusSuccess {
		t.Fatalf("notification failure changed run status: persisted=%q completed=%q", persisted.Status, completed.Status)
	}
	if persisted.NotificationSent || persisted.NotificationError != "notification delivery failed" ||
		persisted.NotificationAttemptedAt == nil || !persisted.NotificationAttemptedAt.Equal(notificationTime) {
		t.Fatalf("persisted notification failure = sent:%t error:%q attempted:%v", persisted.NotificationSent, persisted.NotificationError, persisted.NotificationAttemptedAt)
	}
}

func TestEngineSweepRecordsOverlapAsSkipped(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	firstDue := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	secondDue := firstDue.Add(time.Minute)
	createEngineTestNode(t, nodeStore, "overlap-node", "online")
	script := createEngineTestScript(t, taskStore, "Overlap script", "exit 0", 30)
	createEngineTestTask(t, taskStore, script.ID, "Overlap task", "* * * * *", firstDue, store.NotificationPolicyFailure,
		[]store.NotificationChannel{{Type: "webhook", WebhookURL: "https://notify.invalid/overlap"}}, "overlap-node")

	gate := make(chan struct{})
	defer close(gate)
	executor := newEngineTestExecutor()
	executor.gate = gate
	clock := &engineTestClock{now: firstDue}
	notifier := &engineTestNotifier{
		deliveries: make(chan engineTestNotification, 4),
		result:     alerting.TaskDeliveryResult{Sent: true, AttemptedAt: secondDue},
	}
	auditRecorder := &engineTestAuditRecorder{events: make(chan serveraudit.Event, 4)}
	engine := NewEngineWithDependencies(taskStore, nodeStore, executor, notifier, auditRecorder, EngineOptions{Now: clock.Now})

	if err := engine.Sweep(context.Background(), firstDue); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	waitEngineTestEntry(t, executor.entered)
	firstPage := waitEngineTestRunCount(t, taskStore, 1)
	firstRunID := firstPage.Runs[0].ID
	clock.Set(secondDue)
	if err := engine.Sweep(context.Background(), secondDue); err != nil {
		t.Fatalf("overlap sweep: %v", err)
	}

	page := waitEngineTestRunCount(t, taskStore, 2)
	var skippedRunID int64
	for _, run := range page.Runs {
		if run.Status == store.RunStatusSkipped {
			skippedRunID = run.ID
		}
	}
	if skippedRunID == 0 {
		t.Fatalf("overlap runs = %+v, want a skipped run", page.Runs)
	}
	skipped := waitEngineTestRunStatus(t, taskStore, skippedRunID, store.RunStatusSkipped)
	if skipped.Error != "overlap" || len(skipped.Targets) != 1 || skipped.Targets[0].Status != store.TargetStatusSkipped || skipped.Targets[0].Error != "overlap" {
		t.Fatalf("skipped overlap detail = %+v", skipped)
	}
	delivery := waitEngineTestNotification(t, notifier.deliveries)
	if delivery.Payload.RunID != skippedRunID || delivery.Payload.Status != store.RunStatusSkipped || delivery.Payload.SkippedTargets != 1 {
		t.Fatalf("overlap notification = %+v", delivery.Payload)
	}
	skippedAudit := waitEngineTestAudit(t, auditRecorder.events)
	if skippedAudit.TargetID != strconv.FormatInt(skippedRunID, 10) || skippedAudit.Result != serveraudit.ResultFailure || skippedAudit.Summary != store.RunStatusSkipped {
		t.Fatalf("skipped system audit = %+v", skippedAudit)
	}
	calls, _, _ := executor.snapshot()
	if len(calls) != 1 {
		t.Fatalf("executor calls before release = %d, want one non-overlapping execution", len(calls))
	}

	gate <- struct{}{}
	waitEngineTestRunStatus(t, taskStore, firstRunID, store.RunStatusSuccess)
	successAudit := waitEngineTestAudit(t, auditRecorder.events)
	if successAudit.TargetID != strconv.FormatInt(firstRunID, 10) || successAudit.Result != serveraudit.ResultSuccess {
		t.Fatalf("successful system audit = %+v", successAudit)
	}
	select {
	case extra := <-notifier.deliveries:
		t.Fatalf("successful failure-policy run unexpectedly notified: %+v", extra.Payload)
	case <-time.After(100 * time.Millisecond):
	}
	calls, _, _ = executor.snapshot()
	if len(calls) != 1 {
		t.Fatalf("executor calls after completion = %d, want one", len(calls))
	}
}

func TestEngineRunRecoversQueuedAndRunningRunsAsInterrupted(t *testing.T) {
	taskStore, nodeStore := newEngineTestStores(t)
	script := createEngineTestScript(t, taskStore, "Recovery script", "exit 0", 30)
	createdAt := time.Date(2026, 7, 26, 15, 0, 0, 0, time.UTC)
	queued, err := taskStore.CreateManualScriptRun(context.Background(), script.ID,
		[]store.RunTargetSnapshot{{NodeID: "queued-node", NodeName: "Queued"}}, createdAt)
	if err != nil {
		t.Fatalf("create queued run: %v", err)
	}
	running, err := taskStore.CreateManualScriptRun(context.Background(), script.ID,
		[]store.RunTargetSnapshot{{NodeID: "running-node", NodeName: "Running"}}, createdAt.Add(time.Second))
	if err != nil {
		t.Fatalf("create running run: %v", err)
	}
	if err := taskStore.MarkRunTargetRunning(context.Background(), running.Targets[0].ID, createdAt.Add(2*time.Second)); err != nil {
		t.Fatalf("mark running target: %v", err)
	}
	completed, err := taskStore.CreateManualScriptRun(context.Background(), script.ID,
		[]store.RunTargetSnapshot{{NodeID: "completed-node", NodeName: "Completed"}}, createdAt.Add(3*time.Second))
	if err != nil {
		t.Fatalf("create completed run: %v", err)
	}
	if err := taskStore.MarkRunTargetRunning(context.Background(), completed.Targets[0].ID, createdAt.Add(4*time.Second)); err != nil {
		t.Fatalf("mark completed target running: %v", err)
	}
	if _, err := taskStore.CompleteRunTarget(context.Background(), completed.Targets[0].ID, store.RunTargetResult{
		Status: store.TargetStatusSuccess, CompletedAt: createdAt.Add(5 * time.Second),
	}); err != nil {
		t.Fatalf("complete existing run: %v", err)
	}

	recoveryTime := createdAt.Add(time.Hour)
	recoveryStore := &engineTestRecoveryStore{RunStore: taskStore, recovered: make(chan int64, 1)}
	engine := NewEngineWithDependencies(recoveryStore, nodeStore, newEngineTestExecutor(), nil, nil, EngineOptions{
		SweepInterval: time.Hour,
		Now:           func() time.Time { return recoveryTime },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		engine.Run(ctx)
		close(done)
	}()

	select {
	case count := <-recoveryStore.recovered:
		if count != 2 {
			t.Fatalf("recovered runs = %d, want 2", count)
		}
	case <-time.After(engineTestTimeout):
		t.Fatal("timed out waiting for startup recovery")
	}
	queuedDetail := waitEngineTestRunStatus(t, taskStore, queued.ID, store.RunStatusInterrupted)
	runningDetail := waitEngineTestRunStatus(t, taskStore, running.ID, store.RunStatusInterrupted)
	completedDetail := waitEngineTestRunStatus(t, taskStore, completed.ID, store.RunStatusSuccess)
	for _, detail := range []store.TaskRunDetail{queuedDetail, runningDetail} {
		if detail.Error != "server restarted during execution" || len(detail.Targets) != 1 ||
			detail.Targets[0].Status != store.TargetStatusInterrupted || detail.Targets[0].Error != "server restarted during execution" ||
			detail.CompletedAt == nil || !detail.CompletedAt.Equal(recoveryTime) {
			t.Fatalf("recovered detail = %+v", detail)
		}
	}
	if completedDetail.Targets[0].Status != store.TargetStatusSuccess || completedDetail.Error != "" {
		t.Fatalf("completed run changed during recovery: %+v", completedDetail)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(engineTestTimeout):
		t.Fatal("engine did not stop after cancellation")
	}
}

func newEngineTestStores(t *testing.T) (*store.TaskStore, *store.NodeStore) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "engine.db")
	database, err := sql.Open("sqlite3", "file:"+databasePath+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return store.NewTaskStore(database), store.NewNodeStore(database)
}

func createEngineTestScript(t *testing.T, taskStore *store.TaskStore, name string, content string, timeoutSeconds int) store.AutomationScript {
	t.Helper()
	script := store.AutomationScript{Name: name, Content: content, TimeoutSeconds: timeoutSeconds}
	if err := taskStore.CreateScript(context.Background(), &script); err != nil {
		t.Fatalf("create script %q: %v", name, err)
	}
	return script
}

func createEngineTestNode(t *testing.T, nodeStore *store.NodeStore, nodeID string, status string) {
	t.Helper()
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	if err := nodeStore.Upsert(context.Background(), store.Node{
		ID: nodeID, Name: "Node " + nodeID, Status: status,
		LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create node %q: %v", nodeID, err)
	}
}

func createEngineTestTask(t *testing.T, taskStore *store.TaskStore, scriptID int64, name string, cronExpression string,
	dueAt time.Time, policy string, channels []store.NotificationChannel, nodeIDs ...string,
) store.ScheduledTask {
	t.Helper()
	task := store.ScheduledTask{
		Name: name, ScriptID: scriptID, CronExpression: cronExpression, Timezone: "UTC",
		Enabled: true, TimeoutSeconds: 30, NotificationPolicy: policy,
		NotificationChannels: channels, NodeIDs: nodeIDs, NextRunAt: &dueAt,
	}
	if err := taskStore.CreateScheduledTask(context.Background(), &task); err != nil {
		t.Fatalf("create scheduled task %q: %v", name, err)
	}
	return task
}

func waitEngineTestEntry(t *testing.T, entered <-chan string) string {
	t.Helper()
	select {
	case nodeID := <-entered:
		return nodeID
	case <-time.After(engineTestTimeout):
		t.Fatal("timed out waiting for executor entry")
		return ""
	}
}

func waitEngineTestRunStatus(t *testing.T, taskStore *store.TaskStore, runID int64, status string) store.TaskRunDetail {
	t.Helper()
	deadline := time.Now().Add(engineTestTimeout)
	var last store.TaskRunDetail
	var lastErr error
	for time.Now().Before(deadline) {
		detail, err := taskStore.GetRun(context.Background(), runID)
		if err == nil {
			last = *detail
			if detail.Status == status {
				return *detail
			}
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %d did not reach %q: last=%+v error=%v", runID, status, last, lastErr)
	return store.TaskRunDetail{}
}

func waitEngineTestRunCount(t *testing.T, taskStore *store.TaskStore, count int) store.RunPage {
	t.Helper()
	deadline := time.Now().Add(engineTestTimeout)
	var last store.RunPage
	var lastErr error
	for time.Now().Before(deadline) {
		page, err := taskStore.ListRuns(context.Background(), store.RunFilter{Limit: store.MaxRunPageLimit})
		if err == nil {
			last = page
			if len(page.Runs) == count {
				return page
			}
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run count did not reach %d: last=%d error=%v", count, len(last.Runs), lastErr)
	return store.RunPage{}
}

func waitEngineTestNotification(t *testing.T, deliveries <-chan engineTestNotification) engineTestNotification {
	t.Helper()
	select {
	case delivery := <-deliveries:
		return delivery
	case <-time.After(engineTestTimeout):
		t.Fatal("timed out waiting for task notification")
		return engineTestNotification{}
	}
}

func waitEngineTestAudit(t *testing.T, events <-chan serveraudit.Event) serveraudit.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(engineTestTimeout):
		t.Fatal("timed out waiting for system audit")
		return serveraudit.Event{}
	}
}

func engineTestTargetsByNode(targets []store.TaskRunTarget) map[string]store.TaskRunTarget {
	result := make(map[string]store.TaskRunTarget, len(targets))
	for _, target := range targets {
		result[target.NodeID] = target
	}
	return result
}

func assertEngineTestTarget(t *testing.T, target store.TaskRunTarget, status string, output string, message string, exitCode int) {
	t.Helper()
	if target.Status != status || target.Output != output || target.Error != message {
		t.Fatalf("target %q = status:%q output:%q error:%q, want status:%q output:%q error:%q",
			target.NodeID, target.Status, target.Output, target.Error, status, output, message)
	}
	if exitCode < 0 {
		if target.ExitCode != nil {
			t.Fatalf("target %q exit code = %d, want nil", target.NodeID, *target.ExitCode)
		}
		return
	}
	if target.ExitCode == nil || *target.ExitCode != exitCode {
		t.Fatalf("target %q exit code = %v, want %d", target.NodeID, target.ExitCode, exitCode)
	}
}

func assertEngineTestSecretsAbsent(t *testing.T, payload alerting.TaskPayload, event serveraudit.Event, markers ...string) {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal task payload: %v", err)
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal audit event: %v", err)
	}
	serialized := string(payloadJSON) + string(eventJSON)
	for _, marker := range markers {
		if strings.Contains(serialized, marker) {
			t.Fatalf("notification/audit leaked marker %q: %s", marker, serialized)
		}
	}
}
