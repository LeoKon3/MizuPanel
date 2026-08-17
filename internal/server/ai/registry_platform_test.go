package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	serverk8s "github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type platformKubernetesStub struct {
	cluster *serverk8s.PublicClusterWithNode
	pods    []protocol.K8sPod
}

type platformNodeOperationsStub struct {
	systemd                 protocol.SystemdServiceListResponse
	dockerCreateSupported   bool
	dockerCreateV2Supported bool
}

func (platformNodeOperationsStub) Reboot(context.Context, string) (protocol.RebootResponse, error) {
	return protocol.RebootResponse{}, nil
}

func (platformNodeOperationsStub) AgentLogs(context.Context, string, int) (protocol.AgentLogsResponse, error) {
	return protocol.AgentLogsResponse{}, nil
}

func (platformNodeOperationsStub) AgentUpgrade(context.Context, string, string) (protocol.AgentUpgradeResponse, error) {
	return protocol.AgentUpgradeResponse{}, nil
}

func (platformNodeOperationsStub) ContainerStart(context.Context, string, string) (protocol.ContainerStartResponse, error) {
	return protocol.ContainerStartResponse{}, nil
}

func (platformNodeOperationsStub) ContainerStop(context.Context, string, string) (protocol.ContainerStopResponse, error) {
	return protocol.ContainerStopResponse{}, nil
}

func (platformNodeOperationsStub) ContainerRestart(context.Context, string, string) (protocol.ContainerRestartResponse, error) {
	return protocol.ContainerRestartResponse{}, nil
}
func (platformNodeOperationsStub) DockerContainerCreate(context.Context, string, protocol.DockerContainerCreateRequest) (protocol.DockerContainerCreateResponse, error) {
	return protocol.DockerContainerCreateResponse{Supported: false}, nil
}

func (s platformNodeOperationsStub) DockerContainerCreateSupported(string) bool {
	return s.dockerCreateSupported
}

func (s platformNodeOperationsStub) DockerContainerCreateV2Supported(string) bool {
	return s.dockerCreateV2Supported
}

func (platformNodeOperationsStub) DockerComposeList(context.Context, string) (protocol.DockerComposeListResponse, error) {
	return protocol.DockerComposeListResponse{}, nil
}

func (platformNodeOperationsStub) DockerResourceList(context.Context, string) (protocol.DockerResourceListResponse, error) {
	return protocol.DockerResourceListResponse{}, nil
}

func (platformNodeOperationsStub) DockerComposeAction(context.Context, string, string, string, string) (protocol.DockerComposeActionResponse, error) {
	return protocol.DockerComposeActionResponse{}, nil
}

func (s platformNodeOperationsStub) SystemdServiceList(context.Context, string) (protocol.SystemdServiceListResponse, error) {
	return s.systemd, nil
}

func (platformNodeOperationsStub) SystemdServiceAction(context.Context, string, string, string, int) (protocol.SystemdServiceActionResponse, error) {
	return protocol.SystemdServiceActionResponse{}, nil
}

func (platformNodeOperationsStub) TaskRunnerSupported(string) bool { return false }

func (s platformKubernetesStub) ListClustersWithNodeInfo() ([]*serverk8s.PublicClusterWithNode, error) {
	return []*serverk8s.PublicClusterWithNode{s.cluster}, nil
}

func (s platformKubernetesStub) GetClusterWithNodeInfo(id string) (*serverk8s.PublicClusterWithNode, error) {
	if s.cluster == nil || s.cluster.ID != id {
		return nil, nil
	}
	return s.cluster, nil
}

func (platformKubernetesStub) GetSummary(context.Context, string) (*protocol.K8sResourceSummary, error) {
	return &protocol.K8sResourceSummary{Version: "v1.30"}, nil
}

func (platformKubernetesStub) GetNamespaces(context.Context, string) ([]protocol.K8sNamespace, error) {
	return nil, nil
}

func (platformKubernetesStub) GetNodes(context.Context, string) ([]protocol.K8sNode, error) {
	return nil, nil
}

func (s platformKubernetesStub) GetPods(context.Context, string, string) ([]protocol.K8sPod, error) {
	return s.pods, nil
}

