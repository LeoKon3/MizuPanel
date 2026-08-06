package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/store"
	"github.com/mizupanel/mizupanel/internal/version"
)

type acceptedOperationNodeStub struct {
	platformNodeOperationsStub
	upgrade   protocol.AgentUpgradeStatus
	rebootErr error
}

func (s acceptedOperationNodeStub) AgentUpgradeStatus(string) protocol.AgentUpgradeStatus {
	return s.upgrade
}

func (s acceptedOperationNodeStub) RebootCompletedAfter(context.Context, string, time.Time) (bool, error) {
	return false, s.rebootErr
}

func TestAcceptedOperationVerifierCompletesUpgradeAfterServerRestart(t *testing.T) {
	ops := acceptedOperationNodeStub{upgrade: protocol.AgentUpgradeStatus{NodeID: "node-1", ActualVersion: version.Current, Stage: "idle"}}
	registry := NewRegistry(RegistryDependencies{AgentOps: ops})
	adapter := serviceTestAdapter{complete: func(context.Context, ProviderCredential, ChatRequest) (ChatResponse, error) {
		return ChatResponse{Content: "unused"}, nil
	}}
	service, aiStore, _ := newServiceTestFixture(t, registry, adapter)
	provider := createCapableServiceProvider(t, service, aiStore)
	conversation, err := service.CreateConversation(t.Context(), "Accepted operation")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	turn, _, err := aiStore.StartTurn(t.Context(), conversation.ID, provider, "upgrade agent")
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	call, err := aiStore.CreateToolCall(t.Context(), turn, store.AIToolCall{
		ToolName: "upgrade_agent", Risk: "confirm", Status: "accepted", OperationID: "node-1",
		TargetType: "node", TargetID: "node-1", TargetName: "Node One", NodeID: "node-1",
	})
	if err != nil {
		t.Fatalf("create accepted call: %v", err)
	}
	if _, err := aiStore.CompleteTurn(t.Context(), turn, "操作已接受，正在处理中。"); err != nil {
		t.Fatalf("complete accepted turn: %v", err)
	}
	accepted, err := aiStore.ListAcceptedToolCalls(t.Context(), 10)
	if err != nil {
		t.Fatalf("list accepted calls: %v", err)
	}
	if len(accepted) != 1 || accepted[0].ID != call.ID || accepted[0].OperationID != "node-1" {
		t.Fatalf("accepted calls = %+v", accepted)
	}

	if err := service.verifyAcceptedOperation(t.Context(), call); err != nil {
		t.Fatalf("verify accepted operation: %v", err)
	}
	if err := service.verifyAcceptedOperation(t.Context(), call); err != nil {
		t.Fatalf("repeat verify accepted operation: %v", err)
	}

	completed, _, err := aiStore.GetToolCall(t.Context(), call.ID)
	if err != nil {
		t.Fatalf("get completed call: %v", err)
	}
	if completed.Status != "success" || completed.ResultSummary != "Agent 升级完成" || completed.OperationID != "node-1" {
		t.Fatalf("completed call = %+v", completed)
	}
	messages, err := aiStore.ListMessages(t.Context(), conversation.ID, 20)
	if err != nil {
		t.Fatalf("list conversation messages: %v", err)
	}
	if len(messages) != 3 || messages[2].Content != "Agent 升级完成。" {
		t.Fatalf("messages after accepted completion = %+v", messages)
	}
}

func TestAcceptedOperationVerifierContinuesAfterOneOperationError(t *testing.T) {
	evidenceErr := errors.New("reboot evidence unavailable")
	ops := acceptedOperationNodeStub{
		upgrade:   protocol.AgentUpgradeStatus{NodeID: "node-2", ActualVersion: version.Current, Stage: "idle"},
		rebootErr: evidenceErr,
	}
	registry := NewRegistry(RegistryDependencies{AgentOps: ops})
	adapter := serviceTestAdapter{complete: func(context.Context, ProviderCredential, ChatRequest) (ChatResponse, error) {
		return ChatResponse{Content: "unused"}, nil
	}}
	service, aiStore, database := newServiceTestFixture(t, registry, adapter)
	provider := createCapableServiceProvider(t, service, aiStore)

	firstConversation, err := service.CreateConversation(t.Context(), "Reboot evidence")
	if err != nil {
		t.Fatalf("create first conversation: %v", err)
	}
	firstTurn, _, err := aiStore.StartTurn(t.Context(), firstConversation.ID, provider, "reboot")
	if err != nil {
		t.Fatalf("start first turn: %v", err)
	}
	firstCall, err := aiStore.CreateToolCall(t.Context(), firstTurn, store.AIToolCall{
		ToolName: "reboot_node", Risk: "confirm", Status: "accepted", OperationID: "node-1",
		TargetType: "node", TargetID: "node-1", TargetName: "Node One", NodeID: "node-1",
	})
	if err != nil {
		t.Fatalf("create first call: %v", err)
	}
	if _, err := aiStore.CompleteTurn(t.Context(), firstTurn, "操作已接受，正在处理中。"); err != nil {
		t.Fatalf("complete first turn: %v", err)
	}

	secondConversation, err := service.CreateConversation(t.Context(), "Upgrade state")
	if err != nil {
		t.Fatalf("create second conversation: %v", err)
	}
	secondTurn, _, err := aiStore.StartTurn(t.Context(), secondConversation.ID, provider, "upgrade")
	if err != nil {
		t.Fatalf("start second turn: %v", err)
	}
	secondCall, err := aiStore.CreateToolCall(t.Context(), secondTurn, store.AIToolCall{
		ToolName: "upgrade_agent", Risk: "confirm", Status: "accepted", OperationID: "node-2",
		TargetType: "node", TargetID: "node-2", TargetName: "Node Two", NodeID: "node-2",
	})
	if err != nil {
		t.Fatalf("create second call: %v", err)
	}
	if _, err := aiStore.CompleteTurn(t.Context(), secondTurn, "操作已接受，正在处理中。"); err != nil {
		t.Fatalf("complete second turn: %v", err)
	}

	firstAt := time.Now().UTC().Add(-4 * time.Second)
	secondAt := firstAt.Add(time.Second)
	if _, err := database.Exec(`UPDATE ai_tool_calls SET updated_at = ? WHERE id = ?`, firstAt.Format(time.RFC3339Nano), firstCall.ID); err != nil {
		t.Fatalf("order first call: %v", err)
	}
	if _, err := database.Exec(`UPDATE ai_tool_calls SET updated_at = ? WHERE id = ?`, secondAt.Format(time.RFC3339Nano), secondCall.ID); err != nil {
		t.Fatalf("order second call: %v", err)
	}
	service.now = func() time.Time { return firstAt.Add(acceptedRebootGracePeriod + time.Second) }

	if err := service.verifyAcceptedOperations(t.Context()); !errors.Is(err, evidenceErr) {
		t.Fatalf("verify accepted operations error = %v, want %v", err, evidenceErr)
	}
	completedUpgrade, _, err := aiStore.GetToolCall(t.Context(), secondCall.ID)
	if err != nil {
		t.Fatalf("get completed upgrade: %v", err)
	}
	if completedUpgrade.Status != "success" || completedUpgrade.ResultSummary != "Agent 升级完成" {
		t.Fatalf("upgrade after earlier verifier error = %+v", completedUpgrade)
	}
}
