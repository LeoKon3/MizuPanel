import type { ReactNode, MouseEvent as ReactMouseEvent } from 'react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Copy, Download, FileCheck2, MoreHorizontal, Play, Plus, RotateCw, ScrollText, ShieldAlert, Square, Tags, Terminal, Trash2, Wifi, WifiOff, X } from 'lucide-react'

import type { AgentLogsResponse, AgentRestartResponse, AgentStatusResponse, AgentUpgradeResponse, ConnectionDiagnostics, DockerComposeAction, DockerComposeDeploymentAction, DockerComposeDeploymentRequest, DockerComposeDeploymentResponse, DockerComposeListResponse, DockerComposeProject, DockerComposeRisk, DockerContainer, DockerResourceAction, DockerResourceListResponse, DockerResourceType, DockerSnapshotResponse, FileDeleteResponse, FileEntry, FileListResponse, FileReadResponse, FileUploadResponse, FileWriteResponse, Metric, Node, ProcessInfo, ProcessSnapshotResponse, RangeOption, RebootResponse, SSHAuthType, SSHJobResponse, SSHProgressEvent, SSHUninstallRequest, SystemdService, SystemdServiceAction, SystemdServiceListResponse } from '../types'
import { formatBytes, formatPercent, formatSpeed } from '../lib/format'
import { MetricsChart } from '../components/MetricsChart'
import LogViewer from '../components/LogViewer'
import ContainerLogsModal from '../components/ContainerLogsModal'
import CreateContainerModal from '../components/CreateContainerModal'
import { SingleNodeOrganizationModal } from '../components/SingleNodeOrganizationModal'
import { Toast } from '../components/Toast'
import { DockerResourcesPanel } from '../components/DockerResourcesPanel'

type NodeDetailProps = {
  node?: Node
  metrics: Metric[]
  processSnapshot?: ProcessSnapshotResponse
  dockerSnapshot?: DockerSnapshotResponse
  dockerCompose?: DockerComposeListResponse
  dockerResources?: DockerResourceListResponse
  systemdServices?: SystemdServiceListResponse
  monitoringLoading?: boolean
  range: RangeOption
  onRangeChange: (range: RangeOption) => void
  onLoadFiles?: (nodeID: string, path: string) => Promise<FileListResponse>
  onReadFile?: (nodeID: string, path: string) => Promise<FileReadResponse>
  onWriteFile?: (nodeID: string, path: string, content: string) => Promise<FileWriteResponse>
  onUploadFile?: (nodeID: string, path: string, contentBase64: string) => Promise<FileUploadResponse>
  onDeletePath?: (nodeID: string, path: string) => Promise<FileDeleteResponse>
  onRebootNode?: (nodeID: string) => Promise<RebootResponse>
  onSSHUninstall?: (nodeID: string, request: SSHUninstallRequest) => Promise<SSHJobResponse>
  onGetAgentStatus?: (nodeID: string) => Promise<AgentStatusResponse>
  onGetConnectionDiagnostics?: (nodeID: string) => Promise<ConnectionDiagnostics>
  onUpgradeAgent?: (nodeID: string) => Promise<AgentUpgradeResponse>
  onGetAgentUpgradeStatus?: (nodeID: string) => Promise<{ node_id: string; target_version: string; actual_version?: string; stage: string; error?: string }>
	onGetLegacyAgentUpgradeCommand?: () => Promise<string>
  onRestartAgent?: (nodeID: string) => Promise<AgentRestartResponse>
  onGetAgentLogs?: (nodeID: string, lines: number) => Promise<AgentLogsResponse>
  onRefreshDocker?: (nodeID: string) => Promise<void>
  onRefreshDockerCompose?: (nodeID: string) => Promise<void>
  onDockerComposeAction?: (nodeID: string, projectName: string, action: DockerComposeAction, serviceName?: string) => Promise<{ success: boolean, output?: string, error?: string }>
  onDockerComposeDeployment?: (nodeID: string, request: DockerComposeDeploymentRequest) => Promise<DockerComposeDeploymentResponse>
  onRefreshDockerResources?: (nodeID: string) => Promise<void>
  onDockerResourceAction?: (nodeID: string, resourceType: DockerResourceType, resourceID: string, action: DockerResourceAction) => Promise<{ success: boolean, error?: string }>
  onRefreshSystemdServices?: (nodeID: string) => Promise<void>
  onSystemdServiceAction?: (nodeID: string, serviceName: string, action: SystemdServiceAction) => Promise<{ success: boolean, output?: string, error?: string }>
  onNodeOrganizationChanged?: () => Promise<void> | void
}

type DetailSection = 'overview' | 'processes' | 'containers' | 'services' | 'files' | 'logs' | 'agent'

type NodeDetailQuery = {
  section: DetailSection
  dockerView: 'containers' | 'compose' | 'resources'
  search: string
}

function readNodeDetailQuery(): NodeDetailQuery {
  if (typeof window === 'undefined') return { section: 'overview', dockerView: 'containers', search: '' }
  const params = new URLSearchParams(window.location.search)
  const requestedSection = params.get('section')
  const section: DetailSection = requestedSection === 'processes' || requestedSection === 'containers' || requestedSection === 'services' || requestedSection === 'files' || requestedSection === 'logs' || requestedSection === 'agent'
    ? requestedSection
    : 'overview'
  const requestedDockerView = params.get('docker')
  const dockerView = section === 'containers' && (requestedDockerView === 'compose' || requestedDockerView === 'resources')
    ? requestedDockerView
    : 'containers'
  return { section, dockerView, search: (params.get('q') || '').slice(0, 256) }
}
type ProcessSort = 'cpu' | 'memory' | 'pid' | 'name'
type DockerFilter = 'all' | 'running' | 'stopped' | 'abnormal'
type SSHProgressEventLog = SSHProgressEvent & { logs: string[] }
type ChartRange = Extract<RangeOption, '1h' | '6h'>
type ManagedComposeDeploymentDraft = {
  projectID?: string
  displayName: string
  composeYAML: string
  envFile: string
  pullImages: boolean
}
type ManagedComposeEditor = {
  projectID?: string
  projectName?: string
}
type ManagedComposePreview = {
  draft: ManagedComposeDeploymentDraft
  confirmationToken: string
  risks: DockerComposeRisk[]
  projectName?: string
}
type PendingManagedComposeAction = {
  action: Extract<DockerComposeDeploymentAction, 'rollback' | 'archive'>
  project: DockerComposeProject
}
type PathTreeState = Record<string, {
  expanded: boolean
  loading: boolean
  children: string[]
  error?: string
}>

const FILE_TREE_ROOTS = ['/', '/root', '/data', '/usr', '/var', '/tmp', '/home']

function emptyManagedComposeDeploymentDraft(): ManagedComposeDeploymentDraft {
  return { displayName: '', composeYAML: '', envFile: '', pullImages: true }
}

function isManagedComposeProject(project: DockerComposeProject) {
  return project.management === 'managed'
}

function deploymentRequestForDraft(action: Extract<DockerComposeDeploymentAction, 'preview' | 'apply'>, draft: ManagedComposeDeploymentDraft, confirmationToken?: string): DockerComposeDeploymentRequest {
  return {
    action,
    ...(draft.projectID ? { project_id: draft.projectID } : {}),
    display_name: draft.displayName.trim(),
    compose_yaml: draft.composeYAML,
    ...(draft.envFile ? { env_file: draft.envFile } : {}),
    pull_images: draft.pullImages,
    ...(confirmationToken ? { confirmation_token: confirmationToken } : {})
  }
}

async function copyTextToClipboard(value: string): Promise<void> {
	if (navigator.clipboard) {
		try {
			await navigator.clipboard.writeText(value)
			return
		} catch {
			// Machine-IP HTTP access may reject Clipboard API; fall back below.
		}
	}
	const textarea = document.createElement('textarea')
	textarea.value = value
	textarea.setAttribute('readonly', '')
	textarea.style.position = 'fixed'
	textarea.style.left = '-9999px'
	document.body.appendChild(textarea)
	textarea.select()
	const copied = document.execCommand('copy')
	document.body.removeChild(textarea)
	if (!copied) throw new Error('浏览器拒绝复制')
}

function mergeSSHProgressEvent(current: SSHProgressEventLog[], progress: SSHProgressEvent): SSHProgressEventLog[] {
  const index = current.findIndex((event) => event.step === progress.step)
  if (index === -1) {
    return [...current, { ...progress, logs: progress.message ? [progress.message] : [] }]
  }
  const next = [...current]
  const existing = next[index]
  const logs = progress.message && existing.logs[existing.logs.length - 1] !== progress.message
    ? [...existing.logs, progress.message]
    : existing.logs
  next[index] = { ...existing, ...progress, logs }
  return next
}