func (platformKubernetesStub) GetDeployments(context.Context, string, string) ([]protocol.K8sDeployment, error) {
	return nil, nil
}

func (platformKubernetesStub) GetStatefulSets(context.Context, string, string) ([]protocol.K8sStatefulSet, error) {
	return nil, nil
}

func (platformKubernetesStub) GetDaemonSets(context.Context, string, string) ([]protocol.K8sDaemonSet, error) {
	return nil, nil
}

func (platformKubernetesStub) GetServices(context.Context, string, string) ([]protocol.K8sService, error) {
	return nil, nil
}

func (platformKubernetesStub) GetIngresses(context.Context, string, string) ([]protocol.K8sIngress, error) {
	return nil, nil
}

func TestPlatformReadToolsBoundResultsAndDiagnoseNode(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "worker-1", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	processes := store.NewProcessSnapshotStore(database)
	if err := processes.Upsert(t.Context(), "node-1", protocol.ProcessSnapshot{CollectedAt: 100, Processes: []protocol.ProcessInfo{{PID: 1, Name: "worker", Command: "TOKEN=should-not-leak", CPUUsage: 75, MemoryUsage: 12}}}); err != nil {
		t.Fatalf("upsert processes: %v", err)
	}
	docker := store.NewDockerSnapshotStore(database)
	if err := docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{CollectedAt: 100, Available: true, Containers: []protocol.ContainerInfo{{ID: "container-1", Name: "panel", Image: "panel:latest", State: "exited"}}}); err != nil {
		t.Fatalf("upsert docker: %v", err)
	}
	metrics := store.NewMetricStore(database)
	if err := metrics.Insert(t.Context(), store.Metric{NodeID: "node-1", CPUCores: 2, CPUUsage: 90, MemoryUsage: 85, DiskUsage: 92, Load1: 5}); err != nil {
		t.Fatalf("insert metric: %v", err)
	}

	registry := NewRegistry(RegistryDependencies{
		Nodes:     nodes,
		Metrics:   metrics,
		Processes: processes,
		Docker:    docker,
		AgentOps: platformNodeOperationsStub{systemd: protocol.SystemdServiceListResponse{
			Success:   true,
			Supported: true,
			Services: []protocol.SystemdService{
				{Name: "inactive.service", ActiveState: "inactive", SubState: "dead"},
				{Name: "failed.service", ActiveState: "failed", SubState: "failed"},
			},
		}},
	})
	validated, err := registry.Validate(t.Context(), "list_node_processes", json.RawMessage(`{"node_id":"node-1","limit":1}`))
	if err != nil {
		t.Fatalf("validate process tool: %v", err)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute process tool: %v", err)
	}
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("marshal process result: %v", err)
	}
	if strings.Contains(string(encoded), "should-not-leak") || !strings.Contains(string(encoded), "worker") {
		t.Fatalf("process projection leaked or lost safe data: %s", encoded)
	}

	validated, err = registry.Validate(t.Context(), "diagnose_node", json.RawMessage(`{"node_id":"node-1"}`))
	if err != nil {
		t.Fatalf("validate diagnosis tool: %v", err)
	}
	result, err = registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute diagnosis tool: %v", err)
	}
	encoded, err = json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("marshal diagnosis result: %v", err)
	}
	for _, marker := range []string{"high_cpu", "high_memory", "high_disk", "container_not_running"} {
		if !strings.Contains(string(encoded), marker) {
			t.Fatalf("diagnosis missing signal %q: %s", marker, encoded)
		}
	}
	for _, marker := range []string{"systemd_service_failed", `"failed_services":1`, "failed.service"} {
		if !strings.Contains(string(encoded), marker) {
			t.Fatalf("diagnosis missing failed systemd marker %q: %s", marker, encoded)
		}
	}
	for _, marker := range []string{"systemd_service_inactive", "inactive.service", `"inactive_services"`} {
		if strings.Contains(string(encoded), marker) {
			t.Fatalf("diagnosis incorrectly reported inactive systemd marker %q: %s", marker, encoded)
		}
	}
	if strings.Contains(string(encoded), "should-not-leak") {
		t.Fatalf("diagnosis leaked process command: %s", encoded)
	}
}

