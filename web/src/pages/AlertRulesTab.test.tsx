import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { AlertRulesTab } from './AlertRulesTab'
import * as api from '../api/client'

vi.mock('../api/client', () => ({
  createAlertRule: vi.fn(),
  deleteAlertRule: vi.fn(),
  getAlertRules: vi.fn(),
  toggleAlertRule: vi.fn(),
  updateAlertRule: vi.fn()
}))

describe('AlertRulesTab notification editor integration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getAlertRules).mockResolvedValue({ rules: [] })
    vi.mocked(api.createAlertRule).mockResolvedValue({
      id: 1,
      name: 'CPU High',
      enabled: true,
      metric_field: 'cpu_usage',
      operator: '>',
      threshold: 80,
      duration_seconds: 300,
      scope_type: 'all',
      notification_channels: [{ type: 'webhook', webhook_url: 'https://hooks.example.com/notify' }],
      created_at: '',
      updated_at: ''
    })
  })

  test('creates alert rules through the shared notification channel editor', async () => {
    render(<AlertRulesTab nodes={[]} />)

    fireEvent.click(await screen.findByRole('button', { name: '创建告警规则' }))
    fireEvent.change(screen.getByLabelText('规则名称'), { target: { value: 'CPU High' } })
    fireEvent.click(screen.getByRole('button', { name: '+ Webhook' }))
    fireEvent.change(screen.getByLabelText('Webhook Webhook 地址 1'), { target: { value: 'https://hooks.example.com/notify' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(api.createAlertRule).toHaveBeenCalledWith(expect.objectContaining({
      name: 'CPU High',
      notification_channels: [{ type: 'webhook', webhook_url: 'https://hooks.example.com/notify' }]
    })))
    expect(await screen.findByText('告警规则创建成功')).toBeInTheDocument()
  })
})
