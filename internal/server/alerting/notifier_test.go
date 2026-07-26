package alerting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func responseWithBody(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestSendWebhook(t *testing.T) {
	var receivedPayload AlertPayload
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != "POST" {
			t.Errorf("expected POST, got %s", request.Method)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", request.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(request.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		return responseWithBody(http.StatusOK, ""), nil
	})

	channel := NotificationChannel{
		Type:       "webhook",
		WebhookURL: "http://webhook.invalid/notify",
	}
	payload := AlertPayload{
		RuleName:    "CPU High",
		NodeID:      "node-1",
		NodeName:    "Test Node",
		MetricField: "cpu_usage",
		MetricValue: 85.0,
		Threshold:   80.0,
		Operator:    ">",
		Status:      "triggered",
	}

	err := notifier.Send(context.Background(), channel, payload)
	if err != nil {
		t.Fatalf("send webhook: %v", err)
	}

	if receivedPayload.RuleName != "CPU High" {
		t.Errorf("expected RuleName CPU High, got %s", receivedPayload.RuleName)
	}
	if receivedPayload.MetricValue != 85.0 {
		t.Errorf("expected MetricValue 85.0, got %.2f", receivedPayload.MetricValue)
	}
}

func TestMetricWebhookContractDoesNotGainUptimeFields(t *testing.T) {
	var received map[string]any
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return responseWithBody(http.StatusNoContent, ""), nil
	})
	payload := AlertPayload{
		RuleName: "CPU High", NodeID: "node-1", NodeName: "Node", MetricField: "cpu_usage",
		MetricValue: 90, Threshold: 80, Operator: ">", Status: "triggered",
	}
	if err := notifier.Send(context.Background(), NotificationChannel{Type: "webhook", WebhookURL: "http://webhook.invalid"}, payload); err != nil {
		t.Fatalf("send metric webhook: %v", err)
	}
	wantKeys := []string{"rule_name", "node_id", "node_name", "metric_field", "metric_value", "threshold", "operator", "triggered_at", "status"}
	if len(received) != len(wantKeys) {
		t.Fatalf("metric webhook keys = %#v", received)
	}
	for _, key := range wantKeys {
		if _, ok := received[key]; !ok {
			t.Fatalf("metric webhook missing %q: %#v", key, received)
		}
	}
}

func TestSendUptimeWebhookUsesTypedPayload(t *testing.T) {
	var received UptimePayload
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request method=%s content-type=%s", request.Method, request.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode uptime body: %v", err)
		}
		return responseWithBody(http.StatusNoContent, ""), nil
	})
	triggeredAt := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	payload := UptimePayload{
		MonitorName: "Website", Target: "https://example.com", IncidentKind: store.UptimeIncidentAvailability,
		Status: "triggered", MonitorState: store.UptimeStatusDown, LatencyMS: 5000, Error: "连接超时", TriggeredAt: triggeredAt,
	}
	if err := notifier.SendUptime(context.Background(), NotificationChannel{Type: "webhook", WebhookURL: "http://webhook.invalid"}, payload); err != nil {
		t.Fatalf("send uptime webhook: %v", err)
	}
	if received.MonitorName != payload.MonitorName || received.Target != payload.Target || received.Error != "连接超时" || !received.TriggeredAt.Equal(triggeredAt) {
		t.Fatalf("received uptime payload = %+v", received)
	}
}

func TestSendUptimeDingTalkAndFeishuContent(t *testing.T) {
	for _, channelType := range []string{"dingtalk", "feishu"} {
		t.Run(channelType, func(t *testing.T) {
			var body string
			notifier := NewNotifier()
			notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				encoded, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				body = string(encoded)
				if channelType == "dingtalk" {
					return responseWithBody(http.StatusOK, `{"errcode":0}`), nil
				}
				return responseWithBody(http.StatusOK, `{"code":0}`), nil
			})
			expiresAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
			payload := UptimePayload{
				MonitorName: "TLS Site", Target: "https://example.com", IncidentKind: store.UptimeIncidentCertificate,
				Status: "triggered", MonitorState: store.UptimeStatusWarning, TLSExpiresAt: &expiresAt, TriggeredAt: time.Now().UTC(),
			}
			if err := notifier.SendUptime(context.Background(), NotificationChannel{Type: channelType, WebhookURL: "http://webhook.invalid"}, payload); err != nil {
				t.Fatalf("send %s: %v", channelType, err)
			}
			for _, fragment := range []string{"TLS Site", "https://example.com", "HTTPS", "warning"} {
				if !strings.Contains(body, fragment) {
					t.Fatalf("%s body missing %q: %s", channelType, fragment, body)
				}
			}
		})
	}
}

