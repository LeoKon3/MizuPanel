import { type KeyboardEvent, type PointerEvent, useEffect, useRef, useState } from 'react'
import { Bot, ChevronRight, CircleAlert, LoaderCircle, Maximize2, MessageSquarePlus, PanelRightClose, Plus, Send, Settings2, Square, Trash2, X } from 'lucide-react'

import type { AIProgress, AIToolCall } from '../../types'
import { Toast } from '../Toast'
import { AIModelSelector } from './AIModelSelector'
import type { AIAssistantState } from './useAIAssistantState'
import { findAIModel, isAIModelUsable } from './useAIAssistantState'

type ConversationPanelProps = {
  assistant: AIAssistantState
  mode: 'drawer' | 'workspace'
  onOpenSettings: () => void
}

const toolLabels: Record<string, string> = {
  get_operational_overview: '查询运维概览',
  list_nodes: '查询节点',
  get_node_metrics: '查询节点指标',
  list_alerts: '查询告警',
  list_application_services: '查询应用服务',
  list_uptime_monitors: '查询拨测状态',
  get_log_snapshot: '读取日志快照',
  list_k8s_clusters: '查询 Kubernetes 集群',
  reboot_node: '重启主机',
  upgrade_agent: '升级 Agent',
  docker_container_action: '变更容器状态',
  compose_service_action: '变更 Compose 状态',
  systemd_service_action: '变更 Systemd 服务',
  run_saved_script: '运行已有脚本',
  create_scheduled_task: '创建计划任务',
  create_docker_container: '创建 Docker 容器',
  create_k8s_deployment: '创建 Kubernetes Deployment'
}

function toolTarget(call: AIToolCall) {
  return call.target_name || call.target_id || call.node_id || '当前运维环境'
}

function progressText(progress: AIProgress, labels: Record<string, string>) {
  if (progress.phase === 'model') return '思考中...'
  if (progress.phase === 'fallback') return `当前请求模型暂时不可用，切换到 ${[progress.provider_name, progress.model].filter(Boolean).join(' / ') || '回退模型'}...`
  if (progress.phase === 'tool') return `调用工具: ${labels[progress.tool_name ?? ''] ?? progress.tool_name ?? ''} → ${progress.target_name ?? ''}`
  if (progress.phase === 'composing') return '整理结果...'
  if (progress.phase === 'awaiting_confirmation') return '等待确认...'
  if (progress.phase === 'accepted') return '准备请求...'
  return '模型正在处理请求'
}

