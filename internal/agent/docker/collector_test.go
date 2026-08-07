package docker

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func TestSupportsContainerCreateRequiresReachableDockerEngine(t *testing.T) {
	t.Run("injected create client", func(t *testing.T) {
		collector := NewCollector()
		collector.client = fakeContainerCreateClient{fakeDockerClient: fakeDockerClient{versionErr: errFake("offline")}}
		if !collector.SupportsContainerCreate() {
			t.Fatal("SupportsContainerCreate = false for injected create client")
		}
	})

	t.Run("missing socket", func(t *testing.T) {
		collector := NewCollector()
		collector.socketPath = t.TempDir() + "/missing.sock"
		if collector.SupportsContainerCreate() {
			t.Fatal("SupportsContainerCreate = true for missing socket")
		}
	})

	t.Run("regular file", func(t *testing.T) {
		path := t.TempDir() + "/docker.sock"
		if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
			t.Fatalf("write stale socket path: %v", err)
		}
		collector := NewCollector()
		collector.socketPath = path
		if collector.SupportsContainerCreate() {
			t.Fatal("SupportsContainerCreate = true for non-socket path")
		}
	})

	t.Run("stale socket", func(t *testing.T) {
		if supportsDockerEngine(t.Context(), fakeDockerClient{versionErr: errFake("connection refused")}) {
			t.Fatal("supportsDockerEngine = true for stale socket")
		}
	})

	t.Run("reachable engine", func(t *testing.T) {
		if !supportsDockerEngine(t.Context(), fakeDockerClient{version: "26.1.0"}) {
			t.Fatal("supportsDockerEngine = false for reachable Docker Engine")
		}
	})
}

func TestSupportsContainerCreateProbeIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	if supportsDockerEngine(ctx, fakeDockerClient{versionDelay: 2 * time.Second}) {
		t.Fatal("supportsDockerEngine = true for unresponsive socket")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("capability probe took %s, want bounded response", elapsed)
	}
}

func TestCollectorReturnsUnavailableWhenSocketMissing(t *testing.T) {
	collector := NewCollector()
	collector.socketPath = t.TempDir() + "/missing.sock"

	snapshot := collector.Collect()

	if snapshot.Available {
		t.Fatal("Available = true, want false")
	}
	if snapshot.Error == "" {
		t.Fatal("Error is empty, want socket missing summary")
	}
	if len(snapshot.Containers) != 0 {
		t.Fatalf("containers = %#v, want empty", snapshot.Containers)
	}
}

func TestCollectorParsesContainersAndStats(t *testing.T) {
	collector := NewCollector()
	collector.containerLimit = 10
	collector.client = fakeDockerClient{
		version: "24.0.0",
		containers: []containerListItem{{
			ID:      "abcdef1234567890",
			Names:   []string{"/web"},
			Image:   "nginx:latest",
			State:   "running",
			Status:  "Up 1 minute",
			Created: 1710000000,
		}},
		inspect: map[string]containerInspect{"abcdef1234567890": {RestartCount: 2, State: containerInspectState{StartedAt: "2026-05-26T10:00:00.000000000Z"}}},
		stats: map[string]containerStats{"abcdef1234567890": {
			CPUStats:    cpuStats{CPUUsage: cpuUsage{TotalUsage: 300, PercpuUsage: []uint64{1, 2}}, SystemCPUUsage: 1000, OnlineCPUs: 2},
			PreCPUStats: cpuStats{CPUUsage: cpuUsage{TotalUsage: 100}, SystemCPUUsage: 500, OnlineCPUs: 2},
			MemoryStats: memoryStats{Usage: 1048576, Limit: 67108864},
			Networks:    map[string]networkStats{"eth0": {RxBytes: 1000, TxBytes: 2000}},
		}},
	}

	snapshot := collector.Collect()

	if !snapshot.Available || snapshot.Version != "24.0.0" || snapshot.Error != "" {
		t.Fatalf("snapshot status = %#v", snapshot)
	}
	if len(snapshot.Containers) != 1 {
		t.Fatalf("containers = %#v", snapshot.Containers)
	}
	container := snapshot.Containers[0]
	if container.ID != "abcdef123456" || container.FullID != "abcdef1234567890" || container.Name != "web" || container.Image != "nginx:latest" || container.RestartCount != 2 {
		t.Fatalf("container basics = %#v", container)
	}
	if container.CPUUsage != 80 || container.MemoryUsage != 1048576 || container.MemoryLimit != 67108864 || container.MemoryPercent == 0 {
		t.Fatalf("container stats = %#v", container)
	}
	if container.NetworkRX != 1000 || container.NetworkTX != 2000 || container.StartedAt == 0 {
		t.Fatalf("container network/time = %#v", container)
	}
}