func TestSendUptimeDingTalkWithSecretPreservesParsedSignature(t *testing.T) {
	const secret = "test-secret-4"
	fixedNow := time.UnixMilli(1785052800000)
	notifier := NewNotifier()
	notifier.now = func() time.Time { return fixedNow }
	expectedSign := notifier.dingTalkSign(fixedNow.UnixMilli(), secret)
	if !strings.Contains(expectedSign, "+") {
		t.Fatalf("test signature %q does not exercise query escaping", expectedSign)
	}
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.URL.Query().Get("sign"); got != expectedSign {
			t.Fatalf("parsed sign = %q, want %q", got, expectedSign)
		}
		return responseWithBody(http.StatusOK, `{"errcode":0}`), nil
	})

	err := notifier.SendUptime(context.Background(), NotificationChannel{
		Type:       "dingtalk",
		WebhookURL: "http://dingtalk.invalid/robot/send?access_token=token",
		Secret:     secret,
	}, UptimePayload{Status: "triggered", TriggeredAt: fixedNow})
	if err != nil {
		t.Fatalf("send uptime dingtalk with secret: %v", err)
	}
}

func TestDeliverUptimeRetriesTransientFailuresAndFansOut(t *testing.T) {
	var retryAttempts atomic.Int32
	var successAttempts atomic.Int32
	notifier := NewNotifier()
	notifier.uptimeRetryDelays = []time.Duration{0, time.Millisecond, time.Millisecond}
	notifier.uptimeAttemptTimeout = time.Second
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "retry.invalid" {
			if retryAttempts.Add(1) < 3 {
				return responseWithBody(http.StatusServiceUnavailable, ""), nil
			}
			return responseWithBody(http.StatusNoContent, ""), nil
		}
		successAttempts.Add(1)
		return responseWithBody(http.StatusNoContent, ""), nil
	})
	result := notifier.DeliverUptime(context.Background(), []store.NotificationChannel{
		{Type: "webhook", WebhookURL: "http://retry.invalid/notify"},
		{Type: "webhook", WebhookURL: "http://success.invalid/notify"},
	}, UptimePayload{Status: "triggered"})
	if !result.Sent || result.Error != "" || result.AttemptedAt.IsZero() {
		t.Fatalf("delivery = %+v", result)
	}
	if retryAttempts.Load() != 3 || successAttempts.Load() != 1 {
		t.Fatalf("retry attempts=%d success attempts=%d", retryAttempts.Load(), successAttempts.Load())
	}
}

func TestDeliverUptimeErrorDoesNotExposeWebhookURL(t *testing.T) {
	const secretURL = "://uptime-secret-token"
	result := NewNotifier().DeliverUptime(context.Background(), []store.NotificationChannel{{Type: "webhook", WebhookURL: secretURL}}, UptimePayload{Status: "triggered"})
	if result.Error == "" {
		t.Fatal("expected delivery error")
	}
	if strings.Contains(result.Error, secretURL) || strings.Contains(result.Error, "secret-token") {
		t.Fatalf("delivery leaked webhook URL: %q", result.Error)
	}
}

func TestSendWebhookWithHeaders(t *testing.T) {
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("X-Custom-Header") != "test-value" {
			t.Errorf("expected X-Custom-Header test-value, got %s", request.Header.Get("X-Custom-Header"))
		}
		return responseWithBody(http.StatusOK, ""), nil
	})

	channel := NotificationChannel{
		Type:       "webhook",
		WebhookURL: "http://webhook.invalid/notify",
		Headers: map[string]string{
			"X-Custom-Header": "test-value",
		},
	}
	payload := AlertPayload{
		RuleName: "Test Rule",
		NodeID:   "node-1",
		Status:   "triggered",
	}

	err := notifier.Send(context.Background(), channel, payload)
	if err != nil {
		t.Fatalf("send webhook: %v", err)
	}
}

