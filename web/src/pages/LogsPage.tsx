import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Box, Copy, Download, FileText, Play, RefreshCw, Search, Server, Settings2, Square } from 'lucide-react'

import { getAgentLogs, getNodeDocker, getNodeSystemdServices, getSystemLogs, runNodeSystemdServiceAction } from '../api/client'
import { fetchK8sClusters, fetchK8sNamespaces, fetchK8sPodLogs, fetchK8sPods } from '../api/k8s'
import { Toast } from '../components/Toast'
import type { DockerContainer, K8sCluster, K8sNamespace, K8sPod, LogSource, Node, SystemdService } from '../types'

type LogsPageProps = {
  nodes: Node[]
}

type LogSnapshot = {
  source: LogSource
  target: string
  scope?: string
  lines: string[]
  collectedAt: string
  truncated: boolean
}

type StreamKind = 'docker' | 'file'

type ActiveStream = {
  kind: StreamKind
  socket: WebSocket
  sessionID: string
  nodeID: string
}

const sourceOptions: Array<{ id: LogSource; label: string; description: string; streaming: boolean }> = [
  { id: 'docker', label: 'Docker 容器', description: '容器标准输出与错误输出', streaming: true },
  { id: 'systemd', label: 'Systemd 服务', description: 'journalctl 快照查询', streaming: false },
  { id: 'kubernetes', label: 'Kubernetes Pod', description: '集群 Pod 容器日志', streaming: false },
  { id: 'agent', label: 'Agent 自身', description: '当前节点 Agent 服务日志', streaming: false },
  { id: 'server', label: 'Server 自身', description: '当前进程内存缓冲', streaming: false },
  { id: 'file', label: '主机日志文件', description: '指定路径的持续 tail', streaming: true },
]

const maxClientLines = 10_000

function sourceLabel(source: LogSource) {
  return sourceOptions.find((option) => option.id === source)?.label ?? source
}

function clampLines(value: number) {
  if (!Number.isFinite(value)) return 200
  return Math.min(2000, Math.max(20, Math.round(value)))
}

function formatCollectedAt(value: string | number | undefined) {
  if (typeof value === 'number') return new Date(value * 1000).toLocaleString()
  if (!value) return '—'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
}

function splitLines(content: string) {
  return content.replace(/\r\n/g, '\n').split('\n').filter((line, index, all) => line !== '' || index < all.length - 1)
}

function sessionID() {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '请求失败'
}

function highlightLine(line: string, query: string) {
  const normalizedQuery = query.trim()
  if (!normalizedQuery) return line
  const lowerLine = line.toLowerCase()
  const lowerQuery = normalizedQuery.toLowerCase()
  const fragments: ReactNode[] = []
  let start = 0
  let index = lowerLine.indexOf(lowerQuery, start)
  let key = 0
  while (index >= 0) {
    if (index > start) fragments.push(line.slice(start, index))
    fragments.push(<mark key={key++} className="rounded bg-warning/40 px-0.5 text-inherit">{line.slice(index, index + normalizedQuery.length)}</mark>)
    start = index + normalizedQuery.length
    index = lowerLine.indexOf(lowerQuery, start)
  }
  if (start < line.length) fragments.push(line.slice(start))
  return fragments
}

