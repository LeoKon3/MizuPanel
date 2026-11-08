import { type KeyboardEvent, type MouseEvent, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Plus, RefreshCw, Search, X } from 'lucide-react'

import { createInstallCommand, deleteNodePath, getAgentLogs, getAgentStatus, getAgentUpgradeStatus, getAuthSession, getConnectionDiagnostics, getNodeDocker, getNodeDockerCompose, getNodeDockerResources, getNodeFiles, getNodeMetrics, getNodeProcesses, getNodeSystemdServices, getNodes, getSettings, getSystemAbout, login, logout, readNodeFile, rebootNode, restartAgent, runNodeDockerComposeAction, runNodeDockerComposeDeployment, runNodeDockerResourceAction, runNodeSystemdServiceAction, setUnauthorizedHandler, startSSHUninstall, updateSettings, upgradeAgent, uploadNodeFile, writeNodeFile } from './api/client'
import { AppShell } from './components/AppShell'
import { NodeSwitcher } from './components/NodeSwitcher'
import { usePanelNavigation } from './lib/usePanelNavigation'
import { nodeDisplayStatus } from './lib/nodeStatus'
import { AIAssistantDrawer, AITopbarButton, AIWorkspacePage } from './components/ai/AIAssistant'
import { useAIAssistantState } from './components/ai/useAIAssistantState'
import { HostBatchTable } from './components/HostBatchTable'
import { NodeOrganizationControls, type HostGroupFilter, type HostTagFilter } from './components/NodeOrganizationControls'
import { currentRoute, nodePath, pageForRoute, type AppPage } from './routes'
import { AlertsPage } from './pages/AlertsPage'
import { HistoryPage } from './pages/HistoryPage'
import { NodeDetail } from './pages/NodeDetail'
import { OverviewPage } from './pages/OverviewPage'
import { SystemSettingsPage } from './pages/SystemSettingsPage'
import { TerminalPage } from './pages/TerminalPage'
import { K8sClustersPage } from './pages/K8sClustersPage'
import { K8sClusterDetailPage } from './pages/K8sClusterDetailPage'
import { UptimePage } from './pages/UptimePage'
import { AuditPage } from './pages/AuditPage'
import { TasksPage } from './pages/TasksPage'
import { ServicesPage } from './pages/ServicesPage'
import { LogsPage } from './pages/LogsPage'
import ConnectK8sClusterModal from './components/ConnectK8sClusterModal'
import type { AIRequestContext, DockerComposeAction, DockerComposeDeploymentRequest, DockerComposeDeploymentResponse, DockerComposeListResponse, DockerContainer, DockerResourceAction, DockerResourceListResponse, DockerResourceType, DockerSnapshotResponse, InstallPlatform, Metric, Node, NodeGroupSummary, NodeTagSummary, ProcessSnapshotResponse, RangeOption, SettingsResponse, SystemAboutResponse, SystemdServiceAction, SystemdServiceListResponse } from './types'

type ThemeMode = 'light' | 'dark'

type HostFilter = 'all' | 'online' | 'offline' | 'stale'

const rangeSeconds: Record<RangeOption, number> = {
  '1h': 3600,
  '6h': 21600,
  '24h': 86400,
  '3d': 259200,
  '7d': 604800
}

const orderedRanges: RangeOption[] = ['1h', '6h', '24h', '3d', '7d']

function largestAllowedRange(seconds: number): RangeOption {
  const allowed = orderedRanges.filter((option) => rangeSeconds[option] <= seconds)
  return allowed.length > 0 ? allowed[allowed.length - 1] : '1h'
}

function storedTheme(): ThemeMode {
  const value = window.localStorage.getItem('mizupanel-theme')
  return value === 'dark' ? 'dark' : 'light'
}

function storedSidebarCollapsed() {
  return window.localStorage.getItem('mizupanel-sidebar-collapsed') === 'true'
}

const installNodeDiscoveryIntervalMs = 3_000
const installNodeDiscoveryWindowMs = 2 * 60_000

function installDiscoveryNodesChanged(current: Node[], next: Node[]) {
  if (current.length !== next.length) return true
  const currentByID = new Map(current.map((node) => [node.id, node]))
  return next.some((node) => {
    const previous = currentByID.get(node.id)
    return !previous
      || previous.name !== node.name
      || previous.hostname !== node.hostname
      || previous.ip !== node.ip
      || previous.os !== node.os
      || previous.arch !== node.arch
      || previous.kernel !== node.kernel
      || previous.agent_version !== node.agent_version
      || previous.status !== node.status
      || previous.terminal_enabled !== node.terminal_enabled
      || previous.agent_mode !== node.agent_mode
      || previous.agent_user !== node.agent_user
      || previous.task_runner_supported !== node.task_runner_supported
  })
}

