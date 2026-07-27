import { createRef } from 'react'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import * as api from '../api/client'
import { TaskRunDetailModal } from '../components/TaskRunDetailModal'
import type { AutomationRun, AutomationRunDetail, AutomationScript, Node, ScheduledTask } from '../types'
import { TasksPage } from './TasksPage'

vi.mock('../api/client', () => ({
  createAutomationScript: vi.fn(),
  createScheduledTask: vi.fn(),
  deleteAutomationScript: vi.fn(),
  deleteScheduledTask: vi.fn(),
  getAutomationRun: vi.fn(),
  getAutomationRuns: vi.fn(),
  getAutomationScripts: vi.fn(),
  getScheduledTasks: vi.fn(),
  runAutomationScript: vi.fn(),
  runScheduledTask: vi.fn(),
  toggleScheduledTask: vi.fn(),
  updateAutomationScript: vi.fn(),
  updateScheduledTask: vi.fn()
}))

const nodes: Node[] = [
  { id: 'node-1', name: 'Oracle SG', hostname: 'oracle-sg', ip: '10.0.0.1', os: 'linux', arch: 'arm64', kernel: '6.6', agent_version: '0.1.9', status: 'online', last_seen_at: '', task_runner_supported: true },
  { id: 'node-2', name: 'Legacy HK', hostname: 'legacy-hk', ip: '10.0.0.2', os: 'linux', arch: 'amd64', kernel: '6.1', agent_version: '0.1.8', status: 'online', last_seen_at: '', task_runner_supported: false },
  { id: 'node-3', name: 'Offline SG', hostname: 'offline-sg', ip: '10.0.0.3', os: 'linux', arch: 'amd64', kernel: '6.6', agent_version: '0.1.9', status: 'offline', last_seen_at: '', task_runner_supported: false }
]

const script: AutomationScript = {
  id: 7,
  name: 'Cleanup',
  description: 'Clear cache',
  content: 'echo done',
  timeout_seconds: 300,
  revision: 2,
  created_at: '2026-07-26T08:00:00Z',
  updated_at: '2026-07-26T08:00:00Z'
}

const task: ScheduledTask = {
  id: 9,
  name: 'Nightly cleanup',
  script_id: 7,
  script_name: 'Cleanup',
  script_revision: 2,
  node_ids: ['node-1'],
  cron_expression: '0 2 * * *',
  timezone: 'Asia/Shanghai',
  timeout_seconds: 300,
  enabled: true,
  notification_policy: 'failure',
  notification_channels: [],
  next_run_at: '2026-07-27T18:00:00Z',
  last_scheduled_at: null,
  latest_run_status: 'failed',
  latest_run_at: '2026-07-26T08:00:01Z',
  created_at: '2026-07-26T08:00:00Z',
  updated_at: '2026-07-26T08:00:00Z'
}

const run: AutomationRun = {
  id: 11,
  script_id: 7,
  task_name: '',
  script_name: 'Cleanup',
  script_revision: 2,
  trigger: 'manual',
  status: 'success',
  total_targets: 2,
  completed_targets: 2,
  success_targets: 1,
  failed_targets: 1,
  notification_sent: false,
  started_at: '2026-07-26T08:00:00Z',
  completed_at: '2026-07-26T08:00:01Z',
  created_at: '2026-07-26T08:00:00Z'
}

const runDetail: AutomationRunDetail = {
  ...run,
  targets: [
    { id: 101, run_id: 11, node_id: 'node-1', node_name: 'Oracle SG', status: 'success', exit_code: 0, output: 'done', output_truncated: true, duration_ms: 42, started_at: '2026-07-26T08:00:00Z', completed_at: '2026-07-26T08:00:00Z', created_at: '2026-07-26T08:00:00Z' },
    { id: 102, run_id: 11, node_id: 'node-2', node_name: 'Legacy HK', status: 'unsupported', output: '', output_truncated: false, error: '请升级 Agent', duration_ms: 0, created_at: '2026-07-26T08:00:00Z' }
  ]
}

