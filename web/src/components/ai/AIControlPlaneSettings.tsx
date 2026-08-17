import { useEffect, useRef, useState } from 'react'
import { LoaderCircle, Pause, Play, Save, ShieldCheck, TriangleAlert, X } from 'lucide-react'

import type { AIControlAction, AIControlMode, AIControlSettings, AIControlSettingsUpdate, Node } from '../../types'
import { Toast } from '../Toast'

type ActiveMode = Exclude<AIControlMode, 'paused'>
type Confirmation = 'pause' | 'resume'

type AIControlPlaneSettingsProps = {
  policy?: AIControlSettings
  nodes: Node[]
  onSave: (update: AIControlSettingsUpdate) => Promise<AIControlSettings>
}

const actionGroups: ReadonlyArray<{
  label: string
  actions: ReadonlyArray<{ value: AIControlAction, label: string }>
}> = [
  {
    label: 'Docker 容器',
    actions: [
      { value: 'docker.container.start', label: '启动容器' },
      { value: 'docker.container.restart', label: '重启容器' }
    ]
  },
  {
    label: 'Compose 服务',
    actions: [
      { value: 'compose.service.start', label: '启动服务' },
      { value: 'compose.service.restart', label: '重启服务' }
    ]
  },
  {
    label: 'Systemd 服务',
    actions: [
      { value: 'systemd.service.start', label: '启动服务' },
      { value: 'systemd.service.restart', label: '重启服务' }
    ]
  }
]

const actionOrder = actionGroups.flatMap((group) => group.actions.map((action) => action.value))

function policyModeLabel(mode?: AIControlMode) {
  if (mode === 'low_risk_auto') return '低风险自动'
  if (mode === 'paused') return '已暂停'
  return '全部确认'
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : '未知错误'
}

