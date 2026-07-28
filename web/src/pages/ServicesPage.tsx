import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Activity, ArrowLeft, Boxes, ChevronRight, Clock3, Edit3, ExternalLink, LoaderCircle, Plus, RefreshCw, Search, Server, Trash2, TriangleAlert, X } from 'lucide-react'

import { createApplicationService, deleteApplicationService, getApplicationService, getApplicationServices, updateApplicationService } from '../api/client'
import { ServiceEditorModal } from '../components/services/ServiceEditorModal'
import { ServiceHealthBadge, healthLabel } from '../components/services/ServiceHealthBadge'
import { Toast } from '../components/Toast'
import type { ApplicationServiceDetail, ApplicationServiceHealth, ApplicationServiceInput, ApplicationServiceResourceProjection, ApplicationServiceResourceType, ApplicationServiceSummary } from '../types'

type ServicesPageProps = {
  serviceID?: string
  onOpenService: (id: string) => void
  onBack: () => void
  onNavigate: (path: string) => void
}

type HealthFilter = 'all' | ApplicationServiceHealth

const healthOrder: ApplicationServiceHealth[] = ['unhealthy', 'degraded', 'unknown', 'healthy']
const resourceLabels: Record<ApplicationServiceResourceType, string> = {
  node: '节点',
  compose_project: 'Compose',
  systemd_service: 'Systemd',
  k8s_workload: 'K8s 工作负载',
  uptime_monitor: '服务拨测',
  alert_rule: '告警规则',
  scheduled_task: '计划任务'
}

