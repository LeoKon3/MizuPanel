package taskrunner

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

const (
	MaxScriptBytes        = protocol.ScriptExecutionMaxScriptBytes
	MaxOutputBytes        = protocol.ScriptExecutionMaxOutputBytes
	DefaultTimeoutSeconds = protocol.ScriptExecutionDefaultTimeoutSeconds
	MaxTimeoutSeconds     = protocol.ScriptExecutionMaxTimeoutSeconds
	MaxConcurrentRuns     = 2
)

type executionResult struct {
	status          string
	exitCode        *int
	output          string
	outputTruncated bool
	err             string
}

type executeFunc func(context.Context, string, time.Duration) executionResult

type Runner struct {
	supported bool
	slots     chan struct{}
	execute   executeFunc
}

func New() *Runner {
	return &Runner{
		supported: platformSupported(),
		slots:     make(chan struct{}, MaxConcurrentRuns),
		execute:   executeScript,
	}
}

func (r *Runner) Supported() bool {
	return r != nil && r.supported
}

func (r *Runner) Run(ctx context.Context, request protocol.ScriptExecutionRequest) (response protocol.ScriptExecutionResponse) {
	startedAt := time.Now()
	response = protocol.ScriptExecutionResponse{
		Type:        protocol.MessageTypeScriptExecutionResponse,
		RequestID:   request.RequestID,
		ExecutionID: request.ExecutionID,
		Status:      protocol.ScriptExecutionStatusFailed,
		Output:      "",
	}
	defer func() {
		response.DurationMS = time.Since(startedAt).Milliseconds()
	}()

	if !r.Supported() {
		response.Status = protocol.ScriptExecutionStatusUnsupported
		response.Error = "script execution is not supported on this platform"
		return response
	}
	if len(request.Script) > MaxScriptBytes {
		response.Error = "script exceeds the 128 KiB limit"
		return response
	}
	timeoutSeconds := request.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = DefaultTimeoutSeconds
	}
	if timeoutSeconds < 1 || timeoutSeconds > MaxTimeoutSeconds {
		response.Error = "timeout must be between 1 and 1800 seconds"
		return response
	}
	if err := ctx.Err(); err != nil {
		response.Status = protocol.ScriptExecutionStatusCancelled
		response.Error = "script execution was cancelled"
		return response
	}

	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	default:
		response.Status = protocol.ScriptExecutionStatusBusy
		response.Error = "script execution concurrency limit reached"
		return response
	}

	result := r.execute(ctx, request.Script, time.Duration(timeoutSeconds)*time.Second)
	response.Status = result.status
	response.ExitCode = result.exitCode
	response.Output = boundValidUTF8(result.output, MaxOutputBytes)
	response.OutputTruncated = result.outputTruncated || len(response.Output) < len(result.output)
	response.Error = result.err
	return response
}

type boundedWriter struct {
	mu        sync.Mutex
	buf       []byte
	limit     int
	truncated bool
}

func newBoundedWriter(limit int) *boundedWriter {
	return &boundedWriter{limit: limit, buf: make([]byte, 0, limit)}
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	remaining := w.limit - len(w.buf)
	if remaining > 0 {
		keep := len(data)
		if keep > remaining {
			keep = remaining
		}
		w.buf = append(w.buf, data[:keep]...)
	}
	if len(data) > remaining {
		w.truncated = true
	}
	return len(data), nil
}

func (w *boundedWriter) result() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(append([]byte(nil), w.buf...)), w.truncated
}

func boundValidUTF8(value string, limit int) string {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "\uFFFD")
	}
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
