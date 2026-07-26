import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { NodeDetail } from './NodeDetail'
import type { AgentLogsResponse, AgentRestartResponse, AgentStatusResponse, ConnectionDiagnostics, FileDeleteResponse, FileListResponse, FileReadResponse, FileUploadResponse, Node, RebootResponse, SSHUninstallRequest } from '../types'

const node: Node = {
  id: 'node-1',
  name: 'Oracle SG',
  hostname: 'oracle-sg',
  ip: '10.0.0.1',
  os: 'linux',
  arch: 'amd64',
  kernel: '6.6',
  agent_version: '0.1.0',
  status: 'online',
  last_seen_at: '2026-05-28T10:00:00Z',
  terminal_enabled: true,
  agent_mode: 'ops',
  agent_user: 'root'
}

const rootList: FileListResponse = {
  path: '/',
  entries: [
    { name: 'etc', path: '/etc', type: 'directory' },
    { name: 'app.conf', path: '/app.conf', type: 'file', size: 10 }
  ]
}

const etcList: FileListResponse = {
  path: '/etc',
  entries: [
    { name: 'mizupanel.yaml', path: '/etc/mizupanel.yaml', type: 'file', size: 24 }
  ]
}

const eventSources: FakeEventSource[] = []

class FakeEventSource extends EventTarget {
  onmessage: ((event: MessageEvent) => void) | null = null
  constructor(public url: string) {
    super()
    eventSources.push(this)
  }
  emit(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
  }
  close() {}
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolver) => {
    resolve = resolver
  })
  return { promise, resolve }
}

