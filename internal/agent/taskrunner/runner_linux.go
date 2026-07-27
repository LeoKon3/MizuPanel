//go:build linux

package taskrunner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

const processIOWaitDelay = 500 * time.Millisecond

func platformSupported() bool {
	return true
}

func executeScript(ctx context.Context, script string, timeout time.Duration) executionResult {
	tempDir, err := os.MkdirTemp("", "mizupanel-task-")
	if err != nil {
		return executionResult{status: protocol.ScriptExecutionStatusFailed, err: "failed to prepare script"}
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0700); err != nil {
		return executionResult{status: protocol.ScriptExecutionStatusFailed, err: "failed to prepare script"}
	}

	scriptPath := filepath.Join(tempDir, "task.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return executionResult{status: protocol.ScriptExecutionStatusFailed, err: "failed to prepare script"}
	}
	if err := os.Chmod(scriptPath, 0600); err != nil {
		return executionResult{status: protocol.ScriptExecutionStatusFailed, err: "failed to prepare script"}
	}

	output := newBoundedWriter(MaxOutputBytes)
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Dir = tempDir
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + tempDir,
		"LANG=C.UTF-8",
	}
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = processIOWaitDelay
	if err := cmd.Start(); err != nil {
		return executionResult{status: protocol.ScriptExecutionStatusFailed, err: "failed to start script"}
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var (
		waitErr error
		status  string
		errText string
	)
	select {
	case waitErr = <-waitCh:
		status = protocol.ScriptExecutionStatusSuccess
	case <-timer.C:
		status = protocol.ScriptExecutionStatusTimedOut
		errText = "script execution timed out"
		killProcessGroup(cmd.Process)
		waitErr = <-waitCh
	case <-ctx.Done():
		status = protocol.ScriptExecutionStatusCancelled
		errText = "script execution was cancelled"
		killProcessGroup(cmd.Process)
		waitErr = <-waitCh
	}

	// Scripts are not allowed to leave detached work in their process group.
	killProcessGroup(cmd.Process)
	text, truncated := output.result()
	if status == protocol.ScriptExecutionStatusTimedOut || status == protocol.ScriptExecutionStatusCancelled {
		return executionResult{status: status, output: text, outputTruncated: truncated, err: errText}
	}
	if waitErr == nil {
		exitCode := 0
		return executionResult{status: protocol.ScriptExecutionStatusSuccess, exitCode: &exitCode, output: text, outputTruncated: truncated}
	}
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		return executionResult{status: protocol.ScriptExecutionStatusFailed, output: text, outputTruncated: truncated, err: "script left background processes running"}
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		exitCode := exitErr.ExitCode()
		if exitCode >= 0 {
			return executionResult{status: protocol.ScriptExecutionStatusFailed, exitCode: &exitCode, output: text, outputTruncated: truncated, err: "script exited with a non-zero status"}
		}
	}
	return executionResult{status: protocol.ScriptExecutionStatusFailed, output: text, outputTruncated: truncated, err: "script execution failed"}
}

func killProcessGroup(process *os.Process) {
	if process == nil || process.Pid <= 0 {
		return
	}
	_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	_ = process.Kill()
}
