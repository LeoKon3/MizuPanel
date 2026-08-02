package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestServiceInitializeAllowsProvidersWithoutEncryptedSecrets(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	aiStore := store.NewAIStore(database, serverdb.DialectSQLite)
	if _, err := aiStore.CreateProvider(t.Context(), store.AIProvider{
		ID: "provider-no-key", Name: "No key", Protocol: ProtocolOpenAIChatCompletions,
		BaseURL: "http://model.internal/v1", Model: "model-a", ProbeStatus: "unknown",
	}); err != nil {
		t.Fatalf("create provider without secret: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "ai.key")
	service := NewService(aiStore, NewSecretManager(keyPath), NewRegistry(RegistryDependencies{}), map[string]Adapter{})
	if err := service.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize service without encrypted secrets: %v", err)
	}
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("generated key = info:%v err:%v", info, err)
	}
}

type serviceTestAdapter struct {
	complete func(context.Context, ProviderCredential, ChatRequest) (ChatResponse, error)
	probe    func(context.Context, ProviderCredential) (Capabilities, error)
	models   func(context.Context, ProviderCredential) ([]string, error)
}

func (a serviceTestAdapter) Complete(ctx context.Context, provider ProviderCredential, request ChatRequest) (ChatResponse, error) {
	return a.complete(ctx, provider, request)
}

func (a serviceTestAdapter) Probe(ctx context.Context, provider ProviderCredential) (Capabilities, error) {
	if a.probe == nil {
		return Capabilities{Chat: true, Tools: true}, nil
	}
	return a.probe(ctx, provider)
}

func (a serviceTestAdapter) ListModels(ctx context.Context, provider ProviderCredential) ([]string, error) {
	if a.models != nil {
		return a.models(ctx, provider)
	}
	return nil, nil
}

func newServiceTestFixture(t *testing.T, registry *Registry, adapter Adapter) (*Service, *store.AIStore, *sql.DB) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	if registry == nil {
		registry = NewRegistry(RegistryDependencies{})
	}
	secrets := NewSecretManager(filepath.Join(t.TempDir(), "ai.key"))
	if err := secrets.Initialize(0); err != nil {
		t.Fatalf("initialize secrets: %v", err)
	}
	aiStore := store.NewAIStore(database, serverdb.DialectSQLite)
	service := NewService(aiStore, secrets, registry, map[string]Adapter{ProtocolOpenAIChatCompletions: adapter})
	return service, aiStore, database
}

