package ai

import (
	"context"
	"encoding/json"
)

const ProtocolOpenAIChatCompletions = "openai_chat_completions"

type ProviderCredential struct {
	ID       string
	Name     string
	Protocol string
	BaseURL  string
	APIKey   string
	Model    string
}

type Capabilities struct {
	Chat  bool `json:"chat"`
	Tools bool `json:"tools"`
}

type ChatRequest struct {
	Messages []ChatMessage
	Tools    []ToolDefinition
}

type ChatMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// ContentCallback receives bounded, user-visible assistant text only. Returning
// an error stops the upstream stream immediately.
type ContentCallback func(string) error

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Adapter interface {
	Complete(context.Context, ProviderCredential, ChatRequest) (ChatResponse, error)
	Probe(context.Context, ProviderCredential) (Capabilities, error)
	ListModels(context.Context, ProviderCredential) ([]string, error)
}

// StreamingAdapter is an optional capability. Services keep using Complete for
// adapters that do not implement it.
type StreamingAdapter interface {
	CompleteStream(context.Context, ProviderCredential, ChatRequest, ContentCallback) (ChatResponse, error)
}

type AdapterErrorKind string

const (
	ErrorConfiguration  AdapterErrorKind = "configuration"
	ErrorAuthentication AdapterErrorKind = "authentication"
	ErrorTimeout        AdapterErrorKind = "timeout"
	ErrorRateLimit      AdapterErrorKind = "rate_limit"
	ErrorUpstream       AdapterErrorKind = "upstream"
	ErrorProtocol       AdapterErrorKind = "protocol"
	ErrorCapability     AdapterErrorKind = "capability"
)

type AdapterError struct {
	Kind    AdapterErrorKind
	Message string
}

func (e *AdapterError) Error() string { return e.Message }
