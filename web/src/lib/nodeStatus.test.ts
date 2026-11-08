import { describe, expect, test } from 'vitest'
import type { Node } from '../types'
import { nodeDisplayStatus, nodeStatusLabel, staleMetricAfterMs } from './nodeStatus'

const now = Date.parse('2026-11-08T12:00:00Z')
const node = { id: 'alpha', status: 'online', latest_metric: { cpu_usage: 0, created_at: new Date(now).toISOString() } } as Node

describe('node display freshness', () => {
  test('zero usage is still a live snapshot', () => expect(nodeDisplayStatus(node, now)).toBe('online'))
  test('missing, malformed and old samples are stale, not zero or offline', () => {
    expect(nodeDisplayStatus({ ...node, latest_metric: undefined }, now)).toBe('stale')
    expect(nodeDisplayStatus({ ...node, latest_metric: { ...node.latest_metric!, created_at: 'bad' } }, now)).toBe('stale')
    expect(nodeDisplayStatus(node, now + staleMetricAfterMs + 1)).toBe('stale')
  })
  test('offline wins even with a recent sample', () => {
    expect(nodeDisplayStatus({ ...node, status: 'offline' }, now)).toBe('offline')
    expect(nodeStatusLabel({ ...node, status: 'offline' })).toBe('离线')
  })
})
