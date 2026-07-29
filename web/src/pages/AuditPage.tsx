import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { AlertTriangle, CalendarClock, RefreshCw, Search, ShieldCheck, Trash2, X } from 'lucide-react'

import { cleanupAuditEvents, getAuditEvents } from '../api/client'
import { Toast } from '../components/Toast'
import type { AuditCleanupRequest, AuditEvent, AuditEventsQuery, AuditResult, Node } from '../types'

type AuditPageProps = {
  nodes: Node[]
}

type TimeRange = '24h' | '7d' | '30d' | '90d'

const pageLimit = 50

const timeRanges: Array<{ value: TimeRange, label: string, hours: number }> = [
  { value: '24h', label: '最近 24 小时', hours: 24 },
  { value: '7d', label: '最近 7 天', hours: 24 * 7 },
  { value: '30d', label: '最近 30 天', hours: 24 * 30 },
  { value: '90d', label: '最近 90 天', hours: 24 * 90 }
]

const moduleOptions = [
  ['auth', '认证'],
  ['settings', '系统设置'],
  ['organization', '节点组织'],
  ['node', '节点'],
  ['file', '文件'],
  ['agent', 'Agent'],
  ['docker', 'Docker'],
  ['compose', 'Compose'],
  ['docker_resource', 'Docker 资源'],
  ['systemd', 'Systemd'],
  ['kubernetes', 'Kubernetes'],
  ['alert', '告警'],
  ['uptime', '服务拨测'],
  ['automation', '任务中心'],
  ['service', '应用服务'],
  ['audit', '审计'],
  ['terminal', '终端']
] as const

const moduleCopy = Object.fromEntries(moduleOptions) as Record<string, string>

const resultCopy: Record<AuditResult, { label: string, className: string, dot: string }> = {
  success: { label: '成功', className: 'border-success/30 bg-success/10 text-success', dot: 'bg-success' },
  failure: { label: '失败', className: 'border-danger/30 bg-danger/10 text-danger', dot: 'bg-danger' },
  accepted: { label: '已受理', className: 'border-info/30 bg-info/10 text-info', dot: 'bg-info' }
}

const actorCopy: Record<AuditEvent['actor_type'], string> = {
  admin: '管理员',
  unauthenticated: '未认证请求',
  local_admin: '本地管理员',
  system: '系统'
}

