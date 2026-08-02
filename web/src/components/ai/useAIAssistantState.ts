import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { confirmAIToolCall, createAIConversation, deleteAIConversation, getAIConversation, getAIConversations, getAIProviders, rejectAIToolCall, sendAIMessageStream } from '../../api/client'
import type { AIConversation, AIConversationState, AIMessage, AIProvider, AIToolCall } from '../../types'

export const aiProvidersChangedEvent = 'mizupanel:ai-providers-changed'

export type AIToast = { type: 'success' | 'error', message: string }

export type AIProgress = { phase: string; tool_name?: string; target_name?: string }

export type AIAssistantState = {
  drawerOpen: boolean
  drawerWidth: number
  providers: AIProvider[]
  conversations: AIConversation[]
  activeConversationID?: string
  conversation?: AIConversationState
  selectedProviderID: string
  loading: boolean
  sending: boolean
  operationID?: string
  error?: string
  toast?: AIToast
  progress?: AIProgress
  openDrawer: () => void
  closeDrawer: () => void
  setDrawerWidth: (width: number) => void
  ensureLoaded: () => Promise<void>
  refreshProviders: () => Promise<AIProvider[]>
  selectProvider: (id: string) => void
  selectConversation: (id: string) => Promise<void>
  newConversation: () => Promise<AIConversation | undefined>
  deleteConversation: (id: string) => Promise<void>
  send: (content: string) => Promise<void>
  stop: () => void
  confirm: (call: AIToolCall) => Promise<void>
  reject: (call: AIToolCall) => Promise<void>
  clearToast: () => void
}

const emptyConversationState = (conversation: AIConversation): AIConversationState => ({
  conversation,
  messages: [],
  tool_calls: []
})

function errorText(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback
}

function preferredProvider(providers: AIProvider[], current: string) {
  const usable = providers.filter((provider) => provider.chat_capable && provider.tools_capable)
  if (usable.some((provider) => provider.id === current)) return current
  return usable.find((provider) => provider.is_default)?.id ?? usable[0]?.id ?? providers[0]?.id ?? ''
}

