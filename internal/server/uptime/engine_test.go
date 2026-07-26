package uptime

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/server/alerting"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type proberFunc func(context.Context, store.UptimeMonitor) store.UptimeProbeResult

func (function proberFunc) Probe(ctx context.Context, monitor store.UptimeMonitor) store.UptimeProbeResult {
	return function(ctx, monitor)
}

type recordingUptimeNotifier struct {
	mu            sync.Mutex
	payloads      []alerting.UptimePayload
	contextErrors []error
	deadlines     []time.Time
	delay         time.Duration
	result        alerting.UptimeDeliveryResult
}

func (notifier *recordingUptimeNotifier) DeliverUptime(ctx context.Context, _ []store.NotificationChannel, payload alerting.UptimePayload) alerting.UptimeDeliveryResult {
	notifier.mu.Lock()
	notifier.payloads = append(notifier.payloads, payload)
	notifier.contextErrors = append(notifier.contextErrors, ctx.Err())
	deadline, _ := ctx.Deadline()
	notifier.deadlines = append(notifier.deadlines, deadline)
	delay := notifier.delay
	result := notifier.result
	if result.AttemptedAt.IsZero() {
		result.AttemptedAt = time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	}
	notifier.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	return result
}

func (notifier *recordingUptimeNotifier) recordedContextErrors() []error {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	return append([]error(nil), notifier.contextErrors...)
}

func (notifier *recordingUptimeNotifier) recordedDeadlines() []time.Time {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	return append([]time.Time(nil), notifier.deadlines...)
}

type cancelAfterApplyMonitorStore struct {
	MonitorStore
	cancel context.CancelFunc
}

func (monitorStore cancelAfterApplyMonitorStore) ApplyProbe(ctx context.Context, monitorID int64, probe store.UptimeProbeResult) (store.UptimeTransition, error) {
	transition, err := monitorStore.MonitorStore.ApplyProbe(ctx, monitorID, probe)
	monitorStore.cancel()
	return transition, err
}

type mutateAfterListMonitorStore struct {
	MonitorStore
	afterList func()
}

func (monitorStore mutateAfterListMonitorStore) ListEnabledMonitors(ctx context.Context) ([]store.UptimeMonitor, error) {
	monitors, err := monitorStore.MonitorStore.ListEnabledMonitors(ctx)
	if err == nil && monitorStore.afterList != nil {
		monitorStore.afterList()
	}
	return monitors, err
}

func (notifier *recordingUptimeNotifier) recordedPayloads() []alerting.UptimePayload {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	return append([]alerting.UptimePayload(nil), notifier.payloads...)
}

func testEngineStore(t *testing.T) *store.UptimeStore {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return store.NewUptimeStore(database)
}

func createEngineMonitor(t *testing.T, monitorStore *store.UptimeStore, mutate func(*store.UptimeMonitor)) store.UptimeMonitor {
	t.Helper()
	monitor := validHTTPMonitor()
	if mutate != nil {
		mutate(&monitor)
	}
	if err := ValidateMonitor(&monitor); err != nil {
		t.Fatalf("validate monitor: %v", err)
	}
	if err := monitorStore.CreateMonitor(context.Background(), &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	return monitor
}

func TestEngineCheckNowThresholdRestartSafetyAndRecovery(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, func(monitor *store.UptimeMonitor) {
		monitor.FailureThreshold = 2
		monitor.NotificationChannels = []store.NotificationChannel{{Type: "webhook", WebhookURL: "https://hooks.example.com"}}
	})
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	results := []store.UptimeProbeResult{
		{Error: "连接超时", CheckedAt: base},
		{Error: "连接超时", CheckedAt: base.Add(time.Minute)},
		{Error: "连接超时", CheckedAt: base.Add(2 * time.Minute)},
		{Success: true, StatusCode: 204, LatencyMS: 18, CheckedAt: base.Add(3 * time.Minute)},
	}
	var index atomic.Int32
	prober := proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		return results[int(index.Add(1))-1]
	})
	notifier := &recordingUptimeNotifier{result: alerting.UptimeDeliveryResult{Sent: true}}
	engine := NewEngineWithDependencies(monitorStore, prober, notifier)

	first, err := engine.CheckNow(context.Background(), monitor.ID)
	if err != nil || first.Status != store.UptimeStatusDown || first.ConsecutiveFailures != 1 {
		t.Fatalf("first check monitor=%+v err=%v", first, err)
	}
	if len(notifier.recordedPayloads()) != 0 {
		t.Fatal("first failure must not notify before threshold")
	}
	second, err := engine.CheckNow(context.Background(), monitor.ID)
	if err != nil || second.ConsecutiveFailures != 2 {
		t.Fatalf("second check monitor=%+v err=%v", second, err)
	}
	payloads := notifier.recordedPayloads()
	if len(payloads) != 1 || payloads[0].Status != "triggered" || payloads[0].IncidentKind != store.UptimeIncidentAvailability {
		t.Fatalf("trigger payloads = %+v", payloads)
	}

	// A new Engine instance simulates a Server restart. The active incident in
	// SQLite prevents another trigger notification.
	restarted := NewEngineWithDependencies(monitorStore, prober, notifier)
	if _, err := restarted.CheckNow(context.Background(), monitor.ID); err != nil {
		t.Fatalf("post-restart failure: %v", err)
	}
	if len(notifier.recordedPayloads()) != 1 {
		t.Fatalf("restart caused duplicate notification: %+v", notifier.recordedPayloads())
	}
	recovered, err := restarted.CheckNow(context.Background(), monitor.ID)
	if err != nil || recovered.Status != store.UptimeStatusUp || recovered.ConsecutiveFailures != 0 {
		t.Fatalf("recovery monitor=%+v err=%v", recovered, err)
	}
	payloads = notifier.recordedPayloads()
	if len(payloads) != 2 || payloads[1].Status != "resolved" || payloads[1].ResolvedAt == nil {
		t.Fatalf("recovery payloads = %+v", payloads)
	}
	incidents, err := monitorStore.ListIncidents(context.Background(), monitor.ID, 10)
	if err != nil || len(incidents) != 1 || !incidents[0].NotificationSent || !incidents[0].RecoveryNotificationSent {
		t.Fatalf("persisted incidents=%+v err=%v", incidents, err)
	}
}

