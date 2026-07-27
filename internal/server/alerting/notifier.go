package alerting

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

// Notifier sends alert notifications through various channels
type Notifier struct {
	client               *http.Client
	uptimeAttemptTimeout time.Duration
	uptimeRetryDelays    []time.Duration
	now                  func() time.Time
}

// NewNotifier creates a new notification sender
func NewNotifier() *Notifier {
	return &Notifier{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		uptimeAttemptTimeout: 5 * time.Second,
		uptimeRetryDelays:    []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond},
		now:                  time.Now,
	}
}

// NotificationChannel represents a notification destination
type NotificationChannel struct {
	Type       string
	WebhookURL string
	Secret     string
	Headers    map[string]string
}

// AlertPayload contains information about an alert
type AlertPayload struct {
	RuleName    string     `json:"rule_name"`
	NodeID      string     `json:"node_id"`
	NodeName    string     `json:"node_name"`
	MetricField string     `json:"metric_field"`
	MetricValue float64    `json:"metric_value"`
	Threshold   float64    `json:"threshold"`
	Operator    string     `json:"operator"`
	TriggeredAt time.Time  `json:"triggered_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	Status      string     `json:"status"` // "triggered" or "resolved"
}

// UptimePayload contains bounded operational details for an Uptime incident.
// It intentionally excludes response bodies, request headers, and notification
// credentials.
type UptimePayload struct {
	MonitorName  string     `json:"monitor_name"`
	Target       string     `json:"target"`
	IncidentKind string     `json:"incident_kind"`
	Status       string     `json:"status"` // "triggered" or "resolved"
	MonitorState string     `json:"monitor_state"`
	LatencyMS    int64      `json:"latency_ms"`
	StatusCode   int        `json:"status_code,omitempty"`
	Error        string     `json:"error,omitempty"`
	TLSExpiresAt *time.Time `json:"tls_expires_at,omitempty"`
	TriggeredAt  time.Time  `json:"triggered_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
}

// UptimeDeliveryResult is persisted on the corresponding Uptime incident.
type UptimeDeliveryResult struct {
	Sent        bool
	Error       string
	AttemptedAt time.Time
}

type deliveryError struct {
	message   string
	retryable bool
}

func (e *deliveryError) Error() string {
	return e.message
}

func newDeliveryError(retryable bool, format string, args ...interface{}) error {
	return &deliveryError{message: fmt.Sprintf(format, args...), retryable: retryable}
}

func isRetryableDeliveryError(err error) bool {
	var deliveryErr *deliveryError
	return errors.As(err, &deliveryErr) && deliveryErr.retryable
}

func requestDeliveryError(err error) error {
	if errors.Is(err, context.Canceled) {
		return newDeliveryError(false, "request canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newDeliveryError(true, "request timed out")
	}
	return newDeliveryError(true, "request failed")
}

func statusDeliveryError(channel string, status int) error {
	retryable := status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= 500
	return newDeliveryError(retryable, "%s returned HTTP %d", channel, status)
}

// Send sends a notification through the specified channel
func (n *Notifier) Send(ctx context.Context, channel NotificationChannel, payload AlertPayload) error {
	switch channel.Type {
	case "webhook":
		return n.sendWebhook(ctx, channel, payload)
	case "dingtalk":
		return n.sendDingTalk(ctx, channel, payload)
	case "feishu":
		return n.sendFeishu(ctx, channel, payload)
	default:
		return newDeliveryError(false, "unsupported channel type %s", channel.Type)
	}
}

// SendUptime sends one Uptime notification through the specified channel.
func (n *Notifier) SendUptime(ctx context.Context, channel NotificationChannel, payload UptimePayload) error {
	switch channel.Type {
	case "webhook":
		return n.sendUptimeWebhook(ctx, channel, payload)
	case "dingtalk":
		return n.sendUptimeDingTalk(ctx, channel, payload)
	case "feishu":
		return n.sendUptimeFeishu(ctx, channel, payload)
	default:
		return newDeliveryError(false, "unsupported channel type %s", channel.Type)
	}
}

// DeliverUptime fans out concurrently and applies the same bounded retry
// policy used by metric alert notifications.
func (n *Notifier) DeliverUptime(ctx context.Context, channels []store.NotificationChannel, payload UptimePayload) UptimeDeliveryResult {
	return n.deliverChannels(ctx, channels, func(sendCtx context.Context, channel NotificationChannel) error {
		return n.SendUptime(sendCtx, channel, payload)
	})
}

func (n *Notifier) deliverChannels(ctx context.Context, channels []store.NotificationChannel, send func(context.Context, NotificationChannel) error) UptimeDeliveryResult {
	attemptedAt := n.currentTime().UTC()
	if len(channels) == 0 {
		return UptimeDeliveryResult{AttemptedAt: attemptedAt}
	}

	results := make([]channelDeliveryResult, len(channels))
	var waitGroup sync.WaitGroup
	for index, storedChannel := range channels {
		index := index
		channel := NotificationChannel{
			Type:       storedChannel.Type,
			WebhookURL: storedChannel.WebhookURL,
			Secret:     storedChannel.Secret,
			Headers:    storedChannel.Headers,
		}
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			results[index] = channelDeliveryResult{
				channelType: channel.Type,
				err:         n.deliverChannelWithRetry(ctx, channel, send),
			}
		}()
	}
	waitGroup.Wait()

	errorsByChannel := make([]string, 0)
	for _, result := range results {
		if result.err != nil {
			errorsByChannel = append(errorsByChannel, fmt.Sprintf("%s: %s", result.channelType, result.err.Error()))
		}
	}
	return UptimeDeliveryResult{
		Sent:        len(errorsByChannel) == 0,
		Error:       strings.Join(errorsByChannel, "; "),
		AttemptedAt: n.currentTime().UTC(),
	}
}

