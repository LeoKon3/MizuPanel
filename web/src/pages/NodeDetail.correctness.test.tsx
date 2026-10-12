import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { NodeDetail } from './NodeDetail'
import type { AgentUpgradeStatus, FileListResponse, FileReadResponse, Node } from '../types'

const nodeA: Node = {
  id: 'node-a',
  name: 'Node A',
  hostname: 'node-a',
  ip: '10.0.0.1',
  os: 'linux',
  arch: 'amd64',
  kernel: '6.8',
  agent_version: '0.1.0',
  status: 'online',
  last_seen_at: '2026-07-01T00:00:00Z',
  terminal_enabled: true,
  agent_mode: 'ops',
  agent_user: 'root',
}
const nodeB: Node = { ...nodeA, id: 'node-b', name: 'Node B', hostname: 'node-b', ip: '10.0.0.2' }

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolver) => { resolve = resolver })
  return { promise, resolve }
}

function props(node: Node) {
  return {
    node,
    metrics: [],
    processSnapshot: { node_id: node.id, collected_at: 0, error: '', processes: [] },
    dockerSnapshot: { node_id: node.id, collected_at: 0, available: false, error: '', containers: [] },
    range: '1h' as const,
    onRangeChange: vi.fn(),
  }
}

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('NodeDetail node identity correctness', () => {
  test('clears file workspace on node switch and ignores late directory responses', async () => {
    const stale = deferred<FileListResponse>()
    const onLoadFiles = vi.fn((nodeID: string): Promise<FileListResponse> => nodeID === 'node-a'
      ? stale.promise
      : Promise.resolve({ path: '/', entries: [{ name: 'fresh.conf', path: '/fresh.conf', type: 'file', size: 1 }] }))
    const { rerender } = render(<NodeDetail {...props(nodeA)} onLoadFiles={onLoadFiles} />)

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    expect(await screen.findByText('正在加载目录...')).toBeInTheDocument()

    rerender(<NodeDetail {...props(nodeB)} onLoadFiles={onLoadFiles} />)
    expect(screen.queryByText('正在加载目录...')).not.toBeInTheDocument()
    expect(screen.queryByText('fresh.conf')).not.toBeInTheDocument()

    await act(async () => {
      stale.resolve({ path: '/', entries: [{ name: 'stale.conf', path: '/stale.conf', type: 'file', size: 1 }] })
    })
    expect(screen.queryByText('stale.conf')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    expect(await screen.findByText('fresh.conf')).toBeInTheDocument()
  })

  test('saves an opened file to the node identity captured when the editor opened', async () => {
    const onLoadFiles = vi.fn(async (): Promise<FileListResponse> => ({ path: '/', entries: [{ name: 'app.conf', path: '/app.conf', type: 'file', size: 3 }] }))
    const onReadFile = vi.fn(async (): Promise<FileReadResponse> => ({ path: '/app.conf', content: 'old', editable: true, size: 3 }))
    const write = deferred<{ path: string; saved: boolean }>()
    const onWriteFile = vi.fn(() => write.promise)
    const { rerender } = render(<NodeDetail {...props(nodeA)} onLoadFiles={onLoadFiles} onReadFile={onReadFile} onWriteFile={onWriteFile} />)

    fireEvent.click(screen.getByRole('button', { name: '文件管理' }))
    fireEvent.click(await screen.findByRole('button', { name: '编辑文件 app.conf' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑文件' })
    fireEvent.change(within(dialog).getByLabelText('文件内容'), { target: { value: 'new' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存文件' }))
    expect(onWriteFile).toHaveBeenCalledWith('node-a', '/app.conf', 'new')

    rerender(<NodeDetail {...props(nodeB)} onLoadFiles={onLoadFiles} onReadFile={onReadFile} onWriteFile={onWriteFile} />)
    expect(screen.queryByRole('dialog', { name: '编辑文件' })).not.toBeInTheDocument()
    await act(async () => write.resolve({ path: '/app.conf', saved: true }))
    expect(screen.queryByText('文件已保存。')).not.toBeInTheDocument()
  })

  test('cancels upgrade polling on node switch and never overlaps status requests', async () => {
    vi.useFakeTimers()
    const diagnostics = {
      node_id: 'node-a', online: true, health: 'healthy', agent_version: '0.1.0', protocol_version: 1,
      identity_conflict: false, upgrade_supported: true, latest_version: '0.2.0', upgrade_available: true, events: [],
    }
    const status = deferred<AgentUpgradeStatus>()
    const onGetAgentUpgradeStatus = vi.fn(() => status.promise)
    const onUpgradeAgent = vi.fn(async () => ({ accepted: true, stage: 'preparing' }))
    const { rerender } = render(
      <NodeDetail {...props(nodeA)} onGetConnectionDiagnostics={async () => diagnostics} onUpgradeAgent={onUpgradeAgent} onGetAgentUpgradeStatus={onGetAgentUpgradeStatus} />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    fireEvent.click(screen.getByRole('button', { name: '升级 Agent' }))
    fireEvent.click(within(screen.getByRole('dialog', { name: '升级 Agent' })).getByRole('button', { name: '确认升级' }))
    expect(onUpgradeAgent).toHaveBeenCalledTimes(1)

    await act(async () => { await Promise.resolve(); await vi.advanceTimersByTimeAsync(3000) })
    expect(onGetAgentUpgradeStatus).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(9000) })
    expect(onGetAgentUpgradeStatus).toHaveBeenCalledTimes(1)

    rerender(<NodeDetail {...props(nodeB)} onGetConnectionDiagnostics={async () => ({ ...diagnostics, node_id: 'node-b' })} onUpgradeAgent={onUpgradeAgent} onGetAgentUpgradeStatus={onGetAgentUpgradeStatus} />)
    await act(async () => {
      status.resolve({ node_id: 'node-a', target_version: '0.2.0', stage: 'waiting_reconnect' })
      await Promise.resolve()
      await vi.advanceTimersByTimeAsync(6000)
    })
    expect(onGetAgentUpgradeStatus).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('Agent升级成功')).not.toBeInTheDocument()
  })

  test('stops upgrade polling when the component unmounts', async () => {
    vi.useFakeTimers()
    const status = deferred<AgentUpgradeStatus>()
    const onGetAgentUpgradeStatus = vi.fn(() => status.promise)
    const { unmount } = render(
      <NodeDetail
        {...props(nodeA)}
        onGetConnectionDiagnostics={async () => ({ node_id: 'node-a', online: true, health: 'healthy', agent_version: '0.1.0', protocol_version: 1, identity_conflict: false, upgrade_supported: true, latest_version: '0.2.0', upgrade_available: true, events: [] })}
        onUpgradeAgent={async () => ({ accepted: true, stage: 'preparing' })}
        onGetAgentUpgradeStatus={onGetAgentUpgradeStatus}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    fireEvent.click(screen.getByRole('button', { name: '升级 Agent' }))
    fireEvent.click(within(screen.getByRole('dialog', { name: '升级 Agent' })).getByRole('button', { name: '确认升级' }))
    await act(async () => { await Promise.resolve(); await vi.advanceTimersByTimeAsync(3000) })
    expect(onGetAgentUpgradeStatus).toHaveBeenCalledTimes(1)

    unmount()
    await act(async () => {
      status.resolve({ node_id: 'node-a', target_version: '0.2.0', stage: 'waiting_reconnect' })
      await Promise.resolve()
      await vi.advanceTimersByTimeAsync(6000)
    })
    expect(onGetAgentUpgradeStatus).toHaveBeenCalledTimes(1)
  })

  test('stops upgrade polling after a terminal status', async () => {
    vi.useFakeTimers()
    const onGetAgentUpgradeStatus = vi.fn(async (): Promise<AgentUpgradeStatus> => ({ node_id: 'node-a', target_version: '0.2.0', actual_version: '0.2.0', stage: 'completed' }))
    render(
      <NodeDetail
        {...props(nodeA)}
        onGetConnectionDiagnostics={async () => ({ node_id: 'node-a', online: true, health: 'healthy', agent_version: '0.1.0', protocol_version: 1, identity_conflict: false, upgrade_supported: true, latest_version: '0.2.0', upgrade_available: true, events: [] })}
        onUpgradeAgent={async () => ({ accepted: true, stage: 'preparing' })}
        onGetAgentUpgradeStatus={onGetAgentUpgradeStatus}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Agent 管理' }))
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    fireEvent.click(screen.getByRole('button', { name: '升级 Agent' }))
    fireEvent.click(within(screen.getByRole('dialog', { name: '升级 Agent' })).getByRole('button', { name: '确认升级' }))
    await act(async () => { await Promise.resolve(); await vi.advanceTimersByTimeAsync(3000); await Promise.resolve() })
    expect(onGetAgentUpgradeStatus).toHaveBeenCalledTimes(1)
    expect(screen.getByText('Agent升级成功')).toBeInTheDocument()
    await act(async () => { await vi.advanceTimersByTimeAsync(12000) })
    expect(onGetAgentUpgradeStatus).toHaveBeenCalledTimes(1)
  })
})
