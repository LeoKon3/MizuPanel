import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import App from './App'

const { createInstallCommandMock } = vi.hoisted(() => ({
  createInstallCommandMock: vi.fn(async (platform = 'linux') => ({
    command: platform === 'windows'
      ? [
        `powershell -NoProfile -ExecutionPolicy Bypass -Command "\`$ErrorActionPreference='Stop'; \`$script = Join-Path \`$env:TEMP ('mizupanel-install-' + [guid]::NewGuid().ToString() + '.ps1'); Invoke-WebRequest -Uri 'http://localhost:8080/scripts/install-agent.ps1' -UseBasicParsing -OutFile \`$script -ErrorAction Stop; & \`$script `,
        `    -BinaryBaseUrl 'http://localhost:8080/downloads' `,
        `    -ServerUrl 'ws://localhost:8080/api/agent/ws' `,
        `    -Token 'generated-windows-token' `,
        `    -NodeId \`$env:COMPUTERNAME `,
        `    -Name \`$env:COMPUTERNAME"`
      ].join('\n')
      : [
        `curl -fsSL 'http://localhost:8080/scripts/install-agent.sh' -o install-agent.sh \\`,
        `  && chmod +x install-agent.sh \\`,
        `  && ./install-agent.sh \\`,
        `    --binary-base-url 'http://localhost:8080/downloads' \\`,
        `    --server-url 'ws://localhost:8080/api/agent/ws' \\`,
        `    --token 'generated-install-token' \\`,
        `    --node-id "$(hostname)" \\`,
        `    --name "$(hostname)" \\`,
        `    --mode 'ops' \\`,
        `    --enable-docker \\`,
        `    --enable-terminal`
      ].join('\n'),
    install_token: platform === 'windows' ? 'generated-windows-token' : 'generated-install-token'
  }))
}))

