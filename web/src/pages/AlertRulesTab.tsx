import { useEffect, useState } from 'react'
import { AlertTriangle, Edit2, Plus, Trash2, X } from 'lucide-react'
import { createAlertRule, deleteAlertRule, getAlertRules, toggleAlertRule, updateAlertRule } from '../api/client'
import type { AlertRule, Node, NotificationChannel } from '../types'
import { Toast } from '../components/Toast'
import { NotificationChannelsEditor } from '../components/NotificationChannelsEditor'

type AlertRulesTabProps = {
  nodes: Node[]
}

type FormMode = 'create' | 'edit'

const metricFieldOptions = [
  { value: 'cpu_usage', label: 'CPU 使用率 (%)' },
  { value: 'memory_usage', label: '内存使用率 (%)' },
  { value: 'disk_usage', label: '磁盘使用率 (%)' },
  { value: 'swap_usage', label: 'Swap 使用率 (%)' },
  { value: 'load_1', label: '系统负载 (1分钟)' },
  { value: 'load_5', label: '系统负载 (5分钟)' },
  { value: 'load_15', label: '系统负载 (15分钟)' }
]

const operatorOptions = [
  { value: '>', label: '大于 (>)' },
  { value: '>=', label: '大于等于 (≥)' },
  { value: '<', label: '小于 (<)' },
  { value: '<=', label: '小于等于 (≤)' },
  { value: '=', label: '等于 (=)' }
]

