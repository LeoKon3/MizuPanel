package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type Config struct {
	Listen           string
	DatabasePath     string
	Storage          serverdb.StorageConfig
	MetricsRetention time.Duration
	CleanupInterval  time.Duration
	AuditRetention   time.Duration
	AuditCleanup     time.Duration
	AgentToken       string
	PublicURL        string
	EnableTerminal   bool
	Debug            bool
	AdminAuth        AdminAuthConfig
	Alerting         AlertingConfig
}

type AdminAuthConfig struct {
	Enabled    bool
	Username   string
	Password   string
	SessionTTL time.Duration
}

type AlertingConfig struct {
	Enabled       bool
	CheckInterval time.Duration
	MaxRules      int
}

type fileConfig struct {
	Server struct {
		Listen         string `yaml:"listen"`
		PublicURL      string `yaml:"public_url"`
		EnableTerminal *bool  `yaml:"enable_terminal"`
	} `yaml:"server"`
	Storage struct {
		Driver       string `yaml:"driver"`
		DatabasePath string `yaml:"database_path"`
		SQLite       struct {
			Path string `yaml:"path"`
		} `yaml:"sqlite"`
		MySQL struct {
			Host     string `yaml:"host"`
			Port     int    `yaml:"port"`
			Username string `yaml:"username"`
			Password string `yaml:"password"`
			Database string `yaml:"database"`
		} `yaml:"mysql"`
	} `yaml:"storage"`
	Metrics struct {
		Retention       string `yaml:"retention"`
		CleanupInterval string `yaml:"cleanup_interval"`
	} `yaml:"metrics"`
	Audit struct {
		Retention       string `yaml:"retention"`
		CleanupInterval string `yaml:"cleanup_interval"`
	} `yaml:"audit"`
	Security struct {
		AgentToken string `yaml:"agent_token"`
		Admin      struct {
			Enabled    bool   `yaml:"enabled"`
			Username   string `yaml:"username"`
			Password   string `yaml:"password"`
			SessionTTL string `yaml:"session_ttl"`
		} `yaml:"admin"`
	} `yaml:"security"`
	Alerting struct {
		Enabled       bool   `yaml:"enabled"`
		CheckInterval string `yaml:"check_interval"`
		MaxRules      int    `yaml:"max_rules"`
	} `yaml:"alerting"`
	Logging struct {
		Debug bool `yaml:"debug"`
	} `yaml:"logging"`

	Listen                  string `yaml:"listen"`
	DatabasePath            string `yaml:"database_path"`
	MetricsRetention        string `yaml:"metrics_retention"`
	CleanupInterval         string `yaml:"cleanup_interval"`
	AgentToken              string `yaml:"agent_token"`
	PublicURL               string `yaml:"public_url"`
	LegacyEnableTerminal    bool   `yaml:"enable_terminal"`
	legacyEnableTerminalSet bool
}

func (c *fileConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawFileConfig fileConfig
	var raw rawFileConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*c = fileConfig(raw)
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == "enable_terminal" {
			c.legacyEnableTerminalSet = true
			break
		}
	}
	return nil
}

