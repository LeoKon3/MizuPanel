package agenthub

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/store"
	"github.com/mizupanel/mizupanel/internal/version"
)

type Options struct {
	AgentToken       string
	Interval         int
	Debug            bool
	InstallAuth      *InstallAuthStore
	AgentTokens      *store.AgentTokenStore
	ProcessSnapshots *store.ProcessSnapshotStore
	DockerSnapshots  *store.DockerSnapshotStore
	HeartbeatTimeout time.Duration
	PublicURL        string
	DownloadDir      string
}

const (
	installTokenTTL      = 30 * time.Minute
	maxInstallTokens     = 1024
	maxAgentMessageBytes = 512 * 1024
	maxBrowserInputBytes = 128 * 1024
	// maxServerTerminalSessions is a combined cap across node terminals and container exec sessions per agent connection.
	maxServerTerminalSessions = 4
	terminalWriteTimeout      = 5 * time.Second
)

type installToken struct {
	nodeID    string
	createdAt time.Time
	expiresAt time.Time
}

type InstallAuthStore struct {
	nodeTokens    map[string]string
	installTokens map[string]installToken
	mu            sync.Mutex
}

func NewInstallAuthStore() *InstallAuthStore {
	return &InstallAuthStore{nodeTokens: make(map[string]string), installTokens: make(map[string]installToken)}
}

func randomNodeToken() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func (s *InstallAuthStore) CreateInstallToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredInstallTokensLocked(now)
	if len(s.installTokens) >= maxInstallTokens {
		return false
	}
	s.installTokens[token] = installToken{createdAt: now, expiresAt: now.Add(installTokenTTL)}
	return true
}

func (s *InstallAuthStore) pruneExpiredInstallTokensLocked(now time.Time) {
	for token, entry := range s.installTokens {
		if now.After(entry.expiresAt) {
			delete(s.installTokens, token)
		}
	}
}

func (s *InstallAuthStore) ExchangeInstallToken(token string, nodeID string, saveToken func(string) error) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.installTokens[token]
	if !ok || entry.nodeID != "" {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.installTokens, token)
		return "", false
	}
	nodeToken, err := randomNodeToken()
	if err != nil {
		return "", false
	}
	if saveToken != nil {
		if err := saveToken(nodeToken); err != nil {
			return "", false
		}
	}
	delete(s.installTokens, token)
	s.nodeTokens[nodeID] = nodeToken
	return nodeToken, true
}

func (s *InstallAuthStore) NodeToken(nodeID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.nodeTokens[nodeID]
	return token, ok
}

func (s *InstallAuthStore) RevokeNodeToken(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodeTokens, nodeID)
}

func (s *InstallAuthStore) InstallTokenCreatedAt(token string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	installToken, ok := s.installTokens[token]
	if !ok || time.Now().After(installToken.expiresAt) {
		return time.Time{}, false
	}
	return installToken.createdAt, true
}

func (s *InstallAuthStore) MayAuthenticateInstallToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if installToken, ok := s.installTokens[token]; ok {
		if time.Now().After(installToken.expiresAt) {
			delete(s.installTokens, token)
			return false
		}
		return installToken.nodeID == ""
	}
	return false
}

func (s *InstallAuthStore) MayAuthenticateNodeToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, nodeToken := range s.nodeTokens {
		if token == nodeToken {
			return true
		}
	}
	return false
}

type Handler struct {
	nodes       *store.NodeStore
	metrics     *store.MetricStore
	events      *store.ConnectionEventStore
	options     Options
	upgrader    websocket.Upgrader
	mu          sync.Mutex
	sessions    map[string]string
	connections map[string]*agentConnection
	upgrades    map[string]protocol.AgentUpgradeStatus
}

type agentConnection struct {
	nodeID                   string
	sessionID                string
	hostname                 string
	remoteAddr               string
	agentVersion             string
	protocolVersion          int
	identitySource           string
	conn                     *websocket.Conn
	writeMu                  sync.Mutex
	terminalEnabled          bool
	supportsAgentManagement  bool
	supportsAgentUpgrade     bool
	sessionMu                sync.Mutex
	terminals                map[string]*browserTerminal
	containerExecs           map[string]*browserContainerExec
	mu                       sync.Mutex
	logTailSessions          map[string]chan json.RawMessage
	containerLogsSessions    map[string]chan json.RawMessage
	pendingMu                sync.Mutex
	pendingLists             map[string]chan protocol.FileListResponse
	pendingReads             map[string]chan protocol.FileReadResponse
	pendingWrites            map[string]chan protocol.FileWriteResponse
	pendingUploads           map[string]chan protocol.FileUploadResponse
	pendingDeletes           map[string]chan protocol.FileDeleteResponse
	pendingReboots           map[string]chan protocol.RebootResponse
	pendingAgentStatuses     map[string]chan protocol.AgentStatusResponse
	pendingAgentRestarts     map[string]chan protocol.AgentRestartResponse
	pendingAgentLogs         map[string]chan protocol.AgentLogsResponse
	pendingAgentUpgrades     map[string]chan protocol.AgentUpgradeResponse
	pendingDockerExecs       map[string]chan protocol.DockerExecResponse
	pendingContainerStarts   map[string]chan protocol.ContainerStartResponse
	pendingContainerStops    map[string]chan protocol.ContainerStopResponse
	pendingContainerRestarts map[string]chan protocol.ContainerRestartResponse
	pendingContainerDeletes  map[string]chan protocol.ContainerDeleteResponse
	pendingK8sMessages       map[string]chan json.RawMessage
}

type browserTerminal struct {
	sessionID string
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closeOnce sync.Once
}

type browserContainerExec struct {
	sessionID   string
	containerID string
	conn        *websocket.Conn
	writeMu     sync.Mutex
	closeOnce   sync.Once
}

