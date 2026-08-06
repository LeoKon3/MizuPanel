package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestOpenAIAdapterRequestAndToolResponse(t *testing.T) {
	adapter := NewOpenAIChatCompletionsAdapter(time.Second)
	adapter.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://model.test/v1/chat/completions" {
			t.Fatalf("URL = %s", request.URL)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer key-marker" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "model-a" || body["stream"] != false || body["tool_choice"] != "auto" {
			t.Fatalf("request body = %#v", body)
		}
		return jsonResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"list_nodes","arguments":"{}"}}]}}]}`), nil
	})

	response, err := adapter.Complete(t.Context(), ProviderCredential{BaseURL: "https://model.test/v1", APIKey: "key-marker", Model: "model-a"}, ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "nodes"}},
		Tools:    []ToolDefinition{noArgumentDefinition("list_nodes", "List nodes")},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "list_nodes" || string(response.ToolCalls[0].Arguments) != "{}" {
		t.Fatalf("tool response = %+v", response)
	}
}

func TestOpenAIAdapterStreamsContentAndAssemblesToolCalls(t *testing.T) {
	adapter := NewOpenAIChatCompletionsAdapter(time.Second)
	adapter.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["stream"] != true {
			t.Fatalf("stream = %#v, want true", body["stream"])
		}
		stream := strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"hello "},"finish_reason":null}]}`,
			"",
			`data: {"choices":[{"delta":{"content":"world"},"finish_reason":null}]}`,
			"",
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"list_","arguments":"{\"li"}}]},"finish_reason":null}]}`,
			"",
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"nodes","arguments":"mit\":20}"}}]},"finish_reason":"tool_calls"}]}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")
		response := jsonResponse(http.StatusOK, stream)
		response.Header.Set("Content-Type", "text/event-stream")
		return response, nil
	})

	var deltas strings.Builder
	response, err := adapter.CompleteStream(t.Context(), ProviderCredential{BaseURL: "https://model.test/v1", Model: "model-a"}, ChatRequest{}, func(content string) error {
		deltas.WriteString(content)
		return nil
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if deltas.String() != "hello world" || response.Content != "hello world" {
		t.Fatalf("content callback/response = %q/%q", deltas.String(), response.Content)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].ID != "call-1" || response.ToolCalls[0].Name != "list_nodes" || string(response.ToolCalls[0].Arguments) != `{"limit":20}` {
		t.Fatalf("tool calls = %+v", response.ToolCalls)
	}
}

func TestOpenAIAdapterStreamRequiresTerminalAndSanitizesMalformedFrames(t *testing.T) {
	for _, body := range []string{
		`data: {"choices":[{"delta":{"content":"partial"},"finish_reason":null}]}` + "\n\n",
		"data: {not-json}\n\n",
	} {
		adapter := NewOpenAIChatCompletionsAdapter(time.Second)
		adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, body), nil
		})
		_, err := adapter.CompleteStream(t.Context(), ProviderCredential{BaseURL: "https://model.test/v1", Model: "model-a"}, ChatRequest{}, nil)
		var adapterErr *AdapterError
		if err == nil || !errors.As(err, &adapterErr) || adapterErr.Kind != ErrorProtocol || strings.Contains(err.Error(), "not-json") {
			t.Fatalf("stream error = %#v", err)
		}
	}
}

func TestOpenAIAdapterStreamRejectsInvalidUTF8AndOversizedMultilineFrame(t *testing.T) {
	oversizedFrame := strings.Repeat("data: "+strings.Repeat(" ", 64*1024)+"\n", 5) +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"
	invalidUTF8 := append([]byte(`data: {"choices":[{"delta":{"content":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"},"finish_reason":"stop"}]}`+"\n\n")...)

	for name, body := range map[string][]byte{
		"invalid UTF-8":           invalidUTF8,
		"oversized multiline SSE": []byte(oversizedFrame),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseOpenAICompletionStream(t.Context(), bytes.NewReader(body), nil)
			var adapterErr *AdapterError
			if err == nil || !errors.As(err, &adapterErr) || adapterErr.Kind != ErrorProtocol {
				t.Fatalf("stream error = %#v, want protocol error", err)
			}
		})
	}
}

func TestOpenAIAdapterErrorsAreStableAndSanitized(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
		kind AdapterErrorKind
	}{
		{name: "auth", code: http.StatusUnauthorized, kind: ErrorAuthentication},
		{name: "rate", code: http.StatusTooManyRequests, kind: ErrorRateLimit},
		{name: "redirect", code: http.StatusTemporaryRedirect, kind: ErrorUpstream},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := NewOpenAIChatCompletionsAdapter(time.Second)
			adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(test.code, `{"secret":"upstream-marker"}`), nil
			})
			_, err := adapter.Complete(t.Context(), ProviderCredential{BaseURL: "https://model.test/v1", Model: "m"}, ChatRequest{})
			var adapterErr *AdapterError
			if err == nil || !strings.Contains(err.Error(), "模型") || !errors.As(err, &adapterErr) || adapterErr.Kind != test.kind {
				t.Fatalf("error = %#v, want kind %s", err, test.kind)
			}
			if strings.Contains(err.Error(), "upstream-marker") {
				t.Fatalf("error leaked upstream body: %v", err)
			}
		})
	}

	for _, value := range []string{"", "ftp://model.test/v1", "https://user:pass@model.test/v1", "https://model.test/v1?q=secret", "https://model.test/v1#fragment"} {
		if _, err := NormalizeBaseURL(value); err == nil {
			t.Fatalf("NormalizeBaseURL(%q) succeeded", value)
		}
	}
}

func TestOpenAIAdapterListsModelsWithAuthenticationAndStableOrdering(t *testing.T) {
	adapter := NewOpenAIChatCompletionsAdapter(time.Second)
	adapter.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != "https://model.test/v1/models" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer key-marker" {
			t.Fatalf("Authorization = %q", got)
		}
		return jsonResponse(http.StatusOK, `{"data":[{"id":"model-z"},{"id":" model-a "},{"id":"model-z"},{"id":""}]}`), nil
	})

	models, err := adapter.ListModels(t.Context(), ProviderCredential{BaseURL: "https://model.test/v1/", APIKey: "key-marker"})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if got := strings.Join(models, ","); got != "model-a,model-z" {
		t.Fatalf("models = %q", got)
	}
}

func TestOpenAIAdapterModelListErrorsAreSanitized(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
		body string
		kind AdapterErrorKind
	}{
		{name: "authentication", code: http.StatusUnauthorized, body: `{"secret":"credential-marker"}`, kind: ErrorAuthentication},
		{name: "malformed", code: http.StatusOK, body: `{"data":`, kind: ErrorProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := NewOpenAIChatCompletionsAdapter(time.Second)
			adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(test.code, test.body), nil
			})
			_, err := adapter.ListModels(t.Context(), ProviderCredential{BaseURL: "https://model.test/v1", APIKey: "credential-marker"})
			var adapterErr *AdapterError
			if err == nil || !errors.As(err, &adapterErr) || adapterErr.Kind != test.kind {
				t.Fatalf("error = %#v, want kind %s", err, test.kind)
			}
			if strings.Contains(err.Error(), "credential-marker") {
				t.Fatalf("error leaked secret: %v", err)
			}
		})
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
