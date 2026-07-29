import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import App from './App'
import { createInstallCommand, getNodes } from './api/client'
import type { Node } from './types'

const linuxInstallResponse = {
  command: [
    `curl -fsSL 'http://panel.example:8080/scripts/install-agent.sh' -o install-agent.sh \\`,
    `  && chmod +x install-agent.sh \\`,
    `  && ./install-agent.sh \\`,
    `    --binary-base-url 'http://panel.example:8080/downloads' \\`,
    `    --server-url 'ws://panel.example:8080/api/agent/ws' \\`,
    `    --token 'generated-install-token' \\`,
    `    --node-id "$(hostname)" \\`,
    `    --name "$(hostname)" \\`,
    `    --mode 'ops' \\`,
    `    --enable-docker \\`,
    `    --enable-terminal`
  ].join('\n'),
  install_token: 'generated-install-token'
}

const windowsInstallResponse = {
  command: [
    `powershell -NoProfile -ExecutionPolicy Bypass -Command "\`$ErrorActionPreference='Stop'; \`$script = Join-Path \`$env:TEMP ('mizupanel-install-' + [guid]::NewGuid().ToString() + '.ps1'); Invoke-WebRequest -Uri 'http://panel.example:8080/scripts/install-agent.ps1' -UseBasicParsing -OutFile \`$script -ErrorAction Stop; & \`$script `,
    `    -BinaryBaseUrl 'http://panel.example:8080/downloads' `,
    `    -ServerUrl 'ws://panel.example:8080/api/agent/ws' `,
    `    -Token 'generated-windows-token' `,
    `    -NodeId \`$env:COMPUTERNAME `,
    `    -Name \`$env:COMPUTERNAME"`
  ].join('\n'),
  install_token: 'generated-windows-token'
}

