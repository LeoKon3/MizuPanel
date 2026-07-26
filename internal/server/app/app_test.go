package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/api"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/sshops"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type fakeSSHRunner struct {
	install               sshops.InstallRequest
	uninstall             sshops.UninstallRequest
	resolvedInstallNodeID string
}

func (f *fakeSSHRunner) Install(ctx context.Context, request sshops.InstallRequest, emit sshops.EmitFunc) (string, error) {
	f.install = request
	emit(sshops.ProgressEvent{Step: "connect_ssh", Label: "连接 SSH", Status: sshops.ProgressSuccess, Message: "connected with secret"})
	if f.resolvedInstallNodeID != "" {
		return f.resolvedInstallNodeID, nil
	}
	return request.NodeID, nil
}

func (f *fakeSSHRunner) Uninstall(ctx context.Context, request sshops.UninstallRequest, emit sshops.EmitFunc) error {
	f.uninstall = request
	emit(sshops.ProgressEvent{Step: "run_uninstall", Label: "执行卸载", Status: sshops.ProgressSuccess, Message: "uninstalled"})
	return nil
}

type blockingSSHInstallRunner struct {
	started chan context.Context
	release chan struct{}
}

type hangingSSHInstallRunner struct {
	started chan context.Context
}

func (f *blockingSSHInstallRunner) Install(ctx context.Context, request sshops.InstallRequest, emit sshops.EmitFunc) (string, error) {
	f.started <- ctx
	select {
	case <-f.release:
		emit(sshops.ProgressEvent{Step: "run_install", Label: "执行安装", Status: sshops.ProgressSuccess, Message: "installed"})
		return request.NodeID, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (f *blockingSSHInstallRunner) Uninstall(ctx context.Context, request sshops.UninstallRequest, emit sshops.EmitFunc) error {
	return nil
}

func (f *hangingSSHInstallRunner) Install(ctx context.Context, request sshops.InstallRequest, emit sshops.EmitFunc) (string, error) {
	f.started <- ctx
	<-ctx.Done()
	return "", ctx.Err()
}

func (f *hangingSSHInstallRunner) Uninstall(ctx context.Context, request sshops.UninstallRequest, emit sshops.EmitFunc) error {
	return nil
}

func TestNewHandlerProtectsInstallCommandWhenAdminAuthEnabled(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:     store.NewNodeStore(database),
		Metrics:   store.NewMetricStore(database),
		AdminAuth: api.AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s, want 401", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "authentication required") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestNewHandlerLeavesAgentWebSocketOutsideAdminAuth(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:       store.NewNodeStore(database),
		Metrics:     store.NewMetricStore(database),
		AgentTokens: store.NewAgentTokenStore(database),
		AdminAuth:   api.AuthConfig{Enabled: true, Username: "admin", Password: "secret", SessionTTL: time.Hour},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/ws", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code == http.StatusUnauthorized && strings.Contains(recorder.Body.String(), "authentication required") {
		t.Fatalf("agent websocket should not require dashboard auth: status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestNewHandlerWiresUptimeRoutes(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
		Uptime:  store.NewUptimeStore(database),
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/uptime/monitors", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"monitors":[]`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestNewHandlerCreatesInstallCommandWithoutLogin(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command", nil)
	request.Host = "panel.example:8080"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	var response struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	command := response.Command
	if !strings.Contains(command, "curl -fsSL 'http://panel.example:8080/scripts/install-agent.sh'") {
		t.Fatalf("install command missing script URL: %s", body)
	}
	if !strings.Contains(command, "--binary-base-url 'http://panel.example:8080/downloads'") {
		t.Fatalf("install command missing binary base URL: %s", body)
	}
	if strings.Contains(command, "<install_token>") || strings.Contains(command, "agent_token") {
		t.Fatalf("install command exposed placeholder or global token language: %s", body)
	}
	if !strings.Contains(command, "--mode 'ops'") {
		t.Fatalf("default install command missing ops mode: %s", body)
	}
	if strings.Contains(command, "sudo ./install-agent.sh") {
		t.Fatalf("linux install command should be root-only and not use sudo: %s", body)
	}
	if !strings.Contains(command, "&& ./install-agent.sh") {
		t.Fatalf("linux install command should execute installer directly as root: %s", body)
	}
	if !strings.Contains(command, "--enable-docker") {
		t.Fatalf("default install command missing Docker flag: %s", body)
	}
	if !strings.Contains(command, "--enable-terminal") {
		t.Fatalf("default install command missing terminal flag: %s", body)
	}
}

func TestNewHandlerRejectsSSHInstallForNonRootUser(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})

	body := strings.NewReader(`{"host":"192.168.1.10","username":"ubuntu","auth_type":"password","password":"secret"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/ssh", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "root") {
		t.Fatalf("response should explain root-only constraint: %s", recorder.Body.String())
	}
}

func TestNewHandlerSSHInstallJobUsesIndependentTimeout(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	jobs := sshops.NewManager()
	runner := &hangingSSHInstallRunner{started: make(chan context.Context, 1)}
	handler := NewHandler(Dependencies{
		Nodes:                  store.NewNodeStore(database),
		Metrics:                store.NewMetricStore(database),
		SSHJobs:                jobs,
		SSHRunner:              runner,
		SSHJobTimeout:          20 * time.Millisecond,
		SSHInstallWaitTimeout:  100 * time.Millisecond,
		SSHInstallPollInterval: time.Millisecond,
	})

	body := strings.NewReader(`{"host":"192.168.1.10","username":"root","auth_type":"password","password":"secret","node_id":"node-1","name":"Node 1"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/ssh", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s, want 202", recorder.Code, recorder.Body.String())
	}
	var response struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	<-runner.started
	job := jobs.Wait(response.JobID)
	if job == nil || job.Status != sshops.ProgressFailed {
		t.Fatalf("job = %#v, want timeout failure", job)
	}
}

