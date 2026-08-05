import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import * as api from '../../api/client'
import type { AIProvider, AIProviderModel } from '../../types'
import { AIProviderSettings } from './AIProviderSettings'

vi.mock('../../api/client', () => ({
  createAIProvider: vi.fn(),
  deleteAIModel: vi.fn(),
  deleteAIProvider: vi.fn(),
  discoverAIProvider: vi.fn(),
  getAIProviders: vi.fn(),
  getAIRouting: vi.fn(),
  importAIProviderModels: vi.fn(),
  testAIModel: vi.fn(),
  updateAIModel: vi.fn(),
  updateAIProvider: vi.fn(),
  updateAIRouting: vi.fn()
}))

const childModel: AIProviderModel = {
  id: 'model-1',
  provider_id: 'provider-1',
  model_id: 'ops-model',
  display_name: 'Operations',
  enabled: true,
  chat_capable: true,
  tools_capable: true,
  probe_status: 'success',
  probe_latency_ms: 34,
  probed_at: '2026-08-01T00:00:00Z',
  probe_error: '',
  is_default: true,
  is_fallback: false,
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z'
}

const provider: AIProvider = {
  id: 'provider-1',
  name: 'Internal model',
  protocol: 'openai_chat_completions',
  base_url: 'http://model.internal/v1',
  enabled: true,
  discovery_status: 'success',
  discovery_latency_ms: 20,
  discovered_at: '2026-08-01T00:00:00Z',
  discovery_error: '',
  models: [childModel],
  model: 'ops-model',
  has_api_key: true,
  is_default: true,
  chat_capable: true,
  tools_capable: true,
  probe_status: 'success',
  probed_at: '2026-08-01T00:00:00Z',
  probe_error: '',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z'
}

