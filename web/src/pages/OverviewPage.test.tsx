import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import * as api from '../api/client'
import type { AlertHistory, AutomationRun, K8sCluster, Node } from '../types'
import { OverviewPage } from './OverviewPage'

vi.mock('../api/client', () => ({ getActiveAlerts: vi.fn(), getApplicationServices: vi.fn(), getAutomationRuns: vi.fn(), getK8sClusters: vi.fn(), getUptimeMonitors: vi.fn() }))
const time = () => new Date().toISOString()
const nodes: Node[] = [
  { id: 'node-b', name: 'Bravo', hostname: 'b', ip: '192.0.2.2', os: 'linux', arch: 'amd64', kernel: '', agent_version: '', status: 'offline', last_seen_at: '' },
  { id: 'node-a', name: 'Alpha', hostname: 'a', ip: '192.0.2.1', os: 'linux', arch: 'amd64', kernel: '', agent_version: '', status: 'online', last_seen_at: '' }
]
const makeProps = () => ({ nodes, nodesUpdatedAt: Date.now(), onRefreshNodes: vi.fn(async () => {}), onAddServer: vi.fn(), onNavigate: vi.fn() })
const activeAlert = (): AlertHistory => ({ id: 1, rule_id: 1, rule_name: 'CPU High', node_id: 'node-b', node_name: 'Bravo', metric_field: 'cpu_usage', metric_value: 95, threshold: 80, triggered_at: '2020-01-01T00:00:00Z', notification_sent: false, created_at: time() })
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.getActiveAlerts).mockResolvedValue({ history: [] })
  vi.mocked(api.getApplicationServices).mockResolvedValue([])
  vi.mocked(api.getK8sClusters).mockResolvedValue({ clusters: [] })
  vi.mocked(api.getUptimeMonitors).mockResolvedValue({ monitors: [] })
  vi.mocked(api.getAutomationRuns).mockResolvedValue({ runs: [], next_before_id: null })
})
afterEach(() => { cleanup(); vi.useRealTimers() })

describe('operations overview', () => {
  test('shows operational evidence and stable shortcuts without automatically selecting a host', async () => {
    const props = makeProps()
    vi.mocked(api.getActiveAlerts).mockResolvedValue({ history: [activeAlert()] })
    render(<OverviewPage {...props} />)
    expect(await screen.findByText(/CPU High/)).toBeInTheDocument()
    const summary = screen.getByRole('region', { name: '当前状态摘要' })
    expect(within(summary).getByRole('button', { name: /未恢复告警/ })).toHaveTextContent('1')
    expect(screen.queryByText('平均资源使用率')).not.toBeInTheDocument()
    const shortcuts = screen.getByRole('region', { name: '主机快捷入口' })
    const buttons = within(shortcuts).getAllByRole('button').filter(button => ['Alpha', 'Bravo'].includes(button.textContent ?? ''))
    expect(buttons.map(button => button.textContent)).toEqual(['Alpha', 'Bravo'])
    expect(props.onNavigate).not.toHaveBeenCalled()
    fireEvent.click(buttons[0])
    expect(props.onNavigate).toHaveBeenCalledWith('/nodes/node-a')
    fireEvent.click(screen.getByRole('button', { name: '查看告警' }))
    expect(props.onNavigate).toHaveBeenLastCalledWith('/alerts?status=active&node=node-b')
  })
  test('keeps other evidence visible when one source fails, never reporting it as zero or all healthy', async () => {
    vi.mocked(api.getActiveAlerts).mockRejectedValue(new Error('告警源不可达'))
    render(<OverviewPage {...makeProps()} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('告警源不可达')
    expect(screen.getByRole('button', { name: /未恢复告警/ })).toHaveTextContent('—')
    expect(screen.getByText('Agent 离线')).toBeInTheDocument()
    expect(screen.queryByText('已获取的数据中暂无需要关注的问题。')).not.toBeInTheDocument()
  })
  test('shows unconfigured and missing data honestly without fake faults', async () => {
    render(<OverviewPage {...makeProps()} nodes={[]} />)
    expect(await screen.findByText('尚未接入资源，可先添加主机。')).toBeInTheDocument()
    expect(screen.getByText('尚未配置或启用拨测')).toBeInTheDocument()
    expect(screen.getByText('最近 24 小时没有执行记录。')).toBeInTheDocument()
  })
  test('retains Agent/API joint health checks and stale polling guards', async () => {
    const first = deferred<{ clusters: K8sCluster[] }>()
    vi.mocked(api.getK8sClusters).mockReturnValueOnce(first.promise)
    render(<OverviewPage {...makeProps()} />)
    await screen.findByText('最近 24 小时没有执行记录。')
    const cluster = { id: 'cluster/a', name: 'API offline', node_status: 'offline', status: 'online', health_checked_at: time() } as K8sCluster
    vi.mocked(api.getK8sClusters).mockResolvedValue({ clusters: [cluster] })
    fireEvent.click(screen.getByRole('button', { name: '刷新概览' }))
    expect(await screen.findByText('API offline')).toBeInTheDocument()
    await act(async () => { first.resolve({ clusters: [] }); await Promise.resolve() })
    expect(screen.getByText('API offline')).toBeInTheDocument()
  })
  test('requests only the recent 24-hour run window and links to the exact run, not an invented total', async () => {
    const props = makeProps()
    const run = { id: 7, task_name: 'Nightly', script_name: 'check', trigger: 'scheduled', status: 'failed', total_targets: 2, created_at: time() } as AutomationRun
    vi.mocked(api.getAutomationRuns).mockResolvedValue({ runs: [run, { ...run, id: 6, task_name: 'Old', created_at: '2020-01-01T00:00:00Z' }], next_before_id: 6 })
    render(<OverviewPage {...props} />)
    expect(await screen.findByText('Nightly')).toBeInTheDocument()
    expect(screen.queryByText('Old')).not.toBeInTheDocument()
    const query = vi.mocked(api.getAutomationRuns).mock.calls[0][0]!
    expect(query.limit).toBe(5)
    expect(Date.parse(query.to!) - Date.parse(query.from!)).toBe(86400_000)
    fireEvent.click(screen.getByRole('button', { name: '查看详情' }))
    expect(props.onNavigate).toHaveBeenCalledWith('/tasks?tab=runs&range=24h&run=7')
  })
  test('does not replace a known source snapshot with an empty list on a later failure', async () => {
    vi.mocked(api.getActiveAlerts).mockResolvedValue({ history: [activeAlert()] })
    render(<OverviewPage {...makeProps()} />)
    await screen.findByText(/CPU High/)
    vi.mocked(api.getActiveAlerts).mockRejectedValue(new Error('refresh unavailable'))
    fireEvent.click(screen.getByRole('button', { name: '刷新概览' }))
    await waitFor(() => expect(screen.getAllByRole('alert').some(el => el.textContent?.includes('refresh unavailable'))).toBe(true))
    expect(screen.getByText(/CPU High.*上次结果/)).toBeInTheDocument()
  })
  test('aborts reads on unmount and ignores the late response', async () => {
    const pending = deferred<{ clusters: K8sCluster[] }>()
    vi.mocked(api.getK8sClusters).mockReturnValue(pending.promise)
    const view = render(<OverviewPage {...makeProps()} />)
    const signal = vi.mocked(api.getK8sClusters).mock.calls[0][0]
    view.unmount()
    expect(signal?.aborted).toBe(true)
    await act(async () => { pending.resolve({ clusters: [] }) })
    expect(screen.queryByRole('heading', { name: '概览' })).not.toBeInTheDocument()
  })
})