func TestNewHandlerSSHInstallJobOutlivesRequestContext(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	nodes := store.NewNodeStore(database)
	jobs := sshops.NewManager()
	runner := &blockingSSHInstallRunner{started: make(chan context.Context, 1), release: make(chan struct{})}
	handler := NewHandler(Dependencies{
		Nodes:                  nodes,
		Metrics:                store.NewMetricStore(database),
		SSHJobs:                jobs,
		SSHRunner:              runner,
		SSHInstallWaitTimeout:  100 * time.Millisecond,
		SSHInstallPollInterval: time.Millisecond,
	})

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	body := strings.NewReader(`{"host":"192.168.1.10","username":"root","auth_type":"password","password":"secret","node_id":"node-1","name":"Node 1"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/ssh", body).WithContext(requestCtx)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s, want 202", recorder.Code, recorder.Body.String())
	}
	var response struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	jobCtx := <-runner.started
	cancelRequest()
	select {
	case <-jobCtx.Done():
		t.Fatalf("ssh job context was canceled when request context was canceled")
	case <-time.After(20 * time.Millisecond):
	}
	if err := nodes.Upsert(context.Background(), store.Node{ID: "node-1", Name: "Node 1", Status: "online"}); err != nil {
		t.Fatalf("upsert connected node: %v", err)
	}
	close(runner.release)
	job := jobs.Wait(response.JobID)
	if job == nil || job.Status != sshops.ProgressSuccess {
		t.Fatalf("job = %#v, want success", job)
	}
}

func TestNewHandlerSSHInstallJobFailsWhenAgentDoesNotReconnect(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	jobs := sshops.NewManager()
	runner := &fakeSSHRunner{}
	handler := NewHandler(Dependencies{
		Nodes:                  store.NewNodeStore(database),
		Metrics:                store.NewMetricStore(database),
		SSHJobs:                jobs,
		SSHRunner:              runner,
		SSHInstallWaitTimeout:  20 * time.Millisecond,
		SSHInstallPollInterval: time.Millisecond,
	})

	body := strings.NewReader(`{"host":"192.168.1.10","username":"root","auth_type":"password","password":"secret","node_id":"node-1","name":"Node 1"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/ssh", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s, want 202", recorder.Code, recorder.Body.String())
	}
	var response struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	job := jobs.Wait(response.JobID)
	if job == nil || job.Status != sshops.ProgressFailed {
		t.Fatalf("job = %#v, want failed when Agent does not reconnect", job)
	}
	events := jobs.Events(response.JobID)
	last := events[len(events)-1]
	if !strings.Contains(last.Message, "未连回") {
		t.Fatalf("last event = %#v, want reconnect timeout message", last)
	}
}