func TestPlatformReadToolsReturnUnavailableForMissingSources(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "worker-1", Status: "offline"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	processes := store.NewProcessSnapshotStore(database)
	if err := processes.Upsert(t.Context(), "node-1", protocol.ProcessSnapshot{CollectedAt: 100, Error: "permission denied TOKEN=do-not-leak"}); err != nil {
		t.Fatalf("upsert process error snapshot: %v", err)
	}
	registry := NewRegistry(RegistryDependencies{Nodes: nodes, Processes: processes})
	validated, err := registry.Validate(t.Context(), "get_docker_snapshot", json.RawMessage(`{"node_id":"node-1"}`))
	if err != nil {
		t.Fatalf("validate docker tool: %v", err)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute docker tool: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok || data["available"] != false || data["reason"] != "not_configured" {
		t.Fatalf("unavailable docker projection = %#v", result.Data)
	}

	validated, err = registry.Validate(t.Context(), "list_node_processes", json.RawMessage(`{"node_id":"node-1"}`))
	if err != nil {
		t.Fatalf("validate failed process tool: %v", err)
	}
	result, err = registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute failed process tool: %v", err)
	}
	data, ok = result.Data.(map[string]any)
	if !ok || data["available"] != false || data["reason"] != "query_failed" || strings.Contains(fmt.Sprint(result.Data), "do-not-leak") {
		t.Fatalf("failed process projection = %#v", result.Data)
	}

	validated, err = registry.Validate(t.Context(), "list_k8s_resources", json.RawMessage(`{"cluster_id":"cluster-1","resource":"pods"}`))
	if err != nil {
		t.Fatalf("validate missing Kubernetes tool: %v", err)
	}
	result, err = registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute missing Kubernetes tool: %v", err)
	}
	data, ok = result.Data.(map[string]any)
	if !ok || data["available"] != false || data["reason"] != "not_configured" {
		t.Fatalf("missing Kubernetes projection = %#v", result.Data)
	}
}

func TestPlatformKubernetesResourceUsesStableKeyAndLimit(t *testing.T) {
	stub := platformKubernetesStub{
		cluster: &serverk8s.PublicClusterWithNode{PublicCluster: serverk8s.PublicCluster{ID: "cluster-1", Name: "dev"}},
		pods:    []protocol.K8sPod{{Name: "api-1", Namespace: "default", Status: "Running"}, {Name: "api-2", Namespace: "default", Status: "Pending"}},
	}
	registry := NewRegistry(RegistryDependencies{Kubernetes: stub})
	validated, err := registry.Validate(t.Context(), "list_k8s_resources", json.RawMessage(`{"cluster_id":"cluster-1","resource":"pods","namespace":"default","limit":1}`))
	if err != nil {
		t.Fatalf("validate Kubernetes resource: %v", err)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute Kubernetes resource: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("Kubernetes result type = %T", result.Data)
	}
	pods, ok := data["pods"].([]any)
	if !ok || len(pods) != 1 || data["resource"] != "pods" || data["namespace"] != "default" || data["truncated"] != true {
		t.Fatalf("Kubernetes projection = %#v", data)
	}
}

func TestNodeDiagnosisCapsSignals(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "worker-1", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	containers := make([]protocol.ContainerInfo, 0, maxDiagnosisSignals+5)
	for i := 0; i < maxDiagnosisSignals+5; i++ {
		containers = append(containers, protocol.ContainerInfo{Name: fmt.Sprintf("container-%d", i), State: "exited"})
	}
	docker := store.NewDockerSnapshotStore(database)
	if err := docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{Available: true, Containers: containers}); err != nil {
		t.Fatalf("upsert Docker snapshot: %v", err)
	}
	registry := NewRegistry(RegistryDependencies{Nodes: nodes, Docker: docker})
	validated, err := registry.Validate(t.Context(), "diagnose_node", json.RawMessage(`{"node_id":"node-1"}`))
	if err != nil {
		t.Fatalf("validate diagnosis: %v", err)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute diagnosis: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("diagnosis result type = %T", result.Data)
	}
	signals, ok := data["signals"].([]any)
	if !ok || len(signals) != maxDiagnosisSignals {
		t.Fatalf("diagnosis signals = %T/%d, want %d", data["signals"], len(signals), maxDiagnosisSignals)
	}
}
