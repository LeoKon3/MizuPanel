package docker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type fakeResourceClient struct {
	usage          dockerDiskUsageResponse
	usageErr       error
	networks       []dockerNetworkListItem
	networksErr    error
	pulled         string
	removedImage   string
	removedVolume  string
	removedNetwork string
}

func (f *fakeResourceClient) DiskUsage(context.Context) (dockerDiskUsageResponse, error) {
	return f.usage, f.usageErr
}

func (f *fakeResourceClient) ListNetworks(context.Context) ([]dockerNetworkListItem, error) {
	return f.networks, f.networksErr
}

func (f *fakeResourceClient) PullImage(_ context.Context, reference string) error {
	f.pulled = reference
	return nil
}

func (f *fakeResourceClient) RemoveImage(_ context.Context, id string) error {
	f.removedImage = id
	return nil
}

func (f *fakeResourceClient) RemoveVolume(_ context.Context, name string) error {
	f.removedVolume = name
	return nil
}

func (f *fakeResourceClient) RemoveNetwork(_ context.Context, id string) error {
	f.removedNetwork = id
	return nil
}

func TestDockerResourcesMapsUsageAndProtectsSystemNetworks(t *testing.T) {
	layers := int64(100)
	imageSize := int64(60)
	sharedUnknown := int64(-1)
	imageRefs := int64(0)
	containerSize := int64(12)
	volumeSize := int64(24)
	volumeRefs := int64(0)
	cacheSize := int64(8)
	images := []dockerImageUsage{{ID: "sha256:1234567890abcdef", RepoTags: []string{"nginx:latest"}, Size: &imageSize, SharedSize: &sharedUnknown, Containers: &imageRefs}}
	containers := []dockerContainerUsage{{SizeRW: &containerSize}, {SizeRW: nil}}
	volumes := []dockerVolumeUsage{{Name: "demo_data", Driver: "local", Scope: "local", Labels: map[string]string{dockerComposeProjectLabel: "demo"}, UsageData: &dockerVolumeUsageData{Size: &volumeSize, RefCount: &volumeRefs}}}
	cache := []dockerBuildCacheUsage{{Size: &cacheSize}}
	client := &fakeResourceClient{
		usage: dockerDiskUsageResponse{LayersSize: &layers, Images: &images, Containers: &containers, Volumes: &volumes, BuildCache: &cache},
		networks: []dockerNetworkListItem{
			{Name: "bridge", ID: "bridge-id", Driver: "bridge"},
			{Name: "demo_default", ID: "abcdef1234567890", Driver: "bridge", IPAM: dockerNetworkIPAM{Config: []dockerNetworkIPAMConfig{{Subnet: "172.20.0.0/16"}}}, Containers: map[string]dockerNetworkContainer{"container-id": {Name: "demo-web"}}},
		},
	}
	collector := &Collector{resourceClient: client}

	response, err := collector.DockerResources(t.Context())
	if err != nil {
		t.Fatalf("DockerResources returned error: %v", err)
	}
	if !response.Success || !response.Supported || len(response.Images) != 1 || len(response.Volumes) != 1 || len(response.Networks) != 2 {
		t.Fatalf("unexpected response: %#v", response)
	}
	if response.Usage.ImageLayers == nil || *response.Usage.ImageLayers != 100 || response.Usage.ContainerWritable != nil || response.Usage.Volumes == nil || *response.Usage.Volumes != 24 || response.Usage.BuildCache == nil || *response.Usage.BuildCache != 8 {
		t.Fatalf("unexpected usage: %#v", response.Usage)
	}
	if response.Images[0].ID != "1234567890ab" || response.Images[0].SharedSize != nil {
		t.Fatalf("unexpected image mapping: %#v", response.Images[0])
	}
	if response.Volumes[0].ComposeProject != "demo" || response.Volumes[0].RefCount == nil || *response.Volumes[0].RefCount != 0 {
		t.Fatalf("unexpected volume mapping: %#v", response.Volumes[0])
	}
	if !response.Networks[0].Protected || response.Networks[1].Protected || len(response.Networks[1].Containers) != 1 {
		t.Fatalf("unexpected network mapping: %#v", response.Networks)
	}
}

