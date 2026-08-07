package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	serverk8s "github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type incidentServicesStub struct {
	services []servicecenter.ServiceSummary
	err      error
}

func (s incidentServicesStub) List(context.Context) ([]servicecenter.ServiceSummary, error) {
	return s.services, s.err
}

type incidentKubernetesStub struct {
	cluster        *serverk8s.PublicClusterWithNode
	summary        *protocol.K8sResourceSummary
	pods           []protocol.K8sPod
	deployments    []protocol.K8sDeployment
	podsErr        error
	deploymentsErr error
	summaryErr     error
}

func (s incidentKubernetesStub) ListClustersWithNodeInfo() ([]*serverk8s.PublicClusterWithNode, error) {
	return []*serverk8s.PublicClusterWithNode{s.cluster}, nil
}

func (s incidentKubernetesStub) GetClusterWithNodeInfo(id string) (*serverk8s.PublicClusterWithNode, error) {
	if s.cluster == nil || s.cluster.ID != id {
		return nil, nil
	}
	return s.cluster, nil
}

func (s incidentKubernetesStub) GetSummary(context.Context, string) (*protocol.K8sResourceSummary, error) {
	return s.summary, s.summaryErr
}

func (incidentKubernetesStub) GetNamespaces(context.Context, string) ([]protocol.K8sNamespace, error) {
	return nil, nil
}

func (incidentKubernetesStub) GetNodes(context.Context, string) ([]protocol.K8sNode, error) {
	return nil, nil
}

func (s incidentKubernetesStub) GetPods(context.Context, string, string) ([]protocol.K8sPod, error) {
	return s.pods, s.podsErr
}

func (s incidentKubernetesStub) GetDeployments(context.Context, string, string) ([]protocol.K8sDeployment, error) {
	return s.deployments, s.deploymentsErr
}

func (incidentKubernetesStub) GetStatefulSets(context.Context, string, string) ([]protocol.K8sStatefulSet, error) {
	return nil, nil
}

func (incidentKubernetesStub) GetDaemonSets(context.Context, string, string) ([]protocol.K8sDaemonSet, error) {
	return nil, nil
}

func (incidentKubernetesStub) GetServices(context.Context, string, string) ([]protocol.K8sService, error) {
	return nil, nil
}

func (incidentKubernetesStub) GetIngresses(context.Context, string, string) ([]protocol.K8sIngress, error) {
	return nil, nil
}

func TestIncidentDiagnosisValidatesScopeAndWindow(t *testing.T) {
	database := newIncidentDatabase(t)
	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "worker", Status: "offline"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	cluster := &serverk8s.PublicClusterWithNode{PublicCluster: serverk8s.PublicCluster{ID: "cluster-1", Name: "dev"}}
	services := incidentServicesStub{services: []servicecenter.ServiceSummary{{ID: "service-1", Name: "api"}}}
	registry := NewRegistry(RegistryDependencies{Nodes: nodes, Kubernetes: incidentKubernetesStub{cluster: cluster}, Services: services})

	valid := []struct {
		raw        string
		targetType string
		window     int
	}{
		{`{"scope_type":"platform"}`, "platform", defaultIncidentWindowMinutes},
		{`{"scope_type":"node","scope_id":"node-1","window_minutes":5}`, "node", 5},
		{`{"scope_type":"k8s_cluster","scope_id":"cluster-1","window_minutes":1440}`, "k8s_cluster", 1440},
		{`{"scope_type":"application_service","scope_id":"service-1"}`, "application_service", defaultIncidentWindowMinutes},
	}
	for _, test := range valid {
		call, err := registry.Validate(t.Context(), "diagnose_incident", json.RawMessage(test.raw))
		if err != nil {
			t.Fatalf("validate %s: %v", test.raw, err)
		}
		if call.Risk != RiskRead || call.Target.Type != test.targetType {
			t.Fatalf("validated target = %#v risk=%q", call.Target, call.Risk)
		}
		var args incidentDiagnosisArguments
		if err := json.Unmarshal(call.Arguments, &args); err != nil || args.WindowMinutes != test.window {
			t.Fatalf("normalized arguments = %s, err=%v", call.Arguments, err)
		}
	}

	for _, raw := range []string{
		`{"scope_type":"platform","scope_id":"node-1"}`,
		`{"scope_type":"node"}`,
		`{"scope_type":"unknown"}`,
		`{"scope_type":"node","scope_id":"node-1","window_minutes":4}`,
		`{"scope_type":"node","scope_id":"node-1","window_minutes":1441}`,
		`{"scope_type":"node","scope_id":"node-1","url":"https://example.com"}`,
		`{"scope_type":"node","scope_id":"missing"}`,
	} {
		if _, err := registry.Validate(t.Context(), "diagnose_incident", json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid diagnosis arguments accepted: %s", raw)
		}
	}
}

