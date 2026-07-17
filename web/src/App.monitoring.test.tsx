import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import App from './App'
import { getAgentLogs, getAgentStatus, getNodeDocker, getNodeMetrics, getNodeProcesses, getNodes, restartAgent } from './api/client'
import type { DockerSnapshotResponse, MetricsResponse, NodesResponse, ProcessSnapshotResponse } from './types'

vi.mock('./api/client', () => ({
  setUnauthorizedHandler: vi.fn(),
  getAuthSession: vi.fn(async () => ({ auth_enabled: false, authenticated: true, username: '' })),
  createInstallCommand: vi.fn(async () => ({ command: 'install', install_token: 'token' })),
  getNodes: vi.fn(),
  getNodeMetrics: vi.fn(),
  getNodeProcesses: vi.fn(),
  getNodeDocker: vi.fn(),
  getAgentStatus: vi.fn(async () => ({ version: '0.1.0', user: 'root', mode: 'ops', terminal_enabled: true, docker_available: true, service_name: 'mizupanel-agent', uptime: 3600, collected_at: 1710000000 })),
  restartAgent: vi.fn(async () => ({ accepted: true, message: '重启命令已下发，等待 Agent 重新连接' })),
  getAgentLogs: vi.fn(async () => ({ lines: 100, content: 'mizupanel-agent started', collected_at: 1710000001 })),
  getSettings: vi.fn(async () => ({ metrics_retention: '6h', metrics_retention_seconds: 21600, max_metrics_retention: '7d' })),
  updateSettings: vi.fn(async () => ({ metrics_retention: '6h', metrics_retention_seconds: 21600, max_metrics_retention: '7d' })),
  getNodeFiles: vi.fn(async () => ({ path: '/', entries: [] })),
  readNodeFile: vi.fn(async () => ({ path: '/tmp/a', content: '', editable: true })),
  writeNodeFile: vi.fn(async () => ({ path: '/tmp/a', saved: true })),
  uploadNodeFile: vi.fn(async () => ({ path: '/tmp/upload.bin', uploaded: true })),
  deleteNodePath: vi.fn(async () => ({ path: '/tmp/upload.bin', deleted: true })),
  deleteNode: vi.fn(async () => undefined),
  rebootNode: vi.fn(async () => ({ accepted: true })),
  getConnectionDiagnostics: vi.fn(async () => ({ node_id: 'node-1', online: true, health: 'healthy', agent_version: '0.1.0', protocol_version: 1, identity_conflict: false, upgrade_supported: false, latest_version: '0.1.1', upgrade_available: true, events: [] })),
  upgradeAgent: vi.fn(async () => ({ accepted: true, stage: 'preparing' })),
  getAgentUpgradeStatus: vi.fn(async () => ({ node_id: 'node-1', target_version: '0.1.1', actual_version: '0.1.1', stage: 'completed' })),
  createTerminalSession: vi.fn(async () => ({ token: 'terminal-token' })),
  createContainerExecSession: vi.fn(async () => ({ token: 'exec-token' })),
  startSSHInstall: vi.fn(async () => ({ job_id: 'ssh-install-1' })),
  startSSHUninstall: vi.fn(async () => ({ job_id: 'ssh-uninstall-1' }))
}))

const nodesResponse: NodesResponse = {
  nodes: [
    {
      id: 'node-1',
      name: 'Oracle SG',
      hostname: 'oracle-sg',
      ip: '10.0.0.1',
      os: 'linux',
      arch: 'arm64',
      kernel: '6.6',
      agent_version: '0.1.0',
      status: 'online',
      last_seen_at: '2026-05-24T10:00:00Z',
      latest_metric: {
        id: 1,
        node_id: 'node-1',
        cpu_usage: 42,
        cpu_cores: 4,
        memory_total: 1024,
        memory_used: 512,
        memory_usage: 50,
        disk_total: 2048,
        disk_used: 1024,
        disk_usage: 50,
        rx_speed: 100,
        tx_speed: 200,
        rx_total: 1000,
        tx_total: 2000,
        load1: 0.1,
        load5: 0.2,
        load15: 0.3,
        uptime: 90061,
        disk_read_speed: 4096,
        disk_write_speed: 8192,
        created_at: '2026-05-24T10:00:00Z'
      }
    },
    {
      id: 'node-2',
      name: 'Tokyo JP',
      hostname: 'tokyo-jp',
      ip: '10.0.0.2',
      os: 'linux',
      arch: 'amd64',
      kernel: '6.6',
      agent_version: '0.1.0',
      status: 'online',
      last_seen_at: '2026-05-24T10:00:00Z'
    }
  ]
}

