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

	if cfg.Listen != ":8080" {
		t.Fatalf("Listen = %q, want :8080", cfg.Listen)
	}
	if cfg.DatabasePath != "./data/mizupanel.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.Storage.Driver != "sqlite" {
		t.Fatalf("Storage.Driver = %q, want sqlite", cfg.Storage.Driver)
	}
	if cfg.Storage.SQLite.Path != "./data/mizupanel.db" {
		t.Fatalf("Storage.SQLite.Path = %q", cfg.Storage.SQLite.Path)
	}
	if cfg.MetricsRetention != 6*time.Hour {
		t.Fatalf("MetricsRetention = %s, want 6h", cfg.MetricsRetention)
	}
	if cfg.CleanupInterval != 10*time.Minute {
		t.Fatalf("CleanupInterval = %s, want 10m", cfg.CleanupInterval)
	}
	if cfg.AuditRetention != 90*24*time.Hour {
		t.Fatalf("AuditRetention = %s, want 90d", cfg.AuditRetention)
	}
	if cfg.AuditCleanup != time.Hour {
		t.Fatalf("AuditCleanup = %s, want 1h", cfg.AuditCleanup)
	}
	if cfg.TaskRetention != 30*24*time.Hour {
		t.Fatalf("TaskRetention = %s, want 30d", cfg.TaskRetention)
	}
	if cfg.TaskCleanup != time.Hour {
		t.Fatalf("TaskCleanup = %s, want 1h", cfg.TaskCleanup)
	}
	if cfg.AgentToken != "" {
		t.Fatalf("AgentToken = %q, want empty default", cfg.AgentToken)
	}
	if cfg.AIKeyFile != "./data/ai.key" {
		t.Fatalf("AIKeyFile = %q, want ./data/ai.key", cfg.AIKeyFile)
	}
	if cfg.EnableTerminal {
		t.Fatal("EnableTerminal = true, want false default")
	}
}

func TestLoadAIKeyFileConfigAndEnvironmentOverride(t *testing.T) {
	t.Setenv("MIZUPANEL_AI_KEY_FILE", "/run/secrets/mizupanel-ai.key")
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte("security:\n  ai_key_file: ./data/file-ai.key\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIKeyFile != "/run/secrets/mizupanel-ai.key" {
		t.Fatalf("AIKeyFile = %q, want environment override", cfg.AIKeyFile)
	}
}

func TestLoadRejectsEmptyAIKeyFileEnvironmentOverride(t *testing.T) {
	t.Setenv("MIZUPANEL_AI_KEY_FILE", "  ")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "security.ai_key_file") {
		t.Fatalf("Load error = %v, want security.ai_key_file validation error", err)
	}
}

func TestLoadAuditConfigAndEnvironmentOverrides(t *testing.T) {
	t.Setenv("MIZUPANEL_AUDIT_RETENTION", "120d")
	t.Setenv("MIZUPANEL_AUDIT_CLEANUP_INTERVAL", "2h")
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte("audit:\n  retention: 30d\n  cleanup_interval: 30m\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditRetention != 120*24*time.Hour || cfg.AuditCleanup != 2*time.Hour {
		t.Fatalf("audit config = %s / %s", cfg.AuditRetention, cfg.AuditCleanup)
	}
}

func TestLoadRejectsInvalidAuditDurations(t *testing.T) {
	for _, content := range []string{
		"audit:\n  retention: nope\n",
		"audit:\n  retention: -1h\n",
		"audit:\n  cleanup_interval: 0s\n",
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "server.yaml")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("Load(%q) returned nil error", content)
		}
	}
}

func TestLoadTaskConfigAndEnvironmentOverrides(t *testing.T) {
	t.Setenv("MIZUPANEL_TASK_RETENTION", "45d")
	t.Setenv("MIZUPANEL_TASK_CLEANUP_INTERVAL", "2h")
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte("tasks:\n  retention: 14d\n  cleanup_interval: 30m\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TaskRetention != 45*24*time.Hour || cfg.TaskCleanup != 2*time.Hour {
		t.Fatalf("task config = %s / %s", cfg.TaskRetention, cfg.TaskCleanup)
	}
}

func TestLoadRejectsInvalidTaskDurations(t *testing.T) {
	for _, content := range []string{
		"tasks:\n  retention: nope\n",
		"tasks:\n  retention: -1h\n",
		"tasks:\n  cleanup_interval: 0s\n",
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "server.yaml")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("Load(%q) returned nil error", content)
		}
	}
}