function ConversationPanel({ assistant, mode, onOpenSettings }: ConversationPanelProps) {
  const [draft, setDraft] = useState('')
  const [confirmCall, setConfirmCall] = useState<AIToolCall>()
  const composerRef = useRef<HTMLTextAreaElement>(null)
  const confirmButtonRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const messages = assistant.conversation?.messages ?? []
  const toolCalls = assistant.conversation?.tool_calls ?? []
  const selected = findAIModel(assistant.providers, assistant.selectedModelID)
  const canSend = selected?.provider.id === assistant.selectedProviderID && isAIModelUsable(selected.provider, selected.model)
  const activeSending = assistant.sending && assistant.sendingConversationID === assistant.activeConversationID

  useEffect(() => {
    if (confirmCall) dialogRef.current?.focus()
  }, [confirmCall])

  const closeConfirmation = () => {
    setConfirmCall(undefined)
    requestAnimationFrame(() => confirmButtonRef.current?.focus())
  }

  const submit = () => {
    if (!draft.trim() || assistant.sending) return
    const content = draft
    setDraft('')
    void assistant.send(content)
  }

  const handleComposerKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      submit()
    }
  }

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-card">
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4" data-testid={`ai-${mode}-messages`}>
        {assistant.loading && !assistant.conversation ? (
          <div className="flex flex-1 items-center justify-center text-sm font-bold text-muted-foreground">
            <LoaderCircle className="mr-2 h-4 w-4 animate-spin" aria-hidden="true" />
            正在加载会话
          </div>
        ) : assistant.error ? (
          <div className="m-auto max-w-sm border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-bold leading-6 text-danger">
            {assistant.error}
          </div>
        ) : assistant.providers.length === 0 ? (
          <div className="m-auto max-w-sm text-center">
            <Bot className="mx-auto h-9 w-9 text-muted-foreground" aria-hidden="true" />
            <h3 className="mt-3 text-base font-black text-foreground">还没有模型配置</h3>
            <p className="mt-2 text-sm font-semibold leading-6 text-muted-foreground">添加 OpenAI Chat Completions 兼容配置并完成能力检测后即可开始运维对话。</p>
            <button type="button" onClick={onOpenSettings} className="soft-button mt-4 inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground focus:outline-none focus:ring-4 focus:ring-primary/20">
              <Settings2 size={16} aria-hidden="true" />
              配置模型
            </button>
          </div>
        ) : messages.length === 0 && toolCalls.length === 0 ? (
          <div className="m-auto max-w-md text-center">
            <MessageSquarePlus className="mx-auto h-9 w-9 text-primary" aria-hidden="true" />
            <h3 className="mt-3 text-base font-black text-foreground">开始一次运维对话</h3>
            <p className="mt-2 text-sm font-semibold leading-6 text-muted-foreground">可以询问当前故障、告警、离线节点、应用服务和拨测状态。任何状态变更都会先等待你的确认。</p>
          </div>
        ) : (
          <div className="space-y-3">
            {messages.map((message) => {
              const turn = assistant.turnResults[message.turn_id]
              return (
                <article key={message.id} className={`flex ${message.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                  <div className={`max-w-[88%] min-w-0 border px-3 py-2.5 text-sm font-semibold leading-6 ${message.role === 'user' ? 'border-primary/30 bg-primary text-primary-foreground' : 'border-border bg-surface text-foreground'}`}>
                    <p className="whitespace-pre-wrap break-words">{message.content}</p>
                    {message.role === 'assistant' && (message.provider_name || message.model) ? (
                      <p className="mt-1 truncate text-[11px] font-bold opacity-60">
                        {turn?.fallback_used ? '备用响应 · ' : ''}{[message.provider_name, message.model].filter(Boolean).join(' / ')}
                        {turn?.fallback_used && turn.requested_model ? `（原请求 ${turn.requested_provider_name} / ${turn.requested_model}）` : ''}
                      </p>
                    ) : null}
                  </div>
                </article>
              )
            })}

            {activeSending && assistant.streamedContent ? (
              <article className="flex justify-start" aria-live="polite">
                <div className="max-w-[88%] min-w-0 border border-border bg-surface px-3 py-2.5 text-sm font-semibold leading-6 text-foreground">
                  <p className="whitespace-pre-wrap break-words">{assistant.streamedContent}</p>
                </div>
              </article>
            ) : null}

            {toolCalls.filter((call) => call.status === 'pending').map((call) => (
              <div key={call.id} className={`border px-3 py-3 ${call.status === 'pending' ? 'border-warning/40 bg-warning/10' : 'border-border bg-surface/70'}`}>
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-sm font-black text-foreground">{toolLabels[call.tool_name] ?? call.tool_name}</p>
                    <p className="mt-1 break-words text-xs font-semibold text-muted-foreground">目标：{toolTarget(call)}</p>
                    {call.result_summary ? <p className="mt-1 break-words text-xs font-semibold text-muted-foreground">{call.result_summary}</p> : null}
                  </div>
                  <span className="shrink-0 text-xs font-black text-warning">等待确认</span>
                </div>
                <div className="mt-3 flex flex-wrap justify-end gap-2">
                  <button type="button" disabled={assistant.operationID === call.id} onClick={() => void assistant.reject(call)} className="soft-button min-h-9 border border-border bg-card px-3 text-xs font-black text-muted-foreground hover:text-foreground disabled:opacity-60">拒绝</button>
                  <button ref={confirmButtonRef} type="button" disabled={Boolean(assistant.operationID)} onClick={() => setConfirmCall(call)} className="soft-button min-h-9 bg-danger px-3 text-xs font-black text-white hover:brightness-95 disabled:opacity-60">检查并确认</button>
                </div>
              </div>
            ))}

            {activeSending ? (
              <div className="space-y-1.5 text-xs font-bold text-muted-foreground" role="status">
                {(assistant.timeline.length > 0 ? assistant.timeline : assistant.progress ? [assistant.progress] : []).map((event, index, events) => (
                  <div key={`${event.phase}-${event.tool_name ?? ''}-${event.target_name ?? ''}-${index}`} className={`flex min-w-0 items-center gap-2 ${index === events.length - 1 ? 'text-foreground' : 'opacity-60'}`}>
                    {index === events.length - 1 ? <LoaderCircle className="h-3.5 w-3.5 shrink-0 animate-spin" aria-hidden="true" /> : <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 rounded-full bg-current" />}
                    <span className="min-w-0 break-words">{progressText(event, toolLabels)}</span>
                  </div>
                ))}
                {assistant.timeline.length === 0 && !assistant.progress ? (
                  <div className="flex items-center gap-2"><LoaderCircle className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />模型正在处理请求</div>
                ) : null}
              </div>
            ) : null}
          </div>
        )}
      </div>

      <div className="shrink-0 border-t border-border bg-surface px-3 py-3">
        <label htmlFor={`ai-composer-${mode}`} className="sr-only">发送给 AI 运维助手</label>
        <textarea
          ref={composerRef}
          id={`ai-composer-${mode}`}
          rows={3}
          value={draft}
          maxLength={16 * 1024}
          disabled={!canSend}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={handleComposerKeyDown}
          placeholder={canSend ? '询问当前故障、告警或运维操作...' : '先选择已启用且通过检测的模型'}
          className="soft-input max-h-36 min-h-[76px] w-full resize-y px-3 py-2 text-sm font-semibold leading-6 placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-60"
        />
        <div className="mt-2 flex items-center justify-between gap-3">
          <p className="min-w-0 truncate text-[11px] font-bold text-muted-foreground">变更操作始终需要人工确认</p>
          {activeSending ? (
            <button type="button" onClick={assistant.stop} title="停止模型请求" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center border border-danger/40 bg-card text-danger focus:outline-none focus:ring-4 focus:ring-danger/20">
              <Square size={15} fill="currentColor" aria-hidden="true" />
              <span className="sr-only">停止模型请求</span>
            </button>
          ) : (
            <button type="button" onClick={submit} disabled={!draft.trim() || !canSend || assistant.sending || assistant.selectingModel} title="发送消息" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center bg-primary text-primary-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50">
              <Send size={16} aria-hidden="true" />
              <span className="sr-only">发送消息</span>
            </button>
          )}
        </div>
      </div>

      {confirmCall ? (
        <div className="soft-modal-overlay fixed inset-0 z-[80] flex items-center justify-center p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) closeConfirmation() }}>
          <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="ai-confirm-title" tabIndex={-1} className="soft-modal-shell w-full max-w-md text-left outline-none" onKeyDown={(event) => { if (event.key === 'Escape') closeConfirmation() }}>
            <div className="soft-modal-header flex items-start justify-between gap-3 border-b px-4 py-3">
              <div className="min-w-0">
                <p id="ai-confirm-title" className="text-base font-black text-foreground">确认 AI 运维操作</p>
                <p className="mt-1 text-xs font-semibold text-muted-foreground">该操作将改变目标运行状态，确认后立即执行且不会再次询问模型。</p>
              </div>
              <button type="button" onClick={closeConfirmation} title="关闭确认窗口" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center border border-border text-muted-foreground hover:text-foreground">
                <X size={16} aria-hidden="true" />
                <span className="sr-only">关闭确认窗口</span>
              </button>
            </div>
            <div className="space-y-3 px-4 py-4 text-sm">
              <div className="flex gap-3 border border-warning/30 bg-warning/10 px-3 py-3 text-warning">
                <CircleAlert className="mt-0.5 h-5 w-5 shrink-0" aria-hidden="true" />
                <div className="min-w-0">
                  <p className="font-black">{toolLabels[confirmCall.tool_name] ?? confirmCall.tool_name}</p>
                  <p className="mt-1 break-words font-semibold">目标：{toolTarget(confirmCall)}</p>
                  {confirmCall.node_id ? <p className="mt-1 break-all text-xs font-semibold opacity-80">节点：{confirmCall.node_id}</p> : null}
                  {confirmCall.result_summary ? <p className="mt-2 break-words text-xs font-semibold opacity-90">{confirmCall.result_summary}</p> : null}
                </div>
              </div>
              <p className="font-semibold leading-6 text-muted-foreground">执行前 Server 会重新检查目标是否存在、节点是否在线以及 Agent 能力是否仍满足要求。校验失败时不会执行。</p>
            </div>
            <div className="soft-modal-footer flex justify-end gap-2 border-t px-4 py-3">
              <button type="button" onClick={closeConfirmation} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground">取消</button>
              <button type="button" disabled={Boolean(assistant.operationID)} onClick={() => { const call = confirmCall; closeConfirmation(); void assistant.confirm(call) }} className="soft-button min-h-10 bg-danger px-4 text-sm font-black text-white hover:brightness-95 disabled:opacity-60">
                {assistant.operationID ? '执行中...' : '确认执行'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}

export function AIAssistantDrawer({ assistant, onOpenWorkspace, onOpenSettings }: { assistant: AIAssistantState, onOpenWorkspace: () => void, onOpenSettings: () => void }) {
  const [resizing, setResizing] = useState(false)

  useEffect(() => {
    if (!resizing) return
    const move = (event: globalThis.PointerEvent) => assistant.setDrawerWidth(window.innerWidth - event.clientX)
    const stop = () => setResizing(false)
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop, { once: true })
    return () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', stop)
    }
  }, [assistant, resizing])

  const handleResizeKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      assistant.setDrawerWidth(assistant.drawerWidth + 20)
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      assistant.setDrawerWidth(assistant.drawerWidth - 20)
    } else if (event.key === 'Home') {
      event.preventDefault()
      assistant.setDrawerWidth(420)
    } else if (event.key === 'End') {
      event.preventDefault()
      assistant.setDrawerWidth(720)
    }
  }

  if (!assistant.drawerOpen) return assistant.toast ? <Toast {...assistant.toast} onClose={assistant.clearToast} /> : null

  return (
    <>
      <aside aria-label="AI 运维助手" className="soft-drawer fixed inset-y-0 right-0 z-50 flex min-w-0 flex-col" style={{ width: `${assistant.drawerWidth}px`, maxWidth: 'calc(100vw - 24px)' }}>
        <div role="separator" aria-label="调整 AI 助手宽度" aria-orientation="vertical" aria-valuemin={420} aria-valuemax={720} aria-valuenow={assistant.drawerWidth} tabIndex={0} onPointerDown={(event: PointerEvent<HTMLDivElement>) => { event.preventDefault(); setResizing(true) }} onKeyDown={handleResizeKey} className={`absolute inset-y-0 left-0 z-10 w-2 -translate-x-1/2 cursor-col-resize focus:outline-none focus:ring-2 focus:ring-primary ${resizing ? 'bg-primary/20' : 'hover:bg-primary/10'}`} />
        <header className="flex shrink-0 items-center gap-2 border-b border-border bg-card px-3 py-3">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <Bot className="h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
            <div className="min-w-0">
              <p className="truncate text-sm font-black text-foreground">AI 运维助手</p>
              <p className="truncate text-[11px] font-semibold text-muted-foreground">{assistant.conversation?.conversation.title ?? '新会话'}</p>
            </div>
          </div>
          <button type="button" onClick={() => void assistant.newConversation()} title="新建会话" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center border border-border text-muted-foreground hover:text-foreground"><Plus size={16} aria-hidden="true" /><span className="sr-only">新建会话</span></button>
          <button type="button" onClick={onOpenWorkspace} title="打开完整工作台" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center border border-border text-muted-foreground hover:text-foreground"><Maximize2 size={16} aria-hidden="true" /><span className="sr-only">打开完整工作台</span></button>
          <button type="button" onClick={assistant.closeDrawer} title="关闭 AI 助手" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center border border-border text-muted-foreground hover:text-foreground"><PanelRightClose size={16} aria-hidden="true" /><span className="sr-only">关闭 AI 助手</span></button>
        </header>
        <div className="flex shrink-0 items-center gap-2 border-b border-border bg-surface px-3 py-2">
          <AIModelSelector assistant={assistant} idPrefix="ai-drawer" />
          <button type="button" onClick={onOpenSettings} title="模型设置" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground"><Settings2 size={16} aria-hidden="true" /><span className="sr-only">模型设置</span></button>
        </div>
        <ConversationPanel assistant={assistant} mode="drawer" onOpenSettings={onOpenSettings} />
      </aside>
      {assistant.toast ? <Toast {...assistant.toast} onClose={assistant.clearToast} /> : null}
    </>
  )
}

export function AIWorkspacePage({ assistant, onOpenSettings }: { assistant: AIAssistantState, onOpenSettings: () => void }) {
  const activeConversation = assistant.conversation?.conversation
  return (
    <section aria-label="AI 运维工作台" className="flex h-[calc(100vh-112px)] min-w-0 overflow-hidden border border-border bg-card shadow-sm">
      <aside className="flex w-[284px] shrink-0 flex-col border-r border-border bg-surface">
        <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-3">
          <div className="min-w-0">
            <p className="text-sm font-black text-foreground">AI 运维</p>
            <p className="text-[11px] font-semibold text-muted-foreground">本地持久化会话</p>
          </div>
          <button type="button" title="新建会话" onClick={() => void assistant.newConversation()} className="soft-button flex h-9 w-9 shrink-0 items-center justify-center bg-primary text-primary-foreground"><Plus size={16} aria-hidden="true" /><span className="sr-only">新建会话</span></button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {assistant.conversations.length === 0 ? (
            <p className="px-2 py-6 text-center text-xs font-semibold leading-5 text-muted-foreground">发送第一条消息后，会话会显示在这里。</p>
          ) : assistant.conversations.map((conversation) => (
            <div key={conversation.id} className={`mb-1 flex min-w-0 items-center gap-1 border px-2 py-2 ${assistant.activeConversationID === conversation.id ? 'border-primary/30 bg-primary/10' : 'border-transparent hover:border-border hover:bg-card'}`}>
              <button type="button" onClick={() => void assistant.selectConversation(conversation.id)} className="min-w-0 flex-1 text-left focus:outline-none">
                <span className="block truncate text-sm font-black text-foreground">{conversation.title}</span>
                <span className="mt-0.5 block truncate text-[11px] font-semibold text-muted-foreground">{new Date(conversation.updated_at).toLocaleString()}</span>
              </button>
              <button type="button" title="删除会话" onClick={() => void assistant.deleteConversation(conversation.id)} className="soft-button flex h-8 w-8 shrink-0 items-center justify-center text-muted-foreground hover:bg-danger/10 hover:text-danger"><Trash2 size={14} aria-hidden="true" /><span className="sr-only">删除会话</span></button>
            </div>
          ))}
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex shrink-0 items-center justify-between gap-3 border-b border-border bg-card px-4 py-3">
          <div className="min-w-0">
            <h2 className="truncate text-base font-black text-foreground">{activeConversation?.title ?? '新会话'}</h2>
            <p className="mt-0.5 truncate text-xs font-semibold text-muted-foreground">查询自动执行，变更需要确认</p>
          </div>
          <div className="flex min-w-0 items-center gap-2">
            <div className="w-[min(520px,48vw)] min-w-[360px]"><AIModelSelector assistant={assistant} idPrefix="ai-workspace" /></div>
            <button type="button" onClick={onOpenSettings} title="模型设置" className="soft-button flex h-9 w-9 shrink-0 items-center justify-center border border-border text-muted-foreground hover:text-foreground"><Settings2 size={16} aria-hidden="true" /><span className="sr-only">模型设置</span></button>
          </div>
        </header>
        <ConversationPanel assistant={assistant} mode="workspace" onOpenSettings={onOpenSettings} />
      </div>
    </section>
  )
}

export function AITopbarButton({ onClick, open }: { onClick: () => void, open: boolean }) {
  return (
    <button type="button" onClick={onClick} aria-pressed={open} title="AI 运维助手" className={`soft-button flex h-10 w-10 items-center justify-center border focus:outline-none focus:ring-4 focus:ring-primary/20 ${open ? 'border-primary/40 bg-primary text-primary-foreground' : 'border-border bg-card text-muted-foreground hover:text-foreground'}`}>
      <Bot size={18} aria-hidden="true" />
      <span className="sr-only">AI 运维助手</span>
    </button>
  )
}

export function AIWorkspaceLink({ onClick }: { onClick: () => void }) {
  return <button type="button" onClick={onClick} className="inline-flex items-center gap-1 text-xs font-black text-primary">打开 AI 工作台 <ChevronRight size={14} aria-hidden="true" /></button>
}
