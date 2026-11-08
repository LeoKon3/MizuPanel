import { useMemo, useState } from 'react'
import { ArrowRight } from 'lucide-react'
import type { ApplicationServiceDetail, ApplicationServiceHealthCheck, ServiceEvent } from '../../types'
import { resourcePath, serviceEvents, serviceTime } from './model'
import { ServiceHealthBadge } from './ServiceHealthBadge'

export function ServiceIncidentPanel({ service, check, stale, onNavigate }: {
  service: ApplicationServiceDetail; check?: ApplicationServiceHealthCheck; stale?: boolean; onNavigate: (path: string) => void
}) {
  const problems = service.resources.filter(resource => resource.health !== 'healthy' || resource.state !== 'available')
  const title = check?.status === 'healthy' ? '当前健康' : check?.status === 'issues' ? '仍有异常' : '证据不足'
  return <section aria-label="当前问题与复查" className="service-section service-incident">
    <div className="service-section-heading"><h2>需要关注 <span>{problems.length}</span></h2></div>
    {problems.length ? <ul className="service-problems">{problems.map(resource => <li key={resource.id}>
      <ServiceHealthBadge health={resource.health} compact /><div className="min-w-0 flex-1"><strong>{resource.display_name}</strong><p>{resource.reason || (resource.state === 'missing' ? '关联资源已不存在' : '状态暂不可用')}</p></div>
      <button className="service-link" onClick={() => onNavigate(resourcePath(resource))}>查看资源<ArrowRight size={13} /></button>
    </li>)}</ul> : <p className="service-empty-line">{stale ? '当前快照未能更新，暂无已确认的问题。' : !service.resources.length ? '尚未关联资源，无法判断服务健康状态。' : service.health === 'healthy' ? '当前关联资源未报告异常。' : service.first_reason || '服务状态尚无法确认。'}</p>}
    {check && <div role="status" className="service-check-result"><div className="flex flex-wrap items-center justify-between gap-2"><h3 className={check.status === 'healthy' ? 'text-success' : check.status === 'issues' ? 'text-danger' : 'text-warning'}>{title}</h3><time>{serviceTime(check.checked_at)}</time></div><p>{check.summary}</p>{check.checks.length > 0 && <ul>{check.checks.map(probe => <li key={probe.resource_id}><span>{probe.name}</span><span>{probe.message}</span></li>)}</ul>}</div>}
  </section>
}

export function ServiceEventTimeline({ service, onNavigate }: { service: ApplicationServiceDetail; onNavigate: (path: string) => void }) {
  const [filter, setFilter] = useState<ServiceEvent['kind'] | 'all'>('all')
  const events = useMemo(() => serviceEvents(service), [service])
  const matching = events.filter(event => filter === 'all' || event.kind === filter)
  const visible = matching.slice(0, 8)
  return <section aria-label="近期事件" className="service-section service-events">
    <div className="service-section-heading"><h2>近期事件</h2><div className="service-filter-row" aria-label="事件类别">{([['all', '全部'], ['alert', '告警'], ['task', '任务'], ['audit', '操作']] as const).map(([value, label]) => <button key={value} aria-pressed={filter === value} onClick={() => setFilter(value)}>{label}</button>)}</div></div>
    {visible.length ? <ol>{visible.map(event => <li key={event.id}>
      <time>{serviceTime(event.at)}</time><span className={event.danger ? 'text-danger' : 'text-muted-foreground'}>{event.label}</span>
      <div className="min-w-0"><strong title={event.title}>{event.title}</strong>{event.detail && <p title={event.detail}>{event.detail}</p>}</div>
      <button className="service-link" aria-label={`查看${event.label}：${event.title}`} onClick={() => onNavigate(event.path)}>查看<ArrowRight size={13} /></button>
    </li>)}</ol> : <p className="service-empty-line">暂无{filter === 'all' ? '相关' : { alert: '告警', task: '任务', audit: '操作' }[filter]}事件。</p>}
    <div className="service-events-footer"><span>展示当前返回记录中的最新 {visible.length} 条；时间接近不代表存在因果关系。</span><div className="flex gap-4"><button className="service-link" onClick={() => onNavigate('/alerts')}>告警历史</button><button className="service-link" onClick={() => onNavigate('/tasks?tab=runs')}>执行记录</button><button className="service-link" onClick={() => onNavigate('/audit')}>审计日志</button></div></div>
  </section>
}
