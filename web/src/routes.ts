export type AppRoute =
  | { kind: 'node-terminal', nodeID: string }
  | { kind: 'container-exec', nodeID: string, containerID: string }
  | { kind: 'node-detail', nodeID: string }
  | { kind: 'overview' }
  | { kind: 'history' }
  | { kind: 'settings' }
  | { kind: 'alerts' }
  | { kind: 'uptime' }
  | { kind: 'audit' }
  | { kind: 'tasks' }
  | { kind: 'services' }
  | { kind: 'service-detail', serviceID: string }
  | { kind: 'logs' }
  | { kind: 'ai' }
  | { kind: 'k8s-clusters' }
  | { kind: 'k8s-cluster-detail', clusterID: string }
  | { kind: 'dashboard' }

export type AppPage = 'overview' | 'hosts' | 'services' | 'history' | 'settings' | 'alerts' | 'uptime' | 'audit' | 'tasks' | 'logs' | 'k8s' | 'ai'

function decodeRouteID(value?: string) {
  if (!value) return undefined
  try {
    return decodeURIComponent(value)
  } catch {
    return undefined
  }
}

export function nodePath(nodeID: string) {
  return `/nodes/${encodeURIComponent(nodeID)}`
}

export function routeForPath(pathname: string): AppRoute {
  const terminalMatch = pathname.match(/^\/nodes\/([^/]+)\/terminal$/)
  if (terminalMatch) return { kind: 'node-terminal', nodeID: decodeRouteID(terminalMatch[1]) ?? terminalMatch[1] }
  const execMatch = pathname.match(/^\/nodes\/([^/]+)\/containers\/([^/]+)\/exec$/)
  if (execMatch) return { kind: 'container-exec', nodeID: decodeRouteID(execMatch[1]) ?? execMatch[1], containerID: decodeRouteID(execMatch[2]) ?? execMatch[2] }
  const detailMatch = pathname.match(/^\/nodes\/([^/]+)$/)
  if (detailMatch) return { kind: 'node-detail', nodeID: decodeRouteID(detailMatch[1]) ?? detailMatch[1] }
  const serviceDetailMatch = pathname.match(/^\/services\/([^/]+)$/)
  if (serviceDetailMatch) return { kind: 'service-detail', serviceID: decodeRouteID(serviceDetailMatch[1]) ?? serviceDetailMatch[1] }
  if (pathname === '/services') return { kind: 'services' }
  const k8sClusterDetailMatch = pathname.match(/^\/k8s\/clusters\/([^/]+)$/)
  if (k8sClusterDetailMatch) return { kind: 'k8s-cluster-detail', clusterID: decodeRouteID(k8sClusterDetailMatch[1]) ?? k8sClusterDetailMatch[1] }
  if (pathname === '/k8s/clusters') return { kind: 'k8s-clusters' }
  if (pathname === '/history') return { kind: 'history' }
  if (pathname === '/settings') return { kind: 'settings' }
  if (pathname === '/alerts') return { kind: 'alerts' }
  if (pathname === '/uptime') return { kind: 'uptime' }
  if (pathname === '/audit') return { kind: 'audit' }
  if (pathname === '/tasks') return { kind: 'tasks' }
  if (pathname === '/' || pathname === '/overview') return { kind: 'overview' }
  if (pathname === '/logs') return { kind: 'logs' }
  if (pathname === '/ai') return { kind: 'ai' }
  return { kind: 'dashboard' }
}

export function currentRoute(): AppRoute {
  return routeForPath(window.location.pathname)
}

export function pageForRoute(route: AppRoute): AppPage {
  switch (route.kind) {
    case 'overview': return 'overview'
    case 'history': return 'history'
    case 'settings': return 'settings'
    case 'alerts': return 'alerts'
    case 'uptime': return 'uptime'
    case 'audit': return 'audit'
    case 'tasks': return 'tasks'
    case 'services':
    case 'service-detail': return 'services'
    case 'logs': return 'logs'
    case 'ai': return 'ai'
    case 'k8s-clusters':
    case 'k8s-cluster-detail': return 'k8s'
    default: return 'hosts'
  }
}
