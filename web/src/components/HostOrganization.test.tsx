import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { Node } from '../types'
import { HostBatchTable } from './HostBatchTable'
import { BatchAgentUpgradeModal } from './BatchAgentUpgradeModal'
import { NodeOrganizationControls } from './NodeOrganizationControls'
import { NodeList } from '../pages/NodeList'

const api = vi.hoisted(() => ({
  getNodeGroups: vi.fn(),
  getNodeTags: vi.fn(),
  createNodeGroup: vi.fn(),
  createNodeTag: vi.fn(),
  updateNodeGroup: vi.fn(),
  updateNodeTag: vi.fn(),
  deleteNodeGroup: vi.fn(),
  deleteNodeTag: vi.fn(),
  updateBatchNodeMetadata: vi.fn(),
  getConnectionDiagnostics: vi.fn(),
  upgradeAgent: vi.fn(),
  getAgentUpgradeStatus: vi.fn()
}))

vi.mock('../api/client', () => api)

const nodes: Node[] = [
  {
    id: 'node-a', name: 'Alpha', hostname: 'alpha', ip: '10.0.0.1', os: 'linux', arch: 'amd64', kernel: '6.6', agent_version: '0.1.0', status: 'online', last_seen_at: '',
    group: { id: 'group-production', name: '生产环境' },
    tags: [{ id: 'tag-db', name: '数据库', color: 'blue' }],
    latest_metric: { id: 1, node_id: 'node-a', cpu_usage: 10, cpu_cores: 4, memory_total: 100, memory_used: 20, memory_usage: 20, disk_total: 100, disk_used: 30, disk_usage: 30, rx_speed: 0, tx_speed: 0, rx_total: 0, tx_total: 0, load1: 0, load5: 0, load15: 0, created_at: '' }
  },
  { id: 'node-b', name: 'Beta', hostname: 'beta', ip: '10.0.0.2', os: 'linux', arch: 'amd64', kernel: '6.6', agent_version: '0.1.1', status: 'offline', last_seen_at: '', group: null, tags: [] }
]

beforeEach(() => {
  api.getNodeGroups.mockReset().mockResolvedValue({ groups: [{ id: 'group-production', name: '生产环境', node_count: 1, created_at: '', updated_at: '' }] })
  api.getNodeTags.mockReset().mockResolvedValue({ tags: [{ id: 'tag-db', name: '数据库', color: 'blue', node_count: 1, created_at: '', updated_at: '' }] })
  api.createNodeGroup.mockReset().mockResolvedValue({ id: 'group-test', name: '测试环境', node_count: 0, created_at: '', updated_at: '' })
  api.createNodeTag.mockReset().mockResolvedValue({ id: 'tag-new', name: '重要', color: 'red', node_count: 0, created_at: '', updated_at: '' })
  api.updateNodeGroup.mockReset()
  api.updateNodeTag.mockReset()
  api.deleteNodeGroup.mockReset()
  api.deleteNodeTag.mockReset()
  api.updateBatchNodeMetadata.mockReset().mockResolvedValue({ nodes: {} })
  api.getConnectionDiagnostics.mockReset().mockImplementation(async (nodeID: string) => ({ node_id: nodeID, online: true, health: 'healthy', agent_version: '0.1.0', protocol_version: 1, identity_conflict: false, upgrade_supported: true, latest_version: '0.1.1', upgrade_available: true, events: [] }))
  api.upgradeAgent.mockReset().mockResolvedValue({ accepted: true, stage: 'preparing' })
  api.getAgentUpgradeStatus.mockReset().mockImplementation(async (nodeID: string) => ({ node_id: nodeID, target_version: '0.1.1', actual_version: '0.1.1', stage: 'completed' }))
})

