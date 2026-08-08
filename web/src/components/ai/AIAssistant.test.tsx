import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import * as api from '../../api/client'
import type { AIConversation, AIConversationState, AIMessage, AIOperationPlan, AIProvider, AIProviderModel, AISendResult, AIToolCall, AITurn } from '../../types'
import { AIAssistantDrawer, AIWorkspacePage } from './AIAssistant'
import { type AIAssistantState, useAIAssistantState } from './useAIAssistantState'

vi.mock('../../api/client', () => ({
  confirmAIPlan: vi.fn(),
  confirmAIToolCall: vi.fn(),
  createAIConversation: vi.fn(),
  deleteAIConversation: vi.fn(),
  getAIConversation: vi.fn(),
  getAIConversations: vi.fn(),
  getAIProviders: vi.fn(),
  rejectAIPlan: vi.fn(),
  rejectAIToolCall: vi.fn(),
  sendAIMessageStream: vi.fn(),
  updateAIConversationModel: vi.fn()
}))

function model(providerID: string, id: string, overrides: Partial<AIProviderModel> = {}): AIProviderModel {
  return {
    id,
    provider_id: providerID,
    model_id: `${id}-upstream`,
    display_name: '',
    enabled: true,
    chat_capable: true,
    tools_capable: true,
    probe_status: 'success',
    probe_latency_ms: 25,
    probed_at: '2026-08-01T00:00:00Z',
    probe_error: '',
    is_default: false,
    is_fallback: false,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides
  }
}

function provider(id: string, name: string, models?: AIProviderModel[]): AIProvider {
  const children = models ?? [model(id, `model-${id}`)]
  const compatibility = children[0]
  return {
    id,
    name,
    protocol: 'openai_chat_completions',
    base_url: 'http://model.internal/v1',
    enabled: true,
    discovery_status: 'success',
    discovery_latency_ms: 18,
    discovered_at: '2026-08-01T00:00:00Z',
    discovery_error: '',
    models: children,
    model: compatibility?.model_id ?? '',
    has_api_key: true,
    is_default: Boolean(compatibility?.is_default),
    chat_capable: Boolean(compatibility?.chat_capable),
    tools_capable: Boolean(compatibility?.tools_capable),
    probe_status: compatibility?.probe_status ?? 'unknown',
    probed_at: compatibility?.probed_at ?? null,
    probe_error: compatibility?.probe_error ?? '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z'
  }
}

const primaryModel = model('provider-1', 'model-1', { is_default: true })
const secondaryModel = model('provider-1', 'model-2')
const fallbackModel = model('provider-2', 'model-3', { is_fallback: true })
const providers = [provider('provider-1', 'Primary', [primaryModel, secondaryModel]), provider('provider-2', 'Secondary', [fallbackModel])]

const conversation: AIConversation = {
  id: 'conversation-1',
  title: 'Current incidents',
  model_id: primaryModel.id,
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z'
}

const pendingCall: AIToolCall = {
  id: 'tool-1',
  turn_id: 'turn-1',
  tool_name: 'systemd_service_action',
  risk: 'confirm',
  status: 'pending',
  target_type: 'systemd_service',
  target_id: 'nginx.service',
  target_name: 'nginx.service',
  node_id: 'node-1',
  result_summary: '',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z'
}

function turn(overrides: Partial<AITurn> = {}): AITurn {
  return {
    id: 'turn-1',
    conversation_id: conversation.id,
    model_id: primaryModel.id,
    provider_id: 'provider-1',
    provider_name: 'Primary',
    protocol: 'openai_chat_completions',
    model: primaryModel.model_id,
    requested_provider_id: 'provider-1',
    requested_provider_name: 'Primary',
    requested_model_id: primaryModel.id,
    requested_model: primaryModel.model_id,
    fallback_used: false,
    status: 'completed',
    error_code: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides
  }
}

