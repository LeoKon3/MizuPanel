package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serverk8s "github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type resourceGraphNodeOperationsStub struct {
	platformNodeOperationsStub
	compose protocol.DockerComposeListResponse
	systemd protocol.SystemdServiceListResponse
}

func (s resourceGraphNodeOperationsStub) DockerComposeList(context.Context, string) (protocol.DockerComposeListResponse, error) {
	return s.compose, nil
}

func (s resourceGraphNodeOperationsStub) SystemdServiceList(context.Context, string) (protocol.SystemdServiceListResponse, error) {
	return s.systemd, nil
}

func TestGraphRefIsDeterministicAndRejectsNonCanonicalIdentity(t *testing.T) {
	ref := graphRef("node", "", "", "", "node-1")
	if ref != graphRef("node", "", "", "", "node-1") || !validGraphRef(ref) {
		t.Fatalf("generated graph ref is not deterministic/canonical: %q", ref)
	}
	identity, err := json.Marshal([]string{"", "", "", "node-1"})
	if err != nil {
		t.Fatalf("marshal identity: %v", err)
	}
	nonCanonical := "rg:v1:node:" + base64.RawURLEncoding.EncodeToString(identityWithWhitespace())
	if validGraphRef(nonCanonical) {
		t.Fatalf("accepted non-canonical graph ref %q", nonCanonical)
	}
	if validGraphRef("rg:v1:node:not-json") || !validGraphRef("rg:v1:node:"+base64.RawURLEncoding.EncodeToString(identity)) {
		t.Fatalf("validGraphRef rejected or accepted malformed identity unexpectedly")
	}
}

func identityWithWhitespace() []byte {
	return []byte(" [ \"\", \"\", \"\", \"node-1\" ] ")
}

func TestGraphRelationshipsAreDirectOnlyAndDuplicateNamesKeepScope(t *testing.T) {
	builder := newGraphBuilder()
	nodeA := graphRef("node", "", "", "", "node-a")
	nodeB := graphRef("node", "", "", "", "node-b")
	serviceA := graphRef("systemd_service", "node-a", "", "", "web.service")
	serviceB := graphRef("systemd_service", "node-b", "", "", "web.service")
	for _, resource := range []GraphResource{
		{Ref: nodeA, Type: "node", Name: "worker", Key: "node-a"},
		{Ref: nodeB, Type: "node", Name: "worker", Key: "node-b"},
		{Ref: serviceA, Type: "systemd_service", Name: "web.service", Key: "web.service", NodeID: "node-a"},
		{Ref: serviceB, Type: "systemd_service", Name: "web.service", Key: "web.service", NodeID: "node-b"},
	} {
		if !builder.addResource(resource) {
			t.Fatalf("add resource failed: %+v", resource)
		}
	}
	builder.addEdge(serviceA, nodeA, "runs_on")
	builder.addEdge(serviceB, nodeB, "runs_on")
	snapshot := builder.finish()
	incoming, outgoing := graphRelationships(snapshot, serviceA)
	if len(incoming) != 0 || len(outgoing) != 1 || outgoing[0].Resource.Ref != nodeA {
		t.Fatalf("service topology = incoming:%+v outgoing:%+v", incoming, outgoing)
	}
	if _, ok := snapshot.byRef[serviceB]; !ok || serviceA == serviceB {
		t.Fatalf("duplicate display names lost their scoped identities")
	}
}

func TestGraphComposeSourceCountsAllItemsAfterProjectionCap(t *testing.T) {
	projects := make([]protocol.DockerComposeProject, 101)
	for index := range projects {
		projects[index] = protocol.DockerComposeProject{Name: fmt.Sprintf("project-%03d", index), Services: []protocol.DockerComposeService{{Name: "web", State: "running"}}}
	}
	registry := &Registry{dependencies: RegistryDependencies{AgentOps: resourceGraphNodeOperationsStub{
		compose: protocol.DockerComposeListResponse{Success: true, Supported: true, Projects: projects},
	}}}
	builder := newGraphBuilder()
	registry.addGraphNodeRemote(t.Context(), builder, []store.Node{{ID: "node-1", Status: "online"}})
	snapshot := builder.finish()
	var compose GraphSourceStatus
	for _, source := range snapshot.Sources {
		if source.Name == "compose" {
			compose = source
		}
	}
	if compose.Count != 202 || !compose.Truncated || !compose.Available {
		t.Fatalf("compose source = %+v, want full count 202 and truncation", compose)
	}
	if count := len(snapshot.Resources); count != 100 {
		t.Fatalf("compose projected resources = %d, want 100", count)
	}
}

