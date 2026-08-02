import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import * as api from '../../api/client'
import type { AIProvider } from '../../types'
import { AIProviderSettings } from './AIProviderSettings'

vi.mock('../../api/client', () => ({
  createAIProvider: vi.fn(),
  deleteAIProvider: vi.fn(),
  getAIProviders: vi.fn(),
  listAIProviderModels: vi.fn(),
  setDefaultAIProvider: vi.fn(),
  testAIProvider: vi.fn(),
  updateAIProvider: vi.fn()
}))

const provider: AIProvider = {
  id: 'provider-1',
  name: 'Internal model',
  protocol: 'openai_chat_completions',
  base_url: 'http://model.internal/v1',
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
    vi.mocked(api.updateAIProvider).mockResolvedValue(provider)
	vi.mocked(api.listAIProviderModels).mockResolvedValue([])
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  test('never echoes a stored API key and preserves it when the field is left empty', async () => {
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '编辑模型配置' }))

    const dialog = screen.getByRole('dialog', { name: '编辑模型配置' })
    const keyInput = within(dialog).getByLabelText('API Key')
    expect(keyInput).toHaveValue('')
    expect(keyInput).toHaveAttribute('type', 'password')
    expect(screen.queryByDisplayValue(/secret|key/i)).not.toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '保存配置' }))

    await waitFor(() => expect(api.updateAIProvider).toHaveBeenCalledWith('provider-1', {
      name: 'Internal model',
      protocol: 'openai_chat_completions',
      base_url: 'http://model.internal/v1',
      model: 'ops-model'
    }))
  })

  test('distinguishes API key replacement from explicit clearing', async () => {
    const { rerender } = render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '编辑模型配置' }))
    let dialog = screen.getByRole('dialog', { name: '编辑模型配置' })
    fireEvent.change(within(dialog).getByLabelText('API Key'), { target: { value: 'replacement-key' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存配置' }))
    await waitFor(() => expect(api.updateAIProvider).toHaveBeenLastCalledWith('provider-1', expect.objectContaining({ api_key: 'replacement-key' })))

    rerender(<AIProviderSettings />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑模型配置' }))
    dialog = screen.getByRole('dialog', { name: '编辑模型配置' })
    fireEvent.click(within(dialog).getByLabelText('清除当前 API Key'))
    expect(within(dialog).getByLabelText('API Key')).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: '保存配置' }))
    await waitFor(() => expect(api.updateAIProvider).toHaveBeenLastCalledWith('provider-1', expect.objectContaining({ clear_api_key: true })))
    const updateCalls = vi.mocked(api.updateAIProvider).mock.calls
    expect(updateCalls[updateCalls.length - 1]?.[1]).not.toHaveProperty('api_key')
  })

  test('detects available models and selects from a dropdown', async () => {
    vi.mocked(api.listAIProviderModels).mockResolvedValue(['model-a', 'model-b'])
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '添加模型' }))

    const dialog = screen.getByRole('dialog', { name: '添加模型配置' })
    fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://model.test/v1' } })
    fireEvent.change(within(dialog).getByLabelText('API Key'), { target: { value: 'key-marker' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '检测模型' }))

    await waitFor(() => expect(api.listAIProviderModels).toHaveBeenCalledWith('https://model.test/v1', 'key-marker', undefined))
    const modelSelect = await within(dialog).findByRole('combobox', { name: '模型名称' })
    expect(modelSelect).toHaveValue('model-a')
    expect(within(modelSelect).getAllByRole('option').map((option) => option.textContent)).toEqual(['model-a', 'model-b'])
  })

  test('keeps manual model input when detection returns no models', async () => {
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '添加模型' }))

    const dialog = screen.getByRole('dialog', { name: '添加模型配置' })
    fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://model.test/v1' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '检测模型' }))

    expect(await within(dialog).findByText('服务没有返回可选模型，可继续手动填写模型名称。')).toBeInTheDocument()
    expect(within(dialog).getByRole('textbox', { name: '模型名称' })).toBeInTheDocument()
  })

  test('reuses the saved credential when detecting models for an existing provider', async () => {
    vi.mocked(api.listAIProviderModels).mockResolvedValue(['ops-model'])
    render(<AIProviderSettings />)
    await screen.findByText('Internal model')
    fireEvent.click(screen.getByRole('button', { name: '编辑模型配置' }))

    const dialog = screen.getByRole('dialog', { name: '编辑模型配置' })
    expect(within(dialog).getByLabelText('API Key')).toHaveValue('')
    fireEvent.click(within(dialog).getByRole('button', { name: '检测模型' }))

    await waitFor(() => expect(api.listAIProviderModels).toHaveBeenCalledWith('http://model.internal/v1', '', 'provider-1'))
    expect(await within(dialog).findByRole('combobox', { name: '模型名称' })).toHaveValue('ops-model')
  })
})