func TestIncidentDiagnosisNodeRedactsSecretsAndRanksEvidence(t *testing.T) {
	database := newIncidentDatabase(t)
	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "worker", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	metrics := store.NewMetricStore(database)
	if err := metrics.Insert(t.Context(), store.Metric{NodeID: "node-1", CPUCores: 2, CPUUsage: 85, DiskUsage: 95, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("insert metrics: %v", err)
	}
	processes := store.NewProcessSnapshotStore(database)
	if err := processes.Upsert(t.Context(), "node-1", protocol.ProcessSnapshot{CollectedAt: time.Now().Unix(), Processes: []protocol.ProcessInfo{{PID: 7, Name: "worker TOKEN=secret-value", Command: "--password=must-not-leak", CPUUsage: 70}}}); err != nil {
		t.Fatalf("upsert process snapshot: %v", err)
	}
	docker := store.NewDockerSnapshotStore(database)
	if err := docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{CollectedAt: time.Now().Add(-2 * time.Hour).Unix(), Available: true, Containers: []protocol.ContainerInfo{{Name: "stale PASSWORD=docker-secret", State: "exited"}}}); err != nil {
		t.Fatalf("upsert Docker snapshot: %v", err)
	}

	data := executeIncidentDiagnosisTest(t, NewRegistry(RegistryDependencies{Nodes: nodes, Metrics: metrics, Processes: processes, Docker: docker}), `{"scope_type":"node","scope_id":"node-1"}`)
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal diagnosis: %v", err)
	}
	for _, secret := range []string{"secret-value", "must-not-leak", "--password", "docker-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("diagnosis leaked %q: %s", secret, encoded)
		}
	}
	sources := data["sources"].(map[string]incidentSourceStatus)
	if sources["processes"].Available != true || sources["docker"].Reason != "outside_window" || sources["compose"].Reason != "not_configured" {
		t.Fatalf("source status = %#v", sources)
	}
	window := data["window"].(map[string]any)
	if window["minutes"] != defaultIncidentWindowMinutes || window["from"] == "" || window["to"] == "" {
		t.Fatalf("diagnosis window = %#v", window)
	}
	evidence := data["evidence"].([]incidentEvidence)
	if len(evidence) < 3 || evidence[0].Kind != "high_disk" || evidence[0].Severity != "critical" {
		t.Fatalf("ranked evidence = %#v", evidence)
	}
	for _, item := range evidence {
		if item.RouteKey != "node" || strings.Contains(item.RouteKey, "/") || strings.Contains(item.RouteKey, "://") {
			t.Fatalf("unsafe route projection = %#v", item)
		}
	}
}

func TestIncidentDiagnosisKubernetesKeepsPartialFailures(t *testing.T) {
	cluster := &serverk8s.PublicClusterWithNode{PublicCluster: serverk8s.PublicCluster{ID: "cluster-1", Name: "dev", Status: "online"}, NodeStatus: "online"}
	stub := incidentKubernetesStub{
		cluster:        cluster,
		summary:        &protocol.K8sResourceSummary{PodCount: 1, DeploymentCount: 1},
		pods:           []protocol.K8sPod{{Name: "api TOKEN=k8s-secret", Namespace: "default", Status: "Pending", Ready: "0/1"}},
		deploymentsErr: errors.New("upstream PASSWORD=must-not-leak"),
	}
	data := executeIncidentDiagnosisTest(t, NewRegistry(RegistryDependencies{Kubernetes: stub}), `{"scope_type":"k8s_cluster","scope_id":"cluster-1"}`)
	sources := data["sources"].(map[string]incidentSourceStatus)
	if !sources["kubernetes_pods"].Available || sources["kubernetes_pods"].EvidenceCount != 1 || sources["kubernetes_deployments"].Available || sources["kubernetes_deployments"].Reason != "query_failed" || !sources["kubernetes_summary"].Available {
		t.Fatalf("Kubernetes sources = %#v", sources)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal Kubernetes diagnosis: %v", err)
	}
	if strings.Contains(string(encoded), "k8s-secret") || strings.Contains(string(encoded), "must-not-leak") {
		t.Fatalf("Kubernetes diagnosis leaked source data: %s", encoded)
	}
}

