package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestRegistryExposesOnlyFixedSafeToolWhitelist(t *testing.T) {
	registry := NewRegistry(RegistryDependencies{})
	want := []string{
		"get_operational_overview",
		"list_nodes",
		"get_node_metrics",
		"list_alerts",
		"list_application_services",
		"list_uptime_monitors",
		"get_log_snapshot",
		"list_k8s_clusters",
		"get_docker_snapshot",
		"get_docker_resources",
		"list_compose_projects",
		"list_node_processes",
		"list_systemd_services",
		"get_k8s_cluster_summary",
		"list_k8s_resources",
		"list_automation_runs",
		"list_audit_events",
		"diagnose_node",
		"diagnose_incident",
		"reboot_node",
		"upgrade_agent",
		"docker_container_action",
		"compose_service_action",
		"systemd_service_action",
		"run_saved_script",
		"create_scheduled_task",
		"create_docker_container",
		"create_k8s_deployment",
	}
	definitions := registry.Definitions()
	if len(definitions) != len(want) {
		t.Fatalf("definition count = %d, want %d", len(definitions), len(want))
	}
	for index, definition := range definitions {
		if definition.Name != want[index] {
			t.Fatalf("definition[%d] = %q, want %q", index, definition.Name, want[index])
		}
		if registry.tools[definition.Name].capability == "" {
			t.Fatalf("definition[%d] %q has no capability metadata", index, definition.Name)
		}
		if additional, ok := definition.Parameters["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("tool %q permits additional properties: %#v", definition.Name, definition.Parameters)
		}
	}

	for _, forbidden := range []string{
		"shell", "exec", "docker_exec", "docker_delete", "docker_prune",
		"compose_down", "file_write", "file_delete", "kubernetes_apply",
		"cc-switch", "cc_switch", "ccswitch", "import_cc_switch", "cc-switch-import",
	} {
		if _, ok := registry.tools[forbidden]; ok {
			t.Fatalf("forbidden tool %q is registered", forbidden)
		}
	}
	for _, definition := range definitions {
		name := strings.ToLower(definition.Name)
		for _, marker := range []string{"cc-switch", "cc_switch", "ccswitch"} {
			if strings.Contains(name, marker) {
				t.Fatalf("cc-switch configuration import concept is registered as %q", definition.Name)
			}
		}
	}
}

func TestRegistryListNodesIncludesIP(t *testing.T) {
	_, nodes, _ := newMutationDatabase(t)
	for _, node := range []store.Node{
		{ID: "node-1", Name: "worker", IP: "192.0.2.10", Status: "online"},
		{ID: "node-2", Name: "untrusted", IP: "192.0.2.11", Status: strings.Repeat("s", 64)},
	} {
		if err := nodes.Upsert(t.Context(), node); err != nil {
			t.Fatalf("upsert node: %v", err)
		}
	}

	registry := NewRegistry(RegistryDependencies{Nodes: nodes})
	validated, err := registry.Validate(t.Context(), "list_nodes", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("validate list_nodes: %v", err)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute list_nodes: %v", err)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("list_nodes data = %T, want map[string]any", result.Data)
	}
	items, ok := data["nodes"].([]map[string]any)
	if !ok || len(items) != 2 {
		t.Fatalf("list_nodes nodes = %#v, want two nodes", data["nodes"])
	}
	byID := make(map[string]map[string]any, len(items))
	for _, item := range items {
		byID[item["id"].(string)] = item
	}
	if byID["node-1"]["status"] != "online" || byID["node-1"]["ip"] != "192.0.2.10" {
		t.Fatalf("list_nodes node = %#v, want online node with IP", byID["node-1"])
	}
	if byID["node-2"]["status"] != strings.Repeat("s", 32) || byID["node-2"]["ip"] != "192.0.2.11" {
		t.Fatalf("list_nodes node = %#v, want bounded status and IP", byID["node-2"])
	}
}

func TestRegistryRejectsUnknownFieldsTrailingJSONAndForbiddenEnums(t *testing.T) {
	registry := NewRegistry(RegistryDependencies{})
	tests := []struct {
		name string
		tool string
		raw  string
	}{
		{name: "unknown field", tool: "list_alerts", raw: `{"status":"active","extra":true}`},
		{name: "trailing object", tool: "list_nodes", raw: `{} {}`},
		{name: "alert status", tool: "list_alerts", raw: `{"status":"all"}`},
		{name: "host file logs", tool: "get_log_snapshot", raw: `{"source":"file","path":"/etc/shadow"}`},
		{name: "container delete", tool: "docker_container_action", raw: `{"node_id":"node-1","container_id":"container-1","action":"delete"}`},
		{name: "compose down", tool: "compose_service_action", raw: `{"node_id":"node-1","project_name":"panel","action":"down"}`},
		{name: "systemd enable", tool: "systemd_service_action", raw: `{"node_id":"node-1","service_name":"panel.service","action":"enable"}`},
		{name: "k8s resource enum", tool: "list_k8s_resources", raw: `{"cluster_id":"cluster-1","resource":"raw"}`},
		{name: "k8s resource unknown field", tool: "list_k8s_resources", raw: `{"cluster_id":"cluster-1","resource":"pods","path":"/api"}`},
		{name: "process limit", tool: "list_node_processes", raw: `{"node_id":"node-1","limit":51}`},
		{name: "audit result", tool: "list_audit_events", raw: `{"result":"pending"}`},
		{name: "automation status", tool: "list_automation_runs", raw: `{"status":"pending"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := registry.Validate(t.Context(), test.tool, json.RawMessage(test.raw))
			if !errors.Is(err, ErrInvalidArguments) {
				t.Fatalf("Validate(%s, %s) error = %v, want ErrInvalidArguments", test.tool, test.raw, err)
			}
		})
	}

	if _, err := registry.Validate(t.Context(), "run_shell", json.RawMessage(`{"command":"id"}`)); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("unknown tool error = %v, want ErrUnknownTool", err)
	}
	validated, err := registry.Validate(t.Context(), "list_alerts", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("validate default list_alerts: %v", err)
	}
	if string(validated.Arguments) != `{"status":"active","limit":20}` || validated.Risk != RiskRead {
		t.Fatalf("normalized list_alerts = %+v", validated)
	}
}

func TestSanitizeModelTextRedactsLogSecretsAndEnvironment(t *testing.T) {
	input := "Authorization: Bearer auth-marker\n" +
		"api_key=api-key-marker password: password-marker token=token-marker\n" +
		"DATABASE_URL=postgres://user:database-marker@db/panel\n" +
		"Cookie: session=cookie-marker\n" +
		"ordinary operational line"
	result := sanitizeModelText(input, 16*1024)
	for _, marker := range []string{"auth-marker", "api-key-marker", "password-marker", "token-marker", "database-marker", "cookie-marker"} {
		if strings.Contains(result, marker) {
			t.Fatalf("sanitized model text contains %q: %s", marker, result)
		}
	}
	if !strings.Contains(result, "ordinary operational line") || !strings.Contains(result, "[REDACTED]") {
		t.Fatalf("sanitized model text lost safe content or redaction marker: %s", result)
	}
}
