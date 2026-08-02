package store

import (
	"errors"
	"testing"
	"time"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

func testAIProvider(id, name string) AIProvider {
	return AIProvider{
		ID: id, Name: name, Protocol: "openai_chat_completions",
		BaseURL: "https://model.test/v1", Model: "model-a", ProbeStatus: "unknown",
	}
}

func TestAIStoreProviderCRUDConflictDefaultAndEmptyList(t *testing.T) {
	database := openTestDB(t)
	database.SetMaxOpenConns(1)
	repo := NewAIStore(database, serverdb.DialectSQLite)

	empty, err := repo.ListProviders(t.Context())
	if err != nil {
		t.Fatalf("list empty providers: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty providers = %#v, want non-nil empty slice", empty)
	}
	if count, err := repo.ProviderSecretCount(t.Context()); err != nil || count != 0 {
		t.Fatalf("empty provider secret count = %d, %v", count, err)
	}

	first := testAIProvider("provider-1", "Primary Model")
	first.APIKeyCiphertext = "v1:ciphertext-marker"
	first, err = repo.CreateProvider(t.Context(), first)
	if err != nil {
		t.Fatalf("create first provider: %v", err)
	}
	if !first.HasAPIKey {
		t.Fatal("created provider did not project has_api_key")
	}
	if count, err := repo.ProviderSecretCount(t.Context()); err != nil || count != 1 {
		t.Fatalf("provider secret count = %d, %v; want 1", count, err)
	}
	if _, err := repo.CreateProvider(t.Context(), testAIProvider("provider-duplicate", " primary model ")); !errors.Is(err, ErrAIConflict) {
		t.Fatalf("duplicate provider error = %v, want ErrAIConflict", err)
	}
	probedAt := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if err := repo.SaveProviderProbe(t.Context(), first.ID, true, true, "success", "", probedAt); err != nil {
		t.Fatalf("save first probe: %v", err)
	}
	if err := repo.SetDefaultProvider(t.Context(), first.ID); err != nil {
		t.Fatalf("set first default: %v", err)
	}

	second := testAIProvider("provider-2", "Secondary Model")
	second.ChatCapable, second.ToolsCapable, second.ProbeStatus = true, true, "success"
	second, err = repo.CreateProvider(t.Context(), second)
	if err != nil {
		t.Fatalf("create second provider: %v", err)
	}
	if err := repo.SetDefaultProvider(t.Context(), second.ID); err != nil {
		t.Fatalf("set second default: %v", err)
	}
	if count, err := repo.ProviderSecretCount(t.Context()); err != nil || count != 1 {
		t.Fatalf("provider secret count with unauthenticated provider = %d, %v; want 1", count, err)
	}
	first, err = repo.GetProvider(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("get first provider: %v", err)
	}
	second, err = repo.GetProvider(t.Context(), second.ID)
	if err != nil {
		t.Fatalf("get second provider: %v", err)
	}
	if first.Default || !second.Default {
		t.Fatalf("default projection = first:%v second:%v", first.Default, second.Default)
	}
	if first.ProbedAt == nil || !first.ProbedAt.Equal(probedAt) || !first.ChatCapable || !first.ToolsCapable {
		t.Fatalf("first probe projection = %+v", first)
	}

	second.Name = first.Name
	if _, err := repo.UpdateProvider(t.Context(), second); !errors.Is(err, ErrAIConflict) {
		t.Fatalf("conflicting update error = %v, want ErrAIConflict", err)
	}
	if err := repo.DeleteProvider(t.Context(), first.ID); err != nil {
		t.Fatalf("delete first provider: %v", err)
	}
	if err := repo.DeleteProvider(t.Context(), first.ID); !errors.Is(err, ErrAINotFound) {
		t.Fatalf("repeat provider delete error = %v, want ErrAINotFound", err)
	}
}

func TestAIStoreTurnConflictClaimRecoveryAndConversationCascade(t *testing.T) {
	database := openTestDB(t)
	database.SetMaxOpenConns(1)
	repo := NewAIStore(database, serverdb.DialectSQLite)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO nodes (id, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "node-1", "Node One", "online", now, now); err != nil {
		t.Fatalf("insert unrelated node: %v", err)
	}
	provider, err := repo.CreateProvider(t.Context(), testAIProvider("provider-1", "Model"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	conversation, err := repo.CreateConversation(t.Context(), "Operations")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	turn, _, err := repo.StartTurn(t.Context(), conversation.ID, provider, "first request")
	if err != nil {
		t.Fatalf("start first turn: %v", err)
	}
	if _, _, err := repo.StartTurn(t.Context(), conversation.ID, provider, "must roll back"); !errors.Is(err, ErrAIConflict) {
		t.Fatalf("second active turn error = %v, want ErrAIConflict", err)
	}
	messages, err := repo.ListMessages(t.Context(), conversation.ID, 50)
	if err != nil {
		t.Fatalf("list messages after conflict: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "first request" {
		t.Fatalf("messages after conflict = %#v", messages)
	}

	call, err := repo.CreateToolCall(t.Context(), turn, AIToolCall{
		ProviderCallID: "upstream-call", ToolName: "reboot_node", Risk: "confirm", Status: "pending",
		ArgumentsJSON: `{"node_id":"node-1"}`, TargetType: "node", TargetID: "node-1", TargetName: "Node One", NodeID: "node-1",
	})
	if err != nil {
		t.Fatalf("create pending tool call: %v", err)
	}
	claimed, claimedTurn, err := repo.ClaimToolCall(t.Context(), call.ID)
	if err != nil {
		t.Fatalf("claim pending tool call: %v", err)
	}
	if claimed.Status != "running" || claimedTurn.Status != "awaiting_confirmation" {
		t.Fatalf("claimed state = call:%+v turn:%+v", claimed, claimedTurn)
	}
	if _, _, err := repo.ClaimToolCall(t.Context(), call.ID); !errors.Is(err, ErrAIConflict) {
		t.Fatalf("repeat claim error = %v, want ErrAIConflict", err)
	}

	if err := repo.RecoverInterrupted(t.Context()); err != nil {
		t.Fatalf("recover interrupted state: %v", err)
	}
	recoveredCall, recoveredTurn, err := repo.GetToolCall(t.Context(), call.ID)
	if err != nil {
		t.Fatalf("get recovered call: %v", err)
	}
	if recoveredCall.Status != "interrupted" || recoveredTurn.Status != "interrupted" || recoveredTurn.ErrorCode != "server_restarted" {
		t.Fatalf("recovered state = call:%+v turn:%+v", recoveredCall, recoveredTurn)
	}
	if recoveredCall.ResultSummary != "服务重启，操作未执行" {
		t.Fatalf("recovered summary = %q", recoveredCall.ResultSummary)
	}

	secondTurn, _, err := repo.StartTurn(t.Context(), conversation.ID, provider, "second request")
	if err != nil {
		t.Fatalf("start turn after recovery: %v", err)
	}
	if _, err := repo.CompleteTurn(t.Context(), secondTurn, "completed response"); err != nil {
		t.Fatalf("complete second turn: %v", err)
	}
	messages, err = repo.ListMessages(t.Context(), conversation.ID, 50)
	if err != nil {
		t.Fatalf("list completed messages: %v", err)
	}
	if len(messages) != 3 || messages[1].Content != "second request" || messages[2].Role != "assistant" {
		t.Fatalf("completed message history = %#v", messages)
	}

	if err := repo.DeleteConversation(t.Context(), conversation.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	for _, table := range []string{"ai_conversations", "ai_turns", "ai_messages", "ai_tool_calls"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after conversation delete = %d, want 0", table, count)
		}
	}
	var nodes int
	if err := database.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = 'node-1'`).Scan(&nodes); err != nil {
		t.Fatalf("count unrelated node: %v", err)
	}
	if nodes != 1 {
		t.Fatalf("unrelated node count = %d, want 1", nodes)
	}
}
