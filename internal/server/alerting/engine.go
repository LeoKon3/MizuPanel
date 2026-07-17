package alerting

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

// AlertState tracks the state of an alert for a specific rule and node
type AlertState struct {
	RuleID       int64
	NodeID       string
	ConditionMet bool
	Triggered    bool  // Whether notification has been sent
	HistoryID    int64 // Alert history record ID
	FirstMetAt   time.Time
	LastChecked  time.Time
}

// Engine manages alert rule evaluation and notification
type Engine struct {
	alerts                     *store.AlertStore
	metrics                    *store.MetricStore
	nodes                      *store.NodeStore
	notifier                   *Notifier
	notificationAttemptTimeout time.Duration
	notificationRetryDelays    []time.Duration
	states                     map[string]*AlertState // key: "ruleID:nodeID"
	mu                         sync.RWMutex
}

type notificationDeliveryResult struct {
	Sent        bool
	Error       string
	AttemptedAt time.Time
}

type channelDeliveryResult struct {
	channelType string
	err         error
}

// NewEngine creates a new alerting engine
func NewEngine(alerts *store.AlertStore, metrics *store.MetricStore, nodes *store.NodeStore) *Engine {
	return &Engine{
		alerts:                     alerts,
		metrics:                    metrics,
		nodes:                      nodes,
		notifier:                   NewNotifier(),
		notificationAttemptTimeout: 5 * time.Second,
		notificationRetryDelays:    []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond},
		states:                     make(map[string]*AlertState),
	}
}

// Initialize loads active alerts from database to restore state after restart
func (e *Engine) Initialize(ctx context.Context) error {
	activeAlerts, err := e.alerts.GetActiveAlertHistory()
	if err != nil {
		return fmt.Errorf("load active alerts: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, alert := range activeAlerts {
		stateKey := fmt.Sprintf("%d:%s", alert.RuleID, alert.NodeID)
		e.states[stateKey] = &AlertState{
			RuleID:       alert.RuleID,
			NodeID:       alert.NodeID,
			ConditionMet: true,
			Triggered:    true,
			HistoryID:    alert.ID,
			FirstMetAt:   alert.TriggeredAt,
			LastChecked:  time.Now(),
		}
	}

	return nil
}

// CheckRules evaluates all enabled alert rules against latest metrics
func (e *Engine) CheckRules(ctx context.Context) error {
	if _, err := e.alerts.ResolveActiveAlertHistoryForDisabledRules(time.Now().UTC()); err != nil {
		return fmt.Errorf("resolve disabled rule alerts: %w", err)
	}
	if err := e.syncStatesWithActiveAlerts(); err != nil {
		return err
	}

	rules, err := e.alerts.GetEnabledAlertRules()
	if err != nil {
		return fmt.Errorf("get enabled rules: %w", err)
	}

	nodes, err := e.nodes.List(ctx)
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}

	for _, rule := range rules {
		for _, node := range nodes {
			// Check if node is in scope
			if !e.nodeInScope(&rule, node.ID) {
				continue
			}

			// Get latest metric for this node
			metric, ok, err := e.metrics.Latest(ctx, node.ID)
			if err != nil {
				return fmt.Errorf("get latest metric for node %s: %w", node.ID, err)
			}
			if !ok {
				continue // No metrics yet for this node
			}

			// Evaluate rule
			triggered := e.evaluateRule(&rule, &metric)
			if triggered {
				// Send notification and create alert history
				if err := e.handleAlert(ctx, &rule, &node, &metric); err != nil {
					return fmt.Errorf("handle alert rule %d for node %s: %w", rule.ID, node.ID, err)
				}
			} else {
				// Check if alert was previously triggered and should be resolved
				if err := e.checkResolution(ctx, &rule, &node, &metric); err != nil {
					return fmt.Errorf("resolve alert rule %d for node %s: %w", rule.ID, node.ID, err)
				}
			}
		}
	}

	return nil
}