vi.mock('./api/client', () => ({
  setUnauthorizedHandler: vi.fn(),
  getAuthSession: vi.fn(async () => ({ auth_enabled: false, authenticated: true, username: '' })),
  createInstallCommand: createInstallCommandMock,
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
        terminal_enabled: true,
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
      },
      {
        id: 'node-2',
        name: 'Tokyo Backup',
        hostname: 'tokyo-backup',
        ip: '10.0.0.2',
        os: 'linux',
        arch: 'amd64',
        kernel: '5.15',
        agent_version: '0.1.0',
        status: 'offline',
        last_seen_at: '2026-05-24T09:00:00Z'
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

beforeEach(() => {
  window.history.replaceState({}, '', '/')
  window.localStorage.clear()
  createInstallCommandMock.mockReset()
  createInstallCommandMock.mockImplementation(async (platform = 'linux') => ({
    command: platform === 'windows'
      ? [
        `powershell -NoProfile -ExecutionPolicy Bypass -Command "\`$ErrorActionPreference='Stop'; \`$script = Join-Path \`$env:TEMP ('mizupanel-install-' + [guid]::NewGuid().ToString() + '.ps1'); Invoke-WebRequest -Uri 'http://localhost:8080/scripts/install-agent.ps1' -UseBasicParsing -OutFile \`$script -ErrorAction Stop; & \`$script `,
        `    -BinaryBaseUrl 'http://localhost:8080/downloads' `,
        `    -ServerUrl 'ws://localhost:8080/api/agent/ws' `,
        `    -Token 'generated-windows-token' `,
        `    -NodeId \`$env:COMPUTERNAME `,
        `    -Name \`$env:COMPUTERNAME"`
      ].join('\n')
      : [
        `curl -fsSL 'http://localhost:8080/scripts/install-agent.sh' -o install-agent.sh \\`,
        `  && chmod +x install-agent.sh \\`,
        `  && ./install-agent.sh \\`,
        `    --binary-base-url 'http://localhost:8080/downloads' \\`,
        `    --server-url 'ws://localhost:8080/api/agent/ws' \\`,
        `    --token 'generated-install-token' \\`,
        `    --node-id "$(hostname)" \\`,
        `    --name "$(hostname)" \\`,
        `    --mode 'ops' \\`,
        `    --enable-docker \\`,
        `    --enable-terminal`
      ].join('\n'),
    install_token: platform === 'windows' ? 'generated-windows-token' : 'generated-install-token'
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('reference-style dashboard layout', () => {
  test('renders console sidebar navigation and expanded node detail without auth controls or fake entries', async () => {
    render(<App />)

    const sidebarNavigation = await screen.findByRole('navigation', { name: '侧边导航' })
    expect(within(sidebarNavigation).getByRole('button', { name: '概览' })).toBeInTheDocument()
    expect(within(sidebarNavigation).getByRole('button', { name: '主机' })).toBeInTheDocument()
    expect(within(sidebarNavigation).queryByRole('button', { name: '历史记录' })).not.toBeInTheDocument()
    expect(within(sidebarNavigation).getByRole('button', { name: '告警' })).toBeInTheDocument()
    expect(within(sidebarNavigation).getByRole('button', { name: '系统设置' })).toBeInTheDocument()
    expect(within(sidebarNavigation).queryByRole('button', { name: '日志' })).not.toBeInTheDocument()
    const mobileNavigation = screen.getByRole('navigation', { name: '移动端导航' })
    expect(within(mobileNavigation).queryByRole('button', { name: '历史记录' })).not.toBeInTheDocument()
    expect(within(mobileNavigation).queryByRole('button', { name: '日志' })).not.toBeInTheDocument()
    expect(within(sidebarNavigation).queryByRole('button', { name: 'Docker' })).not.toBeInTheDocument()
    expect(within(sidebarNavigation).queryByRole('button', { name: '文件管理' })).not.toBeInTheDocument()
    expect(within(sidebarNavigation).queryByRole('button', { name: '终端' })).not.toBeInTheDocument()
    expect(within(screen.getByRole('complementary', { name: 'MizuPanel 侧边栏' })).getByLabelText('MizuPanel logo')).toHaveAttribute('data-testid', 'mizupanel-logo-mark')
    expect(await screen.findByRole('button', { name: '打开终端' })).toBeEnabled()
    expect(screen.queryByText('MizuPanel Console')).not.toBeInTheDocument()
    expect(screen.queryByText('轻量级自托管服务器监控面板')).not.toBeInTheDocument()
    expect(screen.queryByText('查看节点状态、指标、文件和节点级操作。')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Light' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dark' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('搜索主机...')).toBeInTheDocument()
    expect(screen.getByText('全部 2')).toBeInTheDocument()
    expect(screen.getByText('在线 1')).toBeInTheDocument()
    expect(screen.getByText('平均磁盘')).toBeInTheDocument()
    expect(screen.getByTestId('host-page-container')).toHaveClass('max-w-[1400px]')
    expect(screen.getByTestId('host-main-grid')).toHaveClass('xl:grid-cols-[320px_minmax(0,1fr)]')
    expect(screen.getByTestId('host-list-panel')).toHaveClass('xl:w-[320px]')
    expect(screen.getByTestId('node-detail-charts')).toHaveClass('xl:grid-cols-3')
    const basicInfo = screen.getByRole('region', { name: '基础信息' })
    expect(basicInfo).toHaveClass('rounded-[14px]')
    expect(within(basicInfo).getByText('操作系统')).toBeInTheDocument()
    expect(within(basicInfo).getByText('内核版本')).toBeInTheDocument()
    expect(within(basicInfo).getByText('架构')).toBeInTheDocument()
    expect(within(basicInfo).getByText('启动时间')).toBeInTheDocument()
    expect(within(basicInfo).getByText('运行时间')).toBeInTheDocument()
    expect(within(basicInfo).getByText('系统负载')).toBeInTheDocument()
    expect(within(basicInfo).getByText('1m 0.10 · 5m 0.20 · 15m 0.30')).toBeInTheDocument()
    expect(within(basicInfo).queryByText('在线用户')).not.toBeInTheDocument()
    expect(within(basicInfo).queryByText('SSH 连接')).not.toBeInTheDocument()
    const cpuChart = screen.getByRole('region', { name: 'CPU 使用率' })
    expect(within(cpuChart).getByText('12.5%')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: '内存使用率' })).getByText('50.0%')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: '磁盘使用率' })).getByText('50.0%')).toBeInTheDocument()
    const diskIOChart = screen.getByRole('region', { name: '磁盘 I/O' })
    expect(within(diskIOChart).getByText('读')).toBeInTheDocument()
    expect(within(diskIOChart).getByText('4.0 KB/s')).toBeInTheDocument()
    expect(within(diskIOChart).getByText('写')).toBeInTheDocument()
    expect(within(diskIOChart).getByText('8.0 KB/s')).toBeInTheDocument()
    const networkChart = screen.getByRole('region', { name: '网络 I/O' })
    expect(within(networkChart).getByText('上行')).toBeInTheDocument()
    expect(within(networkChart).getByText('200 B/s')).toBeInTheDocument()
    expect(within(networkChart).getByText('下行')).toBeInTheDocument()
    expect(within(networkChart).getByText('100 B/s')).toBeInTheDocument()
    const loadChart = screen.getByRole('region', { name: '系统负载' })
    expect(within(loadChart).getByText('1m')).toBeInTheDocument()
    expect(within(loadChart).getByText('0.10')).toBeInTheDocument()
    expect(within(loadChart).getByText('5m')).toBeInTheDocument()
    expect(within(loadChart).getByText('0.20')).toBeInTheDocument()
    expect(within(loadChart).getByText('15m')).toBeInTheDocument()
    expect(within(loadChart).getByText('0.30')).toBeInTheDocument()
    expect(within(sidebarNavigation).queryByText(/OV|HS|HT|ST|LG/)).not.toBeInTheDocument()
    expect(screen.queryByText('本地管理员')).not.toBeInTheDocument()
    expect(screen.queryByText('Self-hosted')).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '登录 MizuPanel' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '退出登录' })).not.toBeInTheDocument()
  })

  test('persists theme and sidebar state without losing host search state', async () => {
    window.localStorage.setItem('mizupanel-theme', 'dark')
    window.localStorage.setItem('mizupanel-sidebar-collapsed', 'true')

    render(<App />)

    await screen.findByText('全部 2')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    const sidebar = screen.getByLabelText('MizuPanel 侧边栏')
    expect(sidebar).toHaveAttribute('data-collapsed', 'true')
    expect(sidebar).toHaveClass('transition-[width]', 'duration-300', 'ease-in-out', 'overflow-hidden')
    const expandSidebarButton = screen.getByRole('button', { name: '展开侧边栏' })
    expect(sidebar).not.toContainElement(expandSidebarButton)
    expect(expandSidebarButton).toHaveClass('absolute', 'right-0', 'translate-x-1/2')
    expect(within(sidebar).getByLabelText('MizuPanel logo')).toHaveAttribute('data-testid', 'mizupanel-logo-mark')
    const collapsedHostButton = within(screen.getByRole('navigation', { name: '侧边导航' })).getByRole('button', { name: '主机' })
    expect(collapsedHostButton).toHaveAttribute('title', '主机')
    expect(collapsedHostButton).toHaveClass('px-0')
    expect(screen.getAllByTestId('sidebar-nav-label')[0]).toHaveClass('opacity-0', 'max-w-0')

    fireEvent.change(screen.getByPlaceholderText('搜索主机...'), { target: { value: 'oracle' } })
    fireEvent.click(screen.getByRole('button', { name: 'Light' }))

    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(window.localStorage.getItem('mizupanel-theme')).toBe('light')
    expect(screen.getByPlaceholderText('搜索主机...')).toHaveValue('oracle')

    fireEvent.click(screen.getByRole('button', { name: '展开侧边栏' }))
    expect(sidebar).toHaveAttribute('data-collapsed', 'false')
    expect(screen.getAllByTestId('sidebar-nav-label')[0]).toHaveClass('opacity-100', 'max-w-[140px]')
    expect(window.localStorage.getItem('mizupanel-sidebar-collapsed')).toBe('false')

    fireEvent.click(screen.getByRole('button', { name: 'Dark' }))
    expect(window.localStorage.getItem('mizupanel-theme')).toBe('dark')
    expect(screen.queryByRole('button', { name: 'System' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '切换到深色主题' })).not.toBeInTheDocument()
  })

  test('keeps the legacy logs route as a direct empty-state shell', async () => {
    window.history.pushState({}, '', '/logs')

    render(<App />)

    expect(await screen.findByRole('heading', { name: '日志' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('搜索日志...')).toBeInTheDocument()
    expect(screen.getByText('等待日志接口接入')).toBeInTheDocument()
    expect(screen.queryByText(/ERROR|WARNING|SUCCESS|INFO/)).not.toBeInTheDocument()
  })

  test('keeps host search state while navigating the sidebar', async () => {
    render(<App />)

    await screen.findByText('全部 2')
    const sidebarNavigation = screen.getByRole('navigation', { name: '侧边导航' })
    fireEvent.change(screen.getByPlaceholderText('搜索主机...'), { target: { value: 'oracle' } })
    fireEvent.click(within(sidebarNavigation).getByRole('button', { name: '系统设置' }))
    expect(await screen.findByRole('region', { name: '系统设置' })).toBeInTheDocument()

    fireEvent.click(within(sidebarNavigation).getByRole('button', { name: '主机' }))

    expect(screen.getByPlaceholderText('搜索主机...')).toHaveValue('oracle')
    expect(screen.getByRole('button', { name: /Oracle SG/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Tokyo Backup/ })).not.toBeInTheDocument()
  })

  test('filters hosts by status and search keyword', async () => {
    render(<App />)

    await screen.findByText('全部 2')
    expect(screen.getByRole('button', { name: '全部 2' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /Oracle SG/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Tokyo Backup/ })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '离线 1' }))

    expect(screen.getByRole('button', { name: '离线 1' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('button', { name: /Oracle SG/ })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Tokyo Backup/ })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Tokyo Backup' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Oracle SG' })).not.toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('搜索主机...'), { target: { value: 'oracle' } })

    expect(screen.queryByRole('button', { name: /Tokyo Backup/ })).not.toBeInTheDocument()
    expect(screen.getByText('未找到匹配主机')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Tokyo Backup' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '在线 1' }))

    expect(screen.getByRole('button', { name: '在线 1' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /Oracle SG/ })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Oracle SG' })).toBeInTheDocument()
    expect(screen.queryByText('未找到匹配主机')).not.toBeInTheDocument()
  })

  test('reveals, copies, and closes the install command from the filter toolbar add host button', async () => {
    vi.stubGlobal('location', new URL('http://localhost:5173/nodes/node-1'))
    const writeText = vi.fn(async () => undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })

    render(<App />)

    await screen.findByText('全部 2')
    const sidebarNavigation = screen.getByRole('navigation', { name: '侧边导航' })
    expect(within(sidebarNavigation).queryByRole('button', { name: '添加主机' })).not.toBeInTheDocument()

    const filterToolbar = screen.getByRole('toolbar', { name: '主机筛选与操作' })
    const addHostButton = within(filterToolbar).getByRole('button', { name: '添加主机' })
    expect(screen.queryByRole('dialog', { name: '添加主机' })).not.toBeInTheDocument()

    fireEvent.click(addHostButton)

    const installRegion = await screen.findByRole('dialog', { name: '添加主机' })
    expect(createInstallCommandMock).toHaveBeenCalledWith('linux')
    expect(screen.getByRole('button', { name: 'Linux' })).toHaveAttribute('aria-pressed', 'true')
    expect(installRegion).toHaveTextContent("curl -fsSL 'http://localhost:8080/scripts/install-agent.sh'")
    expect(installRegion).toHaveTextContent("--binary-base-url 'http://localhost:8080/downloads'")
    expect(installRegion).toHaveTextContent("--server-url 'ws://localhost:8080/api/agent/ws'")
    expect(installRegion).toHaveTextContent("--token 'generated-install-token'")
    expect(installRegion).toHaveTextContent("--mode 'ops'")
    expect(installRegion).toHaveTextContent('--enable-docker')
    expect(installRegion).toHaveTextContent('--enable-terminal')
    expect(screen.queryByLabelText('启用 Docker 容器监控')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('启用节点终端')).not.toBeInTheDocument()
    expect(screen.queryByText('Agent 运行模式')).not.toBeInTheDocument()
    expect(screen.getByText('默认以 root 运维模式安装，自动启用节点终端与 Docker 容器监控。')).toBeInTheDocument()
    expect(screen.getByText('token 来源：点击添加主机时，Server 会自动生成一次性 install_token。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Windows' }))

    expect(await screen.findAllByText(/generated-windows-token/)).not.toHaveLength(0)
    expect(createInstallCommandMock).toHaveBeenCalledWith('windows')
    expect(screen.getByRole('button', { name: 'Windows' })).toHaveAttribute('aria-pressed', 'true')
    expect(installRegion).toHaveTextContent("install-agent.ps1")
    expect(installRegion).toHaveTextContent("-NodeId \`$env:COMPUTERNAME")
    expect(screen.getByText('Windows 命令需要在管理员 PowerShell 中执行。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '复制安装命令' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(expect.stringContaining("-Token 'generated-windows-token'")))
    expect(await screen.findByRole('button', { name: '已复制' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '关闭安装命令' }))

    expect(screen.queryByRole('dialog', { name: '添加主机' })).not.toBeInTheDocument()
    expect(addHostButton).toHaveFocus()
  })

  test('keeps the install command dialog keyboard-contained and closes it with Escape', async () => {
    render(<App />)

    await screen.findByText('全部 2')
    const filterToolbar = screen.getByRole('toolbar', { name: '主机筛选与操作' })
    const addHostButton = within(filterToolbar).getByRole('button', { name: '添加主机' })
    fireEvent.click(addHostButton)

    const installDialog = await screen.findByRole('dialog', { name: '添加主机' })
    const closeButton = within(installDialog).getByRole('button', { name: '关闭安装命令' })
    await waitFor(() => expect(installDialog).toHaveFocus())
    fireEvent.keyDown(installDialog, { key: 'Tab', shiftKey: true })
    expect(closeButton).toHaveFocus()

    fireEvent.keyDown(installDialog, { key: 'Tab' })

    expect(installDialog).toContainElement(document.activeElement as HTMLElement | null)

    fireEvent.keyDown(installDialog, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '添加主机' })).not.toBeInTheDocument())
    expect(addHostButton).toHaveFocus()
  })

  test('shows install command generation failures inside the dialog', async () => {
    createInstallCommandMock.mockRejectedValueOnce(new Error('安装命令生成失败'))

    render(<App />)

    await screen.findByText('全部 2')
    fireEvent.click(screen.getByRole('button', { name: '添加主机' }))

    const installDialog = await screen.findByRole('dialog', { name: '添加主机' })
    expect(await within(installDialog).findByText('安装命令生成失败')).toBeInTheDocument()
    expect(within(installDialog).getByRole('button', { name: '复制安装命令' })).toBeDisabled()
  })

  test('ignores stale install command responses after quick platform switching', async () => {
    const pending: Record<'linux' | 'windows', Array<(value: { command: string, install_token: string }) => void>> = { linux: [], windows: [] }
    createInstallCommandMock.mockImplementation((platform = 'linux') => new Promise((resolve) => {
      pending[platform as 'linux' | 'windows'].push(resolve)
    }))

    render(<App />)

    await screen.findByText('全部 2')
    fireEvent.click(screen.getByRole('button', { name: '添加主机' }))
    pending.linux.shift()?.({ command: 'linux initial command', install_token: 'linux-initial-token' })
    const installRegion = await screen.findByRole('dialog', { name: '添加主机' })
    expect(installRegion).toHaveTextContent('linux initial command')

    fireEvent.click(screen.getByRole('button', { name: 'Windows' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Windows' })).toHaveAttribute('aria-pressed', 'true'))
    expect(installRegion).not.toHaveTextContent('linux initial command')
    expect(screen.getByText('正在生成安装命令...')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制安装命令' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Linux' }))
    pending.linux.shift()?.({ command: 'linux latest command', install_token: 'linux-latest-token' })
    expect(await screen.findByText('linux latest command')).toBeInTheDocument()

    pending.windows.shift()?.({ command: 'windows stale command', install_token: 'windows-stale-token' })

    await waitFor(() => expect(screen.getByRole('button', { name: 'Linux' })).toHaveAttribute('aria-pressed', 'true'))
    expect(installRegion).toHaveTextContent('linux latest command')
    expect(installRegion).not.toHaveTextContent('windows stale command')
  })

  test('ignores stale install command failures after platform switching', async () => {
    const pending: Record<'linux' | 'windows', Array<{ resolve: (value: { command: string, install_token: string }) => void, reject: (reason: Error) => void }>> = { linux: [], windows: [] }
    createInstallCommandMock.mockImplementation((platform = 'linux') => new Promise((resolve, reject) => {
      pending[platform as 'linux' | 'windows'].push({ resolve, reject })
    }))

    render(<App />)

    await screen.findByText('全部 2')
    fireEvent.click(screen.getByRole('button', { name: '添加主机' }))
    pending.linux.shift()?.resolve({ command: 'linux initial command', install_token: 'linux-initial-token' })
    const installRegion = await screen.findByRole('dialog', { name: '添加主机' })
    expect(installRegion).toHaveTextContent('linux initial command')

    fireEvent.click(screen.getByRole('button', { name: 'Windows' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Windows' })).toHaveAttribute('aria-pressed', 'true'))
    expect(installRegion).not.toHaveTextContent('linux initial command')
    expect(screen.getByText('正在生成安装命令...')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制安装命令' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Linux' }))
    pending.linux.shift()?.resolve({ command: 'linux latest command', install_token: 'linux-latest-token' })
    expect(await screen.findByText('linux latest command')).toBeInTheDocument()

    pending.windows.shift()?.reject(new Error('windows stale failure'))

    await waitFor(() => expect(installRegion).toHaveTextContent('linux latest command'))
    expect(screen.queryByText('windows stale failure')).not.toBeInTheDocument()
  })

  test('ignores install command responses after closing the panel', async () => {
    const pending: Record<'linux' | 'windows', Array<(value: { command: string, install_token: string }) => void>> = { linux: [], windows: [] }
    createInstallCommandMock.mockImplementation((platform = 'linux') => new Promise((resolve) => {
      pending[platform as 'linux' | 'windows'].push(resolve)
    }))

    render(<App />)

    await screen.findByText('全部 2')
    fireEvent.click(screen.getByRole('button', { name: '添加主机' }))
    pending.linux.shift()?.({ command: 'linux initial command', install_token: 'linux-initial-token' })
    expect(await screen.findByText('linux initial command')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Windows' }))
    fireEvent.click(screen.getByRole('button', { name: '关闭安装命令' }))
    expect(screen.queryByRole('dialog', { name: '添加主机' })).not.toBeInTheDocument()

    pending.windows.shift()?.({ command: 'windows late command', install_token: 'windows-late-token' })

    await waitFor(() => expect(screen.queryByText('windows late command')).not.toBeInTheDocument())
    expect(screen.queryByRole('dialog', { name: '添加主机' })).not.toBeInTheDocument()
  })

  test('copies with the selected text fallback when clipboard is unavailable', async () => {
    vi.stubGlobal('location', new URL('http://localhost:5173/nodes/node-1'))
    vi.stubGlobal('navigator', {})
    const execCommand = vi.fn(() => true)
    Object.assign(document, { execCommand })

    render(<App />)

    await screen.findByText('全部 2')
    fireEvent.click(screen.getByRole('button', { name: '添加主机' }))
    await screen.findByRole('dialog', { name: '添加主机' })
    await waitFor(() => expect(screen.getByRole('button', { name: '复制安装命令' })).not.toBeDisabled())

    fireEvent.click(screen.getByRole('button', { name: '复制安装命令' }))

    await waitFor(() => expect(execCommand).toHaveBeenCalledWith('copy'))
    expect(await screen.findByRole('button', { name: '已复制' })).toBeInTheDocument()
    expect(window.getSelection()?.toString()).toContain("--token 'generated-install-token'")
  })

  test('shows a manual copy warning when all copy paths fail', async () => {
    vi.stubGlobal('location', new URL('http://localhost:5173/nodes/node-1'))
    vi.stubGlobal('navigator', {})
    Object.assign(document, { execCommand: vi.fn(() => false) })

    render(<App />)

    await screen.findByText('全部 2')
    fireEvent.click(screen.getByRole('button', { name: '添加主机' }))
    await screen.findByRole('dialog', { name: '添加主机' })
    await waitFor(() => expect(screen.getByRole('button', { name: '复制安装命令' })).not.toBeDisabled())

    fireEvent.click(screen.getByRole('button', { name: '复制安装命令' }))

    expect(await screen.findByText('复制失败，已为你选中命令，请按 Ctrl+C 手动复制。')).toBeInTheDocument()
    expect(window.getSelection()?.toString()).toContain("--token 'generated-install-token'")
  })

  test('toggles dark theme and persists sidebar collapse state', async () => {
    render(<App />)

    await screen.findByText('全部 2')
    expect(document.documentElement.classList.contains('dark')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Dark' }))

    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(window.localStorage.getItem('mizupanel-theme')).toBe('dark')

    fireEvent.click(screen.getByRole('button', { name: '收起侧边栏' }))

    expect(window.localStorage.getItem('mizupanel-sidebar-collapsed')).toBe('true')
    expect(screen.getByRole('complementary', { name: 'MizuPanel 侧边栏' })).toHaveAttribute('data-collapsed', 'true')
  })
})
