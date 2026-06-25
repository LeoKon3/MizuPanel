import type { CSSProperties, ReactNode } from 'react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowLeft, Check, ChevronDown, Plus, RefreshCw, Search } from 'lucide-react'
import { createPortal } from 'react-dom'
import {
  fetchK8sCluster,
  fetchK8sSummary,
  fetchK8sNamespaces,
  fetchK8sNodes,
  fetchK8sPods,
  fetchK8sDeployments,
  fetchK8sStatefulSets,
  fetchK8sDaemonSets,
  fetchK8sServices,
  fetchK8sIngresses,
} from '../api/k8s'
import type {
  K8sCluster,
  K8sResourceSummary,
  K8sNamespace,
  K8sNode,
  K8sPod,
  K8sDeployment,
  K8sStatefulSet,
  K8sDaemonSet,
  K8sService,
  K8sIngress,
  K8sResourceKind,
} from '../types'
import { Toast } from '../components/Toast'
import K8sPodLogsModal from '../components/K8sPodLogsModal'
import { K8sDiagnosticsDrawer } from '../components/k8s/K8sDiagnosticsDrawer'
import { K8sNamespaceTable } from '../components/k8s/K8sNamespaceTable'
import { K8sNodeTable } from '../components/k8s/K8sNodeTable'
import { K8sPodTable } from '../components/k8s/K8sPodTable'
import { K8sWorkloadTable } from '../components/k8s/K8sWorkloadTable'
import { K8sServiceTable } from '../components/k8s/K8sServiceTable'
import { K8sIngressTable } from '../components/k8s/K8sIngressTable'
import { K8sCreateResourceModal } from '../components/k8s/K8sCreateResourceModal'

type K8sClusterDetailPageProps = {
  clusterId: string
  onBack: () => void
}

type DetailTab = 'overview' | 'namespaces' | 'nodes' | 'pods' | 'deployments' | 'statefulsets' | 'daemonsets' | 'services' | 'ingresses'

const DETAIL_TABS: Array<{ key: DetailTab; label: string; shortLabel?: string }> = [
  { key: 'overview', label: '集群概览', shortLabel: 'Summary' },
  { key: 'namespaces', label: '命名空间', shortLabel: 'NS' },
  { key: 'nodes', label: '节点', shortLabel: 'Nodes' },
  { key: 'pods', label: 'Pods' },
  { key: 'deployments', label: 'Deployments' },
  { key: 'statefulsets', label: 'StatefulSets' },
  { key: 'daemonsets', label: 'DaemonSets' },
  { key: 'services', label: 'Services' },
  { key: 'ingresses', label: 'Ingresses' },
]

const NAMESPACE_SCOPED_TABS = new Set<DetailTab>(['pods', 'deployments', 'statefulsets', 'daemonsets', 'services', 'ingresses'])

function tabLabel(tab: DetailTab): string {
  return DETAIL_TABS.find((item) => item.key === tab)?.label || tab
}

function formatDateTime(value?: string): string {
  return value ? new Date(value).toLocaleString('zh-CN') : ''
}

function matchesSearch(values: Array<string | number | undefined>, search: string): boolean {
  if (!search) return true
  return values.some((value) => String(value ?? '').toLowerCase().includes(search))
}