export function AlertRulesTab({ nodes }: AlertRulesTabProps) {
  const [rules, setRules] = useState<AlertRule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [showForm, setShowForm] = useState(false)
  const [formMode, setFormMode] = useState<FormMode>('create')
  const [editingRuleID, setEditingRuleID] = useState<number>()
  const [formName, setFormName] = useState('')
  const [formMetricField, setFormMetricField] = useState('cpu_usage')
  const [formOperator, setFormOperator] = useState('>')
  const [formThreshold, setFormThreshold] = useState('80')
  const [formDuration, setFormDuration] = useState('300')
  const [formScopeType, setFormScopeType] = useState<'all' | 'nodes'>('all')
  const [formScopeNodeIDs, setFormScopeNodeIDs] = useState<string[]>([])
  const [formChannels, setFormChannels] = useState<NotificationChannel[]>([])
  const [formSaving, setFormSaving] = useState(false)
  const [formError, setFormError] = useState<string>()
  const [pendingDeleteRule, setPendingDeleteRule] = useState<AlertRule | null>(null)
  const [deletingRuleID, setDeletingRuleID] = useState<number>()
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)

  useEffect(() => {
    loadRules()
  }, [])

  const loadRules = () => {
    setLoading(true)
    setError(undefined)
    getAlertRules()
      .then((response) => setRules(response.rules || []))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : '加载告警规则失败'))
      .finally(() => setLoading(false))
  }

  const openCreateForm = () => {
    setFormMode('create')
    setEditingRuleID(undefined)
    setFormName('')
    setFormMetricField('cpu_usage')
    setFormOperator('>')
    setFormThreshold('80')
    setFormDuration('300')
    setFormScopeType('all')
    setFormScopeNodeIDs([])
    setFormChannels([])
    setFormError(undefined)
    setShowForm(true)
  }

  const openEditForm = (rule: AlertRule) => {
    setFormMode('edit')
    setEditingRuleID(rule.id)
    setFormName(rule.name)
    setFormMetricField(rule.metric_field)
    setFormOperator(rule.operator)
    setFormThreshold(String(rule.threshold))
    setFormDuration(String(rule.duration_seconds))
    setFormScopeType(rule.scope_type)
    setFormScopeNodeIDs(rule.scope_node_ids || [])
    setFormChannels(rule.notification_channels)
    setFormError(undefined)
    setShowForm(true)
  }

  const closeForm = () => {
    setShowForm(false)
    setFormError(undefined)
  }

  const saveRule = () => {
    const threshold = Number.parseFloat(formThreshold)
    const duration = Number.parseInt(formDuration, 10)
    if (!formName.trim()) {
      setFormError('规则名称不能为空')
      return
    }
    if (Number.isNaN(threshold)) {
      setFormError('阈值必须是有效数字')
      return
    }
    if (Number.isNaN(duration) || duration < 0) {
      setFormError('持续时间必须是非负整数')
      return
    }
    if (formScopeType === 'nodes' && formScopeNodeIDs.length === 0) {
      setFormError('指定节点模式下至少选择一个节点')
      return
    }

    const payload = {
      name: formName.trim(),
      enabled: formMode === 'edit' ? rules.find((rule) => rule.id === editingRuleID)?.enabled ?? true : true,
      metric_field: formMetricField,
      operator: formOperator,
      threshold,
      duration_seconds: duration,
      scope_type: formScopeType,
      ...(formScopeType === 'nodes' ? { scope_node_ids: formScopeNodeIDs } : {}),
      notification_channels: formChannels
    }

    setFormSaving(true)
    setFormError(undefined)
    const request = formMode === 'create'
      ? createAlertRule(payload)
      : updateAlertRule(editingRuleID!, payload)

    request
      .then(() => {
        setToast({ message: `告警规则${formMode === 'create' ? '创建' : '保存'}成功`, type: 'success' })
        closeForm()
        loadRules()
      })
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : '网络错误'
        setFormError(`告警规则${formMode === 'create' ? '创建' : '保存'}失败: ${message}`)
        setToast({ message: `告警规则${formMode === 'create' ? '创建' : '保存'}失败: ${message}`, type: 'error' })
      })
      .finally(() => setFormSaving(false))
  }

  const handleToggle = (id: number, enabled: boolean) => {
    toggleAlertRule(id, enabled)
      .then((updated) => {
        setRules((current) => current.map((rule) => (rule.id === id ? updated : rule)))
        setToast({ message: `告警规则${enabled ? '启用' : '禁用'}成功`, type: 'success' })
      })
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : '网络错误'
        setError(`告警规则${enabled ? '启用' : '禁用'}失败: ${message}`)
        setToast({ message: `告警规则${enabled ? '启用' : '禁用'}失败: ${message}`, type: 'error' })
      })
  }

  const confirmDeleteRule = () => {
    if (!pendingDeleteRule) return
    setDeletingRuleID(pendingDeleteRule.id)
    deleteAlertRule(pendingDeleteRule.id)
      .then(() => {
        setRules((current) => current.filter((rule) => rule.id !== pendingDeleteRule.id))
        setToast({ message: '告警规则删除成功', type: 'success' })
        setPendingDeleteRule(null)
      })
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : '网络错误'
        setError(`告警规则删除失败: ${message}`)
        setToast({ message: `告警规则删除失败: ${message}`, type: 'error' })
      })
      .finally(() => setDeletingRuleID(undefined))
  }

  const toggleNodeSelection = (nodeID: string) => {
    setFormScopeNodeIDs((current) =>
      current.includes(nodeID) ? current.filter((id) => id !== nodeID) : [...current, nodeID]
    )
  }

  if (loading) {
    return (
      <section className="soft-empty-state p-5">
        <p className="text-sm font-black text-muted-foreground">正在加载告警规则...</p>
      </section>
    )
  }

  return (
    <>
      {toast ? <Toast message={toast.message} type={toast.type} onClose={() => setToast(null)} /> : null}

      <div className="flex items-center justify-end mb-4">
        <button
          type="button"
          onClick={openCreateForm}
          className="soft-button inline-flex min-h-9 cursor-pointer items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20"
        >
          <Plus size={16} aria-hidden="true" />
          创建告警规则
        </button>
      </div>

      <section className="soft-panel p-5">

        {error ? (
          <div className="mt-4 rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger">
            {error}
          </div>
        ) : null}

        {rules.length === 0 ? (
          <div className="soft-empty-state mt-5 px-6 py-16 text-center">
            <p className="text-2xl font-black text-foreground">暂无告警规则</p>
            <p className="mx-auto mt-3 max-w-xl text-sm font-semibold leading-6 text-muted-foreground">
              创建规则后，系统会自动监控指标并在触发时发送通知。
            </p>
          </div>
        ) : (
          <div className="mt-5 space-y-3">
            {rules.map((rule) => (
              <div
                key={rule.id}
                className="soft-card p-4 transition hover:border-primary/30"
              >
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h3 className="text-base font-black text-foreground">{rule.name}</h3>
                      <span
                        className={`rounded-full px-2 py-0.5 text-xs font-black ${
                          rule.enabled
                            ? 'bg-success/10 text-success'
                            : 'bg-muted text-muted-foreground'
                        }`}
                      >
                        {rule.enabled ? '启用' : '禁用'}
                      </span>
                    </div>
                    <p className="mt-2 text-sm font-semibold text-muted-foreground">
                      {metricFieldOptions.find((opt) => opt.value === rule.metric_field)?.label || rule.metric_field}{' '}
                      {operatorOptions.find((opt) => opt.value === rule.operator)?.label || rule.operator}{' '}
                      {rule.threshold}，持续 {rule.duration_seconds}秒
                    </p>
                    <p className="mt-1 text-xs font-semibold text-muted-foreground">
                      范围：{rule.scope_type === 'all' ? '所有节点' : `指定节点 (${rule.scope_node_ids?.length || 0}个)`}
                      {' · '}
                      通知：{rule.notification_channels.length}个渠道
                    </p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      onClick={() => handleToggle(rule.id, !rule.enabled)}
                      className="soft-button min-h-9 cursor-pointer border border-border bg-card px-3 text-xs font-black text-foreground hover:border-primary/50 focus:outline-none focus:ring-4 focus:ring-primary/20"
                    >
                      {rule.enabled ? '禁用' : '启用'}
                    </button>
                    <button
                      type="button"
                      onClick={() => openEditForm(rule)}
                      className="soft-button inline-flex min-h-9 cursor-pointer items-center gap-1.5 border border-border bg-card px-3 text-xs font-black text-foreground hover:border-primary/50 focus:outline-none focus:ring-4 focus:ring-primary/20"
                    >
                      <Edit2 size={13} aria-hidden="true" />
                      编辑
                    </button>
                    <button
                      type="button"
                      onClick={() => setPendingDeleteRule(rule)}
                      className="soft-button inline-flex min-h-9 cursor-pointer items-center gap-1.5 border border-danger/30 bg-card px-3 text-xs font-black text-danger hover:border-danger/50 focus:outline-none focus:ring-4 focus:ring-danger/20"
                    >
                      <Trash2 size={13} aria-hidden="true" />
                      删除
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {showForm ? (
        <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-6">
          <section
            role="dialog"
            aria-modal="true"
            aria-label={formMode === 'create' ? '创建告警规则' : '编辑告警规则'}
            className="soft-modal-shell max-h-[calc(100vh-3rem)] w-full max-w-3xl overflow-y-auto"
          >
            <div className="soft-modal-header sticky top-0 z-10 border-b px-5 py-4">
              <h3 className="text-lg font-black text-foreground">
                {formMode === 'create' ? '创建告警规则' : '编辑告警规则'}
              </h3>
            </div>

            <div className="space-y-4 p-5">
              <label className="block text-sm font-black text-foreground">
                规则名称
                <input
                  type="text"
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="CPU 使用率过高"
                  className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold"
                />
              </label>

              <div className="grid gap-4 sm:grid-cols-3">
                <label className="block text-sm font-black text-foreground">
                  监控指标
                  <select
                    value={formMetricField}
                    onChange={(e) => setFormMetricField(e.target.value)}
                    className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold"
                  >
                    {metricFieldOptions.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="block text-sm font-black text-foreground">
                  比较运算符
                  <select
                    value={formOperator}
                    onChange={(e) => setFormOperator(e.target.value)}
                    className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold"
                  >
                    {operatorOptions.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="block text-sm font-black text-foreground">
                  阈值
                  <input
                    type="number"
                    step="0.01"
                    value={formThreshold}
                    onChange={(e) => setFormThreshold(e.target.value)}
                    className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold"
                  />
                </label>
              </div>

              <label className="block text-sm font-black text-foreground">
                持续时间（秒）
                <input
                  type="number"
                  value={formDuration}
                  onChange={(e) => setFormDuration(e.target.value)}
                  placeholder="300"
                  className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold"
                />
                <p className="mt-1 text-xs font-semibold text-muted-foreground">
                  条件需要持续满足多少秒才触发告警（0 表示立即触发）
                </p>
              </label>

              <div className="space-y-2">
                <p className="text-sm font-black text-foreground">监控范围</p>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => setFormScopeType('all')}
                    className={`soft-button min-h-10 cursor-pointer px-4 text-sm font-black focus:outline-none focus:ring-4 focus:ring-primary/20 ${
                      formScopeType === 'all'
                        ? 'bg-primary text-primary-foreground shadow-sm'
                        : 'border border-border bg-card text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    所有节点
                  </button>
                  <button
                    type="button"
                    onClick={() => setFormScopeType('nodes')}
                    className={`soft-button min-h-10 cursor-pointer px-4 text-sm font-black focus:outline-none focus:ring-4 focus:ring-primary/20 ${
                      formScopeType === 'nodes'
                        ? 'bg-primary text-primary-foreground shadow-sm'
                        : 'border border-border bg-card text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    指定节点
                  </button>
                </div>

                {formScopeType === 'nodes' ? (
                  <div className="soft-toolbar mt-2 max-h-40 space-y-1 overflow-y-auto p-3">
                    {nodes.length === 0 ? (
                      <p className="text-xs font-semibold text-muted-foreground">暂无节点</p>
                    ) : (
                      nodes.map((node) => (
                        <label
                          key={node.id}
                          className="soft-button flex cursor-pointer items-center gap-2 px-2 py-1 hover:bg-muted"
                        >
                          <input
                            type="checkbox"
                            checked={formScopeNodeIDs.includes(node.id)}
                            onChange={() => toggleNodeSelection(node.id)}
                            className="h-4 w-4 cursor-pointer rounded border-border"
                          />
                          <span className="text-sm font-bold text-foreground">{node.name}</span>
                          <span className="text-xs font-semibold text-muted-foreground">({node.id})</span>
                        </label>
                      ))
                    )}
                  </div>
                ) : null}
              </div>

              <NotificationChannelsEditor value={formChannels} onChange={setFormChannels} disabled={formSaving} />

              {formError ? (
                <div className="rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger">
                  {formError}
                </div>
              ) : null}
            </div>

            <div className="soft-modal-footer sticky bottom-0 border-t px-5 py-4">
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={saveRule}
                  disabled={formSaving}
                  className="soft-button min-h-11 flex-1 cursor-pointer bg-primary px-4 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {formSaving ? '保存中...' : '保存'}
                </button>
                <button
                  type="button"
                  onClick={closeForm}
                  disabled={formSaving}
                  className="soft-button min-h-11 cursor-pointer border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  取消
                </button>
              </div>
            </div>
          </section>
        </div>
      ) : null}

      {pendingDeleteRule ? (
        <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-6">
          <section
            role="dialog"
            aria-modal="true"
            aria-label="删除告警规则"
            className="soft-modal-shell w-full max-w-md"
          >
            <div className="soft-modal-header flex items-start justify-between gap-4 border-b px-5 py-4">
              <div className="flex items-start gap-3">
                <span className="mt-0.5 inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border border-danger/25 bg-danger/10 text-danger">
                  <AlertTriangle size={18} aria-hidden="true" />
                </span>
                <div>
                  <h3 className="text-lg font-black text-foreground">删除告警规则</h3>
                  <p className="mt-1 text-sm font-semibold leading-6 text-muted-foreground">
                    删除后该规则不再触发，未解决告警会先自动收敛。
                  </p>
                </div>
              </div>
              <button
                type="button"
                aria-label="关闭"
                onClick={() => setPendingDeleteRule(null)}
                disabled={deletingRuleID === pendingDeleteRule.id}
                className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <X size={16} aria-hidden="true" />
              </button>
            </div>
            <div className="p-5">
              <p className="rounded-2xl border border-border bg-surface/80 px-4 py-3 text-sm font-black text-foreground">
                {pendingDeleteRule.name}
              </p>
            </div>
            <div className="soft-modal-footer border-t px-5 py-4">
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={confirmDeleteRule}
                  disabled={deletingRuleID === pendingDeleteRule.id}
                  className="soft-button min-h-11 flex-1 cursor-pointer bg-danger px-4 text-sm font-black text-white shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-danger/20 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {deletingRuleID === pendingDeleteRule.id ? '删除中...' : '删除'}
                </button>
                <button
                  type="button"
                  onClick={() => setPendingDeleteRule(null)}
                  disabled={deletingRuleID === pendingDeleteRule.id}
                  className="soft-button min-h-11 cursor-pointer border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  取消
                </button>
              </div>
            </div>
          </section>
        </div>
      ) : null}
    </>
  )
}
