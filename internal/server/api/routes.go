package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	"github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/store"
	"github.com/mizupanel/mizupanel/internal/version"
)

type TerminalHub interface {
	NodeTerminalEnabled(nodeID string) bool
	AttachTerminal(ctx context.Context, nodeID string, browser *websocket.Conn) error
	AttachContainerExec(ctx context.Context, nodeID string, containerID string, browser *websocket.Conn) error
	AttachLogTail(ctx context.Context, nodeID string, browser *websocket.Conn) error
	AttachContainerLogs(ctx context.Context, nodeID string, containerID string, browser *websocket.Conn) error
}

type NodeOperations interface {
	FileList(ctx context.Context, nodeID string, path string) (protocol.FileListResponse, error)
	FileRead(ctx context.Context, nodeID string, path string) (protocol.FileReadResponse, error)
	FileWrite(ctx context.Context, nodeID string, path string, content string) (protocol.FileWriteResponse, error)
	FileUpload(ctx context.Context, nodeID string, path string, contentBase64 string) (protocol.FileUploadResponse, error)
	FileDelete(ctx context.Context, nodeID string, path string) (protocol.FileDeleteResponse, error)
	Reboot(ctx context.Context, nodeID string) (protocol.RebootResponse, error)
	AgentStatus(ctx context.Context, nodeID string) (protocol.AgentStatusResponse, error)
	AgentRestart(ctx context.Context, nodeID string) (protocol.AgentRestartResponse, error)
	AgentLogs(ctx context.Context, nodeID string, lines int) (protocol.AgentLogsResponse, error)
	AgentUpgrade(ctx context.Context, nodeID string, targetVersion string) (protocol.AgentUpgradeResponse, error)
	AgentUpgradeStatus(nodeID string) protocol.AgentUpgradeStatus
	DockerExec(ctx context.Context, nodeID string, command string) (protocol.DockerExecResponse, error)
	ContainerStart(ctx context.Context, nodeID string, containerID string) (protocol.ContainerStartResponse, error)
	ContainerStop(ctx context.Context, nodeID string, containerID string) (protocol.ContainerStopResponse, error)
	ContainerRestart(ctx context.Context, nodeID string, containerID string) (protocol.ContainerRestartResponse, error)
	ContainerDelete(ctx context.Context, nodeID string, containerID string, force bool) (protocol.ContainerDeleteResponse, error)
	DockerComposeList(ctx context.Context, nodeID string) (protocol.DockerComposeListResponse, error)
	DockerComposeAction(ctx context.Context, nodeID string, projectName string, serviceName string, action string) (protocol.DockerComposeActionResponse, error)
	DockerComposeDeployment(ctx context.Context, nodeID string, request protocol.DockerComposeDeploymentRequest) (protocol.DockerComposeDeploymentResponse, error)
	DockerResourceList(ctx context.Context, nodeID string) (protocol.DockerResourceListResponse, error)
	DockerResourceAction(ctx context.Context, nodeID string, resourceType string, resourceID string, action string) (protocol.DockerResourceActionResponse, error)
	SystemdServiceList(ctx context.Context, nodeID string) (protocol.SystemdServiceListResponse, error)
	SystemdServiceAction(ctx context.Context, nodeID string, serviceName string, action string) (protocol.SystemdServiceActionResponse, error)
}

type NodeDisconnecter interface {
	DisconnectNode(nodeID string)
}

type ConnectionDiagnosticsProvider interface {
	ConnectionDiagnostics(ctx context.Context, nodeID string) (store.ConnectionDiagnostics, error)
}

type UptimeChecker interface {
	CheckNow(ctx context.Context, monitorID int64) (store.UptimeMonitor, error)
	BeginMonitorMutation(monitorID int64) (func(), error)
}

type Server struct {
	nodes                   *store.NodeStore
	organizations           *store.NodeOrganizationStore
	metrics                 *store.MetricStore
	processes               *store.ProcessSnapshotStore
	docker                  *store.DockerSnapshotStore
	terminalEnabled         bool
	terminalHub             TerminalHub
	agentOps                NodeOperations
	disconnecter            NodeDisconnecter
	diagnostics             ConnectionDiagnosticsProvider
	settings                *store.SettingsStore
	uptime                  *store.UptimeStore
	uptimeChecker           UptimeChecker
	defaultMetricsRetention time.Duration
	terminalTokens          map[string]terminalToken
	terminalMu              sync.Mutex
	auth                    *Authenticator
	audit                   *serveraudit.Store
}

type terminalToken struct {
	kind        string
	nodeID      string
	containerID string
	expiresAt   time.Time
}

type TerminalConfig struct {
	Enabled bool
}

type SettingsConfig struct {
	Store                   *store.SettingsStore
	DefaultMetricsRetention time.Duration
}

type UptimeConfig struct {
	Store   *store.UptimeStore
	Checker UptimeChecker
}

type AuthConfig struct {
	Enabled    bool
	Username   string
	Password   string
	SessionTTL time.Duration
}

type authSession struct {
	username  string
	expiresAt time.Time
}