describe('host organization UI', () => {
  test('groups browse nodes and collapses a group without selecting a node', () => {
    const onSelectNode = vi.fn()
    render(<NodeList nodes={nodes} selectedNodeID="node-a" onSelectNode={onSelectNode} />)

    expect(screen.getByRole('region', { name: '生产环境' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '未分组' })).toBeInTheDocument()
    expect(screen.getByText('数据库')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /生产环境/ }))
    expect(screen.queryByRole('button', { name: /Alpha/ })).not.toBeInTheDocument()
    expect(onSelectNode).not.toHaveBeenCalled()
  })

  test('creates groups from the catalog modal and reports through the shared workflow', async () => {
    const onChanged = vi.fn()
    render(<NodeOrganizationControls view="browse" onViewChange={vi.fn()} groupFilter="all" onGroupFilterChange={vi.fn()} tagFilter="all" onTagFilterChange={vi.fn()} groups={[{ id: 'group-production', name: '生产环境' }]} tags={[]} onChanged={onChanged} />)

    fireEvent.click(screen.getByRole('button', { name: '管理分组与标签' }))
    const dialog = await screen.findByRole('dialog', { name: '管理主机分组与标签' })
    fireEvent.change(within(dialog).getByLabelText('新分组名称'), { target: { value: '测试环境' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.createNodeGroup).toHaveBeenCalledWith('测试环境'))
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    expect(await screen.findByText('主机分组创建成功')).toBeInTheDocument()
  })

  test('selects hosts and applies a transactional batch group move', async () => {
    const onNodesChanged = vi.fn()
    render(<HostBatchTable nodes={nodes} onOpenNode={vi.fn()} onNodesChanged={onNodesChanged} />)

    fireEvent.click(screen.getByRole('checkbox', { name: '选择 Alpha' }))
    fireEvent.click(screen.getByRole('button', { name: '移动分组' }))
    const dialog = await screen.findByRole('dialog', { name: '批量移动分组' })
    fireEvent.change(within(dialog).getByLabelText('目标分组'), { target: { value: 'group-production' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认应用' }))

    await waitFor(() => expect(api.updateBatchNodeMetadata).toHaveBeenCalledWith({ node_ids: ['node-a'], group_id: 'group-production' }))
    await waitFor(() => expect(onNodesChanged).toHaveBeenCalled())
    expect(await screen.findByText('主机分组调整成功，共 1 台')).toBeInTheDocument()
  })

  test('preflights selected nodes and upgrades only eligible Agents', async () => {
    api.getConnectionDiagnostics.mockImplementation(async (nodeID: string) => nodeID === 'node-a'
      ? { node_id: nodeID, online: true, health: 'healthy', agent_version: '0.1.0', protocol_version: 1, identity_conflict: false, upgrade_supported: true, latest_version: '0.1.1', upgrade_available: true, events: [] }
      : { node_id: nodeID, online: false, health: 'offline', agent_version: '0.1.1', protocol_version: 1, identity_conflict: false, upgrade_supported: true, latest_version: '0.1.1', upgrade_available: false, events: [] })
    const onFinished = vi.fn()
    render(<BatchAgentUpgradeModal nodes={nodes} onClose={vi.fn()} onFinished={onFinished} />)

    expect(await screen.findByText('可升级到 0.1.1')).toBeInTheDocument()
    expect(await screen.findByText('节点离线，已跳过')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认升级' }))

    await waitFor(() => expect(api.upgradeAgent).toHaveBeenCalledTimes(1))
    expect(api.upgradeAgent).toHaveBeenCalledWith('node-a')
    await waitFor(() => expect(onFinished).toHaveBeenCalledWith({ succeeded: 1, failed: 0, skipped: 1 }))
    expect(await screen.findByText('已升级到 0.1.1')).toBeInTheDocument()
  })

  test('closing an accepted batch upgrade stops polling without resubmitting it on reopen', async () => {
    let resolveUpgradeStatus: ((status: { node_id: string, target_version: string, stage: 'waiting_reconnect' }) => void) | undefined
    api.getAgentUpgradeStatus.mockImplementation(() => new Promise((resolve) => {
      resolveUpgradeStatus = resolve
    }))
    const onNodesChanged = vi.fn()
    render(<HostBatchTable nodes={[nodes[0]]} onOpenNode={vi.fn()} onNodesChanged={onNodesChanged} />)

    fireEvent.click(screen.getByRole('checkbox', { name: '选择 Alpha' }))
    fireEvent.click(screen.getByRole('button', { name: '升级 Agent' }))
    expect(await screen.findByText('可升级到 0.1.1')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认升级' }))

    await waitFor(() => expect(api.upgradeAgent).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(api.getAgentUpgradeStatus).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '关闭并停止跟踪' }))
    expect(screen.queryByRole('dialog', { name: '批量升级 Agent' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '升级 Agent' }))
    expect(await screen.findByText('可升级到 0.1.1')).toBeInTheDocument()
    expect(api.upgradeAgent).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    vi.useFakeTimers()
    try {
      await act(async () => {
        resolveUpgradeStatus?.({ node_id: 'node-a', target_version: '0.1.1', stage: 'waiting_reconnect' })
        await Promise.resolve()
      })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3000)
      })
      expect(api.getAgentUpgradeStatus).toHaveBeenCalledTimes(1)
      expect(api.upgradeAgent).toHaveBeenCalledTimes(1)
      expect(onNodesChanged).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })
})
