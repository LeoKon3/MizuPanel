package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mizupanel/mizupanel/internal/agent/filetree"
	"github.com/mizupanel/mizupanel/internal/protocol"
)

func TestMaxServerMessageBytesAllowsDefaultUploadPayload(t *testing.T) {
	encodedUploadBytes := base64.StdEncoding.EncodedLen(filetree.DefaultMaxUploadBytes)
	if maxServerMessageBytes <= encodedUploadBytes {
		t.Fatalf("maxServerMessageBytes = %d, want > encoded default upload size %d", maxServerMessageBytes, encodedUploadBytes)
	}
}

func TestClientUsesNodeTokenFromHelloAckOnNextConnection(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	tokens := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens <- strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		var hello protocol.HelloMessage
		if err := conn.ReadJSON(&hello); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		if err := conn.WriteJSON(protocol.HelloAckMessage{Type: protocol.MessageTypeHelloAck, NodeID: "node-1", NodeToken: "node-token", Interval: 5}); err != nil {
			t.Errorf("write ack: %v", err)
			return
		}
		var metric protocol.MetricsMessage
		_ = conn.ReadJSON(&metric)
	}))
	t.Cleanup(server.Close)

	client := NewClient("ws"+strings.TrimPrefix(server.URL, "http"), "install-token")
	for range 2 {
		_, err := client.SendHelloAndMetric(t.Context(), protocol.HelloMessage{Type: protocol.MessageTypeHello, Hostname: "oracle-sg"}, protocol.MetricsMessage{Type: protocol.MessageTypeMetrics})
		if err != nil {
			t.Fatalf("SendHelloAndMetric returned error: %v", err)
		}
	}
	if first, second := <-tokens, <-tokens; first != "install-token" || second != "node-token" {
		t.Fatalf("tokens = %q, %q; want install-token, node-token", first, second)
	}
}
func TestClientPersistsNodeTokenFromHelloAck(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		var hello protocol.HelloMessage
		if err := conn.ReadJSON(&hello); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		if err := conn.WriteJSON(protocol.HelloAckMessage{Type: protocol.MessageTypeHelloAck, NodeID: "node-1", NodeToken: "node-token", Interval: 5}); err != nil {
			t.Errorf("write ack: %v", err)
			return
		}
		var metric protocol.MetricsMessage
		_ = conn.ReadJSON(&metric)
	}))
	t.Cleanup(server.Close)

	persistedToken := ""
	client := NewClient("ws"+strings.TrimPrefix(server.URL, "http"), "install-token")
	client.SetNodeTokenHandler(func(token string) error {
		persistedToken = token
		return nil
	})
	_, err := client.SendHelloAndMetric(t.Context(), protocol.HelloMessage{Type: protocol.MessageTypeHello, Hostname: "oracle-sg"}, protocol.MetricsMessage{Type: protocol.MessageTypeMetrics})
	if err != nil {
		t.Fatalf("SendHelloAndMetric returned error: %v", err)
	}
	if persistedToken != "node-token" {
		t.Fatalf("persisted token = %q, want node-token", persistedToken)
	}
}

func TestRunRespondsToAgentManagementRequests(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	statusReceived := make(chan protocol.AgentStatusResponse, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		var hello protocol.HelloMessage
		if err := conn.ReadJSON(&hello); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		if err := conn.WriteJSON(protocol.HelloAckMessage{Type: protocol.MessageTypeHelloAck, NodeID: "node-1", Interval: 1}); err != nil {
			t.Errorf("write ack: %v", err)
			return
		}
		var metric protocol.MetricsMessage
		if err := conn.ReadJSON(&metric); err != nil {
			t.Errorf("read metric: %v", err)
			return
		}
		if err := conn.WriteJSON(protocol.AgentStatusRequest{Type: protocol.MessageTypeAgentStatusRequest, RequestID: "req-status", NodeID: "node-1"}); err != nil {
			t.Errorf("write status request: %v", err)
			return
		}
		var response protocol.AgentStatusResponse
		if err := conn.ReadJSON(&response); err != nil {
			t.Errorf("read status response: %v", err)
			return
		}
		statusReceived <- response
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := NewClient("ws"+strings.TrimPrefix(server.URL, "http"), "")
	client.SetAgentManagementHandler(fakeAgentManagementHandler{})
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, protocol.HelloMessage{Type: protocol.MessageTypeHello, Hostname: "oracle-sg"}, time.Hour, func(nodeID string, timestamp int64) (protocol.MetricsMessage, error) {
			return protocol.MetricsMessage{Type: protocol.MessageTypeMetrics, NodeID: nodeID, Timestamp: timestamp}, nil
		})
	}()

	response := <-statusReceived
	cancel()
	if err := <-done; err != nil && err != context.Canceled && !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("Run returned error: %v", err)
	}
	if response.Type != protocol.MessageTypeAgentStatusResponse || response.RequestID != "req-status" || response.User != "root" || response.Mode != "ops" || !response.TerminalEnabled {
		t.Fatalf("status response = %#v", response)
	}
}

