package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/mizupanel/mizupanel/internal/server/k8s"
)

// handleK8sClusters 处理 K8s 集群列表
func (s *Server) handleK8sClusters(k8sService *k8s.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleGetK8sClusters(k8sService, w, r)
		case http.MethodPost:
			s.handleConnectK8sCluster(k8sService, w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// handleGetK8sClusters 获取集群列表
func (s *Server) handleGetK8sClusters(k8sService *k8s.Service, w http.ResponseWriter, r *http.Request) {
	clusters, err := k8sService.ListClustersWithNodeInfo()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取集群列表失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"clusters": clusters,
	})
}

// handleConnectK8sCluster 连接新集群
func (s *Server) handleConnectK8sCluster(k8sService *k8s.Service, w http.ResponseWriter, r *http.Request) {
	markAudit(r, "kubernetes", "cluster_connect", "k8s_cluster", "", "")
	var req k8s.ConnectClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	setAuditTarget(r, "k8s_cluster", "", req.Name)
	setAuditNode(r, req.NodeID)

	cluster, clusterInfo, err := k8sService.ConnectCluster(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditTarget(r, "k8s_cluster", cluster.ID, cluster.Name)
	setAuditNode(r, cluster.NodeID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"cluster":      cluster.Public(),
		"cluster_info": clusterInfo,
	})
}

// handleK8sClusterRoutes 处理单个集群的路由
func (s *Server) handleK8sClusterRoutes(k8sService *k8s.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// /api/k8s/clusters/:id/...
		path := strings.TrimPrefix(r.URL.Path, "/api/k8s/clusters/")
		parts := strings.Split(path, "/")
		if len(parts) < 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		clusterID := parts[0]

		// /api/k8s/clusters/:id
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				s.handleGetK8sCluster(k8sService, clusterID, w, r)
			case http.MethodDelete:
				s.handleDeleteK8sCluster(k8sService, clusterID, w, r)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		}

		// /api/k8s/clusters/:id/pods
		if len(parts) == 2 && parts[1] == "pods" {
			s.handleGetK8sPods(k8sService, clusterID, w, r)
			return
		}

		if len(parts) == 2 {
			switch parts[1] {
			case "summary":
				s.handleGetK8sSummary(k8sService, clusterID, w, r)
				return
			case "namespaces":
				s.handleGetK8sNamespaces(k8sService, clusterID, w, r)
				return
			case "nodes":
				s.handleGetK8sNodes(k8sService, clusterID, w, r)
				return
			case "deployments":
				s.handleGetK8sDeployments(k8sService, clusterID, w, r)
				return
			case "statefulsets":
				s.handleGetK8sStatefulSets(k8sService, clusterID, w, r)
				return
			case "daemonsets":
				s.handleGetK8sDaemonSets(k8sService, clusterID, w, r)
				return
			case "services":
				s.handleGetK8sServices(k8sService, clusterID, w, r)
				return
			case "ingresses":
				s.handleGetK8sIngresses(k8sService, clusterID, w, r)
				return
			}
		}

		// /api/k8s/clusters/:id/pods/:namespace/:name/logs
		if len(parts) == 5 && parts[1] == "pods" && parts[4] == "logs" {
			namespace := parts[2]
			podName := parts[3]
			s.handleGetK8sPodLogs(k8sService, clusterID, namespace, podName, w, r)
			return
		}

		// /api/k8s/clusters/:id/resources/apply
		if len(parts) == 3 && parts[1] == "resources" && parts[2] == "apply" {
			s.handleK8sApplyManifest(k8sService, clusterID, w, r)
			return
		}

		// /api/k8s/clusters/:id/resources/:kind/:namespace/:name/diagnostics
		if len(parts) == 6 && parts[1] == "resources" && parts[5] == "diagnostics" {
			kind := parts[2]
			namespace := parts[3]
			name := parts[4]
			s.handleGetK8sDiagnostics(k8sService, clusterID, kind, namespace, name, w, r)
			return
		}

		// /api/k8s/clusters/:id/resources/:kind/:namespace/:name/actions
		if len(parts) == 6 && parts[1] == "resources" && parts[5] == "actions" {
			kind := parts[2]
			namespace := parts[3]
			name := parts[4]
			s.handleK8sResourceAction(k8sService, clusterID, kind, namespace, name, w, r)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}
}

// handleGetK8sCluster 获取集群详情
func (s *Server) handleGetK8sCluster(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	cluster, err := k8sService.GetClusterWithNodeInfo(clusterID)
	if err != nil {
		writeError(w, http.StatusNotFound, "集群不存在")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster": cluster,
	})
}

