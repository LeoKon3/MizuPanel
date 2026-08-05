package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	serverai "github.com/mizupanel/mizupanel/internal/server/ai"
	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

const maxAIRequestBodyBytes = 64 * 1024

type AIConfig struct {
	Service *serverai.Service
}

type aiProviderRequest struct {
	Name        string  `json:"name"`
	Protocol    string  `json:"protocol"`
	BaseURL     string  `json:"base_url"`
	Model       string  `json:"model"`
	APIKey      *string `json:"api_key,omitempty"`
	ClearAPIKey bool    `json:"clear_api_key,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

type aiConversationRequest struct {
	Title   *string         `json:"title,omitempty"`
	ModelID json.RawMessage `json:"model_id,omitempty"`
}

type aiMessageRequest struct {
	ProviderID string `json:"provider_id"`
	Content    string `json:"content"`
}

type aiProviderModelRequest struct {
	ModelID     string `json:"model_id"`
	DisplayName string `json:"display_name,omitempty"`
}

type aiProviderModelsRequest struct {
	Models  []aiProviderModelRequest `json:"models"`
	Enabled *bool                    `json:"enabled,omitempty"`
}

type aiModelUpdateRequest struct {
	ModelID     string `json:"model_id"`
	DisplayName string `json:"display_name,omitempty"`
	Enabled     *bool  `json:"enabled"`
}

type aiRoutingRequest struct {
	DefaultModelID  json.RawMessage `json:"default_model_id"`
	FallbackModelID json.RawMessage `json:"fallback_model_id"`
}

func (s *Server) handleAIProviders(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeError(w, http.StatusServiceUnavailable, "AI 服务不可用")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/providers"), "/")
	if path == "" {
		s.handleAIProviderCollection(w, r)
		return
	}
	if path == "models" {
		s.handleAIProviderModels(w, r)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) == 1 {
		s.handleAIProviderResource(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "test" {
		s.handleAIProviderTest(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "default" {
		s.handleAIProviderDefault(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "discover" {
		s.handleAIProviderDiscover(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "models" {
		s.handleAIProviderModelCollection(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "AI Provider 不存在")
}

func (s *Server) handleAIProviderCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		providers, err := s.ai.ListProviders(r.Context())
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
	case http.MethodPost:
		serveraudit.Mark(r, "ai", "provider_create")
		if !authorizeAIMutation(w, r, true) {
			return
		}
		var request aiProviderRequest
		if !decodeAIRequest(w, r, &request) {
			return
		}
		if request.ClearAPIKey {
			writeError(w, http.StatusBadRequest, "创建 Provider 时不能清除 API Key")
			return
		}
		apiKey := ""
		if request.APIKey != nil {
			apiKey = *request.APIKey
		}
		provider, err := s.ai.CreateProvider(r.Context(), serverai.ProviderInput{Name: request.Name, Protocol: request.Protocol, BaseURL: request.BaseURL, Model: request.Model, APIKey: apiKey, Enabled: request.Enabled})
		if err != nil {
			writeAIError(w, err)
			return
		}
		serveraudit.SetTarget(r, "ai_provider", provider.ID, provider.Name)
		serveraudit.SetMetadata(r, "provider_id", provider.ID)
		serveraudit.SetMetadata(r, "model", provider.Model)
		writeJSON(w, http.StatusCreated, provider)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleAIProviderResource(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		provider, err := s.ai.GetProvider(r.Context(), id)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, provider)
	case http.MethodPut:
		serveraudit.Mark(r, "ai", "provider_update")
		serveraudit.SetTarget(r, "ai_provider", id, "")
		if !authorizeAIMutation(w, r, true) {
			return
		}
		var request aiProviderRequest
		if !decodeAIRequest(w, r, &request) {
			return
		}
		provider, err := s.ai.UpdateProvider(r.Context(), id, serverai.ProviderUpdate{Name: request.Name, Protocol: request.Protocol, BaseURL: request.BaseURL, Model: request.Model, APIKey: request.APIKey, ClearAPIKey: request.ClearAPIKey, Enabled: request.Enabled})
		if err != nil {
			writeAIError(w, err)
			return
		}
		serveraudit.SetTarget(r, "ai_provider", provider.ID, provider.Name)
		serveraudit.SetMetadata(r, "provider_id", provider.ID)
		serveraudit.SetMetadata(r, "model", provider.Model)
		writeJSON(w, http.StatusOK, provider)
	case http.MethodDelete:
		serveraudit.Mark(r, "ai", "provider_delete")
		serveraudit.SetTarget(r, "ai_provider", id, "")
		if !authorizeAIMutation(w, r, false) {
			return
		}
		if err := s.ai.DeleteProvider(r.Context(), id); err != nil {
			writeAIError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (s *Server) handleAIProviderTest(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	serveraudit.Mark(r, "ai", "provider_test")
	serveraudit.SetTarget(r, "ai_provider", id, "")
	serveraudit.SetMetadata(r, "provider_id", id)
	if !authorizeAIMutation(w, r, false) {
		return
	}
	provider, probeErr := s.ai.TestProvider(r.Context(), id)
	if provider.ID == "" {
		writeAIError(w, probeErr)
		return
	}
	if probeErr != nil {
		serveraudit.SetResult(r, serveraudit.ResultFailure, "capability_probe_failed")
	}
	writeJSON(w, http.StatusOK, provider)
}

func (s *Server) handleAIProviderDefault(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	serveraudit.Mark(r, "ai", "provider_default")
	serveraudit.SetTarget(r, "ai_provider", id, "")
	serveraudit.SetMetadata(r, "provider_id", id)
	if !authorizeAIMutation(w, r, false) {
		return
	}
	if err := s.ai.SetDefaultProvider(r.Context(), id); err != nil {
		writeAIError(w, err)
		return
	}
	provider, err := s.ai.GetProvider(r.Context(), id)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func (s *Server) handleAIProviderDiscover(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	serveraudit.Mark(r, "ai", "provider_discover")
	serveraudit.SetTarget(r, "ai_provider", id, "")
	serveraudit.SetMetadata(r, "provider_id", id)
	if !authorizeAIMutation(w, r, false) {
		return
	}
	models, err := s.ai.DiscoverProvider(r.Context(), id)
	if err != nil {
		serveraudit.SetResult(r, serveraudit.ResultFailure, "model_discovery_failed")
		writeAIError(w, err)
		return
	}
	provider, err := s.ai.GetProvider(r.Context(), id)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": provider, "models": models})
}

func (s *Server) handleAIProviderModelCollection(w http.ResponseWriter, r *http.Request, providerID string) {
	switch r.Method {
	case http.MethodGet:
		models, err := s.ai.ListProviderModels(r.Context(), providerID)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": models})
	case http.MethodPost:
		serveraudit.Mark(r, "ai", "model_import")
		serveraudit.SetTarget(r, "ai_provider", providerID, "")
		serveraudit.SetMetadata(r, "provider_id", providerID)
		if !authorizeAIMutation(w, r, true) {
			return
		}
		var request aiProviderModelsRequest
		if !decodeAIRequest(w, r, &request) {
			return
		}
		inputs := make([]serverai.ModelInput, 0, len(request.Models))
		for _, model := range request.Models {
			inputs = append(inputs, serverai.ModelInput{
				ModelID: model.ModelID, DisplayName: model.DisplayName, Enabled: request.Enabled,
			})
		}
		models, err := s.ai.ImportModels(r.Context(), providerID, inputs)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"models": models})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeError(w, http.StatusServiceUnavailable, "AI 服务不可用")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/models"), "/")
	parts := strings.Split(path, "/")
	if path == "" || len(parts) > 2 || (len(parts) == 2 && parts[1] != "test") {
		writeError(w, http.StatusNotFound, "AI 模型不存在")
		return
	}
	id := parts[0]
	if len(parts) == 2 {
		s.handleAIModelTest(w, r, id)
		return
	}
	switch r.Method {
	case http.MethodGet:
		model, err := s.ai.GetModel(r.Context(), id)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, model)
	case http.MethodPut:
		serveraudit.Mark(r, "ai", "model_update")
		serveraudit.SetTarget(r, "ai_model", id, "")
		serveraudit.SetMetadata(r, "model_id", id)
		if !authorizeAIMutation(w, r, true) {
			return
		}
		var request aiModelUpdateRequest
		if !decodeAIRequest(w, r, &request) {
			return
		}
		if request.Enabled == nil {
			writeError(w, http.StatusBadRequest, "模型启用状态不能为空")
			return
		}
		model, err := s.ai.UpdateModel(r.Context(), id, serverai.ModelUpdate{
			ModelID: request.ModelID, DisplayName: request.DisplayName, Enabled: *request.Enabled,
		})
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, model)
	case http.MethodDelete:
		serveraudit.Mark(r, "ai", "model_delete")
		serveraudit.SetTarget(r, "ai_model", id, "")
		serveraudit.SetMetadata(r, "model_id", id)
		if !authorizeAIMutation(w, r, false) {
			return
		}
		if err := s.ai.DeleteModel(r.Context(), id); err != nil {
			writeAIError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (s *Server) handleAIModelTest(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	serveraudit.Mark(r, "ai", "model_test")
	serveraudit.SetTarget(r, "ai_model", id, "")
	serveraudit.SetMetadata(r, "model_id", id)
	if !authorizeAIMutation(w, r, false) {
		return
	}
	model, err := s.ai.TestModel(r.Context(), id)
	if err != nil {
		serveraudit.SetResult(r, serveraudit.ResultFailure, "capability_probe_failed")
		writeAIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *Server) handleAIRouting(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeError(w, http.StatusServiceUnavailable, "AI 服务不可用")
		return
	}
	switch r.Method {
	case http.MethodGet:
		routing, err := s.ai.GetRouting(r.Context())
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, routing)
	case http.MethodPut:
		serveraudit.Mark(r, "ai", "routing_update")
		serveraudit.SetTarget(r, "ai_routing", "global", "")
		if !authorizeAIMutation(w, r, true) {
			return
		}
		var request aiRoutingRequest
		if !decodeAIRequest(w, r, &request) {
			return
		}
		defaultID, defaultPresent, valid := decodeNullableModelID(w, request.DefaultModelID)
		if !valid {
			return
		}
		fallbackID, fallbackPresent, valid := decodeNullableModelID(w, request.FallbackModelID)
		if !valid {
			return
		}
		if !defaultPresent || !fallbackPresent {
			writeError(w, http.StatusBadRequest, "默认和备用模型字段不能为空")
			return
		}
		routing, err := s.ai.SetRouting(r.Context(), defaultID, fallbackID)
		if err != nil {
			writeAIError(w, err)
			return
		}
		if routing.DefaultModelID != nil {
			serveraudit.SetMetadata(r, "model_id", *routing.DefaultModelID)
		}
		writeJSON(w, http.StatusOK, routing)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

func (s *Server) handleAIConversations(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeError(w, http.StatusServiceUnavailable, "AI 服务不可用")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/conversations"), "/")
	if path == "" {
		s.handleAIConversationCollection(w, r)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) == 1 {
		s.handleAIConversationResource(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "messages" && parts[2] == "stream" {
		s.handleAIConversationMessageStream(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "messages" {
		s.handleAIConversationMessages(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "AI 会话不存在")
}

func (s *Server) handleAIConversationCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit, err := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
		if err != nil || limit < 1 || limit > 100 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		conversations, err := s.ai.ListConversations(r.Context(), limit)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": conversations})
	case http.MethodPost:
		if !authorizeAIMutation(w, r, true) {
			return
		}
		var request aiConversationRequest
		if !decodeAIRequest(w, r, &request) {
			return
		}
		modelID, _, valid := decodeNullableModelID(w, request.ModelID)
		if !valid {
			return
		}
		title := ""
		if request.Title != nil {
			title = *request.Title
		}
		conversation, err := s.ai.CreateConversationWithModel(r.Context(), title, modelID)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, conversation)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleAIConversationResource(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		limit, err := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
		if err != nil || limit < 1 || limit > 100 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		state, err := s.ai.ConversationState(r.Context(), id, limit)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, state)
	case http.MethodPatch:
		if !authorizeAIMutation(w, r, true) {
			return
		}
		var request aiConversationRequest
		if !decodeAIRequest(w, r, &request) {
			return
		}
		if request.Title == nil && request.ModelID == nil {
			writeError(w, http.StatusBadRequest, "AI 会话更新不能为空")
			return
		}
		if request.Title != nil {
			if err := s.ai.RenameConversation(r.Context(), id, *request.Title); err != nil {
				writeAIError(w, err)
				return
			}
		}
		if request.ModelID != nil {
			modelID, _, valid := decodeNullableModelID(w, request.ModelID)
			if !valid {
				return
			}
			if _, err := s.ai.SetConversationModel(r.Context(), id, modelID); err != nil {
				writeAIError(w, err)
				return
			}
		}
		state, err := s.ai.ConversationState(r.Context(), id, 50)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, state.Conversation)
	case http.MethodDelete:
		if !authorizeAIMutation(w, r, false) {
			return
		}
		if err := s.ai.DeleteConversation(r.Context(), id); err != nil {
			writeAIError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
	}
}

func (s *Server) handleAIConversationMessages(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		limit, err := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
		if err != nil || limit < 1 || limit > 100 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		state, err := s.ai.ConversationState(r.Context(), id, limit)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": state.Messages, "tool_calls": state.ToolCalls})
	case http.MethodPost:
		if !authorizeAIMutation(w, r, true) {
			return
		}
		var request aiMessageRequest
		if !decodeAIRequest(w, r, &request) {
			return
		}
		result, err := s.ai.Send(r.Context(), id, request.ProviderID, request.Content, aiAuditCallback(r))
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleAIConversationMessageStream(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !authorizeAIMutation(w, r, true) {
		return
	}
	var request aiMessageRequest
	if !decodeAIRequest(w, r, &request) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "streaming response not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	progress := func(event serverai.ProgressEvent) {
		if ctx.Err() != nil {
			return
		}
		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		if _, writeErr := fmt.Fprintf(w, "event: status\ndata: %s\n\n", data); writeErr != nil {
			return
		}
		flusher.Flush()
	}
	result, err := s.ai.SendWithProgress(ctx, id, request.ProviderID, request.Content, aiAuditCallback(r), progress)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		payload, _ := json.Marshal(map[string]string{"error": serverai.SafeErrorMessage(err)})
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
		flusher.Flush()
		return
	}
	payload, err := json.Marshal(result)
	if err == nil {
		fmt.Fprintf(w, "event: result\ndata: %s\n\n", payload)
		flusher.Flush()
	}
}

func (s *Server) handleAIToolCalls(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeError(w, http.StatusServiceUnavailable, "AI 服务不可用")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/tool-calls"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || (parts[1] != "confirm" && parts[1] != "reject") {
		writeError(w, http.StatusNotFound, "AI 工具调用不存在")
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !authorizeAIMutation(w, r, false) {
		return
	}
	var result any
	var err error
	if parts[1] == "confirm" {
		result, err = s.ai.Confirm(r.Context(), parts[0], aiAuditCallback(r))
	} else {
		result, err = s.ai.Reject(r.Context(), parts[0], aiAuditCallback(r))
	}
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func aiAuditCallback(r *http.Request) serverai.AuditCallback {
	return func(event serverai.AuditEvent) {
		result := serveraudit.ResultSuccess
		if event.Status == "failure" {
			result = serveraudit.ResultFailure
		} else if event.Status == "pending" || event.Status == "running" || event.Status == "accepted" {
			result = serveraudit.ResultAccepted
		}
		metadata := map[string]string{"status": event.Status}
		if event.ToolCall.ToolName != "" {
			metadata["tool_name"] = event.ToolCall.ToolName
		}
		if event.ToolCall.Risk != "" {
			metadata["risk"] = event.ToolCall.Risk
		}
		if event.ProviderID != "" {
			metadata["provider_id"] = event.ProviderID
		}
		if event.ModelID != "" {
			metadata["model_id"] = event.ModelID
		}
		if event.Model != "" {
			metadata["model"] = event.Model
		}
		if event.RequestedProviderID != "" {
			metadata["requested_provider_id"] = event.RequestedProviderID
		}
		if event.RequestedModelID != "" {
			metadata["requested_model_id"] = event.RequestedModelID
		}
		if event.RequestedModel != "" {
			metadata["requested_model"] = event.RequestedModel
		}
		if event.FallbackUsed {
			metadata["fallback_used"] = "true"
		}
		serveraudit.Record(r, serveraudit.RecordOptions{Module: "ai", Action: event.Action,
			TargetType: event.ToolCall.TargetType, TargetID: event.ToolCall.TargetID,
			TargetName: event.ToolCall.TargetName, NodeID: event.ToolCall.NodeID,
			Result: result, Summary: event.Status, Duration: event.Duration,
			Metadata: metadata})
	}
}

func decodeNullableModelID(w http.ResponseWriter, raw json.RawMessage) (*string, bool, bool) {
	if len(raw) == 0 {
		return nil, false, true
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil {
		writeError(w, http.StatusBadRequest, "模型 ID 格式无效")
		return nil, true, false
	}
	if value == nil {
		return nil, true, true
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil, true, true
	}
	if len(normalized) > 128 {
		writeError(w, http.StatusBadRequest, "模型 ID 格式无效")
		return nil, true, false
	}
	return &normalized, true, true
}

func decodeAIRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAIRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request body")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func authorizeAIMutation(w http.ResponseWriter, r *http.Request, requireJSON bool) bool {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if !requireJSON {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return false
	}
	return true
}

func writeAIError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var adapterErr *serverai.AdapterError
	switch {
	case errors.Is(err, store.ErrAIInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, store.ErrAINotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrAIConflict):
		status = http.StatusConflict
	case errors.Is(err, serverai.ErrMessageTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, serverai.ErrProviderCapability):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, serverai.ErrMasterKeyUnavailable), errors.Is(err, serverai.ErrSecretDecrypt), errors.Is(err, serverai.ErrServiceUnavailable):
		status = http.StatusServiceUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	case errors.As(err, &adapterErr):
		if adapterErr.Kind == serverai.ErrorTimeout {
			status = http.StatusGatewayTimeout
		} else {
			status = http.StatusBadGateway
		}
	}
	writeError(w, status, serverai.SafeErrorMessage(err))
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

type aiModelsRequest struct {
	ProviderID string `json:"provider_id"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
}

func (s *Server) handleAIProviderModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !authorizeAIMutation(w, r, true) {
		return
	}
	var request aiModelsRequest
	if !decodeAIRequest(w, r, &request) {
		return
	}
	models, err := s.ai.ListModels(r.Context(), request.ProviderID, request.BaseURL, request.APIKey)
	if err != nil {
		writeAIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}