func TestEngineCertificateWarningAndRenewalNotifications(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, func(monitor *store.UptimeMonitor) {
		monitor.NotificationChannels = []store.NotificationChannel{{Type: "feishu", WebhookURL: "https://hooks.example.com"}}
	})
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	expiring := base.Add(10 * 24 * time.Hour)
	renewed := base.Add(100 * 24 * time.Hour)
	results := []store.UptimeProbeResult{
		{Success: true, TLSChecked: true, TLSExpiring: true, TLSExpiresAt: &expiring, StatusCode: 200, CheckedAt: base},
		{Success: true, TLSChecked: true, TLSExpiresAt: &renewed, StatusCode: 200, CheckedAt: base.Add(time.Hour)},
	}
	index := 0
	notifier := &recordingUptimeNotifier{result: alerting.UptimeDeliveryResult{Sent: true}}
	engine := NewEngineWithDependencies(monitorStore, proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		result := results[index]
		index++
		return result
	}), notifier)
	warning, err := engine.CheckNow(context.Background(), monitor.ID)
	if err != nil || warning.Status != store.UptimeStatusWarning {
		t.Fatalf("warning=%+v err=%v", warning, err)
	}
	up, err := engine.CheckNow(context.Background(), monitor.ID)
	if err != nil || up.Status != store.UptimeStatusUp {
		t.Fatalf("renewal=%+v err=%v", up, err)
	}
	payloads := notifier.recordedPayloads()
	if len(payloads) != 2 || payloads[0].IncidentKind != store.UptimeIncidentCertificate || payloads[0].Status != "triggered" || payloads[1].Status != "resolved" {
		t.Fatalf("certificate payloads = %+v", payloads)
	}
}

func TestEngineGivesEachTransitionNotificationAFreshTimeout(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, func(monitor *store.UptimeMonitor) {
		monitor.FailureThreshold = 1
		monitor.NotificationChannels = []store.NotificationChannel{{Type: "webhook", WebhookURL: "https://hooks.example.com"}}
	})
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	expiring := base.Add(10 * 24 * time.Hour)
	results := []store.UptimeProbeResult{
		{Error: "连接失败", CheckedAt: base},
		{Success: true, TLSChecked: true, TLSExpiring: true, TLSExpiresAt: &expiring, StatusCode: 200, CheckedAt: base.Add(time.Minute)},
	}
	var index atomic.Int32
	notifier := &recordingUptimeNotifier{result: alerting.UptimeDeliveryResult{Sent: true}}
	engine := NewEngineWithDependencies(monitorStore, proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		return results[int(index.Add(1))-1]
	}), notifier)
	if _, err := engine.CheckNow(context.Background(), monitor.ID); err != nil {
		t.Fatalf("trigger availability: %v", err)
	}
	notifier.mu.Lock()
	notifier.delay = 10 * time.Millisecond
	notifier.mu.Unlock()
	if _, err := engine.CheckNow(context.Background(), monitor.ID); err != nil {
		t.Fatalf("resolve availability and trigger certificate: %v", err)
	}
	deadlines := notifier.recordedDeadlines()
	if len(deadlines) != 3 {
		t.Fatalf("delivery deadlines = %v, want 3", deadlines)
	}
	if !deadlines[2].After(deadlines[1]) {
		t.Fatalf("transition deliveries shared a deadline: %v", deadlines[1:])
	}
}

