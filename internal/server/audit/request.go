package audit

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const persistTimeout = 750 * time.Millisecond

type Recorder interface {
	Create(ctx context.Context, event *Event) error
}

type requestKey struct{}

type requestState struct {
	mu         sync.Mutex
	recorder   Recorder
	requestID  string
	startedAt  time.Time
	actorType  string
	actorName  string
	sourceIP   string
	marked     bool
	module     string
	action     string
	targetType string
	targetID   string
	targetName string
	nodeID     string
	result     string
	summary    string
	metadata   map[string]string
}

type RecordOptions struct {
	Module     string
	Action     string
	TargetType string
	TargetID   string
	TargetName string
	NodeID     string
	Result     string
	Summary    string
	Duration   time.Duration
	Metadata   map[string]string
}

var fallbackRequestCounter atomic.Uint64

func Middleware(recorder Recorder, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		startedAt := started.UTC()
		state := &requestState{
			recorder:  recorder,
			requestID: newRequestID(rand.Reader, startedAt),
			startedAt: startedAt,
			actorType: ActorUnauthenticated,
			sourceIP:  SourceIP(r.RemoteAddr),
			metadata:  make(map[string]string),
		}
		w.Header().Set("X-Request-ID", state.requestID)
		capture := &responseCapture{ResponseWriter: w}
		next.ServeHTTP(capture, r.WithContext(context.WithValue(r.Context(), requestKey{}, state)))
		if event, ok := state.operationEvent(capture.statusCode(), time.Since(started)); ok {
			persist(state.recorder, &event)
		}
	})
}

func Mark(r *http.Request, module, action string) {
	state := stateFromRequest(r)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.marked = true
	state.module = bounded(strings.ToLower(module), maxIdentifierLen)
	state.action = bounded(strings.ToLower(action), maxIdentifierLen)
	state.targetType = ""
	state.targetID = ""
	state.targetName = ""
	state.nodeID = ""
	state.result = ""
	state.summary = ""
	state.metadata = make(map[string]string)
}

func Unmark(r *http.Request) {
	if state := stateFromRequest(r); state != nil {
		state.mu.Lock()
		state.marked = false
		state.mu.Unlock()
	}
}

func SetPrincipal(r *http.Request, actorType, actorName string) {
	state := stateFromRequest(r)
	if state == nil {
		return
	}
	if !validActorType(actorType) {
		actorType = ActorUnauthenticated
	}
	state.mu.Lock()
	state.actorType = actorType
	state.actorName = bounded(actorName, maxActorNameLength)
	state.mu.Unlock()
}

func SetAction(r *http.Request, action string) {
	if state := stateFromRequest(r); state != nil {
		action = bounded(strings.ToLower(action), maxIdentifierLen)
		if !identifier.MatchString(action) {
			return
		}
		state.mu.Lock()
		state.action = action
		state.mu.Unlock()
	}
}

func SetTarget(r *http.Request, targetType, targetID, targetName string) {
	state := stateFromRequest(r)
	if state == nil {
		return
	}
	targetType = bounded(strings.ToLower(targetType), maxIdentifierLen)
	if targetType != "" && !identifier.MatchString(targetType) {
		targetType = "resource"
	}
	state.mu.Lock()
	state.targetType = targetType
	state.targetID = bounded(targetID, maxTargetIDLength)
	state.targetName = bounded(targetName, maxTargetNameLen)
	state.mu.Unlock()
}

func SetNodeID(r *http.Request, nodeID string) {
	if state := stateFromRequest(r); state != nil {
		state.mu.Lock()
		state.nodeID = bounded(nodeID, maxNodeIDLength)
		state.mu.Unlock()
	}
}

