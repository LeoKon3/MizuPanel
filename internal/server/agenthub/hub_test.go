package agenthub

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"

	"github.com/mizupanel/mizupanel/internal/protocol"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/store"
	"github.com/mizupanel/mizupanel/internal/version"
)

func TestInstallAuthStoreGeneratesRandomNodeToken(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")

	nodeToken, ok := auth.ExchangeInstallToken("install-token", "node-1", nil)
	if !ok {
		t.Fatal("ExchangeInstallToken returned false")
	}
	if strings.Contains(nodeToken, "install-token") || strings.Contains(nodeToken, "node-1") {
		t.Fatalf("node token %q includes install token or node id", nodeToken)
	}
}

func TestInstallAuthStoreRetriesBoundInstallTokenForSameNode(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	saveCalls := 0
	saveToken := func(string) error {
		saveCalls++
		return nil
	}

	firstToken, ok := auth.ExchangeInstallToken("install-token", "node-1", saveToken)
	if !ok {
		t.Fatal("first ExchangeInstallToken returned false")
	}
	retryToken, ok := auth.ExchangeInstallToken("install-token", "node-1", saveToken)
	if !ok {
		t.Fatal("retry ExchangeInstallToken returned false")
	}
	if retryToken != firstToken {
		t.Fatalf("retry token = %q, want original token %q", retryToken, firstToken)
	}
	if saveCalls != 1 {
		t.Fatalf("save callback calls = %d, want 1", saveCalls)
	}
}

func TestInstallAuthStoreLeavesInstallTokenRetryableWhenPersistenceFails(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	wantErr := errors.New("persistence failed")
	if _, ok := auth.ExchangeInstallToken("install-token", "node-1", func(string) error { return wantErr }); ok {
		t.Fatal("ExchangeInstallToken succeeded after persistence failure")
	}
	if _, ok := auth.NodeToken("node-1"); ok {
		t.Fatal("persistence failure created an in-memory node token")
	}
	if !auth.MayAuthenticateInstallToken("install-token") {
		t.Fatal("persistence failure consumed the install token")
	}
	if _, ok := auth.ExchangeInstallToken("install-token", "node-1", nil); !ok {
		t.Fatal("install token was not retryable after persistence recovered")
	}
}

func TestInstallAuthStoreRejectsBoundInstallTokenForAnotherNode(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	nodeToken, ok := auth.ExchangeInstallToken("install-token", "node-1", nil)
	if !ok {
		t.Fatal("first ExchangeInstallToken returned false")
	}

	if _, ok := auth.ExchangeInstallToken("install-token", "node-2", nil); ok {
		t.Fatal("bound install token authenticated a different node")
	}
	if got, ok := auth.NodeToken("node-1"); !ok || got != nodeToken {
		t.Fatalf("node-1 token = %q, %v; want %q, true", got, ok, nodeToken)
	}
	if _, ok := auth.NodeToken("node-2"); ok {
		t.Fatal("cross-node retry created a node-2 token")
	}
}

func TestInstallAuthStoreCompletesBootstrapOnlyForMatchingCredential(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	nodeToken, ok := auth.ExchangeInstallToken("install-token", "node-1", nil)
	if !ok {
		t.Fatal("ExchangeInstallToken returned false")
	}

	auth.CompleteBootstrap("node-1", "wrong-token")
	if !auth.MayAuthenticateInstallToken("install-token") || !auth.MayAuthenticateNodeToken(nodeToken) {
		t.Fatal("mismatched credential removed bootstrap state")
	}

	auth.CompleteBootstrap("node-1", nodeToken)
	if auth.MayAuthenticateInstallToken("install-token") {
		t.Fatal("completed install token still authenticates")
	}
	if auth.MayAuthenticateNodeToken(nodeToken) {
		t.Fatal("completed in-memory node token still authenticates")
	}
}

func TestInstallAuthStoreRevokesNodeToken(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	nodeToken, ok := auth.ExchangeInstallToken("install-token", "node-1", nil)
	if !ok {
		t.Fatal("ExchangeInstallToken returned false")
	}
	if !auth.MayAuthenticateNodeToken(nodeToken) {
		t.Fatal("node token should authenticate before revoke")
	}

	auth.RevokeNodeToken("node-1")
	if auth.MayAuthenticateNodeToken(nodeToken) {
		t.Fatal("node token still authenticates after revoke")
	}
	if auth.MayAuthenticateInstallToken("install-token") {
		t.Fatal("bound install token still authenticates after revoke")
	}
}