export function NodeDetail({ node, metrics, processSnapshot, dockerSnapshot, dockerCompose, dockerResources, systemdServices, monitoringLoading = false, range, onRangeChange, onLoadFiles, onReadFile, onWriteFile, onUploadFile, onDeletePath, onRebootNode, onSSHUninstall, onGetAgentStatus, onGetConnectionDiagnostics, onUpgradeAgent, onGetAgentUpgradeStatus, onGetLegacyAgentUpgradeCommand, onRestartAgent, onGetAgentLogs, onRefreshDocker, onRefreshDockerCompose, onDockerComposeAction, onDockerComposeDeployment, onRefreshDockerResources, onDockerResourceAction, onRefreshSystemdServices, onSystemdServiceAction, onNodeOrganizationChanged }: NodeDetailProps) {
  const [initialQuery] = useState(readNodeDetailQuery)
  const [activeSection, setActiveSection] = useState<DetailSection>(initialQuery.section)
  const detailSearch = initialQuery.section === 'services' ? initialQuery.search : ''
  const [processSort, setProcessSort] = useState<ProcessSort>('cpu')
  const [processSearch, setProcessSearch] = useState('')
  const [dockerFilter, setDockerFilter] = useState<DockerFilter>('all')
  const [dockerSearch, setDockerSearch] = useState(initialQuery.section === 'containers' ? initialQuery.search : '')
  const [dockerViewState, setDockerViewState] = useState<{ nodeID?: string, view: 'containers' | 'compose' | 'resources' }>({ nodeID: node?.id, view: initialQuery.dockerView })
  // Derive the view from the current node during render. This prevents an old
  // resource selection from issuing a request in the effect pass immediately
  // after a host switch.
  const dockerView = dockerViewState.nodeID === node?.id ? dockerViewState.view : 'containers'
  const setDockerView = (view: 'containers' | 'compose' | 'resources') => setDockerViewState({ nodeID: node?.id, view })
  const [composeActionLoading, setComposeActionLoading] = useState<string>()
  const [composeLoading, setComposeLoading] = useState(false)
  const [resourcesLoading, setResourcesLoading] = useState(false)
  const [pendingComposeDown, setPendingComposeDown] = useState<string>()
  const [composeLogsModal, setComposeLogsModal] = useState<{ projectName: string; output: string }>()
  const [managedComposeEditor, setManagedComposeEditor] = useState<ManagedComposeEditor>()
  const [managedComposeDraft, setManagedComposeDraft] = useState<ManagedComposeDeploymentDraft>(emptyManagedComposeDeploymentDraft)
  const [managedComposePreview, setManagedComposePreview] = useState<ManagedComposePreview>()
  const [pendingManagedComposeAction, setPendingManagedComposeAction] = useState<PendingManagedComposeAction>()
  const [deploymentLoading, setDeploymentLoading] = useState<DockerComposeDeploymentAction>()
  const composeDeploymentRequestSeq = useRef(0)
  const composeDeploymentNodeIDRef = useRef(node?.id)
  composeDeploymentNodeIDRef.current = node?.id
  const [systemdLoading, setSystemdLoading] = useState(false)
  const [systemdActionLoading, setSystemdActionLoading] = useState<string>()
  const [systemdLogsModal, setSystemdLogsModal] = useState<{ serviceName: string; output: string }>()

  useEffect(() => {
    composeDeploymentRequestSeq.current += 1
    setComposeActionLoading(undefined)
    setComposeLoading(false)
    setResourcesLoading(false)
    setPendingComposeDown(undefined)
    setComposeLogsModal(undefined)
    setManagedComposeEditor(undefined)
    setManagedComposeDraft(emptyManagedComposeDeploymentDraft())
    setManagedComposePreview(undefined)
    setPendingManagedComposeAction(undefined)
    setDeploymentLoading(undefined)
    setSystemdLoading(false)
    setSystemdActionLoading(undefined)
    setSystemdLogsModal(undefined)
  }, [node?.id])

  useEffect(() => {
    if (!node || activeSection !== 'containers' || dockerView !== 'compose' || !onRefreshDockerCompose) return
    setComposeLoading(true)
    void onRefreshDockerCompose(node.id).finally(() => setComposeLoading(false))
  }, [activeSection, dockerView, node?.id])

  useEffect(() => {
    if (!node || activeSection !== 'containers' || dockerView !== 'resources' || !onRefreshDockerResources) return
    setResourcesLoading(true)
    void onRefreshDockerResources(node.id).finally(() => setResourcesLoading(false))
  }, [activeSection, dockerView, node?.id])

  useEffect(() => {
    if (!node || activeSection !== 'services' || !onRefreshSystemdServices) return
    setSystemdLoading(true)
    void onRefreshSystemdServices(node.id).finally(() => setSystemdLoading(false))
  }, [activeSection, node?.id])
  const [containerLogsModal, setContainerLogsModal] = useState<{ open: boolean; containerId: string; containerName: string }>({
    open: false,
    containerId: '',
    containerName: '',
  })
  const [createContainerModal, setCreateContainerModal] = useState(false)
  const [chartRanges, setChartRanges] = useState<Record<string, ChartRange>>({ cpu: '1h', memory: '1h', disk: '1h', network: '1h', diskIO: '1h', load: '1h' })
  const [fileList, setFileList] = useState<FileListResponse>()
  const [fileRead, setFileRead] = useState<FileReadResponse>()
  const [fileContent, setFileContent] = useState('')
  const [pathInput, setPathInput] = useState('/')
  const [editorOpen, setEditorOpen] = useState(false)
  const [pathTree, setPathTree] = useState<PathTreeState>({})
  const [dragActive, setDragActive] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<FileEntry>()
  const fileRequestSeq = useRef(0)
  const agentRequestSeq = useRef(0)
  const treeRequestSeqByPath = useRef<Record<string, number>>({})
  const uploadInputRef = useRef<HTMLInputElement>(null)
  const [operationMessage, setOperationMessage] = useState<string>()
  const [fileLoading, setFileLoading] = useState(false)
  const [sshUninstallOpen, setSSHUninstallOpen] = useState(false)
  const [sshAuthType, setSSHAuthType] = useState<SSHAuthType>('password')
  const [sshHost, setSSHHost] = useState('')
  const [sshPort, setSSHPort] = useState(22)
  const [sshPassword, setSSHPassword] = useState('')
  const [sshPrivateKey, setSSHPrivateKey] = useState('')
  const [sshPassphrase, setSSHPassphrase] = useState('')
  const [sshRemoveRecord, setSSHRemoveRecord] = useState(true)
  const [sshUninstallLoading, setSSHUninstallLoading] = useState(false)
  const [sshUninstallMessage, setSSHUninstallMessage] = useState<string>()
  const [sshUninstallError, setSSHUninstallError] = useState<string>()
  const [sshUninstallEvents, setSSHUninstallEvents] = useState<SSHProgressEventLog[]>([])
  const [agentStatus, setAgentStatus] = useState<AgentStatusResponse>()
  const [connectionDiagnostics, setConnectionDiagnostics] = useState<ConnectionDiagnostics>()
  const [agentLogs, setAgentLogs] = useState<AgentLogsResponse>()
  const [agentLoading, setAgentLoading] = useState(false)
  const [agentMessage, setAgentMessage] = useState<string>()
  const [agentError, setAgentError] = useState<string>()
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
	const [rebootOpen, setRebootOpen] = useState(false)
	const [rebootLoading, setRebootLoading] = useState(false)
	const [restartOpen, setRestartOpen] = useState(false)
	const [restartLoading, setRestartLoading] = useState(false)
  const [upgradeOpen, setUpgradeOpen] = useState(false)
  const [upgradeLoading, setUpgradeLoading] = useState(false)
	const [organizationEditorOpen, setOrganizationEditorOpen] = useState(false)
	const [legacyUpgradeCopying, setLegacyUpgradeCopying] = useState(false)

	useEffect(() => {
		if (!rebootOpen && !restartOpen && !upgradeOpen) return undefined
		const handleEscape = (event: KeyboardEvent) => {
			if (event.key !== 'Escape') return
			if (upgradeOpen && !upgradeLoading) setUpgradeOpen(false)
			else if (restartOpen && !restartLoading) setRestartOpen(false)
			else if (rebootOpen && !rebootLoading) setRebootOpen(false)
		}
		document.addEventListener('keydown', handleEscape)
		return () => document.removeEventListener('keydown', handleEscape)
	}, [rebootLoading, rebootOpen, restartLoading, restartOpen, upgradeLoading, upgradeOpen])

  useEffect(() => {
    agentRequestSeq.current += 1
    setAgentStatus(undefined)
    setConnectionDiagnostics(undefined)
    setAgentLogs(undefined)
    setAgentMessage(undefined)
    setAgentError(undefined)
    setAgentLoading(false)
    if (!node || activeSection !== 'agent') return
    const requestID = agentRequestSeq.current
    if (node.status !== 'online') {
      setAgentError('节点离线，状态与日志暂不可用；连接诊断仍可查看。')
    }
    setAgentLoading(true)
    Promise.allSettled([
      node.status === 'online' && onGetAgentStatus ? onGetAgentStatus(node.id) : Promise.resolve(undefined),
      node.status === 'online' && onGetAgentLogs ? onGetAgentLogs(node.id, 100) : Promise.resolve(undefined),
      onGetConnectionDiagnostics ? onGetConnectionDiagnostics(node.id) : Promise.resolve(undefined)
    ])
      .then(([statusResult, logsResult, diagnosticsResult]) => {
        if (requestID !== agentRequestSeq.current) return
        if (statusResult.status === 'fulfilled' && statusResult.value) {
          setAgentStatus(statusResult.value)
        } else if (statusResult.status === 'rejected') {
          setAgentError(statusResult.reason instanceof Error ? statusResult.reason.message : 'Agent 状态加载失败')
        }
        if (logsResult.status === 'fulfilled' && logsResult.value) {
          setAgentLogs(logsResult.value)
          if (logsResult.value.error) setAgentError(logsResult.value.error)
        } else if (logsResult.status === 'rejected') {
          setAgentError(logsResult.reason instanceof Error ? logsResult.reason.message : 'Agent 日志加载失败')
        }
        if (diagnosticsResult.status === 'fulfilled' && diagnosticsResult.value) {
          setConnectionDiagnostics(diagnosticsResult.value)
        } else if (diagnosticsResult.status === 'rejected') {
          const message = diagnosticsResult.reason instanceof Error ? diagnosticsResult.reason.message : '未知错误'
          setToast({ message: `连接诊断加载失败: ${message}`, type: 'error' })
        }
      })
      .finally(() => {
        if (requestID === agentRequestSeq.current) setAgentLoading(false)
      })
  }, [node?.id, node?.status])

  const filteredProcesses = useMemo(() => {
    const keyword = processSearch.trim().toLowerCase()
    const rows = [...(processSnapshot?.processes ?? [])]
      .filter((process) => {
        if (!keyword) return true
        return [process.pid.toString(), process.name, process.user, process.status]
          .some((value) => value.toLowerCase().includes(keyword))
      })
    rows.sort((left, right) => compareProcesses(left, right, processSort))
    return rows
  }, [processSearch, processSnapshot, processSort])

  const filteredContainers = useMemo(() => {
    const keyword = dockerSearch.trim().toLowerCase()
    return (dockerSnapshot?.containers ?? [])
      .filter((container) => dockerFilter === 'all' || dockerFilterFor(container) === dockerFilter)
      .filter((container) => {
        if (!keyword) return true
        return [container.id, container.name, container.image, container.state, container.status]
          .some((value) => value.toLowerCase().includes(keyword))
      })
  }, [dockerFilter, dockerSearch, dockerSnapshot])

  if (!node) {
    return null
  }

  const metric = node.latest_metric
  const latestChartMetric = mergeMetricFallback(metrics.length > 0 ? metrics[metrics.length - 1] : undefined, metric)
  const uptimeText = formatUptime(latestChartMetric?.uptime)
  const bootTimeText = formatBootTime(latestChartMetric)
  const displayName = node.name || node.hostname
  const online = node.status === 'online'
  const agentModeLabel = node.agent_mode === 'ops' ? '运维模式' : '普通模式'
  const agentUserLabel = node.agent_user || '未知用户'
  const nextFileRequest = () => {
    fileRequestSeq.current += 1
    return fileRequestSeq.current
  }
  const isLatestFileRequest = (requestID: number) => requestID === fileRequestSeq.current
  const currentFilePath = fileList?.path || pathInput || '/'

  const mergePathTree = (path: string, entries: FileEntry[]) => {
    const directories = entries
      .filter((entry) => entry.type === 'directory')
      .map((entry) => entry.path)
      .sort((left, right) => left.localeCompare(right))

    setPathTree((current) => ({
      ...current,
      [path]: {
        expanded: current[path]?.expanded ?? false,
        loading: false,
        children: directories,
        error: undefined,
      }
    }))
  }

  const togglePathTree = (path: string) => {
    if (!online || !onLoadFiles) {
      setOperationMessage('节点离线，无法加载路径树。')
      return
    }

    const current = pathTree[path]
    if (current?.expanded) {
      treeRequestSeqByPath.current[path] = (treeRequestSeqByPath.current[path] || 0) + 1
      setPathTree((tree) => ({ ...tree, [path]: { ...tree[path], expanded: false, loading: false } }))
      return
    }

    if (current && current.children.length > 0) {
      setPathTree((tree) => ({ ...tree, [path]: { ...tree[path], expanded: true } }))
      return
    }

    const requestID = (treeRequestSeqByPath.current[path] || 0) + 1
    treeRequestSeqByPath.current[path] = requestID
    setPathTree((tree) => ({
      ...tree,
      [path]: { expanded: true, loading: true, children: tree[path]?.children || [] }
    }))

    onLoadFiles(node.id, path)
      .then((response) => {
        if (treeRequestSeqByPath.current[path] !== requestID) return
        if (response.error) {
          setPathTree((tree) => ({
            ...tree,
            [path]: { expanded: true, loading: false, children: [], error: formatOperationError(response.code, response.error || '路径树加载失败') }
          }))
          return
        }
        const directories = response.entries
          .filter((entry) => entry.type === 'directory')
          .map((entry) => entry.path)
          .sort((left, right) => left.localeCompare(right))
        setPathTree((tree) => ({
          ...tree,
          [path]: { expanded: true, loading: false, children: directories, error: undefined }
        }))
      })
      .catch((err: unknown) => {
        if (treeRequestSeqByPath.current[path] !== requestID) return
        setPathTree((tree) => ({
          ...tree,
          [path]: { expanded: true, loading: false, children: [], error: err instanceof Error ? err.message : '路径树加载失败' }
        }))
      })
  }

  const uploadFiles = (files?: FileList | File[]) => {
    const fileArray = Array.from(files || [])
    if (fileArray.length === 0) return
    uploadFile(fileArray[0], fileArray.length > 1 ? '暂只支持单文件上传，已选择第一个文件。' : undefined)
  }

  const confirmDeleteEntry = (entry: FileEntry) => {
    setPendingDelete(entry)
    setOperationMessage(undefined)
  }

  const cancelDeleteEntry = () => {
    setPendingDelete(undefined)
  }

  const loadFiles = (path: string) => {
    if (!online || !onLoadFiles) {
      setOperationMessage('节点离线，无法发送文件管理命令。')
      return
    }
    setActiveSection('files')
    setFileLoading(true)
    setOperationMessage(undefined)
    const requestID = nextFileRequest()
    onLoadFiles(node.id, path)
      .then((response) => {
        if (!isLatestFileRequest(requestID)) return
        setFileList(response)
        setFileRead(undefined)
        setFileContent('')
        setEditorOpen(false)
        setPendingDelete(undefined)
        if (!response.error) {
          setPathInput(response.path || path)
          mergePathTree(response.path || path, response.entries || [])
        }
        if (response.error) setOperationMessage(formatOperationError(response.code, response.error))
      })
      .catch((err: unknown) => {
        if (isLatestFileRequest(requestID)) setOperationMessage(err instanceof Error ? err.message : '文件目录加载失败')
      })
      .finally(() => {
        if (isLatestFileRequest(requestID)) setFileLoading(false)
      })
  }

  const openFileEntry = (entry: FileEntry) => {
    if (entry.type === 'directory') {
      loadFiles(entry.path)
      return
    }
    setFileLoading(false)
    if (entry.type === 'binary') {
      nextFileRequest()
      setEditorOpen(false)
      setOperationMessage('二进制文件不可编辑')
      return
    }
    if (!onReadFile) return
    setOperationMessage(undefined)
    const requestID = nextFileRequest()
    onReadFile(node.id, entry.path)
      .then((response) => {
        if (!isLatestFileRequest(requestID)) return
        setFileRead(response)
        setFileContent(response.content || '')
        setEditorOpen(Boolean(response.editable && !response.error))
        if (response.error) setOperationMessage(formatOperationError(response.code, response.error))
      })
      .catch((err: unknown) => {
        if (isLatestFileRequest(requestID)) setOperationMessage(err instanceof Error ? err.message : '文件读取失败')
      })
  }

  const openPath = () => {
    const target = pathInput.trim() || '/'
    if (!online || !onLoadFiles) {
      setOperationMessage('节点离线，无法发送文件管理命令。')
      return
    }
    setActiveSection('files')
    setFileLoading(true)
    setOperationMessage(undefined)
    const requestID = nextFileRequest()
    onLoadFiles(node.id, target)
      .then((response) => {
        if (!isLatestFileRequest(requestID)) return
        if (!response.error) {
          setFileList(response)
          setPathInput(response.path || target)
          setFileRead(undefined)
          setFileContent('')
          setEditorOpen(false)
          setPendingDelete(undefined)
          mergePathTree(response.path || target, response.entries || [])
          return
        }
        if (response.code === 'not_directory' && onReadFile) {
          return onReadFile(node.id, target).then((readResponse) => {
            if (!isLatestFileRequest(requestID)) return
            setFileRead(readResponse)
            setFileContent(readResponse.content || '')
            setEditorOpen(Boolean(readResponse.editable && !readResponse.error))
            if (readResponse.error) setOperationMessage(formatOperationError(readResponse.code, readResponse.error))
          })
        }
        setEditorOpen(false)
        setOperationMessage(formatOperationError(response.code, response.error))
      })
      .catch((err: unknown) => {
        if (isLatestFileRequest(requestID)) setOperationMessage(err instanceof Error ? err.message : '路径打开失败')
      })
      .finally(() => {
        if (isLatestFileRequest(requestID)) setFileLoading(false)
      })
  }

  const saveFile = () => {
    if (!fileRead || !onWriteFile) return
    setOperationMessage(undefined)
    onWriteFile(node.id, fileRead.path, fileContent)
      .then((response) => {
        setOperationMessage(response.saved ? '文件已保存。' : formatOperationError(response.code, response.error || '文件保存失败'))
      })
      .catch((err: unknown) => setOperationMessage(err instanceof Error ? err.message : '文件保存失败'))
  }

  const uploadFile = (file?: File, notice?: string) => {
    if (!file || !onUploadFile) return
    if (!online || !onLoadFiles) {
      setOperationMessage('节点离线，无法上传文件。')
      return
    }
    const directory = currentFilePath
    const targetPath = joinRemotePath(directory, file.name)
    const requestID = nextFileRequest()
    setFileLoading(true)
    setOperationMessage(notice)
    fileToBase64(file)
      .then((contentBase64) => onUploadFile(node.id, targetPath, contentBase64))
      .then((response) => {
        if (!isLatestFileRequest(requestID)) return undefined
        setOperationMessage(response.uploaded ? `${notice ? `${notice} ` : ''}文件已上传。` : formatOperationError(response.code, response.error || '文件上传失败'))
        if (!response.uploaded) return undefined
        return onLoadFiles(node.id, directory)
      })
      .then((response) => {
        if (!response || !isLatestFileRequest(requestID)) return
        if (!response.error) {
          setFileList(response)
          setPathInput(response.path || directory)
          mergePathTree(response.path || directory, response.entries || [])
        } else {
          setOperationMessage(formatOperationError(response.code, response.error))
        }
      })
      .catch((err: unknown) => {
        if (isLatestFileRequest(requestID)) setOperationMessage(err instanceof Error ? err.message : '文件上传失败')
      })
      .finally(() => {
        if (isLatestFileRequest(requestID)) setFileLoading(false)
      })
  }

  const deleteEntry = (entry: FileEntry) => {
    if (!onDeletePath || !onLoadFiles) return
    const directory = fileList?.path || '/'
    const requestID = nextFileRequest()
    setFileLoading(true)
    setOperationMessage(undefined)
    onDeletePath(node.id, entry.path)
      .then((response) => {
        if (!isLatestFileRequest(requestID)) return undefined
        setOperationMessage(response.deleted ? '文件已删除。' : formatOperationError(response.code, response.error || '文件删除失败'))
        if (response.deleted) setPendingDelete(undefined)
        if (!response.deleted) return undefined
        return onLoadFiles(node.id, directory)
      })
      .then((response) => {
        if (!response || !isLatestFileRequest(requestID)) return
        if (!response.error) {
          setFileList(response)
          setPathInput(response.path || directory)
          mergePathTree(response.path || directory, response.entries || [])
        } else {
          setOperationMessage(formatOperationError(response.code, response.error))
        }
      })
      .catch((err: unknown) => {
        if (isLatestFileRequest(requestID)) setOperationMessage(err instanceof Error ? err.message : '文件删除失败')
      })
      .finally(() => {
        if (isLatestFileRequest(requestID)) setFileLoading(false)
      })
  }

  const reboot = () => {
    if (!online || !onRebootNode) {
      setOperationMessage('节点离线，无法发送重启命令。')
      return
    }
		setRebootLoading(true)
    setOperationMessage(undefined)
    onRebootNode(node.id)
			.then((response) => {
				if (!response.accepted) throw new Error(formatOperationError(response.code, response.error || '节点重启命令发送失败'))
				setRebootOpen(false)
				setToast({ message: '节点重启命令下发成功', type: 'success' })
			})
			.catch((err: unknown) => setToast({ message: `节点重启失败: ${err instanceof Error ? err.message : '未知错误'}`, type: 'error' }))
			.finally(() => setRebootLoading(false))
  }

  const closeSSHUninstallDialog = () => {
    setSSHHost('')
    setSSHPort(22)
    setSSHAuthType('password')
    setSSHPassword('')
    setSSHPrivateKey('')
    setSSHPassphrase('')
    setSSHRemoveRecord(true)
    setSSHUninstallLoading(false)
    setSSHUninstallMessage(undefined)
    setSSHUninstallError(undefined)
    setSSHUninstallEvents([])
    setSSHUninstallOpen(false)
  }

  const openSSHUninstallDialog = () => {
    setSSHHost(node.ip || '')
    setSSHPort(22)
    setSSHAuthType('password')
    setSSHPassword('')
    setSSHPrivateKey('')
    setSSHPassphrase('')
    setSSHRemoveRecord(true)
    setSSHUninstallMessage(undefined)
    setSSHUninstallError(undefined)
    setSSHUninstallEvents([])
    setSSHUninstallOpen(true)
  }

  const subscribeSSHUninstallProgress = (jobID: string) => {
    const source = new EventSource(`/api/nodes/${encodeURIComponent(node.id)}/ssh-uninstall/${encodeURIComponent(jobID)}/events`)
    source.onmessage = (event) => {
      const progress = JSON.parse(event.data) as SSHProgressEvent
      setSSHUninstallEvents((current) => mergeSSHProgressEvent(current, progress))
      if (progress.done) source.close()
    }
    source.onerror = () => source.close()
  }

  const startSSHUninstall = () => {
    if (!onSSHUninstall || sshUninstallLoading) return
    setSSHUninstallLoading(true)
    setSSHUninstallMessage(undefined)
    setSSHUninstallError(undefined)
    onSSHUninstall(node.id, {
      host: sshHost.trim(),
      port: sshPort || 22,
      username: 'root',
      auth_type: sshAuthType,
      ...(sshAuthType === 'password' ? { password: sshPassword } : { private_key: sshPrivateKey, ...(sshPassphrase ? { passphrase: sshPassphrase } : {}) }),
      remove_node_record: sshRemoveRecord
    })
      .then((response) => {
        setSSHUninstallMessage(`SSH 卸载任务已创建：${response.job_id}`)
        subscribeSSHUninstallProgress(response.job_id)
      })
      .catch((err: unknown) => setSSHUninstallError(err instanceof Error ? err.message : 'SSH 卸载任务创建失败'))
      .finally(() => setSSHUninstallLoading(false))
  }

  const loadAgentManagement = () => {
    setActiveSection('agent')
    setAgentStatus(undefined)
    setAgentLogs(undefined)
    setAgentMessage(undefined)
    setAgentError(undefined)
    const requestID = agentRequestSeq.current + 1
    agentRequestSeq.current = requestID
    if (!online) {
      setAgentError('节点离线，状态与日志暂不可用；连接诊断仍可查看。')
    }
    setAgentLoading(true)
    Promise.allSettled([
      online && onGetAgentStatus ? onGetAgentStatus(node.id) : Promise.resolve(undefined),
      online && onGetAgentLogs ? onGetAgentLogs(node.id, 100) : Promise.resolve(undefined),
      onGetConnectionDiagnostics ? onGetConnectionDiagnostics(node.id) : Promise.resolve(undefined)
    ])
      .then(([statusResult, logsResult, diagnosticsResult]) => {
        if (requestID !== agentRequestSeq.current) return
        if (statusResult.status === 'fulfilled' && statusResult.value) {
          setAgentStatus(statusResult.value)
        } else if (statusResult.status === 'rejected') {
          setAgentError(statusResult.reason instanceof Error ? statusResult.reason.message : 'Agent 状态加载失败')
        }
        if (logsResult.status === 'fulfilled' && logsResult.value) {
          setAgentLogs(logsResult.value)
          if (logsResult.value.error) setAgentError(logsResult.value.error)
        } else if (logsResult.status === 'rejected') {
          setAgentError(logsResult.reason instanceof Error ? logsResult.reason.message : 'Agent 日志加载失败')
        }
        if (diagnosticsResult.status === 'fulfilled' && diagnosticsResult.value) setConnectionDiagnostics(diagnosticsResult.value)
        else if (diagnosticsResult.status === 'rejected') setToast({ message: `连接诊断加载失败: ${diagnosticsResult.reason instanceof Error ? diagnosticsResult.reason.message : '未知错误'}`, type: 'error' })
      })
      .finally(() => {
        if (requestID === agentRequestSeq.current) setAgentLoading(false)
      })
  }

  const refreshAgentLogs = () => {
    if (!online || !onGetAgentLogs) {
      setAgentError('节点离线，无法获取 Agent 日志。')
      return
    }
    const requestID = agentRequestSeq.current + 1
    agentRequestSeq.current = requestID
    setAgentLoading(true)
    setAgentError(undefined)
    onGetAgentLogs(node.id, 100)
      .then((logs) => {
        if (requestID !== agentRequestSeq.current) return
        setAgentLogs(logs)
        if (logs.error) setAgentError(logs.error)
      })
      .catch((err: unknown) => {
        if (requestID === agentRequestSeq.current) setAgentError(err instanceof Error ? err.message : 'Agent 日志加载失败')
      })
      .finally(() => {
        if (requestID === agentRequestSeq.current) setAgentLoading(false)
      })
  }

  const restartAgentService = () => {
    if (!online || !onRestartAgent) {
      setAgentError('节点离线，无法重启 Agent。')
      return
    }
		setRestartLoading(true)
    setAgentMessage(undefined)
    setAgentError(undefined)
    onRestartAgent(node.id)
			.then((response) => {
				if (!response.accepted) throw new Error(formatOperationError(response.code, response.error || 'Agent 重启命令发送失败'))
				setRestartOpen(false)
				setToast({ message: 'Agent重启命令下发成功', type: 'success' })
			})
			.catch((err: unknown) => setToast({ message: `Agent重启失败: ${err instanceof Error ? err.message : '未知错误'}`, type: 'error' }))
			.finally(() => setRestartLoading(false))
  }

  const confirmAgentUpgrade = () => {
    if (!onUpgradeAgent || upgradeLoading) return
    setUpgradeLoading(true)
    onUpgradeAgent(node.id)
      .then((response) => {
        if (!response.accepted) throw new Error(response.error || '升级请求未被接受')
        setUpgradeOpen(false)
        setToast({ message: 'Agent升级已开始，正在等待重新连接', type: 'success' })
        if (onGetAgentUpgradeStatus) {
          let attempts = 0
          const timer = window.setInterval(() => {
            attempts += 1
            onGetAgentUpgradeStatus(node.id).then((status) => {
              if (status.stage === 'completed') { window.clearInterval(timer); setToast({ message: 'Agent升级成功', type: 'success' }) }
              else if (status.stage === 'failed') { window.clearInterval(timer); setToast({ message: `Agent升级失败: ${status.error || '未知错误'}`, type: 'error' }) }
              else if (attempts >= 40) { window.clearInterval(timer); setToast({ message: 'Agent升级失败: 等待重新连接超时', type: 'error' }) }
            }).catch(() => { if (attempts >= 40) window.clearInterval(timer) })
          }, 3000)
        }
      })
      .catch((err: unknown) => setToast({ message: `Agent升级失败: ${err instanceof Error ? err.message : '未知错误'}`, type: 'error' }))
      .finally(() => setUpgradeLoading(false))
  }

	const copyLegacyAgentUpgradeCommand = () => {
		if (!onGetLegacyAgentUpgradeCommand || legacyUpgradeCopying) return
		setLegacyUpgradeCopying(true)
		onGetLegacyAgentUpgradeCommand()
			.then(copyTextToClipboard)
			.then(() => setToast({ message: 'Agent升级命令复制成功', type: 'success' }))
			.catch((err: unknown) => setToast({ message: `Agent升级命令复制失败: ${err instanceof Error ? err.message : '未知错误'}`, type: 'error' }))
			.finally(() => setLegacyUpgradeCopying(false))
	}

  const handleCreateContainer = async (nodeId: string, command: string) => {
    try {
      setOperationMessage(undefined)
      const response = await fetch(`/api/nodes/${nodeId}/docker/exec`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ command }),
      })

      const result = await response.json()

      if (!response.ok) {
        throw new Error(result.error || '执行失败')
      }

      if (!result.accepted) {
        throw new Error(result.error || '命令被拒绝')
      }

      if (result.exit_code !== 0) {
        setOperationMessage(`容器创建失败 (退出码: ${result.exit_code})\n${result.output || result.error || ''}`)
        return
      }

      setOperationMessage(`容器创建成功！\n${result.output || ''}`)

      // Docker data refreshes automatically via snapshot polling
      setCreateContainerModal(false)
    } catch (error) {
      setOperationMessage(`容器创建失败: ${error instanceof Error ? error.message : String(error)}`)
    }
  }

  const runComposeAction = async (projectName: string, action: DockerComposeAction, serviceName?: string) => {
    if (!node || !onDockerComposeAction) return
    setComposeActionLoading(`${projectName}:${serviceName || 'project'}:${action}`)
    const target = serviceName ? `Compose 服务 ${serviceName}` : 'Compose 项目'
    try {
      // Keep project-level invocations at three arguments. Existing integrations can
      // continue to receive exactly the request shape they supported before this feature.
      const result = serviceName
        ? await onDockerComposeAction(node.id, projectName, action, serviceName)
        : await onDockerComposeAction(node.id, projectName, action)
      if (!result.success) {
        setToast({ message: `${target}${composeActionText(action)}失败: ${result.error || result.output || '未知错误'}`, type: 'error' })
      } else if (action === 'logs') {
        setComposeLogsModal({ projectName, output: result.output || '暂无日志输出' })
      } else if (action === 'validate') {
        setToast({ message: 'Compose 配置校验成功', type: 'success' })
      } else {
        setToast({ message: `${target}${composeActionText(action)}成功`, type: 'success' })
        await onRefreshDockerCompose?.(node.id)
      }
    } catch (error) {
      setToast({ message: `${target}${composeActionText(action)}失败: ${error instanceof Error ? error.message : '网络错误'}`, type: 'error' })
    } finally {
      setComposeActionLoading(undefined)
      setPendingComposeDown(undefined)
    }
  }

  const openManagedComposeEditor = (project?: DockerComposeProject) => {
    const projectID = project?.managed_project_id
    if (project && !projectID) {
      setToast({ message: '托管应用更新失败: 项目标识不可用', type: 'error' })
      return
    }
    setManagedComposePreview(undefined)
    setManagedComposeEditor({ projectID, projectName: project?.display_name || project?.name })
    setManagedComposeDraft({
      projectID,
      displayName: project?.display_name || project?.name || '',
      composeYAML: '',
      envFile: '',
      pullImages: true
    })
  }

  const closeManagedComposeEditor = () => {
    if (deploymentLoading) return
    setManagedComposePreview(undefined)
    setManagedComposeEditor(undefined)
    setManagedComposeDraft(emptyManagedComposeDeploymentDraft())
  }

  const previewManagedComposeDeployment = async () => {
    if (!onDockerComposeDeployment) {
      setToast({ message: '托管应用预览失败: 当前页面未配置部署接口', type: 'error' })
      return
    }
    if (!managedComposeDraft.displayName.trim()) {
      setToast({ message: '托管应用预览失败: 请填写应用名称', type: 'error' })
      return
    }
    if (!managedComposeDraft.composeYAML.trim()) {
      setToast({ message: '托管应用预览失败: 请填写 Compose YAML', type: 'error' })
      return
    }
    const draft = { ...managedComposeDraft }
    const nodeID = node.id
    const requestID = ++composeDeploymentRequestSeq.current
    setDeploymentLoading('preview')
    try {
      const result = await onDockerComposeDeployment(nodeID, deploymentRequestForDraft('preview', draft))
      if (requestID !== composeDeploymentRequestSeq.current || composeDeploymentNodeIDRef.current !== nodeID) return
      if (result.supported === false) {
        setToast({ message: `托管应用预览失败: ${result.error || '当前 Agent 不支持 Compose 应用部署，请升级 Agent'}`, type: 'error' })
        return
      }
      if (!result.success) {
        setToast({ message: `托管应用预览失败: ${result.error || '未知错误'}`, type: 'error' })
        return
      }
      if (!result.confirmation_token) {
        setToast({ message: '托管应用预览失败: Agent 未返回确认令牌', type: 'error' })
        return
      }
      const projectID = result.project?.managed_project_id || draft.projectID
      if (!projectID) {
        setToast({ message: '托管应用预览失败: Agent 未返回项目标识', type: 'error' })
        return
      }
      setManagedComposePreview({
        draft: { ...draft, projectID },
        confirmationToken: result.confirmation_token,
        risks: result.risks || [],
        projectName: result.project?.display_name || result.project?.name || managedComposeEditor?.projectName || draft.displayName
      })
    } catch (error) {
      if (requestID !== composeDeploymentRequestSeq.current || composeDeploymentNodeIDRef.current !== nodeID) return
      setToast({ message: `托管应用预览失败: ${error instanceof Error ? error.message : '网络错误'}`, type: 'error' })
    } finally {
      if (requestID === composeDeploymentRequestSeq.current && composeDeploymentNodeIDRef.current === nodeID) {
        setDeploymentLoading(undefined)
      }
    }
  }

  const applyManagedComposeDeployment = async () => {
    if (!managedComposePreview || !onDockerComposeDeployment) return
    const preview = managedComposePreview
    const nodeID = node.id
    const requestID = ++composeDeploymentRequestSeq.current
    // The apply payload uses the exact previewed draft. Clear the secret from
    // React state before the network request returns.
    setManagedComposePreview(undefined)
    setManagedComposeDraft((draft) => ({ ...draft, envFile: '' }))
    setDeploymentLoading('apply')
    try {
      const result = await onDockerComposeDeployment(nodeID, deploymentRequestForDraft('apply', preview.draft, preview.confirmationToken))
      if (requestID !== composeDeploymentRequestSeq.current || composeDeploymentNodeIDRef.current !== nodeID) return
      if (result.supported === false) {
        setToast({ message: `托管应用部署失败: ${result.error || '当前 Agent 不支持 Compose 应用部署，请升级 Agent'}`, type: 'error' })
        return
      }
      if (!result.success) {
        setToast({ message: `托管应用部署失败: ${result.error || '未知错误'}`, type: 'error' })
        return
      }
      setToast({ message: '托管应用部署成功', type: 'success' })
      setManagedComposeEditor(undefined)
      setManagedComposeDraft(emptyManagedComposeDeploymentDraft())
      await onRefreshDockerCompose?.(nodeID)
    } catch (error) {
      if (requestID !== composeDeploymentRequestSeq.current || composeDeploymentNodeIDRef.current !== nodeID) return
      setToast({ message: `托管应用部署失败: ${error instanceof Error ? error.message : '网络错误'}`, type: 'error' })
    } finally {
      if (requestID === composeDeploymentRequestSeq.current && composeDeploymentNodeIDRef.current === nodeID) {
        setDeploymentLoading(undefined)
        setManagedComposePreview(undefined)
      }
    }
  }

  const confirmManagedComposeAction = async () => {
    if (!pendingManagedComposeAction || !onDockerComposeDeployment) return
    const { action, project } = pendingManagedComposeAction
    const projectID = project.managed_project_id
    const actionText = action === 'rollback' ? '回滚' : '归档'
    if (!projectID) {
      setToast({ message: `托管应用${actionText}失败: 项目标识不可用`, type: 'error' })
      setPendingManagedComposeAction(undefined)
      return
    }
    const nodeID = node.id
    const requestID = ++composeDeploymentRequestSeq.current
    setDeploymentLoading(action)
    try {
      const result = await onDockerComposeDeployment(nodeID, { action, project_id: projectID })
      if (requestID !== composeDeploymentRequestSeq.current || composeDeploymentNodeIDRef.current !== nodeID) return
      if (result.supported === false) {
        setToast({ message: `托管应用${actionText}失败: ${result.error || '当前 Agent 不支持 Compose 应用部署，请升级 Agent'}`, type: 'error' })
        return
      }
      if (!result.success) {
        setToast({ message: `托管应用${actionText}失败: ${result.error || '未知错误'}`, type: 'error' })
        return
      }
      setToast({ message: `托管应用${actionText}成功`, type: 'success' })
      await onRefreshDockerCompose?.(nodeID)
    } catch (error) {
      if (requestID !== composeDeploymentRequestSeq.current || composeDeploymentNodeIDRef.current !== nodeID) return
      setToast({ message: `托管应用${actionText}失败: ${error instanceof Error ? error.message : '网络错误'}`, type: 'error' })
    } finally {
      if (requestID === composeDeploymentRequestSeq.current && composeDeploymentNodeIDRef.current === nodeID) {
        setDeploymentLoading(undefined)
        setPendingManagedComposeAction(undefined)
      }
    }
  }

  const runSystemdAction = async (serviceName: string, action: SystemdServiceAction) => {
    if (!node || !onSystemdServiceAction) return
    setSystemdActionLoading(`${serviceName}:${action}`)
    try {
      const result = await onSystemdServiceAction(node.id, serviceName, action)
      if (!result.success) {
        setToast({ message: `系统服务${serviceName}${systemdActionText(action)}失败: ${result.error || result.output || '未知错误'}`, type: 'error' })
      } else if (action === 'logs') {
        setSystemdLogsModal({ serviceName, output: result.output || '暂无日志输出' })
      } else {
        setToast({ message: `系统服务${serviceName}${systemdActionText(action)}成功`, type: 'success' })
        await onRefreshSystemdServices?.(node.id)
      }
    } catch (error) {
      setToast({ message: `系统服务${serviceName}${systemdActionText(action)}失败: ${error instanceof Error ? error.message : '网络错误'}`, type: 'error' })
    } finally {
      setSystemdActionLoading(undefined)
    }
  }

  return (
    <section className="min-w-0 space-y-2">
      <div className="rounded-[14px] border border-border bg-card p-3 shadow-sm">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="truncate font-display text-2xl font-black tracking-tight text-foreground">{displayName}</h2>
              <span className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-black ${online ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'}`}>{online ? '在线' : '离线'}</span>
            </div>
            <p className="mt-2 text-xs font-semibold text-muted-foreground">
              {node.ip || '未知 IP'} · {node.os}/{node.arch} · {node.hostname || '未知主机'} · 运行时间 {uptimeText}
            </p>
            <p className="mt-1 text-xs font-black text-muted-foreground">{agentModeLabel} · {agentUserLabel}</p>
          </div>
          <div role="toolbar" aria-label="节点操作" className="flex flex-wrap justify-start gap-2 lg:justify-end">
            <button
              type="button"
              aria-label="打开终端"
              title={node.terminal_enabled ? '打开终端' : '该节点未启用终端'}
              disabled={!node.terminal_enabled}
              onClick={() => openTerminalPage(node.id)}
              className="inline-flex min-h-9 cursor-pointer items-center gap-2 rounded-xl border border-success/30 bg-success px-3 text-xs font-black text-primary-foreground shadow-sm transition hover:brightness-95 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground disabled:shadow-none"
            >
              <TerminalIcon />
              终端
            </button>
            <button
              type="button"
              aria-label="分组与标签"
              onClick={() => setOrganizationEditorOpen(true)}
              className="soft-button inline-flex min-h-9 cursor-pointer items-center gap-2 border border-border bg-card px-3 text-xs font-black text-foreground shadow-sm transition hover:bg-muted focus:outline-none focus:ring-4 focus:ring-primary/20"
            >
              <Tags size={14} aria-hidden="true" />
              分组与标签
            </button>
            <button
              type="button"
              aria-label="重启"
              title="重启节点"
				disabled={!online || rebootLoading}
				onClick={() => setRebootOpen(true)}
              className="inline-flex min-h-9 cursor-pointer items-center gap-2 rounded-xl border border-warning/30 bg-warning/10 px-3 text-xs font-black text-warning shadow-sm transition hover:bg-warning/15 focus:outline-none focus:ring-4 focus:ring-warning/20 disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground"
            >
              <PowerIcon />
              重启
            </button>
          </div>
        </div>
      </div>

      {operationMessage && activeSection !== 'files' ? <p className="rounded-[28px] border border-warning/30 bg-warning/10 px-4 py-3 text-sm font-black text-warning">{operationMessage}</p> : null}

      <div className="flex flex-wrap gap-1 rounded-[14px] border border-border bg-card px-2 py-1.5 shadow-sm" role="group" aria-label="节点详情视图">
        {([
          ['overview', '主机信息'],
          ['processes', '进程信息'],
          ['containers', '容器信息'],
          ['services', '系统服务'],
          ['files', '文件管理'],
          ['logs', '日志查看'],
          ['agent', 'Agent 管理']
        ] as const).map(([section, label]) => (
          <button
            key={section}
            type="button"
            aria-pressed={activeSection === section}
            onClick={() => section === 'files' ? loadFiles(fileList?.path || '/') : section === 'agent' ? loadAgentManagement() : setActiveSection(section)}
            className={`min-h-9 cursor-pointer rounded-xl px-3 text-xs font-black transition focus:outline-none focus:ring-4 focus:ring-primary/20 ${activeSection === section ? 'bg-primary/10 text-primary shadow-sm' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
          >
            {label}
          </button>
        ))}
      </div>

      {activeSection === 'overview' ? (
        <>
          <section aria-label="基础信息" className="rounded-[14px] border border-border bg-card p-3 shadow-sm">
            <div className="mb-3 flex items-center justify-between gap-3">
              <h3 className="text-base font-black text-foreground">基础信息</h3>
              <span className="rounded-full bg-muted px-2.5 py-1 text-[11px] font-black text-muted-foreground">最新采样</span>
            </div>
            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              <InfoBlock label="操作系统" value={node.os || '未知'} />
              <InfoBlock label="内核版本" value={node.kernel || '未知'} />
              <InfoBlock label="架构" value={node.arch || '未知'} />
              <InfoBlock label="启动时间" value={bootTimeText} />
              <InfoBlock label="运行时间" value={uptimeText} />
              <InfoBlock label="系统负载" value={formatLoadSummary(metric)} wrap />
            </div>
          </section>

          <div data-testid="node-detail-charts" className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            <MetricsChart metrics={filterMetricsByChartRange(metrics, chartRanges.cpu)} dataKey="cpu_usage" title="CPU 使用率" color="rgb(var(--chart-cpu))" summaryItems={[{ value: latestChartMetric ? formatPercent(latestChartMetric.cpu_usage) : '—' }]} range={chartRanges.cpu} onRangeChange={(nextRange) => updateChartRange('cpu', nextRange, range, onRangeChange, setChartRanges)} />
            <MetricsChart metrics={filterMetricsByChartRange(metrics, chartRanges.memory)} dataKey="memory_usage" title="内存使用率" color="rgb(var(--chart-memory))" summaryItems={[{ value: latestChartMetric ? formatPercent(latestChartMetric.memory_usage) : '—' }]} range={chartRanges.memory} onRangeChange={(nextRange) => updateChartRange('memory', nextRange, range, onRangeChange, setChartRanges)} />
            <MetricsChart metrics={filterMetricsByChartRange(metrics, chartRanges.disk)} dataKey="disk_usage" title="磁盘使用率" color="rgb(var(--chart-disk))" summaryItems={[{ value: latestChartMetric ? formatPercent(latestChartMetric.disk_usage) : '—' }]} range={chartRanges.disk} onRangeChange={(nextRange) => updateChartRange('disk', nextRange, range, onRangeChange, setChartRanges)} />
            <MetricsChart metrics={filterMetricsByChartRange(metrics, chartRanges.network)} title="网络 I/O" color="rgb(var(--chart-network-in))" unitLabel="bytes/s" domain={[0, 'auto']} summaryItems={[{ label: '上行', value: latestChartMetric ? formatSpeed(latestChartMetric.tx_speed) : '—', color: 'rgb(var(--chart-network-out))' }, { label: '下行', value: latestChartMetric ? formatSpeed(latestChartMetric.rx_speed) : '—', color: 'rgb(var(--chart-network-in))' }]} range={chartRanges.network} onRangeChange={(nextRange) => updateChartRange('network', nextRange, range, onRangeChange, setChartRanges)} series={[{ dataKey: 'rx_speed', label: '下行', color: 'rgb(var(--chart-network-in))', unitLabel: 'bytes/s' }, { dataKey: 'tx_speed', label: '上行', color: 'rgb(var(--chart-network-out))', unitLabel: 'bytes/s' }]} />
            <MetricsChart metrics={filterMetricsByChartRange(metrics, chartRanges.diskIO)} title="磁盘 I/O" color="rgb(var(--chart-disk))" unitLabel="bytes/s" domain={[0, 'auto']} summaryItems={[{ label: '读', value: latestChartMetric ? formatSpeed(latestChartMetric.disk_read_speed) : '—', color: 'rgb(var(--chart-network-out))' }, { label: '写', value: latestChartMetric ? formatSpeed(latestChartMetric.disk_write_speed) : '—', color: 'rgb(var(--chart-network-in))' }]} range={chartRanges.diskIO} onRangeChange={(nextRange) => updateChartRange('diskIO', nextRange, range, onRangeChange, setChartRanges)} series={[{ dataKey: 'disk_read_speed', label: '读', color: 'rgb(var(--chart-network-out))', unitLabel: 'bytes/s' }, { dataKey: 'disk_write_speed', label: '写', color: 'rgb(var(--chart-network-in))', unitLabel: 'bytes/s' }]} emptyText="当前 Agent 暂未上报磁盘 I/O 指标" />
            <MetricsChart metrics={filterMetricsByChartRange(metrics, chartRanges.load)} title="系统负载" color="rgb(var(--chart-load))" unitLabel="load" domain={[0, 'auto']} summaryItems={[{ label: '1m', value: latestChartMetric ? latestChartMetric.load1.toFixed(2) : '—', color: 'rgb(var(--chart-load))' }, { label: '5m', value: latestChartMetric ? latestChartMetric.load5.toFixed(2) : '—', color: 'rgb(var(--chart-memory))' }, { label: '15m', value: latestChartMetric ? latestChartMetric.load15.toFixed(2) : '—', color: 'rgb(var(--chart-network-out))' }]} range={chartRanges.load} onRangeChange={(nextRange) => updateChartRange('load', nextRange, range, onRangeChange, setChartRanges)} series={[{ dataKey: 'load1', label: 'Load 1m', color: 'rgb(var(--chart-load))' }, { dataKey: 'load5', label: 'Load 5m', color: 'rgb(var(--chart-memory))' }, { dataKey: 'load15', label: 'Load 15m', color: 'rgb(var(--chart-network-out))' }]} />
          </div>
        </>
      ) : null}

      {activeSection === 'processes' ? (
        <section aria-label="进程 Top" className="overflow-hidden rounded-[28px] border border-border bg-card shadow-sm">
          <div className="flex flex-col gap-3 border-b border-border bg-surface p-4 lg:flex-row lg:items-center lg:justify-between">
            <div>
              <p className="text-[11px] font-black uppercase tracking-[0.22em] text-success">Process Snapshot</p>
              <h3 className="mt-1 text-lg font-black text-foreground">进程 Top</h3>
              <p className="mt-1 text-xs font-bold text-muted-foreground">采样时间：{formatUnixTime(processSnapshot?.collected_at)}</p>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <div className="flex rounded-2xl border border-border bg-card p-1 shadow-inner">
                {([
                  ['cpu', '按 CPU 排序'],
                  ['memory', '按内存排序'],
                  ['pid', '按 PID 排序'],
                  ['name', '按名称排序']
                ] as const).map(([sort, label]) => (
                  <button
                    key={sort}
                    type="button"
                    aria-pressed={processSort === sort}
                    onClick={() => setProcessSort(sort)}
                    className={`min-h-9 cursor-pointer rounded-xl px-3 text-xs font-black transition focus:outline-none focus:ring-4 focus:ring-primary/20 ${processSort === sort ? 'bg-slate-950 text-white' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <input
                aria-label="搜索进程"
                value={processSearch}
                onChange={(event) => setProcessSearch(event.target.value)}
                placeholder="搜索进程名、PID 或用户"
                className="min-h-10 rounded-2xl border border-border bg-card px-4 text-sm font-semibold text-foreground outline-none placeholder:text-muted-foreground focus:border-emerald-400 focus:ring-4 focus:ring-primary/20"
              />
            </div>
          </div>
          <MonitoringState loading={monitoringLoading} error={processSnapshot?.error} empty={!monitoringLoading && filteredProcesses.length === 0} emptyText={processSnapshot?.processes.length ? '当前筛选条件下没有进程。' : '暂无进程快照，等待 Agent 下一次上报。'} />
          {filteredProcesses.length > 0 ? <ProcessTable processes={filteredProcesses} /> : null}
        </section>
      ) : null}

      {activeSection === 'containers' ? (
        <section aria-label={dockerView === 'containers' ? 'Docker 容器' : dockerView === 'compose' ? 'Docker Compose' : 'Docker 资源'} className="overflow-hidden rounded-[28px] border border-border bg-card shadow-sm">
          <div className="border-b border-border bg-surface p-4">
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0">
              <p className="text-[11px] font-black uppercase tracking-[0.22em] text-cyan-500">{dockerView === 'containers' ? 'Docker Snapshot' : dockerView === 'compose' ? 'Compose Projects' : 'Docker Resources'}</p>
              <h3 className="mt-1 text-lg font-black text-foreground">{dockerView === 'containers' ? 'Docker 容器' : dockerView === 'compose' ? 'Compose 项目' : 'Docker 资源'}</h3>
              <p className="mt-1 text-xs font-bold text-muted-foreground">
                {dockerView === 'containers'
                  ? (dockerSnapshot?.available ? `Docker ${dockerSnapshot.version || '版本未知'} · ${formatUnixTime(dockerSnapshot.collected_at)}` : 'Docker 状态随 Agent 快照展示')
                  : dockerView === 'compose' ? '实时读取 Agent 所在主机的 Docker Compose 项目' : '集中查看镜像、数据卷、网络与 Docker 磁盘占用'}
              </p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <div className="flex shrink-0 rounded-2xl border border-border bg-card p-1 shadow-inner" role="tablist" aria-label="Docker 视图">
                  <button type="button" role="tab" aria-selected={dockerView === 'containers'} onClick={() => setDockerView('containers')} className={`min-h-9 whitespace-nowrap rounded-xl px-3 text-xs font-black transition ${dockerView === 'containers' ? 'bg-slate-950 text-white' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}>容器</button>
                  <button type="button" role="tab" aria-selected={dockerView === 'compose'} onClick={() => setDockerView('compose')} className={`min-h-9 whitespace-nowrap rounded-xl px-3 text-xs font-black transition ${dockerView === 'compose' ? 'bg-slate-950 text-white' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}>Compose</button>
                  <button type="button" role="tab" aria-selected={dockerView === 'resources'} onClick={() => setDockerView('resources')} className={`min-h-9 whitespace-nowrap rounded-xl px-3 text-xs font-black transition ${dockerView === 'resources' ? 'bg-slate-950 text-white' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}>资源</button>
                </div>
                {dockerView === 'compose' ? (
                <button type="button" aria-label="刷新 Compose 项目" title="刷新 Compose 项目" onClick={() => {
                  if (!node || !onRefreshDockerCompose) return
                  setComposeLoading(true)
                  void onRefreshDockerCompose(node.id).finally(() => setComposeLoading(false))
                }} disabled={!online || Boolean(composeActionLoading) || composeLoading} className="inline-flex h-10 w-10 items-center justify-center rounded-2xl border border-border bg-card text-muted-foreground transition hover:border-primary/30 hover:bg-primary/10 hover:text-primary focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60"><RotateCw size={16} aria-hidden="true" /></button>
                ) : null}
              </div>
            </div>
            {dockerView === 'containers' ? (
              <div data-testid="docker-container-toolbar" className="mt-4 flex flex-wrap items-center gap-2 border-t border-border/70 pt-3">
                <div className="flex shrink-0 rounded-2xl border border-border bg-card p-1 shadow-inner">
                  {([
                    ['all', '全部'],
                    ['running', '运行中'],
                    ['stopped', '已停止'],
                    ['abnormal', '异常']
                  ] as const).map(([filter, label]) => (
                    <button
                      key={filter}
                      type="button"
                      aria-pressed={dockerFilter === filter}
                      onClick={() => setDockerFilter(filter)}
                      className={`min-h-9 cursor-pointer whitespace-nowrap rounded-xl px-3 text-xs font-black transition focus:outline-none focus:ring-4 focus:ring-cyan-100 ${dockerFilter === filter ? 'bg-slate-950 text-white' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                <input
                  aria-label="搜索容器"
                  value={dockerSearch}
                  onChange={(event) => setDockerSearch(event.target.value)}
                  placeholder="搜索容器名、镜像或 ID"
                  className="min-h-10 min-w-[240px] flex-1 rounded-2xl border border-border bg-card px-4 text-sm font-semibold text-foreground outline-none placeholder:text-muted-foreground focus:border-cyan-400 focus:ring-4 focus:ring-cyan-100"
                />
                <button
                  type="button"
                  aria-label="刷新容器列表"
                  title="刷新容器列表"
                  onClick={() => node && onRefreshDocker?.(node.id)}
                  disabled={!online}
                  className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border border-border bg-card text-muted-foreground transition hover:border-primary/30 hover:bg-primary/10 hover:text-primary focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  <RotateCw size={16} aria-hidden="true" />
                </button>
                <button
                  type="button"
                  aria-label="创建容器"
                  title="创建容器"
                  onClick={() => setCreateContainerModal(true)}
                  disabled={!online}
                  className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border border-primary/30 bg-primary/10 text-primary transition hover:bg-primary/15 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  <Plus size={17} aria-hidden="true" />
                </button>
              </div>
            ) : null}
          </div>

          {dockerView === 'containers' ? <>
          {/* containerd 提示 */}
          <div className="mx-4 mt-4 rounded-xl border border-warning/30 bg-warning/5 p-3">
            <div className="flex items-start gap-2">
              <svg viewBox="0 0 24 24" className="h-5 w-5 shrink-0 text-warning" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M12 9v4M12 17h.01M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20z"/>
              </svg>
              <div className="min-w-0 flex-1">
                <p className="text-xs font-bold text-foreground">仅支持 Docker 容器运行时</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  暂不支持 containerd 容器运行时。如需查看 Kubernetes Pod 信息，请前往 K8s 集群详情页面。
                </p>
              </div>
            </div>
          </div>

          {!dockerSnapshot?.available ? (
            <div className="m-4 rounded-2xl border border-dashed border-border bg-surface px-4 py-3 text-sm font-bold text-muted-foreground">
              {formatDockerUnavailableMessage(dockerSnapshot?.error, monitoringLoading)}
            </div>
          ) : null}
          {dockerSnapshot?.available ? <MonitoringState loading={monitoringLoading} error={dockerSnapshot.error} empty={!monitoringLoading && filteredContainers.length === 0} emptyText="当前筛选条件下没有容器。" /> : null}
          {dockerSnapshot?.available && filteredContainers.length > 0 ? (
            <DockerTable
              nodeID={node.id}
              containers={filteredContainers}
              onOpenLogs={(containerId, containerName) => {
                setContainerLogsModal({ open: true, containerId, containerName })
              }}
              onRefresh={async () => {
                if (onRefreshDocker) {
                  await onRefreshDocker(node.id)
                }
              }}
              onShowToast={(message, type) => {
                setToast({ message, type })
              }}
            />
          ) : null}
          </> : dockerView === 'compose' ? (
            <DockerComposePanel
              response={dockerCompose}
              loading={composeLoading}
              online={online}
              actionLoading={composeActionLoading}
              deploymentLoading={deploymentLoading}
              onAction={(projectName, action, serviceName) => {
                if (action === 'down') {
                  setPendingComposeDown(projectName)
                  return
                }
                void runComposeAction(projectName, action, serviceName)
              }}
              onCreateManaged={() => openManagedComposeEditor()}
              onUpdateManaged={openManagedComposeEditor}
              onManagedAction={(project, action) => setPendingManagedComposeAction({ project, action })}
              onOpenLogs={(containerID, containerName) => setContainerLogsModal({ open: true, containerId: containerID, containerName })}
              onOpenTerminal={(containerID) => openContainerExecPage(node.id, containerID)}
            />
          ) : (
            <DockerResourcesPanel
              key={node.id}
              response={dockerResources}
              loading={resourcesLoading}
              online={online}
              onRefresh={async () => {
                if (!onRefreshDockerResources) return
                setResourcesLoading(true)
                try {
                  await onRefreshDockerResources(node.id)
                } finally {
                  setResourcesLoading(false)
                }
              }}
              onAction={async (resourceType, resourceID, action) => {
                if (!onDockerResourceAction) throw new Error('Docker 资源操作不可用')
                return onDockerResourceAction(node.id, resourceType, resourceID, action)
              }}
              onShowToast={(message, type) => setToast({ message, type })}
            />
          )}
        </section>
      ) : null}

      {activeSection === 'services' ? (
        <SystemdServicesPanel
          response={systemdServices}
          loading={systemdLoading}
          online={online}
          actionLoading={systemdActionLoading}
          initialSearch={detailSearch}
          onRefresh={() => {
            if (!node || !onRefreshSystemdServices) return
            setSystemdLoading(true)
            void onRefreshSystemdServices(node.id).finally(() => setSystemdLoading(false))
          }}
          onAction={(serviceName, action) => void runSystemdAction(serviceName, action)}
        />
      ) : null}

      {activeSection === 'files' ? (
        <section role="region" aria-label="文件管理" className="overflow-hidden rounded-[28px] border border-border bg-card shadow-sm">
          <div className="flex flex-col gap-3 border-b border-border bg-surface p-4 lg:flex-row lg:items-center lg:justify-between">
            <div className="min-w-0">
              <p className="text-[11px] font-black uppercase tracking-[0.22em] text-success">File Manager</p>
              <h3 className="mt-1 text-lg font-black text-foreground">文件管理</h3>
              <p className="mt-1 break-all text-xs font-bold text-muted-foreground">当前路径：{currentFilePath}</p>
            </div>
            <div className="flex flex-wrap gap-2">
              <input
                aria-label="直接打开路径"
                value={pathInput}
                onChange={(event) => setPathInput(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') openPath()
                }}
                className="min-h-10 w-64 rounded-2xl border border-border bg-card px-4 text-xs font-bold text-foreground outline-none placeholder:text-muted-foreground focus:border-emerald-400 focus:ring-4 focus:ring-primary/20"
              />
              <button type="button" onClick={openPath} className="min-h-10 rounded-2xl bg-success px-4 text-xs font-black text-white transition hover:brightness-95">打开路径</button>
              <button type="button" onClick={() => loadFiles(parentPath(currentFilePath))} className="min-h-10 rounded-2xl border border-border bg-card px-4 text-xs font-black text-muted-foreground transition hover:text-foreground">返回上级</button>
              <button type="button" onClick={() => loadFiles(currentFilePath)} className="min-h-10 rounded-2xl bg-success px-4 text-xs font-black text-white transition hover:brightness-95">刷新</button>
            </div>
          </div>
          {operationMessage && !editorOpen ? <p className="m-4 rounded-2xl border border-warning/30 bg-warning/10 px-4 py-3 text-sm font-black text-warning">{operationMessage}</p> : null}
          {fileLoading ? <p className="m-4 rounded-2xl border border-info/30 bg-info/10 px-4 py-3 text-sm font-black text-info">正在加载目录...</p> : null}
          <input
            ref={uploadInputRef}
            type="file"
            aria-label="上传文件"
            onChange={(event) => {
              uploadFiles(event.target.files || undefined)
              event.target.value = ''
            }}
            className="sr-only"
          />
          <div className="grid min-w-0 gap-4 p-4 xl:grid-cols-[260px_minmax(0,1fr)]">
            <aside className="min-w-0 rounded-2xl border border-border bg-surface/70 p-3">
              <div className="mb-3 flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <p className="text-[11px] font-black uppercase tracking-[0.18em] text-muted-foreground">Path Tree</p>
                  <p className="mt-1 text-sm font-black text-foreground">路径树</p>
                </div>
                <button type="button" onClick={() => togglePathTree('/')} disabled={!online} className="rounded-xl border border-border bg-card px-2.5 py-1.5 text-xs font-black text-muted-foreground transition hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60">展开 /</button>
              </div>
              <div className="max-h-[520px] space-y-1 overflow-auto pr-1">
                {FILE_TREE_ROOTS.map((path) => (
                  <PathTreeBranch
                    key={path}
                    path={path}
                    level={path === '/' ? 0 : 1}
                    tree={pathTree}
                    currentPath={currentFilePath}
                    disabled={!online}
                    onOpen={loadFiles}
                    onToggle={togglePathTree}
                  />
                ))}
              </div>
            </aside>

            <div className="min-w-0 space-y-4">
              <button
                type="button"
                onClick={() => uploadInputRef.current?.click()}
                onDragEnter={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  if (online) setDragActive(true)
                }}
                onDragOver={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  if (online) setDragActive(true)
                }}
                onDragLeave={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  setDragActive(false)
                }}
                onDrop={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  setDragActive(false)
                  if (!online) {
                    setOperationMessage('节点离线，无法上传文件。')
                    return
                  }
                  uploadFiles(event.dataTransfer.files)
                }}
                disabled={!online || !onUploadFile}
                className={`flex min-h-28 w-full cursor-pointer flex-col items-center justify-center rounded-3xl border border-dashed px-4 py-5 text-center transition focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60 ${dragActive ? 'border-success bg-success/10' : 'border-success/30 bg-success/5 hover:bg-success/10'}`}
              >
                <UploadIcon />
                <span className="mt-2 text-sm font-black text-foreground">拖拽文件到这里上传</span>
                <span className="mt-1 text-xs font-bold text-muted-foreground">或点击选择文件，上传到 {currentFilePath}</span>
              </button>

              <div className="overflow-hidden rounded-2xl border border-border bg-card">
                <div className="flex items-center justify-between gap-3 border-b border-border bg-surface px-4 py-3">
                  <div className="min-w-0">
                    <p className="text-sm font-black text-foreground">目录内容</p>
                    <p className="mt-0.5 text-xs font-bold text-muted-foreground">{(fileList?.entries ?? []).length} 项 · {currentFilePath}</p>
                  </div>
                </div>
                {(fileList?.entries ?? []).length === 0 && !fileLoading ? (
                  <div className="m-4 rounded-2xl border border-dashed border-border bg-surface px-4 py-8 text-center">
                    <FileIcon />
                    <p className="mt-2 text-sm font-black text-foreground">当前目录为空</p>
                    <p className="mt-1 text-xs font-bold text-muted-foreground">可以拖拽文件到上方区域上传。</p>
                  </div>
                ) : null}
                <ul className="divide-y divide-border">
                  {(fileList?.entries ?? []).map((entry) => {
                    const isPendingDelete = pendingDelete?.path === entry.path
                    return (
                      <li key={entry.path || entry.name} className="px-4 py-3 text-sm transition hover:bg-surface">
                        <div className="flex items-center justify-between gap-3">
                          <div className="flex min-w-0 items-center gap-3">
                            <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl ${entry.type === 'directory' ? 'bg-success/10 text-success' : entry.type === 'binary' ? 'bg-muted text-muted-foreground' : 'bg-primary/10 text-primary'}`}>
                              {entry.type === 'directory' ? <FolderIcon /> : entry.type === 'binary' ? <LockIcon /> : <DocumentIcon />}
                            </span>
                            <div className="min-w-0">
                              <p className="truncate font-black text-foreground" title={entry.path}>{entry.name}</p>
                              <p className="mt-1 text-xs font-bold text-muted-foreground">{fileTypeLabel(entry.type)}{entry.size ? ` · ${formatBytes(entry.size)}` : ''}</p>
                            </div>
                          </div>
                          <div className="flex shrink-0 flex-wrap justify-end gap-2">
                            {entry.type === 'directory' ? (
                              <button type="button" aria-label={`进入目录 ${entry.name}`} onClick={() => openFileEntry(entry)} className="rounded-2xl bg-success px-3 py-2 text-xs font-black text-white transition hover:brightness-95">进入</button>
                            ) : entry.type === 'binary' ? (
                              <span className="rounded-2xl bg-muted px-3 py-2 text-xs font-black text-muted-foreground">不可编辑</span>
                            ) : (
                              <button type="button" aria-label={`编辑文件 ${entry.name}`} onClick={() => openFileEntry(entry)} className="rounded-2xl bg-primary px-3 py-2 text-xs font-black text-primary-foreground transition hover:brightness-110">编辑</button>
                            )}
                            <button type="button" aria-label={`删除 ${entry.name}`} onClick={() => confirmDeleteEntry(entry)} className="rounded-2xl border border-danger/30 bg-danger/10 px-3 py-2 text-xs font-black text-danger transition hover:bg-danger/15">删除</button>
                          </div>
                        </div>
                        {isPendingDelete ? (
                          <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-2xl border border-danger/30 bg-danger/10 px-3 py-2">
                            <p className="text-xs font-black text-danger">确认删除 {entry.name}？非空目录不会递归删除。</p>
                            <div className="flex gap-2">
                              <button type="button" onClick={cancelDeleteEntry} className="rounded-xl border border-border bg-card px-3 py-1.5 text-xs font-black text-muted-foreground transition hover:text-foreground">取消</button>
                              <button type="button" onClick={() => deleteEntry(entry)} className="rounded-xl bg-danger px-3 py-1.5 text-xs font-black text-white transition hover:brightness-95">确认删除</button>
                            </div>
                          </div>
                        ) : null}
                      </li>
                    )
                  })}
                </ul>
                {fileList?.truncated ? <p className="border-t border-warning/30 bg-warning/10 px-4 py-2 text-xs font-bold text-warning">目录过大，仅显示前部分结果。</p> : null}
              </div>
            </div>
          </div>
        </section>
      ) : null}

      {activeSection === 'logs' ? (
        <section role="region" aria-label="日志查看" className="h-[600px] overflow-hidden rounded-[28px] border border-border bg-card shadow-sm">
          <div className="flex h-full flex-col p-4">
            <div className="mb-4">
              <p className="text-[11px] font-black uppercase tracking-[0.22em] text-primary">Log Viewer</p>
              <h3 className="mt-1 text-lg font-black text-foreground">日志查看</h3>
              <p className="mt-1 text-xs font-bold text-muted-foreground">实时查看节点日志文件内容</p>
            </div>
            <div className="flex-1 overflow-hidden">
              {node ? <LogViewer nodeId={node.id} /> : null}
            </div>
          </div>
        </section>
      ) : null}

      {activeSection === 'agent' ? (
        <section role="region" aria-label="Agent 管理" className="overflow-hidden rounded-[28px] border border-border bg-card shadow-sm">
          <div className="flex flex-col gap-3 border-b border-border bg-surface p-4 lg:flex-row lg:items-center lg:justify-between">
            <div>
              <p className="text-[11px] font-black uppercase tracking-[0.22em] text-success">Agent Management</p>
              <h3 className="mt-1 text-lg font-black text-foreground">Agent 管理</h3>
              <p className="mt-1 text-xs font-bold text-muted-foreground">单节点 Agent 状态、重启与最近日志。</p>
            </div>
            <div className="flex flex-wrap gap-2">
              <button type="button" onClick={loadAgentManagement} disabled={!online || agentLoading} className="min-h-10 rounded-2xl border border-border bg-card px-4 text-xs font-black text-muted-foreground transition hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60">刷新状态</button>
              <button type="button" onClick={refreshAgentLogs} disabled={!online || agentLoading} className="min-h-10 rounded-2xl border border-success/30 bg-success/10 px-4 text-xs font-black text-success transition hover:bg-success/15 disabled:cursor-not-allowed disabled:opacity-60">刷新日志</button>
              {connectionDiagnostics?.upgrade_available && connectionDiagnostics.upgrade_supported ? <button type="button" onClick={() => setUpgradeOpen(true)} disabled={!online || upgradeLoading} className="min-h-10 rounded-2xl bg-primary px-4 text-xs font-black text-primary-foreground shadow-sm transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-60">升级 Agent</button> : null}
								{connectionDiagnostics?.upgrade_available && !connectionDiagnostics.upgrade_supported ? <button type="button" onClick={copyLegacyAgentUpgradeCommand} disabled={!onGetLegacyAgentUpgradeCommand || legacyUpgradeCopying} className="inline-flex min-h-10 items-center gap-2 rounded-2xl border border-warning/30 bg-warning/10 px-4 text-xs font-black text-warning transition hover:bg-warning/15 disabled:cursor-not-allowed disabled:opacity-60"><Copy size={14} aria-hidden="true" />{legacyUpgradeCopying ? '正在生成...' : '复制升级命令'}</button> : null}
								<button type="button" aria-label="重启 Agent" onClick={() => setRestartOpen(true)} disabled={!online || restartLoading} className="min-h-10 rounded-2xl border border-warning/30 bg-warning/10 px-4 text-xs font-black text-warning transition hover:bg-warning/15 disabled:cursor-not-allowed disabled:opacity-60">重启 Agent</button>
              <button type="button" aria-label="卸载 Agent" title="通过 SSH 卸载远端 Agent" onClick={openSSHUninstallDialog} className="min-h-10 rounded-2xl border border-danger/30 bg-danger/10 px-4 text-xs font-black text-danger transition hover:bg-danger/15 focus:outline-none focus:ring-4 focus:ring-danger/20">卸载 Agent</button>
            </div>
          </div>
          {agentMessage ? <p className="m-4 rounded-2xl border border-success/30 bg-success/10 px-4 py-3 text-sm font-black text-success">{agentMessage}</p> : null}
          {agentError ? <p className="m-4 rounded-2xl border border-warning/30 bg-warning/10 px-4 py-3 text-sm font-black text-warning">{agentError}</p> : null}
          {agentLoading ? <p className="m-4 rounded-2xl border border-info/30 bg-info/10 px-4 py-3 text-sm font-black text-info">正在加载 Agent 管理信息...</p> : null}
          {connectionDiagnostics ? (
            <div className="mx-4 mt-4 rounded-3xl border border-border bg-surface/70 p-4">
              {(() => {
                const versionState = connectionAgentVersionState(connectionDiagnostics)
                return (
                  <>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <p className="text-sm font-black text-foreground">连接诊断</p>
                  <p className="mt-1 text-xs font-bold text-muted-foreground">稳定身份、Agent 版本与当前连接健康</p>
                </div>
                <span className={`soft-chip inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-black ${connectionDiagnostics.identity_conflict ? 'bg-danger/10 text-danger' : connectionDiagnostics.online ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'}`}>
                  {connectionDiagnostics.identity_conflict ? <ShieldAlert size={14} /> : connectionDiagnostics.online ? <Wifi size={14} /> : <WifiOff size={14} />}
                  {connectionDiagnostics.identity_conflict ? '身份冲突' : connectionDiagnostics.online ? '连接正常' : '当前离线'}
                </span>
              </div>
              <div className="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
                <InfoBlock label="Agent ID" value={connectionDiagnostics.node_id} wrap />
                <div className="rounded-2xl border border-border bg-card px-3 py-2.5">
                  <p className="text-[11px] font-black uppercase tracking-[0.14em] text-muted-foreground">Agent 状态</p>
                  <span className={`mt-2 inline-flex items-center gap-1.5 rounded-xl px-2.5 py-1 text-xs font-black ${versionState.tone}`}>
                    {versionState.kind === 'current' ? <Wifi size={13} /> : <ShieldAlert size={13} />}
                    {versionState.label}
                  </span>
                </div>
                <InfoBlock label="Agent 版本" value={connectionDiagnostics.agent_version || node.agent_version || '未知'} />
                <InfoBlock label="最后心跳" value={formatDateTime(connectionDiagnostics.last_heartbeat_at)} />
              </div>
                  </>
                )
              })()}
            </div>
          ) : null}
          <div className="grid gap-3 p-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-1">
              <InfoBlock label="服务名称" value={agentStatus?.service_name || 'mizupanel-agent'} />
              <InfoBlock label="Agent 版本" value={agentStatus?.version || node.agent_version || '未知'} />
              <InfoBlock label="运行用户" value={agentStatus?.user || node.agent_user || '未知'} />
              <InfoBlock label="运行模式" value={formatAgentMode(agentStatus?.mode || node.agent_mode)} />
              <InfoBlock label="运行时间" value={formatUptime(agentStatus?.uptime)} />
              <InfoBlock label="配置路径" value={agentStatus?.config_path || '暂未上报'} wrap />
              <InfoBlock label="终端能力" value={agentStatus?.terminal_enabled ? '已启用' : '未启用'} />
              <InfoBlock label="Docker 能力" value={agentStatus?.docker_available ? '可用' : agentStatus?.docker_error || '不可用'} wrap />
            </div>
            <div className="min-w-0 rounded-2xl border border-border bg-slate-950 p-3 text-slate-100 shadow-inner">
              <div className="mb-2 flex items-center justify-between gap-3">
                <p className="text-xs font-black uppercase tracking-[0.18em] text-slate-400">Recent Logs · {agentLogs?.lines || 100} lines</p>
                <span className="text-xs font-bold text-slate-500">{formatUnixTime(agentLogs?.collected_at)}</span>
              </div>
              {agentLogs?.truncated ? <p className="mb-2 rounded-xl bg-warning/20 px-3 py-2 text-xs font-black text-warning">日志内容较长，已截断显示。</p> : null}
              <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-xl bg-code px-3 py-3 font-mono text-xs font-semibold leading-5 text-code-foreground">{agentLogs?.content || '暂无 Agent 日志，点击刷新日志后查看。'}</pre>
            </div>
          </div>
        </section>
      ) : null}

      {upgradeOpen && connectionDiagnostics ? (
        <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4">
          <section role="dialog" aria-modal="true" aria-label="升级 Agent" className="soft-modal-shell w-full max-w-lg">
            <div className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4">
              <div><h3 className="text-base font-black text-foreground">升级 Agent</h3><p className="mt-1 text-xs font-bold text-muted-foreground">升级到 Server 提供的最新版本</p></div>
              <button type="button" aria-label="关闭" onClick={() => setUpgradeOpen(false)} disabled={upgradeLoading} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={16} /></button>
            </div>
            <div className="space-y-3 p-5">
              <div className="grid gap-2 sm:grid-cols-2"><InfoBlock label="当前版本" value={connectionDiagnostics.agent_version || '未知'} /><InfoBlock label="目标版本" value={connectionDiagnostics.latest_version || '最新版本'} /></div>
              <p className="rounded-2xl border border-warning/25 bg-warning/10 px-4 py-3 text-sm font-bold text-warning">升级期间 Agent 会短暂断开。系统会校验升级包并保留上一版用于失败恢复。</p>
            </div>
            <div className="soft-modal-footer flex justify-end gap-2 border-t px-5 py-4"><button type="button" onClick={() => setUpgradeOpen(false)} disabled={upgradeLoading} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black text-muted-foreground">取消</button><button type="button" onClick={confirmAgentUpgrade} disabled={upgradeLoading} className="soft-button min-h-10 bg-primary px-4 text-xs font-black text-primary-foreground disabled:opacity-60">{upgradeLoading ? '正在准备...' : '确认升级'}</button></div>
          </section>
        </div>
      ) : null}

			{rebootOpen ? (
				<div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4">
					<section role="dialog" aria-modal="true" aria-label="重启节点" className="soft-modal-shell w-full max-w-md">
						<div className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4">
							<div><h3 className="text-base font-black text-foreground">重启节点</h3><p className="mt-1 text-xs font-bold text-muted-foreground">向当前 Agent 下发系统重启命令</p></div>
							<button type="button" aria-label="关闭" onClick={() => setRebootOpen(false)} disabled={rebootLoading} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={16} /></button>
						</div>
						<div className="space-y-3 p-5">
							<div className="grid gap-2 sm:grid-cols-2"><InfoBlock label="节点" value={displayName} /><InfoBlock label="执行用户" value={node.agent_user || '未知'} /></div>
							<p className="rounded-2xl border border-danger/25 bg-danger/10 px-4 py-3 text-sm font-bold text-danger">节点会暂时离线，系统服务和容器工作负载也会中断，请确认当前可以执行重启。</p>
						</div>
						<div className="soft-modal-footer flex justify-end gap-2 border-t px-5 py-4"><button type="button" onClick={() => setRebootOpen(false)} disabled={rebootLoading} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black text-muted-foreground">取消</button><button type="button" onClick={reboot} disabled={rebootLoading} className="soft-button min-h-10 bg-danger px-4 text-xs font-black text-white disabled:opacity-60">{rebootLoading ? '正在下发...' : '确认重启'}</button></div>
					</section>
				</div>
			) : null}

			{restartOpen ? (
				<div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4">
					<section role="dialog" aria-modal="true" aria-label="重启 Agent" className="soft-modal-shell w-full max-w-md">
						<div className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4">
							<div><h3 className="text-base font-black text-foreground">重启 Agent</h3><p className="mt-1 text-xs font-bold text-muted-foreground">重新启动节点上的 Agent 服务</p></div>
							<button type="button" aria-label="关闭" onClick={() => setRestartOpen(false)} disabled={restartLoading} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={16} /></button>
						</div>
						<div className="space-y-3 p-5">
							<InfoBlock label="服务名称" value={agentStatus?.service_name || 'mizupanel-agent'} />
							<p className="rounded-2xl border border-warning/25 bg-warning/10 px-4 py-3 text-sm font-bold text-warning">重启期间 Agent 会短暂离线，并在服务恢复后自动重新连接。</p>
						</div>
						<div className="soft-modal-footer flex justify-end gap-2 border-t px-5 py-4"><button type="button" onClick={() => setRestartOpen(false)} disabled={restartLoading} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black text-muted-foreground">取消</button><button type="button" onClick={restartAgentService} disabled={restartLoading} className="soft-button min-h-10 bg-warning px-4 text-xs font-black text-white disabled:opacity-60">{restartLoading ? '正在下发...' : '确认重启'}</button></div>
					</section>
				</div>
			) : null}

      {sshUninstallOpen ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/35 p-4">
          <section role="dialog" aria-modal="true" aria-label="卸载 Agent" className="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-[30px] border border-danger/30 bg-card shadow-2xl outline-none">
            <div className="flex items-start justify-between gap-3 border-b border-danger/30 bg-danger/10 px-5 py-4">
              <div className="min-w-0">
                <p className="text-[11px] font-black uppercase tracking-[0.24em] text-danger">Root-only SSH</p>
                <h3 className="mt-1 font-display text-2xl font-black tracking-tight text-foreground">卸载 Agent</h3>
                <p className="mt-2 text-sm font-bold leading-6 text-danger">通过 SSH 登录 root，停止并删除目标机器上的 MizuPanel Agent。</p>
              </div>
              <button type="button" aria-label="关闭" onClick={closeSSHUninstallDialog} className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-danger/30 bg-danger/5 text-danger hover:bg-danger/10 focus:outline-none focus:ring-4 focus:ring-danger/20">
                <X size={16} aria-hidden="true" />
              </button>
            </div>
            <div className="grid gap-3 overflow-y-auto px-5 py-4 sm:grid-cols-2">
              <label className="text-xs font-black text-foreground">SSH Host<input aria-label="SSH Host" value={sshHost} onChange={(event) => setSSHHost(event.target.value)} className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-red-400 focus:ring-4 focus:ring-danger/20" /></label>
              <label className="text-xs font-black text-foreground">SSH 端口<input aria-label="SSH 端口" type="number" value={sshPort} onChange={(event) => setSSHPort(Number(event.target.value) || 22)} className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-red-400 focus:ring-4 focus:ring-danger/20" /></label>
              <label className="text-xs font-black text-foreground">SSH 用户<input aria-label="SSH 用户" value="root" readOnly className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-muted px-3 text-sm font-black text-muted-foreground" /></label>
              <label className="text-xs font-black text-foreground">认证方式<select aria-label="SSH 认证方式" value={sshAuthType} onChange={(event) => setSSHAuthType(event.target.value as SSHAuthType)} className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-red-400 focus:ring-4 focus:ring-danger/20"><option value="password">密码</option><option value="private_key">私钥</option></select></label>
              {sshAuthType === 'password' ? (
                <label className="text-xs font-black text-foreground sm:col-span-2">SSH 密码<input aria-label="SSH 密码" type="password" value={sshPassword} onChange={(event) => setSSHPassword(event.target.value)} className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-red-400 focus:ring-4 focus:ring-danger/20" /></label>
              ) : (
                <>
                  <label className="text-xs font-black text-foreground sm:col-span-2">SSH 私钥<textarea aria-label="SSH 私钥" value={sshPrivateKey} onChange={(event) => setSSHPrivateKey(event.target.value)} rows={4} className="mt-1 w-full rounded-2xl border border-border bg-card px-3 py-2 text-sm font-bold outline-none focus:border-red-400 focus:ring-4 focus:ring-danger/20" /></label>
                  <label className="text-xs font-black text-foreground sm:col-span-2">私钥 Passphrase（可选）<input aria-label="私钥 Passphrase" type="password" value={sshPassphrase} onChange={(event) => setSSHPassphrase(event.target.value)} className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-red-400 focus:ring-4 focus:ring-danger/20" /></label>
                </>
              )}
              <label className="flex cursor-pointer items-start gap-3 rounded-2xl border border-warning/30 bg-warning/10 px-3 py-2 text-xs font-bold leading-5 text-warning sm:col-span-2"><input type="checkbox" checked={sshRemoveRecord} onChange={(event) => setSSHRemoveRecord(event.target.checked)} className="mt-1 h-4 w-4" />卸载后同时移除面板节点记录和历史数据</label>
              {sshUninstallMessage ? <p className="rounded-2xl border border-success/30 bg-success/10 px-3 py-2 text-xs font-black text-success sm:col-span-2">{sshUninstallMessage}</p> : null}
              {sshUninstallError ? <p className="rounded-2xl border border-danger/30 bg-danger/10 px-3 py-2 text-xs font-black text-danger sm:col-span-2">{sshUninstallError}</p> : null}
              {sshUninstallEvents.length > 0 ? (
                <div className="rounded-2xl border border-border bg-card p-3 shadow-inner sm:col-span-2">
                  <p className="mb-2 text-xs font-black uppercase tracking-[0.18em] text-muted-foreground">卸载进度</p>
                  <ol className="space-y-2">
                    {sshUninstallEvents.map((event) => (
                      <li key={event.step} className="flex items-start gap-3 rounded-2xl bg-surface px-3 py-2">
                        <span className={`mt-0.5 h-3 w-3 rounded-full ${event.status === 'success' ? 'bg-success' : event.status === 'failed' ? 'bg-danger' : event.status === 'running' ? 'bg-info/100' : 'bg-slate-400'}`} />
                        <span className="min-w-0">
                          <span className="block text-xs font-black text-foreground">{event.label}</span>
                          <span className="block text-xs font-black text-muted-foreground">{event.status === 'success' ? '成功' : event.status === 'failed' ? '失败' : event.status === 'running' ? '进行中' : '待执行'}</span>
                          {event.logs.length > 0 ? (
                            <span className="mt-1 block space-y-1">
                              {event.logs.map((log, index) => <span key={`${event.step}-${index}`} className="block break-words text-xs font-semibold leading-5 text-muted-foreground">{log}</span>)}
                            </span>
                          ) : null}
                        </span>
                      </li>
                    ))}
                  </ol>
                </div>
              ) : null}
            </div>
            <div className="flex shrink-0 flex-wrap justify-end gap-2 border-t border-border bg-surface px-5 py-4">
              {sshUninstallMessage || (sshUninstallEvents.length > 0 && sshUninstallEvents.every((e) => e.status === 'success' || e.status === 'failed')) ? (
                <button type="button" onClick={closeSSHUninstallDialog} className="min-h-11 cursor-pointer rounded-2xl bg-card px-4 text-sm font-black text-foreground shadow-sm transition hover:bg-muted focus:outline-none focus:ring-4 focus:ring-primary/20">
                  关闭
                </button>
              ) : (
                <>
                  <button type="button" onClick={closeSSHUninstallDialog} disabled={sshUninstallLoading} className="min-h-11 cursor-pointer rounded-2xl border border-border bg-card px-4 text-sm font-black text-muted-foreground transition hover:border-success/50 hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60">取消</button>
                  <button type="button" onClick={startSSHUninstall} disabled={sshUninstallLoading} className="min-h-11 cursor-pointer rounded-2xl bg-danger px-4 text-sm font-black text-white shadow-sm transition hover:brightness-95 focus:outline-none focus:ring-4 focus:ring-danger/20 disabled:cursor-not-allowed disabled:opacity-50">{sshUninstallLoading ? '正在创建卸载任务...' : '开始 SSH 卸载'}</button>
                </>
              )}
            </div>
          </section>
        </div>
      ) : null}

      {editorOpen && fileRead?.editable ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/35 p-4">
          <div role="dialog" aria-modal="true" aria-label="编辑文件" className="flex max-h-[90vh] w-full max-w-5xl flex-col rounded-[28px] border border-border bg-card p-4 shadow-2xl">
            <div className="flex items-start justify-between gap-3 border-b border-border pb-3">
              <div className="min-w-0">
                <p className="text-xs font-black text-muted-foreground">正在编辑</p>
                <p className="mt-1 break-all text-sm font-black text-foreground">{fileRead.path}</p>
              </div>
              <button type="button" aria-label="关闭" onClick={() => setEditorOpen(false)} className="shrink-0 rounded-2xl border border-border bg-card px-3 py-2 text-xs font-black text-muted-foreground transition hover:text-foreground">关闭</button>
            </div>
            {operationMessage ? <p className="mt-3 rounded-2xl border border-warning/30 bg-warning/10 px-4 py-3 text-sm font-black text-warning">{operationMessage}</p> : null}
            <textarea autoFocus aria-label="文件内容" value={fileContent} onChange={(event) => setFileContent(event.target.value)} className="mt-3 min-h-[56vh] w-full resize-y rounded-2xl border border-border bg-slate-950 p-4 font-mono text-sm font-semibold leading-6 text-slate-100 outline-none focus:border-emerald-400 focus:ring-4 focus:ring-primary/20" />
            <div className="mt-3 flex flex-wrap justify-end gap-2">
              <button type="button" onClick={saveFile} className="min-h-11 rounded-2xl bg-success px-4 text-sm font-black text-white shadow-sm transition hover:brightness-95">保存文件</button>
              <button type="button" onClick={() => setEditorOpen(false)} className="min-h-11 rounded-2xl border border-border bg-card px-4 text-sm font-black text-muted-foreground transition hover:text-foreground">关闭编辑器</button>
            </div>
          </div>
        </div>
      ) : null}

      {/* Container Logs Modal */}
      {node && (
        <ContainerLogsModal
          nodeId={node.id}
          containerId={containerLogsModal.containerId}
          containerName={containerLogsModal.containerName}
          open={containerLogsModal.open}
          onClose={() => setContainerLogsModal({ open: false, containerId: '', containerName: '' })}
        />
      )}

      {composeLogsModal ? (
        <div className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-950/35 p-4" onClick={() => setComposeLogsModal(undefined)}>
          <section role="dialog" aria-modal="true" aria-label={`Compose 项目日志 ${composeLogsModal.projectName}`} className="flex h-[78vh] w-full max-w-5xl flex-col overflow-hidden rounded-[28px] border border-border bg-card shadow-2xl" onClick={(event) => event.stopPropagation()}>
            <div className="flex items-center justify-between border-b border-border bg-surface px-5 py-4">
              <div className="min-w-0">
                <p className="text-[11px] font-black uppercase tracking-[0.18em] text-cyan-500">Compose Logs</p>
                <h3 className="mt-1 truncate font-mono text-lg font-black text-foreground">{composeLogsModal.projectName}</h3>
              </div>
              <button type="button" aria-label="关闭 Compose 日志" title="关闭" onClick={() => setComposeLogsModal(undefined)} className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-2xl border border-border bg-card text-muted-foreground transition hover:bg-muted hover:text-foreground">
                <X size={17} aria-hidden="true" />
              </button>
            </div>
            <pre className="min-h-0 flex-1 overflow-auto bg-slate-950 px-5 py-4 font-mono text-xs font-semibold leading-5 text-slate-100">{composeLogsModal.output}</pre>
            <div className="flex justify-end border-t border-border bg-surface px-5 py-3">
              <button type="button" onClick={() => setComposeLogsModal(undefined)} className="min-h-10 rounded-2xl border border-border bg-card px-4 text-xs font-black text-muted-foreground transition hover:text-foreground">关闭</button>
            </div>
          </section>
        </div>
      ) : null}

      {systemdLogsModal ? (
        <div className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-950/35 p-4" onClick={() => setSystemdLogsModal(undefined)}>
          <section role="dialog" aria-modal="true" aria-label={`系统服务日志 ${systemdLogsModal.serviceName}`} className="flex h-[78vh] w-full max-w-5xl flex-col overflow-hidden rounded-[28px] border border-border bg-card shadow-2xl" onClick={(event) => event.stopPropagation()}>
            <div className="flex items-center justify-between border-b border-border bg-surface px-5 py-4">
              <div className="min-w-0">
                <p className="text-[11px] font-black uppercase tracking-[0.18em] text-success">Systemd Logs</p>
                <h3 className="mt-1 truncate font-mono text-lg font-black text-foreground">{systemdLogsModal.serviceName}</h3>
              </div>
              <button type="button" aria-label="关闭系统服务日志" title="关闭" onClick={() => setSystemdLogsModal(undefined)} className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-2xl border border-border bg-card text-muted-foreground transition hover:bg-muted hover:text-foreground">
                <X size={17} aria-hidden="true" />
              </button>
            </div>
            <pre className="min-h-0 flex-1 overflow-auto bg-slate-950 px-5 py-4 font-mono text-xs font-semibold leading-5 text-slate-100">{systemdLogsModal.output}</pre>
            <div className="flex justify-end border-t border-border bg-surface px-5 py-3">
              <button type="button" onClick={() => setSystemdLogsModal(undefined)} className="min-h-10 rounded-2xl border border-border bg-card px-4 text-xs font-black text-muted-foreground transition hover:text-foreground">关闭</button>
            </div>
          </section>
        </div>
      ) : null}

      <CreateContainerModal
        open={createContainerModal}
        nodeId={node.id}
        onClose={() => setCreateContainerModal(false)}
        onCreate={handleCreateContainer}
      />

      {organizationEditorOpen ? (
        <SingleNodeOrganizationModal
          node={node}
          onClose={() => setOrganizationEditorOpen(false)}
          onSaved={async () => {
            await onNodeOrganizationChanged?.()
            setToast({ message: '主机组织信息调整成功', type: 'success' })
          }}
        />
      ) : null}

      {managedComposeEditor ? (
        <ManagedComposeEditorModal
          editor={managedComposeEditor}
          draft={managedComposeDraft}
          loading={deploymentLoading === 'preview' || deploymentLoading === 'apply'}
          onClose={closeManagedComposeEditor}
          onDraftChange={(patch) => setManagedComposeDraft((draft) => ({ ...draft, ...patch }))}
          onPreview={() => void previewManagedComposeDeployment()}
        />
      ) : null}

      {managedComposePreview ? (
        <ManagedComposePreviewModal
          preview={managedComposePreview}
          loading={deploymentLoading === 'apply'}
          onCancel={() => setManagedComposePreview(undefined)}
          onConfirm={() => void applyManagedComposeDeployment()}
        />
      ) : null}

      {pendingManagedComposeAction ? (
        <ManagedComposeActionConfirmModal
          action={pendingManagedComposeAction.action}
          projectName={pendingManagedComposeAction.project.display_name || pendingManagedComposeAction.project.name}
          loading={deploymentLoading === pendingManagedComposeAction.action}
          onCancel={() => setPendingManagedComposeAction(undefined)}
          onConfirm={() => void confirmManagedComposeAction()}
        />
      ) : null}

      {pendingComposeDown ? (
        <div className="fixed inset-0 z-[75] flex items-center justify-center bg-slate-950/35 p-4">
          <section role="dialog" aria-modal="true" aria-label="移除 Compose 项目" className="w-full max-w-md overflow-hidden rounded-[28px] border border-border bg-card shadow-2xl">
            <div className="border-b border-danger/20 bg-danger/10 px-5 py-4">
              <h3 className="text-lg font-black text-foreground">移除 Compose 项目</h3>
              <p className="mt-1 text-xs font-bold leading-5 text-muted-foreground">将执行 `docker compose down`，停止并移除项目创建的容器和网络，不删除 Compose 文件。</p>
            </div>
            <div className="px-5 py-5">
              <p className="text-sm font-bold text-foreground">确认操作项目：<span className="font-mono">{pendingComposeDown}</span></p>
            </div>
            <div className="flex justify-end gap-2 border-t border-border bg-surface px-5 py-4">
              <button type="button" onClick={() => setPendingComposeDown(undefined)} disabled={Boolean(composeActionLoading)} className="min-h-10 rounded-2xl border border-border bg-card px-4 text-xs font-black text-muted-foreground disabled:opacity-60">取消</button>
              <button type="button" onClick={() => void runComposeAction(pendingComposeDown, 'down')} disabled={Boolean(composeActionLoading)} className="min-h-10 rounded-2xl bg-danger px-4 text-xs font-black text-white disabled:opacity-60">{composeActionLoading ? '正在移除...' : '确认移除'}</button>
            </div>
          </section>
        </div>
      ) : null}

      {/* Toast Notification */}
      {toast && (
        <Toast
          message={toast.message}
          type={toast.type}
          onClose={() => setToast(null)}
        />
      )}
    </section>
  )
}

