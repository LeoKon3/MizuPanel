package ai

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	"github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

const (
	defaultAIReadLimit  = 20
	maxAIReadLimit      = 50
	maxDiagnosisSignals = 20
)

type nodeReadArguments struct {
	NodeID string `json:"node_id"`
}

type nodeLimitArguments struct {
	NodeID string `json:"node_id"`
	Limit  int    `json:"limit"`
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

func (r *Registry) registerPlatformReadTools() {
	r.add(registeredTool{
		definition: objectDefinition("get_docker_snapshot", "Read the latest bounded Docker container snapshot for one node.", map[string]any{
			"node_id": map[string]any{"type": "string", "maxLength": 191},
		}, []string{"node_id"}),
		risk:       RiskRead,
		capability: capabilityDocker,
		validate:   r.validateNodeRead,
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args nodeReadArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.Docker == nil {
				return unavailableToolResult("docker", "not_configured", "Docker 数据源未配置")
			}
			snapshot, found, err := r.dependencies.Docker.Get(ctx, args.NodeID)
			if err != nil {
				return SafeToolResult{}, err
			}
			if !found {
				return unavailableToolResult("docker", "no_snapshot", "暂无 Docker 快照")
			}
			if !snapshot.Available {
				return unavailableToolResult("docker", "unavailable", "Docker 数据源不可用")
			}
			containers := make([]any, 0, minAIReadLimit(len(snapshot.Containers), maxAIReadLimit))
			for _, container := range snapshot.Containers {
				containers = append(containers, projectContainer(container))
				if len(containers) == maxAIReadLimit {
					break
				}
			}
			return SafeToolResult{Data: map[string]any{
				"available":    snapshot.Available,
				"collected_at": snapshot.CollectedAt,
				"version":      boundedString(snapshot.Version, 64),
				"error":        sanitizeModelText(snapshot.Error, 256),
				"truncated":    len(snapshot.Containers) > len(containers),
				"containers":   containers,
			}, Summary: "Docker 快照查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("get_docker_resources", "Read bounded Docker images, volumes, networks, and disk usage for one online node.", map[string]any{
			"node_id": map[string]any{"type": "string", "maxLength": 191},
		}, []string{"node_id"}),
		risk:       RiskRead,
		capability: capabilityDockerAgent,
		validate:   r.validateOnlineNodeRead,
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args nodeReadArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.AgentOps == nil {
				return unavailableToolResult("docker_resources", "not_configured", "Docker 资源数据源未配置")
			}
			response, err := r.dependencies.AgentOps.DockerResourceList(ctx, args.NodeID)
			if err != nil {
				return SafeToolResult{}, safeRemoteError(err)
			}
			if !response.Success || !response.Supported {
				return unavailableToolResult("docker_resources", "unsupported", "Docker 资源查询不可用")
			}
			return SafeToolResult{Data: map[string]any{
				"available": true,
				"usage":     response.Usage,
				"images":    projectDockerImages(response.Images),
				"volumes":   projectDockerVolumes(response.Volumes),
				"networks":  projectDockerNetworks(response.Networks),
			}, Summary: "Docker 资源查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("list_compose_projects", "List bounded Docker Compose projects and services for one online node.", map[string]any{
			"node_id": map[string]any{"type": "string", "maxLength": 191},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": maxAIReadLimit},
		}, []string{"node_id"}),
		risk:       RiskRead,
		capability: capabilityCompose,
		validate:   r.validateNodeLimitOnline,
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args nodeLimitArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.AgentOps == nil {
				return unavailableToolResult("compose", "not_configured", "Compose 数据源未配置")
			}
			response, err := r.dependencies.AgentOps.DockerComposeList(ctx, args.NodeID)
			if err != nil {
				return SafeToolResult{}, safeRemoteError(err)
			}
			if !response.Success || !response.Supported {
				return unavailableToolResult("compose", "unsupported", "Compose 查询不可用")
			}
			projects := make([]any, 0, minAIReadLimit(len(response.Projects), args.Limit))
			for _, project := range response.Projects {
				services := make([]any, 0, minAIReadLimit(len(project.Services), maxAIReadLimit))
				for _, service := range project.Services {
					services = append(services, map[string]any{
						"name": boundedString(service.Name, 128), "state": boundedString(service.State, 64),
						"status": boundedString(service.Status, 128), "health": boundedString(service.Health, 64),
						"image": boundedString(service.Image, 256),
					})
					if len(services) == maxAIReadLimit {
						break
					}
				}
				projects = append(projects, map[string]any{
					"name": boundedString(project.Name, 128), "display_name": boundedString(project.DisplayName, 128),
					"status": boundedString(project.Status, 64), "services": services,
				})
				if len(projects) == args.Limit {
					break
				}
			}
			return SafeToolResult{Data: map[string]any{"available": true, "projects": projects, "truncated": len(response.Projects) > len(projects)}, Summary: "Compose 项目查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("list_node_processes", "List bounded top host processes for one node without command-line arguments.", map[string]any{
			"node_id": map[string]any{"type": "string", "maxLength": 191},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": maxAIReadLimit},
		}, []string{"node_id"}),
		risk:       RiskRead,
		capability: capabilityProcesses,
		validate:   r.validateNodeLimit,
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args nodeLimitArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.Processes == nil {
				return unavailableToolResult("processes", "not_configured", "进程数据源未配置")
			}
			snapshot, found, err := r.dependencies.Processes.Get(ctx, args.NodeID)
			if err != nil {
				return SafeToolResult{}, err
			}
			if !found {
				return unavailableToolResult("processes", "no_snapshot", "暂无进程快照")
			}
			if strings.TrimSpace(snapshot.Error) != "" {
				return unavailableToolResult("processes", "query_failed", "进程数据源查询失败")
			}
			processes := append([]protocol.ProcessInfo(nil), snapshot.Processes...)
			sort.SliceStable(processes, func(i, j int) bool { return processes[i].CPUUsage > processes[j].CPUUsage })
			truncated := len(processes) > args.Limit
			if truncated {
				processes = processes[:args.Limit]
			}
			items := make([]any, 0, len(processes))
			for _, process := range processes {
				items = append(items, map[string]any{
					"pid": process.PID, "ppid": process.PPID, "name": boundedString(process.Name, 128),
					"user": boundedString(process.User, 128), "status": boundedString(process.Status, 64),
					"cpu_percent": round(process.CPUUsage), "memory_rss": process.MemoryRSS,
					"memory_percent": round(process.MemoryUsage), "created_at": process.CreatedAt,
				})
			}
			return SafeToolResult{Data: map[string]any{"available": true, "collected_at": snapshot.CollectedAt, "error": sanitizeModelText(snapshot.Error, 256), "processes": items, "truncated": truncated}, Summary: "进程列表查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("list_systemd_services", "List bounded Systemd service state for one online node.", map[string]any{
			"node_id": map[string]any{"type": "string", "maxLength": 191},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": maxAIReadLimit},
		}, []string{"node_id"}),
		risk:       RiskRead,
		capability: capabilitySystemd,
		validate:   r.validateNodeLimitOnline,
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args nodeLimitArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.AgentOps == nil {
				return unavailableToolResult("systemd", "not_configured", "Systemd 数据源未配置")
			}
			response, err := r.dependencies.AgentOps.SystemdServiceList(ctx, args.NodeID)
			if err != nil {
				return SafeToolResult{}, safeRemoteError(err)
			}
			if !response.Success || !response.Supported {
				return unavailableToolResult("systemd", "unsupported", "Systemd 查询不可用")
			}
			services := make([]any, 0, minAIReadLimit(len(response.Services), args.Limit))
			for _, service := range response.Services {
				services = append(services, map[string]any{
					"name": boundedString(service.Name, 191), "description": boundedString(service.Description, 256),
					"load_state": boundedString(service.LoadState, 64), "active_state": boundedString(service.ActiveState, 64),
					"sub_state": boundedString(service.SubState, 64), "unit_file_state": boundedString(service.UnitFileState, 64),
				})
				if len(services) == args.Limit {
					break
				}
			}
			return SafeToolResult{Data: map[string]any{"available": true, "services": services, "truncated": len(response.Services) > len(services)}, Summary: "Systemd 服务查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("get_k8s_cluster_summary", "Read a bounded Kubernetes cluster resource summary.", map[string]any{
			"cluster_id": map[string]any{"type": "string", "maxLength": 191},
		}, []string{"cluster_id"}),
		risk:       RiskRead,
		capability: capabilityKubernetesTarget,
		validate:   r.validateK8sCluster,
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args struct {
				ClusterID string `json:"cluster_id"`
			}
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.Kubernetes == nil {
				return unavailableToolResult("kubernetes", "not_configured", "Kubernetes 数据源未配置")
			}
			summary, err := r.dependencies.Kubernetes.GetSummary(ctx, args.ClusterID)
			if err != nil {
				return SafeToolResult{}, safeRemoteError(err)
			}
			if summary == nil {
				return unavailableToolResult("kubernetes", "empty", "Kubernetes 集群没有摘要数据")
			}
			return SafeToolResult{Data: map[string]any{"available": true, "summary": summary}, Summary: "Kubernetes 集群摘要查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("list_k8s_resources", "List one bounded typed Kubernetes resource collection. Arbitrary API paths and manifests are unavailable.", map[string]any{
			"cluster_id": map[string]any{"type": "string", "maxLength": 191},
			"resource":   map[string]any{"type": "string", "enum": []string{"namespaces", "nodes", "pods", "deployments", "statefulsets", "daemonsets", "services", "ingresses"}},
			"namespace":  map[string]any{"type": "string", "maxLength": 191},
			"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": maxAIReadLimit},
		}, []string{"cluster_id", "resource"}),
		risk:       RiskRead,
		capability: capabilityKubernetesTarget,
		validate:   r.validateK8sResource,
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args k8sResourceArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.Kubernetes == nil {
				return unavailableToolResult("kubernetes", "not_configured", "Kubernetes 数据源未配置")
			}
			items, err := r.readK8sResource(ctx, args)
			if err != nil {
				return SafeToolResult{}, safeRemoteError(err)
			}
			if items == nil {
				items = []any{}
			}
			truncated := len(items) > args.Limit
			if truncated {
				items = items[:args.Limit]
			}
			return SafeToolResult{Data: map[string]any{"available": true, "resource": args.Resource, "namespace": args.Namespace, args.Resource: items, "truncated": truncated}, Summary: "Kubernetes 资源查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("list_automation_runs", "List bounded saved-script and scheduled-task run history without script content or output.", map[string]any{
			"status":  map[string]any{"type": "string", "enum": []string{"queued", "running", "success", "partial", "failed", "skipped", "interrupted"}},
			"node_id": map[string]any{"type": "string", "maxLength": 191},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": maxAIReadLimit},
		}, nil),
		risk:       RiskRead,
		capability: capabilityTaskHistory,
		validate: func(_ context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args automationRunsArguments
			if err := strictArguments(raw, &args); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if args.Limit == 0 {
				args.Limit = defaultAIReadLimit
			}
			if args.Limit < 1 || args.Limit > maxAIReadLimit || (args.Status != "" && !oneOf(args.Status, "queued", "running", "success", "partial", "failed", "skipped", "interrupted")) || (args.NodeID != "" && !validIdentifier(args.NodeID, 191)) {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return normalizedArguments(args), ToolTarget{Type: "automation_runs", ID: args.Status, Name: args.Status}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args automationRunsArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.Tasks == nil {
				return unavailableToolResult("automation", "not_configured", "任务数据源未配置")
			}
			page, err := r.dependencies.Tasks.ListRuns(ctx, store.RunFilter{Status: args.Status, NodeID: args.NodeID, Limit: args.Limit})
			if err != nil {
				return SafeToolResult{}, err
			}
			items := make([]any, 0, len(page.Runs))
			for _, run := range page.Runs {
				items = append(items, projectTaskRun(run))
			}
			return SafeToolResult{Data: map[string]any{"available": true, "runs": items, "has_more": page.NextBeforeID != nil}, Summary: "任务执行记录查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("list_audit_events", "List bounded MizuPanel audit events without source IPs, prompts, secrets, or raw payloads.", map[string]any{
			"module":  map[string]any{"type": "string", "maxLength": 64},
			"action":  map[string]any{"type": "string", "maxLength": 64},
			"node_id": map[string]any{"type": "string", "maxLength": 191},
			"result":  map[string]any{"type": "string", "enum": []string{"success", "failure", "accepted"}},
			"query":   map[string]any{"type": "string", "maxLength": 128},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": maxAIReadLimit},
		}, nil),
		risk:       RiskRead,
		capability: capabilityAudit,
		validate: func(_ context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args auditEventsArguments
			if err := strictArguments(raw, &args); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if args.Limit == 0 {
				args.Limit = defaultAIReadLimit
			}
			if args.Limit < 1 || args.Limit > maxAIReadLimit || (args.Module != "" && !validIdentifier(args.Module, 64)) || (args.Action != "" && !validIdentifier(args.Action, 64)) || (args.NodeID != "" && !validIdentifier(args.NodeID, 191)) || (args.Result != "" && !oneOf(args.Result, serveraudit.ResultSuccess, serveraudit.ResultFailure, serveraudit.ResultAccepted)) || len(args.Query) > serveraudit.MaxSearchLength {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return normalizedArguments(args), ToolTarget{Type: "audit_events", ID: args.Module, Name: args.Action}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args auditEventsArguments
			_ = json.Unmarshal(raw, &args)
			if r.dependencies.Audit == nil {
				return unavailableToolResult("audit", "not_configured", "审计数据源未配置")
			}
			page, err := r.dependencies.Audit.List(ctx, serveraudit.Filter{Module: args.Module, Action: args.Action, NodeID: args.NodeID, Result: args.Result, Query: args.Query, Limit: args.Limit})
			if err != nil {
				return SafeToolResult{}, err
			}
			items := make([]any, 0, len(page.Events))
			for _, event := range page.Events {
				items = append(items, map[string]any{
					"id": event.ID, "created_at": event.CreatedAt, "module": event.Module, "action": event.Action,
					"target_type": event.TargetType, "target_id": boundedString(event.TargetID, 256),
					"target_name": boundedString(event.TargetName, 256), "node_id": boundedString(event.NodeID, 191),
					"result": event.Result, "duration_ms": event.DurationMS, "summary": boundedString(event.Summary, 64),
				})
			}
			return SafeToolResult{Data: map[string]any{"available": true, "events": items, "has_more": page.NextBeforeID != nil}, Summary: "审计事件查询完成"}, nil
		},
	})

	r.add(registeredTool{
		definition: objectDefinition("diagnose_node", "Combine bounded node metrics, processes, Docker, Compose, and Systemd signals to explain a node health concern.", map[string]any{
			"node_id": map[string]any{"type": "string", "maxLength": 191},
		}, []string{"node_id"}),
		risk:       RiskRead,
		capability: capabilityOnlineNode,
		validate:   r.validateNodeRead,
		execute:    r.executeNodeDiagnosis,
	})
	r.registerIncidentDiagnosisTool()
}