func NewHandler(nodes *store.NodeStore, metrics *store.MetricStore, options Options) *Handler {
	if options.Interval == 0 {
		options.Interval = 5
	}
	if options.HeartbeatTimeout <= 0 {
		options.HeartbeatTimeout = 2 * time.Minute
	}
	return &Handler{
		nodes:       nodes,
		metrics:     metrics,
		events:      store.NewConnectionEventStore(nodes.DB()),
		options:     options,
		sessions:    make(map[string]string),
		connections: make(map[string]*agentConnection),
		upgrades:    make(map[string]protocol.AgentUpgradeStatus),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	suppliedToken := bearerToken(r)
	usingHeaderToken := suppliedToken != ""
	if suppliedToken == "" {
		suppliedToken = r.URL.Query().Get("token")
	}
	usingAgentToken := usingHeaderToken && h.options.AgentToken != "" && suppliedToken == h.options.AgentToken
	persistentNodeID := ""
	usingPersistentNodeToken := false
	if usingHeaderToken && !usingAgentToken && h.options.AgentTokens != nil && suppliedToken != "" {
		var err error
		persistentNodeID, usingPersistentNodeToken, err = h.options.AgentTokens.NodeIDForToken(r.Context(), suppliedToken)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	if h.options.AgentToken == "" && h.options.InstallAuth == nil && !usingPersistentNodeToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.options.InstallAuth == nil && !usingAgentToken && !usingPersistentNodeToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.options.InstallAuth != nil && !usingAgentToken && !usingPersistentNodeToken {
		if usingHeaderToken {
			if !h.options.InstallAuth.MayAuthenticateNodeToken(suppliedToken) && !h.options.InstallAuth.MayAuthenticateInstallToken(suppliedToken) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else if !h.options.InstallAuth.MayAuthenticateInstallToken(suppliedToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxAgentMessageBytes)

	var raw json.RawMessage
	if err := conn.ReadJSON(&raw); err != nil {
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] read first message failed remote=%s error=%v", r.RemoteAddr, err)
		}
		return
	}
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &header); err != nil || header.Type != protocol.MessageTypeHello {
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] first message not hello remote=%s type=%s parse_err=%v", r.RemoteAddr, header.Type, err)
		}
		return
	}
	var hello protocol.HelloMessage
	if err := json.Unmarshal(raw, &hello); err != nil {
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] hello unmarshal failed remote=%s error=%v", r.RemoteAddr, err)
		}
		return
	}
	nodeID := nodeIDForHello(hello)
	if hello.ProtocolVersion != 0 && hello.ProtocolVersion != protocol.CurrentProtocolVersion {
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] unsupported protocol remote=%s version=%d supported=%d", r.RemoteAddr, hello.ProtocolVersion, protocol.CurrentProtocolVersion)
		}
		if nodeID != "" {
			if _, err := h.nodes.Get(r.Context(), nodeID); err == nil {
				h.recordConnectionEvent(&agentConnection{nodeID: nodeID, hostname: hello.Hostname, remoteAddr: r.RemoteAddr, agentVersion: hello.AgentVersion, protocolVersion: hello.ProtocolVersion, identitySource: hello.IdentitySource}, store.ConnectionEventProtocolRejected, fmt.Sprintf("unsupported protocol version %d; server supports %d", hello.ProtocolVersion, protocol.CurrentProtocolVersion))
			}
		}
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "unsupported protocol version"), time.Now().Add(time.Second))
		return
	}

	if nodeID == "" {
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] hello missing node_id remote=%s", r.RemoteAddr)
		}
		return
	}
	if h.options.Debug {
		log.Printf("[debug][server][agenthub] agent connecting node_id=%s using_agent_token=%v using_persistent=%v install_auth=%v", nodeID, usingAgentToken, usingPersistentNodeToken, h.options.InstallAuth != nil)
	}
	if usingAgentToken && h.nodeDeleted(r.Context(), nodeID) {
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] agent rejected (node deleted) node_id=%s", nodeID)
		}
		return
	}
	nodeToken := ""
	usedInstallToken := false
	installTokenCreatedAt := time.Time{}
	if usingPersistentNodeToken {
		if persistentNodeID != nodeID {
			if h.options.Debug {
				log.Printf("[debug][server][agenthub] persistent token node_id mismatch token_node=%s hello_node=%s", persistentNodeID, nodeID)
			}
			return
		}
		nodeToken = suppliedToken
	} else if h.options.InstallAuth != nil && !usingAgentToken {
		var ok bool
		if existingNodeToken, exists := h.options.InstallAuth.NodeToken(nodeID); exists && suppliedToken == existingNodeToken {
			nodeToken = existingNodeToken
		} else {
			installTokenCreatedAt, _ = h.options.InstallAuth.InstallTokenCreatedAt(suppliedToken)
			saveToken := func(token string) error {
				if h.options.AgentTokens == nil {
					return nil
				}
				return h.options.AgentTokens.SaveNodeToken(r.Context(), nodeID, token, time.Now().UTC())
			}
			if nodeToken, ok = h.options.InstallAuth.ExchangeInstallToken(suppliedToken, nodeID, saveToken); !ok {
				return
			}
			usedInstallToken = true
		}
	}
	now := time.Now().UTC()
	name := hello.Name
	if name == "" {
		name = hello.Hostname
	}
	if err := h.nodes.Upsert(r.Context(), store.Node{
		ID:              nodeID,
		Name:            name,
		Hostname:        hello.Hostname,
		IP:              hello.IP,
		OS:              hello.OS,
		Arch:            hello.Arch,
		Kernel:          hello.Kernel,
		AgentVersion:    hello.AgentVersion,
		AgentMode:       hello.AgentMode,
		AgentUser:       hello.AgentUser,
		Status:          "online",
		TerminalEnabled: hello.Terminal, // 保存 Agent 的终端功能开关
		LastSeenAt:      now,
	}); err != nil {
		return
	}
	if usedInstallToken {
		allowed, err := h.allowNode(r.Context(), nodeID, installTokenCreatedAt)
		if err != nil {
			return
		}
		if !allowed {
			_ = h.nodes.DeleteIfDeleted(context.WithoutCancel(r.Context()), nodeID)
			return
		}
	}
	if !h.agentCredentialStillValid(r.Context(), nodeID, suppliedToken, nodeToken, usingAgentToken, usingPersistentNodeToken) {
		_ = h.nodes.DeleteIfDeleted(context.WithoutCancel(r.Context()), nodeID)
		return
	}
	sessionID := h.startSession(nodeID)
	agent := &agentConnection{nodeID: nodeID, sessionID: sessionID, hostname: hello.Hostname, remoteAddr: r.RemoteAddr, agentVersion: hello.AgentVersion, protocolVersion: hello.ProtocolVersion, identitySource: hello.IdentitySource, conn: conn, terminalEnabled: hello.Terminal, supportsAgentManagement: hello.AgentManagement, supportsAgentUpgrade: hello.AgentUpgrade, terminals: make(map[string]*browserTerminal), containerExecs: make(map[string]*browserContainerExec), pendingLists: make(map[string]chan protocol.FileListResponse), pendingReads: make(map[string]chan protocol.FileReadResponse), pendingWrites: make(map[string]chan protocol.FileWriteResponse), pendingUploads: make(map[string]chan protocol.FileUploadResponse), pendingDeletes: make(map[string]chan protocol.FileDeleteResponse), pendingReboots: make(map[string]chan protocol.RebootResponse), pendingAgentStatuses: make(map[string]chan protocol.AgentStatusResponse), pendingAgentRestarts: make(map[string]chan protocol.AgentRestartResponse), pendingAgentLogs: make(map[string]chan protocol.AgentLogsResponse), pendingAgentUpgrades: make(map[string]chan protocol.AgentUpgradeResponse), pendingDockerExecs: make(map[string]chan protocol.DockerExecResponse), pendingContainerStarts: make(map[string]chan protocol.ContainerStartResponse), pendingContainerStops: make(map[string]chan protocol.ContainerStopResponse), pendingContainerRestarts: make(map[string]chan protocol.ContainerRestartResponse), pendingContainerDeletes: make(map[string]chan protocol.ContainerDeleteResponse), pendingK8sMessages: make(map[string]chan json.RawMessage)}
	h.recordConnectionEvent(agent, store.ConnectionEventConnected, "")
	h.registerConnection(agent)
	h.observeAgentUpgradeReconnect(nodeID, hello.AgentVersion)
	defer h.unregisterConnection(agent)
	defer h.finishSession(context.WithoutCancel(r.Context()), nodeID, sessionID)
	disconnectReason := "connection closed"
	defer func() { h.recordConnectionEvent(agent, store.ConnectionEventDisconnected, disconnectReason) }()
	if err := agent.writeJSON(protocol.HelloAckMessage{Type: protocol.MessageTypeHelloAck, NodeID: nodeID, NodeToken: nodeToken, Interval: h.options.Interval}); err != nil {
		h.unregisterConnection(agent)
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(h.options.HeartbeatTimeout))

	for {
		var rawMessage json.RawMessage
		if err := conn.ReadJSON(&rawMessage); err != nil {
			disconnectReason = err.Error()
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				disconnectReason = "heartbeat timeout"
				h.recordConnectionEvent(agent, store.ConnectionEventHeartbeatTimeout, disconnectReason)
			}
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(h.options.HeartbeatTimeout))
		var incoming struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawMessage, &incoming); err != nil {
			continue
		}
		switch incoming.Type {
		case protocol.MessageTypeMetrics:
			var message protocol.MetricsMessage
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			if err := h.handleMetrics(r.Context(), nodeID, message); err != nil {
				return
			}
		case protocol.MessageTypeFileListResponse:
			var message protocol.FileListResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverFileList(message)
		case protocol.MessageTypeFileReadResponse:
			var message protocol.FileReadResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverFileRead(message)
		case protocol.MessageTypeFileWriteResponse:
			var message protocol.FileWriteResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverFileWrite(message)
		case protocol.MessageTypeFileUploadResponse:
			var message protocol.FileUploadResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverFileUpload(message)
		case protocol.MessageTypeFileDeleteResponse:
			var message protocol.FileDeleteResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverFileDelete(message)
		case protocol.MessageTypeRebootResponse:
			var message protocol.RebootResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverReboot(message)
		case protocol.MessageTypeAgentStatusResponse:
			var message protocol.AgentStatusResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverAgentStatus(message)
		case protocol.MessageTypeAgentRestartResponse:
			var message protocol.AgentRestartResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverAgentRestart(message)
		case protocol.MessageTypeAgentLogsResponse:
			var message protocol.AgentLogsResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverAgentLogs(message)
		case protocol.MessageTypeAgentUpgradeResponse:
			var message protocol.AgentUpgradeResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverAgentUpgrade(message)
			h.recordAgentUpgradeResponse(agent, message)
		case protocol.MessageTypeDockerExecResponse:
			var message protocol.DockerExecResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverDockerExec(message)
		case protocol.MessageTypeContainerStartResponse:
			var message protocol.ContainerStartResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverContainerStart(message)
		case protocol.MessageTypeContainerStopResponse:
			var message protocol.ContainerStopResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverContainerStop(message)
		case protocol.MessageTypeContainerRestartResponse:
			var message protocol.ContainerRestartResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverContainerRestart(message)
		case protocol.MessageTypeContainerDeleteResponse:
			var message protocol.ContainerDeleteResponse
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			agent.deliverContainerDelete(message)
		case protocol.MessageTypeTerminalStarted, protocol.MessageTypeTerminalData, protocol.MessageTypeTerminalExit, protocol.MessageTypeTerminalError, protocol.MessageTypeTerminalClose:
			var message protocol.TerminalMessage
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			h.routeTerminalMessage(agent, message)
		case protocol.MessageTypeContainerExecStarted, protocol.MessageTypeContainerExecData, protocol.MessageTypeContainerExecExit, protocol.MessageTypeContainerExecError, protocol.MessageTypeContainerExecClose:
			var message protocol.ContainerExecMessage
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				continue
			}
			h.routeContainerExecMessage(agent, message)
		case protocol.MessageTypeLogTailResponse, protocol.MessageTypeLogTailData, protocol.MessageTypeLogTailExit, protocol.MessageTypeLogTailError:
			var header struct {
				SessionID string `json:"session_id"`
			}
			if err := json.Unmarshal(rawMessage, &header); err != nil {
				continue
			}
			log.Printf("[LogTail] Received message from agent: type=%s session=%s", incoming.Type, header.SessionID)
			agent.mu.Lock()
			logChan, exists := agent.logTailSessions[header.SessionID]
			agent.mu.Unlock()
			if exists && logChan != nil {
				select {
				case logChan <- rawMessage:
					log.Printf("[LogTail] Message forwarded to session channel")
				default:
					log.Printf("[LogTail] Channel full, dropping message")
				}
			} else {
				log.Printf("[LogTail] No session found for session_id=%s", header.SessionID)
			}
		case protocol.MessageTypeContainerLogsResponse, protocol.MessageTypeContainerLogsData, protocol.MessageTypeContainerLogsExit, protocol.MessageTypeContainerLogsError:
			var header struct {
				SessionID string `json:"session_id"`
			}
			if err := json.Unmarshal(rawMessage, &header); err != nil {
				continue
			}
			agent.mu.Lock()
			logChan, exists := agent.containerLogsSessions[header.SessionID]
			agent.mu.Unlock()
			if exists && logChan != nil {
				select {
				case logChan <- rawMessage:
				default:
				}
			}
		case protocol.MessageTypeK8sClusterConnectResult,
			protocol.MessageTypeK8sGetSummaryResult,
			protocol.MessageTypeK8sGetNamespacesResult,
			protocol.MessageTypeK8sGetNodesResult,
			protocol.MessageTypeK8sGetPodsResult,
			protocol.MessageTypeK8sGetDeploymentsResult,
			protocol.MessageTypeK8sGetStatefulSetsResult,
			protocol.MessageTypeK8sGetDaemonSetsResult,
			protocol.MessageTypeK8sGetServicesResult,
			protocol.MessageTypeK8sGetIngressesResult,
			protocol.MessageTypeK8sGetDiagnosticsResult,
			protocol.MessageTypeK8sResourceActionResult,
			protocol.MessageTypeK8sApplyManifestResult,
			protocol.MessageTypeK8sGetPodLogsResult:
			var header struct {
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(rawMessage, &header); err != nil {
				continue
			}
			agent.deliverK8sMessage(header.RequestID, rawMessage)
		}
	}
}

