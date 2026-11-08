import { useState } from 'react'
import { ArrowDown, ArrowLeft, ArrowUp, ChevronDown, Clock3, Cpu, ExternalLink, RefreshCw, Search, Terminal } from 'lucide-react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { history, percent, processes, tabs, type Host, type HostTab, type Metric, type Scenario } from './model'
import { Notice, StateView, Status } from './UI'

function ResourceChart({ host, metric }: { host: Host; metric: Metric }) {
  const names = { cpu: 'CPU', memory: '内存', disk: '磁盘' }
  const data = history(host, metric).map((value, index) => ({ value, time: `${13 + Math.floor((30 + index * 5) / 60)}:${String((30 + index * 5) % 60).padStart(2, '0')}` }))
  return <div className="resource-chart" role="img" aria-label={`${host.name} ${names[metric]} 使用率，13:30 至 14:30，最近值 ${percent(host[metric])}`}>
    <ResponsiveContainer width="100%" height="100%" minWidth={0} initialDimension={{ width: 600, height: 202 }}>
      <LineChart data={data} margin={{ top: 10, right: 16, left: -18, bottom: 4 }}>
        <CartesianGrid vertical={false} stroke="var(--rule)" />
        <XAxis dataKey="time" ticks={['13:30', '13:45', '14:00', '14:15', '14:30']} axisLine={false} tickLine={false} tick={{ fill: 'var(--muted)', fontSize: 12 }} minTickGap={24} />
        <YAxis domain={[0, 100]} ticks={[0, 25, 50, 75, 100]} tickFormatter={value => `${value}%`} axisLine={false} tickLine={false} tick={{ fill: 'var(--muted)', fontSize: 12 }} />
        <Tooltip contentStyle={{ background: 'var(--surface)', border: '1px solid var(--control)', borderRadius: 6, fontSize: 12, color: 'var(--text)' }} formatter={value => [`${Number(value).toFixed(1)}%`, names[metric]]} />
        <Line type="linear" dataKey="value" stroke="var(--accent)" strokeWidth={2} dot={false} activeDot={{ r: 3 }} isAnimationActive={false} />
      </LineChart>
    </ResponsiveContainer>
  </div>
}

function Overview({ host, onTab }: { host: Host; onTab: (tab: HostTab) => void }) {
  const [metric, setMetric] = useState<Metric>('cpu')
  return <>
    <div className="overview-grid">
      <section className="resource-panel">
        <div className="section-heading"><h2>资源使用</h2><span>过去 1 小时 · 模拟快照</span></div>
        <div className="resource-metrics" aria-label="选择资源趋势">
          {(['cpu', 'memory', 'disk'] as const).map(item => <button className={metric === item ? 'metric active' : 'metric'} key={item} aria-pressed={metric === item} onClick={() => setMetric(item)}>
            <span>{{ cpu: 'CPU', memory: '内存', disk: '磁盘' }[item]}</span><strong>{percent(host[item])}</strong>
            <small>{item === 'cpu' ? `${host.cores} 核` : item === 'memory' ? `共 ${host.memoryGB} GiB` : `共 ${host.diskGB} GiB`}</small>
          </button>)}
        </div>
        {host.status === 'online' ? <ResourceChart host={host} metric={metric} /> : <div className="chart-unavailable"><Clock3 /><p>{host.status === 'offline' ? '主机离线，没有当前指标' : '指标已过期，等待下一次上报'}</p><span>不使用旧数据填充实时曲线</span></div>}
      </section>
      <section className="system-panel"><div className="section-heading"><h2>系统信息</h2><Cpu size={16} /></div>
        <dl className="key-values"><div><dt>操作系统</dt><dd>{host.os}</dd></div><div><dt>架构</dt><dd>linux / amd64</dd></div><div><dt>CPU 核数</dt><dd>{host.cores} vCPU</dd></div><div><dt>内存容量</dt><dd>{host.memoryGB} GiB</dd></div><div><dt>运行时间</dt><dd>{host.uptime}</dd></div><div><dt>Agent 版本</dt><dd>v0.1.29</dd></div><div><dt>所在分组</dt><dd>{host.group}</dd></div></dl>
        <button className="text-button" onClick={() => onTab('agent')}>Agent 管理<ExternalLink size={13} /></button>
      </section>
    </div>
    <section className="disk-section"><div className="section-heading"><h2>文件系统</h2><button className="text-button" onClick={() => onTab('files')}>文件管理<ExternalLink size={13} /></button></div>
      <table><thead><tr><th>挂载点</th><th>文件系统</th><th className="num">已用 / 总量</th><th className="num">使用率</th></tr></thead><tbody><tr><td><code>/</code></td><td>ext4</td><td className="num">{host.disk === null ? '—' : `${(host.diskGB * host.disk / 100).toFixed(1)} / ${host.diskGB} GiB`}</td><td className={`num ${(host.disk ?? 0) > 85 ? 'metric-warning' : ''}`}>{percent(host.disk)}</td></tr></tbody></table>
    </section>
  </>
}

