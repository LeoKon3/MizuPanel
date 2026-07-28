package servicecenter

import (
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func TestHealthMappingsForAllResourceTypes(t *testing.T) {
	base := Resource{ID: "resource-1", ResourceKey: "resource", DisplayName: "Resource"}
	available := int32(2)
	tests := []struct {
		name       string
		projection ResourceProjection
		want       HealthStatus
		state      string
		reason     string
	}{
		{name: "node healthy", projection: nodeHealth(base, nodeSignal{Status: "online"}, true), want: HealthHealthy, state: "available"},
		{name: "node offline", projection: nodeHealth(base, nodeSignal{Status: "offline"}, true), want: HealthUnhealthy, reason: "离线"},
		{name: "node missing", projection: nodeHealth(base, nodeSignal{}, false), want: HealthUnknown, state: "missing"},
		{name: "uptime disabled", projection: uptimeHealth(base, uptimeSignal{Enabled: false}, true), want: HealthDegraded, reason: "禁用"},
		{name: "uptime warning", projection: uptimeHealth(base, uptimeSignal{Enabled: true, Status: "warning"}, true), want: HealthDegraded},
		{name: "uptime down", projection: uptimeHealth(base, uptimeSignal{Enabled: true, Status: "down"}, true), want: HealthUnhealthy},
		{name: "alert active", projection: alertHealth(base, alertSignal{Enabled: true, ActiveCount: 2}, true), want: HealthUnhealthy, reason: "2"},
		{name: "alert clear", projection: alertHealth(base, alertSignal{Enabled: true}, true), want: HealthHealthy},
		{name: "task success", projection: taskHealth(base, taskSignal{Enabled: true, HasRun: true, LatestStatus: "success"}, true), want: HealthHealthy},
		{name: "task failed", projection: taskHealth(base, taskSignal{Enabled: true, HasRun: true, LatestStatus: "failed"}, true), want: HealthDegraded, reason: "failed"},
		{name: "task no run", projection: taskHealth(base, taskSignal{Enabled: true}, true), want: HealthUnknown},
		{name: "compose healthy", projection: composeHealth(base, &protocol.DockerComposeProject{Status: "running", Services: []protocol.DockerComposeService{{Name: "web", State: "running", Health: "healthy"}}}, ""), want: HealthHealthy},
		{name: "compose starting", projection: composeHealth(base, &protocol.DockerComposeProject{Status: "running", Services: []protocol.DockerComposeService{{Name: "web", State: "running", Health: "starting"}}}, ""), want: HealthDegraded},
		{name: "compose unhealthy", projection: composeHealth(base, &protocol.DockerComposeProject{Status: "running", Services: []protocol.DockerComposeService{{Name: "web", State: "running", Health: "unhealthy"}}}, ""), want: HealthUnhealthy, reason: "web"},
		{name: "compose missing", projection: composeHealth(base, nil, ""), want: HealthUnknown, state: "missing"},
		{name: "compose unavailable", projection: composeHealth(base, nil, "timeout"), want: HealthDegraded, state: "unavailable"},
		{name: "systemd running", projection: systemdHealth(base, &protocol.SystemdService{ActiveState: "active", SubState: "running"}, ""), want: HealthHealthy},
		{name: "systemd activating", projection: systemdHealth(base, &protocol.SystemdService{ActiveState: "activating"}, ""), want: HealthDegraded},
		{name: "systemd failed", projection: systemdHealth(base, &protocol.SystemdService{ActiveState: "failed"}, ""), want: HealthUnhealthy},
		{name: "deployment healthy", projection: k8sReadyHealth(base, "2/2", &available, ""), want: HealthHealthy},
		{name: "deployment partial", projection: k8sReadyHealth(base, "1/2", &available, ""), want: HealthDegraded},
		{name: "deployment unavailable", projection: k8sReadyHealth(base, "0/2", &available, ""), want: HealthUnhealthy},
		{name: "deployment malformed", projection: k8sReadyHealth(base, "two/two", nil, ""), want: HealthUnknown},
		{name: "daemon partial", projection: k8sDaemonHealth(base, &protocol.K8sDaemonSet{Desired: 3, Ready: 2, Available: 2}, ""), want: HealthDegraded},
		{name: "daemon missing", projection: k8sDaemonHealth(base, nil, ""), want: HealthUnknown, state: "missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.projection.Health != test.want {
				t.Fatalf("health = %s, want %s: %#v", test.projection.Health, test.want, test.projection)
			}
			if test.state != "" && test.projection.State != test.state {
				t.Fatalf("state = %q, want %q", test.projection.State, test.state)
			}
			if test.reason != "" && !strings.Contains(test.projection.Reason, test.reason) {
				t.Fatalf("reason = %q, want to contain %q", test.projection.Reason, test.reason)
			}
		})
	}
}

func TestAggregateHealthPrecedenceAndStableReasons(t *testing.T) {
	resources := []ResourceProjection{
		projection(Resource{ID: "z", ResourceType: ResourceNode, DisplayName: "Zulu"}, HealthUnknown, "missing", "unknown"),
		projection(Resource{ID: "b", ResourceType: ResourceAlertRule, DisplayName: "Beta"}, HealthUnhealthy, "available", "bad beta"),
		projection(Resource{ID: "a", ResourceType: ResourceComposeProject, DisplayName: "Alpha"}, HealthUnhealthy, "available", "bad alpha"),
		projection(Resource{ID: "h", ResourceType: ResourceScheduledTask, DisplayName: "Healthy"}, HealthHealthy, "available", ""),
	}
	health, reasons, counts := aggregateHealth(resources)
	if health != HealthUnhealthy || counts["unhealthy"] != 2 || counts["unknown"] != 1 {
		t.Fatalf("aggregate = health:%s counts:%#v", health, counts)
	}
	if len(reasons) != 3 || reasons[0].ResourceName != "Alpha" || reasons[1].ResourceName != "Beta" || reasons[2].Status != HealthUnknown {
		t.Fatalf("reasons are not stable/severity ordered: %#v", reasons)
	}

	health, _, _ = aggregateHealth([]ResourceProjection{
		projection(Resource{ID: "h"}, HealthHealthy, "available", ""),
		projection(Resource{ID: "u"}, HealthUnknown, "missing", "missing"),
	})
	if health != HealthDegraded {
		t.Fatalf("healthy plus unknown aggregate = %s, want degraded", health)
	}
	health, reasons, counts = aggregateHealth(nil)
	if health != HealthUnknown || len(reasons) != 0 || counts == nil {
		t.Fatalf("empty aggregate = %s %#v %#v", health, reasons, counts)
	}
}