func TestInstallTokenAllowsNodeOnlyWhenCreatedAfterDeletion(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{})
	deletedAt := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	if _, err := database.Exec(`INSERT INTO deleted_nodes (id, deleted_at) VALUES (?, ?)`, "node-1", deletedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert tombstone: %v", err)
	}
	allowed, err := handler.allowNode(t.Context(), "node-1", deletedAt.Add(-time.Second))
	if err != nil {
		t.Fatalf("allow with old token: %v", err)
	}
	if allowed {
		t.Fatal("old install token should not clear tombstone")
	}
	allowed, err = handler.allowNode(t.Context(), "node-1", deletedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("allow with new token: %v", err)
	}
	if !allowed {
		t.Fatal("new install token should clear tombstone")
	}
}

func TestAgentCredentialStillValidRejectsRevokedPersistentToken(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tokens := store.NewAgentTokenStore(database)
	if err := tokens.SaveNodeToken(t.Context(), "node-1", "node-token", time.Now().UTC()); err != nil {
		t.Fatalf("save node token: %v", err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentTokens: tokens})
	if !handler.agentCredentialStillValid(t.Context(), "node-1", "node-token", "node-token", false, true) {
		t.Fatal("node token should be valid before delete")
	}
	if _, err := database.Exec(`DELETE FROM node_tokens WHERE node_id = ?`, "node-1"); err != nil {
		t.Fatalf("delete token: %v", err)
	}
	if handler.agentCredentialStillValid(t.Context(), "node-1", "node-token", "node-token", false, true) {
		t.Fatal("node token should be invalid after delete")
	}
}