func (h *Handler) agentCredentialStillValid(ctx context.Context, nodeID string, suppliedToken string, nodeToken string, usingAgentToken bool, usingPersistentNodeToken bool) bool {
	if h.nodeDeleted(ctx, nodeID) {
		return false
	}
	if usingAgentToken {
		return true
	}
	if usingPersistentNodeToken {
		if h.options.AgentTokens == nil {
			return false
		}
		currentNodeID, ok, err := h.options.AgentTokens.NodeIDForToken(ctx, suppliedToken)
		return err == nil && ok && currentNodeID == nodeID
	}
	if h.options.InstallAuth != nil && nodeToken != "" {
		currentToken, ok := h.options.InstallAuth.NodeToken(nodeID)
		return ok && currentToken == nodeToken
	}
	return true
}

func (h *Handler) handleMetrics(ctx context.Context, nodeID string, message protocol.MetricsMessage) error {
	createdAt := time.Now().UTC()
	if err := h.nodes.UpdateSystemInfo(ctx, nodeID, message.System.Hostname, message.System.OS, message.System.Arch, message.System.Kernel, createdAt); err != nil {
		return err
	}
	if err := h.metrics.Insert(ctx, store.Metric{
		NodeID:         nodeID,
		CPUUsage:       message.CPU.Usage,
		CPUCores:       message.CPU.Cores,
		MemoryTotal:    message.Memory.Total,
		MemoryUsed:     message.Memory.Used,
		MemoryUsage:    message.Memory.Usage,
		DiskTotal:      message.Disk.Total,
		DiskUsed:       message.Disk.Used,
		DiskUsage:      message.Disk.Usage,
		Uptime:         message.System.Uptime,
		DiskReadSpeed:  message.Disk.ReadSpeed,
		DiskWriteSpeed: message.Disk.WriteSpeed,
		RXSpeed:        message.Network.RXSpeed,
		TXSpeed:        message.Network.TXSpeed,
		RXTotal:        message.Network.RXTotal,
		TXTotal:        message.Network.TXTotal,
		Load1:          message.Load.Load1,
		Load5:          message.Load.Load5,
		Load15:         message.Load.Load15,
		CreatedAt:      createdAt,
	}); err != nil {
		return err
	}
	if message.ProcessSnapshot != nil && h.options.ProcessSnapshots != nil {
		if err := h.options.ProcessSnapshots.Upsert(ctx, nodeID, *message.ProcessSnapshot); err != nil {
			return err
		}
	}
	if message.DockerSnapshot != nil && h.options.DockerSnapshots != nil {
		if err := h.options.DockerSnapshots.Upsert(ctx, nodeID, *message.DockerSnapshot); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) NodeTerminalEnabled(nodeID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	connection := h.connections[nodeID]
	return connection != nil && connection.terminalEnabled
}

func (h *Handler) FileList(ctx context.Context, nodeID string, path string) (protocol.FileListResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.FileListResponse{Type: protocol.MessageTypeFileListResponse, Code: "offline", Error: "节点离线，无法发送文件树命令。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.FileListResponse{}, err
	}
	ch := make(chan protocol.FileListResponse, 1)
	agent.addFileListRequest(requestID, ch)
	defer agent.removeFileListRequest(requestID)
	if err := agent.writeJSON(protocol.FileListRequest{Type: protocol.MessageTypeFileListRequest, RequestID: requestID, NodeID: nodeID, Path: path}); err != nil {
		return protocol.FileListResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.FileListResponse{Type: protocol.MessageTypeFileListResponse, Code: "timeout", Error: "文件树请求超时。"}, nil
	case <-ctx.Done():
		return protocol.FileListResponse{}, ctx.Err()
	}
}

func (h *Handler) AgentUpgradeStatus(nodeID string) protocol.AgentUpgradeStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	if status, ok := h.upgrades[nodeID]; ok {
		return status
	}
	agent := h.connections[nodeID]
	actual := ""
	if agent != nil {
		actual = agent.agentVersion
	}
	return protocol.AgentUpgradeStatus{NodeID: nodeID, ActualVersion: actual, Stage: "idle"}
}

func (h *Handler) observeAgentUpgradeReconnect(nodeID string, actualVersion string) {
	h.mu.Lock()
	if status, ok := h.upgrades[nodeID]; ok {
		if status.TargetVersion == actualVersion {
			status.Stage = "completed"
			status.ActualVersion = actualVersion
			status.Error = ""
			h.upgrades[nodeID] = status
		} else if status.Stage == "waiting_reconnect" || status.Stage == "preparing" {
			status.Stage = "failed"
			status.ActualVersion = actualVersion
			status.Error = fmt.Sprintf("Agent 重连后仍为版本 %s。", actualVersion)
			h.upgrades[nodeID] = status
		}
	}
	h.mu.Unlock()
}

func (h *Handler) expireAgentUpgrade(nodeID string, targetVersion string) {
	h.mu.Lock()
	status, ok := h.upgrades[nodeID]
	if ok && status.TargetVersion == targetVersion && (status.Stage == "preparing" || status.Stage == "waiting_reconnect") {
		status.Stage = "failed"
		status.Error = "等待 Agent 重新连接超时。"
		h.upgrades[nodeID] = status
	}
	h.mu.Unlock()
}

func (h *Handler) recordAgentUpgradeResponse(agent *agentConnection, response protocol.AgentUpgradeResponse) {
	if agent == nil || response.Stage != "failed" {
		return
	}
	h.setAgentUpgradeFailed(agent.nodeID, agent.agentVersion, response.Error)
}

func (h *Handler) setAgentUpgradeFailed(nodeID string, actualVersion string, reason string) {
	h.mu.Lock()
	status := h.upgrades[nodeID]
	status.NodeID = nodeID
	status.ActualVersion = actualVersion
	status.Stage = "failed"
	status.Error = reason
	h.upgrades[nodeID] = status
	h.mu.Unlock()
}

func (h *Handler) FileRead(ctx context.Context, nodeID string, path string) (protocol.FileReadResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.FileReadResponse{Type: protocol.MessageTypeFileReadResponse, Code: "offline", Error: "节点离线，无法读取文件。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.FileReadResponse{}, err
	}
	ch := make(chan protocol.FileReadResponse, 1)
	agent.addFileReadRequest(requestID, ch)
	defer agent.removeFileReadRequest(requestID)
	if err := agent.writeJSON(protocol.FileReadRequest{Type: protocol.MessageTypeFileReadRequest, RequestID: requestID, NodeID: nodeID, Path: path}); err != nil {
		return protocol.FileReadResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.FileReadResponse{Type: protocol.MessageTypeFileReadResponse, Code: "timeout", Error: "文件读取请求超时。"}, nil
	case <-ctx.Done():
		return protocol.FileReadResponse{}, ctx.Err()
	}
}

func (h *Handler) FileWrite(ctx context.Context, nodeID string, path string, content string) (protocol.FileWriteResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.FileWriteResponse{Type: protocol.MessageTypeFileWriteResponse, Code: "offline", Error: "节点离线，无法保存文件。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.FileWriteResponse{}, err
	}
	ch := make(chan protocol.FileWriteResponse, 1)
	agent.addFileWriteRequest(requestID, ch)
	defer agent.removeFileWriteRequest(requestID)
	if err := agent.writeJSON(protocol.FileWriteRequest{Type: protocol.MessageTypeFileWriteRequest, RequestID: requestID, NodeID: nodeID, Path: path, Content: content}); err != nil {
		return protocol.FileWriteResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.FileWriteResponse{Type: protocol.MessageTypeFileWriteResponse, Code: "timeout", Error: "文件保存请求超时。"}, nil
	case <-ctx.Done():
		return protocol.FileWriteResponse{}, ctx.Err()
	}
}

func (h *Handler) FileUpload(ctx context.Context, nodeID string, path string, contentBase64 string) (protocol.FileUploadResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.FileUploadResponse{Type: protocol.MessageTypeFileUploadResponse, Code: "offline", Error: "节点离线，无法上传文件。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.FileUploadResponse{}, err
	}
	ch := make(chan protocol.FileUploadResponse, 1)
	agent.addFileUploadRequest(requestID, ch)
	defer agent.removeFileUploadRequest(requestID)
	if err := agent.writeJSON(protocol.FileUploadRequest{Type: protocol.MessageTypeFileUploadRequest, RequestID: requestID, NodeID: nodeID, Path: path, ContentBase64: contentBase64}); err != nil {
		return protocol.FileUploadResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.FileUploadResponse{Type: protocol.MessageTypeFileUploadResponse, Code: "timeout", Error: "文件上传请求超时。"}, nil
	case <-ctx.Done():
		return protocol.FileUploadResponse{}, ctx.Err()
	}
}

