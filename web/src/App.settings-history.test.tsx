import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import App from './App'
import { getNodeDocker, getNodeMetrics, getNodeProcesses, getNodes, getSettings, getSystemAbout, updateSettings } from './api/client'
import type { Metric, Node } from './types'

vi.mock('./api/client', () => ({
  setUnauthorizedHandler: vi.fn(),
  getAuthSession: vi.fn(async () => ({ auth_enabled: false, authenticated: true, username: '' })),
  createInstallCommand: vi.fn(async () => ({ command: 'install command', install_token: 'install-token' })),
  getNodes: vi.fn(),
  getNodeMetrics: vi.fn(),
  getNodeProcesses: vi.fn(),
  getNodeDocker: vi.fn(),
  getSettings: vi.fn(),
  getSystemAbout: vi.fn(),
  updateSettings: vi.fn(),
  getNodeFiles: vi.fn(async () => ({ path: '/', entries: [] })),
  readNodeFile: vi.fn(async () => ({ path: '/tmp/a', content: '', editable: true })),
  writeNodeFile: vi.fn(async () => ({ path: '/tmp/a', saved: true })),
  uploadNodeFile: vi.fn(async () => ({ path: '/tmp/upload.bin', uploaded: true })),
  deleteNodePath: vi.fn(async () => ({ path: '/tmp/upload.bin', deleted: true })),
  deleteNode: vi.fn(async () => undefined),
  rebootNode: vi.fn(async () => ({ accepted: true })),
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
  }
]

const metrics: Metric[] = [
  {
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
]

describe('App history and system settings', () => {
  beforeEach(() => {
    window.history.pushState({}, '', '/')
    vi.mocked(getNodes).mockReset()
    vi.mocked(getNodeMetrics).mockReset()
    vi.mocked(getNodeProcesses).mockReset()
    vi.mocked(getNodeDocker).mockReset()
    vi.mocked(getSettings).mockReset()
    vi.mocked(getSystemAbout).mockReset()
    vi.mocked(updateSettings).mockReset()
    vi.mocked(getNodes).mockResolvedValue({ nodes })
    vi.mocked(getNodeMetrics).mockResolvedValue({ metrics })
    vi.mocked(getNodeProcesses).mockResolvedValue({ node_id: 'node-1', collected_at: 0, error: '', processes: [] })
    vi.mocked(getNodeDocker).mockResolvedValue({ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] })
    vi.mocked(getSettings).mockResolvedValue({ metrics_retention: '24h', metrics_retention_seconds: 86400, max_metrics_retention: '7d' })
    vi.mocked(getSystemAbout).mockResolvedValue({ version: '0.1.0', github_url: 'https://github.com/LeoKon3/MizuPanel' })
    vi.mocked(updateSettings).mockResolvedValue({ metrics_retention: '7d', metrics_retention_seconds: 604800, max_metrics_retention: '7d' })
  })

  test('keeps the legacy history route and switches metric ranges', async () => {
    window.history.pushState({}, '', '/history')

    render(<App />)

    expect(await screen.findByRole('heading', { name: '指标历史记录' })).toBeInTheDocument()
    expect(screen.getAllByText('Oracle SG').length).toBeGreaterThan(0)
    expect(within(await screen.findByRole('region', { name: 'CPU 历史' })).getByText('42.0%')).toBeInTheDocument()
    const loadChart = await screen.findByRole('region', { name: 'Load 走势' })
    expect(within(loadChart).getByText('0.10')).toBeInTheDocument()
    expect(within(loadChart).queryByText('0.10%')).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: '最近 24 小时' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '最近 24 小时' }))
    await waitFor(() => expect(getNodeMetrics).toHaveBeenCalledWith('node-1', '24h'))
    expect(window.location.pathname).toBe('/history')
  })

  test('disables history ranges beyond the configured retention', async () => {
    vi.mocked(getSettings).mockResolvedValue({ metrics_retention: '6h', metrics_retention_seconds: 21600, max_metrics_retention: '7d' })
    window.history.pushState({}, '', '/history')

    render(<App />)

    const longRange = await screen.findByRole('button', { name: '最近 24 小时' })
    expect(longRange).toBeDisabled()
    fireEvent.click(longRange)
    expect(getNodeMetrics).not.toHaveBeenCalledWith('node-1', '24h')
  })

  test('opens system settings and saves metrics retention without restarting', async () => {
    render(<App />)

    const sidebarNavigation = await screen.findByRole('navigation', { name: '侧边导航' })
    fireEvent.click(within(sidebarNavigation).getByRole('button', { name: '系统设置' }))

    const panel = await screen.findByRole('region', { name: '系统设置' })
    expect(within(panel).getByText('指标保留时间')).toBeInTheDocument()
    expect(await within(panel).findByText('v0.1.0')).toBeInTheDocument()
    expect(within(panel).getByRole('link', { name: '打开 GitHub 仓库' })).toHaveAttribute('href', 'https://github.com/LeoKon3/MizuPanel')
    fireEvent.click(within(panel).getByRole('button', { name: '7 天' }))
    fireEvent.click(within(panel).getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ metrics_retention: '7d' }))
    expect(await within(panel).findByText('设置已保存，新的保留时间会立即用于历史查询和后续清理。')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/settings')
  })
})
