package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	serverai "github.com/mizupanel/mizupanel/internal/server/ai"
	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type aiAPITestAdapter struct {
	capabilities serverai.Capabilities
	probeErr     error
	response     serverai.ChatResponse
	completeErr  error
	complete     func(context.Context, serverai.ProviderCredential, serverai.ChatRequest) (serverai.ChatResponse, error)
	listModels   func(context.Context, serverai.ProviderCredential) ([]string, error)
}

type aiAPIStreamingAdapter struct {
	aiAPITestAdapter
	stream func(context.Context, serverai.ProviderCredential, serverai.ChatRequest, serverai.ContentCallback) (serverai.ChatResponse, error)
}

func (a aiAPIStreamingAdapter) CompleteStream(ctx context.Context, provider serverai.ProviderCredential, request serverai.ChatRequest, callback serverai.ContentCallback) (serverai.ChatResponse, error) {
	return a.stream(ctx, provider, request, callback)
}

func (a aiAPITestAdapter) Complete(ctx context.Context, provider serverai.ProviderCredential, request serverai.ChatRequest) (serverai.ChatResponse, error) {
	if a.complete != nil {
		return a.complete(ctx, provider, request)
	}
	return a.response, a.completeErr
}

func (a aiAPITestAdapter) Probe(context.Context, serverai.ProviderCredential) (serverai.Capabilities, error) {
	return a.capabilities, a.probeErr
}
func (a aiAPITestAdapter) ListModels(ctx context.Context, provider serverai.ProviderCredential) ([]string, error) {
	if a.listModels != nil {
		return a.listModels(ctx, provider)
	}
	return []string{"test-model"}, nil
}

type aiAPIFixture struct {
	handler http.Handler
	db      *sql.DB
}

func newAIAPIFixture(t *testing.T, auth AuthConfig, adapter serverai.Adapter) aiAPIFixture {
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
	nodes := store.NewNodeStore(database)
	registry := serverai.NewRegistry(serverai.RegistryDependencies{Nodes: nodes})
	secrets := serverai.NewSecretManager(filepath.Join(t.TempDir(), "ai.key"))
	aiStore := store.NewAIStore(database, serverdb.DialectSQLite)
	service := serverai.NewService(aiStore, secrets, registry, map[string]serverai.Adapter{
		serverai.ProtocolOpenAIChatCompletions: adapter,
	})
	if err := service.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize AI service: %v", err)
	}
	auditStore := serveraudit.NewStore(database, serverdb.DialectSQLite)
	router := NewRouter(nodes, store.NewMetricStore(database), auth, auditStore, AIConfig{Service: service})
	return aiAPIFixture{handler: serveraudit.Middleware(auditStore, router), db: database}
}

