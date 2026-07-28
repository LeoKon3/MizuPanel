package servicecenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type AgentOperations interface {
	DockerComposeList(ctx context.Context, nodeID string) (protocol.DockerComposeListResponse, error)
	SystemdServiceList(ctx context.Context, nodeID string) (protocol.SystemdServiceListResponse, error)
}

type KubernetesOperations interface {
	GetDeployments(ctx context.Context, clusterID, namespace string) ([]protocol.K8sDeployment, error)
	GetStatefulSets(ctx context.Context, clusterID, namespace string) ([]protocol.K8sStatefulSet, error)
	GetDaemonSets(ctx context.Context, clusterID, namespace string) ([]protocol.K8sDaemonSet, error)
}

type Facade struct {
	store *Store
	agent AgentOperations
	k8s   KubernetesOperations
}

func NewFacade(store *Store, agent AgentOperations, k8s KubernetesOperations) *Facade {
	return &Facade{store: store, agent: agent, k8s: k8s}
}

func (f *Facade) Create(ctx context.Context, input ServiceInput) (ServiceDetail, error) {
	service, err := f.store.Create(ctx, input)
	if err != nil {
		return ServiceDetail{}, err
	}
	return f.projectOne(ctx, service, true)
}

func (f *Facade) Update(ctx context.Context, id string, input ServiceInput) (ServiceDetail, error) {
	service, err := f.store.Update(ctx, id, input)
	if err != nil {
		return ServiceDetail{}, err
	}
	return f.projectOne(ctx, service, true)
}

func (f *Facade) Delete(ctx context.Context, id string) error {
	return f.store.Delete(ctx, id)
}

func (f *Facade) Get(ctx context.Context, id string) (ServiceDetail, error) {
	service, err := f.store.Get(ctx, id)
	if err != nil {
		return ServiceDetail{}, err
	}
	return f.projectOne(ctx, service, true)
}

func (f *Facade) Definition(ctx context.Context, id string) (Service, error) {
	return f.store.Get(ctx, id)
}

func (f *Facade) List(ctx context.Context) ([]ServiceSummary, error) {
	services, err := f.store.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return []ServiceSummary{}, nil
	}
	summaries, _, err := f.project(ctx, services)
	return summaries, err
}

func (f *Facade) projectOne(ctx context.Context, service Service, includeActivity bool) (ServiceDetail, error) {
	summaries, signals, err := f.project(ctx, []Service{service})
	if err != nil {
		return ServiceDetail{}, err
	}
	detail := ServiceDetail{ServiceSummary: summaries[0], RecentAlerts: []AlertActivity{}, RecentTasks: []TaskActivity{}, RecentAudit: []AuditActivity{}}
	if !includeActivity {
		return detail, nil
	}
	alertIDs, taskIDs, nodeIDs := relatedIDs(service.Resources, signals.clusterNode)
	detail.RecentAlerts, err = f.store.recentAlerts(ctx, alertIDs, 10)
	if err != nil {
		return ServiceDetail{}, err
	}
	detail.RecentTasks, err = f.store.recentTasks(ctx, taskIDs, 10)
	if err != nil {
		return ServiceDetail{}, err
	}
	detail.RecentAudit, err = f.store.recentAudit(ctx, service.ID, nodeIDs, 10)
	if err != nil {
		return ServiceDetail{}, err
	}
	return detail, nil
}

type localSignals struct {
	nodes       map[string]nodeSignal
	uptime      map[string]uptimeSignal
	alerts      map[string]alertSignal
	tasks       map[string]taskSignal
	clusterName map[string]string
	clusterNode map[string]string
}

type composeResult struct {
	response protocol.DockerComposeListResponse
	err      error
}

type systemdResult struct {
	response protocol.SystemdServiceListResponse
	err      error
}

type k8sResult struct {
	deployments  []protocol.K8sDeployment
	statefulSets []protocol.K8sStatefulSet
	daemonSets   []protocol.K8sDaemonSet
	err          error
}

