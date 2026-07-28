package servicecenter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type recordingAgentOperations struct {
	mu             sync.Mutex
	composeCalls   map[string]int
	systemdCalls   map[string]int
	composeDelay   time.Duration
	blockCompose   bool
	activeRequests int
	maxActive      int
}

func newRecordingAgentOperations() *recordingAgentOperations {
	return &recordingAgentOperations{composeCalls: map[string]int{}, systemdCalls: map[string]int{}}
}

func (f *recordingAgentOperations) DockerComposeList(ctx context.Context, nodeID string) (protocol.DockerComposeListResponse, error) {
	f.mu.Lock()
	f.composeCalls[nodeID]++
	f.activeRequests++
	if f.activeRequests > f.maxActive {
		f.maxActive = f.activeRequests
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.activeRequests--
		f.mu.Unlock()
	}()
	if f.blockCompose {
		<-ctx.Done()
		return protocol.DockerComposeListResponse{}, ctx.Err()
	}
	if f.composeDelay > 0 {
		timer := time.NewTimer(f.composeDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return protocol.DockerComposeListResponse{}, ctx.Err()
		}
	}
	return protocol.DockerComposeListResponse{
		Success: true, Supported: true,
		Projects: []protocol.DockerComposeProject{{
			Name: "stack", Status: "running",
			Services: []protocol.DockerComposeService{{Name: "web", State: "running", Health: "healthy"}},
		}},
	}, nil
}

func (f *recordingAgentOperations) SystemdServiceList(_ context.Context, nodeID string) (protocol.SystemdServiceListResponse, error) {
	f.mu.Lock()
	f.systemdCalls[nodeID]++
	f.mu.Unlock()
	return protocol.SystemdServiceListResponse{
		Success: true, Supported: true,
		Services: []protocol.SystemdService{
			{Name: "panel.service", ActiveState: "active", SubState: "running"},
			{Name: "failed.service", ActiveState: "failed", SubState: "failed"},
		},
	}, nil
}

type recordingKubernetesOperations struct {
	mu              sync.Mutex
	deploymentCalls map[string]int
}

type unsupportedAgentOperations struct{}

func (unsupportedAgentOperations) DockerComposeList(context.Context, string) (protocol.DockerComposeListResponse, error) {
	return protocol.DockerComposeListResponse{Success: false, Supported: false}, nil
}

func (unsupportedAgentOperations) SystemdServiceList(context.Context, string) (protocol.SystemdServiceListResponse, error) {
	return protocol.SystemdServiceListResponse{Success: false, Supported: false}, nil
}

func (f *recordingKubernetesOperations) GetDeployments(_ context.Context, clusterID, namespace string) ([]protocol.K8sDeployment, error) {
	f.mu.Lock()
	if f.deploymentCalls == nil {
		f.deploymentCalls = map[string]int{}
	}
	f.deploymentCalls[clusterID+"\x00"+namespace]++
	f.mu.Unlock()
	return []protocol.K8sDeployment{
		{Name: "api", Namespace: "default", Ready: "2/2", Available: 2},
		{Name: "worker", Namespace: "default", Ready: "1/1", Available: 1},
	}, nil
}

func (f *recordingKubernetesOperations) GetStatefulSets(context.Context, string, string) ([]protocol.K8sStatefulSet, error) {
	return nil, nil
}

func (f *recordingKubernetesOperations) GetDaemonSets(context.Context, string, string) ([]protocol.K8sDaemonSet, error) {
	return nil, nil
}

func seedServiceCenterNodeAndCluster(t *testing.T, store *Store, nodeID, clusterID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().Exec(`INSERT INTO nodes (id, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, nodeID, "Node One", "online", now, now); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if clusterID != "" {
		if _, err := store.DB().Exec(`INSERT INTO k8s_clusters (id, name, node_id, kubeconfig_path, kubeconfig_content, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, clusterID, "Cluster One", nodeID, "/safe/path", "redacted", "online", now, now); err != nil {
			t.Fatalf("insert cluster: %v", err)
		}
	}
}

