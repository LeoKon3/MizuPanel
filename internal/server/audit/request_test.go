package audit

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type memoryRecorder struct {
	events []Event
	err    error
}

func (r *memoryRecorder) Create(_ context.Context, event *Event) error {
	copy := *event
	copy.Metadata = boundedMetadata(event.Metadata)
	r.events = append(r.events, copy)
	return r.err
}

func TestMiddlewareRecordsBoundedOperationAndIgnoresForwardingHeaders(t *testing.T) {
	recorder := &memoryRecorder{}
	handler := Middleware(recorder, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetPrincipal(r, ActorAdmin, "admin")
		Mark(r, "docker", "restart")
		SetTarget(r, "container", "container-1", "api")
		SetNodeID(r, "node-1")
		SetMetadata(r, "force", "false")
		w.WriteHeader(http.StatusAccepted)
	}))
	request := httptest.NewRequest(http.MethodPost, "http://panel/api/nodes/node-1/containers/container-1/restart", nil)
	request.RemoteAddr = "[2001:db8::1]:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	request.Header.Set("X-Request-ID", "caller-controlled")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted || len(recorder.events) != 1 {
		t.Fatalf("response/events = %d/%d", response.Code, len(recorder.events))
	}
	event := recorder.events[0]
	if event.RequestID == "" || event.RequestID == "caller-controlled" || response.Header().Get("X-Request-ID") != event.RequestID {
		t.Fatalf("request ID = %q, header = %q", event.RequestID, response.Header().Get("X-Request-ID"))
	}
	if event.SourceIP != "2001:db8::1" || event.ActorType != ActorAdmin || event.Result != ResultAccepted {
		t.Fatalf("event = %+v", event)
	}
	if event.Metadata["force"] != "false" || event.TargetID != "container-1" {
		t.Fatalf("safe fields = %+v", event)
	}
}

func TestMiddlewareWriteFailureDoesNotReplaceOperationResponse(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })
	recorder := &memoryRecorder{err: errors.New("secret database detail")}
	handler := Middleware(recorder, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetPrincipal(r, ActorLocalAdmin, "local")
		Mark(r, "settings", "update")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	request := httptest.NewRequest(http.MethodPut, "http://panel/api/settings", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"ok":true}` || len(recorder.events) != 1 {
		t.Fatalf("response = %d %q, events=%d", response.Code, response.Body.String(), len(recorder.events))
	}
	if strings.Contains(logs.String(), "secret database detail") || !strings.Contains(logs.String(), "class=storage_error") {
		t.Fatalf("audit failure log was not sanitized: %s", logs.String())
	}
}

func TestRecordCreatesExplicitSessionEventsWithoutMarkingRequest(t *testing.T) {
	recorder := &memoryRecorder{}
	handler := Middleware(recorder, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		SetPrincipal(r, ActorAdmin, "admin")
		Record(r, RecordOptions{Module: "terminal", Action: "session_open", TargetType: "node", TargetID: "node-1", NodeID: "node-1", Result: ResultSuccess})
		Record(r, RecordOptions{Module: "terminal", Action: "session_close", TargetType: "node", TargetID: "node-1", NodeID: "node-1", Result: ResultSuccess, Duration: 3 * time.Second})
	}))
	request := httptest.NewRequest(http.MethodGet, "http://panel/api/nodes/node-1/terminal/ws?token=top-secret", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if len(recorder.events) != 2 || recorder.events[0].RequestID != recorder.events[1].RequestID || recorder.events[1].DurationMS != 3000 {
		t.Fatalf("events = %+v", recorder.events)
	}
	for _, event := range recorder.events {
		if bytes.Contains([]byte(event.TargetID+event.Summary), []byte("top-secret")) {
			t.Fatalf("event leaked query token: %+v", event)
		}
	}
}

func TestSourceIPAndFallbackRequestID(t *testing.T) {
	for input, expected := range map[string]string{
		"192.0.2.3:99":   "192.0.2.3",
		"2001:db8::2":    "2001:db8::2",
		"not-an-address": "",
	} {
		if actual := SourceIP(input); actual != expected {
			t.Errorf("SourceIP(%q) = %q, want %q", input, actual, expected)
		}
	}
	id := newRequestID(bytes.NewReader(nil), time.Unix(123, 456))
	if id == "" || id == "caller" {
		t.Fatalf("fallback ID = %q", id)
	}
}