export default function App() {
  const [routeVersion, setRouteVersion] = useState(0)
  const route = useMemo(() => currentRoute(), [routeVersion])
  const [page, setPage] = useState<AppPage>(() => pageForRoute(route))
  const [theme, setTheme] = useState<ThemeMode>(() => storedTheme())
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => storedSidebarCollapsed())
  const aiAssistant = useAIAssistantState()
  const [authEnabled, setAuthEnabled] = useState(false)
  const [authenticated, setAuthenticated] = useState(false)
  const [currentUsername, setCurrentUsername] = useState('')
  const [loginUsername, setLoginUsername] = useState('admin')
  const [loginPassword, setLoginPassword] = useState('')
  const [loginError, setLoginError] = useState<string>()
  const [loginLoading, setLoginLoading] = useState(false)
  const [nodes, setNodes] = useState<Node[]>([])
  const [nodesError, setNodesError] = useState<string>()
  const [nodesUpdatedAt, setNodesUpdatedAt] = useState<number>()
  const [nodesLoading, setNodesLoading] = useState(false)
  const [historyNodeID, setSelectedNodeID] = useState<string>()
  const selectedNodeID = route.kind === 'node-detail' || route.kind === 'node-terminal' || route.kind === 'container-exec' ? route.nodeID : page === 'history' ? historyNodeID : undefined
  const [switcherOpen, setSwitcherOpen] = useState(false)
  const hostScrollTop = useRef(0)
  const [nodesRefreshing, setNodesRefreshing] = useState(false)
  const [snapshotNow, setSnapshotNow] = useState(Date.now)
  const [metrics, setMetrics] = useState<Metric[]>([])
  const [processSnapshot, setProcessSnapshot] = useState<ProcessSnapshotResponse>()
  const [dockerSnapshot, setDockerSnapshot] = useState<DockerSnapshotResponse>()
  const [dockerCompose, setDockerCompose] = useState<DockerComposeListResponse>()
  const [dockerResources, setDockerResources] = useState<DockerResourceListResponse>()
  const [systemdServices, setSystemdServices] = useState<SystemdServiceListResponse>()
  const [monitoringNodeID, setMonitoringNodeID] = useState<string>()
  const [monitoringLoading, setMonitoringLoading] = useState(false)
  const [range, setRange] = useState<RangeOption>('1h')
  const [error, setError] = useState<string>()
  const [search, setSearch] = useState('')
  const [hostFilter, setHostFilter] = useState<HostFilter>('all')
  const [hostGroupFilter, setHostGroupFilter] = useState<HostGroupFilter>('all')
  const [hostTagFilter, setHostTagFilter] = useState<HostTagFilter>('all')
  const [installPlatform, setInstallPlatform] = useState<InstallPlatform>('linux')
  const [installCommand, setInstallCommand] = useState<string>()
  const [installCommandWarning, setInstallCommandWarning] = useState<string>()
  const [installCommandError, setInstallCommandError] = useState<string>()
  const [installCommandCopied, setInstallCommandCopied] = useState(false)
  const [installToken, setInstallToken] = useState<string>()
  const [installCommandLoading, setInstallCommandLoading] = useState(false)
  const [installCommandOpen, setInstallCommandOpen] = useState(false)
  const [settings, setSettings] = useState<SettingsResponse>()
  const [systemAbout, setSystemAbout] = useState<SystemAboutResponse>()
  const [settingsRetention, setSettingsRetention] = useState<RangeOption>('6h')
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [settingsMessage, setSettingsMessage] = useState<string>()
  const [settingsError, setSettingsError] = useState<string>()
  const [loading, setLoading] = useState(true)
  const [connectK8sClusterModalOpen, setConnectK8sClusterModalOpen] = useState(false)
  const [k8sClustersRefreshKey, setK8sClustersRefreshKey] = useState(0)
  const addHostButtonRef = useRef<HTMLButtonElement>(null)
  const installTriggerRef = useRef<HTMLElement | null>(null)
  const nodesRequestID = useRef(0)
  const nodesController = useRef<AbortController | null>(null)
  const installCommandCodeRef = useRef<HTMLElement>(null)
  const installCommandDialogRef = useRef<HTMLElement>(null)
  const installCommandRequestID = useRef(0)
  const dockerSnapshotRequestID = useRef(0)
  const dockerComposeRequestID = useRef(0)
  const dockerResourcesRequestID = useRef(0)
  const systemdRequestID = useRef(0)
  const nodesRef = useRef<Node[]>([])
  const selectedNodeIDRef = useRef<string | undefined>(undefined)
  nodesRef.current = nodes
  selectedNodeIDRef.current = selectedNodeID

  const aiRequestContext = useMemo<AIRequestContext>(() => {
    if (route.kind === 'service-detail') return { page, resource_type: 'application_service', resource_id: route.serviceID }
    if (route.kind === 'k8s-cluster-detail') return { page, resource_type: 'k8s_cluster', resource_id: route.clusterID }
    if (route.kind === 'node-detail' || route.kind === 'node-terminal' || route.kind === 'container-exec') {
      return { page, resource_type: 'node', resource_id: route.nodeID }
    }
    if ((page === 'hosts' || page === 'overview') && selectedNodeID) {
      return { page, resource_type: 'node', resource_id: selectedNodeID }
    }
    return { page }
  }, [page, route, selectedNodeID])

  useEffect(() => {
    aiAssistant.setContext(aiRequestContext)
  }, [aiAssistant.setContext, aiRequestContext])

  useEffect(() => {
    const dark = theme === 'dark'
    document.documentElement.classList.toggle('dark', dark)
    document.documentElement.dataset.theme = theme
    window.localStorage.setItem('mizupanel-theme', theme)
  }, [theme])

  useEffect(() => {
    window.localStorage.setItem('mizupanel-sidebar-collapsed', sidebarCollapsed ? 'true' : 'false')
  }, [sidebarCollapsed])

  useEffect(() => {
    if (page !== 'ai' || (authEnabled && !authenticated)) return
    void aiAssistant.ensureLoaded()
  }, [aiAssistant.ensureLoaded, authEnabled, authenticated, page])

  const { navigate: navigateWithinPanel, registerGuard } = usePanelNavigation(() => {
    setPage(pageForRoute(currentRoute()))
    setRouteVersion(value => value + 1)
    setError(undefined)
  })

  useEffect(() => {
    setUnauthorizedHandler(() => {
      nodesRequestID.current += 1
      nodesController.current?.abort()
      nodesController.current = null
      setNodesLoading(false)
      setAuthenticated(false)
      setCurrentUsername('')
      setError('登录已过期，请重新登录')
    })
  }, [])

  const loadNodes = useCallback(async (signal?: AbortSignal) => {
    const requestID = ++nodesRequestID.current
    nodesController.current?.abort()
    const controller = new AbortController()
    nodesController.current = controller
    const cancel = () => controller.abort()
    signal?.addEventListener('abort', cancel, { once: true })
    if (signal?.aborted) controller.abort()
    setNodesLoading(true)
    let timeout: ReturnType<typeof setTimeout> | undefined
    let aborted: (() => void) | undefined
    try {
      const deadline = new Promise<never>((_, reject) => {
        aborted = () => reject(new DOMException('请求已取消', 'AbortError'))
        controller.signal.addEventListener('abort', aborted, { once: true })
        if (controller.signal.aborted) aborted()
        timeout = setTimeout(() => { reject(new Error('主机读取超时')); controller.abort() }, 15_000)
      })
      const response = await Promise.race([getNodes(controller.signal), deadline])
      if (signal?.aborted || requestID !== nodesRequestID.current) return
      setNodes(response.nodes)
      setNodesError(undefined)
      setNodesUpdatedAt(Date.now())
      setSelectedNodeID(current => current && response.nodes.some(node => node.id === current) ? current : response.nodes[0]?.id)
    } catch (error) {
      if (signal?.aborted || requestID !== nodesRequestID.current) return
      setNodesError(error instanceof Error ? error.message : '主机读取失败')
      throw error
    } finally {
      clearTimeout(timeout)
      signal?.removeEventListener('abort', cancel)
      if (aborted) controller.signal.removeEventListener('abort', aborted)
      if (requestID === nodesRequestID.current) {
        nodesController.current = null
        setNodesLoading(false)
      }
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    getAuthSession()
      .then((response) => {
        if (cancelled) return
        setAuthEnabled(response.auth_enabled)
        setAuthenticated(response.authenticated)
        setCurrentUsername(response.username)
        if (!response.auth_enabled || response.authenticated) {
          const pending = loadNodes()
          if (pageForRoute(currentRoute()) === 'overview') {
            // The overview's independent sources should not wait for hosts.
            void pending.catch(() => undefined)
          } else return pending
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : '认证会话检查失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
      nodesRequestID.current += 1
      nodesController.current?.abort()
      nodesController.current = null
    }
  }, [loadNodes])

  useEffect(() => {
    if (loading || (authEnabled && !authenticated) || installCommandOpen || (page !== 'hosts' && page !== 'overview')) return
    let active = true
    let timer: ReturnType<typeof setTimeout>
    let controller: AbortController | undefined
    const refresh = async () => {
      if (nodesController.current) { timer = setTimeout(refresh, 5000); return }
      controller = new AbortController()
      try { await loadNodes(controller.signal) }
      catch { /* Retain the last snapshot; its age remains visible. */ }
      if (active) setSnapshotNow(Date.now())
      if (active) timer = setTimeout(refresh, 5000)
    }
    timer = setTimeout(refresh, 5000)
    return () => { active = false; clearTimeout(timer); controller?.abort() }
  }, [loading, authEnabled, authenticated, page, installCommandOpen, loadNodes])

  useEffect(() => {
    if (page !== 'history' && page !== 'settings') return
    let cancelled = false
    getSettings()
      .then((response) => {
        if (!cancelled) {
          setSettings(response)
          setSettingsRetention(response.metrics_retention)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setSettingsError(err instanceof Error ? err.message : '系统设置加载失败')
      })
    return () => {
      cancelled = true
    }
  }, [page])

  useEffect(() => {
    if (page !== 'settings') return
    let cancelled = false
    getSystemAbout()
      .then((response) => {
        if (!cancelled) setSystemAbout(response)
      })
      .catch((err: unknown) => {
        if (!cancelled) setSettingsError(err instanceof Error ? err.message : '系统信息加载失败')
      })
    return () => {
      cancelled = true
    }
  }, [page])

  useEffect(() => {
    if (!settings || rangeSeconds[range] <= settings.metrics_retention_seconds) return
    setRange(largestAllowedRange(settings.metrics_retention_seconds))
  }, [range, settings])

  useEffect(() => {
    if (loading || (authEnabled && !authenticated) || !selectedNodeID) {
      setMetrics([])
      return
    }
    let cancelled = false
    let timer: ReturnType<typeof setTimeout>
    setMetrics([])
    const refresh = async () => {
      try {
        const response = await getNodeMetrics(selectedNodeID, range)
        if (!cancelled) setMetrics(response.metrics)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : '指标加载失败')
      }
      if (!cancelled) timer = setTimeout(refresh, 15000)
    }
    void refresh()
    return () => { cancelled = true; clearTimeout(timer) }
  }, [selectedNodeID, range, loading, authEnabled, authenticated])

  const refreshDockerSnapshot = async (nodeID: string) => {
    const requestID = ++dockerSnapshotRequestID.current
    try {
      const docker = await getNodeDocker(nodeID)
      if (requestID !== dockerSnapshotRequestID.current || selectedNodeIDRef.current !== nodeID) return
      setDockerSnapshot(docker)
    } catch (err: unknown) {
      if (requestID !== dockerSnapshotRequestID.current || selectedNodeIDRef.current !== nodeID) return
      setError(err instanceof Error ? err.message : 'Docker 快照刷新失败')
    }
  }

  const refreshDockerCompose = async (nodeID: string) => {
    const requestID = ++dockerComposeRequestID.current
    try {
      const response = await getNodeDockerCompose(nodeID)
      if (requestID !== dockerComposeRequestID.current || selectedNodeIDRef.current !== nodeID) return
      setDockerCompose(response)
    } catch (err: unknown) {
      if (requestID !== dockerComposeRequestID.current || selectedNodeIDRef.current !== nodeID) return
      setDockerCompose({
        success: false,
        supported: false,
        projects: [],
        error: err instanceof Error ? err.message : 'Compose 项目刷新失败'
      })
    }
  }

  const runDockerComposeAction = async (nodeID: string, projectName: string, action: DockerComposeAction, serviceName?: string) => {
    const response = await runNodeDockerComposeAction(nodeID, projectName, action, serviceName)
    if (!response.success) throw new Error(response.error || 'Compose 操作失败')
    return response
  }

  const runDockerComposeDeployment = (nodeID: string, request: DockerComposeDeploymentRequest): Promise<DockerComposeDeploymentResponse> => {
    return runNodeDockerComposeDeployment(nodeID, request)
  }

  const refreshDockerResources = async (nodeID: string) => {
    const requestID = ++dockerResourcesRequestID.current
    try {
      const response = await getNodeDockerResources(nodeID)
      if (requestID !== dockerResourcesRequestID.current || selectedNodeIDRef.current !== nodeID) return
      setDockerResources({ ...response, node_id: nodeID })
    } catch (err: unknown) {
      if (requestID !== dockerResourcesRequestID.current || selectedNodeIDRef.current !== nodeID) return
      setDockerResources({
        node_id: nodeID,
        success: false,
        supported: true,
        usage: {},
        images: [],
        volumes: [],
        networks: [],
        error: err instanceof Error ? err.message : 'Docker 资源刷新失败'
      })
    }
  }

  const runDockerResourceAction = async (nodeID: string, resourceType: DockerResourceType, resourceID: string, action: DockerResourceAction) => {
    const response = await runNodeDockerResourceAction(nodeID, resourceType, resourceID, action)
    if (!response.success) throw new Error(response.error || 'Docker 资源操作失败')
    return response
  }

  const refreshSystemdServices = async (nodeID: string) => {
    const requestID = ++systemdRequestID.current
    try {
      const response = await getNodeSystemdServices(nodeID)
      if (requestID !== systemdRequestID.current || selectedNodeIDRef.current !== nodeID) return
      setSystemdServices(response)
    } catch (err: unknown) {
      if (requestID !== systemdRequestID.current || selectedNodeIDRef.current !== nodeID) return
      setError(err instanceof Error ? err.message : '系统服务刷新失败')
    }
  }

  const runSystemdServiceAction = async (nodeID: string, serviceName: string, action: SystemdServiceAction) => {
    const response = await runNodeSystemdServiceAction(nodeID, serviceName, action)
    if (!response.success) throw new Error(response.error || '系统服务操作失败')
    return response
  }

  useLayoutEffect(() => {
    // Reset before the new detail's passive effects start Compose/Systemd reads.
    // A parent passive reset would invalidate those freshly started requests.
    setMonitoringNodeID(selectedNodeID)
    dockerSnapshotRequestID.current += 1
    dockerComposeRequestID.current += 1
    dockerResourcesRequestID.current += 1
    systemdRequestID.current += 1
    setProcessSnapshot(undefined)
    setDockerSnapshot(undefined)
    setDockerCompose(undefined)
    setDockerResources(undefined)
    setSystemdServices(undefined)
    setMonitoringLoading(!loading && (!authEnabled || authenticated) && Boolean(selectedNodeID))
  }, [selectedNodeID, loading, authEnabled, authenticated])

  useEffect(() => {
    if (loading || (authEnabled && !authenticated) || !selectedNodeID) return
    let cancelled = false
    Promise.all([getNodeProcesses(selectedNodeID), getNodeDocker(selectedNodeID)])
      .then(([processes, docker]) => {
        if (!cancelled) {
          setProcessSnapshot(processes)
          setDockerSnapshot(docker)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : '监控快照加载失败')
      })
      .finally(() => {
        if (!cancelled) setMonitoringLoading(false)
      })
      .catch(() => {
        if (!cancelled) setDockerCompose({ success: false, supported: false, projects: [], error: '当前 Agent 不支持 Compose 管理' })
      })
    return () => {
      cancelled = true
    }
  }, [selectedNodeID, loading, authEnabled, authenticated])

  const filteredNodes = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return nodes.filter((node) => {
      if (hostFilter !== 'all' && nodeDisplayStatus(node, snapshotNow) !== hostFilter) return false
      if (hostGroupFilter === 'ungrouped' && node.group) return false
      if (hostGroupFilter !== 'all' && hostGroupFilter !== 'ungrouped' && node.group?.id !== hostGroupFilter) return false
      if (hostTagFilter !== 'all' && !(node.tags ?? []).some((tag) => tag.id === hostTagFilter)) return false
      if (!keyword) return true
      return [node.name, node.hostname, node.ip, node.os, node.arch, node.group?.name ?? '', ...(node.tags ?? []).map((tag) => tag.name)].some((value) => value.toLowerCase().includes(keyword))
    }).sort((a, b) => (a.name || a.hostname).localeCompare(b.name || b.hostname, 'zh-CN', { numeric: true }) || a.id.localeCompare(b.id))
  }, [hostFilter, hostGroupFilter, hostTagFilter, nodes, search, snapshotNow])
  const hostGroups = useMemo(() => uniqueNodeGroups(nodes), [nodes])
  const hostTags = useMemo(() => uniqueNodeTags(nodes), [nodes])
  const selectedMetrics = useMemo(() => selectedNodeID ? metrics.filter((metric) => metric.node_id === selectedNodeID) : [], [metrics, selectedNodeID])
  const selectedProcessSnapshot = processSnapshot?.node_id === selectedNodeID ? processSnapshot : undefined
  const selectedDockerSnapshot = dockerSnapshot?.node_id === selectedNodeID ? dockerSnapshot : undefined
  const selectedDockerCompose = monitoringNodeID === selectedNodeID ? dockerCompose : undefined
  const selectedDockerResources = dockerResources?.node_id === selectedNodeID ? dockerResources : undefined
  const selectedSystemdServices = monitoringNodeID === selectedNodeID ? systemdServices : undefined
  const routeNode = useMemo(() => route.kind === 'node-detail' || route.kind === 'node-terminal' || route.kind === 'container-exec' ? nodes.find((node) => node.id === route.nodeID) : undefined, [nodes, route])
  const routeContainer = useMemo<DockerContainer | undefined>(() => {
    if (route.kind !== 'container-exec') return undefined
    return selectedDockerSnapshot?.containers.find((container) => (container.full_id || container.id) === route.containerID || container.id === route.containerID)
  }, [route, selectedDockerSnapshot])

  useEffect(() => {
    if (!installCommandOpen) return

    nodesRequestID.current += 1
    nodesController.current?.abort()
    nodesController.current = null
    setNodesLoading(false)
    let active = true
    let timeoutID: number | undefined
    let activeRequest: AbortController | undefined
    const expiresAt = Date.now() + installNodeDiscoveryWindowMs

    const scheduleRefresh = () => {
      if (!active || Date.now() + installNodeDiscoveryIntervalMs >= expiresAt) return
      timeoutID = window.setTimeout(() => {
        void refreshNodes()
      }, installNodeDiscoveryIntervalMs)
    }

    const refreshNodes = async () => {
      if (!active || Date.now() >= expiresAt) return
      const controller = new AbortController()
      activeRequest = controller
      try {
        const requestID = ++nodesRequestID.current
        const response = await getNodes(controller.signal)
        if (!active || Date.now() >= expiresAt || requestID !== nodesRequestID.current) return
        setNodesError(undefined)
        setNodesUpdatedAt(Date.now())
        if (installDiscoveryNodesChanged(nodesRef.current, response.nodes)) {
          nodesRef.current = response.nodes
          setNodes(response.nodes)
        }
      } catch {
        // Installation discovery is best-effort and must not replace visible errors.
      } finally {
        if (activeRequest === controller) activeRequest = undefined
      }
      scheduleRefresh()
    }

    scheduleRefresh()
    return () => {
      active = false
      if (timeoutID !== undefined) window.clearTimeout(timeoutID)
      activeRequest?.abort()
    }
  }, [installCommandOpen])

  useEffect(() => {
    if (installCommandOpen) {
      installCommandDialogRef.current?.focus()
    }
  }, [installCommandOpen])

  const requestInstallCommand = (platform: InstallPlatform) => {
    const requestID = installCommandRequestID.current + 1
    installCommandRequestID.current = requestID
    setInstallCommand(undefined)
    setInstallToken(undefined)
    setInstallCommandWarning(undefined)
    setInstallCommandError(undefined)
    setInstallCommandCopied(false)
    setInstallCommandLoading(true)
    return createInstallCommand(platform)
      .then((response) => {
        if (requestID === installCommandRequestID.current) {
          setInstallCommand(response.command)
          setInstallToken(response.install_token)
        }
      })
      .catch((err: unknown) => {
        if (requestID === installCommandRequestID.current) {
          setInstallCommandError(err instanceof Error ? err.message : '安装命令生成失败')
        }
      })
      .finally(() => {
        if (requestID === installCommandRequestID.current) {
          setInstallCommandLoading(false)
        }
      })
  }

  const selectInstallCommand = () => {
    const code = installCommandCodeRef.current
    if (!code) return false
    const range = document.createRange()
    range.selectNodeContents(code)
    const selection = window.getSelection()
    selection?.removeAllRanges()
    selection?.addRange(range)
    return true
  }

  const copyInstallCommand = () => {
    if (!installCommand) return
    Promise.resolve()
      .then(() => navigator.clipboard.writeText(installCommand))
      .catch(() => {
        if (!selectInstallCommand()) return false
        return typeof document.execCommand === 'function' && document.execCommand('copy')
      })
      .then((copied) => {
        if (copied === false) {
          setInstallCommandCopied(false)
          setInstallCommandWarning('复制失败，已为你选中命令，请按 Ctrl+C 手动复制。')
          return
        }
        setInstallCommandWarning(undefined)
        setInstallCommandCopied(true)
      })
      .catch(() => {
        selectInstallCommand()
        setInstallCommandCopied(false)
        setInstallCommandWarning('复制失败，已为你选中命令，请按 Ctrl+C 手动复制。')
      })
  }

  const closeInstallCommand = () => {
    installCommandRequestID.current += 1
    setInstallCommand(undefined)
    setInstallToken(undefined)
    setInstallCommandWarning(undefined)
    setInstallCommandError(undefined)
    setInstallCommandCopied(false)
    setInstallCommandLoading(false)
    setInstallCommandOpen(false)
    if (installTriggerRef.current?.isConnected) installTriggerRef.current.focus()
    else addHostButtonRef.current?.focus()
  }

  const handleInstallCommandKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === 'Escape') {
      closeInstallCommand()
      return
    }
    if (event.key !== 'Tab') return
    const dialog = installCommandDialogRef.current
    if (!dialog) return
    const focusable = Array.from(dialog.querySelectorAll<HTMLElement>('button, input, select, textarea, a[href], [tabindex]:not([tabindex="-1"])'))
      .filter((element) => !element.hasAttribute('disabled') && element.getAttribute('aria-hidden') !== 'true')
    if (focusable.length === 0) {
      event.preventDefault()
      dialog.focus()
      return
    }
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog)) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog)) {
      event.preventDefault()
      first.focus()
    }
  }

  const showInstallCommand = (event?: MouseEvent<HTMLButtonElement>) => {
    installTriggerRef.current = event?.currentTarget ?? (document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : addHostButtonRef.current)
    setInstallCommandOpen(true)
    setInstallPlatform('linux')
    requestInstallCommand('linux')
  }

  const selectInstallPlatform = (platform: InstallPlatform) => {
    if (platform === installPlatform) return
    setInstallPlatform(platform)
    requestInstallCommand(platform)
  }

  const getLegacyAgentUpgradeCommand = () => createInstallCommand('linux').then((response) => response.command)

  const openPage = (nextPage: AppPage) => {
    const paths: Record<AppPage, string> = { hosts: '/nodes', overview: '/overview', history: '/history', ai: '/ai', services: '/services', settings: '/settings', alerts: '/alerts', uptime: '/uptime', audit: '/audit', tasks: '/tasks', logs: '/logs', k8s: '/k8s/clusters' }
    navigateWithinPanel(paths[nextPage])
  }
  const openService = (id: string) => navigateWithinPanel(`/services/${encodeURIComponent(id)}`)
  const backToServices = () => navigateWithinPanel('/services')
  const openNode = (node: Node, preserveSection = false) => {
    const params = new URLSearchParams(window.location.search)
    const section = params.get('section')
    const query = preserveSection && section ? `?section=${encodeURIComponent(section)}` : ''
    setSwitcherOpen(false)
    if (range !== '1h' && range !== '6h') setRange('1h')
    navigateWithinPanel(`${nodePath(node.id)}${query}`)
  }
  const refreshHostList = async () => {
    if (nodesRefreshing) return
    setNodesRefreshing(true)
    try { await loadNodes(); setError(undefined) }
    catch (err) { setError(err instanceof Error ? err.message : '主机列表刷新失败') }
    finally { setNodesRefreshing(false) }
  }

  const openAIWorkspace = () => {
    aiAssistant.closeDrawer()
    openPage('ai')
    void aiAssistant.ensureLoaded()
  }

  const openAISettings = () => {
    aiAssistant.closeDrawer()
    openPage('settings')
  }

  const openK8sClusterDetail = (id: string) => navigateWithinPanel(`/k8s/clusters/${encodeURIComponent(id)}`)
  const backToK8sClusters = () => navigateWithinPanel('/k8s/clusters')

  const saveSettings = () => {
    setSettingsSaving(true)
    setSettingsMessage(undefined)
    setSettingsError(undefined)
    updateSettings({ metrics_retention: settingsRetention })
      .then((response) => {
        setSettings(response)
        setSettingsRetention(response.metrics_retention)
        setSettingsMessage('设置已保存，新的保留时间会立即用于历史查询和后续清理。')
      })
      .catch((err: unknown) => setSettingsError(err instanceof Error ? err.message : '系统设置保存失败'))
      .finally(() => setSettingsSaving(false))
  }

  const handleLogin = () => {
    setLoginLoading(true)
    setLoginError(undefined)
    login(loginUsername, loginPassword)
      .then((response) => {
        setAuthenticated(response.authenticated)
        setCurrentUsername(response.username)
        setLoginPassword('')
        return loadNodes()
      })
      .catch((err: unknown) => setLoginError(err instanceof Error ? err.message : '登录失败'))
      .finally(() => setLoginLoading(false))
  }

  const handleLogout = () => {
    nodesRequestID.current += 1
    nodesController.current?.abort()
    nodesController.current = null
    setNodesLoading(false)
    logout()
      .then(() => {
        setAuthenticated(false)
        setCurrentUsername('')
        setNodes([])
        setNodesError(undefined)
        setNodesUpdatedAt(undefined)
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : '退出登录失败'))
  }

  if (loading) {
    return (
      <main className="soft-page flex min-h-screen items-center justify-center px-4 text-foreground">
        <div className="soft-panel px-6 py-5 text-sm font-black text-muted-foreground">正在加载节点...</div>
      </main>
    )
  }

  if (authEnabled && !authenticated) {
    return (
      <main className="soft-page flex min-h-screen items-center justify-center px-4 text-foreground">
        <div
          role="dialog"
          aria-modal="true"
          aria-label="登录 MizuPanel"
          className="soft-modal-shell w-full max-w-md p-6"
        >
          <h1 className="text-2xl font-black text-foreground">登录 MizuPanel</h1>
          <p className="mt-2 text-sm font-semibold text-muted-foreground">请使用管理员账号登录以继续。</p>
          <div className="mt-6 space-y-4">
            <label className="block text-sm font-black text-foreground">
              用户名
              <input
                aria-label="用户名"
                type="text"
                value={loginUsername}
                onChange={(event) => setLoginUsername(event.target.value)}
                className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold"
              />
            </label>
            <label className="block text-sm font-black text-foreground">
              密码
              <input
                aria-label="密码"
                type="password"
                value={loginPassword}
                onChange={(event) => setLoginPassword(event.target.value)}
                onKeyDown={(event) => event.key === 'Enter' && handleLogin()}
                className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold"
              />
            </label>
            {error ? (
              <div className="rounded-2xl border border-warning/30 bg-warning/10 px-3 py-2 text-sm font-black text-warning">
                {error}
              </div>
            ) : null}
            {loginError ? (
              <div className="rounded-2xl border border-danger/30 bg-danger/10 px-3 py-2 text-sm font-black text-danger">
                {loginError}
              </div>
            ) : null}
            <button
              type="button"
              onClick={handleLogin}
              disabled={loginLoading}
              className="soft-button min-h-11 w-full cursor-pointer bg-primary px-4 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {loginLoading ? '登录中...' : '登录'}
            </button>
          </div>
        </div>
      </main>
    )
  }

  if (route.kind === 'node-terminal') {
    return <TerminalPage kind="node" nodeID={route.nodeID} node={routeNode} />
  }

  if (route.kind === 'container-exec') {
    return <TerminalPage kind="container" nodeID={route.nodeID} node={routeNode} containerID={route.containerID} container={routeContainer} />
  }

  const installCommandDialog = installCommandOpen ? (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-6">
      <section
        id="agent-install-command"
        ref={installCommandDialogRef}
        role="dialog"
        aria-modal="true"
        aria-label="添加主机"
        aria-live="polite"
        tabIndex={-1}
        onKeyDown={handleInstallCommandKeyDown}
        className="soft-modal-shell flex max-h-[90vh] w-full max-w-4xl flex-col text-left outline-none"
      >
      <div className="soft-modal-header flex shrink-0 items-start justify-between gap-3 border-b px-4 py-3">
        <div className="min-w-0">
          <p className="text-sm font-black text-foreground">添加主机</p>
          <p className="mt-1 text-xs font-semibold text-muted-foreground">复制安装命令到目标机器执行，Agent 会自动生成节点身份并连接到 MizuPanel。</p>
        </div>
        <button
          type="button"
          aria-label="关闭"
          onClick={closeInstallCommand}
          className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20"
        >
          <X size={16} aria-hidden="true" />
        </button>
      </div>
      <div className="overflow-y-auto px-4 py-3">
              <div className="soft-card p-3">
                <p className="text-xs font-black uppercase tracking-[0.18em] text-success">简化状态</p>
                <ol className="mt-3 space-y-2">
                  <li className="flex items-start gap-3 rounded-2xl bg-surface/70 px-3 py-2">
                    <span className="mt-0.5 h-3 w-3 rounded-full bg-success" />
                    <span className="min-w-0">
                      <span className="block text-xs font-black text-foreground">已生成短期引导 install_token</span>
                      <span className="block text-xs font-bold text-muted-foreground">{installToken || '等待生成'}</span>
                    </span>
                  </li>
                  <li className="flex items-start gap-3 rounded-2xl bg-surface/70 px-3 py-2">
                    <span className="mt-0.5 h-3 w-3 rounded-full bg-info" />
                    <span className="min-w-0">
                      <span className="block text-xs font-black text-foreground">等待在目标机器执行命令</span>
                      <span className="block text-xs font-bold text-muted-foreground">复制命令到目标机器后执行即可。</span>
                    </span>
                  </li>
                  <li className="flex items-start gap-3 rounded-2xl bg-surface/70 px-3 py-2">
                    <span className="mt-0.5 h-3 w-3 rounded-full bg-muted-foreground" />
                    <span className="min-w-0">
                      <span className="block text-xs font-black text-foreground">等待 Agent 首次注册</span>
                      <span className="block text-xs font-bold text-muted-foreground">安装完成后，Agent 会自动连接到 MizuPanel。</span>
                    </span>
                  </li>
                  <li className="flex items-start gap-3 rounded-2xl bg-surface/70 px-3 py-2">
                    <span className="mt-0.5 h-3 w-3 rounded-full bg-code" />
                    <span className="min-w-0">
                      <span className="block text-xs font-black text-foreground">Agent 已连接，安装成功</span>
                      <span className="block text-xs font-bold text-muted-foreground">上线后就可以在主机看到节点。</span>
                    </span>
                  </li>
                </ol>
                <p className="mt-3 rounded-2xl border border-warning/30 bg-warning/10 px-3 py-2 text-xs font-bold leading-5 text-warning">超时未连接时，请检查 server_url、防火墙或 Agent 日志。</p>
              </div>

              <div className="soft-toolbar mt-3 flex w-fit p-1" aria-label="选择 Agent 安装系统">
                {(['linux', 'windows'] as const).map((platform) => (
                  <button
                    key={platform}
                    type="button"
                    aria-pressed={installPlatform === platform}
                    onClick={() => selectInstallPlatform(platform)}
                    className={`soft-button min-h-9 cursor-pointer px-4 text-xs font-black focus:outline-none focus:ring-4 focus:ring-primary/20 ${installPlatform === platform ? 'bg-code text-primary-foreground shadow-sm' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
                  >
                    {platform === 'linux' ? 'Linux' : 'Windows'}
                  </button>
                ))}
              </div>
              <div className="soft-card mt-3 px-3 py-2">
                {installPlatform === 'linux' ? (
                  <p className="text-xs font-bold leading-5 text-success">默认以 root 运维模式安装，自动启用节点终端与 Docker 容器监控。</p>
                ) : (
                  <p className="text-xs font-bold leading-5 text-muted-foreground">Windows 暂不支持 Docker 监控和节点终端安装配置。</p>
                )}
              </div>

              <div className="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  aria-label={installCommandCopied ? '已复制' : '复制安装命令'}
                  onClick={copyInstallCommand}
                  disabled={!installCommand}
                  className="soft-button min-h-10 cursor-pointer bg-success px-4 text-xs font-black text-primary-foreground shadow-sm hover:brightness-95 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50 disabled:shadow-none"
                >
                  {installCommandCopied ? '已复制' : '复制'}
                </button>
                <button
                  type="button"
                  aria-label="关闭安装命令"
                  onClick={closeInstallCommand}
                  className="soft-button min-h-10 cursor-pointer border border-border bg-card px-4 text-xs font-black text-muted-foreground hover:border-success/50 hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20"
                >
                  关闭
                </button>
              </div>

              {installCommandLoading ? (
                <div className="bg-code px-4 py-4 text-xs font-bold leading-6 text-code-foreground">正在生成安装命令...</div>
              ) : installCommand ? (
                <pre className="overflow-x-auto bg-code px-4 py-4 text-xs leading-6 text-code-foreground"><code ref={installCommandCodeRef}>{installCommand}</code></pre>
              ) : (
                <div className="border-t border-danger/30 bg-danger/10 px-4 py-4 text-xs font-bold leading-5 text-danger">{installCommandError || '安装命令暂不可用，请重试。'}</div>
              )}
              {installCommandWarning ? (
                <div className="border-t border-warning/30 bg-warning/10 px-4 py-3 text-xs font-bold leading-5 text-warning">
                  {installCommandWarning}
                </div>
              ) : null}
              {installPlatform === 'windows' ? (
                <div className="border-t border-sky-200 bg-sky-50 px-4 py-3 text-xs font-bold leading-5 text-sky-700">
                  Windows 命令需要在管理员 PowerShell 中执行。
                </div>
              ) : null}
              <div className="border-t border-border bg-amber-50 px-4 py-3 text-xs font-bold leading-5 text-warning">
                token 来源：点击添加主机时，Server 会自动生成短期引导 install_token。
              </div>
      </div>
      </section>
    </div>
  ) : null

  const hostContent = (
    <section data-testid="host-page-container" className="console-host-page">
      {route.kind === 'node-detail' ? routeNode ? (
        <NodeDetail key={routeNode.id} node={routeNode} navigationVersion={routeVersion} onRegisterLeaveGuard={registerGuard} onBack={() => openPage('hosts')} onSwitchNode={() => setSwitcherOpen(true)} metrics={selectedMetrics} processSnapshot={selectedProcessSnapshot} dockerSnapshot={selectedDockerSnapshot} dockerCompose={selectedDockerCompose} dockerResources={selectedDockerResources} systemdServices={selectedSystemdServices} monitoringLoading={monitoringLoading} range={range} onRangeChange={setRange} onLoadFiles={getNodeFiles} onReadFile={readNodeFile} onWriteFile={writeNodeFile} onUploadFile={uploadNodeFile} onDeletePath={deleteNodePath} onRebootNode={rebootNode} onSSHUninstall={startSSHUninstall} onGetAgentStatus={getAgentStatus} onGetConnectionDiagnostics={getConnectionDiagnostics} onUpgradeAgent={upgradeAgent} onGetAgentUpgradeStatus={getAgentUpgradeStatus} onGetLegacyAgentUpgradeCommand={getLegacyAgentUpgradeCommand} onRestartAgent={restartAgent} onGetAgentLogs={getAgentLogs} onRefreshDocker={refreshDockerSnapshot} onRefreshDockerCompose={refreshDockerCompose} onDockerComposeAction={runDockerComposeAction} onDockerComposeDeployment={runDockerComposeDeployment} onRefreshDockerResources={refreshDockerResources} onDockerResourceAction={runDockerResourceAction} onRefreshSystemdServices={refreshSystemdServices} onSystemdServiceAction={runSystemdServiceAction} onNodeOrganizationChanged={loadNodes} />
      ) : <div className="console-empty"><h1>未找到主机</h1><p>主机可能已被删除，或当前账号无法访问。</p><button className="console-button" onClick={() => openPage('hosts')}>返回主机列表</button></div> : <>
        <div className="console-page-title"><h1>主机 <span>{nodes.length}</span></h1><button ref={addHostButtonRef} type="button" className="console-button console-primary" onClick={showInstallCommand} aria-label="添加主机" aria-expanded={installCommandOpen} aria-controls="agent-install-command"><Plus size={16} />添加主机</button></div>
        <div className="console-status-tabs" role="toolbar" aria-label="主机筛选与操作">{(['all', 'online', 'offline', 'stale'] as const).map(status => <button type="button" key={status} aria-pressed={hostFilter === status} onClick={() => setHostFilter(status)}>{{ all: '全部', online: '在线', offline: '离线', stale: '数据过期' }[status]} {status === 'all' ? nodes.length : nodes.filter(node => nodeDisplayStatus(node, snapshotNow) === status).length}</button>)}</div>
        <div className="console-host-toolbar"><label className="console-search"><Search size={15} /><input aria-label="搜索主机" placeholder="搜索主机..." value={search} onChange={event => setSearch(event.target.value)} /></label>
          <NodeOrganizationControls compact view="browse" onViewChange={() => {}} groupFilter={hostGroupFilter} onGroupFilterChange={setHostGroupFilter} tagFilter={hostTagFilter} onTagFilterChange={setHostTagFilter} groups={hostGroups} tags={hostTags} onChanged={loadNodes} />
          <button type="button" className="console-icon-button" aria-label="刷新主机" disabled={nodesRefreshing} onClick={() => void refreshHostList()}><RefreshCw size={16} className={nodesRefreshing ? 'animate-spin' : undefined} /></button>
        </div>
        {nodes.length === 0 ? <div className="console-empty"><h2>暂无节点接入</h2><p>在目标服务器执行 Agent 安装命令后，节点会自动出现在这里。</p><button className="console-button" aria-expanded={installCommandOpen} aria-controls="agent-install-command" onClick={showInstallCommand}>安装目标主机 Agent 进行采集</button></div> : <>
          <HostBatchTable workspace nodes={filteredNodes} filterKey={`${search}|${hostFilter}|${hostGroupFilter}|${hostTagFilter}`} scrollTop={hostScrollTop.current} onScroll={top => { hostScrollTop.current = top }} onOpenNode={node => openNode(node)} onNodesChanged={loadNodes} />
          <div className="console-table-footer"><span>共 {nodes.length} 台主机 · 当前显示 {filteredNodes.length} 台</span><span>按名称排序</span></div>
        </>}
      </>}
    </section>
  )

  return (
    <>
    <AppShell page={page} collapsed={sidebarCollapsed} onCollapse={() => setSidebarCollapsed(value => !value)} onNavigate={openPage} theme={theme} onTheme={setTheme} username={authenticated ? currentUsername : undefined} onLogout={handleLogout} aiButton={<AITopbarButton onClick={aiAssistant.openDrawer} open={aiAssistant.drawerOpen} />}>
      <div className="console-page-content">
              {error ? <div className="rounded-xl border border-danger/30 bg-danger/10 px-5 py-4 font-semibold text-danger shadow-sm">{error}</div> : null}

              {installCommandDialog}

              {page === 'overview' ? (
                <OverviewPage nodes={nodes} nodesError={nodesError} nodesUpdatedAt={nodesUpdatedAt} nodesLoading={nodesLoading} onRefreshNodes={loadNodes} onAddServer={showInstallCommand} onNavigate={navigateWithinPanel} />
              ) : page === 'ai' ? (
                <AIWorkspacePage assistant={aiAssistant} onOpenSettings={openAISettings} />
              ) : page === 'history' ? (
                <HistoryPage nodes={nodes} selectedNodeID={selectedNodeID} metrics={metrics} range={range} settings={settings} onSelectNode={setSelectedNodeID} onRangeChange={setRange} />
              ) : page === 'settings' ? (
                <SystemSettingsPage settings={settings} about={systemAbout} selectedRetention={settingsRetention} saving={settingsSaving} message={settingsMessage} error={settingsError} onSelectRetention={setSettingsRetention} onSave={saveSettings} />
              ) : page === 'alerts' ? (
                <AlertsPage nodes={nodes} navigationVersion={routeVersion} />
              ) : page === 'uptime' ? (
                <UptimePage navigationVersion={routeVersion} />
              ) : page === 'audit' ? (
                <AuditPage nodes={nodes} />
              ) : page === 'tasks' ? (
                <TasksPage nodes={nodes} navigationVersion={routeVersion} />
              ) : page === 'services' ? (
                <ServicesPage onRegisterLeaveGuard={registerGuard} serviceID={route.kind === 'service-detail' ? route.serviceID : undefined} onOpenService={openService} onBack={backToServices} onNavigate={navigateWithinPanel} diagnosisBusy={aiAssistant.sending || Boolean(aiAssistant.operationID) || aiAssistant.loading} onDiagnose={(service) => aiAssistant.prepareDraft(
                  `请诊断当前应用服务（ID：${service.id}）。先调用 diagnose_incident，scope_type 使用 application_service、scope_id 使用该服务 ID，读取当前证据。按“已观察到的事实、可能原因、尚缺的信息、建议操作”组织回答，标明证据来源和时间；不要把时间接近当作因果关系。先完成只读诊断，如建议变更，必须保留人工确认，并说明处理后应如何验证服务恢复。`,
                  { page: 'services', resource_type: 'application_service', resource_id: service.id }, service.name
                )} />
              ) : page === 'k8s' ? (
                route.kind === 'k8s-cluster-detail' ? (
                  <K8sClusterDetailPage
                    clusterId={route.clusterID}
                    onBack={backToK8sClusters}
                  />
                ) : (
                  <>
                    <K8sClustersPage
                      refreshKey={k8sClustersRefreshKey}
                      onConnectCluster={() => setConnectK8sClusterModalOpen(true)}
                      onViewDetail={openK8sClusterDetail}
                    />
                    <ConnectK8sClusterModal
                      open={connectK8sClusterModalOpen}
                      nodes={nodes}
                      onClose={() => setConnectK8sClusterModalOpen(false)}
                      onSuccess={() => {
                        setConnectK8sClusterModalOpen(false)
                        setK8sClustersRefreshKey((value) => value + 1)
                      }}
                    />
                  </>
                )
              ) : page === 'logs' ? (
                <LogsPage nodes={nodes} />
              ) : hostContent}
      </div>
    </AppShell>
    {switcherOpen && <NodeSwitcher nodes={nodes} selectedID={selectedNodeID} onClose={() => setSwitcherOpen(false)} onSelect={node => openNode(node, true)} />}
    <AIAssistantDrawer assistant={aiAssistant} onOpenWorkspace={openAIWorkspace} onOpenSettings={openAISettings} />
    </>
  )
}

function uniqueNodeGroups(nodes: Node[]): NodeGroupSummary[] {
  const groups = new Map<string, NodeGroupSummary>()
  for (const node of nodes) {
    if (node.group) groups.set(node.group.id, node.group)
  }
  return [...groups.values()]
}

function uniqueNodeTags(nodes: Node[]): NodeTagSummary[] {
  const tags = new Map<string, NodeTagSummary>()
  for (const node of nodes) {
    for (const tag of node.tags ?? []) tags.set(tag.id, tag)
  }
  return [...tags.values()]
}
