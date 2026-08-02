import type { AgentLogsResponse, AgentRestartResponse, AgentStatusResponse, AgentUpgradeResponse, AIConversation, AIConversationState, AIProvider, AIProviderInput, AISendResult, AlertHistory, AlertHistoryResponse, AlertRule, AlertRulesResponse, ApplicationServiceDetail, ApplicationServiceInput, ApplicationServiceSummary, AuditCleanupRequest, AuditCleanupResponse, AuditEventsQuery, AuditEventsResponse, AutomationRun, AutomationRunDetail, AutomationRunsQuery, AutomationRunsResponse, AutomationScript, AutomationScriptInput, AutomationScriptsResponse, AuthSessionResponse, BatchNodeMetadataResponse, BatchNodeMetadataUpdate, ConnectionDiagnostics, DockerComposeAction, DockerComposeActionResponse, DockerComposeDeploymentRequest, DockerComposeDeploymentResponse, DockerComposeListResponse, DockerResourceAction, DockerResourceActionResponse, DockerResourceListResponse, DockerResourceType, DockerSnapshotResponse, FileDeleteResponse, FileListResponse, FileReadResponse, FileUploadResponse, FileWriteResponse, InstallCommandOptions, InstallCommandResponse, InstallPlatform, LoginResponse, MetricsResponse, NodeGroup, NodeGroupsResponse, NodeTag, NodeTagsResponse, NodesResponse, ProcessSnapshotResponse, RangeOption, RebootResponse, ScheduledTask, ScheduledTaskInput, ScheduledTasksResponse, SettingsResponse, SettingsUpdate, SSHInstallRequest, SSHJobResponse, SSHUninstallRequest, SystemdServiceAction, SystemdServiceActionResponse, SystemdServiceListResponse, K8sClustersResponse, SystemAboutResponse, SystemLogsResponse, UptimeIncidentsResponse, UptimeMonitor, UptimeMonitorInput, UptimeMonitorsResponse, UptimeResultsResponse } from '../types'

export type SessionTokenResponse = {
  token: string
}

export class APIError extends Error {
  constructor(public status: number, message = `Request failed: ${status}`) {
    super(message)
  }
}

let onUnauthorized: (() => void) | undefined

export function setUnauthorizedHandler(handler: () => void) {
  onUnauthorized = handler
}

async function errorMessage(response: Response): Promise<string> {
  try {
    const body = await response.json() as { error?: string }
    return body.error || `Request failed: ${response.status}`
  } catch {
    return `Request failed: ${response.status}`
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = init === undefined ? await fetch(path) : await fetch(path, init)
  if (!response.ok) {
    const error = new APIError(response.status, await errorMessage(response))
    if (response.status === 401 && onUnauthorized) {
      onUnauthorized()
    }
    throw error
  }
  return response.json() as Promise<T>
}

async function requestVoid(path: string, init?: RequestInit): Promise<void> {
  const response = init === undefined ? await fetch(path) : await fetch(path, init)
  if (!response.ok) {
    const error = new APIError(response.status, await errorMessage(response))
    if (response.status === 401 && onUnauthorized) {
      onUnauthorized()
    }
    throw error
  }
}

export function getAuthSession(): Promise<AuthSessionResponse> {
  return request<AuthSessionResponse>('/api/auth/session')
}

export function login(username: string, password: string): Promise<LoginResponse> {
  return request<LoginResponse>('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password })
  })
}

export function logout(): Promise<void> {
  return requestVoid('/api/auth/logout', { method: 'POST' })
}

export function getNodes(signal?: AbortSignal): Promise<NodesResponse> {
  return request<NodesResponse>('/api/nodes', signal ? { signal } : undefined)
}

export function getNodeGroups(): Promise<NodeGroupsResponse> {
  return request<NodeGroupsResponse>('/api/node-groups')
}

export function createNodeGroup(name: string): Promise<NodeGroup> {
  return request<NodeGroup>('/api/node-groups', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  })
}

