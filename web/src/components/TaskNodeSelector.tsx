import { useMemo, useState } from 'react'
import { CheckSquare, Search, Square } from 'lucide-react'

import type { Node } from '../types'

type TaskNodeSelectorProps = {
  nodes: Node[]
  selectedIDs: Set<string>
  onChange: (selected: Set<string>) => void
  disabled?: boolean
  max?: number
}

export function TaskNodeSelector({ nodes, selectedIDs, onChange, disabled = false, max = 100 }: TaskNodeSelectorProps) {
  const [query, setQuery] = useState('')
  const normalizedQuery = query.trim().toLowerCase()
  const visibleNodes = useMemo(() => nodes.filter((node) => {
    if (!normalizedQuery) return true
    return [node.name, node.hostname, node.id, node.ip].some((value) => value.toLowerCase().includes(normalizedQuery))
  }), [nodes, normalizedQuery])
  const visibleIDs = visibleNodes.map((node) => node.id)
  const allVisibleSelected = visibleIDs.length > 0 && visibleIDs.every((id) => selectedIDs.has(id))
  const missingIDs = [...selectedIDs].filter((id) => !nodes.some((node) => node.id === id))

  const toggle = (nodeID: string) => {
    const next = new Set(selectedIDs)
    if (next.has(nodeID)) next.delete(nodeID)
    else if (next.size < max) next.add(nodeID)
    onChange(next)
  }

  const toggleVisible = () => {
    const next = new Set(selectedIDs)
    if (allVisibleSelected) visibleIDs.forEach((id) => next.delete(id))
    else {
      for (const id of visibleIDs) {
        if (next.size >= max) break
        next.add(id)
      }
    }
    onChange(next)
  }

  return (
    <section aria-label="选择执行节点" className="min-w-0">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <input
            aria-label="搜索执行节点"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            disabled={disabled}
            placeholder="搜索节点名称、ID 或 IP"
            className="soft-input min-h-10 w-full pl-9 pr-3 text-sm font-bold placeholder:text-muted-foreground disabled:opacity-50"
          />
        </div>
        <button
          type="button"
          aria-pressed={allVisibleSelected}
          onClick={toggleVisible}
          disabled={disabled || visibleIDs.length === 0}
          className="soft-button inline-flex min-h-10 items-center justify-center gap-2 border border-border bg-card px-3 text-xs font-black text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {allVisibleSelected ? <CheckSquare size={15} aria-hidden="true" /> : <Square size={15} aria-hidden="true" />}
          {allVisibleSelected ? '取消当前结果' : '选择当前结果'}
        </button>
      </div>

      <div className="mt-2 flex items-center justify-between gap-3 text-xs font-bold text-muted-foreground">
        <span>已选择 <strong className="text-foreground">{selectedIDs.size}</strong> / {max} 台</span>
        {selectedIDs.size >= max ? <span className="text-warning">已达到上限</span> : null}
      </div>

      <div className="soft-toolbar mt-3 max-h-64 overflow-y-auto p-2">
        {visibleNodes.length === 0 ? <p className="px-3 py-8 text-center text-sm font-bold text-muted-foreground">没有匹配的节点</p> : visibleNodes.map((node) => (
          <label key={node.id} className={`soft-button flex min-w-0 cursor-pointer items-center gap-3 px-3 py-2 hover:bg-muted ${selectedIDs.has(node.id) ? 'bg-primary/5' : ''}`}>
            <input
              type="checkbox"
              aria-label={`选择 ${node.name || node.hostname || node.id}`}
              checked={selectedIDs.has(node.id)}
              onChange={() => toggle(node.id)}
              disabled={disabled || (!selectedIDs.has(node.id) && selectedIDs.size >= max)}
              className="h-4 w-4 shrink-0 accent-primary"
            />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-black text-foreground">{node.name || node.hostname || node.id}</span>
              <span className="block truncate text-[11px] font-semibold text-muted-foreground">{node.id}{node.ip ? ` · ${node.ip}` : ''}</span>
            </span>
            <span className="flex shrink-0 flex-wrap justify-end gap-1">
              <NodeStateChip online={node.status === 'online'} />
              {node.status === 'online' && node.task_runner_supported === false ? <span className="rounded-full bg-warning/10 px-2 py-0.5 text-[10px] font-black text-warning">需升级</span> : null}
            </span>
          </label>
        ))}
        {missingIDs.map((nodeID) => (
          <label key={nodeID} className="soft-button flex min-w-0 cursor-pointer items-center gap-3 px-3 py-2 hover:bg-muted">
            <input type="checkbox" aria-label={`选择已移除节点 ${nodeID}`} checked onChange={() => toggle(nodeID)} disabled={disabled} className="h-4 w-4 shrink-0 accent-primary" />
            <span className="min-w-0 flex-1 truncate text-sm font-bold text-muted-foreground">{nodeID}</span>
            <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] font-black text-muted-foreground">已移除</span>
          </label>
        ))}
      </div>
    </section>
  )
}

function NodeStateChip({ online }: { online: boolean }) {
  return <span className={`rounded-full px-2 py-0.5 text-[10px] font-black ${online ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'}`}>{online ? '在线' : '离线'}</span>
}
