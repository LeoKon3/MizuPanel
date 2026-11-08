// All data is fictional and local. Documentation-only IP ranges cannot identify real hosts.
export type HostStatus = 'online' | 'offline' | 'stale'
export type Metric = 'cpu' | 'memory' | 'disk'
export type Scenario = 'ready' | 'loading' | 'empty' | 'error'
export type HostTab = 'overview' | 'processes' | 'containers' | 'services' | 'files' | 'logs' | 'agent'
export type Route = { page: 'hosts' } | { page: 'host'; id: string; tab: HostTab } | { page: 'ai' } | { page: 'placeholder'; name: string }
export type Host = {
  id: string; name: string; address: string; group: string; status: HostStatus
  cpu: number | null; memory: number | null; disk: number | null
  cores: number; memoryGB: number; diskGB: number; os: string; uptime: string
  note?: string
}
export type ProcessRow = { pid: number; command: string; user: string; cpu: number; memoryMB: number; state: string }
export type Filters = { query: string; status: 'all' | HostStatus; group: string }
export const tabs: { id: HostTab; label: string }[] = [
  { id: 'overview', label: '概览' }, { id: 'processes', label: '进程' },
  { id: 'containers', label: '容器' }, { id: 'services', label: '系统服务' },
  { id: 'files', label: '文件' }, { id: 'logs', label: '日志' }, { id: 'agent', label: 'Agent 管理' }
]
export const hosts: Host[] = [
  { id: 'edge-01', name: 'edge-01', address: '192.0.2.11', group: '边缘', status: 'offline', cpu: null, memory: null, disk: null, cores: 2, memoryGB: 4, diskGB: 80, os: 'Debian 12', uptime: '—', note: '14:18 后未收到 Agent 心跳' },
  { id: 'nas-01', name: 'nas-01', address: '192.0.2.12', group: '实验室', status: 'online', cpu: 6.8, memory: 37.4, disk: 86.2, cores: 4, memoryGB: 16, diskGB: 2048, os: 'Debian 12', uptime: '42 天 8 小时', note: '数据盘使用率超过 85%' },
  { id: 'node-a1', name: 'node-a1', address: '192.0.2.21', group: '生产', status: 'online', cpu: 18.4, memory: 42.6, disk: 34.8, cores: 8, memoryGB: 32, diskGB: 256, os: 'Ubuntu 24.04 LTS', uptime: '18 天 6 小时' },
  { id: 'node-a2', name: 'node-a2', address: '192.0.2.22', group: '生产', status: 'online', cpu: 7.1, memory: 28.3, disk: 21.5, cores: 8, memoryGB: 32, diskGB: 256, os: 'Ubuntu 24.04 LTS', uptime: '18 天 6 小时' },
  { id: 'runner-01', name: 'runner-01', address: '192.0.2.31', group: '实验室', status: 'online', cpu: 0, memory: 12.1, disk: 19.6, cores: 4, memoryGB: 8, diskGB: 128, os: 'Ubuntu 24.04 LTS', uptime: '3 天 2 小时' },
  { id: 'staging-01', name: 'staging-01', address: '192.0.2.32', group: '实验室', status: 'stale', cpu: null, memory: null, disk: null, cores: 4, memoryGB: 8, diskGB: 128, os: 'Rocky Linux 9', uptime: '—', note: 'Agent 已连接，指标已有 4 分钟未更新' },
  { id: 'web-01', name: 'web-01', address: '192.0.2.41', group: '生产', status: 'online', cpu: 12.8, memory: 31.6, disk: 23.7, cores: 4, memoryGB: 8, diskGB: 128, os: 'Ubuntu 24.04 LTS', uptime: '12 天 4 小时' },
  { id: 'web-02', name: 'web-02', address: '192.0.2.42', group: '生产', status: 'online', cpu: 9.3, memory: 29.2, disk: 22.8, cores: 4, memoryGB: 8, diskGB: 128, os: 'Ubuntu 24.04 LTS', uptime: '12 天 4 小时' }
]
export function filterHosts(rows: Host[], filters: Filters): Host[] {
  const query = filters.query.trim().toLowerCase()
  return rows.filter(host =>
    (!query || `${host.name} ${host.address}`.toLowerCase().includes(query)) &&
    (filters.status === 'all' || host.status === filters.status) &&
    (!filters.group || host.group === filters.group)
  ).sort((a, b) => a.name.localeCompare(b.name, 'en'))
}
export function hostCounts(rows: Host[]) {
  return { all: rows.length, online: rows.filter(h => h.status === 'online').length,
    offline: rows.filter(h => h.status === 'offline').length, stale: rows.filter(h => h.status === 'stale').length }
}
export function percent(value: number | null) { return value === null ? '—' : `${value.toFixed(1)}%` }
export function history(host: Host, metric: Metric): number[] {
  const value = host[metric]
  if (value === null) return []
  const pattern = metric === 'cpu' ? [0.54, 0.59, 0.62, 0.58, 0.76, 0.65, 0.72, 1.48, 0.91, 0.68, 0.86, 0.94, 1]
    : metric === 'memory' ? [0.91, 0.91, 0.92, 0.93, 0.92, 0.94, 0.97, 0.97, 0.96, 0.98, 0.99, 1, 1] : [0.97, 0.97, 0.97, 0.98, 0.98, 0.98, 0.98, 0.99, 0.99, 0.99, 1, 1, 1]
  return pattern.map(factor => Math.round(Math.min(100, value * factor) * 10) / 10)
}
export function processes(host: Host): ProcessRow[] {
  if (host.status !== 'online') return []
  const offset = hosts.findIndex(h => h.id === host.id) * 100
  return [
    { pid: 1201 + offset, command: 'containerd', user: 'root', cpu: 4.8, memoryMB: 286.4, state: '运行中' },
    { pid: 1804 + offset, command: 'mizupanel-agent', user: 'root', cpu: 1.2, memoryMB: 38.6, state: '运行中' },
    { pid: 908 + offset, command: 'systemd-journald', user: 'root', cpu: 0.4, memoryMB: 72.1, state: '休眠' },
    { pid: 2216 + offset, command: 'nginx: worker process', user: 'www-data', cpu: 0.2, memoryMB: 12.7, state: '休眠' },
    { pid: 692 + offset, command: '/usr/sbin/sshd -D', user: 'root', cpu: 0, memoryMB: 8.2, state: '休眠' },
    { pid: 1, command: '/sbin/init', user: 'root', cpu: 0, memoryMB: 18.4, state: '休眠' }
  ].map(p => host.cpu === 0 ? { ...p, cpu: 0 } : p)
}
export function parseRoute(hash: string): Route {
  const parts = hash.replace(/^#\/?/, '').split('/')
  if (parts[0] === 'hosts' && parts[1]) {
    return { page: 'host', id: parts[1], tab: tabs.find(t => t.id === parts[2])?.id ?? 'overview' }
  }
  if (parts[0] === 'ai') return { page: 'ai' }
  if (parts[0] === 'section' && parts[1]) return { page: 'placeholder', name: parts[1] }
  return { page: 'hosts' }
}
export function routeHash(route: Route) {
  if (route.page === 'host') return `#/hosts/${route.id}/${route.tab}`
  if (route.page === 'ai') return '#/ai'
  if (route.page === 'placeholder') return `#/section/${route.name}`
  return '#/hosts'
}
export const initialFilters: Filters = { query: '', status: 'all', group: '' }
