import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { NodeDetail } from './NodeDetail'

const node = {
  id: 'node-1',
  name: 'compose-host',
  hostname: 'compose-host',
  ip: '10.0.0.2',
  os: 'linux',
  arch: 'amd64',
  kernel: '6.8',
  agent_version: '0.1.2',
  status: 'online' as const,
  last_seen_at: '2026-07-19T00:00:00Z',
}

describe('NodeDetail Docker Compose management', () => {
  test('shows projects and uses a custom confirmation before down', async () => {
    const onAction = vi.fn(async () => ({ success: true }))
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{
          success: true,
          supported: true,
          projects: [{
            name: 'demo',
            status: 'running(1)',
            config_files: ['/srv/demo/compose.yml'],
            services: [{ name: 'web', container_name: 'demo-web-1', container_id: 'container-1', image: 'nginx:alpine', state: 'running', ports: ['8080:80/tcp'] }],
          }],
        }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeAction={onAction}
        onRefreshDockerCompose={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))

    expect(screen.getByText('demo')).toBeInTheDocument()
    expect(screen.getByText('demo-web-1')).toBeInTheDocument()
    expect(screen.getByText('8080:80/tcp')).toBeInTheDocument()

    const toolbar = screen.getByRole('toolbar', { name: 'demo Compose 操作' })
    fireEvent.click(within(toolbar).getByRole('button', { name: '重启' }))
    await waitFor(() => expect(onAction).toHaveBeenCalledWith('node-1', 'demo', 'restart'))

    fireEvent.click(within(toolbar).getByRole('button', { name: '移除' }))
    const dialog = screen.getByRole('dialog', { name: '移除 Compose 项目' })
    expect(onAction).not.toHaveBeenCalledWith('node-1', 'demo', 'down')
    fireEvent.click(within(dialog).getByRole('button', { name: '确认移除' }))
    await waitFor(() => expect(onAction).toHaveBeenCalledWith('node-1', 'demo', 'down'))
  })

  test('shows project logs, validates config and exposes supported service operations', async () => {
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    const onAction = vi.fn(async (_nodeID: string, _projectName: string, action: string) => (
      action === 'logs' ? { success: true, output: 'web | server ready' } : { success: true }
    ))
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{
          success: true,
          supported: true,
          service_actions_supported: true,
          projects: [{
            name: 'demo',
            config_files: ['/srv/demo/compose.yml'],
            services: [{ name: 'web', container_name: 'demo-web-1', container_id: 'container-1', image: 'nginx:alpine', state: 'running' }],
          }],
        }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeAction={onAction}
        onRefreshDockerCompose={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))
    const toolbar = screen.getByRole('toolbar', { name: 'demo Compose 操作' })

    fireEvent.click(within(toolbar).getByRole('button', { name: '日志' }))
    const logsDialog = await screen.findByRole('dialog', { name: 'Compose 项目日志 demo' })
    expect(within(logsDialog).getByText('web | server ready')).toBeInTheDocument()
    fireEvent.click(within(logsDialog).getByRole('button', { name: '关闭 Compose 日志' }))

    await waitFor(() => expect(within(toolbar).getByRole('button', { name: '校验配置' })).toBeEnabled())
    fireEvent.click(within(toolbar).getByRole('button', { name: '校验配置' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Compose 配置校验成功')

    const serviceActions = screen.getByRole('button', { name: 'web 服务操作' })
    fireEvent.click(serviceActions)
    const serviceMenu = screen.getByRole('menu', { name: 'web 服务操作菜单' })
    expect(within(serviceMenu).getByRole('button', { name: '查看日志' })).toBeEnabled()
    expect(within(serviceMenu).getByRole('button', { name: '拉取镜像' })).toBeEnabled()
    fireEvent.click(within(serviceMenu).getByRole('button', { name: '进入终端' }))
    expect(openSpy).toHaveBeenCalledWith('/nodes/node-1/containers/container-1/exec', '_blank', 'noopener,noreferrer')

    fireEvent.click(screen.getByRole('button', { name: 'web 服务操作' }))
    fireEvent.click(within(screen.getByRole('menu', { name: 'web 服务操作菜单' })).getByRole('button', { name: '重启' }))
    await waitFor(() => expect(onAction).toHaveBeenCalledWith('node-1', 'demo', 'restart', 'web'))
    openSpy.mockRestore()
  })

  test('hides service lifecycle actions until the Agent confirms support', () => {
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{
          success: true,
          supported: true,
          service_actions_supported: false,
          projects: [{
            name: 'demo',
            config_files: ['/srv/demo/compose.yml'],
            services: [{ name: 'web', container_name: 'demo-web-1', container_id: 'container-1', image: 'nginx:alpine', state: 'running' }],
          }],
        }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeAction={vi.fn(async () => ({ success: true }))}
        onRefreshDockerCompose={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))
    fireEvent.click(screen.getByRole('button', { name: 'web 服务操作' }))

    const serviceMenu = screen.getByRole('menu', { name: 'web 服务操作菜单' })
    expect(within(serviceMenu).getByRole('button', { name: '查看日志' })).toBeInTheDocument()
    expect(within(serviceMenu).queryByRole('button', { name: '拉取镜像' })).not.toBeInTheDocument()
    expect(within(serviceMenu).queryByRole('button', { name: '启动 / 重建' })).not.toBeInTheDocument()
    expect(within(serviceMenu).queryByRole('button', { name: '重启' })).not.toBeInTheDocument()
    expect(within(serviceMenu).queryByRole('button', { name: '停止' })).not.toBeInTheDocument()
  })
})
