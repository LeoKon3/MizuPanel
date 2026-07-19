package docker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func TestDecodeComposeRecordsSupportsArrayAndJSONLines(t *testing.T) {
	array, err := decodeComposeRecords[composeProjectRecord](`[{"Name":"alpha","Status":"running(1)"}]`)
	if err != nil || len(array) != 1 || array[0].Name != "alpha" {
		t.Fatalf("array records = %#v, err = %v", array, err)
	}
	lines, err := decodeComposeRecords[composeServiceRecord]("{\"Service\":\"web\",\"State\":\"running\"}\n{\"Service\":\"db\",\"State\":\"exited\"}\n")
	if err != nil || len(lines) != 2 || lines[1].Service != "db" {
		t.Fatalf("line records = %#v, err = %v", lines, err)
	}
}

func TestParseComposeFilesRequiresAbsolutePaths(t *testing.T) {
	raw, _ := json.Marshal("/srv/app/compose.yml,relative.yml, /srv/app/override.yml")
	got := parseComposeFiles(raw)
	want := []string{"/srv/app/compose.yml", "/srv/app/override.yml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}
}

func TestComposeActionUsesOnlyDiscoveredFilesAndStructuredArgs(t *testing.T) {
	var calls [][]string
	handler := &ComposeHandler{supported: true, active: make(map[string]bool)}
	handler.runner = func(_ context.Context, args ...string) (string, string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(calls) == 1 {
			return `[{"Name":"demo","Status":"running(1)","ConfigFiles":"/srv/demo/compose.yml"}]`, "", nil
		}
		if len(calls) == 2 {
			return `[{"Service":"web","Name":"demo-web-1","State":"running"}]`, "", nil
		}
		return "updated", "", nil
	}
	response := handler.HandleDockerComposeAction(context.Background(), protocol.DockerComposeActionRequest{ProjectName: "demo", Action: "up"})
	if !response.Success {
		t.Fatalf("response = %#v", response)
	}
	got := strings.Join(calls[2], " ")
	want := "compose --project-name demo --file /srv/demo/compose.yml up -d"
	if got != want {
		t.Fatalf("action args = %q, want %q", got, want)
	}
}

func TestComposeServiceActionUsesDiscoveredServiceAndStructuredArgs(t *testing.T) {
	var calls [][]string
	handler := &ComposeHandler{supported: true, active: make(map[string]bool)}
	handler.runner = func(_ context.Context, args ...string) (string, string, error) {
		calls = append(calls, append([]string(nil), args...))
		switch len(calls) {
		case 1:
			return `[{"Name":"demo","ConfigFiles":"/srv/demo/compose.yml"}]`, "", nil
		case 2:
			return `[{"Service":"web","Name":"demo-web-1","State":"running"}]`, "", nil
		default:
			return "restarted", "", nil
		}
	}

	response := handler.HandleDockerComposeAction(context.Background(), protocol.DockerComposeActionRequest{ProjectName: "demo", ServiceName: "web", Action: "restart"})
	if !response.Success {
		t.Fatalf("response = %#v", response)
	}
	if got, want := strings.Join(calls[2], " "), "compose --project-name demo --file /srv/demo/compose.yml restart web"; got != want {
		t.Fatalf("service action args = %q, want %q", got, want)
	}
}

func TestComposeServiceActionRejectsUnknownOrUnsupportedServiceScope(t *testing.T) {
	tests := []struct {
		name    string
		request protocol.DockerComposeActionRequest
		calls   int
	}{
		{name: "unknown service", request: protocol.DockerComposeActionRequest{ProjectName: "demo", ServiceName: "worker", Action: "restart"}, calls: 2},
		{name: "project-only action", request: protocol.DockerComposeActionRequest{ProjectName: "demo", ServiceName: "web", Action: "logs"}, calls: 0},
		{name: "invalid service identifier", request: protocol.DockerComposeActionRequest{ProjectName: "demo", ServiceName: "web;down", Action: "restart"}, calls: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler := &ComposeHandler{supported: true, active: make(map[string]bool)}
			handler.runner = func(_ context.Context, _ ...string) (string, string, error) {
				calls++
				if calls == 1 {
					return `[{"Name":"demo","ConfigFiles":"/srv/demo/compose.yml"}]`, "", nil
				}
				return `[{"Service":"web","Name":"demo-web-1","State":"running"}]`, "", nil
			}

			response := handler.HandleDockerComposeAction(context.Background(), test.request)
			if response.Success || response.Error == "" {
				t.Fatalf("response = %#v", response)
			}
			if calls != test.calls {
				t.Fatalf("runner calls = %d, want %d", calls, test.calls)
			}
		})
	}
}

