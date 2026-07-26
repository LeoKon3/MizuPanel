package uptime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/alerting"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

const (
	DefaultSweepInterval  = 5 * time.Second
	DefaultMaxConcurrent  = 8
	maxDeliveryErrorRunes = 1024
	deliveryTimeout       = 20 * time.Second
)

var (
	ErrCheckInProgress = errors.New("uptime check already in progress")
	ErrMonitorDisabled = errors.New("uptime monitor is disabled")
	ErrMonitorNotFound = errors.New("uptime monitor not found")
)

type MonitorStore interface {
	GetMonitor(ctx context.Context, id int64) (*store.UptimeMonitor, error)
	ListEnabledMonitors(ctx context.Context) ([]store.UptimeMonitor, error)
	ApplyProbe(ctx context.Context, monitorID int64, probe store.UptimeProbeResult) (store.UptimeTransition, error)
	UpdateIncidentNotification(ctx context.Context, id int64, recovery bool, sent bool, message string, attemptedAt time.Time) error
}

type UptimeNotifier interface {
	DeliverUptime(ctx context.Context, channels []store.NotificationChannel, payload alerting.UptimePayload) alerting.UptimeDeliveryResult
}

type Engine struct {
	monitors      MonitorStore
	prober        Prober
	notifier      UptimeNotifier
	sweepInterval time.Duration
	semaphore     chan struct{}
	now           func() time.Time

	activeMu sync.Mutex
	active   map[int64]struct{}
}

func NewEngine(monitors MonitorStore) *Engine {
	return NewEngineWithDependencies(monitors, NewNetworkProber(), alerting.NewNotifier())
}

func NewEngineWithDependencies(monitors MonitorStore, prober Prober, notifier UptimeNotifier) *Engine {
	return &Engine{
		monitors:      monitors,
		prober:        prober,
		notifier:      notifier,
		sweepInterval: DefaultSweepInterval,
		semaphore:     make(chan struct{}, DefaultMaxConcurrent),
		now:           time.Now,
		active:        make(map[int64]struct{}),
	}
}

func (e *Engine) SetSweepInterval(interval time.Duration) {
	if interval > 0 {
		e.sweepInterval = interval
	}
}

// Run performs an immediate due sweep and then continues on a fixed ticker.
// Individual sweep failures are logged and do not stop future checks.
func (e *Engine) Run(ctx context.Context) {
	e.runSweep(ctx)
	interval := e.sweepInterval
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.runSweep(ctx)
		}
	}
}

func (e *Engine) runSweep(ctx context.Context) {
	if err := e.Sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("uptime monitor sweep failed: %v", err)
	}
}