export function updateNodeGroup(groupID: string, name: string): Promise<NodeGroup> {
  return request<NodeGroup>(`/api/node-groups/${encodeURIComponent(groupID)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  })
}

export function deleteNodeGroup(groupID: string): Promise<void> {
  return requestVoid(`/api/node-groups/${encodeURIComponent(groupID)}`, { method: 'DELETE' })
}

export function getNodeTags(): Promise<NodeTagsResponse> {
  return request<NodeTagsResponse>('/api/node-tags')
}

export function createNodeTag(name: string, color: NodeTag['color']): Promise<NodeTag> {
  return request<NodeTag>('/api/node-tags', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, color })
  })
}

export function updateNodeTag(tagID: string, name: string, color: NodeTag['color']): Promise<NodeTag> {
  return request<NodeTag>(`/api/node-tags/${encodeURIComponent(tagID)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, color })
  })
}

export function deleteNodeTag(tagID: string): Promise<void> {
  return requestVoid(`/api/node-tags/${encodeURIComponent(tagID)}`, { method: 'DELETE' })
}

export function updateBatchNodeMetadata(update: BatchNodeMetadataUpdate): Promise<BatchNodeMetadataResponse> {
  return request<BatchNodeMetadataResponse>('/api/nodes/batch/metadata', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(update)
  })
}

export function getSettings(): Promise<SettingsResponse> {
  return request<SettingsResponse>('/api/settings')
}

export function getSystemAbout(): Promise<SystemAboutResponse> {
  return request<SystemAboutResponse>('/api/system/about')
}

export function updateSettings(settings: SettingsUpdate): Promise<SettingsResponse> {
  return request<SettingsResponse>('/api/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings)
  })
}

export function deleteNode(nodeID: string): Promise<void> {
  return requestVoid(`/api/nodes/${encodeURIComponent(nodeID)}`, { method: 'DELETE' })
}

export function getNodeMetrics(nodeID: string, range: RangeOption): Promise<MetricsResponse> {
  return request<MetricsResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/metrics?range=${range}`)
}

export function createInstallCommand(platform: InstallPlatform = 'linux', _options: InstallCommandOptions = {}): Promise<InstallCommandResponse> {
  void _options
  const params = new URLSearchParams({ platform })
  return request<InstallCommandResponse>(`/api/install/command?${params.toString()}`, { method: 'POST' })
}

export function startSSHInstall(requestBody: SSHInstallRequest): Promise<SSHJobResponse> {
  return request<SSHJobResponse>('/api/install/ssh', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(requestBody)
  })
}

export function startSSHUninstall(nodeID: string, requestBody: SSHUninstallRequest): Promise<SSHJobResponse> {
  return request<SSHJobResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/ssh-uninstall`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(requestBody)
  })
}

export function getNodeProcesses(nodeID: string): Promise<ProcessSnapshotResponse> {
  return request<ProcessSnapshotResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/processes`)
}

export function getNodeDocker(nodeID: string, signal?: AbortSignal): Promise<DockerSnapshotResponse> {
  return request<DockerSnapshotResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/docker`, signal ? { signal } : undefined)
}

export function getNodeDockerCompose(nodeID: string): Promise<DockerComposeListResponse> {
  return request<DockerComposeListResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/docker/compose`)
}

export function runNodeDockerComposeAction(nodeID: string, projectName: string, action: DockerComposeAction, serviceName?: string): Promise<DockerComposeActionResponse> {
  return request<DockerComposeActionResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/docker/compose/action`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project_name: projectName, action, ...(serviceName ? { service_name: serviceName } : {}) })
  })
}

export function runNodeDockerComposeDeployment(nodeID: string, requestBody: DockerComposeDeploymentRequest): Promise<DockerComposeDeploymentResponse> {
  return request<DockerComposeDeploymentResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/docker/compose/deployment`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(requestBody)
  })
}

export function getNodeDockerResources(nodeID: string): Promise<DockerResourceListResponse> {
  return request<DockerResourceListResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/docker/resources`)
}

