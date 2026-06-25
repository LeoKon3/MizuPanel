import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import App from './App'
import { APIError, createInstallCommand, getAuthSession, getNodes, login, logout, setUnauthorizedHandler } from './api/client'

vi.mock('./api/client', () => ({
  APIError: class APIError extends Error {
    constructor(public status: number, message: string) {
      super(message)
      this.name = 'APIError'
    }
  },
  setUnauthorizedHandler: vi.fn(),
  getAuthSession: vi.fn(async () => ({ auth_enabled: false, authenticated: true, username: '' })),
  login: vi.fn(async () => ({ authenticated: true, username: 'admin' })),
  logout: vi.fn(async () => undefined),
  getNodes: vi.fn(async () => ({
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
          cpu_usage: 12.5,
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
      }
    ]
  })),
  getNodeMetrics: vi.fn(async () => ({ metrics: [] })),
  getNodeProcesses: vi.fn(async () => ({ node_id: 'node-1', collected_at: 0, error: '', processes: [] })),
  getNodeDocker: vi.fn(async () => ({ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] })),
  getSettings: vi.fn(async () => ({ metrics_retention: '6h', metrics_retention_seconds: 21600, max_metrics_retention: '7d' })),
  getSystemAbout: vi.fn(async () => ({ version: '0.1.0', github_url: 'https://github.com/LeoKon3/MizuPanel' })),
  updateSettings: vi.fn(async () => ({ metrics_retention: '6h', metrics_retention_seconds: 21600, max_metrics_retention: '7d' })),
  getAlertHistory: vi.fn(async () => ({ history: [] })),
  getAlertRules: vi.fn(async () => ({ rules: [] })),
  getK8sClusters: vi.fn(async () => ({ clusters: [] })),
  createInstallCommand: vi.fn(async () => ({ command: 'curl install', install_token: 'install-token' })),
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