func performAIRawRequest(handler http.Handler, method, path, body, contentType, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://panel.test"+path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestAIProviderAPISecretProjectionCapabilityAndAuditBoundary(t *testing.T) {
	completeCalls := 0
	adapter := aiAPITestAdapter{
		capabilities: serverai.Capabilities{Chat: true, Tools: true},
		complete: func(context.Context, serverai.ProviderCredential, serverai.ChatRequest) (serverai.ChatResponse, error) {
			completeCalls++
			if completeCalls == 1 {
				return serverai.ChatResponse{ToolCalls: []serverai.ToolCall{{ID: "call-1", Name: "list_nodes", Arguments: json.RawMessage(`{}`)}}}, nil
			}
			return serverai.ChatResponse{Content: "assistant response"}, nil
		},
	}
	fixture := newAIAPIFixture(t, AuthConfig{}, adapter)

	empty := performAIRawRequest(fixture.handler, http.MethodGet, "/api/ai/providers", "", "", "")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != `{"providers":[]}` {
		t.Fatalf("empty providers = %d %s", empty.Code, empty.Body.String())
	}

	const apiKeyMarker = "provider-api-key-secret-marker"
	created := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers",
		`{"name":"Primary","protocol":"openai_chat_completions","base_url":"https://model.test/v1","model":"model-a","api_key":"`+apiKeyMarker+`"}`,
		"application/json", "http://panel.test")
	if created.Code != http.StatusCreated {
		t.Fatalf("create provider = %d %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), apiKeyMarker) || strings.Contains(created.Body.String(), "api_key_ciphertext") || strings.Contains(created.Body.String(), `"api_key"`) {
		t.Fatalf("provider response exposed secret material: %s", created.Body.String())
	}
	var provider store.AIProvider
	if err := json.NewDecoder(created.Body).Decode(&provider); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	if provider.ID == "" || !provider.HasAPIKey {
		t.Fatalf("created provider projection = %+v", provider)
	}
	var ciphertext string
	if err := fixture.db.QueryRow(`SELECT api_key_ciphertext FROM ai_providers WHERE id = ?`, provider.ID).Scan(&ciphertext); err != nil {
		t.Fatalf("query provider ciphertext: %v", err)
	}
	if !strings.HasPrefix(ciphertext, "v1:") || strings.Contains(ciphertext, apiKeyMarker) {
		t.Fatalf("stored ciphertext is not a safe v1 projection: %q", ciphertext)
	}

	probe := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers/"+provider.ID+"/test", "", "", "http://panel.test")
	if probe.Code != http.StatusOK || strings.Contains(probe.Body.String(), apiKeyMarker) {
		t.Fatalf("test provider = %d %s", probe.Code, probe.Body.String())
	}
	makeDefault := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers/"+provider.ID+"/default", "", "", "http://panel.test")
	if makeDefault.Code != http.StatusOK || !strings.Contains(makeDefault.Body.String(), `"is_default":true`) {
		t.Fatalf("default provider = %d %s", makeDefault.Code, makeDefault.Body.String())
	}

	conversationResponse := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations",
		`{"title":"Audit boundary"}`, "application/json", "http://panel.test")
	if conversationResponse.Code != http.StatusCreated {
		t.Fatalf("create conversation = %d %s", conversationResponse.Code, conversationResponse.Body.String())
	}
	var conversation store.AIConversation
	if err := json.NewDecoder(conversationResponse.Body).Decode(&conversation); err != nil {
		t.Fatalf("decode conversation: %v", err)
	}
	const promptMarker = "ordinary-prompt-must-not-enter-audit"
	messageResponse := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations/"+conversation.ID+"/messages",
		`{"provider_id":"`+provider.ID+`","content":"`+promptMarker+`"}`, "application/json", "http://panel.test")
	if messageResponse.Code != http.StatusOK || !strings.Contains(messageResponse.Body.String(), "assistant response") {
		t.Fatalf("send message = %d %s", messageResponse.Code, messageResponse.Body.String())
	}

	rows, err := fixture.db.Query(`SELECT action, target_id, target_name, summary, metadata_json FROM audit_events WHERE module = 'ai' ORDER BY id`)
	if err != nil {
		t.Fatalf("query AI audit events: %v", err)
	}
	defer rows.Close()
	var auditText strings.Builder
	actions := make(map[string]bool)
	for rows.Next() {
		var action, targetID, targetName, summary, metadata string
		if err := rows.Scan(&action, &targetID, &targetName, &summary, &metadata); err != nil {
			t.Fatalf("scan AI audit event: %v", err)
		}
		actions[action] = true
		auditText.WriteString(action + targetID + targetName + summary + metadata)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate AI audit events: %v", err)
	}
	for _, action := range []string{"provider_create", "provider_test", "provider_default", "tool_query"} {
		if !actions[action] {
			t.Errorf("missing AI audit action %q in %#v", action, actions)
		}
	}
	if strings.Contains(auditText.String(), apiKeyMarker) || strings.Contains(auditText.String(), promptMarker) || strings.Contains(auditText.String(), ciphertext) {
		t.Fatalf("AI audit events exposed forbidden content: %s", auditText.String())
	}
}

