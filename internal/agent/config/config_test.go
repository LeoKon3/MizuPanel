package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ServerURL != "ws://localhost:8080/api/agent/ws" {
		t.Fatalf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Interval != 5*time.Second {
		t.Fatalf("Interval = %s, want 5s", cfg.Interval)
	}
	if cfg.Token != "change-me" {
		t.Fatalf("Token = %q, want change-me", cfg.Token)
	}
	if cfg.NodeID == "" {
		t.Fatal("NodeID is empty")
	}
	if cfg.Name == "" {
		t.Fatal("Name is empty")
	}
	if cfg.EnableDocker {
		t.Fatal("EnableDocker = true, want false by default")
	}
	if cfg.EnableTerminal {
		t.Fatal("EnableTerminal = true, want false by default")
	}
}

func TestLoadOverridesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte(`server:
  url: ws://example.test/api/agent/ws
  token: secret
node:
  id: oracle-node-1
  name: Oracle
runtime:
  interval: 3s
  mode: ops
features:
  docker: true
  terminal: true
logging:
  debug: true
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServerURL != "ws://example.test/api/agent/ws" || cfg.Token != "secret" || cfg.NodeID != "oracle-node-1" || cfg.Name != "Oracle" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if cfg.Interval != 3*time.Second {
		t.Fatalf("Interval = %s, want 3s", cfg.Interval)
	}
	if !cfg.EnableDocker {
		t.Fatal("EnableDocker = false, want true")
	}
	if !cfg.EnableTerminal {
		t.Fatal("EnableTerminal = false, want true")
	}
	if cfg.AgentMode != "ops" {
		t.Fatalf("AgentMode = %q, want ops", cfg.AgentMode)
	}
	if !cfg.Debug {
		t.Fatal("Debug = false, want true")
	}
}

func TestLoadDebugEnvironmentOverridesFile(t *testing.T) {
	t.Setenv("MIZUPANEL_DEBUG", "false")
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("logging:\n  debug: true\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Debug {
		t.Fatal("Debug = true, want false from MIZUPANEL_DEBUG override")
	}
}

func TestLoadRejectsInvalidDebugEnvironment(t *testing.T) {
	t.Setenv("MIZUPANEL_DEBUG", "not-bool")
	_, err := Load("")
	if err == nil || !strings.Contains(err.Error(), "MIZUPANEL_DEBUG") {
		t.Fatalf("Load error = %v, want MIZUPANEL_DEBUG parse error", err)
	}
}

func TestSaveTokenUpdatesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte(`server:
  url: ws://example.test/api/agent/ws
  token: install-token
node:
  id: oracle-node-1
  name: Oracle
runtime:
  interval: 3s
  mode: ops
features:
  docker: true
  terminal: true
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := SaveToken(path, "node-token"); err != nil {
		t.Fatalf("SaveToken returned error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Token != "node-token" || cfg.ServerURL != "ws://example.test/api/agent/ws" || cfg.NodeID != "oracle-node-1" || cfg.Interval != 3*time.Second || !cfg.EnableDocker || !cfg.EnableTerminal || cfg.AgentMode != "ops" {
		t.Fatalf("unexpected config after token save: %#v", cfg)
	}
	contentAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(contentAfter), "server:\n") || strings.Contains(string(contentAfter), "server_url:") {
		t.Fatalf("saved config was not canonical nested YAML:\n%s", contentAfter)
	}
}

func TestSaveTokenMigratesLegacyFlatConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("server_url: ws://example.test/api/agent/ws\ntoken: install-token\nnode_id: oracle-node-1\nname: Oracle\ninterval: 3s\nenable_docker: true\nenable_terminal: true\nagent_mode: ops\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := SaveToken(path, "node-token"); err != nil {
		t.Fatalf("SaveToken returned error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Token != "node-token" || cfg.ServerURL != "ws://example.test/api/agent/ws" || cfg.NodeID != "oracle-node-1" || cfg.Interval != 3*time.Second || !cfg.EnableDocker || !cfg.EnableTerminal || cfg.AgentMode != "ops" {
		t.Fatalf("unexpected migrated config: %#v", cfg)
	}
}

func TestLoadSupportsLegacyFlatConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("server_url: ws://example.test/api/agent/ws\ntoken: secret\nnode_id: oracle-node-1\nname: Oracle\ninterval: 3s\nenable_docker: true\nenable_terminal: true\nagent_mode: ops\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServerURL != "ws://example.test/api/agent/ws" || cfg.Token != "secret" || cfg.NodeID != "oracle-node-1" || cfg.Name != "Oracle" || cfg.Interval != 3*time.Second || !cfg.EnableDocker || !cfg.EnableTerminal || cfg.AgentMode != "ops" {
		t.Fatalf("unexpected legacy config: %#v", cfg)
	}
}

func TestLoadRejectsInvalidInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("runtime:\n  interval: nope\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error, want invalid interval error")
	}
}

func TestLoadUsesPersistentAgentIDBesideConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("node:\n  id: legacy-host\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const identity = "7bb62f38-85fd-4b89-9c6d-e2ca51a4c241"
	if err := os.WriteFile(filepath.Join(dir, "agent-id"), []byte(identity+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != identity {
		t.Fatalf("NodeID = %q, want %q", cfg.NodeID, identity)
	}
}

func TestLoadRejectsMalformedPersistentAgentID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("node:\n  id: legacy-host\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-id"), []byte("invalid identity/value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() succeeded with malformed persistent identity")
	}
}

func TestLoadPreservesMigratedLegacyAgentID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("node:\n  id: ignored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-id"), []byte("master\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "master" || cfg.IdentitySource != "legacy" {
		t.Fatalf("config = %#v", cfg)
	}
}