func TestFacadeDeduplicatesRemoteScopesAndProjectsHealthyResources(t *testing.T) {
	store, _ := newServiceCenterTestStore(t)
	seedServiceCenterNodeAndCluster(t, store, "node-1", "cluster-1")
	shared := []Resource{
		{ResourceType: ResourceComposeProject, ScopeID: "node-1", ResourceKind: "external", ResourceKey: "stack", DisplayName: "Stack"},
		{ResourceType: ResourceSystemdService, ScopeID: "node-1", ResourceKey: "panel.service", DisplayName: "Panel"},
	}
	if _, err := store.Create(t.Context(), ServiceInput{Name: "API", Resources: append(append([]Resource{}, shared...), Resource{ResourceType: ResourceK8sWorkload, ScopeID: "cluster-1", ResourceKind: "deployment", Namespace: "default", ResourceKey: "api", DisplayName: "api"})}); err != nil {
		t.Fatalf("create API service: %v", err)
	}
	if _, err := store.Create(t.Context(), ServiceInput{Name: "Worker", Resources: append(append([]Resource{}, shared...), Resource{ResourceType: ResourceK8sWorkload, ScopeID: "cluster-1", ResourceKind: "deployment", Namespace: "default", ResourceKey: "worker", DisplayName: "worker"})}); err != nil {
		t.Fatalf("create Worker service: %v", err)
	}

	agent := newRecordingAgentOperations()
	kubernetes := &recordingKubernetesOperations{}
	summaries, err := NewFacade(store, agent, kubernetes).List(t.Context())
	if err != nil {
		t.Fatalf("list projected services: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d, want 2", len(summaries))
	}
	for _, summary := range summaries {
		if summary.Health != HealthHealthy || summary.ResourceCount != 3 || summary.LocationSummary != "Cluster One、Node One" {
			t.Fatalf("summary = %#v", summary)
		}
		for _, resource := range summary.Resources {
			if resource.Meta == nil {
				t.Fatalf("resource metadata is nil: %#v", resource)
			}
		}
	}
	agent.mu.Lock()
	composeCalls := agent.composeCalls["node-1"]
	systemdCalls := agent.systemdCalls["node-1"]
	agent.mu.Unlock()
	kubernetes.mu.Lock()
	k8sCalls := kubernetes.deploymentCalls["cluster-1\x00"]
	kubernetes.mu.Unlock()
	if composeCalls != 1 || systemdCalls != 1 || k8sCalls != 1 {
		t.Fatalf("remote calls compose=%d systemd=%d k8s=%d, want 1/1/1", composeCalls, systemdCalls, k8sCalls)
	}
}

func TestFacadeProjectsUnsupportedAgentResourcesAsUnavailable(t *testing.T) {
	store, _ := newServiceCenterTestStore(t)
	seedServiceCenterNodeAndCluster(t, store, "node-1", "")
	if _, err := store.Create(t.Context(), ServiceInput{Name: "Legacy Agent", Resources: []Resource{
		{ResourceType: ResourceComposeProject, ScopeID: "node-1", ResourceKind: "external", ResourceKey: "stack", DisplayName: "Stack"},
		{ResourceType: ResourceSystemdService, ScopeID: "node-1", ResourceKey: "panel.service", DisplayName: "Panel"},
	}}); err != nil {
		t.Fatalf("create service: %v", err)
	}

	summaries, err := NewFacade(store, unsupportedAgentOperations{}, nil).List(t.Context())
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Health != HealthDegraded {
		t.Fatalf("summary = %#v, want one degraded service", summaries)
	}
	for _, resource := range summaries[0].Resources {
		if resource.State != "unavailable" || resource.Health != HealthDegraded {
			t.Fatalf("unsupported resource = %#v, want degraded/unavailable", resource)
		}
	}
}

