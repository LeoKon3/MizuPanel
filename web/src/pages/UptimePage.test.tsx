import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { UptimePage } from './UptimePage'
import * as api from '../api/client'
import type { UptimeMonitor } from '../types'

vi.mock('../api/client', () => ({
  checkUptimeMonitor: vi.fn(),
  createUptimeMonitor: vi.fn(),
  deleteUptimeMonitor: vi.fn(),
  getUptimeIncidents: vi.fn(),
  getUptimeMonitors: vi.fn(),
  getUptimeResults: vi.fn(),
  toggleUptimeMonitor: vi.fn(),
  updateUptimeMonitor: vi.fn()
}))

const baseMonitor: UptimeMonitor = {
  id: 1,
  name: 'Website',
  type: 'http',
  target: 'https://example.com/health',
  enabled: true,
  interval_seconds: 60,
  timeout_seconds: 5,
  failure_threshold: 3,
  expected_status_min: 200,
  expected_status_max: 399,
  tls_expiry_threshold_days: 30,
  notification_channels: [],
  status: 'up',
  consecutive_failures: 0,
  last_latency_ms: 42,
  last_status_code: 204,
  last_checked_at: '2026-07-26T08:00:00Z',
  tls_expires_at: '2099-08-26T08:00:00Z',
  tls_remaining_days: 31,
  created_at: '2026-07-26T07:00:00Z',
  updated_at: '2026-07-26T08:00:00Z'
}

