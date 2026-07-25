package protocol

const CurrentProtocolVersion = 1

const (
	MessageTypeHello                = "hello"
	MessageTypeHelloAck             = "hello_ack"
	MessageTypeMetrics              = "metrics"
	MessageTypePing                 = "ping"
	MessageTypePong                 = "pong"
	MessageTypeFileListRequest      = "file_list_request"
	MessageTypeFileListResponse     = "file_list_response"
	MessageTypeFileReadRequest      = "file_read_request"
	MessageTypeFileReadResponse     = "file_read_response"
	MessageTypeFileWriteRequest     = "file_write_request"
	MessageTypeFileWriteResponse    = "file_write_response"
	MessageTypeFileUploadRequest    = "file_upload_request"
	MessageTypeFileUploadResponse   = "file_upload_response"
	MessageTypeFileDeleteRequest    = "file_delete_request"
	MessageTypeFileDeleteResponse   = "file_delete_response"
	MessageTypeRebootRequest        = "reboot_request"
	MessageTypeRebootResponse       = "reboot_response"
	MessageTypeAgentStatusRequest   = "agent_status_request"
	MessageTypeAgentStatusResponse  = "agent_status_response"
	MessageTypeAgentRestartRequest  = "agent_restart_request"
	MessageTypeAgentRestartResponse = "agent_restart_response"
	MessageTypeAgentLogsRequest     = "agent_logs_request"
	MessageTypeAgentLogsResponse    = "agent_logs_response"
	MessageTypeAgentUpgradeRequest  = "agent_upgrade_request"
	MessageTypeAgentUpgradeResponse = "agent_upgrade_response"
	MessageTypeTerminalStart        = "terminal_start"
	MessageTypeTerminalStarted      = "terminal_started"
	MessageTypeTerminalData         = "terminal_data"
	MessageTypeTerminalResize       = "terminal_resize"
	MessageTypeTerminalClose        = "terminal_close"
	MessageTypeTerminalExit         = "terminal_exit"
	MessageTypeTerminalError        = "terminal_error"

	MessageTypeContainerExecStart   = "container_exec_start"
	MessageTypeContainerExecStarted = "container_exec_started"
	MessageTypeContainerExecData    = "container_exec_data"
	MessageTypeContainerExecResize  = "container_exec_resize"
	MessageTypeContainerExecClose   = "container_exec_close"
	MessageTypeContainerExecExit    = "container_exec_exit"
	MessageTypeContainerExecError   = "container_exec_error"

	MessageTypeLogTailRequest  = "log_tail_request"
	MessageTypeLogTailResponse = "log_tail_response"
	MessageTypeLogTailData     = "log_tail_data"
	MessageTypeLogTailStop     = "log_tail_stop"
	MessageTypeLogTailExit     = "log_tail_exit"
	MessageTypeLogTailError    = "log_tail_error"

	MessageTypeContainerLogsRequest  = "container_logs_request"
	MessageTypeContainerLogsResponse = "container_logs_response"
	MessageTypeContainerLogsData     = "container_logs_data"
	MessageTypeContainerLogsStop     = "container_logs_stop"
	MessageTypeContainerLogsExit     = "container_logs_exit"
	MessageTypeContainerLogsError    = "container_logs_error"

	MessageTypeDockerExecRequest  = "docker_exec_request"
	MessageTypeDockerExecResponse = "docker_exec_response"

	MessageTypeContainerStartRequest        = "container_start_request"
	MessageTypeContainerStartResponse       = "container_start_response"
	MessageTypeContainerStopRequest         = "container_stop_request"
	MessageTypeContainerStopResponse        = "container_stop_response"
	MessageTypeContainerRestartRequest      = "container_restart_request"
	MessageTypeContainerRestartResponse     = "container_restart_response"
	MessageTypeContainerDeleteRequest       = "container_delete_request"
	MessageTypeContainerDeleteResponse      = "container_delete_response"
	MessageTypeDockerComposeListRequest     = "docker_compose_list_request"
	MessageTypeDockerComposeListResponse    = "docker_compose_list_response"
	MessageTypeDockerComposeActionRequest   = "docker_compose_action_request"
	MessageTypeDockerComposeActionResponse  = "docker_compose_action_response"
	MessageTypeDockerResourceListRequest    = "docker_resource_list_request"
	MessageTypeDockerResourceListResponse   = "docker_resource_list_response"
	MessageTypeDockerResourceActionRequest  = "docker_resource_action_request"
	MessageTypeDockerResourceActionResponse = "docker_resource_action_response"
	MessageTypeSystemdServiceListRequest    = "systemd_service_list_request"
	MessageTypeSystemdServiceListResponse   = "systemd_service_list_response"
	MessageTypeSystemdServiceActionRequest  = "systemd_service_action_request"
	MessageTypeSystemdServiceActionResponse = "systemd_service_action_response"

	// K8s 集群管理相关消息类型
	MessageTypeK8sClusterConnect        = "k8s_cluster_connect"
	MessageTypeK8sClusterConnectResult  = "k8s_cluster_connect_result"
	MessageTypeK8sGetPods               = "k8s_get_pods"
	MessageTypeK8sGetPodsResult         = "k8s_get_pods_result"
	MessageTypeK8sGetPodLogs            = "k8s_get_pod_logs"
	MessageTypeK8sGetPodLogsResult      = "k8s_get_pod_logs_result"
	MessageTypeK8sGetSummary            = "k8s_get_summary"
	MessageTypeK8sGetSummaryResult      = "k8s_get_summary_result"
	MessageTypeK8sGetNamespaces         = "k8s_get_namespaces"
	MessageTypeK8sGetNamespacesResult   = "k8s_get_namespaces_result"
	MessageTypeK8sGetNodes              = "k8s_get_nodes"
	MessageTypeK8sGetNodesResult        = "k8s_get_nodes_result"
	MessageTypeK8sGetDeployments        = "k8s_get_deployments"
	MessageTypeK8sGetDeploymentsResult  = "k8s_get_deployments_result"
	MessageTypeK8sGetStatefulSets       = "k8s_get_statefulsets"
	MessageTypeK8sGetStatefulSetsResult = "k8s_get_statefulsets_result"
	MessageTypeK8sGetDaemonSets         = "k8s_get_daemonsets"
	MessageTypeK8sGetDaemonSetsResult   = "k8s_get_daemonsets_result"
	MessageTypeK8sGetServices           = "k8s_get_services"
	MessageTypeK8sGetServicesResult     = "k8s_get_services_result"
	MessageTypeK8sGetIngresses          = "k8s_get_ingresses"
	MessageTypeK8sGetIngressesResult    = "k8s_get_ingresses_result"
	MessageTypeK8sGetDiagnostics        = "k8s_get_diagnostics"
	MessageTypeK8sGetDiagnosticsResult  = "k8s_get_diagnostics_result"
	MessageTypeK8sResourceAction        = "k8s_resource_action"
	MessageTypeK8sResourceActionResult  = "k8s_resource_action_result"
	MessageTypeK8sApplyManifest         = "k8s_apply_manifest"
	MessageTypeK8sApplyManifestResult   = "k8s_apply_manifest_result"
)

