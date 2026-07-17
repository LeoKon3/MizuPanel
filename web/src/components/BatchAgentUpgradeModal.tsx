import { useEffect, useMemo, useRef, useState } from 'react'
import { CheckCircle2, CircleAlert, Clock3, Loader2, Rocket, X } from 'lucide-react'

import { getAgentUpgradeStatus, getConnectionDiagnostics, upgradeAgent } from '../api/client'
import { mapWithConcurrency } from '../lib/concurrency'
import type { AgentUpgradeStatus, ConnectionDiagnostics, Node } from '../types'

type UpgradeItemStatus = 'checking' | 'eligible' | 'latest' | 'offline' | 'unsupported' | 'diagnostic_failed' | 'starting' | 'waiting' | 'completed' | 'failed'

type UpgradeItem = {
  node: Node
  status: UpgradeItemStatus
  detail: string
  targetVersion?: string
}

type BatchAgentUpgradeModalProps = {
  nodes: Node[]
  onClose: () => void
  onFinished: (result: { succeeded: number, failed: number, skipped: number }) => Promise<void> | void
}

export function BatchAgentUpgradeModal({ nodes, onClose, onFinished }: BatchAgentUpgradeModalProps) {
  const [items, setItems] = useState<UpgradeItem[]>(() => nodes.map((node) => ({ node, status: 'checking', detail: '正在检查连接与版本' })))
  const [preflightLoading, setPreflightLoading] = useState(true)
  const [executing, setExecuting] = useState(false)
  const operationID = useRef(0)

  const updateItem = (nodeID: string, patch: Partial<UpgradeItem>) => {
    setItems((current) => current.map((item) => item.node.id === nodeID ? { ...item, ...patch } : item))
  }

  useEffect(() => {
    const currentOperation = operationID.current + 1
    operationID.current = currentOperation
    let cancelled = false
    setItems(nodes.map((node) => ({ node, status: 'checking', detail: '正在检查连接与版本' })))
    setPreflightLoading(true)

    void mapWithConcurrency(nodes, 4, async (node) => {
      try {
        const diagnostics = await getConnectionDiagnostics(node.id)
        if (!cancelled && operationID.current === currentOperation) updateItem(node.id, classifyDiagnostics(diagnostics))
      } catch (error) {
        if (!cancelled && operationID.current === currentOperation) updateItem(node.id, { status: 'diagnostic_failed', detail: errorMessage(error) })
      }
    }).finally(() => {
      if (!cancelled && operationID.current === currentOperation) setPreflightLoading(false)
    })

    return () => {
      cancelled = true
      operationID.current += 1
    }
  }, [nodes])

  const summary = useMemo(() => ({
    eligible: items.filter((item) => item.status === 'eligible').length,
    latest: items.filter((item) => item.status === 'latest').length,
    offline: items.filter((item) => item.status === 'offline').length,
    unsupported: items.filter((item) => item.status === 'unsupported').length,
    failedDiagnostics: items.filter((item) => item.status === 'diagnostic_failed').length,
    completed: items.filter((item) => item.status === 'completed').length,
    failed: items.filter((item) => item.status === 'failed').length
  }), [items])

  const execute = async () => {
    if (executing) return
    const eligible = items.filter((item) => item.status === 'eligible')
    if (eligible.length === 0) return
    const currentOperation = operationID.current + 1
    operationID.current = currentOperation
    setExecuting(true)

    const outcomes = await mapWithConcurrency(eligible, 4, async (item): Promise<'success' | 'failed' | 'detached'> => {
      if (operationID.current !== currentOperation) return 'detached'
      updateItem(item.node.id, { status: 'starting', detail: '正在下发升级请求' })
      try {
        const response = await upgradeAgent(item.node.id)
        if (!response.accepted) throw new Error(response.error || '升级请求未被接受')
        if (operationID.current !== currentOperation) return 'detached'
        updateItem(item.node.id, { status: 'waiting', detail: '等待 Agent 重新连接并确认版本' })
        const status = await pollUpgradeStatus(item.node.id, () => operationID.current !== currentOperation, (nextStatus) => {
          if (operationID.current === currentOperation) updateItem(item.node.id, { detail: upgradeStageLabel(nextStatus.stage) })
        })
        if (operationID.current !== currentOperation || !('node_id' in status)) return 'detached'
        if (status.stage === 'completed') {
          updateItem(item.node.id, { status: 'completed', detail: `已升级到 ${status.actual_version || status.target_version}` })
          return 'success'
        }
        updateItem(item.node.id, { status: 'failed', detail: status.error || '升级失败' })
        return 'failed'
      } catch (error) {
        if (operationID.current === currentOperation) updateItem(item.node.id, { status: 'failed', detail: errorMessage(error) })
        return operationID.current === currentOperation ? 'failed' : 'detached'
      }
    })

    if (operationID.current !== currentOperation) return
    setExecuting(false)
    const succeeded = outcomes.filter((outcome) => outcome === 'success').length
    const failed = outcomes.filter((outcome) => outcome === 'failed').length
    await onFinished({ succeeded, failed, skipped: nodes.length - eligible.length })
  }

  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-5">
      <section role="dialog" aria-modal="true" aria-label="批量升级 Agent" className="soft-modal-shell flex max-h-[90vh] w-full max-w-3xl flex-col">
        <header className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4">
          <div><p className="text-xs font-black tracking-[0.16em] text-primary">LATEST AGENT</p><h3 className="mt-1 text-lg font-black text-foreground">批量升级 Agent</h3><p className="mt-1 text-xs font-bold text-muted-foreground">只升级到当前 Server 提供的最新版本，最多并发 4 台。</p></div>
          <button type="button" aria-label="关闭" onClick={onClose} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={16} /></button>
        </header>

        <div className="border-b border-border bg-surface/60 px-5 py-3">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
            <Summary label="可升级" value={summary.eligible} tone="primary" />
            <Summary label="最新版" value={summary.latest + summary.completed} tone="success" />
            <Summary label="离线" value={summary.offline} tone="muted" />
            <Summary label="不支持" value={summary.unsupported} tone="warning" />
            <Summary label="检查/升级失败" value={summary.failedDiagnostics + summary.failed} tone="danger" />
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {preflightLoading ? <div className="mb-3 flex items-center rounded-2xl border border-primary/20 bg-primary/5 px-4 py-3 text-xs font-black text-primary"><Loader2 className="mr-2 animate-spin" size={16} />正在并发检查 {nodes.length} 台节点</div> : null}
          <div className="divide-y divide-border rounded-2xl border border-border bg-card">
            {items.map((item) => <UpgradeItemRow key={item.node.id} item={item} />)}
          </div>
          {executing ? <p className="mt-3 rounded-2xl border border-warning/25 bg-warning/10 px-4 py-3 text-xs font-bold leading-5 text-warning">关闭弹窗只会停止前端轮询，已经被 Agent 接受的升级仍会继续执行。</p> : null}
        </div>

        <footer className="soft-modal-footer flex flex-wrap items-center justify-between gap-3 border-t px-5 py-4">
          <p className="text-xs font-bold text-muted-foreground">{preflightLoading ? '正在检查...' : summary.eligible > 0 ? `${summary.eligible} 台可开始升级` : '当前选择中没有可升级节点'}</p>
          <div className="flex gap-2"><button type="button" onClick={onClose} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black">{executing ? '关闭并停止跟踪' : '取消'}</button><button type="button" onClick={() => void execute()} disabled={preflightLoading || executing || summary.eligible === 0} className="soft-button inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-xs font-black text-primary-foreground disabled:opacity-50">{executing ? <Loader2 className="animate-spin" size={15} /> : <Rocket size={15} />}{executing ? '升级执行中' : '确认升级'}</button></div>
        </footer>
      </section>
    </div>
  )
}

