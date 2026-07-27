package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestTaskRunnerSupportedRequiresOnlineCapableAgent(t *testing.T) {
	handler := &Handler{connections: map[string]*agentConnection{
		"capable": {supportsTaskRunner: true},
		"legacy":  {supportsTaskRunner: false},
	}}
	if !handler.TaskRunnerSupported("capable") {
		t.Fatal("capable online Agent should support task runner")
	}
	if handler.TaskRunnerSupported("legacy") || handler.TaskRunnerSupported("offline") {
		t.Fatal("legacy or offline Agent reported task runner support")
	}
}

func TestRunScriptReturnsStableOfflineAndUnsupportedResults(t *testing.T) {
	handler := &Handler{connections: make(map[string]*agentConnection)}
	request := protocol.ScriptExecutionRequest{ExecutionID: 41, Script: "true", TimeoutSeconds: 5}
	offline, err := handler.RunScript(t.Context(), "offline", request)
	if err != nil {
		t.Fatal(err)
	}
	if offline.Type != protocol.MessageTypeScriptExecutionResponse || offline.ExecutionID != 41 || offline.Status != protocol.ScriptExecutionStatusFailed || !strings.Contains(offline.Error, "离线") {
		t.Fatalf("offline response = %#v", offline)
	}

	handler.connections["legacy"] = &agentConnection{}
	unsupported, err := handler.RunScript(t.Context(), "legacy", request)
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.Status != protocol.ScriptExecutionStatusUnsupported || !strings.Contains(unsupported.Error, "升级") {
		t.Fatalf("unsupported response = %#v", unsupported)
	}
}

func TestRunScriptRoundTripAndBoundsAgentOutput(t *testing.T) {
	handler, conn, nodeID := newTaskRunnerAgent(t, true)
	done := make(chan protocol.ScriptExecutionResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		response, err := handler.RunScript(t.Context(), nodeID, protocol.ScriptExecutionRequest{ExecutionID: 52, Script: "printf ok", TimeoutSeconds: 5})
		done <- response
		errCh <- err
	}()

	var request protocol.ScriptExecutionRequest
	if err := conn.ReadJSON(&request); err != nil {
		t.Fatalf("read request: %v", err)
	}
	if request.Type != protocol.MessageTypeScriptExecutionRequest || request.RequestID == "" || request.ExecutionID != 52 || request.Script != "printf ok" || request.TimeoutSeconds != 5 {
		t.Fatalf("request = %#v", request)
	}
	exitCode := 0
	if err := conn.WriteJSON(protocol.ScriptExecutionResponse{
		Type:        protocol.MessageTypeScriptExecutionResponse,
		RequestID:   request.RequestID,
		ExecutionID: request.ExecutionID,
		Status:      protocol.ScriptExecutionStatusSuccess,
		ExitCode:    &exitCode,
		Output:      strings.Repeat("x", protocol.ScriptExecutionMaxOutputBytes+100),
		DurationMS:  17,
	}); err != nil {
		t.Fatalf("write response: %v", err)
	}
	response := <-done
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if response.Status != protocol.ScriptExecutionStatusSuccess || response.ExitCode == nil || *response.ExitCode != 0 || len(response.Output) != protocol.ScriptExecutionMaxOutputBytes || !response.OutputTruncated || response.DurationMS != 17 {
		t.Fatalf("response = status:%s exit:%v output:%d truncated:%v duration:%d", response.Status, response.ExitCode, len(response.Output), response.OutputTruncated, response.DurationMS)
	}
}

func TestRunScriptCallerCancellationRemovesWaiterAndIgnoresLateResponses(t *testing.T) {
	handler, conn, nodeID := newTaskRunnerAgent(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan protocol.ScriptExecutionResponse, 1)
	go func() {
		response, _ := handler.RunScript(ctx, nodeID, protocol.ScriptExecutionRequest{ExecutionID: 61, Script: "sleep 30", TimeoutSeconds: 30})
		done <- response
	}()

	var request protocol.ScriptExecutionRequest
	if err := conn.ReadJSON(&request); err != nil {
		t.Fatalf("read request: %v", err)
	}
	cancel()
	response := <-done
	if response.Status != protocol.ScriptExecutionStatusCancelled {
		t.Fatalf("response = %#v", response)
	}
	agent := handler.connection(nodeID)
	agent.pendingMu.Lock()
	pending := len(agent.pendingRPCs)
	agent.pendingMu.Unlock()
	if pending != 0 {
		t.Fatalf("pending RPCs after cancellation = %d", pending)
	}

	late := protocol.ScriptExecutionResponse{Type: protocol.MessageTypeScriptExecutionResponse, RequestID: request.RequestID, ExecutionID: request.ExecutionID, Status: protocol.ScriptExecutionStatusSuccess, Output: "late"}
	if err := conn.WriteJSON(late); err != nil {
		t.Fatalf("write late response: %v", err)
	}
	if err := conn.WriteJSON(late); err != nil {
		t.Fatalf("write duplicate response: %v", err)
	}

	secondDone := make(chan protocol.ScriptExecutionResponse, 1)
	go func() {
		second, _ := handler.RunScript(t.Context(), nodeID, protocol.ScriptExecutionRequest{ExecutionID: 62, Script: "true", TimeoutSeconds: 5})
		secondDone <- second
	}()
	var secondRequest protocol.ScriptExecutionRequest
	if err := conn.ReadJSON(&secondRequest); err != nil {
		t.Fatalf("read second request: %v", err)
	}
	if secondRequest.ExecutionID != 62 {
		t.Fatalf("second request = %#v", secondRequest)
	}
	exitCode := 0
	if err := conn.WriteJSON(protocol.ScriptExecutionResponse{Type: protocol.MessageTypeScriptExecutionResponse, RequestID: secondRequest.RequestID, ExecutionID: 62, Status: protocol.ScriptExecutionStatusSuccess, ExitCode: &exitCode}); err != nil {
		t.Fatalf("write second response: %v", err)
	}
	if second := <-secondDone; second.Status != protocol.ScriptExecutionStatusSuccess {
		t.Fatalf("second response = %#v", second)
	}
}

