package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/sshops"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestNewHandlerAuditsInstallCommandWithoutGeneratedToken(t *testing.T) {
	database, auditStore, nodes := newAppAuditStore(t)
	handler := NewHandler(Dependencies{
		Nodes:   nodes,
		Metrics: store.NewMetricStore(database),
		Audit:   auditStore,
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/install/command?platform=linux", nil)
	request.Host = "panel.example:8080"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode install command: %v", err)
	}
	installToken := valueBetween(response.Command, "--token '", "'")
	if installToken == "" {
		t.Fatalf("install command did not contain a generated install token: %s", response.Command)
	}

	event, encoded := appAuditEvent(t, auditStore, "install_command_create")
	if event.Result != serveraudit.ResultSuccess || event.Module != "agent" {
		t.Fatalf("event = %#v, want successful agent install command event", event)
	}
	if event.ActorType != serveraudit.ActorLocalAdmin || event.Metadata["platform"] != "linux" {
		t.Fatalf("event actor/metadata = %#v / %#v", event.ActorType, event.Metadata)
	}
	assertAppAuditSecretsAbsent(t, encoded, response.Command, installToken)
}

func TestNewHandlerAuditsSSHInstallAsAcceptedWithoutJobSecrets(t *testing.T) {
	const password = "audit-password-secret"
	const progressSecret = "connected with secret"

	database, auditStore, nodes := newAppAuditStore(t)
	jobs := sshops.NewManager()
	runner := &fakeSSHRunner{}
	handler := NewHandler(Dependencies{
		Nodes:                  nodes,
		Metrics:                store.NewMetricStore(database),
		SSHJobs:                jobs,
		SSHRunner:              runner,
		SSHInstallWaitTimeout:  100 * time.Millisecond,
		SSHInstallPollInterval: time.Millisecond,
		Audit:                  auditStore,
	})

	body := strings.NewReader(`{"host":"192.0.2.44","username":"root","auth_type":"password","password":"` + password + `","node_id":"audit-node","name":"Audit Node"}`)
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
		t.Fatalf("decode SSH install response: %v", err)
	}
	if err := nodes.Upsert(t.Context(), store.Node{ID: "audit-node", Name: "Audit Node", Status: "online"}); err != nil {
		t.Fatalf("upsert connected node: %v", err)
	}
	if job := jobs.Wait(response.JobID); job == nil || job.Status != sshops.ProgressSuccess {
		t.Fatalf("job = %#v, want success", job)
	}

	event, encoded := appAuditEvent(t, auditStore, "ssh_install")
	if event.Result != serveraudit.ResultAccepted || event.TargetID != "192.0.2.44" || event.NodeID != "audit-node" {
		t.Fatalf("event = %#v, want accepted SSH install target", event)
	}
	assertAppAuditSecretsAbsent(t, encoded, password, runner.install.Token, progressSecret)
}

func TestNewHandlerAuditsSSHUninstallAsAcceptedWithoutPassword(t *testing.T) {
	const password = "uninstall-password-secret"

	database, auditStore, nodes := newAppAuditStore(t)
	if err := nodes.Upsert(t.Context(), store.Node{ID: "audit-node", Name: "Audit Node", Status: "online"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	jobs := sshops.NewManager()
	runner := &fakeSSHRunner{}
	handler := NewHandler(Dependencies{
		Nodes:     nodes,
		Metrics:   store.NewMetricStore(database),
		SSHJobs:   jobs,
		SSHRunner: runner,
		Audit:     auditStore,
	})

	body := strings.NewReader(`{"host":"192.0.2.44","username":"root","auth_type":"password","password":"` + password + `","remove_node_record":true}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/nodes/audit-node/ssh-uninstall", body)
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
		t.Fatalf("decode SSH uninstall response: %v", err)
	}
	if job := jobs.Wait(response.JobID); job == nil || job.Status != sshops.ProgressSuccess {
		t.Fatalf("job = %#v, want success", job)
	}

	event, encoded := appAuditEvent(t, auditStore, "ssh_uninstall")
	if event.Result != serveraudit.ResultAccepted || event.TargetID != "audit-node" || event.NodeID != "audit-node" {
		t.Fatalf("event = %#v, want accepted SSH uninstall target", event)
	}
	if event.Metadata["remove_node_record"] != "true" {
		t.Fatalf("metadata = %#v, want remove_node_record=true", event.Metadata)
	}
	assertAppAuditSecretsAbsent(t, encoded, password)
}

func newAppAuditStore(t *testing.T) (*sql.DB, *serveraudit.Store, *store.NodeStore) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database, serveraudit.NewStore(database, serverdb.DialectSQLite), store.NewNodeStore(database)
}

func appAuditEvent(t *testing.T, auditStore *serveraudit.Store, action string) (serveraudit.Event, string) {
	t.Helper()
	page, err := auditStore.List(t.Context(), serveraudit.Filter{Action: action, Limit: 10})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("events = %#v, want one %s event", page.Events, action)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal audit page: %v", err)
	}
	return page.Events[0], string(encoded)
}

func assertAppAuditSecretsAbsent(t *testing.T, encoded string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(encoded, secret) {
			t.Fatalf("audit event leaked %q: %s", secret, encoded)
		}
	}
}

func valueBetween(value, prefix, suffix string) string {
	start := strings.Index(value, prefix)
	if start < 0 {
		return ""
	}
	remaining := value[start+len(prefix):]
	end := strings.Index(remaining, suffix)
	if end < 0 {
		return ""
	}
	return remaining[:end]
}
