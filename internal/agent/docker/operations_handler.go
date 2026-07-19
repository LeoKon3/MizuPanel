package docker

import (
	"context"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

// OperationsHandler handles container operation requests
type OperationsHandler struct {
	collector *Collector
	compose   *ComposeHandler
}

// NewOperationsHandler creates a new container operations handler
func NewOperationsHandler(collector *Collector) *OperationsHandler {
	return NewOperationsHandlerWithCompose(collector, NewComposeHandler())
}

func NewOperationsHandlerWithCompose(collector *Collector, compose *ComposeHandler) *OperationsHandler {
	return &OperationsHandler{collector: collector, compose: compose}
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
