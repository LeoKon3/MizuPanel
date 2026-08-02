package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
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
		"reboot_node",
		"upgrade_agent",
		"docker_container_action",
		"compose_service_action",
		"systemd_service_action",
		"run_saved_script",
	}
	definitions := registry.Definitions()
	if len(definitions) != len(want) {
		t.Fatalf("definition count = %d, want %d", len(definitions), len(want))
	}
	for index, definition := range definitions {
		if definition.Name != want[index] {
			t.Fatalf("definition[%d] = %q, want %q", index, definition.Name, want[index])
		}
		if additional, ok := definition.Parameters["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("tool %q permits additional properties: %#v", definition.Name, definition.Parameters)
		}
	}

	for _, forbidden := range []string{
		"shell", "exec", "docker_exec", "docker_delete", "docker_prune",
		"compose_down", "file_write", "file_delete", "kubernetes_apply",
	} {
		if _, ok := registry.tools[forbidden]; ok {
			t.Fatalf("forbidden tool %q is registered", forbidden)
		}
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
