package ai

import (
	"context"
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

type mutationNodeOperationsStub struct {
	platformNodeOperationsStub
	dockerResponse protocol.DockerContainerCreateResponse
	dockerNodeID   string
	dockerRequest  protocol.DockerContainerCreateRequest
	taskRunner     bool
}

func (s *mutationNodeOperationsStub) DockerContainerCreate(_ context.Context, nodeID string, request protocol.DockerContainerCreateRequest) (protocol.DockerContainerCreateResponse, error) {
	s.dockerNodeID = nodeID
	s.dockerRequest = request
	return s.dockerResponse, nil
}

func (*mutationNodeOperationsStub) DockerContainerCreateSupported(string) bool { return true }

func (s *mutationNodeOperationsStub) TaskRunnerSupported(string) bool { return s.taskRunner }

type mutationKubernetesStub struct {
	platformKubernetesStub
	result     *serverk8s.CreateDeploymentResult
	clusterID  string
	deployment serverk8s.CreateDeploymentRequest
}

func (s *mutationKubernetesStub) CreateDeployment(_ context.Context, clusterID string, request serverk8s.CreateDeploymentRequest) (*serverk8s.CreateDeploymentResult, error) {
	s.clusterID = clusterID
	s.deployment = request
	return s.result, nil
}

func newMutationDatabase(t *testing.T) (*sql.DB, *store.NodeStore, *store.TaskStore) {
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
	return database, store.NewNodeStore(database), store.NewTaskStore(database)
}

func TestRegistryCreateDockerContainerUsesTypedRequestAndConfirmationRisk(t *testing.T) {
	_, nodes, _ := newMutationDatabase(t)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "worker", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	ops := &mutationNodeOperationsStub{dockerResponse: protocol.DockerContainerCreateResponse{Supported: true, Success: true, ContainerID: "container-1", Name: "web", Started: true}}
	registry := NewRegistry(RegistryDependencies{Nodes: nodes, AgentOps: ops})

	validated, err := registry.Validate(t.Context(), "create_docker_container", json.RawMessage(`{"node_id":"node-1","image":"nginx:1.27","name":"web","ports":[{"host_port":8080,"container_port":80,"protocol":"tcp"}],"start":true}`))
	if err != nil {
		t.Fatalf("validate Docker create: %v", err)
	}
	if validated.Risk != RiskConfirm || validated.Target.ID != "node-1/web" {
		t.Fatalf("validated Docker create = %+v", validated)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute Docker create: %v", err)
	}
	if result.Status != "success" || ops.dockerNodeID != "node-1" || ops.dockerRequest.Image != "nginx:1.27" || !ops.dockerRequest.Start {
		t.Fatalf("Docker create result/request = %+v/%+v", result, ops.dockerRequest)
	}
	encoded, _ := json.Marshal(result.Data)
	if strings.Contains(string(encoded), "docker run") || strings.Contains(string(encoded), "privileged") {
		t.Fatalf("Docker result exposed command-shaped data: %s", encoded)
	}

	for _, raw := range []string{
		`{"node_id":"node-1","image":"nginx","command":["sh"]}`,
		`{"node_id":"node-1","image":"nginx","environment":{"TOKEN":"secret"}}`,
	} {
		if _, err := registry.Validate(t.Context(), "create_docker_container", json.RawMessage(raw)); !errors.Is(err, ErrInvalidArguments) {
			t.Fatalf("Validate Docker forbidden field error = %v, want ErrInvalidArguments", err)
		}
	}
}

func TestRegistryCreateDockerContainerPreservesPartialIdentityOnStartFailure(t *testing.T) {
	_, nodes, _ := newMutationDatabase(t)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "worker", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	ops := &mutationNodeOperationsStub{dockerResponse: protocol.DockerContainerCreateResponse{
		Supported: true, Created: true, ContainerID: "container-1", Name: "web", Error: "Docker container created but start failed",
	}}
	registry := NewRegistry(RegistryDependencies{Nodes: nodes, AgentOps: ops})
	validated, err := registry.Validate(t.Context(), "create_docker_container", json.RawMessage(`{"node_id":"node-1","image":"nginx:1.27","name":"web","start":true}`))
	if err != nil {
		t.Fatalf("validate Docker create: %v", err)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute partial Docker create: %v", err)
	}
	if result.Status != "failure" || result.OperationID != "container-1" || !strings.Contains(result.Summary, "已创建") {
		t.Fatalf("partial Docker result = %+v", result)
	}
	encoded, _ := json.Marshal(result.Data)
	if !strings.Contains(string(encoded), `"container_id":"container-1"`) || strings.Contains(string(encoded), "port is already allocated") {
		t.Fatalf("partial Docker data = %s", encoded)
	}
}

func TestRegistryCreateScheduledTaskUsesExistingScriptAndOnlineTaskRunner(t *testing.T) {
	_, nodes, tasks := newMutationDatabase(t)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "worker", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	script := store.AutomationScript{Name: "health check", Content: "echo secret-script-content", TimeoutSeconds: 30}
	if err := tasks.CreateScript(t.Context(), &script); err != nil {
		t.Fatalf("create script: %v", err)
	}
	registry := NewRegistry(RegistryDependencies{Nodes: nodes, Tasks: tasks, AgentOps: &mutationNodeOperationsStub{taskRunner: true}})
	validated, err := registry.Validate(t.Context(), "create_scheduled_task", json.RawMessage(`{"name":"nightly check","script_id":1,"node_ids":["node-1"],"cron_expression":"*/5 * * * *","timezone":"UTC","timeout_seconds":30,"notification_policy":"never"}`))
	if err != nil {
		t.Fatalf("validate scheduled task: %v", err)
	}
	if validated.Risk != RiskConfirm {
		t.Fatalf("scheduled task risk = %q, want confirm", validated.Risk)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute scheduled task: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("scheduled task result = %+v", result)
	}
	task, err := tasks.GetScheduledTask(t.Context(), 1)
	if err != nil {
		t.Fatalf("get scheduled task: %v", err)
	}
	if task.Name != "nightly check" || task.ScriptID != script.ID || task.NextRunAt == nil || len(task.NodeIDs) != 1 {
		t.Fatalf("scheduled task = %+v", task)
	}
	encoded, _ := json.Marshal(result.Data)
	if strings.Contains(string(encoded), "secret-script-content") {
		t.Fatalf("scheduled task result leaked script content: %s", encoded)
	}
}

func TestRegistryCreateK8sDeploymentUsesTypedFieldsOnly(t *testing.T) {
	cluster := &serverk8s.PublicClusterWithNode{PublicCluster: serverk8s.PublicCluster{ID: "cluster-1", Name: "prod", NodeID: "node-1", Status: "online"}, NodeStatus: "online"}
	mutations := &mutationKubernetesStub{platformKubernetesStub: platformKubernetesStub{cluster: cluster}, result: &serverk8s.CreateDeploymentResult{Success: true}}
	registry := NewRegistry(RegistryDependencies{Kubernetes: mutations, KubernetesMutations: mutations})
	validated, err := registry.Validate(t.Context(), "create_k8s_deployment", json.RawMessage(`{"cluster_id":"cluster-1","namespace":"default","name":"web","image":"nginx:1.27","replicas":3,"container_port":8080}`))
	if err != nil {
		t.Fatalf("validate Deployment create: %v", err)
	}
	result, err := registry.Execute(t.Context(), validated)
	if err != nil {
		t.Fatalf("execute Deployment create: %v", err)
	}
	if result.Status != "success" || mutations.clusterID != "cluster-1" || mutations.deployment.Namespace != "default" || mutations.deployment.Name != "web" || mutations.deployment.Replicas != 3 || mutations.deployment.ContainerPort == nil || *mutations.deployment.ContainerPort != 8080 {
		t.Fatalf("Deployment result/request = %+v/%+v", result, mutations.deployment)
	}
	for _, raw := range []string{
		`{"cluster_id":"cluster-1","namespace":"default","name":"web","image":"nginx","yaml":"kind: Pod"}`,
		`{"cluster_id":"cluster-1","namespace":"default","name":"web","image":"nginx","kind":"StatefulSet"}`,
	} {
		if _, err := registry.Validate(t.Context(), "create_k8s_deployment", json.RawMessage(raw)); !errors.Is(err, ErrInvalidArguments) {
			t.Fatalf("Validate Deployment forbidden field error = %v, want ErrInvalidArguments", err)
		}
	}
}
