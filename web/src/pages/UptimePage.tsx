import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type KeyboardEvent as ReactKeyboardEvent, type ReactNode, type RefObject, type SetStateAction } from 'react'
import { Activity, Clock3, History, Pencil, Play, Plus, RefreshCw, ShieldCheck, Trash2, X } from 'lucide-react'

import { checkUptimeMonitor, createUptimeMonitor, deleteUptimeMonitor, getUptimeIncidents, getUptimeMonitors, getUptimeResults, toggleUptimeMonitor, updateUptimeMonitor } from '../api/client'
import { NotificationChannelsEditor } from '../components/NotificationChannelsEditor'
import { Toast } from '../components/Toast'
import type { NotificationChannel, UptimeIncident, UptimeMonitor, UptimeMonitorInput, UptimeMonitorStatus, UptimeMonitorType, UptimeResult } from '../types'

type FormMode = 'create' | 'edit'

type MonitorForm = {
  name: string
  type: UptimeMonitorType
  target: string
  intervalSeconds: string
  timeoutSeconds: string
  failureThreshold: string
  expectedStatusMin: string
  expectedStatusMax: string
  tlsExpiryThresholdDays: string
  notificationChannels: NotificationChannel[]
}

const emptyForm: MonitorForm = {
  name: '',
  type: 'http',
  target: '',
  intervalSeconds: '60',
  timeoutSeconds: '5',
  failureThreshold: '3',
  expectedStatusMin: '200',
  expectedStatusMax: '399',
  tlsExpiryThresholdDays: '30',
  notificationChannels: []
}

const statusCopy: Record<UptimeMonitorStatus, { label: string, className: string, dot: string }> = {
  pending: { label: '等待检测', className: 'border-border bg-muted text-muted-foreground', dot: 'bg-muted-foreground' },
  up: { label: '正常', className: 'border-success/30 bg-success/10 text-success', dot: 'bg-success' },
  warning: { label: '证书预警', className: 'border-warning/30 bg-warning/10 text-warning', dot: 'bg-warning' },
  down: { label: '故障', className: 'border-danger/30 bg-danger/10 text-danger', dot: 'bg-danger' }
}

