import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { getAlertHistory, getAlertRules, getK8sClusters, getNodeMetrics } from '../api/client'
import type { AlertHistory, K8sCluster, Node } from '../types'
import { OverviewPage } from './OverviewPage'

vi.mock('../api/client', () => ({
  getAlertHistory: vi.fn(),
  getAlertRules: vi.fn(),
  getK8sClusters: vi.fn(),
  getNodeMetrics: vi.fn(),
}))

const nodes: Node[] = [
  {
    id: 'node-a',
    name: 'Server A',
    hostname: 'host-a',
    ip: '10.0.0.1',
    os: 'linux',
    arch: 'amd64',
    kernel: '6.8.0',
    agent_version: '0.1.16',
    status: 'online',
    last_seen_at: '2026-08-05T00:00:00Z',
  },
  {
    id: 'node-b',
    name: 'Server B',
    hostname: 'host-b',
    ip: '10.0.0.2',
    os: 'debian',
    arch: 'arm64',
    kernel: '6.1.0',
    agent_version: '0.1.16',
    status: 'online',
    last_seen_at: '2026-08-05T00:00:00Z',
  },
]

const clusters: K8sCluster[] = [
  {
    id: 'cluster-offline-agent',
    name: 'offline-agent',
    node_id: 'node-a',
    node_name: 'Server A',
    node_ip: '10.0.0.1',
    node_status: 'offline',
    status: 'online',
    created_at: '2026-08-05T00:00:00Z',
    updated_at: '2026-08-05T00:00:00Z',
  },
  {
    id: 'cluster-online',
    name: 'online-cluster',
    node_id: 'node-b',
    node_name: 'Server B',
    node_ip: '10.0.0.2',
    node_status: 'online',
    status: 'online',
    created_at: '2026-08-05T00:00:00Z',
    updated_at: '2026-08-05T00:00:00Z',
  },
]

describe('OverviewPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getAlertHistory).mockResolvedValue({ history: [] })
    vi.mocked(getAlertRules).mockResolvedValue({ rules: [] })
    vi.mocked(getK8sClusters).mockResolvedValue({ clusters })
    vi.mocked(getNodeMetrics).mockResolvedValue({ metrics: [] })
  })

  test('counts a Kubernetes cluster online only when its Agent and API are online', async () => {
    render(<OverviewPage nodes={nodes} onlineNodes={2} onAddServer={vi.fn()} />)

    await waitFor(() => expect(screen.getByText('1/2')).toBeInTheDocument())
    expect(screen.getByText('1 在线')).toBeInTheDocument()
  })

  test('updates system information when a server status card is selected', async () => {
    const onSelectedNodeChange = vi.fn()
    render(<OverviewPage nodes={nodes} onlineNodes={2} onAddServer={vi.fn()} onSelectedNodeChange={onSelectedNodeChange} />)

    await waitFor(() => expect(screen.getByText('host-a')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /Server B/ }))

    expect(screen.getByText('host-b')).toBeInTheDocument()
    expect(screen.getByText('debian arm64')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Server B/ })).toHaveAttribute('aria-pressed', 'true')
    expect(onSelectedNodeChange).toHaveBeenLastCalledWith('node-b')
  })

  test('ignores a null alert history response while rendering valid alerts', async () => {
    const activeAlert: AlertHistory = {
      id: 1,
      rule_id: 1,
      rule_name: 'CPU 使用率过高',
      node_id: 'node-b',
      node_name: 'Server B',
      metric_field: 'cpu_usage',
      metric_value: 95,
      threshold: 80,
      triggered_at: '2026-08-05T00:00:00Z',
      notification_sent: false,
      created_at: '2026-08-05T00:00:00Z',
    }
    vi.mocked(getAlertHistory)
      .mockResolvedValueOnce({ history: null as unknown as AlertHistory[] })
      .mockResolvedValueOnce({ history: [activeAlert] })

    render(<OverviewPage nodes={nodes} onlineNodes={2} onAddServer={vi.fn()} />)

    await waitFor(() => expect(screen.getByText('CPU 使用率过高')).toBeInTheDocument())
  })
})
