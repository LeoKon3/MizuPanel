import { useEffect, useMemo, useRef, useState } from 'react'
import { Boxes, CloudCog, LoaderCircle, Server, X } from 'lucide-react'

import { getAlertRules, getK8sClusters, getNodeDockerCompose, getNodeSystemdServices, getNodes, getScheduledTasks, getUptimeMonitors } from '../../api/client'
import { fetchK8sDaemonSets, fetchK8sDeployments, fetchK8sStatefulSets } from '../../api/k8s'
import type { AlertRule, ApplicationServiceDetail, ApplicationServiceInput, ApplicationServiceResource, DockerComposeProject, K8sCluster, Node, ScheduledTask, SystemdService, UptimeMonitor } from '../../types'

type EditorProps = {
  service?: ApplicationServiceDetail
  saving: boolean
  onClose: () => void
  onSave: (input: ApplicationServiceInput) => Promise<void>
}

type K8sKind = 'deployment' | 'statefulset' | 'daemonset'
type K8sOption = { name: string, namespace: string }

const resourceLabels: Record<ApplicationServiceResource['resource_type'], string> = {
  node: '节点',
  compose_project: 'Compose 项目',
  systemd_service: 'Systemd 服务',
  k8s_workload: 'Kubernetes 工作负载',
  uptime_monitor: '服务拨测',
  alert_rule: '告警规则',
  scheduled_task: '计划任务'
}

