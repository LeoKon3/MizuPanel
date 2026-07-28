package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mizupanel/mizupanel/internal/protocol"
)

// Service K8s 业务逻辑层
type Service struct {
	store *Store
	hub   AgentHub
	debug bool
}

// AgentHub Agent 通信接口
type AgentHub interface {
	IsNodeOnline(nodeID string) bool
	SendToNodeWithTimeout(nodeID string, message interface{}, timeout time.Duration) (json.RawMessage, error)
}

type contextAgentHub interface {
	SendToNodeWithContext(ctx context.Context, nodeID string, message interface{}, timeout time.Duration) (json.RawMessage, error)
}

// NewService 创建业务逻辑层
func NewService(store *Store, hub AgentHub) *Service {
	return &Service{
		store: store,
		hub:   hub,
	}
}

func (s *Service) SetDebug(debug bool) {
	s.debug = debug
}

// ConnectClusterRequest 连接集群请求
type ConnectClusterRequest struct {
	Name              string `json:"name"`
	NodeID            string `json:"node_id"`
	KubeconfigPath    string `json:"kubeconfig_path,omitempty"`
	KubeconfigContent string `json:"kubeconfig_content"`
	Context           string `json:"context,omitempty"`
}

// ConnectCluster 连接 K8s 集群
func (s *Service) ConnectCluster(ctx context.Context, req *ConnectClusterRequest) (*Cluster, *ClusterInfo, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, nil, fmt.Errorf("集群名称不能为空")
	}
	if strings.TrimSpace(req.NodeID) == "" {
		return nil, nil, fmt.Errorf("Agent 节点不能为空")
	}
	if strings.TrimSpace(req.KubeconfigContent) == "" {
		return nil, nil, fmt.Errorf("kubeconfig 内容不能为空")
	}

	// 1. 验证节点在线
	if !s.hub.IsNodeOnline(req.NodeID) {
		return nil, nil, fmt.Errorf("Agent 节点离线，无法连接集群")
	}

	// 2. 生成 ClusterID
	clusterID := uuid.New().String()
	requestID := uuid.New().String()

	// 3. 通过 Agent 验证 kubeconfig
	agentReq := protocol.K8sClusterConnectRequest{
		Type:              protocol.MessageTypeK8sClusterConnect,
		RequestID:         requestID,
		ClusterID:         clusterID,
		KubeconfigContent: req.KubeconfigContent,
		Context:           req.Context,
	}

	start := time.Now()
	if s.debug {
		log.Printf("[debug][server][k8s] connect start request_id=%s cluster_id=%s node_id=%s context=%s", requestID, clusterID, req.NodeID, req.Context)
	}
	rawResp, err := s.hub.SendToNodeWithTimeout(req.NodeID, agentReq, 10*time.Second)
	if err != nil {
		if s.debug {
			log.Printf("[debug][server][k8s] connect done request_id=%s cluster_id=%s node_id=%s elapsed=%s error=%v", requestID, clusterID, req.NodeID, time.Since(start), err)
		}
		return nil, nil, fmt.Errorf("Agent 响应超时: %w", err)
	}
	if s.debug {
		log.Printf("[debug][server][k8s] connect response request_id=%s cluster_id=%s node_id=%s elapsed=%s", requestID, clusterID, req.NodeID, time.Since(start))
	}

	var agentResp protocol.K8sClusterConnectResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}

	if !agentResp.Success {
		if s.debug {
			log.Printf("[debug][server][k8s] connect done request_id=%s cluster_id=%s node_id=%s elapsed=%s agent_success=false error=%s", requestID, clusterID, req.NodeID, time.Since(start), agentResp.Error)
		}
		return nil, nil, fmt.Errorf("连接失败: %s", agentResp.Error)
	}
	if s.debug {
		log.Printf("[debug][server][k8s] connect done request_id=%s cluster_id=%s node_id=%s elapsed=%s success=true", requestID, clusterID, req.NodeID, time.Since(start))
	}

	// 3. 保存集群信息到数据库
	now := time.Now()
	cluster := &Cluster{
		ID:                clusterID,
		Name:              req.Name,
		NodeID:            req.NodeID,
		KubeconfigPath:    req.KubeconfigPath,
		KubeconfigContent: req.KubeconfigContent,
		Context:           req.Context,
		Status:            "online",
		Version:           agentResp.ClusterInfo.Version,
		NodeCount:         agentResp.ClusterInfo.NodeCount,
		NamespaceCount:    agentResp.ClusterInfo.NamespaceCount,
		LastSeenAt:        now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.store.CreateCluster(cluster); err != nil {
		return nil, nil, fmt.Errorf("保存集群信息失败: %w", err)
	}

	// 4. 返回集群信息
	clusterInfo := &ClusterInfo{
		Version:        agentResp.ClusterInfo.Version,
		NodeCount:      agentResp.ClusterInfo.NodeCount,
		NamespaceCount: agentResp.ClusterInfo.NamespaceCount,
	}

	return cluster, clusterInfo, nil
}

