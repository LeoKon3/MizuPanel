package alerting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestSendTaskWebhookUsesSecretFreeTypedPayload(t *testing.T) {
	var body map[string]any
	notifier := NewNotifier()
	notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return responseWithBody(http.StatusNoContent, ""), nil
	})
	payload := TaskPayload{
		RunID: 9, TaskName: "Cleanup", ScriptName: "Docker cleanup", Trigger: "scheduled", Status: "failed",
		TotalTargets: 2, SuccessfulTargets: 1, FailedTargets: 1, Failures: []TaskTargetSummary{{NodeName: "node-a", Status: "failed"}}, FinishedAt: time.Now().UTC(),
	}
	if err := notifier.SendTask(context.Background(), NotificationChannel{Type: "webhook", WebhookURL: "http://webhook.invalid", Secret: "webhook-secret-marker"}, payload); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(body)
	for _, forbidden := range []string{"webhook-secret-marker", "script", "output", "environment"} {
		if forbidden == "script" {
			continue // script_name is an intentional safe field.
		}
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("payload leaked %q: %s", forbidden, encoded)
		}
	}
	if body["run_id"] != float64(9) || body["status"] != "failed" {
		t.Fatalf("payload = %#v", body)
	}
}

func TestSendTaskDingTalkAndFeishuContainOnlySummary(t *testing.T) {
	for _, channelType := range []string{"dingtalk", "feishu"} {
		t.Run(channelType, func(t *testing.T) {
			var body string
			notifier := NewNotifier()
			notifier.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				encoded, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				body = string(encoded)
				if channelType == "dingtalk" {
					return responseWithBody(http.StatusOK, `{"errcode":0}`), nil
				}
				return responseWithBody(http.StatusOK, `{"code":0}`), nil
			})
			exitCode := 7
			payload := TaskPayload{TaskName: "Cleanup", ScriptName: "Disk cleanup", Trigger: "scheduled", Status: "partial", TotalTargets: 2, SuccessfulTargets: 1, FailedTargets: 1, DurationMS: 42, FinishedAt: time.Now().UTC(), Failures: []TaskTargetSummary{{NodeName: "node-a", Status: "failed", ExitCode: &exitCode}}}
			if err := notifier.SendTask(context.Background(), NotificationChannel{Type: channelType, WebhookURL: "http://webhook.invalid"}, payload); err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{"Cleanup", "Disk cleanup", "node-a", "partial"} {
				if !strings.Contains(body, expected) {
					t.Fatalf("body missing %q: %s", expected, body)
				}
			}
			if strings.Contains(body, "output-secret-marker") {
				t.Fatalf("body leaked output: %s", body)
			}
		})
	}
}

func TestDeliverTaskDoesNotExposeWebhookURL(t *testing.T) {
	const secretURL = "://task-secret-token"
	result := NewNotifier().DeliverTask(context.Background(), []store.NotificationChannel{{Type: "webhook", WebhookURL: secretURL}}, TaskPayload{Status: "failed"})
	if result.Error == "" || strings.Contains(result.Error, secretURL) || strings.Contains(result.Error, "secret-token") {
		t.Fatalf("delivery error = %q", result.Error)
	}
}