func (h *Handler) FileDelete(ctx context.Context, nodeID string, path string) (protocol.FileDeleteResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.FileDeleteResponse{Type: protocol.MessageTypeFileDeleteResponse, Code: "offline", Error: "节点离线，无法删除文件。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.FileDeleteResponse{}, err
	}
	ch := make(chan protocol.FileDeleteResponse, 1)
	agent.addFileDeleteRequest(requestID, ch)
	defer agent.removeFileDeleteRequest(requestID)
	if err := agent.writeJSON(protocol.FileDeleteRequest{Type: protocol.MessageTypeFileDeleteRequest, RequestID: requestID, NodeID: nodeID, Path: path}); err != nil {
		return protocol.FileDeleteResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.FileDeleteResponse{Type: protocol.MessageTypeFileDeleteResponse, Code: "timeout", Error: "文件删除请求超时。"}, nil
	case <-ctx.Done():
		return protocol.FileDeleteResponse{}, ctx.Err()
	}
}

func (h *Handler) Reboot(ctx context.Context, nodeID string) (protocol.RebootResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, Code: "offline", Error: "节点离线，无法发送重启命令。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.RebootResponse{}, err
	}
	ch := make(chan protocol.RebootResponse, 1)
	agent.addRebootRequest(requestID, ch)
	defer agent.removeRebootRequest(requestID)
	if err := agent.writeJSON(protocol.RebootRequest{Type: protocol.MessageTypeRebootRequest, RequestID: requestID, NodeID: nodeID}); err != nil {
		return protocol.RebootResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, Code: "timeout", Error: "重启请求超时。"}, nil
	case <-ctx.Done():
		return protocol.RebootResponse{}, ctx.Err()
	}
}

func (h *Handler) AgentStatus(ctx context.Context, nodeID string) (protocol.AgentStatusResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.AgentStatusResponse{Type: protocol.MessageTypeAgentStatusResponse, Code: "offline", Error: "Agent 离线，无法执行管理操作。"}, nil
	}
	if !agent.supportsAgentManagement {
		return protocol.AgentStatusResponse{Type: protocol.MessageTypeAgentStatusResponse, Code: "unsupported", Error: "当前 Agent 版本暂不支持 Agent 管理，请重新安装或升级 Agent 后再试。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.AgentStatusResponse{}, err
	}
	ch := make(chan protocol.AgentStatusResponse, 1)
	agent.addAgentStatusRequest(requestID, ch)
	defer agent.removeAgentStatusRequest(requestID)
	if err := agent.writeJSON(protocol.AgentStatusRequest{Type: protocol.MessageTypeAgentStatusRequest, RequestID: requestID, NodeID: nodeID}); err != nil {
		return protocol.AgentStatusResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.AgentStatusResponse{Type: protocol.MessageTypeAgentStatusResponse, Code: "timeout", Error: "Agent 状态请求超时。"}, nil
	case <-ctx.Done():
		return protocol.AgentStatusResponse{}, ctx.Err()
	}
}

func (h *Handler) AgentRestart(ctx context.Context, nodeID string) (protocol.AgentRestartResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.AgentRestartResponse{Type: protocol.MessageTypeAgentRestartResponse, Code: "offline", Error: "Agent 离线，无法执行管理操作。"}, nil
	}
	if !agent.supportsAgentManagement {
		return protocol.AgentRestartResponse{Type: protocol.MessageTypeAgentRestartResponse, Code: "unsupported", Error: "当前 Agent 版本暂不支持 Agent 管理，请重新安装或升级 Agent 后再试。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.AgentRestartResponse{}, err
	}
	ch := make(chan protocol.AgentRestartResponse, 1)
	agent.addAgentRestartRequest(requestID, ch)
	defer agent.removeAgentRestartRequest(requestID)
	if err := agent.writeJSON(protocol.AgentRestartRequest{Type: protocol.MessageTypeAgentRestartRequest, RequestID: requestID, NodeID: nodeID}); err != nil {
		return protocol.AgentRestartResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.AgentRestartResponse{Type: protocol.MessageTypeAgentRestartResponse, Code: "timeout", Error: "Agent 重启请求超时。"}, nil
	case <-ctx.Done():
		return protocol.AgentRestartResponse{}, ctx.Err()
	}
}

func (h *Handler) AgentLogs(ctx context.Context, nodeID string, lines int) (protocol.AgentLogsResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.AgentLogsResponse{Type: protocol.MessageTypeAgentLogsResponse, Code: "offline", Error: "Agent 离线，无法执行管理操作。"}, nil
	}
	if !agent.supportsAgentManagement {
		return protocol.AgentLogsResponse{Type: protocol.MessageTypeAgentLogsResponse, Code: "unsupported", Error: "当前 Agent 版本暂不支持 Agent 管理，请重新安装或升级 Agent 后再试。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.AgentLogsResponse{}, err
	}
	ch := make(chan protocol.AgentLogsResponse, 1)
	agent.addAgentLogsRequest(requestID, ch)
	defer agent.removeAgentLogsRequest(requestID)
	if err := agent.writeJSON(protocol.AgentLogsRequest{Type: protocol.MessageTypeAgentLogsRequest, RequestID: requestID, NodeID: nodeID, Lines: lines}); err != nil {
		return protocol.AgentLogsResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(10 * time.Second):
		return protocol.AgentLogsResponse{Type: protocol.MessageTypeAgentLogsResponse, Code: "timeout", Error: "Agent 日志请求超时。"}, nil
	case <-ctx.Done():
		return protocol.AgentLogsResponse{}, ctx.Err()
	}
}

func (h *Handler) AgentUpgrade(ctx context.Context, nodeID string, targetVersion string) (protocol.AgentUpgradeResponse, error) {
	if targetVersion != version.Current {
		return protocol.AgentUpgradeResponse{Code: "invalid_target", Error: "Agent 只能升级到当前 Server 提供的最新版本。"}, nil
	}
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.AgentUpgradeResponse{Code: "offline", Error: "Agent 离线，无法升级。"}, nil
	}
	if !agent.supportsAgentUpgrade {
		return protocol.AgentUpgradeResponse{Code: "unsupported", Error: "当前 Agent 需要先通过安装命令升级一次。"}, nil
	}
	node, err := h.nodes.Get(ctx, nodeID)
	if err != nil {
		return protocol.AgentUpgradeResponse{}, err
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.AgentUpgradeResponse{}, err
	}
	request, code, err := h.prepareAgentUpgradeRequest(node, requestID, targetVersion)
	if err != nil {
		return protocol.AgentUpgradeResponse{Code: code, Error: err.Error()}, nil
	}
	ch := make(chan protocol.AgentUpgradeResponse, 1)
	agent.addAgentUpgradeRequest(requestID, ch)
	defer agent.removeAgentUpgradeRequest(requestID)
	h.mu.Lock()
	h.upgrades[nodeID] = protocol.AgentUpgradeStatus{NodeID: nodeID, TargetVersion: targetVersion, ActualVersion: agent.agentVersion, Stage: "preparing"}
	h.mu.Unlock()
	if err := agent.writeJSON(request); err != nil {
		h.setAgentUpgradeFailed(nodeID, agent.agentVersion, err.Error())
		return protocol.AgentUpgradeResponse{}, err
	}
	select {
	case response := <-ch:
		if response.Accepted {
			h.mu.Lock()
			status := h.upgrades[nodeID]
			if status.Stage != "failed" {
				status.Stage = "waiting_reconnect"
				status.Error = ""
				h.upgrades[nodeID] = status
			}
			h.mu.Unlock()
			time.AfterFunc(2*time.Minute, func() { h.expireAgentUpgrade(nodeID, targetVersion) })
		} else {
			h.setAgentUpgradeFailed(nodeID, agent.agentVersion, response.Error)
		}
		return response, nil
	case <-time.After(15 * time.Second):
		h.setAgentUpgradeFailed(nodeID, agent.agentVersion, "Agent 升级请求超时。")
		return protocol.AgentUpgradeResponse{Code: "timeout", Error: "Agent 升级请求超时。"}, nil
	case <-ctx.Done():
		h.setAgentUpgradeFailed(nodeID, agent.agentVersion, ctx.Err().Error())
		return protocol.AgentUpgradeResponse{}, ctx.Err()
	}
}

func (h *Handler) prepareAgentUpgradeRequest(node store.Node, requestID string, targetVersion string) (protocol.AgentUpgradeRequest, string, error) {
	if node.OS != "linux" || (node.Arch != "amd64" && node.Arch != "arm64") {
		return protocol.AgentUpgradeRequest{}, "unsupported_platform", errors.New("当前系统架构暂不支持一键升级。")
	}
	if h.options.PublicURL == "" {
		return protocol.AgentUpgradeRequest{}, "public_url_missing", errors.New("Server public_url 未配置，无法生成安全升级地址。")
	}
	name := "mizupanel-agent-linux-" + node.Arch
	content, err := os.ReadFile(filepath.Join(h.options.DownloadDir, name))
	if err != nil {
		return protocol.AgentUpgradeRequest{}, "package_missing", errors.New("Server 未准备对应的 Agent 升级包。")
	}
	sum := sha256.Sum256(content)
	return protocol.AgentUpgradeRequest{
		Type:          protocol.MessageTypeAgentUpgradeRequest,
		RequestID:     requestID,
		NodeID:        node.ID,
		TargetVersion: targetVersion,
		DownloadURL:   strings.TrimRight(h.options.PublicURL, "/") + "/downloads/" + name,
		SHA256:        hex.EncodeToString(sum[:]),
	}, "", nil
}

