import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { AuditPage } from './AuditPage'
import * as api from '../api/client'
import type { AuditEvent, AuditEventsResponse, Node } from '../types'

vi.mock('../api/client', () => ({
  getAuditEvents: vi.fn(),
  cleanupAuditEvents: vi.fn()
}))

const node: Node = {
  id: 'node-1',
  name: 'Oracle SG',
  hostname: 'oracle-sg',
  ip: '10.0.0.1',
  os: 'linux',
  arch: 'arm64',
  kernel: '6.6',
  agent_version: '0.1.8',
  status: 'online',
  last_seen_at: '2026-07-26T08:00:00Z'
}

const baseEvent: AuditEvent = {
  id: 101,
  request_id: 'request-101',
  created_at: '2026-07-26T08:00:00Z',
  actor_type: 'admin',
  actor_name: 'admin',
  source_ip: '172.18.51.1',
  module: 'docker',
  action: 'container.restart',
  target_type: 'container',
  target_id: 'container-1',
  target_name: 'nginx',
  node_id: 'node-1',
  result: 'success',
  duration_ms: 42,
  summary: '',
  metadata: { resource_kind: 'container' }
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

describe('AuditPage', () => {
  beforeEach(() => {
    vi.mocked(api.getAuditEvents).mockResolvedValue({ events: [], next_before_id: null })
    vi.mocked(api.cleanupAuditEvents).mockResolvedValue({ deleted_count: 0, cutoff: '2026-04-27T00:00:00Z' })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  test('renders loading and ordinary empty states', async () => {
    const pending = deferred<AuditEventsResponse>()
    vi.mocked(api.getAuditEvents).mockImplementationOnce(() => pending.promise)
    render(<AuditPage nodes={[node]} />)

    expect(screen.getByText('正在加载审计日志...')).toBeInTheDocument()
    await act(async () => {
      pending.resolve({ events: [], next_before_id: null })
      await pending.promise
    })
    expect(await screen.findByText('暂无审计记录')).toBeInTheDocument()
  })

  test('renders success, failure, accepted, node labels, and safe details', async () => {
    vi.mocked(api.getAuditEvents).mockResolvedValue({
      events: [
        baseEvent,
        { ...baseEvent, id: 100, request_id: 'request-100', actor_type: 'local_admin', actor_name: 'local', result: 'failure', action: 'container.delete', summary: 'remote_operation_failed' },
        { ...baseEvent, id: 99, request_id: 'request-99', actor_type: 'unauthenticated', actor_name: 'guest', result: 'accepted', module: 'agent', action: 'ssh.install' }
      ],
      next_before_id: null
    })
    render(<AuditPage nodes={[node]} />)

    const table = await screen.findByRole('table')
    expect(table).toHaveClass('min-w-[980px]')
    expect(within(table).getByText('成功')).toBeInTheDocument()
    expect(within(table).getByText('失败')).toBeInTheDocument()
    expect(within(table).getByText('已受理')).toBeInTheDocument()
    expect(within(table).getAllByText(/Oracle SG · node-1/).length).toBeGreaterThan(0)

    const detailTrigger = within(table).getByRole('button', { name: '查看审计事件 101 详情' })
    fireEvent.click(detailTrigger)
    const dialog = screen.getByRole('dialog', { name: '审计事件详情' })
    const closeButton = within(dialog).getByRole('button', { name: '关闭审计事件详情' })
    await waitFor(() => expect(closeButton).toHaveFocus())
    expect(within(dialog).getByText('request-101')).toBeInTheDocument()
    expect(within(dialog).getByText('resource_kind')).toBeInTheDocument()
    expect(within(dialog).getByText('container', { selector: 'dd' })).toBeInTheDocument()

    fireEvent.keyDown(closeButton, { key: 'Tab' })
    expect(closeButton).toHaveFocus()
    fireEvent.keyDown(closeButton, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: '审计事件详情' })).not.toBeInTheDocument()
    expect(detailTrigger).toHaveFocus()
  })

  test('sends time, module, node, result, and keyword filters and shows filtered empty state', async () => {
    render(<AuditPage nodes={[node]} />)
    await screen.findByText('暂无审计记录')

    fireEvent.change(screen.getByLabelText('时间范围'), { target: { value: '7d' } })
    fireEvent.change(screen.getByLabelText('模块'), { target: { value: 'docker' } })
    fireEvent.change(screen.getByLabelText('节点'), { target: { value: 'node-1' } })
    fireEvent.change(screen.getByLabelText('结果'), { target: { value: 'failure' } })
    fireEvent.change(screen.getByLabelText('关键词'), { target: { value: 'nginx' } })

    await waitFor(() => expect(api.getAuditEvents).toHaveBeenCalledWith(expect.objectContaining({
      limit: 50,
      module: 'docker',
      node_id: 'node-1',
      result: 'failure',
      q: 'nginx'
    }), expect.anything()))
    const matchingCall = vi.mocked(api.getAuditEvents).mock.calls.find(([query]) => query?.module === 'docker' && query.node_id === 'node-1' && query.result === 'failure' && query.q === 'nginx')
    if (!matchingCall?.[0]) {
      throw new Error('missing filtered audit request')
    }
    expect(Number.isNaN(Date.parse(matchingCall[0].from || ''))).toBe(false)
    expect(await screen.findByText('没有匹配的审计事件')).toBeInTheDocument()
  })

  test('offers task automation as an audit module', async () => {
    render(<AuditPage nodes={[node]} />)
    await screen.findByText('暂无审计记录')

    const moduleSelect = screen.getByLabelText('模块')
    expect(within(moduleSelect).getByRole('option', { name: '任务中心' })).toHaveValue('automation')
    expect(within(moduleSelect).getByRole('option', { name: '应用服务' })).toHaveValue('service')
    fireEvent.change(moduleSelect, { target: { value: 'automation' } })
    await waitFor(() => expect(api.getAuditEvents).toHaveBeenCalledWith(expect.objectContaining({ module: 'automation' }), expect.anything()))
  })

  test('loads the next keyset page without duplicating events', async () => {
    vi.mocked(api.getAuditEvents)
      .mockResolvedValueOnce({ events: [baseEvent], next_before_id: 101 })
      .mockResolvedValueOnce({ events: [baseEvent, { ...baseEvent, id: 100, request_id: 'request-100', target_name: 'redis' }], next_before_id: null })
    render(<AuditPage nodes={[node]} />)

    await screen.findByText('nginx · container-1')
    const firstQuery = vi.mocked(api.getAuditEvents).mock.calls[0]?.[0]
    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))
    expect(await screen.findByText('redis · container-1')).toBeInTheDocument()
    expect(screen.getAllByText('nginx · container-1')).toHaveLength(1)
    expect(api.getAuditEvents).toHaveBeenCalledWith(expect.objectContaining({ before_id: 101 }), expect.anything())
    const nextQuery = vi.mocked(api.getAuditEvents).mock.calls[1]?.[0]
    expect(nextQuery?.from).toBe(firstQuery?.from)
    expect(screen.getByText('已加载 2 条事件')).toBeInTheDocument()
  })

  test('rejects a stale response after filters change', async () => {
    const stale = deferred<AuditEventsResponse>()
    const currentEvent = { ...baseEvent, id: 202, request_id: 'request-202', result: 'failure' as const, target_name: 'current-filter-result' }
    vi.mocked(api.getAuditEvents)
      .mockImplementationOnce(() => stale.promise)
      .mockResolvedValueOnce({ events: [currentEvent], next_before_id: null })
    render(<AuditPage nodes={[node]} />)

    fireEvent.change(screen.getByLabelText('结果'), { target: { value: 'failure' } })
    expect(await screen.findByText('current-filter-result · container-1')).toBeInTheDocument()
    await act(async () => {
      stale.resolve({ events: [{ ...baseEvent, target_name: 'stale-result' }], next_before_id: null })
      await stale.promise
    })

    expect(screen.getByText('current-filter-result · container-1')).toBeInTheDocument()
    expect(screen.queryByText('stale-result · container-1')).not.toBeInTheDocument()
  })

  test('renders a bounded API error', async () => {
    vi.mocked(api.getAuditEvents).mockRejectedValueOnce(new Error('连接失败'))
    render(<AuditPage nodes={[node]} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('审计日志加载失败: 连接失败')
    expect(screen.queryByText('暂无审计记录')).not.toBeInTheDocument()
  })

  test('cleans old audit events through an accessible confirmation dialog and refreshes locally', async () => {
    vi.mocked(api.cleanupAuditEvents).mockResolvedValue({ deleted_count: 27, cutoff: '2026-04-30T00:00:00Z' })
    const nativeConfirm = vi.spyOn(window, 'confirm')
    render(<AuditPage nodes={[node]} />)
    await screen.findByText('暂无审计记录')

    const trigger = screen.getByRole('button', { name: '清理日志' })
    fireEvent.click(trigger)
    const dialog = screen.getByRole('dialog', { name: '清理审计日志' })
    const closeButton = within(dialog).getByRole('button', { name: '关闭清理审计日志' })
    await waitFor(() => expect(closeButton).toHaveFocus())
    expect(within(dialog).getByLabelText('保留最近天数')).toHaveValue(90)
    expect(within(dialog).getByText(/最近 24 小时始终受到保护。/)).toBeInTheDocument()
    expect(within(dialog).getByText(/本次清理操作本身会保留为新的审计事件。/)).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '确认清理' }))
    await waitFor(() => expect(api.cleanupAuditEvents).toHaveBeenCalledWith({ older_than_days: 90 }))
    expect(await screen.findByRole('alert')).toHaveTextContent('审计日志清理成功，共删除 27 条')
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '清理审计日志' })).not.toBeInTheDocument())
    expect(trigger).toHaveFocus()
    expect(api.getAuditEvents).toHaveBeenCalledTimes(2)
    expect(nativeConfirm).not.toHaveBeenCalled()
    nativeConfirm.mockRestore()
  })

  test('cleans audit events before an exact local cutoff', async () => {
    const localCutoff = '2026-04-01T08:30'
    render(<AuditPage nodes={[node]} />)
    await screen.findByText('暂无审计记录')

    fireEvent.click(screen.getByRole('button', { name: '清理日志' }))
    const dialog = screen.getByRole('dialog', { name: '清理审计日志' })
    fireEvent.change(within(dialog).getByLabelText('清理方式'), { target: { value: 'before' } })
    fireEvent.change(within(dialog).getByLabelText('截止时间'), { target: { value: localCutoff } })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认清理' }))

    await waitFor(() => expect(api.cleanupAuditEvents).toHaveBeenCalledWith({ before: new Date(localCutoff).toISOString() }))
    expect(await screen.findByRole('alert')).toHaveTextContent('审计日志清理成功，未找到符合条件的记录')
  })

  test('validates cleanup bounds and keeps the dialog open when cleanup fails', async () => {
    vi.mocked(api.cleanupAuditEvents).mockRejectedValueOnce(new Error('服务器拒绝请求'))
    render(<AuditPage nodes={[node]} />)
    await screen.findByText('暂无审计记录')

    fireEvent.click(screen.getByRole('button', { name: '清理日志' }))
    const dialog = screen.getByRole('dialog', { name: '清理审计日志' })
    const days = within(dialog).getByLabelText('保留最近天数')
    const confirm = within(dialog).getByRole('button', { name: '确认清理' })
    fireEvent.change(days, { target: { value: '0' } })
    expect(confirm).toBeDisabled()
    expect(within(dialog).getByText('请输入 1–3650 之间的整数。')).toBeInTheDocument()

    fireEvent.change(days, { target: { value: '30' } })
    fireEvent.click(confirm)
    expect(await screen.findByRole('alert')).toHaveTextContent('审计日志清理失败: 服务器拒绝请求')
    expect(screen.getByRole('dialog', { name: '清理审计日志' })).toBeInTheDocument()
  })
})