type Authenticator struct {
	config   AuthConfig
	sessions map[string]authSession
	mu       sync.Mutex
}

func NewAuthenticator(config AuthConfig) *Authenticator {
	if config.Username == "" {
		config.Username = "admin"
	}
	if config.SessionTTL <= 0 {
		config.SessionTTL = 24 * time.Hour
	}
	return &Authenticator{config: config, sessions: make(map[string]authSession)}
}

const (
	terminalTokenKindNode          = "terminal"
	terminalTokenKindContainerExec = "container_exec"
	terminalTokenTTL               = 30 * time.Second
	maxTerminalTokens              = 256
	maxTerminalWebSocketBytes      = 128 * 1024
	maxNodeOperationBodyBytes      = 1024 * 1024
	// JSON escaping can make an Agent-valid 1 MiB Compose YAML plus a 256 KiB
	// .env substantially larger on the wire. Keep this bounded but large enough
	// for the complete deployment contract.
	maxDockerComposeDeploymentBodyBytes = 4 * 1024 * 1024
)

func NewRouter(nodes *store.NodeStore, metrics *store.MetricStore, snapshots ...any) *http.ServeMux {
	server := &Server{nodes: nodes, organizations: store.NewNodeOrganizationStoreWithDialect(nodes.DB(), nodes.Dialect()), metrics: metrics, defaultMetricsRetention: 6 * time.Hour, terminalTokens: make(map[string]terminalToken), auth: NewAuthenticator(AuthConfig{})}
	var alertStore *store.AlertStore
	var k8sService *k8s.Service
	for _, snapshotStore := range snapshots {
		switch typed := snapshotStore.(type) {
		case *store.ProcessSnapshotStore:
			server.processes = typed
		case *store.DockerSnapshotStore:
			server.docker = typed
		case *store.AlertStore:
			alertStore = typed
		case *k8s.Service:
			k8sService = typed
		case TerminalHub:
			server.terminalHub = typed
			if ops, ok := snapshotStore.(NodeOperations); ok {
				server.agentOps = ops
			}
			if disconnecter, ok := snapshotStore.(NodeDisconnecter); ok {
				server.disconnecter = disconnecter
			}
			if diagnostics, ok := snapshotStore.(ConnectionDiagnosticsProvider); ok {
				server.diagnostics = diagnostics
			}
		case TerminalConfig:
			server.terminalEnabled = typed.Enabled
		case SettingsConfig:
			server.settings = typed.Store
			if typed.DefaultMetricsRetention > 0 {
				server.defaultMetricsRetention = typed.DefaultMetricsRetention
			}
		case UptimeConfig:
			server.uptime = typed.Store
			server.uptimeChecker = typed.Checker
		case AuthConfig:
			server.auth = NewAuthenticator(typed)
		case *Authenticator:
			server.auth = typed
		case *serveraudit.Store:
			server.audit = typed
		case NodeOperations:
			server.agentOps = typed
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/session", server.handleAuthSession)
	mux.HandleFunc("/api/auth/login", server.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", server.handleAuthLogout)
	mux.HandleFunc("/api/system/about", server.requireAuth(server.handleSystemAbout))
	mux.HandleFunc("/api/settings", server.requireAuth(server.handleSettings))
	mux.HandleFunc("/api/node-groups", server.requireAuth(server.handleNodeGroups))
	mux.HandleFunc("/api/node-groups/", server.requireAuth(server.handleNodeGroup))
	mux.HandleFunc("/api/node-tags", server.requireAuth(server.handleNodeTags))
	mux.HandleFunc("/api/node-tags/", server.requireAuth(server.handleNodeTag))
	mux.HandleFunc("/api/nodes", server.requireAuth(server.handleNodes))
	mux.HandleFunc("/api/nodes/", server.requireAuth(server.handleNodeRoutes))
	if alertStore != nil {
		mux.HandleFunc("/api/alerts/rules", server.requireAuth(server.handleAlertRules(alertStore)))
		mux.HandleFunc("/api/alerts/rules/", server.requireAuth(server.handleAlertRuleRoutes(alertStore)))
		mux.HandleFunc("/api/alerts/history", server.requireAuth(server.handleAlertHistory(alertStore)))
		mux.HandleFunc("/api/alerts/history/", server.requireAuth(server.handleAlertHistoryRoutes(alertStore)))
	}
	if server.uptime != nil {
		mux.HandleFunc("/api/uptime/monitors", server.requireAuth(server.handleUptimeMonitors))
		mux.HandleFunc("/api/uptime/monitors/", server.requireAuth(server.handleUptimeMonitorRoutes))
	}
	if k8sService != nil {
		mux.HandleFunc("/api/k8s/clusters", server.requireAuth(server.handleK8sClusters(k8sService)))
		mux.HandleFunc("/api/k8s/clusters/", server.requireAuth(server.handleK8sClusterRoutes(k8sService)))
	}
	if server.audit != nil {
		mux.HandleFunc("/api/audit/events", server.requireAuth(server.handleAuditEvents))
	}
	return mux
}

func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	s.auth.HandleSession(w, r)
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	s.auth.HandleLogin(w, r)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.HandleLogout(w, r)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return s.auth.Require(next)
}

func (a *Authenticator) HandleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	username, ok := a.authenticatedUsername(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_enabled":  a.config.Enabled,
		"authenticated": !a.config.Enabled || ok,
		"username":      username,
	})
}

