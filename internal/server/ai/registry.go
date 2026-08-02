package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/logbuffer"
	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
	"github.com/mizupanel/mizupanel/internal/version"
)

type Risk string

var (
	modelSensitiveHeaderPattern = regexp.MustCompile(`(?im)^(authorization|proxy-authorization|cookie|set-cookie)[ \t]*:[^\r\n]*`)
	modelNamedSecretPattern     = regexp.MustCompile(`(?i)\b(authorization|cookie|api[_-]?key|token|access[_-]?token|refresh[_-]?token|session[_-]?token|auth[_-]?token|password|passwd|secret|client[_-]?secret|webhook(?:_url)?)[ \t]*[:=][ \t]*("[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)
	modelBearerPattern          = regexp.MustCompile(`(?i)\bbearer[ \t]+[A-Za-z0-9._~+/=-]+`)
	modelEnvironmentPattern     = regexp.MustCompile(`\b([A-Z][A-Z0-9_]{2,})[ \t]*=[^\s,;]+`)
)

const (
	RiskRead    Risk = "read"
	RiskConfirm Risk = "confirm"
)

var (
	ErrUnknownTool      = errors.New("unknown AI tool")
	ErrInvalidArguments = errors.New("invalid AI tool arguments")
	ErrUnsupportedTool  = errors.New("AI tool unsupported")
)

type NodeOperations interface {
	Reboot(context.Context, string) (protocol.RebootResponse, error)
	AgentLogs(context.Context, string, int) (protocol.AgentLogsResponse, error)
	AgentUpgrade(context.Context, string, string) (protocol.AgentUpgradeResponse, error)
	ContainerStart(context.Context, string, string) (protocol.ContainerStartResponse, error)
	ContainerStop(context.Context, string, string) (protocol.ContainerStopResponse, error)
	ContainerRestart(context.Context, string, string) (protocol.ContainerRestartResponse, error)
	DockerComposeList(context.Context, string) (protocol.DockerComposeListResponse, error)
	DockerComposeAction(context.Context, string, string, string, string) (protocol.DockerComposeActionResponse, error)
	SystemdServiceList(context.Context, string) (protocol.SystemdServiceListResponse, error)
	SystemdServiceAction(context.Context, string, string, string, int) (protocol.SystemdServiceActionResponse, error)
	TaskRunnerSupported(string) bool
}

type AutomationRunner interface {
	RunManualScript(context.Context, int64, []string) (store.TaskRun, error)
}

type ApplicationServices interface {
	List(context.Context) ([]servicecenter.ServiceSummary, error)
}

type KubernetesClusters interface {
	ListClustersWithNodeInfo() ([]*k8s.PublicClusterWithNode, error)
}

type RegistryDependencies struct {
	Nodes      *store.NodeStore
	Metrics    *store.MetricStore
	Docker     *store.DockerSnapshotStore
	Alerts     *store.AlertStore
	Uptime     *store.UptimeStore
	Services   ApplicationServices
	ServerLogs *logbuffer.Buffer
	AgentOps   NodeOperations
	Automation AutomationRunner
	Tasks      *store.TaskStore
	Kubernetes KubernetesClusters
}

type ToolTarget struct {
	Type   string
	ID     string
	Name   string
	NodeID string
}

type ValidatedToolCall struct {
	Definition ToolDefinition
	Risk       Risk
	Arguments  json.RawMessage
	Target     ToolTarget
}

type SafeToolResult struct {
	Data    any
	Summary string
}

type registeredTool struct {
	definition ToolDefinition
	risk       Risk
	validate   func(context.Context, json.RawMessage) (json.RawMessage, ToolTarget, error)
	execute    func(context.Context, json.RawMessage) (SafeToolResult, error)
}

type Registry struct {
	dependencies RegistryDependencies
	tools        map[string]registeredTool
	ordered      []ToolDefinition
}

func NewRegistry(dependencies RegistryDependencies) *Registry {
	r := &Registry{dependencies: dependencies, tools: make(map[string]registeredTool)}
	r.registerReadTools()
	r.registerConfirmTools()
	return r
}

func (r *Registry) Definitions() []ToolDefinition {
	return append([]ToolDefinition(nil), r.ordered...)
}

func (r *Registry) Validate(ctx context.Context, name string, arguments json.RawMessage) (ValidatedToolCall, error) {
	tool, ok := r.tools[name]
	if !ok {
		return ValidatedToolCall{}, ErrUnknownTool
	}
	normalized, target, err := tool.validate(ctx, arguments)
	if err != nil {
		return ValidatedToolCall{}, err
	}
	return ValidatedToolCall{Definition: tool.definition, Risk: tool.risk, Arguments: normalized, Target: target}, nil
}

func (r *Registry) Execute(ctx context.Context, call ValidatedToolCall) (SafeToolResult, error) {
	tool, ok := r.tools[call.Definition.Name]
	if !ok {
		return SafeToolResult{}, ErrUnknownTool
	}
	return tool.execute(ctx, call.Arguments)
}

func (r *Registry) add(tool registeredTool) {
	r.tools[tool.definition.Name] = tool
	r.ordered = append(r.ordered, tool.definition)
}

func (r *Registry) registerReadTools() {
	r.add(registeredTool{
		definition: noArgumentDefinition("get_operational_overview", "Get a bounded overview of nodes, active alerts, uptime monitors, and application services."),
		risk:       RiskRead,
		validate:   noArguments,
		execute: func(ctx context.Context, _ json.RawMessage) (SafeToolResult, error) {
			nodesStore, err := r.requireNodes()
			if err != nil {
				return SafeToolResult{}, err
			}
			nodes, err := nodesStore.List(ctx)
			if err != nil {
				return SafeToolResult{}, err
			}
			overview := map[string]any{"nodes_total": len(nodes), "nodes_online": countOnline(nodes)}
			if r.dependencies.Alerts != nil {
				alerts, err := r.dependencies.Alerts.GetActiveAlertHistory()
				if err != nil {
					return SafeToolResult{}, err
				}
				overview["active_alerts"] = len(alerts)
			}
			if r.dependencies.Uptime != nil {
				monitors, err := r.dependencies.Uptime.ListMonitors(ctx)
				if err != nil {
					return SafeToolResult{}, err
				}
				overview["uptime_monitors"] = len(monitors)
				overview["uptime_failures"] = countMonitorFailures(monitors)
			}
			if r.dependencies.Services != nil {
				services, err := r.dependencies.Services.List(ctx)
				if err != nil {
					return SafeToolResult{}, err
				}
				overview["application_services"] = len(services)
				overview["unhealthy_services"] = countUnhealthyServices(services)
			}
			return SafeToolResult{Data: overview, Summary: "运维概览查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: noArgumentDefinition("list_nodes", "List nodes with safe identity, online state, platform, Agent version, and latest metrics."),
		risk:       RiskRead,
		validate:   noArguments,
		execute: func(ctx context.Context, _ json.RawMessage) (SafeToolResult, error) {
			nodesStore, err := r.requireNodes()
			if err != nil {
				return SafeToolResult{}, err
			}
			nodes, err := nodesStore.List(ctx)
			if err != nil {
				return SafeToolResult{}, err
			}
			result := make([]map[string]any, 0, len(nodes))
			for _, node := range nodes {
				item := map[string]any{"id": node.ID, "name": boundedString(node.Name, 128), "status": node.Status,
					"os": boundedString(node.OS, 64), "arch": boundedString(node.Arch, 64), "agent_version": boundedString(node.AgentVersion, 64)}
				if r.dependencies.Metrics != nil {
					metric, ok, err := r.dependencies.Metrics.Latest(ctx, node.ID)
					if err != nil {
						return SafeToolResult{}, err
					}
					if ok {
						item["metrics"] = metricProjection(metric)
					}
				}
				result = append(result, item)
			}
			return SafeToolResult{Data: map[string]any{"nodes": result}, Summary: "节点列表查询完成"}, nil
		},
	})

	type metricsArguments struct {
		NodeID       string `json:"node_id"`
		RangeMinutes int    `json:"range_minutes"`
	}
	r.add(registeredTool{
		definition: objectDefinition("get_node_metrics", "Get bounded current and historical aggregate metrics for one node.", map[string]any{
			"node_id":       map[string]any{"type": "string", "maxLength": 191},
			"range_minutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 1440},
		}, []string{"node_id"}),
		risk: RiskRead,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args metricsArguments
			if err := strictArguments(raw, &args); err != nil || args.RangeMinutes < 0 || args.RangeMinutes > 1440 {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			node, err := r.onlineNode(ctx, args.NodeID, false)
			if err != nil {
				return nil, ToolTarget{}, err
			}
			if args.RangeMinutes == 0 {
				args.RangeMinutes = 60
			}
			return normalizedArguments(args), ToolTarget{Type: "node", ID: node.ID, Name: node.Name, NodeID: node.ID}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args metricsArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.Metrics == nil {
				return SafeToolResult{}, ErrUnsupportedTool
			}
			to := time.Now().UTC()
			metrics, err := r.dependencies.Metrics.ListRange(ctx, args.NodeID, to.Add(-time.Duration(args.RangeMinutes)*time.Minute), to)
			if err != nil {
				return SafeToolResult{}, err
			}
			return SafeToolResult{Data: aggregateMetrics(metrics), Summary: "节点指标查询完成"}, nil
		},
	})

	type alertsArguments struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	r.add(registeredTool{
		definition: objectDefinition("list_alerts", "List active or recent alerts without notification secrets.", map[string]any{
			"status": map[string]any{"type": "string", "enum": []string{"active", "recent"}},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
		}, nil),
		risk: RiskRead,
		validate: func(_ context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args alertsArguments
			if err := strictArguments(raw, &args); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if args.Status == "" {
				args.Status = "active"
			}
			if args.Status != "active" && args.Status != "recent" {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if args.Limit == 0 {
				args.Limit = 20
			}
			if args.Limit < 1 || args.Limit > 50 {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return normalizedArguments(args), ToolTarget{Type: "alerts", ID: args.Status, Name: args.Status}, nil
		},
		execute: func(_ context.Context, raw json.RawMessage) (SafeToolResult, error) {
			if r.dependencies.Alerts == nil {
				return SafeToolResult{}, ErrUnsupportedTool
			}
			var args alertsArguments
			_ = json.Unmarshal(raw, &args)
			var alerts []store.AlertHistory
			var err error
			if args.Status == "active" {
				alerts, err = r.dependencies.Alerts.GetActiveAlertHistory()
			} else {
				alerts, err = r.dependencies.Alerts.GetAlertHistory("", args.Limit)
			}
			if err != nil {
				return SafeToolResult{}, err
			}
			if len(alerts) > args.Limit {
				alerts = alerts[:args.Limit]
			}
			items := make([]map[string]any, 0, len(alerts))
			for _, alert := range alerts {
				items = append(items, map[string]any{"id": alert.ID, "rule": boundedString(alert.RuleName, 128), "node_id": alert.NodeID, "node": boundedString(alert.NodeName, 128), "metric": alert.MetricField, "value": alert.MetricValue, "threshold": alert.Threshold, "triggered_at": alert.TriggeredAt, "active": alert.ResolvedAt == nil})
			}
			return SafeToolResult{Data: map[string]any{"alerts": items}, Summary: "告警查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: noArgumentDefinition("list_application_services", "List logical application services and their bounded health projection."),
		risk:       RiskRead,
		validate:   noArguments,
		execute: func(ctx context.Context, _ json.RawMessage) (SafeToolResult, error) {
			if r.dependencies.Services == nil {
				return SafeToolResult{}, ErrUnsupportedTool
			}
			services, err := r.dependencies.Services.List(ctx)
			if err != nil {
				return SafeToolResult{}, err
			}
			items := make([]map[string]any, 0, len(services))
			for _, service := range services {
				items = append(items, map[string]any{"id": service.ID, "name": boundedString(service.Name, 128), "health": service.Health, "first_reason": boundedString(service.FirstReason, 256), "resource_count": service.ResourceCount})
			}
			return SafeToolResult{Data: map[string]any{"services": items}, Summary: "应用服务查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: noArgumentDefinition("list_uptime_monitors", "List bounded HTTP and TCP uptime monitor status."),
		risk:       RiskRead,
		validate:   noArguments,
		execute: func(ctx context.Context, _ json.RawMessage) (SafeToolResult, error) {
			if r.dependencies.Uptime == nil {
				return SafeToolResult{}, ErrUnsupportedTool
			}
			monitors, err := r.dependencies.Uptime.ListMonitors(ctx)
			if err != nil {
				return SafeToolResult{}, err
			}
			items := make([]map[string]any, 0, len(monitors))
			for _, monitor := range monitors {
				items = append(items, map[string]any{"id": monitor.ID, "name": boundedString(monitor.Name, 128), "type": monitor.Type, "status": monitor.Status, "enabled": monitor.Enabled, "latency_ms": monitor.LastLatencyMS, "last_checked_at": monitor.LastCheckedAt})
			}
			return SafeToolResult{Data: map[string]any{"monitors": items}, Summary: "拨测状态查询完成"}, nil
		},
	})

	type logsArguments struct {
		Source      string `json:"source"`
		NodeID      string `json:"node_id"`
		ServiceName string `json:"service_name"`
		Lines       int    `json:"lines"`
	}
	r.add(registeredTool{
		definition: objectDefinition("get_log_snapshot", "Read one bounded server, Agent, or Systemd log snapshot. Follow and host file paths are not supported.", map[string]any{
			"source":       map[string]any{"type": "string", "enum": []string{"server", "agent", "systemd"}},
			"node_id":      map[string]any{"type": "string", "maxLength": 191},
			"service_name": map[string]any{"type": "string", "maxLength": 255},
			"lines":        map[string]any{"type": "integer", "minimum": 20, "maximum": 200},
		}, []string{"source"}),
		risk: RiskRead,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args logsArguments
			if err := strictArguments(raw, &args); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if args.Lines == 0 {
				args.Lines = 100
			}
			if args.Lines < 20 || args.Lines > 200 {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			switch args.Source {
			case "server":
				if args.NodeID != "" || args.ServiceName != "" {
					return nil, ToolTarget{}, ErrInvalidArguments
				}
				return normalizedArguments(args), ToolTarget{Type: "server_logs", ID: "current", Name: "Server"}, nil
			case "agent":
				if args.ServiceName != "" {
					return nil, ToolTarget{}, ErrInvalidArguments
				}
			case "systemd":
				if !validIdentifier(args.ServiceName, 255) {
					return nil, ToolTarget{}, ErrInvalidArguments
				}
			default:
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			node, err := r.onlineNode(ctx, args.NodeID, true)
			if err != nil {
				return nil, ToolTarget{}, err
			}
			return normalizedArguments(args), ToolTarget{Type: args.Source + "_logs", ID: args.ServiceName, Name: args.ServiceName, NodeID: node.ID}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args logsArguments
			_ = json.Unmarshal(raw, &args)
			var content string
			var truncated bool
			switch args.Source {
			case "server":
				if r.dependencies.ServerLogs == nil {
					return SafeToolResult{}, ErrUnsupportedTool
				}
				snapshot := r.dependencies.ServerLogs.Snapshot(args.Lines)
				content, truncated = snapshot.Content, snapshot.Truncated
			case "agent":
				if r.dependencies.AgentOps == nil {
					return SafeToolResult{}, ErrUnsupportedTool
				}
				response, err := r.dependencies.AgentOps.AgentLogs(ctx, args.NodeID, args.Lines)
				if err != nil || response.Error != "" {
					return SafeToolResult{}, safeRemoteError(err)
				}
				content, truncated = response.Content, response.Truncated
			case "systemd":
				if r.dependencies.AgentOps == nil {
					return SafeToolResult{}, ErrUnsupportedTool
				}
				response, err := r.dependencies.AgentOps.SystemdServiceAction(ctx, args.NodeID, args.ServiceName, "logs", args.Lines)
				if err != nil || !response.Success {
					return SafeToolResult{}, safeRemoteError(err)
				}
				content = response.Output
			}
			content = sanitizeModelText(content, 16*1024)
			return SafeToolResult{Data: map[string]any{"source": args.Source, "content": content, "truncated": truncated}, Summary: "日志快照查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: noArgumentDefinition("list_k8s_clusters", "List Kubernetes clusters with safe identity, online state, node count, and version."),
		risk:       RiskRead,
		validate:   noArguments,
		execute: func(ctx context.Context, _ json.RawMessage) (SafeToolResult, error) {
			if r.dependencies.Kubernetes == nil {
				return SafeToolResult{Data: map[string]any{"clusters": []any{}}, Summary: "未配置 Kubernetes 集群"}, nil
			}
			clusters, err := r.dependencies.Kubernetes.ListClustersWithNodeInfo()
			if err != nil {
				return SafeToolResult{}, err
			}
			result := make([]map[string]any, 0, len(clusters))
			for _, cluster := range clusters {
				result = append(result, map[string]any{
					"id":             cluster.ID,
					"name":           boundedString(cluster.Name, 128),
					"status":         cluster.Status,
					"version":        boundedString(cluster.Version, 64),
					"node_count":     cluster.NodeCount,
					"namespace_count": cluster.NamespaceCount,
					"node_name":      boundedString(cluster.NodeName, 128),
					"node_status":    cluster.NodeStatus,
				})
			}
			return SafeToolResult{Data: map[string]any{"clusters": result}, Summary: "Kubernetes 集群查询完成"}, nil
		},
	})
}

func (r *Registry) registerConfirmTools() {
	type nodeArguments struct {
		NodeID string `json:"node_id"`
	}
	addNodeOperation := func(name, description string, execute func(context.Context, string) (bool, error)) {
		r.add(registeredTool{
			definition: objectDefinition(name, description, map[string]any{"node_id": map[string]any{"type": "string", "maxLength": 191}}, []string{"node_id"}), risk: RiskConfirm,
			validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
				var args nodeArguments
				if err := strictArguments(raw, &args); err != nil {
					return nil, ToolTarget{}, ErrInvalidArguments
				}
				node, err := r.onlineNode(ctx, args.NodeID, true)
				if err != nil {
					return nil, ToolTarget{}, err
				}
				return normalizedArguments(args), ToolTarget{Type: "node", ID: node.ID, Name: node.Name, NodeID: node.ID}, nil
			},
			execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
				var args nodeArguments
				_ = json.Unmarshal(raw, &args)
				ok, err := execute(ctx, args.NodeID)
				if err != nil || !ok {
					return SafeToolResult{}, safeRemoteError(err)
				}
				return SafeToolResult{Data: map[string]any{"accepted": true}, Summary: "操作已接受"}, nil
			},
		})
	}
	addNodeOperation("reboot_node", "Reboot one existing online node after explicit administrator confirmation.", func(ctx context.Context, nodeID string) (bool, error) {
		if r.dependencies.AgentOps == nil {
			return false, ErrUnsupportedTool
		}
		response, err := r.dependencies.AgentOps.Reboot(ctx, nodeID)
		return response.Accepted, err
	})
	addNodeOperation("upgrade_agent", "Upgrade one online Agent to the current MizuPanel Server version after confirmation.", func(ctx context.Context, nodeID string) (bool, error) {
		if r.dependencies.AgentOps == nil {
			return false, ErrUnsupportedTool
		}
		response, err := r.dependencies.AgentOps.AgentUpgrade(ctx, nodeID, version.Current)
		return response.Accepted, err
	})

	type containerArguments struct {
		NodeID      string `json:"node_id"`
		ContainerID string `json:"container_id"`
		Action      string `json:"action"`
	}
	r.add(registeredTool{
		definition: objectDefinition("docker_container_action", "Start, stop, or restart one existing Docker container after confirmation.", map[string]any{"node_id": map[string]any{"type": "string", "maxLength": 191}, "container_id": map[string]any{"type": "string", "maxLength": 191}, "action": map[string]any{"type": "string", "enum": []string{"start", "stop", "restart"}}}, []string{"node_id", "container_id", "action"}), risk: RiskConfirm,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args containerArguments
			if err := strictArguments(raw, &args); err != nil || !validIdentifier(args.ContainerID, 191) || !oneOf(args.Action, "start", "stop", "restart") {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			node, err := r.onlineNode(ctx, args.NodeID, true)
			if err != nil {
				return nil, ToolTarget{}, err
			}
			if r.dependencies.Docker == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			snapshot, found, err := r.dependencies.Docker.Get(ctx, node.ID)
			if err != nil {
				return nil, ToolTarget{}, err
			}
			container, found := dockerContainerTarget(snapshot, found, args.ContainerID)
			if !found {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return normalizedArguments(args), ToolTarget{Type: "container", ID: containerID(container), Name: container.Name, NodeID: node.ID}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			if r.dependencies.AgentOps == nil {
				return SafeToolResult{}, ErrUnsupportedTool
			}
			var args containerArguments
			_ = json.Unmarshal(raw, &args)
			var success bool
			var err error
			switch args.Action {
			case "start":
				var response protocol.ContainerStartResponse
				response, err = r.dependencies.AgentOps.ContainerStart(ctx, args.NodeID, args.ContainerID)
				success = response.Success
			case "stop":
				var response protocol.ContainerStopResponse
				response, err = r.dependencies.AgentOps.ContainerStop(ctx, args.NodeID, args.ContainerID)
				success = response.Success
			case "restart":
				var response protocol.ContainerRestartResponse
				response, err = r.dependencies.AgentOps.ContainerRestart(ctx, args.NodeID, args.ContainerID)
				success = response.Success
			}
			if err != nil || !success {
				return SafeToolResult{}, safeRemoteError(err)
			}
			return SafeToolResult{Data: map[string]any{"success": true, "action": args.Action}, Summary: "容器操作成功"}, nil
		},
	})

	type composeArguments struct {
		NodeID      string `json:"node_id"`
		ProjectName string `json:"project_name"`
		ServiceName string `json:"service_name"`
		Action      string `json:"action"`
	}
	r.add(registeredTool{
		definition: objectDefinition("compose_service_action", "Start, stop, or restart one Compose project or service after confirmation. Delete, pull, build, and logs are unavailable.", map[string]any{"node_id": map[string]any{"type": "string", "maxLength": 191}, "project_name": map[string]any{"type": "string", "maxLength": 191}, "service_name": map[string]any{"type": "string", "maxLength": 191}, "action": map[string]any{"type": "string", "enum": []string{"up", "stop", "restart"}}}, []string{"node_id", "project_name", "action"}), risk: RiskConfirm,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args composeArguments
			if err := strictArguments(raw, &args); err != nil || !validIdentifier(args.ProjectName, 191) || (args.ServiceName != "" && !validIdentifier(args.ServiceName, 191)) || !oneOf(args.Action, "up", "stop", "restart") {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			node, err := r.onlineNode(ctx, args.NodeID, true)
			if err != nil {
				return nil, ToolTarget{}, err
			}
			if r.dependencies.AgentOps == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			projects, err := r.dependencies.AgentOps.DockerComposeList(ctx, args.NodeID)
			if err != nil || !projects.Success || !composeTargetExists(projects.Projects, args.ProjectName, args.ServiceName) {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			targetID := args.ProjectName
			if args.ServiceName != "" {
				targetID += "/" + args.ServiceName
			}
			return normalizedArguments(args), ToolTarget{Type: "compose_service", ID: targetID, Name: targetID, NodeID: node.ID}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args composeArguments
			_ = json.Unmarshal(raw, &args)
			response, err := r.dependencies.AgentOps.DockerComposeAction(ctx, args.NodeID, args.ProjectName, args.ServiceName, args.Action)
			if err != nil || !response.Success {
				return SafeToolResult{}, safeRemoteError(err)
			}
			return SafeToolResult{Data: map[string]any{"success": true, "action": args.Action}, Summary: "Compose 操作成功"}, nil
		},
	})

	type systemdArguments struct {
		NodeID      string `json:"node_id"`
		ServiceName string `json:"service_name"`
		Action      string `json:"action"`
	}
	r.add(registeredTool{
		definition: objectDefinition("systemd_service_action", "Start, stop, or restart one Systemd service after confirmation.", map[string]any{"node_id": map[string]any{"type": "string", "maxLength": 191}, "service_name": map[string]any{"type": "string", "maxLength": 255}, "action": map[string]any{"type": "string", "enum": []string{"start", "stop", "restart"}}}, []string{"node_id", "service_name", "action"}), risk: RiskConfirm,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args systemdArguments
			if err := strictArguments(raw, &args); err != nil || !validIdentifier(args.ServiceName, 255) || !oneOf(args.Action, "start", "stop", "restart") {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			node, err := r.onlineNode(ctx, args.NodeID, true)
			if err != nil {
				return nil, ToolTarget{}, err
			}
			if r.dependencies.AgentOps == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			list, err := r.dependencies.AgentOps.SystemdServiceList(ctx, args.NodeID)
			if err != nil || !list.Success || !systemdTargetExists(list.Services, args.ServiceName) {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return normalizedArguments(args), ToolTarget{Type: "systemd_service", ID: args.ServiceName, Name: args.ServiceName, NodeID: node.ID}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args systemdArguments
			_ = json.Unmarshal(raw, &args)
			response, err := r.dependencies.AgentOps.SystemdServiceAction(ctx, args.NodeID, args.ServiceName, args.Action, 0)
			if err != nil || !response.Success {
				return SafeToolResult{}, safeRemoteError(err)
			}
			return SafeToolResult{Data: map[string]any{"success": true, "action": args.Action}, Summary: "Systemd 操作成功"}, nil
		},
	})

	type scriptArguments struct {
		ScriptID int64    `json:"script_id"`
		NodeIDs  []string `json:"node_ids"`
	}
	r.add(registeredTool{
		definition: objectDefinition("run_saved_script", "Run an existing saved script on 1 to 100 explicit nodes after confirmation. Script text and arguments cannot be supplied.", map[string]any{"script_id": map[string]any{"type": "integer", "minimum": 1}, "node_ids": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": map[string]any{"type": "string", "maxLength": 191}, "uniqueItems": true}}, []string{"script_id", "node_ids"}), risk: RiskConfirm,
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args scriptArguments
			if err := strictArguments(raw, &args); err != nil || args.ScriptID <= 0 || len(args.NodeIDs) < 1 || len(args.NodeIDs) > store.MaxTaskNodes {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			seen := make(map[string]struct{}, len(args.NodeIDs))
			for _, nodeID := range args.NodeIDs {
				if _, exists := seen[nodeID]; exists {
					return nil, ToolTarget{}, ErrInvalidArguments
				}
				seen[nodeID] = struct{}{}
				if _, err := r.onlineNode(ctx, nodeID, true); err != nil {
					return nil, ToolTarget{}, err
				}
			}
			if r.dependencies.Automation == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			if r.dependencies.Tasks == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			script, err := r.dependencies.Tasks.GetScript(ctx, args.ScriptID)
			if err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if r.dependencies.AgentOps == nil {
				return nil, ToolTarget{}, ErrUnsupportedTool
			}
			for _, nodeID := range args.NodeIDs {
				if !r.dependencies.AgentOps.TaskRunnerSupported(nodeID) {
					return nil, ToolTarget{}, ErrUnsupportedTool
				}
			}
			return normalizedArguments(args), ToolTarget{Type: "automation_script", ID: strconv.FormatInt(args.ScriptID, 10), Name: script.Name}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args scriptArguments
			_ = json.Unmarshal(raw, &args)
			run, err := r.dependencies.Automation.RunManualScript(ctx, args.ScriptID, args.NodeIDs)
			if err != nil {
				return SafeToolResult{}, safeRemoteError(err)
			}
			return SafeToolResult{Data: map[string]any{"run_id": run.ID, "status": run.Status}, Summary: "脚本任务已创建"}, nil
		},
	})
}

func (r *Registry) requireNodes() (*store.NodeStore, error) {
	if r.dependencies.Nodes == nil {
		return nil, ErrUnsupportedTool
	}
	return r.dependencies.Nodes, nil
}

func (r *Registry) onlineNode(ctx context.Context, id string, requireOnline bool) (store.Node, error) {
	if r.dependencies.Nodes == nil || !validIdentifier(id, 191) {
		return store.Node{}, ErrInvalidArguments
	}
	node, err := r.dependencies.Nodes.Get(ctx, id)
	if err != nil {
		return store.Node{}, ErrInvalidArguments
	}
	if requireOnline && node.Status != "online" {
		return store.Node{}, ErrUnsupportedTool
	}
	return node, nil
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
func validIdentifier(value string, maxLength int) bool {
	value = strings.TrimSpace(value)
	return value != "" && utf8.ValidString(value) && len(value) <= maxLength && !strings.ContainsAny(value, "\x00\r\n")
}
func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
func boundedString(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maximum {
		return value
	}
	for maximum > 0 && !utf8.RuneStart(value[maximum]) {
		maximum--
	}
	return value[:maximum]
}

func sanitizeModelText(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "")
	value = modelSensitiveHeaderPattern.ReplaceAllString(value, "$1: [REDACTED]")
	value = modelBearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = modelNamedSecretPattern.ReplaceAllString(value, "$1=[REDACTED]")
	value = modelEnvironmentPattern.ReplaceAllString(value, "$1=[REDACTED]")
	return boundedString(value, maximum)
}
func countOnline(nodes []store.Node) int {
	count := 0
	for _, node := range nodes {
		if node.Status == "online" {
			count++
		}
	}
	return count
}
func countMonitorFailures(monitors []store.UptimeMonitor) int {
	count := 0
	for _, monitor := range monitors {
		if monitor.Status != "up" && monitor.Status != "pending" {
			count++
		}
	}
	return count
}
func countUnhealthyServices(services []servicecenter.ServiceSummary) int {
	count := 0
	for _, service := range services {
		if service.Health == servicecenter.HealthUnhealthy || service.Health == servicecenter.HealthDegraded {
			count++
		}
	}
	return count
}
func metricProjection(metric store.Metric) map[string]any {
	return map[string]any{"cpu_percent": round(metric.CPUUsage), "memory_percent": round(metric.MemoryUsage), "disk_percent": round(metric.DiskUsage), "load_1": round(metric.Load1), "collected_at": metric.CreatedAt}
}
func round(value float64) float64 { return math.Round(value*100) / 100 }
func aggregateMetrics(metrics []store.Metric) map[string]any {
	if len(metrics) == 0 {
		return map[string]any{"samples": 0}
	}
	var cpu, memory, disk, maxCPU float64
	for _, metric := range metrics {
		cpu += metric.CPUUsage
		memory += metric.MemoryUsage
		disk += metric.DiskUsage
		if metric.CPUUsage > maxCPU {
			maxCPU = metric.CPUUsage
		}
	}
	count := float64(len(metrics))
	return map[string]any{"samples": len(metrics), "latest": metricProjection(metrics[len(metrics)-1]), "average_cpu_percent": round(cpu / count), "average_memory_percent": round(memory / count), "average_disk_percent": round(disk / count), "max_cpu_percent": round(maxCPU)}
}
func safeRemoteError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("remote operation failed")
}
func composeTargetExists(projects []protocol.DockerComposeProject, projectName, serviceName string) bool {
	for _, project := range projects {
		if project.Name != projectName && project.ManagedProjectID != projectName {
			continue
		}
		if serviceName == "" {
			return true
		}
		for _, service := range project.Services {
			if service.Name == serviceName {
				return true
			}
		}
	}
	return false
}
func systemdTargetExists(services []protocol.SystemdService, name string) bool {
	for _, service := range services {
		if service.Name == name {
			return true
		}
	}
	return false
}

func dockerContainerTarget(snapshot protocol.DockerSnapshot, found bool, id string) (protocol.ContainerInfo, bool) {
	if !found || !snapshot.Available {
		return protocol.ContainerInfo{}, false
	}
	for _, container := range snapshot.Containers {
		if container.ID == id || container.FullID == id || container.Name == id {
			return container, true
		}
	}
	return protocol.ContainerInfo{}, false
}

func containerID(container protocol.ContainerInfo) string {
	if container.FullID != "" {
		return container.FullID
	}
	return container.ID
}