func (h *Handler) AttachTerminal(ctx context.Context, nodeID string, browser *websocket.Conn) error {
	browser.SetReadLimit(maxBrowserInputBytes)
	agent := h.connection(nodeID)
	if agent == nil {
		_ = browser.WriteJSON(protocol.TerminalMessage{Type: protocol.MessageTypeTerminalError, Error: "节点 Agent 当前不在线"})
		return errors.New("agent offline")
	}
	if !agent.terminalEnabled {
		_ = browser.WriteJSON(protocol.TerminalMessage{Type: protocol.MessageTypeTerminalError, Error: "该节点未启用终端"})
		return errors.New("terminal disabled")
	}
	sessionID, err := randomTerminalSessionID()
	if err != nil {
		return err
	}
	terminal := &browserTerminal{sessionID: sessionID, conn: browser}
	if !agent.addTerminal(terminal) {
		_ = terminal.writeJSON(protocol.TerminalMessage{Type: protocol.MessageTypeTerminalError, Error: "终端会话数量已达上限"})
		return errors.New("too many terminal sessions")
	}
	defer func() {
		agent.removeTerminal(sessionID)
		_ = agent.writeJSON(protocol.TerminalMessage{Type: protocol.MessageTypeTerminalClose, SessionID: sessionID})
	}()
	if err := agent.writeJSON(protocol.TerminalMessage{Type: protocol.MessageTypeTerminalStart, SessionID: sessionID, Rows: 24, Cols: 80}); err != nil {
		return err
	}
	for {
		var message protocol.TerminalMessage
		if err := browser.ReadJSON(&message); err != nil {
			return err
		}
		message.SessionID = sessionID
		message.NodeID = ""
		switch message.Type {
		case protocol.MessageTypeTerminalData, protocol.MessageTypeTerminalResize, protocol.MessageTypeTerminalClose:
			if err := agent.writeJSON(message); err != nil {
				return err
			}
			if message.Type == protocol.MessageTypeTerminalClose {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (h *Handler) AttachContainerExec(ctx context.Context, nodeID string, containerID string, browser *websocket.Conn) error {
	browser.SetReadLimit(maxBrowserInputBytes)
	agent := h.connection(nodeID)
	if agent == nil {
		_ = browser.WriteJSON(protocol.ContainerExecMessage{Type: protocol.MessageTypeContainerExecError, Error: "节点 Agent 当前不在线"})
		return errors.New("agent offline")
	}
	if !agent.terminalEnabled {
		_ = browser.WriteJSON(protocol.ContainerExecMessage{Type: protocol.MessageTypeContainerExecError, Error: "该节点未启用终端或 Docker exec"})
		return errors.New("container exec disabled")
	}
	sessionID, err := randomTerminalSessionID()
	if err != nil {
		return err
	}
	execSession := &browserContainerExec{sessionID: sessionID, containerID: containerID, conn: browser}
	if !agent.addContainerExec(execSession) {
		_ = execSession.writeJSON(protocol.ContainerExecMessage{Type: protocol.MessageTypeContainerExecError, Error: "终端会话数量已达上限"})
		return errors.New("too many container exec sessions")
	}
	defer func() {
		agent.removeContainerExec(sessionID)
		_ = agent.writeJSON(protocol.ContainerExecMessage{Type: protocol.MessageTypeContainerExecClose, SessionID: sessionID, ContainerID: containerID})
	}()
	if err := agent.writeJSON(protocol.ContainerExecMessage{Type: protocol.MessageTypeContainerExecStart, SessionID: sessionID, ContainerID: containerID, Command: "/bin/sh", Rows: 24, Cols: 80}); err != nil {
		return err
	}
	for {
		var message protocol.ContainerExecMessage
		if err := browser.ReadJSON(&message); err != nil {
			return err
		}
		message.SessionID = sessionID
		message.NodeID = ""
		message.ContainerID = containerID
		switch message.Type {
		case protocol.MessageTypeContainerExecData, protocol.MessageTypeContainerExecResize, protocol.MessageTypeContainerExecClose:
			if err := agent.writeJSON(message); err != nil {
				return err
			}
			if message.Type == protocol.MessageTypeContainerExecClose {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (h *Handler) DockerExec(ctx context.Context, nodeID string, command string) (protocol.DockerExecResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.DockerExecResponse{Type: protocol.MessageTypeDockerExecResponse, Accepted: false, ExitCode: 1, Error: "节点离线，无法执行 Docker 命令。"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.DockerExecResponse{}, err
	}
	ch := make(chan protocol.DockerExecResponse, 1)
	agent.addDockerExecRequest(requestID, ch)
	defer agent.removeDockerExecRequest(requestID)

	request := protocol.DockerExecRequest{
		Type:      protocol.MessageTypeDockerExecRequest,
		RequestID: requestID,
		NodeID:    nodeID,
		Command:   command,
	}

	if err := agent.writeJSON(request); err != nil {
		return protocol.DockerExecResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(35 * time.Second):
		return protocol.DockerExecResponse{Type: protocol.MessageTypeDockerExecResponse, Accepted: false, ExitCode: 1, Error: "Docker 命令执行超时。"}, nil
	case <-ctx.Done():
		return protocol.DockerExecResponse{}, ctx.Err()
	}
}

func (h *Handler) ContainerStart(ctx context.Context, nodeID string, containerID string) (protocol.ContainerStartResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.ContainerStartResponse{Type: protocol.MessageTypeContainerStartResponse, Success: false, Error: "节点离线"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.ContainerStartResponse{}, err
	}
	ch := make(chan protocol.ContainerStartResponse, 1)
	agent.addContainerStartRequest(requestID, ch)
	defer agent.removeContainerStartRequest(requestID)

	request := protocol.ContainerStartRequest{
		Type:        protocol.MessageTypeContainerStartRequest,
		RequestID:   requestID,
		NodeID:      nodeID,
		ContainerID: containerID,
	}

	if err := agent.writeJSON(request); err != nil {
		return protocol.ContainerStartResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(30 * time.Second):
		return protocol.ContainerStartResponse{Type: protocol.MessageTypeContainerStartResponse, Success: false, Error: "操作超时"}, nil
	case <-ctx.Done():
		return protocol.ContainerStartResponse{}, ctx.Err()
	}
}

func (h *Handler) ContainerStop(ctx context.Context, nodeID string, containerID string) (protocol.ContainerStopResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.ContainerStopResponse{Type: protocol.MessageTypeContainerStopResponse, Success: false, Error: "节点离线"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.ContainerStopResponse{}, err
	}
	ch := make(chan protocol.ContainerStopResponse, 1)
	agent.addContainerStopRequest(requestID, ch)
	defer agent.removeContainerStopRequest(requestID)

	request := protocol.ContainerStopRequest{
		Type:        protocol.MessageTypeContainerStopRequest,
		RequestID:   requestID,
		NodeID:      nodeID,
		ContainerID: containerID,
	}

	if err := agent.writeJSON(request); err != nil {
		return protocol.ContainerStopResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(30 * time.Second):
		return protocol.ContainerStopResponse{Type: protocol.MessageTypeContainerStopResponse, Success: false, Error: "操作超时"}, nil
	case <-ctx.Done():
		return protocol.ContainerStopResponse{}, ctx.Err()
	}
}

func (h *Handler) ContainerRestart(ctx context.Context, nodeID string, containerID string) (protocol.ContainerRestartResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.ContainerRestartResponse{Type: protocol.MessageTypeContainerRestartResponse, Success: false, Error: "节点离线"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.ContainerRestartResponse{}, err
	}
	ch := make(chan protocol.ContainerRestartResponse, 1)
	agent.addContainerRestartRequest(requestID, ch)
	defer agent.removeContainerRestartRequest(requestID)

	request := protocol.ContainerRestartRequest{
		Type:        protocol.MessageTypeContainerRestartRequest,
		RequestID:   requestID,
		NodeID:      nodeID,
		ContainerID: containerID,
	}

	if err := agent.writeJSON(request); err != nil {
		return protocol.ContainerRestartResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(30 * time.Second):
		return protocol.ContainerRestartResponse{Type: protocol.MessageTypeContainerRestartResponse, Success: false, Error: "操作超时"}, nil
	case <-ctx.Done():
		return protocol.ContainerRestartResponse{}, ctx.Err()
	}
}

func (h *Handler) ContainerDelete(ctx context.Context, nodeID string, containerID string, force bool) (protocol.ContainerDeleteResponse, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return protocol.ContainerDeleteResponse{Type: protocol.MessageTypeContainerDeleteResponse, Success: false, Error: "节点离线"}, nil
	}
	requestID, err := randomTerminalSessionID()
	if err != nil {
		return protocol.ContainerDeleteResponse{}, err
	}
	ch := make(chan protocol.ContainerDeleteResponse, 1)
	agent.addContainerDeleteRequest(requestID, ch)
	defer agent.removeContainerDeleteRequest(requestID)

	request := protocol.ContainerDeleteRequest{
		Type:        protocol.MessageTypeContainerDeleteRequest,
		RequestID:   requestID,
		NodeID:      nodeID,
		ContainerID: containerID,
		Force:       force,
	}

	if err := agent.writeJSON(request); err != nil {
		return protocol.ContainerDeleteResponse{}, err
	}
	select {
	case response := <-ch:
		return response, nil
	case <-time.After(30 * time.Second):
		return protocol.ContainerDeleteResponse{Type: protocol.MessageTypeContainerDeleteResponse, Success: false, Error: "操作超时"}, nil
	case <-ctx.Done():
		return protocol.ContainerDeleteResponse{}, ctx.Err()
	}
}

// IsNodeOnline 检查节点是否在线
func (h *Handler) IsNodeOnline(nodeID string) bool {
	return h.connection(nodeID) != nil
}

func (h *Handler) ConnectionDiagnostics(ctx context.Context, nodeID string) (store.ConnectionDiagnostics, error) {
	node, err := h.nodes.Get(ctx, nodeID)
	if err != nil {
		return store.ConnectionDiagnostics{}, err
	}
	events, err := h.events.List(ctx, nodeID)
	if err != nil {
		return store.ConnectionDiagnostics{}, err
	}
	diagnostics := store.ConnectionDiagnostics{NodeID: nodeID, Online: h.IsNodeOnline(nodeID), Health: "healthy", AgentVersion: node.AgentVersion, Events: events}
	diagnostics.LatestVersion = version.Current
	diagnostics.UpgradeAvailable = node.AgentVersion != "" && node.AgentVersion != version.Current
	if !node.LastSeenAt.IsZero() {
		lastHeartbeat := node.LastSeenAt
		diagnostics.LastHeartbeatAt = &lastHeartbeat
	}
	if connection := h.connection(nodeID); connection != nil {
		diagnostics.AgentVersion = connection.agentVersion
		diagnostics.ProtocolVersion = connection.protocolVersion
		diagnostics.IdentitySource = connection.identitySource
		diagnostics.UpgradeSupported = connection.supportsAgentUpgrade
	}
	for _, event := range events {
		switch event.Type {
		case store.ConnectionEventConnected:
			if diagnostics.LastConnectedAt == nil {
				connectedAt := event.CreatedAt
				diagnostics.LastConnectedAt = &connectedAt
			}
		case store.ConnectionEventDisconnected:
			if diagnostics.LastDisconnectedAt == nil {
				disconnectedAt := event.CreatedAt
				diagnostics.LastDisconnectedAt = &disconnectedAt
				diagnostics.LastDisconnectReason = event.Reason
			}
		case store.ConnectionEventIdentityConflict:
			diagnostics.IdentityConflict = true
		}
	}
	if diagnostics.IdentityConflict {
		diagnostics.Health = "identity_conflict"
	} else if !diagnostics.Online {
		diagnostics.Health = "offline"
	}
	return diagnostics, nil
}

// SendToNodeWithTimeout 发送消息到节点并等待响应
func (h *Handler) SendToNodeWithTimeout(nodeID string, message interface{}, timeout time.Duration) (json.RawMessage, error) {
	agent := h.connection(nodeID)
	if agent == nil {
		return nil, errors.New("节点离线")
	}

	// 从消息中提取 request_id（K8s 消息已经包含了 RequestID）
	var header struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		ClusterID string `json:"cluster_id"`
		Namespace string `json:"namespace"`
	}
	msgBytes, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("序列化消息失败: %w", err)
	}
	if err := json.Unmarshal(msgBytes, &header); err != nil {
		return nil, fmt.Errorf("提取 request_id 失败: %w", err)
	}
	if header.RequestID == "" {
		return nil, errors.New("消息缺少 request_id 字段")
	}

	// 创建响应通道
	ch := make(chan json.RawMessage, 1)
	agent.pendingMu.Lock()
	agent.pendingK8sMessages[header.RequestID] = ch
	agent.pendingMu.Unlock()

	defer func() {
		agent.pendingMu.Lock()
		delete(agent.pendingK8sMessages, header.RequestID)
		agent.pendingMu.Unlock()
	}()

	start := time.Now()
	if h.options.Debug {
		log.Printf("[debug][server][agenthub] send node_id=%s type=%s request_id=%s cluster_id=%s namespace=%s timeout=%s", nodeID, header.Type, header.RequestID, header.ClusterID, header.Namespace, timeout)
	}

	// 发送消息
	if err := agent.writeJSON(message); err != nil {
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] send failed node_id=%s type=%s request_id=%s elapsed=%s error=%v", nodeID, header.Type, header.RequestID, time.Since(start), err)
		}
		return nil, err
	}

	// 等待响应
	select {
	case response := <-ch:
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] response node_id=%s type=%s request_id=%s elapsed=%s success=true", nodeID, header.Type, header.RequestID, time.Since(start))
		}
		return response, nil
	case <-time.After(timeout):
		if h.options.Debug {
			log.Printf("[debug][server][agenthub] timeout node_id=%s type=%s request_id=%s elapsed=%s", nodeID, header.Type, header.RequestID, time.Since(start))
		}
		return nil, errors.New("请求超时")
	}
}

func (h *Handler) AttachLogTail(ctx context.Context, nodeID string, browser *websocket.Conn) error {
	log.Printf("[LogTail] AttachLogTail called for node %s", nodeID)
	browser.SetReadLimit(maxBrowserInputBytes)
	agent := h.connection(nodeID)
	if agent == nil {
		log.Printf("[LogTail] Agent offline for node %s", nodeID)
		_ = browser.WriteJSON(protocol.LogTailError{Type: protocol.MessageTypeLogTailError, Error: "节点 Agent 当前不在线"})
		return errors.New("agent offline")
	}

	sessionID, err := randomTerminalSessionID()
	if err != nil {
		return err
	}
	log.Printf("[LogTail] Created session %s for node %s", sessionID, nodeID)

	// Read initial request from browser
	var request protocol.LogTailRequest
	if err := browser.ReadJSON(&request); err != nil {
		log.Printf("[LogTail] Failed to read browser request: %v", err)
		return err
	}
	log.Printf("[LogTail] Received request from browser: path=%s lines=%d", request.Path, request.Lines)

	request.Type = protocol.MessageTypeLogTailRequest
	request.SessionID = sessionID
	request.NodeID = nodeID

	// Send request to agent
	log.Printf("[LogTail] Sending request to agent: %+v", request)
	if err := agent.writeJSON(request); err != nil {
		log.Printf("[LogTail] Failed to send to agent: %v", err)
		return err
	}
	log.Printf("[LogTail] Request sent to agent successfully")

	// Create a channel to forward messages from agent to browser
	logChan := make(chan json.RawMessage, 16)
	agent.mu.Lock()
	if agent.logTailSessions == nil {
		agent.logTailSessions = make(map[string]chan json.RawMessage)
	}
	agent.logTailSessions[sessionID] = logChan
	agent.mu.Unlock()

	defer func() {
		agent.mu.Lock()
		delete(agent.logTailSessions, sessionID)
		agent.mu.Unlock()
		close(logChan)
		_ = agent.writeJSON(protocol.LogTailStop{Type: protocol.MessageTypeLogTailStop, SessionID: sessionID, NodeID: nodeID})
	}()

	// Forward messages between agent and browser
	errCh := make(chan error, 2)

	// Read from agent and forward to browser
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case msg, ok := <-logChan:
				if !ok {
					return
				}
				log.Printf("[LogTail] Forwarding message from agent to browser: %s", string(msg))
				if err := browser.WriteMessage(websocket.TextMessage, msg); err != nil {
					log.Printf("[LogTail] Failed to write to browser: %v", err)
					errCh <- err
					return
				}
			}
		}
	}()

	// Read stop messages from browser
	go func() {
		for {
			var message protocol.LogTailStop
			if err := browser.ReadJSON(&message); err != nil {
				errCh <- err
				return
			}
			if message.Type == protocol.MessageTypeLogTailStop {
				errCh <- nil
				return
			}
		}
	}()

	return <-errCh
}