export function ServicesPage({ serviceID, onOpenService, onBack, onNavigate }: ServicesPageProps) {
  const [services, setServices] = useState<ApplicationServiceSummary[]>([])
  const [detail, setDetail] = useState<ApplicationServiceDetail>()
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [healthFilter, setHealthFilter] = useState<HealthFilter>('all')
  const [editorOpen, setEditorOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<ApplicationServiceSummary>()
  const [deleting, setDeleting] = useState(false)
  const [toast, setToast] = useState<{ message: string, type: 'success' | 'error' } | null>(null)
  const requestSequence = useRef(0)
  const activeController = useRef<AbortController | null>(null)

  const load = useCallback(async (showLoading = false) => {
    const requestID = ++requestSequence.current
    activeController.current?.abort()
    const controller = new AbortController()
    activeController.current = controller
    if (showLoading) {
      setLoading(true)
      if (serviceID) setDetail(undefined)
    }
    else setRefreshing(true)
    setError('')
    try {
      if (serviceID) {
        const response = await getApplicationService(serviceID, controller.signal)
        if (requestID !== requestSequence.current) return
        setDetail(response)
      } else {
        const response = await getApplicationServices(controller.signal)
        if (requestID !== requestSequence.current) return
        setServices(response || [])
        setDetail(undefined)
      }
    } catch (requestError: unknown) {
      if (requestID !== requestSequence.current || isAbortError(requestError)) return
      setError(requestError instanceof Error ? requestError.message : '应用服务加载失败')
    } finally {
      if (requestID === requestSequence.current) {
        setLoading(false)
        setRefreshing(false)
      }
    }
  }, [serviceID])

  useEffect(() => {
    void load(true)
    return () => {
      requestSequence.current += 1
      activeController.current?.abort()
    }
  }, [load])

  const save = async (input: ApplicationServiceInput) => {
    setSaving(true)
    try {
      if (detail) {
        const updated = await updateApplicationService(detail.id, input)
        setDetail(updated)
        setToast({ message: '应用服务更新成功', type: 'success' })
      } else {
        const created = await createApplicationService(input)
        setToast({ message: '应用服务创建成功', type: 'success' })
        setEditorOpen(false)
        onOpenService(created.id)
        return
      }
      setEditorOpen(false)
    } catch (saveError: unknown) {
      const text = saveError instanceof Error ? saveError.message : '网络错误'
      setToast({ message: `应用服务${detail ? '更新' : '创建'}失败: ${text}`, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setDeleting(true)
    try {
      await deleteApplicationService(pendingDelete.id)
      setPendingDelete(undefined)
      setToast({ message: '应用服务删除成功', type: 'success' })
      if (serviceID) onBack()
      else await load(false)
    } catch (deleteError: unknown) {
      setToast({ message: `应用服务删除失败: ${deleteError instanceof Error ? deleteError.message : '网络错误'}`, type: 'error' })
    } finally { setDeleting(false) }
  }

  if (loading) return <PageLoading />
  if (error && !detail) return <PageError message={error} onRetry={() => void load(true)} onBack={serviceID ? onBack : undefined} />
  if (detail && serviceID) return (
    <>
      {error && <div role="alert" className="mb-4 rounded-2xl border border-danger/25 bg-danger/10 px-4 py-3 text-sm font-bold text-danger">刷新失败: {error}</div>}
      <ServiceDetailView service={detail} refreshing={refreshing} onBack={onBack} onRefresh={() => void load(false)} onEdit={() => setEditorOpen(true)} onDelete={() => setPendingDelete(detail)} onNavigate={onNavigate} />
      {editorOpen && <ServiceEditorModal service={detail} saving={saving} onClose={() => !saving && setEditorOpen(false)} onSave={save} />}
      {pendingDelete && <DeleteDialog service={pendingDelete} deleting={deleting} onClose={() => !deleting && setPendingDelete(undefined)} onConfirm={confirmDelete} />}
      {toast && <Toast message={toast.message} type={toast.type} onClose={() => setToast(null)} />}
    </>
  )

  const counts = healthOrder.reduce<Record<string, number>>((result, health) => ({ ...result, [health]: services.filter((service) => service.health === health).length }), {})
  const normalizedSearch = search.trim().toLowerCase()
  const filtered = services
    .filter((service) => healthFilter === 'all' || service.health === healthFilter)
    .filter((service) => !normalizedSearch || [service.name, service.description, service.location_summary, ...service.resources.map((resource) => resource.display_name)].join(' ').toLowerCase().includes(normalizedSearch))
    .sort((left, right) => healthOrder.indexOf(left.health) - healthOrder.indexOf(right.health) || left.name.localeCompare(right.name, 'zh-CN'))

  return (
    <>
      <div className="space-y-5">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <h1 className="text-2xl font-black text-foreground">应用服务</h1>
            <p className="mt-1 text-sm font-semibold text-muted-foreground">按业务服务聚合运行资源、健康原因与近期运维活动。</p>
          </div>
          <div className="flex gap-2">
            <button type="button" onClick={() => void load(false)} disabled={refreshing} className="soft-button inline-flex min-h-10 items-center gap-2 border border-border bg-card px-4 text-sm font-black text-foreground disabled:opacity-50"><RefreshCw size={16} className={refreshing ? 'animate-spin' : ''} />刷新</button>
            <button type="button" onClick={() => setEditorOpen(true)} className="soft-button inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground"><Plus size={16} />创建服务</button>
          </div>
        </div>

        <section className="grid grid-cols-2 gap-2 sm:grid-cols-5" aria-label="服务状态统计">
          <StatusFilter label="全部" count={services.length} active={healthFilter === 'all'} onClick={() => setHealthFilter('all')} />
          {healthOrder.map((health) => <StatusFilter key={health} label={healthLabel(health)} health={health} count={counts[health] || 0} active={healthFilter === health} onClick={() => setHealthFilter(health)} />)}
        </section>

        <section className="soft-panel p-4">
          <div className="flex flex-col gap-3 md:flex-row md:items-center">
            <label className="relative min-w-0 flex-1"><span className="sr-only">搜索应用服务</span><Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索服务、描述、资源或部署位置" className="soft-input min-h-10 w-full pl-9 pr-3 text-sm font-semibold text-foreground" /></label>
            <p className="text-xs font-black text-muted-foreground">显示 {filtered.length} / {services.length}</p>
          </div>
        </section>

        {error && <div role="alert" className="rounded-2xl border border-danger/25 bg-danger/10 px-4 py-3 text-sm font-bold text-danger">刷新失败: {error}</div>}
        {services.length === 0 ? <EmptyServices onCreate={() => setEditorOpen(true)} /> : filtered.length === 0 ? <FilteredEmpty onReset={() => { setSearch(''); setHealthFilter('all') }} /> : (
          <section className="overflow-hidden rounded-2xl border border-border bg-card shadow-sm" aria-label="应用服务列表">
            <div className="hidden grid-cols-[minmax(180px,1.4fr)_110px_minmax(150px,1fr)_110px_120px_72px] gap-4 border-b border-border bg-muted/35 px-4 py-2 text-[11px] font-black uppercase tracking-wide text-muted-foreground lg:grid">
              <span>服务</span><span>状态</span><span>部署位置</span><span>资源</span><span>更新时间</span><span className="text-right">操作</span>
            </div>
            <div className="divide-y divide-border">
              {filtered.map((service) => <ServiceRow key={service.id} service={service} onOpen={() => onOpenService(service.id)} onDelete={() => setPendingDelete(service)} />)}
            </div>
          </section>
        )}
      </div>
      {editorOpen && <ServiceEditorModal saving={saving} onClose={() => !saving && setEditorOpen(false)} onSave={save} />}
      {pendingDelete && <DeleteDialog service={pendingDelete} deleting={deleting} onClose={() => !deleting && setPendingDelete(undefined)} onConfirm={confirmDelete} />}
      {toast && <Toast message={toast.message} type={toast.type} onClose={() => setToast(null)} />}
    </>
  )
}

function ServiceRow({ service, onOpen, onDelete }: { service: ApplicationServiceSummary, onOpen: () => void, onDelete: () => void }) {
  return <div className="grid gap-3 px-4 py-3 transition hover:bg-muted/20 lg:grid-cols-[minmax(180px,1.4fr)_110px_minmax(150px,1fr)_110px_120px_72px] lg:items-center lg:gap-4">
    <button type="button" onClick={onOpen} className="min-w-0 text-left"><span className="block truncate text-sm font-black text-foreground">{service.name}</span><span className="mt-0.5 block truncate text-xs font-semibold text-muted-foreground">{service.first_reason || service.description || '暂无描述'}</span></button>
    <div><ServiceHealthBadge health={service.health} compact /></div>
    <p className="truncate text-xs font-bold text-muted-foreground"><Server size={13} className="mr-1 inline" />{service.location_summary}</p>
    <p className="text-xs font-black text-foreground">{service.resource_count} 个 <span className="font-semibold text-muted-foreground">· {Object.keys(service.resource_type_counts || {}).length} 类</span></p>
    <p className="text-xs font-semibold text-muted-foreground">{formatDate(service.updated_at)}</p>
    <div className="flex justify-end gap-1"><button type="button" aria-label={`删除 ${service.name}`} onClick={onDelete} className="soft-button flex h-8 w-8 items-center justify-center border border-border bg-card text-muted-foreground hover:text-danger"><Trash2 size={14} /></button><button type="button" aria-label={`查看 ${service.name}`} onClick={onOpen} className="soft-button flex h-8 w-8 items-center justify-center bg-primary/10 text-primary"><ChevronRight size={15} /></button></div>
  </div>
}

function ServiceDetailView({ service, refreshing, onBack, onRefresh, onEdit, onDelete, onNavigate }: { service: ApplicationServiceDetail, refreshing: boolean, onBack: () => void, onRefresh: () => void, onEdit: () => void, onDelete: () => void, onNavigate: (path: string) => void }) {
  const groups = useMemo(() => {
    const result = new Map<ApplicationServiceResourceType, ApplicationServiceResourceProjection[]>()
    for (const resource of service.resources || []) result.set(resource.resource_type, [...(result.get(resource.resource_type) || []), resource])
    return result
  }, [service.resources])
  return <div className="space-y-5">
    <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
      <div className="min-w-0">
        <button type="button" onClick={onBack} className="mb-3 inline-flex items-center gap-1 text-xs font-black text-muted-foreground hover:text-foreground"><ArrowLeft size={14} />返回应用服务</button>
        <div className="flex flex-wrap items-center gap-3"><h1 className="truncate text-2xl font-black text-foreground">{service.name}</h1><ServiceHealthBadge health={service.health} /></div>
        <p className="mt-2 max-w-3xl text-sm font-semibold text-muted-foreground">{service.description || '暂无服务描述。'}</p>
      </div>
      <div className="flex shrink-0 gap-2"><button type="button" onClick={onRefresh} disabled={refreshing} className="soft-button inline-flex min-h-10 items-center gap-2 border border-border bg-card px-3 text-sm font-black disabled:opacity-50"><RefreshCw size={15} className={refreshing ? 'animate-spin' : ''} />刷新</button><button type="button" onClick={onEdit} className="soft-button inline-flex min-h-10 items-center gap-2 border border-border bg-card px-3 text-sm font-black"><Edit3 size={15} />编辑</button><button type="button" onClick={onDelete} className="soft-button inline-flex min-h-10 items-center gap-2 border border-danger/25 bg-danger/10 px-3 text-sm font-black text-danger"><Trash2 size={15} />删除</button></div>
    </div>

    <section className="grid gap-3 sm:grid-cols-3">
      <DetailStat label="整体状态" value={healthLabel(service.health)} detail={service.first_reason || '所有可评估资源运行正常'} icon={<Activity size={18} />} />
      <DetailStat label="部署位置" value={service.location_summary} detail={`${service.resource_count} 个关联资源`} icon={<Server size={18} />} />
      <DetailStat label="状态原因" value={`${service.reasons?.length || 0} 项`} detail={`异常 ${service.reason_counts?.unhealthy || 0} · 降级 ${service.reason_counts?.degraded || 0} · 未知 ${service.reason_counts?.unknown || 0}`} icon={<TriangleAlert size={18} />} />
    </section>

    <div className="grid min-w-0 gap-5 xl:grid-cols-[minmax(0,1.35fr)_minmax(340px,0.65fr)]">
      <section className="min-w-0 space-y-3">
        <div className="flex items-center gap-2"><Boxes size={18} className="text-primary" /><h2 className="text-base font-black text-foreground">关联资源</h2></div>
        {service.resources.length === 0 ? <div className="soft-empty-state p-8 text-center text-sm font-semibold text-muted-foreground">尚未关联资源，编辑服务即可添加。</div> : Array.from(groups.entries()).map(([type, resources]) => (
          <div key={type} className="overflow-hidden rounded-2xl border border-border bg-card">
            <div className="flex items-center justify-between border-b border-border bg-muted/30 px-4 py-2"><h3 className="text-xs font-black text-foreground">{resourceLabels[type]}</h3><span className="text-[11px] font-black text-muted-foreground">{resources.length}</span></div>
            <div className="divide-y divide-border">{resources.map((resource) => <ResourceRow key={resource.id} resource={resource} onNavigate={onNavigate} />)}</div>
          </div>
        ))}
      </section>

      <aside className="min-w-0 space-y-4">
        <Panel title="健康原因" icon={<TriangleAlert size={16} />}>
          {service.reasons.length === 0 ? <p className="py-4 text-center text-xs font-semibold text-muted-foreground">暂无异常或降级原因</p> : <div className="space-y-2">{service.reasons.map((reason) => <div key={`${reason.resource_id}:${reason.message}`} className="rounded-xl border border-border bg-muted/25 p-3"><div className="flex items-center gap-2"><ServiceHealthBadge health={reason.status} compact /><span className="truncate text-xs font-black text-foreground">{reason.resource_name}</span></div><p className="mt-1.5 text-xs font-semibold leading-5 text-muted-foreground">{reason.message}</p></div>)}</div>}
        </Panel>
        <Panel title="近期告警" icon={<TriangleAlert size={16} />}><ActivityList empty="暂无关联告警活动" items={service.recent_alerts.map((item) => ({ id: item.id, title: item.rule_name, detail: `${item.node_name || item.node_id} · ${item.metric_field} ${item.metric_value}`, time: item.triggered_at, tone: item.resolved_at ? 'muted' : 'danger' }))} /></Panel>
        <Panel title="近期任务" icon={<Clock3 size={16} />}><ActivityList empty="暂无关联任务执行" items={service.recent_tasks.map((item) => ({ id: item.id, title: item.task_name || item.script_name, detail: `${item.trigger} · ${item.status}`, time: item.created_at, tone: item.status === 'failed' || item.status === 'partial' ? 'danger' : 'muted' }))} /></Panel>
        <Panel title="相关操作" icon={<Activity size={16} />}><ActivityList empty="暂无服务或相关节点操作" items={service.recent_audit.map((item) => ({ id: item.id, title: `${item.module} · ${item.action}`, detail: item.summary || item.target_name || item.target_id, time: item.created_at, tone: item.result === 'failure' ? 'danger' : 'muted' }))} /></Panel>
      </aside>
    </div>
  </div>
}

function ResourceRow({ resource, onNavigate }: { resource: ApplicationServiceResourceProjection, onNavigate: (path: string) => void }) {
  const path = resourcePath(resource)
  return <div className="flex min-w-0 items-center gap-3 px-4 py-3"><ServiceHealthBadge health={resource.health} compact /><div className="min-w-0 flex-1"><p className="truncate text-sm font-black text-foreground">{resource.display_name}</p><p className="truncate text-xs font-semibold text-muted-foreground">{resource.reason || resourceIdentity(resource)}</p></div><span className={`hidden rounded-full px-2 py-1 text-[10px] font-black sm:inline ${resource.state === 'missing' ? 'bg-danger/10 text-danger' : resource.state === 'unavailable' ? 'bg-warning/10 text-warning' : 'bg-muted text-muted-foreground'}`}>{resource.state === 'missing' ? '关联已失效' : resource.state === 'unavailable' ? '暂不可用' : '已关联'}</span>{path && <button type="button" onClick={() => onNavigate(path)} aria-label={`打开 ${resource.display_name} 的原管理入口`} className="soft-button flex h-8 w-8 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-primary"><ExternalLink size={14} /></button>}</div>
}

function resourcePath(resource: ApplicationServiceResourceProjection) {
  switch (resource.resource_type) {
    case 'node': return `/nodes/${encodeURIComponent(resource.resource_key)}`
    case 'compose_project': return `/nodes/${encodeURIComponent(resource.scope_id)}?section=containers&docker=compose`
    case 'systemd_service': return `/nodes/${encodeURIComponent(resource.scope_id)}?section=services&q=${encodeURIComponent(resource.resource_key)}`
    case 'k8s_workload': return `/k8s/clusters/${encodeURIComponent(resource.scope_id)}?tab=${encodeURIComponent(k8sTabForKind(resource.resource_kind))}&namespace=${encodeURIComponent(resource.namespace)}&q=${encodeURIComponent(resource.resource_key)}`
    case 'uptime_monitor': return '/uptime'
    case 'alert_rule': return '/alerts'
    case 'scheduled_task': return '/tasks'
  }
}

function k8sTabForKind(kind: string) {
  if (kind === 'deployment') return 'deployments'
  if (kind === 'statefulset') return 'statefulsets'
  if (kind === 'daemonset') return 'daemonsets'
  return 'overview'
}

function resourceIdentity(resource: ApplicationServiceResourceProjection) {
  if (resource.resource_type === 'k8s_workload') return `${resource.namespace} · ${resource.resource_kind}`
  if (resource.resource_type === 'compose_project' || resource.resource_type === 'systemd_service') return String(resource.meta?.node_name || resource.scope_id)
  return resourceLabels[resource.resource_type]
}

function StatusFilter({ label, count, health, active, onClick }: { label: string, count: number, health?: ApplicationServiceHealth, active: boolean, onClick: () => void }) {
  return <button type="button" aria-pressed={active} onClick={onClick} className={`soft-button min-h-16 border px-3 text-left transition ${active ? 'border-primary/40 bg-primary/10 ring-2 ring-primary/10' : 'border-border bg-card hover:border-primary/25'}`}><span className="block text-xl font-black text-foreground">{count}</span><span className="flex items-center gap-1 text-[11px] font-black text-muted-foreground">{health && <span className={`h-1.5 w-1.5 rounded-full ${health === 'healthy' ? 'bg-success' : health === 'degraded' ? 'bg-warning' : health === 'unhealthy' ? 'bg-danger' : 'bg-muted-foreground'}`} />}{label}</span></button>
}

function DetailStat({ label, value, detail, icon }: { label: string, value: string, detail: string, icon: React.ReactNode }) {
  return <div className="soft-panel flex min-w-0 items-start gap-3 p-4"><span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">{icon}</span><div className="min-w-0"><p className="text-[11px] font-black uppercase tracking-wide text-muted-foreground">{label}</p><p className="mt-1 truncate text-sm font-black text-foreground">{value}</p><p className="mt-0.5 truncate text-xs font-semibold text-muted-foreground">{detail}</p></div></div>
}

function Panel({ title, icon, children }: { title: string, icon: React.ReactNode, children: React.ReactNode }) {
  return <section className="soft-panel min-w-0 p-4"><div className="mb-3 flex items-center gap-2 text-foreground">{icon}<h2 className="text-sm font-black">{title}</h2></div>{children}</section>
}

function ActivityList({ items, empty }: { items: Array<{ id: number, title: string, detail: string, time: string, tone: 'danger' | 'muted' }>, empty: string }) {
  if (items.length === 0) return <p className="py-3 text-center text-xs font-semibold text-muted-foreground">{empty}</p>
  return <div className="space-y-1">{items.map((item) => <div key={item.id} className="flex min-w-0 gap-2 rounded-xl px-2 py-2 hover:bg-muted/30"><span className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${item.tone === 'danger' ? 'bg-danger' : 'bg-muted-foreground'}`} /><div className="min-w-0 flex-1"><p className="truncate text-xs font-black text-foreground">{item.title}</p><p className="truncate text-[11px] font-semibold text-muted-foreground">{item.detail}</p></div><time className="shrink-0 text-[10px] font-semibold text-muted-foreground">{formatShortDate(item.time)}</time></div>)}</div>
}

function DeleteDialog({ service, deleting, onClose, onConfirm }: { service: ApplicationServiceSummary, deleting: boolean, onClose: () => void, onConfirm: () => Promise<void> }) {
  const dialogRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !deleting) onClose()
      if (event.key !== 'Tab' || !dialogRef.current) return
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>('button:not([disabled])'))
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
      if (previous?.isConnected) previous.focus()
    }
  }, [deleting, onClose])
  return <div className="fixed inset-0 z-[130] flex items-center justify-center bg-black/55 p-4" onMouseDown={(event) => { if (event.target === event.currentTarget && !deleting) onClose() }}><div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="delete-service-title" className="soft-panel w-full max-w-md p-5 shadow-2xl"><div className="flex items-start justify-between gap-3"><div><h2 id="delete-service-title" className="text-lg font-black text-foreground">删除应用服务？</h2><p className="mt-2 text-sm font-semibold leading-6 text-muted-foreground">将删除逻辑服务“{service.name}”及其关联记录，但不会删除任何节点、Compose、Systemd、Kubernetes、拨测、告警或计划任务。</p></div><button type="button" aria-label="关闭删除确认" onClick={onClose} disabled={deleting} className="text-muted-foreground hover:text-foreground"><X size={18} /></button></div><div className="mt-5 flex justify-end gap-2"><button type="button" onClick={onClose} disabled={deleting} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black disabled:opacity-50">取消</button><button type="button" autoFocus onClick={() => void onConfirm()} disabled={deleting} className="soft-button inline-flex min-h-10 items-center gap-2 bg-danger px-4 text-sm font-black text-white disabled:opacity-60">{deleting && <LoaderCircle size={15} className="animate-spin" />}确认删除</button></div></div></div>
}

function DetailStatSkeleton() { return <div className="h-24 animate-pulse rounded-2xl bg-muted" /> }
function PageLoading() { return <div className="space-y-5"><div className="h-10 w-52 animate-pulse rounded-xl bg-muted" /><div className="grid gap-3 sm:grid-cols-3"><DetailStatSkeleton /><DetailStatSkeleton /><DetailStatSkeleton /></div><div className="h-80 animate-pulse rounded-2xl bg-muted" /></div> }
function PageError({ message, onRetry, onBack }: { message: string, onRetry: () => void, onBack?: () => void }) { return <div className="soft-empty-state p-10 text-center"><TriangleAlert size={28} className="mx-auto text-danger" /><h2 className="mt-3 text-lg font-black text-foreground">应用服务加载失败</h2><p role="alert" className="mt-1 text-sm font-semibold text-muted-foreground">{message}</p><div className="mt-4 flex justify-center gap-2">{onBack && <button type="button" onClick={onBack} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black">返回列表</button>}<button type="button" onClick={onRetry} className="soft-button min-h-10 bg-primary px-4 text-sm font-black text-primary-foreground">重试</button></div></div> }
function EmptyServices({ onCreate }: { onCreate: () => void }) { return <div className="soft-empty-state p-12 text-center"><Boxes size={32} className="mx-auto text-primary" /><h2 className="mt-3 text-lg font-black text-foreground">还没有应用服务</h2><p className="mx-auto mt-1 max-w-md text-sm font-semibold text-muted-foreground">创建一个逻辑服务，把分散的运行资源和运维信号聚合到同一入口。</p><button type="button" onClick={onCreate} className="soft-button mt-5 inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground"><Plus size={16} />创建第一个服务</button></div> }
function FilteredEmpty({ onReset }: { onReset: () => void }) { return <div className="soft-empty-state p-10 text-center"><Search size={28} className="mx-auto text-muted-foreground" /><h2 className="mt-3 text-base font-black text-foreground">没有匹配的服务</h2><button type="button" onClick={onReset} className="soft-button mt-4 min-h-9 border border-border bg-card px-3 text-xs font-black">清除筛选</button></div> }

function formatDate(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }
function formatShortDate(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' }) }
function isAbortError(error: unknown) { return error instanceof Error && error.name === 'AbortError' }