type remoteSignals struct {
	compose map[string]composeResult
	systemd map[string]systemdResult
	k8s     map[string]k8sResult
}

func (f *Facade) project(ctx context.Context, services []Service) ([]ServiceSummary, localSignals, error) {
	resources := make([]Resource, 0)
	for _, service := range services {
		resources = append(resources, service.Resources...)
	}
	local, err := f.store.loadLocalSignals(ctx, resources)
	if err != nil {
		return nil, localSignals{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	remote := f.loadRemoteSignals(requestCtx, resources)
	summaries := make([]ServiceSummary, 0, len(services))
	for _, service := range services {
		projected := make([]ResourceProjection, 0, len(service.Resources))
		for _, resource := range service.Resources {
			projection := projectResource(resource, local, remote)
			projection.Meta = resourceMeta(resource, local)
			projected = append(projected, projection)
		}
		health, reasons, reasonCounts := aggregateHealth(projected)
		typeCounts := make(map[string]int)
		for _, resource := range service.Resources {
			typeCounts[string(resource.ResourceType)]++
		}
		firstReason := ""
		if len(reasons) > 0 {
			firstReason = reasons[0].Message
		}
		summaries = append(summaries, ServiceSummary{
			ID:                 service.ID,
			Name:               service.Name,
			Description:        service.Description,
			Health:             health,
			Reasons:            reasons,
			FirstReason:        firstReason,
			ReasonCounts:       reasonCounts,
			ResourceCount:      len(service.Resources),
			ResourceTypeCounts: typeCounts,
			LocationSummary:    locationSummary(service.Resources, local),
			Resources:          projected,
			CreatedAt:          service.CreatedAt,
			UpdatedAt:          service.UpdatedAt,
		})
	}
	return summaries, local, nil
}

func (f *Facade) loadRemoteSignals(ctx context.Context, resources []Resource) remoteSignals {
	composeNodes := make(map[string]struct{})
	systemdNodes := make(map[string]struct{})
	k8sScopes := make(map[string]Resource)
	for _, resource := range resources {
		switch resource.ResourceType {
		case ResourceComposeProject:
			composeNodes[resource.ScopeID] = struct{}{}
		case ResourceSystemdService:
			systemdNodes[resource.ScopeID] = struct{}{}
		case ResourceK8sWorkload:
			k8sScopes[resource.ScopeID+"\x00"+resource.ResourceKind] = resource
		}
	}
	result := remoteSignals{compose: make(map[string]composeResult), systemd: make(map[string]systemdResult), k8s: make(map[string]k8sResult)}
	var wg sync.WaitGroup
	var mu sync.Mutex
	semaphore := make(chan struct{}, 6)
	run := func(work func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			work()
		}()
	}
	for nodeID := range composeNodes {
		nodeID := nodeID
		run(func() {
			var response protocol.DockerComposeListResponse
			var err error
			if f.agent == nil {
				err = fmt.Errorf("agent operations unavailable")
			} else {
				response, err = f.agent.DockerComposeList(ctx, nodeID)
			}
			mu.Lock()
			result.compose[nodeID] = composeResult{response: response, err: err}
			mu.Unlock()
		})
	}
	for nodeID := range systemdNodes {
		nodeID := nodeID
		run(func() {
			var response protocol.SystemdServiceListResponse
			var err error
			if f.agent == nil {
				err = fmt.Errorf("agent operations unavailable")
			} else {
				response, err = f.agent.SystemdServiceList(ctx, nodeID)
			}
			mu.Lock()
			result.systemd[nodeID] = systemdResult{response: response, err: err}
			mu.Unlock()
		})
	}
	for scopeKey, resource := range k8sScopes {
		scopeKey, resource := scopeKey, resource
		run(func() {
			entry := k8sResult{}
			if f.k8s == nil {
				entry.err = fmt.Errorf("kubernetes operations unavailable")
			} else {
				switch resource.ResourceKind {
				case "deployment":
					entry.deployments, entry.err = f.k8s.GetDeployments(ctx, resource.ScopeID, "")
				case "statefulset":
					entry.statefulSets, entry.err = f.k8s.GetStatefulSets(ctx, resource.ScopeID, "")
				case "daemonset":
					entry.daemonSets, entry.err = f.k8s.GetDaemonSets(ctx, resource.ScopeID, "")
				}
			}
			mu.Lock()
			result.k8s[scopeKey] = entry
			mu.Unlock()
		})
	}
	wg.Wait()
	return result
}

func projectResource(resource Resource, local localSignals, remote remoteSignals) ResourceProjection {
	switch resource.ResourceType {
	case ResourceNode:
		signal, exists := local.nodes[resource.ResourceKey]
		return nodeHealth(resource, signal, exists)
	case ResourceUptimeMonitor:
		signal, exists := local.uptime[resource.ResourceKey]
		return uptimeHealth(resource, signal, exists)
	case ResourceAlertRule:
		signal, exists := local.alerts[resource.ResourceKey]
		return alertHealth(resource, signal, exists)
	case ResourceScheduledTask:
		signal, exists := local.tasks[resource.ResourceKey]
		return taskHealth(resource, signal, exists)
	case ResourceComposeProject:
		if _, exists := local.nodes[resource.ScopeID]; !exists {
			return composeHealth(resource, nil, "")
		}
		entry, exists := remote.compose[resource.ScopeID]
		if !exists || entry.err != nil || !entry.response.Success || !entry.response.Supported || entry.response.Error != "" {
			return composeHealth(resource, nil, "unavailable")
		}
		return composeHealth(resource, findComposeProject(entry.response.Projects, resource), "")
	case ResourceSystemdService:
		if _, exists := local.nodes[resource.ScopeID]; !exists {
			return systemdHealth(resource, nil, "")
		}
		entry, exists := remote.systemd[resource.ScopeID]
		if !exists || entry.err != nil || !entry.response.Success || !entry.response.Supported || entry.response.Error != "" {
			return systemdHealth(resource, nil, "unavailable")
		}
		return systemdHealth(resource, findSystemdService(entry.response.Services, resource.ResourceKey), "")
	case ResourceK8sWorkload:
		if _, exists := local.clusterName[resource.ScopeID]; !exists {
			return k8sReadyHealth(resource, "", nil, "")
		}
		entry, exists := remote.k8s[resource.ScopeID+"\x00"+resource.ResourceKind]
		if !exists || entry.err != nil {
			return k8sReadyHealth(resource, "", nil, "unavailable")
		}
		switch resource.ResourceKind {
		case "deployment":
			workload := findDeployment(entry.deployments, resource)
			if workload == nil {
				return k8sReadyHealth(resource, "", nil, "")
			}
			return k8sReadyHealth(resource, workload.Ready, &workload.Available, "")
		case "statefulset":
			workload := findStatefulSet(entry.statefulSets, resource)
			if workload == nil {
				return k8sReadyHealth(resource, "", nil, "")
			}
			return k8sReadyHealth(resource, workload.Ready, nil, "")
		case "daemonset":
			return k8sDaemonHealth(resource, findDaemonSet(entry.daemonSets, resource), "")
		}
	}
	return projection(resource, HealthUnknown, "unavailable", "资源状态暂不可用")
}

func findComposeProject(projects []protocol.DockerComposeProject, resource Resource) *protocol.DockerComposeProject {
	for i := range projects {
		if resource.ResourceKind == "managed" && projects[i].ManagedProjectID == resource.ResourceKey {
			return &projects[i]
		}
		if resource.ResourceKind == "external" && projects[i].Name == resource.ResourceKey {
			return &projects[i]
		}
	}
	return nil
}

func findSystemdService(services []protocol.SystemdService, name string) *protocol.SystemdService {
	for i := range services {
		if services[i].Name == name {
			return &services[i]
		}
	}
	return nil
}

func findDeployment(workloads []protocol.K8sDeployment, resource Resource) *protocol.K8sDeployment {
	for i := range workloads {
		if workloads[i].Namespace == resource.Namespace && workloads[i].Name == resource.ResourceKey {
			return &workloads[i]
		}
	}
	return nil
}

func findStatefulSet(workloads []protocol.K8sStatefulSet, resource Resource) *protocol.K8sStatefulSet {
	for i := range workloads {
		if workloads[i].Namespace == resource.Namespace && workloads[i].Name == resource.ResourceKey {
			return &workloads[i]
		}
	}
	return nil
}

func findDaemonSet(workloads []protocol.K8sDaemonSet, resource Resource) *protocol.K8sDaemonSet {
	for i := range workloads {
		if workloads[i].Namespace == resource.Namespace && workloads[i].Name == resource.ResourceKey {
			return &workloads[i]
		}
	}
	return nil
}

func resourceMeta(resource Resource, local localSignals) map[string]any {
	meta := map[string]any{}
	if resource.ResourceType == ResourceNode {
		if signal, ok := local.nodes[resource.ResourceKey]; ok {
			meta["node_name"] = signal.Name
		}
	} else if resource.ResourceType == ResourceComposeProject || resource.ResourceType == ResourceSystemdService {
		if signal, ok := local.nodes[resource.ScopeID]; ok {
			meta["node_name"] = signal.Name
		}
	} else if resource.ResourceType == ResourceK8sWorkload {
		if name := local.clusterName[resource.ScopeID]; name != "" {
			meta["cluster_name"] = name
		}
	}
	return meta
}

func locationSummary(resources []Resource, local localSignals) string {
	locations := make(map[string]string)
	for _, resource := range resources {
		switch resource.ResourceType {
		case ResourceNode:
			name := resource.DisplayName
			if signal, ok := local.nodes[resource.ResourceKey]; ok && signal.Name != "" {
				name = signal.Name
			}
			locations["node:"+resource.ResourceKey] = name
		case ResourceComposeProject, ResourceSystemdService:
			name := resource.ScopeID
			if signal, ok := local.nodes[resource.ScopeID]; ok && signal.Name != "" {
				name = signal.Name
			}
			locations["node:"+resource.ScopeID] = name
		case ResourceK8sWorkload:
			name := local.clusterName[resource.ScopeID]
			if name == "" {
				name = resource.ScopeID
			}
			locations["cluster:"+resource.ScopeID] = name
		}
	}
	if len(locations) == 0 {
		return "未关联部署位置"
	}
	names := make([]string, 0, len(locations))
	for _, name := range locations {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) <= 2 {
		return strings.Join(names, "、")
	}
	return strings.Join(names[:2], "、") + fmt.Sprintf(" 等 %d 个位置", len(names))
}

func relatedIDs(resources []Resource, clusterNodes map[string]string) (alertIDs, taskIDs []int64, nodeIDs []string) {
	alerts, tasks, nodes := map[int64]struct{}{}, map[int64]struct{}{}, map[string]struct{}{}
	for _, resource := range resources {
		switch resource.ResourceType {
		case ResourceAlertRule:
			id, _ := strconv.ParseInt(resource.ResourceKey, 10, 64)
			alerts[id] = struct{}{}
		case ResourceScheduledTask:
			id, _ := strconv.ParseInt(resource.ResourceKey, 10, 64)
			tasks[id] = struct{}{}
		case ResourceNode:
			nodes[resource.ResourceKey] = struct{}{}
		case ResourceComposeProject, ResourceSystemdService:
			nodes[resource.ScopeID] = struct{}{}
		case ResourceK8sWorkload:
			if nodeID := clusterNodes[resource.ScopeID]; nodeID != "" {
				nodes[nodeID] = struct{}{}
			}
		}
	}
	for id := range alerts {
		alertIDs = append(alertIDs, id)
	}
	for id := range tasks {
		taskIDs = append(taskIDs, id)
	}
	for id := range nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Slice(alertIDs, func(i, j int) bool { return alertIDs[i] < alertIDs[j] })
	sort.Slice(taskIDs, func(i, j int) bool { return taskIDs[i] < taskIDs[j] })
	sort.Strings(nodeIDs)
	return
}

func (s *Store) loadLocalSignals(ctx context.Context, resources []Resource) (localSignals, error) {
	result := localSignals{nodes: map[string]nodeSignal{}, uptime: map[string]uptimeSignal{}, alerts: map[string]alertSignal{}, tasks: map[string]taskSignal{}, clusterName: map[string]string{}, clusterNode: map[string]string{}}
	nodeIDs, monitorIDs, alertIDs, taskIDs, clusterIDs := collectSignalIDs(resources)
	if err := s.loadNodes(ctx, nodeIDs, result.nodes); err != nil {
		return result, err
	}
	if err := s.loadUptime(ctx, monitorIDs, result.uptime); err != nil {
		return result, err
	}
	if err := s.loadAlerts(ctx, alertIDs, result.alerts); err != nil {
		return result, err
	}
	if err := s.loadTasks(ctx, taskIDs, result.tasks); err != nil {
		return result, err
	}
	if err := s.loadClusters(ctx, clusterIDs, result.clusterName, result.clusterNode); err != nil {
		return result, err
	}
	return result, nil
}

func collectSignalIDs(resources []Resource) (nodeIDs, monitorIDs, alertIDs, taskIDs, clusterIDs []string) {
	nodes, monitors, alerts, tasks, clusters := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, resource := range resources {
		switch resource.ResourceType {
		case ResourceNode:
			nodes[resource.ResourceKey] = struct{}{}
		case ResourceComposeProject, ResourceSystemdService:
			nodes[resource.ScopeID] = struct{}{}
		case ResourceUptimeMonitor:
			monitors[resource.ResourceKey] = struct{}{}
		case ResourceAlertRule:
			alerts[resource.ResourceKey] = struct{}{}
		case ResourceScheduledTask:
			tasks[resource.ResourceKey] = struct{}{}
		case ResourceK8sWorkload:
			clusters[resource.ScopeID] = struct{}{}
		}
	}
	return sortedKeys(nodes), sortedKeys(monitors), sortedKeys(alerts), sortedKeys(tasks), sortedKeys(clusters)
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func stringsToAny(values []string) []any {
	args := make([]any, len(values))
	for i := range values {
		args[i] = values[i]
	}
	return args
}

func (s *Store) loadNodes(ctx context.Context, ids []string, target map[string]nodeSignal) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, status FROM nodes WHERE id IN (`+placeholders(len(ids))+`)`, stringsToAny(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var signal nodeSignal
		if err := rows.Scan(&id, &signal.Name, &signal.Status); err != nil {
			return err
		}
		target[id] = signal
	}
	return rows.Err()
}

func (s *Store) loadUptime(ctx context.Context, ids []string, target map[string]uptimeSignal) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, target, enabled, status FROM uptime_monitors WHERE id IN (`+placeholders(len(ids))+`)`, stringsToAny(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var signal uptimeSignal
		if err := rows.Scan(&id, &signal.Name, &signal.Target, &signal.Enabled, &signal.Status); err != nil {
			return err
		}
		target[strconv.FormatInt(id, 10)] = signal
	}
	return rows.Err()
}

func (s *Store) loadAlerts(ctx context.Context, ids []string, target map[string]alertSignal) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.name, r.enabled, COUNT(h.id) FROM alert_rules r LEFT JOIN alert_history h ON h.rule_id = r.id AND h.resolved_at IS NULL WHERE r.id IN (`+placeholders(len(ids))+`) GROUP BY r.id, r.name, r.enabled`, stringsToAny(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var signal alertSignal
		if err := rows.Scan(&id, &signal.Name, &signal.Enabled, &signal.ActiveCount); err != nil {
			return err
		}
		target[strconv.FormatInt(id, 10)] = signal
	}
	return rows.Err()
}

func (s *Store) loadTasks(ctx context.Context, ids []string, target map[string]taskSignal) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.name, t.enabled, (SELECT r.status FROM task_runs r WHERE r.task_id = t.id ORDER BY r.id DESC LIMIT 1) FROM scheduled_tasks t WHERE t.id IN (`+placeholders(len(ids))+`)`, stringsToAny(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var latest sql.NullString
		var signal taskSignal
		if err := rows.Scan(&id, &signal.Name, &signal.Enabled, &latest); err != nil {
			return err
		}
		signal.HasRun, signal.LatestStatus = latest.Valid, latest.String
		target[strconv.FormatInt(id, 10)] = signal
	}
	return rows.Err()
}

