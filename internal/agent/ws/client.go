package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mizupanel/mizupanel/internal/agent/filetree"
	"github.com/mizupanel/mizupanel/internal/agent/reboot"
	"github.com/mizupanel/mizupanel/internal/protocol"
)

type Client struct {
	serverURL                   string
	token                       string
	debug                       bool
	onNodeToken                 func(string) error
	onConnected                 func()
	agentManagementHandler      AgentManagementHandler
	terminalHandlerFactory      TerminalHandlerFactory
	containerExecHandlerFactory ContainerExecHandlerFactory
	logTailHandler              LogTailHandler
	containerLogsHandler        ContainerLogsHandler
	dockerExecHandler           DockerExecHandler
	containerOpsHandler         ContainerOperationsHandler
	systemdServiceHandler       SystemdServiceHandler
	kubectlHandler              KubectlHandler
	taskRunner                  TaskRunner
}

type TerminalSender interface {
	SendTerminal(protocol.TerminalMessage) error
}

type ContainerExecSender interface {
	SendContainerExec(protocol.ContainerExecMessage) error
}

type LogTailSender interface {
	SendLogTail(interface{}) error
}

type ContainerLogsSender interface {
	SendContainerLogs(interface{}) error
}

type AgentManagementHandler interface {
	Status() protocol.AgentStatusResponse
	Restart() protocol.AgentRestartResponse
	Logs(lines int) protocol.AgentLogsResponse
	Upgrade(protocol.AgentUpgradeRequest, func(protocol.AgentUpgradeResponse)) protocol.AgentUpgradeResponse
}

type TerminalHandler interface {
	Handle(protocol.TerminalMessage)
	CloseAll()
}

type ContainerExecHandler interface {
	Handle(protocol.ContainerExecMessage)
	CloseAll()
}

type LogTailHandler interface {
	Handle(context.Context, protocol.LogTailRequest, LogTailSender) error
	Stop(sessionID string)
	CloseAll()
}

type ContainerLogsHandler interface {
	Handle(context.Context, protocol.ContainerLogsRequest, ContainerLogsSender) error
	Stop(sessionID string)
	CloseAll()
}

type DockerExecHandler interface {
	HandleDockerExec(context.Context, protocol.DockerExecRequest) protocol.DockerExecResponse
}

type ContainerOperationsHandler interface {
	HandleContainerStart(context.Context, protocol.ContainerStartRequest) protocol.ContainerStartResponse
	HandleContainerStop(context.Context, protocol.ContainerStopRequest) protocol.ContainerStopResponse
	HandleContainerRestart(context.Context, protocol.ContainerRestartRequest) protocol.ContainerRestartResponse
	HandleContainerDelete(context.Context, protocol.ContainerDeleteRequest) protocol.ContainerDeleteResponse
	HandleDockerContainerCreate(context.Context, protocol.DockerContainerCreateRequest) protocol.DockerContainerCreateResponse
	HandleDockerComposeList(context.Context, protocol.DockerComposeListRequest) protocol.DockerComposeListResponse
	HandleDockerComposeAction(context.Context, protocol.DockerComposeActionRequest) protocol.DockerComposeActionResponse
	HandleDockerComposeDeployment(context.Context, protocol.DockerComposeDeploymentRequest) protocol.DockerComposeDeploymentResponse
	HandleDockerResourceList(context.Context, protocol.DockerResourceListRequest) protocol.DockerResourceListResponse
	HandleDockerResourceAction(context.Context, protocol.DockerResourceActionRequest) protocol.DockerResourceActionResponse
}

type SystemdServiceHandler interface {
	HandleList(context.Context, protocol.SystemdServiceListRequest) protocol.SystemdServiceListResponse
	HandleAction(context.Context, protocol.SystemdServiceActionRequest) protocol.SystemdServiceActionResponse
}

type KubectlHandler interface {
	Handle(context.Context, string, json.RawMessage, func(interface{}) error) error
}

type TaskRunner interface {
	Run(context.Context, protocol.ScriptExecutionRequest) protocol.ScriptExecutionResponse
}

type TerminalHandlerFactory func(TerminalSender) TerminalHandler

type ContainerExecHandlerFactory func(ContainerExecSender) ContainerExecHandler

const maxServerMessageBytes = filetree.DefaultMaxUploadBytes*4/3 + 64*1024

type connectionWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewClient(serverURL string, token string) *Client {
	return &Client{serverURL: serverURL, token: token}
}

func (c *Client) SetDebug(debug bool) {
	c.debug = debug
}

func (c *Client) SetNodeTokenHandler(handler func(string) error) {
	c.onNodeToken = handler
}