function filterMetricsByChartRange(metrics: Metric[], range: ChartRange) {
  if (range === '6h' || metrics.length === 0) return metrics
  const timestamps = metrics.map((metric) => new Date(metric.created_at).getTime()).filter((time) => Number.isFinite(time))
  if (timestamps.length === 0) return metrics
  const latest = Math.max(...timestamps)
  const cutoff = latest - 60 * 60 * 1000
  return metrics.filter((metric) => {
    const time = new Date(metric.created_at).getTime()
    return Number.isFinite(time) && time >= cutoff
  })
}

function updateChartRange(chartKey: string, nextRange: ChartRange, currentRange: RangeOption, onRangeChange: (range: RangeOption) => void, setChartRanges: (updater: (current: Record<string, ChartRange>) => Record<string, ChartRange>) => void) {
  setChartRanges((current) => ({ ...current, [chartKey]: nextRange }))
  if (nextRange === '6h' && currentRange !== '6h') onRangeChange('6h')
}

function mergeMetricFallback(primary?: Metric, fallback?: Metric): Metric | undefined {
  if (!primary) return fallback
  if (!fallback) return primary
  const uptimeSource = hasPositiveNumber(primary.uptime) ? primary : hasPositiveNumber(fallback.uptime) ? fallback : undefined
  return {
    ...fallback,
    ...primary,
    uptime: uptimeSource?.uptime,
    created_at: uptimeSource?.created_at || primary.created_at,
    disk_read_speed: finiteOrUndefined(primary.disk_read_speed, fallback.disk_read_speed),
    disk_write_speed: finiteOrUndefined(primary.disk_write_speed, fallback.disk_write_speed),
    rx_speed: finiteOrFallback(primary.rx_speed, fallback.rx_speed),
    tx_speed: finiteOrFallback(primary.tx_speed, fallback.tx_speed),
    load1: finiteOrFallback(primary.load1, fallback.load1),
    load5: finiteOrFallback(primary.load5, fallback.load5),
    load15: finiteOrFallback(primary.load15, fallback.load15)
  }
}

