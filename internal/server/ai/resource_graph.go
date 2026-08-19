package ai

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

const (
	maxGraphResources    = 500
	maxGraphEdges        = 1000
	maxGraphSourceItems  = 100
	maxGraphRemoteScopes = 8
	maxGraphConcurrency  = 6
	maxGraphSearchLimit  = 50
	maxGraphSnapshotSize = 256 * 1024
	graphSourceTimeout   = 3 * time.Second
)

var graphResourceTypes = []string{
	"node", "docker_container", "compose_project", "compose_service", "systemd_service",
	"k8s_cluster", "k8s_workload", "application_service", "scheduled_task", "alert_rule", "uptime_monitor",
}

var graphSearchStates = []string{
	"available", "unavailable", "online", "offline", "running", "stopped",
	"enabled", "disabled", "healthy", "unhealthy", "unknown",
}

type GraphResource struct {
	Ref       string `json:"ref"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Available bool   `json:"available"`
	Route     string `json:"route,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	ScopeID   string `json:"scope_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Key       string `json:"key"`
}

type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type GraphSourceStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Count     int    `json:"count"`
	Truncated bool   `json:"truncated,omitempty"`
}

type ResourceGraphSnapshot struct {
	Resources []GraphResource     `json:"resources"`
	Edges     []GraphEdge         `json:"edges"`
	Sources   []GraphSourceStatus `json:"sources"`
	Truncated bool                `json:"truncated"`
	byRef     map[string]int
	edgeSet   map[string]struct{}
	sourceMap map[string]int
}

type graphBuilder struct {
	snapshot ResourceGraphSnapshot
}

func newGraphBuilder() *graphBuilder {
	return &graphBuilder{snapshot: ResourceGraphSnapshot{
		Resources: []GraphResource{}, Edges: []GraphEdge{}, Sources: []GraphSourceStatus{},
		byRef: map[string]int{}, edgeSet: map[string]struct{}{}, sourceMap: map[string]int{},
	}}
}

func graphRef(resourceType, scopeID, kind, namespace, key string) string {
	identity, _ := json.Marshal([]string{scopeID, kind, namespace, key})
	return "rg:v1:" + resourceType + ":" + base64.RawURLEncoding.EncodeToString(identity)
}

func validGraphRef(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 4 || parts[0] != "rg" || parts[1] != "v1" || !graphTypeAllowed(parts[2]) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(decoded) > 1024 {
		return false
	}
	var identity []string
	if json.Unmarshal(decoded, &identity) != nil || len(identity) != 4 {
		return false
	}
	limits := []int{191, 32, 191, 255}
	for index, part := range identity {
		if !utf8.ValidString(part) || len(part) > limits[index] || strings.ContainsAny(part, "\x00\r\n") {
			return false
		}
	}
	return value == graphRef(parts[2], identity[0], identity[1], identity[2], identity[3])
}

func graphTypeAllowed(value string) bool {
	for _, resourceType := range graphResourceTypes {
		if value == resourceType {
			return true
		}
	}
	return false
}

func (b *graphBuilder) addResource(resource GraphResource) bool {
	resource.Name = boundedString(sanitizeModelText(resource.Name, 256), 256)
	resource.State = boundedString(sanitizeModelText(resource.State, 64), 64)
	resource.NodeID = boundedString(resource.NodeID, 191)
	resource.ScopeID = boundedString(resource.ScopeID, 191)
	resource.Kind = boundedString(resource.Kind, 32)
	resource.Namespace = boundedString(resource.Namespace, 191)
	resource.Key = boundedString(resource.Key, 255)
	if _, exists := b.snapshot.byRef[resource.Ref]; exists {
		return true
	}
	if len(b.snapshot.Resources) >= maxGraphResources {
		b.snapshot.Truncated = true
		return false
	}
	b.snapshot.byRef[resource.Ref] = len(b.snapshot.Resources)
	b.snapshot.Resources = append(b.snapshot.Resources, resource)
	return true
}

func (b *graphBuilder) addEdge(from, to, kind string) {
	if from == "" || to == "" || from == to {
		return
	}
	if _, exists := b.snapshot.byRef[from]; !exists {
		return
	}
	if _, exists := b.snapshot.byRef[to]; !exists {
		return
	}
	key := from + "\x00" + to + "\x00" + kind
	if _, exists := b.snapshot.edgeSet[key]; exists {
		return
	}
	if len(b.snapshot.Edges) >= maxGraphEdges {
		b.snapshot.Truncated = true
		return
	}
	b.snapshot.edgeSet[key] = struct{}{}
	b.snapshot.Edges = append(b.snapshot.Edges, GraphEdge{From: from, To: to, Kind: kind})
}

func (b *graphBuilder) setSource(status GraphSourceStatus) {
	if index, exists := b.snapshot.sourceMap[status.Name]; exists {
		b.snapshot.Sources[index] = status
		return
	}
	b.snapshot.sourceMap[status.Name] = len(b.snapshot.Sources)
	b.snapshot.Sources = append(b.snapshot.Sources, status)
}