// GetCluster 获取集群信息
func (s *Service) GetCluster(id string) (*Cluster, error) {
	return s.store.GetCluster(id)
}

// GetClusterWithNodeInfo 获取集群详情（包含节点信息）
func (s *Service) GetClusterWithNodeInfo(id string) (*PublicClusterWithNode, error) {
	return s.store.GetClusterWithNodeInfo(id)
}

// ListClusters 获取集群列表
func (s *Service) ListClusters() ([]*Cluster, error) {
	return s.store.ListClusters()
}

// ListClustersWithNodeInfo 获取集群列表（包含节点信息）
func (s *Service) ListClustersWithNodeInfo() ([]*PublicClusterWithNode, error) {
	return s.store.ListClustersWithNodeInfo()
}

// DeleteCluster 删除集群
func (s *Service) DeleteCluster(id string) error {
	return s.store.DeleteCluster(id)
}

// GetPods 获取 Pod 列表
func (s *Service) GetPods(ctx context.Context, clusterID, namespace string) ([]protocol.K8sPod, error) {
	// 1. 获取集群信息
	cluster, err := s.store.GetCluster(clusterID)
	if err != nil {
		return nil, err
	}

	// 2. 验证节点在线
	if !s.hub.IsNodeOnline(cluster.NodeID) {
		return nil, fmt.Errorf("Agent 节点离线")
	}
	if strings.TrimSpace(cluster.KubeconfigContent) == "" {
		return nil, fmt.Errorf("集群缺少 kubeconfig 内容，请重新连接集群")
	}

	// 3. 发送查询请求到 Agent
	requestID := uuid.New().String()
	agentReq := protocol.K8sGetPodsRequest{
		Type:              protocol.MessageTypeK8sGetPods,
		RequestID:         requestID,
		ClusterID:         clusterID,
		Namespace:         namespace,
		KubeconfigContent: cluster.KubeconfigContent,
		Context:           cluster.Context,
	}

	start := time.Now()
	if s.debug {
		log.Printf("[debug][server][k8s] resource start type=%s request_id=%s cluster_id=%s node_id=%s namespace=%s", agentReq.Type, requestID, clusterID, cluster.NodeID, namespace)
	}
	rawResp, err := s.hub.SendToNodeWithTimeout(cluster.NodeID, agentReq, 15*time.Second)
	if err != nil {
		if s.debug {
			log.Printf("[debug][server][k8s] resource done type=%s request_id=%s cluster_id=%s node_id=%s namespace=%s elapsed=%s error=%v", agentReq.Type, requestID, clusterID, cluster.NodeID, namespace, time.Since(start), err)
		}
		return nil, fmt.Errorf("Agent 响应超时: %w", err)
	}
	if s.debug {
		log.Printf("[debug][server][k8s] resource response type=%s request_id=%s cluster_id=%s node_id=%s namespace=%s elapsed=%s", agentReq.Type, requestID, clusterID, cluster.NodeID, namespace, time.Since(start))
	}

	var agentResp protocol.K8sGetPodsResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}

	if !agentResp.Success {
		if s.debug {
			log.Printf("[debug][server][k8s] resource done type=%s request_id=%s cluster_id=%s node_id=%s namespace=%s elapsed=%s agent_success=false error=%s", agentReq.Type, requestID, clusterID, cluster.NodeID, namespace, time.Since(start), agentResp.Error)
		}
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	if s.debug {
		log.Printf("[debug][server][k8s] resource done type=%s request_id=%s cluster_id=%s node_id=%s namespace=%s elapsed=%s success=true", agentReq.Type, requestID, clusterID, cluster.NodeID, namespace, time.Since(start))
	}

	return agentResp.Pods, nil
}