const emptyMetrics: MetricsResponse = { metrics: [] }

const processSnapshot: ProcessSnapshotResponse = {
  node_id: 'node-1',
  collected_at: 1710000000,
  error: '',
  processes: [
    { pid: 101, ppid: 1, name: 'nginx', command: 'nginx -g daemon off', user: 'www-data', status: 'sleeping', cpu_usage: 8.5, memory_rss: 104857600, memory_usage: 5.5 },
    { pid: 202, ppid: 1, name: 'postgres', command: 'postgres --config', user: 'postgres', status: 'running', cpu_usage: 2, memory_rss: 2147483648, memory_usage: 40 }
  ]
}

const dockerSnapshot: DockerSnapshotResponse = {
  node_id: 'node-1',
  collected_at: 1710000001,
  available: true,
  version: '24.0.0',
  error: '',
  containers: [
    { id: 'abcdef123456', name: 'web', image: 'nginx:latest', state: 'running', status: 'Up 1 minute', cpu_usage: 3.5, memory_usage: 104857600, memory_limit: 1073741824, memory_percent: 9.8, network_rx: 1000, network_tx: 2000 },
    { id: 'deadbeef9999', name: 'worker', image: 'queue:latest', state: 'exited', status: 'Exited (0)', cpu_usage: 0, memory_usage: 0, memory_limit: 0, network_rx: 0, network_tx: 0 }
  ]
}

