package docker

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

// OperationsHandler handles container operation requests
type OperationsHandler struct {
	collector       *Collector
	compose         *ComposeHandler
	resourceMu      sync.Mutex
	activeResources map[string]bool
}

// NewOperationsHandler creates a new container operations handler
func NewOperationsHandler(collector *Collector) *OperationsHandler {
	return NewOperationsHandlerWithCompose(collector, NewComposeHandler())
}

func NewOperationsHandlerWithCompose(collector *Collector, compose *ComposeHandler) *OperationsHandler {
	return &OperationsHandler{collector: collector, compose: compose, activeResources: make(map[string]bool)}
}

func (h *OperationsHandler) HandleDockerResourceList(ctx context.Context, req protocol.DockerResourceListRequest) protocol.DockerResourceListResponse {
	response := emptyDockerResourceListResponse()
	response.RequestID = req.RequestID
	if h.collector == nil {
		response.Supported = false
		response.Error = "Docker 资源管理未启用"
		return response
	}
	requestCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	response, err := h.collector.DockerResources(requestCtx)
	response.RequestID = req.RequestID
	if err != nil {
		response.Error = err.Error()
		return response
	}
	return response
}

func (h *OperationsHandler) HandleDockerResourceAction(ctx context.Context, req protocol.DockerResourceActionRequest) protocol.DockerResourceActionResponse {
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	req.ResourceID = strings.TrimSpace(req.ResourceID)
	req.Action = strings.TrimSpace(req.Action)
	response := protocol.DockerResourceActionResponse{
		Type:         protocol.MessageTypeDockerResourceActionResponse,
		RequestID:    req.RequestID,
		Supported:    h.collector != nil,
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		Action:       req.Action,
	}
	if h.collector == nil {
		response.Error = "Docker 资源管理未启用"
		return response
	}
	if err := validateDockerResourceAction(req.ResourceType, req.ResourceID, req.Action); err != nil {
		response.Error = err.Error()
		return response
	}
	operationKey := req.ResourceType + ":" + req.ResourceID
	if !h.beginDockerResourceAction(operationKey) {
		response.Error = "该 Docker 资源正在执行其他操作"
		return response
	}
	defer h.finishDockerResourceAction(operationKey)
	timeout := 90 * time.Second
	if req.ResourceType == "image" && req.Action == "pull" {
		timeout = 6 * time.Minute
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := h.collector.DockerResourceAction(requestCtx, req.ResourceType, req.ResourceID, req.Action); err != nil {
		if dockerResourceAPIUnsupported(err) {
			response.Supported = false
		}
		response.Error = err.Error()
		return response
	}
	response.Success = true
	return response
}

func (h *OperationsHandler) beginDockerResourceAction(key string) bool {
	h.resourceMu.Lock()
	defer h.resourceMu.Unlock()
	if h.activeResources == nil {
		h.activeResources = make(map[string]bool)
	}
	if h.activeResources[key] {
		return false
	}
	h.activeResources[key] = true
	return true
}

func (h *OperationsHandler) finishDockerResourceAction(key string) {
	h.resourceMu.Lock()
	delete(h.activeResources, key)
	h.resourceMu.Unlock()
}

func (h *OperationsHandler) HandleDockerComposeList(ctx context.Context, req protocol.DockerComposeListRequest) protocol.DockerComposeListResponse {
	if h.compose == nil {
		return protocol.DockerComposeListResponse{Type: protocol.MessageTypeDockerComposeListResponse, RequestID: req.RequestID, Error: "Docker Compose CLI 不可用"}
	}
	return h.compose.HandleDockerComposeList(ctx, req)
}

func (h *OperationsHandler) HandleDockerComposeAction(ctx context.Context, req protocol.DockerComposeActionRequest) protocol.DockerComposeActionResponse {
	if h.compose == nil {
		return protocol.DockerComposeActionResponse{Type: protocol.MessageTypeDockerComposeActionResponse, RequestID: req.RequestID, Error: "Docker Compose CLI 不可用"}
	}
	return h.compose.HandleDockerComposeAction(ctx, req)
}

func (h *OperationsHandler) HandleContainerStart(ctx context.Context, req protocol.ContainerStartRequest) protocol.ContainerStartResponse {
	err := h.collector.ContainerStart(ctx, req.ContainerID)
	if err != nil {
		return protocol.ContainerStartResponse{
			Type:    protocol.MessageTypeContainerStartResponse,
			Success: false,
			Error:   err.Error(),
		}
	}
	return protocol.ContainerStartResponse{
		Type:    protocol.MessageTypeContainerStartResponse,
		Success: true,
	}
}

func (h *OperationsHandler) HandleContainerStop(ctx context.Context, req protocol.ContainerStopRequest) protocol.ContainerStopResponse {
	err := h.collector.ContainerStop(ctx, req.ContainerID)
	if err != nil {
		return protocol.ContainerStopResponse{
			Type:    protocol.MessageTypeContainerStopResponse,
			Success: false,
			Error:   err.Error(),
		}
	}
	return protocol.ContainerStopResponse{
		Type:    protocol.MessageTypeContainerStopResponse,
		Success: true,
	}
}

func (h *OperationsHandler) HandleContainerRestart(ctx context.Context, req protocol.ContainerRestartRequest) protocol.ContainerRestartResponse {
	err := h.collector.ContainerRestart(ctx, req.ContainerID)
	if err != nil {
		return protocol.ContainerRestartResponse{
			Type:    protocol.MessageTypeContainerRestartResponse,
			Success: false,
			Error:   err.Error(),
		}
	}
	return protocol.ContainerRestartResponse{
		Type:    protocol.MessageTypeContainerRestartResponse,
		Success: true,
	}
}

func (h *OperationsHandler) HandleContainerDelete(ctx context.Context, req protocol.ContainerDeleteRequest) protocol.ContainerDeleteResponse {
	err := h.collector.ContainerDelete(ctx, req.ContainerID, req.Force)
	if err != nil {
		return protocol.ContainerDeleteResponse{
			Type:    protocol.MessageTypeContainerDeleteResponse,
			Success: false,
			Error:   err.Error(),
		}
	}
	return protocol.ContainerDeleteResponse{
		Type:    protocol.MessageTypeContainerDeleteResponse,
		Success: true,
	}
}
