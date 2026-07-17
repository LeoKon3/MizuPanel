import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { Node } from '../types'
import { SingleNodeOrganizationModal } from './SingleNodeOrganizationModal'

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
  group: { id: 'group-old', name: '旧分组' },
  tags: [{ id: 'tag-db', name: '数据库', color: 'blue' }],
}

beforeEach(() => {
  api.getNodeGroups.mockReset().mockResolvedValue({
    groups: [
      { id: 'group-old', name: '旧分组', node_count: 1, created_at: '', updated_at: '' },
      { id: 'group-production', name: '生产环境', node_count: 3, created_at: '', updated_at: '' },
    ],
  })
  api.getNodeTags.mockReset().mockResolvedValue({
    tags: [
      { id: 'tag-db', name: '数据库', color: 'blue', node_count: 1, created_at: '', updated_at: '' },
      { id: 'tag-cache', name: '缓存', color: 'teal', node_count: 2, created_at: '', updated_at: '' },
    ],
  })
  api.createNodeTag.mockReset().mockResolvedValue({ id: 'tag-important', name: '重要', color: 'red', node_count: 0, created_at: '', updated_at: '' })
  api.updateBatchNodeMetadata.mockReset().mockResolvedValue({ nodes: {} })
})

describe('SingleNodeOrganizationModal', () => {
  test('submits one atomic single-node diff and refreshes only after success', async () => {
    const onClose = vi.fn()
    const onSaved = vi.fn(async () => undefined)
    render(<SingleNodeOrganizationModal node={node} onClose={onClose} onSaved={onSaved} />)

    const dialog = await screen.findByRole('dialog', { name: '调整主机分组与标签' })
    fireEvent.change(within(dialog).getByLabelText('主分组'), { target: { value: 'group-production' } })
    fireEvent.click(within(dialog).getByRole('checkbox', { name: '数据库' }))
    fireEvent.click(within(dialog).getByRole('checkbox', { name: '缓存' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '保存更改' }))

    await waitFor(() => expect(api.updateBatchNodeMetadata).toHaveBeenCalledWith({
      node_ids: ['node-1'],
      group_id: 'group-production',
      add_tag_ids: ['tag-cache'],
      remove_tag_ids: ['tag-db'],
    }))
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  test('keeps its target state and reports an error when save fails', async () => {
    api.updateBatchNodeMetadata.mockRejectedValueOnce(new Error('节点已删除'))
    render(<SingleNodeOrganizationModal node={node} onClose={vi.fn()} onSaved={vi.fn()} />)

    const dialog = await screen.findByRole('dialog', { name: '调整主机分组与标签' })
    fireEvent.change(within(dialog).getByLabelText('主分组'), { target: { value: 'group-production' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存更改' }))

    expect(await screen.findByText('主机组织信息调整失败: 节点已删除')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('主分组')).toHaveValue('group-production')
    expect(within(dialog).getByRole('button', { name: '保存更改' })).toBeEnabled()
  })

  test('creates a tag into the current selection and enforces the 20-tag limit', async () => {
    const twentyTags = Array.from({ length: 20 }, (_, index) => ({
      id: `tag-${index}`,
      name: `标签 ${index + 1}`,
      color: 'gray' as const,
      node_count: 0,
      created_at: '',
      updated_at: '',
    }))
    api.getNodeTags.mockResolvedValueOnce({ tags: [...twentyTags, { id: 'tag-extra', name: '额外标签', color: 'red', node_count: 0, created_at: '', updated_at: '' }] })
    const cappedNode: Node = { ...node, tags: twentyTags.map(({ id, name, color }) => ({ id, name, color })) }
    render(<SingleNodeOrganizationModal node={cappedNode} onClose={vi.fn()} onSaved={vi.fn()} />)

    const dialog = await screen.findByRole('dialog', { name: '调整主机分组与标签' })
    expect(within(dialog).getByText('标签（20 / 20）')).toBeInTheDocument()
    expect(within(dialog).getByRole('checkbox', { name: '额外标签' })).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('checkbox', { name: '标签 1' }))
    expect(within(dialog).getByRole('checkbox', { name: '额外标签' })).toBeEnabled()

    fireEvent.change(within(dialog).getByLabelText('创建或选择标签'), { target: { value: '重要' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '加入标签' }))
    await waitFor(() => expect(api.createNodeTag).toHaveBeenCalledWith('重要', 'teal'))
    expect(await within(dialog).findByRole('checkbox', { name: '重要' })).toBeChecked()
  })

  test('closes with Escape while no metadata request is running', async () => {
    const onClose = vi.fn()
    render(<SingleNodeOrganizationModal node={node} onClose={onClose} onSaved={vi.fn()} />)

    await screen.findByRole('dialog', { name: '调整主机分组与标签' })
    fireEvent.keyDown(document, { key: 'Escape' })

    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