// GetPodLogs 获取 Pod 日志
func (s *Service) GetPodLogs(ctx context.Context, clusterID, namespace, podName, container string, follow bool, tailLines int) (string, error) {
	// 1. 获取集群信息
	cluster, err := s.store.GetCluster(clusterID)
	if err != nil {
		return "", err
	}

	// 2. 验证节点在线
	if !s.hub.IsNodeOnline(cluster.NodeID) {
		return "", fmt.Errorf("Agent 节点离线")
	}
	if strings.TrimSpace(cluster.KubeconfigContent) == "" {
		return "", fmt.Errorf("集群缺少 kubeconfig 内容，请重新连接集群")
	}

	// 3. 发送日志请求到 Agent
	requestID := uuid.New().String()
	agentReq := protocol.K8sGetPodLogsRequest{
		Type:              protocol.MessageTypeK8sGetPodLogs,
		RequestID:         requestID,
		ClusterID:         clusterID,
		Namespace:         namespace,
		PodName:           podName,
		Container:         container,
		Follow:            follow,
		TailLines:         tailLines,
		KubeconfigContent: cluster.KubeconfigContent,
		Context:           cluster.Context,
	}

	rawResp, err := s.hub.SendToNodeWithTimeout(cluster.NodeID, agentReq, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("Agent 响应超时: %w", err)
	}

	var agentResp protocol.K8sGetPodLogsResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return "", fmt.Errorf("解析 Agent 响应失败: %w", err)
	}

	if !agentResp.Success {
		return "", fmt.Errorf("获取日志失败: %s", agentResp.Error)
	}

	return agentResp.Logs, nil
}

func (s *Service) resourceRequest(ctx context.Context, clusterID, namespace, msgType string, timeout time.Duration) (json.RawMessage, *Cluster, error) {
	cluster, err := s.store.GetCluster(clusterID)
	if err != nil {
		return nil, nil, err
	}
	if !s.hub.IsNodeOnline(cluster.NodeID) {
		return nil, nil, fmt.Errorf("Agent 节点离线")
	}
	if strings.TrimSpace(cluster.KubeconfigContent) == "" {
		return nil, nil, fmt.Errorf("集群缺少 kubeconfig 内容，请重新连接集群")
	}
	requestID := uuid.New().String()
	agentReq := protocol.K8sResourceRequest{
		Type:              msgType,
		RequestID:         requestID,
		ClusterID:         clusterID,
		Namespace:         namespace,
		KubeconfigContent: cluster.KubeconfigContent,
		Context:           cluster.Context,
	}
	start := time.Now()
	if s.debug {
		log.Printf("[debug][server][k8s] resource start type=%s request_id=%s cluster_id=%s node_id=%s namespace=%s", msgType, requestID, clusterID, cluster.NodeID, namespace)
	}
	rawResp, err := s.sendToNode(ctx, cluster.NodeID, agentReq, timeout)
	if err != nil {
		if s.debug {
			log.Printf("[debug][server][k8s] resource done type=%s request_id=%s cluster_id=%s node_id=%s namespace=%s elapsed=%s error=%v", msgType, requestID, clusterID, cluster.NodeID, namespace, time.Since(start), err)
		}
		return nil, nil, fmt.Errorf("Agent 响应超时: %w", err)
	}
	if s.debug {
		log.Printf("[debug][server][k8s] resource response type=%s request_id=%s cluster_id=%s node_id=%s namespace=%s elapsed=%s", msgType, requestID, clusterID, cluster.NodeID, namespace, time.Since(start))
	}
	return rawResp, cluster, nil
}

func (s *Service) sendToNode(ctx context.Context, nodeID string, message interface{}, timeout time.Duration) (json.RawMessage, error) {
	if hub, ok := s.hub.(contextAgentHub); ok {
		return hub.SendToNodeWithContext(ctx, nodeID, message, timeout)
	}
	return s.hub.SendToNodeWithTimeout(nodeID, message, timeout)
}