type HelloMessage struct {
	Type                        string `json:"type"`
	NodeID                      string `json:"node_id"`
	AgentVersion                string `json:"agent_version"`
	ProtocolVersion             int    `json:"protocol_version,omitempty"`
	IdentitySource              string `json:"identity_source,omitempty"`
	Hostname                    string `json:"hostname"`
	Name                        string `json:"name"`
	IP                          string `json:"ip"`
	OS                          string `json:"os"`
	Arch                        string `json:"arch"`
	Kernel                      string `json:"kernel"`
	Terminal                    bool   `json:"terminal"`
	DockerCompose               bool   `json:"docker_compose,omitempty"`
	DockerComposeServiceActions bool   `json:"docker_compose_service_actions,omitempty"`
	DockerResources             bool   `json:"docker_resources,omitempty"`
	SystemdServices             bool   `json:"systemd_services,omitempty"`
	AgentMode                   string `json:"agent_mode,omitempty"`
	AgentUser                   string `json:"agent_user,omitempty"`
	AgentManagement             bool   `json:"agent_management,omitempty"`
	AgentUpgrade                bool   `json:"agent_upgrade,omitempty"`
}

type HelloAckMessage struct {
	Type      string `json:"type"`
	NodeID    string `json:"node_id"`
	NodeToken string `json:"node_token,omitempty"`
	Interval  int    `json:"interval"`
}

type FileListRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	NodeID    string `json:"node_id,omitempty"`
	Path      string `json:"path"`
}