func (e *Engine) syncStatesWithActiveAlerts() error {
	activeAlerts, err := e.alerts.GetActiveAlertHistory()
	if err != nil {
		return fmt.Errorf("load active alerts: %w", err)
	}

	activeByKey := make(map[string]store.AlertHistory, len(activeAlerts))
	for _, alert := range activeAlerts {
		activeByKey[fmt.Sprintf("%d:%s", alert.RuleID, alert.NodeID)] = alert
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for stateKey, state := range e.states {
		if !state.Triggered {
			continue
		}
		activeAlert, ok := activeByKey[stateKey]
		if !ok {
			delete(e.states, stateKey)
			continue
		}
		state.HistoryID = activeAlert.ID
	}

	return nil
}

// handleAlert sends notification and creates alert history when rule triggers
func (e *Engine) handleAlert(ctx context.Context, rule *store.AlertRule, node *store.Node, metric *store.Metric) error {
	stateKey := fmt.Sprintf("%d:%s", rule.ID, metric.NodeID)
	e.mu.Lock()
	state := e.states[stateKey]
	e.mu.Unlock()

	// Extract metric value
	metricValue := e.getMetricValue(metric, rule.MetricField)
	floatValue := 0.0
	if metricValue != nil {
		if v, ok := metricValue.(float64); ok {
			floatValue = v
		} else if v, ok := metricValue.(int64); ok {
			floatValue = float64(v)
		}
	}

	// If alert already triggered, update metric value and return
	if state != nil && state.Triggered {
		if state.HistoryID > 0 {
			return e.alerts.UpdateAlertHistoryMetricValue(state.HistoryID, floatValue)
		}
		return nil
	}

	// Create alert history
	history := &store.AlertHistory{
		RuleID:           rule.ID,
		RuleName:         rule.Name,
		NodeID:           node.ID,
		NodeName:         node.Name,
		MetricField:      rule.MetricField,
		MetricValue:      floatValue,
		Threshold:        rule.Threshold,
		TriggeredAt:      time.Now().UTC(),
		NotificationSent: false,
	}

	if err := e.alerts.CreateAlertHistory(history); err != nil {
		return fmt.Errorf("create alert history: %w", err)
	}

	// Send notifications
	payload := AlertPayload{
		RuleName:    rule.Name,
		NodeID:      node.ID,
		NodeName:    node.Name,
		MetricField: rule.MetricField,
		MetricValue: floatValue,
		Threshold:   rule.Threshold,
		Operator:    rule.Operator,
		TriggeredAt: history.TriggeredAt,
		Status:      "triggered",
	}

	delivery := e.deliverNotifications(ctx, rule.NotificationChannels, payload)
	history.NotificationSent = delivery.Sent
	history.NotificationError = delivery.Error
	history.NotificationAttemptedAt = &delivery.AttemptedAt
	updateErr := e.alerts.UpdateAlertHistoryNotificationResult(history.ID, delivery.Sent, delivery.Error, delivery.AttemptedAt)

	// Mark as triggered in state
	e.mu.Lock()
	if e.states[stateKey] != nil {
		e.states[stateKey].Triggered = true
		e.states[stateKey].HistoryID = history.ID
	}
	e.mu.Unlock()

	if updateErr != nil {
		return fmt.Errorf("persist trigger notification result: %w", updateErr)
	}
	return nil
}

// checkResolution checks if a previously triggered alert should be resolved
func (e *Engine) checkResolution(ctx context.Context, rule *store.AlertRule, node *store.Node, metric *store.Metric) error {
	stateKey := fmt.Sprintf("%d:%s", rule.ID, node.ID)
	e.mu.Lock()
	state := e.states[stateKey]
	e.mu.Unlock()

	// If alert was triggered but condition is no longer met, mark as resolved
	if state != nil && state.Triggered && state.HistoryID > 0 {
		history, err := e.alerts.GetAlertHistoryByID(state.HistoryID)
		if err != nil {
			return fmt.Errorf("load alert history: %w", err)
		}
		if history == nil {
			return fmt.Errorf("alert history %d not found", state.HistoryID)
		}

		resolvedAt := time.Now().UTC()
		if err := e.alerts.UpdateAlertHistoryResolved(state.HistoryID, resolvedAt); err != nil {
			return fmt.Errorf("mark alert resolved: %w", err)
		}

		metricValue := e.getMetricValue(metric, rule.MetricField)
		floatValue := 0.0
		if value, ok := metricValue.(float64); ok {
			floatValue = value
		} else if value, ok := metricValue.(int64); ok {
			floatValue = float64(value)
		}
		payload := AlertPayload{
			RuleName:    history.RuleName,
			NodeID:      node.ID,
			NodeName:    node.Name,
			MetricField: rule.MetricField,
			MetricValue: floatValue,
			Threshold:   rule.Threshold,
			Operator:    rule.Operator,
			TriggeredAt: history.TriggeredAt,
			ResolvedAt:  &resolvedAt,
			Status:      "resolved",
		}
		delivery := e.deliverNotifications(ctx, rule.NotificationChannels, payload)
		updateErr := e.alerts.UpdateAlertHistoryRecoveryNotificationResult(state.HistoryID, delivery.Sent, delivery.Error, delivery.AttemptedAt)

		// Clear state
		e.mu.Lock()
		delete(e.states, stateKey)
		e.mu.Unlock()

		if updateErr != nil {
			return fmt.Errorf("persist recovery notification result: %w", updateErr)
		}
	}
	return nil
}

func (e *Engine) deliverNotifications(ctx context.Context, channels []store.NotificationChannel, payload AlertPayload) notificationDeliveryResult {
	attemptedAt := time.Now().UTC()
	if len(channels) == 0 {
		return notificationDeliveryResult{AttemptedAt: attemptedAt}
	}

	results := make([]channelDeliveryResult, len(channels))
	var wg sync.WaitGroup
	for index, channel := range channels {
		index := index
		channel := channel
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index] = channelDeliveryResult{
				channelType: channel.Type,
				err: e.deliverChannelWithRetry(ctx, NotificationChannel{
					Type:       channel.Type,
					WebhookURL: channel.WebhookURL,
					Secret:     channel.Secret,
					Headers:    channel.Headers,
				}, payload),
			}
		}()
	}
	wg.Wait()

	errorsByChannel := make([]string, 0)
	for _, result := range results {
		if result.err != nil {
			errorsByChannel = append(errorsByChannel, fmt.Sprintf("%s: %s", result.channelType, result.err.Error()))
		}
	}
	return notificationDeliveryResult{
		Sent:        len(errorsByChannel) == 0,
		Error:       strings.Join(errorsByChannel, "; "),
		AttemptedAt: time.Now().UTC(),
	}
}