export function LogsPage({ nodes }: LogsPageProps) {
  const [source, setSource] = useState<LogSource>('docker')
  const [nodeID, setNodeID] = useState('')
  const [lines, setLines] = useState(200)
  const [keyword, setKeyword] = useState('')
  const [snapshot, setSnapshot] = useState<LogSnapshot | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [isFollowing, setIsFollowing] = useState(false)
  const [autoScroll, setAutoScroll] = useState(true)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)

  const [containers, setContainers] = useState<DockerContainer[]>([])
  const [containerID, setContainerID] = useState('')
  const [services, setServices] = useState<SystemdService[]>([])
  const [serviceName, setServiceName] = useState('')
  const [filePath, setFilePath] = useState('')
  const [clusters, setClusters] = useState<K8sCluster[]>([])
  const [clusterID, setClusterID] = useState('')
  const [namespaces, setNamespaces] = useState<K8sNamespace[]>([])
  const [namespace, setNamespace] = useState('')
  const [pods, setPods] = useState<K8sPod[]>([])
  const [podName, setPodName] = useState('')
  const [podContainer, setPodContainer] = useState('')
  const [loadingTargets, setLoadingTargets] = useState(false)

  const consoleRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  const requestIDRef = useRef(0)
  const streamTokenRef = useRef(0)
  const activeStreamRef = useRef<ActiveStream | null>(null)

  const onlineNodes = useMemo(() => nodes.filter((node) => node.status === 'online'), [nodes])
  const selectedNode = useMemo(() => nodes.find((node) => node.id === nodeID), [nodes, nodeID])
  const selectedCluster = useMemo(() => clusters.find((cluster) => cluster.id === clusterID), [clusters, clusterID])
  const selectedPod = useMemo(() => pods.find((pod) => pod.name === podName), [pods, podName])
  const selectedSource = useMemo(() => sourceOptions.find((option) => option.id === source)!, [source])
  const filteredLines = useMemo(() => {
    if (!snapshot) return []
    const normalizedKeyword = keyword.trim().toLowerCase()
    return normalizedKeyword ? snapshot.lines.filter((line) => line.toLowerCase().includes(normalizedKeyword)) : snapshot.lines
  }, [keyword, snapshot])

  useEffect(() => {
    if (!nodeID && onlineNodes[0]) setNodeID(onlineNodes[0].id)
  }, [nodeID, onlineNodes])

  const closeStream = useCallback(() => {
    streamTokenRef.current += 1
    const active = activeStreamRef.current
    activeStreamRef.current = null
    setIsFollowing(false)
    setLoading(false)
    if (!active) return
    if (active.socket.readyState === WebSocket.OPEN) {
      const stop = active.kind === 'docker'
        ? { type: 'container_logs_stop', session_id: active.sessionID, node_id: active.nodeID }
        : { type: 'log_tail_stop', session_id: active.sessionID, node_id: active.nodeID }
      active.socket.send(JSON.stringify(stop))
    }
    active.socket.close()
  }, [])

  const cancelSnapshot = useCallback(() => {
    requestIDRef.current += 1
    abortRef.current?.abort()
    abortRef.current = null
  }, [])

  useEffect(() => () => {
    cancelSnapshot()
    closeStream()
  }, [cancelSnapshot, closeStream])

  useEffect(() => {
    if (autoScroll && consoleRef.current) consoleRef.current.scrollTop = consoleRef.current.scrollHeight
  }, [autoScroll, snapshot?.lines.length])

  const beginSnapshot = useCallback(() => {
    closeStream()
    cancelSnapshot()
    const controller = new AbortController()
    abortRef.current = controller
    const requestID = ++requestIDRef.current
    setError('')
    setLoading(true)
    setIsFollowing(false)
    return { controller, requestID }
  }, [cancelSnapshot, closeStream])

  const setSnapshotIfCurrent = useCallback((requestID: number, next: LogSnapshot) => {
    if (requestID !== requestIDRef.current) return
    setSnapshot(next)
    setError('')
  }, [])

  const loadDockerTargets = useCallback(async () => {
    if (!nodeID) {
      setError('请先选择节点')
      return
    }
    const controller = new AbortController()
    setLoadingTargets(true)
    setError('')
    try {
      const response = await getNodeDocker(nodeID, controller.signal)
      if (!response.available) throw new Error(response.error || '当前节点未提供 Docker')
      setContainers(response.containers || [])
      setContainerID((current) => response.containers.some((container) => container.id === current) ? current : response.containers[0]?.id || '')
    } catch (requestError) {
      if ((requestError as { name?: string }).name !== 'AbortError') setError(errorMessage(requestError))
    } finally {
      setLoadingTargets(false)
    }
  }, [nodeID])

  const loadSystemdTargets = useCallback(async () => {
    if (!nodeID) {
      setError('请先选择节点')
      return
    }
    const controller = new AbortController()
    setLoadingTargets(true)
    setError('')
    try {
      const response = await getNodeSystemdServices(nodeID, controller.signal)
      if (!response.success) throw new Error(response.error || '当前节点未提供 Systemd 服务')
      setServices(response.services || [])
      setServiceName((current) => response.services.some((service) => service.name === current) ? current : response.services[0]?.name || '')
    } catch (requestError) {
      if ((requestError as { name?: string }).name !== 'AbortError') setError(errorMessage(requestError))
    } finally {
      setLoadingTargets(false)
    }
  }, [nodeID])

  const loadClusters = useCallback(async () => {
    const controller = new AbortController()
    setLoadingTargets(true)
    setError('')
    try {
      const response = await fetchK8sClusters(controller.signal)
      const available = response.clusters || []
      setClusters(available)
      setClusterID((current) => available.some((cluster) => cluster.id === current) ? current : available[0]?.id || '')
    } catch (requestError) {
      if ((requestError as { name?: string }).name !== 'AbortError') setError(errorMessage(requestError))
    } finally {
      setLoadingTargets(false)
    }
  }, [])

  const loadNamespaces = useCallback(async () => {
    if (!clusterID) {
      setError('请先选择 Kubernetes 集群')
      return
    }
    const controller = new AbortController()
    setLoadingTargets(true)
    setError('')
    try {
      const response = await fetchK8sNamespaces(clusterID, controller.signal)
      const available = response.namespaces || []
      setNamespaces(available)
      setNamespace((current) => available.some((item) => item.name === current) ? current : available[0]?.name || '')
      setPods([])
      setPodName('')
      setPodContainer('')
    } catch (requestError) {
      if ((requestError as { name?: string }).name !== 'AbortError') setError(errorMessage(requestError))
    } finally {
      setLoadingTargets(false)
    }
  }, [clusterID])

  const loadPods = useCallback(async () => {
    if (!clusterID || !namespace) {
      setError('请先选择 Kubernetes 集群和命名空间')
      return
    }
    const controller = new AbortController()
    setLoadingTargets(true)
    setError('')
    try {
      const response = await fetchK8sPods(clusterID, namespace, controller.signal)
      const available = response.pods || []
      setPods(available)
      setPodName((current) => available.some((pod) => pod.name === current) ? current : available[0]?.name || '')
      setPodContainer('')
    } catch (requestError) {
      if ((requestError as { name?: string }).name !== 'AbortError') setError(errorMessage(requestError))
    } finally {
      setLoadingTargets(false)
    }
  }, [clusterID, namespace])

  const appendStreamLines = useCallback((token: number, linesToAppend: string[]) => {
    if (token !== streamTokenRef.current) return
    setSnapshot((current) => {
      if (!current) return current
      return { ...current, lines: [...current.lines, ...linesToAppend].slice(-maxClientLines) }
    })
  }, [])

  const startDockerStream = useCallback((follow: boolean) => {
    if (!nodeID || !containerID) {
      setError('请先选择节点和容器')
      return
    }
    cancelSnapshot()
    closeStream()
    const selectedContainer = containers.find((container) => container.id === containerID)
    const token = ++streamTokenRef.current
    const streamSessionID = sessionID()
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/nodes/${encodeURIComponent(nodeID)}/containers/${encodeURIComponent(containerID)}/logs/stream`)
    activeStreamRef.current = { kind: 'docker', socket, sessionID: streamSessionID, nodeID }
    setSnapshot({ source: 'docker', target: selectedContainer?.name || containerID, scope: selectedNode?.name || nodeID, lines: [], collectedAt: new Date().toISOString(), truncated: false })
    setError('')
    setLoading(true)
    setIsFollowing(false)

    socket.onopen = () => {
      if (token !== streamTokenRef.current) return socket.close()
      socket.send(JSON.stringify({ type: 'container_logs_request', session_id: streamSessionID, node_id: nodeID, container_id: containerID, lines: clampLines(lines), follow, timestamps: false }))
    }
    socket.onmessage = (event) => {
      if (token !== streamTokenRef.current) return
      try {
        const message = JSON.parse(event.data) as { type?: string; data?: string; error?: string; started?: boolean }
        if (message.type === 'container_logs_response') {
          setLoading(false)
          setIsFollowing(Boolean(follow && message.started))
        } else if (message.type === 'container_logs_data' && typeof message.data === 'string') {
          appendStreamLines(token, splitLines(message.data))
        } else if (message.type === 'container_logs_error' || message.type === 'container_logs_exit') {
          if (message.error) setError(message.error)
          setIsFollowing(false)
          setLoading(false)
        }
      } catch {
        setError('容器日志响应格式无效')
        setLoading(false)
      }
    }
    socket.onerror = () => {
      if (token === streamTokenRef.current) {
        setError('容器日志连接失败')
        setLoading(false)
        setIsFollowing(false)
      }
    }
    socket.onclose = () => {
      if (token === streamTokenRef.current) {
        setLoading(false)
        setIsFollowing(false)
      }
    }
  }, [appendStreamLines, cancelSnapshot, closeStream, containerID, containers, lines, nodeID, selectedNode?.name])

  const startFileStream = useCallback(() => {
    if (!nodeID || !filePath.trim()) {
      setError('请先选择节点并输入绝对日志路径')
      return
    }
    cancelSnapshot()
    closeStream()
    const token = ++streamTokenRef.current
    const streamSessionID = sessionID()
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/nodes/${encodeURIComponent(nodeID)}/logs/tail`)
    activeStreamRef.current = { kind: 'file', socket, sessionID: streamSessionID, nodeID }
    setSnapshot({ source: 'file', target: filePath.trim(), scope: selectedNode?.name || nodeID, lines: [], collectedAt: new Date().toISOString(), truncated: false })
    setError('')
    setLoading(true)
    setIsFollowing(false)

    socket.onopen = () => {
      if (token !== streamTokenRef.current) return socket.close()
      socket.send(JSON.stringify({ type: 'log_tail_request', session_id: streamSessionID, path: filePath.trim(), lines: clampLines(lines) }))
    }
    socket.onmessage = (event) => {
      if (token !== streamTokenRef.current) return
      try {
        const message = JSON.parse(event.data) as { type?: string; data?: string; error?: string; started?: boolean }
        if (message.type === 'log_tail_response') {
          setLoading(false)
          setIsFollowing(Boolean(message.started))
        } else if (message.type === 'log_tail_data' && typeof message.data === 'string') {
          appendStreamLines(token, splitLines(message.data))
        } else if (message.type === 'log_tail_error' || message.type === 'log_tail_exit') {
          if (message.error) setError(message.error)
          setLoading(false)
          setIsFollowing(false)
        }
      } catch {
        setError('文件日志响应格式无效')
        setLoading(false)
      }
    }
    socket.onerror = () => {
      if (token === streamTokenRef.current) {
        setError('文件日志连接失败')
        setLoading(false)
        setIsFollowing(false)
      }
    }
    socket.onclose = () => {
      if (token === streamTokenRef.current) {
        setLoading(false)
        setIsFollowing(false)
      }
    }
  }, [appendStreamLines, cancelSnapshot, closeStream, filePath, lines, nodeID, selectedNode?.name])

  const refreshLogs = useCallback(async () => {
    if (source === 'docker') return startDockerStream(false)
    if (source === 'file') return startFileStream()

    const { controller, requestID } = beginSnapshot()
    try {
      if (source === 'agent') {
        if (!nodeID) throw new Error('请先选择节点')
        const response = await getAgentLogs(nodeID, clampLines(lines), controller.signal)
        if (response.error) throw new Error(response.error)
        setSnapshotIfCurrent(requestID, { source, target: 'mizupanel-agent', scope: selectedNode?.name || nodeID, lines: splitLines(response.content || ''), collectedAt: formatCollectedAt(response.collected_at), truncated: Boolean(response.truncated) })
      } else if (source === 'server') {
        const response = await getSystemLogs(clampLines(lines), controller.signal)
        setSnapshotIfCurrent(requestID, { source, target: 'mizupanel-server', lines: splitLines(response.content || ''), collectedAt: response.collected_at, truncated: response.truncated })
      } else if (source === 'systemd') {
        if (!nodeID || !serviceName) throw new Error('请先选择节点和 Systemd 服务')
        const response = await runNodeSystemdServiceAction(nodeID, serviceName, 'logs', clampLines(lines), controller.signal)
        if (!response.success) throw new Error(response.error || '读取 Systemd 服务日志失败')
        setSnapshotIfCurrent(requestID, { source, target: serviceName, scope: selectedNode?.name || nodeID, lines: splitLines(response.output || ''), collectedAt: new Date().toISOString(), truncated: false })
      } else if (source === 'kubernetes') {
        if (!clusterID || !namespace || !podName) throw new Error('请先选择集群、命名空间和 Pod')
        const response = await fetchK8sPodLogs(clusterID, namespace, podName, podContainer || undefined, false, clampLines(lines), controller.signal)
        if (!response.success) throw new Error('读取 Kubernetes Pod 日志失败')
        setSnapshotIfCurrent(requestID, { source, target: podContainer ? `${podName}/${podContainer}` : podName, scope: `${selectedCluster?.name || clusterID} · ${namespace}`, lines: splitLines(response.logs || ''), collectedAt: new Date().toISOString(), truncated: false })
      }
    } catch (requestError) {
      if ((requestError as { name?: string }).name !== 'AbortError' && requestID === requestIDRef.current) setError(errorMessage(requestError))
    } finally {
      if (requestID === requestIDRef.current) setLoading(false)
    }
  }, [beginSnapshot, clusterID, lines, namespace, nodeID, podContainer, podName, selectedCluster?.name, selectedNode?.name, serviceName, setSnapshotIfCurrent, source, startDockerStream, startFileStream])

  const changeSource = useCallback((nextSource: LogSource) => {
    cancelSnapshot()
    closeStream()
    setSource(nextSource)
    setSnapshot(null)
    setError('')
    setIsFollowing(false)
  }, [cancelSnapshot, closeStream])

  const handleCopy = useCallback(async () => {
    const text = filteredLines.join('\n')
    if (!text) return setToast({ message: '日志复制失败: 当前没有可复制内容', type: 'error' })
    try {
      await navigator.clipboard.writeText(text)
      setToast({ message: '日志复制成功', type: 'success' })
    } catch (copyError) {
      setToast({ message: `日志复制失败: ${errorMessage(copyError)}`, type: 'error' })
    }
  }, [filteredLines])

  const handleDownload = useCallback(() => {
    const text = filteredLines.join('\n')
    if (!text) return setToast({ message: '日志下载失败: 当前没有可下载内容', type: 'error' })
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const href = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = href
    link.download = `mizupanel-${source}-${new Date().toISOString().replace(/[:.]/g, '-')}.log`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(href)
    setToast({ message: '日志下载成功', type: 'success' })
  }, [filteredLines, source])

  const targetControls = source === 'docker' ? (
    <>
      <NodeSelect nodes={nodes} value={nodeID} onChange={(value) => { setNodeID(value); setContainers([]); setContainerID('') }} />
      <button type="button" onClick={loadDockerTargets} disabled={loadingTargets || !nodeID} className="soft-button h-10 shrink-0 border border-border bg-card px-3 text-xs font-black text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">
        {loadingTargets ? '加载中...' : '加载容器'}
      </button>
      <select aria-label="Docker 容器" value={containerID} onChange={(event) => setContainerID(event.target.value)} className="soft-input h-10 min-w-[190px] flex-1 px-3 text-sm font-semibold" disabled={containers.length === 0}>
        <option value="">选择容器</option>
        {containers.map((container) => <option key={container.id} value={container.id}>{container.name || container.id.slice(0, 12)} · {container.image}</option>)}
      </select>
    </>
  ) : source === 'systemd' ? (
    <>
      <NodeSelect nodes={nodes} value={nodeID} onChange={(value) => { setNodeID(value); setServices([]); setServiceName('') }} />
      <button type="button" onClick={loadSystemdTargets} disabled={loadingTargets || !nodeID} className="soft-button h-10 shrink-0 border border-border bg-card px-3 text-xs font-black text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">
        {loadingTargets ? '加载中...' : '加载服务'}
      </button>
      <select aria-label="Systemd 服务" value={serviceName} onChange={(event) => setServiceName(event.target.value)} className="soft-input h-10 min-w-[190px] flex-1 px-3 text-sm font-semibold" disabled={services.length === 0}>
        <option value="">选择服务</option>
        {services.map((service) => <option key={service.name} value={service.name}>{service.name}{service.active_state ? ` · ${service.active_state}` : ''}</option>)}
      </select>
    </>
  ) : source === 'agent' ? (
    <NodeSelect nodes={nodes} value={nodeID} onChange={setNodeID} />
  ) : source === 'file' ? (
    <>
      <NodeSelect nodes={nodes} value={nodeID} onChange={setNodeID} />
      <input aria-label="日志文件路径" value={filePath} onChange={(event) => setFilePath(event.target.value)} placeholder="/var/log/messages" className="soft-input h-10 min-w-[260px] flex-1 px-3 text-sm font-semibold placeholder:text-muted-foreground" />
    </>
  ) : source === 'kubernetes' ? (
    <>
      <button type="button" onClick={loadClusters} disabled={loadingTargets} className="soft-button h-10 shrink-0 border border-border bg-card px-3 text-xs font-black text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">{loadingTargets ? '加载中...' : '加载集群'}</button>
      <select aria-label="Kubernetes 集群" value={clusterID} onChange={(event) => { setClusterID(event.target.value); setNamespaces([]); setNamespace(''); setPods([]); setPodName(''); setPodContainer('') }} className="soft-input h-10 min-w-[170px] flex-1 px-3 text-sm font-semibold" disabled={clusters.length === 0}>
        <option value="">选择集群</option>
        {clusters.map((cluster) => <option key={cluster.id} value={cluster.id}>{cluster.name} · {cluster.status}</option>)}
      </select>
      <button type="button" onClick={loadNamespaces} disabled={loadingTargets || !clusterID} className="soft-button h-10 shrink-0 border border-border bg-card px-3 text-xs font-black text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">命名空间</button>
      <select aria-label="Kubernetes 命名空间" value={namespace} onChange={(event) => { setNamespace(event.target.value); setPods([]); setPodName(''); setPodContainer('') }} className="soft-input h-10 min-w-[150px] flex-1 px-3 text-sm font-semibold" disabled={namespaces.length === 0}>
        <option value="">选择命名空间</option>
        {namespaces.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}
      </select>
      <button type="button" onClick={loadPods} disabled={loadingTargets || !clusterID || !namespace} className="soft-button h-10 shrink-0 border border-border bg-card px-3 text-xs font-black text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">Pod</button>
      <select aria-label="Kubernetes Pod" value={podName} onChange={(event) => { setPodName(event.target.value); setPodContainer('') }} className="soft-input h-10 min-w-[160px] flex-1 px-3 text-sm font-semibold" disabled={pods.length === 0}>
        <option value="">选择 Pod</option>
        {pods.map((pod) => <option key={pod.name} value={pod.name}>{pod.name} · {pod.status}</option>)}
      </select>
      {selectedPod && (selectedPod.containers || []).length > 1 ? <select aria-label="Pod 容器" value={podContainer} onChange={(event) => setPodContainer(event.target.value)} className="soft-input h-10 min-w-[150px] flex-1 px-3 text-sm font-semibold"><option value="">默认容器</option>{selectedPod.containers?.map((container) => <option key={container.name} value={container.name}>{container.name}</option>)}</select> : null}
    </>
  ) : <p className="text-sm font-semibold text-muted-foreground">读取当前 Server 进程启动以来的有界内存日志；不会写入数据库。</p>

  return (
    <div className="mx-auto flex h-full w-full max-w-[1400px] flex-col gap-4">
      {toast ? <Toast message={toast.message} type={toast.type} onClose={() => setToast(null)} /> : null}
      <section className="soft-panel shrink-0 p-5">
        <div className="flex flex-wrap items-start justify-between gap-4 border-b border-border pb-4">
          <div>
            <h1 className="text-2xl font-black text-foreground">统一日志中心</h1>
            <p className="mt-1 text-sm font-semibold text-muted-foreground">按需查询单一目标；日志不会集中采集、持久化或建立索引。</p>
          </div>
          <div className="rounded-full border border-border bg-muted/50 px-3 py-1.5 text-xs font-black text-muted-foreground">第一阶段 · 单目标排障</div>
        </div>

        <div className="mt-4 grid gap-2 xl:grid-cols-6">
          {sourceOptions.map((option) => {
            const Icon = option.id === 'docker' ? Box : option.id === 'systemd' ? Settings2 : option.id === 'kubernetes' ? Box : option.id === 'agent' ? Server : option.id === 'server' ? Server : FileText
            const active = option.id === source
            return <button key={option.id} type="button" onClick={() => changeSource(option.id)} className={`rounded-xl border p-3 text-left transition focus:outline-none focus:ring-4 focus:ring-primary/15 ${active ? 'border-primary bg-primary/10 shadow-sm' : 'border-border bg-card hover:border-primary/40 hover:bg-muted/60'}`} aria-pressed={active}>
              <span className="flex items-center gap-2 text-sm font-black text-foreground"><Icon size={16} className={active ? 'text-primary' : 'text-muted-foreground'} />{option.label}</span>
              <span className="mt-1 block text-xs font-semibold leading-5 text-muted-foreground">{option.description}</span>
            </button>
          })}
        </div>

        <div className="soft-toolbar mt-4 flex flex-wrap items-center gap-2 p-3">
          {targetControls}
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-2">
          <label className="flex h-10 items-center gap-2 rounded-lg border border-border bg-card px-3 text-xs font-black text-muted-foreground">
            尾部行数
            <input aria-label="尾部行数" type="number" min="20" max="2000" value={lines} onChange={(event) => setLines(clampLines(Number(event.target.value)))} className="w-16 bg-transparent text-right text-sm font-black text-foreground outline-none" />
          </label>
          <label className="relative min-w-[240px] flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" size={15} />
            <input aria-label="搜索当前日志" value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="在当前结果中搜索关键词" className="soft-input h-10 w-full pl-9 pr-3 text-sm font-semibold placeholder:text-muted-foreground" />
          </label>
          <button type="button" onClick={refreshLogs} disabled={loading} className="soft-button inline-flex h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground hover:brightness-95 disabled:cursor-not-allowed disabled:opacity-60"><RefreshCw size={15} className={loading ? 'animate-spin' : ''} />刷新</button>
          {selectedSource.streaming ? isFollowing ? <button type="button" onClick={closeStream} className="soft-button inline-flex h-10 items-center gap-2 bg-danger px-4 text-sm font-black text-white hover:bg-danger/90"><Square size={14} />停止跟随</button> : <button type="button" onClick={() => source === 'docker' ? startDockerStream(true) : startFileStream()} className="soft-button inline-flex h-10 items-center gap-2 border border-success/40 bg-success/10 px-4 text-sm font-black text-success hover:bg-success/20"><Play size={14} />开始跟随</button> : null}
          <button type="button" onClick={handleCopy} className="soft-button inline-flex h-10 items-center gap-2 border border-border bg-card px-3 text-sm font-black text-foreground hover:bg-muted"><Copy size={15} />复制</button>
          <button type="button" onClick={handleDownload} className="soft-button inline-flex h-10 items-center gap-2 border border-border bg-card px-3 text-sm font-black text-foreground hover:bg-muted"><Download size={15} />下载</button>
        </div>
      </section>

      <section className="soft-panel flex min-h-0 flex-1 flex-col overflow-hidden">
        <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3">
          <div className="min-w-0">
            <p className="truncate text-sm font-black text-foreground">{snapshot ? `${sourceLabel(snapshot.source)} · ${snapshot.target}` : `${sourceLabel(source)} · 等待选择目标`}</p>
            <p className="mt-0.5 text-xs font-semibold text-muted-foreground">{snapshot?.scope ? `${snapshot.scope} · ` : ''}采集于 {snapshot ? formatCollectedAt(snapshot.collectedAt) : '—'}{snapshot?.truncated ? ' · 输出已截断' : ''}</p>
          </div>
          <span className={`inline-flex items-center gap-2 rounded-full px-3 py-1 text-xs font-black ${isFollowing ? 'bg-success/15 text-success' : loading ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}><span className={`h-1.5 w-1.5 rounded-full ${isFollowing ? 'animate-pulse bg-success' : loading ? 'animate-pulse bg-primary' : 'bg-muted-foreground/60'}`} />{isFollowing ? '实时跟随中' : loading ? '查询中' : '快照模式'}</span>
        </header>
        {error ? <div className="mx-5 mt-4 rounded-xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-semibold text-danger">{error}</div> : null}
        <div ref={consoleRef} onScroll={() => { const element = consoleRef.current; if (element) setAutoScroll(Math.abs(element.scrollHeight - element.scrollTop - element.clientHeight) < 12) }} className="m-5 mt-4 min-h-0 flex-1 overflow-auto rounded-xl border border-slate-700/70 bg-code p-4 font-mono text-xs leading-6 text-code-foreground shadow-inner">
          {snapshot && snapshot.lines.length > 0 ? <div className="whitespace-pre-wrap break-words">{filteredLines.map((line, index) => <div key={`${index}-${line.slice(0, 32)}`} className="min-h-6 border-l-2 border-transparent pl-3 hover:border-primary/50 hover:bg-white/5">{highlightLine(line, keyword)}</div>)}</div> : <div className="flex h-full min-h-48 items-center justify-center text-center font-sans"><div><p className="text-sm font-black text-code-foreground">{loading ? '正在读取日志...' : '选择来源和目标后点击刷新'}</p><p className="mt-2 max-w-md text-xs font-semibold leading-5 text-muted-foreground">Docker 和主机文件可开启实时跟随；其他来源仅提供受上限约束的手动快照。</p></div></div>}
        </div>
        <footer className="flex flex-wrap items-center justify-between gap-2 border-t border-border px-5 py-3 text-xs font-semibold text-muted-foreground"><span>当前 {snapshot?.lines.length || 0} 行{keyword.trim() ? ` · 筛选后 ${filteredLines.length} 行` : ''}{snapshot && snapshot.lines.length >= maxClientLines ? ` · 浏览器仅保留最近 ${maxClientLines.toLocaleString()} 行` : ''}</span><span>{autoScroll ? '自动滚动已开启' : '已暂停自动滚动'}</span></footer>
      </section>
    </div>
  )
}

function NodeSelect({ nodes, value, onChange }: { nodes: Node[]; value: string; onChange: (value: string) => void }) {
  return <select aria-label="节点" value={value} onChange={(event) => onChange(event.target.value)} className="soft-input h-10 min-w-[180px] flex-1 px-3 text-sm font-semibold"><option value="">选择节点</option>{nodes.map((node) => <option key={node.id} value={node.id}>{node.name || node.hostname || node.id} · {node.status === 'online' ? '在线' : '离线'}</option>)}</select>
}
