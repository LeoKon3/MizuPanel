package ai

import (
	"bufio"
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
	"unicode/utf8"
)

const maxProviderResponseBytes = 4 << 20

const maxProviderStreamFrameBytes = 256 << 10

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
	httpRequest, err := buildOpenAICompletionRequest(ctx, provider, request, false)
	if err != nil {
		return ChatResponse{}, err
	}
	response, err := a.doCompletionRequest(ctx, httpRequest)
	if err != nil {
		return ChatResponse{}, err
	}
	defer response.Body.Close()
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
	return convertOpenAIMessage(decoded.Choices[0].Message)
}

func (a *OpenAIChatCompletionsAdapter) CompleteStream(ctx context.Context, provider ProviderCredential, request ChatRequest, callback ContentCallback) (ChatResponse, error) {
	httpRequest, err := buildOpenAICompletionRequest(ctx, provider, request, true)
	if err != nil {
		return ChatResponse{}, err
	}
	response, err := a.doCompletionRequest(ctx, httpRequest)
	if err != nil {
		return ChatResponse{}, err
	}
	defer response.Body.Close()
	return parseOpenAICompletionStream(ctx, response.Body, callback)
}

func buildOpenAICompletionRequest(ctx context.Context, provider ProviderCredential, request ChatRequest, stream bool) (*http.Request, error) {
	baseURL, err := NormalizeBaseURL(provider.BaseURL)
	if err != nil || provider.Model == "" {
		return nil, adapterError(ErrorConfiguration, "模型配置无效")
	}
	payload := openAIRequest{Model: provider.Model, Stream: stream, ToolChoice: "auto"}
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
		return nil, adapterError(ErrorProtocol, "模型请求编码失败")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, adapterError(ErrorConfiguration, "模型配置无效")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	return httpRequest, nil
}

func (a *OpenAIChatCompletionsAdapter) doCompletionRequest(ctx context.Context, httpRequest *http.Request) (*http.Response, error) {
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
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, adapterError(ErrorAuthentication, "模型服务拒绝了凭据")
		case http.StatusTooManyRequests:
			return nil, adapterError(ErrorRateLimit, "模型服务请求过于频繁")
		default:
			return nil, adapterError(ErrorUpstream, "模型服务返回异常状态")
		}
	}
	return response, nil
}

func convertOpenAIMessage(message openAIMessage) (ChatResponse, error) {
	result := ChatResponse{Content: message.Content, ToolCalls: make([]ToolCall, 0, len(message.ToolCalls))}
	for _, call := range message.ToolCalls {
		if call.ID == "" {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型工具调用缺少 ID")
		}
		if call.Function.Name == "" {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型工具调用缺少名称")
		}
		if !json.Valid([]byte(call.Function.Arguments)) {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型工具调用参数无效")
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
	}
	return result, nil
}

type openAIStreamToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func resolveOpenAIStreamToolIndex(toolCalls map[int]*openAIStreamToolCall, order []int, frameIndexes map[int]struct{}, providerIndex *int, callID string, position int) int {
	if callID != "" {
		for index, call := range toolCalls {
			if call.ID == callID {
				return index
			}
		}
	}
	if providerIndex != nil {
		index := *providerIndex
		call, exists := toolCalls[index]
		_, alreadyUsed := frameIndexes[index]
		if !alreadyUsed && (!exists || call.ID == "" || callID == "" || call.ID == callID) {
			return index
		}
	}
	if position < len(order) {
		index := order[position]
		call := toolCalls[index]
		if call.ID == "" || callID == "" || call.ID == callID {
			return index
		}
	}
	index := 0
	for {
		if _, exists := toolCalls[index]; !exists {
			return index
		}
		index++
	}
}

func parseOpenAICompletionStream(ctx context.Context, reader io.Reader, callback ContentCallback) (ChatResponse, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, maxProviderResponseBytes+1))
	scanner.Buffer(make([]byte, 64*1024), maxProviderStreamFrameBytes)
	var content strings.Builder
	toolCalls := make(map[int]*openAIStreamToolCall)
	order := make([]int, 0)
	var dataLines []string
	totalBytes := 0
	frameBytes := 0
	terminal := false
	done := false
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		frameBytes = 0
		if data == "[DONE]" {
			done = true
			return nil
		}
		var event openAIStreamResponse
		if err := json.Unmarshal([]byte(data), &event); err != nil || len(event.Choices) == 0 {
			return adapterError(ErrorProtocol, "模型流响应格式无效")
		}
		choice := event.Choices[0]
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			terminal = true
		}
		if choice.Delta.Content != "" {
			if !utf8.ValidString(choice.Delta.Content) {
				return adapterError(ErrorProtocol, "模型流响应格式无效")
			}
			content.WriteString(choice.Delta.Content)
			if callback != nil {
				if err := callback(boundedString(choice.Delta.Content, maxUserMessageBytes)); err != nil {
					return err
				}
			}
		}
		frameIndexes := make(map[int]struct{}, len(choice.Delta.ToolCalls))
		for position, delta := range choice.Delta.ToolCalls {
			index := resolveOpenAIStreamToolIndex(toolCalls, order, frameIndexes, delta.Index, delta.ID, position)
			frameIndexes[index] = struct{}{}
			call, exists := toolCalls[index]
			if !exists {
				call = &openAIStreamToolCall{}
				toolCalls[index] = call
				order = append(order, index)
			}
			if delta.ID != "" {
				if call.ID != "" && call.ID != delta.ID {
					return adapterError(ErrorProtocol, "模型工具调用 ID 冲突")
				}
				call.ID = delta.ID
			}
			call.Name += delta.Function.Name
			call.Arguments.WriteString(delta.Function.Arguments)
			if call.Arguments.Len() > maxProviderStreamFrameBytes {
				return adapterError(ErrorProtocol, "模型工具调用参数过大")
			}
		}
		return nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return ChatResponse{}, err
		}
		line := scanner.Text()
		totalBytes += len(line) + 1
		if totalBytes > maxProviderResponseBytes {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型响应过大")
		}
		if !utf8.ValidString(line) {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型流响应格式无效")
		}
		if line == "" {
			if err := flush(); err != nil {
				return ChatResponse{}, err
			}
			if done {
				break
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型流响应格式无效")
		}
		data := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
		frameBytes += len(data)
		if len(dataLines) > 0 {
			frameBytes++
		}
		if frameBytes > maxProviderStreamFrameBytes {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型流响应格式无效")
		}
		dataLines = append(dataLines, data)
	}
	if err := scanner.Err(); err != nil {
		return ChatResponse{}, adapterError(ErrorProtocol, "模型响应读取失败")
	}
	if !done {
		if err := flush(); err != nil {
			return ChatResponse{}, err
		}
	}
	if !done && !terminal {
		return ChatResponse{}, adapterError(ErrorProtocol, "模型流响应未正常结束")
	}
	sort.Ints(order)
	result := ChatResponse{Content: content.String(), ToolCalls: make([]ToolCall, 0, len(order))}
	for _, index := range order {
		call := toolCalls[index]
		arguments := call.Arguments.String()
		if call.ID == "" {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型工具调用缺少 ID")
		}
		if call.Name == "" {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型工具调用缺少名称")
		}
		if !json.Valid([]byte(arguments)) {
			return ChatResponse{}, adapterError(ErrorProtocol, "模型工具调用参数无效")
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{ID: call.ID, Name: call.Name, Arguments: json.RawMessage(arguments)})
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

type openAIStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    *int   `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
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