func TestGraphKubernetesWorkloadsStayWithinSourceCap(t *testing.T) {
	deployments := make([]protocol.K8sDeployment, 101)
	for index := range deployments {
		deployments[index] = protocol.K8sDeployment{Name: fmt.Sprintf("deployment-%03d", index), Namespace: "default", Ready: "1/1"}
	}
	cluster := &serverk8s.PublicClusterWithNode{
		PublicCluster: serverk8s.PublicCluster{ID: "cluster-1", Name: "production", NodeID: "node-1", Status: "online"},
		NodeStatus:    "online",
	}
	registry := &Registry{dependencies: RegistryDependencies{Kubernetes: incidentKubernetesStub{cluster: cluster, deployments: deployments}}}
	builder := newGraphBuilder()
	registry.addGraphKubernetes(t.Context(), builder)
	snapshot := builder.finish()
	workloads := 0
	var source GraphSourceStatus
	for _, resource := range snapshot.Resources {
		if resource.Type == "k8s_workload" {
			workloads++
		}
	}
	for _, candidate := range snapshot.Sources {
		if candidate.Name == "kubernetes_workloads" {
			source = candidate
		}
	}
	if workloads != maxGraphSourceItems || source.Count != 101 || !source.Truncated || !source.Available {
		t.Fatalf("Kubernetes projection = workloads:%d source:%+v", workloads, source)
	}
}

func TestGraphApplicationServicePreservesDanglingDirectAssociation(t *testing.T) {
	services := incidentServicesStub{services: []servicecenter.ServiceSummary{{
		ID: "service-1", Name: "checkout", Health: servicecenter.HealthUnknown,
		Resources: []servicecenter.ResourceProjection{{Resource: servicecenter.Resource{
			ResourceType: servicecenter.ResourceSystemdService, ScopeID: "node-removed", ResourceKey: "checkout.service", DisplayName: "checkout.service",
		}}},
	}}}
	registry := &Registry{dependencies: RegistryDependencies{Services: services}}
	snapshot, err := registry.resourceGraphSnapshot(t.Context())
	if err != nil {
		t.Fatalf("resourceGraphSnapshot: %v", err)
	}
	serviceRef := graphRef("application_service", "", "", "", "service-1")
	incoming, outgoing := graphRelationships(snapshot, serviceRef)
	if len(incoming) != 1 || len(outgoing) != 1 {
		t.Fatalf("application topology = incoming:%+v outgoing:%+v", incoming, outgoing)
	}
	if incoming[0].Resource.State != "missing" || incoming[0].Resource.Available || outgoing[0].Resource.Ref != incoming[0].Resource.Ref {
		t.Fatalf("dangling association was not preserved safely: incoming:%+v outgoing:%+v", incoming, outgoing)
	}
}

func TestGraphSnapshotIsBoundedAndRedactsDisplayText(t *testing.T) {
	builder := newGraphBuilder()
	refs := make([]string, 500)
	for index := range refs {
		refs[index] = graphRef("node", "", "", "", fmt.Sprintf("node-%03d", index))
		if !builder.addResource(GraphResource{Ref: refs[index], Type: "node", Name: strings.Repeat("x", 256), State: "token=do-not-expose", NodeID: strings.Repeat("n", 191), ScopeID: strings.Repeat("s", 191), Kind: strings.Repeat("k", 32), Namespace: strings.Repeat("q", 191), Key: strings.Repeat("i", 255)}) {
			t.Fatalf("add resource %d failed", index)
		}
	}
	for index := 0; index < 499; index++ {
		builder.addEdge(refs[index], refs[index+1], "runs_on")
	}
	snapshot := builder.finish()
	encoded, err := json.Marshal(snapshot)
	if err != nil || len(encoded) > maxGraphSnapshotSize {
		t.Fatalf("snapshot size = %d, err=%v, max=%d", len(encoded), err, maxGraphSnapshotSize)
	}
	if !snapshot.Truncated {
		t.Fatal("large graph did not report truncation")
	}
	if strings.Contains(string(encoded), "do-not-expose") {
		t.Fatalf("resource state leaked unsanitized secret marker: %s", encoded)
	}
}

func TestResourceGraphToolValidationUsesClosedFilters(t *testing.T) {
	registry := NewRegistry(RegistryDependencies{})
	if _, err := registry.Validate(t.Context(), "search_resources", json.RawMessage(`{"state":"not-a-state"}`)); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("invalid state error = %v, want ErrInvalidArguments", err)
	}
	if _, err := registry.Validate(t.Context(), "get_resource_topology", json.RawMessage(`{"resource_ref":"rg:v1:node:not-json"}`)); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("invalid topology ref error = %v, want ErrInvalidArguments", err)
	}
	for _, state := range graphSearchStates {
		if !graphResourceMatchesState(GraphResource{Available: state == "available", State: state}, state) && state != "running" && state != "healthy" {
			t.Fatalf("state %q did not match its normalized state", state)
		}
	}
}

func TestRunGraphJobsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	runGraphJobs(ctx, []func(context.Context){func(context.Context) { called = true }})
	if called {
		t.Fatal("cancelled graph job ran after cancellation")
	}
}