func (a *Authenticator) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	serveraudit.Mark(r, "auth", "login")
	if !a.config.Enabled {
		serveraudit.SetPrincipal(r, serveraudit.ActorLocalAdmin, "local")
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": ""})
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		serveraudit.SetPrincipal(r, serveraudit.ActorUnauthenticated, "")
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Username), []byte(a.config.Username)) != 1 || subtle.ConstantTimeCompare([]byte(request.Password), []byte(a.config.Password)) != 1 {
		serveraudit.SetPrincipal(r, serveraudit.ActorUnauthenticated, request.Username)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, err := a.createSession(a.config.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	serveraudit.SetPrincipal(r, serveraudit.ActorAdmin, a.config.Username)
	http.SetCookie(w, a.sessionCookie(r, token, int(a.config.SessionTTL.Seconds())))
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": a.config.Username})
}

func (a *Authenticator) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	serveraudit.Mark(r, "auth", "logout")
	if !a.config.Enabled {
		serveraudit.SetPrincipal(r, serveraudit.ActorLocalAdmin, "local")
	} else if username, ok := a.authenticatedUsername(r); ok {
		serveraudit.SetPrincipal(r, serveraudit.ActorAdmin, username)
	} else {
		serveraudit.SetPrincipal(r, serveraudit.ActorUnauthenticated, "")
	}
	if cookie, err := r.Cookie("mizupanel_session"); err == nil {
		a.deleteSession(cookie.Value)
	}
	http.SetCookie(w, a.sessionCookie(r, "", -1))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Authenticator) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.config.Enabled {
			serveraudit.SetPrincipal(r, serveraudit.ActorLocalAdmin, "local")
			next(w, r)
			return
		}
		username, ok := a.authenticatedUsername(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		serveraudit.SetPrincipal(r, serveraudit.ActorAdmin, username)
		next(w, r)
	}
}

func (a *Authenticator) authenticatedUsername(r *http.Request) (string, bool) {
	if !a.config.Enabled {
		return "", true
	}
	cookie, err := r.Cookie("mizupanel_session")
	if err != nil || cookie.Value == "" {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	a.pruneSessionsLocked(now)
	session, ok := a.sessions[cookie.Value]
	if !ok || now.After(session.expiresAt) {
		delete(a.sessions, cookie.Value)
		return "", false
	}
	return session.username, true
}

func (a *Authenticator) createSession(username string) (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes[:])
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	a.pruneSessionsLocked(now)
	a.sessions[token] = authSession{username: username, expiresAt: now.Add(a.config.SessionTTL)}
	return token, nil
}

func (a *Authenticator) deleteSession(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, token)
}

func (a *Authenticator) pruneSessionsLocked(now time.Time) {
	for token, session := range a.sessions {
		if now.After(session.expiresAt) {
			delete(a.sessions, token)
		}
	}
}