func SetMetadata(r *http.Request, key, value string) {
	state := stateFromRequest(r)
	if state == nil {
		return
	}
	key = bounded(strings.ToLower(key), maxMetadataKeyLen)
	if !identifier.MatchString(key) {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if _, exists := state.metadata[key]; !exists && len(state.metadata) >= MaxMetadataItems {
		return
	}
	state.metadata[key] = bounded(value, maxMetadataValue)
}

func SetResult(r *http.Request, result, summary string) {
	state := stateFromRequest(r)
	if state == nil {
		return
	}
	if !validResult(result) {
		return
	}
	state.mu.Lock()
	state.result = result
	state.summary = bounded(strings.ToLower(summary), maxSummaryLength)
	state.mu.Unlock()
}

func RequestID(r *http.Request) string {
	if state := stateFromRequest(r); state != nil {
		return state.requestID
	}
	return ""
}

func Record(r *http.Request, options RecordOptions) {
	state := stateFromRequest(r)
	if state == nil || state.recorder == nil {
		return
	}
	state.mu.Lock()
	event := Event{
		RequestID:  state.requestID,
		CreatedAt:  time.Now().UTC(),
		ActorType:  state.actorType,
		ActorName:  state.actorName,
		SourceIP:   state.sourceIP,
		Module:     bounded(strings.ToLower(options.Module), maxIdentifierLen),
		Action:     bounded(strings.ToLower(options.Action), maxIdentifierLen),
		TargetType: bounded(strings.ToLower(options.TargetType), maxIdentifierLen),
		TargetID:   bounded(options.TargetID, maxTargetIDLength),
		TargetName: bounded(options.TargetName, maxTargetNameLen),
		NodeID:     bounded(options.NodeID, maxNodeIDLength),
		Result:     options.Result,
		DurationMS: max(options.Duration.Milliseconds(), 0),
		Summary:    bounded(strings.ToLower(options.Summary), maxSummaryLength),
		Metadata:   boundedMetadata(options.Metadata),
	}
	state.mu.Unlock()
	if !validResult(event.Result) {
		event.Result = ResultSuccess
	}
	persist(state.recorder, &event)
}

func SourceIP(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		if parsed := net.ParseIP(strings.Trim(host, "[]")); parsed != nil {
			return parsed.String()
		}
		return ""
	}
	if parsed := net.ParseIP(strings.Trim(remoteAddr, "[]")); parsed != nil {
		return parsed.String()
	}
	return ""
}

func stateFromRequest(r *http.Request) *requestState {
	if r == nil {
		return nil
	}
	state, _ := r.Context().Value(requestKey{}).(*requestState)
	return state
}

func (s *requestState) operationEvent(status int, duration time.Duration) (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.marked {
		return Event{}, false
	}
	result := s.result
	if result == "" {
		result = resultForStatus(status)
	}
	summary := s.summary
	if summary == "" {
		summary = summaryForStatus(status, result)
	}
	return Event{
		RequestID:  s.requestID,
		CreatedAt:  s.startedAt,
		ActorType:  s.actorType,
		ActorName:  s.actorName,
		SourceIP:   s.sourceIP,
		Module:     s.module,
		Action:     s.action,
		TargetType: s.targetType,
		TargetID:   s.targetID,
		TargetName: s.targetName,
		NodeID:     s.nodeID,
		Result:     result,
		DurationMS: max(duration.Milliseconds(), 0),
		Summary:    summary,
		Metadata:   boundedMetadata(s.metadata),
	}, true
}

func resultForStatus(status int) string {
	if status == http.StatusAccepted {
		return ResultAccepted
	}
	if status >= 200 && status < 400 {
		return ResultSuccess
	}
	return ResultFailure
}

func summaryForStatus(status int, result string) string {
	if result == ResultAccepted {
		return "accepted"
	}
	if result == ResultSuccess {
		return "completed"
	}
	switch status {
	case http.StatusBadRequest, http.StatusMethodNotAllowed, http.StatusRequestEntityTooLarge:
		return "invalid_request"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication_failed"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "remote_operation_failed"
	default:
		return "internal_error"
	}
}

func boundedMetadata(metadata map[string]string) map[string]string {
	result := make(map[string]string)
	for key, value := range metadata {
		key = bounded(strings.ToLower(key), maxMetadataKeyLen)
		if !identifier.MatchString(key) || len(result) >= MaxMetadataItems {
			continue
		}
		result[key] = bounded(value, maxMetadataValue)
	}
	return result
}

func persist(recorder Recorder, event *Event) {
	if recorder == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	if err := recorder.Create(ctx, event); err != nil {
		log.Printf("audit write failed request_id=%s module=%s action=%s class=%s", event.RequestID, event.Module, event.Action, storeErrorClass(err))
	}
}

func storeErrorClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	return "storage_error"
}

func newRequestID(source io.Reader, now time.Time) string {
	var bytes [16]byte
	if _, err := io.ReadFull(source, bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("fallback-%x-%x", now.UnixNano(), fallbackRequestCounter.Add(1))
}

type responseCapture struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseCapture) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseCapture) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseCapture) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(value)
	w.bytes += int64(n)
	return n, err
}

func (w *responseCapture) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *responseCapture) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *responseCapture) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (w *responseCapture) ReadFrom(reader io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := readerFrom.ReadFrom(reader)
		w.bytes += n
		return n, err
	}
	n, err := io.Copy(struct{ io.Writer }{w}, reader)
	return n, err
}
