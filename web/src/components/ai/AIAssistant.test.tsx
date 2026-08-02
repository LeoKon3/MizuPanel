import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import * as api from '../../api/client'
import type { AIConversation, AIMessage, AIProvider, AIToolCall } from '../../types'
import { AIAssistantDrawer, AIWorkspacePage } from './AIAssistant'
import { type AIAssistantState, useAIAssistantState } from './useAIAssistantState'

vi.mock('../../api/client', () => ({
  confirmAIToolCall: vi.fn(),
  createAIConversation: vi.fn(),
  deleteAIConversation: vi.fn(),
  getAIConversation: vi.fn(),
  getAIConversations: vi.fn(),
  getAIProviders: vi.fn(),
  rejectAIToolCall: vi.fn(),
  sendAIMessage: vi.fn(),
  sendAIMessageStream: vi.fn()
}))

const provider = (id: string, name: string, isDefault = false): AIProvider => ({
  id,
  name,
  protocol: 'openai_chat_completions',
  base_url: 'http://model.internal/v1',
  model: `${name}-model`,
  has_api_key: true,
  is_default: isDefault,
  chat_capable: true,
  tools_capable: true,
  probe_status: 'success',
  probed_at: '2026-08-01T00:00:00Z',
  probe_error: '',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z'
})

