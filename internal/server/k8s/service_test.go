package k8s

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mizupanel/mizupanel/internal/protocol"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

type recordingHub struct {
	online  bool
	lastMsg interface{}
	resp    json.RawMessage
}

func (h *recordingHub) IsNodeOnline(nodeID string) bool { return h.online }
func (h *recordingHub) SendToNodeWithTimeout(nodeID string, message interface{}, timeout time.Duration) (json.RawMessage, error) {
	h.lastMsg = message
	return h.resp, nil
}

type contextRecordingHub struct {
	*recordingHub
	contextCalled bool
	contextValue  any
}

func (h *contextRecordingHub) SendToNodeWithContext(ctx context.Context, nodeID string, message interface{}, timeout time.Duration) (json.RawMessage, error) {
	h.contextCalled = true
	h.contextValue = ctx.Value(contextTestKey{})
	return h.recordingHub.SendToNodeWithTimeout(nodeID, message, timeout)
}

type contextTestKey struct{}

func newTestService(t *testing.T, kubeconfigContent, kubeContext string, hub AgentHub) *Service {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	if _, err := database.Exec(`INSERT INTO nodes (id, name, hostname, ip, os, arch, kernel, agent_version, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"node-1", "test-node", "test-node", "127.0.0.1", "linux", "amd64", "", "", "online", now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatalf("create node: %v", err)
	}
	cluster := &Cluster{
		ID:                "cluster-1",
		Name:              "test-cluster",
		NodeID:            "node-1",
		KubeconfigContent: kubeconfigContent,
		Context:           kubeContext,
		Status:            "online",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	store := NewStore(database)
	if err := store.CreateCluster(cluster); err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	return NewService(store, hub)
}

func TestResourceRequestPrefersContextAwareAgentHub(t *testing.T) {
	hub := &contextRecordingHub{recordingHub: &recordingHub{online: true, resp: json.RawMessage(`{"success":true,"deployments":[]}`)}}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)
	ctx := context.WithValue(t.Context(), contextTestKey{}, "request-context")

	deployments, err := service.GetDeployments(ctx, "cluster-1", "default")
	if err != nil {
		t.Fatalf("get deployments: %v", err)
	}
	if !hub.contextCalled || hub.contextValue != "request-context" || deployments == nil {
		t.Fatalf("context-aware send = called:%v value:%v deployments:%#v", hub.contextCalled, hub.contextValue, deployments)
	}
}

func TestGetClusterWithNodeInfoReturnsAgentFields(t *testing.T) {
	hub := &recordingHub{online: true}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)

	cluster, err := service.GetClusterWithNodeInfo("cluster-1")
	if err != nil {
		t.Fatalf("get cluster with node info: %v", err)
	}
	if cluster.NodeName != "test-node" || cluster.NodeIP != "127.0.0.1" || cluster.NodeStatus != "online" {
		t.Fatalf("expected joined node info, got name=%q ip=%q status=%q", cluster.NodeName, cluster.NodeIP, cluster.NodeStatus)
	}
}

func TestGetPodsRequiresStoredKubeconfigContent(t *testing.T) {
	hub := &recordingHub{online: true, resp: json.RawMessage(`{"success":true,"pods":[]}`)}
	service := newTestService(t, "   ", "ctx-a", hub)

	_, err := service.GetPods(context.Background(), "cluster-1", "default")
	if err == nil || !strings.Contains(err.Error(), "集群缺少 kubeconfig 内容，请重新连接集群") {
		t.Fatalf("expected reconnect guidance error, got %v", err)
	}
	if hub.lastMsg != nil {
		t.Fatalf("expected no agent request when kubeconfig content is missing, got %#v", hub.lastMsg)
	}
}

func TestGetPodLogsRequiresStoredKubeconfigContent(t *testing.T) {
	hub := &recordingHub{online: true, resp: json.RawMessage(`{"success":true,"logs":"ok"}`)}
	service := newTestService(t, "\n\t", "ctx-a", hub)

	_, err := service.GetPodLogs(context.Background(), "cluster-1", "default", "pod-a", "", false, 100)
	if err == nil || !strings.Contains(err.Error(), "集群缺少 kubeconfig 内容，请重新连接集群") {
		t.Fatalf("expected reconnect guidance error, got %v", err)
	}
	if hub.lastMsg != nil {
		t.Fatalf("expected no agent request when kubeconfig content is missing, got %#v", hub.lastMsg)
	}
}

func TestGetPodLogsSendsStoredKubeconfigContentAndContext(t *testing.T) {
	hub := &recordingHub{online: true, resp: json.RawMessage(`{"success":true,"logs":"ok"}`)}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)

	_, err := service.GetPodLogs(context.Background(), "cluster-1", "default", "pod-a", "", false, 100)
	if err != nil {
		t.Fatalf("get pod logs: %v", err)
	}
	req, ok := hub.lastMsg.(protocol.K8sGetPodLogsRequest)
	if !ok {
		t.Fatalf("expected K8sGetPodLogsRequest, got %#v", hub.lastMsg)
	}
	if req.KubeconfigContent != "apiVersion: v1\nkind: Config\n" {
		t.Fatalf("expected stored kubeconfig content to be sent")
	}
	if req.Context != "ctx-a" {
		t.Fatalf("expected context ctx-a, got %q", req.Context)
	}
}

func TestGetDiagnosticsSendsStoredKubeconfigContentContextAndResourceIdentity(t *testing.T) {
	hub := &recordingHub{online: true, resp: json.RawMessage(`{"success":true,"diagnostics":{"kind":"pod","namespace":"default","name":"nginx","status":"Running","yaml":"kind: Pod\n","describe":"Name: nginx\n"}}`)}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)

	diagnostics, err := service.GetDiagnostics(context.Background(), "cluster-1", "pod", "default", "nginx")
	if err != nil {
		t.Fatalf("get diagnostics: %v", err)
	}
	if diagnostics == nil || diagnostics.Kind != "pod" || diagnostics.Name != "nginx" {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	req, ok := hub.lastMsg.(protocol.K8sDiagnosticsRequest)
	if !ok {
		t.Fatalf("expected K8sDiagnosticsRequest, got %#v", hub.lastMsg)
	}
	if req.KubeconfigContent != "apiVersion: v1\nkind: Config\n" || req.Context != "ctx-a" {
		t.Fatalf("expected stored kubeconfig/context, got content=%q context=%q", req.KubeconfigContent, req.Context)
	}
	if req.Kind != "pod" || req.Namespace != "default" || req.Name != "nginx" {
		t.Fatalf("unexpected resource identity: %#v", req)
	}
}

func TestGetDiagnosticsRejectsUnsupportedKindBeforeAgentRequest(t *testing.T) {
	hub := &recordingHub{online: true}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)

	_, err := service.GetDiagnostics(context.Background(), "cluster-1", "service", "default", "nginx")
	if err == nil || !strings.Contains(err.Error(), "不支持的资源类型") {
		t.Fatalf("expected unsupported kind error, got %v", err)
	}
	if hub.lastMsg != nil {
		t.Fatalf("expected no agent request, got %#v", hub.lastMsg)
	}
}

func TestExecuteResourceActionSendsStoredKubeconfigContextAndAction(t *testing.T) {
	replicas := int32(4)
	hub := &recordingHub{online: true, resp: json.RawMessage(`{"success":true,"message":"扩缩容成功"}`)}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)

	result, err := service.ExecuteResourceAction(context.Background(), "cluster-1", "deployment", "default", "web", ResourceActionRequest{Action: "scale", Replicas: &replicas})
	if err != nil {
		t.Fatalf("execute action: %v", err)
	}
	if result.Message != "扩缩容成功" {
		t.Fatalf("unexpected action result: %#v", result)
	}
	req, ok := hub.lastMsg.(protocol.K8sResourceActionRequest)
	if !ok {
		t.Fatalf("expected K8sResourceActionRequest, got %#v", hub.lastMsg)
	}
	if req.Type != protocol.MessageTypeK8sResourceAction || req.Kind != "deployment" || req.Namespace != "default" || req.Name != "web" || req.Action != "scale" || req.Replicas == nil || *req.Replicas != 4 {
		t.Fatalf("unexpected action request: %#v", req)
	}
	if req.KubeconfigContent != "apiVersion: v1\nkind: Config\n" || req.Context != "ctx-a" {
		t.Fatalf("expected stored kubeconfig/context, got content=%q context=%q", req.KubeconfigContent, req.Context)
	}
}

func TestExecuteResourceActionRejectsUnsupportedCombinationBeforeAgentRequest(t *testing.T) {
	hub := &recordingHub{online: true}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)

	_, err := service.ExecuteResourceAction(context.Background(), "cluster-1", "daemonset", "default", "fluent-bit", ResourceActionRequest{Action: "scale"})
	if err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("expected unsupported action error, got %v", err)
	}
	if hub.lastMsg != nil {
		t.Fatalf("expected no agent request, got %#v", hub.lastMsg)
	}
}

func TestApplyManifestSendsStoredKubeconfigContextAndYAML(t *testing.T) {
	hub := &recordingHub{online: true, resp: json.RawMessage(`{"success":true,"message":"资源校验成功"}`)}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)
	body := `apiVersion: v1
kind: Namespace
metadata:
  name: staging
`

	result, err := service.ApplyManifest(context.Background(), "cluster-1", ApplyManifestRequest{YAML: body, DryRun: true})
	if err != nil {
		t.Fatalf("apply manifest: %v", err)
	}
	if result.Message != "资源校验成功" {
		t.Fatalf("unexpected apply result: %#v", result)
	}
	req, ok := hub.lastMsg.(protocol.K8sApplyManifestRequest)
	if !ok {
		t.Fatalf("expected K8sApplyManifestRequest, got %#v", hub.lastMsg)
	}
	if req.Type != protocol.MessageTypeK8sApplyManifest || req.ClusterID != "cluster-1" || req.YAML != body || !req.DryRun {
		t.Fatalf("unexpected apply request: %#v", req)
	}
	if req.KubeconfigContent != "apiVersion: v1\nkind: Config\n" || req.Context != "ctx-a" {
		t.Fatalf("expected stored kubeconfig/context, got content=%q context=%q", req.KubeconfigContent, req.Context)
	}
}

func TestApplyManifestRejectsEmptyYAMLBeforeAgentRequest(t *testing.T) {
	hub := &recordingHub{online: true}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)

	_, err := service.ApplyManifest(context.Background(), "cluster-1", ApplyManifestRequest{YAML: " \n\t"})
	if err == nil || !strings.Contains(err.Error(), "YAML 不能为空") {
		t.Fatalf("expected empty YAML error, got %v", err)
	}
	if hub.lastMsg != nil {
		t.Fatalf("expected no agent request, got %#v", hub.lastMsg)
	}
}
