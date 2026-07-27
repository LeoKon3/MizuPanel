import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import { AlertCircle, CheckCircle2, Clock3, Copy, Loader2, RefreshCw } from 'lucide-react'

import { getAutomationRun } from '../api/client'
import type { AutomationRunDetail, AutomationRunStatus, AutomationTargetStatus } from '../types'
import { TaskDialog } from './TaskDialog'

type TaskRunDetailModalProps = {
  runID: number
  returnFocusRef: RefObject<HTMLElement | null>
  fallbackFocusRef?: RefObject<HTMLElement | null>
  onClose: () => void
  onToast: (message: string, type: 'success' | 'error') => void
  onRunUpdated?: (run: AutomationRunDetail) => void
}

type DisplayStatus = AutomationRunStatus | AutomationTargetStatus

export function TaskRunDetailModal({ runID, returnFocusRef, fallbackFocusRef, onClose, onToast, onRunUpdated }: TaskRunDetailModalProps) {
  const [detail, setDetail] = useState<AutomationRunDetail>()
  const [selectedTargetID, setSelectedTargetID] = useState<number>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const requestSequence = useRef(0)
  const activeController = useRef<AbortController | null>(null)
  const updateRef = useRef(onRunUpdated)
  updateRef.current = onRunUpdated

  const loadDetail = useCallback(async (showLoading: boolean) => {
    const requestID = ++requestSequence.current
    activeController.current?.abort()
    const controller = new AbortController()
    activeController.current = controller
    if (showLoading) setLoading(true)
    try {
      const response = await getAutomationRun(runID, controller.signal)
      if (requestID !== requestSequence.current) return
      const targets = response.targets || []
      setDetail({ ...response, targets })
      setSelectedTargetID((current) => current && targets.some((target) => target.id === current) ? current : targets[0]?.id)
      setError(undefined)
      updateRef.current?.({ ...response, targets })
    } catch (requestError: unknown) {
      if (requestID !== requestSequence.current || isAbortError(requestError)) return
      setError(errorMessage(requestError))
    } finally {
      if (requestID === requestSequence.current) setLoading(false)
    }
  }, [runID])

  useEffect(() => {
    setDetail(undefined)
    setSelectedTargetID(undefined)
    void loadDetail(true)
    return () => {
      requestSequence.current += 1
      activeController.current?.abort()
    }
  }, [loadDetail])

  const shouldPoll = detail !== undefined && !isTerminalRunStatus(detail.status)

  useEffect(() => {
    if (!shouldPoll) return undefined
    let cancelled = false
    let timer: number | undefined
    const schedule = () => {
      timer = window.setTimeout(async () => {
        await loadDetail(false)
        if (!cancelled) schedule()
      }, 2000)
    }
    schedule()
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [loadDetail, shouldPoll])

  const selectedTarget = detail?.targets.find((target) => target.id === selectedTargetID)

  const copyOutput = async () => {
    if (!selectedTarget) return
    try {
      await navigator.clipboard.writeText(selectedTarget.output || '')
      onToast('执行输出复制成功', 'success')
    } catch (copyError) {
      onToast(`执行输出复制失败: ${errorMessage(copyError)}`, 'error')
    }
  }

  return (
    <TaskDialog
      ariaLabel="执行详情"
      title={detail ? `执行 #${detail.id} · ${detail.task_name || detail.script_name}` : `执行 #${runID}`}
      description={detail ? `${triggerLabel(detail.trigger)} · ${formatAutomationDate(detail.created_at)}` : undefined}
      size="xl"
      returnFocusRef={returnFocusRef}
      fallbackFocusRef={fallbackFocusRef}
      onClose={onClose}
      footer={(
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-xs font-bold text-muted-foreground">{detail && !isTerminalRunStatus(detail.status) ? '任务仍在后台执行' : '执行结果已持久化'}</p>
          <div className="flex gap-2">
            <button type="button" onClick={() => void loadDetail(false)} className="soft-button inline-flex min-h-10 items-center gap-2 border border-border bg-card px-4 text-xs font-black text-foreground"><RefreshCw size={14} aria-hidden="true" />刷新</button>
            <button type="button" onClick={onClose} className="soft-button min-h-10 bg-primary px-4 text-xs font-black text-primary-foreground">关闭</button>
          </div>
        </div>
      )}
    >
      {loading && !detail ? <div className="flex min-h-64 items-center justify-center text-sm font-black text-muted-foreground"><Loader2 className="mr-2 animate-spin" size={18} aria-hidden="true" />正在加载执行详情...</div> : null}
      {error && !detail ? <div role="alert" className="break-words rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger [overflow-wrap:anywhere]">执行详情加载失败: {error}</div> : null}
      {detail ? (
        <div className="space-y-4">
          {error ? <div role="alert" className="break-words rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger [overflow-wrap:anywhere]">执行详情刷新失败: {error}</div> : null}
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <RunDatum label="状态" value={<TaskStatusBadge status={detail.status} />} />
            <RunDatum label="目标" value={`${detail.completed_targets}/${detail.total_targets}`} />
            <RunDatum label="成功" value={String(detail.success_targets)} tone="success" />
            <RunDatum label="失败" value={String(detail.failed_targets)} tone={detail.failed_targets > 0 ? 'danger' : undefined} />
          </div>

          {detail.error ? <div className="break-words rounded-2xl border border-danger/25 bg-danger/5 px-4 py-3 text-sm font-bold text-danger [overflow-wrap:anywhere]">{detail.error}</div> : null}

          <div className="grid min-w-0 gap-4 lg:grid-cols-[280px_minmax(0,1fr)]">
            <section className="min-w-0" aria-label="执行目标">
              <h3 className="text-sm font-black text-foreground">执行目标</h3>
              <div className="mt-2 max-h-[48vh] overflow-y-auto rounded-2xl border border-border bg-card p-2">
                {detail.targets.length === 0 ? <p className="px-3 py-10 text-center text-xs font-bold text-muted-foreground">暂无目标结果</p> : detail.targets.map((target) => (
                  <button
                    key={target.id}
                    type="button"
                    aria-pressed={selectedTargetID === target.id}
                    onClick={() => setSelectedTargetID(target.id)}
                    className={`soft-button mb-1 flex min-h-14 w-full min-w-0 items-center gap-3 px-3 py-2 text-left last:mb-0 ${selectedTargetID === target.id ? 'bg-primary/10 ring-1 ring-primary/25' : 'hover:bg-muted'}`}
                  >
                    <StatusIcon status={target.status} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-black text-foreground">{target.node_name || target.node_id}</span>
                      <span className="block truncate text-[11px] font-semibold text-muted-foreground">{target.node_id}</span>
                    </span>
                    <TaskStatusBadge status={target.status} compact />
                  </button>
                ))}
              </div>
            </section>

            <section className="min-w-0" aria-label="节点执行输出">
              <div className="flex min-h-8 flex-wrap items-center justify-between gap-2">
                <h3 className="min-w-0 break-words text-sm font-black text-foreground [overflow-wrap:anywhere]">{selectedTarget ? `${selectedTarget.node_name || selectedTarget.node_id} · 输出` : '执行输出'}</h3>
                {selectedTarget ? (
                  <button type="button" title="复制输出" aria-label="复制执行输出" onClick={() => void copyOutput()} className="soft-button inline-flex h-8 w-8 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground"><Copy size={14} aria-hidden="true" /></button>
                ) : null}
              </div>
              {selectedTarget ? (
                <div className="mt-2 min-w-0">
                  <div className="flex flex-wrap items-center gap-2 text-xs font-bold text-muted-foreground">
                    <TaskStatusBadge status={selectedTarget.status} />
                    <span>退出码 {selectedTarget.exit_code ?? '—'}</span>
                    <span>耗时 {formatDuration(selectedTarget.duration_ms)}</span>
                    {selectedTarget.output_truncated ? <span className="rounded-full bg-warning/10 px-2 py-0.5 font-black text-warning">输出已截断</span> : null}
                  </div>
                  {selectedTarget.error ? <p className="mt-2 break-words rounded-2xl border border-danger/20 bg-danger/5 px-3 py-2 text-xs font-bold text-danger [overflow-wrap:anywhere]">{selectedTarget.error}</p> : null}
                  <pre className="mt-3 h-[38vh] min-h-56 max-h-[420px] min-w-0 overflow-auto whitespace-pre-wrap break-words rounded-2xl border border-border bg-code p-4 font-mono text-xs leading-5 text-code-foreground">{selectedTarget.output || '暂无输出'}</pre>
                </div>
              ) : <div className="mt-2 flex min-h-56 items-center justify-center rounded-2xl border border-dashed border-border text-sm font-bold text-muted-foreground">选择一个节点查看输出</div>}
            </section>
          </div>
        </div>
      ) : null}
    </TaskDialog>
  )
}

function RunDatum({ label, value, tone }: { label: string, value: React.ReactNode, tone?: 'success' | 'danger' }) {
  return <div className="rounded-2xl border border-border bg-surface/70 px-3 py-3"><p className="text-[11px] font-black text-muted-foreground">{label}</p><div className={`mt-1 text-sm font-black ${tone === 'success' ? 'text-success' : tone === 'danger' ? 'text-danger' : 'text-foreground'}`}>{value}</div></div>
}

function StatusIcon({ status }: { status: DisplayStatus }) {
  if (status === 'queued' || status === 'running') return <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary"><Loader2 className="animate-spin" size={15} aria-hidden="true" /></span>
  if (status === 'success') return <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-success/10 text-success"><CheckCircle2 size={15} aria-hidden="true" /></span>
  if (status === 'skipped' || status === 'unsupported' || status === 'offline') return <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-warning/10 text-warning"><Clock3 size={15} aria-hidden="true" /></span>
  return <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-danger/10 text-danger"><AlertCircle size={15} aria-hidden="true" /></span>
}

export function TaskStatusBadge({ status, compact = false }: { status: DisplayStatus, compact?: boolean }) {
  const visual = statusVisual(status)
  return <span className={`inline-flex shrink-0 items-center rounded-full font-black ${compact ? 'px-2 py-0.5 text-[10px]' : 'px-2.5 py-1 text-xs'} ${visual.className}`}>{visual.label}</span>
}

function statusVisual(status: DisplayStatus) {
  switch (status) {
    case 'queued': return { label: '排队中', className: 'bg-muted text-muted-foreground' }
    case 'running': return { label: '执行中', className: 'bg-primary/10 text-primary' }
    case 'success': return { label: '成功', className: 'bg-success/10 text-success' }
    case 'partial': return { label: '部分失败', className: 'bg-warning/10 text-warning' }
    case 'failed': return { label: '失败', className: 'bg-danger/10 text-danger' }
    case 'timed_out': return { label: '超时', className: 'bg-danger/10 text-danger' }
    case 'busy': return { label: '繁忙', className: 'bg-warning/10 text-warning' }
    case 'cancelled': return { label: '已取消', className: 'bg-muted text-muted-foreground' }
    case 'offline': return { label: '离线', className: 'bg-muted text-muted-foreground' }
    case 'unsupported': return { label: '需升级', className: 'bg-warning/10 text-warning' }
    case 'skipped': return { label: '已跳过', className: 'bg-warning/10 text-warning' }
    case 'interrupted': return { label: '已中断', className: 'bg-danger/10 text-danger' }
  }
}

export function isTerminalRunStatus(status: AutomationRunStatus) {
  return status !== 'queued' && status !== 'running'
}

export function formatAutomationDate(value?: string | null) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

export function formatDuration(durationMS: number) {
  if (durationMS < 1000) return `${Math.max(0, durationMS)} ms`
  return `${(durationMS / 1000).toFixed(durationMS < 10_000 ? 2 : 1)} s`
}

export function runDuration(startedAt?: string | null, completedAt?: string | null) {
  if (!startedAt) return '—'
  const start = Date.parse(startedAt)
  const end = completedAt ? Date.parse(completedAt) : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end)) return '—'
  return formatDuration(Math.max(0, end - start))
}

function triggerLabel(trigger: AutomationRunDetail['trigger']) {
  return trigger === 'scheduled' ? '计划触发' : '手动触发'
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '网络错误'
}

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError'
}