func TestComposeActionLogsAndValidateUseFixedArguments(t *testing.T) {
	tests := []struct {
		action string
		want   string
	}{
		{action: "logs", want: "compose --project-name demo --file /srv/demo/compose.yml logs --no-color --tail 200"},
		{action: "validate", want: "compose --project-name demo --file /srv/demo/compose.yml config --quiet"},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			var calls [][]string
			handler := &ComposeHandler{supported: true, active: make(map[string]bool)}
			handler.runner = func(_ context.Context, args ...string) (string, string, error) {
				calls = append(calls, append([]string(nil), args...))
				switch len(calls) {
				case 1:
					return `[{"Name":"demo","ConfigFiles":"/srv/demo/compose.yml"}]`, "", nil
				case 2:
					return `[]`, "", nil
				default:
					return "ok", "", nil
				}
			}
			response := handler.HandleDockerComposeAction(context.Background(), protocol.DockerComposeActionRequest{ProjectName: "demo", Action: test.action})
			if !response.Success {
				t.Fatalf("response = %#v", response)
			}
			if got := strings.Join(calls[2], " "); got != test.want {
				t.Fatalf("action args = %q, want %q", got, test.want)
			}
		})
	}
}

func TestComposeValidationFailureRedactsSensitiveValues(t *testing.T) {
	calls := 0
	handler := &ComposeHandler{supported: true, active: make(map[string]bool)}
	handler.runner = func(_ context.Context, _ ...string) (string, string, error) {
		calls++
		switch calls {
		case 1:
			return `[{"Name":"demo","ConfigFiles":"/srv/demo/compose.yml"}]`, "", nil
		case 2:
			return "[]", "", nil
		default:
			return "", `services.db.environment.PASSWORD: supersecret`, errors.New("exit status 1")
		}
	}
	response := handler.HandleDockerComposeAction(context.Background(), protocol.DockerComposeActionRequest{ProjectName: "demo", Action: "validate"})
	if response.Success || strings.Contains(response.Error, "supersecret") || strings.Contains(response.Output, "supersecret") {
		t.Fatalf("response was not redacted: %#v", response)
	}
	if !strings.Contains(response.Error, "敏感内容已隐藏") {
		t.Fatalf("response did not explain redaction: %#v", response)
	}
}

func TestComposeDiscoveryKeepsDownProjectInPrivateCache(t *testing.T) {
	tempDir := t.TempDir()
	composeFile := filepath.Join(tempDir, "compose.yml")
	if err := os.WriteFile(composeFile, []byte("services: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(tempDir, "var", "compose-projects.json")
	listCalls := 0
	handler := &ComposeHandler{supported: true, active: make(map[string]bool), cachePath: cachePath}
	handler.runner = func(_ context.Context, args ...string) (string, string, error) {
		if reflect.DeepEqual(args, []string{"compose", "ls", "--all", "--format", "json"}) {
			listCalls++
			if listCalls == 1 {
				payload, _ := json.Marshal([]composeProjectRecord{{Name: "demo", ConfigFiles: json.RawMessage(`"` + composeFile + `"`)}})
				return string(payload), "", nil
			}
			return "[]", "", nil
		}
		return "[]", "", nil
	}

	first := handler.HandleDockerComposeList(context.Background(), protocol.DockerComposeListRequest{})
	second := handler.HandleDockerComposeList(context.Background(), protocol.DockerComposeListRequest{})
	if !first.Success || !second.Success || len(second.Projects) != 1 || second.Projects[0].Name != "demo" {
		t.Fatalf("first = %#v, second = %#v", first, second)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("cache mode = %o, want 600", info.Mode().Perm())
	}

	if err := os.Remove(composeFile); err != nil {
		t.Fatal(err)
	}
	third := handler.HandleDockerComposeList(context.Background(), protocol.DockerComposeListRequest{})
	if !third.Success || len(third.Projects) != 0 {
		t.Fatalf("invalid cached project was not skipped: %#v", third)
	}
}

func TestComposeCacheWriteFailureDoesNotBlockLiveDiscovery(t *testing.T) {
	tempDir := t.TempDir()
	blocker := filepath.Join(tempDir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := &ComposeHandler{supported: true, active: make(map[string]bool), cachePath: filepath.Join(blocker, "compose-projects.json")}
	handler.runner = func(_ context.Context, args ...string) (string, string, error) {
		if reflect.DeepEqual(args, []string{"compose", "ls", "--all", "--format", "json"}) {
			return `[{"Name":"demo","ConfigFiles":"/srv/demo/compose.yml"}]`, "", nil
		}
		return "[]", "", nil
	}
	response := handler.HandleDockerComposeList(context.Background(), protocol.DockerComposeListRequest{})
	if !response.Success || len(response.Projects) != 1 {
		t.Fatalf("response = %#v", response)
	}
}

func TestComposeActionRejectsUnknownActionAndProjectName(t *testing.T) {
	handler := &ComposeHandler{supported: true, active: make(map[string]bool), runner: func(context.Context, ...string) (string, string, error) {
		t.Fatal("runner must not be called")
		return "", "", nil
	}}
	for _, request := range []protocol.DockerComposeActionRequest{{ProjectName: "demo", Action: "exec"}, {ProjectName: "../demo", Action: "up"}} {
		if response := handler.HandleDockerComposeAction(context.Background(), request); response.Success || response.Error == "" {
			t.Fatalf("response = %#v", response)
		}
	}
}