func TestAIAPIAuthenticationOriginBodyMethodAndErrorContracts(t *testing.T) {
	adapter := aiAPITestAdapter{capabilities: serverai.Capabilities{Chat: true, Tools: true}, response: serverai.ChatResponse{Content: "ok"}}
	protected := newAIAPIFixture(t, AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour}, adapter)
	unauthenticated := performAIRawRequest(protected.handler, http.MethodGet, "/api/ai/providers", "", "", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated providers = %d %s", unauthenticated.Code, unauthenticated.Body.String())
	}

	fixture := newAIAPIFixture(t, AuthConfig{}, adapter)
	method := performAIRawRequest(fixture.handler, http.MethodPatch, "/api/ai/providers", "", "", "http://panel.test")
	if method.Code != http.StatusMethodNotAllowed || !strings.Contains(method.Header().Get("Allow"), "GET") || !strings.Contains(method.Header().Get("Allow"), "POST") {
		t.Fatalf("unsupported method = %d Allow=%q body=%s", method.Code, method.Header().Get("Allow"), method.Body.String())
	}
	crossOrigin := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers", `{}`, "application/json", "https://evil.test")
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin create = %d %s", crossOrigin.Code, crossOrigin.Body.String())
	}
	wrongMedia := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers", `{}`, "text/plain", "http://panel.test")
	if wrongMedia.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("wrong content type = %d %s", wrongMedia.Code, wrongMedia.Body.String())
	}
	unknown := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers",
		`{"name":"A","protocol":"openai_chat_completions","base_url":"https://model.test","model":"m","secret_field":"must-not-leak"}`,
		"application/json", "http://panel.test")
	if unknown.Code != http.StatusBadRequest || strings.Contains(unknown.Body.String(), "secret_field") {
		t.Fatalf("unknown JSON field = %d %s", unknown.Code, unknown.Body.String())
	}
	trailing := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers", `{} {}`, "application/json", "http://panel.test")
	if trailing.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON = %d %s", trailing.Code, trailing.Body.String())
	}
	oversized := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers",
		`{"name":"`+strings.Repeat("x", maxAIRequestBodyBytes)+`"}`, "application/json", "http://panel.test")
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d %s", oversized.Code, oversized.Body.String())
	}

	validBody := `{"name":"Primary","protocol":"openai_chat_completions","base_url":"https://model.test","model":"m"}`
	created := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers", validBody, "application/json", "http://panel.test")
	if created.Code != http.StatusCreated {
		t.Fatalf("valid create = %d %s", created.Code, created.Body.String())
	}
	duplicate := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers", validBody, "application/json", "http://panel.test")
	if duplicate.Code != http.StatusConflict || strings.Contains(duplicate.Body.String(), "UNIQUE") {
		t.Fatalf("duplicate create = %d %s", duplicate.Code, duplicate.Body.String())
	}
	missing := performAIRawRequest(fixture.handler, http.MethodGet, "/api/ai/providers/missing", "", "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing provider = %d %s", missing.Code, missing.Body.String())
	}
}

