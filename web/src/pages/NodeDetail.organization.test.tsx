import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { Node } from '../types'
import { NodeDetail } from './NodeDetail'

const api = vi.hoisted(() => ({
  getNodeGroups: vi.fn(),
  getNodeTags: vi.fn(),
  createNodeTag: vi.fn(),
  updateBatchNodeMetadata: vi.fn(),
}))

vi.mock('../api/client', () => api)

const node: Node = {
  id: 'node-1',
  name: 'Alpha',
  hostname: 'alpha',
  ip: '10.0.0.1',
  os: 'linux',
  arch: 'amd64',
  kernel: '6.6',
  agent_version: '0.1.1',
  status: 'offline',
  last_seen_at: '',
  terminal_enabled: false,
  group: { id: 'group-old', name: '旧分组' },
  tags: [{ id: 'tag-db', name: '数据库', color: 'blue' }],
}

beforeEach(() => {
  api.getNodeGroups.mockReset().mockResolvedValue({ groups: [{ id: 'group-production', name: '生产环境', node_count: 3, created_at: '', updated_at: '' }] })
  api.getNodeTags.mockReset().mockResolvedValue({ tags: [{ id: 'tag-db', name: '数据库', color: 'blue', node_count: 1, created_at: '', updated_at: '' }] })
  api.createNodeTag.mockReset()
  api.updateBatchNodeMetadata.mockReset().mockResolvedValue({ nodes: {} })
})

describe('NodeDetail organization editing', () => {
  test('opens the organization editor from the header for an offline node and refreshes after save', async () => {
    const onNodeOrganizationChanged = vi.fn(async () => undefined)
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: false, error: '', containers: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onNodeOrganizationChanged={onNodeOrganizationChanged}
      />,
    )

    const actions = screen.getByRole('toolbar', { name: '节点操作' })
    const organizationButton = within(actions).getByRole('button', { name: '分组与标签' })
    expect(organizationButton).toBeEnabled()
    fireEvent.click(organizationButton)

    const dialog = await screen.findByRole('dialog', { name: '调整主机分组与标签' })
    fireEvent.change(within(dialog).getByLabelText('主分组'), { target: { value: 'group-production' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存更改' }))

    await waitFor(() => expect(api.updateBatchNodeMetadata).toHaveBeenCalledWith({ node_ids: ['node-1'], group_id: 'group-production' }))
    await waitFor(() => expect(onNodeOrganizationChanged).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('dialog', { name: '调整主机分组与标签' })).not.toBeInTheDocument()
    expect(await screen.findByText('主机组织信息调整成功')).toBeInTheDocument()
  })
})