func TestNewHandlerSSHInstallUsesResolvedNodeIDWhenRequestNodeIDIsEmpty(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	nodes := store.NewNodeStore(database)
	jobs := sshops.NewManager()
	runner := &fakeSSHRunner{resolvedInstallNodeID: "remote-host"}
	handler := NewHandler(Dependencies{
		Nodes:                  nodes,
		Metrics:                store.NewMetricStore(database),
		SSHJobs:                jobs,
		SSHRunner:              runner,
		SSHInstallWaitTimeout:  100 * time.Millisecond,
		SSHInstallPollInterval: time.Millisecond,
	})

	body := strings.NewReader(`{"host":"192.168.1.10","username":"root","auth_type":"password","password":"secret","enable_terminal":true}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/ssh", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s, want 202", recorder.Code, recorder.Body.String())
	}
	var response struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := nodes.Upsert(context.Background(), store.Node{ID: "remote-host", Name: "remote-host", Status: "online"}); err != nil {
		t.Fatalf("upsert resolved node: %v", err)
	}
	job := jobs.Wait(response.JobID)
	if job == nil || job.Status != sshops.ProgressSuccess {
		t.Fatalf("job = %#v, want success after resolved node reconnects", job)
	}
}

func TestNewHandlerStartsSSHInstallJob(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	nodes := store.NewNodeStore(database)
	jobs := sshops.NewManager()
	runner := &fakeSSHRunner{}
	handler := NewHandler(Dependencies{
		Nodes:                  nodes,
		Metrics:                store.NewMetricStore(database),
		SSHJobs:                jobs,
		SSHRunner:              runner,
		SSHInstallWaitTimeout:  100 * time.Millisecond,
		SSHInstallPollInterval: time.Millisecond,
	})

	body := strings.NewReader(`{"host":"192.168.1.10","username":"root","auth_type":"password","password":"secret","node_id":"node-1","name":"Node 1","enable_terminal":false,"enable_docker":false,"mode":"normal"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/ssh", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s, want 202", recorder.Code, recorder.Body.String())
	}
	var response struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.JobID == "" {
		t.Fatalf("missing job_id")
	}
	if err := nodes.Upsert(context.Background(), store.Node{ID: "node-1", Name: "Node 1", Status: "online"}); err != nil {
		t.Fatalf("upsert connected node: %v", err)
	}
	job := jobs.Wait(response.JobID)
	if job == nil || job.Status != sshops.ProgressSuccess {
		t.Fatalf("job = %#v, want success", job)
	}
	if runner.install.Host != "192.168.1.10" || runner.install.Username != "root" || !runner.install.EnableTerminal || !runner.install.EnableDocker || runner.install.Mode != string(installModeOps) {
		t.Fatalf("runner install request = %#v", runner.install)
	}
	events := jobs.Events(response.JobID)
	for _, event := range events {
		if strings.Contains(event.Message, "secret") || strings.Contains(event.Message, runner.install.Token) {
			t.Fatalf("progress event leaked secret: %#v", event)
		}
	}
}

