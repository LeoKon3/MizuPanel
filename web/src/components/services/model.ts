import type { ApplicationServiceDetail, ApplicationServiceResource, ApplicationServiceResourceProjection, ServiceEvent } from '../../types'

export const resourceLabels: Record<ApplicationServiceResource['resource_type'], string> = {
  node: '主机', compose_project: 'Compose', systemd_service: 'Systemd', k8s_workload: 'K8s 工作负载',
  uptime_monitor: '服务拨测', alert_rule: '告警规则', scheduled_task: '计划任务'
}
export const resourceTypes = Object.keys(resourceLabels) as ApplicationServiceResource['resource_type'][]
export function resourceKey(item: ApplicationServiceResource) {
  return [item.resource_type, item.scope_id || '', item.resource_kind || '', item.namespace || '', item.resource_key].join('\u0000')
}
export function baseResource(item: ApplicationServiceResource): ApplicationServiceResource {
  return { resource_type: item.resource_type, scope_id: item.scope_id || '', resource_kind: item.resource_kind || '', namespace: item.namespace || '', resource_key: item.resource_key, display_name: item.display_name }
}
export function resourcePath(resource: ApplicationServiceResource) {
  const key = encodeURIComponent(resource.resource_key)
  const scope = encodeURIComponent(resource.scope_id)
  switch (resource.resource_type) {
    case 'node': return `/nodes/${key}`
    case 'compose_project': return `/nodes/${scope}?section=containers&docker=compose`
    case 'systemd_service': return `/nodes/${scope}?section=services&q=${key}`
    case 'k8s_workload': {
      const tab = ({ deployment: 'deployments', statefulset: 'statefulsets', daemonset: 'daemonsets' } as Record<string, string>)[resource.resource_kind] || 'overview'
      return `/k8s/clusters/${scope}?tab=${tab}&namespace=${encodeURIComponent(resource.namespace)}&q=${key}`
    }
    case 'uptime_monitor': return `/uptime?monitor=${key}`
    case 'alert_rule': return '/alerts'
    case 'scheduled_task': return '/tasks'
  }
}
export function resourceIdentity(resource: ApplicationServiceResourceProjection) {
  if (resource.resource_type === 'k8s_workload') return `${resource.namespace} · ${resource.resource_kind}`
  return String(resource.meta?.node_name || resource.scope_id || '—')
}
export function serviceTime(value?: string | null) {
  const date = new Date(value ?? '')
  return !value || Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}
const runLabels: Record<string, string> = { success: '成功', failed: '失败', partial: '部分失败', interrupted: '已中断', running: '执行中', queued: '排队中', skipped: '已跳过' }
export function serviceEvents(service: ApplicationServiceDetail): ServiceEvent[] {
  const events: ServiceEvent[] = [
    ...service.recent_alerts.flatMap(item => [
      { id: `alert-${item.id}`, kind: 'alert' as const, at: item.triggered_at, label: '告警触发', title: item.rule_name, detail: `${item.node_name || item.node_id} · ${item.metric_field} ${item.metric_value}`, path: `/alerts?${item.resolved_at ? '' : 'status=active&'}node=${encodeURIComponent(item.node_id)}`, danger: !item.resolved_at },
      ...(item.resolved_at ? [{ id: `resolved-${item.id}`, kind: 'alert' as const, at: item.resolved_at, label: '告警恢复', title: item.rule_name, detail: item.node_name || item.node_id, path: `/alerts?node=${encodeURIComponent(item.node_id)}`, danger: false }] : [])
    ]),
    ...service.recent_tasks.map(item => ({ id: `task-${item.id}`, kind: 'task' as const, at: item.completed_at || item.created_at, label: '任务执行', title: item.task_name || item.script_name, detail: runLabels[item.status] || item.status, path: `/tasks?tab=runs&run=${item.id}`, danger: ['failed', 'partial', 'interrupted'].includes(item.status) })),
    ...service.recent_audit.map(item => ({ id: `audit-${item.id}`, kind: 'audit' as const, at: item.created_at, label: '相关操作', title: item.summary || `${item.module} · ${item.action}`, detail: item.target_name || item.target_id, path: '/audit', danger: item.result === 'failure' }))
  ]
  return events.sort((a, b) => (Date.parse(b.at) || 0) - (Date.parse(a.at) || 0) || a.id.localeCompare(b.id))
}
