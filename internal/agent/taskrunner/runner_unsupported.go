//go:build !linux

package taskrunner

import (
	"context"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

func platformSupported() bool {
	return false
}

func executeScript(context.Context, string, time.Duration) executionResult {
	return executionResult{
		status: protocol.ScriptExecutionStatusUnsupported,
		err:    "script execution is not supported on this platform",
	}
}
