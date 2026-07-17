package upgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func validRequest() protocol.AgentUpgradeRequest {
	return protocol.AgentUpgradeRequest{TargetVersion: "0.1.2", DownloadURL: "http://panel.example.com/downloads/mizupanel-agent-linux-" + runtime.GOARCH, SHA256: strings.Repeat("a", 64)}
}

func TestValidateAcceptsCurrentServerFixedPackage(t *testing.T) {
	u := &Upgrader{serverURL: "ws://panel.example.com/api/agent/ws", currentVersion: "0.1.1", mode: "ops"}
	if err := u.validate(validRequest()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsCrossOriginAndArbitraryPath(t *testing.T) {
	u := &Upgrader{serverURL: "wss://panel.example.com/api/agent/ws", currentVersion: "0.1.1", mode: "ops"}
	request := validRequest()
	request.DownloadURL = "https://evil.example.com/downloads/mizupanel-agent-linux-" + runtime.GOARCH
	if err := u.validate(request); err == nil {
		t.Fatal("cross-origin URL accepted")
	}
	request = validRequest()
	request.DownloadURL = "https://panel.example.com/arbitrary"
	if err := u.validate(request); err == nil {
		t.Fatal("arbitrary path accepted")
	}
}

func TestValidateRejectsNormalModeAndCurrentVersion(t *testing.T) {
	request := validRequest()
	if err := (&Upgrader{serverURL: "ws://panel.example.com", currentVersion: "0.1.1", mode: "normal"}).validate(request); err == nil {
		t.Fatal("normal mode accepted")
	}
	request.TargetVersion = "0.1.1"
	if err := (&Upgrader{serverURL: "ws://panel.example.com", currentVersion: "0.1.1", mode: "ops"}).validate(request); err == nil {
		t.Fatal("current version accepted")
	}
}

func TestStartReportsRestartFailureAndRestoresPreviousBinary(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "bin", "mizupanel-agent")
	if err := os.MkdirAll(filepath.Dir(executable), 0755); err != nil {
		t.Fatal(err)
	}
	oldBinary := []byte("old-agent")
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	newBinary, err := os.ReadFile(testExecutable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, oldBinary, 0755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(newBinary)
	restartCalls := 0
	recoveryScheduled := false
	recoveryCanceled := false
	u := &Upgrader{
		serverURL:      "ws://panel.example.com/api/agent/ws",
		currentVersion: "0.1.1",
		mode:           "ops",
		executable:     executable,
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(newBinary)))}, nil
		})},
		restart: func() error {
			restartCalls++
			if restartCalls == 1 {
				return errors.New("restart rejected")
			}
			return nil
		},
		sleep: func(time.Duration) {},
		scheduleRecovery: func(script string, marker string, backup string, current string) error {
			recoveryScheduled = true
			for _, path := range []string{script, marker, backup, current} {
				if _, err := os.Stat(path); err != nil {
					return err
				}
			}
			return nil
		},
		cancelRecovery: func(marker string) {
			recoveryCanceled = true
			_ = os.Remove(marker)
		},
	}
	request := validRequest()
	request.SHA256 = hex.EncodeToString(sum[:])
	reported := make(chan protocol.AgentUpgradeResponse, 1)
	response := u.Start(request, func(result protocol.AgentUpgradeResponse) { reported <- result })
	if !response.Accepted {
		t.Fatalf("Start response = %#v", response)
	}
	select {
	case result := <-reported:
		if result.Stage != "failed" || result.Code != "restart_failed" || !strings.Contains(result.Error, "已恢复旧版本") {
			t.Fatalf("reported result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for asynchronous failure report")
	}
	content, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(oldBinary) || restartCalls != 2 || !recoveryScheduled || !recoveryCanceled {
		t.Fatalf("binary/restart calls = %q/%d, want restored old binary and two restarts", content, restartCalls)
	}
}

func TestConfirmSuccessfulStartCancelsPendingRecovery(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "bin", "mizupanel-agent")
	marker := filepath.Join(root, "var", "upgrade", "pending")
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("0.1.2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	canceled := ""
	u := &Upgrader{executable: executable, cancelRecovery: func(path string) {
		canceled = path
		_ = os.Remove(path)
	}}
	u.ConfirmSuccessfulStart()
	if canceled != marker {
		t.Fatalf("canceled marker = %q, want %q", canceled, marker)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker still exists: %v", err)
	}
}