func TestRunDispatchesAllK8sResourceRequests(t *testing.T) {
	k8sTypes := []string{
		protocol.MessageTypeK8sClusterConnect,
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
		protocol.MessageTypeK8sApplyManifest,
		protocol.MessageTypeK8sGetPodLogs,
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		var hello protocol.HelloMessage
		if err := conn.ReadJSON(&hello); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		if err := conn.WriteJSON(protocol.HelloAckMessage{Type: protocol.MessageTypeHelloAck, NodeID: "node-1", Interval: 1}); err != nil {
			t.Errorf("write ack: %v", err)
			return
		}
		var metric protocol.MetricsMessage
		if err := conn.ReadJSON(&metric); err != nil {
			t.Errorf("read metric: %v", err)
			return
		}
		for _, msgType := range k8sTypes {
			request := protocol.K8sResourceRequest{Type: msgType, RequestID: msgType, ClusterID: "cluster-1"}
			if err := conn.WriteJSON(request); err != nil {
				t.Errorf("write k8s request: %v", err)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dispatched := make(chan string, len(k8sTypes))
	client := NewClient("ws"+strings.TrimPrefix(server.URL, "http"), "")
	client.SetKubectlHandler(fakeKubectlHandler{handled: dispatched})
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, protocol.HelloMessage{Type: protocol.MessageTypeHello, Hostname: "oracle-sg"}, time.Hour, func(nodeID string, timestamp int64) (protocol.MetricsMessage, error) {
			return protocol.MetricsMessage{Type: protocol.MessageTypeMetrics, NodeID: nodeID, Timestamp: timestamp}, nil
		})
	}()

	deadline := time.After(2 * time.Second)
	seen := map[string]bool{}
	for len(seen) < len(k8sTypes) {
		select {
		case msgType := <-dispatched:
			seen[msgType] = true
		case <-deadline:
			t.Fatalf("dispatched types = %#v, want all %#v", seen, k8sTypes)
		}
	}
	cancel()
	if err := <-done; err != nil && err != context.Canceled && !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("Run returned error: %v", err)
	}
}

type fakeKubectlHandler struct {
	handled chan<- string
}

func (h fakeKubectlHandler) Handle(_ context.Context, msgType string, _ json.RawMessage, _ func(interface{}) error) error {
	h.handled <- msgType
	return nil
}

type fakeAgentManagementHandler struct{}

func (fakeAgentManagementHandler) Status() protocol.AgentStatusResponse {
	return protocol.AgentStatusResponse{Version: "0.1.0", User: "root", Mode: "ops", TerminalEnabled: true, DockerAvailable: true, ConfigPath: "/usr/local/mizupanel/agent.yaml", ServiceName: "mizupanel-agent", Uptime: 12, CollectedAt: 1710000000}
}

func (fakeAgentManagementHandler) Restart() protocol.AgentRestartResponse {
	return protocol.AgentRestartResponse{Accepted: true, Message: "重启命令已下发，等待 Agent 重新连接"}
}

func (fakeAgentManagementHandler) Logs(int) protocol.AgentLogsResponse {
	return protocol.AgentLogsResponse{Lines: 100, Content: "started", CollectedAt: 1710000000}
}

func (fakeAgentManagementHandler) Upgrade(protocol.AgentUpgradeRequest, func(protocol.AgentUpgradeResponse)) protocol.AgentUpgradeResponse {
	return protocol.AgentUpgradeResponse{Accepted: true, Stage: "preparing"}
}

func TestSendHelloAndMetricSendsHelloBeforeMetric(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	messages := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		var hello protocol.HelloMessage
		if err := conn.ReadJSON(&hello); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		messages <- hello.Type
		if err := conn.WriteJSON(protocol.HelloAckMessage{Type: protocol.MessageTypeHelloAck, NodeID: "node-1", Interval: 5}); err != nil {
			t.Errorf("write ack: %v", err)
			return
		}

		var metric protocol.MetricsMessage
		if err := conn.ReadJSON(&metric); err != nil {
			t.Errorf("read metric: %v", err)
			return
		}
		messages <- metric.Type
	}))
	t.Cleanup(server.Close)

	client := NewClient("ws"+strings.TrimPrefix(server.URL, "http"), "secret")
	ack, err := client.SendHelloAndMetric(t.Context(), protocol.HelloMessage{Type: protocol.MessageTypeHello, Hostname: "oracle-sg"}, protocol.MetricsMessage{Type: protocol.MessageTypeMetrics})
	if err != nil {
		t.Fatalf("SendHelloAndMetric returned error: %v", err)
	}
	if ack.NodeID != "node-1" {
		t.Fatalf("NodeID = %q, want node-1", ack.NodeID)
	}
	if first, second := <-messages, <-messages; first != protocol.MessageTypeHello || second != protocol.MessageTypeMetrics {
		t.Fatalf("message order = %s, %s", first, second)
	}
}