export function ServiceEditorModal({ service, saving, onClose, onSave }: EditorProps) {
  const [name, setName] = useState(service?.name || '')
  const [description, setDescription] = useState(service?.description || '')
  const [selected, setSelected] = useState<ApplicationServiceResource[]>(() => (service?.resources || []).map(baseResource))
  const [nodes, setNodes] = useState<Node[]>([])
  const [monitors, setMonitors] = useState<UptimeMonitor[]>([])
  const [rules, setRules] = useState<AlertRule[]>([])
  const [tasks, setTasks] = useState<ScheduledTask[]>([])
  const [clusters, setClusters] = useState<K8sCluster[]>([])
  const [initialLoading, setInitialLoading] = useState(true)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState('')
  const [composeNode, setComposeNode] = useState('')
  const [systemdNode, setSystemdNode] = useState('')
  const [k8sCluster, setK8sCluster] = useState('')
  const [k8sKind, setK8sKind] = useState<K8sKind>('deployment')
  const [composeCache, setComposeCache] = useState<Record<string, DockerComposeProject[]>>({})
  const [systemdCache, setSystemdCache] = useState<Record<string, SystemdService[]>>({})
  const [k8sCache, setK8sCache] = useState<Record<string, K8sOption[]>>({})
  const [remoteLoading, setRemoteLoading] = useState('')
  const dialogRef = useRef<HTMLDivElement>(null)
  const composeRequestSequence = useRef(0)
  const systemdRequestSequence = useRef(0)
  const k8sRequestSequence = useRef(0)
  const composeNodeRef = useRef(composeNode)
  const systemdNodeRef = useRef(systemdNode)
  const k8sScopeRef = useRef(`${k8sCluster}:${k8sKind}`)
  composeNodeRef.current = composeNode
  systemdNodeRef.current = systemdNode
  k8sScopeRef.current = `${k8sCluster}:${k8sKind}`

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      const results = await Promise.allSettled([getNodes(), getUptimeMonitors(), getAlertRules(), getScheduledTasks(), getK8sClusters()])
      if (cancelled) return
      const [nodeResult, monitorResult, ruleResult, taskResult, clusterResult] = results
      if (nodeResult.status === 'fulfilled') {
        setNodes(nodeResult.value.nodes || [])
        setComposeNode(nodeResult.value.nodes?.[0]?.id || '')
        setSystemdNode(nodeResult.value.nodes?.[0]?.id || '')
      } else setErrors((current) => ({ ...current, node: message(nodeResult.reason) }))
      if (monitorResult.status === 'fulfilled') setMonitors(monitorResult.value.monitors || [])
      else setErrors((current) => ({ ...current, uptime_monitor: message(monitorResult.reason) }))
      if (ruleResult.status === 'fulfilled') setRules(ruleResult.value.rules || [])
      else setErrors((current) => ({ ...current, alert_rule: message(ruleResult.reason) }))
      if (taskResult.status === 'fulfilled') setTasks(taskResult.value.tasks || [])
      else setErrors((current) => ({ ...current, scheduled_task: message(taskResult.reason) }))
      if (clusterResult.status === 'fulfilled') {
        setClusters(clusterResult.value.clusters || [])
        setK8sCluster(clusterResult.value.clusters?.[0]?.id || '')
      } else setErrors((current) => ({ ...current, k8s_workload: message(clusterResult.reason) }))
      setInitialLoading(false)
    }
    void load()
    return () => { cancelled = true }
  }, [])

  useDialog(dialogRef, saving, onClose)

  const selectedKeys = useMemo(() => new Set(selected.map(resourceKey)), [selected])
  const toggle = (resource: ApplicationServiceResource) => {
    const key = resourceKey(resource)
    setSelected((current) => current.some((item) => resourceKey(item) === key) ? current.filter((item) => resourceKey(item) !== key) : [...current, resource])
  }

  const loadCompose = async () => {
    if (!composeNode || composeCache[composeNode]) return
    const nodeID = composeNode
    const loadingKey = `compose:${nodeID}`
    const requestID = ++composeRequestSequence.current
    setRemoteLoading(loadingKey)
    setErrors((current) => ({ ...current, compose_project: '' }))
    try {
      const response = await getNodeDockerCompose(nodeID)
      if (response.error) throw new Error(response.error)
      if (requestID !== composeRequestSequence.current || composeNodeRef.current !== nodeID) return
      setComposeCache((current) => ({ ...current, [nodeID]: response.projects || [] }))
    } catch (error) {
      if (requestID !== composeRequestSequence.current || composeNodeRef.current !== nodeID) return
      setErrors((current) => ({ ...current, compose_project: message(error) }))
    } finally { setRemoteLoading((current) => current === loadingKey ? '' : current) }
  }

  const loadSystemd = async () => {
    if (!systemdNode || systemdCache[systemdNode]) return
    const nodeID = systemdNode
    const loadingKey = `systemd:${nodeID}`
    const requestID = ++systemdRequestSequence.current
    setRemoteLoading(loadingKey)
    setErrors((current) => ({ ...current, systemd_service: '' }))
    try {
      const response = await getNodeSystemdServices(nodeID)
      if (response.error) throw new Error(response.error)
      if (requestID !== systemdRequestSequence.current || systemdNodeRef.current !== nodeID) return
      setSystemdCache((current) => ({ ...current, [nodeID]: response.services || [] }))
    } catch (error) {
      if (requestID !== systemdRequestSequence.current || systemdNodeRef.current !== nodeID) return
      setErrors((current) => ({ ...current, systemd_service: message(error) }))
    } finally { setRemoteLoading((current) => current === loadingKey ? '' : current) }
  }

  const loadK8s = async () => {
    const cacheKey = `${k8sCluster}:${k8sKind}`
    if (!k8sCluster || k8sCache[cacheKey]) return
    const clusterID = k8sCluster
    const kind = k8sKind
    const loadingKey = `k8s:${cacheKey}`
    const requestID = ++k8sRequestSequence.current
    setRemoteLoading(loadingKey)
    setErrors((current) => ({ ...current, k8s_workload: '' }))
    try {
      let options: K8sOption[] = []
      if (kind === 'deployment') options = (await fetchK8sDeployments(clusterID)).deployments || []
      else if (kind === 'statefulset') options = (await fetchK8sStatefulSets(clusterID)).statefulsets || []
      else options = (await fetchK8sDaemonSets(clusterID)).daemonsets || []
      if (requestID !== k8sRequestSequence.current || k8sScopeRef.current !== cacheKey) return
      setK8sCache((current) => ({ ...current, [cacheKey]: options }))
    } catch (error) {
      if (requestID !== k8sRequestSequence.current || k8sScopeRef.current !== cacheKey) return
      setErrors((current) => ({ ...current, k8s_workload: message(error) }))
    } finally { setRemoteLoading((current) => current === loadingKey ? '' : current) }
  }

  const submit = async () => {
    if (!name.trim()) {
      setFormError('服务名称不能为空')
      return
    }
    setFormError('')
    await onSave({ name: name.trim(), description: description.trim(), resources: selected.map(baseResource) })
  }

  const composeOptions = composeCache[composeNode] || []
  const systemdOptions = systemdCache[systemdNode] || []
  const k8sOptions = k8sCache[`${k8sCluster}:${k8sKind}`] || []

  return (
    <div className="fixed inset-0 z-[120] flex items-center justify-center bg-black/55 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !saving) onClose() }}>
      <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="service-editor-title" className="soft-panel flex max-h-[90vh] w-full max-w-5xl flex-col overflow-hidden shadow-2xl">
        <header className="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
          <div>
            <h2 id="service-editor-title" className="text-lg font-black text-foreground">{service ? '编辑应用服务' : '创建应用服务'}</h2>
            <p className="mt-1 text-xs font-semibold text-muted-foreground">关联只建立统一视图，不会接管或修改原资源。</p>
          </div>
          <button type="button" aria-label="关闭编辑器" disabled={saving} onClick={onClose} className="soft-button flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground disabled:opacity-50"><X size={17} /></button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <div className="grid gap-4 lg:grid-cols-[minmax(0,0.75fr)_minmax(0,1.25fr)]">
            <section className="space-y-4">
              <label className="block text-xs font-black text-muted-foreground">服务名称
                <input autoFocus value={name} onChange={(event) => setName(event.target.value)} maxLength={128} className="soft-input mt-1 min-h-10 w-full px-3 text-sm font-bold text-foreground" placeholder="例如：MizuPanel" />
              </label>
              <label className="block text-xs font-black text-muted-foreground">描述（可选）
                <textarea value={description} onChange={(event) => setDescription(event.target.value)} maxLength={2048} rows={4} className="soft-input mt-1 w-full resize-y px-3 py-2 text-sm font-semibold text-foreground" placeholder="说明用途、负责人或故障影响范围" />
              </label>
              <div className="rounded-2xl border border-border bg-muted/35 p-3">
                <div className="flex items-center justify-between"><p className="text-sm font-black text-foreground">已选择资源</p><span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-black text-primary">{selected.length}</span></div>
                <div className="mt-2 max-h-72 space-y-1.5 overflow-y-auto">
                  {selected.length === 0 ? <p className="py-4 text-center text-xs font-semibold text-muted-foreground">可以先创建空服务，稍后再关联资源。</p> : selected.map((resource) => (
                    <div key={resourceKey(resource)} className="flex items-center gap-2 rounded-xl border border-border bg-card px-3 py-2">
                      <span className="min-w-0 flex-1 truncate text-xs font-bold text-foreground">{resource.display_name}</span>
                      <span className="shrink-0 text-[10px] font-black text-muted-foreground">{resourceLabels[resource.resource_type]}</span>
                      <button type="button" aria-label={`移除 ${resource.display_name}`} onClick={() => toggle(resource)} className="text-muted-foreground hover:text-danger"><X size={14} /></button>
                    </div>
                  ))}
                </div>
              </div>
            </section>

            <section className="space-y-3" aria-label="关联资源选择器">
              <div className="flex items-center gap-2"><Boxes size={17} className="text-primary" /><h3 className="text-sm font-black text-foreground">关联现有资源</h3></div>
              {initialLoading ? <div className="soft-empty-state flex min-h-36 items-center justify-center gap-2 text-sm font-bold text-muted-foreground"><LoaderCircle size={18} className="animate-spin" />正在加载资源目录</div> : (
                <>
                  <ResourceSection title="节点" icon={<Server size={15} />} error={errors.node}>
                    <OptionList options={nodes.map((node) => ({ key: node.id, label: node.name || node.hostname || node.id, detail: node.status, resource: resource('node', '', '', '', node.id, node.name || node.hostname || node.id) }))} selected={selectedKeys} onToggle={toggle} />
                  </ResourceSection>
                  <ResourceSection title="Compose 项目" icon={<Boxes size={15} />} error={errors.compose_project}>
                    <LazyControls value={composeNode} onChange={(value) => { setComposeNode(value); setErrors((current) => ({ ...current, compose_project: '' })) }} choices={nodes.map((node) => ({ value: node.id, label: node.name || node.hostname || node.id }))} onLoad={loadCompose} loading={remoteLoading === `compose:${composeNode}`} loaded={Boolean(composeCache[composeNode])} />
                    <OptionList options={composeOptions.map((project) => {
                      const managed = project.management === 'managed' && Boolean(project.managed_project_id)
                      return { key: `${composeNode}:${managed ? project.managed_project_id : project.name}`, label: project.display_name || project.name, detail: managed ? '托管项目' : '外部项目', resource: resource('compose_project', composeNode, managed ? 'managed' : 'external', '', managed ? project.managed_project_id! : project.name, project.display_name || project.name) }
                    })} selected={selectedKeys} onToggle={toggle} />
                  </ResourceSection>
                  <ResourceSection title="Systemd 服务" icon={<CloudCog size={15} />} error={errors.systemd_service}>
                    <LazyControls value={systemdNode} onChange={(value) => { setSystemdNode(value); setErrors((current) => ({ ...current, systemd_service: '' })) }} choices={nodes.map((node) => ({ value: node.id, label: node.name || node.hostname || node.id }))} onLoad={loadSystemd} loading={remoteLoading === `systemd:${systemdNode}`} loaded={Boolean(systemdCache[systemdNode])} />
                    <OptionList options={systemdOptions.map((unit) => ({ key: `${systemdNode}:${unit.name}`, label: unit.description || unit.name, detail: unit.name, resource: resource('systemd_service', systemdNode, '', '', unit.name, unit.description || unit.name) }))} selected={selectedKeys} onToggle={toggle} />
                  </ResourceSection>
                  <ResourceSection title="Kubernetes 工作负载" icon={<Boxes size={15} />} error={errors.k8s_workload}>
                    <div className="grid grid-cols-[minmax(0,1fr)_140px_auto] gap-2">
                      <select value={k8sCluster} onChange={(event) => { setK8sCluster(event.target.value); setErrors((current) => ({ ...current, k8s_workload: '' })) }} className="soft-input min-h-9 min-w-0 px-2 text-xs font-bold"><option value="">选择集群</option>{clusters.map((cluster) => <option key={cluster.id} value={cluster.id}>{cluster.name}</option>)}</select>
                      <select value={k8sKind} onChange={(event) => { setK8sKind(event.target.value as K8sKind); setErrors((current) => ({ ...current, k8s_workload: '' })) }} className="soft-input min-h-9 px-2 text-xs font-bold"><option value="deployment">Deployment</option><option value="statefulset">StatefulSet</option><option value="daemonset">DaemonSet</option></select>
                      <button type="button" onClick={() => void loadK8s()} disabled={!k8sCluster || remoteLoading.startsWith('k8s:')} className="soft-button min-h-9 border border-border bg-card px-3 text-xs font-black disabled:opacity-50">{k8sCache[`${k8sCluster}:${k8sKind}`] ? '已加载' : '加载'}</button>
                    </div>
                    <OptionList options={k8sOptions.map((workload) => ({ key: `${k8sCluster}:${k8sKind}:${workload.namespace}:${workload.name}`, label: workload.name, detail: `${workload.namespace} · ${k8sKind}`, resource: resource('k8s_workload', k8sCluster, k8sKind, workload.namespace, workload.name, workload.name) }))} selected={selectedKeys} onToggle={toggle} />
                  </ResourceSection>
                  <ResourceSection title="服务拨测" error={errors.uptime_monitor}><OptionList options={monitors.map((monitor) => ({ key: String(monitor.id), label: monitor.name, detail: monitor.target, resource: resource('uptime_monitor', '', '', '', String(monitor.id), monitor.name) }))} selected={selectedKeys} onToggle={toggle} /></ResourceSection>
                  <ResourceSection title="告警规则" error={errors.alert_rule}><OptionList options={rules.map((rule) => ({ key: String(rule.id), label: rule.name, detail: rule.enabled ? '已启用' : '已禁用', resource: resource('alert_rule', '', '', '', String(rule.id), rule.name) }))} selected={selectedKeys} onToggle={toggle} /></ResourceSection>
                  <ResourceSection title="计划任务" error={errors.scheduled_task}><OptionList options={tasks.map((task) => ({ key: String(task.id), label: task.name, detail: task.cron_expression, resource: resource('scheduled_task', '', '', '', String(task.id), task.name) }))} selected={selectedKeys} onToggle={toggle} /></ResourceSection>
                </>
              )}
            </section>
          </div>
          {formError && <p role="alert" className="mt-4 rounded-xl border border-danger/25 bg-danger/10 px-3 py-2 text-sm font-bold text-danger">{formError}</p>}
        </div>

        <footer className="flex items-center justify-end gap-2 border-t border-border px-5 py-4">
          <button type="button" disabled={saving} onClick={onClose} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-foreground disabled:opacity-50">取消</button>
          <button type="button" disabled={saving} onClick={() => void submit()} className="soft-button inline-flex min-h-10 min-w-28 items-center justify-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground disabled:opacity-60">{saving && <LoaderCircle size={15} className="animate-spin" />}{service ? '保存服务' : '创建服务'}</button>
        </footer>
      </div>
    </div>
  )
}