func TestFacadeProjectsDeletedRemoteScopesAsMissing(t *testing.T) {
	store, _ := newServiceCenterTestStore(t)
	if _, err := store.Create(t.Context(), ServiceInput{Name: "Deleted scopes", Resources: []Resource{
		{ResourceType: ResourceComposeProject, ScopeID: "missing-node", ResourceKind: "external", ResourceKey: "stack", DisplayName: "Stack"},
		{ResourceType: ResourceSystemdService, ScopeID: "missing-node", ResourceKey: "panel.service", DisplayName: "Panel"},
		{ResourceType: ResourceK8sWorkload, ScopeID: "missing-cluster", ResourceKind: "deployment", Namespace: "default", ResourceKey: "api", DisplayName: "API"},
	}}); err != nil {
		t.Fatalf("create service: %v", err)
	}

	summaries, err := NewFacade(store, unsupportedAgentOperations{}, nil).List(t.Context())
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Health != HealthUnknown {
		t.Fatalf("summary = %#v, want one unknown service", summaries)
	}
	for _, resource := range summaries[0].Resources {
		if resource.State != "missing" || resource.Health != HealthUnknown {
			t.Fatalf("deleted-scope resource = %#v, want unknown/missing", resource)
		}
	}
}

func TestRelatedIDsIncludesKubernetesClusterNode(t *testing.T) {
	_, _, nodeIDs := relatedIDs([]Resource{
		{ResourceType: ResourceNode, ResourceKey: "node-direct"},
		{ResourceType: ResourceK8sWorkload, ScopeID: "cluster-1", ResourceKind: "deployment", Namespace: "default", ResourceKey: "api"},
	}, map[string]string{"cluster-1": "node-k8s"})
	if got := strings.Join(nodeIDs, ","); got != "node-direct,node-k8s" {
		t.Fatalf("related node IDs = %q", got)
	}
}

func TestFacadeCancellationReturnsPartialHealth(t *testing.T) {
	store, _ := newServiceCenterTestStore(t)
	seedServiceCenterNodeAndCluster(t, store, "node-1", "")
	if _, err := store.Create(t.Context(), ServiceInput{Name: "Partial", Resources: []Resource{
		{ResourceType: ResourceComposeProject, ScopeID: "node-1", ResourceKind: "external", ResourceKey: "stack", DisplayName: "Stack"},
		{ResourceType: ResourceSystemdService, ScopeID: "node-1", ResourceKey: "failed.service", DisplayName: "Failed"},
	}}); err != nil {
		t.Fatalf("create service: %v", err)
	}
	agent := newRecordingAgentOperations()
	agent.blockCompose = true
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	summaries, err := NewFacade(store, agent, nil).List(ctx)
	if err != nil {
		t.Fatalf("list after remote timeout: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("partial projection took %s", elapsed)
	}
	if len(summaries) != 1 || summaries[0].Health != HealthUnhealthy {
		t.Fatalf("partial summary = %#v", summaries)
	}
	states := map[ResourceType]ResourceProjection{}
	for _, resource := range summaries[0].Resources {
		states[resource.ResourceType] = resource
	}
	if states[ResourceComposeProject].Health != HealthDegraded || states[ResourceComposeProject].State != "unavailable" {
		t.Fatalf("compose partial state = %#v", states[ResourceComposeProject])
	}
	if states[ResourceSystemdService].Health != HealthUnhealthy {
		t.Fatalf("systemd partial state = %#v", states[ResourceSystemdService])
	}
}

func TestRemoteSignalConcurrencyIsBounded(t *testing.T) {
	agent := newRecordingAgentOperations()
	agent.composeDelay = 15 * time.Millisecond
	resources := make([]Resource, 0, 18)
	for index := range 18 {
		resources = append(resources, Resource{ResourceType: ResourceComposeProject, ScopeID: fmt.Sprintf("node-%02d", index), ResourceKind: "external", ResourceKey: "stack"})
	}
	facade := NewFacade(nil, agent, nil)
	results := facade.loadRemoteSignals(t.Context(), resources)
	if len(results.compose) != len(resources) {
		t.Fatalf("compose results = %d, want %d", len(results.compose), len(resources))
	}
	agent.mu.Lock()
	maxActive := agent.maxActive
	totalCalls := 0
	for _, calls := range agent.composeCalls {
		totalCalls += calls
	}
	agent.mu.Unlock()
	if totalCalls != len(resources) || maxActive > 6 || maxActive < 2 {
		t.Fatalf("remote concurrency calls=%d max=%d, want %d and 2..6", totalCalls, maxActive, len(resources))
	}
}