func TestLoadDefaultAdminAuthConfig(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.AdminAuth.Enabled {
		t.Fatal("AdminAuth.Enabled = true, want false default")
	}
	if cfg.AdminAuth.Username != "admin" {
		t.Fatalf("AdminAuth.Username = %q, want admin", cfg.AdminAuth.Username)
	}
	if cfg.AdminAuth.Password != "" {
		t.Fatalf("AdminAuth.Password = %q, want empty default", cfg.AdminAuth.Password)
	}
	if cfg.AdminAuth.SessionTTL != 24*time.Hour {
		t.Fatalf("AdminAuth.SessionTTL = %s, want 24h", cfg.AdminAuth.SessionTTL)
	}
}

func TestLoadAdminAuthFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`security:
  admin:
    enabled: true
    username: root
    password: secret
    session_ttl: 12h
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.AdminAuth.Enabled {
		t.Fatal("AdminAuth.Enabled = false, want true")
	}
	if cfg.AdminAuth.Username != "root" || cfg.AdminAuth.Password != "secret" || cfg.AdminAuth.SessionTTL != 12*time.Hour {
		t.Fatalf("AdminAuth = %+v", cfg.AdminAuth)
	}
}

func TestLoadAdminAuthEnvironmentOverridesFile(t *testing.T) {
	t.Setenv("MIZUPANEL_AUTH_ENABLED", "true")
	t.Setenv("MIZUPANEL_ADMIN_USERNAME", "env-admin")
	t.Setenv("MIZUPANEL_ADMIN_PASSWORD", "env-secret")
	t.Setenv("MIZUPANEL_SESSION_TTL", "6h")
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`security:
  admin:
    enabled: false
    username: file-admin
    password: file-secret
    session_ttl: 12h
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.AdminAuth.Enabled || cfg.AdminAuth.Username != "env-admin" || cfg.AdminAuth.Password != "env-secret" || cfg.AdminAuth.SessionTTL != 6*time.Hour {
		t.Fatalf("AdminAuth = %+v", cfg.AdminAuth)
	}
}

func TestLoadDebugFromFileAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte("logging:\n  debug: true\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.Debug {
		t.Fatal("Debug = false, want true from logging.debug")
	}

	t.Setenv("MIZUPANEL_DEBUG", "false")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load returned error with env override: %v", err)
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

func TestLoadRejectsEnabledAdminAuthWithoutPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`security:
  admin:
    enabled: true
    username: admin
    password: ""
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "security.admin.password") {
		t.Fatalf("Load error = %v, want security.admin.password error", err)
	}
}

func TestLoadRejectsInvalidAdminSessionTTL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`security:
  admin:
    enabled: true
    username: admin
    password: secret
    session_ttl: nope
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "security.admin.session_ttl") {
		t.Fatalf("Load error = %v, want security.admin.session_ttl error", err)
	}
}

func TestLoadOverridesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`server:
  listen: ':9090'
  public_url: https://panel.example/
  enable_terminal: true
storage:
  database_path: /tmp/mizu.db
metrics:
  retention: 24h
  cleanup_interval: 30m
security:
  agent_token: secret
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Fatalf("Listen = %q", cfg.Listen)
	}
	if cfg.DatabasePath != "/tmp/mizu.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.MetricsRetention != 24*time.Hour {
		t.Fatalf("MetricsRetention = %s", cfg.MetricsRetention)
	}
	if cfg.CleanupInterval != 30*time.Minute {
		t.Fatalf("CleanupInterval = %s", cfg.CleanupInterval)
	}
	if cfg.AgentToken != "secret" {
		t.Fatalf("AgentToken = %q", cfg.AgentToken)
	}
	if cfg.Debug {
		t.Fatal("Debug = true, want false when logging.debug is omitted")
	}
	if cfg.PublicURL != "https://panel.example" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
	if !cfg.EnableTerminal {
		t.Fatal("EnableTerminal = false, want true")
	}
}

func TestLoadSupportsStorageDriverConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`storage:
  driver: mysql
  mysql:
    host: db.internal
    port: 3307
    username: mizupanel
    password: secret
    database: mizupanel_prod
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Storage.Driver != "mysql" {
		t.Fatalf("Storage.Driver = %q", cfg.Storage.Driver)
	}
	if cfg.Storage.MySQL.Host != "db.internal" || cfg.Storage.MySQL.Port != 3307 || cfg.Storage.MySQL.Username != "mizupanel" || cfg.Storage.MySQL.Password != "secret" || cfg.Storage.MySQL.Database != "mizupanel_prod" {
		t.Fatalf("Storage.MySQL = %+v", cfg.Storage.MySQL)
	}
}