export function AIControlPlaneSettings({ policy, nodes, onSave }: AIControlPlaneSettingsProps) {
  const [selectedMode, setSelectedMode] = useState<ActiveMode>('confirm_all')
  const [allowedActions, setAllowedActions] = useState<AIControlAction[]>([])
  const [nodeScope, setNodeScope] = useState<string[]>([])
  const [busyAction, setBusyAction] = useState<'save' | 'pause' | 'resume'>()
  const [confirmation, setConfirmation] = useState<Confirmation>()
  const [toast, setToast] = useState<{ message: string, type: 'success' | 'error' }>()
  const dialogRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!policy) return
    if (policy.mode !== 'paused') setSelectedMode(policy.mode)
    setAllowedActions([...policy.allowed_actions])
    setNodeScope([...policy.node_scope])
  }, [policy])

  useEffect(() => {
    if (confirmation) dialogRef.current?.focus()
  }, [confirmation])

  const isPaused = policy?.emergency_stopped === true || policy?.mode === 'paused'
  const busy = Boolean(busyAction)
  const selectedNodes = new Set(nodeScope)
  const missingNodeIDs = nodeScope.filter((nodeID) => !nodes.some((node) => node.id === nodeID))

  const toggleAction = (action: AIControlAction) => {
    setAllowedActions((current) => current.includes(action)
      ? current.filter((value) => value !== action)
      : actionOrder.filter((value) => value === action || current.includes(value)))
  }

  const toggleNode = (node: Node) => {
    const selected = selectedNodes.has(node.id)
    if (node.status !== 'online' && !selected) return
    setNodeScope((current) => selected ? current.filter((nodeID) => nodeID !== node.id) : [...current, node.id])
  }

  const removeMissingNode = (nodeID: string) => {
    setNodeScope((current) => current.filter((value) => value !== nodeID))
  }

  const savePolicy = async (
    mode: AIControlMode,
    action: 'save' | 'pause' | 'resume',
    successMessage: string,
    source: 'draft' | 'persisted' = 'draft'
  ) => {
    if (!policy) return
    const nextActions = source === 'persisted' ? policy.allowed_actions : allowedActions
    const nextScope = source === 'persisted' ? policy.node_scope : nodeScope
    if (mode === 'low_risk_auto') {
      const offlineNode = nodes.find((node) => nextScope.includes(node.id) && node.status !== 'online')
      if (offlineNode) {
        setToast({ type: 'error', message: `AI 控制平面设置保存失败: 节点 ${offlineNode.name} 当前离线` })
        return
      }
    }

    setBusyAction(action)
    try {
      await onSave({ mode, allowed_actions: [...nextActions], node_scope: [...nextScope] })
      setToast({ type: 'success', message: successMessage })
    } catch (error) {
      const operation = action === 'pause' ? '暂停' : action === 'resume' ? '恢复' : '设置保存'
      setToast({ type: 'error', message: `AI 控制平面${operation}失败: ${errorText(error)}` })
    } finally {
      setBusyAction(undefined)
    }
  }

  const confirmTransition = () => {
    const next = confirmation
    setConfirmation(undefined)
    if (next === 'pause') {
      void savePolicy('paused', 'pause', 'AI 控制平面暂停成功', 'persisted')
    } else if (next === 'resume') {
      void savePolicy(selectedMode, 'resume', 'AI 控制平面恢复成功')
    }
  }

  return (
    <section className="border-b border-border bg-card px-4 py-5 sm:px-5" aria-labelledby="ai-control-plane-title">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <ShieldCheck className="h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
          <h3 id="ai-control-plane-title" className="text-xl font-black text-foreground">AI 控制平面</h3>
          <span className={`soft-chip px-2.5 py-1 text-xs font-black ${isPaused ? 'bg-danger/10 text-danger' : policy?.mode === 'low_risk_auto' ? 'bg-success/10 text-success' : 'text-muted-foreground'}`}>
            {policy ? policyModeLabel(policy.mode) : '加载中'}
          </span>
        </div>
        {isPaused ? (
          <button
            type="button"
            disabled={!policy || busy}
            onClick={() => setConfirmation('resume')}
            className="soft-button inline-flex min-h-10 items-center gap-2 bg-success px-4 text-sm font-black text-white shadow-sm hover:brightness-95 focus:outline-none focus:ring-4 focus:ring-success/20 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {busyAction === 'resume' ? <LoaderCircle size={16} className="animate-spin" aria-hidden="true" /> : <Play size={16} aria-hidden="true" />}
            恢复 AI 控制平面
          </button>
        ) : (
          <button
            type="button"
            disabled={!policy || busy}
            onClick={() => setConfirmation('pause')}
            className="soft-button inline-flex min-h-10 items-center gap-2 border border-danger/40 bg-card px-4 text-sm font-black text-danger hover:bg-danger/10 focus:outline-none focus:ring-4 focus:ring-danger/20 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {busyAction === 'pause' ? <LoaderCircle size={16} className="animate-spin" aria-hidden="true" /> : <Pause size={16} aria-hidden="true" />}
            紧急暂停
          </button>
        )}
      </div>

      {isPaused ? (
        <div className="mt-4 flex items-start gap-3 border border-danger/30 bg-danger/10 px-3 py-3 text-danger" role="status">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
          <p className="text-xs font-bold leading-5">新的 AI 变更步骤已停止；已经受理的远端操作仍会继续验证，不会被撤销。</p>
        </div>
      ) : null}

      <div className="mt-4 grid min-w-0 gap-5 xl:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]">
        <div className="min-w-0 space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-4">
            <div>
              <p className="text-sm font-black text-foreground">{isPaused ? '恢复后模式' : '运行模式'}</p>
              <p className="mt-1 text-xs font-semibold text-muted-foreground">策略修订 {policy?.revision ?? '-'}</p>
            </div>
            <div className="inline-flex border border-border bg-surface p-1" role="group" aria-label={isPaused ? '选择恢复后模式' : '选择 AI 控制模式'}>
              {(['confirm_all', 'low_risk_auto'] as const).map((mode) => (
                <button
                  key={mode}
                  type="button"
                  disabled={!policy || busy}
                  aria-pressed={selectedMode === mode}
                  onClick={() => setSelectedMode(mode)}
                  className={`soft-button min-h-9 px-3 text-xs font-black focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60 ${selectedMode === mode ? 'bg-primary text-primary-foreground shadow-sm' : 'text-muted-foreground hover:bg-card hover:text-foreground'}`}
                >
                  {policyModeLabel(mode)}
                </button>
              ))}
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between gap-3">
              <p className="text-sm font-black text-foreground">允许自动执行的动作</p>
              <span className="text-xs font-black text-muted-foreground">已选 {allowedActions.length}/6</span>
            </div>
            <div className="mt-2 grid overflow-hidden border border-border bg-surface md:grid-cols-3">
              {actionGroups.map((group) => (
                <fieldset key={group.label} className="min-w-0 border-b border-border p-3 last:border-b-0 md:border-b-0 md:border-r md:last:border-r-0">
                  <legend className="px-1 text-xs font-black text-foreground">{group.label}</legend>
                  <div className="mt-1 space-y-2">
                    {group.actions.map((action) => (
                      <label key={action.value} className="flex min-h-8 cursor-pointer items-center gap-2 text-xs font-bold text-muted-foreground">
                        <input
                          type="checkbox"
                          aria-label={`${group.label} ${action.label}`}
                          checked={allowedActions.includes(action.value)}
                          disabled={!policy || busy}
                          onChange={() => toggleAction(action.value)}
                          className="h-4 w-4 shrink-0 accent-primary"
                        />
                        <span>{action.label}</span>
                      </label>
                    ))}
                  </div>
                </fieldset>
              ))}
            </div>
          </div>
        </div>

        <div className="min-w-0">
          <div className="flex items-center justify-between gap-3">
            <p className="text-sm font-black text-foreground">自动执行节点范围</p>
            <span className="text-xs font-black text-muted-foreground">已选 {nodeScope.length} 台</span>
          </div>
          <div className="mt-2 max-h-52 overflow-y-auto border border-border bg-surface">
            {!policy ? (
              <div className="flex min-h-24 items-center justify-center text-xs font-bold text-muted-foreground"><LoaderCircle className="mr-2 h-4 w-4 animate-spin" aria-hidden="true" />正在加载策略</div>
            ) : nodes.length === 0 && missingNodeIDs.length === 0 ? (
              <p className="px-3 py-8 text-center text-xs font-semibold text-muted-foreground">暂无可选节点</p>
            ) : (
              <>
                {nodes.map((node) => {
                  const selected = selectedNodes.has(node.id)
                  const online = node.status === 'online'
                  return (
                    <label key={node.id} className={`flex min-h-11 items-center gap-3 border-b border-border px-3 last:border-b-0 ${online || selected ? 'cursor-pointer' : 'cursor-not-allowed opacity-55'}`}>
                      <input
                        type="checkbox"
                        aria-label={`${node.name} ${online ? '在线' : '离线'}`}
                        checked={selected}
                        disabled={busy || (!online && !selected)}
                        onChange={() => toggleNode(node)}
                        className="h-4 w-4 shrink-0 accent-primary"
                      />
                      <span className="min-w-0 flex-1 truncate text-sm font-black text-foreground">{node.name}</span>
                      <span className={`shrink-0 text-[11px] font-black ${online ? 'text-success' : 'text-muted-foreground'}`}>{online ? '在线' : '离线'}</span>
                    </label>
                  )
                })}
                {missingNodeIDs.map((nodeID) => (
                  <label key={nodeID} className="flex min-h-11 cursor-pointer items-center gap-3 border-b border-border px-3 last:border-b-0">
                    <input type="checkbox" aria-label={`${nodeID} 不可用`} checked disabled={busy} onChange={() => removeMissingNode(nodeID)} className="h-4 w-4 shrink-0 accent-primary" />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs font-bold text-muted-foreground">{nodeID}</span>
                    <span className="shrink-0 text-[11px] font-black text-danger">不可用</span>
                  </label>
                ))}
              </>
            )}
          </div>
        </div>
      </div>

      <div className="mt-4 flex justify-end border-t border-border pt-4">
        <button
          type="button"
          disabled={!policy || busy}
          onClick={() => void savePolicy(isPaused ? 'paused' : selectedMode, 'save', 'AI 控制平面设置保存成功')}
          className="soft-button inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {busyAction === 'save' ? <LoaderCircle size={16} className="animate-spin" aria-hidden="true" /> : <Save size={16} aria-hidden="true" />}
          {busyAction === 'save' ? '保存中...' : '保存策略'}
        </button>
      </div>

      {confirmation ? (
        <div className="soft-modal-overlay fixed inset-0 z-[80] flex items-center justify-center p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setConfirmation(undefined) }}>
          <div
            ref={dialogRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="ai-control-confirm-title"
            tabIndex={-1}
            className="soft-modal-shell w-full max-w-md text-left outline-none"
            onKeyDown={(event) => { if (event.key === 'Escape') setConfirmation(undefined) }}
          >
            <div className="soft-modal-header flex items-start justify-between gap-3 border-b px-4 py-3">
              <div className="min-w-0">
                <p id="ai-control-confirm-title" className="text-base font-black text-foreground">{confirmation === 'pause' ? '暂停 AI 控制平面' : '恢复 AI 控制平面'}</p>
                <p className="mt-1 text-xs font-semibold text-muted-foreground">{confirmation === 'pause' ? '暂停后只保留查询与诊断能力。' : `恢复后使用“${policyModeLabel(selectedMode)}”模式。`}</p>
              </div>
              <button type="button" onClick={() => setConfirmation(undefined)} title="关闭确认窗口" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center border border-border text-muted-foreground hover:text-foreground">
                <X size={16} aria-hidden="true" />
                <span className="sr-only">关闭确认窗口</span>
              </button>
            </div>
            <div className="flex gap-3 px-4 py-4 text-sm">
              <TriangleAlert className={`mt-0.5 h-5 w-5 shrink-0 ${confirmation === 'pause' ? 'text-danger' : 'text-warning'}`} aria-hidden="true" />
              <p className="font-semibold leading-6 text-muted-foreground">
                {confirmation === 'pause'
                  ? '新的 AI 变更计划和后续排队步骤将被阻止；已经受理的远端操作不会被撤销。'
                  : '已取消或跳过的步骤不会恢复，也不会自动重试。'}
              </p>
            </div>
            <div className="soft-modal-footer flex justify-end gap-2 border-t px-4 py-3">
              <button type="button" onClick={() => setConfirmation(undefined)} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground">取消</button>
              <button type="button" onClick={confirmTransition} className={`soft-button inline-flex min-h-10 items-center gap-2 px-4 text-sm font-black text-white ${confirmation === 'pause' ? 'bg-danger' : 'bg-success'}`}>
                {confirmation === 'pause' ? <Pause size={16} aria-hidden="true" /> : <Play size={16} aria-hidden="true" />}
                {confirmation === 'pause' ? '确认暂停' : '确认恢复'}
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {toast ? <Toast {...toast} onClose={() => setToast(undefined)} /> : null}
    </section>
  )
}
