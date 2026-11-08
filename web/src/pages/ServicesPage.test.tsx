import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import * as api from '../api/client'
import * as k8sAPI from '../api/k8s'
import type { ApplicationServiceDetail, ApplicationServiceResourceProjection, ApplicationServiceSummary } from '../types'
import { ServicesPage } from './ServicesPage'

vi.mock('../api/client', () => ({
  checkApplicationService: vi.fn(),
  createApplicationService: vi.fn(),
  deleteApplicationService: vi.fn(),
  getAlertRules: vi.fn(),
  getApplicationService: vi.fn(),
  getApplicationServices: vi.fn(),
  getK8sClusters: vi.fn(),
  getNodeDockerCompose: vi.fn(),
  getNodes: vi.fn(),
  getNodeSystemdServices: vi.fn(),
  getScheduledTasks: vi.fn(),
  getUptimeMonitors: vi.fn(),
  updateApplicationService: vi.fn()
}))

vi.mock('../api/k8s', () => ({
  fetchK8sDaemonSets: vi.fn(),
  fetchK8sDeployments: vi.fn(),
  fetchK8sStatefulSets: vi.fn()
}))

const nodeResource: ApplicationServiceResourceProjection = {
  id: 'resource-node',
  service_id: 'service-1',
  resource_type: 'node',
  scope_id: '',
  resource_kind: '',
  namespace: '',
  resource_key: 'node-1',
  display_name: 'Node One',
  health: 'healthy',
  state: 'available',
  reason: '',
  meta: { node_name: 'Node One' }
}

const baseSummary: ApplicationServiceSummary = {
  id: 'service-1',
  name: 'MizuPanel',
  description: 'Operations panel',
  health: 'healthy',
  reasons: [],
  first_reason: '',
  reason_counts: { unhealthy: 0, degraded: 0, unknown: 0 },
  resource_count: 1,
  resource_type_counts: { node: 1 },
  location_summary: 'Node One',
  resources: [nodeResource],
  created_at: '2026-07-28T00:00:00Z',
  updated_at: '2026-07-28T01:00:00Z'
}

const baseDetail: ApplicationServiceDetail = {
  ...baseSummary,
  recent_alerts: [],
  recent_tasks: [],
  recent_audit: []
}

function deferred<Value>() {
  let resolve!: (value: Value | PromiseLike<Value>) => void
  const promise = new Promise<Value>((resolvePromise) => { resolve = resolvePromise })
  return { promise, resolve }
}

function defaultProps(overrides: Partial<React.ComponentProps<typeof ServicesPage>> = {}) {
  return {
    onOpenService: vi.fn(),
    onBack: vi.fn(),
    onNavigate: vi.fn(),
    ...overrides
  }
}