func (c *Client) SetConnectedHandler(handler func()) {
	c.onConnected = handler
}

func (c *Client) SetAgentManagementHandler(handler AgentManagementHandler) {
	c.agentManagementHandler = handler
}

func (c *Client) SetTerminalHandlerFactory(factory TerminalHandlerFactory) {
	c.terminalHandlerFactory = factory
}

func (c *Client) SetContainerExecHandlerFactory(factory ContainerExecHandlerFactory) {
	c.containerExecHandlerFactory = factory
}

func (c *Client) SetLogTailHandler(handler LogTailHandler) {
	c.logTailHandler = handler
}

func (c *Client) SetContainerLogsHandler(handler ContainerLogsHandler) {
	c.containerLogsHandler = handler
}

func (c *Client) SetDockerExecHandler(handler DockerExecHandler) {
	c.dockerExecHandler = handler
}

func (c *Client) SetContainerOperationsHandler(handler ContainerOperationsHandler) {
	c.containerOpsHandler = handler
}

func (c *Client) SetSystemdServiceHandler(handler SystemdServiceHandler) {
	c.systemdServiceHandler = handler
}

func (c *Client) SetKubectlHandler(handler KubectlHandler) {
	c.kubectlHandler = handler
}

func (c *Client) SetTaskRunner(runner TaskRunner) {
	c.taskRunner = runner
}

func (c *Client) SendHelloAndMetric(ctx context.Context, hello protocol.HelloMessage, metric protocol.MetricsMessage) (protocol.HelloAckMessage, error) {
	conn, ack, err := c.connect(ctx, hello)
	if err != nil {
		return protocol.HelloAckMessage{}, err
	}
	defer conn.Close()

	metric.NodeID = ack.NodeID
	if err := conn.WriteJSON(metric); err != nil {
		return protocol.HelloAckMessage{}, err
	}
	return ack, nil
}

type CollectFunc func(nodeID string, timestamp int64) (protocol.MetricsMessage, error)

func (c *Client) Run(ctx context.Context, hello protocol.HelloMessage, interval time.Duration, collect CollectFunc) error {
	conn, ack, err := c.connect(ctx, hello)
	if err != nil {
		return err
	}
	writer := &connectionWriter{conn: conn}
	var terminalHandler TerminalHandler
	if c.terminalHandlerFactory != nil {
		terminalHandler = c.terminalHandlerFactory(writer)
		defer terminalHandler.CloseAll()
	}
	var containerExecHandler ContainerExecHandler
	if c.containerExecHandlerFactory != nil {
		containerExecHandler = c.containerExecHandlerFactory(writer)
		defer containerExecHandler.CloseAll()
	}
	if c.logTailHandler != nil {
		defer c.logTailHandler.CloseAll()
	}

	connectionCtx, cancelConnection := context.WithCancel(ctx)
	var taskExecutions sync.WaitGroup
	readDone := make(chan struct{})
	defer func() {
		cancelConnection()
		_ = conn.Close()
		<-readDone
		taskExecutions.Wait()
	}()
	errCh := make(chan error, 1)
	go func() {
		defer close(readDone)
		c.readLoop(connectionCtx, writer, terminalHandler, containerExecHandler, &taskExecutions, errCh)
	}()
	if err := c.writeCollectedMetric(writer, ack.NodeID, collect); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-ticker.C:
			if err := c.writeCollectedMetric(writer, ack.NodeID, collect); err != nil {
				return err
			}
		}
	}
}

func (c *Client) RunForever(ctx context.Context, hello protocol.HelloMessage, interval time.Duration, reconnectDelay time.Duration, collect CollectFunc) error {
	for {
		if err := c.Run(ctx, hello, interval, collect); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if c.debug {
				log.Printf("[debug][agent][ws] connection loop ended error=%v", err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay):
		}
	}
}

func (c *Client) connect(ctx context.Context, hello protocol.HelloMessage) (*websocket.Conn, protocol.HelloAckMessage, error) {
	header := http.Header{}
	if c.token != "" {
		header.Set("Authorization", "Bearer "+c.token)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.serverURL, header)
	if err != nil {
		return nil, protocol.HelloAckMessage{}, err
	}
	conn.SetReadLimit(maxServerMessageBytes)
	if err := conn.WriteJSON(hello); err != nil {
		conn.Close()
		return nil, protocol.HelloAckMessage{}, err
	}
	var ack protocol.HelloAckMessage
	if err := conn.ReadJSON(&ack); err != nil {
		conn.Close()
		return nil, protocol.HelloAckMessage{}, err
	}
	if ack.NodeToken != "" {
		c.token = ack.NodeToken
		if c.onNodeToken != nil {
			if err := c.onNodeToken(ack.NodeToken); err != nil {
				conn.Close()
				return nil, protocol.HelloAckMessage{}, err
			}
		}
	}
	if c.onConnected != nil {
		c.onConnected()
	}
	return conn, ack, nil
}

