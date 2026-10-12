package agenthub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func TestStreamSessionRegisteredBeforeImmediatePublish(t *testing.T) {
	agent := &agentConnection{}

	logTail, err := agent.startLogTailSession("tail-1", func() error {
		if !agent.publishLogTail("tail-1", json.RawMessage(`{"type":"log_tail_response","session_id":"tail-1"}`)) {
			t.Fatal("log-tail session was not registered before send")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("start log-tail session: %v", err)
	}
	select {
	case message := <-logTail.messages:
		if string(message) == "" {
			t.Fatal("empty log-tail message")
		}
	default:
		t.Fatal("immediate log-tail response was lost")
	}

	containerLogs, err := agent.startContainerLogsSession("logs-1", func() error {
		if !agent.publishContainerLogs("logs-1", json.RawMessage(`{"type":"container_logs_response","session_id":"logs-1"}`)) {
			t.Fatal("container-logs session was not registered before send")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("start container-logs session: %v", err)
	}
	select {
	case message := <-containerLogs.messages:
		if string(message) == "" {
			t.Fatal("empty container-logs message")
		}
	default:
		t.Fatal("immediate container-logs response was lost")
	}
}

func TestStreamSessionUnregisterAndLatePublishAreSafe(t *testing.T) {
	agent := &agentConnection{}
	session := newStreamSession()
	agent.registerLogTail("tail-1", session)
	agent.unregisterLogTail("tail-1", session)

	select {
	case <-session.done:
	default:
		t.Fatal("unregister did not stop stream session")
	}
	if agent.publishLogTail("tail-1", json.RawMessage(`{"late":true}`)) {
		t.Fatal("late publish succeeded after unregister")
	}
	if session.publish(json.RawMessage(`{"late":true}`)) {
		t.Fatal("direct late publish succeeded after stop")
	}

	// Repeated cleanup must stay idempotent and must not close the message channel.
	agent.unregisterLogTail("tail-1", session)
	session.stop()
}

func TestPendingResponseDeliveryIsExactlyOnceAndNonBlocking(t *testing.T) {
	ch := make(chan protocol.FileListResponse, 1)
	agent := &agentConnection{pendingLists: map[string]chan protocol.FileListResponse{"req-1": ch}}
	first := protocol.FileListResponse{RequestID: "req-1", Path: "/first"}
	second := protocol.FileListResponse{RequestID: "req-1", Path: "/second"}

	done := make(chan struct{})
	go func() {
		agent.deliverFileList(first)
		agent.deliverFileList(second)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("duplicate pending response blocked the websocket reader")
	}
	if got := <-ch; got.Path != first.Path {
		t.Fatalf("delivered response path = %q, want %q", got.Path, first.Path)
	}
	select {
	case got := <-ch:
		t.Fatalf("duplicate response was delivered: %#v", got)
	default:
	}
	if len(agent.pendingLists) != 0 {
		t.Fatalf("pending list requests = %d, want 0", len(agent.pendingLists))
	}
}

func TestK8sResponseDeliveryDoesNotBlockWhenChannelIsFull(t *testing.T) {
	ch := make(chan json.RawMessage, 1)
	ch <- json.RawMessage(`{"existing":true}`)
	agent := &agentConnection{pendingK8sMessages: map[string]chan json.RawMessage{"req-1": ch}}

	done := make(chan struct{})
	go func() {
		agent.deliverK8sMessage("req-1", json.RawMessage(`{"request_id":"req-1"}`))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("full K8s response channel blocked the websocket reader")
	}
	if len(agent.pendingK8sMessages) != 0 {
		t.Fatalf("pending K8s requests = %d, want 0", len(agent.pendingK8sMessages))
	}
}

func TestClosePendingOperationsDoesNotBlockOnFullOrUnbufferedChannels(t *testing.T) {
	full := make(chan protocol.FileListResponse, 1)
	full <- protocol.FileListResponse{RequestID: "already-full"}
	unbuffered := make(chan json.RawMessage)
	agent := &agentConnection{
		pendingLists:       map[string]chan protocol.FileListResponse{"req-1": full},
		pendingK8sMessages: map[string]chan json.RawMessage{"req-2": unbuffered},
		closed:             make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		agent.closePendingOperations("disconnected")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closePendingOperations blocked on a pending channel")
	}
	select {
	case <-agent.closed:
	default:
		t.Fatal("connection lifetime was not closed")
	}
}

func TestHandlerCloseClosesMultipleConnectionsAndResourcesIdempotently(t *testing.T) {
	agentServer1, agentConn1 := websocketPair(t)
	agentServer2, agentConn2 := websocketPair(t)
	terminalServer, terminalConn := websocketPair(t)
	execServer, execConn := websocketPair(t)

	logTail := newStreamSession()
	containerLogs := newStreamSession()
	pending := make(chan protocol.FileListResponse, 1)
	first := &agentConnection{
		nodeID:                "node-1",
		conn:                  agentServer1,
		terminals:             map[string]*browserTerminal{"terminal-1": {sessionID: "terminal-1", conn: terminalServer}},
		containerExecs:        map[string]*browserContainerExec{"exec-1": {sessionID: "exec-1", containerID: "container-1", conn: execServer}},
		logTailSessions:       map[string]*streamSession{"tail-1": logTail},
		containerLogsSessions: map[string]*streamSession{"logs-1": containerLogs},
		pendingLists:          map[string]chan protocol.FileListResponse{"req-1": pending},
		closed:                make(chan struct{}),
	}
	second := &agentConnection{nodeID: "node-2", conn: agentServer2, closed: make(chan struct{})}
	handler := &Handler{
		sessions:    map[string]string{"node-1": "session-1", "node-2": "session-2"},
		connections: map[string]*agentConnection{"node-1": first, "node-2": second},
	}

	done := make(chan struct{})
	go func() {
		handler.Close("server shutdown")
		handler.Close("server shutdown again")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Handler.Close blocked")
	}

	handler.mu.Lock()
	if !handler.closed || len(handler.connections) != 0 || len(handler.sessions) != 0 {
		t.Fatalf("handler state closed=%v connections=%d sessions=%d", handler.closed, len(handler.connections), len(handler.sessions))
	}
	handler.mu.Unlock()
	for name, closed := range map[string]<-chan struct{}{"first": first.closed, "second": second.closed, "log-tail": logTail.done, "container-logs": containerLogs.done} {
		select {
		case <-closed:
		default:
			t.Fatalf("%s resource was not closed", name)
		}
	}
	select {
	case response := <-pending:
		if response.Error != "server shutdown" {
			t.Fatalf("pending response error = %q", response.Error)
		}
	default:
		t.Fatal("pending request was not released")
	}

	for name, conn := range map[string]*websocket.Conn{"agent-1": agentConn1, "agent-2": agentConn2, "terminal": terminalConn, "exec": execConn} {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("set %s deadline: %v", name, err)
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}
}

func TestHandlerCloseRejectsNewConnections(t *testing.T) {
	handler := &Handler{sessions: make(map[string]string), connections: make(map[string]*agentConnection)}
	handler.Close("server shutdown")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/agent/ws", nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "service unavailable") {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func websocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConn <- conn
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket pair: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case serverSide := <-serverConn:
		return serverSide, client
	case <-time.After(time.Second):
		t.Fatal("timed out creating websocket pair")
		return nil, nil
	}
}