export function runNodeDockerResourceAction(nodeID: string, resourceType: DockerResourceType, resourceID: string, action: DockerResourceAction): Promise<DockerResourceActionResponse> {
  return request<DockerResourceActionResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/docker/resources/action`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ resource_type: resourceType, resource_id: resourceID, action })
  })
}

export function getNodeSystemdServices(nodeID: string, signal?: AbortSignal): Promise<SystemdServiceListResponse> {
  return request<SystemdServiceListResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/services/systemd`, signal ? { signal } : undefined)
}

export function runNodeSystemdServiceAction(nodeID: string, serviceName: string, action: SystemdServiceAction, lines?: number, signal?: AbortSignal): Promise<SystemdServiceActionResponse> {
  return request<SystemdServiceActionResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/services/systemd/action`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ service_name: serviceName, action, lines }),
    signal
  })
}

export function getNodeFiles(nodeID: string, path: string): Promise<FileListResponse> {
  return request<FileListResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/files?path=${encodeURIComponent(path)}`)
}

export function readNodeFile(nodeID: string, path: string): Promise<FileReadResponse> {
  return request<FileReadResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/files/content?path=${encodeURIComponent(path)}`)
}

export function writeNodeFile(nodeID: string, path: string, content: string): Promise<FileWriteResponse> {
  return request<FileWriteResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/files/content`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content })
  })
}

export function uploadNodeFile(nodeID: string, path: string, contentBase64: string): Promise<FileUploadResponse> {
  return request<FileUploadResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/files/upload`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content_base64: contentBase64 })
  })
}

export function deleteNodePath(nodeID: string, path: string): Promise<FileDeleteResponse> {
  return request<FileDeleteResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/files/content`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path })
  })
}

export function rebootNode(nodeID: string): Promise<RebootResponse> {
  return request<RebootResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/reboot`, { method: 'POST' })
}

export function getAgentStatus(nodeID: string): Promise<AgentStatusResponse> {
  return request<AgentStatusResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/agent/status`)
}

export function getConnectionDiagnostics(nodeID: string): Promise<ConnectionDiagnostics> {
  return request<ConnectionDiagnostics>(`/api/nodes/${encodeURIComponent(nodeID)}/connection-diagnostics`)
}

export function restartAgent(nodeID: string): Promise<AgentRestartResponse> {
  return request<AgentRestartResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/agent/restart`, { method: 'POST' })
}

export function upgradeAgent(nodeID: string): Promise<AgentUpgradeResponse> {
  return request<AgentUpgradeResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/agent/upgrade`, { method: 'POST' })
}

export function getAgentUpgradeStatus(nodeID: string): Promise<{ node_id: string; target_version: string; actual_version?: string; stage: string; error?: string }> {
  return request(`/api/nodes/${encodeURIComponent(nodeID)}/agent/upgrade/status`)
}

export function getAgentLogs(nodeID: string, lines = 100, signal?: AbortSignal): Promise<AgentLogsResponse> {
  return request<AgentLogsResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/agent/logs?lines=${encodeURIComponent(lines.toString())}`, signal ? { signal } : undefined)
}

export function getSystemLogs(lines = 200, signal?: AbortSignal): Promise<SystemLogsResponse> {
  return request<SystemLogsResponse>(`/api/system/logs?lines=${encodeURIComponent(lines.toString())}`, signal ? { signal } : undefined)
}

export function createTerminalSession(nodeID: string): Promise<SessionTokenResponse> {
  return request<SessionTokenResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/terminal/session`, { method: 'POST' })
}

export function createContainerExecSession(nodeID: string, containerID: string): Promise<SessionTokenResponse> {
  return request<SessionTokenResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/containers/${encodeURIComponent(containerID)}/exec/session`, { method: 'POST' })
}

export function getAlertRules(): Promise<AlertRulesResponse> {
  return request<AlertRulesResponse>('/api/alerts/rules')
}

export function createAlertRule(rule: Omit<AlertRule, 'id' | 'created_at' | 'updated_at'>): Promise<AlertRule> {
  return request<AlertRule>('/api/alerts/rules', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(rule)
  })
}

export function updateAlertRule(id: number, rule: Omit<AlertRule, 'id' | 'created_at' | 'updated_at'>): Promise<AlertRule> {
  return request<AlertRule>(`/api/alerts/rules/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(rule)
  })
}

