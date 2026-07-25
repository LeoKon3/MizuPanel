import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { NodeDetail } from './NodeDetail'

const node = {
  id: 'node-1',
  name: 'systemd-host',
  hostname: 'systemd-host',
  ip: '10.0.0.2',
  os: 'linux',
  arch: 'amd64',
  kernel: '6.8',
  agent_version: '0.1.2',
  status: 'online' as const,
  last_seen_at: '2026-07-20T00:00:00Z',
}

function renderSystemdDetail(onAction = vi.fn(async (_nodeID: string, _serviceName: string, action: string) => action === 'logs' ? { success: true, output: 'nginx started' } : { success: true })) {
  render(
    <NodeDetail
      node={node}
      metrics={[]}
      processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
      dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
      systemdServices={{
        success: true,
        supported: true,
        services: [
          { name: 'nginx.service', description: 'A high performance web server', load_state: 'loaded', active_state: 'active', sub_state: 'running', unit_file_state: 'enabled' },
          { name: 'worker.service', description: 'Background worker', load_state: 'loaded', active_state: 'inactive', sub_state: 'dead', unit_file_state: 'disabled' },
        ],
      }}
      range="1h"
      onRangeChange={vi.fn()}
      onRefreshSystemdServices={vi.fn(async () => undefined)}
      onSystemdServiceAction={onAction}
    />,
  )
  return onAction
}

describe('NodeDetail systemd service management', () => {
  test('lists services and opens logs or lifecycle actions through a service menu', async () => {
    const onAction = renderSystemdDetail()

    fireEvent.click(screen.getByRole('button', { name: '系统服务' }))
    expect(screen.getByRole('region', { name: '系统服务' })).toHaveTextContent('nginx.service')
    expect(screen.getByText('A high performance web server')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'nginx.service 服务操作' }))
    const menu = screen.getByRole('menu', { name: 'nginx.service 服务操作菜单' })
    expect(within(menu).getByRole('button', { name: '启动' })).toBeDisabled()
    expect(within(menu).getByRole('button', { name: '重启' })).toBeEnabled()
    fireEvent.click(within(menu).getByRole('button', { name: '查看日志' }))

    const logsDialog = await screen.findByRole('dialog', { name: '系统服务日志 nginx.service' })
    expect(within(logsDialog).getByText('nginx started')).toBeInTheDocument()
    fireEvent.click(within(logsDialog).getByRole('button', { name: '关闭系统服务日志' }))

    fireEvent.click(screen.getByRole('button', { name: 'nginx.service 服务操作' }))
    fireEvent.click(within(screen.getByRole('menu', { name: 'nginx.service 服务操作菜单' })).getByRole('button', { name: '重启' }))
    await waitFor(() => expect(onAction).toHaveBeenCalledWith('node-1', 'nginx.service', 'restart'))
  })

  test('shows the Agent capability state when systemd is unavailable', () => {
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        systemdServices={{ success: false, supported: false, services: [], error: '当前 Agent 不支持 systemd 服务管理' }}
        range="1h"
        onRangeChange={vi.fn()}
        onRefreshSystemdServices={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '系统服务' }))
    expect(screen.getByText('当前 Agent 不支持 systemd 服务管理')).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '搜索系统服务' })).not.toBeInTheDocument()
  })
})
