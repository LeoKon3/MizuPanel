import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import type { AIControlSettings, AIControlSettingsUpdate, Node } from '../../types'
import { AIControlPlaneSettings } from './AIControlPlaneSettings'

const policy: AIControlSettings = {
  mode: 'confirm_all',
  allowed_actions: [],
  node_scope: [],
  revision: 1,
  updated_at: null,
  scoped_node_count: 0,
  emergency_stopped: false
}

const nodes: Node[] = [
  {
    id: 'node-online',
    name: 'Production',
    hostname: 'production',
    ip: '10.0.0.1',
    os: 'linux',
    arch: 'amd64',
    kernel: '6.8',
    agent_version: '0.1.26',
    status: 'online',
    last_seen_at: '2026-08-17T00:00:00Z'
  },
  {
    id: 'node-offline',
    name: 'Staging',
    hostname: 'staging',
    ip: '10.0.0.2',
    os: 'linux',
    arch: 'amd64',
    kernel: '6.8',
    agent_version: '0.1.26',
    status: 'offline',
    last_seen_at: '2026-08-16T00:00:00Z'
  }
]

function savedPolicy(update: AIControlSettingsUpdate): AIControlSettings {
  return {
    ...policy,
    ...update,
    revision: policy.revision + 1,
    scoped_node_count: update.node_scope.length,
    emergency_stopped: update.mode === 'paused'
  }
}

describe('AI Control Plane settings', () => {
  test('edits the fixed recovery allowlist and scopes autonomy to online inventory nodes', async () => {
    const onSave = vi.fn(async (update: AIControlSettingsUpdate) => savedPolicy(update))
    render(<AIControlPlaneSettings policy={policy} nodes={nodes} onSave={onSave} />)

    expect(screen.getByText('全部确认', { selector: 'span' })).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Staging 离线' })).toBeDisabled()
    expect(screen.getAllByRole('checkbox')).toHaveLength(8)

    fireEvent.click(screen.getByRole('button', { name: '低风险自动' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Docker 容器 重启容器' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Compose 服务 启动服务' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Production 在线' }))
    fireEvent.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => expect(onSave).toHaveBeenCalledWith({
      mode: 'low_risk_auto',
      allowed_actions: ['docker.container.restart', 'compose.service.start'],
      node_scope: ['node-online']
    }))
    expect(await screen.findByRole('alert')).toHaveTextContent('AI 控制平面设置保存成功')
  })

  test('requires explicit confirmation to pause and resume', async () => {
    const onSave = vi.fn(async (update: AIControlSettingsUpdate) => savedPolicy(update))
    const view = render(<AIControlPlaneSettings policy={policy} nodes={nodes} onSave={onSave} />)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Docker 容器 启动容器' }))
    fireEvent.click(screen.getByRole('button', { name: '紧急暂停' }))
    const pauseDialog = screen.getByRole('dialog', { name: '暂停 AI 控制平面' })
    expect(onSave).not.toHaveBeenCalled()
    expect(within(pauseDialog).getByText(/已经受理的远端操作不会被撤销/)).toBeInTheDocument()
    fireEvent.click(within(pauseDialog).getByRole('button', { name: '确认暂停' }))

    await waitFor(() => expect(onSave).toHaveBeenNthCalledWith(1, {
      mode: 'paused',
      allowed_actions: [],
      node_scope: []
    }))

    const pausedPolicy = savedPolicy({
      mode: 'paused',
      allowed_actions: ['docker.container.restart'],
      node_scope: ['node-online']
    })
    view.rerender(<AIControlPlaneSettings policy={pausedPolicy} nodes={nodes} onSave={onSave} />)
    expect(await screen.findByText(/新的 AI 变更步骤已停止/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '低风险自动' }))
    fireEvent.click(screen.getByRole('button', { name: '恢复 AI 控制平面' }))
    const resumeDialog = screen.getByRole('dialog', { name: '恢复 AI 控制平面' })
    expect(within(resumeDialog).getByText(/已取消或跳过的步骤不会恢复/)).toBeInTheDocument()
    fireEvent.click(within(resumeDialog).getByRole('button', { name: '确认恢复' }))

    await waitFor(() => expect(onSave).toHaveBeenNthCalledWith(2, {
      mode: 'low_risk_auto',
      allowed_actions: ['docker.container.restart'],
      node_scope: ['node-online']
    }))
  })

  test('reports policy update failures through Toast feedback', async () => {
    const onSave = vi.fn(async () => {
      throw new Error('策略版本冲突')
    })
    render(<AIControlPlaneSettings policy={policy} nodes={nodes} onSave={onSave} />)

    fireEvent.click(screen.getByRole('button', { name: '保存策略' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('AI 控制平面设置保存失败: 策略版本冲突')
  })
})
