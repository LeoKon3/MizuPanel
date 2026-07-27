package taskrunner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func TestRunnerValidatesBoundsWithoutExecuting(t *testing.T) {
	runner := New()
	runner.supported = true
	called := false
	runner.execute = func(context.Context, string, time.Duration) executionResult {
		called = true
		return executionResult{status: protocol.ScriptExecutionStatusSuccess}
	}

	tests := []protocol.ScriptExecutionRequest{
		{Script: strings.Repeat("x", MaxScriptBytes+1), TimeoutSeconds: 1},
		{Script: "true", TimeoutSeconds: -1},
		{Script: "true", TimeoutSeconds: MaxTimeoutSeconds + 1},
	}
	for _, request := range tests {
		response := runner.Run(t.Context(), request)
		if response.Status != protocol.ScriptExecutionStatusFailed || response.Error == "" {
			t.Fatalf("response = %#v", response)
		}
	}
	if called {
		t.Fatal("executor was called for invalid input")
	}
}

func TestRunnerUsesDefaultTimeout(t *testing.T) {
	runner := New()
	runner.supported = true
	var got time.Duration
	runner.execute = func(_ context.Context, _ string, timeout time.Duration) executionResult {
		got = timeout
		exitCode := 0
		return executionResult{status: protocol.ScriptExecutionStatusSuccess, exitCode: &exitCode}
	}
	response := runner.Run(t.Context(), protocol.ScriptExecutionRequest{Script: "true"})
	if response.Status != protocol.ScriptExecutionStatusSuccess || got != DefaultTimeoutSeconds*time.Second {
		t.Fatalf("response/timeout = %#v/%s", response, got)
	}
}

func TestRunnerReturnsBusyAboveConcurrencyLimit(t *testing.T) {
	runner := New()
	runner.supported = true
	started := make(chan struct{}, MaxConcurrentRuns)
	release := make(chan struct{})
	runner.execute = func(context.Context, string, time.Duration) executionResult {
		started <- struct{}{}
		<-release
		exitCode := 0
		return executionResult{status: protocol.ScriptExecutionStatusSuccess, exitCode: &exitCode}
	}

	var wg sync.WaitGroup
	for range MaxConcurrentRuns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner.Run(t.Context(), protocol.ScriptExecutionRequest{Script: "true", TimeoutSeconds: 1})
		}()
	}
	for range MaxConcurrentRuns {
		<-started
	}
	response := runner.Run(t.Context(), protocol.ScriptExecutionRequest{Script: "true", TimeoutSeconds: 1})
	if response.Status != protocol.ScriptExecutionStatusBusy {
		t.Fatalf("response = %#v", response)
	}
	close(release)
	wg.Wait()
}

func TestBoundedWriterDropsExcessBytes(t *testing.T) {
	writer := newBoundedWriter(4)
	if n, err := writer.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if n, err := writer.Write([]byte("gh")); err != nil || n != 2 {
		t.Fatalf("second Write = %d, %v", n, err)
	}
	value, truncated := writer.result()
	if value != "abcd" || !truncated {
		t.Fatalf("result = %q, %v", value, truncated)
	}
}

func TestBoundValidUTF8NeverSplitsRune(t *testing.T) {
	value := boundValidUTF8("abc\xff界", 5)
	if len(value) > 5 || !utf8.ValidString(value) {
		t.Fatalf("value = %q (%d bytes)", value, len(value))
	}
}

func TestRunnerUnsupportedPlatform(t *testing.T) {
	runner := &Runner{slots: make(chan struct{}, MaxConcurrentRuns), execute: executeScript}
	response := runner.Run(t.Context(), protocol.ScriptExecutionRequest{RequestID: "req-1", ExecutionID: 7, Script: "true", TimeoutSeconds: 1})
	if response.Status != protocol.ScriptExecutionStatusUnsupported || response.RequestID != "req-1" || response.ExecutionID != 7 {
		t.Fatalf("response = %#v", response)
	}
}

func TestPlatformSupportMatchesRuntime(t *testing.T) {
	if New().Supported() != (runtime.GOOS == "linux") {
		t.Fatalf("Supported() = %v on %s", New().Supported(), runtime.GOOS)
	}
}