func (h *Handler) AttachContainerLogs(ctx context.Context, nodeID string, containerID string, browser *websocket.Conn) error {
	browser.SetReadLimit(maxBrowserInputBytes)
	agent := h.connection(nodeID)
	if agent == nil {
		_ = browser.WriteJSON(protocol.ContainerLogsError{Type: protocol.MessageTypeContainerLogsError, Error: "节点 Agent 当前不在线"})
		return errors.New("agent offline")
	}

	sessionID, err := randomTerminalSessionID()
	if err != nil {
		return err
	}

	// Read initial request from browser
	var request protocol.ContainerLogsRequest
	if err := browser.ReadJSON(&request); err != nil {
		return err
	}

	request.Type = protocol.MessageTypeContainerLogsRequest
	request.SessionID = sessionID
	request.NodeID = nodeID
	request.ContainerID = containerID

	// Send request to agent
	if err := agent.writeJSON(request); err != nil {
		return err
	}

	// Create a channel to forward messages from agent to browser
	logChan := make(chan json.RawMessage, 16)
	agent.mu.Lock()
	if agent.containerLogsSessions == nil {
		agent.containerLogsSessions = make(map[string]chan json.RawMessage)
	}
	agent.containerLogsSessions[sessionID] = logChan
	agent.mu.Unlock()

	defer func() {
		agent.mu.Lock()
		delete(agent.containerLogsSessions, sessionID)
		agent.mu.Unlock()
		close(logChan)
		_ = agent.writeJSON(protocol.ContainerLogsStop{Type: protocol.MessageTypeContainerLogsStop, SessionID: sessionID, NodeID: nodeID})
	}()

	// Forward messages between agent and browser
	errCh := make(chan error, 2)

	// Read from agent and forward to browser
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case msg, ok := <-logChan:
				if !ok {
					return
				}
				if err := browser.WriteMessage(websocket.TextMessage, msg); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	// Read stop messages from browser
	go func() {
		for {
			var message protocol.ContainerLogsStop
			if err := browser.ReadJSON(&message); err != nil {
				errCh <- err
				return
			}
			if message.Type == protocol.MessageTypeContainerLogsStop {
				errCh <- nil
				return
			}
		}
	}()

	return <-errCh
}

