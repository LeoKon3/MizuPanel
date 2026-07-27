import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from 'react'
import { CalendarClock, Clock3, Code2, Eye, Loader2, Pencil, Play, Plus, RefreshCw, Search, Trash2 } from 'lucide-react'

import { createAutomationScript, createScheduledTask, deleteAutomationScript, deleteScheduledTask, getAutomationRuns, getAutomationScripts, getScheduledTasks, runAutomationScript, runScheduledTask, toggleScheduledTask, updateAutomationScript, updateScheduledTask } from '../api/client'
import { NotificationChannelsEditor } from '../components/NotificationChannelsEditor'
import { TaskDialog } from '../components/TaskDialog'
import { TaskNodeSelector } from '../components/TaskNodeSelector'
import { formatAutomationDate, isTerminalRunStatus, runDuration, TaskRunDetailModal, TaskStatusBadge } from '../components/TaskRunDetailModal'
import { Toast } from '../components/Toast'
import type { AutomationNotificationPolicy, AutomationRun, AutomationRunsQuery, AutomationRunStatus, AutomationScript, AutomationScriptInput, Node, NotificationChannel, ScheduledTask, ScheduledTaskInput } from '../types'

type TasksPageProps = {
  nodes: Node[]
}

type TasksTab = 'tasks' | 'scripts' | 'runs'
type TimeRange = '24h' | '7d' | '30d' | 'all'
type RunsLoadMode = 'replace' | 'merge' | 'append'
type ScriptDraft = { name: string, description: string, content: string, timeoutSeconds: string }
type TaskDraft = {
  name: string
  scriptID: string
  nodeIDs: Set<string>
  cronExpression: string
  timezone: string
  timeoutSeconds: string
  enabled: boolean
  notificationPolicy: AutomationNotificationPolicy
  notificationChannels: NotificationChannel[]
}

const scriptContentLimit = 128 * 1024
const tabs: Array<{ id: TasksTab, label: string, icon: typeof CalendarClock }> = [
  { id: 'tasks', label: '计划任务', icon: CalendarClock },
  { id: 'scripts', label: '脚本库', icon: Code2 },
  { id: 'runs', label: '执行记录', icon: Clock3 }
]

const timeRanges: Array<{ value: TimeRange, label: string, hours?: number }> = [
  { value: '24h', label: '最近 24 小时', hours: 24 },
  { value: '7d', label: '最近 7 天', hours: 24 * 7 },
  { value: '30d', label: '最近 30 天', hours: 24 * 30 },
  { value: 'all', label: '全部时间' }
]

