import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { LoaderCircle, Plus, RefreshCw, Search, X } from 'lucide-react'
import { checkApplicationService, createApplicationService, deleteApplicationService, getApplicationService, getApplicationServices, updateApplicationService } from '../api/client'
import { ServiceDetailView } from '../components/services/ServiceDetailView'
import { ServiceEditorModal } from '../components/services/ServiceEditorModal'
import { ServiceHealthBadge, healthLabel } from '../components/services/ServiceHealthBadge'
import { serviceTime } from '../components/services/model'
import { Toast } from '../components/Toast'
import type { LeaveGuard } from '../components/UnsavedChangesDialog'
import type { ApplicationServiceDetail, ApplicationServiceHealth, ApplicationServiceHealthCheck, ApplicationServiceInput, ApplicationServiceSummary } from '../types'

type ServicesPageProps = {
  serviceID?: string
  onOpenService: (id: string) => void
  onBack: () => void
  onNavigate: (path: string) => void
  onDiagnose?: (service: ApplicationServiceDetail) => void
  diagnosisBusy?: boolean
  onRegisterLeaveGuard?: (guard: LeaveGuard | undefined) => void
}
const healthOrder: ApplicationServiceHealth[] = ['unhealthy', 'degraded', 'unknown', 'healthy']

export function ServicesPage({ serviceID, onOpenService, onBack, onNavigate, onDiagnose, diagnosisBusy, onRegisterLeaveGuard }: ServicesPageProps) {
  const [services, setServices] = useState<ApplicationServiceSummary[]>([])
  const [listLoaded, setListLoaded] = useState(false)
  const [detail, setDetail] = useState<ApplicationServiceDetail>()
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [updatedAt, setUpdatedAt] = useState<string>()
  const [listUpdatedAt, setListUpdatedAt] = useState<string>()
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [healthFilter, setHealthFilter] = useState<'all' | ApplicationServiceHealth>('all')
  const [editorOpen, setEditorOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<ApplicationServiceSummary>()
  const [deleting, setDeleting] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [checking, setChecking] = useState(false)
  const [healthCheck, setHealthCheck] = useState<ApplicationServiceHealthCheck>()
  const requestSequence = useRef(0)
  const activeController = useRef<AbortController | null>(null)
  const checkController = useRef<AbortController | null>(null)
  const checkSequence = useRef(0)
  const target = useRef(serviceID)
  target.current = serviceID
  const mounted = useRef(false)
  const operation = useRef(0)
  const mutationBusy = useRef(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const listPosition = useRef(0)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; operation.current += 1 } }, [])
  const cancelHealthCheck = useCallback(() => {
    checkSequence.current += 1
    checkController.current?.abort()
    checkController.current = null
    setChecking(false)
    setHealthCheck(undefined)
  }, [])
  useLayoutEffect(() => {
    operation.current += 1
    mutationBusy.current = false
    setEditorOpen(false); setPendingDelete(undefined); setSaving(false); setDeleting(false)
  }, [serviceID])
  const load = useCallback(async (initial = false) => {
    cancelHealthCheck()
    const requestID = ++requestSequence.current
    activeController.current?.abort()
    const controller = new AbortController()
    activeController.current = controller
    if (initial) { setLoading(true); if (serviceID) setDetail(undefined) }
    else setRefreshing(true)
    setError('')
    try {
      if (serviceID) {
        const response = await getApplicationService(serviceID, controller.signal)
        if (controller.signal.aborted || requestID !== requestSequence.current) return
        setDetail(response)
      } else {
        const response = await getApplicationServices(controller.signal)
        if (controller.signal.aborted || requestID !== requestSequence.current) return
        setServices(response || []); setListLoaded(true); setDetail(undefined)
        setListUpdatedAt(new Date().toISOString())
      }
      setUpdatedAt(new Date().toISOString())
    } catch (requestError) {
      if (controller.signal.aborted || requestID !== requestSequence.current) return
      setError(requestError instanceof Error ? requestError.message : '应用服务加载失败')
    } finally {
      if (requestID === requestSequence.current) { setLoading(false); setRefreshing(false) }
    }
  }, [cancelHealthCheck, serviceID])
  useEffect(() => {
    void load(true)
    return () => { requestSequence.current += 1; activeController.current?.abort(); checkSequence.current += 1; checkController.current?.abort(); checkController.current = null }
  }, [load])
  useLayoutEffect(() => { if (!serviceID && !loading && scrollRef.current) scrollRef.current.scrollTop = listPosition.current }, [serviceID, loading])

  const checkHealth = async () => {
    if (!serviceID || checkController.current || editorOpen || mutationBusy.current) return
    const sequence = ++checkSequence.current
    const controller = new AbortController()
    checkController.current = controller
    requestSequence.current += 1; activeController.current?.abort()
    setRefreshing(false); setChecking(true); setError(''); setHealthCheck(undefined)
    try {
      const result = await checkApplicationService(serviceID, controller.signal)
      if (sequence !== checkSequence.current || controller.signal.aborted) return
      setDetail(result.service); setHealthCheck(result); setUpdatedAt(result.checked_at)
      setToast({ type: 'success', message: '服务健康复查成功' })
    } catch (checkError) {
      if (sequence !== checkSequence.current || controller.signal.aborted) return
      setToast({ type: 'error', message: `服务健康复查失败: ${checkError instanceof Error ? checkError.message : '网络错误'}` })
    } finally { if (sequence === checkSequence.current) { setChecking(false); checkController.current = null } }
  }
  const save = async (input: ApplicationServiceInput) => {
    if (mutationBusy.current) return
    const id = serviceID
    const sequence = ++operation.current
    mutationBusy.current = true
    cancelHealthCheck(); requestSequence.current += 1; activeController.current?.abort(); setRefreshing(false); setSaving(true)
    try {
      const result = id ? await updateApplicationService(id, input) : await createApplicationService(input)
      if (!mounted.current || target.current !== id || operation.current !== sequence) return
      onRegisterLeaveGuard?.(undefined)
      setEditorOpen(false); setError(''); setUpdatedAt(new Date().toISOString())
      setToast({ message: `应用服务${id ? '更新' : '创建'}成功`, type: 'success' })
      if (id) setDetail(result)
      else onOpenService(result.id)
    } catch (saveError) {
      if (!mounted.current || target.current !== id || operation.current !== sequence) return
      setToast({ message: `应用服务${id ? '更新' : '创建'}失败: ${saveError instanceof Error ? saveError.message : '网络错误'}`, type: 'error' })
    } finally { if (mounted.current && operation.current === sequence) { mutationBusy.current = false; setSaving(false) } }
  }
  const confirmDelete = async () => {
    if (!pendingDelete || mutationBusy.current) return
    const item = pendingDelete
    const page = serviceID
    const sequence = ++operation.current
    mutationBusy.current = true; setDeleting(true)
    try {
      await deleteApplicationService(item.id)
      if (!mounted.current || target.current !== page || operation.current !== sequence) return
      setPendingDelete(undefined)
      setToast({ message: '应用服务删除成功', type: 'success' })
      if (page) onBack()
      else await load(false)
    } catch (deleteError) {
      if (!mounted.current || operation.current !== sequence) return
      setToast({ message: `应用服务删除失败: ${deleteError instanceof Error ? deleteError.message : '网络错误'}`, type: 'error' })
    } finally { if (mounted.current && operation.current === sequence) { mutationBusy.current = false; setDeleting(false) } }
  }

  const current = detail?.id === serviceID ? detail : undefined
  const initial = loading && (serviceID ? !current : !listLoaded)
  const query = search.trim().toLowerCase()
  const searched = services.filter(service => !query || [service.name, service.description, service.location_summary, ...service.resources.map(resource => resource.display_name)].join(' ').toLowerCase().includes(query))
  const filtered = searched.filter(service => healthFilter === 'all' || service.health === healthFilter).sort((a, b) => a.name.localeCompare(b.name, 'zh-CN', { numeric: true }) || a.id.localeCompare(b.id))
  const openEditor = () => { cancelHealthCheck(); setEditorOpen(true) }
  return <div className="services-page">
    {serviceID && current ? <>
      {error && <p role="alert" className="service-error">刷新失败: {error}。保留上次快照。<button onClick={() => void load(false)}>重试</button></p>}
      <ServiceDetailView key={current.id} service={current} refreshing={refreshing || loading} checking={checking} check={healthCheck} stale={Boolean(error)} updatedAt={updatedAt} onCheck={() => void checkHealth()} onDiagnose={onDiagnose ? () => onDiagnose(current) : undefined} diagnosisBusy={diagnosisBusy} onBack={onBack} onRefresh={() => void load(false)} onEdit={openEditor} onDelete={() => { cancelHealthCheck(); setPendingDelete(current) }} onNavigate={onNavigate} />
    </> : <>
      <div className="console-page-title"><h1>{serviceID ? '应用服务详情' : '应用服务'}</h1><div className="flex gap-2">{serviceID ? <button className="console-button" onClick={onBack}>返回应用服务</button> : <><button className="console-button console-ghost" disabled={loading || refreshing} onClick={() => void load(false)}><RefreshCw size={15} />刷新</button><button className="console-button console-primary" disabled={initial} onClick={openEditor}><Plus size={15} />创建服务</button></>}</div></div>
      {initial ? <div className="service-loading" role="status"><LoaderCircle size={17} className="animate-spin" />正在加载应用服务…</div> : error && (serviceID || !listLoaded) ? <div className="service-initial-error"><h2>应用服务加载失败</h2><p role="alert">{error}</p><button className="console-button" onClick={() => void load(true)}>重试</button></div> : !serviceID && <>
        {error && <p role="alert" className="service-error">刷新失败: {error}。保留上次快照。<button onClick={() => void load(false)}>重试</button></p>}
        <div className="console-status-tabs" aria-label="服务状态统计"><button aria-pressed={healthFilter === 'all'} onClick={() => setHealthFilter('all')}>全部 {searched.length}</button>{healthOrder.map(health => <button key={health} aria-pressed={healthFilter === health} onClick={() => setHealthFilter(health)}>{healthLabel(health)} {searched.filter(service => service.health === health).length}</button>)}</div>
        <div className="service-list-toolbar"><label className="console-search"><Search size={15} /><input aria-label="搜索应用服务" placeholder="搜索服务、描述、资源或部署位置" value={search} onChange={event => { listPosition.current = 0; setSearch(event.target.value) }} /></label><span>{refreshing ? '刷新中…' : `快照更新 ${serviceTime(listUpdatedAt)}`}</span></div>
        {!services.length ? <div className="console-empty"><h2>还没有应用服务</h2><p>关联现有资源，建立服务视图；不会接管或修改底层资源。</p><button className="console-button" onClick={openEditor}><Plus size={15} />创建第一个服务</button></div> : <>
          <div ref={scrollRef} className="service-table-scroll service-list-scroll" onScroll={event => { if (!serviceID && !loading) listPosition.current = event.currentTarget.scrollTop }}><table aria-label="应用服务列表" className="service-table service-list-table"><thead><tr><th>服务</th><th>状态</th><th>部署位置</th><th className="num">资源</th><th>主要问题</th><th className="num">操作</th></tr></thead><tbody>{filtered.map(service => <tr key={service.id}>
            <td><button className="service-name" onClick={() => onOpenService(service.id)}>{service.name}</button>{service.description && <p title={service.description}>{service.description}</p>}</td><td><ServiceHealthBadge health={service.health} compact /></td><td>{service.location_summary || '—'}</td><td className="num">{service.resource_count}</td><td><span className="service-clamp" title={service.first_reason}>{service.first_reason || (service.resource_count ? '—' : '尚未关联资源')}</span></td><td className="num"><button className="service-link" aria-label={`查看 ${service.name}`} onClick={() => onOpenService(service.id)}>查看</button></td>
          </tr>)}</tbody></table></div>
          {!filtered.length && <div className="console-empty"><h2>没有匹配的服务</h2><button className="console-button" onClick={() => { setSearch(''); setHealthFilter('all') }}>清除筛选</button></div>}
          <div className="service-table-footer"><span>显示 {filtered.length} / {services.length} 个服务</span><span>按名称排序</span></div>
        </>}
      </>}
    </>}
    {editorOpen && <ServiceEditorModal key={serviceID || 'new'} service={current} saving={saving} onRegisterLeaveGuard={onRegisterLeaveGuard} onClose={() => !saving && setEditorOpen(false)} onSave={save} />}
    {pendingDelete && <DeleteDialog service={pendingDelete} deleting={deleting} onClose={() => !deleting && setPendingDelete(undefined)} onConfirm={confirmDelete} />}
    {toast && <Toast {...toast} onClose={() => setToast(null)} />}
  </div>
}

