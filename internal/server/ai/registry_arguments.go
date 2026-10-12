package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type metricsArguments struct {
	NodeID       string `json:"node_id"`
	RangeMinutes int    `json:"range_minutes"`
}

type alertsArguments struct {
	Status string `json:"status"`
	Limit  int    `json:"limit"`
}

type logsArguments struct {
	Source      string `json:"source"`
	NodeID      string `json:"node_id"`
	ServiceName string `json:"service_name"`
	Lines       int    `json:"lines"`
}

type nodeReadArguments struct {
	NodeID string `json:"node_id"`
}

type nodeLimitArguments struct {
	NodeID string `json:"node_id"`
	Limit  int    `json:"limit"`
}

type containerArguments struct {
	NodeID      string `json:"node_id"`
	ContainerID string `json:"container_id"`
	Action      string `json:"action"`
}

type composeArguments struct {
	NodeID      string `json:"node_id"`
	ProjectName string `json:"project_name"`
	ServiceName string `json:"service_name"`
	Action      string `json:"action"`
}

type systemdArguments struct {
	NodeID      string `json:"node_id"`
	ServiceName string `json:"service_name"`
	Action      string `json:"action"`
}

type scriptArguments struct {
	ScriptID int64    `json:"script_id"`
	NodeIDs  []string `json:"node_ids"`
}

type k8sResourceArguments struct {
	ClusterID string `json:"cluster_id"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Limit     int    `json:"limit"`
}

type automationRunsArguments struct {
	Status string `json:"status"`
	NodeID string `json:"node_id"`
	Limit  int    `json:"limit"`
}

type auditEventsArguments struct {
	Module string `json:"module"`
	Action string `json:"action"`
	NodeID string `json:"node_id"`
	Result string `json:"result"`
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
}

type createScheduledTaskArguments struct {
	Name               string     `json:"name"`
	ScriptID           int64      `json:"script_id"`
	NodeIDs            []string   `json:"node_ids"`
	ScheduleType       string     `json:"schedule_type"`
	RunAt              *time.Time `json:"run_at"`
	CronExpression     string     `json:"cron_expression"`
	Timezone           string     `json:"timezone"`
	Enabled            *bool      `json:"enabled"`
	TimeoutSeconds     int        `json:"timeout_seconds"`
	NotificationPolicy string     `json:"notification_policy"`
}

type createDockerContainerArguments struct {
	NodeID        string                                `json:"node_id"`
	Image         string                                `json:"image"`
	Name          string                                `json:"name"`
	AutoName      bool                                  `json:"auto_name"`
	RestartPolicy string                                `json:"restart_policy"`
	NetworkMode   string                                `json:"network_mode"`
	Ports         []protocol.DockerContainerPort        `json:"ports"`
	Environment   []protocol.DockerContainerEnvironment `json:"environment"`
	Mounts        []protocol.DockerContainerMount       `json:"mounts"`
	Start         bool                                  `json:"start"`
}

type createK8sDeploymentArguments struct {
	ClusterID     string `json:"cluster_id"`
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Replicas      int32  `json:"replicas"`
	ContainerPort int32  `json:"container_port"`
}

func noArguments(_ context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
	var args struct{}
	if err := strictArguments(raw, &args); err != nil {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	return json.RawMessage(`{}`), ToolTarget{}, nil
}

func strictArguments(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidArguments
	}
	return nil
}

func normalizedArguments(value any) json.RawMessage {
	content, _ := json.Marshal(value)
	return content
}

func noArgumentDefinition(name, description string) ToolDefinition {
	return objectDefinition(name, description, map[string]any{}, nil)
}

func objectDefinition(name, description string, properties map[string]any, required []string) ToolDefinition {
	parameters := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return ToolDefinition{Name: name, Description: description, Parameters: parameters}
}
