export type RangeOption = '1h' | '6h' | '24h' | '3d' | '7d'
export type AgentMode = 'normal' | 'ops'

export type AuthSessionResponse = {
  auth_enabled: boolean
  authenticated: boolean
  username: string
}

export type LoginResponse = {
  authenticated: boolean
  username: string
}

export type SettingsResponse = {
  metrics_retention: RangeOption
  metrics_retention_seconds: number
  max_metrics_retention: RangeOption
}

export type SettingsUpdate = {
  metrics_retention: RangeOption
}

export type SystemAboutResponse = {
  version: string
  github_url: string
}

export type Metric = {
  id: number
  node_id: string
  cpu_usage: number
  cpu_cores: number
  memory_total: number
  memory_used: number
  memory_usage: number
  disk_total: number
  disk_used: number
  disk_usage: number
  uptime?: number
  disk_read_speed?: number
  disk_write_speed?: number
  rx_speed: number
  tx_speed: number
  rx_total: number
  tx_total: number
  load1: number
  load5: number
  load15: number
  created_at: string
}

export type Node = {
  id: string
  name: string
  hostname: string
  ip: string
  os: string
  arch: string
  kernel: string
  agent_version: string
  status: 'online' | 'offline' | string
  last_seen_at: string
  terminal_enabled?: boolean
  agent_mode?: AgentMode
  agent_user?: string
  latest_metric?: Metric
}

export type NodesResponse = {
  nodes: Node[]
}

export type MetricsResponse = {
  metrics: Metric[]
}

export type InstallPlatform = 'linux' | 'windows'

export type InstallCommandOptions = {
  enableDocker?: boolean
  enableTerminal?: boolean
  mode?: AgentMode
}

export type InstallCommandResponse = {
  command: string
  install_token: string
}

export type SSHAuthType = 'password' | 'private_key'

export type SSHInstallRequest = {
  host: string
  port: number
  username: 'root'
  auth_type: SSHAuthType
  password?: string
  private_key?: string
  passphrase?: string
  node_id: string
  name: string
  enable_terminal: boolean
  enable_docker: boolean
  mode: AgentMode
}

export type SSHUninstallRequest = {
  host: string
  port: number
  username: 'root'
  auth_type: SSHAuthType
  password?: string
  private_key?: string
  passphrase?: string
  remove_node_record: boolean
}

export type SSHJobResponse = {
  job_id: string
}

export type SSHProgressStatus = 'pending' | 'running' | 'success' | 'failed'

export type SSHProgressEvent = {
  step: string
  label: string
  status: SSHProgressStatus
  message: string
  done?: boolean
}

export type ProcessInfo = {
  pid: number
  ppid: number
  name: string
  command: string
  user: string
  status: string
  cpu_usage: number
  memory_rss: number
  memory_usage: number
  created_at?: number
}

export type ProcessSnapshotResponse = {
  node_id: string
  collected_at: number
  error: string
  processes: ProcessInfo[]
}

export type DockerContainer = {
  id: string
  full_id?: string
  name: string
  image: string
  state: string
  status: string
  created_at?: number
  started_at?: number
  restart_count?: number
  cpu_usage?: number
  memory_usage?: number
  memory_limit?: number
  memory_percent?: number
  network_rx?: number
  network_tx?: number
}

export type DockerSnapshotResponse = {
  node_id: string
  collected_at: number
  available: boolean
  version?: string
  error: string
  containers: DockerContainer[]
}

export type ContainerLogsRequest = {
  type: 'container_logs_request'
  session_id: string
  node_id: string
  container_id: string
  lines: number
  follow: boolean
  timestamps: boolean
}

export type ContainerLogsResponse = {
  type: 'container_logs_response'
  session_id: string
  container_id: string
  started: boolean
  error?: string
}

export type ContainerLogsData = {
  type: 'container_logs_data'
  session_id: string
  data: string
  stream: 'stdout' | 'stderr'
}

