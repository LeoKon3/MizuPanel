import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { NodeDetail } from './NodeDetail'
import type { DockerComposeDeploymentRequest, DockerComposeDeploymentResponse } from '../types'

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

  test('keeps external paths visible but never renders a managed project path', () => {
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{
          success: true,
          supported: true,
          deployment_supported: true,
          projects: [
            { name: 'external-demo', management: 'external', config_files: ['/srv/external-demo/compose.yaml'], services: [] },
            { name: 'internal-derived-name', management: 'managed', managed_project_id: 'managed-1', display_name: '托管演示', revision: 2, rollback_available: false, config_files: ['/private/managed/sentinel-compose.yaml'], services: [] },
          ],
        }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeDeployment={vi.fn(async () => ({ success: true, supported: true }))}
        onRefreshDockerCompose={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))

    expect(screen.getByText('/srv/external-demo/compose.yaml')).toBeInTheDocument()
    expect(screen.queryByText('/private/managed/sentinel-compose.yaml')).not.toBeInTheDocument()
    expect(screen.getByText('托管')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '新建托管应用' })).toBeInTheDocument()
    expect(within(screen.getByRole('toolbar', { name: 'external-demo Compose 操作' })).queryByRole('button', { name: '更新应用' })).not.toBeInTheDocument()
    const managedToolbar = within(screen.getByRole('toolbar', { name: '托管演示 Compose 操作' }))
    expect(managedToolbar.getByRole('button', { name: '更新应用' })).toBeInTheDocument()
    expect(managedToolbar.getByRole('button', { name: '启动 / 重建' })).toBeInTheDocument()
    expect(managedToolbar.getByRole('button', { name: '日志' })).toBeInTheDocument()
    expect(managedToolbar.queryByRole('button', { name: '回滚上一版本' })).not.toBeInTheDocument()
  })

  test('previews a managed draft before applying the exact YAML and environment payload', async () => {
    const onDeployment = vi.fn()
      .mockResolvedValueOnce({
        success: true,
        supported: true,
        action: 'preview',
        confirmation_token: 'preview-token',
        project: { name: 'managed-web', display_name: '网站前台', management: 'managed', managed_project_id: 'managed-1', services: [] },
        risks: [{ code: 'host_network', severity: 'warning', message: '服务使用主机网络。' }],
      })
      .mockResolvedValueOnce({ success: true, supported: true, action: 'apply', project: { name: 'managed-web', display_name: '网站前台', management: 'managed', managed_project_id: 'managed-1', services: [] } })
    const onRefresh = vi.fn(async () => undefined)
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{ success: true, supported: true, deployment_supported: true, projects: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeDeployment={onDeployment}
        onRefreshDockerCompose={onRefresh}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))
    fireEvent.click(screen.getByRole('button', { name: '新建托管应用' }))

    const editor = screen.getByRole('dialog', { name: '新建托管 Compose 应用' })
    const yaml = 'services:\n  web:\n    image: nginx:alpine\n'
    const envFile = 'API_TOKEN=temporary-secret\n'
    fireEvent.change(within(editor).getByLabelText('应用名称'), { target: { value: '网站前台' } })
    fireEvent.change(within(editor).getByLabelText('Compose YAML'), { target: { value: yaml } })
    fireEvent.change(within(editor).getByLabelText('可选 .env'), { target: { value: envFile } })
    fireEvent.click(within(editor).getByLabelText('部署前拉取镜像'))
    fireEvent.click(within(editor).getByRole('button', { name: '预览部署' }))

    await waitFor(() => expect(onDeployment).toHaveBeenNthCalledWith(1, 'node-1', {
      action: 'preview',
      display_name: '网站前台',
      compose_yaml: yaml,
      env_file: envFile,
      pull_images: false,
    }))
    const confirmation = await screen.findByRole('dialog', { name: '确认托管 Compose 部署' })
    expect(within(confirmation).getByText('服务使用主机网络。')).toBeInTheDocument()
    fireEvent.click(within(confirmation).getByRole('button', { name: '确认并部署' }))

    await waitFor(() => expect(onDeployment).toHaveBeenNthCalledWith(2, 'node-1', {
      action: 'apply',
      project_id: 'managed-1',
      display_name: '网站前台',
      compose_yaml: yaml,
      env_file: envFile,
      pull_images: false,
      confirmation_token: 'preview-token',
    }))
    expect(await screen.findByText('托管应用部署成功')).toBeInTheDocument()
    expect(onRefresh).toHaveBeenCalledWith('node-1')
  })

  test('cancelling managed deployment dialogs never applies a draft', async () => {
    const onDeployment = vi.fn(async (_nodeID: string, _request: DockerComposeDeploymentRequest): Promise<DockerComposeDeploymentResponse> => ({
      success: true,
      supported: true,
      action: 'preview',
      confirmation_token: 'preview-token',
      project: { name: 'managed-demo', management: 'managed', managed_project_id: 'managed-1', services: [] },
      risks: [],
    }))
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{ success: true, supported: true, deployment_supported: true, projects: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeDeployment={onDeployment}
        onRefreshDockerCompose={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))
    fireEvent.click(screen.getByRole('button', { name: '新建托管应用' }))
    fireEvent.click(screen.getByRole('button', { name: '取消编辑' }))
    expect(onDeployment).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '新建托管应用' }))
    const editor = screen.getByRole('dialog', { name: '新建托管 Compose 应用' })
    fireEvent.change(within(editor).getByLabelText('应用名称'), { target: { value: '演示' } })
    fireEvent.change(within(editor).getByLabelText('Compose YAML'), { target: { value: 'services: {}' } })
    fireEvent.click(within(editor).getByRole('button', { name: '预览部署' }))
    const confirmation = await screen.findByRole('dialog', { name: '确认托管 Compose 部署' })
    fireEvent.click(within(confirmation).getByRole('button', { name: '取消确认' }))
    expect(onDeployment).toHaveBeenCalledTimes(1)
    expect(onDeployment).not.toHaveBeenCalledWith('node-1', expect.objectContaining({ action: 'apply' }))
  })

  test('does not render deployment output when a preview fails', async () => {
    const onDeployment = vi.fn(async (): Promise<DockerComposeDeploymentResponse> => ({ success: false, supported: true, action: 'preview', output: 'API_TOKEN=should-not-appear' }))
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{ success: true, supported: true, deployment_supported: true, projects: [] }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeDeployment={onDeployment}
        onRefreshDockerCompose={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))
    fireEvent.click(screen.getByRole('button', { name: '新建托管应用' }))
    const editor = screen.getByRole('dialog', { name: '新建托管 Compose 应用' })
    fireEvent.change(within(editor).getByLabelText('应用名称'), { target: { value: '演示' } })
    fireEvent.change(within(editor).getByLabelText('Compose YAML'), { target: { value: 'services: {}' } })
    fireEvent.click(within(editor).getByRole('button', { name: '预览部署' }))

    expect(await screen.findByText('托管应用预览失败: 未知错误')).toBeInTheDocument()
    expect(screen.queryByText('API_TOKEN=should-not-appear')).not.toBeInTheDocument()
  })

  test('ignores a deployment preview that resolves after the selected node changes', async () => {
    let resolvePreview: (response: DockerComposeDeploymentResponse) => void = () => undefined
    const onDeployment = vi.fn(() => new Promise<DockerComposeDeploymentResponse>((resolve) => {
      resolvePreview = resolve
    }))
    const props = {
      metrics: [],
      processSnapshot: { node_id: node.id, collected_at: 0, error: '', processes: [] },
      dockerSnapshot: { node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] },
      dockerCompose: { success: true, supported: true, deployment_supported: true, projects: [] },
      range: '1h' as const,
      onRangeChange: vi.fn(),
      onDockerComposeDeployment: onDeployment,
      onRefreshDockerCompose: vi.fn(async () => undefined),
    }
    const { rerender } = render(<NodeDetail node={node} {...props} />)

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))
    fireEvent.click(screen.getByRole('button', { name: '新建托管应用' }))
    const editor = screen.getByRole('dialog', { name: '新建托管 Compose 应用' })
    fireEvent.change(within(editor).getByLabelText('应用名称'), { target: { value: '迟到的应用' } })
    fireEvent.change(within(editor).getByLabelText('Compose YAML'), { target: { value: 'services:\n  app:\n    image: alpine:3\n' } })
    fireEvent.change(within(editor).getByLabelText('可选 .env'), { target: { value: 'LATE_SECRET=must-not-cross-hosts\n' } })
    fireEvent.click(within(editor).getByRole('button', { name: '预览部署' }))
    await waitFor(() => expect(onDeployment).toHaveBeenCalledTimes(1))

    const nextNode = { ...node, id: 'node-2', name: 'next-host', hostname: 'next-host', ip: '10.0.0.3' }
    rerender(<NodeDetail node={nextNode} {...props} processSnapshot={{ ...props.processSnapshot, node_id: nextNode.id }} dockerSnapshot={{ ...props.dockerSnapshot, node_id: nextNode.id }} />)
    resolvePreview({
      success: true,
      supported: true,
      action: 'preview',
      confirmation_token: 'late-preview-token',
      project: { name: 'late-project', management: 'managed', managed_project_id: 'late-managed-id', services: [] },
      risks: [],
    })

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '确认托管 Compose 部署' })).not.toBeInTheDocument())
    expect(screen.queryByDisplayValue('LATE_SECRET=must-not-cross-hosts\n')).not.toBeInTheDocument()
  })

  test('uses custom rollback and archive confirmations for managed applications', async () => {
    const onDeployment = vi.fn(async (_nodeID: string, request: DockerComposeDeploymentRequest): Promise<DockerComposeDeploymentResponse> => ({ success: true, supported: true, action: request.action }))
    const nativeConfirm = vi.spyOn(window, 'confirm')
    const nativeAlert = vi.spyOn(window, 'alert')
    const nativePrompt = vi.spyOn(window, 'prompt')
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{
          success: true,
          supported: true,
          deployment_supported: true,
          projects: [{ name: 'managed-web', management: 'managed', managed_project_id: 'managed-1', display_name: '网站前台', rollback_available: true, services: [] }],
        }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeDeployment={onDeployment}
        onRefreshDockerCompose={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))
    const toolbar = screen.getByRole('toolbar', { name: '网站前台 Compose 操作' })
    fireEvent.click(within(toolbar).getByRole('button', { name: '回滚上一版本' }))
    const rollback = screen.getByRole('dialog', { name: '确认托管应用回滚' })
    expect(onDeployment).not.toHaveBeenCalled()
    fireEvent.click(within(rollback).getByRole('button', { name: '确认回滚' }))
    await waitFor(() => expect(onDeployment).toHaveBeenCalledWith('node-1', { action: 'rollback', project_id: 'managed-1' }))

    fireEvent.click(within(toolbar).getByRole('button', { name: '归档' }))
    const archive = await screen.findByRole('dialog', { name: '确认托管应用归档' })
    fireEvent.click(within(archive).getByRole('button', { name: '确认归档' }))
    await waitFor(() => expect(onDeployment).toHaveBeenCalledWith('node-1', { action: 'archive', project_id: 'managed-1' }))
    expect(nativeConfirm).not.toHaveBeenCalled()
    expect(nativeAlert).not.toHaveBeenCalled()
    expect(nativePrompt).not.toHaveBeenCalled()
    nativeConfirm.mockRestore()
    nativeAlert.mockRestore()
    nativePrompt.mockRestore()
  })

  test('does not expose managed deployment controls until the Agent advertises support', () => {
    render(
      <NodeDetail
        node={node}
        metrics={[]}
        processSnapshot={{ node_id: node.id, collected_at: 0, error: '', processes: [] }}
        dockerSnapshot={{ node_id: node.id, collected_at: 0, available: true, version: '28.0', error: '', containers: [] }}
        dockerCompose={{ success: true, supported: true, projects: [{ name: 'legacy-managed', management: 'managed', managed_project_id: 'managed-1', display_name: '旧托管应用', rollback_available: true, services: [] }] }}
        range="1h"
        onRangeChange={vi.fn()}
        onDockerComposeDeployment={vi.fn(async () => ({ success: true, supported: true }))}
        onRefreshDockerCompose={vi.fn(async () => undefined)}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '容器信息' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Compose' }))
    const toolbar = screen.getByRole('toolbar', { name: '旧托管应用 Compose 操作' })
    expect(screen.queryByRole('button', { name: '新建托管应用' })).not.toBeInTheDocument()
    expect(within(toolbar).queryByRole('button', { name: '更新应用' })).not.toBeInTheDocument()
    expect(within(toolbar).queryByRole('button', { name: '回滚上一版本' })).not.toBeInTheDocument()
    expect(within(toolbar).queryByRole('button', { name: '归档' })).not.toBeInTheDocument()
  })
})