const registeredNode: Node = {
  id: 'node-new',
  name: 'New Agent Host',
  hostname: 'new-agent-host',
  ip: '10.0.0.9',
  os: 'linux',
  arch: 'amd64',
  kernel: '6.8',
  agent_version: '0.1.12',
  status: 'online',
  last_seen_at: '2026-07-29T10:00:00Z'
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

vi.mock('./api/client', () => ({
  setUnauthorizedHandler: vi.fn(),
  getAuthSession: vi.fn(async () => ({ auth_enabled: false, authenticated: true, username: '' })),
  login: vi.fn(async () => ({ authenticated: true, username: 'admin' })),
  logout: vi.fn(async () => undefined),
  createInstallCommand: vi.fn(),
  getNodes: vi.fn(async () => ({ nodes: [] })),
  getNodeMetrics: vi.fn(async () => ({ metrics: [] })),
  getNodeProcesses: vi.fn(async () => ({ node_id: '', collected_at: 0, error: '', processes: [] })),
  getNodeDocker: vi.fn(async () => ({ node_id: '', collected_at: 0, available: false, error: '', containers: [] })),
  getNodeDockerCompose: vi.fn(async () => ({ success: true, supported: false, projects: [] })),
  getNodeDockerResources: vi.fn(async () => ({ success: true, supported: false, usage: {}, images: [], volumes: [], networks: [] })),
  getNodeSystemdServices: vi.fn(async () => ({ success: true, supported: false, services: [] })),
  runNodeDockerComposeAction: vi.fn(async () => ({ success: true })),
  runNodeDockerComposeDeployment: vi.fn(async () => ({ success: true, project: undefined })),
  runNodeDockerResourceAction: vi.fn(async () => ({ success: true })),
  runNodeSystemdServiceAction: vi.fn(async () => ({ success: true })),
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
  getAgentStatus: vi.fn(async () => ({ version: '0.1.12', user: 'root', mode: 'ops', terminal_enabled: true, docker_available: true, service_name: 'mizupanel-agent', uptime: 60, collected_at: 1710000000 })),
  getConnectionDiagnostics: vi.fn(async () => ({ node_id: 'node-1', online: true, health: 'healthy', agent_version: '0.1.0', protocol_version: 1, identity_conflict: false, upgrade_supported: false, latest_version: '0.1.1', upgrade_available: true, events: [] })),
  restartAgent: vi.fn(async () => ({ accepted: true, message: '重启命令已下发，等待 Agent 重新连接' })),
  upgradeAgent: vi.fn(async () => ({ accepted: true, stage: 'preparing' })),
  getAgentUpgradeStatus: vi.fn(async () => ({ node_id: 'node-1', target_version: '0.1.1', actual_version: '0.1.1', stage: 'completed' })),
  getAgentLogs: vi.fn(async () => ({ lines: 100, content: '', collected_at: 1710000001 })),
  createTerminalSession: vi.fn(async () => ({ token: 'terminal-token' })),
  createContainerExecSession: vi.fn(async () => ({ token: 'exec-token' })),
  startSSHInstall: vi.fn(async () => ({ job_id: 'ssh-install-1' })),
  startSSHUninstall: vi.fn(async () => ({ job_id: 'ssh-uninstall-1' }))
}))

const createInstallCommandMock = vi.mocked(createInstallCommand)
const getNodesMock = vi.mocked(getNodes)

beforeEach(() => {
  createInstallCommandMock.mockReset()
  createInstallCommandMock.mockImplementation(async (platform = 'linux') => {
    if (platform === 'windows') return windowsInstallResponse
    return linuxInstallResponse
  })
  getNodesMock.mockReset()
  getNodesMock.mockResolvedValue({ nodes: [] })
})

describe('App empty state', () => {
  test('opens the dashboard directly without a login dialog', async () => {
    render(<App />)

    expect(await screen.findByText('暂无节点接入')).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '登录 MizuPanel' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '退出登录' })).not.toBeInTheDocument()
    expect(getNodesMock).toHaveBeenCalledTimes(1)
  })

  test('shows a button that reveals a generated install command when no nodes are registered', async () => {
    render(<App />)

    expect(await screen.findByText('暂无节点接入')).toBeInTheDocument()
    expect(screen.getByText('在目标服务器执行 Agent 安装命令后，节点会自动出现在这里。')).toBeInTheDocument()
    expect(screen.queryByText('curl -fsSL')).not.toBeInTheDocument()

    const installButton = screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' })
    expect(installButton).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(installButton)

    expect(await screen.findByText(/ws:\/\/panel\.example:8080\/api\/agent\/ws/)).toBeInTheDocument()
    expect(createInstallCommandMock).toHaveBeenCalledWith('linux')
    expect(installButton).toHaveAttribute('aria-expanded', 'true')
    const installRegion = screen.getByRole('dialog', { name: '添加主机' })
    expect(screen.getByRole('button', { name: 'Linux' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Windows' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.queryByLabelText('启用 Docker 容器监控')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('启用节点终端')).not.toBeInTheDocument()
    expect(screen.queryByText('Agent 运行模式')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /普通模式/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /运维模式/ })).not.toBeInTheDocument()
    expect(screen.getByText('默认以 root 运维模式安装，自动启用节点终端与 Docker 容器监控。')).toBeInTheDocument()
    expect(installRegion).toHaveTextContent(/curl -fsSL 'http:\/\/panel\.example:8080\/scripts\/install-agent\.sh' -o install-agent\.sh \\\s+&& chmod \+x install-agent\.sh \\\s+&& \.\/install-agent\.sh \\\s+--binary-base-url 'http:\/\/panel\.example:8080\/downloads' \\\s+--server-url 'ws:\/\/panel\.example:8080\/api\/agent\/ws' \\\s+--token 'generated-install-token'/)
    expect(installRegion).toHaveTextContent("--mode 'ops'")
    expect(installRegion).toHaveTextContent('--enable-docker')
    expect(installRegion).toHaveTextContent('--enable-terminal')

    fireEvent.click(screen.getByRole('button', { name: 'Windows' }))

    expect(await screen.findByText(/install-agent\.ps1/)).toBeInTheDocument()
    expect(createInstallCommandMock).toHaveBeenCalledWith('windows')
    expect(screen.getByRole('button', { name: 'Linux' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Windows' })).toHaveAttribute('aria-pressed', 'true')
    expect(installRegion).toHaveTextContent(/powershell -NoProfile -ExecutionPolicy Bypass/)
    expect(installRegion).toHaveTextContent(/-NodeId `\$env:COMPUTERNAME/)
    expect(screen.getByText('Windows 命令需要在管理员 PowerShell 中执行。')).toBeInTheDocument()
    expect(screen.getByText('Windows 暂不支持 Docker 监控和节点终端安装配置。')).toBeInTheDocument()
    expect(screen.queryByLabelText('启用 Docker 容器监控')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('启用节点终端')).not.toBeInTheDocument()
    expect(installRegion).not.toHaveTextContent('--enable-docker')
    expect(installRegion).not.toHaveTextContent('--enable-terminal')
    expect(screen.getByText('token 来源：点击添加主机时，Server 会自动生成短期引导 install_token。')).toBeInTheDocument()
    expect(screen.queryByText('Select a node')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '关闭安装命令' }))

    expect(screen.queryByRole('dialog', { name: '添加主机' })).not.toBeInTheDocument()
    expect(installButton).toHaveFocus()
  })

  test('closes the empty-state install dialog with Escape and restores focus', async () => {
    render(<App />)

    expect(await screen.findByText('暂无节点接入')).toBeInTheDocument()
    const installButton = screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' })
    fireEvent.click(installButton)

    const installDialog = await screen.findByRole('dialog', { name: '添加主机' })
    await waitFor(() => expect(installDialog).toHaveFocus())
    fireEvent.keyDown(installDialog, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '添加主机' })).not.toBeInTheDocument())
    expect(installButton).toHaveFocus()
  })

  test('shows empty-state install command failures inside the dialog', async () => {
    createInstallCommandMock.mockRejectedValueOnce(new Error('安装命令生成失败'))

    render(<App />)

    expect(await screen.findByText('暂无节点接入')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' }))

    const installDialog = await screen.findByRole('dialog', { name: '添加主机' })
    expect(await within(installDialog).findByText('安装命令生成失败')).toBeInTheDocument()
    expect(within(installDialog).getByRole('button', { name: '复制安装命令' })).toBeDisabled()
  })

  test('shows a newly registered node without reopening the install dialog', async () => {
    vi.useFakeTimers()
    getNodesMock
      .mockResolvedValueOnce({ nodes: [] })
      .mockResolvedValue({ nodes: [registeredNode] })
    const { unmount } = render(<App />)

    try {
      await act(async () => undefined)
      expect(screen.getByText('暂无节点接入')).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' }))
      await act(async () => undefined)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(3_000)
      })

      expect(screen.getByRole('dialog', { name: '添加主机' })).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: '关闭安装命令' }))

      expect(screen.queryByRole('dialog', { name: '添加主机' })).not.toBeInTheDocument()
      expect(screen.getAllByText('New Agent Host').length).toBeGreaterThan(0)
      expect(createInstallCommandMock).toHaveBeenCalledTimes(1)
      expect(window.location.pathname).toBe('/')
    } finally {
      unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

  test('stops node discovery polling when the install dialog closes', async () => {
    vi.useFakeTimers()
    const { unmount } = render(<App />)

    try {
      await act(async () => undefined)
      fireEvent.click(screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' }))
      await act(async () => undefined)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3_000)
      })
      expect(getNodesMock).toHaveBeenCalledTimes(2)

      fireEvent.click(screen.getByRole('button', { name: '关闭安装命令' }))
      await act(async () => {
        await vi.advanceTimersByTimeAsync(30_000)
      })

      expect(getNodesMock).toHaveBeenCalledTimes(2)
    } finally {
      unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

  test('stops node discovery polling after the two-minute install window', async () => {
    vi.useFakeTimers()
    const { unmount } = render(<App />)

    try {
      await act(async () => undefined)
      fireEvent.click(screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' }))
      await act(async () => undefined)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2 * 60_000)
      })
      const callsAtDeadline = getNodesMock.mock.calls.length
      expect(callsAtDeadline).toBeGreaterThan(1)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000)
      })
      expect(getNodesMock).toHaveBeenCalledTimes(callsAtDeadline)
    } finally {
      unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

  test('waits for a pending node refresh before scheduling the next poll', async () => {
    vi.useFakeTimers()
    const pendingRefresh = deferred<{ nodes: Node[] }>()
    getNodesMock
      .mockResolvedValueOnce({ nodes: [] })
      .mockImplementationOnce(() => pendingRefresh.promise)
      .mockResolvedValue({ nodes: [] })
    const { unmount } = render(<App />)

    try {
      await act(async () => undefined)
      fireEvent.click(screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' }))
      await act(async () => undefined)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3_000)
      })
      expect(getNodesMock).toHaveBeenCalledTimes(2)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(30_000)
      })
      expect(getNodesMock).toHaveBeenCalledTimes(2)

      await act(async () => {
        pendingRefresh.resolve({ nodes: [] })
        await pendingRefresh.promise
      })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2_999)
      })
      expect(getNodesMock).toHaveBeenCalledTimes(2)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1)
      })
      expect(getNodesMock).toHaveBeenCalledTimes(3)
    } finally {
      unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

  test('aborts a pending node refresh and starts fresh after the install dialog reopens', async () => {
    vi.useFakeTimers()
    const staleRefresh = deferred<{ nodes: Node[] }>()
    let staleSignal: AbortSignal | undefined
    getNodesMock
      .mockResolvedValueOnce({ nodes: [] })
      .mockImplementationOnce((signal) => {
        staleSignal = signal
        return staleRefresh.promise
      })
      .mockResolvedValue({ nodes: [] })
    const { unmount } = render(<App />)

    try {
      await act(async () => undefined)
      fireEvent.click(screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' }))
      await act(async () => undefined)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3_000)
      })
      expect(getNodesMock).toHaveBeenCalledTimes(2)
      expect(staleSignal?.aborted).toBe(false)

      fireEvent.click(screen.getByRole('button', { name: '关闭安装命令' }))
      expect(staleSignal?.aborted).toBe(true)
      fireEvent.click(screen.getByRole('button', { name: '安装目标主机 Agent 进行采集' }))
      await act(async () => undefined)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3_000)
      })
      expect(getNodesMock).toHaveBeenCalledTimes(3)

      await act(async () => {
        staleRefresh.resolve({ nodes: [registeredNode] })
        await staleRefresh.promise
      })
      expect(screen.queryByText('New Agent Host')).not.toBeInTheDocument()
    } finally {
      unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })
})
