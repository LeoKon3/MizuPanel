import type {
  K8sClustersResponse,
  ConnectK8sClusterRequest,
  ConnectK8sClusterResponse,
  K8sPodsResponse,
  K8sPodLogsResponse,
  K8sCluster,
  K8sSummaryResponse,
  K8sNamespacesResponse,
  K8sNodesResponse,
  K8sDeploymentsResponse,
  K8sStatefulSetsResponse,
  K8sDaemonSetsResponse,
  K8sServicesResponse,
  K8sIngressesResponse,
  K8sDiagnosticsResponse,
  K8sResourceActionRequest,
  K8sResourceActionResponse,
  K8sApplyManifestRequest,
  K8sApplyManifestResponse,
  K8sResourceKind
} from '../types'

export class K8sAPIError extends Error {
  constructor(public status: number, message = `Request failed: ${status}`) {
    super(message)
  }
}

async function errorMessage(response: Response): Promise<string> {
  try {
    const body = await response.json() as { error?: string }
    return body.error || `Request failed: ${response.status}`
  } catch {
    return `Request failed: ${response.status}`
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = init === undefined ? await fetch(path) : await fetch(path, init)
  if (!response.ok) {
    const error = new K8sAPIError(response.status, await errorMessage(response))
    throw error
  }
  return response.json() as Promise<T>
}

async function requestVoid(path: string, init?: RequestInit): Promise<void> {
  const response = init === undefined ? await fetch(path) : await fetch(path, init)
  if (!response.ok) {
    const error = new K8sAPIError(response.status, await errorMessage(response))
    throw error
  }
}

export function fetchK8sClusters(signal?: AbortSignal): Promise<K8sClustersResponse> {
  return request<K8sClustersResponse>('/api/k8s/clusters', signal ? { signal } : undefined)
}

export function connectK8sCluster(req: ConnectK8sClusterRequest): Promise<ConnectK8sClusterResponse> {
  return request<ConnectK8sClusterResponse>('/api/k8s/clusters', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req)
  })
}

export function fetchK8sCluster(clusterID: string): Promise<{ cluster: K8sCluster }> {
  return request<{ cluster: K8sCluster }>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}`)
}

export function deleteK8sCluster(clusterID: string): Promise<void> {
  return requestVoid(`/api/k8s/clusters/${encodeURIComponent(clusterID)}`, { method: 'DELETE' })
}

function namespaceQuery(namespace?: string): string {
  return namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
}

export function fetchK8sSummary(clusterID: string): Promise<K8sSummaryResponse> {
  return request<K8sSummaryResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/summary`)
}

export function fetchK8sNamespaces(clusterID: string, signal?: AbortSignal): Promise<K8sNamespacesResponse> {
  return request<K8sNamespacesResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/namespaces`, signal ? { signal } : undefined)
}

export function fetchK8sNodes(clusterID: string): Promise<K8sNodesResponse> {
  return request<K8sNodesResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/nodes`)
}

export function fetchK8sDeployments(clusterID: string, namespace?: string): Promise<K8sDeploymentsResponse> {
  return request<K8sDeploymentsResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/deployments${namespaceQuery(namespace)}`)
}

export function fetchK8sStatefulSets(clusterID: string, namespace?: string): Promise<K8sStatefulSetsResponse> {
  return request<K8sStatefulSetsResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/statefulsets${namespaceQuery(namespace)}`)
}

export function fetchK8sDaemonSets(clusterID: string, namespace?: string): Promise<K8sDaemonSetsResponse> {
  return request<K8sDaemonSetsResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/daemonsets${namespaceQuery(namespace)}`)
}

export function fetchK8sServices(clusterID: string, namespace?: string): Promise<K8sServicesResponse> {
  return request<K8sServicesResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/services${namespaceQuery(namespace)}`)
}

export function fetchK8sIngresses(clusterID: string, namespace?: string): Promise<K8sIngressesResponse> {
  return request<K8sIngressesResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/ingresses${namespaceQuery(namespace)}`)
}

export function fetchK8sPods(clusterID: string, namespace?: string, signal?: AbortSignal): Promise<K8sPodsResponse> {
  return request<K8sPodsResponse>(`/api/k8s/clusters/${encodeURIComponent(clusterID)}/pods${namespaceQuery(namespace)}`, signal ? { signal } : undefined)
}

export function fetchK8sDiagnostics(
  clusterID: string,
  kind: K8sResourceKind,
  namespace: string,
  name: string
): Promise<K8sDiagnosticsResponse> {
  return request<K8sDiagnosticsResponse>(
    `/api/k8s/clusters/${encodeURIComponent(clusterID)}/resources/${encodeURIComponent(kind)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/diagnostics`
  )
}

export function executeK8sResourceAction(
  clusterID: string,
  kind: K8sResourceKind,
  namespace: string,
  name: string,
  req: K8sResourceActionRequest
): Promise<K8sResourceActionResponse> {
  return request<K8sResourceActionResponse>(
    `/api/k8s/clusters/${encodeURIComponent(clusterID)}/resources/${encodeURIComponent(kind)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/actions`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req)
    }
  )
}

export function applyK8sManifest(
  clusterID: string,
  req: K8sApplyManifestRequest
): Promise<K8sApplyManifestResponse> {
  return request<K8sApplyManifestResponse>(
    `/api/k8s/clusters/${encodeURIComponent(clusterID)}/resources/apply`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req)
    }
  )
}

export function fetchK8sPodLogs(
  clusterID: string,
  namespace: string,
  podName: string,
  container?: string,
  follow = false,
  tailLines = 100,
  signal?: AbortSignal
): Promise<K8sPodLogsResponse> {
  const params = new URLSearchParams()
  if (container) params.set('container', container)
  if (follow) params.set('follow', 'true')
  params.set('tail_lines', tailLines.toString())

  const queryString = params.toString()
  return request<K8sPodLogsResponse>(
    `/api/k8s/clusters/${encodeURIComponent(clusterID)}/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(podName)}/logs?${queryString}`,
    signal ? { signal } : undefined
  )
}
