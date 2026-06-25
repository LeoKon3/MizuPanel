import type { AgentLogsResponse, AgentRestartResponse, AgentStatusResponse, AlertHistory, AlertHistoryResponse, AlertRule, AlertRulesResponse, AuthSessionResponse, DockerSnapshotResponse, FileDeleteResponse, FileListResponse, FileReadResponse, FileUploadResponse, FileWriteResponse, InstallCommandOptions, InstallCommandResponse, InstallPlatform, LoginResponse, MetricsResponse, NodesResponse, ProcessSnapshotResponse, RangeOption, RebootResponse, SettingsResponse, SettingsUpdate, SSHInstallRequest, SSHJobResponse, SSHUninstallRequest, K8sClustersResponse, SystemAboutResponse } from '../types'

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

export function getNodes(): Promise<NodesResponse> {
  return request<NodesResponse>('/api/nodes')
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

export function getNodeDocker(nodeID: string): Promise<DockerSnapshotResponse> {
  return request<DockerSnapshotResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/docker`)
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

export function restartAgent(nodeID: string): Promise<AgentRestartResponse> {
  return request<AgentRestartResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/agent/restart`, { method: 'POST' })
}

export function getAgentLogs(nodeID: string, lines = 100): Promise<AgentLogsResponse> {
  return request<AgentLogsResponse>(`/api/nodes/${encodeURIComponent(nodeID)}/agent/logs?lines=${encodeURIComponent(lines.toString())}`)
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
