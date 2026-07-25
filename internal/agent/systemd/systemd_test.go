package systemd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func TestParseServices(t *testing.T) {
	services := parseServices("nginx.service loaded active running A high performance web server\n● failed.service loaded failed failed Failed service\nmizupanel-agent.service loaded active running MizuPanel Agent\n", map[string]string{"nginx.service": "enabled", "failed.service": "disabled"})
	if got, want := len(services), 2; got != want {
		t.Fatalf("services = %#v", services)
	}
	if services[0].Name != "nginx.service" || services[0].UnitFileState != "enabled" || services[1].ActiveState != "failed" {
		t.Fatalf("services = %#v", services)
	}
}

func TestServiceActionUsesFixedSystemctlArguments(t *testing.T) {
	var calls [][]string
	handler := &Handler{supported: true, active: make(map[string]bool)}
	handler.runner = func(_ context.Context, name string, args ...string) (string, string, error) {
		calls = append(calls, append([]string{name}, args...))
		switch len(calls) {
		case 1:
			return "nginx.service loaded active running Nginx\n", "", nil
		case 2:
			return "nginx.service enabled\n", "", nil
		default:
			return "", "", nil
		}
	}

	response := handler.HandleAction(context.Background(), protocol.SystemdServiceActionRequest{ServiceName: "nginx.service", Action: "restart"})
	if !response.Success {
		t.Fatalf("response = %#v", response)
	}
	if got, want := calls[2], []string{"systemctl", "restart", "nginx.service"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("action args = %#v, want %#v", got, want)
	}
}

func TestServiceActionRejectsUnknownOrUnsafeService(t *testing.T) {
	tests := []struct {
		name    string
		request protocol.SystemdServiceActionRequest
		calls   int
	}{
		{name: "unknown", request: protocol.SystemdServiceActionRequest{ServiceName: "worker.service", Action: "restart"}, calls: 2},
		{name: "unsafe name", request: protocol.SystemdServiceActionRequest{ServiceName: "nginx.service;reboot", Action: "restart"}, calls: 0},
		{name: "agent unit", request: protocol.SystemdServiceActionRequest{ServiceName: excludedAgentUnit, Action: "restart"}, calls: 0},
		{name: "unsupported action", request: protocol.SystemdServiceActionRequest{ServiceName: "nginx.service", Action: "enable"}, calls: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler := &Handler{supported: true, active: make(map[string]bool)}
			handler.runner = func(_ context.Context, _ string, _ ...string) (string, string, error) {
				calls++
				if calls == 1 {
					return "nginx.service loaded active running Nginx\n", "", nil
				}
				return "nginx.service enabled\n", "", nil
			}

			response := handler.HandleAction(context.Background(), test.request)
			if response.Success || response.Error == "" {
				t.Fatalf("response = %#v", response)
			}
			if calls != test.calls {
				t.Fatalf("runner calls = %d, want %d", calls, test.calls)
			}
		})
	}
}

func TestServiceLogsUseFixedArgumentsAndRedactSensitiveValues(t *testing.T) {
	var calls [][]string
	handler := &Handler{supported: true, active: make(map[string]bool)}
	handler.runner = func(_ context.Context, name string, args ...string) (string, string, error) {
		calls = append(calls, append([]string{name}, args...))
		switch len(calls) {
		case 1:
			return "nginx.service loaded active running Nginx\n", "", nil
		case 2:
			return "nginx.service enabled\n", "", nil
		default:
			return "PASSWORD=not-for-browser", "", errors.New("exit status 1")
		}
	}

	response := handler.HandleAction(context.Background(), protocol.SystemdServiceActionRequest{ServiceName: "nginx.service", Action: "logs"})
	if response.Success || strings.Contains(response.Output, "not-for-browser") || !strings.Contains(response.Output, "敏感内容已隐藏") {
		t.Fatalf("response = %#v", response)
	}
	if got, want := strings.Join(calls[2], " "), "journalctl --unit nginx.service --no-pager --output=short-iso --lines 200"; got != want {
		t.Fatalf("logs args = %q, want %q", got, want)
	}
}
