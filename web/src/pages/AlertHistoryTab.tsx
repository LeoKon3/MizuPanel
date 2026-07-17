import { useEffect, useState } from 'react'
import { deleteAlertHistories, deleteAlertHistory, getAlertHistory, resolveAlertHistory } from '../api/client'
import type { AlertHistory, Node } from '../types'
import { AlertTriangle, CheckCircle2, CheckSquare, Search, ShieldCheck, Square, Trash2, X } from 'lucide-react'
import { Toast } from '../components/Toast'

type AlertHistoryTabProps = {
  nodes: Node[]
}

type FilterStatus = 'all' | 'active' | 'resolved'

type PendingDelete =
  | { mode: 'single'; alert: AlertHistory }
  | { mode: 'batch'; ids: number[] }

export function AlertHistoryTab({ nodes }: AlertHistoryTabProps) {
  const [alertHistory, setAlertHistory] = useState<AlertHistory[]>([])
  const [loading, setLoading] = useState(true)
  const [filterStatus, setFilterStatus] = useState<FilterStatus>('all')
  const [filterNode, setFilterNode] = useState<string>('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [resolvingIDs, setResolvingIDs] = useState<Set<number>>(() => new Set())
  const [selectedResolvedIDs, setSelectedResolvedIDs] = useState<Set<number>>(() => new Set())
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)

  // 加载告警历史
  const loadAlertHistory = () => {
    if (nodes.length === 0) {
      setAlertHistory([])
      setLoading(false)
      return
    }

    setLoading(true)
    Promise.all(
      nodes.map((node) =>
        getAlertHistory(node.id, 100).catch(() => ({ history: [] }))
      )
    ).then((results) => {
      const allHistory = results.flatMap((r) => r.history).filter(Boolean)
      const sorted = allHistory.sort(
        (a, b) => new Date(b.triggered_at).getTime() - new Date(a.triggered_at).getTime()
      )
      setAlertHistory(sorted)
      setSelectedResolvedIDs((current) => {
        if (current.size === 0) return current
        const availableIDs = new Set(sorted.filter((alert) => alert.resolved_at).map((alert) => alert.id))
        const next = new Set([...current].filter((id) => availableIDs.has(id)))
        return next.size === current.size ? current : next
      })
      setLoading(false)
    }).catch(() => {
      setLoading(false)
    })
  }

  // 初始加载
  useEffect(() => {
    loadAlertHistory()
  }, [nodes])

  // 定时刷新活跃告警（每30秒）
  useEffect(() => {
    const interval = setInterval(() => {
      loadAlertHistory()
    }, 30000)

    return () => clearInterval(interval)
  }, [nodes])

  // 过滤逻辑
  const filteredAlerts = alertHistory.filter((alert) => {
    // 状态过滤
    if (filterStatus === 'active' && alert.resolved_at) return false
    if (filterStatus === 'resolved' && !alert.resolved_at) return false

    // 节点过滤
    if (filterNode !== 'all' && alert.node_id !== filterNode) return false

    // 搜索过滤
    if (searchQuery && !alert.rule_name.toLowerCase().includes(searchQuery.toLowerCase())) {
      return false
    }

    return true
  })

  const visibleResolvedAlerts = filteredAlerts.filter((alert) => alert.resolved_at)
  const selectedCount = selectedResolvedIDs.size
  const visibleResolvedIDs = visibleResolvedAlerts.map((alert) => alert.id)
  const allVisibleResolvedSelected =
    visibleResolvedIDs.length > 0 && visibleResolvedIDs.every((id) => selectedResolvedIDs.has(id))

  const handleResolveAlert = (alertID: number) => {
    setResolvingIDs((current) => new Set(current).add(alertID))
    resolveAlertHistory(alertID)
      .then((resolvedAlert) => {
        setAlertHistory((current) => current.map((alert) => (alert.id === resolvedAlert.id ? resolvedAlert : alert)))
        setToast({ message: '告警标记解决成功', type: 'success' })
        loadAlertHistory()
      })
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : '网络错误'
        setToast({ message: `告警标记解决失败: ${message}`, type: 'error' })
      })
      .finally(() => {
        setResolvingIDs((current) => {
          const next = new Set(current)
          next.delete(alertID)
          return next
        })
      })
  }

  const clearSelection = () => {
    setSelectedResolvedIDs(new Set())
  }

  const setStatusFilter = (status: FilterStatus) => {
    setFilterStatus(status)
    clearSelection()
  }

  const handleSearchChange = (value: string) => {
    setSearchQuery(value)
    clearSelection()
  }

  const handleNodeFilterChange = (nodeID: string) => {
    setFilterNode(nodeID)
    clearSelection()
  }

  const toggleResolvedSelection = (alertID: number) => {
    setSelectedResolvedIDs((current) => {
      const next = new Set(current)
      if (next.has(alertID)) {
        next.delete(alertID)
      } else {
        next.add(alertID)
      }
      return next
    })
  }

  const toggleSelectAllVisibleResolved = () => {
    setSelectedResolvedIDs((current) => {
      const next = new Set(current)
      if (allVisibleResolvedSelected) {
        visibleResolvedIDs.forEach((id) => next.delete(id))
      } else {
        visibleResolvedIDs.forEach((id) => next.add(id))
      }
      return next
    })
  }

  const requestSingleDelete = (alert: AlertHistory) => {
    setPendingDelete({ mode: 'single', alert })
  }

  const requestBatchDelete = () => {
    const ids = [...selectedResolvedIDs]
    if (ids.length === 0) return
    setPendingDelete({ mode: 'batch', ids })
  }

  const confirmDelete = () => {
    if (!pendingDelete) return

    setDeleting(true)
    if (pendingDelete.mode === 'single') {
      const alertID = pendingDelete.alert.id
      deleteAlertHistory(alertID)
        .then(() => {
          setAlertHistory((current) => current.filter((alert) => alert.id !== alertID))
          setSelectedResolvedIDs((current) => {
            const next = new Set(current)
            next.delete(alertID)
            return next
          })
          setPendingDelete(null)
          setToast({ message: '告警删除成功', type: 'success' })
          loadAlertHistory()
        })
        .catch((err: unknown) => {
          const message = err instanceof Error ? err.message : '网络错误'
          setToast({ message: `告警删除失败: ${message}`, type: 'error' })
        })
        .finally(() => {
          setDeleting(false)
        })
      return
    }

    const ids = pendingDelete.ids
    deleteAlertHistories(ids)
      .then(() => {
        const idSet = new Set(ids)
        setAlertHistory((current) => current.filter((alert) => !idSet.has(alert.id)))
        setSelectedResolvedIDs(new Set())
        setPendingDelete(null)
        setToast({ message: '告警批量删除成功', type: 'success' })
        loadAlertHistory()
      })
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : '网络错误'
        setToast({ message: `告警批量删除失败: ${message}`, type: 'error' })
      })
      .finally(() => {
        setDeleting(false)
      })
  }

  const formatTimestamp = (timestamp: string) => {
    const date = new Date(timestamp)
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    }).replace(/\//g, '-')
  }

  if (loading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <p className="text-sm font-semibold text-muted-foreground">加载中...</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {toast ? <Toast message={toast.message} type={toast.type} onClose={() => setToast(null)} /> : null}

      {/* 筛选栏 */}
      <div className="soft-toolbar flex flex-wrap items-center gap-3 p-3">
        {/* 状态筛选按钮组 */}
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => setStatusFilter('all')}
            className={`soft-button min-h-9 px-3 text-xs font-black transition ${
              filterStatus === 'all'
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'border border-border bg-card text-muted-foreground hover:text-foreground'
            }`}
          >
            全部
          </button>
          <button
            type="button"
            onClick={() => setStatusFilter('active')}
            className={`soft-button min-h-9 px-3 text-xs font-black transition ${
              filterStatus === 'active'
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'border border-border bg-card text-muted-foreground hover:text-foreground'
            }`}
          >
            活跃
          </button>
          <button
            type="button"
            onClick={() => setStatusFilter('resolved')}
            className={`soft-button min-h-9 px-3 text-xs font-black transition ${
              filterStatus === 'resolved'
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'border border-border bg-card text-muted-foreground hover:text-foreground'
            }`}
          >
            已解决
          </button>
        </div>

        {/* 搜索框 */}
        <div className="relative flex-1 min-w-[200px]">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" size={16} />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => handleSearchChange(e.target.value)}
            placeholder="搜索规则名称..."
            className="soft-input min-h-9 w-full pl-9 pr-3 text-xs font-bold"
          />
        </div>

        {/* 节点筛选下拉 */}
        <select
          value={filterNode}
          onChange={(e) => handleNodeFilterChange(e.target.value)}
          className="soft-input min-h-9 px-3 text-xs font-black"
        >
          <option value="all">所有节点</option>
          {nodes.map((node) => (
            <option key={node.id} value={node.id}>
              {node.name || node.hostname}
            </option>
          ))}
        </select>
      </div>

      {filterStatus === 'resolved' && visibleResolvedAlerts.length > 0 ? (
        <div className="soft-toolbar flex flex-wrap items-center justify-between gap-3 p-3">
          <div>
            <p className="text-xs font-black text-foreground">已解决告警</p>
            <p className="mt-0.5 text-[11px] font-semibold text-muted-foreground">
              当前结果 {visibleResolvedAlerts.length} 条 · 已选 {selectedCount} 条
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              aria-pressed={allVisibleResolvedSelected}
              onClick={toggleSelectAllVisibleResolved}
              className={`soft-button inline-flex min-h-9 items-center gap-1.5 border px-3 text-xs font-black focus:outline-none focus:ring-4 focus:ring-primary/20 ${
                allVisibleResolvedSelected
                  ? 'border-primary/40 bg-primary/10 text-primary hover:border-primary/50'
                  : 'border-border bg-card text-foreground hover:border-primary/40 hover:text-primary'
              }`}
            >
              {allVisibleResolvedSelected ? <CheckSquare size={14} aria-hidden="true" /> : <Square size={14} aria-hidden="true" />}
              全选当前结果
            </button>
            <button
              type="button"
              onClick={requestBatchDelete}
              disabled={selectedCount === 0}
              className="soft-button inline-flex min-h-9 items-center gap-1.5 border border-danger/30 bg-danger/10 px-3 text-xs font-black text-danger hover:border-danger/50 focus:outline-none focus:ring-4 focus:ring-danger/20 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Trash2 size={14} aria-hidden="true" />
              删除所选
            </button>
          </div>
        </div>
      ) : null}

      {/* 告警卡片列表 */}
      <div className="space-y-3">
        {filteredAlerts.length === 0 ? (
          <div className="soft-empty-state p-12 text-center">
            <ShieldCheck className="mx-auto text-success" size={28} aria-hidden="true" />
            <p className="mt-3 text-sm font-black text-success">目前没有{filterStatus === 'active' ? '活跃' : ''}告警</p>
            <p className="mt-1 text-xs font-semibold text-muted-foreground">
              {filterStatus === 'all' ? '所有监控指标正常' : filterStatus === 'active' ? '所有监控指标正常' : '没有已解决的告警记录'}
            </p>
          </div>
        ) : (
          filteredAlerts.map((alert) => (
            <AlertCard
              key={alert.id}
              alert={alert}
              formatTimestamp={formatTimestamp}
              onResolve={handleResolveAlert}
              resolving={resolvingIDs.has(alert.id)}
              selectionEnabled={filterStatus === 'resolved' && Boolean(alert.resolved_at)}
              selected={selectedResolvedIDs.has(alert.id)}
              onToggleSelect={toggleResolvedSelection}
              onDelete={requestSingleDelete}
            />
          ))
        )}
      </div>

      <DeleteAlertHistoryModal
        pendingDelete={pendingDelete}
        deleting={deleting}
        onClose={() => setPendingDelete(null)}
        onConfirm={confirmDelete}
      />
    </div>
  )
}