type FileListResponse struct {
	Type      string      `json:"type"`
	RequestID string      `json:"request_id,omitempty"`
	Path      string      `json:"path,omitempty"`
	Entries   []FileEntry `json:"entries,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
	Error     string      `json:"error,omitempty"`
	Code      string      `json:"code,omitempty"`
}

type FileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size,omitempty"`
	Mode       string `json:"mode,omitempty"`
	ModifiedAt int64  `json:"modified_at,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

type FileReadRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	NodeID    string `json:"node_id,omitempty"`
	Path      string `json:"path"`
}

type FileReadResponse struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id,omitempty"`
	Path       string `json:"path,omitempty"`
	Content    string `json:"content,omitempty"`
	Editable   bool   `json:"editable"`
	Size       int64  `json:"size,omitempty"`
	Mode       string `json:"mode,omitempty"`
	ModifiedAt int64  `json:"modified_at,omitempty"`
	Error      string `json:"error,omitempty"`
	Code       string `json:"code,omitempty"`
}

type FileWriteRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	NodeID    string `json:"node_id,omitempty"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

type FileWriteResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Saved     bool   `json:"saved"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

type FileUploadRequest struct {
	Type          string `json:"type"`
	RequestID     string `json:"request_id"`
	NodeID        string `json:"node_id,omitempty"`
	Path          string `json:"path"`
	ContentBase64 string `json:"content_base64"`
}

type FileUploadResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Uploaded  bool   `json:"uploaded"`
	Size      int64  `json:"size,omitempty"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

type FileDeleteRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	NodeID    string `json:"node_id,omitempty"`
	Path      string `json:"path"`
}

type FileDeleteResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Deleted   bool   `json:"deleted"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

type RebootRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	NodeID    string `json:"node_id,omitempty"`
}

type RebootResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Accepted  bool   `json:"accepted"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

type AgentStatusRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	NodeID    string `json:"node_id,omitempty"`
}

type AgentStatusResponse struct {
	Type            string `json:"type"`
	RequestID       string `json:"request_id,omitempty"`
	NodeID          string `json:"node_id,omitempty"`
	Version         string `json:"version,omitempty"`
	User            string `json:"user,omitempty"`
	Mode            string `json:"mode,omitempty"`
	TerminalEnabled bool   `json:"terminal_enabled"`
	DockerAvailable bool   `json:"docker_available"`
	DockerError     string `json:"docker_error,omitempty"`
	ConfigPath      string `json:"config_path,omitempty"`
	ServiceName     string `json:"service_name,omitempty"`
	Uptime          int64  `json:"uptime,omitempty"`
	CollectedAt     int64  `json:"collected_at,omitempty"`
	Error           string `json:"error,omitempty"`
	Code            string `json:"code,omitempty"`
}

type AgentRestartRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	NodeID    string `json:"node_id,omitempty"`
}

type AgentRestartResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Accepted  bool   `json:"accepted"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

type AgentLogsRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	NodeID    string `json:"node_id,omitempty"`
	Lines     int    `json:"lines"`
}

type AgentLogsResponse struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	Lines       int    `json:"lines"`
	Content     string `json:"content,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	CollectedAt int64  `json:"collected_at,omitempty"`
	Error       string `json:"error,omitempty"`
	Code        string `json:"code,omitempty"`
}

type AgentUpgradeRequest struct {
	Type          string `json:"type"`
	RequestID     string `json:"request_id"`
	NodeID        string `json:"node_id"`
	TargetVersion string `json:"target_version"`
	DownloadURL   string `json:"download_url"`
	SHA256        string `json:"sha256"`
}

type AgentUpgradeResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Accepted  bool   `json:"accepted"`
	Stage     string `json:"stage,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

type AgentUpgradeStatus struct {
	NodeID        string `json:"node_id"`
	TargetVersion string `json:"target_version"`
	ActualVersion string `json:"actual_version,omitempty"`
	Stage         string `json:"stage"`
	Error         string `json:"error,omitempty"`
}

type TerminalMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	NodeID    string `json:"node_id,omitempty"`
	Data      string `json:"data,omitempty"`
	Cols      uint16 `json:"cols,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ContainerExecMessage struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	NodeID      string `json:"node_id,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	Command     string `json:"command,omitempty"`
	Data        string `json:"data,omitempty"`
	Cols        uint16 `json:"cols,omitempty"`
	Rows        uint16 `json:"rows,omitempty"`
	ExitCode    int    `json:"exit_code,omitempty"`
	Error       string `json:"error,omitempty"`
}