function DeleteDialog({ service, deleting, onClose, onConfirm }: { service: ApplicationServiceSummary; deleting: boolean; onClose: () => void; onConfirm: () => Promise<void> }) {
  const ref = useRef<HTMLDivElement>(null)
  const latest = useRef({ deleting, onClose })
  latest.current = { deleting, onClose }
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    ref.current?.querySelector<HTMLButtonElement>('[data-cancel]')?.focus()
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !latest.current.deleting) latest.current.onClose()
      if (event.key !== 'Tab') return
      const buttons = Array.from(ref.current?.querySelectorAll<HTMLButtonElement>('button:not([disabled])') || [])
      if (!buttons.length) { event.preventDefault(); return }
      if (event.shiftKey && document.activeElement === buttons[0]) { event.preventDefault(); buttons[buttons.length - 1].focus() }
      else if (!event.shiftKey && document.activeElement === buttons[buttons.length - 1]) { event.preventDefault(); buttons[0].focus() }
    }
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('keydown', key); if (previous?.isConnected) previous.focus() }
  }, [])
  return <div className="soft-modal-overlay fixed inset-0 z-[130] flex items-center justify-center p-4"><div ref={ref} role="dialog" aria-modal="true" aria-labelledby="delete-service-title" className="soft-modal-shell w-full max-w-md p-5"><div className="flex items-start justify-between gap-3"><h2 id="delete-service-title" className="text-lg font-medium">删除应用服务？</h2><button className="console-icon-button" aria-label="关闭删除确认" onClick={onClose} disabled={deleting}><X size={17} /></button></div><p className="mt-4 text-sm leading-6 text-muted-foreground">将删除逻辑服务“{service.name}”及其关联记录，但不会删除任何节点、Compose、Systemd、Kubernetes、拨测、告警或计划任务。</p><div className="mt-5 flex justify-end gap-2"><button data-cancel className="console-button" onClick={onClose} disabled={deleting}>取消</button><button className="console-button text-danger" onClick={() => void onConfirm()} disabled={deleting}>{deleting && <LoaderCircle size={15} className="animate-spin" />}确认删除</button></div></div></div>
}