function deferred<Value>() {
  let resolve!: (value: Value | PromiseLike<Value>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<Value>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('TasksPage', () => {
  beforeEach(() => {
    vi.mocked(api.getAutomationScripts).mockResolvedValue({ scripts: [] })
    vi.mocked(api.getScheduledTasks).mockResolvedValue({ tasks: [] })
    vi.mocked(api.getAutomationRuns).mockResolvedValue({ runs: [], next_before_id: null })
    vi.mocked(api.getAutomationRun).mockResolvedValue(runDetail)
    vi.mocked(api.createAutomationScript).mockResolvedValue(script)
    vi.mocked(api.updateAutomationScript).mockResolvedValue(script)
    vi.mocked(api.deleteAutomationScript).mockResolvedValue(undefined)
    vi.mocked(api.createScheduledTask).mockResolvedValue(task)
    vi.mocked(api.updateScheduledTask).mockResolvedValue(task)
    vi.mocked(api.deleteScheduledTask).mockResolvedValue(undefined)
    vi.mocked(api.toggleScheduledTask).mockResolvedValue({ ...task, enabled: false })
    vi.mocked(api.runAutomationScript).mockResolvedValue({ ...run, status: 'queued', completed_targets: 0, success_targets: 0, failed_targets: 0, completed_at: undefined })
    vi.mocked(api.runScheduledTask).mockResolvedValue({ ...run, task_id: 9, task_name: task.name, status: 'queued', completed_targets: 0, success_targets: 0, failed_targets: 0, completed_at: undefined })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  test('renders accessible tabs and bounded empty states', async () => {
    render(<TasksPage nodes={nodes} />)

    expect(await screen.findByText('还没有计划任务')).toBeInTheDocument()
    const tablist = screen.getByRole('tablist', { name: '任务中心视图' })
    const tasksTab = within(tablist).getByRole('tab', { name: '计划任务' })
    expect(tasksTab).toHaveAttribute('aria-selected', 'true')

    fireEvent.click(within(tablist).getByRole('tab', { name: '脚本库' }))
    expect(await screen.findByText('还没有脚本')).toBeInTheDocument()
    fireEvent.click(within(tablist).getByRole('tab', { name: '执行记录' }))
    expect(await screen.findByText('暂无执行记录')).toBeInTheDocument()
    expect(screen.getByLabelText('执行记录筛选')).toHaveClass('xl:grid-cols-6')
  })

  test('contains script form focus, restores the trigger, and creates a bounded script', async () => {
    vi.mocked(api.getAutomationScripts)
      .mockResolvedValueOnce({ scripts: [] })
      .mockResolvedValue({ scripts: [script] })
    render(<TasksPage nodes={nodes} />)
    fireEvent.click(screen.getByRole('tab', { name: '脚本库' }))
    await screen.findByText('还没有脚本')

    const createTrigger = screen.getAllByRole('button', { name: '创建脚本' })[1]
    fireEvent.click(createTrigger)
    const firstDialog = screen.getByRole('dialog', { name: '创建脚本' })
    const firstName = within(firstDialog).getByLabelText('脚本名称')
    await waitFor(() => expect(firstName).toHaveFocus())
    fireEvent.keyDown(firstName, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: '创建脚本' })).not.toBeInTheDocument()
    expect(createTrigger).toHaveFocus()

    fireEvent.click(createTrigger)
    const dialog = screen.getByRole('dialog', { name: '创建脚本' })
    fireEvent.change(within(dialog).getByLabelText('脚本名称'), { target: { value: 'Cleanup' } })
    fireEvent.change(within(dialog).getByLabelText('脚本说明'), { target: { value: 'Clear cache' } })
    fireEvent.change(within(dialog).getByLabelText('Shell 内容'), { target: { value: 'echo done' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }))

    await waitFor(() => expect(api.createAutomationScript).toHaveBeenCalledWith({ name: 'Cleanup', description: 'Clear cache', content: 'echo done', timeout_seconds: 300 }))
    expect(await screen.findByText('脚本创建成功')).toBeInTheDocument()
    expect(await screen.findByText('Cleanup')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新当前页' })).toHaveFocus())
  })

  test('creates a five-field schedule with targets and shared notification channels', async () => {
    vi.mocked(api.getAutomationScripts).mockResolvedValue({ scripts: [script] })
    vi.mocked(api.getScheduledTasks)
      .mockResolvedValueOnce({ tasks: [] })
      .mockResolvedValue({ tasks: [task] })
    render(<TasksPage nodes={nodes} />)
    await screen.findByText('还没有计划任务')

    fireEvent.click(screen.getAllByRole('button', { name: '创建计划' })[0])
    const dialog = screen.getByRole('dialog', { name: '创建计划任务' })
    fireEvent.change(within(dialog).getByLabelText('计划任务名称'), { target: { value: 'Nightly cleanup' } })
    fireEvent.change(within(dialog).getByLabelText('Cron 表达式'), { target: { value: '0 2 * * *' } })
    fireEvent.change(within(dialog).getByLabelText('计划任务时区'), { target: { value: 'Asia/Shanghai' } })
    fireEvent.click(within(dialog).getByLabelText('选择 Oracle SG'))
    fireEvent.change(within(dialog).getByLabelText('计划任务通知策略'), { target: { value: 'always' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '+ Webhook' }))
    fireEvent.change(within(dialog).getByLabelText('Webhook Webhook 地址 1'), { target: { value: 'https://hooks.example.com/task' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }))

    await waitFor(() => expect(api.createScheduledTask).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Nightly cleanup',
      script_id: 7,
      node_ids: ['node-1'],
      cron_expression: '0 2 * * *',
      timezone: 'Asia/Shanghai',
      timeout_seconds: 300,
      enabled: true,
      notification_policy: 'always',
      notification_channels: [{ type: 'webhook', webhook_url: 'https://hooks.example.com/task' }]
    })))
    expect(await screen.findByText('计划任务创建成功')).toBeInTheDocument()
    expect(await screen.findByText('Nightly cleanup')).toBeInTheDocument()
  })

  test('runs a script on multiple explicit nodes and shows bounded per-target output', async () => {
    vi.mocked(api.getAutomationScripts).mockResolvedValue({ scripts: [script] })
    render(<TasksPage nodes={nodes} />)
    fireEvent.click(screen.getByRole('tab', { name: '脚本库' }))
    await screen.findByText('Cleanup')

    const runTrigger = screen.getByRole('button', { name: '运行 Cleanup' })
    fireEvent.click(runTrigger)
    const runDialog = screen.getByRole('dialog', { name: '运行脚本' })
    const offlineRow = within(runDialog).getByLabelText('选择 Offline SG').closest('label')
    expect(offlineRow).not.toBeNull()
    expect(within(offlineRow as HTMLElement).getByText('离线')).toBeInTheDocument()
    expect(within(offlineRow as HTMLElement).queryByText('需升级')).not.toBeInTheDocument()
    fireEvent.click(within(runDialog).getByLabelText('选择 Oracle SG'))
    fireEvent.click(within(runDialog).getByLabelText('选择 Legacy HK'))
    expect(within(runDialog).getByText('需升级')).toBeInTheDocument()
    fireEvent.click(within(runDialog).getByRole('button', { name: '运行到 2 台节点' }))

    await waitFor(() => expect(api.runAutomationScript).toHaveBeenCalledWith(7, ['node-1', 'node-2']))
    expect(await screen.findByText('脚本执行请求已受理')).toBeInTheDocument()
    const detailDialog = await screen.findByRole('dialog', { name: '执行详情' })
    expect(await within(detailDialog).findByText('done')).toBeInTheDocument()
    expect(within(detailDialog).getByText('输出已截断')).toBeInTheDocument()
    expect(within(detailDialog).getByText('需升级')).toBeInTheDocument()
    fireEvent.click(within(detailDialog).getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(runTrigger).toHaveFocus())
  })

  test('rejects a stale run-list response after filters change', async () => {
    const stale = deferred<{ runs: AutomationRun[], next_before_id: number | null }>()
    const staleRun = { ...run, id: 20, script_name: 'stale-run' }
    const currentRun = { ...run, id: 21, status: 'failed' as const, script_name: 'current-run' }
    vi.mocked(api.getAutomationRuns)
      .mockImplementationOnce(() => stale.promise)
      .mockResolvedValueOnce({ runs: [currentRun], next_before_id: null })
    render(<TasksPage nodes={nodes} />)
    fireEvent.click(screen.getByRole('tab', { name: '执行记录' }))
    fireEvent.change(screen.getByLabelText('批次状态'), { target: { value: 'failed' } })

    expect(await screen.findByText('current-run')).toBeInTheDocument()
    await act(async () => {
      stale.resolve({ runs: [staleRun], next_before_id: null })
      await stale.promise
    })
    expect(screen.queryByText('stale-run')).not.toBeInTheDocument()
    expect(api.getAutomationRuns).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'failed', limit: 50 }), expect.any(AbortSignal))
  })

  test('does not show previous rows when a changed run filter fails', async () => {
    const previousRun = { ...run, id: 30, script_name: 'previous-filter-run' }
    vi.mocked(api.getAutomationRuns)
      .mockResolvedValueOnce({ runs: [previousRun], next_before_id: null })
      .mockRejectedValueOnce(new Error('filter unavailable'))
    render(<TasksPage nodes={nodes} />)
    fireEvent.click(screen.getByRole('tab', { name: '执行记录' }))
    expect(await screen.findByText('previous-filter-run')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('批次状态'), { target: { value: 'failed' } })

    expect(await screen.findByText('执行记录加载失败: filter unavailable')).toBeInTheDocument()
    expect(screen.queryByText('previous-filter-run')).not.toBeInTheDocument()
  })

  test('retries run polling while preserving and not cancelling loaded history pages', async () => {
    vi.useFakeTimers()
    const activeRun: AutomationRun = {
      ...run,
      id: 100,
      script_name: 'active-run',
      status: 'running',
      completed_targets: 0,
      success_targets: 0,
      failed_targets: 0,
      completed_at: undefined
    }
    const completedRun: AutomationRun = {
      ...activeRun,
      status: 'success',
      completed_targets: 2,
      success_targets: 2,
      completed_at: '2026-07-26T08:00:02Z'
    }
    const olderRun = { ...run, id: 80, script_name: 'older-run' }
    const pendingPage = deferred<{ runs: AutomationRun[], next_before_id: number | null }>()
    let headRequests = 0
    let pageSignal: AbortSignal | undefined
    vi.mocked(api.getAutomationRuns).mockImplementation((query = {}, signal) => {
      if (query.before_id === 90) {
        pageSignal = signal
        return pendingPage.promise
      }
      headRequests += 1
      if (headRequests === 1) return Promise.resolve({ runs: [activeRun], next_before_id: 90 })
      if (headRequests === 2) return Promise.reject(new Error('temporary polling failure'))
      return Promise.resolve({ runs: [completedRun], next_before_id: 90 })
    })

    render(<TasksPage nodes={nodes} />)
    fireEvent.click(screen.getByRole('tab', { name: '执行记录' }))
    await act(async () => { await Promise.resolve() })
    expect(screen.getByText('active-run')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))
    expect(pageSignal).toBeDefined()
    await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
    expect(pageSignal?.aborted).toBe(false)
    expect(screen.getByText('执行记录刷新失败: temporary polling failure')).toBeInTheDocument()

    await act(async () => {
      pendingPage.resolve({ runs: [olderRun], next_before_id: null })
      await pendingPage.promise
    })
    expect(screen.getByText('older-run')).toBeInTheDocument()

    await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
    const activeRow = screen.getByText('active-run').closest('tr')
    expect(activeRow).not.toBeNull()
    expect(within(activeRow as HTMLElement).getByText('成功')).toBeInTheDocument()
    expect(screen.getByText('older-run')).toBeInTheDocument()
    expect(headRequests).toBe(3)
  })

  test('retries run-detail polling after a transient failure', async () => {
    vi.useFakeTimers()
    const runningDetail: AutomationRunDetail = {
      ...runDetail,
      status: 'running',
      completed_targets: 0,
      success_targets: 0,
      failed_targets: 0,
      completed_at: undefined
    }
    const onRunUpdated = vi.fn()
    vi.mocked(api.getAutomationRun)
      .mockResolvedValueOnce(runningDetail)
      .mockRejectedValueOnce(new Error('temporary detail failure'))
      .mockResolvedValueOnce(runDetail)

    render(
      <TaskRunDetailModal
        runID={11}
        returnFocusRef={createRef<HTMLElement>()}
        onClose={vi.fn()}
        onToast={vi.fn()}
        onRunUpdated={onRunUpdated}
      />
    )
    await act(async () => { await Promise.resolve() })
    expect(screen.getByText('执行中')).toBeInTheDocument()

    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(screen.getByText('执行详情刷新失败: temporary detail failure')).toBeInTheDocument()

    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(api.getAutomationRun).toHaveBeenCalledTimes(3)
    expect(screen.queryByText('执行详情刷新失败: temporary detail failure')).not.toBeInTheDocument()
    expect(onRunUpdated).toHaveBeenLastCalledWith(runDetail)
  })

  test('dismisses an accepted-run toast while detail polling continues', async () => {
    vi.useFakeTimers()
    const runningDetail: AutomationRunDetail = {
      ...runDetail,
      status: 'running',
      completed_targets: 0,
      success_targets: 0,
      failed_targets: 0,
      completed_at: undefined
    }
    vi.mocked(api.getAutomationScripts).mockResolvedValue({ scripts: [script] })
    vi.mocked(api.getAutomationRun).mockResolvedValue(runningDetail)

    render(<TasksPage nodes={nodes} />)
    await act(async () => { await Promise.resolve() })
    fireEvent.click(screen.getByRole('tab', { name: '脚本库' }))
    fireEvent.click(screen.getByRole('button', { name: '运行 Cleanup' }))
    const runDialog = screen.getByRole('dialog', { name: '运行脚本' })
    fireEvent.click(within(runDialog).getByLabelText('选择 Oracle SG'))
    fireEvent.click(within(runDialog).getByRole('button', { name: '运行到 1 台节点' }))
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(screen.getByText('脚本执行请求已受理')).toBeInTheDocument()

    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(screen.getByText('脚本执行请求已受理')).toBeInTheDocument()
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.queryByText('脚本执行请求已受理')).not.toBeInTheDocument()
  })

  test('uses confirmation dialogs for task execution and deletion', async () => {
    vi.mocked(api.getAutomationScripts).mockResolvedValue({ scripts: [script] })
    vi.mocked(api.getScheduledTasks)
      .mockResolvedValueOnce({ tasks: [task] })
      .mockResolvedValue({ tasks: [] })
    render(<TasksPage nodes={nodes} />)
    await screen.findByText('Nightly cleanup')
    expect(screen.getByText('失败')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '立即执行 Nightly cleanup' }))
    const runDialog = screen.getByRole('dialog', { name: '立即执行计划任务' })
    fireEvent.click(within(runDialog).getByRole('button', { name: '确认执行' }))
    await waitFor(() => expect(api.runScheduledTask).toHaveBeenCalledWith(9))
    const detailDialog = await screen.findByRole('dialog', { name: '执行详情' })
    fireEvent.click(within(detailDialog).getByRole('button', { name: '关闭' }))

    fireEvent.click(screen.getByRole('button', { name: '删除 Nightly cleanup' }))
    const deleteDialog = screen.getByRole('dialog', { name: '删除计划任务' })
    expect(within(deleteDialog).getByText('此操作无法撤销。')).toBeInTheDocument()
    fireEvent.click(within(deleteDialog).getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(api.deleteScheduledTask).toHaveBeenCalledWith(9))
    expect(await screen.findByText('计划任务删除成功')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新当前页' })).toHaveFocus())
  })
})
