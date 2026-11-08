import { nodeDisplayStatus } from '../../lib/nodeStatus'
import { nodePath } from '../../routes'
import type { AlertHistory, ApplicationServiceSummary, K8sCluster, Node, OverviewAttention, UptimeMonitor } from '../../types'

export const sourceStaleMs = 90_000
export function sourceIsStale(source: { error?: string; updatedAt?: number }, now: number) {
  return Boolean(source.error || (source.updatedAt && now - source.updatedAt > sourceStaleMs))
}
export function validTime(value?: string) { const parsed = Date.parse(value ?? ''); return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined }
export function monitorState(monitor: UptimeMonitor, now: number): 'disabled' | 'up' | 'warning' | 'down' | 'unknown' {
  if (!monitor.enabled) return 'disabled'
  const checked = validTime(monitor.last_checked_at)
  const maxAge = Math.max(120, monitor.interval_seconds * 3 + monitor.timeout_seconds) * 1000
  if (!checked || now - checked > maxAge || monitor.status === 'pending') return 'unknown'
  return monitor.status === 'up' || monitor.status === 'down' || monitor.status === 'warning' ? monitor.status : 'unknown'
}
export function clusterState(cluster: K8sCluster, now: number): 'online' | 'offline' | 'unknown' {
  if (cluster.node_status === 'offline' || cluster.health_reason === 'agent_offline') return 'offline'
  const checked = validTime(cluster.health_checked_at)
  if (!checked || now - checked > 120_000) return 'unknown'
  return cluster.status === 'online' && cluster.node_status === 'online' ? 'online' : cluster.status === 'offline' ? 'offline' : 'unknown'
}

export function buildAttention({ nodes, alerts, monitors, services, clusters, now, stale = {} }: {
  nodes: Node[]; alerts: AlertHistory[]; monitors: UptimeMonitor[]; services: ApplicationServiceSummary[]; clusters: K8sCluster[]; now: number
  stale?: Partial<Record<'nodes' | 'alerts' | 'monitors' | 'services' | 'clusters', boolean>>
}): OverviewAttention[] {
  const items: OverviewAttention[] = []
  const byNode = new Map<string, OverviewAttention>()
  for (const node of nodes) {
    const status = nodeDisplayStatus(node, now)
    if (status === 'online' && !stale.nodes) continue
    const item: OverviewAttention = { id: `node:${node.id}`, name: node.name || node.hostname, source: '主机', severity: stale.nodes ? 'unknown' : status === 'offline' ? 'fault' : 'unknown', reason: stale.nodes ? '主机快照未能更新，当前状态待确认' : status === 'offline' ? 'Agent 离线' : '指标过期或尚未上报', timestamp: status === 'offline' ? node.last_seen_at : node.latest_metric?.created_at, timeLabel: status === 'offline' ? '最后在线' : '最后采样', links: [{ label: '查看主机', path: nodePath(node.id) }] }
    items.push(item); byNode.set(node.id, item)
  }
  const activeAlerts = alerts.filter(alert => alert && !alert.resolved_at)
  const groups = new Map<string, AlertHistory[]>()
  for (const alert of activeAlerts) groups.set(alert.node_id, [...(groups.get(alert.node_id) ?? []), alert])
  for (const [id, group] of groups) {
    group.sort((a, b) => (validTime(a.triggered_at) ?? 0) - (validTime(b.triggered_at) ?? 0) || a.id - b.id)
    const label = `${group.length} 条未恢复告警：${group[0].rule_name}${group.length > 1 ? '等' : ''}${stale.alerts ? '（上次结果）' : ''}`
    const link = { label: '查看告警', path: `/alerts?status=active&node=${encodeURIComponent(id)}` }
    const existing = byNode.get(id)
    if (existing) {
      existing.reason += `；${label}`; existing.source = '主机 / 告警'; existing.links.push(link)
      if (!stale.alerts) existing.severity = 'fault'
    } else items.push({ id: `node:${id}`, name: group[0].node_name || id, source: '告警', severity: stale.alerts ? 'unknown' : 'fault', reason: label, timestamp: group[0].triggered_at, timeLabel: '最早触发', links: [link, { label: '查看主机', path: nodePath(id) }] })
  }
  for (const monitor of monitors) {
    const status = monitorState(monitor, now)
    if (status === 'disabled' || (status === 'up' && !stale.monitors)) continue
    const uncertain = stale.monitors || status === 'unknown'
    items.push({ id: `monitor:${monitor.id}`, name: monitor.name, source: '服务拨测', severity: uncertain ? 'unknown' : status === 'down' ? 'fault' : 'warning', reason: uncertain ? '拨测结果过期或尚未检测，当前状态待确认' : status === 'warning' ? 'TLS 证书到期预警' : monitor.last_error || '拨测失败', timestamp: monitor.last_checked_at, timeLabel: '最后检测', links: [{ label: '查看记录', path: `/uptime?monitor=${monitor.id}` }] })
  }
  for (const service of services) {
    if (service.health === 'healthy' && service.resource_count > 0 && !stale.services) continue
    items.push({ id: `service:${service.id}`, name: service.name, source: '应用服务', severity: stale.services ? 'unknown' : service.health === 'unhealthy' ? 'fault' : service.health === 'degraded' ? 'warning' : 'unknown', reason: stale.services ? '服务快照未能更新，当前状态待确认' : service.first_reason || (service.resource_count === 0 ? '尚未关联资源，健康状态未知' : '健康状态未知'), timeLabel: '当前快照', links: [{ label: '查看服务', path: `/services/${encodeURIComponent(service.id)}` }] })
  }
  for (const cluster of clusters) {
    const status = clusterState(cluster, now)
    if (status === 'online' && !stale.clusters) continue
    const reasons: Record<string, string> = { agent_offline: 'Agent 离线，无法连接集群', api_unreachable: 'Kubernetes API 不可达', configuration: '集群凭据或配置异常', authentication: '集群身份验证失败' }
    items.push({ id: `cluster:${cluster.id}`, name: cluster.name, source: 'Kubernetes', severity: stale.clusters || status === 'unknown' ? 'unknown' : 'fault', reason: stale.clusters || status === 'unknown' ? '健康检测过期或尚未完成，状态待确认' : reasons[cluster.health_reason ?? ''] || '集群离线', timestamp: cluster.health_checked_at, timeLabel: '最后检测', links: [{ label: '查看集群', path: `/k8s/clusters/${encodeURIComponent(cluster.id)}` }] })
  }
  const rank = { fault: 0, warning: 1, unknown: 2 }
  return items.sort((a, b) => rank[a.severity] - rank[b.severity] || a.name.localeCompare(b.name, 'zh-CN', { numeric: true }) || a.id.localeCompare(b.id))
}

export function overviewTime(value?: string | null) {
  if (!validTime(value ?? undefined)) return '—'
  return new Date(value!).toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}
