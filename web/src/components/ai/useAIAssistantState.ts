import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { confirmAIPlan, confirmAIToolCall, createAIConversation, deleteAIConversation, getAIConversation, getAIConversations, getAIProviders, rejectAIPlan, rejectAIToolCall, sendAIMessageStream, updateAIConversationModel } from '../../api/client'
import type { AIConversation, AIConversationState, AIMessage, AIOperationPlan, AIProgress, AIProvider, AIProviderModel, AIRequestContext, AIToolCall, AITurn } from '../../types'

export const aiProvidersChangedEvent = 'mizupanel:ai-providers-changed'

export type AIToast = { type: 'success' | 'error', message: string }

export type AIAssistantState = {
  drawerOpen: boolean
  drawerWidth: number
  providers: AIProvider[]
  conversations: AIConversation[]
  activeConversationID?: string
  conversation?: AIConversationState
  selectedProviderID: string
  selectedModelID: string
  selectingModel: boolean
  loading: boolean
  sending: boolean
  sendingConversationID?: string
  operationID?: string
  error?: string
  toast?: AIToast
  progress?: AIProgress
  timeline: AIProgress[]
  streamedContent: string
  context: AIRequestContext
  turnResults: Record<string, AITurn>
  openDrawer: () => void
  closeDrawer: () => void
  setDrawerWidth: (width: number) => void
  setContext: (context: AIRequestContext) => void
  ensureLoaded: () => Promise<void>
  refreshProviders: () => Promise<AIProvider[]>
  selectProvider: (id: string) => void
  selectModel: (id: string) => Promise<void>
  selectConversation: (id: string) => Promise<void>
  newConversation: () => Promise<AIConversation | undefined>
  deleteConversation: (id: string) => Promise<void>
  send: (content: string) => Promise<void>
  stop: () => void
  confirm: (call: AIToolCall) => Promise<void>
  reject: (call: AIToolCall) => Promise<void>
  confirmPlan: (plan: AIOperationPlan) => Promise<void>
  rejectPlan: (plan: AIOperationPlan) => Promise<void>
  clearToast: () => void
}

const emptyConversationState = (conversation: AIConversation): AIConversationState => ({
  conversation,
  messages: [],
  tool_calls: [],
  plans: []
})

function errorText(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback
}

function confirmedOperationToast(call?: AIToolCall): AIToast {
  if (!call) return { type: 'error', message: 'AI 运维操作结果缺失' }
  switch (call.status) {
    case 'success':
      return { type: 'success', message: 'AI 运维操作执行成功' }
    case 'accepted':
      return { type: 'success', message: 'AI 运维操作已接受，正在处理中' }
    case 'failure':
      return {
        type: 'error',
        message: call.result_summary ? `AI 运维操作执行失败: ${call.result_summary}` : 'AI 运维操作执行失败'
      }
    case 'unsupported':
      return { type: 'error', message: 'AI 运维操作不受支持' }
    default:
      return {
        type: 'error',
        message: call.result_summary || `AI 运维操作状态异常: ${call.status}`
      }
  }
}

function confirmedPlanToast(plan?: AIOperationPlan): AIToast {
  if (!plan) return { type: 'error', message: 'AI 变更计划结果缺失' }
  if (plan.status === 'success') return { type: 'success', message: 'AI 变更计划执行成功' }
  if (plan.status === 'running') return { type: 'success', message: 'AI 变更计划正在执行' }
  if (plan.status === 'rejected') return { type: 'success', message: 'AI 变更计划拒绝成功' }
  return { type: 'error', message: 'AI 变更计划未完全成功，请检查步骤结果' }
}

export function isAIModelUsable(provider: AIProvider, model: AIProviderModel) {
  return provider.enabled && model.enabled && model.probe_status === 'success' && model.chat_capable && model.tools_capable
}

export function findAIModel(providers: AIProvider[], modelID: string) {
  for (const provider of providers) {
    const model = provider.models.find((item) => item.id === modelID)
    if (model) return { provider, model }
  }
  return undefined
}