func (b *graphBuilder) finish() ResourceGraphSnapshot {
	sort.Slice(b.snapshot.Resources, func(i, j int) bool {
		left, right := b.snapshot.Resources[i], b.snapshot.Resources[j]
		if left.Type != right.Type {
			return left.Type < right.Type
		}
		if strings.ToLower(left.Name) != strings.ToLower(right.Name) {
			return strings.ToLower(left.Name) < strings.ToLower(right.Name)
		}
		return left.Ref < right.Ref
	})
	sort.Slice(b.snapshot.Edges, func(i, j int) bool {
		left, right := b.snapshot.Edges[i], b.snapshot.Edges[j]
		if left.From != right.From {
			return left.From < right.From
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.To < right.To
	})
	sort.Slice(b.snapshot.Sources, func(i, j int) bool { return b.snapshot.Sources[i].Name < b.snapshot.Sources[j].Name })
	b.enforceSerializedLimit()
	b.snapshot.byRef = make(map[string]int, len(b.snapshot.Resources))
	for index := range b.snapshot.Resources {
		b.snapshot.byRef[b.snapshot.Resources[index].Ref] = index
	}
	b.snapshot.edgeSet = nil
	b.snapshot.sourceMap = nil
	return b.snapshot
}

func (b *graphBuilder) enforceSerializedLimit() {
	encoded, _ := json.Marshal(b.snapshot)
	if len(encoded) <= maxGraphSnapshotSize {
		return
	}
	b.snapshot.Truncated = true
	resources := b.snapshot.Resources
	edges := b.snapshot.Edges
	b.snapshot.Edges = nil
	encoded, _ = json.Marshal(b.snapshot)
	if len(encoded) > maxGraphSnapshotSize {
		low, high := 0, len(resources)
		for low < high {
			mid := (low + high + 1) / 2
			b.snapshot.Resources = resources[:mid]
			candidate, _ := json.Marshal(b.snapshot)
			if len(candidate) <= maxGraphSnapshotSize {
				low = mid
			} else {
				high = mid - 1
			}
		}
		b.snapshot.Resources = append([]GraphResource(nil), resources[:low]...)
	}
	retained := make(map[string]struct{}, len(b.snapshot.Resources))
	for _, resource := range b.snapshot.Resources {
		retained[resource.Ref] = struct{}{}
	}
	filteredEdges := make([]GraphEdge, 0, len(edges))
	for _, edge := range edges {
		if _, exists := retained[edge.From]; !exists {
			continue
		}
		if _, exists := retained[edge.To]; exists {
			filteredEdges = append(filteredEdges, edge)
		}
	}
	low, high := 0, len(filteredEdges)
	for low < high {
		mid := (low + high + 1) / 2
		b.snapshot.Edges = filteredEdges[:mid]
		candidate, _ := json.Marshal(b.snapshot)
		if len(candidate) <= maxGraphSnapshotSize {
			low = mid
		} else {
			high = mid - 1
		}
	}
	b.snapshot.Edges = append([]GraphEdge(nil), filteredEdges[:low]...)
}

func (r *Registry) registerResourceGraphTools() {
	type searchArguments struct {
		Query  string   `json:"query"`
		Types  []string `json:"types"`
		NodeID string   `json:"node_id"`
		State  string   `json:"state"`
		Limit  int      `json:"limit"`
	}
	r.add(registeredTool{
		definition: objectDefinition("search_resources", "Search the current bounded MizuPanel resource graph by safe identity, type, node scope, or state.", map[string]any{
			"query":   map[string]any{"type": "string", "maxLength": 128},
			"types":   map[string]any{"type": "array", "maxItems": len(graphResourceTypes), "uniqueItems": true, "items": map[string]any{"type": "string", "enum": graphResourceTypes}},
			"node_id": map[string]any{"type": "string", "maxLength": 191},
			"state":   map[string]any{"type": "string", "enum": graphSearchStates},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": maxGraphSearchLimit},
		}, nil),
		risk: RiskRead, capability: capabilityResourceGraph,
		metadata: classifiedPolicyMetadata("resource_graph", "search_resources", "read", "platform"),
		validate: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args searchArguments
			if err := strictArguments(raw, &args); err != nil || !utf8.ValidString(args.Query) || len(args.Query) > 128 || !utf8.ValidString(args.State) || len(args.State) > 64 {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			args.Query = strings.TrimSpace(args.Query)
			args.State = strings.ToLower(strings.TrimSpace(args.State))
			if args.State != "" && !oneOf(args.State, graphSearchStates...) {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if args.NodeID != "" && !validIdentifier(args.NodeID, 191) {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			if args.NodeID != "" {
				if r.dependencies.Nodes == nil {
					return nil, ToolTarget{}, ErrUnsupportedTool
				}
				if _, err := r.dependencies.Nodes.Get(ctx, args.NodeID); err != nil {
					if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNotFound) {
						return nil, ToolTarget{}, ErrInvalidArguments
					}
					return nil, ToolTarget{}, err
				}
			}
			seen := map[string]struct{}{}
			for _, resourceType := range args.Types {
				if !graphTypeAllowed(resourceType) {
					return nil, ToolTarget{}, ErrInvalidArguments
				}
				if _, exists := seen[resourceType]; exists {
					return nil, ToolTarget{}, ErrInvalidArguments
				}
				seen[resourceType] = struct{}{}
			}
			if args.Limit == 0 {
				args.Limit = defaultAIReadLimit
			}
			return normalizedArguments(args), ToolTarget{Type: "resource_graph", ID: "current", Name: ""}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args searchArguments
			_ = json.Unmarshal(raw, &args)
			snapshot, err := r.resourceGraphSnapshot(ctx)
			if err != nil {
				return SafeToolResult{}, err
			}
			types := make(map[string]struct{}, len(args.Types))
			for _, resourceType := range args.Types {
				types[resourceType] = struct{}{}
			}
			query := strings.ToLower(args.Query)
			matches := make([]GraphResource, 0, min(args.Limit, len(snapshot.Resources)))
			total := 0
			for _, resource := range snapshot.Resources {
				if len(types) > 0 {
					if _, ok := types[resource.Type]; !ok {
						continue
					}
				}
				if args.NodeID != "" && resource.NodeID != args.NodeID && !(resource.Type == "node" && resource.Key == args.NodeID) {
					continue
				}
				if args.State != "" && !graphResourceMatchesState(resource, args.State) {
					continue
				}
				if query != "" && !strings.Contains(strings.ToLower(resource.Name), query) && !strings.Contains(strings.ToLower(resource.Key), query) {
					continue
				}
				total++
				if len(matches) < args.Limit {
					matches = append(matches, resource)
				}
			}
			return SafeToolResult{Data: map[string]any{"resources": matches, "sources": snapshot.Sources, "total": total, "truncated": snapshot.Truncated || total > len(matches)}, Summary: "资源搜索完成"}, nil
		},
	})

	type topologyArguments struct {
		ResourceRef string `json:"resource_ref"`
	}
	r.add(registeredTool{
		definition: objectDefinition("get_resource_topology", "Get only direct incoming and outgoing relationships for one exact server-owned resource reference.", map[string]any{
			"resource_ref": map[string]any{"type": "string", "maxLength": 1400},
		}, []string{"resource_ref"}),
		risk: RiskRead, capability: capabilityResourceGraph,
		metadata: classifiedPolicyMetadata("resource_graph", "get_resource_topology", "read", "resource"),
		validate: func(_ context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			var args topologyArguments
			if err := strictArguments(raw, &args); err != nil || !validGraphRef(args.ResourceRef) {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return normalizedArguments(args), ToolTarget{Type: "resource_graph", ID: args.ResourceRef}, nil
		},
		execute: func(ctx context.Context, raw json.RawMessage) (SafeToolResult, error) {
			var args topologyArguments
			_ = json.Unmarshal(raw, &args)
			snapshot, err := r.resourceGraphSnapshot(ctx)
			if err != nil {
				return SafeToolResult{}, err
			}
			index, exists := snapshot.byRef[args.ResourceRef]
			if !exists {
				return SafeToolResult{}, ErrInvalidArguments
			}
			incoming, outgoing := graphRelationships(snapshot, args.ResourceRef)
			impact := graphImpact(snapshot, args.ResourceRef, snapshot.Resources[index])
			return SafeToolResult{Data: map[string]any{"resource": snapshot.Resources[index], "incoming": incoming, "outgoing": outgoing, "impact": impact, "sources": snapshot.Sources}, Summary: "资源拓扑查询完成"}, nil
		},
	})
}

func graphResourceMatchesState(resource GraphResource, filter string) bool {
	state := strings.ToLower(strings.TrimSpace(resource.State))
	switch filter {
	case "available":
		return resource.Available
	case "unavailable":
		return !resource.Available
	case "online":
		return state == "online"
	case "offline":
		return state == "offline" || strings.HasPrefix(state, "node_offline")
	case "running":
		return state == "running" || state == "active" || strings.HasPrefix(state, "running/")
	case "stopped":
		return state == "stopped" || state == "exited" || state == "inactive" || state == "dead"
	case "enabled", "disabled", "unknown":
		return state == filter
	case "healthy":
		return state == "healthy" || strings.HasSuffix(state, "/healthy")
	case "unhealthy":
		return state == "unhealthy" || strings.HasSuffix(state, "/unhealthy") || state == "failed"
	default:
		return false
	}
}

type GraphRelationship struct {
	Kind      string        `json:"kind"`
	Direction string        `json:"direction"`
	Resource  GraphResource `json:"resource"`
}

func graphRelationships(snapshot ResourceGraphSnapshot, ref string) ([]GraphRelationship, []GraphRelationship) {
	incoming := []GraphRelationship{}
	outgoing := []GraphRelationship{}
	for _, edge := range snapshot.Edges {
		if edge.To == ref {
			if index, ok := snapshot.byRef[edge.From]; ok {
				incoming = append(incoming, GraphRelationship{Kind: edge.Kind, Direction: "incoming", Resource: snapshot.Resources[index]})
			}
		}
		if edge.From == ref {
			if index, ok := snapshot.byRef[edge.To]; ok {
				outgoing = append(outgoing, GraphRelationship{Kind: edge.Kind, Direction: "outgoing", Resource: snapshot.Resources[index]})
			}
		}
	}
	return incoming, outgoing
}

func (r *Registry) resourceGraphSnapshot(ctx context.Context) (ResourceGraphSnapshot, error) {
	builder := newGraphBuilder()
	nodes := r.addGraphNodes(ctx, builder)
	if err := ctx.Err(); err != nil {
		return ResourceGraphSnapshot{}, err
	}
	r.addGraphDocker(ctx, builder, nodes)
	r.addGraphNodeRemote(ctx, builder, nodes)
	r.addGraphKubernetes(ctx, builder)
	r.addGraphTasks(ctx, builder)
	r.addGraphAlerts(builder)
	r.addGraphUptime(ctx, builder)
	r.addGraphApplicationServices(ctx, builder)
	if err := ctx.Err(); err != nil {
		return ResourceGraphSnapshot{}, err
	}
	return builder.finish(), nil
}

func (r *Registry) addGraphNodes(ctx context.Context, builder *graphBuilder) []store.Node {
	if r.dependencies.Nodes == nil {
		builder.setSource(GraphSourceStatus{Name: "nodes", Reason: "not_configured"})
		return []store.Node{}
	}
	nodes, err := r.dependencies.Nodes.List(ctx)
	if err != nil {
		builder.setSource(GraphSourceStatus{Name: "nodes", Reason: "query_failed"})
		return []store.Node{}
	}
	limit := min(len(nodes), maxGraphSourceItems)
	for _, node := range nodes[:limit] {
		ref := graphRef("node", "", "", "", node.ID)
		builder.addResource(GraphResource{Ref: ref, Type: "node", Name: node.Name, State: node.Status, Available: node.Status == "online", Route: "/nodes/" + url.PathEscape(node.ID), NodeID: node.ID, Key: node.ID})
	}
	builder.setSource(GraphSourceStatus{Name: "nodes", Available: true, Count: len(nodes), Truncated: len(nodes) > limit})
	return nodes[:limit]
}

func (r *Registry) addGraphDocker(ctx context.Context, builder *graphBuilder, nodes []store.Node) {
	status := GraphSourceStatus{Name: "docker_containers"}
	if r.dependencies.Docker == nil {
		status.Reason = "not_configured"
		builder.setSource(status)
		return
	}
	successfulSnapshots := 0
	added := 0
	for _, node := range nodes {
		snapshot, found, err := r.dependencies.Docker.Get(ctx, node.ID)
		if err != nil {
			status.Reason = "partial_failure"
			continue
		}
		if !found || !snapshot.Available {
			if status.Reason == "" {
				status.Reason = "no_snapshot"
			}
			continue
		}
		successfulSnapshots++
		status.Count += len(snapshot.Containers)
		for _, container := range snapshot.Containers {
			if added >= maxGraphSourceItems {
				status.Truncated = true
				break
			}
			id := containerID(container)
			ref := graphRef("docker_container", node.ID, "", "", id)
			if builder.addResource(GraphResource{Ref: ref, Type: "docker_container", Name: container.Name, State: container.State, Available: true, Route: "/nodes/" + url.PathEscape(node.ID), NodeID: node.ID, ScopeID: node.ID, Key: id}) {
				added++
				builder.addEdge(ref, graphRef("node", "", "", "", node.ID), "runs_on")
			}
		}
	}
	status.Available = len(nodes) == 0 || successfulSnapshots > 0
	if status.Available && successfulSnapshots == len(nodes) {
		status.Reason = ""
	}
	if !status.Available && status.Reason == "" {
		status.Reason = "source_unavailable"
	}
	status.Truncated = status.Truncated || status.Count > maxGraphSourceItems
	builder.setSource(status)
}

type graphNodeRemoteResult struct {
	node     store.Node
	compose  protocol.DockerComposeListResponse
	composeE error
	systemd  protocol.SystemdServiceListResponse
	systemdE error
}

func (r *Registry) addGraphNodeRemote(ctx context.Context, builder *graphBuilder, nodes []store.Node) {
	composeStatus := GraphSourceStatus{Name: "compose"}
	systemdStatus := GraphSourceStatus{Name: "systemd"}
	if r.dependencies.AgentOps == nil {
		composeStatus.Reason, systemdStatus.Reason = "not_configured", "not_configured"
		builder.setSource(composeStatus)
		builder.setSource(systemdStatus)
		return
	}
	online := make([]store.Node, 0, min(len(nodes), maxGraphRemoteScopes))
	for _, node := range nodes {
		if node.Status == "online" && len(online) < maxGraphRemoteScopes {
			online = append(online, node)
		}
	}
	if len(online) == 0 {
		composeStatus.Reason, systemdStatus.Reason = "no_available_scope", "no_available_scope"
		builder.setSource(composeStatus)
		builder.setSource(systemdStatus)
		return
	}
	results := make([]graphNodeRemoteResult, len(online))
	jobs := make([]func(context.Context), 0, len(online)*2)
	for index, node := range online {
		results[index].node = node
		index, node := index, node
		jobs = append(jobs, func(jobCtx context.Context) {
			results[index].compose, results[index].composeE = r.dependencies.AgentOps.DockerComposeList(jobCtx, node.ID)
		}, func(jobCtx context.Context) {
			results[index].systemd, results[index].systemdE = r.dependencies.AgentOps.SystemdServiceList(jobCtx, node.ID)
		})
	}
	runGraphJobs(ctx, jobs)
	composeSuccess, systemdSuccess := 0, 0
	composeAdded, systemdAdded := 0, 0
	for _, result := range results {
		nodeRef := graphRef("node", "", "", "", result.node.ID)
		if result.composeE != nil {
			composeStatus.Reason = "partial_failure"
		} else if result.compose.Success && result.compose.Supported {
			composeSuccess++
			for _, project := range result.compose.Projects {
				composeStatus.Count += 1 + len(project.Services)
				if composeAdded >= maxGraphSourceItems {
					composeStatus.Truncated = true
					continue
				}
				kind, key := "external", project.Name
				if project.Management == "managed" && project.ManagedProjectID != "" {
					kind, key = "managed", project.ManagedProjectID
				}
				projectRef := graphRef("compose_project", result.node.ID, kind, "", key)
				projectName := project.DisplayName
				if projectName == "" {
					projectName = project.Name
				}
				if builder.addResource(GraphResource{Ref: projectRef, Type: "compose_project", Name: projectName, State: project.Status, Available: true, Route: "/nodes/" + url.PathEscape(result.node.ID), NodeID: result.node.ID, ScopeID: result.node.ID, Kind: kind, Key: key}) {
					composeAdded++
					builder.addEdge(projectRef, nodeRef, "runs_on")
				}
				for _, service := range project.Services {
					if composeAdded >= maxGraphSourceItems {
						composeStatus.Truncated = true
						break
					}
					serviceRef := graphRef("compose_service", result.node.ID, kind, key, service.Name)
					state := service.State
					if service.Health != "" {
						state = service.State + "/" + service.Health
					}
					if builder.addResource(GraphResource{Ref: serviceRef, Type: "compose_service", Name: service.Name, State: state, Available: true, Route: "/nodes/" + url.PathEscape(result.node.ID), NodeID: result.node.ID, ScopeID: result.node.ID, Kind: kind, Namespace: key, Key: service.Name}) {
						composeAdded++
						builder.addEdge(projectRef, serviceRef, "contains")
						builder.addEdge(serviceRef, projectRef, "belongs_to")
					}
				}
			}
		} else if composeStatus.Reason == "" {
			composeStatus.Reason = "source_unavailable"
		}

		if result.systemdE != nil {
			systemdStatus.Reason = "partial_failure"
		} else if result.systemd.Success && result.systemd.Supported {
			systemdSuccess++
			systemdStatus.Count += len(result.systemd.Services)
			for _, service := range result.systemd.Services {
				if systemdAdded >= maxGraphSourceItems {
					systemdStatus.Truncated = true
					break
				}
				ref := graphRef("systemd_service", result.node.ID, "", "", service.Name)
				if builder.addResource(GraphResource{Ref: ref, Type: "systemd_service", Name: service.Name, State: service.ActiveState, Available: service.LoadState != "not-found", Route: "/nodes/" + url.PathEscape(result.node.ID), NodeID: result.node.ID, ScopeID: result.node.ID, Key: service.Name}) {
					systemdAdded++
					builder.addEdge(ref, nodeRef, "runs_on")
				}
			}
		} else if systemdStatus.Reason == "" {
			systemdStatus.Reason = "source_unavailable"
		}
	}
	composeStatus.Available = composeSuccess > 0
	systemdStatus.Available = systemdSuccess > 0
	composeStatus.Truncated = composeStatus.Truncated || composeStatus.Count > maxGraphSourceItems || len(online) < countOnline(nodes)
	systemdStatus.Truncated = systemdStatus.Truncated || systemdStatus.Count > maxGraphSourceItems || len(online) < countOnline(nodes)
	if composeStatus.Available && composeSuccess == len(online) {
		composeStatus.Reason = ""
	}
	if systemdStatus.Available && systemdSuccess == len(online) {
		systemdStatus.Reason = ""
	}
	builder.setSource(composeStatus)
	builder.setSource(systemdStatus)
}

type graphKubernetesResult struct {
	clusterID string
	kind      string
	items     any
	err       error
}

func (r *Registry) addGraphKubernetes(ctx context.Context, builder *graphBuilder) {
	clusterStatus := GraphSourceStatus{Name: "kubernetes_clusters"}
	workloadStatus := GraphSourceStatus{Name: "kubernetes_workloads"}
	if r.dependencies.Kubernetes == nil {
		clusterStatus.Reason, workloadStatus.Reason = "not_configured", "not_configured"
		builder.setSource(clusterStatus)
		builder.setSource(workloadStatus)
		return
	}
	clusters, err := r.dependencies.Kubernetes.ListClustersWithNodeInfo()
	if err != nil {
		clusterStatus.Reason, workloadStatus.Reason = "query_failed", "query_failed"
		builder.setSource(clusterStatus)
		builder.setSource(workloadStatus)
		return
	}
	clusterStatus.Available, clusterStatus.Count = true, len(clusters)
	clusterLimit := min(len(clusters), maxGraphSourceItems)
	online := make([]string, 0, min(clusterLimit, maxGraphRemoteScopes))
	for _, cluster := range clusters[:clusterLimit] {
		if cluster == nil {
			continue
		}
		available := cluster.Status == "online" && cluster.NodeStatus == "online"
		state := cluster.Status
		if cluster.NodeStatus != "online" {
			state = "node_" + cluster.NodeStatus
		}
		ref := graphRef("k8s_cluster", cluster.NodeID, "", "", cluster.ID)
		if builder.addResource(GraphResource{Ref: ref, Type: "k8s_cluster", Name: cluster.Name, State: state, Available: available, Route: "/k8s/clusters/" + url.PathEscape(cluster.ID), NodeID: cluster.NodeID, ScopeID: cluster.NodeID, Key: cluster.ID}) && cluster.NodeID != "" {
			builder.addEdge(ref, graphRef("node", "", "", "", cluster.NodeID), "runs_on")
		}
		if available && len(online) < maxGraphRemoteScopes {
			online = append(online, cluster.ID)
		}
	}
	clusterStatus.Truncated = len(clusters) > clusterLimit
	if len(clusters) == 0 {
		workloadStatus.Available = true
		builder.setSource(clusterStatus)
		builder.setSource(workloadStatus)
		return
	}
	if len(online) == 0 {
		workloadStatus.Reason = "no_available_scope"
		builder.setSource(clusterStatus)
		builder.setSource(workloadStatus)
		return
	}
	results := make([]graphKubernetesResult, len(online)*3)
	jobs := make([]func(context.Context), 0, len(results))
	for clusterIndex, clusterID := range online {
		base := clusterIndex * 3
		for offset, kind := range []string{"deployment", "statefulset", "daemonset"} {
			index, clusterID, kind := base+offset, clusterID, kind
			results[index].clusterID, results[index].kind = clusterID, kind
			jobs = append(jobs, func(jobCtx context.Context) {
				switch kind {
				case "deployment":
					results[index].items, results[index].err = r.dependencies.Kubernetes.GetDeployments(jobCtx, clusterID, "")
				case "statefulset":
					results[index].items, results[index].err = r.dependencies.Kubernetes.GetStatefulSets(jobCtx, clusterID, "")
				case "daemonset":
					results[index].items, results[index].err = r.dependencies.Kubernetes.GetDaemonSets(jobCtx, clusterID, "")
				}
			})
		}
	}
	runGraphJobs(ctx, jobs)
	successes, added := 0, 0
	for _, result := range results {
		if result.err != nil {
			workloadStatus.Reason = "partial_failure"
			continue
		}
		successes++
		clusterRef := graphRef("k8s_cluster", graphClusterNodeID(builder.snapshot, result.clusterID), "", "", result.clusterID)
		switch items := result.items.(type) {
		case []protocol.K8sDeployment:
			workloadStatus.Count += len(items)
			for _, item := range items {
				if added >= maxGraphSourceItems {
					workloadStatus.Truncated = true
					break
				}
				addGraphWorkload(builder, clusterRef, result.clusterID, "deployment", item.Namespace, item.Name, item.Ready)
				added++
			}
		case []protocol.K8sStatefulSet:
			workloadStatus.Count += len(items)
			for _, item := range items {
				if added >= maxGraphSourceItems {
					workloadStatus.Truncated = true
					break
				}
				addGraphWorkload(builder, clusterRef, result.clusterID, "statefulset", item.Namespace, item.Name, item.Ready)
				added++
			}
		case []protocol.K8sDaemonSet:
			workloadStatus.Count += len(items)
			for _, item := range items {
				if added >= maxGraphSourceItems {
					workloadStatus.Truncated = true
					break
				}
				addGraphWorkload(builder, clusterRef, result.clusterID, "daemonset", item.Namespace, item.Name, fmt.Sprintf("%d/%d ready", item.Ready, item.Desired))
				added++
			}
		}
	}
	workloadStatus.Available = successes > 0
	if successes == len(results) {
		workloadStatus.Reason = ""
	}
	workloadStatus.Truncated = workloadStatus.Count > maxGraphSourceItems || len(online) < len(clusters)
	builder.setSource(clusterStatus)
	builder.setSource(workloadStatus)
}

func graphClusterNodeID(snapshot ResourceGraphSnapshot, clusterID string) string {
	for _, resource := range snapshot.Resources {
		if resource.Type == "k8s_cluster" && resource.Key == clusterID {
			return resource.NodeID
		}
	}
	return ""
}

func addGraphWorkload(builder *graphBuilder, clusterRef, clusterID, kind, namespace, name, state string) {
	ref := graphRef("k8s_workload", clusterID, kind, namespace, name)
	if builder.addResource(GraphResource{Ref: ref, Type: "k8s_workload", Name: name, State: state, Available: true, Route: "/k8s/clusters/" + url.PathEscape(clusterID), ScopeID: clusterID, Kind: kind, Namespace: namespace, Key: name}) {
		builder.addEdge(ref, clusterRef, "belongs_to")
	}
}

func (r *Registry) addGraphTasks(ctx context.Context, builder *graphBuilder) {
	status := GraphSourceStatus{Name: "scheduled_tasks"}
	if r.dependencies.Tasks == nil {
		status.Reason = "not_configured"
		builder.setSource(status)
		return
	}
	tasks, err := r.dependencies.Tasks.ListScheduledTasks(ctx)
	if err != nil {
		status.Reason = "query_failed"
		builder.setSource(status)
		return
	}
	status.Available, status.Count = true, len(tasks)
	limit := min(len(tasks), maxGraphSourceItems)
	for _, task := range tasks[:limit] {
		key := strconv.FormatInt(task.ID, 10)
		ref := graphRef("scheduled_task", "", "", "", key)
		state := "disabled"
		if task.Enabled {
			state = "enabled"
		}
		builder.addResource(GraphResource{Ref: ref, Type: "scheduled_task", Name: task.Name, State: state, Available: true, Route: "/tasks", Key: key})
		for _, nodeID := range task.NodeIDs {
			builder.addEdge(ref, graphRef("node", "", "", "", nodeID), "targets")
		}
	}
	status.Truncated = len(tasks) > limit
	builder.setSource(status)
}

func (r *Registry) addGraphAlerts(builder *graphBuilder) {
	status := GraphSourceStatus{Name: "alert_rules"}
	if r.dependencies.Alerts == nil {
		status.Reason = "not_configured"
		builder.setSource(status)
		return
	}
	rules, err := r.dependencies.Alerts.GetAlertRules()
	if err != nil {
		status.Reason = "query_failed"
		builder.setSource(status)
		return
	}
	status.Available, status.Count = true, len(rules)
	limit := min(len(rules), maxGraphSourceItems)
	for _, rule := range rules[:limit] {
		key := strconv.FormatInt(rule.ID, 10)
		ref := graphRef("alert_rule", "", "", "", key)
		state := "disabled"
		if rule.Enabled {
			state = "enabled"
		}
		builder.addResource(GraphResource{Ref: ref, Type: "alert_rule", Name: rule.Name, State: state, Available: true, Route: "/alerts", Key: key})
		if rule.ScopeType == "nodes" {
			for _, nodeID := range rule.ScopeNodeIDs {
				nodeRef := graphRef("node", "", "", "", nodeID)
				builder.addEdge(ref, nodeRef, "targets")
				builder.addEdge(nodeRef, ref, "guarded_by")
			}
		}
	}
	status.Truncated = len(rules) > limit
	builder.setSource(status)
}

func (r *Registry) addGraphUptime(ctx context.Context, builder *graphBuilder) {
	status := GraphSourceStatus{Name: "uptime_monitors"}
	if r.dependencies.Uptime == nil {
		status.Reason = "not_configured"
		builder.setSource(status)
		return
	}
	monitors, err := r.dependencies.Uptime.ListMonitors(ctx)
	if err != nil {
		status.Reason = "query_failed"
		builder.setSource(status)
		return
	}
	status.Available, status.Count = true, len(monitors)
	limit := min(len(monitors), maxGraphSourceItems)
	for _, monitor := range monitors[:limit] {
		key := strconv.FormatInt(monitor.ID, 10)
		builder.addResource(GraphResource{Ref: graphRef("uptime_monitor", "", "", "", key), Type: "uptime_monitor", Name: monitor.Name, State: monitor.Status, Available: monitor.Enabled, Route: "/uptime", Key: key})
	}
	status.Truncated = len(monitors) > limit
	builder.setSource(status)
}

func (r *Registry) addGraphApplicationServices(ctx context.Context, builder *graphBuilder) {
	status := GraphSourceStatus{Name: "application_services"}
	if r.dependencies.Services == nil {
		status.Reason = "not_configured"
		builder.setSource(status)
		return
	}
	services, err := r.dependencies.Services.List(ctx)
	if err != nil {
		status.Reason = "query_failed"
		builder.setSource(status)
		return
	}
	status.Available, status.Count = true, len(services)
	limit := min(len(services), maxGraphSourceItems)
	for _, service := range services[:limit] {
		serviceRef := graphRef("application_service", "", "", "", service.ID)
		builder.addResource(GraphResource{Ref: serviceRef, Type: "application_service", Name: service.Name, State: string(service.Health), Available: service.Health != servicecenter.HealthUnknown, Route: "/services/" + url.PathEscape(service.ID), Key: service.ID})
		for _, projection := range service.Resources {
			resource := graphResourceFromAssociation(projection.Resource)
			if resource.Ref == "" {
				continue
			}
			if _, exists := builder.snapshot.byRef[resource.Ref]; !exists {
				resource.Available = false
				resource.State = "missing"
				builder.addResource(resource)
			}
			builder.addEdge(serviceRef, resource.Ref, "contains")
			builder.addEdge(resource.Ref, serviceRef, "member_of_application")
		}
	}
	status.Truncated = len(services) > limit
	builder.setSource(status)
}

func graphResourceFromAssociation(resource servicecenter.Resource) GraphResource {
	switch resource.ResourceType {
	case servicecenter.ResourceNode:
		return GraphResource{Ref: graphRef("node", "", "", "", resource.ResourceKey), Type: "node", Name: resource.DisplayName, Route: "/nodes/" + url.PathEscape(resource.ResourceKey), NodeID: resource.ResourceKey, Key: resource.ResourceKey}
	case servicecenter.ResourceComposeProject:
		return GraphResource{Ref: graphRef("compose_project", resource.ScopeID, resource.ResourceKind, "", resource.ResourceKey), Type: "compose_project", Name: resource.DisplayName, Route: "/nodes/" + url.PathEscape(resource.ScopeID), NodeID: resource.ScopeID, ScopeID: resource.ScopeID, Kind: resource.ResourceKind, Key: resource.ResourceKey}
	case servicecenter.ResourceSystemdService:
		return GraphResource{Ref: graphRef("systemd_service", resource.ScopeID, "", "", resource.ResourceKey), Type: "systemd_service", Name: resource.DisplayName, Route: "/nodes/" + url.PathEscape(resource.ScopeID), NodeID: resource.ScopeID, ScopeID: resource.ScopeID, Key: resource.ResourceKey}
	case servicecenter.ResourceK8sWorkload:
		return GraphResource{Ref: graphRef("k8s_workload", resource.ScopeID, resource.ResourceKind, resource.Namespace, resource.ResourceKey), Type: "k8s_workload", Name: resource.DisplayName, Route: "/k8s/clusters/" + url.PathEscape(resource.ScopeID), ScopeID: resource.ScopeID, Kind: resource.ResourceKind, Namespace: resource.Namespace, Key: resource.ResourceKey}
	case servicecenter.ResourceUptimeMonitor:
		return GraphResource{Ref: graphRef("uptime_monitor", "", "", "", resource.ResourceKey), Type: "uptime_monitor", Name: resource.DisplayName, Route: "/uptime", Key: resource.ResourceKey}
	case servicecenter.ResourceAlertRule:
		return GraphResource{Ref: graphRef("alert_rule", "", "", "", resource.ResourceKey), Type: "alert_rule", Name: resource.DisplayName, Route: "/alerts", Key: resource.ResourceKey}
	case servicecenter.ResourceScheduledTask:
		return GraphResource{Ref: graphRef("scheduled_task", "", "", "", resource.ResourceKey), Type: "scheduled_task", Name: resource.DisplayName, Route: "/tasks", Key: resource.ResourceKey}
	default:
		return GraphResource{}
	}
}

func runGraphJobs(ctx context.Context, jobs []func(context.Context)) {
	semaphore := make(chan struct{}, maxGraphConcurrency)
	var wait sync.WaitGroup
	for _, job := range jobs {
		job := job
		wait.Add(1)
		go func() {
			defer wait.Done()
			if ctx.Err() != nil {
				return
			}
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			if ctx.Err() != nil {
				return
			}
			jobCtx, cancel := context.WithTimeout(ctx, graphSourceTimeout)
			defer cancel()
			job(jobCtx)
		}()
	}
	wait.Wait()
}

func (r *Registry) resourceGraphCapability() CapabilitySource {
	configured := 0
	for _, present := range []bool{
		r.dependencies.Nodes != nil, r.dependencies.Docker != nil, r.dependencies.AgentOps != nil,
		r.dependencies.Kubernetes != nil, r.dependencies.Services != nil, r.dependencies.Tasks != nil,
		r.dependencies.Alerts != nil, r.dependencies.Uptime != nil,
	} {
		if present {
			configured++
		}
	}
	if configured == 0 {
		return unavailableCapability()
	}
	return availableCapability(nil, configured)
}

func graphImpact(snapshot ResourceGraphSnapshot, ref string, target GraphResource) store.AIToolImpact {
	relatedByRef := map[string]GraphResource{}
	for _, edge := range snapshot.Edges {
		other := ""
		if edge.From == ref {
			other = edge.To
		} else if edge.To == ref {
			other = edge.From
		}
		if index, exists := snapshot.byRef[other]; other != "" && exists {
			relatedByRef[other] = snapshot.Resources[index]
		}
	}
	relatedResources := make([]GraphResource, 0, len(relatedByRef))
	for _, resource := range relatedByRef {
		relatedResources = append(relatedResources, resource)
	}
	sort.Slice(relatedResources, func(i, j int) bool {
		if relatedResources[i].Type != relatedResources[j].Type {
			return relatedResources[i].Type < relatedResources[j].Type
		}
		if relatedResources[i].Name != relatedResources[j].Name {
			return relatedResources[i].Name < relatedResources[j].Name
		}
		return relatedResources[i].Ref < relatedResources[j].Ref
	})
	impact := store.AIToolImpact{Version: 1, Available: true, Complete: !snapshot.Truncated, Target: graphImpactResource(target), Related: []store.AIImpactResource{}, Sources: []store.AIImpactSource{}}
	for _, source := range snapshot.Sources {
		impact.Sources = append(impact.Sources, store.AIImpactSource{Name: source.Name, Available: source.Available, Reason: source.Reason, Truncated: source.Truncated})
		if !source.Available || source.Truncated {
			impact.Complete = false
		}
	}
	impact.Total = len(relatedResources)
	for _, resource := range relatedResources[:min(len(relatedResources), 5)] {
		impact.Related = append(impact.Related, graphImpactResource(resource))
	}
	impact.Overflow = impact.Total - len(impact.Related)
	return impact
}

func graphImpactResource(resource GraphResource) store.AIImpactResource {
	return store.AIImpactResource{Type: resource.Type, Name: resource.Name, State: resource.State, Available: resource.Available, Route: resource.Route}
}

func (r *Registry) impactForToolCall(snapshot ResourceGraphSnapshot, call ValidatedToolCall) store.AIToolImpact {
	if resource, ok := graphResourceForToolTarget(snapshot, call.Target); ok {
		return graphImpact(snapshot, resource.Ref, resource)
	}
	target := GraphResource{Type: call.Target.Type, Name: call.Target.Name, State: "proposed", Available: false}
	relatedRefs := []string{}
	switch call.Target.Type {
	case "docker_container":
		target.Type = "docker_container"
		target.Route = "/nodes/" + url.PathEscape(call.Target.NodeID)
		relatedRefs = append(relatedRefs, graphRef("node", "", "", "", call.Target.NodeID))
	case "scheduled_task", "automation_script":
		target.Type = call.Target.Type
		target.Route = "/tasks"
		var args struct {
			NodeIDs []string `json:"node_ids"`
		}
		if json.Unmarshal(call.Arguments, &args) == nil {
			for _, nodeID := range args.NodeIDs {
				relatedRefs = append(relatedRefs, graphRef("node", "", "", "", nodeID))
			}
		}
	case "k8s_deployment":
		target.Type = "k8s_workload"
		var args struct {
			ClusterID string `json:"cluster_id"`
		}
		if json.Unmarshal(call.Arguments, &args) == nil {
			target.Route = "/k8s/clusters/" + url.PathEscape(args.ClusterID)
			if cluster, ok := graphClusterByID(snapshot, args.ClusterID); ok {
				relatedRefs = append(relatedRefs, cluster.Ref)
			}
		}
	default:
		if call.Target.NodeID != "" {
			relatedRefs = append(relatedRefs, graphRef("node", "", "", "", call.Target.NodeID))
		}
	}
	return impactFromRelatedRefs(snapshot, target, relatedRefs)
}

func graphResourceForToolTarget(snapshot ResourceGraphSnapshot, target ToolTarget) (GraphResource, bool) {
	find := func(match func(GraphResource) bool) (GraphResource, bool) {
		for _, resource := range snapshot.Resources {
			if match(resource) {
				return resource, true
			}
		}
		return GraphResource{}, false
	}
	switch target.Type {
	case "node":
		if index, ok := snapshot.byRef[graphRef("node", "", "", "", target.ID)]; ok {
			return snapshot.Resources[index], true
		}
	case "container":
		if index, ok := snapshot.byRef[graphRef("docker_container", target.NodeID, "", "", target.ID)]; ok {
			return snapshot.Resources[index], true
		}
	case "systemd_service":
		if index, ok := snapshot.byRef[graphRef("systemd_service", target.NodeID, "", "", target.ID)]; ok {
			return snapshot.Resources[index], true
		}
	case "compose_service":
		parts := strings.SplitN(target.ID, "/", 2)
		projectName := parts[0]
		if len(parts) == 1 {
			return find(func(resource GraphResource) bool {
				return resource.Type == "compose_project" && resource.NodeID == target.NodeID && (resource.Name == projectName || resource.Key == projectName)
			})
		}
		serviceName := parts[1]
		projects := map[string]struct{}{}
		for _, resource := range snapshot.Resources {
			if resource.Type == "compose_project" && resource.NodeID == target.NodeID && (resource.Name == projectName || resource.Key == projectName) {
				projects[resource.Ref] = struct{}{}
			}
		}
		for _, edge := range snapshot.Edges {
			if _, ok := projects[edge.From]; ok && edge.Kind == "contains" {
				if index, exists := snapshot.byRef[edge.To]; exists && snapshot.Resources[index].Type == "compose_service" && snapshot.Resources[index].Name == serviceName {
					return snapshot.Resources[index], true
				}
			}
		}
	case "scheduled_task":
		return find(func(resource GraphResource) bool {
			return resource.Type == "scheduled_task" && (resource.Key == target.ID || resource.Name == target.Name)
		})
	case "k8s_deployment":
		parts := strings.SplitN(target.ID, "/", 3)
		if len(parts) == 3 {
			ref := graphRef("k8s_workload", parts[0], "deployment", parts[1], parts[2])
			if index, ok := snapshot.byRef[ref]; ok {
				return snapshot.Resources[index], true
			}
		}
	}
	return GraphResource{}, false
}

func graphClusterByID(snapshot ResourceGraphSnapshot, clusterID string) (GraphResource, bool) {
	for _, resource := range snapshot.Resources {
		if resource.Type == "k8s_cluster" && resource.Key == clusterID {
			return resource, true
		}
	}
	return GraphResource{}, false
}

func impactFromRelatedRefs(snapshot ResourceGraphSnapshot, target GraphResource, refs []string) store.AIToolImpact {
	impact := store.AIToolImpact{Version: 1, Available: len(refs) > 0, Complete: !snapshot.Truncated, Target: graphImpactResource(target), Related: []store.AIImpactResource{}, Sources: []store.AIImpactSource{}}
	for _, source := range snapshot.Sources {
		impact.Sources = append(impact.Sources, store.AIImpactSource{Name: source.Name, Available: source.Available, Reason: source.Reason, Truncated: source.Truncated})
		if !source.Available || source.Truncated {
			impact.Complete = false
		}
	}
	seen := map[string]struct{}{}
	resources := make([]GraphResource, 0, len(refs))
	for _, ref := range refs {
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		if index, exists := snapshot.byRef[ref]; exists {
			resources = append(resources, snapshot.Resources[index])
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Type != resources[j].Type {
			return resources[i].Type < resources[j].Type
		}
		return resources[i].Name < resources[j].Name
	})
	impact.Total = len(resources)
	for _, resource := range resources[:min(len(resources), 5)] {
		impact.Related = append(impact.Related, graphImpactResource(resource))
	}
	impact.Overflow = impact.Total - len(impact.Related)
	if len(resources) > 0 {
		impact.Available = true
	}
	return impact
}