function ResourceSection({ title, icon, error, children }: { title: string, icon?: React.ReactNode, error?: string, children: React.ReactNode }) {
  return <details className="group rounded-2xl border border-border bg-card" open><summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 text-xs font-black text-foreground">{icon}{title}<span className="ml-auto text-muted-foreground transition group-open:rotate-180">⌄</span></summary><div className="border-t border-border px-3 py-3">{error && <p role="alert" className="mb-2 rounded-lg bg-danger/10 px-2 py-1.5 text-xs font-bold text-danger">加载失败: {error}</p>}{children}</div></details>
}

type Option = { key: string, label: string, detail: string, resource: ApplicationServiceResource }
function OptionList({ options, selected, onToggle }: { options: Option[], selected: Set<string>, onToggle: (resource: ApplicationServiceResource) => void }) {
  if (options.length === 0) return <p className="py-2 text-center text-xs font-semibold text-muted-foreground">暂无已加载资源</p>
  return <div className="mt-2 grid max-h-40 gap-1 overflow-y-auto sm:grid-cols-2">{options.map((option) => {
    const checked = selected.has(resourceKey(option.resource))
    return <label key={option.key} className={`flex cursor-pointer items-start gap-2 rounded-xl border px-2.5 py-2 transition ${checked ? 'border-primary/40 bg-primary/10' : 'border-border bg-muted/20 hover:border-primary/25'}`}><input type="checkbox" checked={checked} onChange={() => onToggle(option.resource)} className="mt-0.5 accent-[hsl(var(--primary))]" /><span className="min-w-0"><span className="block truncate text-xs font-black text-foreground">{option.label}</span><span className="block truncate text-[10px] font-semibold text-muted-foreground">{option.detail}</span></span></label>
  })}</div>
}