export function useAIAssistantState(): AIAssistantState {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [drawerWidth, setDrawerWidthState] = useState(480)
  const [providers, setProviders] = useState<AIProvider[]>([])
  const [conversations, setConversations] = useState<AIConversation[]>([])
  const [activeConversationID, setActiveConversationID] = useState<string>()
  const [conversation, setConversation] = useState<AIConversationState>()
  const [selectedProviderID, setSelectedProviderID] = useState('')
  const [loading, setLoading] = useState(false)
  const [sending, setSending] = useState(false)
  const [operationID, setOperationID] = useState<string>()
  const [error, setError] = useState<string>()
  const [toast, setToast] = useState<AIToast>()
  const [progress, setProgress] = useState<AIProgress>()
  const conversationRequest = useRef(0)
  const activeModelRequest = useRef<AbortController | undefined>(undefined)
  const initialized = useRef(false)

  const refreshProviders = useCallback(async () => {
    const response = await getAIProviders()
    setProviders(response.providers)
    setSelectedProviderID((current) => preferredProvider(response.providers, current))
    return response.providers
  }, [])

  const loadConversation = useCallback(async (id: string) => {
    const requestID = ++conversationRequest.current
    setLoading(true)
    setError(undefined)
    try {
      const response = await getAIConversation(id, 100)
      if (requestID !== conversationRequest.current) return
      setConversation(response)
      setActiveConversationID(id)
    } catch (loadError) {
      if (requestID !== conversationRequest.current) return
      setError(errorText(loadError, 'AI 会话加载失败'))
    } finally {
      if (requestID === conversationRequest.current) setLoading(false)
    }
  }, [])

  const refreshConversations = useCallback(async () => {
    const response = await getAIConversations(100)
    setConversations(response.conversations)
    return response.conversations
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

  const selectConversation = useCallback(async (id: string) => {
    if (id === activeConversationID && conversation) return
    await loadConversation(id)
  }, [activeConversationID, conversation, loadConversation])

  const newConversation = useCallback(async () => {
    try {
      const created = await createAIConversation()
      setConversations((current) => [created, ...current.filter((item) => item.id !== created.id)])
      setActiveConversationID(created.id)
      setConversation(emptyConversationState(created))
      setError(undefined)
      return created
    } catch (createError) {
      setToast({ type: 'error', message: `会话创建失败: ${errorText(createError, '未知错误')}` })
      return undefined
    }
  }, [])

  const deleteConversation = useCallback(async (id: string) => {
    try {
      await deleteAIConversation(id)
      const remaining = conversations.filter((item) => item.id !== id)
      setConversations(remaining)
      if (activeConversationID === id) {
        conversationRequest.current += 1
        setActiveConversationID(remaining[0]?.id)
        setConversation(undefined)
        if (remaining[0]) await loadConversation(remaining[0].id)
      }
      setToast({ type: 'success', message: '会话删除成功' })
    } catch (deleteError) {
      setToast({ type: 'error', message: `会话删除失败: ${errorText(deleteError, '未知错误')}` })
    }
  }, [activeConversationID, conversations, loadConversation])

  const send = useCallback(async (content: string) => {
    const trimmed = content.trim()
    if (!trimmed || sending) return
    const provider = providers.find((item) => item.id === selectedProviderID)
    if (!provider || !provider.chat_capable || !provider.tools_capable) {
      setToast({ type: 'error', message: '消息发送失败: 请先选择已通过聊天和工具检测的模型' })
      return
    }
    let conversationID = activeConversationID
    if (!conversationID) {
      const created = await newConversation()
      conversationID = created?.id
    }
    if (!conversationID) return

    const optimisticMessage: AIMessage = {
      id: `local-${Date.now()}`,
      conversation_id: conversationID,
      turn_id: '',
      role: 'user',
      content: trimmed,
      provider_name: provider.name,
      model: provider.model,
      created_at: new Date().toISOString()
    }
    setConversation((prev) => prev ? { ...prev, messages: [...prev.messages, optimisticMessage] } : prev)

    const controller = new AbortController()
    activeModelRequest.current = controller
    setSending(true)
    setError(undefined)
    setProgress(undefined)
    try {
      await sendAIMessageStream(conversationID, provider.id, trimmed, controller.signal, setProgress)
      await Promise.all([loadConversation(conversationID), refreshConversations()])
    } catch (sendError) {
      if (controller.signal.aborted) {
        setToast({ type: 'success', message: '模型请求已停止' })
      } else {
        setToast({ type: 'error', message: `消息发送失败: ${errorText(sendError, '未知错误')}` })
      }
      await loadConversation(conversationID)
    } finally {
      setProgress(undefined)
      if (activeModelRequest.current === controller) {
        activeModelRequest.current = undefined
        setSending(false)
      }
    }
  }, [activeConversationID, loadConversation, newConversation, providers, refreshConversations, selectedProviderID, sending])

  const stop = useCallback(() => activeModelRequest.current?.abort(), [])

  const runToolDecision = useCallback(async (call: AIToolCall, decision: 'confirm' | 'reject') => {
    if (operationID) return
    setOperationID(call.id)
    try {
      if (decision === 'confirm') await confirmAIToolCall(call.id)
      else await rejectAIToolCall(call.id)
      if (activeConversationID) await loadConversation(activeConversationID)
      setToast({ type: 'success', message: decision === 'confirm' ? 'AI 运维操作执行完成' : 'AI 运维操作已拒绝' })
    } catch (operationError) {
      setToast({ type: 'error', message: `AI 运维操作失败: ${errorText(operationError, '未知错误')}` })
      if (activeConversationID) await loadConversation(activeConversationID)
    } finally {
      setOperationID(undefined)
    }
  }, [activeConversationID, loadConversation, operationID])

  const confirm = useCallback((call: AIToolCall) => runToolDecision(call, 'confirm'), [runToolDecision])
  const reject = useCallback((call: AIToolCall) => runToolDecision(call, 'reject'), [runToolDecision])
  const clearToast = useCallback(() => setToast(undefined), [])

  useEffect(() => {
    const handleProvidersChanged = () => {
      void refreshProviders().catch((refreshError) => {
        setToast({ type: 'error', message: `模型列表刷新失败: ${errorText(refreshError, '未知错误')}` })
      })
    }
    window.addEventListener(aiProvidersChangedEvent, handleProvidersChanged)
    return () => window.removeEventListener(aiProvidersChangedEvent, handleProvidersChanged)
  }, [refreshProviders])

  useEffect(() => () => activeModelRequest.current?.abort(), [])

  return useMemo(() => ({
    drawerOpen,
    drawerWidth,
    providers,
    conversations,
    activeConversationID,
    conversation,
    selectedProviderID,
    loading,
    sending,
    operationID,
    error,
    toast,
    progress,
    openDrawer,
    closeDrawer,
    setDrawerWidth,
    ensureLoaded,
    refreshProviders,
    selectProvider: setSelectedProviderID,
    selectConversation,
    newConversation,
    deleteConversation,
    send,
    stop,
    confirm,
    reject,
    clearToast
  }), [activeConversationID, clearToast, closeDrawer, confirm, conversation, conversations, deleteConversation, drawerOpen, drawerWidth, ensureLoaded, error, loading, newConversation, openDrawer, operationID, progress, providers, refreshProviders, reject, selectedProviderID, selectConversation, send, sending, setDrawerWidth, stop, toast])
}