func TestDeliverK8sMessageForAllResultTypes(t *testing.T) {
	resultTypes := []string{
		protocol.MessageTypeK8sClusterConnectResult,
		protocol.MessageTypeK8sGetSummaryResult,
		protocol.MessageTypeK8sGetNamespacesResult,
		protocol.MessageTypeK8sGetNodesResult,
		protocol.MessageTypeK8sGetPodsResult,
		protocol.MessageTypeK8sGetDeploymentsResult,
		protocol.MessageTypeK8sGetStatefulSetsResult,
		protocol.MessageTypeK8sGetDaemonSetsResult,
		protocol.MessageTypeK8sGetServicesResult,
		protocol.MessageTypeK8sGetIngressesResult,
		protocol.MessageTypeK8sGetDiagnosticsResult,
		protocol.MessageTypeK8sGetPodLogsResult,
	}
	conn := &agentConnection{pendingK8sMessages: make(map[string]chan json.RawMessage)}
	for _, resultType := range resultTypes {
		requestID := resultType + "-request"
		ch := make(chan json.RawMessage, 1)
		conn.pendingK8sMessages[requestID] = ch
		raw := json.RawMessage(`{"type":"` + resultType + `","request_id":"` + requestID + `","success":true}`)
		conn.deliverK8sMessage(requestID, raw)
		select {
		case got := <-ch:
			var header struct {
				Type      string `json:"type"`
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(got, &header); err != nil {
				t.Fatalf("unmarshal delivered result: %v", err)
			}
			if header.Type != resultType || header.RequestID != requestID {
				t.Fatalf("delivered header = %#v, want type=%s request_id=%s", header, resultType, requestID)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for %s delivery", resultType)
		}
	}
}

func TestClosePendingOperationsUnblocksK8sMessages(t *testing.T) {
	conn := &agentConnection{pendingK8sMessages: map[string]chan json.RawMessage{"req-1": make(chan json.RawMessage, 1)}}
	ch := conn.pendingK8sMessages["req-1"]
	conn.closePendingOperations("节点离线")
	select {
	case raw := <-ch:
		var response protocol.K8sGetSummaryResult
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatalf("unmarshal offline k8s response: %v", err)
		}
		if response.Success || response.RequestID != "req-1" || !strings.Contains(response.Error, "节点离线") {
			t.Fatalf("offline response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for pending k8s message to unblock")
	}
	if len(conn.pendingK8sMessages) != 0 {
		t.Fatalf("pendingK8sMessages length = %d, want 0", len(conn.pendingK8sMessages))
	}
}

func TestClosePendingOperationsUnblocksAgentUpgrade(t *testing.T) {
	conn := &agentConnection{pendingAgentUpgrades: map[string]chan protocol.AgentUpgradeResponse{"upgrade-1": make(chan protocol.AgentUpgradeResponse, 1)}}
	ch := conn.pendingAgentUpgrades["upgrade-1"]
	conn.closePendingOperations("Agent 连接已断开")
	select {
	case response := <-ch:
		if response.RequestID != "upgrade-1" || response.Stage != "failed" || response.Code != "offline" || !strings.Contains(response.Error, "连接已断开") {
			t.Fatalf("offline upgrade response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for pending upgrade to unblock")
	}
	if len(conn.pendingAgentUpgrades) != 0 {
		t.Fatalf("pendingAgentUpgrades length = %d, want 0", len(conn.pendingAgentUpgrades))
	}
}

func TestAgentUpgradeRejectsNonCurrentTarget(t *testing.T) {
	handler := &Handler{}
	response, err := handler.AgentUpgrade(t.Context(), "node-1", "0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if response.Code != "invalid_target" || response.Accepted {
		t.Fatalf("response = %#v", response)
	}
}

func TestAgentUpgradeFailureAndReconnectStatusTransitions(t *testing.T) {
	handler := &Handler{connections: make(map[string]*agentConnection), upgrades: map[string]protocol.AgentUpgradeStatus{
		"node-1": {NodeID: "node-1", TargetVersion: "0.1.2", ActualVersion: "0.1.1", Stage: "waiting_reconnect"},
	}}
	agent := &agentConnection{nodeID: "node-1", agentVersion: "0.1.1"}
	handler.recordAgentUpgradeResponse(agent, protocol.AgentUpgradeResponse{Stage: "failed", Error: "SHA-256 校验失败"})
	failed := handler.AgentUpgradeStatus("node-1")
	if failed.Stage != "failed" || !strings.Contains(failed.Error, "SHA-256") {
		t.Fatalf("failed status = %#v", failed)
	}
	handler.observeAgentUpgradeReconnect("node-1", "0.1.2")
	completed := handler.AgentUpgradeStatus("node-1")
	if completed.Stage != "completed" || completed.ActualVersion != "0.1.2" || completed.Error != "" {
		t.Fatalf("completed status = %#v", completed)
	}
}

func TestAgentUpgradeOldVersionReconnectAndTimeoutFail(t *testing.T) {
	handler := &Handler{connections: make(map[string]*agentConnection), upgrades: map[string]protocol.AgentUpgradeStatus{
		"old-node":     {NodeID: "old-node", TargetVersion: "0.1.2", ActualVersion: "0.1.1", Stage: "waiting_reconnect"},
		"timeout-node": {NodeID: "timeout-node", TargetVersion: "0.1.2", ActualVersion: "0.1.1", Stage: "waiting_reconnect"},
	}}
	handler.observeAgentUpgradeReconnect("old-node", "0.1.1")
	oldStatus := handler.AgentUpgradeStatus("old-node")
	if oldStatus.Stage != "failed" || oldStatus.ActualVersion != "0.1.1" || !strings.Contains(oldStatus.Error, "仍为版本") {
		t.Fatalf("old-version reconnect status = %#v", oldStatus)
	}
	handler.expireAgentUpgrade("timeout-node", "0.1.2")
	timeoutStatus := handler.AgentUpgradeStatus("timeout-node")
	if timeoutStatus.Stage != "failed" || !strings.Contains(timeoutStatus.Error, "超时") {
		t.Fatalf("timeout status = %#v", timeoutStatus)
	}
	handler.observeAgentUpgradeReconnect("timeout-node", "0.1.2")
	completed := handler.AgentUpgradeStatus("timeout-node")
	if completed.Stage != "completed" || completed.Error != "" {
		t.Fatalf("late successful reconnect status = %#v", completed)
	}
}

func TestPrepareAgentUpgradeRequestUsesCurrentServerArtifact(t *testing.T) {
	downloadDir := t.TempDir()
	artifact := []byte("static-agent-binary")
	if err := os.WriteFile(filepath.Join(downloadDir, "mizupanel-agent-linux-amd64"), artifact, 0755); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{options: Options{PublicURL: "https://panel.example.com/", DownloadDir: downloadDir}}
	request, code, err := handler.prepareAgentUpgradeRequest(store.Node{ID: "node-1", OS: "linux", Arch: "amd64"}, "request-1", version.Current)
	if err != nil || code != "" {
		t.Fatalf("prepare request code/error = %q/%v", code, err)
	}
	sum := sha256.Sum256(artifact)
	if request.TargetVersion != version.Current || request.DownloadURL != "https://panel.example.com/downloads/mizupanel-agent-linux-amd64" || request.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("upgrade request = %#v", request)
	}
}

func TestPrepareAgentUpgradeRequestRejectsMissingPackageAndUnsupportedPlatform(t *testing.T) {
	handler := &Handler{options: Options{PublicURL: "https://panel.example.com", DownloadDir: t.TempDir()}}
	_, code, err := handler.prepareAgentUpgradeRequest(store.Node{ID: "node-1", OS: "linux", Arch: "amd64"}, "request-1", version.Current)
	if code != "package_missing" || err == nil {
		t.Fatalf("missing package code/error = %q/%v", code, err)
	}
	_, code, err = handler.prepareAgentUpgradeRequest(store.Node{ID: "node-1", OS: "windows", Arch: "amd64"}, "request-2", version.Current)
	if code != "unsupported_platform" || err == nil {
		t.Fatalf("unsupported platform code/error = %q/%v", code, err)
	}
}

func TestAgentWebSocketExchangesInstallTokenForNodeToken(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	tokens := store.NewAgentTokenStore(database)
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{InstallAuth: auth, AgentTokens: tokens, Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=install-token"
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
	if ack.NodeToken == "" || ack.NodeToken == "install-token" {
		t.Fatalf("NodeToken = %q, want generated node token", ack.NodeToken)
	}

	secondURL := "ws" + strings.TrimPrefix(server.URL, "http")
	secondHeader := http.Header{"Authorization": {"Bearer " + ack.NodeToken}}
	secondConn, _, err := websocket.DefaultDialer.Dial(secondURL, secondHeader)
	if err != nil {
		t.Fatalf("dial node token websocket: %v", err)
	}
	if err := secondConn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "node-1", Hostname: "oracle-sg"}); err != nil {
		secondConn.Close()
		t.Fatalf("write reconnect hello: %v", err)
	}
	var reconnectAck protocol.HelloAckMessage
	if err := secondConn.ReadJSON(&reconnectAck); err != nil {
		secondConn.Close()
		t.Fatalf("read reconnect ack: %v", err)
	}
	secondConn.Close()
	if reconnectAck.NodeToken != ack.NodeToken {
		t.Fatalf("reconnect node token = %q, want %q", reconnectAck.NodeToken, ack.NodeToken)
	}
	if auth.MayAuthenticateNodeToken(ack.NodeToken) {
		t.Fatal("persistent reconnect left an in-memory node-token fallback")
	}

	_, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("completed install token still authenticated")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("completed install token status = %#v, want 401", response)
	}
	if _, err := database.Exec(`DELETE FROM node_tokens WHERE node_id = ?`, "node-1"); err != nil {
		t.Fatalf("revoke persisted node token: %v", err)
	}
	_, response, err = websocket.DefaultDialer.Dial(secondURL, secondHeader)
	if err == nil {
		t.Fatal("revoked persistent node token authenticated through install auth")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked persistent node token status = %#v, want 401", response)
	}
}

