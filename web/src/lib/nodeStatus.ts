import type { Node } from '../types'

// Agent sampling is normally seconds apart; older snapshots must not look live.
export const staleMetricAfterMs = 120_000
export type NodeDisplayStatus = 'online' | 'offline' | 'stale'

export function nodeDisplayStatus(node: Node, now = Date.now()): NodeDisplayStatus {
  if (node.status !== 'online') return 'offline'
  const sampledAt = Date.parse(node.latest_metric?.created_at ?? '')
  return !Number.isFinite(sampledAt) || now - sampledAt > staleMetricAfterMs ? 'stale' : 'online'
}

export function nodeStatusLabel(node: Node) {
  const status = nodeDisplayStatus(node)
  return { online: '在线', offline: '离线', stale: '数据过期' }[status]
}
