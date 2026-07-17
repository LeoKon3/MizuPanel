import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import App from './App'
import { createInstallCommand, deleteNode, getNodeDocker, getNodeMetrics, getNodeProcesses, getNodes } from './api/client'
import type { Metric, Node } from './types'

vi.mock('./api/client', () => ({
  setUnauthorizedHandler: vi.fn(),
  getAuthSession: vi.fn(async () => ({ auth_enabled: false, authenticated: true, username: '' })),
  createInstallCommand: vi.fn(),
  getNodes: vi.fn(),
  getNodeMetrics: vi.fn(),
  getNodeProcesses: vi.fn(),
  getNodeDocker: vi.fn(),
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
  getAgentStatus: vi.fn(async () => ({ version: '0.1.0', user: 'root', mode: 'ops', terminal_enabled: true, docker_available: true, service_name: 'mizupanel-agent', uptime: 3600, collected_at: 1710000000 })),
  restartAgent: vi.fn(async () => ({ accepted: true, message: '重启命令已下发，等待 Agent 重新连接' })),
  getAgentLogs: vi.fn(async () => ({ lines: 100, content: 'mizupanel-agent started', collected_at: 1710000001 })),
  createTerminalSession: vi.fn(async () => ({ token: 'terminal-token' })),
  createContainerExecSession: vi.fn(async () => ({ token: 'exec-token' })),
  startSSHInstall: vi.fn(async () => ({ job_id: 'ssh-install-1' })),
  startSSHUninstall: vi.fn(async () => ({ job_id: 'ssh-uninstall-1' }))
}))

const nodes: Node[] = [
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
    last_seen_at: '2026-05-24T10:00:00Z'
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

const metric: Metric = {
  id: 1,
  node_id: 'node-1',
  cpu_usage: 72,
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

describe('App regression behavior', () => {
  beforeEach(() => {
    vi.mocked(createInstallCommand).mockReset()
    vi.mocked(createInstallCommand).mockResolvedValue({ command: 'install command', install_token: 'install-token' })
    vi.mocked(getNodes).mockReset()
    vi.mocked(deleteNode).mockReset()
    vi.mocked(deleteNode).mockResolvedValue(undefined)
    vi.mocked(getNodeMetrics).mockReset()
    vi.mocked(getNodeMetrics).mockResolvedValue({ metrics: [] })
    vi.mocked(getNodeProcesses).mockReset()
    vi.mocked(getNodeDocker).mockReset()
    vi.mocked(getNodeProcesses).mockResolvedValue({ node_id: 'node-1', collected_at: 0, error: '', processes: [] })
    vi.mocked(getNodeDocker).mockResolvedValue({ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] })
  })

  test('falls back to the first node when the route node id is invalid', async () => {
    window.history.pushState({}, '', '/nodes/missing-node')
    vi.mocked(getNodes).mockResolvedValueOnce({ nodes })
    vi.mocked(getNodeMetrics).mockResolvedValue({ metrics: [] })

    render(<App />)

    expect(await screen.findAllByText('Oracle SG')).toHaveLength(2)
    await waitFor(() => expect(window.location.pathname).toBe('/nodes/node-1'))
  })

  test('clears previous chart metrics while loading a newly selected node', async () => {
    vi.mocked(getNodes).mockResolvedValueOnce({ nodes })
    vi.mocked(getNodeMetrics)
      .mockResolvedValueOnce({ metrics: [metric] })
      .mockReturnValueOnce(new Promise(() => {}))

    render(<App />)

    expect(await screen.findAllByText('Oracle SG')).toHaveLength(2)
    await waitFor(() => expect(screen.queryAllByText('等待指标数据')).toHaveLength(0))

    fireEvent.click(screen.getByRole('button', { name: /Tokyo JP/ }))

    expect(await screen.findAllByText('Tokyo JP')).toHaveLength(2)
    expect(screen.getAllByText('等待指标数据').length).toBeGreaterThan(0)
  })

  test('does not show a separate remove-node action because Agent uninstall handles cleanup', async () => {
    window.history.pushState({}, '', '/')
    vi.mocked(getNodes).mockResolvedValueOnce({ nodes })
    vi.mocked(getNodeMetrics).mockResolvedValue({ metrics: [] })

    render(<App />)

    expect(await screen.findAllByText('Oracle SG')).toHaveLength(2)
    expect(screen.queryByRole('button', { name: '移除节点记录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '移除节点记录' })).not.toBeInTheDocument()
    expect(deleteNode).not.toHaveBeenCalled()
  })

  test('shows a dashboard error when nodes fail without opening login', async () => {
    vi.mocked(getNodes).mockRejectedValueOnce(new Error('network'))
    vi.mocked(getNodeMetrics).mockResolvedValue({ metrics: [] })

    render(<App />)

    expect(await screen.findByText('network')).toBeInTheDocument()
    expect(screen.getByText('暂无节点接入')).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '登录 MizuPanel' })).not.toBeInTheDocument()
  })

  test('shows metric load errors without opening login', async () => {
    vi.mocked(getNodes).mockResolvedValueOnce({ nodes })
    vi.mocked(getNodeMetrics).mockRejectedValueOnce(Object.assign(new Error('Request failed: 401'), { status: 401 }))

    render(<App />)

    expect(await screen.findByText('Request failed: 401')).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '登录 MizuPanel' })).not.toBeInTheDocument()
    expect(screen.queryByText('退出登录')).not.toBeInTheDocument()
  })

  test('shows a dashboard error when install command generation fails', async () => {
    vi.mocked(getNodes).mockResolvedValue({ nodes })
    vi.mocked(getNodeMetrics).mockResolvedValue({ metrics: [] })
    vi.mocked(createInstallCommand).mockRejectedValueOnce(new Error('network'))

    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: '添加主机' }))

    expect(await screen.findByText('network')).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '登录 MizuPanel' })).not.toBeInTheDocument()
  })

  test('restores encoded route node ids without falling back to the first node', async () => {
    const encodedRouteNodes: Node[] = [
      nodes[0],
      {
        ...nodes[1],
        id: '节点 2',
        name: '北京 CN'
      }
    ]
    window.history.pushState({}, '', `/nodes/${encodeURIComponent('节点 2')}`)
    vi.mocked(getNodes).mockResolvedValueOnce({ nodes: encodedRouteNodes })
    vi.mocked(getNodeMetrics).mockResolvedValue({ metrics: [] })

    render(<App />)

    expect(await screen.findAllByText('北京 CN')).toHaveLength(2)
    await waitFor(() => expect(getNodeMetrics).toHaveBeenCalledWith('节点 2', '1h'))
  })
})