type MetricsMessage struct {
	Type            string           `json:"type"`
	NodeID          string           `json:"node_id"`
	Timestamp       int64            `json:"timestamp"`
	System          SystemInfo       `json:"system"`
	CPU             CPUInfo          `json:"cpu"`
	Memory          MemoryInfo       `json:"memory"`
	Disk            DiskInfo         `json:"disk"`
	Network         NetworkInfo      `json:"network"`
	Load            LoadInfo         `json:"load"`
	ProcessSnapshot *ProcessSnapshot `json:"process_snapshot,omitempty"`
	DockerSnapshot  *DockerSnapshot  `json:"docker_snapshot,omitempty"`
}

// ProcessSnapshot is the latest bounded process list sampled by an agent.
type ProcessSnapshot struct {
	CollectedAt int64         `json:"collected_at"`
	Processes   []ProcessInfo `json:"processes"`
	Error       string        `json:"error,omitempty"`
}

type ProcessInfo struct {
	PID         int32   `json:"pid"`
	PPID        int32   `json:"ppid"`
	Name        string  `json:"name"`
	Command     string  `json:"command"`
	User        string  `json:"user"`
	Status      string  `json:"status"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryRSS   uint64  `json:"memory_rss"`
	MemoryUsage float64 `json:"memory_usage"`
	CreatedAt   int64   `json:"created_at,omitempty"`
}

// DockerSnapshot is the latest bounded Docker Engine view sampled by an agent.
type DockerSnapshot struct {
	CollectedAt int64           `json:"collected_at"`
	Available   bool            `json:"available"`
	Version     string          `json:"version,omitempty"`
	Error       string          `json:"error,omitempty"`
	Containers  []ContainerInfo `json:"containers"`
}

type ContainerInfo struct {
	ID            string  `json:"id"`
	FullID        string  `json:"full_id,omitempty"`
	Name          string  `json:"name"`
	Image         string  `json:"image"`
	State         string  `json:"state"`
	Status        string  `json:"status"`
	CreatedAt     int64   `json:"created_at,omitempty"`
	StartedAt     int64   `json:"started_at,omitempty"`
	RestartCount  int     `json:"restart_count,omitempty"`
	CPUUsage      float64 `json:"cpu_usage,omitempty"`
	MemoryUsage   uint64  `json:"memory_usage,omitempty"`
	MemoryLimit   uint64  `json:"memory_limit,omitempty"`
	MemoryPercent float64 `json:"memory_percent,omitempty"`
	NetworkRX     uint64  `json:"network_rx,omitempty"`
	NetworkTX     uint64  `json:"network_tx,omitempty"`
}

type SystemInfo struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kernel   string `json:"kernel"`
	Uptime   int64  `json:"uptime"`
}

type CPUInfo struct {
	Cores int     `json:"cores"`
	Usage float64 `json:"usage"`
}

type MemoryInfo struct {
	Total int64   `json:"total"`
	Used  int64   `json:"used"`
	Usage float64 `json:"usage"`
}

type DiskInfo struct {
	Total      int64   `json:"total"`
	Used       int64   `json:"used"`
	Usage      float64 `json:"usage"`
	ReadSpeed  int64   `json:"read_speed"`
	WriteSpeed int64   `json:"write_speed"`
}

type NetworkInfo struct {
	RXSpeed int64 `json:"rx_speed"`
	TXSpeed int64 `json:"tx_speed"`
	RXTotal int64 `json:"rx_total"`
	TXTotal int64 `json:"tx_total"`
}

type LoadInfo struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type LogTailRequest struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	NodeID    string `json:"node_id,omitempty"`
	Path      string `json:"path"`
	Lines     int    `json:"lines"` // Initial number of lines to read (like tail -n)
}

type LogTailResponse struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	NodeID    string `json:"node_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Started   bool   `json:"started"`
	Error     string `json:"error,omitempty"`
}

type LogTailData struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Data      string `json:"data"` // New log lines
}

type LogTailStop struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	NodeID    string `json:"node_id,omitempty"`
}

type LogTailExit struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Error     string `json:"error,omitempty"`
}

type LogTailError struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// Container Logs messages

type ContainerLogsRequest struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	NodeID      string `json:"node_id,omitempty"`
	ContainerID string `json:"container_id"`
	Lines       int    `json:"lines"`      // Initial number of lines to read (like tail -n)
	Follow      bool   `json:"follow"`     // Whether to follow logs (like -f)
	Timestamps  bool   `json:"timestamps"` // Whether to show timestamps
}

type ContainerLogsResponse struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	ContainerID string `json:"container_id,omitempty"`
	Started     bool   `json:"started"`
	Error       string `json:"error,omitempty"`
}

