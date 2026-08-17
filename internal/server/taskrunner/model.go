package taskrunner

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	_ "time/tzdata" // Keep IANA zones available in minimal container images.

	"github.com/mizupanel/mizupanel/internal/server/store"
	"github.com/robfig/cron/v3"
)

const (
	MaxNotificationChannels = 16
	MaxWebhookURLBytes      = 2048
	MaxChannelHeaders       = 32
	MaxHeaderNameBytes      = 128
	MaxHeaderValueBytes     = 1024
	MaxChannelSecretBytes   = 1024
)

var standardCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func ValidateScript(script *store.AutomationScript) error {
	return store.ValidateScriptInput(script)
}

func ValidateScheduledTask(task *store.ScheduledTask) error {
	if err := store.ValidateTaskInput(task); err != nil {
		return err
	}
	scheduleType := task.ScheduleType
	if scheduleType == "" {
		scheduleType = store.ScheduleTypeCron
	}
	if scheduleType == store.ScheduleTypeCron {
		if _, _, err := ParseCronSchedule(task.CronExpression, task.Timezone); err != nil {
			return err
		}
	} else if _, err := taskLocation(task.Timezone); err != nil {
		return err
	}
	return ValidateNotificationChannels(task.NotificationChannels)
}

func ValidateNotificationChannels(channels []store.NotificationChannel) error {
	if len(channels) > MaxNotificationChannels {
		return invalid("notification channels")
	}
	for _, channel := range channels {
		if channel.Type != "webhook" && channel.Type != "dingtalk" && channel.Type != "feishu" {
			return invalid("notification channel type")
		}
		if !utf8.ValidString(channel.WebhookURL) || len(channel.WebhookURL) == 0 || len(channel.WebhookURL) > MaxWebhookURLBytes {
			return invalid("notification webhook")
		}
		parsed, err := url.ParseRequestURI(channel.WebhookURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return invalid("notification webhook")
		}
		if len(channel.Headers) > MaxChannelHeaders {
			return invalid("notification headers")
		}
		for name, value := range channel.Headers {
			if !utf8.ValidString(name) || !utf8.ValidString(value) || strings.TrimSpace(name) == "" ||
				len(name) > MaxHeaderNameBytes || len(value) > MaxHeaderValueBytes ||
				strings.ContainsAny(name, "\r\n") || strings.ContainsAny(value, "\r\n") {
				return invalid("notification headers")
			}
		}
		if !utf8.ValidString(channel.Secret) || len(channel.Secret) > MaxChannelSecretBytes {
			return invalid("notification secret")
		}
	}
	return nil
}

func ParseCronSchedule(expression string, timezone string) (cron.Schedule, *time.Location, error) {
	if strings.ContainsAny(expression, "\r\n") {
		return nil, nil, invalid("cron expression")
	}
	expression = strings.TrimSpace(expression)
	if len(expression) == 0 || len(expression) > store.MaxCronExpressionBytes ||
		len(strings.Fields(expression)) != 5 {
		return nil, nil, invalid("cron expression")
	}
	if strings.HasPrefix(expression, "@") || strings.Contains(strings.ToUpper(expression), "CRON_TZ=") ||
		strings.Contains(strings.ToUpper(expression), "TZ=") {
		return nil, nil, invalid("cron expression")
	}
	schedule, err := standardCronParser.Parse(expression)
	if err != nil {
		return nil, nil, invalid("cron expression")
	}
	location, err := taskLocation(timezone)
	if err != nil {
		return nil, nil, err
	}
	return schedule, location, nil
}

func taskLocation(timezone string) (*time.Location, error) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" || timezone == "Local" || len(timezone) > store.MaxTaskTimezoneBytes || strings.ContainsAny(timezone, "\r\n") {
		return nil, invalid("timezone")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, invalid("timezone")
	}
	return location, nil
}

func NextRun(expression string, timezone string, after time.Time) (time.Time, error) {
	if after.IsZero() {
		return time.Time{}, invalid("reference time")
	}
	schedule, location, err := ParseCronSchedule(expression, timezone)
	if err != nil {
		return time.Time{}, err
	}
	next := schedule.Next(after.In(location))
	if next.IsZero() {
		return time.Time{}, invalid("cron expression")
	}
	return next.UTC(), nil
}

func SetNextRun(task *store.ScheduledTask, after time.Time) error {
	if err := ValidateScheduledTask(task); err != nil {
		return err
	}
	scheduleType := task.ScheduleType
	if scheduleType == "" {
		scheduleType = store.ScheduleTypeCron
	}
	if scheduleType == store.ScheduleTypeOnce {
		if !task.RunAt.After(after.UTC()) {
			return invalid("run at")
		}
		if !task.Enabled {
			task.NextRunAt = nil
			return nil
		}
		next := task.RunAt.UTC()
		task.NextRunAt = &next
		return nil
	}
	if !task.Enabled {
		task.NextRunAt = nil
		return nil
	}
	next, err := NextRun(task.CronExpression, task.Timezone, after)
	if err != nil {
		return err
	}
	task.NextRunAt = &next
	return nil
}

func invalid(field string) error {
	return fmt.Errorf("%w: %s", store.ErrInvalid, field)
}
