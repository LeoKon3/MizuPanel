import { useEffect, useMemo, useRef, useState } from 'react'
import { LoaderCircle, Search, X } from 'lucide-react'
import { useUnsavedChangesGuard, type LeaveGuard } from '../UnsavedChangesDialog'
import type { ApplicationServiceDetail, ApplicationServiceInput, ApplicationServiceResource } from '../../types'
import { baseResource, resourceKey, resourceLabels, resourceTypes } from './model'
import { useResourceCatalog } from './useResourceCatalog'

type EditorProps = {
  onRegisterLeaveGuard?: (guard: LeaveGuard | undefined) => void
  service?: ApplicationServiceDetail
  saving: boolean
  onClose: () => void
  onSave: (input: ApplicationServiceInput) => Promise<void>
}
function signature(name: string, description: string, selected: ApplicationServiceResource[]) {
  return JSON.stringify({ name, description, resources: selected.map(baseResource).sort((a, b) => resourceKey(a).localeCompare(resourceKey(b))) })
}

export function ServiceEditorModal({ service, saving, onClose, onSave, onRegisterLeaveGuard }: EditorProps) {
  const [name, setName] = useState(service?.name || '')
  const [description, setDescription] = useState(service?.description || '')
  const [selected, setSelected] = useState<ApplicationServiceResource[]>(() => (service?.resources || []).map(baseResource))
  const initial = useRef(signature(name, description, selected))
  const dirty = initial.current !== signature(name, description, selected)
  const [type, setType] = useState<ApplicationServiceResource['resource_type']>('node')
  const [query, setQuery] = useState('')
  const [scopeByType, setScopeByType] = useState<Record<string, string>>({})
  const [kind, setKind] = useState('deployment')
  const [formError, setFormError] = useState('')
  const [selectedOpen, setSelectedOpen] = useState(true)
  const dialogRef = useRef<HTMLDivElement>(null)
  const latest = useRef({ saving, onClose })
  latest.current = { saving, onClose }
  const navigationGuard = useUnsavedChangesGuard(dirty, () => {}, { description: '服务信息或资源关联尚未保存。离开会丢弃本次修改，不会修改底层资源。', zIndex: 140 })
  const close = () => { if (!saving) navigationGuard.request(onClose) }
  const catalog = useResourceCatalog()
  const remote = ['compose_project', 'systemd_service', 'k8s_workload'].includes(type)
  const scope = scopeByType[type] ?? (type === 'k8s_workload' ? catalog.clusters[0]?.id : catalog.nodes[0]?.id) ?? ''
  const key = remote ? `${type}:${scope}:${type === 'k8s_workload' ? kind : ''}` : type
  const parentKey = type === 'k8s_workload' ? 'clusters' : 'node'
  const parentError = remote ? catalog.errors[parentKey] : undefined
  const error = catalog.errors[key] || parentError
  const busy = Boolean(catalog.busy[key] || (remote && catalog.busy[parentKey]))
  const loaded = catalog.cache[key] !== undefined
  const options = catalog.cache[key] || []
  const visible = options.filter(option => `${option.label} ${option.detail}`.toLowerCase().includes(query.trim().toLowerCase()))
  const selectedKeys = useMemo(() => new Set(selected.map(resourceKey)), [selected])
  const orderedSelected = useMemo(() => [...selected].sort((a, b) => resourceTypes.indexOf(a.resource_type) - resourceTypes.indexOf(b.resource_type) || a.display_name.localeCompare(b.display_name, 'zh-CN')), [selected])
  const guardRef = useRef<LeaveGuard>(() => {})
  guardRef.current = leave => {
    if (latest.current.saving) { setFormError('服务正在保存，请等待请求完成'); return }
    navigationGuard.request(() => { latest.current.onClose(); leave() })
  }
  const stableGuard = useRef<LeaveGuard>(leave => guardRef.current(leave))
  useEffect(() => {
    onRegisterLeaveGuard?.(dirty || saving ? stableGuard.current : undefined)
    return () => onRegisterLeaveGuard?.(undefined)
  }, [dirty, saving, onRegisterLeaveGuard])
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    dialogRef.current?.querySelector<HTMLInputElement>('input')?.focus()
    const keyDown = (event: KeyboardEvent) => {
      if (document.querySelector('[role="dialog"][aria-label="未保存的修改"]')) return
      if (event.key === 'Escape') { if (!latest.current.saving) navigationGuard.request(() => latest.current.onClose()); return }
      if (event.key !== 'Tab') return
      const elements = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),textarea:not([disabled]),select:not([disabled])') || [])
      if (!elements.length) { event.preventDefault(); return }
      if (event.shiftKey && document.activeElement === elements[0]) { event.preventDefault(); elements[elements.length - 1].focus() }
      else if (!event.shiftKey && document.activeElement === elements[elements.length - 1]) { event.preventDefault(); elements[0].focus() }
    }
    document.addEventListener('keydown', keyDown)
    return () => { document.removeEventListener('keydown', keyDown); if (previous?.isConnected) previous.focus() }
  }, [navigationGuard.request])
  const toggle = (resource: ApplicationServiceResource) => {
    if (saving) return
    const key = resourceKey(resource)
    setSelected(current => current.some(item => resourceKey(item) === key) ? current.filter(item => resourceKey(item) !== key) : [...current, resource])
  }
  const retry = () => {
    if (parentError) void catalog.retryStatic(parentKey)
    else if (remote) void catalog.loadRemote(type, scope, kind)
    else void catalog.retryStatic(type)
  }
  const submit = async () => {
    if (saving) return
    if (!name.trim()) { setFormError('服务名称不能为空'); return }
    setFormError('')
    await onSave({ name: name.trim(), description: description.trim(), resources: selected.map(baseResource) })
  }
  return <>
    {navigationGuard.dialog}
    <div className="soft-modal-overlay fixed inset-0 z-[120] flex items-center justify-center p-4" onMouseDown={event => { if (event.currentTarget === event.target) close() }}>
      <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="service-editor-title" className="soft-modal-shell service-editor">
        <header><h2 id="service-editor-title">{service ? '编辑应用服务' : '创建应用服务'}</h2><button className="console-icon-button" aria-label="关闭编辑器" onClick={close} disabled={saving}><X size={17} /></button></header>
        <div className="service-editor-body">
          <div className="service-editor-fields"><label>服务名称<input disabled={saving} value={name} onChange={event => setName(event.target.value)} maxLength={128} placeholder="例如：MizuPanel" /></label><label>描述（可选）<textarea disabled={saving} value={description} onChange={event => setDescription(event.target.value)} maxLength={2048} rows={2} placeholder="用途、负责人或故障影响范围" /></label></div>
          <div className="service-picker-grid">
            <section aria-label="关联资源选择器" className="service-picker">
              <h3>关联现有资源</h3>
              <div className="service-filter-row service-picker-types" aria-label="选择资源类型">{resourceTypes.map(value => <button type="button" key={value} aria-label={resourceLabels[value]} aria-pressed={type === value} onClick={() => { setType(value); setQuery('') }}>{resourceLabels[value]}{selected.filter(item => item.resource_type === value).length > 0 && <span> {selected.filter(item => item.resource_type === value).length}</span>}</button>)}</div>
              {remote && <div className="service-picker-scope"><label className="sr-only" htmlFor="service-resource-scope">{type === 'k8s_workload' ? '资源集群' : '资源主机'}</label><select id="service-resource-scope" value={scope} disabled={saving} onChange={event => setScopeByType(current => ({ ...current, [type]: event.target.value }))}><option value="">{type === 'k8s_workload' ? '选择集群' : '选择主机'}</option>{type === 'k8s_workload' ? catalog.clusters.map(cluster => <option key={cluster.id} value={cluster.id}>{cluster.name}</option>) : catalog.nodes.map(node => <option key={node.id} value={node.id}>{node.name || node.hostname || node.id}</option>)}</select>{type === 'k8s_workload' && <select value={kind} aria-label="工作负载类型" disabled={saving} onChange={event => setKind(event.target.value)}><option value="deployment">Deployment</option><option value="statefulset">StatefulSet</option><option value="daemonset">DaemonSet</option></select>}<button className="console-button" disabled={saving || busy || !scope} onClick={() => void catalog.loadRemote(type, scope, kind)}>{busy ? '加载中' : loaded ? '刷新资源' : '加载'}</button></div>}
              <label className="console-search"><Search size={15} /><input aria-label="搜索可选资源" placeholder="搜索当前分类的名称或位置" value={query} onChange={event => setQuery(event.target.value)} /></label>
              {error && <div role="alert" className="service-error">资源加载失败: {error}<button onClick={retry} disabled={saving || busy}>重试</button></div>}
              <div className="service-options" aria-busy={busy}>
                {busy && !loaded ? <p className="service-empty-line" role="status"><LoaderCircle size={15} className="animate-spin" />正在加载资源目录</p> : !loaded ? <p className="service-empty-line">{error ? '资源暂不可用，已选关联仍保留。' : remote ? '选择范围后加载资源；不会修改这些资源。' : '资源目录尚未获取。'}</p> : visible.length ? visible.map(option => <label key={option.key} className={selectedKeys.has(option.key) ? 'selected' : undefined}><input type="checkbox" disabled={saving} checked={selectedKeys.has(option.key)} onChange={() => toggle(option.resource)} /><span><strong>{option.label}</strong><small>{option.detail}</small></span></label>) : <p className="service-empty-line">{options.length ? '没有匹配的资源。' : '当前范围没有可关联的资源。'}</p>}
              </div>
            </section>
            <section aria-label="已选择资源" className="service-selected"><div className="service-section-heading"><h3>已选择资源 <span>{selected.length}</span></h3><button className="service-link" aria-expanded={selectedOpen} onClick={() => setSelectedOpen(!selectedOpen)}>{selectedOpen ? '收起' : '展开'}</button></div>
              {selectedOpen && <div className="service-selected-list">{!selected.length ? <p className="service-empty-line">可以先创建空服务，稍后再关联资源。</p> : orderedSelected.map(resource => <div key={resourceKey(resource)}><div className="min-w-0 flex-1"><strong title={resource.display_name}>{resource.display_name}</strong><p>{resourceLabels[resource.resource_type]}{resource.scope_id ? ` · ${catalog.nodes.find(node => node.id === resource.scope_id)?.name || catalog.clusters.find(cluster => cluster.id === resource.scope_id)?.name || resource.scope_id}` : ''}{resource.namespace ? ` · ${resource.namespace}` : ''}</p></div><button className="console-icon-button" disabled={saving} aria-label={`移除 ${resource.display_name}`} onClick={() => toggle(resource)}><X size={14} /></button></div>)}</div>}
            </section>
          </div>
          {formError && <p role="alert" className="service-error mt-4">{formError}</p>}
        </div>
        <footer><span>仅保存逻辑关联，不修改底层资源。</span><div className="flex gap-2"><button className="console-button" disabled={saving} onClick={close}>取消</button><button className="console-button console-primary" disabled={saving} onClick={() => void submit()}>{saving && <LoaderCircle size={15} className="animate-spin" />}{service ? '保存服务' : '创建服务'}</button></div></footer>
      </div>
    </div>
  </>
}
