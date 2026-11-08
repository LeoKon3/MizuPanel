import { describe, expect, test } from 'vitest'
import type { AlertHistory, ApplicationServiceSummary, K8sCluster, Node, UptimeMonitor } from '../../types'
import { buildAttention, clusterState, monitorState, sourceIsStale } from './model'

const now = Date.parse('2026-11-08T12:00:00Z')
const checked = new Date(now - 1000).toISOString()
const node = { id: 'node/a', name: 'Alpha', hostname: 'a', status: 'offline', last_seen_at: checked } as Node
const alert = { id: 1, node_id: node.id, node_name: node.name, rule_name: 'CPU High', triggered_at: checked } as AlertHistory
const monitor = { id: 1, name: 'Website', enabled: true, interval_seconds: 60, timeout_seconds: 5, status: 'up', last_checked_at: checked } as UptimeMonitor
const cluster = { id: 'cluster/a', name: 'Cluster', status: 'online', node_status: 'online', health_checked_at: checked } as K8sCluster
const empty = { nodes: [], alerts: [], monitors: [], services: [], clusters: [], now }

describe('overview evidence model', () => {
  test('combines alerts and host state by identity, with independent evidence links', () => {
    const items = buildAttention({ ...empty, nodes: [node], alerts: [alert, { ...alert, id: 2 }, { ...alert, id: 3, resolved_at: checked }] })
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({ id: 'node:node/a', source: '主机 / 告警', severity: 'fault' })
    expect(items[0].reason).toContain('2 条未恢复告警')
    expect(items[0].links.map(link => link.path)).toEqual(['/nodes/node%2Fa', '/alerts?status=active&node=node%2Fa'])
  })
  test('does not call old, disabled, or missing probe data healthy', () => {
    expect(monitorState(monitor, now)).toBe('up')
    expect(monitorState({ ...monitor, last_checked_at: undefined }, now)).toBe('unknown')
    expect(monitorState({ ...monitor, last_checked_at: '2026-11-01T00:00:00Z' }, now)).toBe('unknown')
    expect(monitorState({ ...monitor, enabled: false, status: 'down' }, now)).toBe('disabled')
    expect(buildAttention({ ...empty, monitors: [{ ...monitor, enabled: false, status: 'down' }] })).toEqual([])
  })
  test('Kubernetes requires fresh API evidence and an online Agent', () => {
    expect(clusterState(cluster, now)).toBe('online')
    expect(clusterState({ ...cluster, node_status: 'offline' }, now)).toBe('offline')
    expect(clusterState({ ...cluster, health_checked_at: undefined }, now)).toBe('unknown')
    expect(clusterState({ ...cluster, health_checked_at: '0001-01-01T00:00:00Z' }, now)).toBe('unknown')
    expect(clusterState({ ...cluster, status: 'unknown' }, now)).toBe('unknown')
  })
  test('failed sources cannot retain a positive health conclusion', () => {
    const onlineNode = { ...node, status: 'online', latest_metric: { created_at: checked } } as Node
    const service = { id: 'service/a', name: 'API', health: 'healthy', resource_count: 1 } as ApplicationServiceSummary
    const items = buildAttention({ ...empty, nodes: [onlineNode], monitors: [monitor], clusters: [cluster], services: [service], stale: { nodes: true, monitors: true, clusters: true, services: true } })
    expect(items).toHaveLength(4)
    expect(items.every(item => item.severity === 'unknown')).toBe(true)
    expect(sourceIsStale({ updatedAt: now - 100_000 }, now)).toBe(true)
    expect(sourceIsStale({ updatedAt: now, error: 'failed' }, now)).toBe(true)
  })
  test('known failures sort first, names stay stable, and empty service scope is unknown', () => {
    const service = { id: 'service/a', name: 'API', health: 'healthy', resource_count: 0 } as ApplicationServiceSummary
    const items = buildAttention({ ...empty, nodes: [node], monitors: [{ ...monitor, status: 'warning' }], services: [service] })
    expect(items.map(item => item.severity)).toEqual(['fault', 'warning', 'unknown'])
    expect(items[2].links[0].path).toBe('/services/service%2Fa')
  })
})