func TestEngineCheckNowRejectsSameMonitorOverlap(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, nil)
	entered := make(chan struct{})
	release := make(chan struct{})
	prober := proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		close(entered)
		<-release
		return store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: time.Now().UTC()}
	})
	engine := NewEngineWithDependencies(monitorStore, prober, &recordingUptimeNotifier{})
	done := make(chan error, 1)
	go func() {
		_, err := engine.CheckNow(context.Background(), monitor.ID)
		done <- err
	}()
	<-entered
	if _, err := engine.CheckNow(context.Background(), monitor.ID); !errors.Is(err, ErrCheckInProgress) {
		t.Fatalf("overlap error = %v, want ErrCheckInProgress", err)
	}
	if _, err := engine.BeginMonitorMutation(monitor.ID); !errors.Is(err, ErrCheckInProgress) {
		t.Fatalf("mutation overlap error = %v, want ErrCheckInProgress", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first check: %v", err)
	}
}

func TestEngineCheckNowRejectsDisabledMonitor(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, nil)
	if _, err := monitorStore.SetMonitorEnabled(context.Background(), monitor.ID, false); err != nil {
		t.Fatalf("disable monitor: %v", err)
	}
	var probeCalls atomic.Int32
	engine := NewEngineWithDependencies(monitorStore, proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		probeCalls.Add(1)
		return store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: time.Now().UTC()}
	}), &recordingUptimeNotifier{})
	if _, err := engine.CheckNow(context.Background(), monitor.ID); !errors.Is(err, ErrMonitorDisabled) {
		t.Fatalf("disabled check error = %v, want ErrMonitorDisabled", err)
	}
	if probeCalls.Load() != 0 {
		t.Fatalf("disabled monitor probe calls = %d, want 0", probeCalls.Load())
	}
	results, err := monitorStore.ListResults(context.Background(), monitor.ID, 10)
	if err != nil || len(results) != 0 {
		t.Fatalf("disabled monitor results=%+v err=%v", results, err)
	}
}

func TestEngineMutationGuardBlocksChecks(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, nil)
	engine := NewEngineWithDependencies(monitorStore, proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		return store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: time.Now().UTC()}
	}), &recordingUptimeNotifier{})
	release, err := engine.BeginMonitorMutation(monitor.ID)
	if err != nil {
		t.Fatalf("begin mutation: %v", err)
	}
	if _, err := engine.CheckNow(context.Background(), monitor.ID); !errors.Is(err, ErrCheckInProgress) {
		t.Fatalf("check during mutation error = %v, want ErrCheckInProgress", err)
	}
	release()
	if _, err := engine.CheckNow(context.Background(), monitor.ID); err != nil {
		t.Fatalf("check after mutation release: %v", err)
	}
}

func TestEnginePersistsNotificationAfterRequestCancellation(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, func(monitor *store.UptimeMonitor) {
		monitor.FailureThreshold = 1
		monitor.NotificationChannels = []store.NotificationChannel{{Type: "webhook", WebhookURL: "https://hooks.example.com"}}
	})
	ctx, cancel := context.WithCancel(context.Background())
	notifier := &recordingUptimeNotifier{result: alerting.UptimeDeliveryResult{Sent: true}}
	wrappedStore := cancelAfterApplyMonitorStore{MonitorStore: monitorStore, cancel: cancel}
	engine := NewEngineWithDependencies(wrappedStore, proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		return store.UptimeProbeResult{Error: "连接失败", CheckedAt: time.Now().UTC()}
	}), notifier)
	if _, err := engine.CheckNow(ctx, monitor.ID); err != nil {
		t.Fatalf("check after request cancellation: %v", err)
	}
	contextErrors := notifier.recordedContextErrors()
	if len(contextErrors) != 1 || contextErrors[0] != nil {
		t.Fatalf("delivery context errors = %v, want [nil]", contextErrors)
	}
	incidents, err := monitorStore.ListIncidents(context.Background(), monitor.ID, 10)
	if err != nil || len(incidents) != 1 || !incidents[0].NotificationSent {
		t.Fatalf("persisted incidents=%+v err=%v", incidents, err)
	}
}