function classifyDiagnostics(diagnostics: ConnectionDiagnostics): Partial<UpgradeItem> {
  if (!diagnostics.online) return { status: 'offline', detail: '节点离线，已跳过' }
  if (!diagnostics.upgrade_available) return { status: 'latest', detail: `已是最新版本 ${diagnostics.latest_version || diagnostics.agent_version || ''}`.trim() }
  if (!diagnostics.upgrade_supported) return { status: 'unsupported', detail: '旧版 Agent 不支持面板内升级' }
  return { status: 'eligible', detail: `可升级到 ${diagnostics.latest_version || 'Server 最新版本'}`, targetVersion: diagnostics.latest_version }
}

async function pollUpgradeStatus(nodeID: string, cancelled: () => boolean, onStatus: (status: AgentUpgradeStatus) => void): Promise<AgentUpgradeStatus | { stage: 'detached' }> {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    if (cancelled()) return { stage: 'detached' }
    try {
      const status = await getAgentUpgradeStatus(nodeID)
      onStatus(status)
      if (status.stage === 'completed' || status.stage === 'failed') return status
    } catch (error) {
      if (attempt === 39) return { node_id: nodeID, target_version: '', stage: 'failed', error: errorMessage(error) }
    }
    await delay(3000)
  }
  return { node_id: nodeID, target_version: '', stage: 'failed', error: '等待 Agent 重新连接超时' }
}

