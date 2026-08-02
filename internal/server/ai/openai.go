package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const maxProviderResponseBytes = 4 << 20

type OpenAIChatCompletionsAdapter struct {
	client *http.Client
}

func NewOpenAIChatCompletionsAdapter(timeout time.Duration) *OpenAIChatCompletionsAdapter {
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = timeout
	return &OpenAIChatCompletionsAdapter{client: &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (a *OpenAIChatCompletionsAdapter) Complete(ctx context.Context, provider ProviderCredential, request ChatRequest) (ChatResponse, error) {
	baseURL, err := NormalizeBaseURL(provider.BaseURL)
	if err != nil || provider.Model == "" {
		return ChatResponse{}, adapterError(ErrorConfiguration, "模型配置无效")
	}
	payload := openAIRequest{Model: provider.Model, Stream: false, ToolChoice: "auto"}
	payload.Messages = make([]openAIMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		converted := openAIMessage{Role: message.Role, Content: message.Content, ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, openAIToolCall{ID: call.ID, Type: "function", Function: openAIFunctionCall{Name: call.Name, Arguments: string(call.Arguments)}})
		}
		payload.Messages = append(payload.Messages, converted)
	}
	for _, definition := range request.Tools {
		payload.Tools = append(payload.Tools, openAITool{Type: "function", Function: definition})
	}
	if len(payload.Tools) == 0 {
		payload.ToolChoice = ""
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResponse{}, adapterError(ErrorProtocol, "模型请求编码失败")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, adapterError(ErrorConfiguration, "模型配置无效")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	response, err := a.client.Do(httpRequest)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ChatResponse{}, err
		}
		if errors.Is(err, context.DeadlineExceeded) || isNetTimeout(err) {
			return ChatResponse{}, adapterError(ErrorTimeout, "模型请求超时")
		}
		return ChatResponse{}, adapterError(ErrorUpstream, "模型服务不可用")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ChatResponse{}, adapterError(ErrorAuthentication, "模型服务拒绝了凭据")
		case http.StatusTooManyRequests:
			return ChatResponse{}, adapterError(ErrorRateLimit, "模型服务请求过于频繁")
		default:
			return ChatResponse{}, adapterError(ErrorUpstream, "模型服务返回异常状态")
		}
	}
	limited := io.LimitReader(response.Body, maxProviderResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return ChatResponse{}, adapterError(ErrorProtocol, "模型响应读取失败")
	}
	if len(responseBody) > maxProviderResponseBytes {
		return ChatResponse{}, adapterError(ErrorProtocol, "模型响应过大")
	}
	var decoded openAIResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil || len(decoded.Choices) == 0 {
		return ChatResponse{}, adapterError(ErrorProtocol, "模型响应格式无效")
	}
	message := decoded.Choices[0].Message
	result := ChatResponse{Content: message.Content, ToolCalls: make([]ToolCall, 0, len(message.ToolCalls))}
	for _, call := range message.ToolCalls {
		if call.ID == "" || call.Function.Name == "" {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型工具调用格式无效")
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
	}
	return result, nil
}

func (a *OpenAIChatCompletionsAdapter) Probe(ctx context.Context, provider ProviderCredential) (Capabilities, error) {
	result := Capabilities{}
	text, err := a.Complete(ctx, provider, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "Reply with OK."}}})
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(text.Content) == "" {
		return result, adapterError(ErrorCapability, "模型未返回文本响应")
	}
	result.Chat = true
	probe := ToolDefinition{Name: "mizupanel_capability_probe", Description: "Return the fixed probe value.", Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"value": map[string]any{"type": "string", "enum": []string{"ok"}}},
		"required":   []string{"value"},
	}}
	toolResult, err := a.Complete(ctx, provider, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "Call mizupanel_capability_probe with value ok."}}, Tools: []ToolDefinition{probe}})
	if err != nil {
		return result, err
	}
	for _, call := range toolResult.ToolCalls {
		if call.Name == probe.Name && json.Valid(call.Arguments) {
			result.Tools = true
			return result, nil
		}
	}
	return result, adapterError(ErrorCapability, "模型不支持工具调用")
}

func NormalizeBaseURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" || len(value) > 2048 {
		return "", fmt.Errorf("invalid base URL")
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid base URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func adapterError(kind AdapterErrorKind, message string) error {
	return &AdapterError{Kind: kind, Message: message}
}

func isNetTimeout(err error) bool {
	var netError net.Error
	return errors.As(err, &netError) && netError.Timeout()
}

type openAIRequest struct {
	Model      string          `json:"model"`
	Messages   []openAIMessage `json:"messages"`
	Tools      []openAITool    `json:"tools,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
	Stream     bool            `json:"stream"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function ToolDefinition `json:"function"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

type openAIModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (a *OpenAIChatCompletionsAdapter) ListModels(ctx context.Context, provider ProviderCredential) ([]string, error) {
	baseURL, err := NormalizeBaseURL(provider.BaseURL)
	if err != nil {
		return nil, adapterError(ErrorConfiguration, "Base URL 无效")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, adapterError(ErrorConfiguration, "Base URL 无效")
	}
	if provider.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	response, err := a.client.Do(httpRequest)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		if errors.Is(err, context.DeadlineExceeded) || isNetTimeout(err) {
			return nil, adapterError(ErrorTimeout, "模型请求超时")
		}
		return nil, adapterError(ErrorUpstream, "模型服务不可用")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, adapterError(ErrorAuthentication, "模型服务拒绝了凭据")
		case http.StatusTooManyRequests:
			return nil, adapterError(ErrorRateLimit, "模型服务请求过于频繁")
		default:
			return nil, adapterError(ErrorUpstream, "模型服务返回异常状态")
		}
	}
	limited := io.LimitReader(response.Body, maxProviderResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, adapterError(ErrorProtocol, "模型列表读取失败")
	}
	if len(responseBody) > maxProviderResponseBytes {
		return nil, adapterError(ErrorProtocol, "模型列表响应过大")
	}
	var modelsResponse openAIModelsResponse
	if err := json.Unmarshal(responseBody, &modelsResponse); err != nil {
		return nil, adapterError(ErrorProtocol, "模型列表解析失败")
	}
	seen := make(map[string]struct{}, len(modelsResponse.Data))
	models := make([]string, 0, len(modelsResponse.Data))
	for _, model := range modelsResponse.Data {
		id := strings.TrimSpace(model.ID)
		if id != "" {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models, nil
}