function finiteOrUndefined(primary: number | undefined, fallback: number | undefined) {
  if (typeof primary === 'number' && Number.isFinite(primary)) return primary
  if (typeof fallback === 'number' && Number.isFinite(fallback)) return fallback
  return undefined
}

function finiteOrFallback(primary: number | undefined, fallback: number | undefined) {
  return finiteOrUndefined(primary, fallback) ?? 0
}

function hasPositiveNumber(value?: number) {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function formatLoadSummary(metric?: Metric) {
  if (!metric) return '1m — · 5m — · 15m —'
  const load1 = Number.isFinite(metric.load1) ? metric.load1.toFixed(2) : '—'
  const load5 = Number.isFinite(metric.load5) ? metric.load5.toFixed(2) : '—'
  const load15 = Number.isFinite(metric.load15) ? metric.load15.toFixed(2) : '—'
  return `1m ${load1} · 5m ${load5} · 15m ${load15}`
}

function formatAgentMode(mode?: string) {
  if (mode === 'ops') return '运维模式'
  if (mode === 'normal') return '普通模式'
  return mode || '未知'
}

function formatDateTime(value?: string) {
  if (!value || value.startsWith('0001-')) return '暂无记录'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '暂无记录'
  return date.toLocaleString('zh-CN', { hour12: false })
}

function connectionAgentVersionState(diagnostics: ConnectionDiagnostics) {
  if (diagnostics.events.some((event) => event.type === 'protocol_rejected')) {
    return { kind: 'incompatible', label: '版本不兼容', tone: 'bg-danger/10 text-danger' }
  }
  if (diagnostics.protocol_version >= 1) {
    return { kind: 'current', label: '新版 Agent', tone: 'bg-success/10 text-success' }
  }
  return { kind: 'legacy', label: '待升级', tone: 'bg-warning/10 text-warning' }
}

function formatUptime(seconds?: number) {
  if (!Number.isFinite(seconds) || !seconds || seconds <= 0) return '暂未上报'
  if (seconds < 60) return '< 1 分钟'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days} 天 ${hours} 小时`
  if (hours > 0) return `${hours} 小时 ${minutes} 分钟`
  return `${minutes} 分钟`
}

function formatBootTime(metric?: Metric) {
  if (!metric) return '暂未上报'
  const uptime = metric.uptime
  if (typeof uptime !== 'number' || !Number.isFinite(uptime) || uptime <= 0) return '暂未上报'
  const sampledAt = new Date(metric.created_at).getTime()
  if (!Number.isFinite(sampledAt)) return '暂未上报'
  return new Date(sampledAt - uptime * 1000).toLocaleString('zh-CN', { hour12: false })
}

function InfoBlock({ label, value, wrap = false }: { label: string, value: string, wrap?: boolean }) {
  return (
    <div className="rounded-2xl border border-border bg-surface px-4 py-3">
      <p className="text-[11px] font-black uppercase tracking-[0.16em] text-muted-foreground">{label}</p>
      <p className={`mt-2 text-sm font-black text-foreground ${wrap ? 'leading-5' : 'truncate'}`} title={value}>{value}</p>
    </div>
  )
}

function openTerminalPage(nodeID: string) {
  window.open(`/nodes/${encodeURIComponent(nodeID)}/terminal`, '_blank', 'noopener,noreferrer')
}

function openContainerExecPage(nodeID: string, containerID: string) {
  window.open(`/nodes/${encodeURIComponent(nodeID)}/containers/${encodeURIComponent(containerID)}/exec`, '_blank', 'noopener,noreferrer')
}

function PathTreeBranch({
  path,
  level,
  tree,
  currentPath,
  disabled,
  onOpen,
  onToggle,
}: {
  path: string
  level: number
  tree: PathTreeState
  currentPath: string
  disabled: boolean
  onOpen: (path: string) => void
  onToggle: (path: string) => void
}) {
  const state = tree[path]
  const expanded = Boolean(state?.expanded)
  const loading = Boolean(state?.loading)
  const active = currentPath === path
  const ancestor = path !== '/' && currentPath.startsWith(`${path}/`)
  const segments = path.split('/').filter(Boolean)
  const label = path === '/' ? '/' : segments[segments.length - 1] || path

  return (
    <div>
      <div className={`flex items-center gap-1 rounded-xl px-2 py-1.5 ${active ? 'bg-success/10 text-success' : ancestor ? 'bg-card text-foreground' : 'text-muted-foreground hover:bg-card hover:text-foreground'}`} style={{ paddingLeft: `${8 + level * 14}px` }}>
        <button
          type="button"
          aria-label={`${expanded ? '收起' : '展开'} ${path}`}
          onClick={() => onToggle(path)}
          disabled={disabled}
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-lg text-xs font-black transition hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
        >
          <ChevronIcon expanded={expanded} />
        </button>
        <button
          type="button"
          onClick={() => onOpen(path)}
          disabled={disabled}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-lg text-left disabled:cursor-not-allowed disabled:opacity-50"
          title={path}
        >
          <FolderIcon />
          <span className="truncate text-xs font-black">{label}</span>
        </button>
        {loading ? <span className="text-[10px] font-black text-muted-foreground">加载</span> : null}
      </div>
      {state?.error ? <p className="ml-8 mt-1 rounded-lg bg-warning/10 px-2 py-1 text-[10px] font-black text-warning">{state.error}</p> : null}
      {expanded && state?.children?.length ? (
        <div className="mt-1 space-y-1">
          {state.children.map((childPath) => (
            <PathTreeBranch
              key={childPath}
              path={childPath}
              level={level + 1}
              tree={tree}
              currentPath={currentPath}
              disabled={disabled}
              onOpen={onOpen}
              onToggle={onToggle}
            />
          ))}
        </div>
      ) : null}
    </div>
  )
}

function fileTypeLabel(type: FileEntry['type']) {
  if (type === 'directory') return '目录'
  if (type === 'binary') return '二进制文件'
  return '文本文件'
}

function ChevronIcon({ expanded }: { expanded: boolean }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className={`h-3.5 w-3.5 transition ${expanded ? 'rotate-90' : ''}`} fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
      <path d="m9 6 6 6-6 6" />
    </svg>
  )
}

function UploadIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-7 w-7 text-success" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 16V4" />
      <path d="m7 9 5-5 5 5" />
      <path d="M5 16.5v1.75A1.75 1.75 0 0 0 6.75 20h10.5A1.75 1.75 0 0 0 19 18.25V16.5" />
    </svg>
  )
}

function FolderIcon() {
  return <FileIcon className="h-5 w-5" />
}

function DocumentIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round">
      <path d="M6.5 3.75h7l4 4v12.5h-11z" />
      <path d="M13.5 3.75V8h4" />
      <path d="M9 12h6" />
      <path d="M9 15.5h4" />
    </svg>
  )
}

function LockIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round">
      <rect x="5" y="10" width="14" height="10" rx="2" />
      <path d="M8.5 10V7.75a3.5 3.5 0 0 1 7 0V10" />
      <path d="M12 14v2" />
    </svg>
  )
}

function TerminalIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M4.75 5.75h14.5a1.5 1.5 0 0 1 1.5 1.5v9.5a1.5 1.5 0 0 1-1.5 1.5H4.75a1.5 1.5 0 0 1-1.5-1.5v-9.5a1.5 1.5 0 0 1 1.5-1.5Z" />
      <path d="m7.5 9 2.5 2.5L7.5 14" />
      <path d="M12.5 14h4" />
    </svg>
  )
}

function LogIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M4.75 5.75h14.5a1.5 1.5 0 0 1 1.5 1.5v9.5a1.5 1.5 0 0 1-1.5 1.5H4.75a1.5 1.5 0 0 1-1.5-1.5v-9.5a1.5 1.5 0 0 1 1.5-1.5Z" />
      <path d="M7.5 8.5h9" />
      <path d="M7.5 12h9" />
      <path d="M7.5 15.5h5" />
    </svg>
  )
}

function FileIcon({ className = 'h-5 w-5' }: { className?: string }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M4.75 4.75h5.5l2 2h7a1.5 1.5 0 0 1 1.5 1.5v9.5a1.5 1.5 0 0 1-1.5 1.5H4.75a1.5 1.5 0 0 1-1.5-1.5V6.25a1.5 1.5 0 0 1 1.5-1.5Z" />
    </svg>
  )
}

function PowerIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 3.75v8" />
      <path d="M7.25 6.75a7 7 0 1 0 9.5 0" />
    </svg>
  )
}

function TrashIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M5 7h14" />
      <path d="M9 7V5.75A1.75 1.75 0 0 1 10.75 4h2.5A1.75 1.75 0 0 1 15 5.75V7" />
      <path d="M18 7l-.75 11.25A1.75 1.75 0 0 1 15.5 20h-7a1.75 1.75 0 0 1-1.75-1.75L6 7" />
      <path d="M10 11v5" />
      <path d="M14 11v5" />
    </svg>
  )
}

function parentPath(path: string) {
  if (!path || path === '/') return '/'
  const trimmed = path.replace(/\/+$/, '')
  const index = trimmed.lastIndexOf('/')
  return index <= 0 ? '/' : trimmed.slice(0, index)
}

function joinRemotePath(directory: string, name: string) {
  const safeName = name.replace(/^\/+/, '')
  if (!directory || directory === '/') return `/${safeName}`
  return `${directory.replace(/\/+$/, '')}/${safeName}`
}

async function fileToBase64(file: File) {
  const bytes = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte)
  })
  return btoa(binary)
}

function formatOperationError(code: string | undefined, fallback: string) {
  if (code === 'permission_denied') return '权限不足：当前 Agent 运行用户无权执行该操作。如需使用该能力，请使用运维模式重新安装 Agent，或在目标机器上调整 Agent 用户权限。'
  if (code === 'binary_file') return '二进制文件不可编辑'
  if (code === 'too_large') return fallback.includes('上传') ? '文件过大，暂不支持上传。' : '文件过大，暂不支持在线编辑。'
  if (code === 'directory_not_empty') return '目录非空，暂不支持直接删除。'
  if (code === 'timeout') return fallback
  if (code === 'offline') return '节点离线，无法发送文件管理或重启命令。'
  return fallback
}

function formatDockerUnavailableMessage(error: string | undefined, loading: boolean) {
  if (loading) return '正在检测 Docker...'
  if (!error) return '未检测到 Docker 或暂无 Docker 快照。'
  if (error.includes('permission denied') && error.includes('/var/run/docker.sock')) {
    return 'Agent 当前用户没有权限访问 Docker。请把 Agent 运行用户加入 docker 组，或用有 Docker socket 权限的用户运行 Agent。'
  }
  if (error.includes('/var/run/docker.sock')) {
    return `Docker 当前不可用：${error}`
  }
  return error
}

function MonitoringState({ loading, error, empty, emptyText }: { loading: boolean, error?: string, empty: boolean, emptyText: string }) {
  if (loading) {
    return <div className="m-4 rounded-2xl border border-info/30 bg-info/10 px-4 py-3 text-sm font-black text-info">正在加载进程 / Docker 快照...</div>
  }
  if (error) {
    return <div className="m-4 rounded-2xl border border-warning/30 bg-warning/10 px-4 py-3 text-sm font-black text-warning">采集提示：{error}</div>
  }
  if (empty) {
    return <div className="m-4 rounded-2xl border border-dashed border-border bg-surface px-4 py-3 text-sm font-bold text-muted-foreground">{emptyText}</div>
  }
  return null
}

function ProcessTable({ processes }: { processes: ProcessInfo[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="min-w-full divide-y divide-slate-200 text-left text-sm">
        <thead className="bg-card text-[11px] font-black uppercase tracking-[0.14em] text-muted-foreground">
          <tr>
            <th className="px-4 py-3">PID</th>
            <th className="px-4 py-3">名称</th>
            <th className="px-4 py-3">用户</th>
            <th className="px-4 py-3">状态</th>
            <th className="px-4 py-3">CPU</th>
            <th className="px-4 py-3">内存</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border bg-card">
          {processes.map((process) => (
            <tr key={`${process.pid}-${process.name}`} className="align-top hover:bg-surface">
              <td className="px-4 py-3 font-mono text-xs font-black text-foreground">{process.pid}</td>
              <td className="px-4 py-3 font-black text-foreground">{process.name || 'unknown'}</td>
              <td className="px-4 py-3 font-semibold text-muted-foreground">{process.user || '—'}</td>
              <td className="px-4 py-3"><StatusPill value={process.status} /></td>
              <td className="px-4 py-3 font-black text-success">{formatPercent(process.cpu_usage)}</td>
              <td className="px-4 py-3 font-semibold text-foreground">{formatBytes(process.memory_rss)} <span className="text-muted-foreground">({formatPercent(process.memory_usage)})</span></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function DockerTable({ nodeID, containers, onOpenLogs, onRefresh, onShowToast }: { nodeID: string; containers: DockerContainer[]; onOpenLogs: (containerId: string, containerName: string) => void; onRefresh: () => void; onShowToast: (message: string, type: 'success' | 'error') => void }) {
  return (
    <div data-testid="docker-table-scroll" className="min-w-0 max-w-full overflow-x-auto">
      <table className="w-full min-w-0 table-fixed divide-y divide-slate-200 text-left text-sm">
        <thead className="bg-card text-[11px] font-black uppercase tracking-[0.14em] text-muted-foreground">
          <tr>
            <th className="w-[20%] px-4 py-3">容器</th>
            <th className="w-[20%] px-4 py-3">镜像</th>
            <th className="w-[14%] px-4 py-3">状态</th>
            <th className="w-[8%] px-4 py-3">CPU</th>
            <th className="w-[14%] px-4 py-3">内存</th>
            <th className="hidden w-[14%] px-4 py-3 2xl:table-cell">网络</th>
            <th className="hidden w-[12%] px-4 py-3 2xl:table-cell">创建时间</th>
            <th className="w-[10%] px-4 py-3">操作</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border bg-card">
          {containers.map((container) => {
            const running = container.state.toLowerCase() === 'running'
            const execID = container.full_id || container.id
            return (
              <tr key={container.id} className="align-top hover:bg-surface">
                <td className="min-w-0 px-4 py-3"><p className="truncate font-black text-foreground" title={container.name || container.id}>{container.name || container.id}</p><p className="break-all font-mono text-xs font-bold text-muted-foreground" title={container.full_id || container.id}>{container.id}</p></td>
                <td className="min-w-0 px-4 py-3 font-semibold text-muted-foreground"><p className="line-clamp-2 break-all" title={container.image || '—'}>{container.image || '—'}</p></td>
                <td className="min-w-0 px-4 py-3">
                  <div className="truncate">
                    <StatusPill value={container.state || 'unknown'} detail={container.status} />
                  </div>
                </td>
                <td className="px-4 py-3 font-black text-cyan-600">{formatPercent(container.cpu_usage ?? 0)}</td>
                <td className="min-w-0 px-4 py-3 font-semibold text-foreground"><p className="line-clamp-2 break-words" title={`${formatBytes(container.memory_usage ?? 0)}${container.memory_limit ? ` / ${formatBytes(container.memory_limit)} (${formatPercent(container.memory_percent ?? 0)})` : ''}`}>{formatBytes(container.memory_usage ?? 0)}{container.memory_limit ? <span className="text-muted-foreground"> / {formatBytes(container.memory_limit)} ({formatPercent(container.memory_percent ?? 0)})</span> : null}</p></td>
                <td className="hidden min-w-0 px-4 py-3 font-semibold text-muted-foreground 2xl:table-cell"><p className="truncate">↓ {formatBytes(container.network_rx ?? 0)} · ↑ {formatBytes(container.network_tx ?? 0)}</p></td>
                <td className="hidden px-4 py-3 font-semibold text-muted-foreground 2xl:table-cell">{formatUnixTime(container.created_at)}</td>
                <td className="px-4 py-3">
                  <div className="flex items-center justify-center">
                    <ContainerActionsDropdown
                      container={container}
                      nodeID={nodeID}
                      onRefresh={onRefresh}
                      onShowToast={onShowToast}
                      onOpenLogs={onOpenLogs}
                    />
                  </div>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function StatusPill({ value, detail }: { value: string, detail?: string }) {
  const normalized = value.toLowerCase()
  const className = normalized.includes('run')
    ? 'bg-success/10 text-success ring-success/20'
    : normalized.includes('exit') || normalized.includes('stop')
      ? 'bg-muted text-muted-foreground ring-slate-200'
      : normalized.includes('restart') || normalized.includes('zombie')
        ? 'bg-warning/10 text-warning ring-warning/20'
        : 'bg-info/10 text-info ring-info/20'
  const widthClass = detail ? 'w-full min-w-0' : 'w-fit max-w-full whitespace-nowrap'
  return (
    <span className={`inline-flex ${widthClass} flex-col rounded-2xl px-3 py-1 text-xs font-black ring-1 ${className}`}>
      <span className={detail ? 'truncate' : undefined}>{value || 'unknown'}</span>
      {detail ? <span className="mt-0.5 max-w-full truncate font-semibold opacity-75">{detail}</span> : null}
    </span>
  )
}

function ContainerActionsDropdown({ container, nodeID, onRefresh, onShowToast, onOpenLogs }: { container: DockerContainer; nodeID: string; onRefresh: () => void; onShowToast: (message: string, type: 'success' | 'error') => void; onOpenLogs: (containerId: string, containerName: string) => void }) {
  const menuId = useId()
  const [open, setOpen] = useState(false)
  const [menuPosition, setMenuPosition] = useState<{ top: number; left: number }>()
  const [loading, setLoading] = useState(false)
  const running = container.state.toLowerCase() === 'running'
  const execID = container.full_id || container.id

  useEffect(() => {
    if (!open) return
    const closeOnOutsideClick = (event: globalThis.MouseEvent) => {
      let element = event.target instanceof HTMLElement ? event.target : null
      while (element) {
        if (element.getAttribute('data-container-actions-menu') === menuId) {
          return
        }
        element = element.parentElement
      }
      setOpen(false)
    }
    const timeoutID = window.setTimeout(() => {
      document.addEventListener('click', closeOnOutsideClick)
    }, 0)
    return () => {
      window.clearTimeout(timeoutID)
      document.removeEventListener('click', closeOnOutsideClick)
    }
  }, [menuId, open])

  const toggleMenu = (event: ReactMouseEvent<HTMLButtonElement>) => {
    event.stopPropagation()
    const rect = event.currentTarget.getBoundingClientRect()
    const menuWidth = 176
    setMenuPosition({
      top: rect.bottom + 6,
      left: Math.max(8, Math.min(rect.right - menuWidth, window.innerWidth - menuWidth - 8)),
    })
    setOpen((value) => !value)
  }

  const handleAction = async (action: 'start' | 'stop' | 'restart' | 'delete' | 'exec' | 'logs') => {
    setOpen(false)

    // Handle non-API actions
    if (action === 'exec') {
      openContainerExecPage(nodeID, execID)
      return
    }

    if (action === 'logs') {
      onOpenLogs(execID, container.name || container.id)
      return
    }

    setLoading(true)

    const actionText = action === 'start' ? '启动' : action === 'stop' ? '停止' : action === 'restart' ? '重启' : '删除'

    try {
      const containerID = container.full_id || container.id
      let response: Response

      if (action === 'delete') {
        response = await fetch(`/api/nodes/${nodeID}/containers/${containerID}?force=true`, {
          method: 'DELETE',
        })
      } else {
        response = await fetch(`/api/nodes/${nodeID}/containers/${containerID}/${action}`, {
          method: 'POST',
        })
      }

      const result = await response.json()

      if (result.success) {
        onShowToast(`容器${actionText}成功`, 'success')
        // Refresh docker snapshot
        onRefresh()
      } else {
        onShowToast(`容器${actionText}失败: ${result.error || '未知错误'}`, 'error')
      }
    } catch (err) {
      onShowToast(`容器${actionText}失败: ${err instanceof Error ? err.message : '网络错误'}`, 'error')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="relative flex items-center justify-center">
      <button
        type="button"
        aria-label="容器操作"
        title="容器操作"
        data-container-actions-menu={menuId}
        onClick={toggleMenu}
        disabled={loading}
        className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-xl border border-border bg-surface text-muted-foreground shadow-sm transition hover:border-primary/30 hover:bg-primary/10 hover:text-primary focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
      >
        <MoreHorizontal size={16} aria-hidden="true" />
      </button>
      {open && !loading && menuPosition ? createPortal(
        <div
          data-container-actions-menu={menuId}
          className="fixed z-[70] w-44 rounded-2xl border border-border/80 bg-card/95 p-1.5 text-left shadow-[0_18px_45px_rgb(15_23_42/0.16)] backdrop-blur"
          style={{ top: menuPosition.top, left: menuPosition.left }}
          onClick={(event) => event.stopPropagation()}
        >
          <ContainerActionMenuItem icon={<Terminal size={15} />} label="进入容器" disabled={!running} onClick={() => handleAction('exec')} />
          <ContainerActionMenuItem icon={<ScrollText size={15} />} label="查看日志" onClick={() => handleAction('logs')} />
          <div className="my-1 h-px bg-border/70" />
          {!running ? <ContainerActionMenuItem icon={<Play size={15} />} label="启动" onClick={() => handleAction('start')} /> : null}
          {running ? <ContainerActionMenuItem icon={<Square size={15} />} label="停止" onClick={() => handleAction('stop')} /> : null}
          {running ? <ContainerActionMenuItem prominent icon={<RotateCw size={15} />} label="重启" onClick={() => handleAction('restart')} /> : null}
          <div className="my-1 h-px bg-border/70" />
          <ContainerActionMenuItem danger icon={<Trash2 size={15} />} label="删除" onClick={() => handleAction('delete')} />
        </div>,
        document.body
      ) : null}
    </div>
  )
}

function ContainerActionMenuItem({ icon, label, danger, disabled, prominent, onClick }: { icon: ReactNode; label: string; danger?: boolean; disabled?: boolean; prominent?: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`group flex w-full items-center gap-2 px-3 py-2.5 text-left text-xs font-bold transition disabled:cursor-not-allowed disabled:opacity-45 ${prominent ? 'rounded-[10px] border border-slate-200 bg-white text-slate-800 shadow-sm shadow-blue-500/10 hover:border-blue-300 hover:bg-blue-50 hover:text-blue-700 focus:outline-none focus:ring-4 focus:ring-blue-100 dark:border-blue-400/25 dark:bg-card dark:text-foreground dark:hover:border-blue-400/50 dark:hover:bg-blue-500/15 dark:hover:text-blue-300 dark:focus:ring-blue-400/20' : danger ? 'rounded-xl text-danger hover:bg-danger/10' : 'rounded-xl text-foreground hover:bg-primary/10 hover:text-primary'}`}
    >
      <span aria-hidden="true" className={`inline-flex h-5 w-5 items-center justify-center rounded-lg ${prominent ? 'bg-blue-50 text-blue-600 transition-colors group-hover:bg-blue-100 group-hover:text-blue-700 dark:bg-blue-500/15 dark:text-blue-300 dark:group-hover:bg-blue-500/25 dark:group-hover:text-blue-200' : 'bg-surface text-current'}`}>
        {icon}
      </span>
      {label}
    </button>
  )
}

function SystemdServicesPanel({ response, loading, online, actionLoading, initialSearch, onRefresh, onAction }: { response?: SystemdServiceListResponse; loading: boolean; online: boolean; actionLoading?: string; initialSearch: string; onRefresh: () => void; onAction: (serviceName: string, action: SystemdServiceAction) => void }) {
  const [filter, setFilter] = useState<'all' | 'active' | 'inactive' | 'failed'>('all')
  const [search, setSearch] = useState(initialSearch)
  useEffect(() => setSearch(initialSearch), [initialSearch])
  const services = response?.services ?? []
  const keyword = search.trim().toLowerCase()
  const filteredServices = services.filter((service) => {
    const state = service.active_state?.toLowerCase() || ''
    if (filter === 'active' && state !== 'active') return false
    if (filter === 'inactive' && state !== 'inactive') return false
    if (filter === 'failed' && state !== 'failed') return false
    if (!keyword) return true
    return [service.name, service.description, service.load_state, service.active_state, service.sub_state, service.unit_file_state]
      .some((value) => value?.toLowerCase().includes(keyword))
  })

  return (
    <section role="region" aria-label="系统服务" className="overflow-hidden rounded-[28px] border border-border bg-card shadow-sm">
      <div className="border-b border-border bg-surface p-4">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0">
            <p className="text-[11px] font-black uppercase tracking-[0.22em] text-success">Systemd Services</p>
            <h3 className="mt-1 text-lg font-black text-foreground">系统服务</h3>
            <p className="mt-1 text-xs font-bold text-muted-foreground">查看当前已加载的 systemd 服务；开机自启设置不在此处修改。</p>
          </div>
          <button type="button" aria-label="刷新系统服务" title="刷新系统服务" onClick={onRefresh} disabled={!online || loading || Boolean(actionLoading)} className="inline-flex h-10 w-10 shrink-0 items-center justify-center self-start rounded-2xl border border-border bg-card text-muted-foreground transition hover:border-primary/30 hover:bg-primary/10 hover:text-primary focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60">
            <RotateCw size={16} aria-hidden="true" />
          </button>
        </div>
        {response?.supported ? (
          <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-border/70 pt-3">
            <div className="flex shrink-0 rounded-2xl border border-border bg-card p-1 shadow-inner" role="group" aria-label="系统服务状态筛选">
              {([
                ['all', '全部'],
                ['active', '运行中'],
                ['inactive', '已停止'],
                ['failed', '失败']
              ] as const).map(([value, label]) => (
                <button key={value} type="button" aria-pressed={filter === value} onClick={() => setFilter(value)} className={`min-h-9 cursor-pointer whitespace-nowrap rounded-xl px-3 text-xs font-black transition focus:outline-none focus:ring-4 focus:ring-primary/20 ${filter === value ? 'bg-slate-950 text-white' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}>{label}</button>
              ))}
            </div>
            <input aria-label="搜索系统服务" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索服务名、描述或状态" className="min-h-10 min-w-[260px] flex-1 rounded-2xl border border-border bg-card px-4 text-sm font-semibold text-foreground outline-none placeholder:text-muted-foreground focus:border-emerald-400 focus:ring-4 focus:ring-primary/20" />
          </div>
        ) : null}
      </div>

      {!response ? <div className="m-4 rounded-2xl border border-dashed border-border bg-surface px-4 py-8 text-center text-sm font-bold text-muted-foreground">{loading ? '正在读取 systemd 服务...' : '切换到系统服务视图后读取服务列表。'}</div> : null}
      {response && !response.supported ? <div className="m-4 rounded-2xl border border-warning/30 bg-warning/10 px-4 py-4 text-sm font-bold text-warning">{response.error || '当前 Agent 或主机未启用 systemd 服务管理。'}</div> : null}
      {response?.supported && response.error && !response.success ? <div className="m-4 rounded-2xl border border-danger/30 bg-danger/10 px-4 py-4 text-sm font-bold text-danger">系统服务加载失败: {response.error}</div> : null}
      {response?.supported && response.success && filteredServices.length === 0 ? <div className="m-4 rounded-2xl border border-dashed border-border bg-surface px-4 py-8 text-center"><p className="text-sm font-black text-foreground">未找到系统服务</p><p className="mt-1 text-xs font-semibold text-muted-foreground">请调整状态筛选或搜索条件。</p></div> : null}
      {response?.supported && response.success && filteredServices.length > 0 ? (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-left text-sm">
            <thead className="bg-card text-[11px] font-black uppercase tracking-[0.12em] text-muted-foreground"><tr><th className="px-4 py-3">服务</th><th className="px-4 py-3">描述</th><th className="px-4 py-3">状态</th><th className="px-4 py-3">启动方式</th><th className="w-20 px-4 py-3">操作</th></tr></thead>
            <tbody className="divide-y divide-border">
              {filteredServices.map((service) => {
                const busy = actionLoading?.startsWith(`${service.name}:`) ?? false
                return <SystemdServiceRow key={service.name} service={service} online={online} busy={busy} onAction={onAction} />
              })}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  )
}

