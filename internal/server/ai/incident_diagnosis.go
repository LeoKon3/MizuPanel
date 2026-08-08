package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

const (
	defaultIncidentWindowMinutes = 60
	minIncidentWindowMinutes     = 5
	maxIncidentWindowMinutes     = 1440
	maxIncidentEvidence          = 40
	maxIncidentNodes             = 20
	incidentSourceTimeout        = 750 * time.Millisecond
	incidentOverallTimeout       = 4 * time.Second
)

type incidentDiagnosisArguments struct {
	ScopeType     string `json:"scope_type"`
	ScopeID       string `json:"scope_id"`
	WindowMinutes int    `json:"window_minutes"`
}

type incidentSourceStatus struct {
	Available     bool   `json:"available"`
	Reason        string `json:"reason,omitempty"`
	EvidenceCount int    `json:"evidence_count"`
}

type incidentEvidence struct {
	Kind         string `json:"kind"`
	Severity     string `json:"severity"`
	Confidence   string `json:"confidence"`
	ObservedAt   string `json:"observed_at,omitempty"`
	Summary      string `json:"summary"`
	Source       string `json:"source"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	RouteKey     string `json:"route_key,omitempty"`
}

func (r *Registry) registerIncidentDiagnosisTool() {
	r.add(registeredTool{
		definition: objectDefinition("diagnose_incident", "Diagnose a bounded platform, node, Kubernetes cluster, or application-service incident using safe read-only evidence.", map[string]any{
			"scope_type":     map[string]any{"type": "string", "enum": []string{"platform", "node", "k8s_cluster", "application_service"}},
			"scope_id":       map[string]any{"type": "string", "maxLength": 191},
			"window_minutes": map[string]any{"type": "integer", "minimum": minIncidentWindowMinutes, "maximum": maxIncidentWindowMinutes},
		}, []string{"scope_type"}),
		risk:       RiskRead,
		capability: capabilityIncidentDiagnosis,
		validate:   r.validateIncidentDiagnosis,
		execute:    r.executeIncidentDiagnosis,
	})
}

func (r *Registry) validateIncidentDiagnosis(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
	var args incidentDiagnosisArguments
	if err := strictArguments(raw, &args); err != nil {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	args.ScopeType = strings.TrimSpace(args.ScopeType)
	args.ScopeID = strings.TrimSpace(args.ScopeID)
	if !oneOf(args.ScopeType, "platform", "node", "k8s_cluster", "application_service") {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	if args.WindowMinutes == 0 {
		args.WindowMinutes = defaultIncidentWindowMinutes
	}
	if args.WindowMinutes < minIncidentWindowMinutes || args.WindowMinutes > maxIncidentWindowMinutes {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	if args.ScopeType == "platform" {
		if args.ScopeID != "" {
			return nil, ToolTarget{}, ErrInvalidArguments
		}
		return normalizedArguments(args), ToolTarget{Type: "platform", ID: "platform", Name: "MizuPanel"}, nil
	}
	if !validIdentifier(args.ScopeID, 191) {
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	switch args.ScopeType {
	case "node":
		node, err := r.onlineNode(ctx, args.ScopeID, false)
		if err != nil {
			return nil, ToolTarget{}, err
		}
		return normalizedArguments(args), ToolTarget{Type: "node", ID: node.ID, Name: node.Name, NodeID: node.ID}, nil
	case "k8s_cluster":
		cluster, err := r.k8sCluster(args.ScopeID)
		if err != nil {
			return nil, ToolTarget{}, err
		}
		return normalizedArguments(args), ToolTarget{Type: "k8s_cluster", ID: cluster.ID, Name: cluster.Name, NodeID: cluster.NodeID}, nil
	case "application_service":
		if r.dependencies.Services == nil {
			return nil, ToolTarget{}, ErrUnsupportedTool
		}
		services, err := r.dependencies.Services.List(ctx)
		if err != nil {
			return nil, ToolTarget{}, err
		}
		for _, service := range services {
			if service.ID == args.ScopeID {
				return normalizedArguments(args), ToolTarget{Type: "application_service", ID: service.ID, Name: service.Name}, nil
			}
		}
		return nil, ToolTarget{}, ErrInvalidArguments
	}
	return nil, ToolTarget{}, ErrInvalidArguments
}

func (r *Registry) executeIncidentDiagnosis(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
	var args incidentDiagnosisArguments
	if err := strictArguments(raw, &args); err != nil {
		return SafeToolResult{}, ErrInvalidArguments
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, incidentOverallTimeout)
	defer cancel()
	windowEnd := time.Now().UTC()
	windowStart := windowEnd.Add(-time.Duration(args.WindowMinutes) * time.Minute)

	sources := make(map[string]incidentSourceStatus)
	evidence := make([]incidentEvidence, 0, maxIncidentEvidence)
	addSource := func(name string, available bool, reason string, items []incidentEvidence) {
		if len(items) > maxIncidentEvidence {
			items = items[:maxIncidentEvidence]
		}
		if !available {
			if reason == "" {
				reason = "source_unavailable"
			}
		}
		sources[name] = incidentSourceStatus{Available: available, Reason: reason, EvidenceCount: len(items)}
		evidence = append(evidence, items...)
	}

	scope := map[string]any{"type": args.ScopeType}
	if args.ScopeID != "" {
		scope["id"] = args.ScopeID
	}

	switch args.ScopeType {
	case "platform":
		nodes, err := r.listIncidentNodes(deadlineCtx)
		if err != nil {
			addSource("nodes", false, incidentReason(err), nil)
		} else {
			addSource("nodes", true, "", r.nodeStatusEvidence(nodes))
			aggregated := make(map[string]*incidentSourceAggregate)
			aggregateSource := func(name string, available bool, reason string, items []incidentEvidence) {
				value := aggregated[name]
				if value == nil {
					value = &incidentSourceAggregate{}
					aggregated[name] = value
				}
				value.Attempts++
				if available {
					value.Successes++
				} else if value.Reason == "" {
					value.Reason = reason
				}
				value.Evidence = append(value.Evidence, items...)
			}
			for index, node := range nodes {
				if index == maxIncidentNodes {
					break
				}
				r.collectNodeSources(deadlineCtx, node, windowStart, windowEnd, aggregateSource)
			}
			names := make([]string, 0, len(aggregated))
			for name := range aggregated {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				value := aggregated[name]
				reason := value.Reason
				if value.Successes > 0 && value.Successes < value.Attempts {
					reason = "partial_failure"
				}
				addSource(name, value.Successes > 0, reason, value.Evidence)
			}
		}
		r.collectPlatformSources(deadlineCtx, addSource)
	case "node":
		node, err := r.dependencies.Nodes.Get(deadlineCtx, args.ScopeID)
		if err != nil {
			return SafeToolResult{}, err
		}
		addSource("nodes", true, "", r.nodeStatusEvidence([]store.Node{node}))
		r.collectNodeSources(deadlineCtx, node, windowStart, windowEnd, addSource)
	case "k8s_cluster":
		r.collectKubernetesSources(deadlineCtx, args.ScopeID, addSource)
	case "application_service":
		r.collectApplicationServiceSources(deadlineCtx, args.ScopeID, addSource)
	}

	evidence = rankIncidentEvidence(evidence, args.ScopeType, args.ScopeID)
	if len(evidence) > maxIncidentEvidence {
		evidence = evidence[:maxIncidentEvidence]
	}
	return SafeToolResult{Data: map[string]any{
		"scope":    scope,
		"window":   map[string]any{"minutes": args.WindowMinutes, "from": incidentObservedAt(windowStart), "to": incidentObservedAt(windowEnd), "bounded": true},
		"sources":  sources,
		"evidence": evidence,
	}, Summary: "事件诊断完成"}, nil
}

type incidentSourceAggregate struct {
	Attempts  int
	Successes int
	Reason    string
	Evidence  []incidentEvidence
}

func (r *Registry) listIncidentNodes(ctx context.Context) ([]store.Node, error) {
	if r.dependencies.Nodes == nil {
		return nil, ErrUnsupportedTool
	}
	return r.dependencies.Nodes.List(ctx)
}

func (r *Registry) nodeStatusEvidence(nodes []store.Node) []incidentEvidence {
	items := make([]incidentEvidence, 0, min(len(nodes), maxIncidentEvidence))
	for _, node := range nodes {
		if strings.EqualFold(node.Status, "online") {
			continue
		}
		observed := incidentObservedAt(node.LastSeenAt)
		items = append(items, incidentEvidence{Kind: "node_offline", Severity: "critical", Confidence: "high", ObservedAt: observed, Summary: "节点当前不在线", Source: "nodes", ResourceType: "node", ResourceID: node.ID, RouteKey: "node"})
		if len(items) == maxIncidentEvidence {
			break
		}
	}
	return items
}

func (r *Registry) collectNodeSources(ctx context.Context, node store.Node, windowStart, windowEnd time.Time, addSource func(string, bool, string, []incidentEvidence)) {
	collect := func(name string, fn func(context.Context) ([]incidentEvidence, error)) {
		sourceCtx, cancel := context.WithTimeout(ctx, incidentSourceTimeout)
		defer cancel()
		items, err := fn(sourceCtx)
		if err != nil {
			addSource(name, false, incidentReason(err), nil)
			return
		}
		addSource(name, true, "", items)
	}

	collect("metrics", func(sourceCtx context.Context) ([]incidentEvidence, error) {
		if r.dependencies.Metrics == nil {
			return nil, ErrUnsupportedTool
		}
		metrics, err := r.dependencies.Metrics.ListRange(sourceCtx, node.ID, windowStart, windowEnd)
		if err != nil {
			return nil, err
		}
		if len(metrics) == 0 {
			return nil, errIncidentNoSnapshot
		}
		latest := metrics[len(metrics)-1]
		items := make([]incidentEvidence, 0, 4)
		observed := incidentObservedAt(latest.CreatedAt)
		addMetric := func(kind, summary string, severity string) {
			items = append(items, incidentEvidence{Kind: kind, Severity: severity, Confidence: "high", ObservedAt: observed, Summary: summary, Source: "metrics", ResourceType: "node", ResourceID: node.ID, RouteKey: "node"})
		}
		if latest.CPUUsage >= 80 {
			addMetric("high_cpu", "节点 CPU 使用率较高", "warning")
		}
		if latest.MemoryUsage >= 80 {
			addMetric("high_memory", "节点内存使用率较高", "warning")
		}
		if latest.DiskUsage >= 90 {
			addMetric("high_disk", "节点磁盘使用率较高", "critical")
		}
		if latest.Load1 > float64(maxInt(latest.CPUCores, 1))*2 {
			addMetric("high_load", "节点系统负载较高", "warning")
		}
		return items, nil
	})

	collect("processes", func(sourceCtx context.Context) ([]incidentEvidence, error) {
		if r.dependencies.Processes == nil {
			return nil, ErrUnsupportedTool
		}
		snapshot, found, err := r.dependencies.Processes.Get(sourceCtx, node.ID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errIncidentNoSnapshot
		}
		if !incidentUnixInWindow(snapshot.CollectedAt, windowStart, windowEnd) {
			return nil, errIncidentOutsideWindow
		}
		if strings.TrimSpace(snapshot.Error) != "" {
			return nil, errIncidentQueryFailed
		}
		processes := append([]protocol.ProcessInfo(nil), snapshot.Processes...)
		sort.SliceStable(processes, func(i, j int) bool { return processes[i].CPUUsage > processes[j].CPUUsage })
		items := make([]incidentEvidence, 0, 5)
		for _, process := range processes {
			if process.CPUUsage < 50 {
				break
			}
			items = append(items, incidentEvidence{Kind: "high_process_cpu", Severity: "warning", Confidence: "medium", ObservedAt: incidentObservedAtUnix(snapshot.CollectedAt), Summary: incidentSafeSummary(fmt.Sprintf("进程 %s CPU 占用较高", process.Name)), Source: "processes", ResourceType: "node", ResourceID: node.ID, RouteKey: "node"})
			if len(items) == 5 {
				break
			}
		}
		return items, nil
	})

	collect("docker", func(sourceCtx context.Context) ([]incidentEvidence, error) {
		if r.dependencies.Docker == nil {
			return nil, ErrUnsupportedTool
		}
		snapshot, found, err := r.dependencies.Docker.Get(sourceCtx, node.ID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errIncidentNoSnapshot
		}
		if !incidentUnixInWindow(snapshot.CollectedAt, windowStart, windowEnd) {
			return nil, errIncidentOutsideWindow
		}
		if !snapshot.Available {
			return nil, errIncidentUnavailable
		}
		items := make([]incidentEvidence, 0, 5)
		for _, container := range snapshot.Containers {
			if strings.EqualFold(container.State, "running") {
				continue
			}
			items = append(items, incidentEvidence{Kind: "container_not_running", Severity: "warning", Confidence: "high", ObservedAt: incidentObservedAtUnix(snapshot.CollectedAt), Summary: incidentSafeSummary(fmt.Sprintf("容器 %s 未处于运行状态", container.Name)), Source: "docker", ResourceType: "node", ResourceID: node.ID, RouteKey: "node"})
			if len(items) == 5 {
				break
			}
		}
		return items, nil
	})

	collect("alerts", func(sourceCtx context.Context) ([]incidentEvidence, error) {
		if r.dependencies.Alerts == nil {
			return nil, ErrUnsupportedTool
		}
		alerts, err := r.dependencies.Alerts.GetActiveAlertHistory()
		if err != nil {
			return nil, err
		}
		items := make([]incidentEvidence, 0, 5)
		for _, alert := range alerts {
			if alert.NodeID != node.ID || alert.TriggeredAt.Before(windowStart) || alert.TriggeredAt.After(windowEnd) {
				continue
			}
			items = append(items, incidentEvidence{Kind: "active_alert", Severity: "critical", Confidence: "high", ObservedAt: incidentObservedAt(alert.TriggeredAt), Summary: incidentSafeSummary(fmt.Sprintf("存在活动告警：%s", alert.RuleName)), Source: "alerts", ResourceType: "node", ResourceID: node.ID, RouteKey: "node"})
			if len(items) == 5 {
				break
			}
		}
		return items, nil
	})

	if r.dependencies.AgentOps == nil {
		addSource("compose", false, "not_configured", nil)
		addSource("systemd", false, "not_configured", nil)
		return
	}
	if !strings.EqualFold(node.Status, "online") {
		addSource("compose", false, "node_offline", nil)
		addSource("systemd", false, "node_offline", nil)
		return
	}
	collect("compose", func(sourceCtx context.Context) ([]incidentEvidence, error) {
		response, err := r.dependencies.AgentOps.DockerComposeList(sourceCtx, node.ID)
		if err != nil {
			return nil, err
		}
		if !response.Success || !response.Supported {
			return nil, errIncidentUnsupported
		}
		items := make([]incidentEvidence, 0, 5)
		for _, project := range response.Projects {
			for _, service := range project.Services {
				if service.Health == "" || strings.EqualFold(service.Health, "healthy") {
					continue
				}
				items = append(items, incidentEvidence{Kind: "compose_service_unhealthy", Severity: "warning", Confidence: "high", Summary: incidentSafeSummary(fmt.Sprintf("Compose 服务 %s 健康状态异常", service.Name)), Source: "compose", ResourceType: "node", ResourceID: node.ID, RouteKey: "node"})
				if len(items) == 5 {
					return items, nil
				}
			}
		}
		return items, nil
	})
	collect("systemd", func(sourceCtx context.Context) ([]incidentEvidence, error) {
		response, err := r.dependencies.AgentOps.SystemdServiceList(sourceCtx, node.ID)
		if err != nil {
			return nil, err
		}
		if !response.Success || !response.Supported {
			return nil, errIncidentUnsupported
		}
		items := make([]incidentEvidence, 0, 5)
		for _, service := range response.Services {
			if !strings.EqualFold(service.ActiveState, "failed") && !strings.EqualFold(service.SubState, "failed") {
				continue
			}
			items = append(items, incidentEvidence{Kind: "systemd_service_failed", Severity: "warning", Confidence: "high", Summary: incidentSafeSummary(fmt.Sprintf("Systemd 服务 %s 处于失败状态", service.Name)), Source: "systemd", ResourceType: "node", ResourceID: node.ID, RouteKey: "node"})
			if len(items) == 5 {
				break
			}
		}
		return items, nil
	})
}

func (r *Registry) collectPlatformSources(ctx context.Context, addSource func(string, bool, string, []incidentEvidence)) {
	if r.dependencies.Kubernetes == nil {
		addSource("kubernetes", false, "not_configured", nil)
	} else if clusters, err := r.dependencies.Kubernetes.ListClustersWithNodeInfo(); err != nil {
		addSource("kubernetes", false, incidentReason(err), nil)
	} else {
		items := make([]incidentEvidence, 0, 5)
		for _, cluster := range clusters {
			if cluster == nil || (strings.EqualFold(cluster.Status, "online") && strings.EqualFold(cluster.NodeStatus, "online")) {
				continue
			}
			if cluster == nil {
				continue
			}
			items = append(items, incidentEvidence{Kind: "k8s_cluster_unavailable", Severity: "critical", Confidence: "high", Summary: "Kubernetes 集群或关联节点不可用", ObservedAt: incidentObservedAt(cluster.UpdatedAt), Source: "kubernetes", ResourceType: "k8s_cluster", ResourceID: cluster.ID, RouteKey: "k8s_cluster"})
			if len(items) == 5 {
				break
			}
		}
		addSource("kubernetes", true, "", items)
	}
	if r.dependencies.Services == nil {
		addSource("application_services", false, "not_configured", nil)
	} else if services, err := r.dependencies.Services.List(ctx); err != nil {
		addSource("application_services", false, incidentReason(err), nil)
	} else {
		addSource("application_services", true, "", serviceEvidence(services, ""))
	}
	if r.dependencies.ServerLogs == nil {
		addSource("server_logs", false, "not_configured", nil)
	} else {
		addSource("server_logs", true, "", nil)
	}
}

func (r *Registry) collectKubernetesSources(ctx context.Context, clusterID string, addSource func(string, bool, string, []incidentEvidence)) {
	if r.dependencies.Kubernetes == nil {
		addSource("kubernetes", false, "not_configured", nil)
		return
	}
	cluster, err := r.k8sCluster(clusterID)
	if err != nil {
		addSource("kubernetes", false, incidentReason(err), nil)
		return
	}
	if !strings.EqualFold(cluster.Status, "online") || !strings.EqualFold(cluster.NodeStatus, "online") {
		addSource("kubernetes", false, "cluster_unavailable", []incidentEvidence{{Kind: "k8s_cluster_unavailable", Severity: "critical", Confidence: "high", ObservedAt: incidentObservedAt(cluster.UpdatedAt), Summary: "Kubernetes 集群或关联节点不可用", Source: "kubernetes", ResourceType: "k8s_cluster", ResourceID: cluster.ID, RouteKey: "k8s_cluster"}})
		return
	}
	add := func(items *[]incidentEvidence, kind, severity, confidence, summary, resourceType, resourceID string) {
		if len(*items) < 10 {
			*items = append(*items, incidentEvidence{Kind: kind, Severity: severity, Confidence: confidence, Summary: summary, Source: "kubernetes", ResourceType: resourceType, ResourceID: resourceID, RouteKey: "k8s_cluster"})
		}
	}
	if pods, podErr := r.dependencies.Kubernetes.GetPods(ctx, clusterID, ""); podErr != nil {
		addSource("kubernetes_pods", false, incidentReason(podErr), nil)
	} else {
		items := make([]incidentEvidence, 0, 10)
		for _, pod := range pods {
			if !strings.EqualFold(pod.Status, "Running") || (pod.Ready != "" && strings.HasPrefix(pod.Ready, "0/")) || pod.Restarts >= 5 {
				add(&items, "k8s_pod_unhealthy", "warning", "high", incidentSafeSummary(fmt.Sprintf("Pod %s 状态异常", pod.Name)), "k8s_cluster", clusterID)
			}
		}
		addSource("kubernetes_pods", true, "", items)
	}
	if deployments, depErr := r.dependencies.Kubernetes.GetDeployments(ctx, clusterID, ""); depErr != nil {
		addSource("kubernetes_deployments", false, incidentReason(depErr), nil)
	} else {
		items := make([]incidentEvidence, 0, 10)
		for _, deployment := range deployments {
			parts := strings.SplitN(deployment.Ready, "/", 2)
			if len(parts) == 2 && parts[0] != parts[1] {
				add(&items, "k8s_deployment_unready", "warning", "high", incidentSafeSummary(fmt.Sprintf("Deployment %s 未达到期望副本数", deployment.Name)), "k8s_cluster", clusterID)
			}
		}
		addSource("kubernetes_deployments", true, "", items)
	}
	if summary, summaryErr := r.dependencies.Kubernetes.GetSummary(ctx, clusterID); summaryErr != nil {
		addSource("kubernetes_summary", false, incidentReason(summaryErr), nil)
	} else if summary == nil {
		addSource("kubernetes_summary", false, "empty", nil)
	} else {
		addSource("kubernetes_summary", true, "", nil)
	}
}

func (r *Registry) collectApplicationServiceSources(ctx context.Context, serviceID string, addSource func(string, bool, string, []incidentEvidence)) {
	if r.dependencies.Services == nil {
		addSource("application_services", false, "not_configured", nil)
		return
	}
	services, err := r.dependencies.Services.List(ctx)
	if err != nil {
		addSource("application_services", false, incidentReason(err), nil)
		return
	}
	addSource("application_services", true, "", serviceEvidence(services, serviceID))
}

func serviceEvidence(services []servicecenter.ServiceSummary, serviceID string) []incidentEvidence {
	items := make([]incidentEvidence, 0, 10)
	for _, service := range services {
		if serviceID != "" && service.ID != serviceID {
			continue
		}
		if service.Health == servicecenter.HealthHealthy {
			continue
		}
		severity := "warning"
		if service.Health == servicecenter.HealthUnhealthy {
			severity = "critical"
		}
		items = append(items, incidentEvidence{Kind: "application_service_unhealthy", Severity: severity, Confidence: "high", Summary: incidentSafeSummary(fmt.Sprintf("应用服务 %s 健康状态为 %s", service.Name, service.Health)), Source: "application_services", ResourceType: "application_service", ResourceID: service.ID, RouteKey: "application_service"})
		for _, resource := range service.Resources {
			if resource.Health == servicecenter.HealthHealthy {
				continue
			}
			items = append(items, incidentEvidence{Kind: "application_resource_unhealthy", Severity: "warning", Confidence: "medium", Summary: incidentSafeSummary(fmt.Sprintf("关联资源 %s 状态异常", resource.DisplayName)), Source: "application_services", ResourceType: "application_service", ResourceID: service.ID, RouteKey: "application_service"})
			if len(items) >= 10 {
				return items[:10]
			}
		}
		if len(items) >= 10 {
			return items[:10]
		}
	}
	return items
}

func incidentReason(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrUnsupportedTool):
		return "not_configured"
	case errors.Is(err, errIncidentNoSnapshot):
		return "no_snapshot"
	case errors.Is(err, errIncidentUnavailable):
		return "unavailable"
	case errors.Is(err, errIncidentUnsupported):
		return "unsupported"
	case errors.Is(err, errIncidentOutsideWindow):
		return "outside_window"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "query_failed"
	}
}

var (
	errIncidentNoSnapshot    = errors.New("incident source has no snapshot")
	errIncidentUnavailable   = errors.New("incident source unavailable")
	errIncidentUnsupported   = errors.New("incident source unsupported")
	errIncidentOutsideWindow = errors.New("incident source outside requested window")
	errIncidentQueryFailed   = errors.New("incident source query failed")
)

func incidentObservedAt(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func incidentObservedAtUnix(value int64) string {
	if value <= 0 {
		return ""
	}
	return incidentObservedAt(time.Unix(value, 0))
}

func incidentUnixInWindow(value int64, from, to time.Time) bool {
	if value <= 0 {
		return false
	}
	observed := time.Unix(value, 0)
	return !observed.Before(from) && !observed.After(to)
}

func incidentSafeSummary(value string) string {
	return sanitizeModelText(value, 256)
}

func rankIncidentEvidence(items []incidentEvidence, scopeType, scopeID string) []incidentEvidence {
	corroboratingSources := make(map[string]map[string]struct{})
	for index := range items {
		if items[index].Confidence == "" {
			items[index].Confidence = "low"
		}
		key := items[index].ResourceType + "\x00" + items[index].ResourceID
		if key == "\x00" {
			continue
		}
		if corroboratingSources[key] == nil {
			corroboratingSources[key] = make(map[string]struct{})
		}
		corroboratingSources[key][items[index].Source] = struct{}{}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := incidentSeverityRank(items[i].Severity), incidentSeverityRank(items[j].Severity)
		if left != right {
			return left > right
		}
		leftDirect := items[i].ResourceType == scopeType && (scopeID == "" || items[i].ResourceID == scopeID)
		rightDirect := items[j].ResourceType == scopeType && (scopeID == "" || items[j].ResourceID == scopeID)
		if leftDirect != rightDirect {
			return leftDirect
		}
		if items[i].ObservedAt != items[j].ObservedAt {
			return items[i].ObservedAt > items[j].ObservedAt
		}
		leftSources := len(corroboratingSources[items[i].ResourceType+"\x00"+items[i].ResourceID])
		rightSources := len(corroboratingSources[items[j].ResourceType+"\x00"+items[j].ResourceID])
		if leftSources != rightSources {
			return leftSources > rightSources
		}
		leftKey := items[i].Kind + "\x00" + items[i].Source + "\x00" + items[i].ResourceID + "\x00" + items[i].Summary
		rightKey := items[j].Kind + "\x00" + items[j].Source + "\x00" + items[j].ResourceID + "\x00" + items[j].Summary
		return leftKey < rightKey
	})
	return items
}

func incidentSeverityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}