func (h *Handler) connection(nodeID string) *agentConnection {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connections[nodeID]
}

func (h *Handler) RevokeNode(nodeID string) {
	if h.options.InstallAuth != nil {
		h.options.InstallAuth.RevokeNodeToken(nodeID)
	}
}

func (h *Handler) allowNode(ctx context.Context, nodeID string, installTokenCreatedAt time.Time) (bool, error) {
	return h.nodes.AllowIfDeletedBefore(ctx, nodeID, installTokenCreatedAt)
}

func (h *Handler) nodeDeleted(ctx context.Context, nodeID string) bool {
	persisted, err := h.nodes.IsDeleted(ctx, nodeID)
	return err == nil && persisted
}

func (h *Handler) DisconnectNode(nodeID string) {
	h.mu.Lock()
	connection := h.connections[nodeID]
	delete(h.connections, nodeID)
	delete(h.sessions, nodeID)
	h.mu.Unlock()
	h.RevokeNode(nodeID)
	if connection != nil {
		connection.closeTerminals("节点已从面板移除")
		connection.closeContainerExecs("节点已从面板移除")
		connection.closePendingOperations("节点已从面板移除")
		_ = connection.conn.Close()
	}
}

func (h *Handler) registerConnection(connection *agentConnection) {
	h.mu.Lock()
	previous := h.connections[connection.nodeID]
	h.connections[connection.nodeID] = connection
	h.mu.Unlock()
	if previous != nil && previous != connection {
		h.recordConnectionEvent(previous, store.ConnectionEventReplaced, "replaced by a newer connection")
		if previous.hostname != "" && connection.hostname != "" && previous.hostname != connection.hostname {
			h.recordConnectionEvent(connection, store.ConnectionEventIdentityConflict, fmt.Sprintf("same Agent ID reported different hostnames: %s and %s", previous.hostname, connection.hostname))
		}
		previous.closeTerminals("Agent 已重新连接，旧终端会话已关闭")
		previous.closeContainerExecs("Agent 已重新连接，旧容器终端会话已关闭")
		previous.closePendingOperations("Agent 已重新连接，请重试操作")
		_ = previous.conn.Close()
	}
}

func (h *Handler) recordConnectionEvent(connection *agentConnection, eventType string, reason string) {
	if h.events == nil || connection == nil {
		return
	}
	event := &store.ConnectionEvent{NodeID: connection.nodeID, Type: eventType, Reason: reason, AgentVersion: connection.agentVersion, ProtocolVersion: connection.protocolVersion, IdentitySource: connection.identitySource, Hostname: connection.hostname, RemoteAddr: connection.remoteAddr, CreatedAt: time.Now().UTC()}
	if err := h.events.Create(context.Background(), event); err != nil && h.options.Debug {
		log.Printf("[debug][server][agenthub] record connection event failed node_id=%s type=%s error=%v", connection.nodeID, eventType, err)
	}
}

func (h *Handler) unregisterConnection(connection *agentConnection) {
	h.mu.Lock()
	current := h.connections[connection.nodeID]
	if current == connection {
		delete(h.connections, connection.nodeID)
	}
	h.mu.Unlock()
	connection.closeTerminals("Agent 连接已断开")
	connection.closeContainerExecs("Agent 连接已断开")
	connection.closePendingOperations("Agent 连接已断开")
}

func (h *Handler) routeTerminalMessage(agent *agentConnection, message protocol.TerminalMessage) {
	terminal := agent.terminal(message.SessionID)
	if terminal == nil {
		return
	}
	if err := terminal.writeJSON(message); err != nil {
		agent.removeTerminal(message.SessionID)
		terminal.close()
		return
	}
	if message.Type == protocol.MessageTypeTerminalExit || message.Type == protocol.MessageTypeTerminalError || message.Type == protocol.MessageTypeTerminalClose {
		agent.removeTerminal(message.SessionID)
		terminal.close()
	}
}

func (h *Handler) routeContainerExecMessage(agent *agentConnection, message protocol.ContainerExecMessage) {
	execSession := agent.containerExec(message.SessionID)
	if execSession == nil {
		return
	}
	if err := execSession.writeJSON(message); err != nil {
		agent.removeContainerExec(message.SessionID)
		execSession.close()
		return
	}
	if message.Type == protocol.MessageTypeContainerExecExit || message.Type == protocol.MessageTypeContainerExecError || message.Type == protocol.MessageTypeContainerExecClose {
		agent.removeContainerExec(message.SessionID)
		execSession.close()
	}
}

func (c *agentConnection) addFileListRequest(requestID string, ch chan protocol.FileListResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingLists[requestID] = ch
}

func (c *agentConnection) addFileReadRequest(requestID string, ch chan protocol.FileReadResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingReads[requestID] = ch
}

func (c *agentConnection) addFileWriteRequest(requestID string, ch chan protocol.FileWriteResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingWrites[requestID] = ch
}

func (c *agentConnection) addFileUploadRequest(requestID string, ch chan protocol.FileUploadResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingUploads[requestID] = ch
}

func (c *agentConnection) addFileDeleteRequest(requestID string, ch chan protocol.FileDeleteResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingDeletes[requestID] = ch
}

func (c *agentConnection) addRebootRequest(requestID string, ch chan protocol.RebootResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingReboots[requestID] = ch
}

func (c *agentConnection) removeFileListRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingLists, requestID)
}

func (c *agentConnection) removeFileReadRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingReads, requestID)
}

func (c *agentConnection) removeFileWriteRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingWrites, requestID)
}

func (c *agentConnection) removeFileUploadRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingUploads, requestID)
}

func (c *agentConnection) removeFileDeleteRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingDeletes, requestID)
}

func (c *agentConnection) removeRebootRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingReboots, requestID)
}

func (c *agentConnection) addAgentStatusRequest(requestID string, ch chan protocol.AgentStatusResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingAgentStatuses[requestID] = ch
}

func (c *agentConnection) addAgentRestartRequest(requestID string, ch chan protocol.AgentRestartResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingAgentRestarts[requestID] = ch
}

func (c *agentConnection) addAgentLogsRequest(requestID string, ch chan protocol.AgentLogsResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingAgentLogs[requestID] = ch
}

func (c *agentConnection) addAgentUpgradeRequest(requestID string, ch chan protocol.AgentUpgradeResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingAgentUpgrades[requestID] = ch
}

func (c *agentConnection) removeAgentStatusRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingAgentStatuses, requestID)
}

func (c *agentConnection) removeAgentRestartRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingAgentRestarts, requestID)
}

func (c *agentConnection) removeAgentLogsRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingAgentLogs, requestID)
}

func (c *agentConnection) removeAgentUpgradeRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingAgentUpgrades, requestID)
}

func (c *agentConnection) addDockerExecRequest(requestID string, ch chan protocol.DockerExecResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingDockerExecs[requestID] = ch
}

func (c *agentConnection) removeDockerExecRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingDockerExecs, requestID)
}

func (c *agentConnection) addContainerStartRequest(requestID string, ch chan protocol.ContainerStartResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingContainerStarts[requestID] = ch
}

func (c *agentConnection) removeContainerStartRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingContainerStarts, requestID)
}

func (c *agentConnection) addContainerStopRequest(requestID string, ch chan protocol.ContainerStopResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingContainerStops[requestID] = ch
}

func (c *agentConnection) removeContainerStopRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingContainerStops, requestID)
}

func (c *agentConnection) addContainerRestartRequest(requestID string, ch chan protocol.ContainerRestartResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingContainerRestarts[requestID] = ch
}

func (c *agentConnection) removeContainerRestartRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingContainerRestarts, requestID)
}

func (c *agentConnection) addContainerDeleteRequest(requestID string, ch chan protocol.ContainerDeleteResponse) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pendingContainerDeletes[requestID] = ch
}

func (c *agentConnection) removeContainerDeleteRequest(requestID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pendingContainerDeletes, requestID)
}