func (e *Engine) deliverChannelWithRetry(ctx context.Context, channel NotificationChannel, payload AlertPayload) error {
	var lastErr error
	for attempt, delay := range e.notificationRetryDelays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return newDeliveryError(false, "request canceled")
			case <-timer.C:
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, e.notificationAttemptTimeout)
		lastErr = e.notifier.Send(attemptCtx, channel, payload)
		cancel()
		if lastErr == nil {
			return nil
		}
		if !isRetryableDeliveryError(lastErr) || attempt == len(e.notificationRetryDelays)-1 {
			return lastErr
		}
	}
	return lastErr
}

// evaluateRule evaluates a single rule against a metric
func (e *Engine) evaluateRule(rule *store.AlertRule, metric *store.Metric) bool {
	// Check if node is in scope first
	if !e.nodeInScope(rule, metric.NodeID) {
		return false
	}

	// Check if threshold is met
	thresholdMet := e.checkThreshold(rule, metric)

	stateKey := fmt.Sprintf("%d:%s", rule.ID, metric.NodeID)
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UTC()
	state, exists := e.states[stateKey]

	if !thresholdMet {
		// Clear pending duration-only state. Triggered alerts are resolved by checkResolution.
		if exists && !state.Triggered {
			delete(e.states, stateKey)
		}
		return false
	}

	// Threshold is met
	if !exists {
		// First time condition met - record state
		e.states[stateKey] = &AlertState{
			RuleID:       rule.ID,
			NodeID:       metric.NodeID,
			ConditionMet: true,
			FirstMetAt:   now,
			LastChecked:  now,
		}
		// If no duration requirement, trigger immediately
		if rule.DurationSeconds == 0 {
			return true
		}
		return false
	}

	// Condition was already met - check if duration elapsed
	state.LastChecked = now
	if rule.DurationSeconds == 0 {
		return true
	}

	elapsed := now.Sub(state.FirstMetAt)
	return elapsed >= time.Duration(rule.DurationSeconds)*time.Second
}

// checkThreshold checks if the metric value meets the rule threshold
func (e *Engine) checkThreshold(rule *store.AlertRule, metric *store.Metric) bool {
	value := e.getMetricValue(metric, rule.MetricField)
	if value == nil {
		return false
	}

	floatValue, ok := value.(float64)
	if !ok {
		// Try converting int64 to float64
		if intValue, ok := value.(int64); ok {
			floatValue = float64(intValue)
		} else {
			return false
		}
	}

	switch rule.Operator {
	case ">":
		return floatValue > rule.Threshold
	case ">=":
		return floatValue >= rule.Threshold
	case "<":
		return floatValue < rule.Threshold
	case "<=":
		return floatValue <= rule.Threshold
	case "=":
		return floatValue == rule.Threshold
	default:
		return false
	}
}

// getMetricValue extracts a specific field value from a metric
func (e *Engine) getMetricValue(metric *store.Metric, field string) interface{} {
	v := reflect.ValueOf(metric)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	fieldMap := map[string]string{
		"cpu_usage":        "CPUUsage",
		"memory_usage":     "MemoryUsage",
		"disk_usage":       "DiskUsage",
		"swap_usage":       "SwapUsage",
		"network_rx_bytes": "RXTotal",
		"network_tx_bytes": "TXTotal",
		"load_1":           "Load1",
		"load_5":           "Load5",
		"load_15":          "Load15",
	}

	structField, ok := fieldMap[field]
	if !ok {
		return nil
	}

	fieldValue := v.FieldByName(structField)
	if !fieldValue.IsValid() {
		return nil
	}

	return fieldValue.Interface()
}

// nodeInScope checks if a node is in the scope of a rule
func (e *Engine) nodeInScope(rule *store.AlertRule, nodeID string) bool {
	if rule.ScopeType == "all" {
		return true
	}

	if rule.ScopeType == "nodes" {
		for _, id := range rule.ScopeNodeIDs {
			if id == nodeID {
				return true
			}
		}
		return false
	}

	return false
}

// getAlertState retrieves the alert state for a rule and node (for testing)
func (e *Engine) getAlertState(ruleID int64, nodeID string) *AlertState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	key := fmt.Sprintf("%d:%s", ruleID, nodeID)
	return e.states[key]
}