describe('node monitoring detail', () => {
  beforeEach(() => {
    vi.mocked(getNodes).mockReset()
    vi.mocked(getNodeMetrics).mockReset()
    vi.mocked(getNodeProcesses).mockReset()
    vi.mocked(getNodeDocker).mockReset()
    vi.mocked(getAgentStatus).mockReset()
    vi.mocked(getAgentLogs).mockReset()
    vi.mocked(restartAgent).mockReset()
    vi.mocked(getNodes).mockResolvedValue(nodesResponse)
    vi.mocked(getNodeMetrics).mockResolvedValue(emptyMetrics)
    vi.mocked(getNodeProcesses).mockResolvedValue(processSnapshot)
    vi.mocked(getNodeDocker).mockResolvedValue(dockerSnapshot)
    vi.mocked(getAgentStatus).mockResolvedValue({ version: '0.1.0', user: 'root', mode: 'ops', terminal_enabled: true, docker_available: true, service_name: 'mizupanel-agent', uptime: 3600, collected_at: 1710000000 })
    vi.mocked(getAgentLogs).mockResolvedValue({ lines: 100, content: 'mizupanel-agent started', collected_at: 1710000001 })
    vi.mocked(restartAgent).mockResolvedValue({ accepted: true, message: '重启命令已下发，等待 Agent 重新连接' })
  })

  test('loads snapshots and shows them behind detail section buttons', async () => {
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Oracle SG' })).toBeInTheDocument()
    await waitFor(() => expect(getNodeProcesses).toHaveBeenCalledWith('node-1'))
    expect(getNodeDocker).toHaveBeenCalledWith('node-1')
    expect(screen.getByRole('button', { name: '主机信息' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('基础信息')).toBeInTheDocument()
    expect(screen.getByText('操作系统')).toBeInTheDocument()
    expect(screen.getByText('内核版本')).toBeInTheDocument()
    expect(screen.getByText('架构')).toBeInTheDocument()
    expect(screen.getByText('启动时间')).toBeInTheDocument()
    expect(screen.getByText('运行时间')).toBeInTheDocument()
    expect(screen.getAllByText('系统负载').length).toBe(2)
    expect(screen.getByText('1 天 1 小时')).toBeInTheDocument()
    expect(screen.getByText('1m 0.10 · 5m 0.20 · 15m 0.30')).toBeInTheDocument()
    expect(screen.queryByText('在线用户')).not.toBeInTheDocument()
    expect(screen.queryByText('SSH 连接')).not.toBeInTheDocument()
    const loadChart = screen.getByRole('region', { name: '系统负载' })
    expect(within(loadChart).getByText('1m')).toBeInTheDocument()
    expect(within(loadChart).getByText('5m')).toBeInTheDocument()
    expect(within(loadChart).getByText('15m')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'CPU 使用率' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '内存使用率' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '磁盘使用率' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '网络 I/O' })).toBeInTheDocument()
    const diskIOChart = screen.getByRole('region', { name: '磁盘 I/O' })
    expect(within(diskIOChart).getByText('读')).toBeInTheDocument()
    expect(within(diskIOChart).getByText('4.0 KB/s')).toBeInTheDocument()
    expect(within(diskIOChart).getByText('写')).toBeInTheDocument()
    expect(within(diskIOChart).getByText('8.0 KB/s')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '系统负载' })).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'CPU 使用率' })).getByRole('button', { name: '1h' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(within(screen.getByRole('region', { name: 'CPU 使用率' })).getByRole('button', { name: '6h' }))
    expect(within(screen.getByRole('region', { name: 'CPU 使用率' })).getByRole('button', { name: '6h' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(screen.getByRole('region', { name: '内存使用率' })).getByRole('button', { name: '1h' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('region', { name: '进程 Top' })).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Docker 容器' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '进程信息' }))
    expect(screen.getByRole('button', { name: '进程信息' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('region', { name: '进程 Top' })).toHaveTextContent('nginx')
    expect(screen.getByRole('region', { name: '进程 Top' })).not.toHaveTextContent('nginx -g daemon off')
    expect(screen.queryByText('硬件概览')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    expect(screen.getByRole('button', { name: '容器信息' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('region', { name: 'Docker 容器' })).toHaveTextContent('Docker 24.0.0')
    expect(screen.getByRole('region', { name: 'Docker 容器' })).toHaveTextContent('nginx:latest')
    expect(screen.queryByRole('region', { name: '进程 Top' })).not.toBeInTheDocument()
  })

  test('wires Agent management actions through the API client', async () => {
    render(<App />)

    await screen.findByRole('heading', { name: 'Oracle SG' })
    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    const panel = await screen.findByRole('region', { name: 'Agent 管理' })

    await waitFor(() => expect(getAgentStatus).toHaveBeenCalledWith('node-1'))
    expect(getAgentLogs).toHaveBeenCalledWith('node-1', 100)
    expect(within(panel).getByText('mizupanel-agent started')).toBeInTheDocument()

    fireEvent.click(within(panel).getByRole('button', { name: '重启 Agent' }))
		const dialog = screen.getByRole('dialog', { name: '重启 Agent' })
		expect(within(dialog).getByText('mizupanel-agent')).toBeInTheDocument()
		fireEvent.click(within(dialog).getByRole('button', { name: '确认重启' }))
    expect(restartAgent).toHaveBeenCalledWith('node-1')
		expect(await screen.findByText('Agent重启命令下发成功')).toBeInTheDocument()
  })

  test('uses placeholders for missing disk I/O fields and derives boot time from uptime', async () => {
    const legacyNodesResponse = {
      nodes: [{
        ...nodesResponse.nodes[0],
        latest_metric: {
          ...nodesResponse.nodes[0].latest_metric,
          uptime: 3661,
          disk_read_speed: undefined,
          disk_write_speed: undefined,
          created_at: '2026-05-24T10:00:00Z'
        }
      }]
    } as unknown as NodesResponse
    vi.mocked(getNodes).mockResolvedValueOnce(legacyNodesResponse)
    vi.mocked(getNodeMetrics).mockResolvedValueOnce({
      metrics: [{
        ...nodesResponse.nodes[0].latest_metric,
        uptime: 0,
        disk_read_speed: undefined,
        disk_write_speed: undefined,
        created_at: '2026-05-24T10:00:05Z'
      }]
    } as unknown as MetricsResponse)

    render(<App />)

    await screen.findByRole('heading', { name: 'Oracle SG' })
    const basicInfo = screen.getByRole('region', { name: '基础信息' })
    const expectedBootTime = new Date(new Date('2026-05-24T10:00:00Z').getTime() - 3661 * 1000).toLocaleString('zh-CN', { hour12: false })
    expect(within(basicInfo).getByText('启动时间').closest('div')).toHaveTextContent(expectedBootTime)
    expect(within(basicInfo).getByText('运行时间').closest('div')).toHaveTextContent('1 小时 1 分钟')
    const diskIOChart = screen.getByRole('region', { name: '磁盘 I/O' })
    expect(within(diskIOChart).getByText('读').closest('div')).toHaveTextContent('—')
    expect(within(diskIOChart).getByText('写').closest('div')).toHaveTextContent('—')
    expect(diskIOChart).not.toHaveTextContent('NaN')
    expect(diskIOChart).not.toHaveTextContent('undefined/s')
  })

  test('filters and sorts process rows locally', async () => {
    render(<App />)

    await screen.findByRole('heading', { name: 'Oracle SG' })
    fireEvent.click(screen.getByRole('button', { name: '进程信息' }))
    const processRegion = await screen.findByRole('region', { name: '进程 Top' })
    await waitFor(() => expect(processRegion).toHaveTextContent('postgres'))
    fireEvent.click(within(processRegion).getByRole('button', { name: '按内存排序' }))

    await waitFor(() => {
      const rows = within(processRegion).getAllByRole('row')
      expect(rows[1]).toHaveTextContent('postgres')
    })

    fireEvent.change(within(processRegion).getByLabelText('搜索进程'), { target: { value: 'nginx' } })

    await waitFor(() => {
      expect(within(processRegion).getByText('nginx')).toBeInTheDocument()
      expect(within(processRegion).queryByText('postgres')).not.toBeInTheDocument()
    })

    fireEvent.change(within(processRegion).getByLabelText('搜索进程'), { target: { value: 'not-found' } })

    expect(await within(processRegion).findByText('当前筛选条件下没有进程。')).toBeInTheDocument()
  })

  test('keeps Docker container table constrained inside the detail panel', async () => {
    vi.mocked(getNodeDocker).mockResolvedValueOnce({
      ...dockerSnapshot,
      containers: [{
        id: 'abcdef123456',
        full_id: 'abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890',
        name: 'very-long-container-name-that-should-not-stretch-the-whole-dashboard-layout',
        image: 'registry.example.com/namespace/very/very/very/long/image/name/that/used/to/stretch/the/page:latest',
        state: 'running',
        status: 'Up 1 minute with a very long docker status string that should stay clipped inside the table cell',
        cpu_usage: 3.5,
        memory_usage: 104857600,
        memory_limit: 1073741824,
        memory_percent: 9.8,
        network_rx: 1000,
        network_tx: 2000
      }]
    })

    render(<App />)

    await screen.findByRole('heading', { name: 'Oracle SG' })
    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    const dockerRegion = await screen.findByRole('region', { name: 'Docker 容器' })
    const scrollArea = dockerRegion.querySelector('[data-testid="docker-table-scroll"]')
    const table = dockerRegion.querySelector('table')

    expect(scrollArea).toHaveClass('min-w-0', 'max-w-full', 'overflow-x-auto')
    expect(table).toHaveClass('w-full', 'min-w-0')
    expect(within(dockerRegion).getByTitle('registry.example.com/namespace/very/very/very/long/image/name/that/used/to/stretch/the/page:latest')).toHaveClass('line-clamp-2')
  })

  test('shows Docker unavailable and filters container status', async () => {
    vi.mocked(getNodeDocker).mockResolvedValueOnce({ node_id: 'node-1', collected_at: 1710000001, available: false, error: 'Get "http://docker/version": dial unix /var/run/docker.sock: connect: permission denied', containers: [] })

    render(<App />)

    await screen.findByRole('heading', { name: 'Oracle SG' })
    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    const dockerRegion = await screen.findByRole('region', { name: 'Docker 容器' })
    await waitFor(() => expect(dockerRegion).toHaveTextContent('Agent 当前用户没有权限访问 Docker'))

    vi.mocked(getNodeDocker).mockResolvedValue({ ...dockerSnapshot, node_id: 'node-2' })
    fireEvent.click(screen.getByRole('button', { name: /Tokyo JP/ }))
    await waitFor(() => expect(getNodeDocker).toHaveBeenCalledWith('node-2'))
    const refreshedDockerRegion = screen.getByRole('region', { name: 'Docker 容器' })
    expect(within(refreshedDockerRegion).getByLabelText('搜索容器')).toBeInTheDocument()
    fireEvent.click(within(refreshedDockerRegion).getByRole('button', { name: '运行中' }))

    expect(refreshedDockerRegion).toHaveTextContent('web')
    expect(refreshedDockerRegion).not.toHaveTextContent('worker')
  })
})