func (s *Service) GetSummary(ctx context.Context, clusterID string) (*protocol.K8sResourceSummary, error) {
	rawResp, _, err := s.resourceRequest(ctx, clusterID, "", protocol.MessageTypeK8sGetSummary, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var agentResp protocol.K8sGetSummaryResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	return agentResp.Summary, nil
}

func (s *Service) GetNamespaces(ctx context.Context, clusterID string) ([]protocol.K8sNamespace, error) {
	rawResp, _, err := s.resourceRequest(ctx, clusterID, "", protocol.MessageTypeK8sGetNamespaces, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var agentResp protocol.K8sGetNamespacesResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	return agentResp.Namespaces, nil
}

func (s *Service) GetNodes(ctx context.Context, clusterID string) ([]protocol.K8sNode, error) {
	rawResp, _, err := s.resourceRequest(ctx, clusterID, "", protocol.MessageTypeK8sGetNodes, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var agentResp protocol.K8sGetNodesResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	return agentResp.Nodes, nil
}

func (s *Service) GetDeployments(ctx context.Context, clusterID, namespace string) ([]protocol.K8sDeployment, error) {
	rawResp, _, err := s.resourceRequest(ctx, clusterID, namespace, protocol.MessageTypeK8sGetDeployments, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var agentResp protocol.K8sGetDeploymentsResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	return agentResp.Deployments, nil
}

func (s *Service) GetStatefulSets(ctx context.Context, clusterID, namespace string) ([]protocol.K8sStatefulSet, error) {
	rawResp, _, err := s.resourceRequest(ctx, clusterID, namespace, protocol.MessageTypeK8sGetStatefulSets, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var agentResp protocol.K8sGetStatefulSetsResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	return agentResp.StatefulSets, nil
}

func (s *Service) GetDaemonSets(ctx context.Context, clusterID, namespace string) ([]protocol.K8sDaemonSet, error) {
	rawResp, _, err := s.resourceRequest(ctx, clusterID, namespace, protocol.MessageTypeK8sGetDaemonSets, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var agentResp protocol.K8sGetDaemonSetsResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	return agentResp.DaemonSets, nil
}

func (s *Service) GetServices(ctx context.Context, clusterID, namespace string) ([]protocol.K8sService, error) {
	rawResp, _, err := s.resourceRequest(ctx, clusterID, namespace, protocol.MessageTypeK8sGetServices, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var agentResp protocol.K8sGetServicesResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	return agentResp.Services, nil
}

func (s *Service) GetIngresses(ctx context.Context, clusterID, namespace string) ([]protocol.K8sIngress, error) {
	rawResp, _, err := s.resourceRequest(ctx, clusterID, namespace, protocol.MessageTypeK8sGetIngresses, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var agentResp protocol.K8sGetIngressesResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	return agentResp.Ingresses, nil
}

func supportedDiagnosticsKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "pod", "deployment", "statefulset", "daemonset":
		return true
	default:
		return false
	}
}

type ResourceActionRequest struct {
	Action   string `json:"action"`
	Replicas *int32 `json:"replicas,omitempty"`
	YAML     string `json:"yaml,omitempty"`
}

type ResourceActionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

type ApplyManifestRequest struct {
	YAML   string `json:"yaml"`
	DryRun bool   `json:"dry_run,omitempty"`
}

type ApplyManifestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

func supportedResourceAction(kind, action string) bool {
	switch action {
	case "delete":
		return kind == "pod"
	case "restart":
		return kind == "deployment" || kind == "statefulset" || kind == "daemonset"
	case "scale":
		return kind == "deployment" || kind == "statefulset"
	case "dry_run_apply", "apply":
		return supportedDiagnosticsKind(kind)
	default:
		return false
	}
}

func (s *Service) GetDiagnostics(ctx context.Context, clusterID, kind, namespace, name string) (*protocol.K8sDiagnostics, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if !supportedDiagnosticsKind(kind) {
		return nil, fmt.Errorf("不支持的资源类型: %s", kind)
	}
	if namespace == "" {
		return nil, fmt.Errorf("命名空间不能为空")
	}
	if name == "" {
		return nil, fmt.Errorf("资源名称不能为空")
	}

	cluster, err := s.store.GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	if !s.hub.IsNodeOnline(cluster.NodeID) {
		return nil, fmt.Errorf("Agent 节点离线")
	}
	if strings.TrimSpace(cluster.KubeconfigContent) == "" {
		return nil, fmt.Errorf("集群缺少 kubeconfig 内容，请重新连接集群")
	}

	requestID := uuid.New().String()
	agentReq := protocol.K8sDiagnosticsRequest{
		Type:              protocol.MessageTypeK8sGetDiagnostics,
		RequestID:         requestID,
		ClusterID:         clusterID,
		Kind:              kind,
		Namespace:         namespace,
		Name:              name,
		KubeconfigContent: cluster.KubeconfigContent,
		Context:           cluster.Context,
	}
	start := time.Now()
	if s.debug {
		log.Printf("[debug][server][k8s] diagnostics start request_id=%s cluster_id=%s node_id=%s kind=%s namespace=%s name=%s", requestID, clusterID, cluster.NodeID, kind, namespace, name)
	}
	rawResp, err := s.hub.SendToNodeWithTimeout(cluster.NodeID, agentReq, 15*time.Second)
	if err != nil {
		if s.debug {
			log.Printf("[debug][server][k8s] diagnostics done request_id=%s cluster_id=%s node_id=%s kind=%s namespace=%s name=%s elapsed=%s error=%v", requestID, clusterID, cluster.NodeID, kind, namespace, name, time.Since(start), err)
		}
		return nil, fmt.Errorf("Agent 响应超时: %w", err)
	}
	var agentResp protocol.K8sGetDiagnosticsResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		if s.debug {
			log.Printf("[debug][server][k8s] diagnostics done request_id=%s cluster_id=%s node_id=%s kind=%s namespace=%s name=%s elapsed=%s agent_success=false error=%s", requestID, clusterID, cluster.NodeID, kind, namespace, name, time.Since(start), agentResp.Error)
		}
		return nil, fmt.Errorf("查询失败: %s", agentResp.Error)
	}
	if s.debug {
		log.Printf("[debug][server][k8s] diagnostics done request_id=%s cluster_id=%s node_id=%s kind=%s namespace=%s name=%s elapsed=%s success=true", requestID, clusterID, cluster.NodeID, kind, namespace, name, time.Since(start))
	}
	return agentResp.Diagnostics, nil
}

func (s *Service) ExecuteResourceAction(ctx context.Context, clusterID, kind, namespace, name string, req ResourceActionRequest) (*ResourceActionResult, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if !supportedResourceAction(kind, action) {
		return nil, fmt.Errorf("不支持的资源操作: %s/%s", kind, action)
	}
	if namespace == "" {
		return nil, fmt.Errorf("命名空间不能为空")
	}
	if name == "" {
		return nil, fmt.Errorf("资源名称不能为空")
	}
	if action == "scale" && req.Replicas == nil {
		return nil, fmt.Errorf("副本数不能为空")
	}

	cluster, err := s.store.GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	if !s.hub.IsNodeOnline(cluster.NodeID) {
		return nil, fmt.Errorf("Agent 节点离线")
	}
	if strings.TrimSpace(cluster.KubeconfigContent) == "" {
		return nil, fmt.Errorf("集群缺少 kubeconfig 内容，请重新连接集群")
	}

	requestID := uuid.New().String()
	agentReq := protocol.K8sResourceActionRequest{
		Type:              protocol.MessageTypeK8sResourceAction,
		RequestID:         requestID,
		ClusterID:         clusterID,
		Kind:              kind,
		Namespace:         namespace,
		Name:              name,
		Action:            action,
		Replicas:          req.Replicas,
		YAML:              req.YAML,
		KubeconfigContent: cluster.KubeconfigContent,
		Context:           cluster.Context,
	}
	start := time.Now()
	if s.debug {
		log.Printf("[debug][server][k8s] action start request_id=%s cluster_id=%s node_id=%s kind=%s namespace=%s name=%s action=%s", requestID, clusterID, cluster.NodeID, kind, namespace, name, action)
	}
	rawResp, err := s.hub.SendToNodeWithTimeout(cluster.NodeID, agentReq, 20*time.Second)
	if err != nil {
		if s.debug {
			log.Printf("[debug][server][k8s] action done request_id=%s cluster_id=%s node_id=%s kind=%s namespace=%s name=%s action=%s elapsed=%s error=%v", requestID, clusterID, cluster.NodeID, kind, namespace, name, action, time.Since(start), err)
		}
		return nil, fmt.Errorf("Agent 响应超时: %w", err)
	}
	var agentResp protocol.K8sResourceActionResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		if s.debug {
			log.Printf("[debug][server][k8s] action done request_id=%s cluster_id=%s node_id=%s kind=%s namespace=%s name=%s action=%s elapsed=%s agent_success=false error=%s", requestID, clusterID, cluster.NodeID, kind, namespace, name, action, time.Since(start), agentResp.Error)
		}
		return nil, fmt.Errorf("操作失败: %s", agentResp.Error)
	}
	if s.debug {
		log.Printf("[debug][server][k8s] action done request_id=%s cluster_id=%s node_id=%s kind=%s namespace=%s name=%s action=%s elapsed=%s success=true", requestID, clusterID, cluster.NodeID, kind, namespace, name, action, time.Since(start))
	}
	return &ResourceActionResult{Success: true, Message: agentResp.Message}, nil
}

func (s *Service) ApplyManifest(ctx context.Context, clusterID string, req ApplyManifestRequest) (*ApplyManifestResult, error) {
	body := strings.TrimSpace(req.YAML)
	if body == "" {
		return nil, fmt.Errorf("YAML 不能为空")
	}

	cluster, err := s.store.GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	if !s.hub.IsNodeOnline(cluster.NodeID) {
		return nil, fmt.Errorf("Agent 节点离线")
	}
	if strings.TrimSpace(cluster.KubeconfigContent) == "" {
		return nil, fmt.Errorf("集群缺少 kubeconfig 内容，请重新连接集群")
	}

	requestID := uuid.New().String()
	agentReq := protocol.K8sApplyManifestRequest{
		Type:              protocol.MessageTypeK8sApplyManifest,
		RequestID:         requestID,
		ClusterID:         clusterID,
		YAML:              req.YAML,
		DryRun:            req.DryRun,
		KubeconfigContent: cluster.KubeconfigContent,
		Context:           cluster.Context,
	}
	start := time.Now()
	if s.debug {
		log.Printf("[debug][server][k8s] apply_manifest start request_id=%s cluster_id=%s node_id=%s dry_run=%v", requestID, clusterID, cluster.NodeID, req.DryRun)
	}
	rawResp, err := s.hub.SendToNodeWithTimeout(cluster.NodeID, agentReq, 30*time.Second)
	if err != nil {
		if s.debug {
			log.Printf("[debug][server][k8s] apply_manifest done request_id=%s cluster_id=%s node_id=%s dry_run=%v elapsed=%s error=%v", requestID, clusterID, cluster.NodeID, req.DryRun, time.Since(start), err)
		}
		return nil, fmt.Errorf("Agent 响应超时: %w", err)
	}
	var agentResp protocol.K8sApplyManifestResult
	if err := json.Unmarshal(rawResp, &agentResp); err != nil {
		return nil, fmt.Errorf("解析 Agent 响应失败: %w", err)
	}
	if !agentResp.Success {
		if s.debug {
			log.Printf("[debug][server][k8s] apply_manifest done request_id=%s cluster_id=%s node_id=%s dry_run=%v elapsed=%s agent_success=false error=%s", requestID, clusterID, cluster.NodeID, req.DryRun, time.Since(start), agentResp.Error)
		}
		return nil, fmt.Errorf("操作失败: %s", agentResp.Error)
	}
	if s.debug {
		log.Printf("[debug][server][k8s] apply_manifest done request_id=%s cluster_id=%s node_id=%s dry_run=%v elapsed=%s success=true", requestID, clusterID, cluster.NodeID, req.DryRun, time.Since(start))
	}
	return &ApplyManifestResult{Success: true, Message: agentResp.Message}, nil
}