function SystemdServiceRow({ service, online, busy, onAction }: { service: SystemdService; online: boolean; busy: boolean; onAction: (serviceName: string, action: SystemdServiceAction) => void }) {
  const active = service.active_state?.toLowerCase() === 'active'
  const state = service.active_state || service.sub_state || 'unknown'
  return (
    <tr className="hover:bg-surface">
      <td className="px-4 py-3"><p className="font-mono text-xs font-black text-foreground">{service.name}</p><p className="mt-1 text-[11px] font-semibold text-muted-foreground">{service.load_state || '加载状态未知'} · {service.sub_state || '子状态未知'}</p></td>
      <td className="max-w-[260px] truncate px-4 py-3 text-xs font-semibold text-muted-foreground" title={service.description}>{service.description || '—'}</td>
      <td className="px-4 py-3"><SystemdStatusPill value={state} /></td>
      <td className="px-4 py-3"><span className={`inline-flex rounded-xl px-2.5 py-1 text-xs font-black ${service.unit_file_state === 'enabled' ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'}`}>{service.unit_file_state || '未知'}</span></td>
      <td className="w-20 px-4 py-3"><SystemdServiceActionMenu serviceName={service.name} active={active} online={online} busy={busy} onAction={onAction} /></td>
    </tr>
  )
}

