import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import {
  fetchK8sCluster,
  fetchK8sClusters,
  fetchK8sDaemonSets,
  fetchK8sDeployments,
  fetchK8sIngresses,
  fetchK8sNamespaces,
  fetchK8sNodes,
  fetchK8sPods,
  fetchK8sServices,
  fetchK8sStatefulSets,
  fetchK8sSummary,
} from '../api/k8s'
import type { K8sCluster } from '../types'
import { K8sClusterDetailPage } from './K8sClusterDetailPage'
import { K8sClustersPage } from './K8sClustersPage'

vi.mock('../api/k8s', () => ({
  fetchK8sCluster: vi.fn(),
  fetchK8sClusters: vi.fn(),
  fetchK8sSummary: vi.fn(),
  fetchK8sNamespaces: vi.fn(),
  fetchK8sNodes: vi.fn(),
  fetchK8sPods: vi.fn(),
  fetchK8sDeployments: vi.fn(),
  fetchK8sStatefulSets: vi.fn(),
  fetchK8sDaemonSets: vi.fn(),
  fetchK8sServices: vi.fn(),
  fetchK8sIngresses: vi.fn(),
  fetchK8sDiagnostics: vi.fn(),
  fetchK8sPodLogs: vi.fn(),
  executeK8sResourceAction: vi.fn(),
  applyK8sManifest: vi.fn(),
  deleteK8sCluster: vi.fn(),
}))

const cluster: K8sCluster = {
  id: 'cluster-1',
  name: 'prod-east',
  node_id: 'node-1',
  node_name: 'master-1',
  node_ip: '10.0.0.10',
  node_status: 'online',
  node_last_seen_at: '2026-06-20T02:24:13Z',
  context: 'prod-context',
  status: 'online',
  version: 'v1.30.0',
  node_count: 2,
  namespace_count: 3,
  last_seen_at: '2026-06-20T02:24:13Z',
  created_at: '2026-06-20T02:20:00Z',
  updated_at: '2026-06-20T02:20:00Z',
}

function mockDetailAPI() {
  vi.mocked(fetchK8sCluster).mockResolvedValue({ cluster })
  vi.mocked(fetchK8sSummary).mockResolvedValue({
    success: true,
    summary: {
      version: 'v1.30.0',
      node_count: 2,
      namespace_count: 3,
      pod_count: 2,
      deployment_count: 2,
      statefulset_count: 0,
      daemonset_count: 0,
      service_count: 0,
      ingress_count: 0,
    },
  })
  vi.mocked(fetchK8sNamespaces).mockResolvedValue({
    success: true,
    namespaces: [
      { name: 'default', status: 'Active', age: '10d' },
      { name: 'payments', status: 'Active', age: '8d' },
      { name: 'kube-system', status: 'Active', age: '30d' },
    ],
  })
  vi.mocked(fetchK8sNodes).mockResolvedValue({ success: true, nodes: [] })
  vi.mocked(fetchK8sPods).mockResolvedValue({
    success: true,
    pods: [
      { name: 'worker-main', namespace: 'payments', status: 'Running', ready: '1/1', restarts: 0, age: '2d', node: 'node-a', ip: '10.42.0.11' },
      { name: 'edge-gateway', namespace: 'default', status: 'Pending', ready: '0/1', restarts: 1, age: '1h', node: 'node-b', ip: '10.42.0.12' },
    ],
  })
  vi.mocked(fetchK8sDeployments).mockResolvedValue({
    success: true,
    deployments: [
      { name: 'api-server', namespace: 'payments', ready: '2/2', up_to_date: 2, available: 2, age: '5d' },
      { name: 'web-console', namespace: 'default', ready: '1/1', up_to_date: 1, available: 1, age: '1d' },
    ],
  })
  vi.mocked(fetchK8sStatefulSets).mockResolvedValue({ success: true, statefulsets: [] })
  vi.mocked(fetchK8sDaemonSets).mockResolvedValue({ success: true, daemonsets: [] })
  vi.mocked(fetchK8sServices).mockResolvedValue({ success: true, services: [] })
  vi.mocked(fetchK8sIngresses).mockResolvedValue({ success: true, ingresses: [] })
}

