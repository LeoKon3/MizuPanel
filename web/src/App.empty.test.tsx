import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import App from './App'
import { createInstallCommand, getNodes } from './api/client'

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

vi.mock('./api/client', () => ({
  setUnauthorizedHandler: vi.fn(),
  getAuthSession: vi.fn(async () => ({ auth_enabled: false, authenticated: true, username: '' })),
  createInstallCommand: vi.fn(),
  getNodes: vi.fn(async () => ({ nodes: [] })),
  getNodeMetrics: vi.fn(async () => ({ metrics: [] })),
  getNodeProcesses: vi.fn(async () => ({ node_id: '', collected_at: 0, error: '', processes: [] })),
  getNodeDocker: vi.fn(async () => ({ node_id: '', collected_at: 0, available: false, error: '', containers: [] })),
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
    expect(screen.getByText('token 来源：点击添加主机时，Server 会自动生成一次性 install_token。')).toBeInTheDocument()
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
})
