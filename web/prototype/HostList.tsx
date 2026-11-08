import { useEffect, useRef } from 'react'
import { ArrowUp, ArrowUpRight, FolderCog, Plus, RefreshCw, Search, Terminal, X } from 'lucide-react'
import { filterHosts, hostCounts, percent, type Filters, type Host, type Scenario } from './model'
import { StateView, Status } from './UI'

export function HostList({ rows, filters, onFilter, selected, onSelect, onOpen, onTerminal, onAction, scenario, onScenario, scrollTop, onScroll, refreshedAt, refreshing, onRefresh }: {
  rows: Host[]; filters: Filters; onFilter: (next: Filters) => void; selected: Set<string>; onSelect: (next: Set<string>) => void
  onOpen: (host: Host) => void; onTerminal: (host: Host) => void; onAction: (label: string) => void
  scenario: Scenario; onScenario: (scenario: Scenario) => void; scrollTop: number; onScroll: (top: number) => void
  refreshedAt: string; refreshing: boolean; onRefresh: () => void
}) {
  const visible = filterHosts(rows, filters)
  const scoped = filterHosts(rows, { ...filters, status: 'all' })
  const counts = hostCounts(scoped)
  const container = useRef<HTMLDivElement>(null)
  const selectAll = useRef<HTMLInputElement>(null)
  const selectedVisible = visible.filter(h => selected.has(h.id)).length
  useEffect(() => { if (container.current) container.current.scrollTop = scrollTop }, [])
  useEffect(() => { if (selectAll.current) selectAll.current.indeterminate = selectedVisible > 0 && selectedVisible < visible.length }, [selectedVisible, visible.length])
  const toggle = (id: string) => { const next = new Set(selected); next.has(id) ? next.delete(id) : next.add(id); onSelect(next) }
  const setFilter = (next: Filters) => { onSelect(new Set()); onFilter(next) }
  return <section className="page host-list">
    <header className="page-title"><h1>主机 <span className="title-count">{scenario === 'empty' ? 0 : rows.length}</span></h1><button className="button primary" onClick={() => onAction('添加主机')}><Plus />添加主机</button></header>
    <div className="status-tabs" aria-label="主机状态筛选">
      {(['all', 'online', 'offline', 'stale'] as const).map(key => <button key={key} aria-pressed={filters.status === key} className={filters.status === key ? 'active' : ''} onClick={() => setFilter({ ...filters, status: key })}>
        {{ all: '全部', online: '在线', offline: '离线', stale: '数据过期' }[key]}<span>{scenario === 'empty' ? 0 : counts[key]}</span>
      </button>)}
    </div>
    <div className="toolbar">
      <div className="flex items-center gap-3 min-w-0">
        <label className="search-field"><Search aria-hidden="true" /><input aria-label="搜索主机" placeholder="搜索名称或 IP 地址" value={filters.query} onChange={e => setFilter({ ...filters, query: e.target.value })} />{filters.query && <button className="clear-search" aria-label="清空搜索" onClick={() => setFilter({ ...filters, query: '' })}><X /></button>}</label>
        <select aria-label="筛选分组" value={filters.group} onChange={e => setFilter({ ...filters, group: e.target.value })}><option value="">全部分组</option>{Array.from(new Set(rows.map(h => h.group))).map(group => <option key={group}>{group}</option>)}</select>
      </div>
      <div className="flex items-center gap-3"><span className="freshness" role="status">{refreshing ? '正在刷新…' : `${refreshedAt} 更新`}</span><button className="icon-button" aria-label="刷新主机" disabled={refreshing} onClick={onRefresh}><RefreshCw /></button><button className="button ghost" onClick={() => onAction('管理分组')}><FolderCog />分组</button></div>
    </div>
    {selected.size > 0 && <div className="selection-bar"><span>已选 {selected.size} 台</span><button className="button compact" onClick={() => onAction('批量管理')}>批量管理</button><button className="text-button" onClick={() => onSelect(new Set())}>取消选择</button></div>}
    {scenario !== 'ready' ? <StateView scenario={scenario} onRetry={() => onScenario('ready')} onReset={() => onScenario('ready')} /> : <>
      <div className="table-scroll" ref={container} onScroll={e => onScroll(e.currentTarget.scrollTop)}>
        <table className="host-table"><thead><tr>
          <th className="check-cell"><input ref={selectAll} type="checkbox" aria-label="选择当前主机" checked={visible.length > 0 && selectedVisible === visible.length} disabled={!visible.length} onChange={() => onSelect(selectedVisible === visible.length ? new Set() : new Set(visible.map(h => h.id)))} /></th>
          <th><span className="inline-flex items-center gap-1">名称 / 地址<ArrowUp size={12} aria-label="按名称升序" /></span></th><th>状态</th>
          <th className="num">CPU</th><th className="num">内存</th><th className="num">磁盘</th><th className="actions-cell">操作</th>
        </tr></thead><tbody>{visible.map(host => <tr key={host.id} className={selected.has(host.id) ? 'selected' : ''}>
          <td className="check-cell"><input type="checkbox" aria-label={`选择 ${host.name}`} checked={selected.has(host.id)} onChange={() => toggle(host.id)} /></td>
          <td><button className="host-link" onClick={() => onOpen(host)}><span>{host.name}</span><ArrowUpRight size={13} aria-hidden="true" /></button><div className="host-address">{host.address}<span className="inline-group">{host.group}</span></div></td>
          <td><Status status={host.status} /></td>
          {(['cpu', 'memory', 'disk'] as const).map(metric => <td className={`num ${host[metric] === null ? 'missing' : ''} ${metric === 'disk' && (host.disk ?? 0) > 85 ? 'metric-warning' : ''}`} key={metric} title={metric === 'disk' && (host.disk ?? 0) > 85 ? '磁盘使用率超过 85%' : undefined}>{percent(host[metric])}</td>)}
          <td className="actions-cell"><button className="button ghost compact" disabled={host.status === 'offline'} title={host.status === 'offline' ? '主机离线，无法连接' : '查看只读终端样板'} aria-label={`打开 ${host.name} 终端`} onClick={() => onTerminal(host)}><Terminal />终端</button></td>
        </tr>)}</tbody></table>
        {!visible.length && <div className="filtered-empty"><p>没有匹配的主机</p><button className="text-button" onClick={() => setFilter({ query: '', group: '', status: 'all' })}>清除筛选</button></div>}
      </div>
      <footer className="table-footer"><span>显示 {visible.length} 台，共 {rows.length} 台</span><span>按名称排列 · 指标更新不改变行位置</span></footer>
    </>}
  </section>
}