async function clickDetailTab(label: string) {
  const tabLabels = await screen.findAllByText(label)
  const tabButton = tabLabels.map((item) => item.closest('button')).find(Boolean)
  expect(tabButton).not.toBeNull()
  fireEvent.click(tabButton as HTMLButtonElement)
}

describe('K8s filtering UX', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.history.replaceState({}, '', '/')
    mockDetailAPI()
  })

  test('initializes workload tab, namespace, and search from a service deep link', async () => {
    window.history.replaceState({}, '', '/k8s/clusters/cluster-1?tab=deployment&namespace=payments&q=api-server')
    render(<K8sClusterDetailPage clusterId="cluster-1" onBack={vi.fn()} />)

    expect(await screen.findByText('api-server')).toBeInTheDocument()
    expect(screen.queryByText('web-console')).not.toBeInTheDocument()
    expect(screen.getByPlaceholderText(/搜索 Deployment/)).toHaveValue('api-server')
    await waitFor(() => expect(fetchK8sDeployments).toHaveBeenCalledWith('cluster-1', 'payments'))
  })

  test('Pod resource search does not match namespace text', async () => {
    render(<K8sClusterDetailPage clusterId="cluster-1" onBack={vi.fn()} />)

    await clickDetailTab('Pods')
    expect(await screen.findByText('worker-main')).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText(/搜索 Pod/), { target: { value: 'payments' } })

    await waitFor(() => expect(screen.queryByText('worker-main')).not.toBeInTheDocument())
    expect(screen.getByText('没有匹配的 Pod')).toBeInTheDocument()
  })

  test('namespace filter is a searchable dropdown instead of a free text field', async () => {
    render(<K8sClusterDetailPage clusterId="cluster-1" onBack={vi.fn()} />)

    await clickDetailTab('Pods')

    expect(screen.queryByPlaceholderText('命名空间 (留空查看全部)')).not.toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: /全部命名空间/ }))
    const namespaceSearch = await screen.findByPlaceholderText('搜索命名空间')
    fireEvent.change(namespaceSearch, { target: { value: 'kube' } })

    expect(screen.getByRole('option', { name: 'kube-system Active' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'payments Active' })).not.toBeInTheDocument()
  })

  test('namespace dropdown closes with Escape', async () => {
    render(<K8sClusterDetailPage clusterId="cluster-1" onBack={vi.fn()} />)

    await clickDetailTab('Pods')
    fireEvent.click(await screen.findByRole('button', { name: /全部命名空间/ }))
    expect(await screen.findByPlaceholderText('搜索命名空间')).toBeInTheDocument()

    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByPlaceholderText('搜索命名空间')).not.toBeInTheDocument())
  })

  test('Deployment resource search matches names but not namespace text', async () => {
    render(<K8sClusterDetailPage clusterId="cluster-1" onBack={vi.fn()} />)

    await clickDetailTab('Deployments')
    expect(await screen.findByText('api-server')).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText(/搜索 Deployment/), { target: { value: 'payments' } })

    await waitFor(() => expect(screen.queryByText('api-server')).not.toBeInTheDocument())
    expect(screen.getByText('没有匹配的 Deployment')).toBeInTheDocument()
  })

  test('cluster list can search by agent IP', async () => {
    vi.mocked(fetchK8sClusters).mockResolvedValue({
      clusters: [
        cluster,
        {
          ...cluster,
          id: 'cluster-2',
          name: 'staging-west',
          node_name: 'edge-agent',
          node_ip: '10.0.0.22',
          context: 'staging',
        },
      ],
    })

    render(<K8sClustersPage onConnectCluster={vi.fn()} />)

    expect(await screen.findByText('prod-east')).toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText('搜索集群、Agent、IP、Context'), { target: { value: '10.0.0.22' } })

    const clusterGrid = screen.getByTestId('k8s-cluster-grid')
    expect(within(clusterGrid).getByText('staging-west')).toBeInTheDocument()
    expect(within(clusterGrid).queryByText('prod-east')).not.toBeInTheDocument()
  })
})