func TestCollectorKeepsContainerBasicsWhenStatsFail(t *testing.T) {
	collector := NewCollector()
	collector.client = fakeDockerClient{
		version:    "24.0.0",
		containers: []containerListItem{{ID: "abcdef1234567890", Names: []string{"/web"}, Image: "nginx:latest", State: "running", Status: "Up", Created: 1}},
		inspect:    map[string]containerInspect{"abcdef1234567890": {}},
		statsErr:   errFake("stats timeout"),
	}

	snapshot := collector.Collect()

	if !snapshot.Available || len(snapshot.Containers) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Error == "" {
		t.Fatal("Error is empty, want stats failure summary")
	}
	if snapshot.Containers[0].Name != "web" || snapshot.Containers[0].CPUUsage != 0 {
		t.Fatalf("container = %#v", snapshot.Containers[0])
	}
}

func TestCollectorStopsWithinCollectionTimeoutWhenStatsStall(t *testing.T) {
	collector := NewCollector()
	collector.collectionTimeout = 30 * time.Millisecond
	collector.client = fakeDockerClient{
		version: "24.0.0",
		containers: []containerListItem{
			{ID: "abcdef1234567890", Names: []string{"/web"}, Image: "nginx:latest", State: "running", Status: "Up", Created: 1},
			{ID: "deadbeef99999999", Names: []string{"/worker"}, Image: "queue:latest", State: "running", Status: "Up", Created: 2},
		},
		inspect: map[string]containerInspect{
			"abcdef1234567890": {},
			"deadbeef99999999": {},
		},
		statsDelay: 2 * time.Second,
	}

	started := time.Now()
	snapshot := collector.Collect()
	elapsed := time.Since(started)

	if elapsed > 250*time.Millisecond {
		t.Fatalf("Collect took %s, want bounded by collection timeout", elapsed)
	}
	if !snapshot.Available {
		t.Fatalf("snapshot.Available = false, want true: %#v", snapshot)
	}
	if snapshot.Error == "" {
		t.Fatal("Error is empty, want collection timeout summary")
	}
}

type fakeDockerClient struct {
	version      string
	versionErr   error
	versionDelay time.Duration
	containers   []containerListItem
	listErr      error
	inspect      map[string]containerInspect
	inspectErr   error
	stats        map[string]containerStats
	statsErr     error
	statsDelay   time.Duration
}

type fakeContainerCreateClient struct {
	fakeDockerClient
}

func (fakeContainerCreateClient) CreateContainer(context.Context, protocol.DockerContainerCreateRequest) (containerCreateResult, error) {
	return containerCreateResult{}, nil
}

func (c fakeDockerClient) Version(ctx context.Context) (string, error) {
	if c.versionDelay > 0 {
		select {
		case <-time.After(c.versionDelay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return c.version, c.versionErr
}
func (c fakeDockerClient) ListContainers(ctx context.Context, limit int) ([]containerListItem, error) {
	return c.containers, c.listErr
}
func (c fakeDockerClient) InspectContainer(ctx context.Context, id string) (containerInspect, error) {
	return c.inspect[id], c.inspectErr
}
func (c fakeDockerClient) ContainerStats(ctx context.Context, id string) (containerStats, error) {
	if c.statsDelay > 0 {
		select {
		case <-time.After(c.statsDelay):
		case <-ctx.Done():
			return containerStats{}, ctx.Err()
		}
	}
	return c.stats[id], c.statsErr
}
func (c fakeDockerClient) ContainerLogs(ctx context.Context, id string, tail int, follow bool, timestamps bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

var _ clientAPI = fakeDockerClient{}
var _ = protocol.DockerSnapshot{}

type errFake string

func (e errFake) Error() string { return string(e) }