function UpgradeItemRow({ item }: { item: UpgradeItem }) {
  const visual = statusVisual(item.status)
  return <div className="flex min-h-16 items-start gap-3 px-4 py-3"><span className={`mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full ${visual.tone}`}>{visual.icon}</span><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><p className="truncate text-sm font-black text-foreground">{item.node.name || item.node.hostname}</p><span className={`rounded-full px-2 py-0.5 text-[10px] font-black ${visual.chip}`}>{visual.label}</span></div><p className="mt-1 text-xs font-bold leading-5 text-muted-foreground">{item.detail}</p></div><span className="shrink-0 font-mono text-[11px] font-bold text-muted-foreground">{item.node.agent_version || '未知'}</span></div>
}

function statusVisual(status: UpgradeItemStatus) {
  if (status === 'checking' || status === 'starting' || status === 'waiting') return { label: status === 'checking' ? '检查中' : '升级中', tone: 'bg-primary/10 text-primary', chip: 'bg-primary/10 text-primary', icon: <Loader2 className="animate-spin" size={16} /> }
  if (status === 'eligible') return { label: '可升级', tone: 'bg-primary/10 text-primary', chip: 'bg-primary/10 text-primary', icon: <Rocket size={16} /> }
  if (status === 'latest' || status === 'completed') return { label: status === 'latest' ? '最新版' : '成功', tone: 'bg-success/10 text-success', chip: 'bg-success/10 text-success', icon: <CheckCircle2 size={16} /> }
  if (status === 'offline') return { label: '离线', tone: 'bg-muted text-muted-foreground', chip: 'bg-muted text-muted-foreground', icon: <Clock3 size={16} /> }
  if (status === 'unsupported') return { label: '不支持', tone: 'bg-warning/10 text-warning', chip: 'bg-warning/10 text-warning', icon: <CircleAlert size={16} /> }
  return { label: '失败', tone: 'bg-danger/10 text-danger', chip: 'bg-danger/10 text-danger', icon: <CircleAlert size={16} /> }
}

function Summary({ label, value, tone }: { label: string, value: number, tone: 'primary' | 'success' | 'muted' | 'warning' | 'danger' }) {
  const toneClass = tone === 'primary' ? 'text-primary' : tone === 'success' ? 'text-success' : tone === 'warning' ? 'text-warning' : tone === 'danger' ? 'text-danger' : 'text-muted-foreground'
  return <div className="rounded-xl border border-border bg-card px-3 py-2"><p className="text-[10px] font-black text-muted-foreground">{label}</p><p className={`mt-0.5 text-lg font-black ${toneClass}`}>{value}</p></div>
}

function upgradeStageLabel(stage: string) {
  if (stage === 'preparing') return '正在准备并校验升级包'
  if (stage === 'replacing') return '正在替换 Agent 二进制'
  if (stage === 'waiting_reconnect') return '等待 Agent 重新连接'
  if (stage === 'completed') return '升级完成'
  if (stage === 'failed') return '升级失败'
  return '升级执行中'
}

function delay(milliseconds: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds))
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '未知错误'
}