func (r *Registry) validateNodeRead(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
	var args nodeReadArguments
	if err := strictArguments(raw, &args); err != nil || !validIdentifier(args.NodeID, 191) {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	node, err := r.onlineNode(ctx, args.NodeID, false)
	if err != nil {
		return nil, ToolTarget{}, err
	}
	return normalizedArguments(args), ToolTarget{Type: "node", ID: node.ID, Name: node.Name, NodeID: node.ID}, nil
}

func (r *Registry) validateOnlineNodeRead(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
	arguments, target, err := r.validateNodeRead(ctx, raw)
	if err != nil {
		return nil, ToolTarget{}, err
	}
	if _, err := r.onlineNode(ctx, target.ID, true); err != nil {
		return nil, ToolTarget{}, err
	}
	return arguments, target, nil
}

func (r *Registry) validateNodeLimit(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
	var args nodeLimitArguments
	if err := strictArguments(raw, &args); err != nil || !validIdentifier(args.NodeID, 191) {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	args.Limit = normalizedReadLimit(args.Limit)
	if args.Limit == 0 {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	node, err := r.onlineNode(ctx, args.NodeID, false)
	if err != nil {
		return nil, ToolTarget{}, err
	}
	return normalizedArguments(args), ToolTarget{Type: "node", ID: node.ID, Name: node.Name, NodeID: node.ID}, nil
}

func (r *Registry) validateNodeLimitOnline(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
	arguments, target, err := r.validateNodeLimit(ctx, raw)
	if err != nil {
		return nil, ToolTarget{}, err
	}
	if _, err := r.onlineNode(ctx, target.ID, true); err != nil {
		return nil, ToolTarget{}, err
	}
	return arguments, target, nil
}

func (r *Registry) validateK8sCluster(_ context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
	var args struct {
		ClusterID string `json:"cluster_id"`
	}
	if err := strictArguments(raw, &args); err != nil || !validIdentifier(args.ClusterID, 191) {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	if r.dependencies.Kubernetes == nil {
		return normalizedArguments(args), ToolTarget{Type: "k8s_cluster", ID: args.ClusterID}, nil
	}
	cluster, err := r.k8sCluster(args.ClusterID)
	if err != nil {
		return nil, ToolTarget{}, err
	}
	return normalizedArguments(args), ToolTarget{Type: "k8s_cluster", ID: cluster.ID, Name: cluster.Name, NodeID: cluster.NodeID}, nil
}

func (r *Registry) validateK8sResource(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
	var args k8sResourceArguments
	if err := strictArguments(raw, &args); err != nil || !validIdentifier(args.ClusterID, 191) || !oneOf(args.Resource, "namespaces", "nodes", "pods", "deployments", "statefulsets", "daemonsets", "services", "ingresses") || (args.Namespace != "" && !validIdentifier(args.Namespace, 191)) || (args.Namespace != "" && oneOf(args.Resource, "namespaces", "nodes")) {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	args.Limit = normalizedReadLimit(args.Limit)
	if args.Limit == 0 {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	if r.dependencies.Kubernetes == nil {
		return normalizedArguments(args), ToolTarget{Type: "k8s_resource", ID: args.ClusterID + "/" + args.Resource, Name: args.Resource}, nil
	}
	cluster, err := r.k8sCluster(args.ClusterID)
	if err != nil {
		return nil, ToolTarget{}, err
	}
	return normalizedArguments(args), ToolTarget{Type: "k8s_resource", ID: cluster.ID + "/" + args.Resource, Name: args.Resource, NodeID: cluster.NodeID}, nil
}

func (r *Registry) k8sCluster(id string) (*k8s.PublicClusterWithNode, error) {
	if r.dependencies.Kubernetes == nil {
		return nil, ErrUnsupportedTool
	}
	cluster, err := r.dependencies.Kubernetes.GetClusterWithNodeInfo(id)
	if err != nil || cluster == nil {
		return nil, ErrInvalidArguments
	}
	return cluster, nil
}

func normalizedReadLimit(limit int) int {
	if limit == 0 {
		return defaultAIReadLimit
	}
	if limit < 1 || limit > maxAIReadLimit {
		return 0
	}
	return limit
}

func minAIReadLimit(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func unavailableToolResult(source, reason, summary string) (SafeToolResult, error) {
	return SafeToolResult{Data: map[string]any{"available": false, "source": source, "reason": reason}, Summary: summary}, nil
}

func projectContainer(container protocol.ContainerInfo) map[string]any {
	return map[string]any{
		"id": boundedString(container.ID, 96), "name": boundedString(container.Name, 128), "image": boundedString(container.Image, 256),
		"state": boundedString(container.State, 64), "status": boundedString(container.Status, 128), "restart_count": container.RestartCount,
		"cpu_percent": round(container.CPUUsage), "memory_usage": container.MemoryUsage, "memory_limit": container.MemoryLimit,
		"memory_percent": round(container.MemoryPercent),
	}
}

func projectDockerImages(images []protocol.DockerImage) []any {
	items := make([]any, 0, minAIReadLimit(len(images), maxAIReadLimit))
	for _, image := range images {
		items = append(items, map[string]any{"id": boundedString(image.ID, 96), "tags": boundedStrings(image.Tags, 8, 256), "size": image.Size, "containers": image.Containers})
		if len(items) == maxAIReadLimit {
			break
		}
	}
	return items
}

func projectDockerVolumes(volumes []protocol.DockerVolume) []any {
	items := make([]any, 0, minAIReadLimit(len(volumes), maxAIReadLimit))
	for _, volume := range volumes {
		items = append(items, map[string]any{"name": boundedString(volume.Name, 128), "driver": boundedString(volume.Driver, 64), "scope": boundedString(volume.Scope, 64), "compose_project": boundedString(volume.ComposeProject, 128), "size": volume.Size, "ref_count": volume.RefCount})
		if len(items) == maxAIReadLimit {
			break
		}
	}
	return items
}

func projectDockerNetworks(networks []protocol.DockerNetwork) []any {
	items := make([]any, 0, minAIReadLimit(len(networks), maxAIReadLimit))
	for _, network := range networks {
		items = append(items, map[string]any{"id": boundedString(network.ID, 96), "name": boundedString(network.Name, 128), "driver": boundedString(network.Driver, 64), "scope": boundedString(network.Scope, 64), "subnets": boundedStrings(network.Subnets, 8, 128), "internal": network.Internal, "ingress": network.Ingress, "protected": network.Protected})
		if len(items) == maxAIReadLimit {
			break
		}
	}
	return items
}

func boundedStrings(values []string, limit, maximum int) []string {
	if len(values) > limit {
		values = values[:limit]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, boundedString(value, maximum))
	}
	return result
}

func projectTaskRun(run store.TaskRun) map[string]any {
	return map[string]any{
		"id": run.ID, "task_name": boundedString(run.TaskName, 128), "script_name": boundedString(run.ScriptName, 128),
		"trigger": run.Trigger, "status": run.Status, "total_targets": run.TotalTargets, "completed_targets": run.CompletedTargets,
		"success_targets": run.SuccessTargets, "failed_targets": run.FailedTargets,
		"started_at": run.StartedAt, "completed_at": run.CompletedAt, "created_at": run.CreatedAt,
	}
}

func (r *Registry) readK8sResource(ctx context.Context, args k8sResourceArguments) ([]any, error) {
	switch args.Resource {
	case "namespaces":
		values, err := r.dependencies.Kubernetes.GetNamespaces(ctx, args.ClusterID)
		return projectK8sNamespaces(values), err
	case "nodes":
		values, err := r.dependencies.Kubernetes.GetNodes(ctx, args.ClusterID)
		return projectK8sNodes(values), err
	case "pods":
		values, err := r.dependencies.Kubernetes.GetPods(ctx, args.ClusterID, args.Namespace)
		return projectK8sPods(values), err
	case "deployments":
		values, err := r.dependencies.Kubernetes.GetDeployments(ctx, args.ClusterID, args.Namespace)
		return projectK8sDeployments(values), err
	case "statefulsets":
		values, err := r.dependencies.Kubernetes.GetStatefulSets(ctx, args.ClusterID, args.Namespace)
		return projectK8sStatefulSets(values), err
	case "daemonsets":
		values, err := r.dependencies.Kubernetes.GetDaemonSets(ctx, args.ClusterID, args.Namespace)
		return projectK8sDaemonSets(values), err
	case "services":
		values, err := r.dependencies.Kubernetes.GetServices(ctx, args.ClusterID, args.Namespace)
		return projectK8sServices(values), err
	case "ingresses":
		values, err := r.dependencies.Kubernetes.GetIngresses(ctx, args.ClusterID, args.Namespace)
		return projectK8sIngresses(values), err
	default:
		return nil, ErrInvalidArguments
	}
}

func projectK8sNamespaces(values []protocol.K8sNamespace) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]any{"name": boundedString(value.Name, 191), "status": boundedString(value.Status, 64), "age": boundedString(value.Age, 64)})
	}
	return items
}

func projectK8sNodes(values []protocol.K8sNode) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]any{"name": boundedString(value.Name, 191), "status": boundedString(value.Status, 64), "roles": boundedString(value.Roles, 128), "version": boundedString(value.Version, 64), "internal_ip": boundedString(value.InternalIP, 64), "pod_capacity": value.PodCapacity, "pod_allocatable": value.PodAllocatable})
	}
	return items
}