func (a *Authenticator) sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "mizupanel_session",
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeError(w, http.StatusNotFound, "settings unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeSettings(w, r)
	case http.MethodPut:
		markAudit(r, "settings", "update", "settings", "metrics", "")
		if !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		var request struct {
			MetricsRetention string `json:"metrics_retention"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.settings.SetMetricsRetention(r.Context(), request.MetricsRetention); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		serveraudit.SetMetadata(r, "metrics_retention", request.MetricsRetention)
		s.writeSettings(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) writeSettings(w http.ResponseWriter, r *http.Request) {
	retention, err := s.currentMetricsRetention(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"metrics_retention":         store.FormatMetricsRetention(retention),
		"metrics_retention_seconds": int64(retention.Seconds()),
		"max_metrics_retention":     store.FormatMetricsRetention(store.MetricsRetentionMax),
	})
}

func (s *Server) currentMetricsRetention(ctx context.Context) (time.Duration, error) {
	if s.settings == nil {
		return s.defaultMetricsRetention, nil
	}
	return s.settings.MetricsRetention(ctx, s.defaultMetricsRetention)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	nodes, err := s.nodes.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	organizations, err := s.organizations.ListNodeOrganizations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load node organization metadata")
		return
	}

	response := struct {
		Nodes []NodeResponse `json:"nodes"`
	}{Nodes: make([]NodeResponse, 0, len(nodes))}
	for _, node := range nodes {
		item := nodeResponse(node)
		organization := organizations[node.ID]
		item.Group = organization.Group
		item.Tags = organization.Tags
		if s.terminalEnabled && s.terminalHub != nil {
			item.TerminalEnabled = s.terminalHub.NodeTerminalEnabled(node.ID)
		}
		metric, ok, err := s.metrics.Latest(r.Context(), node.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ok {
			latest := metricResponse(metric)
			item.LatestMetric = &latest
		}
		response.Nodes = append(response.Nodes, item)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && parts[0] == "batch" && parts[1] == "metadata" {
		s.handleBatchNodeMetadata(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "metrics" {
		s.handleNodeMetrics(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "processes" {
		s.handleNodeProcesses(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "docker" && parts[2] == "exec" {
		s.handleNodeDockerExec(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "docker" {
		s.handleNodeDocker(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "docker" && parts[2] == "compose" {
		s.handleNodeDockerCompose(w, r, parts[0])
		return
	}
	if len(parts) == 4 && parts[1] == "docker" && parts[2] == "compose" && parts[3] == "action" {
		s.handleNodeDockerComposeAction(w, r, parts[0])
		return
	}
	if len(parts) == 4 && parts[1] == "docker" && parts[2] == "compose" && parts[3] == "deployment" {
		s.handleNodeDockerComposeDeployment(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "docker" && parts[2] == "resources" {
		s.handleNodeDockerResources(w, r, parts[0])
		return
	}
	if len(parts) == 4 && parts[1] == "docker" && parts[2] == "resources" && parts[3] == "action" {
		s.handleNodeDockerResourceAction(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "services" && parts[2] == "systemd" {
		s.handleNodeSystemdServices(w, r, parts[0])
		return
	}
	if len(parts) == 4 && parts[1] == "services" && parts[2] == "systemd" && parts[3] == "action" {
		s.handleNodeSystemdServiceAction(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "files" {
		s.handleNodeFiles(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "files" && parts[2] == "content" {
		s.handleNodeFileContent(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "files" && parts[2] == "upload" {
		s.handleNodeFileUpload(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "reboot" {
		s.handleNodeReboot(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "agent" && parts[2] == "status" {
		s.handleNodeAgentStatus(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "agent" && parts[2] == "restart" {
		s.handleNodeAgentRestart(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "agent" && parts[2] == "logs" {
		s.handleNodeAgentLogs(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "agent" && parts[2] == "upgrade" {
		s.handleNodeAgentUpgrade(w, r, parts[0])
		return
	}
	if len(parts) == 4 && parts[1] == "agent" && parts[2] == "upgrade" && parts[3] == "status" {
		s.handleNodeAgentUpgradeStatus(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "connection-diagnostics" {
		s.handleNodeConnectionDiagnostics(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "terminal" && parts[2] == "session" {
		s.handleNodeTerminalSession(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "terminal" && parts[2] == "ws" {
		s.handleNodeTerminal(w, r, parts[0])
		return
	}
	if len(parts) == 5 && parts[1] == "containers" && parts[3] == "exec" && parts[4] == "session" {
		s.handleContainerExecSession(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 5 && parts[1] == "containers" && parts[3] == "exec" && parts[4] == "ws" {
		s.handleContainerExec(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 5 && parts[1] == "containers" && parts[3] == "logs" && parts[4] == "stream" {
		s.handleContainerLogs(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 4 && parts[1] == "containers" && parts[3] == "start" {
		s.handleContainerStart(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 4 && parts[1] == "containers" && parts[3] == "stop" {
		s.handleContainerStop(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 4 && parts[1] == "containers" && parts[3] == "restart" {
		s.handleContainerRestart(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 3 && parts[1] == "containers" {
		s.handleContainerDelete(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 3 && parts[1] == "logs" && parts[2] == "tail" {
		s.handleNodeLogTail(w, r, parts[0])
		return
	}
	if len(parts) == 1 && parts[0] != "" {
		switch r.Method {
		case http.MethodGet:
			s.handleNode(w, r, parts[0])
		case http.MethodDelete:
			s.handleDeleteNode(w, r, parts[0])
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request, id string) {
	node, err := s.nodes.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	response := nodeResponse(node)
	organization, err := s.organizations.GetNodeOrganization(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load node organization metadata")
		return
	}
	response.Group = organization.Group
	response.Tags = organization.Tags
	if s.terminalEnabled && s.terminalHub != nil {
		response.TerminalEnabled = s.terminalHub.NodeTerminalEnabled(id)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request, id string) {
	markAudit(r, "node", "delete", "node", id, id)
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := s.nodes.Delete(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.disconnecter != nil {
		s.disconnecter.DisconnectNode(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNodeMetrics(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	rangeValue := r.URL.Query().Get("range")
	duration, ok := map[string]time.Duration{
		"1h":  time.Hour,
		"6h":  6 * time.Hour,
		"24h": 24 * time.Hour,
		"3d":  3 * 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
	}[rangeValue]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be 1h, 6h, 24h, 3d, or 7d")
		return
	}
	retention, err := s.currentMetricsRetention(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if duration > retention {
		writeError(w, http.StatusBadRequest, "range exceeds metrics retention")
		return
	}
	now := time.Now().UTC()
	metrics, err := s.metrics.ListRange(r.Context(), nodeID, now.Add(-duration), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response := struct {
		Metrics []MetricResponse `json:"metrics"`
	}{Metrics: make([]MetricResponse, 0, len(metrics))}
	for _, metric := range metrics {
		response.Metrics = append(response.Metrics, metricResponse(metric))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeProcesses(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	response := ProcessSnapshotResponse{NodeID: nodeID, Processes: []protocol.ProcessInfo{}}
	if s.processes != nil {
		snapshot, ok, err := s.processes.Get(r.Context(), nodeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ok {
			response.CollectedAt = snapshot.CollectedAt
			response.Error = snapshot.Error
			response.Processes = sanitizedProcessInfos(snapshot.Processes)
			if response.Processes == nil {
				response.Processes = []protocol.ProcessInfo{}
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeFiles(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !authorizeBrowserNodeOperation(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations are not available")
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	response, err := s.agentOps.FileList(r.Context(), nodeID, path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeFileContent(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method == http.MethodPut {
		markAudit(r, "file", "write", "path", "", nodeID)
	} else if r.Method == http.MethodDelete {
		markAudit(r, "file", "delete", "path", "", nodeID)
	}
	if !authorizeBrowserNodeOperation(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations are not available")
		return
	}
	switch r.Method {
	case http.MethodGet:
		path := r.URL.Query().Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		response, err := s.agentOps.FileRead(r.Context(), nodeID, path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, maxNodeOperationBodyBytes)
		var request struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if strings.TrimSpace(request.Path) == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		setAuditTarget(r, "path", request.Path, "")
		response, err := s.agentOps.FileWrite(r.Context(), nodeID, request.Path, request.Content)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuditOutcome(r, response.Saved)
		writeJSON(w, http.StatusOK, response)
	case http.MethodDelete:
		r.Body = http.MaxBytesReader(w, r.Body, maxNodeOperationBodyBytes)
		var request struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if strings.TrimSpace(request.Path) == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		setAuditTarget(r, "path", request.Path, "")
		response, err := s.agentOps.FileDelete(r.Context(), nodeID, request.Path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuditOutcome(r, response.Deleted)
		writeJSON(w, http.StatusOK, response)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNodeFileUpload(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "file", "upload", "path", "", nodeID)
	if !authorizeBrowserNodeOperation(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations are not available")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxNodeOperationBodyBytes)
	var request struct {
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(request.Path) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	setAuditTarget(r, "path", request.Path, "")
	response, err := s.agentOps.FileUpload(r.Context(), nodeID, request.Path, request.ContentBase64)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Uploaded)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeReboot(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "node", "reboot", "node", nodeID, nodeID)
	if !authorizeBrowserNodeOperation(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations are not available")
		return
	}
	response, err := s.agentOps.Reboot(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditAccepted(r, response.Accepted)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeDockerExec(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "docker", "exec", "node", nodeID, nodeID)
	if !authorizeBrowserNodeOperation(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations are not available")
		return
	}
	var request struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(request.Command) == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	response, err := s.agentOps.DockerExec(r.Context(), nodeID, request.Command)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Accepted && response.ExitCode == 0 && response.Error == "")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeAgentStatus(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.agentManagementAvailable(w, r, nodeID) {
		return
	}
	response, err := s.agentOps.AgentStatus(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOperationResponse(w, response.Code, response.Error, response)
}

func (s *Server) handleNodeAgentRestart(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "agent", "restart", "node", nodeID, nodeID)
	if !authorizeBrowserNodeOperation(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !s.agentManagementAvailable(w, r, nodeID) {
		return
	}
	response, err := s.agentOps.AgentRestart(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditAccepted(r, response.Accepted)
	writeAgentOperationResponse(w, response.Code, response.Error, response)
}

func (s *Server) handleNodeAgentLogs(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.agentManagementAvailable(w, r, nodeID) {
		return
	}
	response, err := s.agentOps.AgentLogs(r.Context(), nodeID, clampAgentLogLines(r.URL.Query().Get("lines")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOperationResponse(w, response.Code, response.Error, response)
}

func (s *Server) handleNodeAgentUpgrade(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "agent", "upgrade", "node", nodeID, nodeID)
	serveraudit.SetMetadata(r, "target_version", version.Current)
	if !authorizeBrowserNodeOperation(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "Agent upgrade unavailable")
		return
	}
	response, err := s.agentOps.AgentUpgrade(r.Context(), nodeID, version.Current)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditAccepted(r, response.Accepted)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeAgentUpgradeStatus(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "Agent upgrade unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.agentOps.AgentUpgradeStatus(nodeID))
}

func (s *Server) agentManagementAvailable(w http.ResponseWriter, r *http.Request, nodeID string) bool {
	node, err := s.nodes.Get(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return false
	}
	if strings.EqualFold(node.OS, "windows") {
		writeError(w, http.StatusNotImplemented, "当前版本暂不支持 Windows Agent 管理")
		return false
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations are not available")
		return false
	}
	return true
}

func clampAgentLogLines(value string) int {
	lines := 100
	if parsed, err := strconv.Atoi(value); err == nil {
		lines = parsed
	}
	if lines < 1 {
		return 1
	}
	if lines > 500 {
		return 500
	}
	return lines
}

func writeAgentOperationResponse(w http.ResponseWriter, code string, message string, response any) {
	if code == "offline" {
		writeError(w, http.StatusServiceUnavailable, message)
		return
	}
	if code == "timeout" {
		writeError(w, http.StatusGatewayTimeout, message)
		return
	}
	if code == "unsupported" {
		writeError(w, http.StatusNotImplemented, message)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func authorizeBrowserNodeOperation(r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	if !sameOrigin(r) {
		return false
	}
	if r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/reboot") || strings.HasSuffix(r.URL.Path, "/agent/restart") || strings.HasSuffix(r.URL.Path, "/agent/upgrade")) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json")
}

func (s *Server) handleNodeTerminalSession(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return
	}
	if !s.terminalEnabled || s.terminalHub == nil {
		writeError(w, http.StatusServiceUnavailable, "terminal is not available")
		return
	}
	if !s.terminalHub.NodeTerminalEnabled(nodeID) {
		writeError(w, http.StatusForbidden, "terminal is not enabled for this node")
		return
	}
	token, err := s.createTerminalToken(terminalTokenKindNode, nodeID, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create terminal session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) handleNodeTerminal(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return
	}
	if !s.terminalEnabled || s.terminalHub == nil {
		writeError(w, http.StatusServiceUnavailable, "terminal is not available")
		return
	}
	if !s.consumeTerminalToken(terminalTokenKindNode, nodeID, "", r.URL.Query().Get("token")) {
		writeError(w, http.StatusUnauthorized, "terminal session is invalid or expired")
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: sameOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxTerminalWebSocketBytes)
	startedAt := time.Now()
	serveraudit.Record(r, serveraudit.RecordOptions{Module: "terminal", Action: "session_open", TargetType: "node", TargetID: nodeID, NodeID: nodeID, Result: serveraudit.ResultSuccess, Summary: "connected"})
	err = s.terminalHub.AttachTerminal(r.Context(), nodeID, conn)
	result, summary := auditWebSocketCloseOutcome(err)
	serveraudit.Record(r, serveraudit.RecordOptions{Module: "terminal", Action: "session_close", TargetType: "node", TargetID: nodeID, NodeID: nodeID, Result: result, Summary: summary, Duration: time.Since(startedAt)})
}

func (s *Server) handleContainerExecSession(w http.ResponseWriter, r *http.Request, nodeID string, containerID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if containerID == "" {
		writeError(w, http.StatusBadRequest, "container id is required")
		return
	}
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return
	}
	if !s.terminalEnabled || s.terminalHub == nil {
		writeError(w, http.StatusServiceUnavailable, "container exec is not available")
		return
	}
	if !s.terminalHub.NodeTerminalEnabled(nodeID) {
		writeError(w, http.StatusForbidden, "terminal is not enabled for this node")
		return
	}
	token, err := s.createTerminalToken(terminalTokenKindContainerExec, nodeID, containerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create container exec session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) handleContainerExec(w http.ResponseWriter, r *http.Request, nodeID string, containerID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return
	}
	if !s.terminalEnabled || s.terminalHub == nil {
		writeError(w, http.StatusServiceUnavailable, "container exec is not available")
		return
	}
	if !s.consumeTerminalToken(terminalTokenKindContainerExec, nodeID, containerID, r.URL.Query().Get("token")) {
		writeError(w, http.StatusUnauthorized, "container exec session is invalid or expired")
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: sameOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxTerminalWebSocketBytes)
	startedAt := time.Now()
	serveraudit.Record(r, serveraudit.RecordOptions{Module: "terminal", Action: "container_exec_open", TargetType: "container", TargetID: containerID, NodeID: nodeID, Result: serveraudit.ResultSuccess, Summary: "connected"})
	err = s.terminalHub.AttachContainerExec(r.Context(), nodeID, containerID, conn)
	result, summary := auditWebSocketCloseOutcome(err)
	serveraudit.Record(r, serveraudit.RecordOptions{Module: "terminal", Action: "container_exec_close", TargetType: "container", TargetID: containerID, NodeID: nodeID, Result: result, Summary: summary, Duration: time.Since(startedAt)})
}

func auditWebSocketCloseOutcome(err error) (string, string) {
	if err == nil || websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return serveraudit.ResultSuccess, "closed"
	}
	return serveraudit.ResultFailure, "remote_operation_failed"
}

func (s *Server) handleNodeLogTail(w http.ResponseWriter, r *http.Request, nodeID string) {
	log.Printf("[Route] handleNodeLogTail called for node %s", nodeID)
	if r.Method != http.MethodGet {
		log.Printf("[Route] Method not allowed: %s", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		log.Printf("[Route] Node not found: %s", nodeID)
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if !sameOrigin(r) {
		log.Printf("[Route] Origin not allowed")
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return
	}
	if s.terminalHub == nil {
		log.Printf("[Route] Terminal hub is nil")
		writeError(w, http.StatusServiceUnavailable, "log tail is not available")
		return
	}
	log.Printf("[Route] Upgrading to WebSocket")
	upgrader := websocket.Upgrader{CheckOrigin: sameOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[Route] Failed to upgrade: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxTerminalWebSocketBytes)
	log.Printf("[Route] Calling AttachLogTail")
	_ = s.terminalHub.AttachLogTail(r.Context(), nodeID, conn)
	log.Printf("[Route] AttachLogTail returned")
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request, nodeID string, containerID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return
	}
	if s.terminalHub == nil {
		writeError(w, http.StatusServiceUnavailable, "container logs is not available")
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: sameOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxTerminalWebSocketBytes)
	_ = s.terminalHub.AttachContainerLogs(r.Context(), nodeID, containerID, conn)
}

func (s *Server) createTerminalToken(kind string, nodeID string, containerID string) (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes[:])
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	now := time.Now()
	s.pruneTerminalTokensLocked(now)
	if len(s.terminalTokens) >= maxTerminalTokens {
		return "", http.ErrHandlerTimeout
	}
	s.terminalTokens[token] = terminalToken{kind: kind, nodeID: nodeID, containerID: containerID, expiresAt: now.Add(terminalTokenTTL)}
	return token, nil
}

func (s *Server) consumeTerminalToken(kind string, nodeID string, containerID string, token string) bool {
	if token == "" {
		return false
	}
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	now := time.Now()
	s.pruneTerminalTokensLocked(now)
	entry, ok := s.terminalTokens[token]
	if !ok || entry.kind != kind || entry.nodeID != nodeID || entry.containerID != containerID || now.After(entry.expiresAt) {
		delete(s.terminalTokens, token)
		return false
	}
	delete(s.terminalTokens, token)
	return true
}

func (s *Server) pruneTerminalTokensLocked(now time.Time) {
	for token, entry := range s.terminalTokens {
		if now.After(entry.expiresAt) {
			delete(s.terminalTokens, token)
		}
	}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	expectedScheme := "http"
	if r.TLS != nil {
		expectedScheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, expectedScheme) && strings.EqualFold(parsed.Host, r.Host)
}

func sanitizedProcessInfos(processes []protocol.ProcessInfo) []protocol.ProcessInfo {
	if processes == nil {
		return nil
	}
	sanitized := make([]protocol.ProcessInfo, len(processes))
	copy(sanitized, processes)
	for index := range sanitized {
		sanitized[index].Command = ""
	}
	return sanitized
}

func (s *Server) handleNodeDocker(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	response := DockerSnapshotResponse{NodeID: nodeID, Containers: []protocol.ContainerInfo{}}
	if s.docker != nil {
		snapshot, ok, err := s.docker.Get(r.Context(), nodeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ok {
			response.CollectedAt = snapshot.CollectedAt
			response.Available = snapshot.Available
			response.Version = snapshot.Version
			response.Error = snapshot.Error
			response.Containers = snapshot.Containers
			if response.Containers == nil {
				response.Containers = []protocol.ContainerInfo{}
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeDockerCompose(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	response, err := s.agentOps.DockerComposeList(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeDockerComposeAction(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "compose", "action", "compose_project", "", nodeID)
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	var request struct {
		ProjectName string `json:"project_name"`
		ServiceName string `json:"service_name"`
		Action      string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid Compose action request")
		return
	}
	setAuditTarget(r, "compose_project", request.ProjectName, "")
	if strings.TrimSpace(request.ProjectName) == "" || strings.TrimSpace(request.Action) == "" {
		writeError(w, http.StatusBadRequest, "Compose project_name and action are required")
		return
	}
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	if !validDockerComposeAction(request.Action) {
		writeError(w, http.StatusBadRequest, "unsupported Compose action")
		return
	}
	if isMutatingDockerComposeAction(request.Action) {
		setAuditAction(r, request.Action)
		serveraudit.SetMetadata(r, "service", request.ServiceName)
	} else {
		serveraudit.Unmark(r)
	}
	response, err := s.agentOps.DockerComposeAction(r.Context(), nodeID, request.ProjectName, request.ServiceName, request.Action)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if isMutatingDockerComposeAction(request.Action) {
		setAuditOutcome(r, response.Success)
	}
	writeJSON(w, http.StatusOK, response)
}

func validDockerComposeAction(action string) bool {
	switch action {
	case "pull", "up", "restart", "stop", "down", "logs", "validate":
		return true
	default:
		return false
	}
}

func isMutatingDockerComposeAction(action string) bool {
	return validDockerComposeAction(action) && action != "logs" && action != "validate"
}

func (s *Server) handleNodeDockerComposeDeployment(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "compose", "deployment", "compose_project", "", nodeID)
	if !authorizeBrowserNodeOperation(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := s.nodes.Get(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations are not available")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDockerComposeDeploymentBodyBytes)
	var request protocol.DockerComposeDeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid Compose deployment request")
		return
	}
	request.Action = strings.TrimSpace(request.Action)
	setAuditTarget(r, "compose_project", request.ProjectID, request.DisplayName)
	switch request.Action {
	case "preview":
		serveraudit.Unmark(r)
		if strings.TrimSpace(request.ComposeYAML) == "" {
			writeError(w, http.StatusBadRequest, "compose_yaml is required for preview")
			return
		}
	case "apply":
		serveraudit.SetAction(r, request.Action)
		if strings.TrimSpace(request.ComposeYAML) == "" || strings.TrimSpace(request.ConfirmationToken) == "" {
			writeError(w, http.StatusBadRequest, "compose_yaml and confirmation_token are required for apply")
			return
		}
	case "rollback", "archive":
		serveraudit.SetAction(r, request.Action)
		if strings.TrimSpace(request.ProjectID) == "" {
			writeError(w, http.StatusBadRequest, "project_id is required")
			return
		}
		if request.ComposeYAML != "" || request.EnvFile != "" || request.ConfirmationToken != "" || request.DisplayName != "" || request.PullImages {
			writeError(w, http.StatusBadRequest, "deployment content is not allowed for this action")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "unsupported Compose deployment action")
		return
	}
	response, err := s.agentOps.DockerComposeDeployment(r.Context(), nodeID, request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Success)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeDockerResources(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	response, err := s.agentOps.DockerResourceList(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeDockerResourceAction(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "docker_resource", "action", "docker_resource", "", nodeID)
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	var request struct {
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
		Action       string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid Docker resource action request")
		return
	}
	request.ResourceType = strings.TrimSpace(request.ResourceType)
	request.ResourceID = strings.TrimSpace(request.ResourceID)
	request.Action = strings.TrimSpace(request.Action)
	if request.ResourceType == "" || request.ResourceID == "" || request.Action == "" {
		writeError(w, http.StatusBadRequest, "Docker resource_type, resource_id and action are required")
		return
	}
	if !validDockerResourceAction(request.ResourceType, request.Action) {
		writeError(w, http.StatusBadRequest, "unsupported Docker resource action")
		return
	}
	serveraudit.SetAction(r, request.Action)
	setAuditTarget(r, request.ResourceType, request.ResourceID, "")
	serveraudit.SetMetadata(r, "resource_type", request.ResourceType)
	response, err := s.agentOps.DockerResourceAction(r.Context(), nodeID, request.ResourceType, request.ResourceID, request.Action)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Success)
	writeJSON(w, http.StatusOK, response)
}

func validDockerResourceAction(resourceType, action string) bool {
	switch resourceType {
	case "image":
		return action == "pull" || action == "remove"
	case "volume", "network":
		return action == "remove"
	default:
		return false
	}
}

func (s *Server) handleNodeSystemdServices(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	response, err := s.agentOps.SystemdServiceList(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNodeSystemdServiceAction(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "systemd", "action", "service", "", nodeID)
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	var request struct {
		ServiceName string `json:"service_name"`
		Action      string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid systemd service action request")
		return
	}
	if strings.TrimSpace(request.ServiceName) == "" || strings.TrimSpace(request.Action) == "" {
		writeError(w, http.StatusBadRequest, "systemd service_name and action are required")
		return
	}
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	if !validSystemdServiceAction(request.Action) {
		writeError(w, http.StatusBadRequest, "unsupported systemd service action")
		return
	}
	if request.Action == "logs" {
		serveraudit.Unmark(r)
	} else {
		serveraudit.SetAction(r, request.Action)
	}
	setAuditTarget(r, "service", request.ServiceName, request.ServiceName)
	response, err := s.agentOps.SystemdServiceAction(r.Context(), nodeID, request.ServiceName, request.Action)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Success)
	writeJSON(w, http.StatusOK, response)
}

func validSystemdServiceAction(action string) bool {
	switch action {
	case "start", "stop", "restart", "logs":
		return true
	default:
		return false
	}
}

func (s *Server) handleContainerStart(w http.ResponseWriter, r *http.Request, nodeID string, containerID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "docker", "container_start", "container", containerID, nodeID)
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	response, err := s.agentOps.ContainerStart(r.Context(), nodeID, containerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Success)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleContainerStop(w http.ResponseWriter, r *http.Request, nodeID string, containerID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "docker", "container_stop", "container", containerID, nodeID)
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	response, err := s.agentOps.ContainerStop(r.Context(), nodeID, containerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Success)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleContainerRestart(w http.ResponseWriter, r *http.Request, nodeID string, containerID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "docker", "container_restart", "container", containerID, nodeID)
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	response, err := s.agentOps.ContainerRestart(r.Context(), nodeID, containerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Success)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleContainerDelete(w http.ResponseWriter, r *http.Request, nodeID string, containerID string) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "docker", "container_delete", "container", containerID, nodeID)
	if s.agentOps == nil {
		writeError(w, http.StatusServiceUnavailable, "agent operations not available")
		return
	}
	// Parse force parameter from query string
	force := r.URL.Query().Get("force") == "true"
	serveraudit.SetMetadata(r, "force", strconv.FormatBool(force))
	response, err := s.agentOps.ContainerDelete(r.Context(), nodeID, containerID, force)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditOutcome(r, response.Success)
	writeJSON(w, http.StatusOK, response)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