func TestAgentWebSocketRetriesInstallTokenAfterFirstNodeUpsertFails(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fail_first_node_upsert BEFORE INSERT ON nodes BEGIN SELECT RAISE(FAIL, 'forced node upsert failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	tokens := store.NewAgentTokenStore(database)
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{InstallAuth: auth, AgentTokens: tokens, Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=install-token"
	firstConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial first websocket: %v", err)
	}
	if err := firstConn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "node-1", Hostname: "oracle-sg"}); err != nil {
		firstConn.Close()
		t.Fatalf("write first hello: %v", err)
	}
	_ = firstConn.SetReadDeadline(time.Now().Add(time.Second))
	var failedAck protocol.HelloAckMessage
	if err := firstConn.ReadJSON(&failedAck); err == nil {
		firstConn.Close()
		t.Fatalf("first registration unexpectedly returned ack: %#v", failedAck)
	}
	firstConn.Close()

	boundNodeToken, ok := auth.NodeToken("node-1")
	if !ok || boundNodeToken == "" {
		t.Fatal("failed registration did not retain its bound node token")
	}
	if _, err := database.Exec(`DROP TRIGGER fail_first_node_upsert`); err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}

	retryConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial retry websocket: %v", err)
	}
	defer retryConn.Close()
	if err := retryConn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "node-1", Hostname: "oracle-sg"}); err != nil {
		t.Fatalf("write retry hello: %v", err)
	}
	var retryAck protocol.HelloAckMessage
	if err := retryConn.ReadJSON(&retryAck); err != nil {
		t.Fatalf("read retry ack: %v", err)
	}
	if retryAck.NodeToken != boundNodeToken {
		t.Fatalf("retry node token = %q, want retained token %q", retryAck.NodeToken, boundNodeToken)
	}
	if gotNodeID, ok, err := tokens.NodeIDForToken(t.Context(), retryAck.NodeToken); err != nil || !ok || gotNodeID != "node-1" {
		t.Fatalf("persisted retry token lookup = %q, %v, %v; want node-1, true, nil", gotNodeID, ok, err)
	}
}