function assistant(overrides: Partial<AIAssistantState> = {}): AIAssistantState {
  return {
    drawerOpen: true,
    drawerWidth: 480,
    providers,
    conversations: [conversation],
    activeConversationID: conversation.id,
    conversation: { conversation, messages: [], tool_calls: [] },
    selectedProviderID: 'provider-1',
    selectedModelID: primaryModel.id,
    selectingModel: false,
    loading: false,
    sending: false,
    sendingConversationID: undefined,
    progress: undefined,
    timeline: [],
    streamedContent: '',
    context: { page: 'overview' },
    turnResults: {},
    openDrawer: vi.fn(),
    closeDrawer: vi.fn(),
    setDrawerWidth: vi.fn(),
    setContext: vi.fn(),
    ensureLoaded: vi.fn(),
    refreshProviders: vi.fn(),
    selectProvider: vi.fn(),
    selectModel: vi.fn(),
    selectConversation: vi.fn(),
    newConversation: vi.fn(),
    deleteConversation: vi.fn(),
    send: vi.fn(),
    stop: vi.fn(),
    confirm: vi.fn(),
    reject: vi.fn(),
    confirmPlan: vi.fn(),
    rejectPlan: vi.fn(),
    clearToast: vi.fn(),
    ...overrides
  }
}

function SharedStateHarness() {
  const state = useAIAssistantState()
  return (
    <>
      <button type="button" onClick={state.openDrawer}>Open assistant</button>
      <button type="button" onClick={() => state.setContext({ page: 'hosts', resource_type: 'node', resource_id: 'node-1' })}>Set node context</button>
      <output data-testid="selected-provider">{state.selectedProviderID}</output>
      <output data-testid="selected-model">{state.selectedModelID}</output>
      <AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />
      <AIWorkspacePage assistant={state} onOpenSettings={vi.fn()} />
    </>
  )
}

function ToolDecisionHarness({ call }: { call: AIToolCall }) {
  const state = useAIAssistantState()
  return (
    <>
      <button type="button" onClick={state.openDrawer}>Open assistant</button>
      <button type="button" onClick={() => void state.confirm(call)}>Confirm tool</button>
      {state.toast ? <output data-testid="tool-decision-toast">{state.toast.type}:{state.toast.message}</output> : null}
      <AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />
    </>
  )
}