describe('ServicesPage', () => {
  beforeEach(() => {
    vi.mocked(api.getApplicationServices).mockResolvedValue([])
    vi.mocked(api.getApplicationService).mockResolvedValue(baseDetail)
    vi.mocked(api.createApplicationService).mockResolvedValue(baseDetail)
    vi.mocked(api.updateApplicationService).mockResolvedValue(baseDetail)
    vi.mocked(api.deleteApplicationService).mockResolvedValue(undefined)
    vi.mocked(api.getNodes).mockResolvedValue({ nodes: [{
      id: 'node-1', name: 'Node One', hostname: 'node-one', ip: '10.0.0.1', os: 'linux', arch: 'amd64', kernel: '6.8', agent_version: '0.1.10', status: 'online', last_seen_at: '2026-07-28T00:00:00Z'
    }] })
    vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [{ id: 11, name: 'Panel Health', target: 'http://panel/health' }] } as never)
    vi.mocked(api.getAlertRules).mockResolvedValue({ rules: [{ id: 12, name: 'CPU Alert', enabled: true }] } as never)
    vi.mocked(api.getScheduledTasks).mockResolvedValue({ tasks: [{ id: 13, name: 'Cleanup', cron_expression: '0 * * * *' }] } as never)
    vi.mocked(api.getK8sClusters).mockResolvedValue({ clusters: [{ id: 'cluster-1', name: 'Cluster One' }] } as never)
    vi.mocked(api.getNodeDockerCompose).mockResolvedValue({ success: true, supported: true, projects: [{ name: 'panel', display_name: 'Panel Stack', management: 'external', services: [] }] })
    vi.mocked(api.getNodeSystemdServices).mockResolvedValue({ success: true, supported: true, services: [{ name: 'mizupanel.service', description: 'MizuPanel Service' }] })
    vi.mocked(k8sAPI.fetchK8sDeployments).mockResolvedValue({ success: true, deployments: [{ name: 'panel-api', namespace: 'default', ready: '1/1', up_to_date: 1, available: 1, age: '1d' }] })
    vi.mocked(k8sAPI.fetchK8sStatefulSets).mockResolvedValue({ success: true, statefulsets: [] })
    vi.mocked(k8sAPI.fetchK8sDaemonSets).mockResolvedValue({ success: true, daemonsets: [] })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  test('keeps name order stable instead of moving unhealthy services first', async () => {
    vi.mocked(api.getApplicationServices).mockResolvedValue([
      { ...baseSummary, id: 'b', name: 'Beta', health: 'unhealthy' },
      { ...baseSummary, id: 'a', name: 'Alpha', health: 'healthy' }
    ])
    render(<ServicesPage {...defaultProps()} />)
    const table = await screen.findByRole('table', { name: '应用服务列表' })
    const rows = within(table).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('Alpha')
    expect(rows[2]).toHaveTextContent('Beta')
  })

  test('retains a loaded list and distinguishes refresh failure from an empty list', async () => {
    vi.mocked(api.getApplicationServices).mockResolvedValueOnce([baseSummary]).mockRejectedValueOnce(new Error('refresh failed'))
    render(<ServicesPage {...defaultProps()} />)
    await screen.findByText('MizuPanel')
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('refresh failed')
    expect(screen.getByText('MizuPanel')).toBeInTheDocument()
    expect(screen.queryByText('还没有应用服务')).not.toBeInTheDocument()
  })

  test('filters the entire supplied event set before limiting to eight', async () => {
    vi.mocked(api.getApplicationService).mockResolvedValue({ ...baseDetail,
      recent_tasks: Array.from({ length: 10 }, (_, index) => ({ id: index + 1, task_id: 1, task_name: `Run ${index}`, script_name: '', status: 'success', trigger: 'manual', created_at: `2026-11-08T12:${String(index).padStart(2, '0')}:00Z`, completed_at: null })),
      recent_alerts: [{ id: 88, rule_id: 1, rule_name: 'Older warning', node_id: 'node-1', node_name: 'Node One', metric_field: 'cpu', metric_value: 95, triggered_at: '2026-11-07T10:00:00Z', resolved_at: null }]
    })
    render(<ServicesPage {...defaultProps({ serviceID: 'service-1' })} />)
    const events = await screen.findByRole('region', { name: '近期事件' })
    expect(within(events).getAllByRole('listitem')).toHaveLength(8)
    fireEvent.click(within(events).getByRole('button', { name: '告警' }))
    expect(within(events).getAllByRole('listitem')).toHaveLength(1)
    expect(within(events).getByText('Older warning')).toBeInTheDocument()
  })

  test('preserves list filters and scroll position after returning from detail', async () => {
    vi.mocked(api.getApplicationServices).mockResolvedValue([baseSummary])
    const props = defaultProps()
    const view = render(<ServicesPage {...props} />)
    await screen.findByText('MizuPanel')
    fireEvent.change(screen.getByRole('textbox', { name: '搜索应用服务' }), { target: { value: 'Panel' } })
    fireEvent.click(screen.getByRole('button', { name: '健康 1' }))
    const scroll = screen.getByRole('table', { name: '应用服务列表' }).parentElement!
    scroll.scrollTop = 120
    fireEvent.scroll(scroll)
    view.rerender(<ServicesPage {...props} serviceID="service-1" />)
    await screen.findByRole('region', { name: '关联资源' })
    view.rerender(<ServicesPage {...props} />)
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新' })).toBeEnabled())
    expect(screen.getByRole('textbox', { name: '搜索应用服务' })).toHaveValue('Panel')
    expect(screen.getByRole('button', { name: '健康 1' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('table', { name: '应用服务列表' }).parentElement?.scrollTop).toBe(120)
  })

  test('guards unsaved edits across close, Escape and registered navigation', async () => {
    const register = vi.fn()
    render(<ServicesPage {...defaultProps({ serviceID: 'service-1', onRegisterLeaveGuard: register })} />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑应用服务' })
    fireEvent.change(within(dialog).getByRole('textbox', { name: '服务名称' }), { target: { value: 'Unsaved service' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '关闭编辑器' }))
    expect(screen.getByRole('dialog', { name: '未保存的修改' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '继续编辑' }))
    expect(within(dialog).getByRole('textbox', { name: '服务名称' })).toHaveValue('Unsaved service')
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.getByRole('dialog', { name: '未保存的修改' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '继续编辑' }))
    const leave = vi.fn()
    const guard = register.mock.calls[register.mock.calls.length - 1][0]
    act(() => guard(leave))
    expect(leave).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '丢弃并离开' }))
    expect(leave).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog', { name: '编辑应用服务' })).not.toBeInTheDocument()
    expect(api.updateApplicationService).not.toHaveBeenCalled()
  })

  test('keeps an existing association when its catalog fails and retries independently', async () => {
    vi.mocked(api.getNodes).mockRejectedValueOnce(new Error('catalog offline'))
    render(<ServicesPage {...defaultProps({ serviceID: 'service-1' })} />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑应用服务' })
    const picker = within(dialog).getByRole('region', { name: '关联资源选择器' })
    expect(await within(picker).findByRole('alert')).toHaveTextContent('catalog offline')
    expect(within(dialog).getByRole('region', { name: '已选择资源' })).toHaveTextContent('Node One')
    fireEvent.click(within(picker).getByRole('button', { name: '重试' }))
    expect(await within(picker).findByRole('checkbox')).toBeChecked()
    fireEvent.click(within(dialog).getByRole('button', { name: '保存服务' }))
    await waitFor(() => expect(api.updateApplicationService).toHaveBeenCalledWith('service-1', expect.objectContaining({ resources: [expect.objectContaining({ resource_key: 'node-1' })] })))
  })

  test('does not lose selections when switching resource types and searches', async () => {
    render(<ServicesPage {...defaultProps()} />)
    fireEvent.click(await screen.findByRole('button', { name: '创建第一个服务' }))
    const dialog = await screen.findByRole('dialog', { name: '创建应用服务' })
    const picker = within(dialog).getByRole('region', { name: '关联资源选择器' })
    fireEvent.click(await within(picker).findByRole('checkbox'))
    fireEvent.change(within(picker).getByRole('textbox', { name: '搜索可选资源' }), { target: { value: 'does-not-match' } })
    expect(within(picker).queryByRole('checkbox')).not.toBeInTheDocument()
    fireEvent.click(within(picker).getByRole('button', { name: '服务拨测' }))
    expect(within(picker).getByRole('textbox', { name: '搜索可选资源' })).toHaveValue('')
    fireEvent.click(await within(picker).findByRole('checkbox'))
    expect(within(dialog).getByRole('region', { name: '已选择资源' })).toHaveTextContent('Node One')
    expect(within(dialog).getByRole('region', { name: '已选择资源' })).toHaveTextContent('Panel Health')
    fireEvent.click(within(picker).getByRole('button', { name: '主机' }))
    expect(within(picker).getByRole('checkbox')).toBeChecked()
  })

  test('ignores a previous scope result and aborts catalog reads on close', async () => {
    const pending = deferred<Awaited<ReturnType<typeof api.getNodeDockerCompose>>>()
    vi.mocked(api.getNodes).mockResolvedValue({ nodes: [
      { id: 'node-1', name: 'Node One', hostname: 'one', status: 'online' },
      { id: 'node-2', name: 'Node Two', hostname: 'two', status: 'online' }
    ] } as never)
    vi.mocked(api.getNodeDockerCompose).mockImplementation(node => node === 'node-1' ? pending.promise : Promise.resolve({ success: true, supported: true, projects: [{ name: 'new', services: [] }] }))
    render(<ServicesPage {...defaultProps()} />)
    fireEvent.click(await screen.findByRole('button', { name: '创建第一个服务' }))
    const dialog = await screen.findByRole('dialog', { name: '创建应用服务' })
    const picker = within(dialog).getByRole('region', { name: '关联资源选择器' })
    fireEvent.click(within(picker).getByRole('button', { name: 'Compose' }))
    await waitFor(() => expect(within(picker).getByRole('button', { name: '加载' })).toBeEnabled())
    fireEvent.click(within(picker).getByRole('button', { name: '加载' }))
    const signal = vi.mocked(api.getNodeDockerCompose).mock.calls[0][1]
    fireEvent.change(within(picker).getByLabelText('资源主机'), { target: { value: 'node-2' } })
    fireEvent.click(within(picker).getByRole('button', { name: '加载' }))
    await within(picker).findByText('new')
    await act(async () => pending.resolve({ success: true, supported: true, projects: [{ name: 'old', services: [] }] }))
    expect(within(picker).queryByText('old')).not.toBeInTheDocument()
    // A fresh pending read is cancelled, not left running when the editor closes.
    vi.mocked(api.getNodeDockerCompose).mockReturnValue(new Promise(() => {}))
    fireEvent.click(within(picker).getByRole('button', { name: '刷新资源' }))
    const calls = vi.mocked(api.getNodeDockerCompose).mock.calls
    const activeSignal = calls[calls.length - 1][1]
    fireEvent.click(within(dialog).getByRole('button', { name: '关闭编辑器' }))
    expect(activeSignal?.aborted).toBe(true)
    expect(signal?.aborted).toBe(false)
  })

  test('late save responses never replace a different service', async () => {
    const pending = deferred<ApplicationServiceDetail>()
    vi.mocked(api.updateApplicationService).mockReturnValue(pending.promise)
    const props = defaultProps({ serviceID: 'service-1' })
    const view = render(<ServicesPage {...props} />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    fireEvent.click(screen.getByRole('button', { name: '保存服务' }))
    vi.mocked(api.getApplicationService).mockResolvedValue({ ...baseDetail, id: 'service-2', name: 'New target' })
    view.rerender(<ServicesPage {...props} serviceID="service-2" />)
    await screen.findByRole('heading', { name: 'New target' })
    await act(async () => pending.resolve({ ...baseDetail, name: 'Old saved target' }))
    expect(screen.queryByText('Old saved target')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'New target' })).toBeInTheDocument()
  })

  test('save failure keeps the draft and unsaved protection instead of closing the editor', async () => {
    vi.mocked(api.updateApplicationService).mockRejectedValueOnce(new Error('write failed'))
    render(<ServicesPage {...defaultProps({ serviceID: 'service-1' })} />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑应用服务' })
    fireEvent.change(within(dialog).getByRole('textbox', { name: '服务名称' }), { target: { value: 'Keep my draft' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存服务' }))
    expect(await screen.findByText('应用服务更新失败: write failed')).toBeInTheDocument()
    expect(within(dialog).getByRole('textbox', { name: '服务名称' })).toHaveValue('Keep my draft')
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
    expect(screen.getByRole('dialog', { name: '未保存的修改' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '继续编辑' }))
    expect(api.updateApplicationService).toHaveBeenCalledTimes(1)
  })

  test('keeps catalog loading independent and distinguishes unsupported remote reads from empty', async () => {
    vi.mocked(api.getUptimeMonitors).mockReturnValue(new Promise(() => {}))
    vi.mocked(api.getNodeDockerCompose).mockResolvedValue({ success: false, supported: false, projects: [], error: 'Docker unavailable' })
    render(<ServicesPage {...defaultProps()} />)
    fireEvent.click(await screen.findByRole('button', { name: '创建第一个服务' }))
    const dialog = await screen.findByRole('dialog', { name: '创建应用服务' })
    const picker = within(dialog).getByRole('region', { name: '关联资源选择器' })
    expect(await within(picker).findByRole('checkbox')).toBeEnabled()
    fireEvent.click(within(picker).getByRole('button', { name: 'Compose' }))
    fireEvent.click(within(picker).getByRole('button', { name: '加载' }))
    expect(await within(picker).findByRole('alert')).toHaveTextContent('Docker unavailable')
    expect(within(picker).queryByText('当前范围没有可关联的资源。')).not.toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '关闭编辑器' }))
  })

  test('does not start creating a service while the initial list read is unresolved', async () => {
    const pending = deferred<ApplicationServiceSummary[]>()
    vi.mocked(api.getApplicationServices).mockReturnValueOnce(pending.promise)
    render(<ServicesPage {...defaultProps()} />)
    expect(screen.getByRole('button', { name: '创建服务' })).toBeDisabled()
    await act(async () => pending.resolve([]))
    expect(screen.getByRole('button', { name: '创建服务' })).toBeEnabled()
  })

  test('renders a clean empty state and keeps an initial error distinct from empty', async () => {
    const props = defaultProps()
    const { unmount } = render(<ServicesPage {...props} />)
    expect(await screen.findByText('还没有应用服务')).toBeInTheDocument()
    unmount()

    vi.mocked(api.getApplicationServices).mockRejectedValueOnce(new Error('连接失败'))
    render(<ServicesPage {...defaultProps()} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('连接失败')
    expect(screen.queryByText('还没有应用服务')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
  })

  test('prepares a service-scoped diagnosis and reports inconclusive verification honestly', async () => {
    const onDiagnose = vi.fn()
    vi.mocked(api.checkApplicationService).mockResolvedValue({
      service: baseDetail, status: 'inconclusive', checks: [],
      started_at: '2026-11-07T12:00:00Z', checked_at: '2026-11-07T12:00:01Z',
      summary: '未关联拨测，尚不能确认恢复'
    })
    render(<ServicesPage {...defaultProps({ serviceID: 'service-1', onDiagnose })} />)
    const workspace = await screen.findByRole('region', { name: '故障工作台' })
    fireEvent.click(within(workspace).getByRole('button', { name: 'AI 诊断' }))
    expect(onDiagnose).toHaveBeenCalledWith(baseDetail)
    fireEvent.click(within(workspace).getByRole('button', { name: '复查健康' }))
    expect(await within(workspace).findByText('证据不足')).toBeInTheDocument()
    expect(within(workspace).getByText('未关联拨测，尚不能确认恢复')).toBeInTheDocument()
    expect(api.checkApplicationService).toHaveBeenCalledWith('service-1', expect.any(AbortSignal))
    expect(within(workspace).queryByText('当前健康')).not.toBeInTheDocument()
  })

  test('ignores a late verification after navigating to a different service', async () => {
    const pending = deferred<Awaited<ReturnType<typeof api.checkApplicationService>>>()
    vi.mocked(api.checkApplicationService).mockReturnValue(pending.promise)
    const props = defaultProps({ serviceID: 'service-1' })
    const view = render(<ServicesPage {...props} />)
    fireEvent.click(await screen.findByRole('button', { name: '复查健康' }))
    const signal = vi.mocked(api.checkApplicationService).mock.calls[0][1]
    vi.mocked(api.getApplicationService).mockResolvedValue({ ...baseDetail, id: 'service-2', name: 'Other Service' })
    view.rerender(<ServicesPage {...props} serviceID="service-2" />)
    await screen.findByRole('heading', { name: 'Other Service' })
    expect(signal?.aborted).toBe(true)
    await act(async () => pending.resolve({ service: baseDetail, status: 'healthy', checks: [], started_at: '', checked_at: '', summary: 'old-result' }))
    expect(screen.queryByText('old-result')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Other Service' })).toBeInTheDocument()
  })

  test('does not let an in-flight health check overwrite a saved service', async () => {
    const pending = deferred<Awaited<ReturnType<typeof api.checkApplicationService>>>()
    vi.mocked(api.checkApplicationService).mockReturnValue(pending.promise)
    const updated = { ...baseDetail, name: 'Updated Service', resources: [], resource_count: 0 }
    vi.mocked(api.updateApplicationService).mockResolvedValue(updated)
    render(<ServicesPage {...defaultProps({ serviceID: 'service-1' })} />)
    fireEvent.click(await screen.findByRole('button', { name: '复查健康' }))
    const signal = vi.mocked(api.checkApplicationService).mock.calls[0][1]
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑应用服务' })
    await waitFor(() => expect(within(dialog).queryByText('正在加载资源目录')).not.toBeInTheDocument())
    fireEvent.change(within(dialog).getByPlaceholderText('例如：MizuPanel'), { target: { value: updated.name } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存服务' }))
    await screen.findByRole('heading', { name: updated.name })
    await act(async () => pending.resolve({ service: baseDetail, status: 'healthy', checks: [], started_at: '', checked_at: '', summary: 'stale-check-result' }))
    expect(screen.getByRole('heading', { name: updated.name })).toBeInTheDocument()
    expect(screen.queryByText('stale-check-result')).not.toBeInTheDocument()
    expect(signal?.aborted).toBe(true)
  })

  test('merges recent events by time including recovery without asserting a cause', async () => {
    const onNavigate = vi.fn()
    vi.mocked(api.getApplicationService).mockResolvedValue({ ...baseDetail,
      recent_alerts: [{ id: 1, rule_id: 1, rule_name: 'CPU spike', node_id: 'node-1', node_name: 'Node One', metric_field: 'cpu', metric_value: 95, triggered_at: '2026-11-07T10:00:00Z', resolved_at: '2026-11-07T10:10:00Z' }],
      recent_audit: [{ id: 2, created_at: '2026-11-07T10:05:00Z', actor_type: 'admin', actor_name: 'admin', module: 'docker', action: 'restart', target_type: 'container', target_id: 'c1', target_name: 'worker', node_id: 'node-1', result: 'success', summary: 'Restarted worker', metadata: {} }]
    })
    render(<ServicesPage {...defaultProps({ serviceID: 'service-1', onNavigate })} />)
    const workspace = await screen.findByRole('region', { name: '近期事件' })
    const items = within(workspace).getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('告警恢复')
    expect(items[1]).toHaveTextContent('Restarted worker')
    expect(items[2]).toHaveTextContent('告警触发')
    expect(workspace).toHaveTextContent('时间接近不代表存在因果关系')
    fireEvent.click(within(workspace).getByRole('button', { name: '查看告警触发：CPU spike' }))
    // Historical triggers that recovered must not enter the active-only view.
    expect(onNavigate).toHaveBeenCalledWith('/alerts?node=node-1')
  })

  test('filters by health and searches resource names', async () => {
    const unhealthy = {
      ...baseSummary,
      id: 'service-2',
      name: 'Worker',
      health: 'unhealthy' as const,
      first_reason: '节点离线',
      resources: [{ ...nodeResource, id: 'resource-worker', display_name: 'Edge Worker', health: 'unhealthy' as const, reason: '节点离线' }]
    }
    vi.mocked(api.getApplicationServices).mockResolvedValue([baseSummary, unhealthy])
    render(<ServicesPage {...defaultProps()} />)

    expect(await screen.findByText('MizuPanel')).toBeInTheDocument()
    expect(screen.getByText('Worker')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '异常 1' }))
    expect(screen.queryByText('MizuPanel')).not.toBeInTheDocument()
    expect(screen.getByText('Worker')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '全部 2' }))
    fireEvent.change(screen.getByPlaceholderText('搜索服务、描述、资源或部署位置'), { target: { value: 'Edge Worker' } })
    expect(screen.queryByText('MizuPanel')).not.toBeInTheDocument()
    expect(screen.getByText('Worker')).toBeInTheDocument()
  })

  test('rejects a stale list response after navigating to detail', async () => {
    const stale = deferred<ApplicationServiceSummary[]>()
    vi.mocked(api.getApplicationServices)
      .mockResolvedValueOnce([baseSummary])
      .mockImplementationOnce(() => stale.promise)
    const props = defaultProps()
    const view = render(<ServicesPage {...props} />)
    await screen.findByText('MizuPanel')
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(api.getApplicationServices).toHaveBeenCalledTimes(2))

    view.rerender(<ServicesPage {...props} serviceID="service-1" />)
    expect(await screen.findByText('关联资源')).toBeInTheDocument()
    await act(async () => {
      stale.resolve([{ ...baseSummary, name: 'Stale Service' }])
      await stale.promise
    })
    expect(screen.queryByText('Stale Service')).not.toBeInTheDocument()
    expect(screen.getByText('关联资源')).toBeInTheDocument()
  })

  test('opens original resource links and uses a custom delete confirmation', async () => {
    const onNavigate = vi.fn()
    const onBack = vi.fn()
    render(<ServicesPage {...defaultProps({ serviceID: 'service-1', onNavigate, onBack })} />)
    await screen.findByText('关联资源')
    fireEvent.click(screen.getByRole('button', { name: '打开 Node One 的原管理入口' }))
    expect(onNavigate).toHaveBeenCalledWith('/nodes/node-1')

    fireEvent.click(screen.getByRole('button', { name: '更多服务操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '删除服务' }))
    const dialog = screen.getByRole('dialog', { name: '删除应用服务？' })
    expect(within(dialog).getByText(/不会删除任何节点/)).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(api.deleteApplicationService).toHaveBeenCalledWith('service-1'))
    expect(await screen.findByText('应用服务删除成功')).toBeInTheDocument()
    expect(onBack).toHaveBeenCalled()
  })

  test('selects all seven resource types with lazy remote loaders and creates a service', async () => {
    const onOpenService = vi.fn()
    render(<ServicesPage {...defaultProps({ onOpenService })} />)
    await screen.findByText('还没有应用服务')
    fireEvent.click(screen.getByRole('button', { name: '创建第一个服务' }))
    const dialog = await screen.findByRole('dialog', { name: '创建应用服务' })
    await waitFor(() => expect(within(dialog).queryByText('正在加载资源目录')).not.toBeInTheDocument())

    fireEvent.change(within(dialog).getByPlaceholderText('例如：MizuPanel'), { target: { value: 'Unified Service' } })
    const picker = within(dialog).getByRole('region', { name: '关联资源选择器' })
    for (const [type, label] of [['主机', 'Node One'], ['服务拨测', 'Panel Health'], ['告警规则', 'CPU Alert'], ['计划任务', 'Cleanup']] as const) {
      fireEvent.click(within(picker).getByRole('button', { name: type }))
      const option = await within(picker).findByText(label)
      fireEvent.click(within(option.closest('label') as HTMLElement).getByRole('checkbox'))
    }
    for (const [type, label] of [['Compose', 'Panel Stack'], ['Systemd', 'MizuPanel Service'], ['K8s 工作负载', 'panel-api']] as const) {
      fireEvent.click(within(picker).getByRole('button', { name: type }))
      fireEvent.click(within(picker).getByRole('button', { name: '加载' }))
      const option = await within(picker).findByText(label)
      fireEvent.click(within(option.closest('label') as HTMLElement).getByRole('checkbox'))
    }

    fireEvent.click(within(dialog).getByRole('button', { name: '创建服务' }))
    await waitFor(() => expect(api.createApplicationService).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Unified Service',
      resources: expect.arrayContaining([
        expect.objectContaining({ resource_type: 'node' }),
        expect.objectContaining({ resource_type: 'compose_project' }),
        expect.objectContaining({ resource_type: 'systemd_service' }),
        expect.objectContaining({ resource_type: 'k8s_workload' }),
        expect.objectContaining({ resource_type: 'uptime_monitor' }),
        expect.objectContaining({ resource_type: 'alert_rule' }),
        expect.objectContaining({ resource_type: 'scheduled_task' })
      ])
    })))
    const submitted = vi.mocked(api.createApplicationService).mock.calls[0]?.[0]
    expect(submitted?.resources).toHaveLength(7)
    expect(await screen.findByText('应用服务创建成功')).toBeInTheDocument()
    expect(onOpenService).toHaveBeenCalledWith('service-1')
  })
})