// handleDeleteK8sCluster 删除集群
func (s *Server) handleDeleteK8sCluster(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	markAudit(r, "kubernetes", "cluster_delete", "k8s_cluster", clusterID, "")
	setK8sAuditCluster(r, k8sService, clusterID, true)
	if err := k8sService.DeleteCluster(clusterID); err != nil {
		writeError(w, http.StatusInternalServerError, "删除集群失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// handleGetK8sPods 获取 Pod 列表
func (s *Server) handleGetK8sPods(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	namespace := r.URL.Query().Get("namespace")

	pods, err := k8sService.GetPods(r.Context(), clusterID, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"pods":    pods,
	})
}

// handleGetK8sPodLogs 获取 Pod 日志
func (s *Server) handleGetK8sPodLogs(k8sService *k8s.Service, clusterID, namespace, podName string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	container := r.URL.Query().Get("container")
	follow := r.URL.Query().Get("follow") == "true"
	tailLines := 100
	if t := r.URL.Query().Get("tail_lines"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			tailLines = n
		}
	}

	logs, err := k8sService.GetPodLogs(r.Context(), clusterID, namespace, podName, container, follow, tailLines)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"logs":    logs,
	})
}

func (s *Server) handleGetK8sSummary(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	summary, err := k8sService.GetSummary(r.Context(), clusterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "summary": summary})
}

func (s *Server) handleGetK8sNamespaces(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	namespaces, err := k8sService.GetNamespaces(r.Context(), clusterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "namespaces": namespaces})
}

func (s *Server) handleGetK8sNodes(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	nodes, err := k8sService.GetNodes(r.Context(), clusterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "nodes": nodes})
}

func (s *Server) handleGetK8sDeployments(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	deployments, err := k8sService.GetDeployments(r.Context(), clusterID, r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "deployments": deployments})
}

func (s *Server) handleGetK8sStatefulSets(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	statefulSets, err := k8sService.GetStatefulSets(r.Context(), clusterID, r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "statefulsets": statefulSets})
}

func (s *Server) handleGetK8sDaemonSets(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	daemonSets, err := k8sService.GetDaemonSets(r.Context(), clusterID, r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "daemonsets": daemonSets})
}

func (s *Server) handleGetK8sServices(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	services, err := k8sService.GetServices(r.Context(), clusterID, r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "services": services})
}

func (s *Server) handleGetK8sIngresses(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ingresses, err := k8sService.GetIngresses(r.Context(), clusterID, r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "ingresses": ingresses})
}

func (s *Server) handleGetK8sDiagnostics(k8sService *k8s.Service, clusterID, kind, namespace, name string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	diagnostics, err := k8sService.GetDiagnostics(r.Context(), clusterID, kind, namespace, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "diagnostics": diagnostics})
}

func (s *Server) handleK8sResourceAction(k8sService *k8s.Service, clusterID, kind, namespace, name string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "kubernetes", "resource_action", kind, namespace+"/"+name, "")
	setK8sAuditCluster(r, k8sService, clusterID, false)
	setAuditMetadata(r, "cluster_id", clusterID)
	setAuditMetadata(r, "namespace", namespace)
	var req k8s.ResourceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if validK8sAuditAction(req.Action) {
		setAuditAction(r, strings.ToLower(strings.TrimSpace(req.Action)))
	}
	if req.Replicas != nil {
		setAuditMetadata(r, "replicas", strconv.FormatInt(int64(*req.Replicas), 10))
	}
	result, err := k8sService.ExecuteResourceAction(r.Context(), clusterID, kind, namespace, name, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, result.Success)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": result.Message})
}

func validK8sAuditAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "delete", "restart", "scale", "dry_run_apply", "apply":
		return true
	default:
		return false
	}
}

func (s *Server) handleK8sApplyManifest(k8sService *k8s.Service, clusterID string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "kubernetes", "manifest_apply", "k8s_cluster", clusterID, "")
	setK8sAuditCluster(r, k8sService, clusterID, true)
	var req k8s.ApplyManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.DryRun {
		setAuditAction(r, "manifest_dry_run")
	}
	result, err := k8sService.ApplyManifest(r.Context(), clusterID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, result.Success)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": result.Message})
}

func setK8sAuditCluster(r *http.Request, service *k8s.Service, clusterID string, useAsTarget bool) {
	cluster, err := service.GetCluster(clusterID)
	if err != nil {
		return
	}
	setAuditNode(r, cluster.NodeID)
	if useAsTarget {
		setAuditTarget(r, "k8s_cluster", cluster.ID, cluster.Name)
	}
}