func TestLoadExpandsEnvironmentVariables(t *testing.T) {
	t.Setenv("MIZUPANEL_MYSQL_PASSWORD", `secret"\value`)
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`storage:
  driver: mysql
  mysql:
    host: mysql
    username: mizupanel
    password: ${MIZUPANEL_MYSQL_PASSWORD}
    database: mizupanel
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Storage.MySQL.Password != `secret"\value` {
		t.Fatalf("mysql password = %q", cfg.Storage.MySQL.Password)
	}
}

func TestLoadRejectsInvalidStorageDriver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte("storage:\n  driver: postgres\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error, want invalid storage driver error")
	}
}

func TestLoadRejectsIncompleteMySQLStorageConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte("storage:\n  driver: mysql\n  mysql:\n    host: db.internal\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error, want incomplete mysql config error")
	}
}

func TestLoadRejectsMetricsRetentionOutsideRuntimeBounds(t *testing.T) {
	for _, retention := range []string{"30m", "8d"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "server.yaml")
		content := []byte("metrics:\n  retention: " + retention + "\n")
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("Load(%s) returned nil error, want bounds error", retention)
		}
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte("metrics:\n  retention: nope\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error, want invalid duration error")
	}
}

func TestLoadSupportsLegacyFlatConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte("listen: ':9090'\ndatabase_path: /tmp/mizu.db\nmetrics_retention: 24h\ncleanup_interval: 30m\nagent_token: secret\npublic_url: https://panel.example/\nenable_terminal: true\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Listen != ":9090" || cfg.DatabasePath != "/tmp/mizu.db" || cfg.MetricsRetention != 24*time.Hour || cfg.CleanupInterval != 30*time.Minute || cfg.AgentToken != "secret" || cfg.PublicURL != "https://panel.example" || !cfg.EnableTerminal {
		t.Fatalf("loaded legacy config = %+v", cfg)
	}
}

func TestLoadDefaultAlertingConfig(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !cfg.Alerting.Enabled {
		t.Fatal("Alerting.Enabled = false, want true default")
	}
	if cfg.Alerting.CheckInterval != 30*time.Second {
		t.Fatalf("Alerting.CheckInterval = %s, want 30s", cfg.Alerting.CheckInterval)
	}
	if cfg.Alerting.MaxRules != 100 {
		t.Fatalf("Alerting.MaxRules = %d, want 100", cfg.Alerting.MaxRules)
	}
}

func TestLoadAlertingFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`alerting:
  enabled: false
  check_interval: 1m
  max_rules: 50
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Alerting.Enabled {
		t.Fatal("Alerting.Enabled = true, want false")
	}
	if cfg.Alerting.CheckInterval != time.Minute {
		t.Fatalf("Alerting.CheckInterval = %s, want 1m", cfg.Alerting.CheckInterval)
	}
	if cfg.Alerting.MaxRules != 50 {
		t.Fatalf("Alerting.MaxRules = %d, want 50", cfg.Alerting.MaxRules)
	}
}

func TestLoadAlertingEnvironmentOverridesFile(t *testing.T) {
	t.Setenv("MIZUPANEL_ALERTING_ENABLED", "false")
	t.Setenv("MIZUPANEL_ALERT_CHECK_INTERVAL", "2m")
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`alerting:
  enabled: true
  check_interval: 30s
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Alerting.Enabled {
		t.Fatal("Alerting.Enabled = true, want false from env")
	}
	if cfg.Alerting.CheckInterval != 2*time.Minute {
		t.Fatalf("Alerting.CheckInterval = %s, want 2m from env", cfg.Alerting.CheckInterval)
	}
}

func TestLoadRejectsInvalidAlertCheckInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`alerting:
  check_interval: invalid
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "alerting.check_interval") {
		t.Fatalf("Load error = %v, want alerting.check_interval error", err)
	}
}
