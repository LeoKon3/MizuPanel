import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { executeK8sResourceAction, fetchK8sDiagnostics } from '../../api/k8s'
import { K8sDiagnosticsDrawer } from './K8sDiagnosticsDrawer'

vi.mock('../../api/k8s', () => ({
  executeK8sResourceAction: vi.fn(),
  fetchK8sDiagnostics: vi.fn(),
}))

const diagnostics = {
  kind: 'pod' as const,
  namespace: 'default',
  name: 'nginx',
  status: 'Running',
  metadata: { app: 'nginx' },
  summary: { node: 'node-1' },
  events: [],
  yaml: 'apiVersion: v1\nkind: Pod\nmetadata:\n  name: nginx\n',
  describe: 'Name: nginx\nNamespace: default\n',
}

describe('K8sDiagnosticsDrawer', () => {
  const originalExecCommand = document.execCommand

  afterEach(() => {
    Object.assign(document, { execCommand: originalExecCommand })
    vi.clearAllMocks()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  test('copies YAML with a textarea fallback when clipboard write is rejected', async () => {
    const onToast = vi.fn()
    const writeText = vi.fn(async () => {
      throw new Error('denied')
    })
    const execCommand = vi.fn(() => true)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    Object.assign(document, { execCommand })
    vi.mocked(fetchK8sDiagnostics).mockResolvedValue({ success: true, diagnostics })

    render(
      <K8sDiagnosticsDrawer
        clusterId="cluster-1"
        resource={{ kind: 'pod', namespace: 'default', name: 'nginx' }}
        open
        onClose={vi.fn()}
        onToast={onToast}
        onOpenLogs={vi.fn()}
      />
    )

    fireEvent.click(await screen.findByRole('button', { name: 'YAML' }))
    fireEvent.click(screen.getByRole('button', { name: '复制' }))

    await waitFor(() => expect(writeText).toHaveBeenCalledWith(diagnostics.yaml))
    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(onToast).toHaveBeenCalledWith('YAML复制成功', 'success')
  })

  test('shows YAML editing in the YAML pane without adding a drawer action menu', async () => {
    vi.mocked(fetchK8sDiagnostics).mockResolvedValue({ success: true, diagnostics })

    render(
      <K8sDiagnosticsDrawer
        clusterId="cluster-1"
        resource={{ kind: 'pod', namespace: 'default', name: 'nginx' }}
        open
        onClose={vi.fn()}
        onToast={vi.fn()}
        onOpenLogs={vi.fn()}
      />
    )

    await screen.findByText('nginx')
    expect(screen.queryByRole('button', { name: '操作' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'YAML' }))
    expect(screen.getByRole('button', { name: '编辑' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '删除 Pod' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重启' })).not.toBeInTheDocument()
  })

  test('requires a successful dry run before saving YAML edits', async () => {
    const onToast = vi.fn()
    const onResourceChanged = vi.fn()
    vi.mocked(fetchK8sDiagnostics).mockResolvedValue({ success: true, diagnostics })
    vi.mocked(executeK8sResourceAction).mockResolvedValue({ success: true, message: 'ok' })

    render(
      <K8sDiagnosticsDrawer
        clusterId="cluster-1"
        resource={{ kind: 'pod', namespace: 'default', name: 'nginx' }}
        open
        onClose={vi.fn()}
        onToast={onToast}
        onOpenLogs={vi.fn()}
        onResourceChanged={onResourceChanged}
      />
    )

    fireEvent.click(await screen.findByRole('button', { name: 'YAML' }))
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))

    const editor = await screen.findByLabelText('YAML 内容')
    expect(editor).toHaveValue(diagnostics.yaml)
    const saveButton = screen.getByRole('button', { name: '保存' })
    expect(saveButton).toBeDisabled()

    fireEvent.change(editor, { target: { value: `${diagnostics.yaml}spec:\n  activeDeadlineSeconds: 30\n` } })
    fireEvent.click(screen.getByRole('button', { name: 'Dry Run' }))

    await waitFor(() => {
      expect(executeK8sResourceAction).toHaveBeenCalledWith('cluster-1', 'pod', 'default', 'nginx', {
        action: 'dry_run_apply',
        yaml: expect.stringContaining('activeDeadlineSeconds: 30'),
      })
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '保存' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(executeK8sResourceAction).toHaveBeenLastCalledWith('cluster-1', 'pod', 'default', 'nginx', {
        action: 'apply',
        yaml: expect.stringContaining('activeDeadlineSeconds: 30'),
      })
    })
    expect(onToast).toHaveBeenCalledWith('YAML校验成功', 'success')
    expect(onToast).toHaveBeenCalledWith('YAML保存成功', 'success')
    expect(onResourceChanged).toHaveBeenCalled()
  })

  test('surfaces related unhealthy pods for workload pending diagnosis', async () => {
    const onSwitchResource = vi.fn()
    vi.mocked(fetchK8sDiagnostics).mockResolvedValue({
      success: true,
      diagnostics: {
        ...diagnostics,
        kind: 'deployment',
        name: 'api',
        status: '0/1 ready',
        events: [],
      },
    })

    render(
      <K8sDiagnosticsDrawer
        clusterId="cluster-1"
        resource={{ kind: 'deployment', namespace: 'default', name: 'api' }}
        relatedPods={[
          { name: 'api-abc', namespace: 'default', status: 'Pending', ready: '0/1', restarts: 0, age: '2m', node: '', workload_kind: 'deployment', workload_name: 'api' },
        ]}
        open
        onClose={vi.fn()}
        onToast={vi.fn()}
        onOpenLogs={vi.fn()}
        onSwitchResource={onSwitchResource}
      />
    )

    await screen.findByText('关联 Pod 状态')
    expect(screen.getByText('api-abc')).toBeInTheDocument()
    expect(screen.getByText('Pending')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '查看 Pod 诊断 api-abc' }))
    expect(onSwitchResource).toHaveBeenCalledWith('pod', 'default', 'api-abc')
  })
})
