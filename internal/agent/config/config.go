package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var legacyIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,190}$`)

type Config struct {
	ServerURL      string
	Token          string
	NodeID         string
	Name           string
	Interval       time.Duration
	EnableDocker   bool
	EnableTerminal bool
	AgentMode      string
	Debug          bool
	IdentitySource string
}

type fileConfig struct {
	Server struct {
		URL   string `yaml:"url"`
		Token string `yaml:"token"`
	} `yaml:"server"`
	Node struct {
		ID   string `yaml:"id"`
		Name string `yaml:"name"`
	} `yaml:"node"`
	Runtime struct {
		Interval string `yaml:"interval"`
		Mode     string `yaml:"mode"`
	} `yaml:"runtime"`
	Features struct {
		Docker   *bool `yaml:"docker"`
		Terminal *bool `yaml:"terminal"`
	} `yaml:"features"`
	Logging struct {
		Debug bool `yaml:"debug"`
	} `yaml:"logging"`

	ServerURL          string `yaml:"server_url,omitempty"`
	Token              string `yaml:"token,omitempty"`
	NodeID             string `yaml:"node_id,omitempty"`
	Name               string `yaml:"name,omitempty"`
	Interval           string `yaml:"interval,omitempty"`
	LegacyEnableDocker bool   `yaml:"enable_docker,omitempty"`
	LegacyEnableTerm   bool   `yaml:"enable_terminal,omitempty"`
	AgentMode          string `yaml:"agent_mode,omitempty"`
	legacyDockerSet    bool
	legacyTerminalSet  bool
}

func (c *fileConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawFileConfig fileConfig
	var raw rawFileConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*c = fileConfig(raw)
	for i := 0; i+1 < len(value.Content); i += 2 {
		switch value.Content[i].Value {
		case "enable_docker":
			c.legacyDockerSet = true
		case "enable_terminal":
			c.legacyTerminalSet = true
		}
	}
	return nil
}

func Load(path string) (Config, error) {
	hostname, _ := os.Hostname()
	cfg := Config{
		ServerURL:      "ws://localhost:8080/api/agent/ws",
		Token:          "change-me",
		NodeID:         hostname,
		Name:           hostname,
		Interval:       5 * time.Second,
		EnableDocker:   false,
		EnableTerminal: false,
		AgentMode:      "normal",
		IdentitySource: "legacy",
	}
	if cfg.NodeID == "" {
		cfg.NodeID = hostname
	}
	if cfg.Name == "" {
		cfg.Name = "Mizu Agent"
	}
	if path == "" {
		if err := applyEnvironmentConfig(&cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}

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
	if err := applyEnvironmentConfig(&cfg); err != nil {
		return Config{}, err
	}
	identityPath := filepath.Join(filepath.Dir(path), "agent-id")
	identity, err := os.ReadFile(identityPath)
	if err == nil {
		value := strings.TrimSpace(string(identity))
		if !uuidPattern.MatchString(value) && !legacyIdentityPattern.MatchString(value) {
			return Config{}, fmt.Errorf("invalid agent identity in %s", identityPath)
		}
		cfg.NodeID = value
		if uuidPattern.MatchString(value) {
			cfg.NodeID = strings.ToLower(value)
			cfg.IdentitySource = "persistent_uuid"
		}
	} else if !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("read agent identity %s: %w", identityPath, err)
	}
	return cfg, nil
}

func SaveToken(path string, token string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var file fileConfig
	if err := yaml.Unmarshal(content, &file); err != nil {
		return err
	}
	canonicalizeFileConfig(&file)
	file.Server.Token = token
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func applyEnvironmentConfig(cfg *Config) error {
	if value, ok := os.LookupEnv("MIZUPANEL_DEBUG"); ok {
		debug, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse MIZUPANEL_DEBUG: %w", err)
		}
		cfg.Debug = debug
	}
	return nil
}

func applyFileConfig(cfg *Config, file fileConfig) error {
	if file.ServerURL != "" {
		cfg.ServerURL = file.ServerURL
	}
	if file.Token != "" {
		cfg.Token = file.Token
	}
	if file.NodeID != "" {
		cfg.NodeID = file.NodeID
	}
	if file.Name != "" {
		cfg.Name = file.Name
	}
	if file.Interval != "" {
		duration, err := time.ParseDuration(file.Interval)
		if err != nil {
			return fmt.Errorf("parse interval: %w", err)
		}
		cfg.Interval = duration
	}
	if file.legacyDockerSet {
		cfg.EnableDocker = file.LegacyEnableDocker
	}
	if file.legacyTerminalSet {
		cfg.EnableTerminal = file.LegacyEnableTerm
	}
	if file.AgentMode == "ops" {
		cfg.AgentMode = "ops"
	} else if file.AgentMode != "" {
		cfg.AgentMode = "normal"
	}

	if file.Server.URL != "" {
		cfg.ServerURL = file.Server.URL
	}
	if file.Server.Token != "" {
		cfg.Token = file.Server.Token
	}
	if file.Node.ID != "" {
		cfg.NodeID = file.Node.ID
	}
	if file.Node.Name != "" {
		cfg.Name = file.Node.Name
	}
	if file.Runtime.Interval != "" {
		duration, err := time.ParseDuration(file.Runtime.Interval)
		if err != nil {
			return fmt.Errorf("parse runtime.interval: %w", err)
		}
		cfg.Interval = duration
	}
	if file.Runtime.Mode == "ops" {
		cfg.AgentMode = "ops"
	} else if file.Runtime.Mode != "" {
		cfg.AgentMode = "normal"
	}
	if file.Features.Docker != nil {
		cfg.EnableDocker = *file.Features.Docker
	}
	if file.Features.Terminal != nil {
		cfg.EnableTerminal = *file.Features.Terminal
	}
	cfg.Debug = file.Logging.Debug
	return nil
}

func canonicalizeFileConfig(file *fileConfig) {
	if file.Server.URL == "" {
		file.Server.URL = file.ServerURL
	}
	if file.Server.Token == "" {
		file.Server.Token = file.Token
	}
	if file.Node.ID == "" {
		file.Node.ID = file.NodeID
	}
	if file.Node.Name == "" {
		file.Node.Name = file.Name
	}
	if file.Runtime.Interval == "" {
		file.Runtime.Interval = file.Interval
	}
	if file.Runtime.Mode == "" {
		file.Runtime.Mode = file.AgentMode
	}
	if file.Runtime.Mode != "ops" {
		file.Runtime.Mode = "normal"
	}
	if file.Features.Docker == nil {
		docker := file.LegacyEnableDocker
		file.Features.Docker = &docker
	}
	if file.Features.Terminal == nil {
		terminal := file.LegacyEnableTerm
		file.Features.Terminal = &terminal
	}
	file.ServerURL = ""
	file.Token = ""
	file.NodeID = ""
	file.Name = ""
	file.Interval = ""
	file.LegacyEnableDocker = false
	file.LegacyEnableTerm = false
	file.AgentMode = ""
}