export function AuditPage({ nodes }: AuditPageProps) {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [nextBeforeID, setNextBeforeID] = useState<number | null>(null)
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')
  const [module, setModule] = useState('')
  const [nodeID, setNodeID] = useState('')
  const [result, setResult] = useState<AuditResult | ''>('')
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string>()
  const [selectedEvent, setSelectedEvent] = useState<AuditEvent>()
  const [cleanupOpen, setCleanupOpen] = useState(false)
  const [cleanupLoading, setCleanupLoading] = useState(false)
  const [toast, setToast] = useState<{ message: string, type: 'success' | 'error' } | null>(null)
  const requestSequence = useRef(0)
  const activeController = useRef<AbortController | null>(null)
  const rangeStart = useRef('')
  const detailTriggerRef = useRef<HTMLButtonElement | null>(null)
  const cleanupTriggerRef = useRef<HTMLButtonElement | null>(null)

  const nodeNames = useMemo(() => new Map(nodes.map((node) => [node.id, node.name || node.hostname || node.id])), [nodes])
  const hasActiveFilters = timeRange !== '24h' || module !== '' || nodeID !== '' || result !== '' || query.trim() !== ''

  const createQuery = useCallback((reset: boolean, beforeID?: number): AuditEventsQuery => {
    const selectedRange = timeRanges.find((item) => item.value === timeRange) || timeRanges[0]
    if (reset || !rangeStart.current) {
      rangeStart.current = new Date(Date.now() - selectedRange.hours * 60 * 60 * 1000).toISOString()
    }
    return {
      limit: pageLimit,
      before_id: beforeID,
      from: rangeStart.current,
      module: module || undefined,
      node_id: nodeID || undefined,
      result: result || undefined,
      q: query.trim() || undefined
    }
  }, [module, nodeID, query, result, timeRange])

  const loadEvents = useCallback(async (reset: boolean, beforeID?: number) => {
    const requestID = ++requestSequence.current
    activeController.current?.abort()
    const controller = new AbortController()
    activeController.current = controller
    if (reset) {
      setLoading(true)
      setEvents([])
      setNextBeforeID(null)
    } else {
      setLoadingMore(true)
    }
    setError(undefined)
    try {
      const response = await getAuditEvents(createQuery(reset, beforeID), controller.signal)
      if (requestID !== requestSequence.current) return
      const received = response.events || []
      setEvents((current) => {
        if (reset) return received
        const knownIDs = new Set(current.map((event) => event.id))
        return [...current, ...received.filter((event) => !knownIDs.has(event.id))]
      })
      setNextBeforeID(typeof response.next_before_id === 'number' ? response.next_before_id : null)
    } catch (requestError: unknown) {
      if (requestID !== requestSequence.current || isAbortError(requestError)) return
      setError(requestError instanceof Error ? requestError.message : '网络错误')
    } finally {
      if (requestID === requestSequence.current) {
        setLoading(false)
        setLoadingMore(false)
      }
    }
  }, [createQuery])

  useEffect(() => {
    void loadEvents(true)
    return () => {
      requestSequence.current += 1
      activeController.current?.abort()
    }
  }, [loadEvents])

  const resetFilters = () => {
    setTimeRange('24h')
    setModule('')
    setNodeID('')
    setResult('')
    setQuery('')
  }

  const openDetails = (event: AuditEvent, trigger: HTMLButtonElement) => {
    detailTriggerRef.current = trigger
    setSelectedEvent(event)
  }

  const handleCleanup = async (request: AuditCleanupRequest) => {
    setCleanupLoading(true)
    try {
      const response = await cleanupAuditEvents(request)
      setCleanupOpen(false)
      setToast({
        message: response.deleted_count > 0 ? `审计日志清理成功，共删除 ${response.deleted_count} 条` : '审计日志清理成功，未找到符合条件的记录',
        type: 'success'
      })
      await loadEvents(true)
    } catch (cleanupError: unknown) {
      setToast({ message: `审计日志清理失败: ${cleanupError instanceof Error ? cleanupError.message : '网络错误'}`, type: 'error' })
    } finally {
      setCleanupLoading(false)
    }
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1 className="text-2xl font-black text-foreground">审计日志</h1>
          <p className="mt-1 text-sm font-semibold text-muted-foreground">追溯敏感操作的发起者、目标与结果；日志不会记录命令、文件内容、凭据或原始请求响应。</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button ref={cleanupTriggerRef} type="button" onClick={() => setCleanupOpen(true)} className="soft-button inline-flex min-h-10 cursor-pointer items-center justify-center gap-2 border border-danger/30 bg-danger/5 px-4 text-sm font-black text-danger hover:bg-danger/10 focus:outline-none focus:ring-4 focus:ring-danger/20">
            <Trash2 size={16} aria-hidden="true" />清理日志
          </button>
          <button type="button" onClick={() => void loadEvents(true)} disabled={loading} className="soft-button inline-flex min-h-10 cursor-pointer items-center justify-center gap-2 border border-border bg-card px-4 text-sm font-black text-foreground hover:border-primary/40 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50">
            <RefreshCw size={16} aria-hidden="true" className={loading ? 'animate-spin' : ''} />刷新
          </button>
        </div>
      </div>

      <section className="soft-panel p-4" aria-label="审计日志筛选">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <FilterField label="时间范围">
            <select aria-label="时间范围" value={timeRange} onChange={(event) => setTimeRange(event.target.value as TimeRange)} className="soft-input min-h-10 w-full px-3 text-sm font-bold text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20">
              {timeRanges.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </FilterField>
          <FilterField label="模块">
            <select aria-label="模块" value={module} onChange={(event) => setModule(event.target.value)} className="soft-input min-h-10 w-full px-3 text-sm font-bold text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20">
              <option value="">全部模块</option>
              {moduleOptions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
          </FilterField>
          <FilterField label="节点">
            <select aria-label="节点" value={nodeID} onChange={(event) => setNodeID(event.target.value)} className="soft-input min-h-10 w-full px-3 text-sm font-bold text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20">
              <option value="">全部节点</option>
              {nodes.map((node) => <option key={node.id} value={node.id}>{node.name || node.hostname || node.id}</option>)}
            </select>
          </FilterField>
          <FilterField label="结果">
            <select aria-label="结果" value={result} onChange={(event) => setResult(event.target.value as AuditResult | '')} className="soft-input min-h-10 w-full px-3 text-sm font-bold text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20">
              <option value="">全部结果</option>
              <option value="success">成功</option>
              <option value="failure">失败</option>
              <option value="accepted">已受理</option>
            </select>
          </FilterField>
        </div>
        <div className="mt-3 flex flex-col gap-3 lg:flex-row lg:items-end">
          <FilterField label="关键词" className="min-w-0 flex-1">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
              <input aria-label="关键词" value={query} maxLength={128} onChange={(event) => setQuery(event.target.value)} placeholder="搜索安全的目标、名称或结果摘要" className="soft-input min-h-10 w-full pl-10 pr-3 text-sm font-bold text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-4 focus:ring-primary/20" />
            </div>
          </FilterField>
          <button type="button" onClick={resetFilters} disabled={!hasActiveFilters} className="soft-button min-h-10 cursor-pointer border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50">重置筛选</button>
        </div>
      </section>

      {error ? (
        <div role="alert" className="rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger">审计日志加载失败: {error}</div>
      ) : null}

      {loading ? (
        <section className="soft-empty-state px-6 py-14 text-center" aria-live="polite">
          <RefreshCw className="mx-auto h-8 w-8 animate-spin text-primary" aria-hidden="true" />
          <p className="mt-3 text-sm font-black text-muted-foreground">正在加载审计日志...</p>
        </section>
      ) : !error && events.length === 0 ? (
        <section className="soft-empty-state px-6 py-16 text-center">
          <ShieldCheck className="mx-auto h-10 w-10 text-muted-foreground" aria-hidden="true" />
          <h2 className="mt-4 text-xl font-black text-foreground">{hasActiveFilters ? '没有匹配的审计事件' : '暂无审计记录'}</h2>
          <p className="mx-auto mt-2 max-w-xl text-sm font-semibold leading-6 text-muted-foreground">{hasActiveFilters ? '请调整时间范围、模块、节点、结果或关键词。' : '执行登录、配置变更或资源操作后，安全的操作摘要会显示在这里。'}</p>
        </section>
      ) : events.length > 0 ? (
        <section className="soft-panel overflow-hidden" aria-label="审计事件列表">
          <div className="overflow-x-auto">
            <table className="min-w-[980px] w-full border-collapse text-left">
              <thead className="border-b border-border bg-muted/40 text-xs font-black text-muted-foreground">
                <tr>
                  <th className="px-4 py-3">时间</th>
                  <th className="px-4 py-3">结果</th>
                  <th className="px-4 py-3">操作者</th>
                  <th className="px-4 py-3">模块 / 操作</th>
                  <th className="px-4 py-3">目标 / 节点</th>
                  <th className="px-4 py-3 text-right">耗时</th>
                  <th className="px-4 py-3 text-right">详情</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {events.map((event) => (
                  <tr key={event.id} className="align-top transition hover:bg-muted/25">
                    <td className="whitespace-nowrap px-4 py-3 text-xs font-bold text-muted-foreground">{formatDate(event.created_at)}</td>
                    <td className="px-4 py-3"><ResultBadge result={event.result} /></td>
                    <td className="px-4 py-3 text-sm font-bold text-foreground">{actorLabel(event)}</td>
                    <td className="px-4 py-3">
                      <p className="text-sm font-black text-foreground">{moduleCopy[event.module] || event.module}</p>
                      <p className="mt-0.5 text-xs font-semibold text-muted-foreground">{event.action}</p>
                    </td>
                    <td className="max-w-[330px] px-4 py-3">
                      <p className="truncate text-sm font-bold text-foreground" title={targetLabel(event)}>{targetLabel(event)}</p>
                      <p className="mt-0.5 truncate text-xs font-semibold text-muted-foreground" title={nodeLabel(event.node_id, nodeNames)}>{nodeLabel(event.node_id, nodeNames)}</p>
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right text-xs font-black text-muted-foreground">{formatDuration(event.duration_ms)}</td>
                    <td className="px-4 py-3 text-right">
                      <button type="button" aria-label={`查看审计事件 ${event.id} 详情`} onClick={(clickEvent) => openDetails(event, clickEvent.currentTarget)} className="soft-button min-h-8 cursor-pointer border border-border bg-card px-3 text-xs font-black text-foreground hover:border-primary/40 focus:outline-none focus:ring-4 focus:ring-primary/20">详情</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-4 py-3">
            <p className="text-xs font-black text-muted-foreground">已加载 {events.length} 条事件</p>
            {nextBeforeID !== null ? (
              <button type="button" onClick={() => void loadEvents(false, nextBeforeID)} disabled={loadingMore} className="soft-button min-h-9 cursor-pointer border border-border bg-card px-4 text-xs font-black text-foreground hover:border-primary/40 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50">{loadingMore ? '正在加载...' : '加载更多'}</button>
            ) : <span className="text-xs font-black text-muted-foreground">已到达当前范围末尾</span>}
          </div>
        </section>
      ) : null}

      {selectedEvent ? <AuditDetailDialog event={selectedEvent} nodeName={nodeLabel(selectedEvent.node_id, nodeNames)} returnFocusRef={detailTriggerRef} onClose={() => setSelectedEvent(undefined)} /> : null}
      {cleanupOpen ? <AuditCleanupDialog loading={cleanupLoading} returnFocusRef={cleanupTriggerRef} onClose={() => { if (!cleanupLoading) setCleanupOpen(false) }} onConfirm={(request) => void handleCleanup(request)} /> : null}
      {toast ? <Toast message={toast.message} type={toast.type} onClose={() => setToast(null)} /> : null}
    </div>
  )
}

function FilterField({ label, className = '', children }: { label: string, className?: string, children: React.ReactNode }) {
  return <label className={`block ${className}`}><span className="mb-1.5 block text-xs font-black text-muted-foreground">{label}</span>{children}</label>
}

function ResultBadge({ result }: { result: AuditResult }) {
  const copy = resultCopy[result]
  return <span className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-1 text-xs font-black ${copy.className}`}><span className={`h-2 w-2 rounded-full ${copy.dot}`} />{copy.label}</span>
}

function AuditDetailDialog({ event, nodeName, returnFocusRef, onClose }: { event: AuditEvent, nodeName: string, returnFocusRef: React.RefObject<HTMLButtonElement | null>, onClose: () => void }) {
  const dialogRef = useRef<HTMLElement | null>(null)
  const closeButtonRef = useRef<HTMLButtonElement | null>(null)
  const metadata = Object.entries(event.metadata || {}).sort(([left], [right]) => left.localeCompare(right))

  useEffect(() => {
    closeButtonRef.current?.focus()
    return () => {
      if (returnFocusRef.current?.isConnected) returnFocusRef.current.focus()
    }
  }, [returnFocusRef])

  const handleKeyDown = (keyboardEvent: ReactKeyboardEvent<HTMLElement>) => {
    if (keyboardEvent.key === 'Escape') {
      keyboardEvent.preventDefault()
      onClose()
      return
    }
    if (keyboardEvent.key === 'Tab') {
      keyboardEvent.preventDefault()
      closeButtonRef.current?.focus()
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4" onMouseDown={(mouseEvent) => { if (mouseEvent.target === mouseEvent.currentTarget) onClose() }}>
      <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="audit-detail-title" tabIndex={-1} onKeyDown={handleKeyDown} className="soft-panel max-h-[88vh] w-full max-w-3xl overflow-y-auto p-5 shadow-2xl sm:p-6">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-xs font-black uppercase tracking-[0.16em] text-primary">Audit Event #{event.id}</p>
            <h2 id="audit-detail-title" className="mt-1 text-xl font-black text-foreground">审计事件详情</h2>
          </div>
          <button ref={closeButtonRef} type="button" aria-label="关闭审计事件详情" onClick={onClose} className="soft-button flex h-9 w-9 cursor-pointer items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20"><X size={18} aria-hidden="true" /></button>
        </div>

        <div className="mt-5 grid gap-3 sm:grid-cols-2">
          <DetailItem label="时间" value={formatDate(event.created_at)} />
          <DetailItem label="结果" value={resultCopy[event.result].label} />
          <DetailItem label="操作者" value={actorLabel(event)} />
          <DetailItem label="来源 IP" value={event.source_ip || '—'} />
          <DetailItem label="模块 / 操作" value={`${moduleCopy[event.module] || event.module} / ${event.action}`} />
          <DetailItem label="耗时" value={formatDuration(event.duration_ms)} />
          <DetailItem label="目标类型" value={event.target_type || '—'} />
          <DetailItem label="目标" value={targetLabel(event)} />
          <DetailItem label="节点" value={nodeName} />
          <DetailItem label="结果摘要" value={event.summary || '—'} />
          <DetailItem label="请求 ID" value={event.request_id} wide />
        </div>

        <section className="mt-4 rounded-2xl border border-border bg-muted/25 p-4" aria-label="安全元数据">
          <h3 className="text-sm font-black text-foreground">安全元数据</h3>
          {metadata.length > 0 ? (
            <dl className="mt-3 grid gap-3 sm:grid-cols-2">
              {metadata.map(([key, value]) => <div key={key} className="min-w-0"><dt className="text-[11px] font-black text-muted-foreground">{key}</dt><dd className="mt-1 break-all text-sm font-bold text-foreground">{value || '—'}</dd></div>)}
            </dl>
          ) : <p className="mt-2 text-sm font-semibold text-muted-foreground">此事件没有附加元数据。</p>}
        </section>
      </section>
    </div>
  )
}

function AuditCleanupDialog({ loading, returnFocusRef, onClose, onConfirm }: { loading: boolean, returnFocusRef: React.RefObject<HTMLButtonElement | null>, onClose: () => void, onConfirm: (request: AuditCleanupRequest) => void }) {
  const dialogRef = useRef<HTMLElement | null>(null)
  const closeButtonRef = useRef<HTMLButtonElement | null>(null)
  const [mode, setMode] = useState<'days' | 'before'>('days')
  const [days, setDays] = useState('90')
  const [before, setBefore] = useState(() => toLocalDateTimeInput(new Date(Date.now() - 30 * 24 * 60 * 60 * 1000)))
  const parsedDays = Number(days)
  const daysValid = /^\d+$/.test(days) && parsedDays >= 1 && parsedDays <= 3650
  const beforeTime = new Date(before).getTime()
  const beforeValid = before !== '' && !Number.isNaN(beforeTime) && beforeTime <= Date.now() - 24 * 60 * 60 * 1000
  const valid = mode === 'days' ? daysValid : beforeValid
  const cutoffCopy = mode === 'days'
    ? (daysValid ? `${parsedDays} 天以前` : '所选天数以前')
    : (beforeValid ? `${formatDate(new Date(beforeTime).toISOString())} 以前` : '所选时间以前')

  useEffect(() => {
    closeButtonRef.current?.focus()
    return () => {
      if (returnFocusRef.current?.isConnected) returnFocusRef.current.focus()
    }
  }, [returnFocusRef])

  const handleKeyDown = (keyboardEvent: ReactKeyboardEvent<HTMLElement>) => {
    if (keyboardEvent.key === 'Escape' && !loading) {
      keyboardEvent.preventDefault()
      onClose()
      return
    }
    if (keyboardEvent.key !== 'Tab') return
    const focusable = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]), select:not([disabled]), input:not([disabled])') || [])
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (keyboardEvent.shiftKey && document.activeElement === first) {
      keyboardEvent.preventDefault()
      last.focus()
    } else if (!keyboardEvent.shiftKey && document.activeElement === last) {
      keyboardEvent.preventDefault()
      first.focus()
    }
  }

  const submit = () => {
    if (!valid || loading) return
    if (mode === 'days') {
      onConfirm({ older_than_days: parsedDays })
      return
    }
    onConfirm({ before: new Date(beforeTime).toISOString() })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4" onMouseDown={(mouseEvent) => { if (!loading && mouseEvent.target === mouseEvent.currentTarget) onClose() }}>
      <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="audit-cleanup-title" aria-describedby="audit-cleanup-description" tabIndex={-1} onKeyDown={handleKeyDown} className="soft-panel w-full max-w-xl overflow-hidden shadow-2xl">
        <div className="flex items-start justify-between gap-4 border-b border-border p-5 sm:p-6">
          <div className="flex min-w-0 gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl bg-danger/10 text-danger"><Trash2 size={19} aria-hidden="true" /></div>
            <div>
              <h2 id="audit-cleanup-title" className="text-xl font-black text-foreground">清理审计日志</h2>
              <p id="audit-cleanup-description" className="mt-1 text-sm font-semibold leading-6 text-muted-foreground">按保留天数或指定截止时间批量删除旧记录。最近 24 小时始终受到保护。</p>
            </div>
          </div>
          <button ref={closeButtonRef} type="button" aria-label="关闭清理审计日志" onClick={onClose} disabled={loading} className="soft-button flex h-9 w-9 shrink-0 cursor-pointer items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"><X size={18} aria-hidden="true" /></button>
        </div>

        <div className="space-y-4 p-5 sm:p-6">
          <FilterField label="清理方式">
            <select aria-label="清理方式" value={mode} onChange={(event) => setMode(event.target.value as 'days' | 'before')} disabled={loading} className="soft-input min-h-11 w-full px-3 text-sm font-bold text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:opacity-60">
              <option value="days">保留最近若干天</option>
              <option value="before">删除指定时间之前</option>
            </select>
          </FilterField>

          {mode === 'days' ? (
            <FilterField label="保留最近天数">
              <div className="relative">
                <CalendarClock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
                <input aria-label="保留最近天数" type="number" min={1} max={3650} step={1} value={days} onChange={(event) => setDays(event.target.value)} disabled={loading} className="soft-input min-h-11 w-full pl-10 pr-16 text-sm font-bold text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:opacity-60" />
                <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs font-black text-muted-foreground">天</span>
              </div>
              {!daysValid ? <span className="mt-1.5 block text-xs font-bold text-danger">请输入 1–3650 之间的整数。</span> : null}
            </FilterField>
          ) : (
            <FilterField label="截止时间">
              <input aria-label="截止时间" type="datetime-local" value={before} max={toLocalDateTimeInput(new Date(Date.now() - 24 * 60 * 60 * 1000))} onChange={(event) => setBefore(event.target.value)} disabled={loading} className="soft-input min-h-11 w-full px-3 text-sm font-bold text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:opacity-60" />
              {!beforeValid ? <span className="mt-1.5 block text-xs font-bold text-danger">截止时间必须早于当前时间至少 24 小时。</span> : null}
            </FilterField>
          )}

          <div className="rounded-2xl border border-danger/25 bg-danger/5 p-4">
            <div className="flex gap-3">
              <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-danger" aria-hidden="true" />
              <div>
                <p className="text-sm font-black text-foreground">将永久删除 {cutoffCopy}的审计日志</p>
                <p className="mt-1 text-xs font-semibold leading-5 text-muted-foreground">删除后无法恢复；本次清理操作本身会保留为新的审计事件。</p>
              </div>
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-end gap-2 border-t border-border bg-muted/20 px-5 py-4 sm:px-6">
          <button type="button" onClick={onClose} disabled={loading} className="soft-button min-h-10 cursor-pointer border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50">取消</button>
          <button type="button" onClick={submit} disabled={!valid || loading} className="soft-button inline-flex min-h-10 cursor-pointer items-center gap-2 bg-danger px-4 text-sm font-black text-white hover:bg-danger/90 focus:outline-none focus:ring-4 focus:ring-danger/20 disabled:cursor-not-allowed disabled:opacity-50"><Trash2 size={16} aria-hidden="true" />{loading ? '正在清理...' : '确认清理'}</button>
        </div>
      </section>
    </div>
  )
}

function DetailItem({ label, value, wide = false }: { label: string, value: string, wide?: boolean }) {
  return <div className={`min-w-0 rounded-2xl border border-border bg-muted/20 p-3 ${wide ? 'sm:col-span-2' : ''}`}><p className="text-[11px] font-black text-muted-foreground">{label}</p><p className="mt-1 break-all text-sm font-bold text-foreground">{value}</p></div>
}

function actorLabel(event: AuditEvent) {
  const type = actorCopy[event.actor_type] || event.actor_type
  return event.actor_name ? `${type} · ${event.actor_name}` : type
}

function targetLabel(event: AuditEvent) {
  if (event.target_name && event.target_id && event.target_name !== event.target_id) return `${event.target_name} · ${event.target_id}`
  return event.target_name || event.target_id || '—'
}

function nodeLabel(nodeID: string, nodeNames: Map<string, string>) {
  if (!nodeID) return '未关联节点'
  const name = nodeNames.get(nodeID)
  return name && name !== nodeID ? `${name} · ${nodeID}` : nodeID
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function formatDuration(durationMS: number) {
  if (durationMS < 1000) return `${Math.max(0, durationMS)} ms`
  return `${(durationMS / 1000).toFixed(durationMS < 10_000 ? 2 : 1)} s`
}

function toLocalDateTimeInput(date: Date) {
  const offset = date.getTimezoneOffset() * 60 * 1000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError'
}
