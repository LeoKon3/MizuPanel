package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/store"
	"github.com/mizupanel/mizupanel/internal/server/taskrunner"
)

const (
	maxAICreatePorts = 8
	maxAIImageBytes  = 256
)

var dockerContainerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

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

func (r *Registry) registerCreationTools() {
	r.add(registeredTool{
		definition: objectDefinition("create_scheduled_task", "Create a one-time or recurring scheduled task from an existing saved script after confirmation. Use schedule_type=once with a future RFC3339 run_at, or schedule_type=cron with a five-field Cron expression and IANA timezone. Before calling, ask for every creation setting. Script content and shell commands are not accepted.", map[string]any{
			"name":                map[string]any{"type": "string", "maxLength": store.MaxAutomationNameRunes},
			"script_id":           map[string]any{"type": "integer", "minimum": 1},
			"node_ids":            map[string]any{"type": "array", "minItems": 1, "maxItems": store.MaxTaskNodes, "uniqueItems": true, "items": map[string]any{"type": "string", "maxLength": store.MaxTaskNodeIDBytes}},
			"schedule_type":       map[string]any{"type": "string", "enum": []string{store.ScheduleTypeCron, store.ScheduleTypeOnce}},
			"run_at":              map[string]any{"type": "string", "format": "date-time"},
			"cron_expression":     map[string]any{"type": "string", "maxLength": store.MaxCronExpressionBytes},
			"timezone":            map[string]any{"type": "string", "maxLength": store.MaxTaskTimezoneBytes},
			"enabled":             map[string]any{"type": "boolean"},
			"timeout_seconds":     map[string]any{"type": "integer", "minimum": 0, "maximum": store.MaxTaskTimeoutSeconds},
			"notification_policy": map[string]any{"type": "string", "enum": []string{store.NotificationPolicyNever, store.NotificationPolicyFailure, store.NotificationPolicyAlways}},
		}, []string{"name", "script_id", "node_ids", "schedule_type", "enabled", "timeout_seconds", "notification_policy"}),
		risk:       RiskConfirm,
		capability: capabilityTaskCreation,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args createScheduledTaskArguments
			if err := requireCreationParameters(raw, "create_scheduled_task", "name", "script_id", "node_ids", "schedule_type", "run_at", "cron_expression", "timezone", "enabled", "timeout_seconds", "notification_policy"); err != nil {
				return nil, ToolTarget{}, err
			}
			if err := strictArguments(raw, &args); err != nil || args.ScriptID <= 0 || len(args.NodeIDs) < 1 || len(args.NodeIDs) > store.MaxTaskNodes {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if r.dependencies.Tasks == nil || r.dependencies.AgentOps == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			args.Name = strings.TrimSpace(args.Name)
			args.ScheduleType = strings.TrimSpace(args.ScheduleType)
			args.CronExpression = strings.TrimSpace(args.CronExpression)
			args.Timezone = strings.TrimSpace(args.Timezone)
			args.NotificationPolicy = strings.TrimSpace(args.NotificationPolicy)
			for index := range args.NodeIDs {
				args.NodeIDs[index] = strings.TrimSpace(args.NodeIDs[index])
			}
			sort.Strings(args.NodeIDs)
			if args.NotificationPolicy == "" || args.Enabled == nil || !oneOf(args.ScheduleType, store.ScheduleTypeCron, store.ScheduleTypeOnce) {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if err := validateScheduledTaskTargets(ctx, r, args); err != nil {
				return nil, ToolTarget{}, err
			}
			_, err := r.dependencies.Tasks.GetScript(ctx, args.ScriptID)
			if err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			task := scheduledTaskFromAIArgs(args)
			if err := taskrunner.ValidateScheduledTask(&task); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if err := taskrunner.SetNextRun(&task, time.Now().UTC()); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return normalizedArguments(args), ToolTarget{Type: "scheduled_task", ID: scheduledTaskTargetID(args), Name: boundedString(args.Name, 128)}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args createScheduledTaskArguments
			_ = json.Unmarshal(raw, &args)
			task := scheduledTaskFromAIArgs(args)
			if err := taskrunner.SetNextRun(&task, time.Now().UTC()); err != nil {
				return SafeToolResult{}, ErrInvalidArguments
			}
			if err := r.dependencies.Tasks.CreateScheduledTask(ctx, &task); err != nil {
				return SafeToolResult{}, safeAutomationError(err)
			}
			return SafeToolResult{Data: map[string]any{"success": true, "task_id": task.ID, "name": boundedString(task.Name, 128), "schedule_type": task.ScheduleType, "run_at": task.RunAt, "cron_expression": task.CronExpression, "timezone": task.Timezone, "node_count": len(task.NodeIDs), "status": "success"}, Summary: "计划任务创建成功", Status: "success"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("create_docker_container", "Create a bounded Docker container through the structured Docker API after confirmation. Before calling, ask for the target node, image, name or auto-name, restart policy, network mode, port mappings, environment variables or an explicit empty list, typed mounts or an explicit empty list, and whether to start immediately. Shell commands, privileged mode, devices and Docker Socket mounts are unavailable.", map[string]any{
			"node_id":        map[string]any{"type": "string", "maxLength": 191},
			"image":          map[string]any{"type": "string", "maxLength": maxAIImageBytes},
			"name":           map[string]any{"type": "string", "maxLength": 128},
			"auto_name":      map[string]any{"type": "boolean"},
			"restart_policy": map[string]any{"type": "string", "enum": []string{"no", "always", "on-failure", "unless-stopped"}},
			"network_mode":   map[string]any{"type": "string", "enum": []string{"bridge", "host", "none"}},
			"ports":          map[string]any{"type": "array", "maxItems": maxAICreatePorts, "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"host_port": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}, "container_port": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}, "protocol": map[string]any{"type": "string", "enum": []string{"tcp", "udp"}}}, "required": []string{"host_port", "container_port", "protocol"}}},
			"environment":    map[string]any{"type": "array", "maxItems": protocol.DockerContainerMaxEnvironment, "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"key": map[string]any{"type": "string", "maxLength": 128}, "value": map[string]any{"type": "string", "maxLength": protocol.DockerContainerMaxEnvValue}}, "required": []string{"key", "value"}}},
			"mounts":         map[string]any{"type": "array", "maxItems": protocol.DockerContainerMaxMounts, "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"type": map[string]any{"type": "string", "enum": []string{"bind", "volume"}}, "source": map[string]any{"type": "string", "maxLength": 4096}, "target": map[string]any{"type": "string", "maxLength": 4096}, "read_only": map[string]any{"type": "boolean"}}, "required": []string{"type", "source", "target", "read_only"}}},
			"start":          map[string]any{"type": "boolean"},
		}, []string{"node_id", "image", "auto_name", "restart_policy", "network_mode", "ports", "environment", "mounts", "start"}),
		risk:       RiskConfirm,
		capability: capabilityDockerAgent,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args createDockerContainerArguments
			if err := requireCreationParameters(raw, "create_docker_container", "node_id", "image", "name", "auto_name", "restart_policy", "network_mode", "ports", "environment", "mounts", "start"); err != nil {
				return nil, ToolTarget{}, err
			}
			if err := strictArguments(raw, &args); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			args.NodeID = strings.TrimSpace(args.NodeID)
			args.Image = strings.TrimSpace(args.Image)
			args.Name = strings.TrimSpace(args.Name)
			if !validDockerImage(args.Image) || len(args.Ports) > maxAICreatePorts || (args.AutoName && args.Name != "") || (!args.AutoName && !dockerContainerNamePattern.MatchString(args.Name)) || !oneOf(args.RestartPolicy, "no", "always", "on-failure", "unless-stopped") || !oneOf(args.NetworkMode, "bridge", "host", "none") {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if r.dependencies.AgentOps == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			if err := validateDockerPorts(args.Ports); err != nil {
				return nil, ToolTarget{}, err
			}
			if protocol.ValidateDockerContainerEnvironment(args.Environment) != nil || protocol.ValidateDockerContainerMounts(args.Mounts) != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if _, err := r.onlineNode(ctx, args.NodeID, true); err != nil {
				return nil, ToolTarget{}, err
			}
			if !r.dependencies.AgentOps.DockerContainerCreateSupported(args.NodeID) {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			if (len(args.Environment) > 0 || len(args.Mounts) > 0) && !r.dependencies.AgentOps.DockerContainerCreateV2Supported(args.NodeID) {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			name := args.Name
			if name == "" {
				name = args.Image
			}
			return normalizedArguments(args), ToolTarget{Type: "docker_container", ID: args.NodeID + "/" + name, Name: boundedString(name, 128), NodeID: args.NodeID}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args createDockerContainerArguments
			_ = json.Unmarshal(raw, &args)
			response, err := r.dependencies.AgentOps.DockerContainerCreate(ctx, args.NodeID, protocol.DockerContainerCreateRequest{Image: args.Image, Name: args.Name, RestartPolicy: args.RestartPolicy, NetworkMode: args.NetworkMode, Ports: args.Ports, Environment: args.Environment, Mounts: args.Mounts, Start: args.Start})
			if err != nil {
				return SafeToolResult{}, safeRemoteError(err)
			}
			if !response.Supported {
				return SafeToolResult{}, ErrUnsupportedTool
			}
			if !response.Success {
				if (response.Created || response.ContainerID != "") && response.ContainerID != "" {
					return SafeToolResult{
						Data: map[string]any{
							"success":      false,
							"created":      true,
							"container_id": boundedString(response.ContainerID, 128),
							"name":         boundedString(response.Name, 128),
							"started":      response.Started,
							"status":       "failure",
						},
						Summary:     "Docker 容器已创建，但后续操作失败",
						Status:      "failure",
						OperationID: boundedString(response.ContainerID, 128),
					}, nil
				}
				return SafeToolResult{}, safeRemoteError(fmt.Errorf("docker create failed"))
			}
			return SafeToolResult{Data: map[string]any{"success": true, "container_id": boundedString(response.ContainerID, 128), "name": boundedString(response.Name, 128), "started": response.Started, "status": "success"}, Summary: "Docker 容器创建成功", Status: "success"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("create_k8s_deployment", "Create one generated Kubernetes Deployment after confirmation. Before calling, ask for and receive the cluster, namespace, Deployment name, image, replica count, and whether to expose a container port. Raw YAML and arbitrary resource kinds are unavailable.", map[string]any{
			"cluster_id":     map[string]any{"type": "string", "maxLength": 191},
			"namespace":      map[string]any{"type": "string", "maxLength": 63},
			"name":           map[string]any{"type": "string", "maxLength": 63},
			"image":          map[string]any{"type": "string", "maxLength": maxAIImageBytes},
			"replicas":       map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			"container_port": map[string]any{"type": "integer", "minimum": 0, "maximum": 65535},
		}, []string{"cluster_id", "namespace", "name", "image", "replicas", "container_port"}),
		risk:       RiskConfirm,
		capability: capabilityKubernetesMutation,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args createK8sDeploymentArguments
			if err := requireCreationParameters(raw, "create_k8s_deployment", "cluster_id", "namespace", "name", "image", "replicas", "container_port"); err != nil {
				return nil, ToolTarget{}, err
			}
			if err := strictArguments(raw, &args); err != nil || !validIdentifier(args.ClusterID, 191) || args.Replicas < 1 || args.ContainerPort < 0 || args.ContainerPort > 65535 || !validDockerImage(args.Image) {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			args.ClusterID = strings.TrimSpace(args.ClusterID)
			args.Namespace = strings.TrimSpace(args.Namespace)
			args.Name = strings.TrimSpace(args.Name)
			args.Image = strings.TrimSpace(args.Image)
			if r.dependencies.Kubernetes == nil || r.dependencies.KubernetesMutations == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			cluster, err := r.k8sCluster(args.ClusterID)
			if err != nil {
				return nil, ToolTarget{}, err
			}
			if cluster.NodeStatus != "online" {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			port := (*int32)(nil)
			if args.ContainerPort > 0 {
				value := args.ContainerPort
				port = &value
			}
			if err := k8s.ValidateCreateDeploymentRequest(k8s.CreateDeploymentRequest{Namespace: args.Namespace, Name: args.Name, Image: args.Image, Replicas: args.Replicas, ContainerPort: port}); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return normalizedArguments(args), ToolTarget{Type: "k8s_deployment", ID: args.ClusterID + "/" + args.Namespace + "/" + args.Name, Name: args.Namespace + "/" + args.Name, NodeID: cluster.NodeID}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args createK8sDeploymentArguments
			_ = json.Unmarshal(raw, &args)
			port := (*int32)(nil)
			if args.ContainerPort > 0 {
				value := args.ContainerPort
				port = &value
			}
			result, err := r.dependencies.KubernetesMutations.CreateDeployment(ctx, args.ClusterID, k8s.CreateDeploymentRequest{Namespace: args.Namespace, Name: args.Name, Image: args.Image, Replicas: args.Replicas, ContainerPort: port})
			if err != nil {
				return SafeToolResult{}, safeRemoteError(err)
			}
			if result == nil || !result.Success {
				return SafeToolResult{}, safeRemoteError(fmt.Errorf("kubernetes deployment create failed"))
			}
			return SafeToolResult{Data: map[string]any{"success": true, "cluster_id": args.ClusterID, "namespace": boundedString(args.Namespace, 63), "name": boundedString(args.Name, 63), "replicas": args.Replicas, "status": "success"}, Summary: "Kubernetes Deployment 创建成功", Status: "success"}, nil
		},
	})
}

func scheduledTaskFromAIArgs(args createScheduledTaskArguments) store.ScheduledTask {
	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}
	policy := args.NotificationPolicy
	if policy == "" {
		policy = store.NotificationPolicyFailure
	}
	return store.ScheduledTask{Name: args.Name, ScriptID: args.ScriptID, NodeIDs: append([]string(nil), args.NodeIDs...), ScheduleType: args.ScheduleType, RunAt: args.RunAt, CronExpression: args.CronExpression, Timezone: args.Timezone, Enabled: enabled, TimeoutSeconds: args.TimeoutSeconds, NotificationPolicy: policy, NotificationChannels: []store.NotificationChannel{}}
}

func toolPlanSummary(call ValidatedToolCall) string {
	switch call.Definition.Name {
	case "create_scheduled_task":
		var args createScheduledTaskArguments
		if json.Unmarshal(call.Arguments, &args) == nil {
			if args.ScheduleType == store.ScheduleTypeOnce && args.RunAt != nil {
				return fmt.Sprintf("计划：创建一次性任务 %s，于 %s 执行，目标 %d 个节点", boundedString(args.Name, 128), args.RunAt.UTC().Format(time.RFC3339), len(args.NodeIDs))
			}
			return fmt.Sprintf("计划：创建周期任务 %s，Cron %s，目标 %d 个节点", boundedString(args.Name, 128), boundedString(args.CronExpression, 128), len(args.NodeIDs))
		}
	case "create_docker_container":
		var args createDockerContainerArguments
		if json.Unmarshal(call.Arguments, &args) == nil {
			name := args.Name
			if name == "" {
				name = "自动命名"
			}
			return fmt.Sprintf("计划：创建 Docker 容器 %s，镜像 %s，端口 %d 项、环境变量 %d 项、数据卷 %d 项，立即启动=%t", boundedString(name, 128), boundedString(args.Image, maxAIImageBytes), len(args.Ports), len(args.Environment), len(args.Mounts), args.Start)
		}
	case "create_k8s_deployment":
		var args createK8sDeploymentArguments
		if json.Unmarshal(call.Arguments, &args) == nil {
			return fmt.Sprintf("计划：创建 Deployment %s/%s，镜像 %s，副本 %d", boundedString(args.Namespace, 63), boundedString(args.Name, 63), boundedString(args.Image, maxAIImageBytes), args.Replicas)
		}
	}
	return "计划：变更目标运行状态"
}

func scheduledTaskTargetID(args createScheduledTaskArguments) string {
	return "script:" + strconv.FormatInt(args.ScriptID, 10) + "/task:" + strings.TrimSpace(args.Name)
}

func validateScheduledTaskTargets(ctx context.Context, r *Registry, args createScheduledTaskArguments) error {
	seen := make(map[string]struct{}, len(args.NodeIDs))
	for _, nodeID := range args.NodeIDs {
		if _, ok := seen[nodeID]; ok {
			return ErrInvalidArguments
		}
		seen[nodeID] = struct{}{}
		node, err := r.onlineNode(ctx, nodeID, true)
		if err != nil || node.ID == "" {
			return err
		}
		if !r.dependencies.AgentOps.TaskRunnerSupported(nodeID) {
			return ErrUnsupportedTool
		}
	}
	return nil
}

func safeAutomationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrInvalid) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
		return ErrInvalidArguments
	}
	return safeRemoteError(err)
}

func validDockerImage(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= maxAIImageBytes && !strings.ContainsAny(value, " \t\r\n;|&$<>\"'`\x00")
}

type missingCreationParametersError struct {
	ToolName string
	Fields   []string
}

func (e *missingCreationParametersError) Error() string {
	return "missing AI creation parameters"
}

func (e *missingCreationParametersError) Unwrap() error {
	return ErrInvalidArguments
}

func requireCreationParameters(raw json.RawMessage, toolName string, fields ...string) error {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return ErrInvalidArguments
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return ErrInvalidArguments
	}
	missing := make([]string, 0)
	for _, field := range fields {
		if creationParameterValueMissing(toolName, field, values[field], values) {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return &missingCreationParametersError{ToolName: toolName, Fields: missing}
	}
	return nil
}

func creationParameterValueMissing(toolName, field string, value json.RawMessage, values map[string]json.RawMessage) bool {
	if toolName == "create_docker_container" && field == "name" {
		var autoName bool
		if json.Unmarshal(values["auto_name"], &autoName) == nil && autoName {
			return false
		}
	}
	if toolName == "create_scheduled_task" {
		var scheduleType string
		_ = json.Unmarshal(values["schedule_type"], &scheduleType)
		if field == "run_at" && scheduleType != store.ScheduleTypeOnce {
			return false
		}
		if field == "cron_expression" && scheduleType != store.ScheduleTypeCron {
			return false
		}
	}
	if len(value) == 0 || strings.TrimSpace(string(value)) == "null" {
		return true
	}
	switch field {
	case "node_ids":
		var items []string
		return json.Unmarshal(value, &items) != nil || len(items) == 0
	case "script_id":
		var id int64
		return json.Unmarshal(value, &id) != nil || id <= 0
	case "replicas":
		var replicas int32
		return json.Unmarshal(value, &replicas) != nil || replicas < 1
	case "name", "cluster_id", "namespace", "image", "schedule_type", "run_at", "cron_expression", "timezone", "restart_policy", "network_mode", "notification_policy":
		var text string
		return json.Unmarshal(value, &text) != nil || strings.TrimSpace(text) == ""
	default:
		return false
	}
}

func missingCreationParameterFields(err error) []string {
	var missing *missingCreationParametersError
	if !errors.As(err, &missing) {
		return nil
	}
	return append([]string(nil), missing.Fields...)
}

func creationParameterPrompt(fields []string) string {
	labels := map[string]string{
		"node_id":             "目标节点",
		"image":               "镜像",
		"name":                "名称",
		"auto_name":           "是否使用自动命名",
		"restart_policy":      "重启策略",
		"network_mode":        "网络模式",
		"ports":               "端口映射（或确认不映射）",
		"environment":         "环境变量（或确认不配置）",
		"mounts":              "数据卷（或确认不配置）",
		"start":               "是否立即启动",
		"cluster_id":          "Kubernetes 集群",
		"namespace":           "命名空间",
		"replicas":            "副本数",
		"container_port":      "端口暴露选择（或确认不暴露）",
		"script_id":           "已有脚本",
		"node_ids":            "目标节点",
		"schedule_type":       "调度类型（一次性或周期）",
		"run_at":              "一次性执行时间",
		"cron_expression":     "Cron 表达式",
		"timezone":            "时区",
		"enabled":             "是否启用",
		"timeout_seconds":     "超时时间",
		"notification_policy": "通知策略",
	}
	seen := make(map[string]struct{}, len(fields))
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		if label, ok := labels[field]; ok {
			values = append(values, label)
		} else {
			values = append(values, "创建参数")
		}
	}
	if len(values) == 0 {
		return ""
	}
	return "创建操作还需要确认：" + strings.Join(values, "、") + "。请补充这些参数后再继续，当前未创建任何资源。"
}

func validateDockerPorts(ports []protocol.DockerContainerPort) error {
	for _, port := range ports {
		if port.HostPort == 0 || port.ContainerPort == 0 || !oneOf(port.Protocol, "tcp", "udp") {
			return ErrInvalidArguments
		}
	}
	return nil
}