function Processes({ host, onAction }: { host: Host; onAction: (label: string) => void }) {
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState<'cpu' | 'memoryMB' | 'pid'>('cpu')
  const [ascending, setAscending] = useState(false)
  const rows = processes(host).filter(p => `${p.command} ${p.pid} ${p.user}`.toLowerCase().includes(query.toLowerCase()))
    .sort((a, b) => (ascending ? 1 : -1) * (a[sort] - b[sort]) || a.pid - b.pid)
  const order = (key: typeof sort) => { setAscending(key === sort ? !ascending : false); setSort(key) }
  return <section aria-label="进程列表">
    <div className="toolbar"><label className="search-field"><Search /><input aria-label="搜索进程" value={query} onChange={e => setQuery(e.target.value)} placeholder="进程名称、PID 或用户" /></label><span className="freshness">当前 {rows.length} 个进程 · 模拟快照</span></div>
    <div className="table-scroll"><table className="process-table"><thead><tr>
      <th>进程</th><th>用户</th>
      {(['pid', 'cpu', 'memoryMB'] as const).map(key => <th className="num" key={key} aria-sort={sort === key ? ascending ? 'ascending' : 'descending' : 'none'}><button className="sort-button" onClick={() => order(key)}>{{ pid: 'PID', cpu: 'CPU', memoryMB: '内存' }[key]}{sort === key && (ascending ? <ArrowUp size={12} /> : <ArrowDown size={12} />)}</button></th>)}
      <th>状态</th><th className="actions-cell">操作</th></tr></thead><tbody>{rows.map(p => <tr key={p.pid}><td className="process-command"><code>{p.command}</code></td><td>{p.user}</td><td className="num">{p.pid}</td><td className="num">{p.cpu.toFixed(1)}%</td><td className="num">{p.memoryMB.toFixed(1)} MiB</td><td>{p.state}</td><td className="actions-cell"><button className="text-button" onClick={() => onAction(`进程 ${p.command}`)}>详情</button></td></tr>)}</tbody></table></div>
    {!rows.length && <div className="filtered-empty"><p>{host.status === 'online' ? '没有匹配的进程' : '当前没有可用的进程快照'}</p>{query && <button className="text-button" onClick={() => setQuery('')}>清空搜索</button>}</div>}
    <div className="table-footer"><span>CPU 按单个进程统计</span><span>按列标题切换排序</span></div>
  </section>
}

export function HostDetail({ host, tab, onTab, onBack, onSwitch, onTerminal, onAction, scenario, onScenario, fileDraft, savedFile, onFileChange, onFileSave, refreshing, onRefresh }: {
  host: Host; tab: HostTab; onTab: (tab: HostTab) => void; onBack: () => void; onSwitch: () => void; onTerminal: () => void; onAction: (label: string) => void
  scenario: Scenario; onScenario: (scenario: Scenario) => void; fileDraft: string; savedFile: string; onFileChange: (value: string) => void; onFileSave: () => void; refreshing: boolean; onRefresh: () => void
}) {
  return <section className="page host-detail">
    <header className="page-title"><div className="flex items-center gap-3 min-w-0"><button className="icon-button" aria-label="返回主机列表" onClick={onBack}><ArrowLeft /></button><h1><button className="host-switch" aria-label={`切换主机，当前 ${host.name}`} onClick={onSwitch}>{host.name}<ChevronDown size={18} /></button></h1><Status status={host.status} /></div>
      <div className="flex items-center gap-2"><button className="icon-button" aria-label="刷新当前主机" disabled={refreshing} onClick={onRefresh}><RefreshCw /></button><button className="button primary" onClick={onTerminal} disabled={host.status === 'offline'} title={host.status === 'offline' ? '主机离线，无法连接' : undefined}><Terminal />打开终端</button></div>
    </header>
    <div className="host-context"><span>{host.address}</span><span>{host.group}</span><span>{host.os}</span><span>{host.cores} 核 / {host.memoryGB} GiB</span></div>
    <div className="detail-tabs" role="tablist" aria-label="主机分区" onKeyDown={event => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
      event.preventDefault()
      const index = tabs.findIndex(item => item.id === tab)
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
      onTab(tabs[next].id)
      document.getElementById(`tab-${tabs[next].id}`)?.focus()
    }}>{tabs.map(item => <button key={item.id} role="tab" tabIndex={tab === item.id ? 0 : -1} aria-selected={tab === item.id} aria-controls="host-tab-panel" id={`tab-${item.id}`} onClick={() => onTab(item.id)}>{item.label}</button>)}</div>
    {host.note && <Notice tone="warning">{host.note}</Notice>}
    <div id="host-tab-panel" role="tabpanel" aria-labelledby={`tab-${tab}`}>
      {scenario !== 'ready' ? <StateView subject={tabs.find(item => item.id === tab)?.label ?? '当前分区'} scenario={scenario} onRetry={() => onScenario('ready')} onReset={() => onScenario('ready')} /> : tab === 'overview' ? <Overview key={host.id} host={host} onTab={onTab} /> : tab === 'processes' ? <Processes key={host.id} host={host} onAction={onAction} /> : tab === 'files' ? <section className="file-demo">
        <div className="section-heading"><h2>本地编辑交互样板</h2><span>{fileDraft !== savedFile ? '有未保存的修改' : '草稿未修改'}</span></div>
        <Notice>仅用于验证切换主机前的未保存保护，不读取或保存主机文件。</Notice>
        <label htmlFor="sample-file">/etc/mizupanel/notes.txt</label><textarea id="sample-file" spellCheck={false} value={fileDraft} onChange={e => onFileChange(e.target.value)} />
        <div className="flex justify-end"><button className="button" disabled={fileDraft === savedFile} onClick={onFileSave}>保存样板草稿</button></div>
      </section> : <div className="empty-state"><h2>{tabs.find(t => t.id === tab)?.label}</h2><p>该分区保留入口，内容不在本轮样板范围内。</p><button className="button" onClick={() => onTab('overview')}>返回主机概览</button></div>}
    </div>
  </section>
}