export type ContainerLogsStop = {
  type: 'container_logs_stop'
  session_id: string
  node_id?: string
}

export type ContainerLogsExit = {
  type: 'container_logs_exit'
  session_id: string
  error?: string
}

export type ContainerLogsError = {
  type: 'container_logs_error'
  session_id: string
  error: string
}

export type FileEntry = {
  name: string
  path: string
  type: 'directory' | 'file' | 'binary' | 'symlink' | string
  size?: number
  mode?: string
  modified_at?: number
  link_target?: string
}

export type FileListResponse = {
  path: string
  entries: FileEntry[]
  truncated?: boolean
  error?: string
  code?: string
}

export type FileReadResponse = {
  path: string
  content?: string
  editable: boolean
  size?: number
  mode?: string
  modified_at?: number
  error?: string
  code?: string
}

export type FileWriteResponse = {
  path: string
  saved: boolean
  error?: string
  code?: string
}

export type FileUploadResponse = {
  path: string
  uploaded: boolean
  size?: number
  error?: string
  code?: string
}

export type FileDeleteResponse = {
  path: string
  deleted: boolean
  error?: string
  code?: string
}

export type RebootResponse = {
  accepted: boolean
  error?: string
  code?: string
}

export type AgentStatusResponse = {
  version?: string
  user?: string
  mode?: AgentMode | string
  terminal_enabled: boolean
  docker_available: boolean
  docker_error?: string
  config_path?: string
  service_name?: string
  uptime?: number
  collected_at?: number
  error?: string
  code?: string
}

export type AgentRestartResponse = {
  accepted: boolean
  message?: string
  error?: string
  code?: string
}

export type AgentLogsResponse = {
  lines: number
  content?: string
  truncated?: boolean
  collected_at?: number
  error?: string
  code?: string
}

export type AlertRule = {
  id: number
  name: string
  enabled: boolean
  metric_field: string
  operator: string
  threshold: number
  duration_seconds: number
  scope_type: 'all' | 'nodes'
  scope_node_ids?: string[]
  notification_channels: NotificationChannel[]
  created_at: string
  updated_at: string
}

export type NotificationChannel = {
  type: 'webhook' | 'dingtalk' | 'feishu' | 'email'
  webhook_url?: string
  secret?: string
  headers?: Record<string, string>
}

export type AlertHistory = {
  id: number
  rule_id: number
  rule_name: string
  node_id: string
  node_name: string
  metric_field: string
  metric_value: number
  threshold: number
  triggered_at: string
  resolved_at?: string
  notification_sent: boolean
  notification_error?: string
  created_at: string
}

export type AlertRulesResponse = {
  rules: AlertRule[]
}

export type AlertHistoryResponse = {
  history: AlertHistory[]
}

// K8s 集群管理类型

export type K8sCluster = {
  id: string
  name: string
  node_id: string
  node_name: string
  node_ip: string
  node_status: 'online' | 'offline'  // Agent 节点状态
  node_last_seen_at?: string
  kubeconfig_path?: string
  context?: string
  status: 'online' | 'offline'  // K8s API 连接状态
  version?: string
  node_count?: number
  namespace_count?: number
  last_seen_at?: string
  created_at: string
  updated_at: string
}

export type K8sClusterInfo = {
  version: string
  node_count: number
  namespace_count: number
}

export type K8sClustersResponse = {
  clusters: K8sCluster[]
}

export type ConnectK8sClusterRequest = {
  name: string
  node_id: string
  kubeconfig_content: string
  context?: string
}

export type ConnectK8sClusterResponse = {
  success: boolean
  cluster: K8sCluster
  cluster_info: K8sClusterInfo
}

export type K8sResourceSummary = {
  version: string
  node_count: number
  namespace_count: number
  pod_count: number
  deployment_count: number
  statefulset_count: number
  daemonset_count: number
  service_count: number
  ingress_count: number
}