export function deleteAlertRule(id: number): Promise<void> {
  return requestVoid(`/api/alerts/rules/${id}`, { method: 'DELETE' })
}

export function toggleAlertRule(id: number, enabled: boolean): Promise<AlertRule> {
  return request<AlertRule>(`/api/alerts/rules/${id}/toggle`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled })
  })
}

export function getUptimeMonitors(): Promise<UptimeMonitorsResponse> {
  return request<UptimeMonitorsResponse>('/api/uptime/monitors')
}

export function createUptimeMonitor(monitor: UptimeMonitorInput): Promise<UptimeMonitor> {
  return request<UptimeMonitor>('/api/uptime/monitors', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(monitor)
  })
}

export function updateUptimeMonitor(id: number, monitor: UptimeMonitorInput): Promise<UptimeMonitor> {
  return request<UptimeMonitor>(`/api/uptime/monitors/${encodeURIComponent(id.toString())}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(monitor)
  })
}

export function deleteUptimeMonitor(id: number): Promise<void> {
  return requestVoid(`/api/uptime/monitors/${encodeURIComponent(id.toString())}`, { method: 'DELETE' })
}

export function toggleUptimeMonitor(id: number, enabled: boolean): Promise<UptimeMonitor> {
  return request<UptimeMonitor>(`/api/uptime/monitors/${encodeURIComponent(id.toString())}/toggle`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled })
  })
}

export function checkUptimeMonitor(id: number): Promise<UptimeMonitor> {
  return request<UptimeMonitor>(`/api/uptime/monitors/${encodeURIComponent(id.toString())}/check`, { method: 'POST' })
}

export function getUptimeResults(id: number, limit = 50): Promise<UptimeResultsResponse> {
  return request<UptimeResultsResponse>(`/api/uptime/monitors/${encodeURIComponent(id.toString())}/results?limit=${encodeURIComponent(limit.toString())}`)
}

export function getUptimeIncidents(id: number, limit = 50): Promise<UptimeIncidentsResponse> {
  return request<UptimeIncidentsResponse>(`/api/uptime/monitors/${encodeURIComponent(id.toString())}/incidents?limit=${encodeURIComponent(limit.toString())}`)
}

export function getAuditEvents(query: AuditEventsQuery = {}, signal?: AbortSignal): Promise<AuditEventsResponse> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== '') params.set(key, String(value))
  }
  const suffix = params.size > 0 ? `?${params.toString()}` : ''
  return request<AuditEventsResponse>(`/api/audit/events${suffix}`, signal ? { signal } : undefined)
}

export function cleanupAuditEvents(cleanup: AuditCleanupRequest): Promise<AuditCleanupResponse> {
  return request<AuditCleanupResponse>('/api/audit/events/cleanup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cleanup)
  })
}

export function getAutomationScripts(signal?: AbortSignal): Promise<AutomationScriptsResponse> {
  return request<AutomationScriptsResponse>('/api/automation/scripts', signal ? { signal } : undefined)
}

export function createAutomationScript(script: AutomationScriptInput): Promise<AutomationScript> {
  return request<AutomationScript>('/api/automation/scripts', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(script)
  })
}

export function updateAutomationScript(id: number, script: AutomationScriptInput): Promise<AutomationScript> {
  return request<AutomationScript>(`/api/automation/scripts/${encodeURIComponent(id.toString())}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(script)
  })
}

export function deleteAutomationScript(id: number): Promise<void> {
  return requestVoid(`/api/automation/scripts/${encodeURIComponent(id.toString())}`, { method: 'DELETE' })
}

export function runAutomationScript(id: number, nodeIDs: string[]): Promise<AutomationRun> {
  return request<AutomationRun>(`/api/automation/scripts/${encodeURIComponent(id.toString())}/runs`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ node_ids: nodeIDs })
  })
}