func (c *agentConnection) deliverFileList(response protocol.FileListResponse) {
	c.pendingMu.Lock()
	ch := c.pendingLists[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverFileRead(response protocol.FileReadResponse) {
	c.pendingMu.Lock()
	ch := c.pendingReads[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverFileWrite(response protocol.FileWriteResponse) {
	c.pendingMu.Lock()
	ch := c.pendingWrites[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverFileUpload(response protocol.FileUploadResponse) {
	c.pendingMu.Lock()
	ch := c.pendingUploads[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverFileDelete(response protocol.FileDeleteResponse) {
	c.pendingMu.Lock()
	ch := c.pendingDeletes[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverReboot(response protocol.RebootResponse) {
	c.pendingMu.Lock()
	ch := c.pendingReboots[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverAgentStatus(response protocol.AgentStatusResponse) {
	c.pendingMu.Lock()
	ch := c.pendingAgentStatuses[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverAgentRestart(response protocol.AgentRestartResponse) {
	c.pendingMu.Lock()
	ch := c.pendingAgentRestarts[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverAgentLogs(response protocol.AgentLogsResponse) {
	c.pendingMu.Lock()
	ch := c.pendingAgentLogs[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverAgentUpgrade(response protocol.AgentUpgradeResponse) {
	c.pendingMu.Lock()
	ch := c.pendingAgentUpgrades[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverDockerExec(response protocol.DockerExecResponse) {
	c.pendingMu.Lock()
	ch := c.pendingDockerExecs[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverContainerStart(response protocol.ContainerStartResponse) {
	c.pendingMu.Lock()
	ch := c.pendingContainerStarts[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverContainerStop(response protocol.ContainerStopResponse) {
	c.pendingMu.Lock()
	ch := c.pendingContainerStops[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverContainerRestart(response protocol.ContainerRestartResponse) {
	c.pendingMu.Lock()
	ch := c.pendingContainerRestarts[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverContainerDelete(response protocol.ContainerDeleteResponse) {
	c.pendingMu.Lock()
	ch := c.pendingContainerDeletes[response.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (c *agentConnection) deliverK8sMessage(requestID string, rawMessage json.RawMessage) {
	c.pendingMu.Lock()
	ch := c.pendingK8sMessages[requestID]
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- rawMessage
	}
}

func (c *agentConnection) closePendingOperations(reason string) {
	c.pendingMu.Lock()
	lists := c.pendingLists
	reads := c.pendingReads
	writes := c.pendingWrites
	uploads := c.pendingUploads
	deletes := c.pendingDeletes
	reboots := c.pendingReboots
	agentStatuses := c.pendingAgentStatuses
	agentRestarts := c.pendingAgentRestarts
	agentLogs := c.pendingAgentLogs
	agentUpgrades := c.pendingAgentUpgrades
	dockerExecs := c.pendingDockerExecs
	containerStarts := c.pendingContainerStarts
	containerStops := c.pendingContainerStops
	containerRestarts := c.pendingContainerRestarts
	containerDeletes := c.pendingContainerDeletes
	k8sMessages := c.pendingK8sMessages
	c.pendingLists = make(map[string]chan protocol.FileListResponse)
	c.pendingReads = make(map[string]chan protocol.FileReadResponse)
	c.pendingWrites = make(map[string]chan protocol.FileWriteResponse)
	c.pendingUploads = make(map[string]chan protocol.FileUploadResponse)
	c.pendingDeletes = make(map[string]chan protocol.FileDeleteResponse)
	c.pendingReboots = make(map[string]chan protocol.RebootResponse)
	c.pendingAgentStatuses = make(map[string]chan protocol.AgentStatusResponse)
	c.pendingAgentRestarts = make(map[string]chan protocol.AgentRestartResponse)
	c.pendingAgentLogs = make(map[string]chan protocol.AgentLogsResponse)
	c.pendingAgentUpgrades = make(map[string]chan protocol.AgentUpgradeResponse)
	c.pendingDockerExecs = make(map[string]chan protocol.DockerExecResponse)
	c.pendingContainerStarts = make(map[string]chan protocol.ContainerStartResponse)
	c.pendingContainerStops = make(map[string]chan protocol.ContainerStopResponse)
	c.pendingContainerRestarts = make(map[string]chan protocol.ContainerRestartResponse)
	c.pendingContainerDeletes = make(map[string]chan protocol.ContainerDeleteResponse)
	c.pendingK8sMessages = make(map[string]chan json.RawMessage)
	c.pendingMu.Unlock()
	for requestID, ch := range lists {
		ch <- protocol.FileListResponse{Type: protocol.MessageTypeFileListResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range reads {
		ch <- protocol.FileReadResponse{Type: protocol.MessageTypeFileReadResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range writes {
		ch <- protocol.FileWriteResponse{Type: protocol.MessageTypeFileWriteResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range uploads {
		ch <- protocol.FileUploadResponse{Type: protocol.MessageTypeFileUploadResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range deletes {
		ch <- protocol.FileDeleteResponse{Type: protocol.MessageTypeFileDeleteResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range reboots {
		ch <- protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range agentStatuses {
		ch <- protocol.AgentStatusResponse{Type: protocol.MessageTypeAgentStatusResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range agentRestarts {
		ch <- protocol.AgentRestartResponse{Type: protocol.MessageTypeAgentRestartResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range agentLogs {
		ch <- protocol.AgentLogsResponse{Type: protocol.MessageTypeAgentLogsResponse, RequestID: requestID, Code: "offline", Error: reason}
	}
	for requestID, ch := range agentUpgrades {
		ch <- protocol.AgentUpgradeResponse{Type: protocol.MessageTypeAgentUpgradeResponse, RequestID: requestID, Stage: "failed", Code: "offline", Error: reason}
	}
	for _, ch := range dockerExecs {
		ch <- protocol.DockerExecResponse{Type: protocol.MessageTypeDockerExecResponse, Accepted: false, ExitCode: 1, Error: reason}
	}
	for _, ch := range containerStarts {
		ch <- protocol.ContainerStartResponse{Type: protocol.MessageTypeContainerStartResponse, Success: false, Error: reason}
	}
	for _, ch := range containerStops {
		ch <- protocol.ContainerStopResponse{Type: protocol.MessageTypeContainerStopResponse, Success: false, Error: reason}
	}
	for _, ch := range containerRestarts {
		ch <- protocol.ContainerRestartResponse{Type: protocol.MessageTypeContainerRestartResponse, Success: false, Error: reason}
	}
	for _, ch := range containerDeletes {
		ch <- protocol.ContainerDeleteResponse{Type: protocol.MessageTypeContainerDeleteResponse, Success: false, Error: reason}
	}
	for requestID, ch := range k8sMessages {
		response := protocol.K8sGetSummaryResult{Type: protocol.MessageTypeK8sGetSummaryResult, RequestID: requestID, Success: false, Error: reason}
		data, err := json.Marshal(response)
		if err != nil {
			continue
		}
		ch <- data
	}
}

func (c *agentConnection) writeJSON(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(terminalWriteTimeout))
	defer c.conn.SetWriteDeadline(time.Time{})
	return c.conn.WriteJSON(value)
}

func (c *agentConnection) addTerminal(terminal *browserTerminal) bool {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if len(c.terminals)+len(c.containerExecs) >= maxServerTerminalSessions {
		return false
	}
	c.terminals[terminal.sessionID] = terminal
	return true
}

func (c *agentConnection) removeTerminal(sessionID string) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	delete(c.terminals, sessionID)
}

func (c *agentConnection) terminal(sessionID string) *browserTerminal {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.terminals[sessionID]
}

func (c *agentConnection) closeTerminals(reason string) {
	c.sessionMu.Lock()
	terminals := make([]*browserTerminal, 0, len(c.terminals))
	for sessionID, terminal := range c.terminals {
		terminals = append(terminals, terminal)
		delete(c.terminals, sessionID)
	}
	c.sessionMu.Unlock()
	for _, terminal := range terminals {
		go func(terminal *browserTerminal) {
			defer terminal.close()
			_ = terminal.writeJSON(protocol.TerminalMessage{Type: protocol.MessageTypeTerminalError, SessionID: terminal.sessionID, Error: reason})
		}(terminal)
	}
}

func (c *agentConnection) addContainerExec(execSession *browserContainerExec) bool {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if len(c.terminals)+len(c.containerExecs) >= maxServerTerminalSessions {
		return false
	}
	c.containerExecs[execSession.sessionID] = execSession
	return true
}

func (c *agentConnection) removeContainerExec(sessionID string) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	delete(c.containerExecs, sessionID)
}

func (c *agentConnection) containerExec(sessionID string) *browserContainerExec {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.containerExecs[sessionID]
}

func (c *agentConnection) closeContainerExecs(reason string) {
	c.sessionMu.Lock()
	execSessions := make([]*browserContainerExec, 0, len(c.containerExecs))
	for sessionID, execSession := range c.containerExecs {
		execSessions = append(execSessions, execSession)
		delete(c.containerExecs, sessionID)
	}
	c.sessionMu.Unlock()
	for _, execSession := range execSessions {
		go func(execSession *browserContainerExec) {
			defer execSession.close()
			_ = execSession.writeJSON(protocol.ContainerExecMessage{Type: protocol.MessageTypeContainerExecError, SessionID: execSession.sessionID, ContainerID: execSession.containerID, Error: reason})
		}(execSession)
	}
}

func (t *browserTerminal) writeJSON(value any) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_ = t.conn.SetWriteDeadline(time.Now().Add(terminalWriteTimeout))
	defer t.conn.SetWriteDeadline(time.Time{})
	return t.conn.WriteJSON(value)
}

func (t *browserTerminal) close() {
	t.closeOnce.Do(func() { _ = t.conn.Close() })
}

func (e *browserContainerExec) writeJSON(value any) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	_ = e.conn.SetWriteDeadline(time.Now().Add(terminalWriteTimeout))
	defer e.conn.SetWriteDeadline(time.Time{})
	return e.conn.WriteJSON(value)
}

func (e *browserContainerExec) close() {
	e.closeOnce.Do(func() { _ = e.conn.Close() })
}

func randomTerminalSessionID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func bearerToken(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func nodeIDForHello(hello protocol.HelloMessage) string {
	if nodeID := strings.TrimSpace(hello.NodeID); nodeID != "" {
		return nodeID
	}
	sum := sha1.Sum([]byte(hello.Hostname + "|" + hello.OS + "|" + hello.Arch))
	return "node_" + hex.EncodeToString(sum[:])[:12]
}

func (h *Handler) startSession(nodeID string) string {
	sessionID := strconv.FormatInt(time.Now().UnixNano(), 10)
	h.mu.Lock()
	h.sessions[nodeID] = sessionID
	h.mu.Unlock()
	return sessionID
}

func (h *Handler) finishSession(ctx context.Context, nodeID string, sessionID string) {
	h.mu.Lock()
	current := h.sessions[nodeID]
	if current == sessionID {
		delete(h.sessions, nodeID)
	}
	h.mu.Unlock()
	if current == sessionID {
		_ = h.nodes.SetStatus(ctx, nodeID, "offline", time.Now().UTC())
	}
}