export type K8sNamespace = { name: string; status: string; age: string }
export type K8sNode = {
  name: string
  status: string
  roles: string
  version: string
  internal_ip: string
  pod_cidr?: string
  age: string
  cpu_capacity_milli?: number
  cpu_allocatable_milli?: number
  memory_capacity_bytes?: number
  memory_allocatable_bytes?: number
  pod_capacity?: number
  pod_allocatable?: number
}
export type K8sDeployment = { name: string; namespace: string; ready: string; up_to_date: number; available: number; age: string }
export type K8sStatefulSet = { name: string; namespace: string; ready: string; service_name: string; age: string }
export type K8sDaemonSet = { name: string; namespace: string; desired: number; current: number; ready: number; available: number; age: string }
export type K8sService = { name: string; namespace: string; type: string; cluster_ip: string; external_ip?: string; ports: string; age: string }
export type K8sIngress = { name: string; namespace: string; class?: string; hosts: string; address?: string; ports: string; age: string }
export type K8sResourceKind = 'pod' | 'deployment' | 'statefulset' | 'daemonset'

export type K8sContainerDetail = {
  name: string
  image: string
  ready: boolean
  restart_count: number
  state?: string
}

export type K8sCondition = {
  type: string
  status: string
  reason?: string
  message?: string
}

export type K8sEvent = {
  type: string
  reason: string
  message: string
  count: number
  age?: string
}

export type K8sDiagnostics = {
  kind: K8sResourceKind
  namespace: string
  name: string
  status: string
  age?: string
  node?: string
  ip?: string
  metadata?: Record<string, string>
  summary?: Record<string, string>
  containers?: K8sContainerDetail[]
  conditions?: K8sCondition[]
  events?: K8sEvent[]
  yaml: string
  describe: string
}

export type K8sSummaryResponse = { success: boolean; summary: K8sResourceSummary }
export type K8sNamespacesResponse = { success: boolean; namespaces: K8sNamespace[] }
export type K8sNodesResponse = { success: boolean; nodes: K8sNode[] }
export type K8sDeploymentsResponse = { success: boolean; deployments: K8sDeployment[] }
export type K8sStatefulSetsResponse = { success: boolean; statefulsets: K8sStatefulSet[] }
export type K8sDaemonSetsResponse = { success: boolean; daemonsets: K8sDaemonSet[] }
export type K8sServicesResponse = { success: boolean; services: K8sService[] }
export type K8sIngressesResponse = { success: boolean; ingresses: K8sIngress[] }
export type K8sDiagnosticsResponse = { success: boolean; diagnostics: K8sDiagnostics }

export type K8sResourceActionName = 'delete' | 'restart' | 'scale' | 'dry_run_apply' | 'apply'

export type K8sResourceActionRequest = {
  action: K8sResourceActionName
  replicas?: number
  yaml?: string
}

export type K8sResourceActionResponse = {
  success: boolean
  message?: string
}

export type K8sApplyManifestRequest = {
  yaml: string
  dry_run?: boolean
}

export type K8sApplyManifestResponse = {
  success: boolean
  message?: string
}

export type K8sPod = {
  name: string
  namespace: string
  status: string
  ready: string
  restarts: number
  age: string
  node: string
  ip?: string
  workload_kind?: string
  workload_name?: string
  metrics_available?: boolean
  cpu_usage_milli?: number
  memory_usage_bytes?: number
  containers?: K8sPodContainer[]
}

export type K8sPodContainer = {
  name: string
  image?: string
  ready: boolean
  restart_count: number
  state?: string
  state_reason?: string
  cpu_usage_milli?: number
  memory_usage_bytes?: number
  cpu_request_milli?: number
  cpu_limit_milli?: number
  memory_request_bytes?: number
  memory_limit_bytes?: number
}

export type K8sPodsResponse = {
  success: boolean
  pods: K8sPod[]
}

export type K8sPodLogsResponse = {
  success: boolean
  logs: string
}