func TestLinuxRunnerExecutesWithPrivateFilesAndMinimalEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux runner test")
	}
	t.Setenv("MIZUPANEL_SECRET_MARKER", "must-not-leak")
	runner := New()
	script := `
printf '%s\n' "$HOME"
printf '%s %s\n' "$(/usr/bin/stat -c '%a' "$HOME")" "$(/usr/bin/stat -c '%a' "$0")"
if [ -n "${MIZUPANEL_SECRET_MARKER+x}" ]; then exit 91; fi
printf '%s|%s\n' "$PATH" "$LANG"
`
	response := runner.Run(t.Context(), protocol.ScriptExecutionRequest{RequestID: "req-1", ExecutionID: 9, Script: script, TimeoutSeconds: 5})
	if response.Status != protocol.ScriptExecutionStatusSuccess || response.ExitCode == nil || *response.ExitCode != 0 {
		t.Fatalf("response = %#v", response)
	}
	lines := strings.Split(strings.TrimSpace(response.Output), "\n")
	if len(lines) != 3 || lines[1] != "700 600" || !strings.Contains(lines[2], "/usr/bin") || !strings.HasSuffix(lines[2], "|C.UTF-8") {
		t.Fatalf("output = %q", response.Output)
	}
	if _, err := os.Stat(lines[0]); !os.IsNotExist(err) {
		t.Fatalf("temporary HOME still exists, stat error = %v", err)
	}
}

func TestLinuxRunnerReportsNonZeroExitAndBoundsOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux runner test")
	}
	runner := New()
	response := runner.Run(t.Context(), protocol.ScriptExecutionRequest{Script: "/usr/bin/head -c 70000 /dev/zero | /usr/bin/tr '\\000' x\nexit 7\n", TimeoutSeconds: 5})
	if response.Status != protocol.ScriptExecutionStatusFailed || response.ExitCode == nil || *response.ExitCode != 7 || !response.OutputTruncated || len(response.Output) != MaxOutputBytes {
		t.Fatalf("response status/exit/truncation/output = %s/%v/%v/%d", response.Status, response.ExitCode, response.OutputTruncated, len(response.Output))
	}
}

func TestLinuxRunnerTimeoutKillsProcessGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux runner test")
	}
	marker := filepath.Join(t.TempDir(), "descendant-finished")
	script := fmt.Sprintf("(/bin/sleep 2; /usr/bin/touch %q) &\nwait\n", marker)
	response := New().Run(t.Context(), protocol.ScriptExecutionRequest{Script: script, TimeoutSeconds: 1})
	if response.Status != protocol.ScriptExecutionStatusTimedOut || response.ExitCode != nil {
		t.Fatalf("response = %#v", response)
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("descendant survived process-group timeout, stat error = %v", err)
	}
}

func TestLinuxRunnerCancellationKillsExecution(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux runner test")
	}
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)
	startedAt := time.Now()
	response := New().Run(ctx, protocol.ScriptExecutionRequest{Script: "/bin/sleep 30\n", TimeoutSeconds: 30})
	if response.Status != protocol.ScriptExecutionStatusCancelled || response.ExitCode != nil || time.Since(startedAt) > 3*time.Second {
		t.Fatalf("response/elapsed = %#v/%s", response, time.Since(startedAt))
	}
}

func TestLinuxRunnerTimeoutDoesNotWaitForDetachedOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux runner test")
	}
	setsidPath, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("setsid is unavailable")
	}
	pidFile := filepath.Join(t.TempDir(), "detached.pid")
	cleanupDetachedProcess(t, pidFile)
	script := fmt.Sprintf("printf '%%s\\n' \"$HOME\"\n%q /bin/sh -c 'echo $$ > %q; exec /bin/sleep 30' &\nwait\n", setsidPath, pidFile)
	startedAt := time.Now()
	response := New().Run(t.Context(), protocol.ScriptExecutionRequest{Script: script, TimeoutSeconds: 1})
	elapsed := time.Since(startedAt)
	if response.Status != protocol.ScriptExecutionStatusTimedOut || response.ExitCode != nil || elapsed > time.Second+processIOWaitDelay+2*time.Second {
		t.Fatalf("response/elapsed = %#v/%s", response, elapsed)
	}
	tempDir := strings.TrimSpace(strings.Split(response.Output, "\n")[0])
	if tempDir == "" {
		t.Fatalf("missing temporary HOME in output %q", response.Output)
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("temporary HOME still exists, stat error = %v", err)
	}
}

func TestLinuxRunnerRejectsDetachedOutputAfterSuccessfulParentExit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux runner test")
	}
	setsidPath, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("setsid is unavailable")
	}
	pidFile := filepath.Join(t.TempDir(), "detached.pid")
	cleanupDetachedProcess(t, pidFile)
	script := fmt.Sprintf("%q /bin/sh -c 'echo $$ > %q; exec /bin/sleep 30' &\nexit 0\n", setsidPath, pidFile)
	startedAt := time.Now()
	response := New().Run(t.Context(), protocol.ScriptExecutionRequest{Script: script, TimeoutSeconds: 10})
	elapsed := time.Since(startedAt)
	if response.Status != protocol.ScriptExecutionStatusFailed || response.ExitCode != nil || response.Error != "script left background processes running" || elapsed > processIOWaitDelay+2*time.Second {
		t.Fatalf("response/elapsed = %#v/%s", response, elapsed)
	}
}

func cleanupDetachedProcess(t *testing.T, pidFile string) {
	t.Helper()
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 {
			return
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	})
}