func TestAgentWebSocketRejectsConfiguredAgentTokenInQuery(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentToken: "secret", Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=secret"
	_, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("dial succeeded, want unauthorized failure")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %#v, want 401", response)
	}
}

func TestAgentWebSocketRejectsPersistedNodeTokenInQuery(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tokens := store.NewAgentTokenStore(database)
	if err := tokens.SaveNodeToken(t.Context(), "node-1", "node-token", time.Now().UTC()); err != nil {
		t.Fatalf("save node token: %v", err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentTokens: tokens, Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=node-token"
	_, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("dial succeeded, want unauthorized failure")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %#v, want 401", response)
	}
}

func TestAgentWebSocketAcceptsConfiguredAgentTokenWhenInstallAuthExists(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auth := NewInstallAuthStore()
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentToken: "secret", InstallAuth: auth, Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Authorization": {"Bearer secret"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
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
	if ack.Type != protocol.MessageTypeHelloAck || ack.NodeID != "node-1" {
		t.Fatalf("unexpected ack: %#v", ack)
	}
}

func TestInstallAuthStoreRejectsExpiredInstallToken(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	auth.installTokens["install-token"] = installToken{expiresAt: time.Now().Add(-time.Second)}

	if auth.MayAuthenticateInstallToken("install-token") {
		t.Fatal("MayAuthenticateInstallToken accepted expired install token")
	}
	if _, ok := auth.ExchangeInstallToken("install-token", "node-1", nil); ok {
		t.Fatal("ExchangeInstallToken accepted expired install token")
	}
}

func TestInstallAuthStoreRejectsExpiredBoundInstallToken(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	nodeToken, ok := auth.ExchangeInstallToken("install-token", "node-1", nil)
	if !ok {
		t.Fatal("first ExchangeInstallToken returned false")
	}
	auth.mu.Lock()
	entry := auth.installTokens["install-token"]
	entry.expiresAt = time.Now().Add(-time.Second)
	auth.installTokens["install-token"] = entry
	auth.mu.Unlock()

	if auth.MayAuthenticateInstallToken("install-token") {
		t.Fatal("expired bound install token authenticated")
	}
	if _, ok := auth.ExchangeInstallToken("install-token", "node-1", nil); ok {
		t.Fatal("expired bound install token was exchanged")
	}
	if !auth.MayAuthenticateNodeToken(nodeToken) {
		t.Fatal("bootstrap token expiry revoked the established node token")
	}
}

func TestInstallAuthStorePrunesAndCapsOutstandingInstallTokens(t *testing.T) {
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("expired")
	auth.installTokens["expired"] = installToken{expiresAt: time.Now().Add(-time.Second)}

	for i := range maxInstallTokens {
		if !auth.CreateInstallToken("install-token-" + strconv.Itoa(i)) {
			t.Fatalf("CreateInstallToken rejected token %d before cap", i)
		}
	}
	if auth.CreateInstallToken("one-too-many") {
		t.Fatal("CreateInstallToken accepted token beyond cap")
	}
	if _, ok := auth.installTokens["expired"]; ok {
		t.Fatal("expired token was not pruned before cap check")
	}
}

func TestAgentWebSocketAcceptsPersistedNodeTokenWithoutInstallAuth(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tokens := store.NewAgentTokenStore(database)
	if err := tokens.SaveNodeToken(t.Context(), "node-1", "node-token", time.Now().UTC()); err != nil {
		t.Fatalf("save node token: %v", err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentTokens: tokens, Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Authorization": {"Bearer node-token"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
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
	if ack.NodeID != "node-1" {
		t.Fatalf("NodeID = %q, want node-1", ack.NodeID)
	}
}

func TestAgentWebSocketPrefersConfiguredAgentTokenOverStoredNodeToken(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tokens := store.NewAgentTokenStore(database)
	if err := tokens.SaveNodeToken(t.Context(), "node-2", "secret", time.Now().UTC()); err != nil {
		t.Fatalf("save node token: %v", err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentToken: "secret", AgentTokens: tokens, Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Authorization": {"Bearer secret"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
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
	if ack.NodeID != "node-1" {
		t.Fatalf("NodeID = %q, want node-1", ack.NodeID)
	}
}

func TestBearerTokenAcceptsCaseInsensitiveScheme(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/agent/ws", nil)
	request.Header.Set("Authorization", "bearer secret")

	if got := bearerToken(request); got != "secret" {
		t.Fatalf("bearerToken = %q, want secret", got)
	}
}

func TestAgentManagementRejectsConnectionsWithoutCapability(t *testing.T) {
	handler := &Handler{connections: map[string]*agentConnection{"node-1": {nodeID: "node-1", supportsAgentManagement: false}}}

	status, err := handler.AgentStatus(t.Context(), "node-1")
	if err != nil {
		t.Fatalf("AgentStatus returned error: %v", err)
	}
	restart, err := handler.AgentRestart(t.Context(), "node-1")
	if err != nil {
		t.Fatalf("AgentRestart returned error: %v", err)
	}
	logs, err := handler.AgentLogs(t.Context(), "node-1", 100)
	if err != nil {
		t.Fatalf("AgentLogs returned error: %v", err)
	}
	for name, code := range map[string]string{"status": status.Code, "restart": restart.Code, "logs": logs.Code} {
		if code != "unsupported" {
			t.Fatalf("%s code = %q, want unsupported", name, code)
		}
	}
}

func TestComposeServiceActionRejectsAgentWithoutServiceCapability(t *testing.T) {
	handler := &Handler{connections: map[string]*agentConnection{
		"node-1": {nodeID: "node-1", supportsDockerCompose: true, supportsDockerComposeServiceActions: false},
	}}

	response, err := handler.DockerComposeAction(t.Context(), "node-1", "demo", "web", "stop")
	if err != nil {
		t.Fatalf("DockerComposeAction returned error: %v", err)
	}
	if response.Success || response.Error != "当前 Agent 不支持 Compose 服务操作" {
		t.Fatalf("response = %#v", response)
	}
}

func TestComposeDeploymentRejectsAgentWithoutCapability(t *testing.T) {
	handler := &Handler{connections: map[string]*agentConnection{
		"node-1": {nodeID: "node-1", supportsDockerCompose: true, supportsDockerComposeDeployment: false},
	}}

	response, err := handler.DockerComposeDeployment(t.Context(), "node-1", protocol.DockerComposeDeploymentRequest{Action: "preview", ComposeYAML: "services: {}"})
	if err != nil {
		t.Fatalf("DockerComposeDeployment returned error: %v", err)
	}
	if response.Success || response.Supported || response.Error != "当前 Agent 不支持 Compose 应用部署，请升级 Agent" || response.Risks == nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestDockerResourcesRejectAgentWithoutCapability(t *testing.T) {
	handler := &Handler{connections: map[string]*agentConnection{
		"node-1": {nodeID: "node-1", supportsDockerResources: false},
	}}

	list, err := handler.DockerResourceList(t.Context(), "node-1")
	if err != nil {
		t.Fatalf("DockerResourceList returned error: %v", err)
	}
	if list.Success || list.Supported || list.Error == "" || list.Images == nil || list.Volumes == nil || list.Networks == nil {
		t.Fatalf("list response = %#v", list)
	}
	action, err := handler.DockerResourceAction(t.Context(), "node-1", "image", "nginx:latest", "pull")
	if err != nil {
		t.Fatalf("DockerResourceAction returned error: %v", err)
	}
	if action.Success || action.Supported || action.Error == "" {
		t.Fatalf("action response = %#v", action)
	}
}

func TestSystemdServiceActionRejectsAgentWithoutCapability(t *testing.T) {
	handler := &Handler{connections: map[string]*agentConnection{
		"node-1": {nodeID: "node-1", supportsSystemdServices: false},
	}}

	response, err := handler.SystemdServiceAction(t.Context(), "node-1", "nginx.service", "restart", 0)
	if err != nil {
		t.Fatalf("SystemdServiceAction returned error: %v", err)
	}
	if response.Success || response.Error != "当前 Agent 不支持 systemd 服务管理" {
		t.Fatalf("response = %#v", response)
	}
}

func TestAgentConnectionEnforcesCombinedTerminalSessionLimit(t *testing.T) {
	agent := &agentConnection{
		terminals:      make(map[string]*browserTerminal),
		containerExecs: make(map[string]*browserContainerExec),
	}

	for i := range maxServerTerminalSessions - 1 {
		if !agent.addTerminal(&browserTerminal{sessionID: "terminal-" + strconv.Itoa(i)}) {
			t.Fatalf("addTerminal rejected terminal %d before combined cap", i)
		}
	}
	if !agent.addContainerExec(&browserContainerExec{sessionID: "exec-1", containerID: "container-1"}) {
		t.Fatal("addContainerExec rejected session at combined cap")
	}
	if agent.addTerminal(&browserTerminal{sessionID: "terminal-over-limit"}) {
		t.Fatal("addTerminal accepted session beyond combined terminal/container exec cap")
	}
}

func TestAgentConnectionMixedSessionLimitConcurrentAccess(t *testing.T) {
	agent := &agentConnection{
		terminals:      make(map[string]*browserTerminal),
		containerExecs: make(map[string]*browserContainerExec),
	}
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = agent.addTerminal(&browserTerminal{sessionID: "terminal-" + strconv.Itoa(i)})
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = agent.addContainerExec(&browserContainerExec{sessionID: "exec-" + strconv.Itoa(i), containerID: "container-1"})
		}(i)
	}
	wg.Wait()

	if got := len(agent.terminals) + len(agent.containerExecs); got > maxServerTerminalSessions {
		t.Fatalf("combined sessions = %d, want at most %d", got, maxServerTerminalSessions)
	}
}

func TestAgentWebSocketReconnectsWithPersistedNodeTokenAfterRestart(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tokens := store.NewAgentTokenStore(database)
	auth := NewInstallAuthStore()
	auth.CreateInstallToken("install-token")
	firstHandler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{InstallAuth: auth, AgentTokens: tokens, Interval: 5})
	firstServer := httptest.NewServer(firstHandler)

	firstURL := "ws" + strings.TrimPrefix(firstServer.URL, "http") + "?token=install-token"
	firstConn, _, err := websocket.DefaultDialer.Dial(firstURL, nil)
	if err != nil {
		firstServer.Close()
		t.Fatalf("dial first websocket: %v", err)
	}
	if err := firstConn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "node-1", Hostname: "oracle-sg"}); err != nil {
		firstConn.Close()
		firstServer.Close()
		t.Fatalf("write first hello: %v", err)
	}
	var firstAck protocol.HelloAckMessage
	if err := firstConn.ReadJSON(&firstAck); err != nil {
		firstConn.Close()
		firstServer.Close()
		t.Fatalf("read first ack: %v", err)
	}
	firstConn.Close()
	firstServer.Close()
	if firstAck.NodeToken == "" {
		t.Fatal("first ack node token is empty")
	}

	restartedHandler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{InstallAuth: NewInstallAuthStore(), AgentTokens: store.NewAgentTokenStore(database), Interval: 5})
	restartedServer := httptest.NewServer(restartedHandler)
	t.Cleanup(restartedServer.Close)

	reconnectURL := "ws" + strings.TrimPrefix(restartedServer.URL, "http")
	reconnectHeader := http.Header{"Authorization": {"Bearer " + firstAck.NodeToken}}
	reconnectConn, _, err := websocket.DefaultDialer.Dial(reconnectURL, reconnectHeader)
	if err != nil {
		t.Fatalf("dial restarted websocket: %v", err)
	}
	defer reconnectConn.Close()
	if err := reconnectConn.WriteJSON(protocol.HelloMessage{Type: protocol.MessageTypeHello, NodeID: "node-1", Hostname: "oracle-sg"}); err != nil {
		t.Fatalf("write reconnect hello: %v", err)
	}
	var reconnectAck protocol.HelloAckMessage
	if err := reconnectConn.ReadJSON(&reconnectAck); err != nil {
		t.Fatalf("read reconnect ack: %v", err)
	}
	if reconnectAck.NodeToken != firstAck.NodeToken {
		t.Fatalf("reconnect node token = %q, want persisted token", reconnectAck.NodeToken)
	}
}

