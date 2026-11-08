import { useEffect, useRef, useState } from 'react'
import { ArrowRight, Plus, RefreshCw } from 'lucide-react'
import { Toast } from '../components/Toast'
import { formatPercent } from '../lib/format'
import { nodeDisplayStatus } from '../lib/nodeStatus'
import { nodePath } from '../routes'
import type { AutomationRunStatus, Node, OverviewSourceState } from '../types'
import { buildAttention, monitorState, overviewTime, sourceIsStale } from './overview/model'
import { useOverviewSources } from './overview/useOverviewSources'

type OverviewPageProps = {
  nodes: Node[]
  nodesLoading?: boolean
  nodesError?: string
  nodesUpdatedAt?: number
  onRefreshNodes: () => Promise<void>
  onAddServer: () => void
  onNavigate: (path: string) => void
}

const runLabels: Record<AutomationRunStatus, string> = { queued: '排队中', running: '执行中', success: '成功', partial: '部分失败', failed: '失败', skipped: '已跳过', interrupted: '已中断' }

function SourceStamp({ source, now }: { source: OverviewSourceState<unknown>; now: number }) {
  const stale = sourceIsStale(source, now)
  if (source.loading && source.data === undefined) return <span className="text-muted-foreground">读取中</span>
  if (source.data === undefined) return <span className="text-warning">未获取</span>
  return <span className={stale ? 'text-warning' : 'text-muted-foreground'}>{stale ? '上次快照 · ' : ''}{source.updatedAt ? new Date(source.updatedAt).toLocaleTimeString('zh-CN', { hour12: false }) : '更新时间未知'}</span>
}