type ContainerLogsData struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Data      string `json:"data"`   // Log content
	Stream    string `json:"stream"` // "stdout" or "stderr"
}

type ContainerLogsStop struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	NodeID    string `json:"node_id,omitempty"`
}

type ContainerLogsExit struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Error     string `json:"error,omitempty"`
}

type ContainerLogsError struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// Docker Exec messages

type DockerExecRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	Command   string `json:"command"` // Full docker command to execute
}

type DockerExecResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Accepted  bool   `json:"accepted"`
	Output    string `json:"output,omitempty"` // Command output (stdout + stderr)
	ExitCode  int    `json:"exit_code"`
	Error     string `json:"error,omitempty"`
}

type ContainerStartRequest struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	ContainerID string `json:"container_id"`
}

type ContainerStartResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

type ContainerStopRequest struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	ContainerID string `json:"container_id"`
}

type ContainerStopResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

type ContainerRestartRequest struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	ContainerID string `json:"container_id"`
}

type ContainerRestartResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

type ContainerDeleteRequest struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	ContainerID string `json:"container_id"`
	Force       bool   `json:"force"` // Force delete even if running
}

type ContainerDeleteResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

type DockerComposeListRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
}

type DockerComposeListResponse struct {
	Type                    string                 `json:"type"`
	RequestID               string                 `json:"request_id,omitempty"`
	Success                 bool                   `json:"success"`
	Supported               bool                   `json:"supported"`
	ServiceActionsSupported bool                   `json:"service_actions_supported"`
	Projects                []DockerComposeProject `json:"projects"`
	Error                   string                 `json:"error,omitempty"`
}

type DockerComposeProject struct {
	Name        string                 `json:"name"`
	Status      string                 `json:"status,omitempty"`
	ConfigFiles []string               `json:"config_files,omitempty"`
	Services    []DockerComposeService `json:"services"`
	Error       string                 `json:"error,omitempty"`
}

type DockerComposeService struct {
	Name          string   `json:"name"`
	ContainerName string   `json:"container_name,omitempty"`
	ContainerID   string   `json:"container_id,omitempty"`
	Image         string   `json:"image,omitempty"`
	State         string   `json:"state,omitempty"`
	Status        string   `json:"status,omitempty"`
	Health        string   `json:"health,omitempty"`
	Ports         []string `json:"ports,omitempty"`
}

type DockerComposeActionRequest struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	ProjectName string `json:"project_name"`
	ServiceName string `json:"service_name,omitempty"`
	Action      string `json:"action"`
}

type DockerComposeActionResponse struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	Success     bool   `json:"success"`
	ProjectName string `json:"project_name,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	Action      string `json:"action,omitempty"`
	Output      string `json:"output,omitempty"`
	Error       string `json:"error,omitempty"`
}

type DockerResourceListRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
}

type DockerResourceListResponse struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	NodeID    string          `json:"node_id,omitempty"`
	Success   bool            `json:"success"`
	Supported bool            `json:"supported"`
	Usage     DockerDiskUsage `json:"usage"`
	Images    []DockerImage   `json:"images"`
	Volumes   []DockerVolume  `json:"volumes"`
	Networks  []DockerNetwork `json:"networks"`
	Error     string          `json:"error,omitempty"`
}

type DockerDiskUsage struct {
	ImageLayers       *int64 `json:"image_layers,omitempty"`
	ContainerWritable *int64 `json:"container_writable,omitempty"`
	Volumes           *int64 `json:"volumes,omitempty"`
	BuildCache        *int64 `json:"build_cache,omitempty"`
}

type DockerImage struct {
	ID         string   `json:"id"`
	FullID     string   `json:"full_id,omitempty"`
	Tags       []string `json:"tags"`
	Size       *int64   `json:"size,omitempty"`
	SharedSize *int64   `json:"shared_size,omitempty"`
	CreatedAt  int64    `json:"created_at,omitempty"`
	Containers *int64   `json:"containers,omitempty"`
}

type DockerVolume struct {
	Name           string `json:"name"`
	Driver         string `json:"driver,omitempty"`
	Scope          string `json:"scope,omitempty"`
	Mountpoint     string `json:"mountpoint,omitempty"`
	ComposeProject string `json:"compose_project,omitempty"`
	Size           *int64 `json:"size,omitempty"`
	RefCount       *int64 `json:"ref_count,omitempty"`
}

type DockerNetwork struct {
	ID         string   `json:"id"`
	FullID     string   `json:"full_id,omitempty"`
	Name       string   `json:"name"`
	Driver     string   `json:"driver,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Subnets    []string `json:"subnets"`
	Containers []string `json:"containers"`
	Internal   bool     `json:"internal,omitempty"`
	Ingress    bool     `json:"ingress,omitempty"`
	Protected  bool     `json:"protected"`
}