func TestAIProviderModelsAPIUsesTransientCredentialsWithoutEchoingSecrets(t *testing.T) {
	const keyMarker = "transient-model-list-key"
	adapter := aiAPITestAdapter{listModels: func(_ context.Context, credential serverai.ProviderCredential) ([]string, error) {
		if credential.BaseURL != "https://model.test/v1" || credential.APIKey != keyMarker || credential.Protocol != serverai.ProtocolOpenAIChatCompletions {
			t.Fatalf("credential = %+v", credential)
		}
		return []string{"model-a", "model-b"}, nil
	}}
	fixture := newAIAPIFixture(t, AuthConfig{}, adapter)

	response := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers/models",
		`{"provider_id":"","base_url":"https://model.test/v1","api_key":"`+keyMarker+`"}`, "application/json", "http://panel.test")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"models":["model-a","model-b"]}` {
		t.Fatalf("list models = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), keyMarker) {
		t.Fatalf("model list response exposed API key: %s", response.Body.String())
	}

	method := performAIRawRequest(fixture.handler, http.MethodGet, "/api/ai/providers/models", "", "", "")
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("method contract = %d Allow=%q", method.Code, method.Header().Get("Allow"))
	}
	unknown := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers/models",
		`{"base_url":"https://model.test/v1","api_key":"`+keyMarker+`","extra":"secret-marker"}`, "application/json", "http://panel.test")
	if unknown.Code != http.StatusBadRequest || strings.Contains(unknown.Body.String(), "secret-marker") || strings.Contains(unknown.Body.String(), keyMarker) {
		t.Fatalf("strict request = %d %s", unknown.Code, unknown.Body.String())
	}
}

func TestAIProviderModelRoutingAndConversationSelectionAPI(t *testing.T) {
	const keyMarker = "saved-routing-key-marker"
	adapter := aiAPITestAdapter{
		capabilities: serverai.Capabilities{Chat: true, Tools: true},
		response:     serverai.ChatResponse{Content: "ok"},
		listModels: func(_ context.Context, credential serverai.ProviderCredential) ([]string, error) {
			if credential.APIKey != keyMarker || credential.Model != "" {
				t.Fatalf("discovery credential = %+v", credential)
			}
			return []string{"model-a", "model-b"}, nil
		},
	}
	fixture := newAIAPIFixture(t, AuthConfig{}, adapter)

	created := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers",
		`{"name":"Routing","protocol":"openai_chat_completions","base_url":"https://model.test/v1","model":"","api_key":"`+keyMarker+`"}`,
		"application/json", "http://panel.test")
	if created.Code != http.StatusCreated || strings.Contains(created.Body.String(), keyMarker) {
		t.Fatalf("create connection provider = %d %s", created.Code, created.Body.String())
	}
	var provider store.AIProvider
	if err := json.NewDecoder(created.Body).Decode(&provider); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	if provider.ID == "" || len(provider.Models) != 0 {
		t.Fatalf("connection provider = %+v", provider)
	}

	discovered := performAIRawRequest(fixture.handler, http.MethodPost,
		"/api/ai/providers/"+provider.ID+"/discover", "", "", "http://panel.test")
	if discovered.Code != http.StatusOK || !strings.Contains(discovered.Body.String(), `"models":["model-a","model-b"]`) ||
		strings.Contains(discovered.Body.String(), keyMarker) {
		t.Fatalf("discover saved provider = %d %s", discovered.Code, discovered.Body.String())
	}
	importedResponse := performAIRawRequest(fixture.handler, http.MethodPost,
		"/api/ai/providers/"+provider.ID+"/models",
		`{"models":[{"model_id":"model-a","display_name":"Primary"},{"model_id":"model-b","display_name":"Fallback"}],"enabled":true}`,
		"application/json", "http://panel.test")
	if importedResponse.Code != http.StatusCreated {
		t.Fatalf("import provider models = %d %s", importedResponse.Code, importedResponse.Body.String())
	}
	var imported struct {
		Models []store.AIProviderModel `json:"models"`
	}
	if err := json.NewDecoder(importedResponse.Body).Decode(&imported); err != nil || len(imported.Models) != 2 {
		t.Fatalf("decode imported models = %+v err=%v", imported, err)
	}
	defaultModel, fallbackModel := imported.Models[0], imported.Models[1]

	unverifiedRouting := performAIRawRequest(fixture.handler, http.MethodPut, "/api/ai/routing",
		`{"default_model_id":"`+defaultModel.ID+`","fallback_model_id":"`+fallbackModel.ID+`"}`,
		"application/json", "http://panel.test")
	if unverifiedRouting.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unverified routing = %d %s", unverifiedRouting.Code, unverifiedRouting.Body.String())
	}
	for _, model := range imported.Models {
		probe := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/models/"+model.ID+"/test", "", "", "http://panel.test")
		if probe.Code != http.StatusOK || !strings.Contains(probe.Body.String(), `"probe_status":"success"`) {
			t.Fatalf("probe model %s = %d %s", model.ID, probe.Code, probe.Body.String())
		}
	}
	routing := performAIRawRequest(fixture.handler, http.MethodPut, "/api/ai/routing",
		`{"default_model_id":"`+defaultModel.ID+`","fallback_model_id":"`+fallbackModel.ID+`"}`,
		"application/json", "http://panel.test")
	if routing.Code != http.StatusOK || !strings.Contains(routing.Body.String(), defaultModel.ID) || !strings.Contains(routing.Body.String(), fallbackModel.ID) {
		t.Fatalf("set routing = %d %s", routing.Code, routing.Body.String())
	}
	readRouting := performAIRawRequest(fixture.handler, http.MethodGet, "/api/ai/routing", "", "", "")
	if readRouting.Code != http.StatusOK || readRouting.Body.String() != routing.Body.String() {
		t.Fatalf("read routing = %d %s, want %s", readRouting.Code, readRouting.Body.String(), routing.Body.String())
	}

	createdConversation := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations",
		`{"title":"Persist selection"}`, "application/json", "http://panel.test")
	if createdConversation.Code != http.StatusCreated {
		t.Fatalf("create default conversation = %d %s", createdConversation.Code, createdConversation.Body.String())
	}
	var conversation store.AIConversation
	if err := json.NewDecoder(createdConversation.Body).Decode(&conversation); err != nil {
		t.Fatalf("decode conversation: %v", err)
	}
	if conversation.ModelID == nil || *conversation.ModelID != defaultModel.ID {
		t.Fatalf("new conversation selection = %+v", conversation)
	}
	selected := performAIRawRequest(fixture.handler, http.MethodPatch, "/api/ai/conversations/"+conversation.ID,
		`{"model_id":"`+fallbackModel.ID+`"}`, "application/json", "http://panel.test")
	if selected.Code != http.StatusOK || !strings.Contains(selected.Body.String(), fallbackModel.ID) {
		t.Fatalf("persist fallback selection = %d %s", selected.Code, selected.Body.String())
	}

	disableSpecial := performAIRawRequest(fixture.handler, http.MethodPut, "/api/ai/models/"+fallbackModel.ID,
		`{"model_id":"model-b","display_name":"Fallback","enabled":false}`,
		"application/json", "http://panel.test")
	if disableSpecial.Code != http.StatusConflict {
		t.Fatalf("disable routed fallback = %d %s", disableSpecial.Code, disableSpecial.Body.String())
	}
	clearFallback := performAIRawRequest(fixture.handler, http.MethodPut, "/api/ai/routing",
		`{"default_model_id":"`+defaultModel.ID+`","fallback_model_id":null}`,
		"application/json", "http://panel.test")
	if clearFallback.Code != http.StatusOK {
		t.Fatalf("clear fallback = %d %s", clearFallback.Code, clearFallback.Body.String())
	}
	disabled := performAIRawRequest(fixture.handler, http.MethodPut, "/api/ai/models/"+fallbackModel.ID,
		`{"model_id":"model-b","display_name":"Fallback","enabled":false}`,
		"application/json", "http://panel.test")
	if disabled.Code != http.StatusOK || !strings.Contains(disabled.Body.String(), `"enabled":false`) {
		t.Fatalf("disable fallback model = %d %s", disabled.Code, disabled.Body.String())
	}
	reselectDisabled := performAIRawRequest(fixture.handler, http.MethodPatch, "/api/ai/conversations/"+conversation.ID,
		`{"model_id":"`+fallbackModel.ID+`"}`, "application/json", "http://panel.test")
	if reselectDisabled.Code != http.StatusUnprocessableEntity {
		t.Fatalf("select disabled model = %d %s", reselectDisabled.Code, reselectDisabled.Body.String())
	}
	deleted := performAIRawRequest(fixture.handler, http.MethodDelete, "/api/ai/models/"+fallbackModel.ID, "", "", "http://panel.test")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete selected model = %d %s", deleted.Code, deleted.Body.String())
	}
	state := performAIRawRequest(fixture.handler, http.MethodGet, "/api/ai/conversations/"+conversation.ID, "", "", "")
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), `"model_id":null`) {
		t.Fatalf("conversation after selected model delete = %d %s", state.Code, state.Body.String())
	}
	deleteDefault := performAIRawRequest(fixture.handler, http.MethodDelete, "/api/ai/models/"+defaultModel.ID, "", "", "http://panel.test")
	if deleteDefault.Code != http.StatusConflict {
		t.Fatalf("delete default model = %d %s", deleteDefault.Code, deleteDefault.Body.String())
	}

	strict := performAIRawRequest(fixture.handler, http.MethodPut, "/api/ai/routing",
		`{"default_model_id":null,"fallback_model_id":null,"api_key":"`+keyMarker+`"}`,
		"application/json", "http://panel.test")
	if strict.Code != http.StatusBadRequest || strings.Contains(strict.Body.String(), keyMarker) {
		t.Fatalf("strict routing request = %d %s", strict.Code, strict.Body.String())
	}
	method := performAIRawRequest(fixture.handler, http.MethodGet, "/api/ai/models/"+defaultModel.ID+"/test", "", "", "")
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("model test method = %d Allow=%q", method.Code, method.Header().Get("Allow"))
	}

	var auditText string
	if err := fixture.db.QueryRow(`SELECT COALESCE(GROUP_CONCAT(metadata_json, ''), '') FROM audit_events WHERE module = 'ai'`).Scan(&auditText); err != nil {
		t.Fatalf("query AI audit metadata: %v", err)
	}
	if strings.Contains(auditText, keyMarker) || strings.Contains(auditText, "https://model.test") {
		t.Fatalf("AI routing audit exposed connection secret: %s", auditText)
	}
}

func TestAIErrorStatusMappingIsStableAndSanitized(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid", err: store.ErrAIInvalid, status: http.StatusBadRequest},
		{name: "missing", err: store.ErrAINotFound, status: http.StatusNotFound},
		{name: "conflict", err: store.ErrAIConflict, status: http.StatusConflict},
		{name: "capability", err: serverai.ErrProviderCapability, status: http.StatusUnprocessableEntity},
		{name: "timeout", err: &serverai.AdapterError{Kind: serverai.ErrorTimeout, Message: "模型请求超时"}, status: http.StatusGatewayTimeout},
		{name: "upstream", err: &serverai.AdapterError{Kind: serverai.ErrorUpstream, Message: "模型服务不可用"}, status: http.StatusBadGateway},
		{name: "unknown", err: errors.New("database-secret-marker"), status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeAIError(recorder, test.err)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.status, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "database-secret-marker") {
				t.Fatalf("error response leaked internal error: %s", recorder.Body.String())
			}
		})
	}
}
func TestAIConversationMessageStreamEmitsSafeProgressEvents(t *testing.T) {
	completeCalls := 0
	adapter := aiAPITestAdapter{
		capabilities: serverai.Capabilities{Chat: true, Tools: true},
		complete: func(ctx context.Context, _ serverai.ProviderCredential, request serverai.ChatRequest) (serverai.ChatResponse, error) {
			completeCalls++
			if completeCalls == 1 {
				return serverai.ChatResponse{ToolCalls: []serverai.ToolCall{{ID: "call-1", Name: "list_nodes", Arguments: json.RawMessage(`{}`)}}}, nil
			}
			return serverai.ChatResponse{Content: "assistant final answer"}, nil
		},
	}
	fixture := newAIAPIFixture(t, AuthConfig{}, adapter)

	created := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers",
		`{"name":"Primary","protocol":"openai_chat_completions","base_url":"https://model.test/v1","model":"model-a","api_key":"stream-key-secret"}`,
		"application/json", "http://panel.test")
	if created.Code != http.StatusCreated {
		t.Fatalf("create provider = %d %s", created.Code, created.Body.String())
	}
	var provider store.AIProvider
	if err := json.NewDecoder(created.Body).Decode(&provider); err != nil {
		t.Fatalf("decode provider: %v", err)
	}

	probe := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers/"+provider.ID+"/test", "", "", "http://panel.test")
	if probe.Code != http.StatusOK {
		t.Fatalf("probe provider = %d %s", probe.Code, probe.Body.String())
	}

	conversationResponse := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations",
		`{"title":"Stream"}`, "application/json", "http://panel.test")
	if conversationResponse.Code != http.StatusCreated {
		t.Fatalf("create conversation = %d %s", conversationResponse.Code, conversationResponse.Body.String())
	}
	var conversation store.AIConversation
	if err := json.NewDecoder(conversationResponse.Body).Decode(&conversation); err != nil {
		t.Fatalf("decode conversation: %v", err)
	}

	const promptMarker = "stream-prompt-must-not-leak"
	streamBody := `{"provider_id":"` + provider.ID + `","content":"` + promptMarker + `"}`
	recorder := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations/"+conversation.ID+"/messages/stream",
		streamBody, "application/json", "http://panel.test")
	if recorder.Code != http.StatusOK {
		t.Fatalf("stream = %d %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", contentType)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "event: status") {
		t.Fatalf("stream missing status events:\n%s", body)
	}
	if !strings.Contains(body, "event: result") {
		t.Fatalf("stream missing result event:\n%s", body)
	}
	if !strings.Contains(body, "assistant final answer") {
		t.Fatalf("stream result missing assistant content:\n%s", body)
	}

	// Phases must appear in the documented orchestration order: accepted, model,
	// tool, model, composing, completed.
	wantPhases := []string{
		`"phase":"accepted"`,
		`"phase":"model"`,
		`"phase":"tool"`,
		`"phase":"model"`,
		`"phase":"composing"`,
		`"phase":"completed"`,
	}
	offset := 0
	for _, want := range wantPhases {
		idx := strings.Index(body[offset:], want)
		if idx < 0 {
			t.Fatalf("phase %q not found in order after offset %d in:\n%s", want, offset, body)
		}
		offset += idx + len(want)
	}
	// The read tool call must surface its safe tool name and nothing else.
	if !strings.Contains(body, `"tool_name":"list_nodes"`) {
		t.Fatalf("stream missing safe tool_name for list_nodes:\n%s", body)
	}

	// Safety boundary: progress status events must carry only the fixed phase
	// enum plus safe tool/target names. Prompts, arguments JSON, raw tool
	// results, model responses, and secret material must never surface.
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: {") || !strings.Contains(line, `"phase"`) {
			continue
		}
		for _, forbidden := range []string{promptMarker, `"arguments"`, "provider_id", "stream-key-secret", "tool_call_id", "untrusted_operational_data", "model-a", "assistant"} {
			if strings.Contains(line, forbidden) {
				t.Fatalf("status event leaked forbidden content %q: %s", forbidden, line)
			}
		}
	}
	// Secret material must never appear anywhere in the stream.
	if strings.Contains(body, "stream-key-secret") {
		t.Fatalf("stream leaked provider api key:\n%s", body)
	}
}

func TestAIConversationMessageStreamEmitsDeltaResetAndServerResolvedContext(t *testing.T) {
	streamCalls := 0
	adapter := aiAPIStreamingAdapter{
		aiAPITestAdapter: aiAPITestAdapter{capabilities: serverai.Capabilities{Chat: true, Tools: true}},
		stream: func(_ context.Context, _ serverai.ProviderCredential, request serverai.ChatRequest, callback serverai.ContentCallback) (serverai.ChatResponse, error) {
			streamCalls++
			if streamCalls == 1 {
				if len(request.Messages) == 0 || request.Messages[0].Role != "system" {
					t.Fatalf("first model request missing system context: %+v", request.Messages)
				}
				system := request.Messages[0].Content
				for _, want := range []string{`"page":"hosts"`, `"type":"node"`, `"id":"node-context-1"`, `"name":"Server Resolved Name"`, `"route":"/nodes/node-context-1"`} {
					if !strings.Contains(system, want) {
						t.Fatalf("system context missing %s:\n%s", want, system)
					}
				}
				if err := callback("checking "); err != nil {
					return serverai.ChatResponse{}, err
				}
				return serverai.ChatResponse{Content: "checking ", ToolCalls: []serverai.ToolCall{{ID: "call-1", Name: "list_nodes", Arguments: json.RawMessage(`{}`)}}}, nil
			}
			if err := callback("final answer"); err != nil {
				return serverai.ChatResponse{}, err
			}
			return serverai.ChatResponse{Content: "final answer"}, nil
		},
	}
	fixture := newAIAPIFixture(t, AuthConfig{}, adapter)
	if err := store.NewNodeStore(fixture.db).Upsert(t.Context(), store.Node{ID: "node-context-1", Name: "Server Resolved Name", Status: "online"}); err != nil {
		t.Fatalf("upsert context node: %v", err)
	}

	created := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers",
		`{"name":"Primary","protocol":"openai_chat_completions","base_url":"https://model.test/v1","model":"model-a"}`,
		"application/json", "http://panel.test")
	var provider store.AIProvider
	if created.Code != http.StatusCreated || json.NewDecoder(created.Body).Decode(&provider) != nil {
		t.Fatalf("create provider = %d %s", created.Code, created.Body.String())
	}
	probe := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers/"+provider.ID+"/test", "", "", "http://panel.test")
	if probe.Code != http.StatusOK {
		t.Fatalf("probe provider = %d %s", probe.Code, probe.Body.String())
	}
	conversationResponse := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations", `{"title":"Context stream"}`, "application/json", "http://panel.test")
	var conversation store.AIConversation
	if conversationResponse.Code != http.StatusCreated || json.NewDecoder(conversationResponse.Body).Decode(&conversation) != nil {
		t.Fatalf("create conversation = %d %s", conversationResponse.Code, conversationResponse.Body.String())
	}

	stream := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations/"+conversation.ID+"/messages/stream",
		`{"provider_id":"`+provider.ID+`","content":"inspect selected node","context":{"page":"hosts","resource_type":"node","resource_id":"node-context-1"}}`,
		"application/json", "http://panel.test")
	if stream.Code != http.StatusOK {
		t.Fatalf("stream = %d %s", stream.Code, stream.Body.String())
	}
	body := stream.Body.String()
	for _, want := range []string{
		"event: delta\ndata: {", `"content":"checking "`,
		"event: reset\ndata: {", `"reason":"tool"`,
		`"content":"final answer"`, "event: result",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream missing %q:\n%s", want, body)
		}
	}
	if strings.Index(body, `"content":"checking "`) > strings.Index(body, `"reason":"tool"`) || strings.Index(body, `"reason":"tool"`) > strings.LastIndex(body, `"content":"final answer"`) {
		t.Fatalf("delta/reset order is invalid:\n%s", body)
	}
	var persisted string
	if err := fixture.db.QueryRow(`SELECT COALESCE(GROUP_CONCAT(content, ''), '') FROM ai_messages WHERE conversation_id = ?`, conversation.ID).Scan(&persisted); err != nil {
		t.Fatalf("query persisted messages: %v", err)
	}
	if strings.Contains(persisted, "untrusted_platform_context") || strings.Contains(persisted, "Server Resolved Name") {
		t.Fatalf("transient context was persisted: %s", persisted)
	}
}

func TestAIConversationMessageStreamRejectsInvalidContextBeforeSSE(t *testing.T) {
	adapter := aiAPITestAdapter{capabilities: serverai.Capabilities{Chat: true, Tools: true}, response: serverai.ChatResponse{Content: "unused"}}
	fixture := newAIAPIFixture(t, AuthConfig{}, adapter)
	created := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers",
		`{"name":"Primary","protocol":"openai_chat_completions","base_url":"https://model.test/v1","model":"model-a"}`,
		"application/json", "http://panel.test")
	var provider store.AIProvider
	_ = json.NewDecoder(created.Body).Decode(&provider)
	_ = performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers/"+provider.ID+"/test", "", "", "http://panel.test")
	conversationResponse := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations", `{"title":"Invalid context"}`, "application/json", "http://panel.test")
	var conversation store.AIConversation
	_ = json.NewDecoder(conversationResponse.Body).Decode(&conversation)

	response := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations/"+conversation.ID+"/messages/stream",
		`{"provider_id":"`+provider.ID+`","content":"hello","context":{"page":"hosts","resource_type":"node","resource_id":"missing-node","name":"browser supplied"}}`,
		"application/json", "http://panel.test")
	if response.Code != http.StatusBadRequest || strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("invalid context = %d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	var turns int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM ai_turns WHERE conversation_id = ?`, conversation.ID).Scan(&turns); err != nil || turns != 0 {
		t.Fatalf("turns after invalid context = %d err=%v", turns, err)
	}
}