func TestRunScriptMapsCallerDeadlineAndDisconnect(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		handler, conn, nodeID := newTaskRunnerAgent(t, true)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer cancel()
		done := make(chan protocol.ScriptExecutionResponse, 1)
		go func() {
			response, _ := handler.RunScript(ctx, nodeID, protocol.ScriptExecutionRequest{ExecutionID: 71, Script: "sleep 30", TimeoutSeconds: 30})
			done <- response
		}()
		var request protocol.ScriptExecutionRequest
		if err := conn.ReadJSON(&request); err != nil {
			t.Fatalf("read request: %v", err)
		}
		if response := <-done; response.Status != protocol.ScriptExecutionStatusTimedOut {
			t.Fatalf("response = %#v", response)
		}
	})

	t.Run("disconnect", func(t *testing.T) {
		handler, conn, nodeID := newTaskRunnerAgent(t, true)
		done := make(chan protocol.ScriptExecutionResponse, 1)
		go func() {
			response, _ := handler.RunScript(t.Context(), nodeID, protocol.ScriptExecutionRequest{ExecutionID: 72, Script: "sleep 30", TimeoutSeconds: 30})
			done <- response
		}()
		var request protocol.ScriptExecutionRequest
		if err := conn.ReadJSON(&request); err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("close Agent connection: %v", err)
		}
		select {
		case response := <-done:
			if response.Status != protocol.ScriptExecutionStatusFailed || !strings.Contains(response.Error, "断开") {
				t.Fatalf("response = %#v", response)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("RunScript did not unblock after disconnect")
		}
	})
}

func TestGenericRPCDeliveryIsNonBlockingForDuplicateAndDisconnect(t *testing.T) {
	requestID := "req-1"
	ch := make(chan rpcDelivery, 1)
	agent := &agentConnection{
		pendingRPCs: map[string]chan rpcDelivery{requestID: ch},
		closed:      make(chan struct{}),
	}
	raw := json.RawMessage(`{"type":"script_execution_response","request_id":"req-1"}`)
	agent.deliverRPC(requestID, raw)
	agent.deliverRPC(requestID, raw)
	agent.closePendingOperations("disconnected")
	select {
	case delivery := <-ch:
		if delivery.err == nil && string(delivery.raw) != string(raw) {
			t.Fatalf("delivery = %#v", delivery)
		}
	case <-time.After(time.Second):
		t.Fatal("generic RPC was not delivered")
	}
	select {
	case <-agent.closed:
	default:
		t.Fatal("connection lifetime was not closed")
	}
}

func TestGenericRPCLateResponseAndDisconnectRace(t *testing.T) {
	for i := 0; i < 100; i++ {
		requestID := "req"
		ch := make(chan rpcDelivery, 1)
		agent := &agentConnection{
			pendingRPCs: map[string]chan rpcDelivery{requestID: ch},
			closed:      make(chan struct{}),
		}
		raw := json.RawMessage(`{"type":"script_execution_response","request_id":"req"}`)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			agent.deliverRPC(requestID, raw)
			agent.deliverRPC(requestID, raw)
		}()
		go func() {
			defer wg.Done()
			<-start
			agent.removeRPC(requestID, ch)
		}()
		go func() {
			defer wg.Done()
			<-start
			agent.closePendingOperations("disconnected")
		}()
		close(start)
		wg.Wait()
	}
}

func newTaskRunnerAgent(t *testing.T, capable bool) (*Handler, *websocket.Conn, string) {
	t.Helper()
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
	t.Cleanup(func() { conn.Close() })
	if err := conn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "task-node", Hostname: "task-host", Name: "Task Host", OS: "linux", Arch: "amd64", TaskRunner: capable}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var ack protocol.HelloAckMessage
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if ack.NodeID == "" || handler.TaskRunnerSupported(ack.NodeID) != capable {
		t.Fatalf("ack/capability = %#v/%v", ack, handler.TaskRunnerSupported(ack.NodeID))
	}
	return handler, conn, ack.NodeID
}