type DockerResourceActionRequest struct {
	Type         string `json:"type"`
	RequestID    string `json:"request_id,omitempty"`
	NodeID       string `json:"node_id,omitempty"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Action       string `json:"action"`
}

type DockerResourceActionResponse struct {
	Type         string `json:"type"`
	RequestID    string `json:"request_id,omitempty"`
	Success      bool   `json:"success"`
	Supported    bool   `json:"supported"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Action       string `json:"action,omitempty"`
	Error        string `json:"error,omitempty"`
}

type SystemdServiceListRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
}

type SystemdServiceListResponse struct {
	Type      string           `json:"type"`
	RequestID string           `json:"request_id,omitempty"`
	Success   bool             `json:"success"`
	Supported bool             `json:"supported"`
	Services  []SystemdService `json:"services"`
	Error     string           `json:"error,omitempty"`
}

type SystemdService struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	LoadState     string `json:"load_state,omitempty"`
	ActiveState   string `json:"active_state,omitempty"`
	SubState      string `json:"sub_state,omitempty"`
	UnitFileState string `json:"unit_file_state,omitempty"`
}

type SystemdServiceActionRequest struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	ServiceName string `json:"service_name"`
	Action      string `json:"action"`
}

type SystemdServiceActionResponse struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id,omitempty"`
	Success     bool   `json:"success"`
	ServiceName string `json:"service_name,omitempty"`
	Action      string `json:"action,omitempty"`
	Output      string `json:"output,omitempty"`
	Error       string `json:"error,omitempty"`
}

// K8s 集群连接验证

type K8sClusterConnectRequest struct {
	Type              string `json:"type"`
	RequestID         string `json:"request_id"`
	ClusterID         string `json:"cluster_id"`
	KubeconfigPath    string `json:"kubeconfig_path,omitempty"`
	KubeconfigContent string `json:"kubeconfig_content,omitempty"`
	Context           string `json:"context,omitempty"`
}

type K8sResourceRequest struct {
	Type              string `json:"type"`
	RequestID         string `json:"request_id"`
	ClusterID         string `json:"cluster_id"`
	Namespace         string `json:"namespace,omitempty"`
	KubeconfigContent string `json:"kubeconfig_content"`
	Context           string `json:"context,omitempty"`
}

type K8sResourceSummary struct {
	Version          string `json:"version"`
	NodeCount        int    `json:"node_count"`
	NamespaceCount   int    `json:"namespace_count"`
	PodCount         int    `json:"pod_count"`
	DeploymentCount  int    `json:"deployment_count"`
	StatefulSetCount int    `json:"statefulset_count"`
	DaemonSetCount   int    `json:"daemonset_count"`
	ServiceCount     int    `json:"service_count"`
	IngressCount     int    `json:"ingress_count"`
}

type K8sNamespace struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Age    string `json:"age"`
}

type K8sNode struct {
	Name                   string `json:"name"`
	Status                 string `json:"status"`
	Roles                  string `json:"roles"`
	Version                string `json:"version"`
	InternalIP             string `json:"internal_ip"`
	PodCIDR                string `json:"pod_cidr,omitempty"`
	Age                    string `json:"age"`
	CPUCapacityMilli       int64  `json:"cpu_capacity_milli,omitempty"`
	CPUAllocatableMilli    int64  `json:"cpu_allocatable_milli,omitempty"`
	MemoryCapacityBytes    int64  `json:"memory_capacity_bytes,omitempty"`
	MemoryAllocatableBytes int64  `json:"memory_allocatable_bytes,omitempty"`
	PodCapacity            int64  `json:"pod_capacity,omitempty"`
	PodAllocatable         int64  `json:"pod_allocatable,omitempty"`
}

type K8sDeployment struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"`
	UpToDate  int32  `json:"up_to_date"`
	Available int32  `json:"available"`
	Age       string `json:"age"`
}

type K8sStatefulSet struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Ready       string `json:"ready"`
	ServiceName string `json:"service_name"`
	Age         string `json:"age"`
}

type K8sDaemonSet struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Desired   int32  `json:"desired"`
	Current   int32  `json:"current"`
	Ready     int32  `json:"ready"`
	Available int32  `json:"available"`
	Age       string `json:"age"`
}