func projectK8sPods(values []protocol.K8sPod) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]any{"name": boundedString(value.Name, 191), "namespace": boundedString(value.Namespace, 191), "status": boundedString(value.Status, 64), "ready": boundedString(value.Ready, 32), "restarts": value.Restarts, "age": boundedString(value.Age, 64), "node": boundedString(value.Node, 191), "workload_kind": boundedString(value.WorkloadKind, 64), "workload_name": boundedString(value.WorkloadName, 191), "metrics_available": value.MetricsAvailable, "cpu_usage_milli": value.CPUUsageMilli, "memory_usage_bytes": value.MemoryUsageBytes})
	}
	return items
}

func projectK8sDeployments(values []protocol.K8sDeployment) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]any{"name": boundedString(value.Name, 191), "namespace": boundedString(value.Namespace, 191), "ready": boundedString(value.Ready, 32), "up_to_date": value.UpToDate, "available": value.Available, "age": boundedString(value.Age, 64)})
	}
	return items
}

func projectK8sStatefulSets(values []protocol.K8sStatefulSet) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]any{"name": boundedString(value.Name, 191), "namespace": boundedString(value.Namespace, 191), "ready": boundedString(value.Ready, 32), "service_name": boundedString(value.ServiceName, 191), "age": boundedString(value.Age, 64)})
	}
	return items
}

