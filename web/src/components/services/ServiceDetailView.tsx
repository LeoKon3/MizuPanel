import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, Bot, Edit3, MoreHorizontal, RefreshCw, Search, Trash2 } from 'lucide-react'
import type { ApplicationServiceDetail, ApplicationServiceHealthCheck, ApplicationServiceResourceType } from '../../types'
import { ServiceHealthBadge } from './ServiceHealthBadge'
import { ServiceEventTimeline, ServiceIncidentPanel } from './ServiceIncidentPanel'
import { resourceIdentity, resourceLabels, resourcePath, resourceTypes, serviceTime } from './model'

export function ServiceMoreMenu({ onDelete, disabled }: { onDelete: () => void; disabled: boolean }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    root.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus()
    const click = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false) }
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') { setOpen(false); trigger.current?.focus() } }
    document.addEventListener('pointerdown', click)
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('pointerdown', click); document.removeEventListener('keydown', key) }
  }, [open])
  return <div ref={root} className="service-more" onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false) }}>
    <button ref={trigger} className="console-icon-button" aria-label="更多服务操作" aria-haspopup="menu" aria-expanded={open} disabled={disabled} onClick={() => setOpen(!open)}><MoreHorizontal size={18} /></button>
    {open && <div role="menu" aria-label="服务操作菜单"><button role="menuitem" onClick={() => { setOpen(false); trigger.current?.focus(); onDelete() }}><Trash2 size={15} />删除服务</button></div>}
  </div>
}

export function ServiceDetailView({ service, refreshing, checking, check, updatedAt, stale, diagnosisBusy, onRefresh, onCheck, onDiagnose, onEdit, onDelete, onBack, onNavigate }: {
  service: ApplicationServiceDetail; refreshing: boolean; checking: boolean; check?: ApplicationServiceHealthCheck; updatedAt?: string; stale: boolean; diagnosisBusy?: boolean
  onRefresh: () => void; onCheck: () => void; onDiagnose?: () => void; onEdit: () => void; onDelete: () => void; onBack: () => void; onNavigate: (path: string) => void
}) {
  const [type, setType] = useState<ApplicationServiceResourceType | 'all'>('all')
  const [query, setQuery] = useState('')
  const scoped = service.resources.filter(resource => type === 'all' || resource.resource_type === type)
  const resources = scoped.filter(resource => `${resource.display_name} ${resource.resource_key} ${resourceIdentity(resource)} ${resource.reason}`.toLowerCase().includes(query.trim().toLowerCase()))
  return <section aria-label="故障工作台" className="service-detail">
    <div className="console-page-title service-detail-title"><div className="flex min-w-0 items-center gap-3"><button className="console-icon-button" aria-label="返回应用服务" onClick={onBack}><ArrowLeft size={17} /></button><h1 title={service.name}>{service.name}</h1><ServiceHealthBadge health={service.health} /></div>
      <div className="service-actions"><button className="console-button console-primary" disabled={checking || !service.resources.length} onClick={onCheck}><RefreshCw size={15} className={checking ? 'animate-spin' : undefined} />{checking ? '正在复查' : '复查健康'}</button>
        {onDiagnose && <button className="console-button" disabled={diagnosisBusy || !service.resources.length} onClick={onDiagnose}><Bot size={15} />AI 诊断</button>}
        <button className="console-button console-ghost" onClick={onEdit}><Edit3 size={15} />编辑</button><ServiceMoreMenu onDelete={onDelete} disabled={false} />
      </div>
    </div>
    <div className="service-detail-meta"><span>{service.location_summary || '部署位置未确定'}</span><span>{service.resource_count} 个关联资源</span><span>{stale ? '上次快照' : '快照更新'} {serviceTime(updatedAt)}</span><button className="service-link" onClick={onRefresh} disabled={refreshing || checking}><RefreshCw size={13} />刷新</button></div>
    {service.description && <p className="service-description">{service.description}</p>}
    <p className="service-operation-note">复查会读取资源并运行关联拨测，可能发送已有故障或恢复通知，不执行修复。AI 诊断只准备对话草稿。</p>
    <ServiceIncidentPanel service={service} check={check} stale={stale} onNavigate={onNavigate} />
    <section aria-label="关联资源" className="service-section service-resources">
      <div className="service-section-heading"><h2>关联资源 <span>{service.resources.length}</span></h2></div>
      <div className="service-resource-toolbar"><div className="service-filter-row" aria-label="资源类型筛选"><button aria-pressed={type === 'all'} onClick={() => setType('all')}>全部</button>{resourceTypes.filter(value => service.resources.some(resource => resource.resource_type === value)).map(value => <button key={value} aria-pressed={type === value} onClick={() => setType(value)}>{resourceLabels[value]}</button>)}</div><label className="console-search"><Search size={15} /><input aria-label="搜索关联资源" placeholder="搜索资源名称或位置" value={query} onChange={event => setQuery(event.target.value)} /></label></div>
      {!service.resources.length ? <div className="service-empty-line">尚未关联资源。<button className="service-link" onClick={onEdit}>编辑服务</button></div> : <>
        <div className="service-table-scroll"><table className="service-table resource-table"><thead><tr><th>资源</th><th>类型</th><th>状态</th><th>位置 / 关联状态</th><th className="num">操作</th></tr></thead><tbody>{resources.map(resource => <tr key={resource.id}>
          <td><strong title={resource.display_name}>{resource.display_name}</strong>{resource.reason && <p title={resource.reason}>{resource.reason}</p>}</td><td>{resourceLabels[resource.resource_type]}</td><td><ServiceHealthBadge health={resource.health} compact /></td><td><span>{resourceIdentity(resource)}</span><p>{resource.state === 'missing' ? '关联已失效' : resource.state === 'unavailable' ? '暂不可用' : '已关联'}</p></td>
          <td className="num"><button className="service-link" aria-label={`打开 ${resource.display_name} 的原管理入口`} onClick={() => onNavigate(resourcePath(resource))}>查看<ArrowRight size={13} /></button></td>
        </tr>)}</tbody></table></div>
        {!resources.length && <p className="service-empty-line">没有匹配的资源。</p>}<div className="service-table-footer">显示 {resources.length} / {service.resources.length} 个资源</div>
      </>}
    </section>
    <ServiceEventTimeline service={service} onNavigate={onNavigate} />
  </section>
}
