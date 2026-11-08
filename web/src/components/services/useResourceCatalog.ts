import { useEffect, useRef, useState } from 'react'
import { getAlertRules, getK8sClusters, getNodeDockerCompose, getNodes, getNodeSystemdServices, getScheduledTasks, getUptimeMonitors } from '../../api/client'
import { fetchK8sDaemonSets, fetchK8sDeployments, fetchK8sStatefulSets } from '../../api/k8s'
import type { ApplicationServiceResource, K8sCluster, Node, ServiceResourceOption } from '../../types'
import { resourceKey } from './model'

type Type = ApplicationServiceResource['resource_type']
function option(type: Type, key: string, label: string, detail: string, scope = '', kind = '', namespace = ''): ServiceResourceOption {
  const resource: ApplicationServiceResource = { resource_type: type, resource_key: key, display_name: label, scope_id: scope, resource_kind: kind, namespace }
  return { key: resourceKey(resource), label, detail, resource }
}

export function useResourceCatalog() {
  const [nodes, setNodes] = useState<Node[]>([])
  const [clusters, setClusters] = useState<K8sCluster[]>([])
  const [cache, setCache] = useState<Record<string, ServiceResourceOption[]>>({})
  const [busy, setBusy] = useState<Record<string, boolean>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const active = useRef(false)
  const requests = useRef(new Map<string, AbortController>())
  const load = async (key: string, fetcher: (signal: AbortSignal) => Promise<ServiceResourceOption[]>) => {
    if (!active.current || requests.current.has(key)) return
    const controller = new AbortController()
    requests.current.set(key, controller)
    setBusy(state => ({ ...state, [key]: true }))
    setErrors(state => ({ ...state, [key]: '' }))
    let timeout: ReturnType<typeof setTimeout> | undefined
    let abort: (() => void) | undefined
    try {
      const deadline = new Promise<never>((_, reject) => {
        abort = () => reject(new DOMException('请求已取消', 'AbortError'))
        controller.signal.addEventListener('abort', abort, { once: true })
        timeout = setTimeout(() => { reject(new Error('资源读取超时，请重试')); controller.abort() }, key.includes(':') ? 40_000 : 15_000)
      })
      const result = await Promise.race([fetcher(controller.signal), deadline])
      if (!active.current || requests.current.get(key) !== controller) return
      setCache(state => ({ ...state, [key]: result }))
    } catch (error) {
      if (active.current && requests.current.get(key) === controller) setErrors(state => ({ ...state, [key]: error instanceof Error ? error.message : '资源读取失败' }))
    } finally {
      clearTimeout(timeout)
      if (abort) controller.signal.removeEventListener('abort', abort)
      if (requests.current.get(key) === controller) {
        requests.current.delete(key)
        if (active.current) setBusy(state => ({ ...state, [key]: false }))
      }
    }
  }
  const fetchStatic = async (type: Type | 'clusters', signal: AbortSignal): Promise<ServiceResourceOption[]> => {
    switch (type) {
      case 'node': {
        const result = await getNodes(signal)
        if (!signal.aborted) setNodes(result.nodes || [])
        return (result.nodes || []).map(node => option('node', node.id, node.name || node.hostname || node.id, `${node.ip || '—'} · ${node.status === 'online' ? '在线' : '离线'}`))
      }
      case 'clusters': {
        const result = await getK8sClusters(signal)
        if (!signal.aborted) setClusters(result.clusters || [])
        return []
      }
      case 'uptime_monitor': return ((await getUptimeMonitors(signal)).monitors || []).map(item => option(type, String(item.id), item.name, item.target))
      case 'alert_rule': return ((await getAlertRules(signal)).rules || []).map(item => option(type, String(item.id), item.name, item.enabled ? '已启用' : '已禁用'))
      case 'scheduled_task': return ((await getScheduledTasks(signal)).tasks || []).map(item => option(type, String(item.id), item.name, item.cron_expression))
      default: return []
    }
  }
  const retryStatic = (type: Type | 'clusters') => load(type, signal => fetchStatic(type, signal))
  useEffect(() => {
    active.current = true
    for (const type of ['node', 'clusters', 'uptime_monitor', 'alert_rule', 'scheduled_task'] as const) void retryStatic(type)
    return () => { active.current = false; requests.current.forEach(controller => controller.abort()); requests.current.clear() }
  }, [])

  const loadRemote = (type: Type, scope: string, kind = 'deployment') => {
    if (!scope) return Promise.resolve()
    const key = `${type}:${scope}:${type === 'k8s_workload' ? kind : ''}`
    return load(key, async signal => {
      if (type === 'compose_project') {
        const response = await getNodeDockerCompose(scope, signal)
        if (response.error || !response.success || !response.supported) throw new Error(response.error || '此主机暂不支持 Compose 查询')
        return (response.projects || []).map(item => {
          const managed = item.management === 'managed' && Boolean(item.managed_project_id)
          return option(type, managed ? item.managed_project_id! : item.name, item.display_name || item.name, `${scope} · ${managed ? '托管项目' : '外部项目'}`, scope, managed ? 'managed' : 'external')
        })
      }
      if (type === 'systemd_service') {
        const response = await getNodeSystemdServices(scope, signal)
        if (response.error || !response.success || !response.supported) throw new Error(response.error || '此主机暂不支持 Systemd 查询')
        return (response.services || []).map(item => option(type, item.name, item.description || item.name, item.name, scope))
      }
      if (type === 'k8s_workload') {
        const response = kind === 'statefulset' ? await fetchK8sStatefulSets(scope, undefined, signal) : kind === 'daemonset' ? await fetchK8sDaemonSets(scope, undefined, signal) : await fetchK8sDeployments(scope, undefined, signal)
        if (!response.success) throw new Error('Kubernetes 资源暂不可用')
        const items = 'deployments' in response ? response.deployments : 'statefulsets' in response ? response.statefulsets : 'daemonsets' in response ? response.daemonsets : []
        return (items || []).map(item => option(type, item.name, item.name, `${item.namespace} · ${kind}`, scope, kind, item.namespace))
      }
      return []
    })
  }
  return { nodes, clusters, cache, busy, errors, retryStatic, loadRemote }
}