func TestIncidentDiagnosisApplicationServiceProjectsRelatedResources(t *testing.T) {
	services := incidentServicesStub{services: []servicecenter.ServiceSummary{{
		ID: "service-1", Name: "payments TOKEN=service-secret", Health: servicecenter.HealthUnhealthy,
		Resources: []servicecenter.ResourceProjection{{Resource: servicecenter.Resource{DisplayName: "deployment PASSWORD=resource-secret"}, Health: servicecenter.HealthDegraded}},
	}}}
	data := executeIncidentDiagnosisTest(t, NewRegistry(RegistryDependencies{Services: services}), `{"scope_type":"application_service","scope_id":"service-1"}`)
	evidence := data["evidence"].([]incidentEvidence)
	if len(evidence) != 2 || evidence[0].Kind != "application_service_unhealthy" || evidence[1].Kind != "application_resource_unhealthy" {
		t.Fatalf("service evidence = %#v", evidence)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal service diagnosis: %v", err)
	}
	if strings.Contains(string(encoded), "service-secret") || strings.Contains(string(encoded), "resource-secret") {
		t.Fatalf("service diagnosis leaked source data: %s", encoded)
	}
}

func TestIncidentDiagnosisPlatformAggregatesPartialSourcesAndCapsEvidence(t *testing.T) {
	database := newIncidentDatabase(t)
	nodes := store.NewNodeStore(database)
	for index := 0; index < maxIncidentEvidence+5; index++ {
		status := "offline"
		if index < 2 {
			status = "online"
		}
		if err := nodes.Upsert(t.Context(), store.Node{ID: fmt.Sprintf("node-%02d", index), Name: "node", Status: status}); err != nil {
			t.Fatalf("upsert node %d: %v", index, err)
		}
	}
	metrics := store.NewMetricStore(database)
	if err := metrics.Insert(t.Context(), store.Metric{NodeID: "node-00", DiskUsage: 95, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("insert metric: %v", err)
	}
	data := executeIncidentDiagnosisTest(t, NewRegistry(RegistryDependencies{Nodes: nodes, Metrics: metrics}), `{"scope_type":"platform"}`)
	sources := data["sources"].(map[string]incidentSourceStatus)
	if !sources["metrics"].Available || sources["metrics"].Reason != "partial_failure" {
		t.Fatalf("aggregated metrics source = %#v", sources["metrics"])
	}
	evidence := data["evidence"].([]incidentEvidence)
	if len(evidence) != maxIncidentEvidence {
		t.Fatalf("evidence count = %d, want %d", len(evidence), maxIncidentEvidence)
	}
}

func TestRankIncidentEvidenceUsesSeverityScopeRecencyAndCorroboration(t *testing.T) {
	observed := "2026-08-07T10:00:00Z"
	items := rankIncidentEvidence([]incidentEvidence{
		{Kind: "single", Severity: "warning", ObservedAt: observed, Source: "metrics", ResourceType: "node", ResourceID: "node-2"},
		{Kind: "corroborated-a", Severity: "warning", ObservedAt: observed, Source: "metrics", ResourceType: "node", ResourceID: "node-3"},
		{Kind: "direct", Severity: "warning", ObservedAt: "2026-08-07T09:00:00Z", Source: "metrics", ResourceType: "node", ResourceID: "node-1"},
		{Kind: "critical", Severity: "critical", ObservedAt: "2026-08-07T08:00:00Z", Source: "nodes", ResourceType: "node", ResourceID: "node-4"},
		{Kind: "corroborated-b", Severity: "warning", ObservedAt: observed, Source: "alerts", ResourceType: "node", ResourceID: "node-3"},
	}, "node", "node-1")
	if items[0].Kind != "critical" || items[1].Kind != "direct" || items[2].ResourceID != "node-3" || items[3].ResourceID != "node-3" || items[4].Kind != "single" {
		t.Fatalf("ranked evidence = %#v", items)
	}
}

func newIncidentDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return database
}

func executeIncidentDiagnosisTest(t *testing.T, registry *Registry, raw string) map[string]any {
	t.Helper()
	call, err := registry.Validate(t.Context(), "diagnose_incident", json.RawMessage(raw))
	if err != nil {
		t.Fatalf("validate diagnosis: %v", err)
	}
	result, err := registry.Execute(t.Context(), call)
	if err != nil {
		t.Fatalf("execute diagnosis: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("diagnosis result type = %T", result.Data)
	}
	return data
}