func Load(path string) (Config, error) {
	cfg := Config{
		Listen:       ":8080",
		DatabasePath: "./data/mizupanel.db",
		Storage: serverdb.StorageConfig{
			Driver: "sqlite",
			SQLite: serverdb.SQLiteConfig{Path: "./data/mizupanel.db"},
			MySQL:  serverdb.MySQLConfig{Port: 3306},
		},
		MetricsRetention: 6 * time.Hour,
		CleanupInterval:  10 * time.Minute,
		AuditRetention:   90 * 24 * time.Hour,
		AuditCleanup:     time.Hour,
		AgentToken:       os.Getenv("MIZUPANEL_AGENT_TOKEN"),
		AdminAuth: AdminAuthConfig{
			Username:   "admin",
			SessionTTL: 24 * time.Hour,
		},
		Alerting: AlertingConfig{
			Enabled:       true,
			CheckInterval: 30 * time.Second,
			MaxRules:      100,
		},
	}
	if path != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			return Config{}, err
		}

		var file fileConfig
		if err := yaml.Unmarshal(content, &file); err != nil {
			return Config{}, err
		}
		if err := applyFileConfig(&cfg, file); err != nil {
			return Config{}, err
		}
	}
	if err := applyEnvironmentConfig(&cfg); err != nil {
		return Config{}, err
	}
	if err := validateConfig(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyFileConfig(cfg *Config, file fileConfig) error {
	if file.Listen != "" {
		cfg.Listen = file.Listen
	}
	if file.DatabasePath != "" {
		cfg.DatabasePath = file.DatabasePath
		cfg.Storage.SQLite.Path = file.DatabasePath
	}
	if file.MetricsRetention != "" {
		duration, err := store.ParseMetricsRetention(file.MetricsRetention)
		if err != nil {
			return fmt.Errorf("parse metrics_retention: %w", err)
		}
		cfg.MetricsRetention = duration
	}
	if file.CleanupInterval != "" {
		duration, err := parseDuration(file.CleanupInterval)
		if err != nil {
			return fmt.Errorf("parse cleanup_interval: %w", err)
		}
		cfg.CleanupInterval = duration
	}
	if file.AgentToken != "" {
		cfg.AgentToken = file.AgentToken
	}
	if file.PublicURL != "" {
		cfg.PublicURL = strings.TrimRight(file.PublicURL, "/")
	}
	if file.legacyEnableTerminalSet {
		cfg.EnableTerminal = file.LegacyEnableTerminal
	}

	if file.Server.Listen != "" {
		cfg.Listen = file.Server.Listen
	}
	if file.Storage.Driver != "" {
		cfg.Storage.Driver = strings.ToLower(strings.TrimSpace(file.Storage.Driver))
	}
	if file.Storage.DatabasePath != "" {
		cfg.DatabasePath = file.Storage.DatabasePath
		cfg.Storage.SQLite.Path = file.Storage.DatabasePath
	}
	if file.Storage.SQLite.Path != "" {
		cfg.DatabasePath = file.Storage.SQLite.Path
		cfg.Storage.SQLite.Path = file.Storage.SQLite.Path
	}
	if file.Storage.MySQL.Host != "" {
		cfg.Storage.MySQL.Host = file.Storage.MySQL.Host
	}
	if file.Storage.MySQL.Port != 0 {
		cfg.Storage.MySQL.Port = file.Storage.MySQL.Port
	}
	if file.Storage.MySQL.Username != "" {
		cfg.Storage.MySQL.Username = file.Storage.MySQL.Username
	}
	if file.Storage.MySQL.Password != "" {
		cfg.Storage.MySQL.Password = file.Storage.MySQL.Password
	}
	if file.Storage.MySQL.Database != "" {
		cfg.Storage.MySQL.Database = file.Storage.MySQL.Database
	}
	if file.Metrics.Retention != "" {
		duration, err := store.ParseMetricsRetention(file.Metrics.Retention)
		if err != nil {
			return fmt.Errorf("parse metrics.retention: %w", err)
		}
		cfg.MetricsRetention = duration
	}
	if file.Metrics.CleanupInterval != "" {
		duration, err := parseDuration(file.Metrics.CleanupInterval)
		if err != nil {
			return fmt.Errorf("parse metrics.cleanup_interval: %w", err)
		}
		cfg.CleanupInterval = duration
	}
	if file.Audit.Retention != "" {
		duration, err := parseDuration(file.Audit.Retention)
		if err != nil {
			return fmt.Errorf("parse audit.retention: %w", err)
		}
		cfg.AuditRetention = duration
	}
	if file.Audit.CleanupInterval != "" {
		duration, err := parseDuration(file.Audit.CleanupInterval)
		if err != nil {
			return fmt.Errorf("parse audit.cleanup_interval: %w", err)
		}
		cfg.AuditCleanup = duration
	}
	if file.Security.AgentToken != "" {
		cfg.AgentToken = file.Security.AgentToken
	}
	cfg.AdminAuth.Enabled = file.Security.Admin.Enabled
	if file.Security.Admin.Username != "" {
		cfg.AdminAuth.Username = file.Security.Admin.Username
	}
	if file.Security.Admin.Password != "" {
		cfg.AdminAuth.Password = file.Security.Admin.Password
	}
	if file.Security.Admin.SessionTTL != "" {
		duration, err := parseDuration(file.Security.Admin.SessionTTL)
		if err != nil {
			return fmt.Errorf("parse security.admin.session_ttl: %w", err)
		}
		cfg.AdminAuth.SessionTTL = duration
	}
	if file.Server.PublicURL != "" {
		cfg.PublicURL = strings.TrimRight(file.Server.PublicURL, "/")
	}
	if file.Server.EnableTerminal != nil {
		cfg.EnableTerminal = *file.Server.EnableTerminal
	}
	cfg.Alerting.Enabled = file.Alerting.Enabled
	if file.Alerting.CheckInterval != "" {
		duration, err := parseDuration(file.Alerting.CheckInterval)
		if err != nil {
			return fmt.Errorf("parse alerting.check_interval: %w", err)
		}
		cfg.Alerting.CheckInterval = duration
	}
	if file.Alerting.MaxRules != 0 {
		cfg.Alerting.MaxRules = file.Alerting.MaxRules
	}
	cfg.Debug = file.Logging.Debug
	expandStorageEnv(&cfg.Storage)
	return validateStorageConfig(&cfg.Storage)
}

func applyEnvironmentConfig(cfg *Config) error {
	if value, ok := os.LookupEnv("MIZUPANEL_DEBUG"); ok {
		debug, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse MIZUPANEL_DEBUG: %w", err)
		}
		cfg.Debug = debug
	}
	if value, ok := os.LookupEnv("MIZUPANEL_AUTH_ENABLED"); ok {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse MIZUPANEL_AUTH_ENABLED: %w", err)
		}
		cfg.AdminAuth.Enabled = enabled
	}
	if value, ok := os.LookupEnv("MIZUPANEL_ADMIN_USERNAME"); ok {
		cfg.AdminAuth.Username = value
	}
	if value, ok := os.LookupEnv("MIZUPANEL_ADMIN_PASSWORD"); ok {
		cfg.AdminAuth.Password = value
	}
	if value, ok := os.LookupEnv("MIZUPANEL_SESSION_TTL"); ok {
		duration, err := parseDuration(value)
		if err != nil {
			return fmt.Errorf("parse MIZUPANEL_SESSION_TTL: %w", err)
		}
		cfg.AdminAuth.SessionTTL = duration
	}
	if value, ok := os.LookupEnv("MIZUPANEL_ALERTING_ENABLED"); ok {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse MIZUPANEL_ALERTING_ENABLED: %w", err)
		}
		cfg.Alerting.Enabled = enabled
	}
	if value, ok := os.LookupEnv("MIZUPANEL_ALERT_CHECK_INTERVAL"); ok {
		duration, err := parseDuration(value)
		if err != nil {
			return fmt.Errorf("parse MIZUPANEL_ALERT_CHECK_INTERVAL: %w", err)
		}
		cfg.Alerting.CheckInterval = duration
	}
	if value, ok := os.LookupEnv("MIZUPANEL_AUDIT_RETENTION"); ok {
		duration, err := parseDuration(value)
		if err != nil {
			return fmt.Errorf("parse MIZUPANEL_AUDIT_RETENTION: %w", err)
		}
		cfg.AuditRetention = duration
	}
	if value, ok := os.LookupEnv("MIZUPANEL_AUDIT_CLEANUP_INTERVAL"); ok {
		duration, err := parseDuration(value)
		if err != nil {
			return fmt.Errorf("parse MIZUPANEL_AUDIT_CLEANUP_INTERVAL: %w", err)
		}
		cfg.AuditCleanup = duration
	}
	return nil
}