func projectK8sDaemonSets(values []protocol.K8sDaemonSet) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]any{"name": boundedString(value.Name, 191), "namespace": boundedString(value.Namespace, 191), "desired": value.Desired, "current": value.Current, "ready": value.Ready, "available": value.Available, "age": boundedString(value.Age, 64)})
	}
	return items
}

func projectK8sServices(values []protocol.K8sService) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]any{"name": boundedString(value.Name, 191), "namespace": boundedString(value.Namespace, 191), "type": boundedString(value.Type, 64), "cluster_ip": boundedString(value.ClusterIP, 64), "external_ip": boundedString(value.ExternalIP, 128), "ports": boundedString(value.Ports, 256), "age": boundedString(value.Age, 64)})
	}
	return items
}

func projectK8sIngresses(values []protocol.K8sIngress) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]any{"name": boundedString(value.Name, 191), "namespace": boundedString(value.Namespace, 191), "class": boundedString(value.Class, 64), "hosts": boundedString(value.Hosts, 256), "address": boundedString(value.Address, 128), "ports": boundedString(value.Ports, 128), "age": boundedString(value.Age, 64)})
	}
	return items
}

func (r *Registry) executeNodeDiagnosis(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
	var args nodeReadArguments
	_ = json.Unmarshal(raw, &args)
	node, err := r.onlineNode(ctx, args.NodeID, false)
	if err != nil {
		return SafeToolResult{}, err
	}
	sources := map[string]any{}
	signals := make([]any, 0, 8)
	addSignal := func(kind, severity, message string, value any) {
		if len(signals) >= maxDiagnosisSignals {
			return
		}
		signals = append(signals, map[string]any{"kind": kind, "severity": severity, "message": message, "value": value})
	}

	if r.dependencies.Metrics == nil {
		sources["metrics"] = map[string]any{"available": false, "reason": "not_configured"}
	} else if metric, found, metricErr := r.dependencies.Metrics.Latest(ctx, node.ID); metricErr != nil {
		sources["metrics"] = map[string]any{"available": false, "reason": "query_failed"}
	} else if !found {
		sources["metrics"] = map[string]any{"available": false, "reason": "no_snapshot"}
	} else {
		sources["metrics"] = map[string]any{"available": true, "latest": metricProjection(metric), "load_5": round(metric.Load5), "load_15": round(metric.Load15)}
		if metric.CPUUsage >= 80 {
			addSignal("high_cpu", "warning", "CPU 使用率较高", round(metric.CPUUsage))
		}
		if metric.MemoryUsage >= 80 {
			addSignal("high_memory", "warning", "内存使用率较高", round(metric.MemoryUsage))
		}
		if metric.DiskUsage >= 90 {
			addSignal("high_disk", "warning", "磁盘使用率较高", round(metric.DiskUsage))
		}
		if metric.Load1 > float64(maxInt(metric.CPUCores, 1))*2 {
			addSignal("high_load", "warning", "系统负载较高", round(metric.Load1))
		}
	}

	if r.dependencies.Processes == nil {
		sources["processes"] = map[string]any{"available": false, "reason": "not_configured"}
	} else if snapshot, found, processErr := r.dependencies.Processes.Get(ctx, node.ID); processErr != nil {
		sources["processes"] = map[string]any{"available": false, "reason": "query_failed"}
	} else if !found {
		sources["processes"] = map[string]any{"available": false, "reason": "no_snapshot"}
	} else if strings.TrimSpace(snapshot.Error) != "" {
		sources["processes"] = map[string]any{"available": false, "reason": "query_failed"}
	} else {
		processes := append([]protocol.ProcessInfo(nil), snapshot.Processes...)
		sort.SliceStable(processes, func(i, j int) bool { return processes[i].CPUUsage > processes[j].CPUUsage })
		top := make([]any, 0, minAIReadLimit(len(processes), 5))
		for _, process := range processes {
			top = append(top, map[string]any{"pid": process.PID, "name": boundedString(process.Name, 128), "cpu_percent": round(process.CPUUsage), "memory_percent": round(process.MemoryUsage), "memory_rss": process.MemoryRSS})
			if process.CPUUsage >= 50 {
				addSignal("top_process_cpu", "warning", "进程 CPU 占用较高", map[string]any{"pid": process.PID, "name": boundedString(process.Name, 128), "cpu_percent": round(process.CPUUsage)})
			}
			if len(top) == 5 {
				break
			}
		}
		sources["processes"] = map[string]any{"available": true, "collected_at": snapshot.CollectedAt, "top": top}
	}

	if r.dependencies.Docker == nil {
		sources["docker"] = map[string]any{"available": false, "reason": "not_configured"}
	} else if snapshot, found, dockerErr := r.dependencies.Docker.Get(ctx, node.ID); dockerErr != nil {
		sources["docker"] = map[string]any{"available": false, "reason": "query_failed"}
	} else if !found || !snapshot.Available {
		sources["docker"] = map[string]any{"available": false, "reason": "unavailable"}
	} else {
		running := 0
		for _, container := range snapshot.Containers {
			if strings.EqualFold(container.State, "running") {
				running++
			} else {
				addSignal("container_not_running", "warning", "存在未运行的容器", map[string]any{"name": boundedString(container.Name, 128), "state": boundedString(container.State, 64)})
			}
		}
		sources["docker"] = map[string]any{"available": true, "collected_at": snapshot.CollectedAt, "containers": len(snapshot.Containers), "running": running}
	}

	if r.dependencies.AgentOps == nil || node.Status != "online" {
		sources["compose"] = map[string]any{"available": false, "reason": "node_offline"}
		sources["systemd"] = map[string]any{"available": false, "reason": "node_offline"}
	} else {
		if response, remoteErr := r.dependencies.AgentOps.DockerComposeList(ctx, node.ID); remoteErr != nil || !response.Success || !response.Supported {
			sources["compose"] = map[string]any{"available": false, "reason": "unavailable"}
		} else {
			unhealthy := 0
			for _, project := range response.Projects {
				for _, service := range project.Services {
					if service.Health != "" && !strings.EqualFold(service.Health, "healthy") {
						unhealthy++
						addSignal("compose_service_unhealthy", "warning", "Compose 服务健康状态异常", map[string]any{"project": boundedString(project.Name, 128), "service": boundedString(service.Name, 128), "health": boundedString(service.Health, 64)})
					}
				}
			}
			sources["compose"] = map[string]any{"available": true, "projects": len(response.Projects), "unhealthy_services": unhealthy}
		}
		if response, remoteErr := r.dependencies.AgentOps.SystemdServiceList(ctx, node.ID); remoteErr != nil || !response.Success || !response.Supported {
			sources["systemd"] = map[string]any{"available": false, "reason": "unavailable"}
		} else {
			failed := 0
			for _, service := range response.Services {
				if strings.EqualFold(service.ActiveState, "failed") || strings.EqualFold(service.SubState, "failed") {
					failed++
					if failed <= 5 {
						addSignal("systemd_service_failed", "warning", "Systemd 服务处于失败状态", map[string]any{"name": boundedString(service.Name, 191), "active_state": boundedString(service.ActiveState, 64), "sub_state": boundedString(service.SubState, 64)})
					}
				}
			}
			sources["systemd"] = map[string]any{"available": true, "services": len(response.Services), "failed_services": failed}
		}
	}

	return SafeToolResult{Data: map[string]any{"node": map[string]any{"id": node.ID, "name": boundedString(node.Name, 128), "status": node.Status}, "sources": sources, "signals": signals}, Summary: "节点诊断完成"}, nil
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