function defaultModelID(providers: AIProvider[]) {
  for (const provider of providers) {
    const model = provider.models.find((item) => item.is_default)
    if (model) return model.id
  }
  return ''
}

function sameProgress(left: AIProgress | undefined, right: AIProgress) {
  return left?.phase === right.phase
    && left.tool_name === right.tool_name
    && left.target_name === right.target_name
    && left.provider_name === right.provider_name
    && left.model === right.model
}

export function useAIAssistantState(): AIAssistantState {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [drawerWidth, setDrawerWidthState] = useState(480)
  const [providers, setProviders] = useState<AIProvider[]>([])
  const providersRef = useRef<AIProvider[]>([])
  const [conversations, setConversations] = useState<AIConversation[]>([])
  const [activeConversationID, setActiveConversationID] = useState<string>()
  const [conversation, setConversation] = useState<AIConversationState>()
  const [selectedProviderID, setSelectedProviderID] = useState('')
  const [selectedModelID, setSelectedModelID] = useState('')
  const [selectingModel, setSelectingModel] = useState(false)
  const [loading, setLoading] = useState(false)
  const [sending, setSending] = useState(false)
  const [sendingConversationID, setSendingConversationID] = useState<string>()
  const [operationID, setOperationID] = useState<string>()
  const [error, setError] = useState<string>()
  const [toast, setToast] = useState<AIToast>()
  const [progress, setProgress] = useState<AIProgress>()
  const [timeline, setTimeline] = useState<AIProgress[]>([])
  const [streamedContent, setStreamedContent] = useState('')
  const [context, setContextState] = useState<AIRequestContext>({ page: 'overview' })
  const [turnResults, setTurnResults] = useState<Record<string, AITurn>>({})
  const conversationRequest = useRef(0)
  const conversationController = useRef<AbortController | undefined>(undefined)
  const conversationRefreshRequest = useRef(0)
  const conversationRefreshController = useRef<AbortController | undefined>(undefined)
  const modelSelectionRequest = useRef(0)
  const modelSelectionController = useRef<AbortController | undefined>(undefined)
  const activeModelRequest = useRef<AbortController | undefined>(undefined)
  const activeConversationRef = useRef<string | undefined>(undefined)
  const selectedProviderRef = useRef('')
  const selectedModelRef = useRef('')
  const pendingModelExplicit = useRef(false)
  const contextRef = useRef<AIRequestContext>({ page: 'overview' })
  const initialized = useRef(false)

  const setContext = useCallback((next: AIRequestContext) => {
    contextRef.current = next
    setContextState(next)
  }, [])

  const commitSelectedModel = useCallback((id: string, availableProviders = providersRef.current) => {
    selectedModelRef.current = id
    setSelectedModelID(id)
    const selected = findAIModel(availableProviders, id)
    const nextProviderID = selected?.provider.id
      ?? (availableProviders.some((provider) => provider.id === selectedProviderRef.current) ? selectedProviderRef.current : undefined)
      ?? availableProviders.find((provider) => provider.enabled)?.id
      ?? availableProviders[0]?.id
      ?? ''
    selectedProviderRef.current = nextProviderID
    setSelectedProviderID(nextProviderID)
  }, [])

  const cancelModelSelection = useCallback(() => {
    modelSelectionRequest.current += 1
    modelSelectionController.current?.abort()
    modelSelectionController.current = undefined
    setSelectingModel(false)
  }, [])

  const refreshProviders = useCallback(async () => {
    const response = await getAIProviders()
    providersRef.current = response.providers
    setProviders(response.providers)
    if (!activeConversationRef.current && !pendingModelExplicit.current) {
      commitSelectedModel(defaultModelID(response.providers), response.providers)
    } else {
      commitSelectedModel(selectedModelRef.current, response.providers)
    }
    return response.providers
  }, [commitSelectedModel])

  const refreshConversations = useCallback(async () => {
    const response = await getAIConversations(100)
    setConversations(response.conversations)
    return response.conversations
  }, [])

  const loadConversation = useCallback(async (id: string) => {
    conversationRefreshController.current?.abort()
    conversationRefreshRequest.current += 1
    conversationController.current?.abort()
    const controller = new AbortController()
    conversationController.current = controller
    const requestID = ++conversationRequest.current
    cancelModelSelection()
    activeConversationRef.current = id
    setActiveConversationID(id)
    setConversation(undefined)
    setLoading(true)
    setError(undefined)
    try {
      const response = await getAIConversation(id, 100, controller.signal)
      if (requestID !== conversationRequest.current || activeConversationRef.current !== id) return
      setConversation(response)
      setConversations((current) => current.map((item) => item.id === id ? response.conversation : item))
      pendingModelExplicit.current = false
      commitSelectedModel(response.conversation.model_id ?? '')
    } catch (loadError) {
      if (controller.signal.aborted || requestID !== conversationRequest.current) return
      setError(errorText(loadError, 'AI 会话加载失败'))
    } finally {
      if (requestID === conversationRequest.current) {
        if (conversationController.current === controller) conversationController.current = undefined
        setLoading(false)
      }
    }
  }, [cancelModelSelection, commitSelectedModel])

  const refreshConversation = useCallback(async (id: string) => {
    conversationRefreshController.current?.abort()
    const controller = new AbortController()
    conversationRefreshController.current = controller
    const requestID = ++conversationRefreshRequest.current
    const conversationRequestID = conversationRequest.current
    const modelSelectionRequestID = modelSelectionRequest.current

    try {
      const response = await getAIConversation(id, 100, controller.signal)
      if (
        controller.signal.aborted
        || requestID !== conversationRefreshRequest.current
        || conversationRequestID !== conversationRequest.current
        || activeConversationRef.current !== id
      ) return

      setConversation((current) => {
        if (current?.conversation.id !== id) return current
        const nextConversation = modelSelectionRequestID === modelSelectionRequest.current
          ? response.conversation
          : current.conversation
        return { ...response, conversation: nextConversation }
      })
      setConversations((current) => current.map((item) => {
        if (item.id !== id) return item
        return modelSelectionRequestID === modelSelectionRequest.current ? response.conversation : item
      }))
    } catch (refreshError) {
      if (controller.signal.aborted || requestID !== conversationRefreshRequest.current) return
      // Background operation polling must keep the current transcript usable when a refresh fails.
      void refreshError
    } finally {
      if (conversationRefreshController.current === controller) conversationRefreshController.current = undefined
    }
  }, [])

  const ensureLoaded = useCallback(async () => {
    if (initialized.current) return
    initialized.current = true
    setLoading(true)
    setError(undefined)
    try {
      const [, conversationRows] = await Promise.all([refreshProviders(), refreshConversations()])
      if (conversationRows.length > 0) await loadConversation(conversationRows[0].id)
    } catch (loadError) {
      initialized.current = false
      setError(errorText(loadError, 'AI 助手加载失败'))
    } finally {
      setLoading(false)
    }
  }, [loadConversation, refreshConversations, refreshProviders])

  const openDrawer = useCallback(() => {
    setDrawerOpen(true)
    void ensureLoaded()
  }, [ensureLoaded])

  const closeDrawer = useCallback(() => setDrawerOpen(false), [])

  const setDrawerWidth = useCallback((width: number) => {
    setDrawerWidthState(Math.max(420, Math.min(720, Math.round(width))))
  }, [])

  const selectProvider = useCallback((id: string) => {
    if (!providers.some((provider) => provider.id === id)) return
    selectedProviderRef.current = id
    setSelectedProviderID(id)
  }, [providers])

  const selectModel = useCallback(async (id: string) => {
    const selected = findAIModel(providers, id)
    if (!selected || !isAIModelUsable(selected.provider, selected.model)) {
      setToast({ type: 'error', message: '模型切换失败: 请选择已启用且通过 Chat/Tools 检测的模型' })
      return
    }
    const conversationID = activeConversationRef.current
    if (!conversationID) {
      pendingModelExplicit.current = true
      commitSelectedModel(id)
      return
    }
    if (id === selectedModelRef.current) return

    modelSelectionController.current?.abort()
    const controller = new AbortController()
    modelSelectionController.current = controller
    const requestID = ++modelSelectionRequest.current
    setSelectingModel(true)
    try {
      const updated = await updateAIConversationModel(conversationID, id, controller.signal)
      if (controller.signal.aborted || requestID !== modelSelectionRequest.current || activeConversationRef.current !== conversationID) return
      setConversation((current) => current?.conversation.id === conversationID ? { ...current, conversation: updated } : current)
      setConversations((current) => current.map((item) => item.id === conversationID ? updated : item))
      pendingModelExplicit.current = false
      commitSelectedModel(updated.model_id ?? '')
    } catch (selectionError) {
      if (!controller.signal.aborted && requestID === modelSelectionRequest.current) {
        setToast({ type: 'error', message: `模型切换失败: ${errorText(selectionError, '未知错误')}` })
      }
    } finally {
      if (requestID === modelSelectionRequest.current) {
        if (modelSelectionController.current === controller) modelSelectionController.current = undefined
        setSelectingModel(false)
      }
    }
  }, [commitSelectedModel, providers])

  const selectConversation = useCallback(async (id: string) => {
    if (id === activeConversationRef.current && conversation) return
    await loadConversation(id)
  }, [conversation, loadConversation])

  const newConversation = useCallback(async () => {
    conversationController.current?.abort()
    const requestID = ++conversationRequest.current
    cancelModelSelection()
    const explicitModelID = pendingModelExplicit.current ? selectedModelRef.current : undefined
    try {
      const created = await createAIConversation('', explicitModelID)
      setConversations((current) => [created, ...current.filter((item) => item.id !== created.id)])
      if (requestID !== conversationRequest.current) return undefined
      activeConversationRef.current = created.id
      setActiveConversationID(created.id)
      setConversation(emptyConversationState(created))
      pendingModelExplicit.current = false
      commitSelectedModel(created.model_id ?? '')
      setError(undefined)
      return created
    } catch (createError) {
      if (requestID === conversationRequest.current) {
        setToast({ type: 'error', message: `会话创建失败: ${errorText(createError, '未知错误')}` })
      }
      return undefined
    }
  }, [cancelModelSelection, commitSelectedModel])

  const deleteConversation = useCallback(async (id: string) => {
    try {
      await deleteAIConversation(id)
      const remaining = conversations.filter((item) => item.id !== id)
      setConversations(remaining)
      if (activeConversationRef.current === id) {
        conversationController.current?.abort()
        conversationRequest.current += 1
        cancelModelSelection()
        activeConversationRef.current = undefined
        setActiveConversationID(undefined)
        setConversation(undefined)
        commitSelectedModel(defaultModelID(providers))
        if (remaining[0]) await loadConversation(remaining[0].id)
      }
      setToast({ type: 'success', message: '会话删除成功' })
    } catch (deleteError) {
      setToast({ type: 'error', message: `会话删除失败: ${errorText(deleteError, '未知错误')}` })
    }
  }, [cancelModelSelection, commitSelectedModel, conversations, loadConversation, providers])

  const send = useCallback(async (content: string) => {
    const trimmed = content.trim()
    if (!trimmed || sending || selectingModel) return

    let conversationID = activeConversationRef.current
    let executionModelID = selectedModelRef.current
    if (!conversationID) {
      const created = await newConversation()
      conversationID = created?.id
      executionModelID = created?.model_id ?? ''
    }
    if (!conversationID) return

    const selected = findAIModel(providers, executionModelID)
    if (!selected || selected.provider.id !== selectedProviderRef.current || !isAIModelUsable(selected.provider, selected.model)) {
      setToast({ type: 'error', message: '消息发送失败: 请从当前供应商选择已启用且通过 Chat/Tools 检测的模型' })
      return
    }

    const optimisticMessage: AIMessage = {
      id: `local-${Date.now()}`,
      conversation_id: conversationID,
      turn_id: '',
      role: 'user',
      content: trimmed,
      provider_name: selected.provider.name,
      model: selected.model.model_id,
      created_at: new Date().toISOString()
    }
    setConversation((current) => current?.conversation.id === conversationID ? { ...current, messages: [...current.messages, optimisticMessage] } : current)

    const controller = new AbortController()
    activeModelRequest.current = controller
    setSending(true)
    setSendingConversationID(conversationID)
    setError(undefined)
    setProgress(undefined)
    setTimeline([])
    setStreamedContent('')
    try {
      const result = await sendAIMessageStream(conversationID, trimmed, controller.signal, (event) => {
        if (activeConversationRef.current !== conversationID) return
        setProgress(event)
        setTimeline((current) => sameProgress(current[current.length - 1], event) ? current : [...current, event].slice(-8))
      }, {
        context: contextRef.current,
        onDelta: (event) => {
          if (activeConversationRef.current === conversationID) {
            setStreamedContent((current) => `${current}${event.content}`.slice(0, 16 * 1024))
          }
        },
        onReset: () => {
          if (activeConversationRef.current === conversationID) setStreamedContent('')
        }
      })
      setStreamedContent('')
      setTurnResults((current) => ({ ...current, [result.turn.id]: result.turn }))
      if (result.message && activeConversationRef.current === conversationID) {
        setConversation((current) => {
          if (current?.conversation.id !== conversationID || current.messages.some((item) => item.id === result.message?.id)) return current
          return { ...current, messages: [...current.messages, result.message as AIMessage] }
        })
      }
      if (activeConversationRef.current === conversationID) await refreshConversation(conversationID)
      await refreshConversations()
    } catch (sendError) {
      if (controller.signal.aborted) {
        setToast({ type: 'success', message: '模型请求已停止' })
      } else {
        setToast({ type: 'error', message: `消息发送失败: ${errorText(sendError, '未知错误')}` })
      }
      if (activeConversationRef.current === conversationID) await refreshConversation(conversationID)
    } finally {
      if (activeModelRequest.current === controller) {
        activeModelRequest.current = undefined
        setSending(false)
        setSendingConversationID(undefined)
        setProgress(undefined)
        setTimeline([])
        setStreamedContent('')
      }
    }
  }, [newConversation, providers, refreshConversation, refreshConversations, selectingModel, sending])

  const stop = useCallback(() => activeModelRequest.current?.abort(), [])

  const runToolDecision = useCallback(async (call: AIToolCall, decision: 'confirm' | 'reject') => {
    if (operationID) return
    const conversationID = activeConversationRef.current
    setOperationID(call.id)
    try {
      const result = decision === 'confirm' ? await confirmAIToolCall(call.id) : await rejectAIToolCall(call.id)
      setTurnResults((current) => ({ ...current, [result.turn.id]: result.turn }))
      if (conversationID && activeConversationRef.current === conversationID) await refreshConversation(conversationID)
      setToast(decision === 'confirm'
        ? confirmedOperationToast(result.tool_call)
        : { type: 'success', message: 'AI 运维操作拒绝成功' })
    } catch (operationError) {
      setToast({ type: 'error', message: `AI 运维操作失败: ${errorText(operationError, '未知错误')}` })
      if (conversationID && activeConversationRef.current === conversationID) await refreshConversation(conversationID)
    } finally {
      setOperationID(undefined)
    }
  }, [operationID, refreshConversation])

  const confirm = useCallback((call: AIToolCall) => runToolDecision(call, 'confirm'), [runToolDecision])
  const reject = useCallback((call: AIToolCall) => runToolDecision(call, 'reject'), [runToolDecision])

  const runPlanDecision = useCallback(async (plan: AIOperationPlan, decision: 'confirm' | 'reject') => {
    if (operationID) return
    const conversationID = activeConversationRef.current
    setOperationID(plan.id)
    try {
      const result = decision === 'confirm' ? await confirmAIPlan(plan.id) : await rejectAIPlan(plan.id)
      setTurnResults((current) => ({ ...current, [result.turn.id]: result.turn }))
      if (conversationID && activeConversationRef.current === conversationID) await refreshConversation(conversationID)
      setToast(decision === 'confirm'
        ? confirmedPlanToast(result.plan)
        : { type: 'success', message: 'AI 变更计划拒绝成功' })
    } catch (operationError) {
      setToast({ type: 'error', message: `AI 变更计划操作失败: ${errorText(operationError, '未知错误')}` })
      if (conversationID && activeConversationRef.current === conversationID) await refreshConversation(conversationID)
    } finally {
      setOperationID(undefined)
    }
  }, [operationID, refreshConversation])

  const confirmPlan = useCallback((plan: AIOperationPlan) => runPlanDecision(plan, 'confirm'), [runPlanDecision])
  const rejectPlan = useCallback((plan: AIOperationPlan) => runPlanDecision(plan, 'reject'), [runPlanDecision])
  const clearToast = useCallback(() => setToast(undefined), [])

  useEffect(() => {
    const handleProvidersChanged = () => {
      void (async () => {
        try {
          await refreshProviders()
          const activeID = activeConversationRef.current
          if (activeID) await loadConversation(activeID)
        } catch (refreshError) {
          setToast({ type: 'error', message: `模型列表刷新失败: ${errorText(refreshError, '未知错误')}` })
        }
      })()
    }
    window.addEventListener(aiProvidersChangedEvent, handleProvidersChanged)
    return () => window.removeEventListener(aiProvidersChangedEvent, handleProvidersChanged)
  }, [loadConversation, refreshProviders])

  useEffect(() => {
    const conversationID = activeConversationRef.current
    const hasActiveOperation = conversation?.tool_calls.some((call) => call.status === 'accepted')
      || conversation?.plans?.some((plan) => plan.status === 'running')
    if (!conversationID || !hasActiveOperation) return
    const poll = window.setInterval(() => {
      if (document.visibilityState === 'hidden' || activeConversationRef.current !== conversationID) return
      void refreshConversation(conversationID)
    }, 2000)
    return () => window.clearInterval(poll)
  }, [conversation?.plans, conversation?.tool_calls, refreshConversation])

  useEffect(() => () => {
    conversationController.current?.abort()
    conversationRefreshController.current?.abort()
    modelSelectionController.current?.abort()
    activeModelRequest.current?.abort()
  }, [])

  return useMemo(() => ({
    drawerOpen,
    drawerWidth,
    providers,
    conversations,
    activeConversationID,
    conversation,
    selectedProviderID,
    selectedModelID,
    selectingModel,
    loading,
    sending,
    sendingConversationID,
    operationID,
    error,
    toast,
    progress,
    timeline,
    streamedContent,
    context,
    turnResults,
    openDrawer,
    closeDrawer,
    setDrawerWidth,
    setContext,
    ensureLoaded,
    refreshProviders,
    selectProvider,
    selectModel,
    selectConversation,
    newConversation,
    deleteConversation,
    send,
    stop,
    confirm,
    reject,
    confirmPlan,
    rejectPlan,
    clearToast
  }), [activeConversationID, clearToast, closeDrawer, confirm, confirmPlan, context, conversation, conversations, deleteConversation, drawerOpen, drawerWidth, ensureLoaded, error, loading, newConversation, openDrawer, operationID, progress, providers, refreshProviders, reject, rejectPlan, selectedModelID, selectedProviderID, selectConversation, selectModel, selectProvider, selectingModel, send, sending, sendingConversationID, setContext, setDrawerWidth, stop, streamedContent, timeline, toast, turnResults])
}