func createCapableServiceProvider(t *testing.T, service *Service, aiStore *store.AIStore) store.AIProvider {
	t.Helper()
	provider, err := service.CreateProvider(t.Context(), ProviderInput{
		Name: "Test Model", Protocol: ProtocolOpenAIChatCompletions,
		BaseURL: "https://model.test/v1", Model: "model-a",
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := aiStore.SaveProviderProbe(t.Context(), provider.ID, true, true, "success", "", provider.CreatedAt); err != nil {
		t.Fatalf("save provider probe: %v", err)
	}
	provider, err = aiStore.GetProvider(t.Context(), provider.ID)
	if err != nil {
		t.Fatalf("get capable provider: %v", err)
	}
	return provider
}

func TestServiceListModelsReusesSavedProviderCredential(t *testing.T) {
	const savedKey = "saved-provider-key-marker"
	adapter := serviceTestAdapter{
		complete: func(context.Context, ProviderCredential, ChatRequest) (ChatResponse, error) {
			return ChatResponse{}, nil
		},
		models: func(_ context.Context, credential ProviderCredential) ([]string, error) {
			if credential.BaseURL != "https://replacement.test/v1" || credential.APIKey != savedKey {
				t.Fatalf("credential = %+v", credential)
			}
			return []string{"model-a"}, nil
		},
	}
	service, _, _ := newServiceTestFixture(t, nil, adapter)
	provider, err := service.CreateProvider(t.Context(), ProviderInput{
		Name: "Saved Key", Protocol: ProtocolOpenAIChatCompletions,
		BaseURL: "https://model.test/v1", Model: "model-a", APIKey: savedKey,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	models, err := service.ListModels(t.Context(), provider.ID, "https://replacement.test/v1", "")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0] != "model-a" {
		t.Fatalf("models = %#v", models)
	}
}

func TestServiceCancellationFailsTurnWithoutAssistantMessage(t *testing.T) {
	entered := make(chan struct{})
	adapter := serviceTestAdapter{complete: func(ctx context.Context, _ ProviderCredential, _ ChatRequest) (ChatResponse, error) {
		close(entered)
		<-ctx.Done()
		return ChatResponse{}, ctx.Err()
	}}
	service, aiStore, database := newServiceTestFixture(t, nil, adapter)
	provider := createCapableServiceProvider(t, service, aiStore)
	conversation, err := service.CreateConversation(t.Context(), "Cancellation")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, sendErr := service.Send(ctx, conversation.ID, provider.ID, "cancel this request", nil)
		result <- sendErr
	}()
	<-entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Send error = %v, want context.Canceled", err)
	}

	var status, errorCode string
	if err := database.QueryRow(`SELECT status, error_code FROM ai_turns WHERE conversation_id = ?`, conversation.ID).Scan(&status, &errorCode); err != nil {
		t.Fatalf("query cancelled turn: %v", err)
	}
	if status != "failed" || errorCode != "cancelled" {
		t.Fatalf("cancelled turn = status %q error %q", status, errorCode)
	}
	var assistantMessages int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ai_messages WHERE conversation_id = ? AND role = 'assistant'`, conversation.ID).Scan(&assistantMessages); err != nil {
		t.Fatalf("count assistant messages: %v", err)
	}
	if assistantMessages != 0 {
		t.Fatalf("assistant messages after cancellation = %d, want 0", assistantMessages)
	}
}

func TestServiceConnectionUpdateInvalidatesCapabilitiesAndDefault(t *testing.T) {
	var completeCalls atomic.Int32
	adapter := serviceTestAdapter{complete: func(context.Context, ProviderCredential, ChatRequest) (ChatResponse, error) {
		completeCalls.Add(1)
		return ChatResponse{Content: "unexpected"}, nil
	}}
	service, aiStore, _ := newServiceTestFixture(t, nil, adapter)
	provider := createCapableServiceProvider(t, service, aiStore)
	if err := service.SetDefaultProvider(t.Context(), provider.ID); err != nil {
		t.Fatalf("set default provider: %v", err)
	}

	updated, err := service.UpdateProvider(t.Context(), provider.ID, ProviderUpdate{
		Name: provider.Name, Protocol: provider.Protocol, BaseURL: provider.BaseURL, Model: "model-b",
	})
	if err != nil {
		t.Fatalf("update provider connection: %v", err)
	}
	if updated.ChatCapable || updated.ToolsCapable || updated.Default || updated.ProbeStatus != "unknown" || updated.ProbedAt != nil {
		t.Fatalf("connection update retained stale capability state: %+v", updated)
	}
	conversation, err := service.CreateConversation(t.Context(), "Capabilities")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := service.Send(t.Context(), conversation.ID, provider.ID, "hello", nil); !errors.Is(err, ErrProviderCapability) {
		t.Fatalf("Send error = %v, want ErrProviderCapability", err)
	}
	if completeCalls.Load() != 0 {
		t.Fatalf("adapter calls = %d, want 0", completeCalls.Load())
	}
}

func TestServiceRepeatedConfirmationExecutesToolOnce(t *testing.T) {
	var executions atomic.Int32
	registry := &Registry{tools: make(map[string]registeredTool)}
	definition := noArgumentDefinition("test_confirm", "Test confirmation")
	registry.add(registeredTool{
		definition: definition,
		risk:       RiskConfirm,
		validate: func(_ context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			if err := strictArguments(raw, &struct{}{}); err != nil {
				return nil, ToolTarget{}, ErrInvalidArguments
			}
			return json.RawMessage(`{}`), ToolTarget{Type: "node", ID: "node-1", Name: "Node One", NodeID: "node-1"}, nil
		},
		execute: func(context.Context, json.RawMessage) (SafeToolResult, error) {
			executions.Add(1)
			return SafeToolResult{Summary: "executed"}, nil
		},
	})
	adapter := serviceTestAdapter{complete: func(context.Context, ProviderCredential, ChatRequest) (ChatResponse, error) {
		return ChatResponse{ToolCalls: []ToolCall{{ID: "call-1", Name: definition.Name, Arguments: json.RawMessage(`{}`)}}}, nil
	}}
	service, aiStore, _ := newServiceTestFixture(t, registry, adapter)
	provider := createCapableServiceProvider(t, service, aiStore)
	conversation, err := service.CreateConversation(t.Context(), "Exactly once")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	proposal, err := service.Send(t.Context(), conversation.ID, provider.ID, "run it", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if proposal.ToolCall == nil || proposal.ToolCall.Status != "pending" {
		t.Fatalf("proposal = %+v, want pending tool call", proposal)
	}
	confirmed, err := service.Confirm(t.Context(), proposal.ToolCall.ID, nil)
	if err != nil {
		t.Fatalf("first Confirm: %v", err)
	}
	if confirmed.ToolCall == nil || confirmed.ToolCall.Status != "success" {
		t.Fatalf("confirmed result = %+v", confirmed)
	}
	if _, err := service.Confirm(t.Context(), proposal.ToolCall.ID, nil); !errors.Is(err, store.ErrAIConflict) {
		t.Fatalf("second Confirm error = %v, want ErrAIConflict", err)
	}
	if executions.Load() != 1 {
		t.Fatalf("tool executions = %d, want 1", executions.Load())
	}
}
