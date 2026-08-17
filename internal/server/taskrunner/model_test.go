package taskrunner

import (
	"errors"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

func validTask() store.ScheduledTask {
	return store.ScheduledTask{
		Name: "Nightly cleanup", ScriptID: 1, CronExpression: "0 2 * * *",
		Timezone: "Asia/Shanghai", Enabled: true, TimeoutSeconds: 60,
		NotificationPolicy:   store.NotificationPolicyFailure,
		NotificationChannels: []store.NotificationChannel{}, NodeIDs: []string{"node-1"},
	}
}

func TestParseCronScheduleUsesFiveFieldsAndSeparateTimezone(t *testing.T) {
	base := time.Date(2026, 7, 26, 17, 30, 0, 0, time.UTC)
	next, err := NextRun("0 2 * * *", "Asia/Shanghai", base)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2026, 7, 26, 18, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next run = %s, want %s", next, want)
	}
	for _, testCase := range []struct {
		expression string
		timezone   string
	}{
		{expression: "@daily", timezone: "UTC"},
		{expression: "0 0 2 * * *", timezone: "UTC"},
		{expression: "CRON_TZ=UTC 0 2 * * *", timezone: "UTC"},
		{expression: "0 2 * * *\n", timezone: "UTC"},
		{expression: "0 2 * * *", timezone: ""},
		{expression: "0 2 * * *", timezone: "Local"},
		{expression: "0 2 * * *", timezone: "Mars/Olympus"},
	} {
		if _, _, err := ParseCronSchedule(testCase.expression, testCase.timezone); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("ParseCronSchedule(%q, %q) error = %v, want ErrInvalid", testCase.expression, testCase.timezone, err)
		}
	}
}

func TestSetNextRunAndValidation(t *testing.T) {
	task := validTask()
	base := time.Date(2026, 7, 26, 17, 30, 0, 0, time.UTC)
	if err := SetNextRun(&task, base); err != nil {
		t.Fatalf("SetNextRun: %v", err)
	}
	if task.NextRunAt == nil || !task.NextRunAt.After(base) || task.NextRunAt.Location() != time.UTC {
		t.Fatalf("next run = %v", task.NextRunAt)
	}
	task.Enabled = false
	if err := SetNextRun(&task, base); err != nil {
		t.Fatalf("SetNextRun disabled: %v", err)
	}
	if task.NextRunAt != nil {
		t.Fatalf("disabled next run = %v, want nil", task.NextRunAt)
	}

	invalid := validTask()
	invalid.CronExpression = "@hourly"
	if err := ValidateScheduledTask(&invalid); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid task error = %v, want ErrInvalid", err)
	}
}

func TestSetNextRunSupportsFutureOnceSchedule(t *testing.T) {
	base := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	runAt := base.Add(time.Hour)
	task := validTask()
	task.ScheduleType = store.ScheduleTypeOnce
	task.CronExpression = ""
	task.RunAt = &runAt
	if err := SetNextRun(&task, base); err != nil {
		t.Fatalf("SetNextRun once: %v", err)
	}
	if task.NextRunAt == nil || !task.NextRunAt.Equal(runAt) {
		t.Fatalf("once next run = %v, want %v", task.NextRunAt, runAt)
	}
	task.Timezone = "Local"
	if err := SetNextRun(&task, base); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid once timezone error = %v, want ErrInvalid", err)
	}
	task.Timezone = "UTC"
	task.RunAt = &base
	if err := SetNextRun(&task, base); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("past once run error = %v, want ErrInvalid", err)
	}
}

func TestValidateScriptAndNotificationChannels(t *testing.T) {
	script := store.AutomationScript{Name: "Cleanup", Content: "echo ok"}
	if err := ValidateScript(&script); err != nil {
		t.Fatalf("ValidateScript: %v", err)
	}
	if err := ValidateScript(&store.AutomationScript{Name: "Empty"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("empty script error = %v, want ErrInvalid", err)
	}
	valid := []store.NotificationChannel{{
		Type: "webhook", WebhookURL: "https://example.com/hook",
		Headers: map[string]string{"X-Source": "mizupanel"},
	}}
	if err := ValidateNotificationChannels(valid); err != nil {
		t.Fatalf("validate channels: %v", err)
	}
	for _, channels := range [][]store.NotificationChannel{
		{{Type: "email", WebhookURL: "https://example.com"}},
		{{Type: "webhook", WebhookURL: "://secret.invalid"}},
		{{Type: "webhook", WebhookURL: "https://example.com", Headers: map[string]string{"X-Test\nInjected": "yes"}}},
	} {
		if err := ValidateNotificationChannels(channels); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("ValidateNotificationChannels(%#v) error = %v, want ErrInvalid", channels, err)
		}
	}
}