export function K8sClusterDetailPage({ clusterId, onBack }: K8sClusterDetailPageProps) {
  const [cluster, setCluster] = useState<K8sCluster>()
  const [summary, setSummary] = useState<K8sResourceSummary>()
  const [namespaces, setNamespaces] = useState<K8sNamespace[]>([])
  const [nodes, setNodes] = useState<K8sNode[]>([])
  const [pods, setPods] = useState<K8sPod[]>([])
  const [deployments, setDeployments] = useState<K8sDeployment[]>([])
  const [statefulsets, setStatefulSets] = useState<K8sStatefulSet[]>([])
  const [daemonsets, setDaemonSets] = useState<K8sDaemonSet[]>([])
  const [services, setServices] = useState<K8sService[]>([])
  const [ingresses, setIngresses] = useState<K8sIngress[]>([])
  const [activeTab, setActiveTab] = useState<DetailTab>('overview')
  const [namespace, setNamespace] = useState<string>('')
  const [resourceSearch, setResourceSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [resourcesLoading, setResourcesLoading] = useState(false)
  const [namespacesLoading, setNamespacesLoading] = useState(false)
  const [resourceError, setResourceError] = useState<string>()
  const [error, setError] = useState<string>()
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [logsModal, setLogsModal] = useState<{ open: boolean; namespace: string; podName: string }>({
    open: false,
    namespace: '',
    podName: '',
  })
  const [diagnosticsDrawer, setDiagnosticsDrawer] = useState<{ open: boolean; kind: K8sResourceKind; namespace: string; name: string; relatedPods?: K8sPod[] } | null>(null)
  const [createResourceOpen, setCreateResourceOpen] = useState(false)
  const clusterRequestSeq = useRef(0)
  const resourceRequestSeq = useRef(0)
  const namespaceRequestSeq = useRef(0)
  const namespaceRef = useRef('')
  const previousNamespaceRef = useRef(namespace)

  useEffect(() => {
    namespaceRef.current = namespace
  }, [namespace])

  const loadCluster = useCallback(() => {
    const requestID = clusterRequestSeq.current + 1
    clusterRequestSeq.current = requestID
    resourceRequestSeq.current += 1
    namespaceRequestSeq.current += 1
    const isCurrentRequest = () => clusterRequestSeq.current === requestID

    setLoading(true)
    setNamespacesLoading(false)
    setError(undefined)
    setResourceError(undefined)
    setSummary(undefined)
    setNamespaces([])
    setNodes([])
    setPods([])
    setDeployments([])
    setStatefulSets([])
    setDaemonSets([])
    setServices([])
    setIngresses([])

    fetchK8sCluster(clusterId)
      .then((response) => {
        if (isCurrentRequest()) setCluster(response.cluster)
      })
      .catch((err: Error) => {
        if (!isCurrentRequest()) return
        setError(err.message)
        setToast({ message: `集群加载失败: ${err.message}`, type: 'error' })
      })
      .finally(() => {
        if (isCurrentRequest()) setLoading(false)
      })
  }, [clusterId])

  const loadActiveResource = useCallback((requestedNamespace?: string) => {
    const effectiveNamespace = requestedNamespace ?? namespaceRef.current
    const agentOnline = cluster?.node_status ? cluster.node_status === 'online' : true
    if (!cluster || cluster.status !== 'online' || !agentOnline) return

    const requestID = resourceRequestSeq.current + 1
    resourceRequestSeq.current = requestID
    const isCurrentRequest = () => resourceRequestSeq.current === requestID

    setResourcesLoading(true)
    setResourceError(undefined)

    const fail = (label: string, err: Error) => {
      if (!isCurrentRequest()) return
      setResourceError(`${label}加载失败: ${err.message}`)
      setToast({ message: `${label}加载失败: ${err.message}`, type: 'error' })
    }

    let request: Promise<unknown>
    switch (activeTab) {
      case 'overview':
        request = fetchK8sSummary(clusterId).then((response) => {
          if (isCurrentRequest()) setSummary(response.summary)
        })
        break
      case 'namespaces':
        request = fetchK8sNamespaces(clusterId).then((response) => {
          if (isCurrentRequest()) setNamespaces(response.namespaces || [])
        })
        break
      case 'nodes':
        request = Promise.all([
          fetchK8sNodes(clusterId),
          fetchK8sPods(clusterId),
        ]).then(([nodesResponse, podsResponse]) => {
          if (!isCurrentRequest()) return
          setNodes(nodesResponse.nodes || [])
          setPods(podsResponse.pods || [])
        })
        break
      case 'pods':
        request = fetchK8sPods(clusterId, effectiveNamespace || undefined).then((response) => {
          if (isCurrentRequest()) setPods(response.pods || [])
        })
        break
      case 'deployments':
        request = Promise.all([
          fetchK8sDeployments(clusterId, effectiveNamespace || undefined),
          fetchK8sPods(clusterId, effectiveNamespace || undefined),
        ]).then(([deploymentsResponse, podsResponse]) => {
          if (!isCurrentRequest()) return
          setDeployments(deploymentsResponse.deployments || [])
          setPods(podsResponse.pods || [])
        })
        break
      case 'statefulsets':
        request = Promise.all([
          fetchK8sStatefulSets(clusterId, effectiveNamespace || undefined),
          fetchK8sPods(clusterId, effectiveNamespace || undefined),
        ]).then(([statefulSetsResponse, podsResponse]) => {
          if (!isCurrentRequest()) return
          setStatefulSets(statefulSetsResponse.statefulsets || [])
          setPods(podsResponse.pods || [])
        })
        break
      case 'daemonsets':
        request = Promise.all([
          fetchK8sDaemonSets(clusterId, effectiveNamespace || undefined),
          fetchK8sPods(clusterId, effectiveNamespace || undefined),
        ]).then(([daemonSetsResponse, podsResponse]) => {
          if (!isCurrentRequest()) return
          setDaemonSets(daemonSetsResponse.daemonsets || [])
          setPods(podsResponse.pods || [])
        })
        break
      case 'services':
        request = fetchK8sServices(clusterId, effectiveNamespace || undefined).then((response) => {
          if (isCurrentRequest()) setServices(response.services || [])
        })
        break
      case 'ingresses':
        request = fetchK8sIngresses(clusterId, effectiveNamespace || undefined).then((response) => {
          if (isCurrentRequest()) setIngresses(response.ingresses || [])
        })
        break
    }

    request
      .catch((err: Error) => fail(tabLabel(activeTab), err))
      .finally(() => {
        if (isCurrentRequest()) setResourcesLoading(false)
      })
  }, [activeTab, cluster, clusterId])

  const loadNamespaceOptions = useCallback(() => {
    const agentOnline = cluster?.node_status ? cluster.node_status === 'online' : true
    if (!cluster || cluster.status !== 'online' || !agentOnline) return

    const requestID = namespaceRequestSeq.current + 1
    namespaceRequestSeq.current = requestID
    const isCurrentRequest = () => namespaceRequestSeq.current === requestID

    setNamespacesLoading(true)
    fetchK8sNamespaces(clusterId)
      .then((response) => {
        if (isCurrentRequest()) setNamespaces(response.namespaces || [])
      })
      .catch((err: Error) => {
        if (!isCurrentRequest()) return
        setToast({ message: `命名空间加载失败: ${err.message}`, type: 'error' })
      })
      .finally(() => {
        if (isCurrentRequest()) setNamespacesLoading(false)
      })
  }, [cluster, clusterId])

  useEffect(() => {
    loadCluster()
  }, [loadCluster])

  useEffect(() => {
    const agentOnline = cluster?.node_status ? cluster.node_status === 'online' : true
    if (cluster?.status === 'online' && agentOnline) {
      loadActiveResource()
    }
  }, [cluster?.status, cluster?.node_status, loadActiveResource])

  useEffect(() => {
    const agentOnline = cluster?.node_status ? cluster.node_status === 'online' : true
    if (!cluster || cluster.status !== 'online' || !agentOnline) return
    if (!NAMESPACE_SCOPED_TABS.has(activeTab) && !createResourceOpen) return
    if (namespaces.length > 0 || namespacesLoading) return

    loadNamespaceOptions()
  }, [activeTab, cluster, createResourceOpen, loadNamespaceOptions, namespaces.length, namespacesLoading])

  useEffect(() => {
    if (previousNamespaceRef.current === namespace) return
    previousNamespaceRef.current = namespace
    if (!cluster || !NAMESPACE_SCOPED_TABS.has(activeTab)) return
    const agentOnline = cluster.node_status ? cluster.node_status === 'online' : true
    if (cluster.status !== 'online' || !agentOnline) return

    loadActiveResource(namespace)
  }, [activeTab, cluster, loadActiveResource, namespace])

  const normalizedResourceSearch = resourceSearch.trim().toLowerCase()
  const safePods = Array.isArray(pods) ? pods : []
  const safeDeployments = Array.isArray(deployments) ? deployments : []
  const safeStatefulSets = Array.isArray(statefulsets) ? statefulsets : []
  const safeDaemonSets = Array.isArray(daemonsets) ? daemonsets : []
  const safeServices = Array.isArray(services) ? services : []
  const safeIngresses = Array.isArray(ingresses) ? ingresses : []
  const filteredPods = safePods.filter((pod) => matchesSearch([pod.name, pod.status, pod.ready, pod.node, pod.ip, pod.restarts], normalizedResourceSearch))
  const filteredDeployments = safeDeployments.filter((deployment) => matchesSearch([deployment.name, deployment.ready, deployment.up_to_date, deployment.available], normalizedResourceSearch))
  const filteredStatefulSets = safeStatefulSets.filter((statefulset) => matchesSearch([statefulset.name, statefulset.ready, statefulset.service_name], normalizedResourceSearch))
  const filteredDaemonSets = safeDaemonSets.filter((daemonset) => matchesSearch([daemonset.name, daemonset.desired, daemonset.current, daemonset.ready, daemonset.available], normalizedResourceSearch))
  const filteredServices = safeServices.filter((service) => matchesSearch([service.name, service.type, service.cluster_ip, service.external_ip, service.ports], normalizedResourceSearch))
  const filteredIngresses = safeIngresses.filter((ingress) => matchesSearch([ingress.name, ingress.class, ingress.hosts, ingress.address, ingress.ports], normalizedResourceSearch))
  const noMatches = (resourceName: string, total: number) => normalizedResourceSearch && total > 0 ? `没有匹配的 ${resourceName}` : `暂无 ${resourceName}`

  const handleSelectTab = (tab: DetailTab) => {
    if (tab !== activeTab) setResourceSearch('')
    setActiveTab(tab)
  }

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="soft-empty-state px-10 py-8 text-center">
          <div className="mb-3 inline-block h-8 w-8 animate-spin rounded-full border-4 border-primary/30 border-t-primary" />
          <p className="text-sm font-semibold text-muted-foreground">加载集群信息...</p>
        </div>
      </div>
    )
  }

  if (error || !cluster) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="soft-empty-state px-10 py-8 text-center">
          <p className="text-sm font-semibold text-danger">{error || '集群不存在'}</p>
          <button
            type="button"
            onClick={onBack}
            className="soft-button mt-4 bg-primary px-4 py-2 text-sm font-bold text-primary-foreground hover:bg-primary/90"
          >
            返回列表
          </button>
        </div>
      </div>
    )
  }

  const agentOnline = cluster.node_status ? cluster.node_status === 'online' : true
  const isOnline = cluster.status === 'online' && agentOnline
  const agentLastSeenAt = cluster.node_last_seen_at || cluster.last_seen_at
  const resourceCounts: Partial<Record<DetailTab, number | undefined>> = {
    overview: undefined,
    namespaces: summary?.namespace_count ?? namespaces.length,
    nodes: summary?.node_count ?? nodes.length,
    pods: summary?.pod_count ?? pods.length,
    deployments: summary?.deployment_count ?? deployments.length,
    statefulsets: summary?.statefulset_count ?? statefulsets.length,
    daemonsets: summary?.daemonset_count ?? daemonsets.length,
    services: summary?.service_count ?? services.length,
    ingresses: summary?.ingress_count ?? ingresses.length,
  }

  const relatedPodsFor = (kind: K8sResourceKind, namespace: string, name: string) => {
    if (kind === 'pod') return []
    return safePods.filter((pod) => pod.namespace === namespace && pod.workload_kind === kind && pod.workload_name === name)
  }

  const openDiagnostics = (kind: K8sResourceKind, namespace: string, name: string) => {
    setDiagnosticsDrawer({ open: true, kind, namespace, name, relatedPods: relatedPodsFor(kind, namespace, name) })
  }

  return (
    <div className="h-full overflow-auto p-4 sm:p-6">
      {toast && (
        <Toast
          message={toast.message}
          type={toast.type}
          onClose={() => setToast(null)}
        />
      )}

      <K8sPodLogsModal
        clusterId={clusterId}
        namespace={logsModal.namespace}
        podName={logsModal.podName}
        open={logsModal.open}
        onClose={() => setLogsModal({ open: false, namespace: '', podName: '' })}
      />

      <K8sDiagnosticsDrawer
        clusterId={clusterId}
        open={Boolean(diagnosticsDrawer?.open)}
        resource={diagnosticsDrawer ? { kind: diagnosticsDrawer.kind, namespace: diagnosticsDrawer.namespace, name: diagnosticsDrawer.name } : undefined}
        relatedPods={diagnosticsDrawer?.relatedPods || []}
        onClose={() => setDiagnosticsDrawer(null)}
        onToast={(message, type) => setToast({ message, type })}
        onOpenLogs={(namespace, name) => {
          setLogsModal({ open: true, namespace, podName: name })
        }}
        onSwitchResource={(kind, namespace, name) => {
          setDiagnosticsDrawer({ open: true, kind, namespace, name, relatedPods: relatedPodsFor(kind, namespace, name) })
        }}
        onResourceChanged={() => loadActiveResource()}
      />

      <K8sCreateResourceModal
        open={createResourceOpen}
        clusterId={clusterId}
        currentNamespace={namespace}
        namespaces={namespaces}
        onClose={() => setCreateResourceOpen(false)}
        onToast={(message, type) => setToast({ message, type })}
        onCreated={() => {
          loadNamespaceOptions()
          loadActiveResource()
        }}
      />

      <div className="mx-auto max-w-[1380px] space-y-5">
        <header className="soft-panel p-5">
          <button
            type="button"
            onClick={onBack}
            className="soft-button mb-4 inline-flex items-center gap-2 rounded-xl px-2 py-1.5 text-sm font-bold text-muted-foreground transition hover:bg-muted/70 hover:text-foreground"
          >
            <ArrowLeft size={16} aria-hidden="true" />
            返回 Kubernetes 集群
          </button>

          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-3">
                <span className={`h-3 w-3 shrink-0 rounded-full ${isOnline ? 'bg-success shadow-[0_0_14px_rgb(var(--success)/0.35)]' : 'bg-muted-foreground/40'}`} />
                <h1 className="min-w-0 truncate text-2xl font-black text-foreground">{cluster.name}</h1>
                <span className={`rounded-full border px-3 py-1 text-xs font-black ${isOnline ? 'border-success/20 bg-success/10 text-success' : 'border-border bg-muted text-muted-foreground'}`}>
                  {isOnline ? '在线' : '离线'}
                </span>
              </div>
              <p className="mt-2 text-sm font-semibold text-muted-foreground">
                Agent: {cluster.node_name || '未知 Agent'} · {cluster.node_ip || '未知 IP'}
                {cluster.context ? ` · Context: ${cluster.context}` : ''}
                {agentLastSeenAt ? ` · Agent Last Seen: ${formatDateTime(agentLastSeenAt)}` : ''}
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={() => setCreateResourceOpen(true)}
                disabled={!isOnline}
                className="soft-button inline-flex items-center gap-2 bg-primary px-4 py-2 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Plus size={15} aria-hidden="true" />
                创建资源
              </button>
              <button
                type="button"
                onClick={() => loadActiveResource()}
                disabled={resourcesLoading || !isOnline}
                className="soft-button inline-flex items-center gap-2 border border-primary/20 bg-primary/10 px-4 py-2 text-sm font-black text-primary hover:bg-primary/15 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <RefreshCw size={15} className={resourcesLoading ? 'animate-spin' : ''} aria-hidden="true" />
                {resourcesLoading ? '加载中...' : '刷新资源'}
              </button>
            </div>
          </div>
        </header>

        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-6">
          <K8sSummaryCard label="K8s 版本" value={summary?.version || cluster.version || '-'} accent />
          <K8sSummaryCard label="节点" value={String(summary?.node_count ?? cluster.node_count ?? '-')} />
          <K8sSummaryCard label="命名空间" value={String(summary?.namespace_count ?? cluster.namespace_count ?? '-')} />
          <K8sSummaryCard label="Pods" value={String(summary?.pod_count ?? pods.length)} />
          <K8sSummaryCard label="Deployments" value={String(summary?.deployment_count ?? deployments.length)} />
          <K8sSummaryCard label="Services" value={String(summary?.service_count ?? services.length)} />
        </div>

        <div className="grid gap-4 lg:grid-cols-[220px_minmax(0,1fr)]">
          <K8sDetailSidebar activeTab={activeTab} counts={resourceCounts} onSelect={handleSelectTab} />
          <main className="min-w-0 space-y-4">
            {activeTab !== 'overview' && resourceError && isOnline && (
              <ResourceError message={resourceError} onRetry={loadActiveResource} compact />
            )}

            {activeTab === 'overview' && (
              <div className="space-y-4">
                {!isOnline ? (
                  <K8sOfflineState message="集群或 Agent 节点离线，无法查看集群资源信息" />
                ) : resourcesLoading ? (
                  <div className="soft-empty-state p-8 text-center">
                    <div className="mb-3 inline-block h-6 w-6 animate-spin rounded-full border-4 border-primary/20 border-t-primary" />
                    <p className="text-sm font-bold text-muted-foreground">加载集群信息...</p>
                  </div>
                ) : resourceError ? (
                  <ResourceError message={resourceError} onRetry={loadActiveResource} />
                ) : (
                  <>
                    <K8sDetailPanel title="资源统计">
                      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
                        <K8sSummaryCard label="Pods" value={String(summary?.pod_count ?? pods.length)} />
                        <K8sSummaryCard label="Deployments" value={String(summary?.deployment_count ?? deployments.length)} />
                        <K8sSummaryCard label="StatefulSets" value={String(summary?.statefulset_count ?? statefulsets.length)} />
                        <K8sSummaryCard label="DaemonSets" value={String(summary?.daemonset_count ?? daemonsets.length)} />
                        <K8sSummaryCard label="Services" value={String(summary?.service_count ?? services.length)} />
                        <K8sSummaryCard label="Ingresses" value={String(summary?.ingress_count ?? ingresses.length)} />
                      </div>
                    </K8sDetailPanel>

                    <K8sDetailPanel title="连接信息">
                      <div className="divide-y divide-border rounded-2xl border border-border/80 bg-surface/70 px-4">
                        {cluster.kubeconfig_path && <InfoRow label="kubeconfig 路径" value={cluster.kubeconfig_path} />}
                        {cluster.context && <InfoRow label="Context" value={cluster.context} />}
                        <InfoRow label="Agent 节点" value={`${cluster.node_name || '未知 Agent'} (${cluster.node_ip || '未知 IP'})`} />
                        {cluster.node_last_seen_at && <InfoRow label="Agent 最近上报" value={formatDateTime(cluster.node_last_seen_at)} />}
                        <InfoRow label="创建时间" value={formatDateTime(cluster.created_at)} />
                        {cluster.last_seen_at && <InfoRow label="集群连接时间" value={formatDateTime(cluster.last_seen_at)} />}
                      </div>
                    </K8sDetailPanel>
                  </>
                )}
              </div>
            )}

            {activeTab === 'namespaces' && (
              <K8sDetailPanel title="命名空间">
                {!isOnline ? <K8sOfflineState message="集群或 Agent 节点离线，无法查看 Namespace 列表" /> : <K8sNamespaceTable items={namespaces} loading={resourcesLoading} />}
              </K8sDetailPanel>
            )}

            {activeTab === 'nodes' && (
              <K8sDetailPanel title="节点">
                {!isOnline ? <K8sOfflineState message="集群或 Agent 节点离线，无法查看 Node 列表" /> : <K8sNodeTable items={nodes} pods={safePods} loading={resourcesLoading} />}
              </K8sDetailPanel>
            )}

            {activeTab === 'pods' && (
              <K8sDetailPanel title="Pods">
                <ResourceFilters
                  namespace={namespace}
                  namespaces={namespaces}
                  namespacesLoading={namespacesLoading}
                  setNamespace={setNamespace}
                  resourceSearch={resourceSearch}
                  setResourceSearch={setResourceSearch}
                  searchPlaceholder="搜索 Pod / 状态 / 节点 / IP"
                />

                {!isOnline ? (
                  <K8sOfflineState message="集群或 Agent 节点离线，无法查看 Pod 列表" />
                ) : filteredPods.length === 0 && !resourcesLoading ? (
                  <K8sOfflineState message={noMatches('Pod', safePods.length)} />
                ) : (
                  <K8sPodTable
                    clusterId={clusterId}
                    items={filteredPods}
                    loading={resourcesLoading}
                    onViewLogs={(ns, name) => setLogsModal({ open: true, namespace: ns, podName: name })}
                    onViewDiagnostics={openDiagnostics}
                    onToast={(message, type) => setToast({ message, type })}
                    onResourceChanged={() => loadActiveResource()}
                  />
                )}
              </K8sDetailPanel>
            )}

            {activeTab === 'deployments' && (
              <K8sDetailPanel title="Deployments">
                <ResourceFilters
                  namespace={namespace}
                  namespaces={namespaces}
                  namespacesLoading={namespacesLoading}
                  setNamespace={setNamespace}
                  resourceSearch={resourceSearch}
                  setResourceSearch={setResourceSearch}
                  searchPlaceholder="搜索 Deployment / Ready / Available"
                />
                {!isOnline ? (
                  <K8sOfflineState message="集群或 Agent 节点离线，无法查看 Deployment 列表" />
                ) : filteredDeployments.length === 0 && !resourcesLoading ? (
                  <K8sOfflineState message={noMatches('Deployment', safeDeployments.length)} />
                ) : (
                  <K8sWorkloadTable
                    clusterId={clusterId}
                    mode="deployment"
                    items={filteredDeployments}
                    pods={safePods}
                    loading={resourcesLoading}
                    onViewDiagnostics={openDiagnostics}
                    onToast={(message, type) => setToast({ message, type })}
                    onResourceChanged={() => loadActiveResource()}
                  />
                )}
              </K8sDetailPanel>
            )}

            {activeTab === 'statefulsets' && (
              <K8sDetailPanel title="StatefulSets">
                <ResourceFilters
                  namespace={namespace}
                  namespaces={namespaces}
                  namespacesLoading={namespacesLoading}
                  setNamespace={setNamespace}
                  resourceSearch={resourceSearch}
                  setResourceSearch={setResourceSearch}
                  searchPlaceholder="搜索 StatefulSet / Service / Ready"
                />
                {!isOnline ? (
                  <K8sOfflineState message="集群或 Agent 节点离线，无法查看 StatefulSet 列表" />
                ) : filteredStatefulSets.length === 0 && !resourcesLoading ? (
                  <K8sOfflineState message={noMatches('StatefulSet', safeStatefulSets.length)} />
                ) : (
                  <K8sWorkloadTable
                    clusterId={clusterId}
                    mode="statefulset"
                    items={filteredStatefulSets}
                    pods={safePods}
                    loading={resourcesLoading}
                    onViewDiagnostics={openDiagnostics}
                    onToast={(message, type) => setToast({ message, type })}
                    onResourceChanged={() => loadActiveResource()}
                  />
                )}
              </K8sDetailPanel>
            )}

            {activeTab === 'daemonsets' && (
              <K8sDetailPanel title="DaemonSets">
                <ResourceFilters
                  namespace={namespace}
                  namespaces={namespaces}
                  namespacesLoading={namespacesLoading}
                  setNamespace={setNamespace}
                  resourceSearch={resourceSearch}
                  setResourceSearch={setResourceSearch}
                  searchPlaceholder="搜索 DaemonSet / Ready / Available"
                />
                {!isOnline ? (
                  <K8sOfflineState message="集群或 Agent 节点离线，无法查看 DaemonSet 列表" />
                ) : filteredDaemonSets.length === 0 && !resourcesLoading ? (
                  <K8sOfflineState message={noMatches('DaemonSet', safeDaemonSets.length)} />
                ) : (
                  <K8sWorkloadTable
                    clusterId={clusterId}
                    mode="daemonset"
                    items={filteredDaemonSets}
                    pods={safePods}
                    loading={resourcesLoading}
                    onViewDiagnostics={openDiagnostics}
                    onToast={(message, type) => setToast({ message, type })}
                    onResourceChanged={() => loadActiveResource()}
                  />
                )}
              </K8sDetailPanel>
            )}

            {activeTab === 'services' && (
              <K8sDetailPanel title="Services">
                <ResourceFilters
                  namespace={namespace}
                  namespaces={namespaces}
                  namespacesLoading={namespacesLoading}
                  setNamespace={setNamespace}
                  resourceSearch={resourceSearch}
                  setResourceSearch={setResourceSearch}
                  searchPlaceholder="搜索 Service / 类型 / IP / 端口"
                />
                {!isOnline ? (
                  <K8sOfflineState message="集群或 Agent 节点离线，无法查看 Service 列表" />
                ) : filteredServices.length === 0 && !resourcesLoading ? (
                  <K8sOfflineState message={noMatches('Service', safeServices.length)} />
                ) : (
                  <K8sServiceTable items={filteredServices} loading={resourcesLoading} />
                )}
              </K8sDetailPanel>
            )}

            {activeTab === 'ingresses' && (
              <K8sDetailPanel title="Ingresses">
                <ResourceFilters
                  namespace={namespace}
                  namespaces={namespaces}
                  namespacesLoading={namespacesLoading}
                  setNamespace={setNamespace}
                  resourceSearch={resourceSearch}
                  setResourceSearch={setResourceSearch}
                  searchPlaceholder="搜索 Ingress / Host / 地址 / 端口"
                />
                {!isOnline ? (
                  <K8sOfflineState message="集群或 Agent 节点离线，无法查看 Ingress 列表" />
                ) : filteredIngresses.length === 0 && !resourcesLoading ? (
                  <K8sOfflineState message={noMatches('Ingress', safeIngresses.length)} />
                ) : (
                  <K8sIngressTable items={filteredIngresses} loading={resourcesLoading} />
                )}
              </K8sDetailPanel>
            )}
          </main>
        </div>
      </div>
    </div>
  )
}

