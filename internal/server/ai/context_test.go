package ai

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	serverk8s "github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestOperationalContextProjectsResolvedResourceInventoryAndUnavailableSources(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	nodes := store.NewNodeStore(database)
	for _, node := range []store.Node{
		{ID: "node-online", Name: "Online Node", Status: "online", OS: "linux", Arch: "amd64"},
		{ID: "node-offline", Name: "Offline Node", Status: "offline", OS: "linux", Arch: "arm64"},
	} {
		if err := nodes.Upsert(t.Context(), node); err != nil {
			t.Fatalf("upsert node: %v", err)
		}
	}
	cluster := &serverk8s.PublicClusterWithNode{PublicCluster: serverk8s.PublicCluster{
		ID: "cluster-1", Name: "Production Cluster", NodeID: "node-online", Status: "online", Version: "v1.30", NodeCount: 3, NamespaceCount: 5,
	}, NodeName: "Online Node", NodeStatus: "online"}
	registry := NewRegistry(RegistryDependencies{Nodes: nodes, Kubernetes: platformKubernetesStub{cluster: cluster}})
	if err := registry.ValidateRequestContext(t.Context(), &RequestContext{Page: "overview", ResourceType: "node", ResourceID: "node-online"}); err != nil {
		t.Fatalf("overview node context: %v", err)
	}

	content, err := registry.OperationalContext(t.Context(), &RequestContext{Page: "k8s", ResourceType: "k8s_cluster", ResourceID: "cluster-1"})
	if err != nil {
		t.Fatalf("OperationalContext: %v", err)
	}
	for _, want := range []string{
		`"selected_resource":{"type":"k8s_cluster","id":"cluster-1","name":"Production Cluster","route":"/k8s/clusters/cluster-1","available":true`,
		`"nodes":{"available":true,"count":1,"unavailable_count":1`,
		`{"arch":"arm64","available":false,"id":"node-offline","name":"Offline Node","os":"linux","status":"offline"}`,
		`"kubernetes_clusters":{"available":true,"count":1`,
		`"docker":{"available":false,"reason":"source_unavailable"`,
		`"application_services":{"available":false,"reason":"source_unavailable"`,
		`"operations":[`,
		`"name":"list_nodes","risk":"read","available":true`,
		`"name":"reboot_node","risk":"confirm","available":false,"reason":"source_unavailable"`,
		`"max_plan_steps":5`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("operational context missing %s:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{"kubeconfig", "arguments_json", "operation_id", "api_key", "authorization"} {
		if strings.Contains(strings.ToLower(content), forbidden) {
			t.Fatalf("operational context exposed forbidden field %q: %s", forbidden, content)
		}
	}
}

func TestOperationalContextFiltersUnavailableToolDefinitions(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "Node One", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	registry := NewRegistry(RegistryDependencies{Nodes: nodes})

	content, definitions, err := registry.OperationalContextWithTools(t.Context(), &RequestContext{Page: "overview"})
	if err != nil {
		t.Fatalf("OperationalContextWithTools: %v", err)
	}
	available := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		available[definition.Name] = true
	}
	if !available["list_nodes"] || !available["diagnose_incident"] {
		t.Fatalf("available definitions = %v, want node and incident reads", available)
	}
	for _, unavailable := range []string{"reboot_node", "create_docker_container", "create_k8s_deployment"} {
		if available[unavailable] {
			t.Fatalf("unavailable tool %q was exposed to model", unavailable)
		}
		if !strings.Contains(content, `"name":"`+unavailable+`"`) || !strings.Contains(content, `"available":false`) {
			t.Fatalf("context did not explain unavailable tool %q: %s", unavailable, content)
		}
	}
}