func TestSendDingTalk(t *testing.T) {
	var receivedPayload map[string]interface{}
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		return responseWithBody(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`), nil
	})

	channel := NotificationChannel{
		Type:       "dingtalk",
		WebhookURL: "http://dingtalk.invalid/robot/send",
	}
	payload := AlertPayload{
		RuleName:    "Memory High",
		NodeID:      "node-1",
		NodeName:    "Test Node",
		MetricField: "memory_usage",
		MetricValue: 95.0,
		Threshold:   90.0,
		Operator:    ">=",
		Status:      "triggered",
	}

	err := notifier.Send(context.Background(), channel, payload)
	if err != nil {
		t.Fatalf("send dingtalk: %v", err)
	}

	if receivedPayload["msgtype"] != "markdown" {
		t.Errorf("expected msgtype markdown, got %v", receivedPayload["msgtype"])
	}

	markdown, ok := receivedPayload["markdown"].(map[string]interface{})
	if !ok {
		t.Fatal("expected markdown field")
	}
	if markdown["title"] == "" {
		t.Error("expected non-empty title")
	}
}

func TestSendDingTalkWithSecret(t *testing.T) {
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		timestamp := request.URL.Query().Get("timestamp")
		sign := request.URL.Query().Get("sign")
		if timestamp == "" {
			t.Error("expected timestamp query parameter")
		}
		if sign == "" {
			t.Error("expected sign query parameter")
		}
		return responseWithBody(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`), nil
	})

	channel := NotificationChannel{
		Type:       "dingtalk",
		WebhookURL: "http://dingtalk.invalid/robot/send",
		Secret:     "test-secret",
	}
	payload := AlertPayload{
		RuleName: "Test Rule",
		NodeID:   "node-1",
		NodeName: "Test Node",
		Status:   "triggered",
	}

	err := notifier.Send(context.Background(), channel, payload)
	if err != nil {
		t.Fatalf("send dingtalk with secret: %v", err)
	}
}

func TestSendUnsupportedChannel(t *testing.T) {
	notifier := NewNotifier()
	channel := NotificationChannel{
		Type:       "email", // Not implemented yet
		WebhookURL: "https://example.com",
	}
	payload := AlertPayload{
		RuleName: "Test Rule",
		NodeID:   "node-1",
		Status:   "triggered",
	}

	err := notifier.Send(context.Background(), channel, payload)
	if err == nil {
		t.Fatal("expected error for unsupported channel type")
	}
}

func TestDeliverChannelWithRetryRetriesTransientFailure(t *testing.T) {
	var attempts atomic.Int32
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		if attempts.Add(1) < 3 {
			return responseWithBody(http.StatusServiceUnavailable, ""), nil
		}
		return responseWithBody(http.StatusNoContent, ""), nil
	})

	engine := &Engine{
		notifier:                   notifier,
		notificationAttemptTimeout: time.Second,
		notificationRetryDelays:    []time.Duration{0, time.Millisecond, time.Millisecond},
	}
	err := engine.deliverChannelWithRetry(context.Background(), NotificationChannel{Type: "webhook", WebhookURL: "http://webhook.invalid/notify"}, AlertPayload{Status: "triggered"})
	if err != nil {
		t.Fatalf("deliver with retry: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestDeliverChannelWithRetryDoesNotRetryBadRequest(t *testing.T) {
	var attempts atomic.Int32
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		attempts.Add(1)
		return responseWithBody(http.StatusBadRequest, ""), nil
	})

	engine := &Engine{
		notifier:                   notifier,
		notificationAttemptTimeout: time.Second,
		notificationRetryDelays:    []time.Duration{0, time.Millisecond, time.Millisecond},
	}
	err := engine.deliverChannelWithRetry(context.Background(), NotificationChannel{Type: "webhook", WebhookURL: "http://webhook.invalid/notify"}, AlertPayload{Status: "triggered"})
	if err == nil {
		t.Fatal("expected bad request error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestDeliveryErrorsDoNotExposeWebhookURL(t *testing.T) {
	const secretURL = "://webhook-secret-token"
	err := NewNotifier().Send(context.Background(), NotificationChannel{Type: "webhook", WebhookURL: secretURL}, AlertPayload{Status: "triggered"})
	if err == nil {
		t.Fatal("expected invalid webhook error")
	}
	if strings.Contains(err.Error(), secretURL) || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked webhook URL: %q", err)
	}
}

func TestConvertNotificationChannels(t *testing.T) {
	storeChannels := []store.NotificationChannel{
		{
			Type:       "webhook",
			WebhookURL: "https://example.com/webhook",
			Headers:    map[string]string{"X-Token": "secret"},
		},
		{
			Type:       "dingtalk",
			WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=xxx",
			Secret:     "dingtalk-secret",
		},
	}

	channels := convertNotificationChannels(storeChannels)
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}

	if channels[0].Type != "webhook" {
		t.Errorf("expected webhook type, got %s", channels[0].Type)
	}
	if channels[0].Headers["X-Token"] != "secret" {
		t.Error("expected headers to be preserved")
	}

	if channels[1].Type != "dingtalk" {
		t.Errorf("expected dingtalk type, got %s", channels[1].Type)
	}
	if channels[1].Secret != "dingtalk-secret" {
		t.Error("expected secret to be preserved")
	}
}

func convertNotificationChannels(storeChannels []store.NotificationChannel) []NotificationChannel {
	channels := make([]NotificationChannel, len(storeChannels))
	for i, sc := range storeChannels {
		channels[i] = NotificationChannel{
			Type:       sc.Type,
			WebhookURL: sc.WebhookURL,
			Secret:     sc.Secret,
			Headers:    sc.Headers,
		}
	}
	return channels
}