func TestAgentWebSocketRegistersNodeAndStoresMetrics(t *testing.T) {
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
	metrics := store.NewMetricStore(database)
	handler := NewHandler(nodes, metrics, Options{AgentToken: "secret", Interval: 5})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Authorization": {"Bearer secret"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{
		"type":          protocol.MessageTypeHello,
		"agent_version": "0.1.0",
		"hostname":      "oracle-sg",
		"name":          "Oracle SG",
		"ip":            "10.0.0.8",
		"os":            "linux",
		"arch":          "arm64",
		"kernel":        "6.6",
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	var ack protocol.HelloAckMessage
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if ack.Type != protocol.MessageTypeHelloAck || ack.NodeID == "" || ack.Interval != 5 {
		t.Fatalf("unexpected ack: %#v", ack)
	}

	if err := conn.WriteJSON(protocol.MetricsMessage{
		Type:      protocol.MessageTypeMetrics,
		NodeID:    ack.NodeID,
		Timestamp: time.Now().Unix(),
		System:    protocol.SystemInfo{Uptime: 86400},
		CPU:       protocol.CPUInfo{Cores: 4, Usage: 12.5},
		Memory:    protocol.MemoryInfo{Total: 1000, Used: 500, Usage: 50},
		Disk:      protocol.DiskInfo{Total: 2000, Used: 500, Usage: 25, ReadSpeed: 4096, WriteSpeed: 8192},
		Network:   protocol.NetworkInfo{RXSpeed: 10, TXSpeed: 20, RXTotal: 100, TXTotal: 200},
		Load:      protocol.LoadInfo{Load1: 0.1, Load5: 0.2, Load15: 0.3},
	}); err != nil {
		t.Fatalf("write metrics: %v", err)
	}

	gotNode, err := nodes.Get(t.Context(), ack.NodeID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if gotNode.Name != "Oracle SG" || gotNode.Status != "online" || gotNode.IP != "10.0.0.8" {
		t.Fatalf("node = %#v", gotNode)
	}

	var gotMetrics []store.Metric
	for range 20 {
		gotMetrics, err = metrics.ListRange(t.Context(), ack.NodeID, time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("list metrics: %v", err)
		}
		if len(gotMetrics) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(gotMetrics) != 1 || gotMetrics[0].CPUUsage != 12.5 || gotMetrics[0].TXTotal != 200 || gotMetrics[0].Uptime != 86400 || gotMetrics[0].DiskReadSpeed != 4096 || gotMetrics[0].DiskWriteSpeed != 8192 {
		t.Fatalf("metrics = %#v", gotMetrics)
	}
}

func TestAgentWebSocketRejectsInvalidToken(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	handler := NewHandler(store.NewNodeStore(database), store.NewMetricStore(database), Options{AgentToken: "secret"})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=wrong"
	_, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("dial succeeded, want unauthorized failure")
	}
	if response == nil || response.StatusCode != 401 {
		t.Fatalf("status = %#v, want 401", response)
	}
}