func (s *Store) loadClusters(ctx context.Context, ids []string, names, nodes map[string]string) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, node_id FROM k8s_clusters WHERE id IN (`+placeholders(len(ids))+`)`, stringsToAny(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, nodeID string
		if err := rows.Scan(&id, &name, &nodeID); err != nil {
			return err
		}
		names[id] = name
		nodes[id] = nodeID
	}
	return rows.Err()
}

func (s *Store) recentAlerts(ctx context.Context, ids []int64, limit int) ([]AlertActivity, error) {
	result := []AlertActivity{}
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, rule_id, rule_name, node_id, node_name, metric_field, metric_value, triggered_at, resolved_at FROM alert_history WHERE rule_id IN (`+placeholders(len(ids))+`) ORDER BY CASE WHEN resolved_at IS NULL THEN 0 ELSE 1 END, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AlertActivity
		var resolved sql.NullString
		if err := rows.Scan(&item.ID, &item.RuleID, &item.RuleName, &item.NodeID, &item.NodeName, &item.MetricField, &item.MetricValue, &item.TriggeredAt, &resolved); err != nil {
			return nil, err
		}
		if resolved.Valid {
			value := resolved.String
			item.ResolvedAt = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) recentTasks(ctx context.Context, ids []int64, limit int) ([]TaskActivity, error) {
	result := []TaskActivity{}
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, task_name, script_name, status, trigger_type, created_at, completed_at FROM task_runs WHERE task_id IN (`+placeholders(len(ids))+`) ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item TaskActivity
		var taskID sql.NullInt64
		var completed sql.NullString
		if err := rows.Scan(&item.ID, &taskID, &item.TaskName, &item.ScriptName, &item.Status, &item.Trigger, &item.CreatedAt, &completed); err != nil {
			return nil, err
		}
		if taskID.Valid {
			value := taskID.Int64
			item.TaskID = &value
		}
		if completed.Valid {
			value := completed.String
			item.CompletedAt = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) recentAudit(ctx context.Context, serviceID string, nodeIDs []string, limit int) ([]AuditActivity, error) {
	query := `SELECT id, created_at, actor_type, actor_name, module, action, target_type, target_id, target_name, node_id, result, summary, metadata_json FROM audit_events WHERE (target_type = 'application_service' AND target_id = ?)`
	args := []any{serviceID}
	if len(nodeIDs) > 0 {
		query += ` OR node_id IN (` + placeholders(len(nodeIDs)) + `)`
		args = append(args, stringsToAny(nodeIDs)...)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AuditActivity{}
	for rows.Next() {
		var item AuditActivity
		var metadataJSON string
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.ActorType, &item.ActorName, &item.Module, &item.Action, &item.TargetType, &item.TargetID, &item.TargetName, &item.NodeID, &item.Result, &item.Summary, &metadataJSON); err != nil {
			return nil, err
		}
		item.Metadata = map[string]string{}
		if err := json.Unmarshal([]byte(metadataJSON), &item.Metadata); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