func validateConfig(cfg *Config) error {
	if cfg.AdminAuth.Username == "" {
		cfg.AdminAuth.Username = "admin"
	}
	if cfg.AdminAuth.SessionTTL <= 0 {
		return fmt.Errorf("security.admin.session_ttl must be positive")
	}
	if cfg.AdminAuth.Enabled && cfg.AdminAuth.Password == "" {
		return fmt.Errorf("security.admin.password is required when admin auth is enabled")
	}
	if cfg.AuditRetention <= 0 {
		return fmt.Errorf("audit.retention must be positive")
	}
	if cfg.AuditCleanup <= 0 {
		return fmt.Errorf("audit.cleanup_interval must be positive")
	}
	return nil
}

func expandStorageEnv(storage *serverdb.StorageConfig) {
	storage.SQLite.Path = os.ExpandEnv(storage.SQLite.Path)
	storage.MySQL.Host = os.ExpandEnv(storage.MySQL.Host)
	storage.MySQL.Username = os.ExpandEnv(storage.MySQL.Username)
	storage.MySQL.Password = os.ExpandEnv(storage.MySQL.Password)
	storage.MySQL.Database = os.ExpandEnv(storage.MySQL.Database)
}

func validateStorageConfig(storage *serverdb.StorageConfig) error {
	storage.Driver = strings.ToLower(strings.TrimSpace(storage.Driver))
	if storage.Driver == "" {
		storage.Driver = "sqlite"
	}
	if storage.SQLite.Path == "" {
		storage.SQLite.Path = "./data/mizupanel.db"
	}
	if storage.MySQL.Port == 0 {
		storage.MySQL.Port = 3306
	}
	switch storage.Driver {
	case "sqlite":
		return nil
	case "mysql":
		if storage.MySQL.Host == "" || storage.MySQL.Username == "" || storage.MySQL.Database == "" {
			return fmt.Errorf("storage.mysql host, username, and database are required")
		}
		return nil
	default:
		return fmt.Errorf("storage.driver must be sqlite or mysql")
	}
}

func parseDuration(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}