func (c *Client) readLoop(ctx context.Context, writer *connectionWriter, terminalHandler TerminalHandler, containerExecHandler ContainerExecHandler, taskExecutions *sync.WaitGroup, errCh chan<- error) {
	for {
		var raw json.RawMessage
		if err := writer.conn.ReadJSON(&raw); err != nil {
			errCh <- err
			return
		}
		var header struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			ClusterID string `json:"cluster_id"`
			Namespace string `json:"namespace"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			if c.debug {
				log.Printf("[debug][agent][ws] parse message header failed error=%v", err)
			}
			continue
		}
		if c.debug && header.Type != protocol.MessageTypeTerminalData && header.Type != protocol.MessageTypeContainerExecData {
			log.Printf("[debug][agent][ws] received type=%s request_id=%s cluster_id=%s namespace=%s", header.Type, header.RequestID, header.ClusterID, header.Namespace)
		}
		switch header.Type {
		case protocol.MessageTypeFileListRequest:
			var request protocol.FileListRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := filetree.List(request.Path, filetree.DefaultMaxEntries)
			response.Type = protocol.MessageTypeFileListResponse
			response.RequestID = request.RequestID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeFileReadRequest:
			var request protocol.FileReadRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := filetree.Read(request.Path, filetree.DefaultMaxEditableBytes)
			response.Type = protocol.MessageTypeFileReadResponse
			response.RequestID = request.RequestID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeFileWriteRequest:
			var request protocol.FileWriteRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := filetree.Write(request.Path, request.Content, filetree.DefaultMaxEditableBytes)
			response.Type = protocol.MessageTypeFileWriteResponse
			response.RequestID = request.RequestID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeFileUploadRequest:
			var request protocol.FileUploadRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := filetree.Upload(request.Path, request.ContentBase64, filetree.DefaultMaxUploadBytes)
			response.Type = protocol.MessageTypeFileUploadResponse
			response.RequestID = request.RequestID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeFileDeleteRequest:
			var request protocol.FileDeleteRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := filetree.Delete(request.Path)
			response.Type = protocol.MessageTypeFileDeleteResponse
			response.RequestID = request.RequestID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeRebootRequest:
			var request protocol.RebootRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			// Acknowledge acceptance before the reboot takes effect so the Server
			// does not time out and misreport a successful reboot as a failure.
			response := reboot.Accept(reboot.CurrentOS(), nil, 2*time.Second, nil)
			response.Type = protocol.MessageTypeRebootResponse
			response.RequestID = request.RequestID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeAgentStatusRequest:
			if c.agentManagementHandler == nil {
				continue
			}
			var request protocol.AgentStatusRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.agentManagementHandler.Status()
			response.Type = protocol.MessageTypeAgentStatusResponse
			response.RequestID = request.RequestID
			response.NodeID = request.NodeID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeAgentRestartRequest:
			if c.agentManagementHandler == nil {
				continue
			}
			var request protocol.AgentRestartRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.agentManagementHandler.Restart()
			response.Type = protocol.MessageTypeAgentRestartResponse
			response.RequestID = request.RequestID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeAgentLogsRequest:
			if c.agentManagementHandler == nil {
				continue
			}
			var request protocol.AgentLogsRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.agentManagementHandler.Logs(request.Lines)
			response.Type = protocol.MessageTypeAgentLogsResponse
			response.RequestID = request.RequestID
			response.NodeID = request.NodeID
			_ = writer.writeJSON(response)
		case protocol.MessageTypeAgentUpgradeRequest:
			if c.agentManagementHandler == nil {
				continue
			}
			var request protocol.AgentUpgradeRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			initialSent := make(chan struct{})
			response := c.agentManagementHandler.Upgrade(request, func(result protocol.AgentUpgradeResponse) {
				<-initialSent
				result.Type = protocol.MessageTypeAgentUpgradeResponse
				result.RequestID = request.RequestID
				_ = writer.writeJSON(result)
			})
			response.Type = protocol.MessageTypeAgentUpgradeResponse
			response.RequestID = request.RequestID
			_ = writer.writeJSON(response)
			close(initialSent)
		case protocol.MessageTypeTerminalStart, protocol.MessageTypeTerminalData, protocol.MessageTypeTerminalResize, protocol.MessageTypeTerminalClose:
			if terminalHandler == nil {
				continue
			}
			var message protocol.TerminalMessage
			if err := json.Unmarshal(raw, &message); err != nil {
				continue
			}
			terminalHandler.Handle(message)
		case protocol.MessageTypeContainerExecStart, protocol.MessageTypeContainerExecData, protocol.MessageTypeContainerExecResize, protocol.MessageTypeContainerExecClose:
			if containerExecHandler == nil {
				continue
			}
			var message protocol.ContainerExecMessage
			if err := json.Unmarshal(raw, &message); err != nil {
				continue
			}
			containerExecHandler.Handle(message)
		case protocol.MessageTypeLogTailRequest:
			if c.logTailHandler == nil {
				continue
			}
			var request protocol.LogTailRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go c.logTailHandler.Handle(ctx, request, writer)
		case protocol.MessageTypeLogTailStop:
			if c.logTailHandler == nil {
				continue
			}
			var message protocol.LogTailStop
			if err := json.Unmarshal(raw, &message); err != nil {
				continue
			}
			c.logTailHandler.Stop(message.SessionID)
		case protocol.MessageTypeContainerLogsRequest:
			if c.containerLogsHandler == nil {
				continue
			}
			var request protocol.ContainerLogsRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go c.containerLogsHandler.Handle(ctx, request, writer)
		case protocol.MessageTypeContainerLogsStop:
			if c.containerLogsHandler == nil {
				continue
			}
			var message protocol.ContainerLogsStop
			if err := json.Unmarshal(raw, &message); err != nil {
				continue
			}
			c.containerLogsHandler.Stop(message.SessionID)
		case protocol.MessageTypeDockerExecRequest:
			if c.dockerExecHandler == nil {
				continue
			}
			var request protocol.DockerExecRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.dockerExecHandler.HandleDockerExec(ctx, request)
			response.RequestID = request.RequestID
			if err := writer.writeJSON(response); err != nil {
				continue
			}
		case protocol.MessageTypeContainerStartRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.ContainerStartRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.containerOpsHandler.HandleContainerStart(ctx, request)
			response.RequestID = request.RequestID
			if err := writer.writeJSON(response); err != nil {
				continue
			}
		case protocol.MessageTypeContainerStopRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.ContainerStopRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.containerOpsHandler.HandleContainerStop(ctx, request)
			response.RequestID = request.RequestID
			if err := writer.writeJSON(response); err != nil {
				continue
			}
		case protocol.MessageTypeContainerRestartRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.ContainerRestartRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.containerOpsHandler.HandleContainerRestart(ctx, request)
			response.RequestID = request.RequestID
			if err := writer.writeJSON(response); err != nil {
				continue
			}
		case protocol.MessageTypeContainerDeleteRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.ContainerDeleteRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.containerOpsHandler.HandleContainerDelete(ctx, request)
			response.RequestID = request.RequestID
			if err := writer.writeJSON(response); err != nil {
				continue
			}
		case protocol.MessageTypeDockerContainerCreateRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.DockerContainerCreateRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			response := c.containerOpsHandler.HandleDockerContainerCreate(ctx, request)
			response.RequestID = request.RequestID
			if err := writer.writeJSON(response); err != nil {
				continue
			}
		case protocol.MessageTypeDockerComposeListRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.DockerComposeListRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go func() {
				response := c.containerOpsHandler.HandleDockerComposeList(ctx, request)
				response.RequestID = request.RequestID
				_ = writer.writeJSON(response)
			}()
		case protocol.MessageTypeDockerComposeActionRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.DockerComposeActionRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go func() {
				response := c.containerOpsHandler.HandleDockerComposeAction(ctx, request)
				response.RequestID = request.RequestID
				_ = writer.writeJSON(response)
			}()
		case protocol.MessageTypeDockerComposeDeploymentRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.DockerComposeDeploymentRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go func() {
				response := c.containerOpsHandler.HandleDockerComposeDeployment(ctx, request)
				response.RequestID = request.RequestID
				_ = writer.writeJSON(response)
			}()
		case protocol.MessageTypeDockerResourceListRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.DockerResourceListRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go func() {
				response := c.containerOpsHandler.HandleDockerResourceList(ctx, request)
				response.RequestID = request.RequestID
				_ = writer.writeJSON(response)
			}()
		case protocol.MessageTypeDockerResourceActionRequest:
			if c.containerOpsHandler == nil {
				continue
			}
			var request protocol.DockerResourceActionRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go func() {
				response := c.containerOpsHandler.HandleDockerResourceAction(ctx, request)
				response.RequestID = request.RequestID
				_ = writer.writeJSON(response)
			}()
		case protocol.MessageTypeSystemdServiceListRequest:
			if c.systemdServiceHandler == nil {
				continue
			}
			var request protocol.SystemdServiceListRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go func() {
				response := c.systemdServiceHandler.HandleList(ctx, request)
				response.RequestID = request.RequestID
				_ = writer.writeJSON(response)
			}()
		case protocol.MessageTypeSystemdServiceActionRequest:
			if c.systemdServiceHandler == nil {
				continue
			}
			var request protocol.SystemdServiceActionRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			go func() {
				response := c.systemdServiceHandler.HandleAction(ctx, request)
				response.RequestID = request.RequestID
				_ = writer.writeJSON(response)
			}()
		case protocol.MessageTypeScriptExecutionRequest:
			var request protocol.ScriptExecutionRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				continue
			}
			taskExecutions.Add(1)
			go func() {
				defer taskExecutions.Done()
				response := protocol.ScriptExecutionResponse{
					Status: protocol.ScriptExecutionStatusUnsupported,
					Error:  "script execution is not supported on this Agent",
					Output: "",
				}
				if c.taskRunner != nil {
					response = c.taskRunner.Run(ctx, request)
				}
				response.Type = protocol.MessageTypeScriptExecutionResponse
				response.RequestID = request.RequestID
				response.ExecutionID = request.ExecutionID
				_ = writer.writeJSON(response)
			}()
		case protocol.MessageTypeK8sClusterConnect,
			protocol.MessageTypeK8sGetSummary,
			protocol.MessageTypeK8sGetNamespaces,
			protocol.MessageTypeK8sGetNodes,
			protocol.MessageTypeK8sGetPods,
			protocol.MessageTypeK8sGetDeployments,
			protocol.MessageTypeK8sGetStatefulSets,
			protocol.MessageTypeK8sGetDaemonSets,
			protocol.MessageTypeK8sGetServices,
			protocol.MessageTypeK8sGetIngresses,
			protocol.MessageTypeK8sGetDiagnostics,
			protocol.MessageTypeK8sResourceAction,
			protocol.MessageTypeK8sApplyManifest,
			protocol.MessageTypeK8sGetPodLogs:
			if c.kubectlHandler == nil {
				if c.debug {
					log.Printf("[debug][agent][k8s] handler missing type=%s request_id=%s cluster_id=%s namespace=%s", header.Type, header.RequestID, header.ClusterID, header.Namespace)
				}
				continue
			}
			if c.debug {
				log.Printf("[debug][agent][k8s] dispatch type=%s request_id=%s cluster_id=%s namespace=%s", header.Type, header.RequestID, header.ClusterID, header.Namespace)
			}
			requestCtx, cancel := context.WithTimeout(ctx, k8sRequestTimeout(header.Type))
			if err := c.kubectlHandler.Handle(requestCtx, header.Type, raw, writer.writeJSON); err != nil {
				cancel()
				if c.debug {
					log.Printf("[debug][agent][k8s] dispatch failed type=%s request_id=%s cluster_id=%s namespace=%s error=%v", header.Type, header.RequestID, header.ClusterID, header.Namespace, err)
				}
				continue
			}
			cancel()
		}
	}
}

func k8sRequestTimeout(msgType string) time.Duration {
	if msgType == protocol.MessageTypeK8sClusterConnect || msgType == protocol.MessageTypeK8sGetPodLogs {
		return 10 * time.Second
	}
	if msgType == protocol.MessageTypeK8sApplyManifest {
		return 30 * time.Second
	}
	return 15 * time.Second
}

func (c *Client) writeCollectedMetric(writer *connectionWriter, nodeID string, collect CollectFunc) error {
	message, err := collect(nodeID, time.Now().Unix())
	if err != nil {
		return err
	}
	message.NodeID = nodeID
	message.Type = protocol.MessageTypeMetrics
	return writer.writeJSON(message)
}

func (w *connectionWriter) SendTerminal(message protocol.TerminalMessage) error {
	return w.writeJSON(message)
}

func (w *connectionWriter) SendContainerExec(message protocol.ContainerExecMessage) error {
	return w.writeJSON(message)
}

func (w *connectionWriter) SendLogTail(message interface{}) error {
	return w.writeJSON(message)
}

func (w *connectionWriter) SendContainerLogs(message interface{}) error {
	return w.writeJSON(message)
}

func (w *connectionWriter) writeJSON(value any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(value)
}
