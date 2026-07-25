import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import type { DockerResourceListResponse } from '../types'
import { DockerResourcesPanel } from './DockerResourcesPanel'

const response: DockerResourceListResponse = {
  success: true,
  supported: true,
  usage: { image_layers: 1024, container_writable: 2048, volumes: 4096, build_cache: 0 },
  images: [
    { id: 'image-free', full_id: 'sha256:image-free', tags: ['nginx:latest'], size: 1024, shared_size: 256, created_at: 1710000000, containers: 0 },
    { id: 'image-used', full_id: 'sha256:image-used', tags: ['redis:7'], size: 2048, containers: 1 },
  ],
  volumes: [{ name: 'demo_data', driver: 'local', scope: 'local', mountpoint: '/var/lib/docker/volumes/demo_data/_data', compose_project: 'demo', size: 4096, ref_count: 0 }],
  networks: [
    { id: 'bridge-id', full_id: 'bridge-full-id', name: 'bridge', driver: 'bridge', scope: 'local', subnets: ['172.17.0.0/16'], containers: [], protected: true },
    { id: 'demo-id', full_id: 'demo-full-id', name: 'demo_default', driver: 'bridge', scope: 'local', subnets: ['172.20.0.0/16'], containers: [], protected: false },
  ],
}

describe('DockerResourcesPanel', () => {
  test('keeps disk usage compact and exposes only safe delete actions', () => {
    render(<DockerResourcesPanel response={response} loading={false} online onRefresh={vi.fn()} onAction={vi.fn(async () => ({ success: true }))} onShowToast={vi.fn()} />)

    const usage = screen.getByLabelText('Docker 磁盘占用')
    expect(within(usage).getByText('镜像层')).toBeInTheDocument()
    expect(within(usage).getByText('容器写层')).toBeInTheDocument()
    expect(screen.getByText('nginx:latest')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除镜像 nginx:latest' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '删除镜像 redis:7' })).toBeDisabled()

    fireEvent.click(screen.getByRole('tab', { name: /网络/ }))
    expect(screen.getByRole('button', { name: '删除网络 bridge' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '删除网络 demo_default' })).toBeEnabled()
  })

  test('pulls an image and refreshes only the resource panel', async () => {
    const onAction = vi.fn(async () => ({ success: true }))
    const onRefresh = vi.fn(async () => undefined)
    const onShowToast = vi.fn()
    render(<DockerResourcesPanel response={response} loading={false} online onRefresh={onRefresh} onAction={onAction} onShowToast={onShowToast} />)

    fireEvent.change(screen.getByLabelText('镜像引用'), { target: { value: 'registry.example/team/app:1.0' } })
    fireEvent.click(screen.getByRole('button', { name: '拉取镜像' }))

    await waitFor(() => expect(onAction).toHaveBeenCalledWith('image', 'registry.example/team/app:1.0', 'pull'))
    await waitFor(() => expect(onRefresh).toHaveBeenCalledTimes(1))
    expect(onShowToast).toHaveBeenCalledWith('镜像拉取成功', 'success')
  })

  test('requires a custom confirmation and warns before deleting volume data', async () => {
    const onAction = vi.fn(async () => ({ success: true }))
    render(<DockerResourcesPanel response={response} loading={false} online onRefresh={vi.fn(async () => undefined)} onAction={onAction} onShowToast={vi.fn()} />)

    fireEvent.click(screen.getByRole('tab', { name: /数据卷/ }))
    fireEvent.click(screen.getByRole('button', { name: '删除数据卷 demo_data' }))
    const dialog = screen.getByRole('dialog', { name: '删除数据卷' })
    expect(within(dialog).getByText(/永久删除且无法恢复/)).toBeInTheDocument()
    expect(onAction).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(onAction).toHaveBeenCalledWith('volume', 'demo_data', 'remove'))
  })

  test('shows a clear upgrade state for an older Agent', () => {
    render(<DockerResourcesPanel response={{ success: false, supported: false, usage: {}, images: [], volumes: [], networks: [], error: '当前 Agent 不支持 Docker 资源管理，请升级 Agent' }} loading={false} online onRefresh={vi.fn()} onAction={vi.fn(async () => ({ success: true }))} onShowToast={vi.fn()} />)
    expect(screen.getByText('当前 Agent 不支持资源管理')).toBeInTheDocument()
    expect(screen.getByText(/请升级 Agent/)).toBeInTheDocument()
  })
})