type K8sService struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Type       string `json:"type"`
	ClusterIP  string `json:"cluster_ip"`
	ExternalIP string `json:"external_ip,omitempty"`
	Ports      string `json:"ports"`
	Age        string `json:"age"`
}

type K8sIngress struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Class     string `json:"class,omitempty"`
	Hosts     string `json:"hosts"`
	Address   string `json:"address,omitempty"`
	Ports     string `json:"ports"`
	Age       string `json:"age"`
}

type K8sGetSummaryResult struct {
	Type      string              `json:"type"`
	RequestID string              `json:"request_id"`
	Success   bool                `json:"success"`
	Error     string              `json:"error,omitempty"`
	Summary   *K8sResourceSummary `json:"summary,omitempty"`
}

type K8sGetNamespacesResult struct {
	Type       string         `json:"type"`
	RequestID  string         `json:"request_id"`
	Success    bool           `json:"success"`
	Error      string         `json:"error,omitempty"`
	Namespaces []K8sNamespace `json:"namespaces,omitempty"`
}

type K8sGetNodesResult struct {
	Type      string    `json:"type"`
	RequestID string    `json:"request_id"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	Nodes     []K8sNode `json:"nodes,omitempty"`
}

type K8sGetDeploymentsResult struct {
	Type        string          `json:"type"`
	RequestID   string          `json:"request_id"`
	Success     bool            `json:"success"`
	Error       string          `json:"error,omitempty"`
	Deployments []K8sDeployment `json:"deployments,omitempty"`
}

type K8sGetStatefulSetsResult struct {
	Type         string           `json:"type"`
	RequestID    string           `json:"request_id"`
	Success      bool             `json:"success"`
	Error        string           `json:"error,omitempty"`
	StatefulSets []K8sStatefulSet `json:"statefulsets,omitempty"`
}

type K8sGetDaemonSetsResult struct {
	Type       string         `json:"type"`
	RequestID  string         `json:"request_id"`
	Success    bool           `json:"success"`
	Error      string         `json:"error,omitempty"`
	DaemonSets []K8sDaemonSet `json:"daemonsets,omitempty"`
}

type K8sGetServicesResult struct {
	Type      string       `json:"type"`
	RequestID string       `json:"request_id"`
	Success   bool         `json:"success"`
	Error     string       `json:"error,omitempty"`
	Services  []K8sService `json:"services,omitempty"`
}

type K8sGetIngressesResult struct {
	Type      string       `json:"type"`
	RequestID string       `json:"request_id"`
	Success   bool         `json:"success"`
	Error     string       `json:"error,omitempty"`
	Ingresses []K8sIngress `json:"ingresses,omitempty"`
}

type K8sDiagnosticsRequest struct {
	Type              string `json:"type"`
	RequestID         string `json:"request_id"`
	ClusterID         string `json:"cluster_id"`
	Kind              string `json:"kind"`
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	KubeconfigContent string `json:"kubeconfig_content,omitempty"`
	Context           string `json:"context,omitempty"`
}

type K8sDiagnostics struct {
	Kind       string               `json:"kind"`
	Namespace  string               `json:"namespace"`
	Name       string               `json:"name"`
	Status     string               `json:"status"`
	Age        string               `json:"age,omitempty"`
	Node       string               `json:"node,omitempty"`
	IP         string               `json:"ip,omitempty"`
	Metadata   map[string]string    `json:"metadata,omitempty"`
	Summary    map[string]string    `json:"summary,omitempty"`
	Containers []K8sContainerDetail `json:"containers,omitempty"`
	Conditions []K8sCondition       `json:"conditions,omitempty"`
	Events     []K8sEvent           `json:"events,omitempty"`
	YAML       string               `json:"yaml"`
	Describe   string               `json:"describe"`
}

type K8sContainerDetail struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restart_count"`
	State        string `json:"state,omitempty"`
}

type K8sCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type K8sEvent struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Count   int32  `json:"count"`
	Age     string `json:"age,omitempty"`
}

type K8sGetDiagnosticsResult struct {
	Type        string          `json:"type"`
	RequestID   string          `json:"request_id"`
	Success     bool            `json:"success"`
	Error       string          `json:"error,omitempty"`
	Diagnostics *K8sDiagnostics `json:"diagnostics,omitempty"`
}