export function UptimePage() {
  const [monitors, setMonitors] = useState<UptimeMonitor[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [formMode, setFormMode] = useState<FormMode>('create')
  const [editingMonitor, setEditingMonitor] = useState<UptimeMonitor>()
  const [form, setForm] = useState<MonitorForm>(emptyForm)
  const [formOpen, setFormOpen] = useState(false)
  const [formError, setFormError] = useState<string>()
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<UptimeMonitor>()
  const [busyAction, setBusyAction] = useState<string>()
  const [historyMonitor, setHistoryMonitor] = useState<UptimeMonitor>()
  const [historyResults, setHistoryResults] = useState<UptimeResult[]>([])
  const [historyIncidents, setHistoryIncidents] = useState<UptimeIncident[]>([])
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyError, setHistoryError] = useState<string>()
  const [toast, setToast] = useState<{ message: string, type: 'success' | 'error' } | null>(null)
  const monitorListRequestSequence = useRef(0)
  const historyRequestSequence = useRef(0)
  const formTriggerRef = useRef<HTMLButtonElement | null>(null)
  const historyTriggerRef = useRef<HTMLButtonElement | null>(null)
  const deleteTriggerRef = useRef<HTMLButtonElement | null>(null)
  const pageFallbackFocusRef = useRef<HTMLButtonElement | null>(null)

  const invalidateMonitorLoads = useCallback(() => {
    monitorListRequestSequence.current += 1
  }, [])

  const loadMonitors = useCallback(async (showLoading = false) => {
    const requestID = ++monitorListRequestSequence.current
    if (showLoading) setLoading(true)
    try {
      const response = await getUptimeMonitors()
      if (requestID !== monitorListRequestSequence.current) return
      setMonitors(response.monitors || [])
      setError(undefined)
    } catch (requestError: unknown) {
      if (requestID !== monitorListRequestSequence.current) return
      setError(requestError instanceof Error ? requestError.message : '服务拨测加载失败')
    } finally {
      if (requestID === monitorListRequestSequence.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadMonitors(true)
    const timer = window.setInterval(() => void loadMonitors(false), 15_000)
    return () => {
      window.clearInterval(timer)
      monitorListRequestSequence.current += 1
      historyRequestSequence.current += 1
    }
  }, [loadMonitors])

  const summary = useMemo(() => ({
    total: monitors.length,
    up: monitors.filter((monitor) => monitor.enabled && monitor.status === 'up').length,
    warning: monitors.filter((monitor) => monitor.enabled && monitor.status === 'warning').length,
    down: monitors.filter((monitor) => monitor.enabled && monitor.status === 'down').length
  }), [monitors])

  const openCreateForm = (trigger: HTMLButtonElement) => {
    formTriggerRef.current = trigger
    setFormMode('create')
    setEditingMonitor(undefined)
    setForm(emptyForm)
    setFormError(undefined)
    setFormOpen(true)
  }

  const openEditForm = (monitor: UptimeMonitor, trigger: HTMLButtonElement) => {
    formTriggerRef.current = trigger
    setFormMode('edit')
    setEditingMonitor(monitor)
    setForm({
      name: monitor.name,
      type: monitor.type,
      target: monitor.target,
      intervalSeconds: String(monitor.interval_seconds),
      timeoutSeconds: String(monitor.timeout_seconds),
      failureThreshold: String(monitor.failure_threshold),
      expectedStatusMin: String(monitor.expected_status_min),
      expectedStatusMax: String(monitor.expected_status_max),
      tlsExpiryThresholdDays: String(monitor.tls_expiry_threshold_days),
      notificationChannels: monitor.notification_channels || []
    })
    setFormError(undefined)
    setFormOpen(true)
  }

  const closeForm = () => {
    if (saving) return
    setFormOpen(false)
    setFormError(undefined)
  }

  const saveMonitor = async () => {
    const numeric = {
      interval: Number.parseInt(form.intervalSeconds, 10),
      timeout: Number.parseInt(form.timeoutSeconds, 10),
      failures: Number.parseInt(form.failureThreshold, 10),
      statusMin: Number.parseInt(form.expectedStatusMin, 10),
      statusMax: Number.parseInt(form.expectedStatusMax, 10),
      tlsDays: Number.parseInt(form.tlsExpiryThresholdDays, 10)
    }
    if (!form.name.trim()) {
      setFormError('拨测名称不能为空')
      return
    }
    if (!form.target.trim()) {
      setFormError('拨测目标不能为空')
      return
    }
    if (Object.values(numeric).some(Number.isNaN)) {
      setFormError('检测参数必须是有效整数')
      return
    }
    const payload: UptimeMonitorInput = {
      name: form.name.trim(),
      type: form.type,
      target: form.target.trim(),
      interval_seconds: numeric.interval,
      timeout_seconds: numeric.timeout,
      failure_threshold: numeric.failures,
      expected_status_min: numeric.statusMin,
      expected_status_max: numeric.statusMax,
      tls_expiry_threshold_days: numeric.tlsDays,
      notification_channels: form.notificationChannels
    }
    invalidateMonitorLoads()
    setSaving(true)
    setFormError(undefined)
    try {
      if (formMode === 'create') {
        await createUptimeMonitor({ ...payload, enabled: true })
      } else if (editingMonitor) {
        await updateUptimeMonitor(editingMonitor.id, payload)
      }
      if (formMode === 'create') formTriggerRef.current = pageFallbackFocusRef.current
      setToast({ message: `服务拨测${formMode === 'create' ? '创建' : '保存'}成功`, type: 'success' })
      setFormOpen(false)
      await loadMonitors(false)
    } catch (requestError: unknown) {
      const message = requestError instanceof Error ? requestError.message : '网络错误'
      const toastMessage = `服务拨测${formMode === 'create' ? '创建' : '保存'}失败: ${message}`
      setFormError(toastMessage)
      setToast({ message: toastMessage, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  const toggleMonitor = async (monitor: UptimeMonitor) => {
    const enabled = !monitor.enabled
    invalidateMonitorLoads()
    setBusyAction(`toggle-${monitor.id}`)
    try {
      const updated = await toggleUptimeMonitor(monitor.id, enabled)
      invalidateMonitorLoads()
      setMonitors((current) => current.map((item) => item.id === monitor.id ? updated : item))
      setToast({ message: `服务拨测${enabled ? '启用' : '停用'}成功`, type: 'success' })
    } catch (requestError: unknown) {
      setToast({ message: `服务拨测${enabled ? '启用' : '停用'}失败: ${errorMessage(requestError)}`, type: 'error' })
    } finally {
      setBusyAction(undefined)
    }
  }

  const checkMonitor = async (monitor: UptimeMonitor) => {
    invalidateMonitorLoads()
    setBusyAction(`check-${monitor.id}`)
    try {
      const updated = await checkUptimeMonitor(monitor.id)
      invalidateMonitorLoads()
      setMonitors((current) => current.map((item) => item.id === monitor.id ? updated : item))
      setToast({ message: '服务拨测检测成功', type: 'success' })
    } catch (requestError: unknown) {
      setToast({ message: `服务拨测检测失败: ${errorMessage(requestError)}`, type: 'error' })
    } finally {
      setBusyAction(undefined)
    }
  }

  const confirmDelete = async () => {
    if (!pendingDelete) return
    invalidateMonitorLoads()
    setBusyAction(`delete-${pendingDelete.id}`)
    try {
      await deleteUptimeMonitor(pendingDelete.id)
      invalidateMonitorLoads()
      deleteTriggerRef.current = pageFallbackFocusRef.current
      setMonitors((current) => current.filter((monitor) => monitor.id !== pendingDelete.id))
      setToast({ message: '服务拨测删除成功', type: 'success' })
      setPendingDelete(undefined)
    } catch (requestError: unknown) {
      setToast({ message: `服务拨测删除失败: ${errorMessage(requestError)}`, type: 'error' })
    } finally {
      setBusyAction(undefined)
    }
  }

  const openHistory = async (monitor: UptimeMonitor, trigger: HTMLButtonElement) => {
    const requestID = ++historyRequestSequence.current
    historyTriggerRef.current = trigger
    setHistoryMonitor(monitor)
    setHistoryResults([])
    setHistoryIncidents([])
    setHistoryError(undefined)
    setHistoryLoading(true)
    try {
      const [results, incidents] = await Promise.all([
        getUptimeResults(monitor.id, 50),
        getUptimeIncidents(monitor.id, 50)
      ])
      if (requestID !== historyRequestSequence.current) return
      setHistoryResults(results.results || [])
      setHistoryIncidents(incidents.incidents || [])
    } catch (requestError: unknown) {
      if (requestID !== historyRequestSequence.current) return
      setHistoryError(errorMessage(requestError))
    } finally {
      if (requestID === historyRequestSequence.current) setHistoryLoading(false)
    }
  }

  const closeHistory = () => {
    historyRequestSequence.current += 1
    setHistoryMonitor(undefined)
    setHistoryLoading(false)
  }

  const openDelete = (monitor: UptimeMonitor, trigger: HTMLButtonElement) => {
    deleteTriggerRef.current = trigger
    setPendingDelete(monitor)
  }

  const closeDelete = () => {
    if (pendingDelete && busyAction === `delete-${pendingDelete.id}`) return
    setPendingDelete(undefined)
  }

  if (loading) {
    return <section className="soft-empty-state p-6 text-sm font-black text-muted-foreground">正在加载服务拨测...</section>
  }

  return (
    <div className="space-y-5">
      {toast ? <Toast message={toast.message} type={toast.type} onClose={() => setToast(null)} /> : null}

      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1 className="text-2xl font-black text-foreground">服务拨测</h1>
          <p className="mt-1 text-sm font-semibold text-muted-foreground">由 Server 定时检查 HTTP、HTTPS 和 TCP 服务，并复用告警渠道发送故障与恢复通知。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" onClick={() => void loadMonitors(false)} className="soft-button inline-flex min-h-10 cursor-pointer items-center gap-2 border border-border bg-card px-4 text-sm font-black text-foreground hover:border-primary/40 focus:outline-none focus:ring-4 focus:ring-primary/20">
            <RefreshCw size={16} aria-hidden="true" />刷新
          </button>
          <button ref={pageFallbackFocusRef} type="button" onClick={(event) => openCreateForm(event.currentTarget)} className="soft-button inline-flex min-h-10 cursor-pointer items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20">
            <Plus size={16} aria-hidden="true" />创建拨测
          </button>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard label="全部服务" value={summary.total} tone="info" />
        <SummaryCard label="运行正常" value={summary.up} tone="success" />
        <SummaryCard label="证书预警" value={summary.warning} tone="warning" />
        <SummaryCard label="当前故障" value={summary.down} tone="danger" />
      </div>

      {error ? (
        <div role="alert" className="rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger">
          服务拨测加载失败: {error}
        </div>
      ) : null}

      {!error && monitors.length === 0 ? (
        <section className="soft-empty-state px-6 py-16 text-center">
          <Activity className="mx-auto h-10 w-10 text-muted-foreground" aria-hidden="true" />
          <h2 className="mt-4 text-xl font-black text-foreground">还没有服务拨测</h2>
          <p className="mx-auto mt-2 max-w-xl text-sm font-semibold leading-6 text-muted-foreground">创建第一个 HTTP 或 TCP 目标。拨测请求从 MizuPanel Server 所在网络发起。</p>
          <button type="button" onClick={(event) => openCreateForm(event.currentTarget)} className="soft-button mt-5 min-h-10 cursor-pointer bg-primary px-4 text-sm font-black text-primary-foreground">创建拨测</button>
        </section>
      ) : monitors.length > 0 ? (
        <section className="grid gap-4 xl:grid-cols-2" aria-label="服务拨测列表">
          {monitors.map((monitor) => (
            <MonitorCard
              key={monitor.id}
              monitor={monitor}
              busyAction={busyAction}
              onToggle={() => void toggleMonitor(monitor)}
              onCheck={() => void checkMonitor(monitor)}
              onEdit={(trigger) => openEditForm(monitor, trigger)}
              onHistory={(trigger) => void openHistory(monitor, trigger)}
              onDelete={(trigger) => openDelete(monitor, trigger)}
            />
          ))}
        </section>
      ) : null}

      {formOpen ? (
        <MonitorFormModal
          mode={formMode}
          form={form}
          setForm={setForm}
          error={formError}
          saving={saving}
          returnFocusRef={formTriggerRef}
          fallbackFocusRef={pageFallbackFocusRef}
          onClose={closeForm}
          onSave={() => void saveMonitor()}
        />
      ) : null}

      {pendingDelete ? (
        <DeleteMonitorModal
          monitor={pendingDelete}
          deleting={busyAction === `delete-${pendingDelete.id}`}
          returnFocusRef={deleteTriggerRef}
          fallbackFocusRef={pageFallbackFocusRef}
          onClose={closeDelete}
          onConfirm={() => void confirmDelete()}
        />
      ) : null}

      {historyMonitor ? (
        <HistoryModal
          monitor={historyMonitor}
          results={historyResults}
          incidents={historyIncidents}
          loading={historyLoading}
          error={historyError}
          returnFocusRef={historyTriggerRef}
          fallbackFocusRef={pageFallbackFocusRef}
          onClose={closeHistory}
        />
      ) : null}
    </div>
  )
}

function SummaryCard({ label, value, tone }: { label: string, value: number, tone: 'info' | 'success' | 'warning' | 'danger' }) {
  const color = tone === 'success' ? 'text-success' : tone === 'warning' ? 'text-warning' : tone === 'danger' ? 'text-danger' : 'text-info'
  return (
    <div className="soft-stat-card p-4">
      <p className="text-xs font-black text-muted-foreground">{label}</p>
      <p className={`mt-2 text-3xl font-black ${color}`}>{value}</p>
    </div>
  )
}

type MonitorCardProps = {
  monitor: UptimeMonitor
  busyAction?: string
  onToggle: () => void
  onCheck: () => void
  onEdit: (trigger: HTMLButtonElement) => void
  onHistory: (trigger: HTMLButtonElement) => void
  onDelete: (trigger: HTMLButtonElement) => void
}

function MonitorCard({ monitor, busyAction, onToggle, onCheck, onEdit, onHistory, onDelete }: MonitorCardProps) {
  const status = statusCopy[monitor.status]
  const disabled = !monitor.enabled
  const remainingDays = monitor.tls_remaining_days
  return (
    <article className={`soft-panel p-5 ${disabled ? 'opacity-75' : ''}`}>
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-lg font-black text-foreground">{monitor.name}</h2>
            <span className="soft-chip px-2 py-1 text-[11px] font-black uppercase text-muted-foreground">{monitor.type}</span>
            <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-black ${disabled ? 'border-border bg-muted text-muted-foreground' : status.className}`}>
              <span className={`h-2 w-2 rounded-full ${disabled ? 'bg-muted-foreground' : status.dot}`} />
              {disabled ? '已停用' : status.label}
            </span>
          </div>
          <p className="mt-2 break-all text-sm font-semibold text-muted-foreground">{monitor.target}</p>
        </div>
        <button type="button" role="switch" aria-checked={monitor.enabled} aria-label={`${monitor.enabled ? '停用' : '启用'} ${monitor.name}`} onClick={onToggle} disabled={busyAction === `toggle-${monitor.id}`} className={`relative h-7 w-12 shrink-0 cursor-pointer rounded-full transition focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50 ${monitor.enabled ? 'bg-success' : 'bg-muted'}`}>
          <span className={`absolute left-1 top-1 h-5 w-5 rounded-full bg-white shadow transition-transform ${monitor.enabled ? 'translate-x-5' : 'translate-x-0'}`} />
        </button>
      </div>

      <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MonitorDatum label="响应" value={monitor.last_checked_at ? `${monitor.last_latency_ms} ms` : '—'} />
        <MonitorDatum label="状态码" value={monitor.last_status_code > 0 ? String(monitor.last_status_code) : '—'} />
        <MonitorDatum label="连续失败" value={`${monitor.consecutive_failures}/${monitor.failure_threshold}`} />
        <MonitorDatum label="证书剩余" value={remainingDays == null ? '—' : `${remainingDays} 天`} tone={remainingDays != null && remainingDays <= monitor.tls_expiry_threshold_days ? 'warning' : undefined} />
      </div>

      {monitor.last_error ? <p className="mt-3 rounded-2xl border border-danger/20 bg-danger/5 px-3 py-2 text-xs font-bold text-danger">{monitor.last_error}</p> : null}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
        <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
          <Clock3 size={14} aria-hidden="true" />
          {monitor.last_checked_at ? `上次检测 ${formatDate(monitor.last_checked_at)}` : '尚未检测'}
        </div>
        <div className="flex flex-wrap gap-2">
          <CardAction label={busyAction === `check-${monitor.id}` ? '检测中...' : '立即检测'} icon={<Play size={14} />} onClick={() => onCheck()} disabled={disabled || busyAction === `check-${monitor.id}`} />
          <CardAction label="历史" icon={<History size={14} />} onClick={onHistory} />
          <CardAction label="编辑" icon={<Pencil size={14} />} onClick={onEdit} />
          <CardAction label="删除" icon={<Trash2 size={14} />} onClick={onDelete} danger />
        </div>
      </div>
    </article>
  )
}

function MonitorDatum({ label, value, tone }: { label: string, value: string, tone?: 'warning' }) {
  return (
    <div className="rounded-2xl bg-surface/80 px-3 py-2">
      <p className="text-[11px] font-black text-muted-foreground">{label}</p>
      <p className={`mt-1 text-sm font-black ${tone === 'warning' ? 'text-warning' : 'text-foreground'}`}>{value}</p>
    </div>
  )
}

function CardAction({ label, icon, onClick, disabled = false, danger = false }: { label: string, icon: ReactNode, onClick: (trigger: HTMLButtonElement) => void, disabled?: boolean, danger?: boolean }) {
  return (
    <button type="button" onClick={(event) => onClick(event.currentTarget)} disabled={disabled} className={`soft-button inline-flex min-h-8 cursor-pointer items-center gap-1.5 border px-3 text-xs font-black focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50 ${danger ? 'border-danger/25 bg-danger/5 text-danger' : 'border-border bg-card text-muted-foreground hover:text-foreground'}`}>
      {icon}{label}
    </button>
  )
}

type MonitorFormModalProps = {
  mode: FormMode
  form: MonitorForm
  setForm: Dispatch<SetStateAction<MonitorForm>>
  error?: string
  saving: boolean
  returnFocusRef: RefObject<HTMLElement | null>
  fallbackFocusRef: RefObject<HTMLElement | null>
  onClose: () => void
  onSave: () => void
}

function MonitorFormModal({ mode, form, setForm, error, saving, returnFocusRef, fallbackFocusRef, onClose, onSave }: MonitorFormModalProps) {
  const nameInputRef = useRef<HTMLInputElement | null>(null)
  const { dialogRef, handleKeyDown } = useDialogFocus(onClose, nameInputRef, returnFocusRef, fallbackFocusRef)
  const update = <Key extends keyof MonitorForm>(key: Key, value: MonitorForm[Key]) => setForm((current) => ({ ...current, [key]: value }))
  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-6">
      <section ref={dialogRef} role="dialog" aria-modal="true" aria-label={mode === 'create' ? '创建服务拨测' : '编辑服务拨测'} tabIndex={-1} onKeyDown={handleKeyDown} className="soft-modal-shell flex max-h-[92vh] w-full max-w-3xl flex-col outline-none">
        <header className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4">
          <div>
            <h2 className="text-lg font-black text-foreground">{mode === 'create' ? '创建服务拨测' : '编辑服务拨测'}</h2>
            <p className="mt-1 text-xs font-semibold text-muted-foreground">检测由 Server 发起，内网和本机可达目标均可使用。</p>
          </div>
          <button type="button" aria-label="关闭服务拨测表单" onClick={onClose} disabled={saving} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={16} /></button>
        </header>
        <div className="space-y-5 overflow-y-auto px-5 py-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField label="名称">
              <input ref={nameInputRef} aria-label="拨测名称" value={form.name} onChange={(event) => update('name', event.target.value)} disabled={saving} maxLength={120} className="soft-input min-h-10 w-full px-3 text-sm font-bold" />
            </FormField>
            <FormField label="类型">
              <select aria-label="拨测类型" value={form.type} onChange={(event) => update('type', event.target.value as UptimeMonitorType)} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold">
                <option value="http">HTTP / HTTPS</option><option value="tcp">TCP Connect</option>
              </select>
            </FormField>
          </div>
          <FormField label={form.type === 'http' ? 'HTTP / HTTPS 地址' : 'TCP 地址'}>
            <input aria-label="拨测目标" value={form.target} onChange={(event) => update('target', event.target.value)} disabled={saving} placeholder={form.type === 'http' ? 'https://example.com/health' : 'example.com:443'} className="soft-input min-h-10 w-full px-3 text-sm font-bold" />
          </FormField>
          <div className="grid gap-4 sm:grid-cols-3">
            <NumberField label="检测间隔（秒）" ariaLabel="检测间隔" value={form.intervalSeconds} onChange={(value) => update('intervalSeconds', value)} min={30} max={86400} disabled={saving} />
            <NumberField label="超时（秒）" ariaLabel="检测超时" value={form.timeoutSeconds} onChange={(value) => update('timeoutSeconds', value)} min={1} max={30} disabled={saving} />
            <NumberField label="连续失败阈值" ariaLabel="连续失败阈值" value={form.failureThreshold} onChange={(value) => update('failureThreshold', value)} min={1} max={10} disabled={saving} />
          </div>
          {form.type === 'http' ? (
            <div className="grid gap-4 sm:grid-cols-3">
              <NumberField label="成功状态码起点" ariaLabel="成功状态码起点" value={form.expectedStatusMin} onChange={(value) => update('expectedStatusMin', value)} min={100} max={599} disabled={saving} />
              <NumberField label="成功状态码终点" ariaLabel="成功状态码终点" value={form.expectedStatusMax} onChange={(value) => update('expectedStatusMax', value)} min={100} max={599} disabled={saving} />
              <NumberField label="证书提前预警（天）" ariaLabel="证书预警天数" value={form.tlsExpiryThresholdDays} onChange={(value) => update('tlsExpiryThresholdDays', value)} min={1} max={365} disabled={saving} />
            </div>
          ) : null}
          <NotificationChannelsEditor value={form.notificationChannels} onChange={(channels) => update('notificationChannels', channels)} disabled={saving} maxChannels={10} />
          {error ? <div role="alert" className="rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger">{error}</div> : null}
        </div>
        <footer className="soft-modal-footer flex justify-end gap-2 border-t px-5 py-4">
          <button type="button" onClick={onClose} disabled={saving} className="soft-button min-h-10 cursor-pointer border border-border bg-card px-4 text-sm font-black text-muted-foreground">取消</button>
          <button type="button" onClick={onSave} disabled={saving} className="soft-button min-h-10 cursor-pointer bg-primary px-5 text-sm font-black text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50">{saving ? '保存中...' : '保存'}</button>
        </footer>
      </section>
    </div>
  )
}

function FormField({ label, children }: { label: string, children: ReactNode }) {
  return <label className="block text-sm font-black text-foreground">{label}<span className="mt-1 block">{children}</span></label>
}

function NumberField({ label, ariaLabel, value, onChange, min, max, disabled }: { label: string, ariaLabel: string, value: string, onChange: (value: string) => void, min: number, max: number, disabled: boolean }) {
  return <FormField label={label}><input type="number" aria-label={ariaLabel} value={value} onChange={(event) => onChange(event.target.value)} min={min} max={max} disabled={disabled} className="soft-input min-h-10 w-full px-3 text-sm font-bold" /></FormField>
}

function DeleteMonitorModal({ monitor, deleting, returnFocusRef, fallbackFocusRef, onClose, onConfirm }: { monitor: UptimeMonitor, deleting: boolean, returnFocusRef: RefObject<HTMLElement | null>, fallbackFocusRef: RefObject<HTMLElement | null>, onClose: () => void, onConfirm: () => void }) {
  const cancelButtonRef = useRef<HTMLButtonElement | null>(null)
  const { dialogRef, handleKeyDown } = useDialogFocus(onClose, cancelButtonRef, returnFocusRef, fallbackFocusRef)
  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-4 py-6">
      <section ref={dialogRef} role="dialog" aria-modal="true" aria-label="删除服务拨测" tabIndex={-1} onKeyDown={handleKeyDown} className="soft-modal-shell w-full max-w-md p-5 outline-none">
        <h2 className="text-lg font-black text-foreground">删除“{monitor.name}”？</h2>
        <p className="mt-2 text-sm font-semibold leading-6 text-muted-foreground">检测结果和事件记录也会一并删除，此操作无法撤销。</p>
        <div className="mt-5 flex justify-end gap-2">
          <button ref={cancelButtonRef} type="button" onClick={onClose} disabled={deleting} className="soft-button min-h-10 cursor-pointer border border-border bg-card px-4 text-sm font-black text-muted-foreground">取消</button>
          <button type="button" onClick={onConfirm} disabled={deleting} className="soft-button min-h-10 cursor-pointer bg-danger px-4 text-sm font-black text-white disabled:cursor-not-allowed disabled:opacity-50">{deleting ? '删除中...' : '确认删除'}</button>
        </div>
      </section>
    </div>
  )
}

function HistoryModal({ monitor, results, incidents, loading, error, returnFocusRef, fallbackFocusRef, onClose }: { monitor: UptimeMonitor, results: UptimeResult[], incidents: UptimeIncident[], loading: boolean, error?: string, returnFocusRef: RefObject<HTMLElement | null>, fallbackFocusRef: RefObject<HTMLElement | null>, onClose: () => void }) {
  const closeButtonRef = useRef<HTMLButtonElement | null>(null)
  const { dialogRef, handleKeyDown } = useDialogFocus(onClose, closeButtonRef, returnFocusRef, fallbackFocusRef)
  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-6">
      <section ref={dialogRef} role="dialog" aria-modal="true" aria-label="服务拨测历史" tabIndex={-1} onKeyDown={handleKeyDown} className="soft-modal-shell flex max-h-[92vh] w-full max-w-5xl flex-col outline-none">
        <header className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4">
          <div><h2 className="text-lg font-black text-foreground">{monitor.name} · 历史</h2><p className="mt-1 break-all text-xs font-semibold text-muted-foreground">{monitor.target}</p></div>
          <button ref={closeButtonRef} type="button" aria-label="关闭服务拨测历史" onClick={onClose} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={16} /></button>
        </header>
        <div className="overflow-y-auto px-5 py-4">
          {loading ? <p className="py-10 text-center text-sm font-black text-muted-foreground">正在加载历史...</p> : error ? <div role="alert" className="rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger">历史记录加载失败: {error}</div> : (
            <div className="grid gap-5 lg:grid-cols-2">
              <HistorySection title="最近检测" icon={<Activity size={16} />} empty="暂无检测结果">
                {results.map((result) => <div key={result.id} className="rounded-2xl border border-border bg-surface/70 px-3 py-3"><div className="flex items-center justify-between gap-3"><span className={`text-xs font-black ${result.success ? 'text-success' : 'text-danger'}`}>{result.success ? '成功' : '失败'}</span><span className="text-xs font-semibold text-muted-foreground">{formatDate(result.checked_at)}</span></div><p className="mt-1 text-sm font-black text-foreground">{result.latency_ms} ms{result.status_code ? ` · HTTP ${result.status_code}` : ''}</p>{result.error ? <p className="mt-1 text-xs font-bold text-danger">{result.error}</p> : null}</div>)}
              </HistorySection>
              <HistorySection title="事件记录" icon={<ShieldCheck size={16} />} empty="暂无故障或证书事件">
                {incidents.map((incident) => <div key={incident.id} className="rounded-2xl border border-border bg-surface/70 px-3 py-3"><div className="flex items-center justify-between gap-3"><span className="text-xs font-black text-foreground">{incident.kind === 'certificate' ? '证书预警' : '可用性故障'}</span><span className={`text-xs font-black ${incident.resolved_at ? 'text-success' : 'text-danger'}`}>{incident.resolved_at ? '已恢复' : '进行中'}</span></div><p className="mt-1 text-xs font-semibold text-muted-foreground">{incident.message}</p><p className="mt-2 text-[11px] font-semibold text-muted-foreground">开始 {formatDate(incident.started_at)}{incident.resolved_at ? ` · 恢复 ${formatDate(incident.resolved_at)}` : ''}</p><NotificationOutcome incident={incident} /></div>)}
              </HistorySection>
            </div>
          )}
        </div>
      </section>
    </div>
  )
}

function HistorySection({ title, icon, empty, children }: { title: string, icon: ReactNode, empty: string, children: ReactNode }) {
  const items = Array.isArray(children) ? children : [children]
  return <section><h3 className="flex items-center gap-2 text-sm font-black text-foreground">{icon}{title}</h3><div className="mt-3 space-y-2">{items.length === 0 ? <p className="rounded-2xl border border-dashed border-border px-3 py-8 text-center text-xs font-semibold text-muted-foreground">{empty}</p> : children}</div></section>
}

function NotificationOutcome({ incident }: { incident: UptimeIncident }) {
  const trigger = incident.notification_sent ? '触发通知成功' : incident.notification_error ? `触发通知失败: ${incident.notification_error}` : '未配置触发通知'
  const recovery = incident.resolved_at ? incident.recovery_notification_sent ? '恢复通知成功' : incident.recovery_notification_error ? `恢复通知失败: ${incident.recovery_notification_error}` : '未配置恢复通知' : undefined
  return <p className="mt-2 text-[11px] font-semibold text-muted-foreground">{trigger}{recovery ? ` · ${recovery}` : ''}</p>
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '网络错误'
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

const dialogFocusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

function getDialogFocusableElements(dialog: HTMLElement) {
  return Array.from(dialog.querySelectorAll<HTMLElement>(dialogFocusableSelector))
    .filter((element) => element.getAttribute('aria-hidden') !== 'true' && !element.closest('[hidden]'))
}

function useDialogFocus<InitialElement extends HTMLElement>(onClose: () => void, initialFocusRef: RefObject<InitialElement | null>, returnFocusRef: RefObject<HTMLElement | null>, fallbackFocusRef: RefObject<HTMLElement | null>) {
  const dialogRef = useRef<HTMLElement | null>(null)
  const closeRef = useRef(onClose)
  closeRef.current = onClose

  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return undefined
    const initialFocus = initialFocusRef.current || getDialogFocusableElements(dialog)[0] || dialog
    initialFocus.focus()

    return () => {
      const trigger = returnFocusRef.current
      const focusTarget = trigger?.isConnected ? trigger : fallbackFocusRef.current
      if (focusTarget?.isConnected) focusTarget.focus()
    }
  }, [fallbackFocusRef, initialFocusRef, returnFocusRef])

  const handleKeyDown = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      closeRef.current()
      return
    }
    if (event.key !== 'Tab') return

    const dialog = dialogRef.current
    if (!dialog) return
    const focusable = getDialogFocusableElements(dialog)
    if (focusable.length === 0) {
      event.preventDefault()
      dialog.focus()
      return
    }

    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const active = document.activeElement
    if (event.shiftKey && (active === first || active === dialog || !dialog.contains(active))) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && (active === last || active === dialog || !dialog.contains(active))) {
      event.preventDefault()
      first.focus()
    }
  }

  return { dialogRef, handleKeyDown }
}
