import { describe, expect, test } from 'vitest'

import { nodePath, pageForRoute, routeForPath } from './routes'

describe('route parsing', () => {
  test.each([
    ['/nodes/node%2Fone/terminal', { kind: 'node-terminal', nodeID: 'node/one' }],
    ['/nodes/node%2Fone/containers/container%2Fone/exec', { kind: 'container-exec', nodeID: 'node/one', containerID: 'container/one' }],
    ['/nodes/node%2Fone', { kind: 'node-detail', nodeID: 'node/one' }],
    ['/services/service%2Fone', { kind: 'service-detail', serviceID: 'service/one' }],
    ['/k8s/clusters/cluster%2Fone', { kind: 'k8s-cluster-detail', clusterID: 'cluster/one' }],
    ['/tasks', { kind: 'tasks' }],
    ['/unknown', { kind: 'dashboard' }],
  ])('parses %s', (pathname, expected) => {
    expect(routeForPath(pathname)).toEqual(expected)
  })

  test('preserves malformed encoded route identifiers as their raw value', () => {
    expect(routeForPath('/nodes/%E0%A4%A')).toEqual({ kind: 'node-detail', nodeID: '%E0%A4%A' })
  })

  test.each([
    [{ kind: 'service-detail', serviceID: 'service-1' } as const, 'services'],
    [{ kind: 'k8s-cluster-detail', clusterID: 'cluster-1' } as const, 'k8s'],
    [{ kind: 'node-terminal', nodeID: 'node-1' } as const, 'hosts'],
    [{ kind: 'dashboard' } as const, 'hosts'],
  ])('maps route $route.kind to page $page', (route, page) => {
    expect(pageForRoute(route)).toBe(page)
  })

  test('encodes node identifiers when building detail paths', () => {
    expect(nodePath('node/one')).toBe('/nodes/node%2Fone')
  })
})