func TestDockerResourcesReportsUnsupportedEngineAPI(t *testing.T) {
	collector := &Collector{resourceClient: &fakeResourceClient{usageErr: &dockerAPIStatusError{status: http.StatusNotFound}}}
	response, err := collector.DockerResources(t.Context())
	if err == nil || response.Supported || response.Success || response.Images == nil || response.Volumes == nil || response.Networks == nil {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestDockerResourceActionRechecksReferencesAndProtectedNetworks(t *testing.T) {
	imageRefs := int64(1)
	volumeRefs := int64(1)
	images := []dockerImageUsage{{ID: "sha256:image-id", RepoTags: []string{"demo:latest"}, Containers: &imageRefs}}
	volumes := []dockerVolumeUsage{{Name: "demo_data", UsageData: &dockerVolumeUsageData{RefCount: &volumeRefs}}}
	client := &fakeResourceClient{
		usage:    dockerDiskUsageResponse{Images: &images, Volumes: &volumes},
		networks: []dockerNetworkListItem{{Name: "bridge", ID: "bridge-id"}, {Name: "demo", ID: "network-id", Containers: map[string]dockerNetworkContainer{"container-id": {Name: "demo"}}}},
	}
	collector := &Collector{resourceClient: client}

	for _, test := range []struct {
		resourceType string
		resourceID   string
		action       string
	}{
		{resourceType: "image", resourceID: "image-id", action: "remove"},
		{resourceType: "volume", resourceID: "demo_data", action: "remove"},
		{resourceType: "network", resourceID: "bridge", action: "remove"},
		{resourceType: "network", resourceID: "demo", action: "remove"},
		{resourceType: "network", resourceID: "demo", action: "prune"},
	} {
		if err := collector.DockerResourceAction(t.Context(), test.resourceType, test.resourceID, test.action); err == nil {
			t.Fatalf("action %#v unexpectedly succeeded", test)
		}
	}
	if client.removedImage != "" || client.removedVolume != "" || client.removedNetwork != "" {
		t.Fatalf("unsafe resource was removed: %#v", client)
	}
}

func TestDockerResourceActionUsesFixedSafeOperations(t *testing.T) {
	zero := int64(0)
	images := []dockerImageUsage{{ID: "sha256:image-id", Containers: &zero}}
	volumes := []dockerVolumeUsage{{Name: "demo_data", UsageData: &dockerVolumeUsageData{RefCount: &zero}}}
	client := &fakeResourceClient{
		usage:    dockerDiskUsageResponse{Images: &images, Volumes: &volumes},
		networks: []dockerNetworkListItem{{Name: "demo", ID: "network-id"}},
	}
	collector := &Collector{resourceClient: client}

	if err := collector.DockerResourceAction(t.Context(), "image", "nginx:1.27", "pull"); err != nil || client.pulled != "nginx:1.27" {
		t.Fatalf("pull error = %v, reference = %q", err, client.pulled)
	}
	if err := collector.DockerResourceAction(t.Context(), "image", "image-id", "remove"); err != nil || client.removedImage != "sha256:image-id" {
		t.Fatalf("remove image error = %v, id = %q", err, client.removedImage)
	}
	if err := collector.DockerResourceAction(t.Context(), "volume", "demo_data", "remove"); err != nil || client.removedVolume != "demo_data" {
		t.Fatalf("remove volume error = %v, name = %q", err, client.removedVolume)
	}
	if err := collector.DockerResourceAction(t.Context(), "network", "demo", "remove"); err != nil || client.removedNetwork != "network-id" {
		t.Fatalf("remove network error = %v, id = %q", err, client.removedNetwork)
	}
	for _, reference := range []string{"https://registry.invalid/image", "nginx:", "repo//image", "image@@digest", "UPPERCASE/image", "nginx@sha256:not-a-digest"} {
		if err := collector.DockerResourceAction(t.Context(), "image", reference, "pull"); err == nil {
			t.Fatalf("invalid image reference %q unexpectedly accepted", reference)
		}
	}
}

func TestDockerResourceActionMarksUnsupportedEngine(t *testing.T) {
	client := &fakeResourceClient{usageErr: &dockerAPIStatusError{status: http.StatusNotImplemented}}
	handler := NewOperationsHandler(&Collector{resourceClient: client})
	response := handler.HandleDockerResourceAction(t.Context(), protocol.DockerResourceActionRequest{RequestID: "request-1", ResourceType: "image", ResourceID: "image-id", Action: "remove"})
	if response.Success || response.Supported || response.Error == "" || response.RequestID != "request-1" {
		t.Fatalf("response = %#v", response)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSocketClientPullImageEncodesReferenceAndReadsStreamErrors(t *testing.T) {
	var requested *http.Request
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requested = request
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{\"status\":\"Pulling\"}\n{\"errorDetail\":{\"message\":\"registry denied\"}}\n")), Header: make(http.Header)}, nil
	})
	client := &socketClient{httpClientNoTimeout: &http.Client{Transport: transport}, baseURL: "http://docker"}

	err := client.PullImage(t.Context(), "registry.example:5000/team/app:1.0")
	if err == nil || !strings.Contains(err.Error(), "registry denied") {
		t.Fatalf("PullImage error = %v", err)
	}
	if requested == nil || requested.Method != http.MethodPost || requested.URL.Query().Get("fromImage") != "registry.example:5000/team/app:1.0" {
		t.Fatalf("unexpected request: %#v", requested)
	}
}

func TestSocketClientListNetworksInspectsContainerAttachments(t *testing.T) {
	requests := make([]string, 0, 2)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.EscapedPath())
		var body string
		switch request.URL.EscapedPath() {
		case "/networks":
			body = `[{"Name":"demo","Id":"network-id","Driver":"bridge"}]`
		case "/networks/network-id":
			body = `{"Name":"demo","Id":"network-id","Driver":"bridge","Containers":{"container-id":{"Name":"demo-web"}}}`
		default:
			t.Fatalf("unexpected Docker request path %q", request.URL.EscapedPath())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	client := &socketClient{httpClientNoTimeout: &http.Client{Transport: transport}, baseURL: "http://docker"}

	networks, err := client.ListNetworks(t.Context())
	if err != nil {
		t.Fatalf("ListNetworks returned error: %v", err)
	}
	if len(networks) != 1 || len(networks[0].Containers) != 1 || networks[0].Containers["container-id"].Name != "demo-web" {
		t.Fatalf("unexpected networks: %#v", networks)
	}
	if strings.Join(requests, ",") != "/networks,/networks/network-id" {
		t.Fatalf("requests = %v", requests)
	}
}