func TestOperationalContextRequiresAgentDockerCreateCapability(t *testing.T) {
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
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "Node One", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	docker := store.NewDockerSnapshotStore(database)
	if err := docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{Available: true, Version: "20.10.24"}); err != nil {
		t.Fatalf("upsert Docker snapshot: %v", err)
	}

	legacy := NewRegistry(RegistryDependencies{Nodes: nodes, Docker: docker, AgentOps: platformNodeOperationsStub{}})
	_, definitions, err := legacy.OperationalContextWithTools(t.Context(), &RequestContext{Page: "overview"})
	if err != nil {
		t.Fatalf("legacy OperationalContextWithTools: %v", err)
	}
	if hasToolDefinition(definitions, "create_docker_container") {
		t.Fatal("Docker create was advertised for a legacy Agent")
	}
	if _, err := legacy.Validate(t.Context(), "create_docker_container", json.RawMessage(`{"node_id":"node-1","image":"nginx:latest"}`)); !errors.Is(err, ErrUnsupportedTool) {
		t.Fatalf("legacy Docker create validation error = %v, want unsupported", err)
	}

	capable := NewRegistry(RegistryDependencies{Nodes: nodes, Docker: docker, AgentOps: platformNodeOperationsStub{dockerCreateSupported: true}})
	_, definitions, err = capable.OperationalContextWithTools(t.Context(), &RequestContext{Page: "overview"})
	if err != nil {
		t.Fatalf("capable OperationalContextWithTools: %v", err)
	}
	if !hasToolDefinition(definitions, "create_docker_container") {
		t.Fatal("Docker create was hidden for a capable Agent")
	}
}

func TestOperationalContextAdvertisesMetricsOnlyWhenANodeIsRegistered(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	nodes := store.NewNodeStore(database)
	registry := NewRegistry(RegistryDependencies{Nodes: nodes, Metrics: store.NewMetricStore(database)})

	_, definitions, err := registry.OperationalContextWithTools(t.Context(), &RequestContext{Page: "overview"})
	if err != nil {
		t.Fatalf("empty OperationalContextWithTools: %v", err)
	}
	if hasToolDefinition(definitions, "get_node_metrics") {
		t.Fatal("get_node_metrics was advertised without any registered node")
	}

	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-offline", Name: "Offline Node", Status: "offline"}); err != nil {
		t.Fatalf("upsert offline node: %v", err)
	}
	_, definitions, err = registry.OperationalContextWithTools(t.Context(), &RequestContext{Page: "overview"})
	if err != nil {
		t.Fatalf("offline OperationalContextWithTools: %v", err)
	}
	if !hasToolDefinition(definitions, "get_node_metrics") {
		t.Fatal("get_node_metrics was hidden for a registered offline node")
	}
}

func hasToolDefinition(definitions []ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func TestOperationalContextRejectsMismatchedOrMissingResources(t *testing.T) {
	registry := NewRegistry(RegistryDependencies{})
	for _, request := range []*RequestContext{
		{Page: "unknown"},
		{Page: "hosts", ResourceType: "node"},
		{Page: "services", ResourceType: "node", ResourceID: "node-1"},
		{Page: "hosts", ResourceType: "node", ResourceID: "missing"},
	} {
		if err := registry.ValidateRequestContext(t.Context(), request); err == nil {
			t.Fatalf("ValidateRequestContext(%+v) succeeded", request)
		}
	}
}

func TestEncodeOperationalContextKeepsBoundedProjectionValid(t *testing.T) {
	items := make([]map[string]any, 0, maxCapabilityItems)
	for index := 0; index < maxCapabilityItems; index++ {
		items = append(items, map[string]any{
			"id":    strings.Repeat("i", 191),
			"name":  strings.Repeat("n", 128),
			"state": strings.Repeat("s", 64),
		})
	}
	source := availableCapability(items, len(items))
	content, err := encodeOperationalContext(operationalContext{
		Page: "overview",
		Capabilities: PlatformCapabilityProjection{
			Nodes: source, KubernetesClusters: source, Docker: source, Compose: source,
			Systemd: source, TaskRunner: source, ApplicationServices: source,
		},
	})
	if err != nil {
		t.Fatalf("encodeOperationalContext: %v", err)
	}
	if len(content) > maxOperationalContext || !strings.HasSuffix(content, operationalContextSuffix) {
		t.Fatalf("bounded context length/suffix = %d/%t", len(content), strings.HasSuffix(content, operationalContextSuffix))
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(content, operationalContextPrefix), operationalContextSuffix)
	if !json.Valid([]byte(encoded)) {
		t.Fatalf("bounded operational context is not valid JSON: %s", encoded)
	}
	if !strings.Contains(encoded, `"truncated":true`) {
		t.Fatalf("bounded operational context did not mark truncated sources: %s", encoded)
	}
}