function ResourceError({ message, onRetry, compact = false }: { message: string; onRetry: () => void; compact?: boolean }) {
  return (
    <div className={`soft-empty-state border-danger/30 bg-danger/5 text-center ${compact ? 'p-4' : 'p-8'}`}>
      <p className="text-sm font-bold text-danger">{message}</p>
      <button
        type="button"
        onClick={() => onRetry()}
        className="soft-button mt-3 bg-primary px-4 py-2 text-sm font-bold text-primary-foreground hover:bg-primary/90"
      >
        重试
      </button>
    </div>
  )
}

function K8sSummaryCard({ label, value, accent = false }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="soft-stat-card p-4">
      <div className="absolute inset-x-0 bottom-0 h-0.5 bg-gradient-to-r from-primary/40 to-accent/20" />
      <p className="text-xs font-black text-muted-foreground">{label}</p>
      <p className={`mt-2 text-2xl font-black tracking-tight ${accent ? 'text-primary' : 'text-foreground'}`}>{value}</p>
    </div>
  )
}

function K8sOfflineState({ message }: { message: string }) {
  return (
    <div className="soft-empty-state p-8 text-center">
      <p className="text-sm font-bold text-muted-foreground">{message}</p>
    </div>
  )
}

function K8sDetailPanel({ title, actions, children }: { title: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="soft-panel">
      <div className="soft-panel-header flex flex-wrap items-center justify-between gap-3 px-4 py-3">
        <h2 className="text-base font-black text-foreground">{title}</h2>
        {actions}
      </div>
      <div className="p-4">{children}</div>
    </section>
  )
}