export function OverviewPage({ nodes, nodesError, nodesUpdatedAt, nodesLoading = false, onRefreshNodes, onAddServer, onNavigate }: OverviewPageProps) {
  const sources = useOverviewSources()
  const mounted = useRef(false)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const [now, setNow] = useState(Date.now)
  const [expanded, setExpanded] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  useEffect(() => { const timer = setInterval(() => setNow(Date.now()), 15_000); return () => clearInterval(timer) }, [])
  const nodeSource: OverviewSourceState<Node[]> = { data: nodesUpdatedAt ? nodes : undefined, error: nodesError, updatedAt: nodesUpdatedAt, loading: nodesLoading }
  const sourceRows = [
    { name: '主机', source: nodeSource }, { name: '告警', source: sources.alerts },
    { name: '拨测', source: sources.monitors }, { name: '应用服务', source: sources.services },
    { name: 'Kubernetes', source: sources.clusters }, { name: '任务', source: sources.runs }
  ]
  const waiting = sourceRows.some(row => row.source.loading && row.source.data === undefined)
  const unavailable = sourceRows.filter(row => sourceIsStale(row.source, now) || (!row.source.loading && row.source.data === undefined))
  const stale = {
    nodes: sourceIsStale(nodeSource, now), alerts: sourceIsStale(sources.alerts, now), monitors: sourceIsStale(sources.monitors, now),
    services: sourceIsStale(sources.services, now), clusters: sourceIsStale(sources.clusters, now)
  }
  const alerts = (sources.alerts.data ?? []).filter(alert => alert && !alert.resolved_at)
  const monitors = sources.monitors.data ?? []
  const attention = buildAttention({ nodes: nodeSource.data ?? [], alerts, monitors, services: sources.services.data ?? [], clusters: sources.clusters.data ?? [], stale, now })
  const shown = expanded ? attention : attention.slice(0, 5)
  const shortcutNodes = [...nodes].sort((a, b) => (a.name || a.hostname).localeCompare(b.name || b.hostname, 'zh-CN', { numeric: true }) || a.id.localeCompare(b.id)).slice(0, 5)
  const online = nodes.filter(node => node.status === 'online').length
  const expired = nodes.filter(node => nodeDisplayStatus(node, now) === 'stale').length
  const enabledMonitors = monitors.filter(monitor => monitor.enabled)
  const monitorIssues = monitors.filter(monitor => ['down', 'warning'].includes(monitorState(monitor, now))).length
  const unknownMonitors = monitors.filter(monitor => monitorState(monitor, now) === 'unknown').length
  const recentRuns = (sources.runs.data?.runs ?? []).filter(run => Date.parse(run.created_at) >= now - 86400_000 && Date.parse(run.created_at) <= now + 1000).sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) || b.id - a.id).slice(0, 5)
  const refresh = async () => {
    if (refreshing) return
    setRefreshing(true)
    const results = await Promise.allSettled([sources.refresh(), onRefreshNodes()])
    if (!mounted.current) return
    setNow(Date.now())
    setRefreshing(false)
    if (results.some(result => result.status === 'rejected') || (results[0].status === 'fulfilled' && results[0].value.some(success => !success))) setToast({ type: 'error', message: '概览刷新失败: 部分来源未更新，请查看数据来源状态' })
    else setToast({ type: 'success', message: '概览刷新成功' })
  }
  return <div className="overview-page">
    {toast && <Toast {...toast} onClose={() => setToast(null)} />}
    <div className="console-page-title"><h1>概览</h1><div className="flex items-center gap-2"><button className="console-button console-ghost" disabled={refreshing} onClick={() => void refresh()}><RefreshCw size={15} />{refreshing ? '刷新中' : '刷新概览'}</button><button className="console-button" onClick={() => onNavigate('/nodes')}>查看主机<ArrowRight size={15} /></button></div></div>
    <section className="overview-summary" aria-label="当前状态摘要">
      <button onClick={() => onNavigate('/nodes')}><span>主机在线情况</span><strong>{nodeSource.data ? <>{online}<small> / {nodes.length}</small></> : '—'}</strong><p>{nodeSource.data ? `${nodes.length - online} 离线 · ${expired} 指标过期` : nodesLoading ? '读取中' : '主机数据未获取'}{stale.nodes ? ' · 上次快照' : ''}</p></button>
      <button onClick={() => onNavigate('/alerts?status=active')}><span>未恢复告警</span><strong>{sources.alerts.data ? alerts.length : '—'}</strong><p>{sources.alerts.loading && !sources.alerts.data ? '读取中' : !sources.alerts.data ? '未获取' : stale.alerts ? '上次快照，等待更新' : '当前启用规则 · 不限触发时间'}</p></button>
      <button onClick={() => onNavigate('/uptime')}><span>拨测异常</span><strong>{sources.monitors.data ? monitorIssues : '—'}</strong><p>{!sources.monitors.data ? sources.monitors.loading ? '读取中' : '未获取' : enabledMonitors.length === 0 ? '尚未配置或启用拨测' : `${enabledMonitors.length} 已启用 · ${unknownMonitors} 状态未知`}{stale.monitors ? ' · 上次快照' : ''}</p></button>
    </section>
    {unavailable.length > 0 && <div role="alert" className="overview-source-error"><span>部分数据未更新，不能据此确认全部正常。</span>{unavailable.map(row => <span key={row.name}>{row.name}：{row.source.error || (row.source.data === undefined ? '未获取' : '快照已过期')}</span>)}</div>}
    <div className="overview-columns">
      <section aria-label="需要关注" className="overview-attention">
        <div className="overview-section-heading"><h2>需要关注 <span>{attention.length}</span></h2>{attention.length > 5 && <button className="overview-link" onClick={() => setExpanded(!expanded)}>{expanded ? '收起' : `查看全部 ${attention.length} 项`}</button>}</div>
        {shown.length ? <ul>{shown.map(item => <li key={item.id} className={`attention-row attention-${item.severity}`}>
          <div className="attention-title"><i aria-hidden="true" /><strong>{item.name}</strong><span>{item.source}</span></div>
          <p className="attention-reason">{item.reason}</p>
          <div className="attention-footer"><span>{item.timeLabel}{item.timestamp ? ` ${overviewTime(item.timestamp)}` : ''}</span><div>{item.links.map(link => <button key={link.path} onClick={() => onNavigate(link.path)} className="overview-link">{link.label}<ArrowRight size={13} /></button>)}</div></div>
        </li>)}</ul> : <div className="overview-empty">{waiting ? '正在汇总已接入资源…' : unavailable.some(row => row.name !== '任务') ? '暂无已确认的问题，部分资源状态尚未获取。' : !nodes.length && !(sources.services.data?.length) && !(sources.monitors.data?.length) && !(sources.clusters.data?.length) ? '尚未接入资源，可先添加主机。' : '已获取的数据中暂无需要关注的问题。'}</div>}
      </section>
      <section aria-label="主机快捷入口" className="overview-hosts">
        <div className="overview-section-heading"><h2>主机快捷入口</h2><button className="overview-link" onClick={() => onNavigate('/nodes')}>全部主机<ArrowRight size={13} /></button></div>
        {nodeSource.data === undefined ? <div className="overview-empty">{nodesLoading ? '正在读取主机…' : '主机数据未获取'}</div> : shortcutNodes.length ? <table><thead><tr><th>名称 / 状态</th><th className="num">CPU</th><th className="num">内存</th></tr></thead><tbody>{shortcutNodes.map(node => {
          const status = nodeDisplayStatus(node, now)
          const metric = !stale.nodes && status === 'online' ? node.latest_metric : undefined
          return <tr key={node.id}><td><button className="overview-host-name" onClick={() => onNavigate(nodePath(node.id))}>{node.name || node.hostname}</button><span className={`console-status status-${stale.nodes ? 'stale' : status}`}><i aria-hidden="true" />{stale.nodes ? '待确认' : { online: '在线', offline: '离线', stale: '数据过期' }[status]}</span></td><td className="num">{metric ? formatPercent(metric.cpu_usage) : '—'}</td><td className="num">{metric ? formatPercent(metric.memory_usage) : '—'}</td></tr>
        })}</tbody></table> : <div className="overview-empty"><p>还没有接入主机</p><button className="console-button" onClick={onAddServer}><Plus size={15} />添加主机</button></div>}
        {nodes.length > 5 && <p className="overview-footnote">按名称显示前 5 台，共 {nodes.length} 台</p>}
        {nodes.length > 0 && <button className="overview-link mt-4" onClick={onAddServer}><Plus size={14} />添加主机</button>}
        <div className="overview-resource-links"><button onClick={() => onNavigate('/services')}>应用服务 <span>{sources.services.data?.length ?? '—'}</span><ArrowRight size={13} /></button><button onClick={() => onNavigate('/k8s/clusters')}>Kubernetes <span>{sources.clusters.data?.length ?? '—'}</span><ArrowRight size={13} /></button></div>
      </section>
    </div>
    <section aria-label="近期任务" className="overview-runs">
      <div className="overview-section-heading"><h2>近期任务</h2><div className="flex items-center gap-4"><span className="text-[13px] text-muted-foreground">最近 24 小时 · 最新 5 条</span><button className="overview-link" onClick={() => onNavigate('/tasks?tab=runs&range=24h')}>全部记录<ArrowRight size={13} /></button></div></div>
      {recentRuns.length ? <div className="overflow-x-auto"><table><thead><tr><th>任务 / 脚本</th><th className="num">目标</th><th>结果</th><th className="num">创建时间</th><th className="num">操作</th></tr></thead><tbody>{recentRuns.map(run => <tr key={run.id}><td><span className="overview-run-name" title={run.task_name || run.script_name}>{run.task_name || run.script_name}</span><span className="overview-run-trigger">{run.trigger === 'manual' ? '手动执行' : '计划任务'}</span></td><td className="num">{run.total_targets} 台</td><td><span className={`run-state state-${run.status}`}>{runLabels[run.status] ?? '未知状态'}</span></td><td className="num">{overviewTime(run.created_at)}</td><td className="num"><button className="overview-link" onClick={() => onNavigate(`/tasks?tab=runs&range=24h&run=${run.id}`)}>查看详情</button></td></tr>)}</tbody></table></div> : <div className="overview-empty">{sources.runs.loading && !sources.runs.data ? '正在读取执行记录…' : sources.runs.error ? '执行记录未获取，无法判断近期结果。' : '最近 24 小时没有执行记录。'}</div>}
      {sourceIsStale(sources.runs, now) && recentRuns.length > 0 && <p className="overview-footnote text-warning">以上为上次获取的记录，当前结果待更新。</p>}
    </section>
    <footer className="overview-freshness" aria-label="数据来源状态">{sourceRows.map(row => <div key={row.name}><span>{row.name}</span><SourceStamp source={row.source} now={now} /></div>)}</footer>
  </div>
}