func (n *Notifier) deliverChannelWithRetry(ctx context.Context, channel NotificationChannel, send func(context.Context, NotificationChannel) error) error {
	delays := n.uptimeRetryDelays
	if len(delays) == 0 {
		delays = []time.Duration{0}
	}
	attemptTimeout := n.uptimeAttemptTimeout
	if attemptTimeout <= 0 {
		attemptTimeout = 5 * time.Second
	}
	var lastErr error
	for attempt, delay := range delays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return newDeliveryError(false, "request canceled")
			case <-timer.C:
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		lastErr = send(attemptCtx, channel)
		cancel()
		if lastErr == nil {
			return nil
		}
		if !isRetryableDeliveryError(lastErr) || attempt == len(delays)-1 {
			return lastErr
		}
	}
	return lastErr
}

func (n *Notifier) currentTime() time.Time {
	if n.now != nil {
		return n.now()
	}
	return time.Now()
}

func (n *Notifier) sendUptimeWebhook(ctx context.Context, channel NotificationChannel, payload UptimePayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return newDeliveryError(false, "encode webhook payload failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid webhook configuration")
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range channel.Headers {
		req.Header.Set(key, value)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusDeliveryError("webhook", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendUptimeDingTalk(ctx context.Context, channel NotificationChannel, payload UptimePayload) error {
	statusEmoji, statusText := uptimeStatusText(payload.Status)
	markdown := fmt.Sprintf(`### %s %s：%s

- **类型**: %s
- **目标**: %s
- **当前状态**: %s
- **响应时间**: %d ms
- **发生时间**: %s`,
		statusEmoji, statusText, payload.MonitorName, uptimeKindText(payload.IncidentKind), payload.Target,
		payload.MonitorState, payload.LatencyMS, payload.TriggeredAt.Format("2006-01-02 15:04:05"))
	if payload.StatusCode > 0 {
		markdown += fmt.Sprintf("\n- **HTTP 状态码**: %d", payload.StatusCode)
	}
	if payload.Error != "" {
		markdown += fmt.Sprintf("\n- **错误**: %s", payload.Error)
	}
	if payload.TLSExpiresAt != nil {
		markdown += fmt.Sprintf("\n- **证书到期时间**: %s", payload.TLSExpiresAt.Format("2006-01-02 15:04:05"))
	}
	if payload.ResolvedAt != nil {
		markdown += fmt.Sprintf("\n- **恢复时间**: %s", payload.ResolvedAt.Format("2006-01-02 15:04:05"))
	}

	dingPayload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": fmt.Sprintf("%s: %s", statusText, payload.MonitorName),
			"text":  markdown,
		},
	}
	webhookURL := channel.WebhookURL
	if channel.Secret != "" {
		timestamp := n.currentTime().UnixMilli()
		sign := n.dingTalkSign(timestamp, channel.Secret)
		separator := "?"
		if strings.Contains(webhookURL, "?") {
			separator = "&"
		}
		webhookURL = fmt.Sprintf("%s%stimestamp=%d&sign=%s", webhookURL, separator, timestamp, url.QueryEscape(sign))
	}
	body, err := json.Marshal(dingPayload)
	if err != nil {
		return newDeliveryError(false, "encode dingtalk payload failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid dingtalk configuration")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusDeliveryError("dingtalk", resp.StatusCode)
	}
	var result struct {
		ErrCode int `json:"errcode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.ErrCode != 0 {
		return newDeliveryError(false, "dingtalk rejected request (code %d)", result.ErrCode)
	}
	return nil
}

func (n *Notifier) sendUptimeFeishu(ctx context.Context, channel NotificationChannel, payload UptimePayload) error {
	statusEmoji, statusText := uptimeStatusText(payload.Status)
	content := fmt.Sprintf("**类型**: %s\n**目标**: %s\n**当前状态**: %s\n**响应时间**: %d ms\n**发生时间**: %s",
		uptimeKindText(payload.IncidentKind), payload.Target, payload.MonitorState, payload.LatencyMS,
		payload.TriggeredAt.Format("2006-01-02 15:04:05"))
	if payload.StatusCode > 0 {
		content += fmt.Sprintf("\n**HTTP 状态码**: %d", payload.StatusCode)
	}
	if payload.Error != "" {
		content += fmt.Sprintf("\n**错误**: %s", payload.Error)
	}
	if payload.TLSExpiresAt != nil {
		content += fmt.Sprintf("\n**证书到期时间**: %s", payload.TLSExpiresAt.Format("2006-01-02 15:04:05"))
	}
	if payload.ResolvedAt != nil {
		content += fmt.Sprintf("\n**恢复时间**: %s", payload.ResolvedAt.Format("2006-01-02 15:04:05"))
	}
	feishuPayload := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title": map[string]interface{}{"tag": "plain_text", "content": fmt.Sprintf("%s %s - %s", statusEmoji, statusText, payload.MonitorName)},
				"template": func() string {
					if payload.Status == "resolved" {
						return "green"
					}
					if payload.IncidentKind == store.UptimeIncidentCertificate {
						return "orange"
					}
					return "red"
				}(),
			},
			"elements": []map[string]interface{}{{"tag": "markdown", "content": content}},
		},
	}
	if channel.Secret != "" {
		timestamp := n.currentTime().Unix()
		feishuPayload["timestamp"] = fmt.Sprintf("%d", timestamp)
		feishuPayload["sign"] = n.feishuSign(timestamp, channel.Secret)
	}
	body, err := json.Marshal(feishuPayload)
	if err != nil {
		return newDeliveryError(false, "encode feishu payload failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid feishu configuration")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusDeliveryError("feishu", resp.StatusCode)
	}
	var result struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Code != 0 {
		return newDeliveryError(false, "feishu rejected request (code %d)", result.Code)
	}
	return nil
}

func uptimeStatusText(status string) (string, string) {
	if status == "resolved" {
		return "✅", "服务拨测恢复"
	}
	return "🔴", "服务拨测告警"
}

func uptimeKindText(kind string) string {
	if kind == store.UptimeIncidentCertificate {
		return "HTTPS 证书"
	}
	return "服务可用性"
}

// sendWebhook sends a generic webhook notification
func (n *Notifier) sendWebhook(ctx context.Context, channel NotificationChannel, payload AlertPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return newDeliveryError(false, "encode webhook payload failed")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", channel.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid webhook configuration")
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range channel.Headers {
		req.Header.Set(key, value)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusDeliveryError("webhook", resp.StatusCode)
	}

	return nil
}

// sendDingTalk sends a notification to DingTalk robot
func (n *Notifier) sendDingTalk(ctx context.Context, channel NotificationChannel, payload AlertPayload) error {
	// Build markdown message
	statusEmoji := "🔴"
	statusText := "告警触发"
	if payload.Status == "resolved" {
		statusEmoji = "✅"
		statusText = "告警恢复"
	}

	markdown := fmt.Sprintf(`### %s %s：%s

- **节点**: %s (%s)
- **指标**: %s
- **当前值**: %.2f
- **阈值**: %s %.2f
- **触发时间**: %s`,
		statusEmoji,
		statusText,
		payload.RuleName,
		payload.NodeName,
		payload.NodeID,
		payload.MetricField,
		payload.MetricValue,
		payload.Operator,
		payload.Threshold,
		payload.TriggeredAt.Format("2006-01-02 15:04:05"),
	)
	if payload.ResolvedAt != nil {
		markdown += fmt.Sprintf("\n- **恢复时间**: %s", payload.ResolvedAt.Format("2006-01-02 15:04:05"))
	}

	dingPayload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": fmt.Sprintf("%s: %s", statusText, payload.RuleName),
			"text":  markdown,
		},
	}

	// Add signature if secret is provided
	webhookURL := channel.WebhookURL
	if channel.Secret != "" {
		timestamp := time.Now().UnixMilli()
		sign := n.dingTalkSign(timestamp, channel.Secret)
		// Check if URL already has query parameters
		separator := "?"
		if strings.Contains(webhookURL, "?") {
			separator = "&"
		}
		webhookURL = fmt.Sprintf("%s%stimestamp=%d&sign=%s", channel.WebhookURL, separator, timestamp, sign)
	}

	body, err := json.Marshal(dingPayload)
	if err != nil {
		return newDeliveryError(false, "encode dingtalk payload failed")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid dingtalk configuration")
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusDeliveryError("dingtalk", resp.StatusCode)
	}

	// Check response body for DingTalk-specific errors
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		if result.ErrCode != 0 {
			return newDeliveryError(false, "dingtalk rejected request (code %d)", result.ErrCode)
		}
	}

	return nil
}

// dingTalkSign calculates the signature for DingTalk webhook
func (n *Notifier) dingTalkSign(timestamp int64, secret string) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// sendFeishu sends a notification to Feishu (Lark) robot
func (n *Notifier) sendFeishu(ctx context.Context, channel NotificationChannel, payload AlertPayload) error {
	// Build markdown message
	statusEmoji := "🔴"
	statusText := "告警触发"
	if payload.Status == "resolved" {
		statusEmoji = "✅"
		statusText = "告警解除"
	}

	// Feishu uses card format for rich messages
	content := fmt.Sprintf(`**节点**: %s (%s)
**指标**: %s
**当前值**: %.2f
**阈值**: %s %.2f
**触发时间**: %s`,
		payload.NodeName,
		payload.NodeID,
		payload.MetricField,
		payload.MetricValue,
		payload.Operator,
		payload.Threshold,
		payload.TriggeredAt.Format("2006-01-02 15:04:05"),
	)
	if payload.ResolvedAt != nil {
		content += fmt.Sprintf("\n**恢复时间**: %s", payload.ResolvedAt.Format("2006-01-02 15:04:05"))
	}

	feishuPayload := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title": map[string]interface{}{
					"tag":     "plain_text",
					"content": fmt.Sprintf("%s %s - %s", statusEmoji, statusText, payload.RuleName),
				},
				"template": func() string {
					if payload.Status == "resolved" {
						return "green"
					}
					return "red"
				}(),
			},
			"elements": []map[string]interface{}{
				{
					"tag":     "markdown",
					"content": content,
				},
			},
		},
	}

	// Add signature if secret is provided
	webhookURL := channel.WebhookURL
	if channel.Secret != "" {
		timestamp := time.Now().Unix()
		sign := n.feishuSign(timestamp, channel.Secret)
		feishuPayload["timestamp"] = fmt.Sprintf("%d", timestamp)
		feishuPayload["sign"] = sign
	}

	body, err := json.Marshal(feishuPayload)
	if err != nil {
		return newDeliveryError(false, "encode feishu payload failed")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return newDeliveryError(false, "invalid feishu configuration")
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return requestDeliveryError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusDeliveryError("feishu", resp.StatusCode)
	}

	// Check response body for Feishu-specific errors
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		if result.Code != 0 {
			return newDeliveryError(false, "feishu rejected request (code %d)", result.Code)
		}
	}

	return nil
}

// feishuSign calculates the signature for Feishu webhook
func (n *Notifier) feishuSign(timestamp int64, secret string) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(stringToSign))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