function K8sFilterBar({ children }: { children: ReactNode }) {
  return <div className="soft-toolbar mb-4 flex flex-wrap items-center gap-3 p-2.5">{children}</div>
}

function ResourceFilters({
  namespace,
  namespaces,
  namespacesLoading,
  setNamespace,
  resourceSearch,
  setResourceSearch,
  searchPlaceholder,
}: {
  namespace: string
  namespaces: K8sNamespace[]
  namespacesLoading: boolean
  setNamespace: (namespace: string) => void
  resourceSearch: string
  setResourceSearch: (search: string) => void
  searchPlaceholder: string
}) {
  return (
    <K8sFilterBar>
      <NamespaceSelect
        namespace={namespace}
        namespaces={namespaces}
        loading={namespacesLoading}
        onChange={setNamespace}
      />
      <ResourceSearchInput value={resourceSearch} onChange={setResourceSearch} placeholder={searchPlaceholder} />
    </K8sFilterBar>
  )
}

function ResourceSearchInput({ value, onChange, placeholder }: { value: string; onChange: (value: string) => void; placeholder: string }) {
  return (
    <div className="relative min-w-[260px] flex-1">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="soft-input h-11 w-full pl-9 pr-4 text-sm font-semibold placeholder:text-muted-foreground"
      />
    </div>
  )
}