func TestEngineSweepChecksOnlyDueEnabledMonitors(t *testing.T) {
	monitorStore := testEngineStore(t)
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	due := createEngineMonitor(t, monitorStore, nil)
	notDue := createEngineMonitor(t, monitorStore, nil)
	disabled := createEngineMonitor(t, monitorStore, nil)
	if _, err := monitorStore.ApplyProbe(context.Background(), notDue.ID, store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: base.Add(-10 * time.Second)}); err != nil {
		t.Fatalf("seed recent result: %v", err)
	}
	if _, err := monitorStore.SetMonitorEnabled(context.Background(), disabled.ID, false); err != nil {
		t.Fatalf("disable monitor: %v", err)
	}
	var checkedMu sync.Mutex
	checked := make([]int64, 0)
	engine := NewEngineWithDependencies(monitorStore, proberFunc(func(_ context.Context, monitor store.UptimeMonitor) store.UptimeProbeResult {
		checkedMu.Lock()
		checked = append(checked, monitor.ID)
		checkedMu.Unlock()
		return store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: base}
	}), &recordingUptimeNotifier{})
	engine.now = func() time.Time { return base }
	if err := engine.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(checked) != 1 || checked[0] != due.ID {
		t.Fatalf("checked monitors = %v, want [%d]", checked, due.ID)
	}
}

func TestEngineSweepReloadsMonitorAfterDueList(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, nil)
	wrappedStore := mutateAfterListMonitorStore{
		MonitorStore: monitorStore,
		afterList: func() {
			if _, err := monitorStore.SetMonitorEnabled(context.Background(), monitor.ID, false); err != nil {
				t.Errorf("disable monitor after list: %v", err)
			}
		},
	}
	var probeCalls atomic.Int32
	engine := NewEngineWithDependencies(wrappedStore, proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		probeCalls.Add(1)
		return store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: time.Now().UTC()}
	}), &recordingUptimeNotifier{})
	if err := engine.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if probeCalls.Load() != 0 {
		t.Fatalf("stale enabled snapshot probe calls = %d, want 0", probeCalls.Load())
	}
}

func TestEngineSweepRechecksDueStateAfterList(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, nil)
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	wrappedStore := mutateAfterListMonitorStore{
		MonitorStore: monitorStore,
		afterList: func() {
			if _, err := monitorStore.ApplyProbe(context.Background(), monitor.ID, store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: base}); err != nil {
				t.Errorf("complete check after list: %v", err)
			}
		},
	}
	var probeCalls atomic.Int32
	engine := NewEngineWithDependencies(wrappedStore, proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		probeCalls.Add(1)
		return store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: base}
	}), &recordingUptimeNotifier{})
	engine.now = func() time.Time { return base }
	if err := engine.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if probeCalls.Load() != 0 {
		t.Fatalf("recently checked monitor probe calls = %d, want 0", probeCalls.Load())
	}
}

func TestEngineSweepBoundsCrossMonitorConcurrency(t *testing.T) {
	monitorStore := testEngineStore(t)
	for index := 0; index < 5; index++ {
		createEngineMonitor(t, monitorStore, nil)
	}
	var current atomic.Int32
	var maximum atomic.Int32
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	prober := proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		active := current.Add(1)
		for {
			observed := maximum.Load()
			if active <= observed || maximum.CompareAndSwap(observed, active) {
				break
			}
		}
		started <- struct{}{}
		<-release
		current.Add(-1)
		return store.UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: time.Now().UTC()}
	})
	engine := NewEngineWithDependencies(monitorStore, prober, &recordingUptimeNotifier{})
	engine.semaphore = make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() { done <- engine.Sweep(context.Background()) }()
	for index := 0; index < 2; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for bounded checks")
		}
	}
	select {
	case <-started:
		t.Fatal("more than two probes started before a slot was released")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", maximum.Load())
	}
}

func TestEngineBoundsPersistedDeliveryErrors(t *testing.T) {
	monitorStore := testEngineStore(t)
	monitor := createEngineMonitor(t, monitorStore, func(monitor *store.UptimeMonitor) {
		monitor.FailureThreshold = 1
		monitor.NotificationChannels = []store.NotificationChannel{{Type: "webhook", WebhookURL: "https://hooks.example.com"}}
	})
	notifier := &recordingUptimeNotifier{result: alerting.UptimeDeliveryResult{Error: string(make([]rune, maxDeliveryErrorRunes+100))}}
	engine := NewEngineWithDependencies(monitorStore, proberFunc(func(context.Context, store.UptimeMonitor) store.UptimeProbeResult {
		return store.UptimeProbeResult{Error: "连接失败", CheckedAt: time.Now().UTC()}
	}), notifier)
	if _, err := engine.CheckNow(context.Background(), monitor.ID); err != nil {
		t.Fatalf("check now: %v", err)
	}
	incidents, err := monitorStore.ListIncidents(context.Background(), monitor.ID, 10)
	if err != nil || len(incidents) != 1 {
		t.Fatalf("incidents=%+v err=%v", incidents, err)
	}
	if len([]rune(incidents[0].NotificationError)) != maxDeliveryErrorRunes {
		t.Fatalf("delivery error runes = %d", len([]rune(incidents[0].NotificationError)))
	}
}