export function getScheduledTasks(signal?: AbortSignal): Promise<ScheduledTasksResponse> {
  return request<ScheduledTasksResponse>('/api/automation/tasks', signal ? { signal } : undefined)
}

export function createScheduledTask(task: ScheduledTaskInput): Promise<ScheduledTask> {
  return request<ScheduledTask>('/api/automation/tasks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(task)
  })
}

export function updateScheduledTask(id: number, task: ScheduledTaskInput): Promise<ScheduledTask> {
  return request<ScheduledTask>(`/api/automation/tasks/${encodeURIComponent(id.toString())}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(task)
  })
}

export function deleteScheduledTask(id: number): Promise<void> {
  return requestVoid(`/api/automation/tasks/${encodeURIComponent(id.toString())}`, { method: 'DELETE' })
}

export function toggleScheduledTask(id: number, enabled: boolean): Promise<ScheduledTask> {
  return request<ScheduledTask>(`/api/automation/tasks/${encodeURIComponent(id.toString())}/toggle`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled })
  })
}

export function runScheduledTask(id: number): Promise<AutomationRun> {
  return request<AutomationRun>(`/api/automation/tasks/${encodeURIComponent(id.toString())}/runs`, { method: 'POST' })
}

export function getAutomationRuns(query: AutomationRunsQuery = {}, signal?: AbortSignal): Promise<AutomationRunsResponse> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== '') params.set(key, String(value))
  }
  const suffix = params.size > 0 ? `?${params.toString()}` : ''
  return request<AutomationRunsResponse>(`/api/automation/runs${suffix}`, signal ? { signal } : undefined)
}

export function getAutomationRun(id: number, signal?: AbortSignal): Promise<AutomationRunDetail> {
  return request<AutomationRunDetail>(`/api/automation/runs/${encodeURIComponent(id.toString())}`, signal ? { signal } : undefined)
}

export function getAlertHistory(nodeID: string, limit = 100): Promise<AlertHistoryResponse> {
  return request<AlertHistoryResponse>(`/api/alerts/history?node_id=${encodeURIComponent(nodeID)}&limit=${limit}`)
}

export function resolveAlertHistory(id: number): Promise<AlertHistory> {
  return request<AlertHistory>(`/api/alerts/history/${encodeURIComponent(id.toString())}/resolve`, { method: 'PATCH' })
}

export function deleteAlertHistory(id: number): Promise<void> {
  return requestVoid(`/api/alerts/history/${encodeURIComponent(id.toString())}`, { method: 'DELETE' })
}

export function deleteAlertHistories(ids: number[]): Promise<{ deleted: number }> {
  return request<{ deleted: number }>('/api/alerts/history', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids })
  })
}

export function getK8sClusters(): Promise<K8sClustersResponse> {
  return request<K8sClustersResponse>('/api/k8s/clusters')
}

export function getApplicationServices(signal?: AbortSignal): Promise<ApplicationServiceSummary[]> {
  return request<ApplicationServiceSummary[]>('/api/services', signal ? { signal } : undefined)
}

export function getApplicationService(id: string, signal?: AbortSignal): Promise<ApplicationServiceDetail> {
  return request<ApplicationServiceDetail>(`/api/services/${encodeURIComponent(id)}`, signal ? { signal } : undefined)
}

export function createApplicationService(input: ApplicationServiceInput): Promise<ApplicationServiceDetail> {
  return request<ApplicationServiceDetail>('/api/services', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input)
  })
}

export function updateApplicationService(id: string, input: ApplicationServiceInput): Promise<ApplicationServiceDetail> {
  return request<ApplicationServiceDetail>(`/api/services/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input)
  })
}

export function deleteApplicationService(id: string): Promise<void> {
  return requestVoid(`/api/services/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function getAIProviders(signal?: AbortSignal): Promise<{ providers: AIProvider[] }> {
  return request('/api/ai/providers', signal ? { signal } : undefined)
}

export function createAIProvider(input: AIProviderInput): Promise<AIProvider> {
  return request('/api/ai/providers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input)
  })
}

export function updateAIProvider(id: string, input: AIProviderInput): Promise<AIProvider> {
  return request(`/api/ai/providers/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input)
  })
}