function LazyControls({ value, onChange, choices, onLoad, loading, loaded }: { value: string, onChange: (value: string) => void, choices: Array<{ value: string, label: string }>, onLoad: () => Promise<void>, loading: boolean, loaded: boolean }) {
  return <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2"><select value={value} onChange={(event) => onChange(event.target.value)} className="soft-input min-h-9 min-w-0 px-2 text-xs font-bold"><option value="">选择节点</option>{choices.map((choice) => <option key={choice.value} value={choice.value}>{choice.label}</option>)}</select><button type="button" onClick={() => void onLoad()} disabled={!value || loading || loaded} className="soft-button inline-flex min-h-9 items-center gap-1 border border-border bg-card px-3 text-xs font-black disabled:opacity-50">{loading && <LoaderCircle size={13} className="animate-spin" />}{loaded ? '已加载' : '加载'}</button></div>
}

function resource(resourceType: ApplicationServiceResource['resource_type'], scopeID: string, kind: string, namespace: string, key: string, displayName: string): ApplicationServiceResource {
  return { resource_type: resourceType, scope_id: scopeID, resource_kind: kind, namespace, resource_key: key, display_name: displayName }
}

function baseResource(item: ApplicationServiceResource): ApplicationServiceResource {
  return { resource_type: item.resource_type, scope_id: item.scope_id || '', resource_kind: item.resource_kind || '', namespace: item.namespace || '', resource_key: item.resource_key, display_name: item.display_name }
}

function resourceKey(item: ApplicationServiceResource) {
  return [item.resource_type, item.scope_id, item.resource_kind, item.namespace, item.resource_key].join('\u0000')
}

function message(error: unknown) {
  return error instanceof Error ? error.message : '网络错误'
}

function useDialog(dialogRef: React.RefObject<HTMLDivElement | null>, busy: boolean, onClose: () => void) {
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    const keyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busy) onClose()
      if (event.key !== 'Tab' || !dialogRef.current) return
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), summary'))
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', keyDown)
    return () => {
      document.removeEventListener('keydown', keyDown)
      if (previous?.isConnected) previous.focus()
    }
  }, [busy, dialogRef, onClose])
}
