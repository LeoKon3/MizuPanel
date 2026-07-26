package uptime

import (
	"errors"
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

func validHTTPMonitor() store.UptimeMonitor {
	return store.UptimeMonitor{
		Name:                   "Website",
		Type:                   MonitorTypeHTTP,
		Target:                 "https://example.com/health",
		Enabled:                true,
		IntervalSeconds:        DefaultIntervalSeconds,
		TimeoutSeconds:         DefaultTimeoutSeconds,
		FailureThreshold:       DefaultFailureThreshold,
		ExpectedStatusMin:      DefaultExpectedStatusMin,
		ExpectedStatusMax:      DefaultExpectedStatusMax,
		TLSExpiryThresholdDays: DefaultTLSExpiryThresholdDays,
		NotificationChannels:   []store.NotificationChannel{},
	}
}

func TestApplyDefaultsNormalizesMonitor(t *testing.T) {
	monitor := store.UptimeMonitor{Name: "  Website  ", Target: "  https://example.com  "}
	ApplyDefaults(&monitor)

	if monitor.Name != "Website" || monitor.Type != MonitorTypeHTTP || monitor.Target != "https://example.com" {
		t.Fatalf("normalized monitor = %+v", monitor)
	}
	if monitor.IntervalSeconds != 60 || monitor.TimeoutSeconds != 5 || monitor.FailureThreshold != 3 {
		t.Fatalf("default timing = %+v", monitor)
	}
	if monitor.ExpectedStatusMin != 200 || monitor.ExpectedStatusMax != 399 || monitor.TLSExpiryThresholdDays != 30 {
		t.Fatalf("default HTTP fields = %+v", monitor)
	}
	if monitor.NotificationChannels == nil {
		t.Fatal("notification channels must be a typed empty slice")
	}
}

func TestValidateMonitorAcceptsSupportedTargetsAndChannels(t *testing.T) {
	tests := []store.UptimeMonitor{
		validHTTPMonitor(),
		func() store.UptimeMonitor {
			monitor := validHTTPMonitor()
			monitor.Type = MonitorTypeTCP
			monitor.Target = "[2001:db8::1]:443"
			return monitor
		}(),
		func() store.UptimeMonitor {
			monitor := validHTTPMonitor()
			monitor.NotificationChannels = []store.NotificationChannel{
				{Type: "webhook", WebhookURL: "https://hooks.example.com/path"},
				{Type: "dingtalk", WebhookURL: "https://hooks.example.com/dingtalk", Secret: "secret"},
				{Type: "feishu", WebhookURL: "http://hooks.example.com/feishu"},
			}
			return monitor
		}(),
	}

	for index := range tests {
		monitor := tests[index]
		if err := ValidateMonitor(&monitor); err != nil {
			t.Fatalf("case %d rejected: %v", index, err)
		}
	}
}

func TestValidateMonitorRejectsEveryConfigurationBoundary(t *testing.T) {
	longName := strings.Repeat("界", MaxMonitorNameRunes+1)
	longTarget := "https://example.com/" + strings.Repeat("a", MaxMonitorTargetBytes)
	tooManyChannels := make([]store.NotificationChannel, MaxNotificationChannels+1)
	for index := range tooManyChannels {
		tooManyChannels[index] = store.NotificationChannel{Type: "webhook", WebhookURL: "https://hooks.example.com"}
	}

	tests := []struct {
		name   string
		mutate func(*store.UptimeMonitor)
	}{
		{name: "empty name", mutate: func(m *store.UptimeMonitor) { m.Name = " " }},
		{name: "invalid UTF-8 name", mutate: func(m *store.UptimeMonitor) { m.Name = string([]byte{0xff}) }},
		{name: "name too long", mutate: func(m *store.UptimeMonitor) { m.Name = longName }},
		{name: "empty target", mutate: func(m *store.UptimeMonitor) { m.Target = " " }},
		{name: "target too long", mutate: func(m *store.UptimeMonitor) { m.Target = longTarget }},
		{name: "interval below minimum", mutate: func(m *store.UptimeMonitor) { m.IntervalSeconds = MinIntervalSeconds - 1 }},
		{name: "interval above maximum", mutate: func(m *store.UptimeMonitor) { m.IntervalSeconds = MaxIntervalSeconds + 1 }},
		{name: "timeout below minimum", mutate: func(m *store.UptimeMonitor) { m.TimeoutSeconds = -1 }},
		{name: "timeout above maximum", mutate: func(m *store.UptimeMonitor) { m.TimeoutSeconds = MaxTimeoutSeconds + 1 }},
		{name: "timeout equals interval", mutate: func(m *store.UptimeMonitor) { m.IntervalSeconds = 30; m.TimeoutSeconds = 30 }},
		{name: "failure threshold below minimum", mutate: func(m *store.UptimeMonitor) { m.FailureThreshold = -1 }},
		{name: "failure threshold above maximum", mutate: func(m *store.UptimeMonitor) { m.FailureThreshold = MaxFailureThreshold + 1 }},
		{name: "status minimum too low", mutate: func(m *store.UptimeMonitor) { m.ExpectedStatusMin = 99 }},
		{name: "status maximum too high", mutate: func(m *store.UptimeMonitor) { m.ExpectedStatusMax = 600 }},
		{name: "status range reversed", mutate: func(m *store.UptimeMonitor) { m.ExpectedStatusMin = 400; m.ExpectedStatusMax = 399 }},
		{name: "TLS threshold below minimum", mutate: func(m *store.UptimeMonitor) { m.TLSExpiryThresholdDays = -1 }},
		{name: "TLS threshold above maximum", mutate: func(m *store.UptimeMonitor) { m.TLSExpiryThresholdDays = MaxTLSExpiryThresholdDays + 1 }},
		{name: "unsupported type", mutate: func(m *store.UptimeMonitor) { m.Type = "icmp" }},
		{name: "relative HTTP URL", mutate: func(m *store.UptimeMonitor) { m.Target = "/health" }},
		{name: "unsupported HTTP scheme", mutate: func(m *store.UptimeMonitor) { m.Target = "ftp://example.com/health" }},
		{name: "HTTP credentials", mutate: func(m *store.UptimeMonitor) { m.Target = "https://user:password@example.com/health" }},
		{name: "HTTP fragment", mutate: func(m *store.UptimeMonitor) { m.Target = "https://example.com/health#secret" }},
		{name: "TCP missing port", mutate: func(m *store.UptimeMonitor) { m.Type = MonitorTypeTCP; m.Target = "example.com" }},
		{name: "TCP empty host", mutate: func(m *store.UptimeMonitor) { m.Type = MonitorTypeTCP; m.Target = ":443" }},
		{name: "TCP port zero", mutate: func(m *store.UptimeMonitor) { m.Type = MonitorTypeTCP; m.Target = "example.com:0" }},
		{name: "TCP port too high", mutate: func(m *store.UptimeMonitor) { m.Type = MonitorTypeTCP; m.Target = "example.com:65536" }},
		{name: "too many channels", mutate: func(m *store.UptimeMonitor) { m.NotificationChannels = tooManyChannels }},
		{name: "unsupported channel", mutate: func(m *store.UptimeMonitor) {
			m.NotificationChannels = []store.NotificationChannel{{Type: "email", WebhookURL: "https://hooks.example.com"}}
		}},
		{name: "custom channel headers", mutate: func(m *store.UptimeMonitor) {
			m.NotificationChannels = []store.NotificationChannel{{Type: "webhook", WebhookURL: "https://hooks.example.com", Headers: map[string]string{"Authorization": "secret"}}}
		}},
		{name: "channel URL credentials", mutate: func(m *store.UptimeMonitor) {
			m.NotificationChannels = []store.NotificationChannel{{Type: "webhook", WebhookURL: "https://token@hooks.example.com"}}
		}},
		{name: "channel URL unsupported scheme", mutate: func(m *store.UptimeMonitor) {
			m.NotificationChannels = []store.NotificationChannel{{Type: "webhook", WebhookURL: "ftp://hooks.example.com"}}
		}},
		{name: "channel value too long", mutate: func(m *store.UptimeMonitor) {
			m.NotificationChannels = []store.NotificationChannel{{Type: "webhook", WebhookURL: "https://hooks.example.com/" + strings.Repeat("x", MaxNotificationValueBytes)}}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			monitor := validHTTPMonitor()
			test.mutate(&monitor)
			if err := ValidateMonitor(&monitor); !errors.Is(err, ErrInvalidMonitor) {
				t.Fatalf("ValidateMonitor error = %v, want ErrInvalidMonitor", err)
			}
		})
	}
}