describe('AIProviderSettings', () => {
  beforeEach(() => {
    vi.mocked(api.getAIProviders).mockResolvedValue({ providers: [provider] })
    vi.mocked(api.getAIRouting).mockResolvedValue({ default_model_id: childModel.id, fallback_model_id: null })
    vi.mocked(api.updateAIProvider).mockResolvedValue(provider)
    vi.mocked(api.updateAIRouting).mockResolvedValue({ default_model_id: childModel.id, fallback_model_id: null })
    vi.mocked(api.importAIProviderModels).mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  test('renders compact connection health and expandable child model rows', async () => {
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    expect(screen.getByText(/连接正常 · 20 ms/)).toBeInTheDocument()
    expect(screen.queryByText('Operations')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '展开模型' }))
    expect(screen.getByText('Operations')).toBeInTheDocument()
    expect(screen.getByText(/Chat 可用 · Tools 可用/)).toBeInTheDocument()
  })

  test('never echoes a stored API key and preserves it when left empty', async () => {
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '编辑 Provider' }))

    const dialog = screen.getByRole('dialog', { name: '编辑 Provider' })
    const keyInput = within(dialog).getByLabelText('API Key')
    expect(keyInput).toHaveValue('')
    expect(keyInput).toHaveAttribute('type', 'password')
    fireEvent.click(within(dialog).getByRole('button', { name: '保存 Provider' }))

    await waitFor(() => expect(api.updateAIProvider).toHaveBeenCalledWith('provider-1', {
      name: 'Internal model',
      protocol: 'openai_chat_completions',
      base_url: 'http://model.internal/v1',
      model: 'ops-model',
      enabled: true
    }))
  })

  test('distinguishes API key replacement from explicit clearing', async () => {
    const { unmount } = render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '编辑 Provider' }))
    let dialog = screen.getByRole('dialog', { name: '编辑 Provider' })
    fireEvent.change(within(dialog).getByLabelText('API Key'), { target: { value: 'replacement-key' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存 Provider' }))
    await waitFor(() => expect(api.updateAIProvider).toHaveBeenLastCalledWith('provider-1', expect.objectContaining({ api_key: 'replacement-key' })))

    unmount()
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '编辑 Provider' }))
    dialog = screen.getByRole('dialog', { name: '编辑 Provider' })
    fireEvent.click(within(dialog).getByLabelText('清除当前 API Key'))
    expect(within(dialog).getByLabelText('API Key')).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: '保存 Provider' }))
    await waitFor(() => expect(api.updateAIProvider).toHaveBeenLastCalledWith('provider-1', expect.objectContaining({ clear_api_key: true })))
    const updateCalls = vi.mocked(api.updateAIProvider).mock.calls
    const lastInput = updateCalls[updateCalls.length - 1]?.[1]
    expect(lastInput).not.toHaveProperty('api_key')
  })

  test('discovers a selectable list and imports selected plus manual models without probing', async () => {
    vi.mocked(api.discoverAIProvider).mockResolvedValue({ provider, models: ['model-a', 'model-b'] })
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '获取模型' }))

    const dialog = await screen.findByRole('dialog', { name: '导入发现的模型' })
    expect(api.discoverAIProvider).toHaveBeenCalledWith('provider-1')
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'model-b' }))
    fireEvent.change(within(dialog).getByLabelText('手动模型 ID'), { target: { value: 'manual-model' } })
    fireEvent.change(within(dialog).getByLabelText('显示名称（可选）'), { target: { value: 'Manual' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '导入模型' }))

    await waitFor(() => expect(api.importAIProviderModels).toHaveBeenCalledWith('provider-1', [
      { model_id: 'model-a', display_name: '' },
      { model_id: 'manual-model', display_name: 'Manual' }
    ]))
    expect(api.testAIModel).not.toHaveBeenCalled()
  })

  test('saves a Provider and immediately opens model discovery', async () => {
    vi.mocked(api.discoverAIProvider).mockResolvedValue({ provider, models: ['model-a'] })
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '编辑 Provider' }))
    fireEvent.click(screen.getByRole('button', { name: '保存并获取模型' }))

    await waitFor(() => expect(api.updateAIProvider).toHaveBeenCalledWith('provider-1', expect.objectContaining({
      base_url: provider.base_url,
      enabled: true
    })))
    await waitFor(() => expect(api.discoverAIProvider).toHaveBeenCalledWith(provider.id))
    expect(await screen.findByRole('dialog', { name: '导入发现的模型' })).toBeInTheDocument()
  })

  test('supports manual model add and child probe actions independently', async () => {
    vi.mocked(api.testAIModel).mockResolvedValue(childModel)
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '手动添加模型' }))
    const addDialog = screen.getByRole('dialog', { name: '手动添加模型' })
    fireEvent.change(within(addDialog).getByLabelText('上游模型 ID'), { target: { value: 'manual-model' } })
    fireEvent.change(within(addDialog).getByLabelText('显示名称（可选）'), { target: { value: 'Manual' } })
    fireEvent.click(within(addDialog).getByRole('button', { name: '保存模型' }))
    await waitFor(() => expect(api.importAIProviderModels).toHaveBeenCalledWith('provider-1', [{ model_id: 'manual-model', display_name: 'Manual' }], true))

    fireEvent.click(screen.getByRole('button', { name: '检测模型能力' }))
    await waitFor(() => expect(api.testAIModel).toHaveBeenCalledWith(childModel.id))
  })

  test('selects and deletes multiple unrouted models in one confirmation', async () => {
    const models = [
      { ...childModel, id: 'model-a', model_id: 'model-a', display_name: 'Model A', is_default: false },
      { ...childModel, id: 'model-b', model_id: 'model-b', display_name: 'Model B', is_default: false }
    ]
    vi.mocked(api.getAIProviders).mockResolvedValue({ providers: [{ ...provider, is_default: false, models }] })
    vi.mocked(api.getAIRouting).mockResolvedValue({ default_model_id: null, fallback_model_id: null })
    vi.mocked(api.deleteAIModel).mockResolvedValue()
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')

    fireEvent.click(screen.getByRole('button', { name: '全选模型' }))
    expect(screen.getByText('已选择 2 个模型')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '删除已选模型' }))
    const dialog = screen.getByRole('dialog', { name: '删除已选模型' })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))

    await waitFor(() => expect(api.deleteAIModel).toHaveBeenCalledWith('model-a'))
    expect(api.deleteAIModel).toHaveBeenCalledWith('model-b')
  })

  test('updates default and fallback routing with distinct model IDs', async () => {
    const unroutedModel = { ...childModel, is_default: false }
    vi.mocked(api.getAIProviders).mockResolvedValue({ providers: [{ ...provider, models: [unroutedModel], is_default: false }] })
    vi.mocked(api.getAIRouting).mockResolvedValue({ default_model_id: null, fallback_model_id: null })
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '展开模型' }))
    fireEvent.click(screen.getByRole('button', { name: '设为默认模型' }))
    await waitFor(() => expect(api.updateAIRouting).toHaveBeenCalledWith({ default_model_id: childModel.id, fallback_model_id: null }))
  })

  test('uses a custom guarded delete dialog', async () => {
    vi.mocked(api.deleteAIProvider).mockResolvedValue()
    const nativeConfirm = vi.spyOn(window, 'confirm')
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '删除 Provider' }))
    const dialog = screen.getByRole('dialog', { name: '删除 Provider' })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(api.deleteAIProvider).toHaveBeenCalledWith(provider.id))
    expect(nativeConfirm).not.toHaveBeenCalled()
    nativeConfirm.mockRestore()
  })
})
