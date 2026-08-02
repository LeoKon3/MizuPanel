package reboot

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

)

type fakeRunner struct {
	calls   atomic.Int32
	errFunc func() error
}

func (f *fakeRunner) Run(context.Context, string, ...string) error {
	f.calls.Add(1)
	if f.errFunc != nil {
		return f.errFunc()
	}
	return nil
}

func TestRunNonLinuxReturnsUnsupported(t *testing.T) {
	resp := Run(context.Background(), "windows", nil)
	if resp.Code != "unsupported" {
		t.Fatalf("expected unsupported, got %s", resp.Code)
	}
	if resp.Accepted {
		t.Fatal("windows should not be accepted")
	}
}

func TestRunLinuxSuccessReturnsAccepted(t *testing.T) {
	runner := &fakeRunner{}
	resp := Run(context.Background(), "linux", runner)
	if !resp.Accepted {
		t.Fatal("expected accepted")
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", runner.calls.Load())
	}
}

func TestRunLinuxFailureReturnsErrorCode(t *testing.T) {
	runner := &fakeRunner{errFunc: func() error { return context.DeadlineExceeded }}
	resp := Run(context.Background(), "linux", runner)
	if resp.Accepted {
		t.Fatal("should not be accepted on failure")
	}
	if resp.Code != "failed" {
		t.Fatalf("expected failed, got %s", resp.Code)
	}
}

func TestRunLinuxPermissionDeniedReturnsPermissionCode(t *testing.T) {
	runner := &fakeRunner{errFunc: func() error {
		return &permErr{msg: "permission denied"}
	}}
	resp := Run(context.Background(), "linux", runner)
	if resp.Code != "permission_denied" {
		t.Fatalf("expected permission_denied, got %s", resp.Code)
	}
}

type permErr struct{ msg string }

func (e *permErr) Error() string { return e.msg }

func TestAcceptNonLinuxReturnsUnsupported(t *testing.T) {
	resp := Accept("darwin", nil, 0, nil)
	if resp.Code != "unsupported" {
		t.Fatalf("expected unsupported, got %s", resp.Code)
	}
}

func TestAcceptLinuxReturnsAcceptedImmediatelyAndSchedulesReboot(t *testing.T) {
	runner := &fakeRunner{}
	done := make(chan struct{})
	executor := func(fn func()) {
		go func() {
			fn()
			close(done)
		}()
	}
	resp := Accept("linux", runner, 1*time.Millisecond, executor)
	if !resp.Accepted {
		t.Fatal("expected accepted immediately")
	}
	if resp.Code != "accepted" {
		t.Fatalf("expected code accepted, got %s", resp.Code)
	}
	if runner.calls.Load() != 0 {
		t.Fatal("reboot should not have run synchronously")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reboot was not scheduled within timeout")
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("expected 1 deferred call, got %d", runner.calls.Load())
	}
}

func TestAcceptLinuxFailureIsSilentlyIgnored(t *testing.T) {
	runner := &fakeRunner{errFunc: func() error { return context.DeadlineExceeded }}
	done := make(chan struct{})
	executor := func(fn func()) {
		go func() {
			fn()
			close(done)
		}()
	}
	resp := Accept("linux", runner, 1*time.Millisecond, executor)
	if !resp.Accepted {
		t.Fatal("accepted must be true regardless of deferred failure")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled function did not execute")
	}
}