function deferred<Value>() {
  let resolve!: (value: Value | PromiseLike<Value>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<Value>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('UptimePage', () => {
  beforeEach(() => {
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [] })
    vi.mocked(api.createUptimeMonitor).mockResolvedValue(baseMonitor)
    vi.mocked(api.updateUptimeMonitor).mockResolvedValue(baseMonitor)
    vi.mocked(api.toggleUptimeMonitor).mockResolvedValue({ ...baseMonitor, enabled: false })
    vi.mocked(api.checkUptimeMonitor).mockResolvedValue(baseMonitor)
    vi.mocked(api.deleteUptimeMonitor).mockResolvedValue(undefined)
    vi.mocked(api.getUptimeResults).mockResolvedValue({ results: [] })
    vi.mocked(api.getUptimeIncidents).mockResolvedValue({ incidents: [] })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  test('renders the loading and empty states', async () => {
    let resolveRequest: ((value: { monitors: UptimeMonitor[] }) => void) | undefined
    vi.mocked(api.getUptimeMonitors).mockImplementationOnce(() => new Promise((resolve) => { resolveRequest = resolve }))
    render(<UptimePage />)

    expect(screen.getByText('正在加载服务拨测...')).toBeInTheDocument()
    resolveRequest?.({ monitors: [] })
    expect(await screen.findByText('还没有服务拨测')).toBeInTheDocument()
    expect(screen.getByText(/拨测请求从 MizuPanel Server 所在网络发起/)).toBeInTheDocument()
  })

  test('renders error, pending, up, warning, down, and disabled states', async () => {
    const monitors: UptimeMonitor[] = [
      { ...baseMonitor, id: 1, name: 'Pending', status: 'pending', last_checked_at: undefined, last_status_code: 0 },
      { ...baseMonitor, id: 2, name: 'Healthy', status: 'up' },
      { ...baseMonitor, id: 3, name: 'TLS Warning', status: 'warning' },
      { ...baseMonitor, id: 4, name: 'Down', status: 'down', consecutive_failures: 1, last_error: '连接超时' },
      { ...baseMonitor, id: 5, name: 'Disabled', enabled: false, status: 'up' }
    ]
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors })
    render(<UptimePage />)

    expect(await screen.findByText('Pending')).toBeInTheDocument()
    expect(screen.getByText('等待检测')).toBeInTheDocument()
    expect(screen.getAllByText('正常').length).toBeGreaterThan(0)
    expect(screen.getAllByText('证书预警').length).toBeGreaterThan(0)
    expect(screen.getAllByText('故障').length).toBeGreaterThan(0)
    expect(screen.getByText('已停用')).toBeInTheDocument()
    expect(screen.getByText('连接超时')).toBeInTheDocument()
    const disabledCard = screen.getByText('Disabled').closest('article')
    expect(disabledCard).not.toBeNull()
    expect(within(disabledCard as HTMLElement).getByRole('button', { name: '立即检测' })).toBeDisabled()
  })

  test('shows a bounded API error state', async () => {
    vi.mocked(api.getUptimeMonitors).mockRejectedValueOnce(new Error('连接失败'))
    render(<UptimePage />)

    expect(await screen.findByRole('alert')).toHaveTextContent('服务拨测加载失败: 连接失败')
    expect(screen.queryByText('还没有服务拨测')).not.toBeInTheDocument()
  })

  test('does not let a stale list refresh overwrite a completed operation', async () => {
    const staleRefresh = deferred<{ monitors: UptimeMonitor[] }>()
    const disabledMonitor = { ...baseMonitor, enabled: false }
    vi.mocked(api.getUptimeMonitors)
      .mockResolvedValueOnce({ monitors: [baseMonitor] })
      .mockImplementationOnce(() => staleRefresh.promise)
    vi.mocked(api.toggleUptimeMonitor).mockResolvedValueOnce(disabledMonitor)
    render(<UptimePage />)

    await screen.findByText('Website')
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(api.getUptimeMonitors).toHaveBeenCalledTimes(2))
    fireEvent.click(screen.getByRole('switch', { name: '停用 Website' }))

    await waitFor(() => expect(screen.getByRole('switch', { name: '启用 Website' })).toHaveAttribute('aria-checked', 'false'))
    await act(async () => {
      staleRefresh.resolve({ monitors: [baseMonitor] })
      await staleRefresh.promise
    })

    expect(screen.getByRole('switch', { name: '启用 Website' })).toHaveAttribute('aria-checked', 'false')
  })

  test('renders backend-projected TLS remaining days without using the browser clock', async () => {
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [{
      ...baseMonitor,
      tls_expires_at: '2099-08-26T08:00:00Z',
      tls_remaining_days: 7
    }] })
    render(<UptimePage />)

    expect(await screen.findByText('7 天')).toBeInTheDocument()
  })

  test('creates a monitor with the shared notification editor', async () => {
    vi.mocked(api.getUptimeMonitors)
      .mockResolvedValueOnce({ monitors: [] })
      .mockResolvedValueOnce({ monitors: [baseMonitor] })
    render(<UptimePage />)
    await screen.findByText('还没有服务拨测')
    const createTriggers = screen.getAllByRole('button', { name: '创建拨测' })
    const stableCreateTrigger = createTriggers[0]
    fireEvent.click(createTriggers[1])

    fireEvent.change(screen.getByLabelText('拨测名称'), { target: { value: 'API Health' } })
    fireEvent.change(screen.getByLabelText('拨测目标'), { target: { value: 'https://api.example.com/health' } })
    fireEvent.click(screen.getByRole('button', { name: '+ Webhook' }))
    fireEvent.change(screen.getByLabelText('Webhook Webhook 地址 1'), { target: { value: 'https://hooks.example.com/notify' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(api.createUptimeMonitor).toHaveBeenCalledWith(expect.objectContaining({
      name: 'API Health',
      target: 'https://api.example.com/health',
      interval_seconds: 60,
      timeout_seconds: 5,
      failure_threshold: 3,
      notification_channels: [{ type: 'webhook', webhook_url: 'https://hooks.example.com/notify' }]
    })))
    expect(await screen.findByText('服务拨测创建成功')).toBeInTheDocument()
    expect(await screen.findByText('Website')).toBeInTheDocument()
    await waitFor(() => expect(stableCreateTrigger).toHaveFocus())
  })

  test('contains create and edit form focus, closes with Escape, and restores each trigger', async () => {
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [baseMonitor] })
    render(<UptimePage />)
    await screen.findByText('Website')

    const createTrigger = screen.getByRole('button', { name: '创建拨测' })
    fireEvent.click(createTrigger)
    const createDialog = screen.getByRole('dialog', { name: '创建服务拨测' })
    const createNameInput = within(createDialog).getByLabelText('拨测名称')
    const createCloseButton = within(createDialog).getByRole('button', { name: '关闭服务拨测表单' })
    const createSaveButton = within(createDialog).getByRole('button', { name: '保存' })
    await waitFor(() => expect(createNameInput).toHaveFocus())

    createCloseButton.focus()
    fireEvent.keyDown(createCloseButton, { key: 'Tab', shiftKey: true })
    expect(createSaveButton).toHaveFocus()
    fireEvent.keyDown(createSaveButton, { key: 'Tab' })
    expect(createCloseButton).toHaveFocus()
    fireEvent.keyDown(createCloseButton, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: '创建服务拨测' })).not.toBeInTheDocument()
    expect(createTrigger).toHaveFocus()

    const monitorCard = screen.getByText('Website').closest('article')
    expect(monitorCard).not.toBeNull()
    const editTrigger = within(monitorCard as HTMLElement).getByRole('button', { name: '编辑' })
    fireEvent.click(editTrigger)
    const editDialog = screen.getByRole('dialog', { name: '编辑服务拨测' })
    await waitFor(() => expect(within(editDialog).getByLabelText('拨测名称')).toHaveFocus())
    fireEvent.keyDown(within(editDialog).getByLabelText('拨测名称'), { key: 'Escape' })

    expect(screen.queryByRole('dialog', { name: '编辑服务拨测' })).not.toBeInTheDocument()
    expect(editTrigger).toHaveFocus()
  })

  test('contains history and delete dialog focus and restores their triggers', async () => {
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [baseMonitor] })
    render(<UptimePage />)
    await screen.findByText('Website')
    const monitorCard = screen.getByText('Website').closest('article')
    expect(monitorCard).not.toBeNull()

    const historyTrigger = within(monitorCard as HTMLElement).getByRole('button', { name: '历史' })
    fireEvent.click(historyTrigger)
    const historyDialog = screen.getByRole('dialog', { name: '服务拨测历史' })
    const historyCloseButton = within(historyDialog).getByRole('button', { name: '关闭服务拨测历史' })
    await waitFor(() => expect(historyCloseButton).toHaveFocus())
    fireEvent.keyDown(historyCloseButton, { key: 'Tab' })
    expect(historyCloseButton).toHaveFocus()
    fireEvent.keyDown(historyCloseButton, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: '服务拨测历史' })).not.toBeInTheDocument()
    expect(historyTrigger).toHaveFocus()

    const deleteTrigger = within(monitorCard as HTMLElement).getByRole('button', { name: '删除' })
    fireEvent.click(deleteTrigger)
    const deleteDialog = screen.getByRole('dialog', { name: '删除服务拨测' })
    const cancelButton = within(deleteDialog).getByRole('button', { name: '取消' })
    const confirmButton = within(deleteDialog).getByRole('button', { name: '确认删除' })
    await waitFor(() => expect(cancelButton).toHaveFocus())
    fireEvent.keyDown(cancelButton, { key: 'Tab', shiftKey: true })
    expect(confirmButton).toHaveFocus()
    fireEvent.keyDown(confirmButton, { key: 'Tab' })
    expect(cancelButton).toHaveFocus()
    fireEvent.keyDown(cancelButton, { key: 'Escape' })

    expect(screen.queryByRole('dialog', { name: '删除服务拨测' })).not.toBeInTheDocument()
    expect(deleteTrigger).toHaveFocus()
  })

  test('edits, toggles, checks, and deletes without a page reload', async () => {
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [baseMonitor] })
    render(<UptimePage />)
    await screen.findByText('Website')

    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    fireEvent.change(screen.getByLabelText('拨测名称'), { target: { value: 'Website v2' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.updateUptimeMonitor).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Website v2' })))

    fireEvent.click(screen.getByRole('button', { name: '立即检测' }))
    await waitFor(() => expect(api.checkUptimeMonitor).toHaveBeenCalledWith(1))

    fireEvent.click(screen.getByRole('switch', { name: '停用 Website' }))
    await waitFor(() => expect(api.toggleUptimeMonitor).toHaveBeenCalledWith(1, false))

    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    expect(screen.getByRole('dialog', { name: '删除服务拨测' })).toBeInTheDocument()
    const stableCreateTrigger = screen.getByRole('button', { name: '创建拨测' })
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(api.deleteUptimeMonitor).toHaveBeenCalledWith(1))
    expect(await screen.findByText('服务拨测删除成功')).toBeInTheDocument()
    await waitFor(() => expect(stableCreateTrigger).toHaveFocus())
  })

  test('loads result and incident history', async () => {
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [baseMonitor] })
    vi.mocked(api.getUptimeResults).mockResolvedValue({ results: [{ id: 10, monitor_id: 1, success: false, latency_ms: 5000, error: '连接超时', checked_at: '2026-07-26T08:00:00Z' }] })
    vi.mocked(api.getUptimeIncidents).mockResolvedValue({ incidents: [{ id: 20, monitor_id: 1, kind: 'availability', message: '连续 3 次检测失败', started_at: '2026-07-26T08:00:00Z', notification_sent: true, recovery_notification_sent: false, created_at: '2026-07-26T08:00:00Z' }] })
    render(<UptimePage />)
    await screen.findByText('Website')

    fireEvent.click(screen.getByRole('button', { name: '历史' }))
    expect(await screen.findByRole('dialog', { name: '服务拨测历史' })).toBeInTheDocument()
    await waitFor(() => expect(api.getUptimeResults).toHaveBeenCalledWith(1, 50))
    expect(await screen.findByText('最近检测')).toBeInTheDocument()
    expect(screen.getByText('连续 3 次检测失败')).toBeInTheDocument()
    expect(screen.getByText('触发通知成功')).toBeInTheDocument()
  })

  test('does not let one monitor history request populate another monitor dialog', async () => {
    const monitorB = { ...baseMonitor, id: 2, name: 'API' }
    const staleResults = deferred<Awaited<ReturnType<typeof api.getUptimeResults>>>()
    const staleIncidents = deferred<Awaited<ReturnType<typeof api.getUptimeIncidents>>>()
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [baseMonitor, monitorB] })
    vi.mocked(api.getUptimeResults).mockImplementation((id) => id === baseMonitor.id
      ? staleResults.promise
      : Promise.resolve({ results: [{ id: 11, monitor_id: monitorB.id, success: false, latency_ms: 25, error: 'API 当前结果', checked_at: '2026-07-26T08:01:00Z' }] }))
    vi.mocked(api.getUptimeIncidents).mockImplementation((id) => id === baseMonitor.id
      ? staleIncidents.promise
      : Promise.resolve({ incidents: [{ id: 21, monitor_id: monitorB.id, kind: 'availability', message: 'API 当前事件', started_at: '2026-07-26T08:01:00Z', notification_sent: false, recovery_notification_sent: false, created_at: '2026-07-26T08:01:00Z' }] }))
    render(<UptimePage />)
    await screen.findByText('Website')

    const websiteCard = screen.getByText('Website').closest('article')
    const apiCard = screen.getByText('API').closest('article')
    expect(websiteCard).not.toBeNull()
    expect(apiCard).not.toBeNull()
    fireEvent.click(within(websiteCard as HTMLElement).getByRole('button', { name: '历史' }))
    fireEvent.click(screen.getByRole('button', { name: '关闭服务拨测历史' }))
    fireEvent.click(within(apiCard as HTMLElement).getByRole('button', { name: '历史' }))

    expect(await screen.findByText('API 当前结果')).toBeInTheDocument()
    expect(screen.getByText('API 当前事件')).toBeInTheDocument()
    await act(async () => {
      staleResults.resolve({ results: [{ id: 10, monitor_id: baseMonitor.id, success: false, latency_ms: 5000, error: 'Website 过期结果', checked_at: '2026-07-26T08:00:00Z' }] })
      staleIncidents.resolve({ incidents: [{ id: 20, monitor_id: baseMonitor.id, kind: 'availability', message: 'Website 过期事件', started_at: '2026-07-26T08:00:00Z', notification_sent: true, recovery_notification_sent: false, created_at: '2026-07-26T08:00:00Z' }] })
      await Promise.all([staleResults.promise, staleIncidents.promise])
    })

    expect(screen.getByText('API 当前结果')).toBeInTheDocument()
    expect(screen.getByText('API 当前事件')).toBeInTheDocument()
    expect(screen.queryByText('Website 过期结果')).not.toBeInTheDocument()
    expect(screen.queryByText('Website 过期事件')).not.toBeInTheDocument()
  })
})