describe('App', () => {
  beforeEach(() => {
    window.history.pushState({}, '', '/')
    vi.clearAllMocks()
  })

  test('renders dashboard title and node card without authentication', async () => {
    render(<App />)

    expect(await screen.findByText('MizuPanel')).toBeInTheDocument()
    expect(getAuthSession).toHaveBeenCalled()
    expect(screen.queryByText('MizuPanel Console')).not.toBeInTheDocument()
    expect(screen.queryByText('轻量级自托管服务器监控面板')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Light' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dark' })).toBeInTheDocument()
    expect(await screen.findAllByText('Oracle SG')).toHaveLength(2)
    expect(screen.getByText('平均磁盘')).toBeInTheDocument()
    expect(screen.getByText('异常节点')).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '登录 MizuPanel' })).not.toBeInTheDocument()
  })

  test('shows login page when admin auth is enabled and session is unauthenticated', async () => {
    vi.mocked(getAuthSession).mockResolvedValueOnce({ auth_enabled: true, authenticated: false, username: '' })

    render(<App />)

    expect(await screen.findByRole('dialog', { name: '登录 MizuPanel' })).toBeInTheDocument()
    expect(screen.getByLabelText('用户名')).toHaveValue('admin')
    expect(screen.getByLabelText('密码')).toHaveValue('')
    expect(screen.getByRole('button', { name: '登录' })).toBeInTheDocument()
    expect(screen.queryByText('Oracle SG')).not.toBeInTheDocument()
  })

  test('logs in and logs out with configured admin credentials', async () => {
    vi.mocked(getAuthSession).mockResolvedValueOnce({ auth_enabled: true, authenticated: false, username: '' })

    render(<App />)

    await screen.findByRole('dialog', { name: '登录 MizuPanel' })
    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(login).toHaveBeenCalledWith('admin', 'secret'))
    expect(await screen.findAllByText('Oracle SG')).toHaveLength(2)
    expect(screen.getByText('admin')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '退出登录' }))

    await waitFor(() => expect(logout).toHaveBeenCalled())
    expect(await screen.findByRole('dialog', { name: '登录 MizuPanel' })).toBeInTheDocument()
  })

  test('returns to login page when API returns 401 after authentication', async () => {
    let unauthorizedCallback: (() => void) | undefined
    vi.mocked(setUnauthorizedHandler).mockImplementation((handler) => {
      unauthorizedCallback = handler
    })

    vi.mocked(getAuthSession).mockResolvedValueOnce({ auth_enabled: true, authenticated: true, username: 'admin' })

    render(<App />)

    expect(await screen.findAllByText('Oracle SG')).toHaveLength(2)
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(unauthorizedCallback).toBeDefined()

    act(() => {
      unauthorizedCallback!()
    })

    expect(await screen.findByRole('dialog', { name: '登录 MizuPanel' })).toBeInTheDocument()
    expect(screen.getByText(/登录已过期/)).toBeInTheDocument()
  })

  test('opens add host directly in manual command mode and hides SSH install controls', async () => {
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: '添加主机' }))
    expect(await screen.findByRole('dialog', { name: '添加主机' })).toBeInTheDocument()

    await waitFor(() => expect(createInstallCommand).toHaveBeenCalledWith('linux'))
    expect(await screen.findByText('已生成一次性 install_token')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Linux' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('button', { name: 'SSH 自动安装' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('SSH Host')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('启用节点终端')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('启用 Docker 容器监控')).not.toBeInTheDocument()
    expect(screen.queryByText('Agent 运行模式')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /普通模式/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /运维模式/ })).not.toBeInTheDocument()
    expect(screen.getByText('默认以 root 运维模式安装，自动启用节点终端与 Docker 容器监控。')).toBeInTheDocument()
    expect(screen.getByLabelText('选择 Agent 安装系统')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制安装命令' })).toBeInTheDocument()
    expect(screen.getByText('token 来源：点击添加主机时，Server 会自动生成一次性 install_token。')).toBeInTheDocument()
  })

  test('opens the add host dialog from overview quick actions', async () => {
    render(<App />)

    const sidebarNavigation = await screen.findByRole('navigation', { name: '侧边导航' })
    fireEvent.click(within(sidebarNavigation).getByRole('button', { name: '概览' }))
    fireEvent.click(await screen.findByRole('button', { name: '添加服务器' }))

    expect(await screen.findByRole('dialog', { name: '添加主机' })).toBeInTheDocument()
    await waitFor(() => expect(createInstallCommand).toHaveBeenCalledWith('linux'))
  })

  test('creates a fresh manual install token when the add host dialog opens', async () => {
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: '添加主机' }))
    expect(await screen.findByRole('dialog', { name: '添加主机' })).toBeInTheDocument()
    await waitFor(() => expect(createInstallCommand).toHaveBeenCalledWith('linux'))

    expect(screen.queryByLabelText('启用 Docker 容器监控')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('启用节点终端')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /运维模式/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'SSH 自动安装' })).not.toBeInTheDocument()
  })

  test('reopens the add host dialog with a fresh install command', async () => {
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: '添加主机' }))
    await waitFor(() => expect(createInstallCommand).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '关闭安装命令' }))

    fireEvent.click(await screen.findByRole('button', { name: '添加主机' }))

    await waitFor(() => expect(createInstallCommand).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: 'Linux' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByLabelText('SSH Host')).not.toBeInTheDocument()
  })

  test('shows simplified manual install status in the add host dialog', async () => {
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: '添加主机' }))

    await waitFor(() => expect(createInstallCommand).toHaveBeenCalled())
    expect(await screen.findByText('已生成一次性 install_token')).toBeInTheDocument()
    expect(screen.getByText('等待在目标机器执行命令')).toBeInTheDocument()
    expect(screen.getByText('等待 Agent 首次注册')).toBeInTheDocument()
    expect(screen.getByText(/超时未连接时，请检查 server_url、防火墙或 Agent 日志/)).toBeInTheDocument()
    expect(screen.queryByLabelText('SSH Host')).not.toBeInTheDocument()
  })
})