func TestNewHandlerStartsSSHUninstallJobAndRemovesNodeRecord(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	nodes := store.NewNodeStore(database)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "Node 1", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	jobs := sshops.NewManager()
	runner := &fakeSSHRunner{}
	handler := NewHandler(Dependencies{
		Nodes:     nodes,
		Metrics:   store.NewMetricStore(database),
		SSHJobs:   jobs,
		SSHRunner: runner,
	})

	body := strings.NewReader(`{"host":"192.168.1.10","username":"root","auth_type":"password","password":"secret","remove_node_record":true}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/nodes/node-1/ssh-uninstall", body)
	request.Host = "panel.example"
	request.Header.Set("Origin", "http://panel.example")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s, want 202", recorder.Code, recorder.Body.String())
	}
	var response struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	jobs.Wait(response.JobID)
	if runner.uninstall.Host != "192.168.1.10" || !runner.uninstall.RemoveNodeRecord {
		t.Fatalf("runner uninstall request = %#v", runner.uninstall)
	}
	if _, err := nodes.Get(t.Context(), "node-1"); err == nil {
		t.Fatalf("node still exists after uninstall with remove_node_record")
	}
}

func TestNewHandlerCreatesLinuxInstallCommandWithOpsMode(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command?platform=linux&mode=ops", nil)
	request.Host = "panel.example:8080"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "--mode 'ops'") {
		t.Fatalf("ops install command missing mode flag: %s", body)
	}
}

func TestNewHandlerCreatesLinuxInstallCommandWithDockerOptIn(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command?platform=linux&enable_docker=true", nil)
	request.Host = "panel.example:8080"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "--enable-docker") {
		t.Fatalf("linux install command missing Docker opt-in flag: %s", body)
	}
}

func TestNewHandlerCreatesLinuxInstallCommandWithTerminalOptIn(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command?platform=linux&enable_terminal=true", nil)
	request.Host = "panel.example:8080"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "--enable-terminal") {
		t.Fatalf("linux install command missing terminal opt-in flag: %s", body)
	}
}

func TestNewHandlerIgnoresLinuxOptInsForWindowsInstallCommand(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command?platform=windows&enable_docker=true&enable_terminal=true", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, flag := range []string{"--enable-docker", "--enable-terminal"} {
		if strings.Contains(body, flag) {
			t.Fatalf("windows install command contains linux-only opt-in flag %q: %s", flag, body)
		}
	}
}

func TestNewHandlerQuotesHostDerivedInstallCommandURLs(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{Nodes: store.NewNodeStore(database), Metrics: store.NewMetricStore(database)})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command", nil)
	request.Host = "panel.example'$(touch /tmp/pwned)"
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var response struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.Contains(response.Command, "'$(touch /tmp/pwned)'") || strings.Contains(response.Command, "http://panel.example'$(touch") {
		t.Fatalf("install command contains unescaped host payload: %s", response.Command)
	}
}

func TestNewHandlerCreatesInstallCommandFromPublicURL(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:     store.NewNodeStore(database),
		Metrics:   store.NewMetricStore(database),
		PublicURL: "https://panel.example",
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command", nil)
	request.Host = "internal:8080"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "curl -fsSL 'https://panel.example/scripts/install-agent.sh'") {
		t.Fatalf("install command missing public script URL: %s", body)
	}
	if !strings.Contains(body, "--binary-base-url 'https://panel.example/downloads'") {
		t.Fatalf("install command missing public binary base URL: %s", body)
	}
	if !strings.Contains(body, "--server-url 'wss://panel.example/api/agent/ws'") {
		t.Fatalf("install command missing public websocket URL: %s", body)
	}
	if strings.Contains(body, "internal:8080") {
		t.Fatalf("install command leaked internal host: %s", body)
	}
}

func TestNewHandlerCreatesInstallCommandFromPublicURLWithPathPrefix(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:     store.NewNodeStore(database),
		Metrics:   store.NewMetricStore(database),
		PublicURL: "https://panel.example/mizupanel",
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command", nil)
	request.Host = "internal:8080"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "curl -fsSL 'https://panel.example/mizupanel/scripts/install-agent.sh'") {
		t.Fatalf("install command missing public script URL with path prefix: %s", body)
	}
	if !strings.Contains(body, "--binary-base-url 'https://panel.example/mizupanel/downloads'") {
		t.Fatalf("install command missing public binary base URL with path prefix: %s", body)
	}
	if !strings.Contains(body, "--server-url 'wss://panel.example/mizupanel/api/agent/ws'") {
		t.Fatalf("install command missing public websocket URL with path prefix: %s", body)
	}
	if strings.Contains(body, "internal:8080") {
		t.Fatalf("install command leaked internal host: %s", body)
	}
}

func TestNewHandlerCreatesWindowsInstallCommand(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:     store.NewNodeStore(database),
		Metrics:   store.NewMetricStore(database),
		PublicURL: "https://panel.example/mizupanel",
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command?platform=windows", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var response struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode install command response: %v", err)
	}
	body := response.Command
	for _, want := range []string{
		"powershell -NoProfile -ExecutionPolicy Bypass",
		"`$ErrorActionPreference='Stop'",
		"`$script = Join-Path `$env:TEMP",
		"mizupanel-install-",
		"[guid]::NewGuid()",
		"/scripts/install-agent.ps1",
		"Invoke-WebRequest",
		"-UseBasicParsing",
		"-OutFile `$script",
		"-ErrorAction Stop",
		"& `$script",
		"-BinaryBaseUrl 'https://panel.example/mizupanel/downloads'",
		"-ServerUrl 'wss://panel.example/mizupanel/api/agent/ws'",
		"-Token '",
		"-NodeId `$env:COMPUTERNAME",
		"-Name `$env:COMPUTERNAME",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("windows install command missing %q: %s", want, body)
		}
	}
	for _, unsafe := range []string{"\"$ErrorActionPreference='Stop'", " $script =", " $env:TEMP", "-OutFile $script", "& $script", "-NodeId $env:COMPUTERNAME", "-Name $env:COMPUTERNAME"} {
		if strings.Contains(body, unsafe) {
			t.Fatalf("windows install command contains parent-expanded PowerShell variable %q: %s", unsafe, body)
		}
	}
	if strings.Contains(body, "OutFile install-agent.ps1") {
		t.Fatalf("windows install command downloads into a reusable current-directory script: %s", body)
	}
	if strings.Contains(body, "; \\") || strings.Contains(body, "install-agent.sh") || strings.Contains(body, "$(hostname)") {
		t.Fatalf("windows install command contains linux-only fragments: %s", body)
	}
}

