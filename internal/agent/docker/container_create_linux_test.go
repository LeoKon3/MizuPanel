//go:build linux

package docker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type dockerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f dockerRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCreateContainerUsesDockerEngineCreateAndOptionalStart(t *testing.T) {
	var paths []string
	client := &socketClient{
		httpClient: &http.Client{Transport: dockerRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			paths = append(paths, request.URL.Path)
			switch request.URL.Path {
			case "/containers/create":
				if request.Method != http.MethodPost {
					t.Fatalf("create method = %s, want POST", request.Method)
				}
				body, _ := io.ReadAll(request.Body)
				if !strings.Contains(string(body), `"Image":"nginx:1.27"`) || !strings.Contains(string(body), `"NetworkMode":"bridge"`) {
					t.Fatalf("Docker create payload = %s", body)
				}
				return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"Id":"container-1"}`)), Header: make(http.Header)}, nil
			case "/containers/container-1/json":
				if request.Method != http.MethodGet {
					t.Fatalf("inspect method = %s, want GET", request.Method)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Name":"/auto-web"}`)), Header: make(http.Header)}, nil
			case "/containers/container-1/start":
				if request.Method != http.MethodPost {
					t.Fatalf("start method = %s, want POST", request.Method)
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			default:
				t.Fatalf("unexpected Docker API path %q", request.URL.Path)
				return nil, nil
			}
		})},
		baseURL: "http://docker",
	}

	id, err := client.CreateContainer(context.Background(), protocol.DockerContainerCreateRequest{
		Image: "nginx:1.27", RestartPolicy: "no", NetworkMode: "bridge", Start: true,
		Ports: []protocol.DockerContainerPort{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
	})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if id.ID != "container-1" || id.Name != "auto-web" || strings.Join(paths, ",") != "/containers/create,/containers/container-1/json,/containers/container-1/start" {
		t.Fatalf("result/paths = %+v/%v", id, paths)
	}
}

func TestCreateContainerRejectsUnsafeConfigurationBeforeRequest(t *testing.T) {
	called := false
	client := &socketClient{
		httpClient: &http.Client{Transport: dockerRoundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		})},
		baseURL: "http://docker",
	}
	_, err := client.CreateContainer(context.Background(), protocol.DockerContainerCreateRequest{
		Image: "nginx;id", RestartPolicy: "no", NetworkMode: "bridge",
	})
	if err == nil || called {
		t.Fatalf("unsafe image error/call = %v/%t", err, called)
	}
}

func TestHandleDockerContainerCreatePreservesCreatedIdentityWhenStartFails(t *testing.T) {
	client := &socketClient{
		httpClient: &http.Client{Transport: dockerRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/containers/create":
				return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"Id":"container-1"}`)), Header: make(http.Header)}, nil
			case "/containers/container-1/start":
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(`{"message":"port is already allocated"}`)), Header: make(http.Header)}, nil
			default:
				t.Fatalf("unexpected Docker API path %q", request.URL.Path)
				return nil, nil
			}
		})},
		baseURL: "http://docker",
	}
	response := NewOperationsHandler(&Collector{client: client}).HandleDockerContainerCreate(t.Context(), protocol.DockerContainerCreateRequest{
		RequestID: "request-1", Image: "nginx:1.27", Name: "web", Start: true,
	})
	if response.Success || !response.Supported || !response.Created || response.ContainerID != "container-1" || response.Name != "web" || response.Started || response.Error == "" {
		t.Fatalf("partial Docker create response = %#v", response)
	}
}