export function deleteAIProvider(id: string): Promise<void> {
  return requestVoid(`/api/ai/providers/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function testAIProvider(id: string, signal?: AbortSignal): Promise<AIProvider> {
  return request(`/api/ai/providers/${encodeURIComponent(id)}/test`, { method: 'POST', signal })
}

export function setDefaultAIProvider(id: string): Promise<AIProvider> {
  return request(`/api/ai/providers/${encodeURIComponent(id)}/default`, { method: 'POST' })
}

export function getAIConversations(limit = 50, signal?: AbortSignal): Promise<{ conversations: AIConversation[] }> {
  return request(`/api/ai/conversations?limit=${encodeURIComponent(String(limit))}`, signal ? { signal } : undefined)
}

export function createAIConversation(title = ''): Promise<AIConversation> {
  return request('/api/ai/conversations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title })
  })
}

export function renameAIConversation(id: string, title: string): Promise<AIConversation> {
  return request(`/api/ai/conversations/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title })
  })
}

export function deleteAIConversation(id: string): Promise<void> {
  return requestVoid(`/api/ai/conversations/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function getAIConversation(id: string, limit = 50, signal?: AbortSignal): Promise<AIConversationState> {
  return request(`/api/ai/conversations/${encodeURIComponent(id)}?limit=${encodeURIComponent(String(limit))}`, signal ? { signal } : undefined)
}

export function sendAIMessage(id: string, providerID: string, content: string, signal?: AbortSignal): Promise<AISendResult> {
  return request(`/api/ai/conversations/${encodeURIComponent(id)}/messages`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ provider_id: providerID, content }),
    signal
  })
}

export async function sendAIMessageStream(
  id: string, providerID: string, content: string,
  signal: AbortSignal | undefined,
  onProgress: (event: { phase: string; tool_name?: string; target_name?: string }) => void
): Promise<AISendResult> {
  const response = await fetch(`/api/ai/conversations/${encodeURIComponent(id)}/messages/stream`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ provider_id: providerID, content }),
    signal
  })
  if (!response.ok) throw new APIError(response.status, await errorMessage(response))
  if (!response.body) throw new APIError(0, '流式响应不可用')
  // Parse SSE: split blocks on blank lines, read `event:` and `data:` lines.
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let result: AISendResult | undefined
  let error: string | undefined
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const events = buffer.split('\n\n')
    buffer = events.pop() ?? ''
    for (const block of events) {
      const trimmed = block.trim()
      if (!trimmed) continue
      const lines = trimmed.split('\n')
      let eventType = ''
      let data = ''
      for (const line of lines) {
        if (line.startsWith('event: ')) eventType = line.slice(7)
        else if (line.startsWith('data: ')) data = line.slice(6)
      }
      if (eventType === 'status' && data) onProgress(JSON.parse(data))
      else if (eventType === 'result' && data) result = JSON.parse(data) as AISendResult
      else if (eventType === 'error' && data) error = (JSON.parse(data) as { error: string }).error
    }
  }
  if (error) throw new APIError(0, error)
  if (!result) throw new APIError(0, '流式响应未返回结果')
  return result
}

export function confirmAIToolCall(id: string): Promise<AISendResult> {
  return request(`/api/ai/tool-calls/${encodeURIComponent(id)}/confirm`, { method: 'POST' })
}

export function rejectAIToolCall(id: string): Promise<AISendResult> {
  return request(`/api/ai/tool-calls/${encodeURIComponent(id)}/reject`, { method: 'POST' })
}

export async function listAIProviderModels(baseURL: string, apiKey: string, providerID?: string): Promise<string[]> {
  const response = await request<{ models: string[] }>('/api/ai/providers/models', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ provider_id: providerID || '', base_url: baseURL, api_key: apiKey })
  })
  return response.models
}