function AlertCard({
  alert,
  formatTimestamp,
  onResolve,
  resolving,
  selectionEnabled,
  selected,
  onToggleSelect,
  onDelete
}: {
  alert: AlertHistory
  formatTimestamp: (ts: string) => string
  onResolve: (alertID: number) => void
  resolving: boolean
  selectionEnabled: boolean
  selected: boolean
  onToggleSelect: (alertID: number) => void
  onDelete: (alert: AlertHistory) => void
}) {
  const isActive = !alert.resolved_at

  return (
    <div
      className={`soft-card border-l-4 p-4 transition ${
        isActive
          ? 'border-danger bg-danger/5'
          : 'border-success/50 bg-card'
      }`}
    >
      <div className="flex items-start justify-between gap-3">
        {selectionEnabled ? (
          <button
            type="button"
            aria-label={selected ? `取消选择 ${alert.rule_name}` : `选择 ${alert.rule_name}`}
            onClick={() => onToggleSelect(alert.id)}
            className={`soft-button mt-0.5 inline-flex h-9 w-9 shrink-0 items-center justify-center border text-muted-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 ${
              selected
                ? 'border-primary/40 bg-primary/10 text-primary'
                : 'border-border bg-card hover:border-primary/30 hover:text-primary'
            }`}
          >
            {selected ? <CheckSquare size={16} aria-hidden="true" /> : <Square size={16} aria-hidden="true" />}
          </button>
        ) : null}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <p className="truncate text-sm font-bold text-foreground">{alert.rule_name}</p>
            <span
              className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-black ${
                isActive
                  ? 'bg-danger/10 text-danger'
                  : 'bg-success/10 text-success'
              }`}
            >
              {isActive ? '活跃' : '已解决'}
            </span>
          </div>
          <p className="mt-1 text-xs font-semibold text-muted-foreground">
            节点: {alert.node_name} · {alert.metric_field}: {alert.metric_value.toFixed(1)}% {'>'} {alert.threshold}%
          </p>
          <p className="mt-2 text-xs font-medium text-muted-foreground">
            {isActive ? (
              <>触发时间: {formatTimestamp(alert.triggered_at)}</>
            ) : (
              <>触发: {formatTimestamp(alert.triggered_at)} · 解决: {formatTimestamp(alert.resolved_at!)}</>
            )}
          </p>
          <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:flex-wrap">
            <NotificationDeliveryStatus
              label="触发通知"
              sent={alert.notification_sent}
              error={alert.notification_error}
              attemptedAt={alert.notification_attempted_at}
            />
            {!isActive ? (
              <NotificationDeliveryStatus
                label="恢复通知"
                sent={Boolean(alert.recovery_notification_sent)}
                error={alert.recovery_notification_error}
                attemptedAt={alert.recovery_notification_attempted_at}
              />
            ) : null}
          </div>
        </div>
        {isActive ? (
          <button
            type="button"
            onClick={() => onResolve(alert.id)}
            disabled={resolving}
            className="soft-button inline-flex min-h-9 shrink-0 cursor-pointer items-center gap-1.5 border border-success/30 bg-success/10 px-3 text-xs font-black text-success hover:border-success/50 focus:outline-none focus:ring-4 focus:ring-success/20 disabled:cursor-not-allowed disabled:opacity-60"
          >
            <CheckCircle2 size={14} aria-hidden="true" />
            {resolving ? '处理中...' : '标记解决'}
          </button>
        ) : (
          <button
            type="button"
            onClick={() => onDelete(alert)}
            className="soft-button inline-flex min-h-9 shrink-0 cursor-pointer items-center gap-1.5 border border-danger/25 bg-danger/10 px-3 text-xs font-black text-danger hover:border-danger/45 focus:outline-none focus:ring-4 focus:ring-danger/20"
          >
            <Trash2 size={14} aria-hidden="true" />
            删除
          </button>
        )}
      </div>
    </div>
  )
}

function NotificationDeliveryStatus({
  label,
  sent,
  error,
  attemptedAt
}: {
  label: string
  sent: boolean
  error?: string
  attemptedAt?: string
}) {
  const status = getNotificationDeliveryStatus(sent, error, attemptedAt)
  return (
    <div className={`min-w-0 rounded-xl border px-2.5 py-1.5 ${status.containerTone}`}>
      <div className="flex items-center gap-2">
        <span className="text-[10px] font-black text-muted-foreground">{label}</span>
        <span className={`text-[10px] font-black ${status.textTone}`}>{status.label}</span>
      </div>
      {status.kind === 'failed' && error ? (
        <p className="mt-1 max-w-md break-words text-[11px] font-semibold leading-4 text-danger">{error}</p>
      ) : null}
    </div>
  )
}

function getNotificationDeliveryStatus(sent: boolean, error?: string, attemptedAt?: string) {
  if (!attemptedAt) {
    return { kind: 'unknown', label: '未知', containerTone: 'border-warning/20 bg-warning/5', textTone: 'text-warning' }
  }
  if (sent) {
    return { kind: 'success', label: '成功', containerTone: 'border-success/20 bg-success/5', textTone: 'text-success' }
  }
  if (error) {
    return { kind: 'failed', label: '失败', containerTone: 'border-danger/20 bg-danger/5', textTone: 'text-danger' }
  }
  return { kind: 'unconfigured', label: '未配置', containerTone: 'border-border bg-muted/40', textTone: 'text-muted-foreground' }
}

function DeleteAlertHistoryModal({
  pendingDelete,
  deleting,
  onClose,
  onConfirm
}: {
  pendingDelete: PendingDelete | null
  deleting: boolean
  onClose: () => void
  onConfirm: () => void
}) {
  if (!pendingDelete) return null

  const isBatch = pendingDelete.mode === 'batch'
  const title = isBatch ? '删除已解决告警' : '删除告警记录'
  const body = isBatch
    ? `将从历史列表中删除 ${pendingDelete.ids.length} 条已解决告警记录。`
    : '将从历史列表中删除这条已解决告警记录。'
  const targetLabel = isBatch ? `${pendingDelete.ids.length} 条告警记录` : pendingDelete.alert.rule_name

  return (
    <div
      className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-6"
      onClick={(event) => {
        if (event.target === event.currentTarget && !deleting) onClose()
      }}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="soft-modal-shell w-full max-w-md"
      >
        <div className="soft-modal-header flex items-start justify-between gap-4 border-b px-5 py-4">
          <div className="flex items-start gap-3">
            <span className="mt-0.5 inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border border-danger/25 bg-danger/10 text-danger">
              <AlertTriangle size={18} aria-hidden="true" />
            </span>
            <div>
              <h3 className="text-lg font-black text-foreground">{title}</h3>
              <p className="mt-1 text-sm font-semibold leading-6 text-muted-foreground">
                {body}
              </p>
            </div>
          </div>
          <button
            type="button"
            aria-label="关闭"
            onClick={onClose}
            disabled={deleting}
            className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <X size={16} aria-hidden="true" />
          </button>
        </div>
        <div className="p-5">
          <p className="rounded-2xl border border-border bg-surface/80 px-4 py-3 text-sm font-black text-foreground">
            {targetLabel}
          </p>
          <p className="mt-3 text-xs font-semibold leading-5 text-muted-foreground">
            删除只会收敛历史列表，不会修改告警规则，也不会影响后续触发。
          </p>
        </div>
        <div className="soft-modal-footer border-t px-5 py-4">
          <div className="flex gap-2">
            <button
              type="button"
              onClick={onConfirm}
              disabled={deleting}
              className="soft-button min-h-11 flex-1 cursor-pointer bg-danger px-4 text-sm font-black text-white shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-danger/20 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {deleting ? '删除中...' : '确认删除'}
            </button>
            <button
              type="button"
              onClick={onClose}
              disabled={deleting}
              className="soft-button min-h-11 cursor-pointer border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
            >
              取消
            </button>
          </div>
        </div>
      </section>
    </div>
  )
}