// Sweep synchronously checks all enabled monitors that are currently due.
func (e *Engine) Sweep(ctx context.Context) error {
	monitors, err := e.monitors.ListEnabledMonitors(ctx)
	if err != nil {
		return fmt.Errorf("list enabled uptime monitors: %w", err)
	}
	now := e.currentTime().UTC()
	var waitGroup sync.WaitGroup
	var errorsMu sync.Mutex
	checkErrors := make([]error, 0)
	for index := range monitors {
		monitor := monitors[index]
		if !monitorDue(monitor, now) {
			continue
		}
		if !e.beginCheck(monitor.ID) {
			continue
		}
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			defer e.endCheck(monitor.ID)
			if err := e.acquireSlot(ctx); err != nil {
				errorsMu.Lock()
				checkErrors = append(checkErrors, err)
				errorsMu.Unlock()
				return
			}
			defer e.releaseSlot()
			current, err := e.monitors.GetMonitor(ctx, monitor.ID)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && current == nil) {
				return
			}
			if err != nil {
				errorsMu.Lock()
				checkErrors = append(checkErrors, fmt.Errorf("reload uptime monitor %d: %w", monitor.ID, err))
				errorsMu.Unlock()
				return
			}
			if !monitorDue(*current, e.currentTime().UTC()) {
				return
			}
			if _, err := e.checkMonitor(ctx, *current); err != nil {
				errorsMu.Lock()
				checkErrors = append(checkErrors, fmt.Errorf("check uptime monitor %d: %w", monitor.ID, err))
				errorsMu.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	return errors.Join(checkErrors...)
}

// CheckNow runs one synchronous check and rejects overlap with scheduler or
// another manual request for the same monitor.
func (e *Engine) CheckNow(ctx context.Context, monitorID int64) (store.UptimeMonitor, error) {
	if !e.beginCheck(monitorID) {
		return store.UptimeMonitor{}, ErrCheckInProgress
	}
	defer e.endCheck(monitorID)
	monitor, err := e.monitors.GetMonitor(ctx, monitorID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && monitor == nil) {
		return store.UptimeMonitor{}, ErrMonitorNotFound
	}
	if err != nil {
		return store.UptimeMonitor{}, fmt.Errorf("load uptime monitor: %w", err)
	}
	if !monitor.Enabled {
		return store.UptimeMonitor{}, ErrMonitorDisabled
	}
	if err := e.acquireSlot(ctx); err != nil {
		return store.UptimeMonitor{}, err
	}
	defer e.releaseSlot()
	return e.checkMonitor(ctx, *monitor)
}

func (e *Engine) checkMonitor(ctx context.Context, monitor store.UptimeMonitor) (store.UptimeMonitor, error) {
	probe := e.prober.Probe(ctx, monitor)
	transition, err := e.monitors.ApplyProbe(ctx, monitor.ID, probe)
	if errors.Is(err, sql.ErrNoRows) {
		return store.UptimeMonitor{}, ErrMonitorNotFound
	}
	if err != nil {
		return store.UptimeMonitor{}, fmt.Errorf("persist uptime probe: %w", err)
	}
	if err := e.deliverTransitions(context.WithoutCancel(ctx), transition); err != nil {
		return transition.Monitor, err
	}
	return transition.Monitor, nil
}

// BeginMonitorMutation serializes configuration changes with checks for the
// same monitor. The caller must invoke the returned release function.
func (e *Engine) BeginMonitorMutation(monitorID int64) (func(), error) {
	if !e.beginCheck(monitorID) {
		return nil, ErrCheckInProgress
	}
	var once sync.Once
	return func() {
		once.Do(func() { e.endCheck(monitorID) })
	}, nil
}

func (e *Engine) deliverTransitions(ctx context.Context, transition store.UptimeTransition) error {
	for _, incident := range transition.Triggered {
		if err := e.deliverIncident(ctx, transition, incident, false); err != nil {
			return err
		}
	}
	for _, incident := range transition.Resolved {
		if err := e.deliverIncident(ctx, transition, incident, true); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) deliverIncident(ctx context.Context, transition store.UptimeTransition, incident store.UptimeIncident, recovery bool) error {
	deliveryCtx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	payload := uptimePayload(transition, incident, recovery)
	delivery := e.notifier.DeliverUptime(deliveryCtx, transition.Monitor.NotificationChannels, payload)
	if err := e.monitors.UpdateIncidentNotification(deliveryCtx, incident.ID, recovery, delivery.Sent, boundedDeliveryError(delivery.Error), delivery.AttemptedAt); err != nil {
		kind := "trigger"
		if recovery {
			kind = "recovery"
		}
		return fmt.Errorf("persist uptime %s delivery: %w", kind, err)
	}
	return nil
}

func uptimePayload(transition store.UptimeTransition, incident store.UptimeIncident, recovery bool) alerting.UptimePayload {
	status := "triggered"
	if recovery {
		status = "resolved"
	}
	return alerting.UptimePayload{
		MonitorName:  transition.Monitor.Name,
		Target:       transition.Monitor.Target,
		IncidentKind: incident.Kind,
		Status:       status,
		MonitorState: transition.Monitor.Status,
		LatencyMS:    transition.Result.LatencyMS,
		StatusCode:   transition.Result.StatusCode,
		Error:        transition.Result.Error,
		TLSExpiresAt: transition.Result.TLSExpiresAt,
		TriggeredAt:  incident.StartedAt,
		ResolvedAt:   incident.ResolvedAt,
	}
}

func monitorDue(monitor store.UptimeMonitor, now time.Time) bool {
	if !monitor.Enabled || monitor.LastCheckedAt == nil {
		return monitor.Enabled
	}
	return !now.Before(monitor.LastCheckedAt.Add(time.Duration(monitor.IntervalSeconds) * time.Second))
}

func (e *Engine) beginCheck(id int64) bool {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	if _, exists := e.active[id]; exists {
		return false
	}
	e.active[id] = struct{}{}
	return true
}

func (e *Engine) endCheck(id int64) {
	e.activeMu.Lock()
	delete(e.active, id)
	e.activeMu.Unlock()
}

func (e *Engine) acquireSlot(ctx context.Context) error {
	select {
	case e.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) releaseSlot() {
	<-e.semaphore
}

func (e *Engine) currentTime() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

func boundedDeliveryError(value string) string {
	runes := []rune(value)
	if len(runes) <= maxDeliveryErrorRunes {
		return value
	}
	return string(runes[:maxDeliveryErrorRunes])
}