func TestAIConversationMessageStreamStopsOnRequestCancellation(t *testing.T) {
	proceed := make(chan struct{})
	adapter := aiAPITestAdapter{
		capabilities: serverai.Capabilities{Chat: true, Tools: true},
		complete: func(ctx context.Context, _ serverai.ProviderCredential, _ serverai.ChatRequest) (serverai.ChatResponse, error) {
			select {
			case <-ctx.Done():
				return serverai.ChatResponse{}, ctx.Err()
			case <-proceed:
				return serverai.ChatResponse{Content: "late"}, nil
			}
		},
	}
	fixture := newAIAPIFixture(t, AuthConfig{}, adapter)

	created := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers",
		`{"name":"Primary","protocol":"openai_chat_completions","base_url":"https://model.test/v1","model":"model-a","api_key":"cancel-key"}`,
		"application/json", "http://panel.test")
	if created.Code != http.StatusCreated {
		t.Fatalf("create provider = %d %s", created.Code, created.Body.String())
	}
	var provider store.AIProvider
	_ = json.NewDecoder(created.Body).Decode(&provider)

	probe := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/providers/"+provider.ID+"/test", "", "", "http://panel.test")
	if probe.Code != http.StatusOK {
		t.Fatalf("probe provider = %d %s", probe.Code, probe.Body.String())
	}

	conversationResponse := performAIRawRequest(fixture.handler, http.MethodPost, "/api/ai/conversations",
		`{"title":"Cancel"}`, "application/json", "http://panel.test")
	var conversation store.AIConversation
	_ = json.NewDecoder(conversationResponse.Body).Decode(&conversation)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost,
		"http://panel.test/api/ai/conversations/"+conversation.ID+"/messages/stream",
		strings.NewReader(`{"provider_id":"`+provider.ID+`","content":"blocked-prompt"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://panel.test")
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		fixture.handler.ServeHTTP(recorder, request)
		close(done)
	}()
	cancel()
	close(proceed)
	<-done

	body := recorder.Body.String()
	if strings.Contains(body, "event: result") {
		t.Fatalf("cancelled stream emitted a result event:\n%s", body)
	}
	if strings.Contains(body, "blocked-prompt") {
		t.Fatalf("cancelled stream leaked prompt:\n%s", body)
	}
}
