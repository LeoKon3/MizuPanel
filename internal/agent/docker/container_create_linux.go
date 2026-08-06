//go:build linux

package docker

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func (c *socketClient) CreateContainer(ctx context.Context, request protocol.DockerContainerCreateRequest) (containerCreateResult, error) {
	image := strings.TrimSpace(request.Image)
	if image == "" || len(image) > 256 || strings.ContainsAny(image, " \t\r\n;|&$<>\"'`\x00") {
		return containerCreateResult{}, errors.New("Docker image is required")
	}
	if request.Name != "" && !validContainerName(request.Name) {
		return containerCreateResult{}, errors.New("invalid Docker container name")
	}
	if request.RestartPolicy == "" {
		request.RestartPolicy = "no"
	}
	if request.NetworkMode == "" {
		request.NetworkMode = "bridge"
	}
	if request.RestartPolicy != "no" && request.RestartPolicy != "always" && request.RestartPolicy != "on-failure" && request.RestartPolicy != "unless-stopped" {
		return containerCreateResult{}, errors.New("invalid Docker restart policy")
	}
	if request.NetworkMode != "bridge" && request.NetworkMode != "host" && request.NetworkMode != "none" {
		return containerCreateResult{}, errors.New("invalid Docker network mode")
	}
	exposed := make(map[string]struct{}, len(request.Ports))
	bindings := make(map[string][]map[string]string, len(request.Ports))
	for _, port := range request.Ports {
		if port.HostPort == 0 || port.ContainerPort == 0 || (port.Protocol != "tcp" && port.Protocol != "udp") {
			return containerCreateResult{}, errors.New("invalid Docker port mapping")
		}
		key := fmt.Sprintf("%d/%s", port.ContainerPort, port.Protocol)
		exposed[key] = struct{}{}
		bindings[key] = append(bindings[key], map[string]string{"HostPort": fmt.Sprintf("%d", port.HostPort)})
	}
	payload := map[string]any{
		"Image":        image,
		"ExposedPorts": exposed,
		"HostConfig": map[string]any{
			"RestartPolicy": map[string]string{"Name": request.RestartPolicy},
			"NetworkMode":   request.NetworkMode,
			"PortBindings":  bindings,
		},
	}
	path := "/containers/create"
	if strings.TrimSpace(request.Name) != "" {
		path += "?name=" + url.QueryEscape(strings.TrimSpace(request.Name))
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.postJSON(ctx, path, payload, &created); err != nil {
		return containerCreateResult{}, err
	}
	if strings.TrimSpace(created.ID) == "" {
		return containerCreateResult{}, errors.New("Docker create returned empty id")
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		inspect, err := c.InspectContainer(ctx, created.ID)
		if err != nil {
			return containerCreateResult{ID: created.ID}, fmt.Errorf("Docker container created but name lookup failed: %w", err)
		}
		name = strings.TrimPrefix(strings.TrimSpace(inspect.Name), "/")
	}
	if request.Start {
		if err := c.postJSON(ctx, "/containers/"+created.ID+"/start", nil, nil); err != nil {
			return containerCreateResult{ID: created.ID, Name: name}, fmt.Errorf("Docker container created but start failed: %w", err)
		}
	}
	return containerCreateResult{ID: created.ID, Name: name}, nil
}

func validContainerName(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if index == 0 {
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')) {
				return false
			}
			continue
		}
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '.' || character == '-') {
			return false
		}
	}
	return true
}