export function TasksPage({ nodes }: TasksPageProps) {
  const [activeTab, setActiveTab] = useState<TasksTab>('tasks')
  const [scripts, setScripts] = useState<AutomationScript[]>([])
  const [tasks, setTasks] = useState<ScheduledTask[]>([])
  const [runs, setRuns] = useState<AutomationRun[]>([])
  const [scriptsLoading, setScriptsLoading] = useState(true)
  const [tasksLoading, setTasksLoading] = useState(true)
  const [runsLoading, setRunsLoading] = useState(false)
  const [runsLoadingMore, setRunsLoadingMore] = useState(false)
  const [scriptsError, setScriptsError] = useState<string>()
  const [tasksError, setTasksError] = useState<string>()
  const [runsError, setRunsError] = useState<string>()
  const [nextBeforeID, setNextBeforeID] = useState<number | null>(null)
  const [scriptSearch, setScriptSearch] = useState('')
  const [taskSearch, setTaskSearch] = useState('')
  const [taskEnabledFilter, setTaskEnabledFilter] = useState<'all' | 'enabled' | 'paused'>('all')
  const [runStatus, setRunStatus] = useState<AutomationRunStatus | ''>('')
  const [runTrigger, setRunTrigger] = useState<'manual' | 'scheduled' | ''>('')
  const [runNodeID, setRunNodeID] = useState('')
  const [runScriptID, setRunScriptID] = useState('')
  const [runTaskID, setRunTaskID] = useState('')
  const [runTimeRange, setRunTimeRange] = useState<TimeRange>('7d')
  const [toast, setToast] = useState<{ message: string, type: 'success' | 'error' } | null>(null)
  const [scriptForm, setScriptForm] = useState<{ mode: 'create' | 'edit', script?: AutomationScript, draft: ScriptDraft }>()
  const [taskForm, setTaskForm] = useState<{ mode: 'create' | 'edit', task?: ScheduledTask, draft: TaskDraft }>()
  const [scriptRun, setScriptRun] = useState<{ script: AutomationScript, nodeIDs: Set<string> }>()
  const [pendingScriptDelete, setPendingScriptDelete] = useState<AutomationScript>()
  const [pendingTaskDelete, setPendingTaskDelete] = useState<ScheduledTask>()
  const [pendingTaskRun, setPendingTaskRun] = useState<ScheduledTask>()
  const [detailRunID, setDetailRunID] = useState<number>()
  const [busyAction, setBusyAction] = useState<string>()

  const scriptsRequestSequence = useRef(0)
  const tasksRequestSequence = useRef(0)
  const runsQuerySequence = useRef(0)
  const runsHeadRequestSequence = useRef(0)
  const runsPageRequestSequence = useRef(0)
  const scriptsController = useRef<AbortController | null>(null)
  const tasksController = useRef<AbortController | null>(null)
  const runsHeadController = useRef<AbortController | null>(null)
  const runsPageController = useRef<AbortController | null>(null)
  const runsTailLoaded = useRef(false)
  const runRangeStart = useRef('')
  const pageFallbackRef = useRef<HTMLButtonElement | null>(null)
  const modalTriggerRef = useRef<HTMLElement | null>(null)
  const detailTriggerRef = useRef<HTMLElement | null>(null)

  const loadScripts = useCallback(async (showLoading = false) => {
    const requestID = ++scriptsRequestSequence.current
    scriptsController.current?.abort()
    const controller = new AbortController()
    scriptsController.current = controller
    if (showLoading) setScriptsLoading(true)
    try {
      const response = await getAutomationScripts(controller.signal)
      if (requestID !== scriptsRequestSequence.current) return
      setScripts(response.scripts || [])
      setScriptsError(undefined)
    } catch (requestError: unknown) {
      if (requestID !== scriptsRequestSequence.current || isAbortError(requestError)) return
      setScriptsError(errorMessage(requestError))
    } finally {
      if (requestID === scriptsRequestSequence.current) setScriptsLoading(false)
    }
  }, [])

  const loadTasks = useCallback(async (showLoading = false) => {
    const requestID = ++tasksRequestSequence.current
    tasksController.current?.abort()
    const controller = new AbortController()
    tasksController.current = controller
    if (showLoading) setTasksLoading(true)
    try {
      const response = await getScheduledTasks(controller.signal)
      if (requestID !== tasksRequestSequence.current) return
      setTasks(response.tasks || [])
      setTasksError(undefined)
    } catch (requestError: unknown) {
      if (requestID !== tasksRequestSequence.current || isAbortError(requestError)) return
      setTasksError(errorMessage(requestError))
    } finally {
      if (requestID === tasksRequestSequence.current) setTasksLoading(false)
    }
  }, [])

  const runQuery = useCallback((updateRangeStart: boolean, beforeID?: number): AutomationRunsQuery => {
    const selectedRange = timeRanges.find((range) => range.value === runTimeRange)
    if (updateRangeStart) runRangeStart.current = selectedRange?.hours ? new Date(Date.now() - selectedRange.hours * 60 * 60 * 1000).toISOString() : ''
    return {
      before_id: beforeID,
      limit: 50,
      status: runStatus || undefined,
      trigger: runTrigger || undefined,
      node_id: runNodeID || undefined,
      script_id: runScriptID ? Number(runScriptID) : undefined,
      task_id: runTaskID ? Number(runTaskID) : undefined,
      from: runRangeStart.current || undefined
    }
  }, [runNodeID, runScriptID, runStatus, runTaskID, runTimeRange, runTrigger])

  const loadRuns = useCallback(async (mode: RunsLoadMode, beforeID?: number) => {
    const replacing = mode === 'replace'
    const appending = mode === 'append'
    const queryID = replacing ? ++runsQuerySequence.current : runsQuerySequence.current
    const requestID = appending ? ++runsPageRequestSequence.current : ++runsHeadRequestSequence.current
    const controller = new AbortController()
    if (appending) {
      runsPageController.current?.abort()
      runsPageController.current = controller
      setRunsLoadingMore(true)
    } else {
      runsHeadController.current?.abort()
      runsHeadController.current = controller
    }
    if (replacing) {
      runsPageRequestSequence.current += 1
      runsPageController.current?.abort()
      runsTailLoaded.current = false
      setRuns([])
      setRunsError(undefined)
      setRunsLoading(true)
      setRunsLoadingMore(false)
      setNextBeforeID(null)
    }
    const isCurrent = () => queryID === runsQuerySequence.current && requestID === (appending ? runsPageRequestSequence.current : runsHeadRequestSequence.current)
    try {
      const response = await getAutomationRuns(runQuery(replacing, beforeID), controller.signal)
      if (!isCurrent()) return
      const received = response.runs || []
      const responseCursor = typeof response.next_before_id === 'number' ? response.next_before_id : null
      if (replacing) {
        setRuns(mergeUniqueRuns(received))
        setNextBeforeID(responseCursor)
      } else if (appending) {
        runsTailLoaded.current = true
        setRuns((current) => mergeUniqueRuns(current, received))
        setNextBeforeID(responseCursor)
      } else if (responseCursor === null) {
        runsPageRequestSequence.current += 1
        runsPageController.current?.abort()
        runsTailLoaded.current = false
        setRuns(mergeUniqueRuns(received))
        setNextBeforeID(null)
        setRunsLoadingMore(false)
      } else {
        setRuns((current) => mergeRunHead(current, received, responseCursor))
        if (!runsTailLoaded.current) setNextBeforeID(responseCursor)
      }
      setRunsError(undefined)
    } catch (requestError: unknown) {
      if (!isCurrent() || isAbortError(requestError)) return
      setRunsError(errorMessage(requestError))
    } finally {
      if (!isCurrent()) return
      if (appending) setRunsLoadingMore(false)
      else setRunsLoading(false)
    }
  }, [runQuery])

  useEffect(() => {
    void loadScripts(true)
    void loadTasks(true)
    return () => {
      scriptsRequestSequence.current += 1
      tasksRequestSequence.current += 1
      runsQuerySequence.current += 1
      runsHeadRequestSequence.current += 1
      runsPageRequestSequence.current += 1
      scriptsController.current?.abort()
      tasksController.current?.abort()
      runsHeadController.current?.abort()
      runsPageController.current?.abort()
    }
  }, [loadScripts, loadTasks])

  useEffect(() => {
    if (activeTab === 'runs') void loadRuns('replace')
  }, [activeTab, loadRuns])

  const shouldPollRuns = activeTab === 'runs' && runs.some((run) => !isTerminalRunStatus(run.status))

  useEffect(() => {
    if (!shouldPollRuns) return undefined
    let cancelled = false
    let timer: number | undefined
    const schedule = () => {
      timer = window.setTimeout(async () => {
        await loadRuns('merge')
        if (!cancelled) schedule()
      }, 3000)
    }
    schedule()
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [loadRuns, shouldPollRuns])

  const filteredScripts = useMemo(() => {
    const query = scriptSearch.trim().toLowerCase()
    if (!query) return scripts
    return scripts.filter((script) => [script.name, script.description].some((value) => value.toLowerCase().includes(query)))
  }, [scriptSearch, scripts])

  const filteredTasks = useMemo(() => {
    const query = taskSearch.trim().toLowerCase()
    return tasks.filter((task) => {
      if (taskEnabledFilter === 'enabled' && !task.enabled) return false
      if (taskEnabledFilter === 'paused' && task.enabled) return false
      if (!query) return true
      return [task.name, task.script_name, task.cron_expression, task.timezone].some((value) => value.toLowerCase().includes(query))
    })
  }, [taskEnabledFilter, taskSearch, tasks])

  const showToast = useCallback((message: string, type: 'success' | 'error') => setToast({ message, type }), [])
  const closeToast = useCallback(() => setToast(null), [])

  const openScriptForm = (trigger: HTMLElement, script?: AutomationScript) => {
    modalTriggerRef.current = trigger
    setScriptForm({
      mode: script ? 'edit' : 'create',
      script,
      draft: script
        ? { name: script.name, description: script.description, content: script.content, timeoutSeconds: String(script.timeout_seconds) }
        : { name: '', description: '', content: '', timeoutSeconds: '300' }
    })
  }

  const openTaskForm = (trigger: HTMLElement, task?: ScheduledTask) => {
    if (!task && scripts.length === 0) {
      showToast('计划任务创建失败: 请先创建脚本', 'error')
      return
    }
    modalTriggerRef.current = trigger
    const defaultScript = task ? scripts.find((script) => script.id === task.script_id) : scripts[0]
    setTaskForm({
      mode: task ? 'edit' : 'create',
      task,
      draft: task
        ? {
            name: task.name,
            scriptID: String(task.script_id),
            nodeIDs: new Set(task.node_ids || []),
            cronExpression: task.cron_expression,
            timezone: task.timezone,
            timeoutSeconds: String(task.timeout_seconds),
            enabled: task.enabled,
            notificationPolicy: task.notification_policy,
            notificationChannels: task.notification_channels || []
          }
        : {
            name: '',
            scriptID: defaultScript ? String(defaultScript.id) : '',
            nodeIDs: new Set(),
            cronExpression: '0 * * * *',
            timezone: defaultTimezone(),
            timeoutSeconds: String(defaultScript?.timeout_seconds || 300),
            enabled: true,
            notificationPolicy: 'failure',
            notificationChannels: []
          }
    })
  }

  const saveScript = async () => {
    if (!scriptForm) return
    const validation = validateScript(scriptForm.draft)
    if (validation.error) {
      setScriptForm({ ...scriptForm, draft: scriptForm.draft })
      showToast(`脚本保存失败: ${validation.error}`, 'error')
      return
    }
    const action = scriptForm.mode === 'create' ? '创建' : '保存'
    setBusyAction('save-script')
    try {
      let saved: AutomationScript
      if (scriptForm.mode === 'create') saved = await createAutomationScript(validation.input)
      else if (scriptForm.script) saved = await updateAutomationScript(scriptForm.script.id, validation.input)
      else return
      setScripts((current) => upsertByID(current, saved))
      setScriptForm(undefined)
      showToast(`脚本${action}成功`, 'success')
      await loadScripts(false)
    } catch (requestError) {
      showToast(`脚本${action}失败: ${errorMessage(requestError)}`, 'error')
    } finally {
      setBusyAction(undefined)
    }
  }

  const saveTask = async () => {
    if (!taskForm) return
    const validation = validateTask(taskForm.draft)
    if (validation.error) {
      showToast(`计划任务保存失败: ${validation.error}`, 'error')
      return
    }
    const action = taskForm.mode === 'create' ? '创建' : '保存'
    setBusyAction('save-task')
    try {
      let saved: ScheduledTask
      if (taskForm.mode === 'create') saved = await createScheduledTask(validation.input)
      else if (taskForm.task) saved = await updateScheduledTask(taskForm.task.id, validation.input)
      else return
      setTasks((current) => upsertByID(current, saved))
      setTaskForm(undefined)
      showToast(`计划任务${action}成功`, 'success')
      await loadTasks(false)
    } catch (requestError) {
      showToast(`计划任务${action}失败: ${errorMessage(requestError)}`, 'error')
    } finally {
      setBusyAction(undefined)
    }
  }

  const confirmScriptDelete = async () => {
    if (!pendingScriptDelete) return
    setBusyAction(`delete-script-${pendingScriptDelete.id}`)
    try {
      await deleteAutomationScript(pendingScriptDelete.id)
      setScripts((current) => current.filter((script) => script.id !== pendingScriptDelete.id))
      setPendingScriptDelete(undefined)
      showToast('脚本删除成功', 'success')
      await loadScripts(false)
    } catch (requestError) {
      showToast(`脚本删除失败: ${errorMessage(requestError)}`, 'error')
    } finally {
      setBusyAction(undefined)
    }
  }

  const confirmTaskDelete = async () => {
    if (!pendingTaskDelete) return
    setBusyAction(`delete-task-${pendingTaskDelete.id}`)
    try {
      await deleteScheduledTask(pendingTaskDelete.id)
      setTasks((current) => current.filter((task) => task.id !== pendingTaskDelete.id))
      setPendingTaskDelete(undefined)
      showToast('计划任务删除成功', 'success')
      await loadTasks(false)
    } catch (requestError) {
      showToast(`计划任务删除失败: ${errorMessage(requestError)}`, 'error')
    } finally {
      setBusyAction(undefined)
    }
  }

  const toggleTask = async (task: ScheduledTask) => {
    const enabled = !task.enabled
    setBusyAction(`toggle-task-${task.id}`)
    try {
      const updated = await toggleScheduledTask(task.id, enabled)
      setTasks((current) => current.map((item) => item.id === task.id ? updated : item))
      showToast(`计划任务${enabled ? '启用' : '暂停'}成功`, 'success')
    } catch (requestError) {
      showToast(`计划任务${enabled ? '启用' : '暂停'}失败: ${errorMessage(requestError)}`, 'error')
    } finally {
      setBusyAction(undefined)
    }
  }

  const confirmScriptRun = async () => {
    if (!scriptRun || scriptRun.nodeIDs.size === 0) return
    setBusyAction(`run-script-${scriptRun.script.id}`)
    try {
      const run = await runAutomationScript(scriptRun.script.id, [...scriptRun.nodeIDs])
      setScriptRun(undefined)
      setDetailRunID(run.id)
      showToast('脚本执行请求已受理', 'success')
    } catch (requestError) {
      showToast(`脚本执行失败: ${errorMessage(requestError)}`, 'error')
    } finally {
      setBusyAction(undefined)
    }
  }

  const confirmTaskRun = async () => {
    if (!pendingTaskRun) return
    setBusyAction(`run-task-${pendingTaskRun.id}`)
    try {
      const run = await runScheduledTask(pendingTaskRun.id)
      setPendingTaskRun(undefined)
      setDetailRunID(run.id)
      showToast('计划任务执行请求已受理', 'success')
    } catch (requestError) {
      showToast(`计划任务执行失败: ${errorMessage(requestError)}`, 'error')
    } finally {
      setBusyAction(undefined)
    }
  }

  const openRunDetail = (runID: number, trigger: HTMLElement) => {
    detailTriggerRef.current = trigger
    setDetailRunID(runID)
  }

  const closeRunDetail = useCallback(() => setDetailRunID(undefined), [])
  const updateRunFromDetail = useCallback((updated: AutomationRun) => {
    setRuns((current) => {
      const index = current.findIndex((run) => run.id === updated.id)
      if (index < 0) return current
      const next = [...current]
      next[index] = updated
      return next
    })
    if (activeTab === 'runs' && isTerminalRunStatus(updated.status)) void loadRuns('merge')
  }, [activeTab, loadRuns])

  const refreshActive = () => {
    if (activeTab === 'scripts') void loadScripts(false)
    else if (activeTab === 'tasks') void Promise.all([loadTasks(false), loadScripts(false)])
    else void loadRuns('merge')
  }

  return (
    <div className="min-w-0 space-y-5">
      {toast ? <Toast message={toast.message} type={toast.type} onClose={closeToast} /> : null}

      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1 className="text-2xl font-black text-foreground">任务中心</h1>
          <p className="mt-1 text-sm font-semibold text-muted-foreground">统一管理 Server 端脚本、Cron 计划和每台 Agent 的有限执行结果。</p>
        </div>
        <button ref={pageFallbackRef} type="button" onClick={refreshActive} className="soft-button inline-flex min-h-10 items-center justify-center gap-2 border border-border bg-card px-4 text-sm font-black text-foreground hover:border-primary/40 focus:outline-none focus:ring-4 focus:ring-primary/20">
          <RefreshCw size={16} aria-hidden="true" />刷新当前页
        </button>
      </div>

      <div role="tablist" aria-label="任务中心视图" className="soft-toolbar flex max-w-full gap-1 overflow-x-auto p-1">
        {tabs.map((tab) => {
          const Icon = tab.icon
          return (
            <button
              key={tab.id}
              id={`tasks-tab-${tab.id}`}
              role="tab"
              aria-selected={activeTab === tab.id}
              aria-controls={`tasks-panel-${tab.id}`}
              type="button"
              onClick={() => setActiveTab(tab.id)}
              className={`soft-button inline-flex min-h-10 shrink-0 items-center gap-2 px-4 text-sm font-black focus:outline-none focus:ring-4 focus:ring-primary/20 ${activeTab === tab.id ? 'bg-primary text-primary-foreground shadow-sm' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
            >
              <Icon size={15} aria-hidden="true" />{tab.label}
            </button>
          )
        })}
      </div>

      {activeTab === 'tasks' ? (
        <section id="tasks-panel-tasks" role="tabpanel" aria-labelledby="tasks-tab-tasks" className="min-w-0 space-y-4">
          <TaskToolbar search={taskSearch} onSearch={setTaskSearch} enabledFilter={taskEnabledFilter} onEnabledFilter={setTaskEnabledFilter} onCreate={(trigger) => openTaskForm(trigger)} />
          <TasksTable
            tasks={filteredTasks}
            nodes={nodes}
            loading={tasksLoading}
            error={tasksError}
            busyAction={busyAction}
            onRun={(task, trigger) => { modalTriggerRef.current = trigger; detailTriggerRef.current = trigger; setPendingTaskRun(task) }}
            onToggle={(task) => void toggleTask(task)}
            onEdit={(task, trigger) => openTaskForm(trigger, task)}
            onDelete={(task, trigger) => { modalTriggerRef.current = trigger; setPendingTaskDelete(task) }}
            onCreate={(trigger) => openTaskForm(trigger)}
          />
        </section>
      ) : null}

      {activeTab === 'scripts' ? (
        <section id="tasks-panel-scripts" role="tabpanel" aria-labelledby="tasks-tab-scripts" className="min-w-0 space-y-4">
          <ScriptToolbar search={scriptSearch} onSearch={setScriptSearch} onCreate={(trigger) => openScriptForm(trigger)} />
          <ScriptsTable
            scripts={filteredScripts}
            loading={scriptsLoading}
            error={scriptsError}
            onRun={(script, trigger) => { modalTriggerRef.current = trigger; detailTriggerRef.current = trigger; setScriptRun({ script, nodeIDs: new Set() }) }}
            onEdit={(script, trigger) => openScriptForm(trigger, script)}
            onDelete={(script, trigger) => { modalTriggerRef.current = trigger; setPendingScriptDelete(script) }}
            onCreate={(trigger) => openScriptForm(trigger)}
          />
        </section>
      ) : null}

      {activeTab === 'runs' ? (
        <section id="tasks-panel-runs" role="tabpanel" aria-labelledby="tasks-tab-runs" className="min-w-0 space-y-4">
          <RunsFilters
            nodes={nodes}
            scripts={scripts}
            tasks={tasks}
            status={runStatus}
            trigger={runTrigger}
            nodeID={runNodeID}
            scriptID={runScriptID}
            taskID={runTaskID}
            timeRange={runTimeRange}
            onStatus={setRunStatus}
            onTrigger={setRunTrigger}
            onNodeID={setRunNodeID}
            onScriptID={setRunScriptID}
            onTaskID={setRunTaskID}
            onTimeRange={setRunTimeRange}
          />
          <RunsTable runs={runs} loading={runsLoading} loadingMore={runsLoadingMore} error={runsError} nextBeforeID={nextBeforeID} onLoadMore={() => nextBeforeID !== null && void loadRuns('append', nextBeforeID)} onOpen={openRunDetail} />
        </section>
      ) : null}

      {scriptForm ? <ScriptFormModal form={scriptForm} setForm={setScriptForm} saving={busyAction === 'save-script'} returnFocusRef={modalTriggerRef} fallbackFocusRef={pageFallbackRef} onClose={() => busyAction !== 'save-script' && setScriptForm(undefined)} onSave={() => void saveScript()} /> : null}
      {taskForm ? <ScheduledTaskFormModal form={taskForm} setForm={setTaskForm} scripts={scripts} nodes={nodes} saving={busyAction === 'save-task'} returnFocusRef={modalTriggerRef} fallbackFocusRef={pageFallbackRef} onClose={() => busyAction !== 'save-task' && setTaskForm(undefined)} onSave={() => void saveTask()} /> : null}
      {scriptRun ? <ScriptRunModal value={scriptRun} nodes={nodes} running={busyAction === `run-script-${scriptRun.script.id}`} returnFocusRef={modalTriggerRef} fallbackFocusRef={pageFallbackRef} onChange={setScriptRun} onClose={() => !busyAction?.startsWith('run-script-') && setScriptRun(undefined)} onRun={() => void confirmScriptRun()} /> : null}
      {pendingScriptDelete ? <DeleteModal kind="脚本" name={pendingScriptDelete.name} detail="被计划任务引用的脚本无法删除，执行历史不会阻止删除。" deleting={busyAction === `delete-script-${pendingScriptDelete.id}`} returnFocusRef={modalTriggerRef} fallbackFocusRef={pageFallbackRef} onClose={() => !busyAction?.startsWith('delete-script-') && setPendingScriptDelete(undefined)} onConfirm={() => void confirmScriptDelete()} /> : null}
      {pendingTaskDelete ? <DeleteModal kind="计划任务" name={pendingTaskDelete.name} detail="删除后不会影响已经保存的执行记录。" deleting={busyAction === `delete-task-${pendingTaskDelete.id}`} returnFocusRef={modalTriggerRef} fallbackFocusRef={pageFallbackRef} onClose={() => !busyAction?.startsWith('delete-task-') && setPendingTaskDelete(undefined)} onConfirm={() => void confirmTaskDelete()} /> : null}
      {pendingTaskRun ? <RunTaskModal task={pendingTaskRun} running={busyAction === `run-task-${pendingTaskRun.id}`} returnFocusRef={modalTriggerRef} fallbackFocusRef={pageFallbackRef} onClose={() => !busyAction?.startsWith('run-task-') && setPendingTaskRun(undefined)} onConfirm={() => void confirmTaskRun()} /> : null}
      {detailRunID !== undefined ? <TaskRunDetailModal runID={detailRunID} returnFocusRef={detailTriggerRef} fallbackFocusRef={pageFallbackRef} onClose={closeRunDetail} onToast={showToast} onRunUpdated={updateRunFromDetail} /> : null}
    </div>
  )
}

function TaskToolbar({ search, onSearch, enabledFilter, onEnabledFilter, onCreate }: { search: string, onSearch: (value: string) => void, enabledFilter: 'all' | 'enabled' | 'paused', onEnabledFilter: (value: 'all' | 'enabled' | 'paused') => void, onCreate: (trigger: HTMLElement) => void }) {
  return (
    <div className="soft-toolbar flex flex-col gap-3 p-3 sm:flex-row sm:items-center">
      <SearchInput label="搜索计划任务" placeholder="搜索任务、脚本、Cron 或时区" value={search} onChange={onSearch} />
      <select aria-label="计划任务状态" value={enabledFilter} onChange={(event) => onEnabledFilter(event.target.value as typeof enabledFilter)} className="soft-input min-h-10 px-3 text-sm font-black">
        <option value="all">全部状态</option><option value="enabled">已启用</option><option value="paused">已暂停</option>
      </select>
      <button type="button" onClick={(event) => onCreate(event.currentTarget)} className="soft-button inline-flex min-h-10 shrink-0 items-center justify-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground"><Plus size={16} aria-hidden="true" />创建计划</button>
    </div>
  )
}

function ScriptToolbar({ search, onSearch, onCreate }: { search: string, onSearch: (value: string) => void, onCreate: (trigger: HTMLElement) => void }) {
  return (
    <div className="soft-toolbar flex flex-col gap-3 p-3 sm:flex-row sm:items-center">
      <SearchInput label="搜索脚本" placeholder="搜索脚本名称或说明" value={search} onChange={onSearch} />
      <button type="button" onClick={(event) => onCreate(event.currentTarget)} className="soft-button inline-flex min-h-10 shrink-0 items-center justify-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground"><Plus size={16} aria-hidden="true" />创建脚本</button>
    </div>
  )
}

function SearchInput({ label, placeholder, value, onChange }: { label: string, placeholder: string, value: string, onChange: (value: string) => void }) {
  return <div className="relative min-w-0 flex-1"><Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" /><input aria-label={label} value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} className="soft-input min-h-10 w-full pl-9 pr-3 text-sm font-bold placeholder:text-muted-foreground" /></div>
}

function TasksTable({ tasks, nodes, loading, error, busyAction, onRun, onToggle, onEdit, onDelete, onCreate }: { tasks: ScheduledTask[], nodes: Node[], loading: boolean, error?: string, busyAction?: string, onRun: (task: ScheduledTask, trigger: HTMLElement) => void, onToggle: (task: ScheduledTask) => void, onEdit: (task: ScheduledTask, trigger: HTMLElement) => void, onDelete: (task: ScheduledTask, trigger: HTMLElement) => void, onCreate: (trigger: HTMLElement) => void }) {
  if (loading) return <LoadingState label="正在加载计划任务..." />
  if (error) return <ErrorState label={`计划任务加载失败: ${error}`} />
  if (tasks.length === 0) return <EmptyState icon={<CalendarClock size={38} />} title="还没有计划任务" action="创建计划" onAction={onCreate} />
  const names = new Map(nodes.map((node) => [node.id, node.name || node.hostname || node.id]))
  return (
    <section className="soft-panel min-w-0 overflow-hidden" aria-label="计划任务列表">
      <div className="overflow-x-auto">
        <table className="soft-table min-w-[1120px] w-full text-left">
          <thead><tr><th className="px-4 py-3">任务</th><th className="px-3 py-3">状态</th><th className="px-3 py-3">脚本</th><th className="px-3 py-3">目标节点</th><th className="px-3 py-3">Cron / 时区</th><th className="px-3 py-3">下次执行</th><th className="px-3 py-3">最近执行</th><th className="px-3 py-3 text-right">操作</th></tr></thead>
          <tbody>{tasks.map((task) => (
            <tr key={task.id} className="align-top">
              <td className="px-4 py-3"><p className="max-w-48 truncate text-sm font-black text-foreground" title={task.name}>{task.name}</p><p className="mt-0.5 text-[11px] font-bold text-muted-foreground">#{task.id}</p></td>
              <td className="px-3 py-3"><button type="button" role="switch" aria-checked={task.enabled} aria-label={`${task.enabled ? '暂停' : '启用'}计划任务 ${task.name}`} onClick={() => onToggle(task)} disabled={busyAction === `toggle-task-${task.id}`} className={`relative h-7 w-12 rounded-full focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:opacity-50 ${task.enabled ? 'bg-success' : 'bg-muted'}`}><span className={`absolute left-1 top-1 h-5 w-5 rounded-full bg-white shadow transition-transform ${task.enabled ? 'translate-x-5' : ''}`} /></button><p className="mt-1 text-[10px] font-black text-muted-foreground">{task.enabled ? '已启用' : '已暂停'}</p></td>
              <td className="px-3 py-3"><p className="max-w-44 truncate text-sm font-bold text-foreground" title={task.script_name}>{task.script_name}</p><p className="mt-0.5 text-[11px] font-semibold text-muted-foreground">rev {task.script_revision}</p></td>
              <td className="max-w-56 px-3 py-3"><p className="truncate text-xs font-bold text-foreground" title={task.node_ids.map((id) => names.get(id) || id).join(', ')}>{task.node_ids.slice(0, 2).map((id) => names.get(id) || id).join('、') || '—'}{task.node_ids.length > 2 ? ` +${task.node_ids.length - 2}` : ''}</p><p className="mt-0.5 text-[11px] font-semibold text-muted-foreground">{task.node_ids.length} 台</p></td>
              <td className="px-3 py-3"><p className="whitespace-nowrap font-mono text-xs font-black text-foreground">{task.cron_expression}</p><p className="mt-0.5 text-[11px] font-semibold text-muted-foreground">{task.timezone}</p></td>
              <td className="whitespace-nowrap px-3 py-3 text-xs font-bold text-muted-foreground">{task.enabled ? formatAutomationDate(task.next_run_at) : '已暂停'}</td>
              <td className="whitespace-nowrap px-3 py-3"><div>{task.latest_run_status ? <TaskStatusBadge status={task.latest_run_status} compact /> : <span className="text-xs font-bold text-muted-foreground">暂无结果</span>}</div><p className="mt-1 text-[11px] font-semibold text-muted-foreground">{formatAutomationDate(task.latest_run_at ?? task.last_scheduled_at)}</p></td>
              <td className="px-3 py-3"><div className="flex justify-end gap-1"><IconAction label={`立即执行 ${task.name}`} title="立即执行" icon={<Play size={14} />} onClick={(trigger) => onRun(task, trigger)} /><IconAction label={`编辑 ${task.name}`} title="编辑" icon={<Pencil size={14} />} onClick={(trigger) => onEdit(task, trigger)} /><IconAction label={`删除 ${task.name}`} title="删除" icon={<Trash2 size={14} />} danger onClick={(trigger) => onDelete(task, trigger)} /></div></td>
            </tr>
          ))}</tbody>
        </table>
      </div>
    </section>
  )
}

function ScriptsTable({ scripts, loading, error, onRun, onEdit, onDelete, onCreate }: { scripts: AutomationScript[], loading: boolean, error?: string, onRun: (script: AutomationScript, trigger: HTMLElement) => void, onEdit: (script: AutomationScript, trigger: HTMLElement) => void, onDelete: (script: AutomationScript, trigger: HTMLElement) => void, onCreate: (trigger: HTMLElement) => void }) {
  if (loading) return <LoadingState label="正在加载脚本库..." />
  if (error) return <ErrorState label={`脚本库加载失败: ${error}`} />
  if (scripts.length === 0) return <EmptyState icon={<Code2 size={38} />} title="还没有脚本" action="创建脚本" onAction={onCreate} />
  return (
    <section className="soft-panel min-w-0 overflow-hidden" aria-label="脚本列表">
      <div className="overflow-x-auto">
        <table className="soft-table min-w-[820px] w-full text-left">
          <thead><tr><th className="px-4 py-3">脚本</th><th className="px-3 py-3">说明</th><th className="px-3 py-3">默认超时</th><th className="px-3 py-3">版本</th><th className="px-3 py-3">更新时间</th><th className="px-3 py-3 text-right">操作</th></tr></thead>
          <tbody>{scripts.map((script) => (
            <tr key={script.id}>
              <td className="px-4 py-3"><p className="max-w-52 truncate text-sm font-black text-foreground" title={script.name}>{script.name}</p><p className="mt-0.5 text-[11px] font-bold text-muted-foreground">#{script.id}</p></td>
              <td className="max-w-sm px-3 py-3"><p className="truncate text-xs font-bold text-muted-foreground" title={script.description}>{script.description || '—'}</p></td>
              <td className="whitespace-nowrap px-3 py-3 text-xs font-black text-foreground">{script.timeout_seconds} 秒</td>
              <td className="px-3 py-3 font-mono text-xs font-bold text-muted-foreground">rev {script.revision}</td>
              <td className="whitespace-nowrap px-3 py-3 text-xs font-bold text-muted-foreground">{formatAutomationDate(script.updated_at)}</td>
              <td className="px-3 py-3"><div className="flex justify-end gap-1"><IconAction label={`运行 ${script.name}`} title="运行" icon={<Play size={14} />} onClick={(trigger) => onRun(script, trigger)} /><IconAction label={`编辑 ${script.name}`} title="编辑" icon={<Pencil size={14} />} onClick={(trigger) => onEdit(script, trigger)} /><IconAction label={`删除 ${script.name}`} title="删除" icon={<Trash2 size={14} />} danger onClick={(trigger) => onDelete(script, trigger)} /></div></td>
            </tr>
          ))}</tbody>
        </table>
      </div>
    </section>
  )
}

function RunsFilters({ nodes, scripts, tasks, status, trigger, nodeID, scriptID, taskID, timeRange, onStatus, onTrigger, onNodeID, onScriptID, onTaskID, onTimeRange }: { nodes: Node[], scripts: AutomationScript[], tasks: ScheduledTask[], status: AutomationRunStatus | '', trigger: 'manual' | 'scheduled' | '', nodeID: string, scriptID: string, taskID: string, timeRange: TimeRange, onStatus: (value: AutomationRunStatus | '') => void, onTrigger: (value: 'manual' | 'scheduled' | '') => void, onNodeID: (value: string) => void, onScriptID: (value: string) => void, onTaskID: (value: string) => void, onTimeRange: (value: TimeRange) => void }) {
  return (
    <section className="soft-toolbar grid gap-3 p-3 sm:grid-cols-2 xl:grid-cols-6" aria-label="执行记录筛选">
      <FilterSelect label="执行时间" value={timeRange} onChange={(value) => onTimeRange(value as TimeRange)} options={timeRanges.map((item) => [item.value, item.label])} />
      <FilterSelect label="批次状态" value={status} onChange={(value) => onStatus(value as AutomationRunStatus | '')} options={[['', '全部状态'], ['queued', '排队中'], ['running', '执行中'], ['success', '成功'], ['partial', '部分失败'], ['failed', '失败'], ['skipped', '已跳过'], ['interrupted', '已中断']]} />
      <FilterSelect label="触发来源" value={trigger} onChange={(value) => onTrigger(value as typeof trigger)} options={[['', '全部来源'], ['manual', '手动触发'], ['scheduled', '计划触发']]} />
      <FilterSelect label="节点" value={nodeID} onChange={onNodeID} options={[['', '全部节点'], ...nodes.map((node) => [node.id, node.name || node.hostname || node.id] as [string, string])]} />
      <FilterSelect label="脚本" value={scriptID} onChange={onScriptID} options={[['', '全部脚本'], ...scripts.map((script) => [String(script.id), script.name] as [string, string])]} />
      <FilterSelect label="计划任务" value={taskID} onChange={onTaskID} options={[['', '全部计划'], ...tasks.map((task) => [String(task.id), task.name] as [string, string])]} />
    </section>
  )
}

function FilterSelect({ label, value, options, onChange }: { label: string, value: string, options: Array<readonly [string, string]>, onChange: (value: string) => void }) {
  return <label className="min-w-0 text-xs font-black text-muted-foreground">{label}<select aria-label={label} value={value} onChange={(event) => onChange(event.target.value)} className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold text-foreground">{options.map(([optionValue, optionLabel]) => <option key={optionValue} value={optionValue}>{optionLabel}</option>)}</select></label>
}

function RunsTable({ runs, loading, loadingMore, error, nextBeforeID, onLoadMore, onOpen }: { runs: AutomationRun[], loading: boolean, loadingMore: boolean, error?: string, nextBeforeID: number | null, onLoadMore: () => void, onOpen: (runID: number, trigger: HTMLElement) => void }) {
  if (loading) return <LoadingState label="正在加载执行记录..." />
  if (error && runs.length === 0) return <ErrorState label={`执行记录加载失败: ${error}`} />
  if (runs.length === 0) return <EmptyState icon={<Clock3 size={38} />} title="暂无执行记录" />
  return (
    <section className="soft-panel min-w-0 overflow-hidden" aria-label="执行记录列表">
      {error ? <div role="alert" className="break-words border-b border-danger/25 bg-danger/5 px-4 py-3 text-sm font-black text-danger [overflow-wrap:anywhere]">执行记录刷新失败: {error}</div> : null}
      <div className="overflow-x-auto">
        <table className="soft-table min-w-[1050px] w-full text-left">
          <thead><tr><th className="px-4 py-3">时间</th><th className="px-3 py-3">状态</th><th className="px-3 py-3">来源</th><th className="px-3 py-3">脚本 / 计划</th><th className="px-3 py-3">进度</th><th className="px-3 py-3">结果</th><th className="px-3 py-3">耗时</th><th className="px-3 py-3 text-right">详情</th></tr></thead>
          <tbody>{runs.map((run) => (
            <tr key={run.id}>
              <td className="whitespace-nowrap px-4 py-3"><p className="text-xs font-bold text-foreground">{formatAutomationDate(run.created_at)}</p><p className="mt-0.5 text-[11px] font-semibold text-muted-foreground">#{run.id}</p></td>
              <td className="px-3 py-3"><TaskStatusBadge status={run.status} /></td>
              <td className="px-3 py-3 text-xs font-black text-muted-foreground">{run.trigger === 'scheduled' ? '计划触发' : '手动触发'}</td>
              <td className="max-w-64 px-3 py-3"><p className="truncate text-sm font-black text-foreground" title={run.script_name}>{run.script_name}</p><p className="mt-0.5 truncate text-[11px] font-semibold text-muted-foreground">{run.task_name || `脚本 rev ${run.script_revision}`}</p></td>
              <td className="whitespace-nowrap px-3 py-3 text-xs font-black text-foreground">{run.completed_targets}/{run.total_targets}</td>
              <td className="whitespace-nowrap px-3 py-3 text-xs font-bold text-muted-foreground"><span className="text-success">{run.success_targets} 成功</span> · <span className={run.failed_targets > 0 ? 'text-danger' : ''}>{run.failed_targets} 失败</span></td>
              <td className="whitespace-nowrap px-3 py-3 text-xs font-bold text-muted-foreground">{runDuration(run.started_at, run.completed_at)}</td>
              <td className="px-3 py-3 text-right"><IconAction label={`查看执行 ${run.id} 详情`} title="查看详情" icon={<Eye size={14} />} onClick={(trigger) => onOpen(run.id, trigger)} /></td>
            </tr>
          ))}</tbody>
        </table>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-4 py-3"><p className="text-xs font-black text-muted-foreground">已加载 {runs.length} 个批次</p>{nextBeforeID !== null ? <button type="button" onClick={onLoadMore} disabled={loadingMore} className="soft-button min-h-9 border border-border bg-card px-4 text-xs font-black text-foreground disabled:opacity-50">{loadingMore ? '正在加载...' : '加载更多'}</button> : <span className="text-xs font-black text-muted-foreground">已到达当前范围末尾</span>}</div>
    </section>
  )
}

function IconAction({ label, title, icon, danger = false, onClick }: { label: string, title: string, icon: React.ReactNode, danger?: boolean, onClick: (trigger: HTMLElement) => void }) {
  return <button type="button" aria-label={label} title={title} onClick={(event) => onClick(event.currentTarget)} className={`soft-button inline-flex h-8 w-8 items-center justify-center border focus:outline-none focus:ring-4 focus:ring-primary/20 ${danger ? 'border-danger/25 bg-danger/5 text-danger' : 'border-border bg-card text-muted-foreground hover:text-foreground'}`}>{icon}</button>
}

function ScriptFormModal({ form, setForm, saving, returnFocusRef, fallbackFocusRef, onClose, onSave }: { form: { mode: 'create' | 'edit', script?: AutomationScript, draft: ScriptDraft }, setForm: (value: { mode: 'create' | 'edit', script?: AutomationScript, draft: ScriptDraft } | undefined) => void, saving: boolean, returnFocusRef: RefObject<HTMLElement | null>, fallbackFocusRef: RefObject<HTMLElement | null>, onClose: () => void, onSave: () => void }) {
  const nameRef = useRef<HTMLInputElement | null>(null)
  const update = <Key extends keyof ScriptDraft>(key: Key, value: ScriptDraft[Key]) => setForm({ ...form, draft: { ...form.draft, [key]: value } })
  const contentBytes = new TextEncoder().encode(form.draft.content).length
  return (
    <TaskDialog ariaLabel={form.mode === 'create' ? '创建脚本' : '编辑脚本'} title={form.mode === 'create' ? '创建脚本' : `编辑 ${form.script?.name || '脚本'}`} description="脚本将由 Agent 使用固定 /bin/sh 执行。" size="lg" initialFocusRef={nameRef} returnFocusRef={returnFocusRef} fallbackFocusRef={fallbackFocusRef} closeDisabled={saving} onClose={onClose} footer={<ModalActions busy={saving} busyLabel="保存中..." confirmLabel="保存" onClose={onClose} onConfirm={onSave} />}>
      <div className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_180px]">
          <FormField label="脚本名称"><input ref={nameRef} aria-label="脚本名称" value={form.draft.name} onChange={(event) => update('name', event.target.value)} maxLength={120} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold" /></FormField>
          <FormField label="默认超时（秒）"><input aria-label="脚本默认超时" type="number" min={1} max={1800} value={form.draft.timeoutSeconds} onChange={(event) => update('timeoutSeconds', event.target.value)} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold" /></FormField>
        </div>
        <FormField label="说明"><input aria-label="脚本说明" value={form.draft.description} onChange={(event) => update('description', event.target.value)} maxLength={500} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold" /></FormField>
        <FormField label="Shell 内容">
          <textarea aria-label="Shell 内容" value={form.draft.content} onChange={(event) => update('content', event.target.value)} disabled={saving} spellCheck={false} className="soft-input min-h-72 w-full resize-y px-4 py-3 font-mono text-xs leading-5" />
          <span className={`mt-1 block text-right text-[11px] font-bold ${contentBytes > scriptContentLimit ? 'text-danger' : 'text-muted-foreground'}`}>{contentBytes.toLocaleString()} / {scriptContentLimit.toLocaleString()} bytes</span>
        </FormField>
      </div>
    </TaskDialog>
  )
}

function ScheduledTaskFormModal({ form, setForm, scripts, nodes, saving, returnFocusRef, fallbackFocusRef, onClose, onSave }: { form: { mode: 'create' | 'edit', task?: ScheduledTask, draft: TaskDraft }, setForm: (value: { mode: 'create' | 'edit', task?: ScheduledTask, draft: TaskDraft } | undefined) => void, scripts: AutomationScript[], nodes: Node[], saving: boolean, returnFocusRef: RefObject<HTMLElement | null>, fallbackFocusRef: RefObject<HTMLElement | null>, onClose: () => void, onSave: () => void }) {
  const nameRef = useRef<HTMLInputElement | null>(null)
  const update = <Key extends keyof TaskDraft>(key: Key, value: TaskDraft[Key]) => setForm({ ...form, draft: { ...form.draft, [key]: value } })
  const timezones = uniqueTimezones(form.draft.timezone)
  return (
    <TaskDialog ariaLabel={form.mode === 'create' ? '创建计划任务' : '编辑计划任务'} title={form.mode === 'create' ? '创建计划任务' : `编辑 ${form.task?.name || '计划任务'}`} description="Cron 使用标准五段表达式，下次运行时间由 Server 计算。" size="lg" initialFocusRef={nameRef} returnFocusRef={returnFocusRef} fallbackFocusRef={fallbackFocusRef} closeDisabled={saving} onClose={onClose} footer={<ModalActions busy={saving} busyLabel="保存中..." confirmLabel="保存" onClose={onClose} onConfirm={onSave} />}>
      <div className="space-y-5">
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField label="任务名称"><input ref={nameRef} aria-label="计划任务名称" value={form.draft.name} onChange={(event) => update('name', event.target.value)} maxLength={120} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold" /></FormField>
          <FormField label="脚本"><select aria-label="计划任务脚本" value={form.draft.scriptID} onChange={(event) => { const script = scripts.find((item) => String(item.id) === event.target.value); setForm({ ...form, draft: { ...form.draft, scriptID: event.target.value, timeoutSeconds: form.mode === 'create' && script ? String(script.timeout_seconds) : form.draft.timeoutSeconds } }) }} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold">{scripts.map((script) => <option key={script.id} value={script.id}>{script.name} · rev {script.revision}</option>)}</select></FormField>
        </div>
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_160px]">
          <FormField label="五段 Cron"><input aria-label="Cron 表达式" value={form.draft.cronExpression} onChange={(event) => update('cronExpression', event.target.value)} placeholder="0 2 * * *" disabled={saving} className="soft-input min-h-10 w-full px-3 font-mono text-sm font-bold" /></FormField>
          <FormField label="IANA 时区"><input aria-label="计划任务时区" list="automation-timezones" value={form.draft.timezone} onChange={(event) => update('timezone', event.target.value)} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold" /><datalist id="automation-timezones">{timezones.map((timezone) => <option key={timezone} value={timezone} />)}</datalist></FormField>
          <FormField label="超时（秒）"><input aria-label="计划任务超时" type="number" min={1} max={1800} value={form.draft.timeoutSeconds} onChange={(event) => update('timeoutSeconds', event.target.value)} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold" /></FormField>
        </div>
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_160px]">
          <FormField label="通知策略"><select aria-label="计划任务通知策略" value={form.draft.notificationPolicy} onChange={(event) => update('notificationPolicy', event.target.value as AutomationNotificationPolicy)} disabled={saving} className="soft-input min-h-10 w-full px-3 text-sm font-bold"><option value="never">不通知</option><option value="failure">失败时通知</option><option value="always">始终通知</option></select></FormField>
          <label className="flex min-h-10 items-center justify-between gap-3 self-end rounded-2xl border border-border bg-surface/70 px-3 py-2 text-sm font-black text-foreground">启用计划<input type="checkbox" role="switch" aria-label="启用计划任务" checked={form.draft.enabled} onChange={(event) => update('enabled', event.target.checked)} disabled={saving} className="h-4 w-4 accent-primary" /></label>
        </div>
        {form.draft.notificationPolicy !== 'never' ? <NotificationChannelsEditor value={form.draft.notificationChannels} onChange={(channels) => update('notificationChannels', channels)} disabled={saving} maxChannels={10} /> : null}
        <div><p className="mb-2 text-sm font-black text-foreground">目标节点</p><TaskNodeSelector nodes={nodes} selectedIDs={form.draft.nodeIDs} onChange={(nodeIDs) => update('nodeIDs', nodeIDs)} disabled={saving} /></div>
      </div>
    </TaskDialog>
  )
}

function ScriptRunModal({ value, nodes, running, returnFocusRef, fallbackFocusRef, onChange, onClose, onRun }: { value: { script: AutomationScript, nodeIDs: Set<string> }, nodes: Node[], running: boolean, returnFocusRef: RefObject<HTMLElement | null>, fallbackFocusRef: RefObject<HTMLElement | null>, onChange: (value: { script: AutomationScript, nodeIDs: Set<string> } | undefined) => void, onClose: () => void, onRun: () => void }) {
  return <TaskDialog ariaLabel="运行脚本" title={`运行 ${value.script.name}`} description={`默认超时 ${value.script.timeout_seconds} 秒 · rev ${value.script.revision}`} size="lg" returnFocusRef={returnFocusRef} fallbackFocusRef={fallbackFocusRef} closeDisabled={running} onClose={onClose} footer={<ModalActions busy={running} busyLabel="正在受理..." confirmLabel={`运行到 ${value.nodeIDs.size} 台节点`} confirmDisabled={value.nodeIDs.size === 0} onClose={onClose} onConfirm={onRun} />}><TaskNodeSelector nodes={nodes} selectedIDs={value.nodeIDs} onChange={(nodeIDs) => onChange({ ...value, nodeIDs })} disabled={running} /></TaskDialog>
}

function DeleteModal({ kind, name, detail, deleting, returnFocusRef, fallbackFocusRef, onClose, onConfirm }: { kind: string, name: string, detail: string, deleting: boolean, returnFocusRef: RefObject<HTMLElement | null>, fallbackFocusRef: RefObject<HTMLElement | null>, onClose: () => void, onConfirm: () => void }) {
  return <TaskDialog ariaLabel={`删除${kind}`} title={`删除“${name}”？`} description={detail} size="sm" returnFocusRef={returnFocusRef} fallbackFocusRef={fallbackFocusRef} closeDisabled={deleting} onClose={onClose} footer={<ModalActions busy={deleting} busyLabel="删除中..." confirmLabel="确认删除" danger onClose={onClose} onConfirm={onConfirm} />}><div className="rounded-2xl border border-danger/25 bg-danger/5 px-4 py-3 text-sm font-bold text-danger">此操作无法撤销。</div></TaskDialog>
}

function RunTaskModal({ task, running, returnFocusRef, fallbackFocusRef, onClose, onConfirm }: { task: ScheduledTask, running: boolean, returnFocusRef: RefObject<HTMLElement | null>, fallbackFocusRef: RefObject<HTMLElement | null>, onClose: () => void, onConfirm: () => void }) {
  return <TaskDialog ariaLabel="立即执行计划任务" title={`立即执行“${task.name}”？`} description={`${task.script_name} · ${task.node_ids.length} 台目标节点`} size="sm" returnFocusRef={returnFocusRef} fallbackFocusRef={fallbackFocusRef} closeDisabled={running} onClose={onClose} footer={<ModalActions busy={running} busyLabel="正在受理..." confirmLabel="确认执行" onClose={onClose} onConfirm={onConfirm} />}><div className="grid gap-3 sm:grid-cols-2"><RunPreview label="Cron" value={task.cron_expression} mono /><RunPreview label="时区" value={task.timezone} /><RunPreview label="超时" value={`${task.timeout_seconds} 秒`} /><RunPreview label="通知" value={notificationPolicyLabel(task.notification_policy)} /></div></TaskDialog>
}

function RunPreview({ label, value, mono = false }: { label: string, value: string, mono?: boolean }) {
  return <div className="rounded-2xl border border-border bg-surface/70 px-3 py-3"><p className="text-[11px] font-black text-muted-foreground">{label}</p><p className={`mt-1 break-all text-sm font-black text-foreground ${mono ? 'font-mono' : ''}`}>{value}</p></div>
}

function ModalActions({ busy, busyLabel, confirmLabel, confirmDisabled = false, danger = false, onClose, onConfirm }: { busy: boolean, busyLabel: string, confirmLabel: string, confirmDisabled?: boolean, danger?: boolean, onClose: () => void, onConfirm: () => void }) {
  return <div className="flex flex-wrap justify-end gap-2"><button type="button" onClick={onClose} disabled={busy} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground disabled:opacity-50">取消</button><button type="button" onClick={onConfirm} disabled={busy || confirmDisabled} className={`soft-button inline-flex min-h-10 items-center gap-2 px-4 text-sm font-black text-white disabled:cursor-not-allowed disabled:opacity-50 ${danger ? 'bg-danger' : 'bg-primary'}`}>{busy ? <Loader2 className="animate-spin" size={15} aria-hidden="true" /> : null}{busy ? busyLabel : confirmLabel}</button></div>
}

function FormField({ label, children }: { label: string, children: React.ReactNode }) {
  return <label className="block min-w-0 text-sm font-black text-foreground">{label}<span className="mt-1 block min-w-0">{children}</span></label>
}

function LoadingState({ label }: { label: string }) {
  return <section className="soft-empty-state flex min-h-64 items-center justify-center text-sm font-black text-muted-foreground"><Loader2 className="mr-2 animate-spin" size={18} aria-hidden="true" />{label}</section>
}

function ErrorState({ label }: { label: string }) {
  return <div role="alert" className="break-words rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger [overflow-wrap:anywhere]">{label}</div>
}

function EmptyState({ icon, title, action, onAction }: { icon: React.ReactNode, title: string, action?: string, onAction?: (trigger: HTMLElement) => void }) {
  return <section className="soft-empty-state px-6 py-16 text-center"><span className="mx-auto flex h-12 w-12 items-center justify-center text-muted-foreground">{icon}</span><h2 className="mt-4 text-xl font-black text-foreground">{title}</h2>{action && onAction ? <button type="button" onClick={(event) => onAction(event.currentTarget)} className="soft-button mt-5 inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground"><Plus size={16} aria-hidden="true" />{action}</button> : null}</section>
}

function validateScript(draft: ScriptDraft): { input: AutomationScriptInput, error?: undefined } | { input: AutomationScriptInput, error: string } {
  const timeout = Number.parseInt(draft.timeoutSeconds, 10)
  const input = { name: draft.name.trim(), description: draft.description.trim(), content: draft.content, timeout_seconds: timeout }
  if (!input.name) return { input, error: '名称不能为空' }
  if (!input.content.trim()) return { input, error: 'Shell 内容不能为空' }
  if (new TextEncoder().encode(input.content).length > scriptContentLimit) return { input, error: 'Shell 内容超过 128 KiB' }
  if (!Number.isInteger(timeout) || timeout < 1 || timeout > 1800) return { input, error: '超时必须在 1 到 1800 秒之间' }
  return { input }
}

function validateTask(draft: TaskDraft): { input: ScheduledTaskInput, error?: undefined } | { input: ScheduledTaskInput, error: string } {
  const timeout = Number.parseInt(draft.timeoutSeconds, 10)
  const scriptID = Number.parseInt(draft.scriptID, 10)
  const input = { name: draft.name.trim(), script_id: scriptID, node_ids: [...draft.nodeIDs], cron_expression: draft.cronExpression.trim().replace(/\s+/g, ' '), timezone: draft.timezone.trim(), timeout_seconds: timeout, enabled: draft.enabled, notification_policy: draft.notificationPolicy, notification_channels: draft.notificationChannels }
  if (!input.name) return { input, error: '名称不能为空' }
  if (!Number.isInteger(scriptID) || scriptID < 1) return { input, error: '请选择脚本' }
  if (input.node_ids.length === 0) return { input, error: '至少选择一个节点' }
  if (input.node_ids.length > 100) return { input, error: '目标节点不能超过 100 台' }
  if (input.cron_expression.split(' ').length !== 5) return { input, error: 'Cron 必须是标准五段表达式' }
  if (!input.timezone) return { input, error: '时区不能为空' }
  if (!Number.isInteger(timeout) || timeout < 1 || timeout > 1800) return { input, error: '超时必须在 1 到 1800 秒之间' }
  return { input }
}

function defaultTimezone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'
}

function uniqueTimezones(current: string) {
  return [...new Set([current, defaultTimezone(), 'Asia/Shanghai', 'UTC', 'Asia/Tokyo', 'Europe/London', 'America/New_York'].filter(Boolean))]
}

function notificationPolicyLabel(policy: AutomationNotificationPolicy) {
  if (policy === 'never') return '不通知'
  if (policy === 'always') return '始终通知'
  return '失败时通知'
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '网络错误'
}

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError'
}

function mergeUniqueRuns(...groups: AutomationRun[][]) {
  const runs = new Map<number, AutomationRun>()
  for (const group of groups) {
    for (const run of group) {
      if (!runs.has(run.id)) runs.set(run.id, run)
    }
  }
  return [...runs.values()].sort((left, right) => right.id - left.id)
}

function mergeRunHead(current: AutomationRun[], received: AutomationRun[], nextBeforeID: number) {
  return mergeUniqueRuns(received, current.filter((run) => run.id < nextBeforeID))
}

function upsertByID<Value extends { id: number }>(values: Value[], value: Value) {
  const index = values.findIndex((item) => item.id === value.id)
  if (index < 0) return [value, ...values]
  return values.map((item) => item.id === value.id ? value : item)
}