function SystemdStatusPill({ value }: { value: string }) {
  const normalized = value.toLowerCase()
  const tone = normalized === 'active' ? 'bg-success/10 text-success ring-success/20' : normalized === 'failed' ? 'bg-danger/10 text-danger ring-danger/20' : 'bg-muted text-muted-foreground ring-slate-200'
  return <span className={`inline-flex rounded-xl px-2.5 py-1 text-xs font-black ring-1 ${tone}`}>{value}</span>
}

function SystemdServiceActionMenu({ serviceName, active, online, busy, onAction }: { serviceName: string; active: boolean; online: boolean; busy: boolean; onAction: (serviceName: string, action: SystemdServiceAction) => void }) {
  const [open, setOpen] = useState(false)
  const [menuPosition, setMenuPosition] = useState<{ top: number; left: number }>()
  const menuId = useId()

  useEffect(() => {
    if (!open) return
    const closeOnOutsideClick = (event: MouseEvent) => {
      let element = event.target instanceof Element ? event.target : null
      while (element) {
        if (element.getAttribute('data-systemd-service-actions-menu') === menuId) return
        element = element.parentElement
      }
      setOpen(false)
    }
    const timeoutID = window.setTimeout(() => document.addEventListener('click', closeOnOutsideClick), 0)
    return () => {
      window.clearTimeout(timeoutID)
      document.removeEventListener('click', closeOnOutsideClick)
    }
  }, [menuId, open])

  const toggleMenu = (event: ReactMouseEvent<HTMLButtonElement>) => {
    event.stopPropagation()
    const rect = event.currentTarget.getBoundingClientRect()
    const menuWidth = 176
    const menuHeight = 224
    setMenuPosition({
      top: Math.max(8, Math.min(rect.bottom + 6, window.innerHeight - menuHeight - 8)),
      left: Math.max(8, Math.min(rect.right - menuWidth, window.innerWidth - menuWidth - 8)),
    })
    setOpen((value) => !value)
  }

  const chooseAction = (action: SystemdServiceAction) => {
    setOpen(false)
    onAction(serviceName, action)
  }

  return (
    <div className="relative flex items-center justify-center">
      <button type="button" aria-label={`${serviceName} 服务操作`} title={`${serviceName} 服务操作`} data-systemd-service-actions-menu={menuId} onClick={toggleMenu} disabled={busy} className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-xl border border-border bg-card text-muted-foreground transition hover:border-primary/30 hover:bg-primary/10 hover:text-primary focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"><MoreHorizontal size={16} aria-hidden="true" /></button>
      {open && !busy && menuPosition ? createPortal(
        <div role="menu" aria-label={`${serviceName} 服务操作菜单`} data-systemd-service-actions-menu={menuId} className="fixed z-[70] w-44 rounded-2xl border border-border/80 bg-card/95 p-1.5 text-left shadow-[0_18px_45px_rgb(15_23_42/0.16)] backdrop-blur" style={{ top: menuPosition.top, left: menuPosition.left }} onClick={(event) => event.stopPropagation()}>
          <p className="px-3 pb-1 pt-1.5 font-mono text-[10px] font-black text-muted-foreground" title={serviceName}>{serviceName}</p>
          <ContainerActionMenuItem icon={<ScrollText size={15} />} label="查看日志" disabled={!online} onClick={() => chooseAction('logs')} />
          <div className="my-1 h-px bg-border/70" />
          <ContainerActionMenuItem icon={<Play size={15} />} label="启动" disabled={!online || active} onClick={() => chooseAction('start')} />
          <ContainerActionMenuItem icon={<RotateCw size={15} />} label="重启" disabled={!online || !active} onClick={() => chooseAction('restart')} />
          <ContainerActionMenuItem icon={<Square size={15} />} label="停止" disabled={!online || !active} onClick={() => chooseAction('stop')} />
        </div>,
        document.body
      ) : null}
    </div>
  )
}

