package ai

import (
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
