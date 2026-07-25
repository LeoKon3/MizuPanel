import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { NodeDetail } from './NodeDetail'

describe('NodeDetail Docker resource workspace', () => {
  test('loads resources lazily from the merged Docker view', async () => {
    const onRefresh = vi.fn(async () => undefined)
    render(
      <NodeDetail
        node={{ id: 'node-1', name: 'docker-host', hostname: 'docker-host', ip: '10.0.0.2', os: 'linux', arch: 'amd64', kernel: '6.8', agent_version: '0.1.2', status: 'online', last_seen_at: '2026-07-24T00:00:00Z' }}
        metrics={[]}
        processSnapshot={{ node_id: 'node-1', collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: 'node-1', collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerResources={{ node_id: 'node-1', success: true, supported: true, usage: {}, images: [], volumes: [], networks: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onRefreshDockerResources={onRefresh}
        onDockerResourceAction={vi.fn(async () => ({ success: true }))}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    expect(onRefresh).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('tab', { name: '资源' }))
    await waitFor(() => expect(onRefresh).toHaveBeenCalledWith('node-1'))
    expect(screen.getByText('Docker 资源')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '镜像 0' })).toBeInTheDocument()
  })

  test('does not load resources for a newly selected node until resources is selected again', async () => {
    const onRefresh = vi.fn(async () => undefined)
    const baseProps = {
      metrics: [],
      processSnapshot: { node_id: 'node-1', collected_at: 0, error: '', processes: [] },
      dockerSnapshot: { node_id: 'node-1', collected_at: 0, available: true, version: '28.0', error: '', containers: [] },
      dockerResources: { node_id: 'node-1', success: true, supported: true, usage: {}, images: [], volumes: [], networks: [] },
      range: '1h' as const,
      onRangeChange: vi.fn(),
      onRefreshDockerResources: onRefresh,
      onDockerResourceAction: vi.fn(async () => ({ success: true })),
    }
    const firstNode = { id: 'node-1', name: 'docker-host-1', hostname: 'docker-host-1', ip: '10.0.0.2', os: 'linux', arch: 'amd64', kernel: '6.8', agent_version: '0.1.2', status: 'online' as const, last_seen_at: '2026-07-24T00:00:00Z' }
    const secondNode = { ...firstNode, id: 'node-2', name: 'docker-host-2', hostname: 'docker-host-2', ip: '10.0.0.3' }
    const { rerender } = render(<NodeDetail {...baseProps} node={firstNode} />)

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: '资源' }))
    await waitFor(() => expect(onRefresh).toHaveBeenCalledTimes(1))

    rerender(<NodeDetail {...baseProps} node={secondNode} />)
    await waitFor(() => expect(screen.getByRole('tab', { name: '资源' })).toHaveAttribute('aria-selected', 'false'))
    expect(onRefresh).toHaveBeenCalledTimes(1)
  })
})
