import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import * as api from '../api/client'
import * as k8sAPI from '../api/k8s'
import type { ApplicationServiceDetail, ApplicationServiceResourceProjection, ApplicationServiceSummary } from '../types'
import { ServicesPage } from './ServicesPage'

vi.mock('../api/client', () => ({
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
    fireEvent.click(screen.getByRole('button', { name: /1异常/ }))
    expect(screen.queryByText('MizuPanel')).not.toBeInTheDocument()
    expect(screen.getByText('Worker')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /2全部/ }))
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

    fireEvent.click(screen.getByRole('button', { name: '删除' }))
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
    for (const [sectionTitle, label] of [['节点', 'Node One'], ['服务拨测', 'Panel Health'], ['告警规则', 'CPU Alert'], ['计划任务', 'Cleanup']] as const) {
      const section = within(dialog).getByText(sectionTitle).closest('details') as HTMLElement
      const option = within(section).getByText(label).closest('label')
      expect(option).not.toBeNull()
      fireEvent.click(within(option as HTMLElement).getByRole('checkbox'))
    }

    const composeSection = within(dialog).getByText('Compose 项目').closest('details') as HTMLElement
    fireEvent.click(within(composeSection).getByRole('button', { name: '加载' }))
    const composeOption = await within(composeSection).findByText('Panel Stack')
    fireEvent.click(within(composeOption.closest('label') as HTMLElement).getByRole('checkbox'))

    const systemdSection = within(dialog).getByText('Systemd 服务').closest('details') as HTMLElement
    fireEvent.click(within(systemdSection).getByRole('button', { name: '加载' }))
    const systemdOption = await within(systemdSection).findByText('MizuPanel Service')
    fireEvent.click(within(systemdOption.closest('label') as HTMLElement).getByRole('checkbox'))

    const k8sSection = within(dialog).getByText('Kubernetes 工作负载').closest('details') as HTMLElement
    fireEvent.click(within(k8sSection).getByRole('button', { name: '加载' }))
    const k8sOption = await within(k8sSection).findByText('panel-api')
    fireEvent.click(within(k8sOption.closest('label') as HTMLElement).getByRole('checkbox'))

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