func TestNewHandlerRejectsUnknownInstallPlatform(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command?platform=darwin", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestNewHandlerGeneratedInstallTokenRegistersAgentAndReturnsNodeToken(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:    store.NewNodeStore(database),
		Metrics:  store.NewMetricStore(database),
		Interval: 5,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command", nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("install command status = %d, want 200", recorder.Code)
	}
	var response struct {
		InstallToken string `json:"install_token"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode install command response: %v", err)
	}
	if response.InstallToken == "" {
		t.Fatal("install_token is empty")
	}

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/agent/ws?token=" + response.InstallToken
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "node-1", Hostname: "oracle-sg"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var ack protocol.HelloAckMessage
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if ack.NodeToken == "" || ack.NodeToken == response.InstallToken {
		t.Fatalf("NodeToken = %q, want generated node token", ack.NodeToken)
	}
}

func TestServedAgentInstallScriptSelectsBinaryURLForMachineArchitecture(t *testing.T) {
	downloads := make(map[string]int)
	binaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads[r.URL.Path]++
		if r.URL.Path != "/downloads/mizupanel-agent-linux-arm64" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("arm64-agent"))
	}))
	t.Cleanup(binaryServer.Close)

	script := filepath.Join(t.TempDir(), "install-agent.sh")
	if err := os.WriteFile(script, agentInstallScript, 0755); err != nil {
		t.Fatalf("write embedded script: %v", err)
	}
	dest := t.TempDir()
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "uname"), []byte("#!/bin/sh\nif [ \"$1\" = \"-s\" ]; then echo Linux; else echo aarch64; fi\n"), 0755); err != nil {
		t.Fatalf("write fake uname: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "systemctl"), []byte("#!/bin/sh\necho systemctl should not run in dest-root mode >&2\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}

	output, err := runCommand(t, script, map[string]string{"PATH": fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")},
		"--dest-root", dest,
		"--binary-base-url", binaryServer.URL+"/downloads",
		"--server-url", "ws://panel.example.com:8080/api/agent/ws",
		"--token", "install-token",
		"--node-id", "oracle-sg-01",
		"--name", "Oracle SG",
	)
	if err != nil {
		t.Fatalf("embedded install script failed: %v\n%s", err, output)
	}
	if downloads["/downloads/mizupanel-agent-linux-arm64"] != 1 {
		t.Fatalf("arm64 binary downloads = %d, want 1", downloads["/downloads/mizupanel-agent-linux-arm64"])
	}
	installedBinary, err := os.ReadFile(filepath.Join(dest, "usr", "local", "mizupanel", "bin", "mizupanel-agent"))
	if err != nil {
		t.Fatalf("read downloaded binary: %v", err)
	}
	if string(installedBinary) != "arm64-agent" {
		t.Fatalf("installed binary = %q, want arm64 payload", installedBinary)
	}
}

func TestEmbeddedAgentInstallScriptMatchesRepositoryScript(t *testing.T) {
	repositoryScript, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "install-agent.sh"))
	if err != nil {
		t.Fatalf("read repository install script: %v", err)
	}
	if string(agentInstallScript) != string(repositoryScript) {
		t.Fatal("embedded install script differs from scripts/install-agent.sh")
	}
}

func TestEmbeddedWindowsAgentInstallScriptMatchesRepositoryScript(t *testing.T) {
	repositoryScript, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "install-agent.ps1"))
	if err != nil {
		t.Fatalf("read repository windows install script: %v", err)
	}
	if string(windowsAgentInstallScript) != string(repositoryScript) {
		t.Fatal("embedded install script differs from scripts/install-agent.ps1")
	}
}

func TestEmbeddedAgentUninstallScriptMatchesRepositoryScript(t *testing.T) {
	repositoryScript, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "uninstall-agent.sh"))
	if err != nil {
		t.Fatalf("read repository uninstall script: %v", err)
	}
	if string(agentUninstallScript) != string(repositoryScript) {
		t.Fatal("embedded uninstall script differs from scripts/uninstall-agent.sh")
	}
}

func TestEmbeddedWindowsAgentUninstallScriptMatchesRepositoryScript(t *testing.T) {
	repositoryScript, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "uninstall-agent.ps1"))
	if err != nil {
		t.Fatalf("read repository windows uninstall script: %v", err)
	}
	if string(windowsAgentUninstallScript) != string(repositoryScript) {
		t.Fatal("embedded uninstall script differs from scripts/uninstall-agent.ps1")
	}
}

func TestNewHandlerMountsNodeAPIWithoutLogin(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestNewHandlerServesAgentBinaryDownload(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "mizupanel-agent-linux-amd64")
	if err := os.WriteFile(binaryPath, []byte("agent-binary"), 0755); err != nil {
		t.Fatalf("write agent binary: %v", err)
	}

	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:       store.NewNodeStore(database),
		Metrics:     store.NewMetricStore(database),
		DownloadDir: tempDir,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/downloads/mizupanel-agent-linux-amd64", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if recorder.Body.String() != "agent-binary" {
		t.Fatalf("body = %q, want agent binary", recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", contentType)
	}
}

func TestNewHandlerServesAgentInstallScript(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/scripts/install-agent.sh", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/x-shellscript; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want shell script", contentType)
	}
	if body := recorder.Body.String(); body == "" || body[:19] != "#!/usr/bin/env bash" {
		t.Fatalf("unexpected script body prefix: %q", body)
	}
}

func TestNewHandlerServesWindowsAgentInstallScript(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/scripts/install-agent.ps1", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want powershell script", contentType)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "mizupanel-agent-windows-amd64.exe") {
		t.Fatalf("unexpected windows script body: %q", body)
	}
}

func TestNewHandlerServesAgentUninstallScript(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/scripts/uninstall-agent.sh", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/x-shellscript; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want shell script", contentType)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "MizuPanel agent uninstalled") {
		t.Fatalf("unexpected uninstall script body: %q", body)
	}
}

func TestNewHandlerServesWindowsAgentUninstallScript(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/scripts/uninstall-agent.ps1", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want powershell script", contentType)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "MizuPanel agent uninstalled") {
		t.Fatalf("unexpected windows uninstall script body: %q", body)
	}
}

func TestNewHandlerServesWindowsAgentInstallScriptHead(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/scripts/install-agent.ps1", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body length = %d, want 0", recorder.Body.Len())
	}
}

func TestNewHandlerServesEmbeddedWindowsAgentInstallScript(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tempDir, "scripts"), 0755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "scripts", "install-agent.ps1"), []byte("Write-Output 'poisoned'\n"), 0644); err != nil {
		t.Fatalf("write poisoned script: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/scripts/install-agent.ps1", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "poisoned") {
		t.Fatalf("served windows script from process working directory: %q", body)
	}
	if !strings.Contains(body, "MizuPanel agent installed") {
		t.Fatalf("served windows script does not look like bundled installer: %q", body)
	}
}

func TestNewHandlerServesEmbeddedAgentInstallScript(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tempDir, "scripts"), 0755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "scripts", "install-agent.sh"), []byte("#!/usr/bin/env bash\nprintf poisoned\n"), 0644); err != nil {
		t.Fatalf("write poisoned script: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:   store.NewNodeStore(database),
		Metrics: store.NewMetricStore(database),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/scripts/install-agent.sh", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "poisoned") {
		t.Fatalf("served script from process working directory: %q", body)
	}
	if !strings.Contains(body, "MizuPanel agent installed") {
		t.Fatalf("served script does not look like bundled installer: %q", body)
	}
}

func TestServedAgentInstallScriptGeneratesConfig(t *testing.T) {
	script := filepath.Join(t.TempDir(), "install-agent.sh")
	if err := os.WriteFile(script, agentInstallScript, 0755); err != nil {
		t.Fatalf("write embedded script: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "mizupanel-agent")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	dest := t.TempDir()
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "systemctl"), []byte("#!/bin/sh\necho systemctl should not run in dest-root mode >&2\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}

	output, err := runCommand(t, script, map[string]string{"PATH": fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")},
		"--dest-root", dest,
		"--binary", binary,
		"--server-url", "ws://panel.example.com:8080/api/agent/ws",
		"--token", "secret-token",
		"--node-id", "oracle-sg-01",
		"--name", "Oracle SG",
	)
	if err != nil {
		t.Fatalf("embedded install script failed: %v\n%s", err, output)
	}
	config, err := os.ReadFile(filepath.Join(dest, "usr", "local", "mizupanel", "etc", "agent.yaml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if !strings.Contains(string(config), `  token: "secret-token"`) {
		t.Fatalf("generated config missing token:\n%s", config)
	}
}

func TestServedAgentInstallScriptDownloadsBinaryURL(t *testing.T) {
	binaryPayload := []byte("#!/bin/sh\necho mizupanel-agent\n")
	binaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/downloads/mizupanel-agent-linux-amd64" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(binaryPayload)
	}))
	t.Cleanup(binaryServer.Close)

	script := filepath.Join(t.TempDir(), "install-agent.sh")
	if err := os.WriteFile(script, agentInstallScript, 0755); err != nil {
		t.Fatalf("write embedded script: %v", err)
	}
	dest := t.TempDir()
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "systemctl"), []byte("#!/bin/sh\necho systemctl should not run in dest-root mode >&2\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}

	output, err := runCommand(t, script, map[string]string{"PATH": fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")},
		"--dest-root", dest,
		"--binary-url", binaryServer.URL+"/downloads/mizupanel-agent-linux-amd64",
		"--server-url", "ws://panel.example.com:8080/api/agent/ws",
		"--token", "install-token",
		"--node-id", "oracle-sg-01",
		"--name", "Oracle SG",
	)
	if err != nil {
		t.Fatalf("embedded install script failed: %v\n%s", err, output)
	}
	installedBinary, err := os.ReadFile(filepath.Join(dest, "usr", "local", "mizupanel", "bin", "mizupanel-agent"))
	if err != nil {
		t.Fatalf("read downloaded binary: %v", err)
	}
	if string(installedBinary) != string(binaryPayload) {
		t.Fatalf("installed binary = %q, want downloaded payload", installedBinary)
	}
	config, err := os.ReadFile(filepath.Join(dest, "usr", "local", "mizupanel", "etc", "agent.yaml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if !strings.Contains(string(config), `  token: "install-token"`) {
		t.Fatalf("generated config missing install token:\n%s", config)
	}
}

func runCommand(t *testing.T, path string, env map[string]string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(path, args...)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestNewHandlerMountsAgentWebSocketWithTokenCheck(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := NewHandler(Dependencies{
		Nodes:      store.NewNodeStore(database),
		Metrics:    store.NewMetricStore(database),
		AgentToken: "secret",
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/ws?token=wrong", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}