function PlanDecisionHarness({ plan }: { plan: AIOperationPlan }) {
  const state = useAIAssistantState()
  return (
    <>
      <button type="button" onClick={state.openDrawer}>Open assistant</button>
      <button type="button" onClick={() => void state.confirmPlan(plan)}>Confirm plan</button>
      {state.toast ? <output data-testid="plan-decision-toast">{state.toast.type}:{state.toast.message}</output> : null}
      <AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />
    </>
  )
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('AI assistant', () => {
  beforeEach(() => {
    vi.mocked(api.getAIProviders).mockResolvedValue({ providers })
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [] })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  test('shares two-stage Provider and model selection between drawer and workspace', async () => {
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))

    const drawerProvider = await screen.findByLabelText('选择供应商', { selector: '#ai-drawer-provider' })
    const drawerModel = screen.getByLabelText('选择模型', { selector: '#ai-drawer-model' })
    const workspaceProvider = screen.getByLabelText('选择供应商', { selector: '#ai-workspace-provider' })
    const workspaceModel = screen.getByLabelText('选择模型', { selector: '#ai-workspace-model' })
    expect(drawerProvider).toHaveValue('provider-1')
    expect(workspaceProvider).toHaveValue('provider-1')
    expect(drawerModel).toHaveValue(primaryModel.id)
    expect(workspaceModel).toHaveValue(primaryModel.id)
    expect(within(drawerModel).getByRole('option', { name: /model-2-upstream/ })).toBeInTheDocument()
    expect(within(drawerModel).queryByRole('option', { name: /model-3-upstream/ })).not.toBeInTheDocument()

    fireEvent.change(drawerProvider, { target: { value: 'provider-2' } })
    await waitFor(() => expect(workspaceProvider).toHaveValue('provider-2'))
    expect(drawerModel).toHaveValue('')
    expect(screen.getByLabelText('发送给 AI 运维助手', { selector: '#ai-composer-drawer' })).toBeDisabled()
    expect(within(drawerModel).getByRole('option', { name: /model-3-upstream/ })).toBeInTheDocument()
    expect(api.updateAIConversationModel).not.toHaveBeenCalled()

    fireEvent.change(drawerModel, { target: { value: fallbackModel.id } })
    await waitFor(() => expect(workspaceModel).toHaveValue(fallbackModel.id))
    expect(screen.getByLabelText('发送给 AI 运维助手', { selector: '#ai-composer-drawer' })).not.toBeDisabled()
    expect(api.updateAIConversationModel).not.toHaveBeenCalled()
  })

  test('persists selection for an active conversation and rolls back on failure', async () => {
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation).mockResolvedValue({ conversation, messages: [], tool_calls: [] })
    vi.mocked(api.updateAIConversationModel).mockRejectedValue(new Error('模型不可用'))
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))

    const selector = await screen.findByLabelText('选择模型', { selector: '#ai-drawer-model' })
    await waitFor(() => expect(selector).toHaveValue(primaryModel.id))
    fireEvent.change(selector, { target: { value: secondaryModel.id } })

    await waitFor(() => expect(api.updateAIConversationModel).toHaveBeenCalledWith(conversation.id, secondaryModel.id, expect.any(AbortSignal)))
    expect(selector).toHaveValue(primaryModel.id)
    expect(await screen.findByText('模型切换失败: 模型不可用')).toBeInTheDocument()
  })

  test('ignores stale conversation responses and restores each persisted model', async () => {
    const secondConversation = { ...conversation, id: 'conversation-2', title: 'Second', model_id: fallbackModel.id }
    const firstLoad = deferred<{ conversation: AIConversation; messages: []; tool_calls: [] }>()
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation, secondConversation] })
    vi.mocked(api.getAIConversation).mockImplementation((id) => id === conversation.id
      ? firstLoad.promise
      : Promise.resolve({ conversation: secondConversation, messages: [], tool_calls: [] }))
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))

    fireEvent.click(await screen.findByRole('button', { name: /Second/ }))
    await waitFor(() => expect(screen.getByTestId('selected-model')).toHaveTextContent(fallbackModel.id))
    expect(screen.getByTestId('selected-provider')).toHaveTextContent('provider-2')
    firstLoad.resolve({ conversation, messages: [], tool_calls: [] })
    await Promise.resolve()
    expect(screen.getByTestId('selected-model')).toHaveTextContent(fallbackModel.id)
    expect(screen.getByTestId('selected-provider')).toHaveTextContent('provider-2')
  })

  test('ignores stale model PATCH responses', async () => {
    const firstPatch = deferred<AIConversation>()
    const secondPatch = deferred<AIConversation>()
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation).mockResolvedValue({ conversation, messages: [], tool_calls: [] })
    vi.mocked(api.updateAIConversationModel)
      .mockImplementationOnce(() => firstPatch.promise)
      .mockImplementationOnce(() => secondPatch.promise)
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))

    const selector = await screen.findByLabelText('选择模型', { selector: '#ai-drawer-model' })
    const providerSelector = screen.getByLabelText('选择供应商', { selector: '#ai-drawer-provider' })
    await waitFor(() => expect(selector).toHaveValue(primaryModel.id))
    fireEvent.change(selector, { target: { value: secondaryModel.id } })
    fireEvent.change(providerSelector, { target: { value: 'provider-2' } })
    fireEvent.change(selector, { target: { value: fallbackModel.id } })
    secondPatch.resolve({ ...conversation, model_id: fallbackModel.id })
    await waitFor(() => expect(selector).toHaveValue(fallbackModel.id))
    firstPatch.resolve({ ...conversation, model_id: secondaryModel.id })
    await Promise.resolve()
    expect(selector).toHaveValue(fallbackModel.id)
  })

  test('refreshes accepted operations without clearing the transcript or aborting model selection', async () => {
    const pollCallbacks: Array<() => void> = []
    vi.spyOn(window, 'setInterval').mockImplementation((callback) => {
      if (typeof callback === 'function') pollCallbacks.push(callback as () => void)
      return 1
    })
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    const selection = deferred<AIConversation>()
    const priorMessage: AIMessage = {
      id: 'message-existing',
      conversation_id: conversation.id,
      turn_id: 'turn-existing',
      role: 'assistant',
      content: '正在等待节点重启结果。',
      provider_name: 'Primary',
      model: primaryModel.model_id,
      created_at: '2026-08-05T00:00:00Z'
    }
    const accepted = { ...pendingCall, status: 'accepted' as const, result_summary: '重启请求已提交' }
    const completed = { ...accepted, status: 'success' as const, result_summary: '节点已重启' }
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation)
      .mockResolvedValueOnce({ conversation, messages: [priorMessage], tool_calls: [accepted] })
      .mockResolvedValueOnce({ conversation, messages: [priorMessage], tool_calls: [completed] })
    vi.mocked(api.updateAIConversationModel).mockReturnValue(selection.promise)

    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    const selector = await screen.findByLabelText('选择模型', { selector: '#ai-drawer-model' })
    const drawer = screen.getByTestId('ai-drawer-messages')
    await waitFor(() => expect(api.getAIConversation).toHaveBeenCalledTimes(1))
    expect(within(drawer).getByText('正在等待节点重启结果。')).toBeInTheDocument()

    fireEvent.change(selector, { target: { value: secondaryModel.id } })
    await waitFor(() => expect(api.updateAIConversationModel).toHaveBeenCalledWith(conversation.id, secondaryModel.id, expect.any(AbortSignal)))
    const selectionSignal = vi.mocked(api.updateAIConversationModel).mock.calls[0]?.[2]
    expect(selectionSignal?.aborted).toBe(false)

    const poll = pollCallbacks.find((callback) => String(callback).includes('refreshConversation'))
    expect(poll).toBeDefined()
    poll?.()
    await waitFor(() => expect(api.getAIConversation).toHaveBeenCalledTimes(2))
    expect(within(drawer).getByText('正在等待节点重启结果。')).toBeInTheDocument()
    expect(selectionSignal?.aborted).toBe(false)

    selection.resolve({ ...conversation, model_id: secondaryModel.id })
    await waitFor(() => expect(selector).toHaveValue(secondaryModel.id))
  })

  test('confirms a plan through non-destructive conversation refresh', async () => {
    const priorMessage: AIMessage = {
      id: 'message-plan', conversation_id: conversation.id, turn_id: 'turn-plan', role: 'assistant',
      content: '计划准备完成。', provider_name: 'Primary', model: primaryModel.model_id, created_at: '2026-08-05T00:00:00Z'
    }
    const step = { ...pendingCall, turn_id: 'turn-plan', step_index: 0, result_summary: '重启节点' }
    const plan: AIOperationPlan = { id: 'turn-plan', turn_id: 'turn-plan', status: 'pending', current_step: -1, steps: [step] }
    const completedPlan: AIOperationPlan = { ...plan, status: 'success', steps: [{ ...step, status: 'success', result_summary: '节点已重启' }] }
    const refresh = deferred<AIConversationState>()
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation)
      .mockResolvedValueOnce({ conversation, messages: [priorMessage], tool_calls: [step], plans: [plan] })
      .mockReturnValueOnce(refresh.promise)
    vi.mocked(api.confirmAIPlan).mockResolvedValue({ turn: turn(), plan: completedPlan })

    render(<PlanDecisionHarness plan={plan} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    expect(await screen.findByText('计划准备完成。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm plan' }))
    await waitFor(() => expect(api.confirmAIPlan).toHaveBeenCalledWith(plan.id))
    expect(screen.getByText('计划准备完成。')).toBeInTheDocument()

    refresh.resolve({ conversation, messages: [priorMessage], tool_calls: completedPlan.steps, plans: [completedPlan] })
    await waitFor(() => expect(screen.getByTestId('plan-decision-toast')).toHaveTextContent('success:AI 变更计划执行成功'))
    expect(screen.getByText('计划准备完成。')).toBeInTheDocument()
  })

  test('confirms a historical tool call without unmounting the transcript', async () => {
    const priorMessage: AIMessage = {
      id: 'message-tool', conversation_id: conversation.id, turn_id: 'turn-tool', role: 'assistant',
      content: '历史单操作等待确认。', provider_name: 'Primary', model: primaryModel.model_id, created_at: '2026-08-05T00:00:00Z'
    }
    const completedCall = { ...pendingCall, status: 'success' as const, result_summary: '节点已重启' }
    const refresh = deferred<AIConversationState>()
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation)
      .mockResolvedValueOnce({ conversation, messages: [priorMessage], tool_calls: [pendingCall] })
      .mockReturnValueOnce(refresh.promise)
    vi.mocked(api.confirmAIToolCall).mockResolvedValue({ turn: turn(), tool_call: completedCall })

    render(<ToolDecisionHarness call={pendingCall} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    expect(await screen.findByText('历史单操作等待确认。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm tool' }))
    await waitFor(() => expect(api.confirmAIToolCall).toHaveBeenCalledWith(pendingCall.id))
    expect(screen.getByText('历史单操作等待确认。')).toBeInTheDocument()

    refresh.resolve({ conversation, messages: [priorMessage], tool_calls: [completedCall] })
    await waitFor(() => expect(screen.getByTestId('tool-decision-toast')).toHaveTextContent('success:AI 运维操作执行成功'))
    expect(screen.getByText('历史单操作等待确认。')).toBeInTheDocument()
  })

  test('new conversations inherit the server default unless a model was explicitly selected', async () => {
    vi.mocked(api.createAIConversation)
      .mockResolvedValueOnce(conversation)
      .mockResolvedValueOnce({ ...conversation, id: 'conversation-2', model_id: fallbackModel.id })
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))

    await screen.findByLabelText('选择模型', { selector: '#ai-drawer-model' })
    fireEvent.click(screen.getAllByRole('button', { name: '新建会话' })[0])
    await waitFor(() => expect(api.createAIConversation).toHaveBeenNthCalledWith(1, '', undefined))

    vi.mocked(api.deleteAIConversation).mockResolvedValue()
    fireEvent.click(screen.getAllByRole('button', { name: '删除会话' })[0])
    await waitFor(() => expect(api.deleteAIConversation).toHaveBeenCalledWith(conversation.id))
    const providerSelector = screen.getByLabelText('选择供应商', { selector: '#ai-drawer-provider' })
    const selector = screen.getByLabelText('选择模型', { selector: '#ai-drawer-model' })
    fireEvent.change(providerSelector, { target: { value: 'provider-2' } })
    fireEvent.change(selector, { target: { value: fallbackModel.id } })
    await waitFor(() => expect(selector).toHaveValue(fallbackModel.id))
    fireEvent.click(screen.getAllByRole('button', { name: '新建会话' })[0])
    await waitFor(() => expect(api.createAIConversation).toHaveBeenNthCalledWith(2, '', fallbackModel.id))
  })

  test('blocks a disabled or deleted conversation model until an explicit replacement is selected', () => {
    const disabled = model('provider-1', 'disabled-model', { enabled: false })
    const disabledProvider = provider('provider-1', 'Primary', [disabled, secondaryModel])
    const disabledConversation = { ...conversation, model_id: disabled.id }
    const state = assistant({
      providers: [disabledProvider],
      conversation: { conversation: disabledConversation, messages: [], tool_calls: [] },
      selectedModelID: disabled.id
    })
    const { rerender } = render(<AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />)
    expect(screen.getByLabelText('发送给 AI 运维助手')).toBeDisabled()
    expect(screen.getByLabelText('选择模型')).toHaveValue(disabled.id)

    rerender(<AIAssistantDrawer assistant={{ ...state, conversation: { conversation: { ...disabledConversation, model_id: null }, messages: [], tool_calls: [] }, selectedModelID: '' }} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />)
    expect(screen.getByLabelText('发送给 AI 运维助手')).toBeDisabled()
    expect(screen.getByLabelText('选择模型')).toHaveValue('')
  })

  test('supports bounded keyboard resizing and a Provider empty state', () => {
    const state = assistant({ providers: [], selectedModelID: '' })
    const onOpenSettings = vi.fn()
    render(<AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={onOpenSettings} />)

    const separator = screen.getByRole('separator', { name: '调整 AI 助手宽度' })
    fireEvent.keyDown(separator, { key: 'ArrowLeft' })
    fireEvent.keyDown(separator, { key: 'ArrowRight' })
    fireEvent.keyDown(separator, { key: 'Home' })
    fireEvent.keyDown(separator, { key: 'End' })
    expect(state.setDrawerWidth).toHaveBeenNthCalledWith(1, 500)
    expect(state.setDrawerWidth).toHaveBeenNthCalledWith(2, 460)
    expect(state.setDrawerWidth).toHaveBeenNthCalledWith(3, 420)
    expect(state.setDrawerWidth).toHaveBeenNthCalledWith(4, 720)
    fireEvent.click(screen.getByRole('button', { name: '配置模型' }))
    expect(onOpenSettings).toHaveBeenCalledTimes(1)
  })

  test('follows new output after send without pulling a manually scrolled transcript down', () => {
    const send = vi.fn(async () => undefined)
    const priorMessage: AIMessage = {
      id: 'message-prior',
      conversation_id: conversation.id,
      turn_id: 'turn-prior',
      role: 'assistant',
      content: 'Earlier answer',
      provider_name: 'Primary',
      model: primaryModel.model_id,
      created_at: '2026-08-05T00:00:00Z'
    }
    const base = assistant({
      conversation: { conversation, messages: [priorMessage], tool_calls: [] },
      send
    })
    const { rerender } = render(<AIAssistantDrawer assistant={base} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />)
    const viewport = screen.getByTestId('ai-drawer-messages')
    let scrollHeight = 1000
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, get: () => scrollHeight })
    Object.defineProperty(viewport, 'clientHeight', { configurable: true, value: 300 })

    viewport.scrollTop = 100
    fireEvent.scroll(viewport)
    const composer = screen.getByLabelText('发送给 AI 运维助手')
    fireEvent.change(composer, { target: { value: 'new question' } })
    fireEvent.keyDown(composer, { key: 'Enter' })
    expect(send).toHaveBeenCalledWith('new question')

    const optimisticMessage = { ...priorMessage, id: 'message-local', role: 'user' as const, content: 'new question' }
    scrollHeight = 1200
    rerender(<AIAssistantDrawer assistant={{ ...base, conversation: { conversation, messages: [priorMessage, optimisticMessage], tool_calls: [] }, sending: true, sendingConversationID: conversation.id }} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />)
    expect(viewport.scrollTop).toBe(1200)

    viewport.scrollTop = 200
    fireEvent.scroll(viewport)
    scrollHeight = 1400
    rerender(<AIAssistantDrawer assistant={{ ...base, conversation: { conversation, messages: [priorMessage, optimisticMessage], tool_calls: [] }, sending: true, sendingConversationID: conversation.id, streamedContent: 'partial answer' }} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />)
    expect(viewport.scrollTop).toBe(200)
  })

  test('renders only pending write-risk cards and uses the custom confirmation dialog', async () => {
    const confirm = vi.fn(async () => undefined)
    const completedRead = { ...pendingCall, id: 'read-1', risk: 'read' as const, status: 'success' as const, tool_name: 'list_nodes' }
    const state = assistant({ conversation: { conversation, messages: [], tool_calls: [completedRead, pendingCall] }, confirm })
    const nativeConfirm = vi.spyOn(window, 'confirm')
    render(<AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />)

    expect(screen.queryByText('查询节点')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '检查并确认' }))
    const dialog = screen.getByRole('dialog', { name: '确认 AI 运维操作' })
    await waitFor(() => expect(dialog).toHaveFocus())
    fireEvent.click(within(dialog).getByRole('button', { name: '确认执行' }))
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1))
    expect(nativeConfirm).not.toHaveBeenCalled()
    nativeConfirm.mockRestore()
  })

  test('renders one ordered plan surface and confirms the complete plan once', async () => {
    const confirmPlan = vi.fn(async () => undefined)
    const first = { ...pendingCall, step_index: 0, result_summary: '重启节点' }
    const second = { ...pendingCall, id: 'tool-2', step_index: 1, tool_name: 'upgrade_agent', result_summary: '升级 Agent' }
    const plan: AIOperationPlan = {
      id: pendingCall.turn_id,
      turn_id: pendingCall.turn_id,
      status: 'pending',
      current_step: -1,
      steps: [first, second]
    }
    const state = assistant({
      conversation: { conversation, messages: [], tool_calls: [first, second], plans: [plan] },
      confirmPlan
    })
    render(<AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />)

    expect(screen.getAllByRole('region', { name: 'AI 变更计划' })).toHaveLength(1)
    expect(screen.getAllByRole('button', { name: '检查并确认' })).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: '检查并确认' }))
    const dialog = screen.getByRole('dialog', { name: '确认 AI 变更计划' })
    expect(within(dialog).getByText('1. 变更 Systemd 服务')).toBeInTheDocument()
    expect(within(dialog).getByText('2. 升级 Agent')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认执行计划' }))
    await waitFor(() => expect(confirmPlan).toHaveBeenCalledWith(plan))
  })

  test.each([
    { status: 'success', summary: '节点已重启', expected: 'success:AI 运维操作执行成功' },
    { status: 'accepted', summary: '重启请求已提交', expected: 'success:AI 运维操作已接受，正在处理中' },
    { status: 'failure', summary: '节点重启失败', expected: 'error:AI 运维操作执行失败: 节点重启失败' },
    { status: 'unsupported', summary: '当前 Agent 不支持重启', expected: 'error:AI 运维操作不受支持' }
  ] satisfies Array<{ status: AIToolCall['status'], summary: string, expected: string }>)('reports confirmed tool status $status without claiming every operation succeeded', async ({ status, summary, expected }) => {
    const completedCall = { ...pendingCall, status, result_summary: summary }
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation).mockResolvedValue({ conversation, messages: [], tool_calls: [pendingCall] })
    vi.mocked(api.confirmAIToolCall).mockResolvedValue({ turn: turn(), tool_call: completedCall })
    render(<ToolDecisionHarness call={pendingCall} />)

    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    await waitFor(() => expect(api.getAIConversation).toHaveBeenCalled())
    fireEvent.click(screen.getByRole('button', { name: 'Confirm tool' }))

    await waitFor(() => expect(api.confirmAIToolCall).toHaveBeenCalledWith(pendingCall.id))
    expect(await screen.findByTestId('tool-decision-toast')).toHaveTextContent(expected)
  })

  test('keeps read progress inline and labels the actual fallback result', async () => {
    const userMessage: AIMessage = { id: 'user-1', conversation_id: conversation.id, turn_id: 'turn-1', role: 'user', content: 'check nodes', provider_name: 'Primary', model: primaryModel.model_id, created_at: '' }
    const assistantMessage: AIMessage = { id: 'assistant-1', conversation_id: conversation.id, turn_id: 'turn-1', role: 'assistant', content: 'All nodes are healthy.', provider_name: 'Secondary', model: fallbackModel.model_id, created_at: '' }
    const fallbackTurn = turn({
      model_id: fallbackModel.id,
      provider_id: 'provider-2',
      provider_name: 'Secondary',
      model: fallbackModel.model_id,
      fallback_used: true
    })
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation)
      .mockResolvedValueOnce({ conversation, messages: [], tool_calls: [] })
      .mockResolvedValue({ conversation, messages: [userMessage, assistantMessage], tool_calls: [] })
    vi.mocked(api.sendAIMessageStream).mockImplementation(async (_id, _content, _signal, onProgress) => {
      onProgress({ phase: 'tool', tool_name: 'list_nodes', target_name: 'all nodes' })
      onProgress({ phase: 'fallback', provider_name: 'Secondary', model: fallbackModel.model_id })
      return { turn: fallbackTurn, message: assistantMessage }
    })
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    const composer = await screen.findByLabelText('发送给 AI 运维助手', { selector: '#ai-composer-drawer' })
    await waitFor(() => expect(composer).not.toBeDisabled())
    fireEvent.change(composer, { target: { value: 'check nodes' } })
    fireEvent.keyDown(composer, { key: 'Enter' })

    await waitFor(() => expect(api.sendAIMessageStream).toHaveBeenCalledWith(conversation.id, 'check nodes', expect.any(AbortSignal), expect.any(Function), expect.objectContaining({
      context: { page: 'overview' },
      onDelta: expect.any(Function),
      onReset: expect.any(Function)
    })))
    expect(vi.mocked(api.sendAIMessageStream).mock.calls[0]?.[1]).toBe('check nodes')
    expect(JSON.stringify(vi.mocked(api.sendAIMessageStream).mock.calls[0])).not.toContain('provider_id')
    const drawer = screen.getByTestId('ai-drawer-messages')
    expect(await within(drawer).findByText(/备用响应 · Secondary/)).toHaveTextContent(`原请求 Primary / ${primaryModel.model_id}`)
  })

  test('aborts the stream and clears inline progress on stop', async () => {
    const abortSpy = vi.spyOn(AbortController.prototype, 'abort')
    const stream = deferred<AISendResult>()
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation).mockResolvedValue({ conversation, messages: [], tool_calls: [] })
    vi.mocked(api.sendAIMessageStream).mockImplementation((_id, _content, _signal, onProgress) => {
      onProgress({ phase: 'model' })
      return stream.promise
    })
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    const composer = await screen.findByLabelText('发送给 AI 运维助手', { selector: '#ai-composer-drawer' })
    await waitFor(() => expect(composer).not.toBeDisabled())
    fireEvent.change(composer, { target: { value: 'query nodes' } })
    fireEvent.keyDown(composer, { key: 'Enter' })
    await waitFor(() => expect(within(screen.getByTestId('ai-drawer-messages')).getByRole('status')).toHaveTextContent('思考中...'))
    fireEvent.click(screen.getAllByRole('button', { name: '停止模型请求' })[0])
    expect(abortSpy).toHaveBeenCalled()
    stream.resolve({ turn: turn() })
    await waitFor(() => expect(api.getAIConversation).toHaveBeenCalled())
    abortSpy.mockRestore()
  })

  test('sends selected resource context and replaces reset stream content', async () => {
    const stream = deferred<AISendResult>()
    const refresh = deferred<AIConversationState>()
    const finalMessage: AIMessage = {
      id: 'message-final',
      conversation_id: conversation.id,
      turn_id: 'turn-1',
      role: 'assistant',
      content: 'live answer',
      provider_name: 'Primary',
      model: primaryModel.model_id,
      created_at: '2026-08-05T00:00:00Z'
    }
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.getAIConversation)
      .mockResolvedValueOnce({ conversation, messages: [], tool_calls: [] })
      .mockImplementation(() => refresh.promise)
    vi.mocked(api.sendAIMessageStream).mockImplementation((_id, _content, _signal, onProgress, options) => {
      onProgress({ phase: 'model' })
      options?.onDelta?.({ content: 'stale' })
      options?.onReset?.({ reason: 'fallback' })
      options?.onDelta?.({ content: 'live answer' })
      return stream.promise
    })
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Set node context' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    const composer = await screen.findByLabelText('发送给 AI 运维助手', { selector: '#ai-composer-drawer' })
    await waitFor(() => expect(composer).not.toBeDisabled())
    fireEvent.change(composer, { target: { value: 'inspect this node' } })
    fireEvent.keyDown(composer, { key: 'Enter' })

    const drawer = screen.getByTestId('ai-drawer-messages')
    expect(await within(drawer).findByText('live answer')).toBeInTheDocument()
    expect(within(drawer).queryByText('stale')).not.toBeInTheDocument()
    expect(api.sendAIMessageStream).toHaveBeenCalledWith(conversation.id, 'inspect this node', expect.any(AbortSignal), expect.any(Function), expect.objectContaining({
      context: { page: 'hosts', resource_type: 'node', resource_id: 'node-1' }
    }))
    stream.resolve({ turn: turn(), message: finalMessage })
    await waitFor(() => expect(api.getAIConversation).toHaveBeenCalledTimes(2))
    expect(within(drawer).getByText('inspect this node')).toBeInTheDocument()
    expect(within(drawer).getByText('live answer')).toBeInTheDocument()
    expect(within(drawer).queryByText('正在加载会话')).not.toBeInTheDocument()
    refresh.resolve({ conversation, messages: [finalMessage], tool_calls: [] })
    await waitFor(() => expect(within(drawer).queryByRole('status')).not.toBeInTheDocument())
  })
})
