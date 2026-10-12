package k8s

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type healthHub struct {
	mu       sync.Mutex
	online   bool
	response json.RawMessage
	started  chan struct{}
	release  chan struct{}
	active   int
	max      int
}

func (h *healthHub) IsNodeOnline(string) bool { return h.online }

func (h *healthHub) SendToNodeWithTimeout(string, interface{}, time.Duration) (json.RawMessage, error) {
	return h.response, nil
}

func (h *healthHub) SendToNodeWithContext(ctx context.Context, _ string, _ interface{}, _ time.Duration) (json.RawMessage, error) {
	h.mu.Lock()
	h.active++
	if h.active > h.max {
		h.max = h.active
	}
	h.mu.Unlock()
	if h.started != nil {
		select {
		case h.started <- struct{}{}:
		default:
		}
	}
	if h.release != nil {
		select {
		case <-h.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	h.mu.Lock()
	h.active--
	h.mu.Unlock()
	return h.response, nil
}

func TestHealthMonitorMarksSuccessfulProbeOnline(t *testing.T) {
	hub := &healthHub{online: true, response: json.RawMessage(`{"success":true,"summary":{"version":"v1"}}`)}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)
	monitor := NewHealthMonitor(service)
	if err := monitor.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	cluster, err := service.GetClusterWithNodeInfo("cluster-1")
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if cluster.Status != ClusterStatusOnline || cluster.HealthReason != "" || cluster.HealthCheckedAt == nil {
		t.Fatalf("successful health projection = %#v", cluster)
	}
}

func TestPublicClusterOmitsUncheckedHealthTimestamp(t *testing.T) {
	encoded, err := json.Marshal((&Cluster{ID: "cluster-1"}).Public())
	if err != nil {
		t.Fatalf("marshal public cluster: %v", err)
	}
	if string(encoded) == "" || containsJSONField(encoded, "health_checked_at") {
		t.Fatalf("unchecked health timestamp leaked into public JSON: %s", encoded)
	}
}

func TestEffectiveClusterStatusRechecksAfterAgentRecovery(t *testing.T) {
	status, reason := EffectiveClusterStatus(
		ClusterStatusOffline,
		HealthReasonAgentOffline,
		ClusterStatusOnline,
		time.Time{},
		time.Now().UTC(),
	)
	if status != ClusterStatusUnknown || reason != HealthReasonChecking {
		t.Fatalf("recovered agent without a probe = %q/%q", status, reason)
	}
}

func containsJSONField(encoded []byte, field string) bool {
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return false
	}
	_, ok := payload[field]
	return ok
}

func TestHealthMonitorMarksProbeFailureUnavailable(t *testing.T) {
	hub := &healthHub{online: true, response: json.RawMessage(`{"success":false,"error":"timeout"}`)}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)
	monitor := NewHealthMonitor(service)
	if err := monitor.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	cluster, err := service.GetClusterWithNodeInfo("cluster-1")
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if cluster.Status != ClusterStatusOffline || cluster.HealthReason != HealthReasonAPIUnreachable {
		t.Fatalf("failed health projection = %#v", cluster)
	}
}

func TestHealthMonitorBoundsConcurrentProbes(t *testing.T) {
	hub := &healthHub{
		online:   true,
		response: json.RawMessage(`{"success":true,"summary":{}}`),
		started:  make(chan struct{}, 8),
		release:  make(chan struct{}),
	}
	service := newTestService(t, "apiVersion: v1\nkind: Config\n", "ctx-a", hub)
	for i := 0; i < 8; i++ {
		cluster := &Cluster{ID: string(rune('a' + i)), Name: "cluster", NodeID: "node-1", KubeconfigContent: "config", Status: ClusterStatusUnknown, HealthReason: HealthReasonChecking, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := service.store.CreateCluster(cluster); err != nil {
			t.Fatalf("create cluster %d: %v", i, err)
		}
	}
	monitor := NewHealthMonitor(service)
	done := make(chan error, 1)
	go func() { done <- monitor.Sweep(context.Background()) }()
	for i := 0; i < DefaultHealthConcurrency; i++ {
		select {
		case <-hub.started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for probe")
		}
	}
	close(hub.release)
	if err := <-done; err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if hub.max > DefaultHealthConcurrency {
		t.Fatalf("max concurrent probes = %d, want <= %d", hub.max, DefaultHealthConcurrency)
	}
}
