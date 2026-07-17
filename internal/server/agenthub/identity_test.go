package agenthub

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestAgentWebSocketUsesAgentProvidedNodeID(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentToken: "secret", Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	header := http.Header{"Authorization": {"Bearer secret"}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "agent-unique-1", Hostname: "same-host", Name: "Same Host", OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var ack protocol.HelloAckMessage
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if ack.NodeID != "agent-unique-1" {
		t.Fatalf("NodeID = %q, want agent-unique-1", ack.NodeID)
	}
}

func TestAgentWebSocketRejectsUnsupportedProtocolVersion(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatal(err)
	}
	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "agent-1", Name: "agent", Status: "offline", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(nodes, store.NewMetricStore(database), Options{AgentToken: "secret", Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	header := http.Header{"Authorization": {"Bearer secret"}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "agent-1", ProtocolVersion: protocol.CurrentProtocolVersion + 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("unsupported protocol received a normal response")
	}
	events, err := store.NewConnectionEventStore(database).List(t.Context(), "agent-1")
	if err != nil || len(events) != 1 || events[0].Type != store.ConnectionEventProtocolRejected {
		t.Fatalf("protocol events = %#v err=%v", events, err)
	}
}

func TestAgentWebSocketRecordsHeartbeatTimeout(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentToken: "secret", Interval: 5, HeartbeatTimeout: 50 * time.Millisecond})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	header := http.Header{"Authorization": {"Bearer secret"}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "timeout-agent", Hostname: "host", Name: "host", ProtocolVersion: protocol.CurrentProtocolVersion}); err != nil {
		t.Fatal(err)
	}
	var ack protocol.HelloAckMessage
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("connection did not time out")
	}
	time.Sleep(20 * time.Millisecond)
	events, err := store.NewConnectionEventStore(database).List(t.Context(), "timeout-agent")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Type == store.ConnectionEventHeartbeatTimeout {
			found = true
		}
	}
	if !found {
		t.Fatalf("heartbeat timeout event missing: %#v", events)
	}
}

func TestAgentWebSocketRecordsReplacementAndIdentityConflict(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentToken: "secret", Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	connect := func(hostname string) *websocket.Conn {
		header := http.Header{"Authorization": {"Bearer secret"}}
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "shared-agent", Hostname: hostname, Name: hostname, OS: "linux", Arch: "amd64", ProtocolVersion: protocol.CurrentProtocolVersion, IdentitySource: "persistent_uuid"}); err != nil {
			t.Fatal(err)
		}
		var ack protocol.HelloAckMessage
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	first := connect("host-a")
	defer first.Close()
	second := connect("host-b")
	defer second.Close()
	events, err := store.NewConnectionEventStore(database).List(t.Context(), "shared-agent")
	if err != nil {
		t.Fatal(err)
	}
	types := make(map[string]bool)
	for _, event := range events {
		types[event.Type] = true
	}
	if !types[store.ConnectionEventConnected] || !types[store.ConnectionEventReplaced] || !types[store.ConnectionEventIdentityConflict] {
		t.Fatalf("event types = %#v", types)
	}
	diagnostics, err := handler.ConnectionDiagnostics(t.Context(), "shared-agent")
	if err != nil {
		t.Fatal(err)
	}
	if !diagnostics.Online || diagnostics.Health != "identity_conflict" || !diagnostics.IdentityConflict {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}
