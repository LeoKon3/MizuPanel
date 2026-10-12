package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type rpcDelivery struct {
	raw json.RawMessage
	err error
}

var errAgentRPCDisconnected = errors.New("agent RPC connection closed")

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
	trySend(takePending(&c.pendingMu, &c.pendingLists, response.RequestID), response)
}

func (c *agentConnection) deliverFileRead(response protocol.FileReadResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingReads, response.RequestID), response)
}

func (c *agentConnection) deliverFileWrite(response protocol.FileWriteResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingWrites, response.RequestID), response)
}

func (c *agentConnection) deliverFileUpload(response protocol.FileUploadResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingUploads, response.RequestID), response)
}

func (c *agentConnection) deliverFileDelete(response protocol.FileDeleteResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingDeletes, response.RequestID), response)
}

func (c *agentConnection) deliverReboot(response protocol.RebootResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingReboots, response.RequestID), response)
}

func (c *agentConnection) deliverAgentStatus(response protocol.AgentStatusResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingAgentStatuses, response.RequestID), response)
}

func (c *agentConnection) deliverAgentRestart(response protocol.AgentRestartResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingAgentRestarts, response.RequestID), response)
}

func (c *agentConnection) deliverAgentLogs(response protocol.AgentLogsResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingAgentLogs, response.RequestID), response)
}

func (c *agentConnection) deliverAgentUpgrade(response protocol.AgentUpgradeResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingAgentUpgrades, response.RequestID), response)
}

func (c *agentConnection) deliverDockerExec(response protocol.DockerExecResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingDockerExecs, response.RequestID), response)
}

func (c *agentConnection) deliverContainerStart(response protocol.ContainerStartResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingContainerStarts, response.RequestID), response)
}

func (c *agentConnection) deliverContainerStop(response protocol.ContainerStopResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingContainerStops, response.RequestID), response)
}

func (c *agentConnection) deliverContainerRestart(response protocol.ContainerRestartResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingContainerRestarts, response.RequestID), response)
}

func (c *agentConnection) deliverContainerDelete(response protocol.ContainerDeleteResponse) {
	trySend(takePending(&c.pendingMu, &c.pendingContainerDeletes, response.RequestID), response)
}

func takePending[K comparable, V any](mu *sync.Mutex, pending *map[K]chan V, key K) chan V {
	mu.Lock()
	defer mu.Unlock()
	ch := (*pending)[key]
	if ch != nil {
		delete(*pending, key)
	}
	return ch
}

func trySend[T any](ch chan T, value T) bool {
	if ch == nil {
		return false
	}
	select {
	case ch <- value:
		return true
	default:
		return false
	}
}

func (c *agentConnection) deliverK8sMessage(requestID string, rawMessage json.RawMessage) {
	rawMessage = append(json.RawMessage(nil), rawMessage...)
	trySend(takePending(&c.pendingMu, &c.pendingK8sMessages, requestID), rawMessage)
}

func (c *agentConnection) callRPC(ctx context.Context, requestID string, request any, response any) error {
	ch := make(chan rpcDelivery, 1)
	c.pendingMu.Lock()
	if c.closed != nil {
		select {
		case <-c.closed:
			c.pendingMu.Unlock()
			return errAgentRPCDisconnected
		default:
		}
	}
	if c.pendingRPCs == nil {
		c.pendingRPCs = make(map[string]chan rpcDelivery)
	}
	if _, exists := c.pendingRPCs[requestID]; exists {
		c.pendingMu.Unlock()
		return errors.New("duplicate agent RPC request ID")
	}
	c.pendingRPCs[requestID] = ch
	c.pendingMu.Unlock()
	defer c.removeRPC(requestID, ch)

	if err := c.writeJSON(request); err != nil {
		return err
	}
	select {
	case delivery := <-ch:
		if delivery.err != nil {
			return delivery.err
		}
		if err := json.Unmarshal(delivery.raw, response); err != nil {
			return errors.New("invalid agent RPC response")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *agentConnection) removeRPC(requestID string, ch chan rpcDelivery) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if c.pendingRPCs[requestID] == ch {
		delete(c.pendingRPCs, requestID)
	}
}

func (c *agentConnection) deliverRPC(requestID string, rawMessage json.RawMessage) {
	ch := takePending(&c.pendingMu, &c.pendingRPCs, requestID)
	delivery := rpcDelivery{raw: append(json.RawMessage(nil), rawMessage...)}
	trySend(ch, delivery)
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
	rpcs := c.pendingRPCs
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
	c.pendingRPCs = make(map[string]chan rpcDelivery)
	if c.closed != nil {
		c.closeOnce.Do(func() { close(c.closed) })
	}
	c.pendingMu.Unlock()
	for _, ch := range rpcs {
		trySend(ch, rpcDelivery{err: errAgentRPCDisconnected})
	}
	for requestID, ch := range lists {
		trySend(ch, protocol.FileListResponse{Type: protocol.MessageTypeFileListResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range reads {
		trySend(ch, protocol.FileReadResponse{Type: protocol.MessageTypeFileReadResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range writes {
		trySend(ch, protocol.FileWriteResponse{Type: protocol.MessageTypeFileWriteResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range uploads {
		trySend(ch, protocol.FileUploadResponse{Type: protocol.MessageTypeFileUploadResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range deletes {
		trySend(ch, protocol.FileDeleteResponse{Type: protocol.MessageTypeFileDeleteResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range reboots {
		trySend(ch, protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range agentStatuses {
		trySend(ch, protocol.AgentStatusResponse{Type: protocol.MessageTypeAgentStatusResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range agentRestarts {
		trySend(ch, protocol.AgentRestartResponse{Type: protocol.MessageTypeAgentRestartResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range agentLogs {
		trySend(ch, protocol.AgentLogsResponse{Type: protocol.MessageTypeAgentLogsResponse, RequestID: requestID, Code: "offline", Error: reason})
	}
	for requestID, ch := range agentUpgrades {
		trySend(ch, protocol.AgentUpgradeResponse{Type: protocol.MessageTypeAgentUpgradeResponse, RequestID: requestID, Stage: "failed", Code: "offline", Error: reason})
	}
	for _, ch := range dockerExecs {
		trySend(ch, protocol.DockerExecResponse{Type: protocol.MessageTypeDockerExecResponse, Accepted: false, ExitCode: 1, Error: reason})
	}
	for _, ch := range containerStarts {
		trySend(ch, protocol.ContainerStartResponse{Type: protocol.MessageTypeContainerStartResponse, Success: false, Error: reason})
	}
	for _, ch := range containerStops {
		trySend(ch, protocol.ContainerStopResponse{Type: protocol.MessageTypeContainerStopResponse, Success: false, Error: reason})
	}
	for _, ch := range containerRestarts {
		trySend(ch, protocol.ContainerRestartResponse{Type: protocol.MessageTypeContainerRestartResponse, Success: false, Error: reason})
	}
	for _, ch := range containerDeletes {
		trySend(ch, protocol.ContainerDeleteResponse{Type: protocol.MessageTypeContainerDeleteResponse, Success: false, Error: reason})
	}
	for requestID, ch := range k8sMessages {
		response := protocol.K8sGetSummaryResult{Type: protocol.MessageTypeK8sGetSummaryResult, RequestID: requestID, Success: false, Error: reason}
		data, err := json.Marshal(response)
		if err != nil {
			continue
		}
		trySend(ch, data)
	}
}
