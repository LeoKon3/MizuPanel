package protocol

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestHelloMessageJSON(t *testing.T) {
	msg := HelloMessage{
		Type:         MessageTypeHello,
		NodeID:       "agent-1",
		AgentVersion: "0.1.0",
		Hostname:     "oracle-sg",
		Name:         "Oracle",
		OS:           "linux",
		Arch:         "arm64",
		Kernel:       "6.1.0",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got HelloMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != MessageTypeHello || got.NodeID != "agent-1" || got.Hostname != "oracle-sg" || got.AgentVersion != "0.1.0" {
		t.Fatalf("unexpected hello message: %#v", got)
	}
}

func TestHelloMessageJSONIncludesProtocolIdentityMetadata(t *testing.T) {
	data, err := json.Marshal(HelloMessage{Type: MessageTypeHello, NodeID: "agent-1", ProtocolVersion: CurrentProtocolVersion, IdentitySource: "persistent_uuid"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"protocol_version":1`) || !strings.Contains(text, `"identity_source":"persistent_uuid"`) {
		t.Fatalf("hello metadata missing: %s", text)
	}
}

func TestHelloAckMessageJSONIncludesNodeToken(t *testing.T) {
	msg := HelloAckMessage{Type: MessageTypeHelloAck, NodeID: "node-1", NodeToken: "node-token", Interval: 5}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got HelloAckMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != MessageTypeHelloAck || got.NodeID != "node-1" || got.NodeToken != "node-token" || got.Interval != 5 {
		t.Fatalf("unexpected hello ack message: %#v", got)
	}
}

func TestFileOperationMessagesJSON(t *testing.T) {
	read := FileReadResponse{Type: MessageTypeFileReadResponse, RequestID: "req-1", Path: "/etc/hosts", Content: "127.0.0.1 localhost\n", Editable: true, Size: 20}
	data, err := json.Marshal(read)
	if err != nil {
		t.Fatalf("marshal read response: %v", err)
	}
	var gotRead FileReadResponse
	if err := json.Unmarshal(data, &gotRead); err != nil {
		t.Fatalf("unmarshal read response: %v", err)
	}
	if gotRead.Type != MessageTypeFileReadResponse || gotRead.RequestID != "req-1" || gotRead.Path != "/etc/hosts" || !gotRead.Editable || gotRead.Content == "" {
		t.Fatalf("unexpected read response: %#v", gotRead)
	}

	write := FileWriteRequest{Type: MessageTypeFileWriteRequest, RequestID: "req-2", NodeID: "node-1", Path: "/etc/app.conf", Content: "port=8080\n"}
	data, err = json.Marshal(write)
	if err != nil {
		t.Fatalf("marshal write request: %v", err)
	}
	var gotWrite FileWriteRequest
	if err := json.Unmarshal(data, &gotWrite); err != nil {
		t.Fatalf("unmarshal write request: %v", err)
	}
	if gotWrite.Type != MessageTypeFileWriteRequest || gotWrite.NodeID != "node-1" || gotWrite.Content != "port=8080\n" {
		t.Fatalf("unexpected write request: %#v", gotWrite)
	}

	upload := FileUploadRequest{Type: MessageTypeFileUploadRequest, RequestID: "req-3", NodeID: "node-1", Path: "/tmp/app.bin", ContentBase64: base64.StdEncoding.EncodeToString([]byte{0, 1, 2})}
	data, err = json.Marshal(upload)
	if err != nil {
		t.Fatalf("marshal upload request: %v", err)
	}
	var gotUpload FileUploadRequest
	if err := json.Unmarshal(data, &gotUpload); err != nil {
		t.Fatalf("unmarshal upload request: %v", err)
	}
	if gotUpload.Type != MessageTypeFileUploadRequest || gotUpload.ContentBase64 == "" || gotUpload.Path != "/tmp/app.bin" {
		t.Fatalf("unexpected upload request: %#v", gotUpload)
	}

	deleteRequest := FileDeleteRequest{Type: MessageTypeFileDeleteRequest, RequestID: "req-4", NodeID: "node-1", Path: "/tmp/app.bin"}
	data, err = json.Marshal(deleteRequest)
	if err != nil {
		t.Fatalf("marshal delete request: %v", err)
	}
	var gotDelete FileDeleteRequest
	if err := json.Unmarshal(data, &gotDelete); err != nil {
		t.Fatalf("unmarshal delete request: %v", err)
	}
	if gotDelete.Type != MessageTypeFileDeleteRequest || gotDelete.Path != "/tmp/app.bin" {
		t.Fatalf("unexpected delete request: %#v", gotDelete)
	}
}

func TestAgentManagementMessagesJSON(t *testing.T) {
	status := AgentStatusResponse{Type: MessageTypeAgentStatusResponse, RequestID: "req-1", NodeID: "node-1", Version: "0.1.0", User: "root", Mode: "ops", TerminalEnabled: true, DockerAvailable: true, ConfigPath: "/usr/local/mizupanel/agent.yaml", ServiceName: "mizupanel-agent", Uptime: 1234, CollectedAt: 1710000000}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	var gotStatus AgentStatusResponse
	if err := json.Unmarshal(data, &gotStatus); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if gotStatus.Type != MessageTypeAgentStatusResponse || gotStatus.RequestID != "req-1" || gotStatus.User != "root" || gotStatus.Mode != "ops" || !gotStatus.TerminalEnabled || !gotStatus.DockerAvailable || gotStatus.ServiceName != "mizupanel-agent" || gotStatus.Uptime != 1234 {
		t.Fatalf("unexpected status response: %#v", gotStatus)
	}

	logsRequest := AgentLogsRequest{Type: MessageTypeAgentLogsRequest, RequestID: "req-2", NodeID: "node-1", Lines: 500}
	data, err = json.Marshal(logsRequest)
	if err != nil {
		t.Fatalf("marshal logs request: %v", err)
	}
	var gotLogsRequest AgentLogsRequest
	if err := json.Unmarshal(data, &gotLogsRequest); err != nil {
		t.Fatalf("unmarshal logs request: %v", err)
	}
	if gotLogsRequest.Type != MessageTypeAgentLogsRequest || gotLogsRequest.Lines != 500 {
		t.Fatalf("unexpected logs request: %#v", gotLogsRequest)
	}

	restart := AgentRestartResponse{Type: MessageTypeAgentRestartResponse, RequestID: "req-3", Accepted: true, Message: "重启命令已下发，等待 Agent 重新连接"}
	data, err = json.Marshal(restart)
	if err != nil {
		t.Fatalf("marshal restart response: %v", err)
	}
	var gotRestart AgentRestartResponse
	if err := json.Unmarshal(data, &gotRestart); err != nil {
		t.Fatalf("unmarshal restart response: %v", err)
	}
	if gotRestart.Type != MessageTypeAgentRestartResponse || !gotRestart.Accepted || gotRestart.Message == "" {
		t.Fatalf("unexpected restart response: %#v", gotRestart)
	}
}

func TestTerminalMessageJSON(t *testing.T) {
	payload := []byte("whoami\n")
	msg := TerminalMessage{Type: MessageTypeTerminalData, SessionID: "term-1", NodeID: "node-1", Data: base64.StdEncoding.EncodeToString(payload), Rows: 24, Cols: 80}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got TerminalMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		t.Fatalf("decode terminal data: %v", err)
	}
	if got.Type != MessageTypeTerminalData || got.SessionID != "term-1" || got.NodeID != "node-1" || got.Rows != 24 || got.Cols != 80 || string(decoded) != string(payload) {
		t.Fatalf("unexpected terminal message: %#v", got)
	}
}

func TestContainerExecMessageJSON(t *testing.T) {
	payload := []byte("ls /\n")
	msg := ContainerExecMessage{Type: MessageTypeContainerExecData, SessionID: "exec-1", NodeID: "node-1", ContainerID: "container-1", Command: "/bin/sh", Data: base64.StdEncoding.EncodeToString(payload), Rows: 30, Cols: 120, ExitCode: 7, Error: "boom"}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ContainerExecMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		t.Fatalf("decode container exec data: %v", err)
	}
	if got.Type != MessageTypeContainerExecData || got.SessionID != "exec-1" || got.NodeID != "node-1" || got.ContainerID != "container-1" || got.Command != "/bin/sh" || got.Rows != 30 || got.Cols != 120 || got.ExitCode != 7 || got.Error != "boom" || string(decoded) != string(payload) {
		t.Fatalf("unexpected container exec message: %#v", got)
	}
}

func TestMetricsMessageJSON(t *testing.T) {
	msg := MetricsMessage{
		Type:      MessageTypeMetrics,
		NodeID:    "node-1",
		Timestamp: 1710000000,
		System: SystemInfo{
			Hostname: "oracle-sg",
			OS:       "linux",
			Arch:     "arm64",
			Kernel:   "6.1.0",
			Uptime:   123,
		},
		CPU:     CPUInfo{Cores: 4, Usage: 17.6},
		Memory:  MemoryInfo{Total: 1000, Used: 250, Usage: 25},
		Disk:    DiskInfo{Total: 2000, Used: 1000, Usage: 50, ReadSpeed: 4096, WriteSpeed: 8192},
		Network: NetworkInfo{RXSpeed: 10, TXSpeed: 20, RXTotal: 100, TXTotal: 200},
		Load:    LoadInfo{Load1: 0.2, Load5: 0.1, Load15: 0.05},
		ProcessSnapshot: &ProcessSnapshot{
			CollectedAt: 1710000001,
			Processes:   []ProcessInfo{{PID: 123, PPID: 1, Name: "nginx", Command: "nginx -g daemon off", User: "www-data", Status: "sleeping", CPUUsage: 2.5, MemoryRSS: 1048576, MemoryUsage: 1.2, CreatedAt: 1710000000}},
		},
		DockerSnapshot: &DockerSnapshot{
			CollectedAt: 1710000002,
			Available:   true,
			Version:     "24.0.0",
			Containers:  []ContainerInfo{{ID: "abcdef123456", FullID: "abcdef1234567890", Name: "web", Image: "nginx:latest", State: "running", Status: "Up 1 minute", CreatedAt: 1710000000, StartedAt: 1710000001, RestartCount: 1, CPUUsage: 3.4, MemoryUsage: 2097152, MemoryLimit: 104857600, MemoryPercent: 2, NetworkRX: 1000, NetworkTX: 2000}},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got MetricsMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != MessageTypeMetrics || got.NodeID != "node-1" || got.CPU.Usage != 17.6 || got.Network.TXTotal != 200 || got.Disk.ReadSpeed != 4096 || got.Disk.WriteSpeed != 8192 {
		t.Fatalf("unexpected metrics message: %#v", got)
	}
	if got.ProcessSnapshot == nil || len(got.ProcessSnapshot.Processes) != 1 || got.ProcessSnapshot.Processes[0].Command != "nginx -g daemon off" {
		t.Fatalf("unexpected process snapshot: %#v", got.ProcessSnapshot)
	}
	if got.DockerSnapshot == nil || !got.DockerSnapshot.Available || len(got.DockerSnapshot.Containers) != 1 || got.DockerSnapshot.Containers[0].Name != "web" {
		t.Fatalf("unexpected docker snapshot: %#v", got.DockerSnapshot)
	}
}

func TestMetricsMessageJSONOmitsAbsentSnapshots(t *testing.T) {
	data, err := json.Marshal(MetricsMessage{Type: MessageTypeMetrics})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) == "" || json.Valid(data) == false {
		t.Fatalf("invalid json: %s", data)
	}
	if body := string(data); strings.Contains(body, "process_snapshot") || strings.Contains(body, "docker_snapshot") {
		t.Fatalf("empty snapshots should be omitted: %s", data)
	}
}

func TestK8sDiagnosticsMessagesJSON(t *testing.T) {
	request := K8sDiagnosticsRequest{
		Type:              MessageTypeK8sGetDiagnostics,
		RequestID:         "req-1",
		ClusterID:         "cluster-1",
		Kind:              "pod",
		Namespace:         "default",
		Name:              "nginx",
		KubeconfigContent: "secret",
		Context:           "prod",
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var gotRequest K8sDiagnosticsRequest
	if err := json.Unmarshal(data, &gotRequest); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if gotRequest.Type != MessageTypeK8sGetDiagnostics || gotRequest.Kind != "pod" || gotRequest.Namespace != "default" || gotRequest.Name != "nginx" || gotRequest.KubeconfigContent != "secret" {
		t.Fatalf("unexpected diagnostics request: %#v", gotRequest)
	}

	result := K8sGetDiagnosticsResult{
		Type:      MessageTypeK8sGetDiagnosticsResult,
		RequestID: "req-1",
		Success:   true,
		Diagnostics: &K8sDiagnostics{
			Kind:      "pod",
			Namespace: "default",
			Name:      "nginx",
			Status:    "Running",
			Metadata:  map[string]string{"app": "nginx"},
			Containers: []K8sContainerDetail{{
				Name:         "nginx",
				Image:        "nginx:1.27",
				Ready:        true,
				RestartCount: 2,
			}},
			Conditions: []K8sCondition{{Type: "Ready", Status: "True", Reason: "ContainersReady"}},
			Events:     []K8sEvent{{Type: "Normal", Reason: "Started", Message: "Started container nginx"}},
			YAML:       "apiVersion: v1\nkind: Pod\n",
			Describe:   "Name: nginx\n",
		},
	}
	data, err = json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var gotResult K8sGetDiagnosticsResult
	if err := json.Unmarshal(data, &gotResult); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if gotResult.Type != MessageTypeK8sGetDiagnosticsResult || !gotResult.Success || gotResult.Diagnostics == nil {
		t.Fatalf("unexpected diagnostics result: %#v", gotResult)
	}
	if gotResult.Diagnostics.Containers[0].Name != "nginx" || gotResult.Diagnostics.Events[0].Reason != "Started" || !strings.Contains(gotResult.Diagnostics.YAML, "kind: Pod") {
		t.Fatalf("unexpected diagnostics payload: %#v", gotResult.Diagnostics)
	}
}

func TestK8sResourceActionMessagesJSON(t *testing.T) {
	replicas := int32(3)
	request := K8sResourceActionRequest{
		Type:              MessageTypeK8sResourceAction,
		RequestID:         "req-action",
		ClusterID:         "cluster-1",
		Kind:              "deployment",
		Namespace:         "default",
		Name:              "web",
		Action:            "scale",
		Replicas:          &replicas,
		YAML:              "kind: Deployment\n",
		KubeconfigContent: "secret",
		Context:           "prod",
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal action request: %v", err)
	}
	var gotRequest K8sResourceActionRequest
	if err := json.Unmarshal(data, &gotRequest); err != nil {
		t.Fatalf("unmarshal action request: %v", err)
	}
	if gotRequest.Type != MessageTypeK8sResourceAction || gotRequest.Action != "scale" || gotRequest.Replicas == nil || *gotRequest.Replicas != 3 || gotRequest.YAML == "" || gotRequest.KubeconfigContent != "secret" {
		t.Fatalf("unexpected action request: %#v", gotRequest)
	}

	result := K8sResourceActionResult{Type: MessageTypeK8sResourceActionResult, RequestID: "req-action", Success: true, Message: "扩缩容成功"}
	data, err = json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal action result: %v", err)
	}
	var gotResult K8sResourceActionResult
	if err := json.Unmarshal(data, &gotResult); err != nil {
		t.Fatalf("unmarshal action result: %v", err)
	}
	if gotResult.Type != MessageTypeK8sResourceActionResult || !gotResult.Success || gotResult.Message != "扩缩容成功" {
		t.Fatalf("unexpected action result: %#v", gotResult)
	}
}

func TestK8sApplyManifestMessagesJSON(t *testing.T) {
	request := K8sApplyManifestRequest{
		Type:              MessageTypeK8sApplyManifest,
		RequestID:         "req-apply",
		ClusterID:         "cluster-1",
		YAML:              "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: staging\n",
		DryRun:            true,
		KubeconfigContent: "secret",
		Context:           "prod",
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal apply request: %v", err)
	}
	var gotRequest K8sApplyManifestRequest
	if err := json.Unmarshal(data, &gotRequest); err != nil {
		t.Fatalf("unmarshal apply request: %v", err)
	}
	if gotRequest.Type != MessageTypeK8sApplyManifest || !gotRequest.DryRun || gotRequest.YAML == "" || gotRequest.KubeconfigContent != "secret" || gotRequest.Context != "prod" {
		t.Fatalf("unexpected apply request: %#v", gotRequest)
	}

	result := K8sApplyManifestResult{Type: MessageTypeK8sApplyManifestResult, RequestID: "req-apply", Success: true, Message: "资源校验成功"}
	data, err = json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal apply result: %v", err)
	}
	var gotResult K8sApplyManifestResult
	if err := json.Unmarshal(data, &gotResult); err != nil {
		t.Fatalf("unmarshal apply result: %v", err)
	}
	if gotResult.Type != MessageTypeK8sApplyManifestResult || !gotResult.Success || gotResult.Message != "资源校验成功" {
		t.Fatalf("unexpected apply result: %#v", gotResult)
	}
}
