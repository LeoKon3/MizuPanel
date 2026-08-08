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
		BaseURL: "https://model.test/v1", Model: "model-a", Enabled: true, ProbeStatus: "unknown",
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
	if recoveredCall.ResultSummary != "服务重启，操作结果无法确认，可能已执行" {
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

func TestAIStoreToolPlanCreationClaimRejectAndRecoveryAreAtomic(t *testing.T) {
	database := openTestDB(t)
	database.SetMaxOpenConns(1)
	repo := NewAIStore(database, serverdb.DialectSQLite)
	provider, err := repo.CreateProvider(t.Context(), testAIProvider("provider-plan", "Plan Model"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	conversation, err := repo.CreateConversation(t.Context(), "Plan")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	turn, _, err := repo.StartTurn(t.Context(), conversation.ID, provider, "plan request")
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	step := func(id, name string) AIToolCall {
		return AIToolCall{ID: id, ProviderCallID: id, ToolName: name, Risk: "confirm", Status: "pending",
			ArgumentsJSON: `{}`, TargetType: "node", TargetID: name, TargetName: name, NodeID: name, ResultSummary: "planned"}
	}
	if _, err := repo.CreateToolPlan(t.Context(), turn, []AIToolCall{step("duplicate", "one"), step("duplicate", "two")}); err == nil {
		t.Fatal("duplicate step IDs did not roll back plan creation")
	}
	var callCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ai_tool_calls WHERE turn_id = ?`, turn.ID).Scan(&callCount); err != nil || callCount != 0 {
		t.Fatalf("tool rows after failed plan = %d err=%v", callCount, err)
	}
	currentTurn, err := repo.GetTurn(t.Context(), turn.ID)
	if err != nil || currentTurn.Status != "running" {
		t.Fatalf("turn after failed plan = %+v err=%v", currentTurn, err)
	}

	created, err := repo.CreateToolPlan(t.Context(), turn, []AIToolCall{step("step-1", "one"), step("step-2", "two")})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if len(created) != 2 || created[0].StepIndex != 0 || created[1].StepIndex != 1 {
		t.Fatalf("created plan steps = %+v", created)
	}
	claimed, claimedTurn, err := repo.ClaimToolPlan(t.Context(), turn.ID)
	if err != nil {
		t.Fatalf("claim plan: %v", err)
	}
	if claimedTurn.Status != "running" || claimed[0].Status != "queued" || claimed[1].Status != "queued" {
		t.Fatalf("claimed plan = turn:%+v steps:%+v", claimedTurn, claimed)
	}
	if _, _, err := repo.ClaimToolPlan(t.Context(), turn.ID); !errors.Is(err, ErrAIConflict) {
		t.Fatalf("repeat plan claim error = %v, want conflict", err)
	}
	if err := repo.TransitionToolPlanStep(t.Context(), created[0].ID, "queued", "running", "started", ""); err != nil {
		t.Fatalf("start first step: %v", err)
	}
	if err := repo.TransitionToolPlanStep(t.Context(), created[0].ID, "running", "accepted", "waiting", "operation-1"); err != nil {
		t.Fatalf("accept first step: %v", err)
	}
	if err := repo.RecoverInterrupted(t.Context()); err != nil {
		t.Fatalf("recover accepted plan: %v", err)
	}
	recovered, err := repo.ListPlanSteps(t.Context(), turn.ID)
	if err != nil {
		t.Fatalf("list recovered plan: %v", err)
	}
	recoveredTurn, err := repo.GetTurn(t.Context(), turn.ID)
	if err != nil || recoveredTurn.Status != "running" || recovered[0].Status != "accepted" || recovered[1].Status != "queued" {
		t.Fatalf("accepted recovery replay boundary = turn:%+v steps:%+v err=%v", recoveredTurn, recovered, err)
	}

	rejectConversation, err := repo.CreateConversation(t.Context(), "Reject plan")
	if err != nil {
		t.Fatalf("create reject conversation: %v", err)
	}
	rejectTurn, _, err := repo.StartTurn(t.Context(), rejectConversation.ID, provider, "reject request")
	if err != nil {
		t.Fatalf("start reject turn: %v", err)
	}
	if _, err := repo.CreateToolPlan(t.Context(), rejectTurn, []AIToolCall{step("reject-1", "reject-one"), step("reject-2", "reject-two")}); err != nil {
		t.Fatalf("create rejected plan: %v", err)
	}
	rejected, rejectedTurn, message, err := repo.RejectToolPlan(t.Context(), rejectTurn.ID, "cancelled")
	if err != nil {
		t.Fatalf("reject plan: %v", err)
	}
	if rejectedTurn.Status != "completed" || message.Content != "cancelled" || rejected[0].Status != "rejected" || rejected[1].Status != "rejected" {
		t.Fatalf("rejected plan = turn:%+v message:%+v steps:%+v", rejectedTurn, message, rejected)
	}
}

func TestAIStoreProviderModelsRoutingAndAtomicFallback(t *testing.T) {
	database := openTestDB(t)
	database.SetMaxOpenConns(1)
	repo := NewAIStore(database, serverdb.DialectSQLite)

	provider, err := repo.CreateProvider(t.Context(), testAIProvider("provider-routing", "Routing"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := repo.SaveModelProbe(t.Context(), provider.ID, true, true, "success", 12, "", time.Now().UTC()); err != nil {
		t.Fatalf("probe first model: %v", err)
	}
	created, err := repo.CreateProviderModels(t.Context(), provider.ID, []AIProviderModel{{
		ModelID: "model-b", DisplayName: "Fallback", Enabled: true, ProbeStatus: "unknown",
	}})
	if err != nil {
		t.Fatalf("create second model: %v", err)
	}
	if len(created) != 1 || created[0].ProviderID != provider.ID {
		t.Fatalf("created models = %+v", created)
	}
	fallback := created[0]
	if err := repo.SaveModelProbe(t.Context(), fallback.ID, true, true, "success", 18, "", time.Now().UTC()); err != nil {
		t.Fatalf("probe fallback model: %v", err)
	}
	defaultID, fallbackID := provider.ID, fallback.ID
	if err := repo.SetRouting(t.Context(), &defaultID, &fallbackID); err != nil {
		t.Fatalf("set routing: %v", err)
	}
	routing, err := repo.GetRouting(t.Context())
	if err != nil {
		t.Fatalf("get routing: %v", err)
	}
	if routing.DefaultModelID == nil || *routing.DefaultModelID != defaultID ||
		routing.FallbackModelID == nil || *routing.FallbackModelID != fallbackID {
		t.Fatalf("routing = %+v", routing)
	}
	if err := repo.SetRouting(t.Context(), &defaultID, &defaultID); !errors.Is(err, ErrAIInvalid) {
		t.Fatalf("same default/fallback error = %v, want ErrAIInvalid", err)
	}
	if _, err := database.Exec(`UPDATE ai_provider_models SET is_default = 1 WHERE id = ?`, fallbackID); err == nil {
		t.Fatal("database accepted a second global default model")
	}
	if err := repo.DeleteModel(t.Context(), fallbackID); !errors.Is(err, ErrAIConflict) {
		t.Fatalf("delete fallback model error = %v, want ErrAIConflict", err)
	}

	conversation, err := repo.CreateConversationWithModel(t.Context(), "Routing", &defaultID)
	if err != nil {
		t.Fatalf("create selected conversation: %v", err)
	}
	provider, err = repo.GetProvider(t.Context(), provider.ID)
	if err != nil {
		t.Fatalf("reload provider: %v", err)
	}
	first, err := repo.GetModel(t.Context(), defaultID)
	if err != nil {
		t.Fatalf("reload default model: %v", err)
	}
	fallback, err = repo.GetModel(t.Context(), fallbackID)
	if err != nil {
		t.Fatalf("reload fallback model: %v", err)
	}
	turn, _, err := repo.StartModelTurn(t.Context(), conversation.ID, provider, first, "use fallback")
	if err != nil {
		t.Fatalf("start fallback turn: %v", err)
	}
	switched, err := repo.SwitchTurnModelBeforeTools(t.Context(), turn.ID, provider, fallback)
	if err != nil {
		t.Fatalf("switch turn model: %v", err)
	}
	if !switched.FallbackUsed || switched.ModelID == nil || *switched.ModelID != fallbackID ||
		switched.RequestedModelID == nil || *switched.RequestedModelID != defaultID ||
		switched.Model != "model-b" || switched.RequestedModel != "model-a" {
		t.Fatalf("switched turn = %+v", switched)
	}
	if _, err := repo.CompleteTurn(t.Context(), switched, "fallback response"); err != nil {
		t.Fatalf("complete fallback turn: %v", err)
	}

	guarded, _, err := repo.StartModelTurn(t.Context(), conversation.ID, provider, first, "tool first")
	if err != nil {
		t.Fatalf("start guarded turn: %v", err)
	}
	if _, err := repo.CreateToolCall(t.Context(), guarded, AIToolCall{
		ProviderCallID: "read-call", ToolName: "list_nodes", Risk: "read", Status: "running",
		ArgumentsJSON: `{}`, TargetType: "system", TargetID: "nodes",
	}); err != nil {
		t.Fatalf("create tool call before fallback: %v", err)
	}
	if _, err := repo.SwitchTurnModelBeforeTools(t.Context(), guarded.ID, provider, fallback); !errors.Is(err, ErrAIConflict) {
		t.Fatalf("post-tool fallback error = %v, want ErrAIConflict", err)
	}

	if err := repo.SetRouting(t.Context(), nil, nil); err != nil {
		t.Fatalf("clear routing: %v", err)
	}
	if err := repo.DeleteModel(t.Context(), defaultID); err != nil {
		t.Fatalf("delete selected model: %v", err)
	}
	conversation, err = repo.GetConversation(t.Context(), conversation.ID)
	if err != nil {
		t.Fatalf("reload conversation after model delete: %v", err)
	}
	if conversation.ModelID != nil {
		t.Fatalf("conversation model after delete = %v, want nil", *conversation.ModelID)
	}
	guarded, err = repo.GetTurn(t.Context(), guarded.ID)
	if err != nil {
		t.Fatalf("reload historical turn: %v", err)
	}
	if guarded.ModelID != nil || guarded.RequestedModelID == nil || *guarded.RequestedModelID != defaultID ||
		guarded.RequestedModel != "model-a" {
		t.Fatalf("historical routing after model delete = %+v", guarded)
	}
}
