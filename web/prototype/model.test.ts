import { describe, expect, test } from 'vitest'
import { filterHosts, history, hostCounts, hosts, initialFilters, parseRoute, percent, processes, routeHash } from './model'

describe('prototype data contract', () => {
  test('counts reconcile and all addresses are documentation-only', () => {
    const counts = hostCounts(hosts)
    expect(counts.online + counts.offline + counts.stale).toBe(counts.all)
    expect(hosts.every(h => h.address.startsWith('192.0.2.'))).toBe(true)
    expect(new Set(hosts.map(h => h.id)).size).toBe(hosts.length)
  })
  test('filters by name/address, status and group without changing source ordering', () => {
    expect(filterHosts(hosts, { query: '192.0.2.2', status: 'online', group: '生产' }).map(h => h.id)).toEqual(['node-a1', 'node-a2'])
    expect(filterHosts([...hosts].reverse(), initialFilters).map(h => h.id)).toEqual(hosts.map(h => h.id))
  })
  test('zero, offline and stale values are not interchangeable', () => {
    expect(percent(hosts.find(h => h.id === 'runner-01')!.cpu)).toBe('0.0%')
    for (const host of hosts.filter(h => h.status !== 'online')) {
      expect(percent(host.cpu)).toBe('—')
      expect(history(host, 'cpu')).toEqual([])
      expect(processes(host)).toEqual([])
    }
  })
  test('the last chart sample and displayed metric are one value', () => {
    for (const host of hosts.filter(h => h.status === 'online')) {
      for (const metric of ['cpu', 'memory', 'disk'] as const) {
        const series = history(host, metric)
        expect(series[series.length - 1]).toBe(host[metric])
        expect(series.every(value => value >= 0 && value <= 100)).toBe(true)
      }
    }
  })
  test('deep links preserve the current host section', () => {
    const route = { page: 'host', id: 'node-a2', tab: 'processes' } as const
    expect(parseRoute(routeHash(route))).toEqual(route)
    expect(parseRoute('#/hosts/node-a2/not-a-tab')).toEqual({ ...route, tab: 'overview' })
    expect(parseRoute('')).toEqual({ page: 'hosts' })
  })
})