function systemdActionText(action: SystemdServiceAction) {
  return action === 'start' ? '启动' : action === 'stop' ? '停止' : action === 'restart' ? '重启' : '查看日志'
}

function DockerComposePanel({ response, loading, online, actionLoading, deploymentLoading, onAction, onCreateManaged, onUpdateManaged, onManagedAction, onOpenLogs, onOpenTerminal }: { response?: DockerComposeListResponse; loading: boolean; online: boolean; actionLoading?: string; deploymentLoading?: DockerComposeDeploymentAction; onAction: (projectName: string, action: DockerComposeAction, serviceName?: string) => void; onCreateManaged: () => void; onUpdateManaged: (project: DockerComposeProject) => void; onManagedAction: (project: DockerComposeProject, action: Extract<DockerComposeDeploymentAction, 'rollback' | 'archive'>) => void; onOpenLogs: (containerID: string, containerName: string) => void; onOpenTerminal: (containerID: string) => void }) {
  if (!response) {
    return <div className="m-4 rounded-2xl border border-dashed border-border bg-surface px-4 py-8 text-center text-sm font-bold text-muted-foreground">{loading ? '正在读取 Compose 项目...' : '切换到 Compose 视图后刷新项目列表。'}</div>
  }
  if (!response.supported) {
    return <div className="m-4 rounded-2xl border border-warning/30 bg-warning/10 px-4 py-4 text-sm font-bold text-warning">{response.error || '当前 Agent 未检测到 Docker Compose CLI，请安装 Docker Compose 插件并升级 Agent。'}</div>
  }
  if (response.error && !response.success) {
    return <div className="m-4 rounded-2xl border border-danger/30 bg-danger/10 px-4 py-4 text-sm font-bold text-danger">Compose 项目加载失败: {response.error}</div>
  }
  const serviceActionsSupported = response.service_actions_supported === true
  const deploymentSupported = response.deployment_supported === true
  return (
    <>
      {deploymentSupported ? (
        <div className="mx-4 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-primary/20 bg-primary/5 px-4 py-3">
          <div>
            <p className="text-sm font-black text-foreground">托管 Compose 应用</p>
            <p className="mt-1 text-xs font-semibold text-muted-foreground">以预览和风险确认的方式创建或更新应用配置。</p>
          </div>
          <button type="button" onClick={onCreateManaged} disabled={!online || Boolean(actionLoading) || Boolean(deploymentLoading)} className="inline-flex min-h-10 items-center gap-2 rounded-2xl border border-primary/30 bg-primary px-4 text-xs font-black text-primary-foreground shadow-sm transition hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-55"><Plus size={15} aria-hidden="true" />新建托管应用</button>
        </div>
      ) : null}
      {response.projects.length === 0 ? (
        <div className="m-4 rounded-2xl border border-dashed border-border bg-surface px-4 py-8 text-center"><p className="text-sm font-black text-foreground">未发现 Compose 项目</p><p className="mt-1 text-xs font-semibold text-muted-foreground">Agent 只展示 Docker Compose 当前可识别的项目，不扫描主机文件系统。</p></div>
      ) : (
        <div className="m-4 overflow-hidden rounded-2xl border border-border">
          {response.projects.map((project) => {
            const managed = isManagedComposeProject(project)
            const managedControls = managed && deploymentSupported
            const projectLabel = managed ? project.display_name || project.name : project.name
            const running = project.services.filter((service) => service.state?.toLowerCase() === 'running').length
            const legacyBusy = actionLoading?.startsWith(`${project.name}:`) ?? false
            const busy = legacyBusy || Boolean(deploymentLoading)
            return (
              <article key={project.managed_project_id || project.name} className="overflow-hidden border-b border-border bg-surface/35 last:border-b-0">
                <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border bg-surface px-4 py-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <h4 className="font-mono text-sm font-black text-foreground">{projectLabel}</h4>
                      <span className={`rounded-full px-2.5 py-1 text-[11px] font-black ${managed ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}>{managed ? '托管' : '外部'}</span>
                      <span className={`rounded-full px-2.5 py-1 text-[11px] font-black ${running > 0 ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'}`}>{running}/{project.services.length} 运行</span>
                      {managed && project.revision ? <span className="rounded-full bg-slate-950/5 px-2.5 py-1 text-[11px] font-black text-muted-foreground">版本 {project.revision}</span> : null}
                    </div>
                    {managed ? <p className="mt-1 text-xs font-semibold text-muted-foreground">由 MizuPanel Agent 托管</p> : <p className="mt-1 truncate text-xs font-semibold text-muted-foreground" title={project.config_files?.join(', ')}>{project.config_files?.join(' · ') || '配置文件路径不可用'}</p>}
                    {project.error ? <p className="mt-2 text-xs font-bold text-warning">{project.error}</p> : null}
                  </div>
                  <div className="flex flex-wrap gap-2" role="toolbar" aria-label={`${projectLabel} Compose 操作`}>
                    {managedControls ? <>
                      <ComposeActionButton icon={<FileCheck2 size={14} aria-hidden="true" />} label="更新应用" primary disabled={!online || busy || !project.managed_project_id} onClick={() => onUpdateManaged(project)} />
                      {project.rollback_available ? <ComposeActionButton icon={<RotateCw size={14} aria-hidden="true" />} label="回滚上一版本" disabled={!online || busy || !project.managed_project_id} onClick={() => onManagedAction(project, 'rollback')} /> : null}
                    </> : null}
                    <ComposeActionButton icon={<Download size={14} aria-hidden="true" />} label="拉取镜像" disabled={!online || busy} onClick={() => onAction(project.name, 'pull')} />
                    <ComposeActionButton icon={<Play size={14} aria-hidden="true" />} label="启动 / 重建" primary disabled={!online || busy} onClick={() => onAction(project.name, 'up')} />
                    <ComposeActionButton icon={<RotateCw size={14} aria-hidden="true" />} label="重启" disabled={!online || busy || running === 0} onClick={() => onAction(project.name, 'restart')} />
                    <ComposeActionButton icon={<Square size={14} aria-hidden="true" />} label="停止" disabled={!online || busy || running === 0} onClick={() => onAction(project.name, 'stop')} />
                    <ComposeActionButton icon={<ScrollText size={14} aria-hidden="true" />} label="日志" disabled={!online || busy} onClick={() => onAction(project.name, 'logs')} />
                    <ComposeActionButton icon={<FileCheck2 size={14} aria-hidden="true" />} label="校验配置" disabled={!online || busy} onClick={() => onAction(project.name, 'validate')} />
                    {managedControls
                      ? <ComposeActionButton icon={<Trash2 size={14} aria-hidden="true" />} label="归档" danger disabled={!online || busy || !project.managed_project_id} onClick={() => onManagedAction(project, 'archive')} />
                      : <ComposeActionButton icon={<Trash2 size={14} aria-hidden="true" />} label="移除" danger disabled={!online || busy} onClick={() => onAction(project.name, 'down')} />}
                  </div>
                </div>
                {busy ? <div className="border-b border-primary/20 bg-primary/5 px-4 py-2 text-xs font-black text-primary">{deploymentLoading ? `正在${managedDeploymentActionText(deploymentLoading)}托管应用，请稍候...` : `正在执行 ${composeActionText(actionLoading!.split(':').pop() as DockerComposeAction)}，请稍候...`}</div> : null}
                <div className="overflow-x-auto">
                  <table className="w-full min-w-[760px] text-left text-sm">
                    <thead className="bg-card text-[11px] font-black uppercase tracking-[0.12em] text-muted-foreground"><tr><th className="px-4 py-3">服务</th><th className="px-4 py-3">容器</th><th className="px-4 py-3">镜像</th><th className="px-4 py-3">状态</th><th className="px-4 py-3">端口</th><th className="w-20 px-4 py-3">操作</th></tr></thead>
                    <tbody className="divide-y divide-border">
                      {project.services.length > 0 ? project.services.map((service) => {
                        const containerID = service.container_id || ''
                        const runningService = service.state?.toLowerCase() === 'running'
                        return (
                          <tr key={`${service.name}-${service.container_id || service.container_name}`} className="hover:bg-surface">
                            <td className="px-4 py-3 font-black text-foreground">{service.name}</td>
                            <td className="max-w-[180px] truncate px-4 py-3 font-mono text-xs font-bold text-muted-foreground" title={service.container_name}>{service.container_name || '—'}</td>
                            <td className="max-w-[220px] truncate px-4 py-3 text-xs font-semibold text-foreground" title={service.image}>{service.image || '—'}</td>
                            <td className="px-4 py-3"><StatusPill value={service.health || service.state || service.status || 'unknown'} /></td>
                            <td className="max-w-[150px] truncate px-4 py-3 font-mono text-xs font-semibold text-muted-foreground" title={service.ports?.join(', ')}>{service.ports?.join(', ') || '—'}</td>
                            <td className="w-20 px-4 py-3">
                              <ComposeServiceActionMenu
                                projectName={project.name}
                                serviceName={service.name}
                                containerID={containerID}
                                containerName={service.container_name || service.name}
                                online={online}
                                running={runningService}
                                busy={busy}
                                lifecycleSupported={serviceActionsSupported}
                                onAction={(action) => onAction(project.name, action, service.name)}
                                onOpenLogs={() => onOpenLogs(containerID, service.container_name || service.name)}
                                onOpenTerminal={() => onOpenTerminal(containerID)}
                              />
                            </td>
                          </tr>
                        )
                      }) : <tr><td colSpan={6} className="px-4 py-6 text-center text-sm font-bold text-muted-foreground">项目当前没有容器，完成部署或启动后将在这里显示服务。</td></tr>}
                    </tbody>
                  </table>
                </div>
              </article>
            )
          })}
        </div>
      )}
    </>
  )
}

function ManagedComposeEditorModal({ editor, draft, loading, onClose, onDraftChange, onPreview }: { editor: ManagedComposeEditor; draft: ManagedComposeDeploymentDraft; loading: boolean; onClose: () => void; onDraftChange: (patch: Partial<ManagedComposeDeploymentDraft>) => void; onPreview: () => void }) {
  const updating = Boolean(editor.projectID)
  const title = updating ? '更新托管 Compose 应用' : '新建托管 Compose 应用'
  return (
    <div className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-950/45 p-4 sm:p-7" onClick={onClose}>
      <section role="dialog" aria-modal="true" aria-label={title} className="flex max-h-[calc(100vh-2rem)] w-full max-w-6xl flex-col overflow-hidden rounded-[28px] border border-border bg-card shadow-2xl" onClick={(event) => event.stopPropagation()}>
        <div className="flex items-start justify-between gap-5 border-b border-border bg-surface px-5 py-4 sm:px-6">
          <div className="min-w-0">
            <p className="text-[11px] font-black uppercase tracking-[0.18em] text-primary">Managed Compose</p>
            <h3 className="mt-1 text-xl font-black text-foreground">{title}</h3>
            <p className="mt-1 text-xs font-semibold leading-5 text-muted-foreground">先预览校验和风险，再确认部署。Agent 不会把配置或环境变量回传到页面。</p>
          </div>
          <button type="button" aria-label="关闭托管 Compose 编辑器" title="关闭" onClick={onClose} disabled={loading} className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-2xl border border-border bg-card text-muted-foreground transition hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-55"><X size={17} aria-hidden="true" /></button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6">
          <div className="grid gap-5 lg:grid-cols-[minmax(0,1.3fr)_minmax(320px,0.7fr)]">
            <label className="flex min-h-[420px] flex-col gap-2">
              <span className="text-xs font-black text-foreground">Compose YAML</span>
              <textarea aria-label="Compose YAML" value={draft.composeYAML} onChange={(event) => onDraftChange({ composeYAML: event.target.value })} spellCheck={false} placeholder={'services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "8080:80"'} className="min-h-[360px] flex-1 resize-y rounded-2xl border border-border bg-slate-950 px-4 py-3 font-mono text-xs font-semibold leading-5 text-slate-100 outline-none placeholder:text-slate-500 focus:border-primary focus:ring-4 focus:ring-primary/15" />
              <span className="text-[11px] font-semibold leading-5 text-muted-foreground">不支持 build、include、extends、YAML env_file、profiles 或 .env 中的 COMPOSE_* 控制变量。预览会提示需要确认的运行风险。</span>
            </label>
            <div className="space-y-5">
              <label className="block space-y-2">
                <span className="text-xs font-black text-foreground">应用名称</span>
                <input aria-label="应用名称" value={draft.displayName} onChange={(event) => onDraftChange({ displayName: event.target.value })} maxLength={120} placeholder="例如：网站前台" className="min-h-11 w-full rounded-2xl border border-border bg-card px-4 text-sm font-semibold text-foreground outline-none placeholder:text-muted-foreground focus:border-primary focus:ring-4 focus:ring-primary/15" />
                <span className="block text-[11px] font-semibold leading-5 text-muted-foreground">用于显示和识别，不会作为文件位置。</span>
              </label>
              <label className="block space-y-2">
                <span className="text-xs font-black text-foreground">可选 .env</span>
                <textarea aria-label="可选 .env" value={draft.envFile} onChange={(event) => onDraftChange({ envFile: event.target.value })} spellCheck={false} placeholder={'APP_ENV=production\nAPI_KEY=...'} className="min-h-48 w-full resize-y rounded-2xl border border-border bg-slate-950 px-4 py-3 font-mono text-xs font-semibold leading-5 text-slate-100 outline-none placeholder:text-slate-500 focus:border-primary focus:ring-4 focus:ring-primary/15" />
                <span className="block text-[11px] font-semibold leading-5 text-muted-foreground">仅随本次预览和部署请求传输；提交部署后会从编辑状态清除。</span>
              </label>
              <label className="flex cursor-pointer items-start gap-3 rounded-2xl border border-border bg-surface px-4 py-3">
                <input aria-label="部署前拉取镜像" type="checkbox" checked={draft.pullImages} onChange={(event) => onDraftChange({ pullImages: event.target.checked })} className="mt-0.5 h-4 w-4 rounded border-border text-primary focus:ring-primary" />
                <span><span className="block text-xs font-black text-foreground">部署前拉取镜像</span><span className="mt-1 block text-[11px] font-semibold leading-5 text-muted-foreground">关闭后使用目标节点当前已缓存的镜像。</span></span>
              </label>
            </div>
          </div>
        </div>
        <div className="flex flex-wrap justify-end gap-2 border-t border-border bg-surface px-5 py-4 sm:px-6">
          <button type="button" onClick={onClose} disabled={loading} className="min-h-10 rounded-2xl border border-border bg-card px-4 text-xs font-black text-muted-foreground transition hover:text-foreground disabled:cursor-not-allowed disabled:opacity-55">取消编辑</button>
          <button type="button" onClick={onPreview} disabled={loading} className="inline-flex min-h-10 items-center gap-2 rounded-2xl bg-primary px-4 text-xs font-black text-primary-foreground shadow-sm transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-55"><ShieldAlert size={15} aria-hidden="true" />{loading ? '正在预览...' : '预览部署'}</button>
        </div>
      </section>
    </div>
  )
}

function ManagedComposePreviewModal({ preview, loading, onCancel, onConfirm }: { preview: ManagedComposePreview; loading: boolean; onCancel: () => void; onConfirm: () => void }) {
  return (
    <div className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/55 p-4" onClick={() => { if (!loading) onCancel() }}>
      <section role="dialog" aria-modal="true" aria-label="确认托管 Compose 部署" className="w-full max-w-2xl overflow-hidden rounded-[28px] border border-border bg-card shadow-2xl" onClick={(event) => event.stopPropagation()}>
        <div className="border-b border-warning/25 bg-warning/10 px-5 py-4">
          <p className="text-[11px] font-black uppercase tracking-[0.18em] text-warning">Deployment Preview</p>
          <h3 className="mt-1 text-lg font-black text-foreground">确认托管 Compose 部署</h3>
          <p className="mt-1 text-xs font-semibold leading-5 text-muted-foreground">已验证应用 <span className="font-mono text-foreground">{preview.projectName || preview.draft.displayName}</span>。确认后 Agent 将使用预览对应的内容部署。</p>
        </div>
        <div className="max-h-[50vh] overflow-y-auto px-5 py-5">
          <p className="text-xs font-black text-foreground">风险摘要</p>
          {preview.risks.length > 0 ? <ul className="mt-3 space-y-2">{preview.risks.map((risk, index) => <li key={`${risk.code}-${index}`} className="rounded-2xl border border-warning/20 bg-warning/5 px-4 py-3"><div className="flex flex-wrap items-center gap-2"><span className="rounded-full bg-warning/15 px-2 py-0.5 text-[10px] font-black uppercase tracking-[0.08em] text-warning">{risk.severity || 'warning'}</span><span className="font-mono text-xs font-black text-foreground">{risk.code}</span></div><p className="mt-1.5 text-xs font-semibold leading-5 text-muted-foreground">{risk.message}</p></li>)}</ul> : <div className="mt-3 rounded-2xl border border-success/20 bg-success/5 px-4 py-3 text-xs font-semibold leading-5 text-muted-foreground">未检测到需要额外确认的风险。仍会按 Agent 的受控参数部署。</div>}
        </div>
        <div className="flex flex-wrap justify-end gap-2 border-t border-border bg-surface px-5 py-4">
          <button type="button" onClick={onCancel} disabled={loading} className="min-h-10 rounded-2xl border border-border bg-card px-4 text-xs font-black text-muted-foreground transition hover:text-foreground disabled:cursor-not-allowed disabled:opacity-55">取消确认</button>
          <button type="button" onClick={onConfirm} disabled={loading} className="inline-flex min-h-10 items-center gap-2 rounded-2xl bg-primary px-4 text-xs font-black text-primary-foreground shadow-sm transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-55"><Play size={15} aria-hidden="true" />{loading ? '正在部署...' : '确认并部署'}</button>
        </div>
      </section>
    </div>
  )
}

function ManagedComposeActionConfirmModal({ action, projectName, loading, onCancel, onConfirm }: { action: Extract<DockerComposeDeploymentAction, 'rollback' | 'archive'>; projectName: string; loading: boolean; onCancel: () => void; onConfirm: () => void }) {
  const rollback = action === 'rollback'
  const title = rollback ? '确认托管应用回滚' : '确认托管应用归档'
  return (
    <div className="fixed inset-0 z-[85] flex items-center justify-center bg-slate-950/45 p-4" onClick={() => { if (!loading) onCancel() }}>
      <section role="dialog" aria-modal="true" aria-label={title} className="w-full max-w-md overflow-hidden rounded-[28px] border border-border bg-card shadow-2xl" onClick={(event) => event.stopPropagation()}>
        <div className={`border-b px-5 py-4 ${rollback ? 'border-warning/25 bg-warning/10' : 'border-danger/25 bg-danger/10'}`}>
          <h3 className="text-lg font-black text-foreground">{title}</h3>
          <p className="mt-1 text-xs font-semibold leading-5 text-muted-foreground">{rollback ? '将恢复上一份保留版本并重新应用服务。' : '将停止应用并保留托管文件以便后续恢复，不会立即物理删除。'}</p>
        </div>
        <div className="px-5 py-5"><p className="text-sm font-bold text-foreground">确认操作应用：<span className="font-mono">{projectName}</span></p></div>
        <div className="flex justify-end gap-2 border-t border-border bg-surface px-5 py-4">
          <button type="button" onClick={onCancel} disabled={loading} className="min-h-10 rounded-2xl border border-border bg-card px-4 text-xs font-black text-muted-foreground disabled:cursor-not-allowed disabled:opacity-55">取消</button>
          <button type="button" onClick={onConfirm} disabled={loading} className={`min-h-10 rounded-2xl px-4 text-xs font-black text-white disabled:cursor-not-allowed disabled:opacity-55 ${rollback ? 'bg-warning' : 'bg-danger'}`}>{loading ? '正在处理...' : rollback ? '确认回滚' : '确认归档'}</button>
        </div>
      </section>
    </div>
  )
}

function ComposeServiceActionMenu({ projectName, serviceName, containerID, containerName, online, running, busy, lifecycleSupported, onAction, onOpenLogs, onOpenTerminal }: { projectName: string; serviceName: string; containerID: string; containerName: string; online: boolean; running: boolean; busy: boolean; lifecycleSupported: boolean; onAction: (action: 'pull' | 'up' | 'restart' | 'stop') => void; onOpenLogs: () => void; onOpenTerminal: () => void }) {
  const [open, setOpen] = useState(false)
  const [menuPosition, setMenuPosition] = useState<{ top: number; left: number }>()
  const menuId = useId()

  useEffect(() => {
    if (!open) return
    const closeOnOutsideClick = (event: MouseEvent) => {
      let element = event.target instanceof Element ? event.target : null
      while (element) {
        if (element.getAttribute('data-compose-service-actions-menu') === menuId) return
        element = element.parentElement
      }
      setOpen(false)
    }
    const timeoutID = window.setTimeout(() => document.addEventListener('click', closeOnOutsideClick), 0)
    return () => {
      window.clearTimeout(timeoutID)
      document.removeEventListener('click', closeOnOutsideClick)
    }
  }, [menuId, open])

  const toggleMenu = (event: ReactMouseEvent<HTMLButtonElement>) => {
    event.stopPropagation()
    const rect = event.currentTarget.getBoundingClientRect()
    const menuWidth = 176
    const menuHeight = lifecycleSupported ? 300 : 120
    setMenuPosition({
      top: Math.max(8, Math.min(rect.bottom + 6, window.innerHeight - menuHeight - 8)),
      left: Math.max(8, Math.min(rect.right - menuWidth, window.innerWidth - menuWidth - 8)),
    })
    setOpen((value) => !value)
  }

  const chooseAction = (action: 'logs' | 'terminal' | 'pull' | 'up' | 'restart' | 'stop') => {
    setOpen(false)
    if (action === 'logs') {
      onOpenLogs()
      return
    }
    if (action === 'terminal') {
      onOpenTerminal()
      return
    }
    onAction(action)
  }

  return (
    <div className="relative flex items-center justify-center">
      <button
        type="button"
        aria-label={`${serviceName} 服务操作`}
        title={`${serviceName} 服务操作`}
        data-compose-service-actions-menu={menuId}
        onClick={toggleMenu}
        disabled={busy}
        className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-xl border border-border bg-card text-muted-foreground transition hover:border-primary/30 hover:bg-primary/10 hover:text-primary focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
      >
        <MoreHorizontal size={16} aria-hidden="true" />
      </button>
      {open && !busy && menuPosition ? createPortal(
        <div
          role="menu"
          aria-label={`${serviceName} 服务操作菜单`}
          data-compose-service-actions-menu={menuId}
          className="fixed z-[70] w-44 rounded-2xl border border-border/80 bg-card/95 p-1.5 text-left shadow-[0_18px_45px_rgb(15_23_42/0.16)] backdrop-blur"
          style={{ top: menuPosition.top, left: menuPosition.left }}
          onClick={(event) => event.stopPropagation()}
        >
          <p className="px-3 pb-1 pt-1.5 font-mono text-[10px] font-black text-muted-foreground" title={`${projectName} / ${containerName}`}>{serviceName}</p>
          <ContainerActionMenuItem icon={<ScrollText size={15} />} label="查看日志" disabled={!online || !containerID} onClick={() => chooseAction('logs')} />
          <ContainerActionMenuItem icon={<Terminal size={15} />} label="进入终端" disabled={!online || !containerID || !running} onClick={() => chooseAction('terminal')} />
          {lifecycleSupported ? <>
            <div className="my-1 h-px bg-border/70" />
            <ContainerActionMenuItem icon={<Download size={15} />} label="拉取镜像" disabled={!online} onClick={() => chooseAction('pull')} />
            <ContainerActionMenuItem icon={<Play size={15} />} label="启动 / 重建" disabled={!online} onClick={() => chooseAction('up')} />
            <ContainerActionMenuItem icon={<RotateCw size={15} />} label="重启" disabled={!online || !running} onClick={() => chooseAction('restart')} />
            <ContainerActionMenuItem icon={<Square size={15} />} label="停止" disabled={!online || !running} onClick={() => chooseAction('stop')} />
          </> : null}
        </div>,
        document.body
      ) : null}
    </div>
  )
}

function ComposeActionButton({ icon, label, primary, danger, disabled, onClick }: { icon?: ReactNode; label: string; primary?: boolean; danger?: boolean; disabled?: boolean; onClick: () => void }) {
  const tone = danger ? 'border-danger/30 bg-danger/10 text-danger hover:bg-danger/15' : primary ? 'border-primary/30 bg-primary/10 text-primary hover:bg-primary/15' : 'border-border bg-card text-muted-foreground hover:text-foreground'
  return <button type="button" onClick={onClick} disabled={disabled} className={`inline-flex min-h-9 items-center gap-1.5 rounded-xl border px-3 text-xs font-black transition disabled:cursor-not-allowed disabled:opacity-45 ${tone}`}>{icon}<span>{label}</span></button>
}

function composeActionText(action: DockerComposeAction) {
  return action === 'pull' ? '拉取镜像' : action === 'up' ? '启动/重建' : action === 'restart' ? '重启' : action === 'stop' ? '停止' : action === 'down' ? '移除' : action === 'logs' ? '查看日志' : '校验配置'
}

function managedDeploymentActionText(action: DockerComposeDeploymentAction) {
  return action === 'preview' ? '预览' : action === 'apply' ? '部署' : action === 'rollback' ? '回滚' : '归档'
}

function compareProcesses(left: ProcessInfo, right: ProcessInfo, sort: ProcessSort) {
  if (sort === 'memory') return right.memory_rss - left.memory_rss || left.pid - right.pid
  if (sort === 'pid') return left.pid - right.pid
  if (sort === 'name') return left.name.localeCompare(right.name) || left.pid - right.pid
  return right.cpu_usage - left.cpu_usage || left.pid - right.pid
}

function dockerFilterFor(container: DockerContainer): DockerFilter {
  const state = container.state.toLowerCase()
  if (state === 'running') return 'running'
  if (state === 'exited' || state === 'dead' || state === 'created') return 'stopped'
  if (state === 'restarting' || state === 'removing' || state === 'paused') return 'abnormal'
  return 'abnormal'
}

function formatUnixTime(value?: number) {
  if (!value) return '暂无快照'
  return new Date(value * 1000).toLocaleString('zh-CN', { hour12: false })
}