function NamespaceSelect({
  namespace,
  namespaces,
  loading,
  onChange,
}: {
  namespace: string
  namespaces: K8sNamespace[]
  loading: boolean
  onChange: (namespace: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [menuStyle, setMenuStyle] = useState<CSSProperties>()
  const buttonRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const selectedNamespace = namespace || '全部命名空间'
  const normalizedSearch = search.trim().toLowerCase()
  const filteredNamespaces = namespaces.filter((item) => {
    if (!normalizedSearch) return true
    return item.name.toLowerCase().includes(normalizedSearch) || item.status.toLowerCase().includes(normalizedSearch)
  })

  const updateMenuPosition = useCallback(() => {
    const button = buttonRef.current
    if (!button) return
    const rect = button.getBoundingClientRect()
    const margin = 12
    const width = Math.min(Math.max(rect.width, 280), window.innerWidth - margin * 2)
    const left = Math.min(Math.max(margin, rect.left), window.innerWidth - width - margin)
    const top = Math.min(rect.bottom + 8, window.innerHeight - 320)
    setMenuStyle({
      left,
      top: Math.max(margin, top),
      width,
    })
  }, [])

  useEffect(() => {
    if (!open) return
    updateMenuPosition()

    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (buttonRef.current?.contains(target) || menuRef.current?.contains(target)) return
      setOpen(false)
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setOpen(false)
      setSearch('')
    }
    const handleViewportChange = () => updateMenuPosition()

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    window.addEventListener('resize', handleViewportChange)
    window.addEventListener('scroll', handleViewportChange, true)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('resize', handleViewportChange)
      window.removeEventListener('scroll', handleViewportChange, true)
    }
  }, [open, updateMenuPosition])

  const chooseNamespace = (nextNamespace: string) => {
    onChange(nextNamespace)
    setOpen(false)
    setSearch('')
  }

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => {
          setOpen((current) => !current)
          window.requestAnimationFrame(updateMenuPosition)
        }}
        className="soft-input flex h-11 min-w-[220px] items-center justify-between gap-3 px-3 text-left text-sm font-semibold hover:bg-muted/70"
      >
        <span className="min-w-0">
          <span className="block text-[10px] font-black leading-none text-muted-foreground">命名空间</span>
          <span className="mt-0.5 block max-w-[190px] truncate">{selectedNamespace}</span>
        </span>
        <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition ${open ? 'rotate-180' : ''}`} aria-hidden="true" />
      </button>

      {open && menuStyle && createPortal(
        <div
          ref={menuRef}
          style={menuStyle}
          className="fixed z-[80] rounded-2xl border border-border/80 bg-card/95 p-2 shadow-[0_22px_70px_rgb(15_23_42/0.18)] backdrop-blur"
        >
          <div className="relative mb-2">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <input
              autoFocus
              type="text"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="搜索命名空间"
              className="soft-input h-10 w-full pl-9 pr-3 text-sm font-semibold placeholder:text-muted-foreground"
            />
          </div>

          <div role="listbox" className="max-h-64 space-y-1 overflow-y-auto pr-1">
            <NamespaceOption
              name="全部命名空间"
              selected={!namespace}
              onClick={() => chooseNamespace('')}
            />
            {loading ? (
              <div className="rounded-xl px-3 py-2.5 text-xs font-bold text-muted-foreground">加载命名空间...</div>
            ) : filteredNamespaces.length === 0 ? (
              <div className="rounded-xl px-3 py-2.5 text-xs font-bold text-muted-foreground">没有匹配的命名空间</div>
            ) : (
              filteredNamespaces.map((item) => (
                <NamespaceOption
                  key={item.name}
                  name={item.name}
                  status={item.status}
                  selected={namespace === item.name}
                  onClick={() => chooseNamespace(item.name)}
                />
              ))
            )}
          </div>
        </div>,
        document.body
      )}
    </>
  )
}

function NamespaceOption({ name, status, selected, onClick }: { name: string; status?: string; selected: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={selected}
      aria-label={status ? `${name} ${status}` : name}
      onClick={onClick}
      className={`flex w-full items-center justify-between gap-3 rounded-xl px-3 py-2.5 text-left text-xs font-bold transition ${selected ? 'bg-primary/10 text-primary' : 'text-foreground hover:bg-muted'}`}
    >
      <span className="min-w-0">
        <span className="block truncate">{name}</span>
        {status && <span className="mt-0.5 block text-[10px] font-black text-muted-foreground">{status}</span>}
      </span>
      {selected && <Check className="h-4 w-4 shrink-0" aria-hidden="true" />}
    </button>
  )
}

function K8sDetailSidebar({ activeTab, counts, onSelect }: { activeTab: DetailTab; counts: Partial<Record<DetailTab, number | undefined>>; onSelect: (tab: DetailTab) => void }) {
  return (
    <aside className="soft-panel p-2 lg:sticky lg:top-6">
      <nav className="grid gap-1 sm:grid-cols-2 lg:grid-cols-1">
        {DETAIL_TABS.map((item) => {
          const active = activeTab === item.key
          const count = counts[item.key]
          return (
            <button
              key={item.key}
              type="button"
              onClick={() => onSelect(item.key)}
              className={`soft-button flex items-center justify-between gap-3 px-3 py-2.5 text-left text-sm font-black ${active ? 'border border-primary/20 bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
            >
              <span className="truncate">{item.label}</span>
              <span className="shrink-0 text-xs opacity-75">{count === undefined ? item.shortLabel || '' : count}</span>
            </button>
          )
        })}
      </nav>
    </aside>
  )
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 py-3">
      <span className="text-sm font-bold text-muted-foreground">{label}</span>
      <span className="break-all text-right text-sm font-semibold text-foreground">{value}</span>
    </div>
  )
}