describe('NodeDetail file manager operations', () => {
  beforeEach(() => {
    eventSources.length = 0
    vi.stubGlobal('EventSource', FakeEventSource)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  test('opens terminal, switches to file panel, enters directories, edits text files and reboots with project modal', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const onLoadFiles = vi.fn(async (_nodeID: string, path: string): Promise<FileListResponse> => path === '/etc' ? etcList : rootList)
    const onReadFile = vi.fn(async (_nodeID: string, path: string): Promise<FileReadResponse> => ({ path, content: 'port=8080\n', editable: true, size: 10 }))
    const onWriteFile = vi.fn(async (_nodeID: string, path: string, content: string) => ({ path, saved: true, content }))
    const onRebootNode = vi.fn(async (): Promise<RebootResponse> => ({ accepted: true }))

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onLoadFiles={onLoadFiles}
        onReadFile={onReadFile}
        onWriteFile={onWriteFile}
        onRebootNode={onRebootNode}
      />
    )

    expect(screen.getByText('运维模式 · root')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '打开终端' }))
    expect(open).toHaveBeenCalledWith('/nodes/node-1/terminal', '_blank', 'noopener,noreferrer')

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    expect(await screen.findByRole('region', { name: '文件管理' })).toBeInTheDocument()
    expect(onLoadFiles).toHaveBeenCalledWith('node-1', '/')

    fireEvent.click(await screen.findByRole('button', { name: '进入目录 etc' }))
    expect(onLoadFiles).toHaveBeenLastCalledWith('node-1', '/etc')
    expect(await screen.findByText((_content, element) => element?.textContent === '当前路径：/etc')).toBeInTheDocument()

    fireEvent.click(await screen.findByRole('button', { name: '编辑文件 mizupanel.yaml' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑文件' })
    const editor = within(dialog).getByLabelText('文件内容')
    expect(editor).toHaveValue('port=8080\n')
    fireEvent.change(editor, { target: { value: 'port=9090\n' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存文件' }))
    expect(onWriteFile).toHaveBeenCalledWith('node-1', '/etc/mizupanel.yaml', 'port=9090\n')

    fireEvent.click(screen.getByRole('button', { name: '重启' }))
		const rebootDialog = screen.getByRole('dialog', { name: '重启节点' })
		expect(within(rebootDialog).getByText('root')).toBeInTheDocument()
		fireEvent.click(within(rebootDialog).getByRole('button', { name: '确认重启' }))
    expect(onRebootNode).toHaveBeenCalledWith('node-1')
		expect(await screen.findByText('节点重启命令下发成功')).toBeInTheDocument()
  })

	test('copies the current install command for a legacy Agent without leaving the Agent tab', async () => {
		const writeText = vi.fn(async () => undefined)
		Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
		const onGetLegacyAgentUpgradeCommand = vi.fn(async () => 'curl -fsSL http://panel/scripts/install-agent.sh')
		const diagnostics: ConnectionDiagnostics = {
			node_id: 'node-1',
			online: true,
			health: 'healthy',
			agent_version: '0.1.0',
			protocol_version: 1,
			identity_source: 'legacy_config',
			identity_conflict: false,
			upgrade_supported: false,
			latest_version: '0.1.1',
			upgrade_available: true,
			events: [],
		}

		render(
			<NodeDetail
				node={node}
				metrics={[]}
				processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
				dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
				range="1h"
				onRangeChange={vi.fn()}
				onGetConnectionDiagnostics={async () => diagnostics}
				onGetLegacyAgentUpgradeCommand={onGetLegacyAgentUpgradeCommand}
			/>
		)

		fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
		const copyButton = await screen.findByRole('button', { name: '复制升级命令' })
		fireEvent.click(copyButton)
		await waitFor(() => expect(onGetLegacyAgentUpgradeCommand).toHaveBeenCalledTimes(1))
		expect(writeText).toHaveBeenCalledWith('curl -fsSL http://panel/scripts/install-agent.sh')
		expect(await screen.findByText('Agent升级命令复制成功')).toBeInTheDocument()
		expect(screen.getByRole('region', { name: 'Agent 管理' })).toBeInTheDocument()
	})

  test('keeps node actions on the detail header right without a separate remove-node action', () => {
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
      />
    )

    const actions = screen.getByRole('toolbar', { name: '节点操作' })
    expect(within(actions).getByRole('button', { name: '打开终端' })).toBeInTheDocument()
    expect(within(actions).getByRole('button', { name: '分组与标签' })).toBeInTheDocument()
    expect(within(actions).getByRole('button', { name: '重启' })).toBeInTheDocument()
    expect(within(actions).queryByRole('button', { name: '卸载 Agent' })).not.toBeInTheDocument()
    const views = screen.getByRole('group', { name: '节点详情视图' })
    expect(within(views).getByRole('button', { name: '文件管理' })).toBeInTheDocument()
    expect(within(views).getByRole('button', { name: 'Agent 管理' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '移除节点记录' })).not.toBeInTheDocument()
  })

  test('groups SSH uninstall step logs and closes from the bottom after completion', async () => {
    const onSSHUninstall = vi.fn(async (_nodeID: string, _request: SSHUninstallRequest) => ({ job_id: 'ssh-uninstall-1' }))

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onSSHUninstall={onSSHUninstall}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    const agentPanel = await screen.findByRole('region', { name: 'Agent 管理' })
    fireEvent.click(within(agentPanel).getByRole('button', { name: '卸载 Agent' }))
    const dialog = screen.getByRole('dialog', { name: '卸载 Agent' })
    expect(within(dialog).getByDisplayValue('10.0.0.1')).toBeInTheDocument()
    expect(within(dialog).getByDisplayValue('root')).toBeInTheDocument()
    fireEvent.change(within(dialog).getByLabelText('SSH 密码'), { target: { value: 'secret' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '开始 SSH 卸载' }))

    await waitFor(() => expect(onSSHUninstall).toHaveBeenCalledWith('node-1', expect.objectContaining({
      host: '10.0.0.1',
      port: 22,
      username: 'root',
      auth_type: 'password',
      password: 'secret',
      remove_node_record: true
    })))
    expect(await within(dialog).findByText('SSH 卸载任务已创建：ssh-uninstall-1')).toBeInTheDocument()
    expect(eventSources[0]?.url).toBe('/api/nodes/node-1/ssh-uninstall/ssh-uninstall-1/events')
    act(() => {
      eventSources[0].emit({ step: 'run_uninstall', label: '执行卸载', status: 'running', message: '正在执行 Agent 卸载脚本' })
      eventSources[0].emit({ step: 'run_uninstall', label: '执行卸载', status: 'success', message: 'Agent 卸载脚本执行完成' })
      eventSources[0].emit({ step: 'done', label: '完成', status: 'success', message: '任务已完成', done: true })
    })
    expect(await within(dialog).findByText('执行卸载')).toBeInTheDocument()
    expect(within(dialog).getByText('Agent 卸载脚本执行完成')).toBeInTheDocument()
    expect(within(dialog).getByText('正在执行 Agent 卸载脚本')).toBeInTheDocument()
    expect(within(dialog).getAllByText('执行卸载')).toHaveLength(1)
    expect(within(dialog).getAllByText('成功').length).toBeGreaterThan(0)
    const closeButtons = within(dialog).getAllByRole('button', { name: '关闭' })
    fireEvent.click(closeButtons[closeButtons.length - 1])
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '卸载 Agent' })).not.toBeInTheDocument())
  })

  test('shows reboot feedback from the default overview view', async () => {
    const onRebootNode = vi.fn(async (): Promise<RebootResponse> => ({ accepted: true }))

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onRebootNode={onRebootNode}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: '重启' }))
    const dialog = screen.getByRole('dialog', { name: '重启节点' })
    expect(within(dialog).getByText('root')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认重启' }))
    expect(onRebootNode).toHaveBeenCalledWith('node-1')
    expect(await screen.findByText('节点重启命令下发成功')).toBeInTheDocument()
  })

  test('shows Agent management status, recent logs and restart feedback', async () => {
    const onGetAgentStatus = vi.fn(async (): Promise<AgentStatusResponse> => ({
      version: '0.1.0',
      user: 'root',
      mode: 'ops',
      terminal_enabled: true,
      docker_available: true,
      config_path: '/usr/local/mizupanel/agent.yaml',
      service_name: 'mizupanel-agent',
      uptime: 3661,
      collected_at: 1710000000
    }))
    const onGetAgentLogs = vi.fn(async (): Promise<AgentLogsResponse> => ({ lines: 100, content: 'mizupanel-agent started', collected_at: 1710000001 }))
    const onRestartAgent = vi.fn(async (): Promise<AgentRestartResponse> => ({ accepted: true, message: '重启命令已下发，等待 Agent 重新连接' }))

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onGetAgentStatus={onGetAgentStatus}
        onRestartAgent={onRestartAgent}
        onGetAgentLogs={onGetAgentLogs}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    const panel = await screen.findByRole('region', { name: 'Agent 管理' })

    await waitFor(() => expect(onGetAgentStatus).toHaveBeenCalledWith('node-1'))
    expect(onGetAgentLogs).toHaveBeenCalledWith('node-1', 100)
    expect(within(panel).getByText('mizupanel-agent')).toBeInTheDocument()
    expect(within(panel).getByText('0.1.0')).toBeInTheDocument()
    expect(within(panel).getByText('root')).toBeInTheDocument()
    expect(within(panel).getByText('运维模式')).toBeInTheDocument()
    expect(within(panel).getByText('1 小时 1 分钟')).toBeInTheDocument()
    expect(within(panel).getByText('mizupanel-agent started')).toBeInTheDocument()

    fireEvent.click(within(panel).getByRole('button', { name: '重启 Agent' }))
    const dialog = screen.getByRole('dialog', { name: '重启 Agent' })
    expect(within(dialog).getByText('mizupanel-agent')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认重启' }))
    expect(onRestartAgent).toHaveBeenCalledWith('node-1')
    expect(await screen.findByText('Agent重启命令下发成功')).toBeInTheDocument()
  })

  test('clears stale Agent management data when switching to an offline node', async () => {
    const onGetAgentStatus = vi.fn(async (): Promise<AgentStatusResponse> => ({
      version: '0.1.0',
      user: 'root',
      mode: 'ops',
      terminal_enabled: true,
      docker_available: true,
      config_path: '/usr/local/mizupanel/agent.yaml',
      service_name: 'mizupanel-agent',
      uptime: 3661,
      collected_at: 1710000000
    }))
    const onGetAgentLogs = vi.fn(async (): Promise<AgentLogsResponse> => ({ lines: 100, content: 'node-a-log', collected_at: 1710000001 }))
    const offlineNode = { ...node, id: 'node-2', name: 'Tokyo JP', status: 'offline' }

    const { rerender } = render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onGetAgentStatus={onGetAgentStatus}
        onGetAgentLogs={onGetAgentLogs}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    expect(await screen.findByText('node-a-log')).toBeInTheDocument()

    rerender(
      <NodeDetail
        node={offlineNode}
        metrics={[]}
        processSnapshot={{ node_id: 'node-2', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-2', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onGetAgentStatus={onGetAgentStatus}
        onGetAgentLogs={onGetAgentLogs}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))

    expect(await screen.findByText('节点离线，状态与日志暂不可用；连接诊断仍可查看。')).toBeInTheDocument()
    expect(screen.queryByText('node-a-log')).not.toBeInTheDocument()
    expect(screen.queryByText('/usr/local/mizupanel/agent.yaml')).not.toBeInTheDocument()
  })

  test('shows Agent logs errors while keeping the loaded status visible', async () => {
    const onGetAgentStatus = vi.fn(async (): Promise<AgentStatusResponse> => ({
      version: '0.1.0',
      user: 'root',
      mode: 'ops',
      terminal_enabled: true,
      docker_available: true,
      service_name: 'mizupanel-agent',
      uptime: 3661,
      collected_at: 1710000000
    }))
    const onGetAgentLogs = vi.fn(async (): Promise<AgentLogsResponse> => ({ lines: 100, error: 'journalctl failed', code: 'failed', collected_at: 1710000001 }))

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onGetAgentStatus={onGetAgentStatus}
        onGetAgentLogs={onGetAgentLogs}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    const panel = await screen.findByRole('region', { name: 'Agent 管理' })

    expect(await within(panel).findByText('journalctl failed')).toBeInTheDocument()
    expect(within(panel).getByText('0.1.0')).toBeInTheDocument()
    expect(within(panel).getByText('暂无 Agent 日志，点击刷新日志后查看。')).toBeInTheDocument()
  })

  test('clears stale Agent management data when the selected node goes offline', async () => {
    const onGetAgentStatus = vi.fn(async (): Promise<AgentStatusResponse> => ({
      version: '0.1.0',
      user: 'root',
      mode: 'ops',
      terminal_enabled: true,
      docker_available: true,
      config_path: '/usr/local/mizupanel/agent.yaml',
      service_name: 'mizupanel-agent',
      uptime: 3661,
      collected_at: 1710000000
    }))
    const onGetAgentLogs = vi.fn(async (): Promise<AgentLogsResponse> => ({ lines: 100, content: 'same-node-log', collected_at: 1710000001 }))

    const { rerender } = render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onGetAgentStatus={onGetAgentStatus}
        onGetAgentLogs={onGetAgentLogs}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    expect(await screen.findByText('same-node-log')).toBeInTheDocument()

    rerender(
      <NodeDetail
        node={{ ...node, status: 'offline' }}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onGetAgentStatus={onGetAgentStatus}
        onGetAgentLogs={onGetAgentLogs}
      />
    )

    expect(await screen.findByText('节点离线，状态与日志暂不可用；连接诊断仍可查看。')).toBeInTheDocument()
    expect(screen.queryByText('same-node-log')).not.toBeInTheDocument()
    expect(screen.queryByText('/usr/local/mizupanel/agent.yaml')).not.toBeInTheDocument()
  })

  test('reloads Agent management for the newly selected node and ignores stale responses', async () => {
    const nodeOneStatus = deferred<AgentStatusResponse>()
    const nodeOneLogs = deferred<AgentLogsResponse>()
    const nodeTwoStatus = deferred<AgentStatusResponse>()
    const nodeTwoLogs = deferred<AgentLogsResponse>()
    const onGetAgentStatus = vi.fn((_nodeID: string): Promise<AgentStatusResponse> => _nodeID === 'node-2' ? nodeTwoStatus.promise : nodeOneStatus.promise)
    const onGetAgentLogs = vi.fn((_nodeID: string): Promise<AgentLogsResponse> => _nodeID === 'node-2' ? nodeTwoLogs.promise : nodeOneLogs.promise)
    const nextNode = { ...node, id: 'node-2', name: 'Tokyo JP', ip: '10.0.0.2' }

    const { rerender } = render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onGetAgentStatus={onGetAgentStatus}
        onGetAgentLogs={onGetAgentLogs}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    await waitFor(() => expect(onGetAgentStatus).toHaveBeenCalledWith('node-1'))

    rerender(
      <NodeDetail
        node={nextNode}
        metrics={[]}
        processSnapshot={{ node_id: 'node-2', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-2', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onGetAgentStatus={onGetAgentStatus}
        onGetAgentLogs={onGetAgentLogs}
      />
    )

    await waitFor(() => expect(onGetAgentStatus).toHaveBeenCalledWith('node-2'))
    await act(async () => {
      nodeOneStatus.resolve({ version: 'stale', user: 'old-root', mode: 'ops', terminal_enabled: true, docker_available: true, service_name: 'old-agent', uptime: 1 })
      nodeOneLogs.resolve({ lines: 100, content: 'old-node-log' })
      nodeTwoStatus.resolve({ version: '0.2.0', user: 'new-root', mode: 'ops', terminal_enabled: true, docker_available: true, service_name: 'new-agent', uptime: 120 })
      nodeTwoLogs.resolve({ lines: 100, content: 'new-node-log' })
    })

    const panel = await screen.findByRole('region', { name: 'Agent 管理' })
    expect(await within(panel).findByText('new-node-log')).toBeInTheDocument()
    expect(within(panel).getByText('0.2.0')).toBeInTheDocument()
    expect(within(panel).queryByText('old-node-log')).not.toBeInTheDocument()
    expect(within(panel).queryByText('stale')).not.toBeInTheDocument()
  })

  test('uploads files to the current directory and deletes entries after confirmation', async () => {
    let entries = [...etcList.entries]
    const onLoadFiles = vi.fn(async (_nodeID: string, path: string): Promise<FileListResponse> => ({ path, entries }))
    const onUploadFile = vi.fn(async (_nodeID: string, path: string, contentBase64: string): Promise<FileUploadResponse> => {
      entries = [{ name: 'upload.bin', path, type: 'binary', size: 3 }, ...entries]
      return { path, contentBase64, uploaded: true, size: 3 } as FileUploadResponse
    })
    const onDeletePath = vi.fn(async (_nodeID: string, path: string): Promise<FileDeleteResponse> => {
      entries = entries.filter((entry) => entry.path !== path)
      return { path, deleted: true }
    })

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onLoadFiles={onLoadFiles}
        onUploadFile={onUploadFile}
        onDeletePath={onDeletePath}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    fireEvent.change(await screen.findByLabelText('直接打开路径'), { target: { value: '/etc' } })
    fireEvent.click(screen.getByRole('button', { name: '打开路径' }))
    expect(await screen.findByText((_content, element) => element?.textContent === '当前路径：/etc')).toBeInTheDocument()
    expect(await screen.findByText('mizupanel.yaml')).toBeInTheDocument()

    const file = new File([new Uint8Array([0, 1, 2])], 'upload.bin', { type: 'application/octet-stream' })
    fireEvent.change(screen.getByLabelText('上传文件'), { target: { files: [file] } })
    await waitFor(() => expect(onUploadFile).toHaveBeenCalledWith('node-1', '/etc/upload.bin', 'AAEC'))
    expect(await screen.findByText('文件已上传。')).toBeInTheDocument()
    expect(await screen.findByText('upload.bin')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '删除 upload.bin' }))
    expect(screen.getByText('确认删除 upload.bin？非空目录不会递归删除。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(onDeletePath).toHaveBeenCalledWith('node-1', '/etc/upload.bin'))
    expect(await screen.findByText('文件已删除。')).toBeInTheDocument()
    expect(screen.queryByText('upload.bin')).not.toBeInTheDocument()
  })

  test('opens direct paths as directories or editable files and reports missing paths', async () => {
    const onLoadFiles = vi.fn(async (_nodeID: string, path: string): Promise<FileListResponse> => {
      if (path === '/var/log') return { path, entries: [{ name: 'app.log', path: '/var/log/app.log', type: 'file', size: 12 }] }
      if (path === '/etc/app.conf') return { path, entries: [], code: 'not_directory', error: '路径不是目录。' }
      return { path, entries: [], code: 'not_found', error: '路径不存在或已被删除。' }
    })
    const onReadFile = vi.fn(async (_nodeID: string, path: string): Promise<FileReadResponse> => ({ path, content: 'hello\n', editable: true, size: 6 }))

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onLoadFiles={onLoadFiles}
        onReadFile={onReadFile}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    const pathInput = await screen.findByLabelText('直接打开路径')
    fireEvent.change(pathInput, { target: { value: '/var/log' } })
    fireEvent.click(screen.getByRole('button', { name: '打开路径' }))
    expect(onLoadFiles).toHaveBeenLastCalledWith('node-1', '/var/log')
    expect(await screen.findByText('app.log')).toBeInTheDocument()

    fireEvent.change(pathInput, { target: { value: '/etc/app.conf' } })
    fireEvent.click(screen.getByRole('button', { name: '打开路径' }))
    await waitFor(() => expect(onReadFile).toHaveBeenCalledWith('node-1', '/etc/app.conf'))
    expect(await screen.findByRole('dialog', { name: '编辑文件' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '关闭编辑器' }))
    fireEvent.change(pathInput, { target: { value: '/missing' } })
    fireEvent.click(screen.getByRole('button', { name: '打开路径' }))
    expect(await screen.findByText('路径不存在或已被删除。')).toBeInTheDocument()
  })

  test('keeps the latest directory when an older path response finishes later', async () => {
    const oldPath = deferred<FileListResponse>()
    const latestPath = deferred<FileListResponse>()
    const onLoadFiles = vi.fn((_nodeID: string, path: string): Promise<FileListResponse> => {
      if (path === '/var/log') return oldPath.promise
      if (path === '/tmp') return latestPath.promise
      return Promise.resolve(rootList)
    })

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onLoadFiles={onLoadFiles}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    const pathInput = await screen.findByLabelText('直接打开路径')

    fireEvent.change(pathInput, { target: { value: '/var/log' } })
    fireEvent.click(screen.getByRole('button', { name: '打开路径' }))
    fireEvent.change(pathInput, { target: { value: '/tmp' } })
    fireEvent.click(screen.getByRole('button', { name: '打开路径' }))

    await act(async () => {
      latestPath.resolve({ path: '/tmp', entries: [{ name: 'current.log', path: '/tmp/current.log', type: 'file', size: 1 }] })
    })
    expect(await screen.findByText('current.log')).toBeInTheDocument()

    await act(async () => {
      oldPath.resolve({ path: '/var/log', entries: [{ name: 'stale.log', path: '/var/log/stale.log', type: 'file', size: 1 }] })
    })
    expect(screen.getByText((_content, element) => element?.textContent === '当前路径：/tmp')).toBeInTheDocument()
    expect(screen.queryByText('stale.log')).not.toBeInTheDocument()
  })

  test('keeps the latest file when an older read response finishes later', async () => {
    const oldRead = deferred<FileReadResponse>()
    const latestRead = deferred<FileReadResponse>()
    const onLoadFiles = vi.fn(async (): Promise<FileListResponse> => ({
      path: '/',
      entries: [
        { name: 'old.conf', path: '/old.conf', type: 'file', size: 3 },
        { name: 'latest.conf', path: '/latest.conf', type: 'file', size: 6 }
      ]
    }))
    const onReadFile = vi.fn((_nodeID: string, path: string): Promise<FileReadResponse> => {
      if (path === '/old.conf') return oldRead.promise
      return latestRead.promise
    })

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onLoadFiles={onLoadFiles}
        onReadFile={onReadFile}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    fireEvent.click(await screen.findByRole('button', { name: '编辑文件 old.conf' }))
    fireEvent.click(await screen.findByRole('button', { name: '编辑文件 latest.conf' }))

    await act(async () => {
      latestRead.resolve({ path: '/latest.conf', content: 'latest', editable: true, size: 6 })
    })
    const dialog = await screen.findByRole('dialog', { name: '编辑文件' })
    expect(within(dialog).getByText('/latest.conf')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('文件内容')).toHaveValue('latest')

    await act(async () => {
      oldRead.resolve({ path: '/old.conf', content: 'old', editable: true, size: 3 })
    })
    expect(within(dialog).getByText('/latest.conf')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('文件内容')).toHaveValue('latest')
  })

  test('clears directory loading when opening a visible file cancels an older directory request', async () => {
    const pendingDirectory = deferred<FileListResponse>()
    const onLoadFiles = vi.fn((_nodeID: string, path: string): Promise<FileListResponse> => {
      if (path === '/var/log') return pendingDirectory.promise
      return Promise.resolve(rootList)
    })
    const onReadFile = vi.fn(async (_nodeID: string, path: string): Promise<FileReadResponse> => ({ path, content: 'port=8080\n', editable: true, size: 10 }))

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onLoadFiles={onLoadFiles}
        onReadFile={onReadFile}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    const pathInput = await screen.findByLabelText('直接打开路径')
    fireEvent.change(pathInput, { target: { value: '/var/log' } })
    fireEvent.click(screen.getByRole('button', { name: '打开路径' }))
    expect(await screen.findByText('正在加载目录...')).toBeInTheDocument()

    fireEvent.click(await screen.findByRole('button', { name: '编辑文件 app.conf' }))
    expect(await screen.findByRole('dialog', { name: '编辑文件' })).toBeInTheDocument()
    expect(screen.queryByText('正在加载目录...')).not.toBeInTheDocument()

    await act(async () => {
      pendingDirectory.resolve({ path: '/var/log', entries: [{ name: 'stale.log', path: '/var/log/stale.log', type: 'file', size: 1 }] })
    })
    expect(screen.queryByText('stale.log')).not.toBeInTheDocument()
  })

  test('shows binary files as not editable', async () => {
    const onLoadFiles = vi.fn(async (): Promise<FileListResponse> => ({
      path: '/',
      entries: [{ name: 'image.bin', path: '/image.bin', type: 'binary', size: 3 }]
    }))

    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onLoadFiles={onLoadFiles}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    const panel = await screen.findByRole('region', { name: '文件管理' })
    expect(await within(panel).findByText(/二进制文件/)).toBeInTheDocument()
    expect(within(panel).getByText('不可编辑')).toBeInTheDocument()
  })
})