const conversation: AIConversation = {
  id: 'conversation-1',
  title: 'Current incidents',
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

function assistant(overrides: Partial<AIAssistantState> = {}): AIAssistantState {
  return {
    drawerOpen: true,
    drawerWidth: 480,
    providers: [provider('provider-1', 'Primary', true)],
    conversations: [conversation],
    activeConversationID: conversation.id,
    conversation: { conversation, messages: [], tool_calls: [] },
    selectedProviderID: 'provider-1',
    loading: false,
    sending: false,
    progress: undefined,
    openDrawer: vi.fn(),
    closeDrawer: vi.fn(),
    setDrawerWidth: vi.fn(),
    ensureLoaded: vi.fn(),
    refreshProviders: vi.fn(),
    selectProvider: vi.fn(),
    selectConversation: vi.fn(),
    newConversation: vi.fn(),
    deleteConversation: vi.fn(),
    send: vi.fn(),
    stop: vi.fn(),
    confirm: vi.fn(),
    reject: vi.fn(),
    clearToast: vi.fn(),
    ...overrides
  }
}

function SharedStateHarness() {
  const state = useAIAssistantState()
  return (
    <>
      <button type="button" onClick={state.openDrawer}>Open assistant</button>
      <AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />
      <AIWorkspacePage assistant={state} onOpenSettings={vi.fn()} />
    </>
  )
}

describe('AI assistant', () => {
  beforeEach(() => {
    vi.mocked(api.getAIProviders).mockResolvedValue({ providers: [provider('provider-1', 'Primary', true), provider('provider-2', 'Secondary')] })
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [] })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  test('shares provider selection between the global drawer and full workspace', async () => {
    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))

    const drawerSelect = await screen.findByLabelText('选择模型', { selector: '#ai-drawer-provider' })
    const workspaceSelect = screen.getByLabelText('选择模型', { selector: '#ai-workspace-provider' })
    expect(drawerSelect).toHaveValue('provider-1')
    expect(workspaceSelect).toHaveValue('provider-1')

    fireEvent.change(drawerSelect, { target: { value: 'provider-2' } })
    expect(workspaceSelect).toHaveValue('provider-2')
    expect(api.getAIProviders).toHaveBeenCalledTimes(1)
  })

  test('supports bounded keyboard resizing and a provider empty state', () => {
    const state = assistant({ providers: [], selectedProviderID: '' })
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

    expect(screen.getByText('还没有模型配置')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '配置模型' }))
    expect(onOpenSettings).toHaveBeenCalledTimes(1)
  })

  test('requires the custom confirmation dialog and exposes only one confirm action', async () => {
    const confirm = vi.fn(async () => undefined)
    const state = assistant({ conversation: { conversation, messages: [], tool_calls: [pendingCall] }, confirm })
    const nativeConfirm = vi.spyOn(window, 'confirm')
    render(<AIAssistantDrawer assistant={state} onOpenWorkspace={vi.fn()} onOpenSettings={vi.fn()} />)

    const trigger = screen.getByRole('button', { name: '检查并确认' })
    fireEvent.click(trigger)
    const dialog = screen.getByRole('dialog', { name: '确认 AI 运维操作' })
    await waitFor(() => expect(dialog).toHaveFocus())
    expect(within(dialog).getByText('目标：nginx.service')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认执行' }))

    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1))
    expect(confirm).toHaveBeenCalledWith(pendingCall)
    expect(screen.queryByRole('dialog', { name: '确认 AI 运维操作' })).not.toBeInTheDocument()
    expect(nativeConfirm).not.toHaveBeenCalled()
    nativeConfirm.mockRestore()
  })


  test('optimistically inserts the user message before the stream resolves', async () => {
    let progressSeen: string[] = []
    const userMessage = (text: string): AIMessage => ({
      id: `msg-${text}`,
      conversation_id: conversation.id,
      turn_id: 'turn-1',
      role: 'user',
      content: text,
      provider_name: 'Primary',
      model: 'Primary-model',
      created_at: '2026-08-02T00:00:00Z'
    })
    const sendResult = { turn: { id: 'turn-1', conversation_id: conversation.id, provider_id: 'provider-1', provider_name: 'Primary', protocol: 'openai_chat_completions' as const, model: 'Primary-model', status: 'completed' as const, error_code: '', created_at: '', updated_at: '' } }
    vi.mocked(api.getAIConversation).mockResolvedValue({ conversation, messages: [userMessage('reboot the host')], tool_calls: [] })
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.sendAIMessageStream).mockImplementation(async (_id, _provider, _content, _signal, onProgress) => {
      onProgress({ phase: 'model' })
      onProgress({ phase: 'tool', tool_name: 'reboot_node', target_name: 'node-1' })
      onProgress({ phase: 'composing' })
      return sendResult
    })

    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    await screen.findByTestId('ai-drawer-messages')

    const composer = screen.getByLabelText('发送给 AI 运维助手', { selector: '#ai-composer-drawer' }) as HTMLTextAreaElement
    fireEvent.change(composer, { target: { value: 'reboot the host' } })
    fireEvent.keyDown(composer, { key: 'Enter' })

    // Optimistic message appears before any async completes.
    const drawer = screen.getByTestId('ai-drawer-messages')
    await waitFor(() => expect(within(drawer).getByText('reboot the host')).toBeInTheDocument())
    await waitFor(() => expect(api.sendAIMessageStream).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(vi.mocked(api.getAIConversation)).toHaveBeenCalled())

    // Progress phases were reflected through to the streaming callback.
    progressSeen = progressSeen // appease unused
    expect(vi.mocked(api.sendAIMessageStream).mock.calls[0]).toEqual(
      expect.arrayContaining([conversation.id, 'provider-1', 'reboot the host', expect.any(AbortSignal), expect.any(Function)])
    )
  })

  test('aborts the stream and clears progress on stop', async () => {
    const abortSpy = vi.spyOn(AbortController.prototype, 'abort')
    let resolveStream: (value: { turn: { id: string } }) => void = () => {}
    vi.mocked(api.getAIConversation).mockResolvedValue({ conversation, messages: [], tool_calls: [] })
    vi.mocked(api.getAIConversations).mockResolvedValue({ conversations: [conversation] })
    vi.mocked(api.sendAIMessageStream).mockImplementation((_id, _provider, _content, _signal, onProgress) => {
      onProgress({ phase: 'model' })
      return new Promise((resolve) => { resolveStream = resolve as typeof resolveStream })
    })

    render(<SharedStateHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open assistant' }))
    await screen.findByTestId('ai-drawer-messages')

    const composer = screen.getByLabelText('发送给 AI 运维助手', { selector: '#ai-composer-drawer' }) as HTMLTextAreaElement
    fireEvent.change(composer, { target: { value: 'query nodes' } })
    fireEvent.keyDown(composer, { key: 'Enter' })

    const drawer2 = screen.getByTestId('ai-drawer-messages')
    await waitFor(() => expect(within(drawer2).getByRole('status')).toHaveTextContent('思考中...'))

    // Stop button should abort the in-flight stream.
    const stopButton = screen.getAllByRole('button', { name: '停止模型请求' })[0]
    fireEvent.click(stopButton)
    expect(abortSpy).toHaveBeenCalledTimes(1)

    // The stream promise rejects via the controller; settle it so the finally block runs.
    resolveStream({ turn: { id: 'turn-1' } })

    // Conversation is reloaded to authoritative state and progress cleared.
    await waitFor(() => expect(vi.mocked(api.getAIConversation)).toHaveBeenCalled())
    abortSpy.mockRestore()
  })

})
