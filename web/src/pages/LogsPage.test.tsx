import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { LogsPage } from './LogsPage'

const { systemLogsMock } = vi.hoisted(() => ({
  systemLogsMock: vi.fn(),
}))

vi.mock('../api/client', () => ({
  getAgentLogs: vi.fn(),
  getNodeDocker: vi.fn(),
  getNodeSystemdServices: vi.fn(),
  getSystemLogs: systemLogsMock,
  runNodeSystemdServiceAction: vi.fn(),
}))

vi.mock('../api/k8s', () => ({
  fetchK8sClusters: vi.fn(),
  fetchK8sNamespaces: vi.fn(),
  fetchK8sPodLogs: vi.fn(),
  fetchK8sPods: vi.fn(),
}))

const node = {
  id: 'node-1',
  name: 'Oracle',
  hostname: 'oracle',
  ip: '10.0.0.1',
  os: 'linux',
  arch: 'amd64',
  kernel: '6.6',
  agent_version: '0.1.12',
  status: 'online',
  last_seen_at: '2026-07-29T10:00:00Z',
} as const

beforeEach(() => {
  systemLogsMock.mockReset()
})

describe('LogsPage', () => {
  test('queries and filters the bounded Server log snapshot', async () => {
    systemLogsMock.mockResolvedValue({
      content: 'server started\ndatabase ready\n',
      lines: 200,
      returned_lines: 2,
      collected_at: '2026-07-29T10:00:00Z',
      started_at: '2026-07-29T09:00:00Z',
      truncated: false,
    })

    render(<LogsPage nodes={[node]} />)
    fireEvent.click(screen.getByRole('button', { name: /Server 自身/ }))
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))

    expect(await screen.findByText('server started')).toBeInTheDocument()
    expect(systemLogsMock).toHaveBeenCalledWith(200, expect.any(AbortSignal))

    fireEvent.change(screen.getByRole('textbox', { name: '搜索当前日志' }), { target: { value: 'database' } })
    expect(screen.queryByText('server started')).not.toBeInTheDocument()
    expect(screen.getAllByText((_content, element) => element?.textContent === 'database ready')).not.toHaveLength(0)
  })

  test('ignores a late snapshot after changing sources', async () => {
    let resolve!: (value: { content: string; lines: number; returned_lines: number; collected_at: string; started_at: string; truncated: boolean }) => void
    systemLogsMock.mockImplementation(() => new Promise((done) => { resolve = done }))

    render(<LogsPage nodes={[node]} />)
    fireEvent.click(screen.getByRole('button', { name: /Server 自身/ }))
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    fireEvent.click(screen.getByRole('button', { name: /Agent 自身/ }))
    resolve({ content: 'must not render', lines: 200, returned_lines: 1, collected_at: '2026-07-29T10:00:00Z', started_at: '2026-07-29T09:00:00Z', truncated: false })

    await waitFor(() => expect(screen.queryByText('must not render')).not.toBeInTheDocument())
  })
})
