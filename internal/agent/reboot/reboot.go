package reboot

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type Runner interface {
	Run(context.Context, string, ...string) error
}

type commandRunner struct{}

func CurrentOS() string {
	return runtime.GOOS
}

// Run executes the reboot command synchronously and returns the final result.
// It is retained for direct callers and tests that need the terminal outcome.
func Run(ctx context.Context, goos string, runner Runner) protocol.RebootResponse {
	if goos != "linux" {
		return protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, Code: "unsupported", Error: "当前平台暂不支持重启。"}
	}
	if runner == nil {
		runner = commandRunner{}
	}
	if err := runner.Run(ctx, "systemctl", "reboot"); err != nil {
		code := "failed"
		message := err.Error()
		if permissionDenied(message) {
			code = "permission_denied"
			message = "权限不足：当前 Agent 运行用户无权重启机器。"
		}
		return protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, Code: code, Error: message}
	}
	return protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, Accepted: true}
}

// Accept schedules the reboot command asynchronously and immediately returns
// an accepted response. The caller should write this response before the
// reboot takes effect, so the Server does not time out and misreport a
// successful reboot as a failure.
//
// On non-Linux platforms the command is never scheduled and the response
// carries Code "unsupported" so callers can short-circuit before applying any
// delay.
func Accept(goos string, runner Runner, delay time.Duration, executor func(func())) protocol.RebootResponse {
	if goos != "linux" {
		return protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, Code: "unsupported", Error: "当前平台暂不支持重启。"}
	}
	if runner == nil {
		runner = commandRunner{}
	}
	if executor == nil {
		executor = func(fn func()) { go fn() }
	}
	executor(func() {
		time.Sleep(delay)
		_ = runner.Run(context.Background(), "systemctl", "reboot")
	})
	return protocol.RebootResponse{Type: protocol.MessageTypeRebootResponse, Accepted: true, Code: "accepted"}
}

func (commandRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func permissionDenied(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "permission denied") || strings.Contains(message, "access denied") || strings.Contains(message, "not authorized") || strings.Contains(message, "interactive authentication required")
}
