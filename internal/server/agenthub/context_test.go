package agenthub

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSendToNodeWithContextCancelsAndCleansPendingRequest(t *testing.T) {
	handler, conn, nodeID := newTaskRunnerAgent(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := handler.SendToNodeWithContext(ctx, nodeID, map[string]string{
			"type":       "context_test_request",
			"request_id": "context-request-1",
		}, time.Minute)
		done <- err
	}()

	var request map[string]any
	if err := conn.ReadJSON(&request); err != nil {
		t.Fatalf("read context request: %v", err)
	}
	if request["request_id"] != "context-request-1" {
		t.Fatalf("request = %#v", request)
	}
	agent := handler.connection(nodeID)
	agent.pendingMu.Lock()
	pendingBeforeCancel := len(agent.pendingK8sMessages)
	agent.pendingMu.Unlock()
	if pendingBeforeCancel != 1 {
		t.Fatalf("pending before cancel = %d, want 1", pendingBeforeCancel)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("context send error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not unblock send")
	}
	agent.pendingMu.Lock()
	pendingAfterCancel := len(agent.pendingK8sMessages)
	agent.pendingMu.Unlock()
	if pendingAfterCancel != 0 {
		t.Fatalf("pending after cancel = %d, want 0", pendingAfterCancel)
	}
}

func TestSendToNodeWithTimeoutRetainsLegacyTimeoutBehavior(t *testing.T) {
	handler, conn, nodeID := newTaskRunnerAgent(t, true)
	done := make(chan error, 1)
	go func() {
		_, err := handler.SendToNodeWithTimeout(nodeID, map[string]string{
			"type":       "legacy_timeout_test_request",
			"request_id": "legacy-request-1",
		}, 30*time.Millisecond)
		done <- err
	}()

	var request map[string]any
	if err := conn.ReadJSON(&request); err != nil {
		t.Fatalf("read legacy request: %v", err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "超时") {
			t.Fatalf("legacy timeout error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("legacy timeout wrapper did not return")
	}
	agent := handler.connection(nodeID)
	agent.pendingMu.Lock()
	pending := len(agent.pendingK8sMessages)
	agent.pendingMu.Unlock()
	if pending != 0 {
		t.Fatalf("pending after legacy timeout = %d, want 0", pending)
	}
}