type K8sResourceActionRequest struct {
	Type              string `json:"type"`
	RequestID         string `json:"request_id"`
	ClusterID         string `json:"cluster_id"`
	Kind              string `json:"kind"`
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	Action            string `json:"action"`
	Replicas          *int32 `json:"replicas,omitempty"`
	YAML              string `json:"yaml,omitempty"`
	KubeconfigContent string `json:"kubeconfig_content,omitempty"`
	Context           string `json:"context,omitempty"`
}

type K8sResourceActionResult struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	Message   string `json:"message,omitempty"`
}

type K8sApplyManifestRequest struct {
	Type              string `json:"type"`
	RequestID         string `json:"request_id"`
	ClusterID         string `json:"cluster_id"`
	YAML              string `json:"yaml"`
	DryRun            bool   `json:"dry_run"`
	KubeconfigContent string `json:"kubeconfig_content,omitempty"`
	Context           string `json:"context,omitempty"`
}

type K8sApplyManifestResult struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	Message   string `json:"message,omitempty"`
}

type K8sClusterConnectResult struct {
	Type        string          `json:"type"`
	RequestID   string          `json:"request_id"`
	Success     bool            `json:"success"`
	Error       string          `json:"error,omitempty"`
	ClusterInfo *K8sClusterInfo `json:"cluster_info,omitempty"`
}

type K8sClusterInfo struct {
	Version        string `json:"version"`
	NodeCount      int    `json:"node_count"`
	NamespaceCount int    `json:"namespace_count"`
}

// K8s Pod 查询

type K8sGetPodsRequest struct {
	Type              string `json:"type"`
	RequestID         string `json:"request_id"`
	ClusterID         string `json:"cluster_id"`
	Namespace         string `json:"namespace,omitempty"`
	KubeconfigPath    string `json:"kubeconfig_path,omitempty"`
	KubeconfigContent string `json:"kubeconfig_content,omitempty"`
	Context           string `json:"context,omitempty"`
}

type K8sGetPodsResult struct {
	Type      string   `json:"type"`
	RequestID string   `json:"request_id"`
	Success   bool     `json:"success"`
	Error     string   `json:"error,omitempty"`
	Pods      []K8sPod `json:"pods,omitempty"`
}

type K8sPod struct {
	Name             string            `json:"name"`
	Namespace        string            `json:"namespace"`
	Status           string            `json:"status"`   // Running, Pending, Failed, etc.
	Ready            string            `json:"ready"`    // 1/1, 0/1, etc.
	Restarts         int               `json:"restarts"` // 重启次数
	Age              string            `json:"age"`      // 运行时间
	Node             string            `json:"node"`     // 所在节点
	IP               string            `json:"ip,omitempty"`
	WorkloadKind     string            `json:"workload_kind,omitempty"`
	WorkloadName     string            `json:"workload_name,omitempty"`
	MetricsAvailable bool              `json:"metrics_available"`
	CPUUsageMilli    int64             `json:"cpu_usage_milli,omitempty"`
	MemoryUsageBytes int64             `json:"memory_usage_bytes,omitempty"`
	Containers       []K8sPodContainer `json:"containers,omitempty"`
}

type K8sPodContainer struct {
	Name               string `json:"name"`
	Image              string `json:"image,omitempty"`
	Ready              bool   `json:"ready"`
	RestartCount       int    `json:"restart_count"`
	State              string `json:"state,omitempty"`
	StateReason        string `json:"state_reason,omitempty"`
	CPUUsageMilli      int64  `json:"cpu_usage_milli,omitempty"`
	MemoryUsageBytes   int64  `json:"memory_usage_bytes,omitempty"`
	CPURequestMilli    int64  `json:"cpu_request_milli,omitempty"`
	CPULimitMilli      int64  `json:"cpu_limit_milli,omitempty"`
	MemoryRequestBytes int64  `json:"memory_request_bytes,omitempty"`
	MemoryLimitBytes   int64  `json:"memory_limit_bytes,omitempty"`
}

// K8s Pod 日志

type K8sGetPodLogsRequest struct {
	Type              string `json:"type"`
	RequestID         string `json:"request_id"`
	ClusterID         string `json:"cluster_id"`
	Namespace         string `json:"namespace"`
	PodName           string `json:"pod_name"`
	Container         string `json:"container,omitempty"`
	Follow            bool   `json:"follow"`
	TailLines         int    `json:"tail_lines"`
	KubeconfigContent string `json:"kubeconfig_content,omitempty"`
	Context           string `json:"context,omitempty"`
}

type K8sGetPodLogsResult struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	Logs      string `json:"logs,omitempty"` // 日志内容
	Stream    bool   `json:"stream"`         // 是否为流式响应
}
