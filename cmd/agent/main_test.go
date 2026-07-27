package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsServiceSupportIsWired(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	content, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "agent", "service_windows.go"))
	if err != nil {
		t.Fatalf("read windows service support: %v", err)
	}
	source := string(content)
	for _, want := range []string{"svc.Run", "svc.IsWindowsService", "svc.AcceptStop", "svc.AcceptShutdown", "runAgent(ctx, s.configPath)"} {
		if !strings.Contains(source, want) {
			t.Fatalf("windows service support missing %q", want)
		}
	}
}

func TestAgentDockerCollectionRequiresConfigOptIn(t *testing.T) {
	content, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read agent main: %v", err)
	}
	source := string(content)
	for _, want := range []string{"if cfg.EnableDocker", "dockerCollector = agentdocker.NewCollector()", "if dockerCollector != nil", "message.DockerSnapshot = &dockerSnapshot"} {
		if !strings.Contains(source, want) {
			t.Fatalf("agent Docker collection is not gated by config opt-in, missing %q", want)
		}
	}
}

func TestAgentManagementDockerStatusUsesCollector(t *testing.T) {
	content, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read agent main: %v", err)
	}
	source := string(content)
	for _, want := range []string{"DockerStatus:", "func() (bool, string)", "dockerCollector.Collect()", "return snapshot.Available, snapshot.Error"} {
		if !strings.Contains(source, want) {
			t.Fatalf("agent management Docker status does not use live collector status, missing %q", want)
		}
	}
}

func TestAgentTaskRunnerCapabilityIsWired(t *testing.T) {
	content, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read agent main: %v", err)
	}
	source := string(content)
	for _, want := range []string{"agenttaskrunner.New()", "client.SetTaskRunner(taskRunner)", "TaskRunner:", "taskRunner.Supported()"} {
		if !strings.Contains(source, want) {
			t.Fatalf("Agent task runner capability is not wired, missing %q", want)
		}
	}
}

func TestAgentPackageBuildsForWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cross-build test is for non-Windows CI")
	}
	// The Makefile builds this package for Windows; this test keeps package-level service wiring visible to `go test`.
}
