package ai

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

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
		`"kubernetes_clusters":{"available":true,"count":1`,
		`"docker":{"available":false,"reason":"source_unavailable"`,
		`"application_services":{"available":false,"reason":"source_unavailable"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("operational context missing %s:\n%s", want, content)
		}
	}
	if strings.Contains(content, "kubeconfig") {
		t.Fatalf("operational context exposed kubeconfig data: %s", content)
	}
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
