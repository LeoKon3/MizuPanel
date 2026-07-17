import { useMemo, useState } from 'react'
import { ChevronDown } from 'lucide-react'

import { NodeTagChip } from '../components/NodeOrganizationControls'
import { formatPercent } from '../lib/format'
import type { Node } from '../types'

type NodeListProps = {
  nodes: Node[]
  selectedNodeID?: string
  onSelectNode: (node: Node) => void
}

type NodeGroupSection = {
  id: string
  name: string
  nodes: Node[]
  ungrouped: boolean
}

export function NodeList({ nodes, selectedNodeID, onSelectNode }: NodeListProps) {
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set())
  const sections = useMemo(() => groupNodes(nodes), [nodes])

  const toggleGroup = (groupID: string) => {
    setCollapsedGroups((current) => {
      const next = new Set(current)
      if (next.has(groupID)) next.delete(groupID)
      else next.add(groupID)
      return next
    })
  }

  return (
    <div className="space-y-3">
      {sections.map((section) => {
        const collapsed = collapsedGroups.has(section.id)
        return (
          <section key={section.id} aria-label={section.name}>
            <button type="button" aria-expanded={!collapsed} onClick={() => toggleGroup(section.id)} className="flex min-h-8 w-full items-center gap-2 px-1 text-left text-[11px] font-black uppercase tracking-[0.12em] text-muted-foreground hover:text-foreground">
              <ChevronDown size={14} className={`shrink-0 transition-transform ${collapsed ? '-rotate-90' : ''}`} aria-hidden="true" />
              <span className="min-w-0 flex-1 truncate normal-case tracking-normal">{section.name}</span>
              <span className="rounded-full bg-muted px-2 py-0.5 text-[10px]">{section.nodes.length}</span>
            </button>
            {!collapsed ? <div className="mt-1 space-y-2">{section.nodes.map((node) => <NodeListItem key={node.id} node={node} active={node.id === selectedNodeID} onSelect={() => onSelectNode(node)} />)}</div> : null}
          </section>
        )
      })}
    </div>
  )
}

function NodeListItem({ node, active, onSelect }: { node: Node, active: boolean, onSelect: () => void }) {
  const metric = node.latest_metric
  const statusText = node.status === 'online' ? '在线' : '离线'
  const tags = node.tags ?? []
  return (
    <button type="button" onClick={onSelect} className={`soft-button group w-full cursor-pointer border px-3 py-3 text-left duration-200 focus:outline-none focus:ring-4 focus:ring-primary/20 ${active ? 'border-primary/40 bg-primary/10 shadow-sm' : 'border-border bg-card hover:bg-surface'}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2"><span className={`h-2.5 w-2.5 rounded-full ${node.status === 'online' ? 'bg-success shadow-[0_0_14px_rgb(var(--success)/0.45)]' : 'bg-muted-foreground/40'}`} /><p className="truncate text-sm font-black text-foreground">{node.name || node.hostname}</p></div>
          <p className="mt-1 truncate text-xs font-semibold text-muted-foreground">{node.ip || '未知 IP'}</p>
          <p className="mt-0.5 truncate text-[11px] font-semibold text-muted-foreground">{node.hostname || '未知主机'} · {node.os}/{node.arch}</p>
        </div>
        <span className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-black ${node.status === 'online' ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'}`}>{statusText}</span>
      </div>
      {tags.length > 0 ? <div className="mt-2 flex min-w-0 flex-wrap gap-1">{tags.slice(0, 2).map((tag) => <NodeTagChip key={tag.id} tag={tag} compact />)}{tags.length > 2 ? <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] font-black text-muted-foreground">+{tags.length - 2}</span> : null}</div> : null}
      {active ? <div className="mt-3 grid grid-cols-3 gap-2"><MiniStat label="CPU" value={metric ? formatPercent(metric.cpu_usage) : '—'} /><MiniStat label="内存" value={metric ? formatPercent(metric.memory_usage) : '—'} /><MiniStat label="磁盘" value={metric ? formatPercent(metric.disk_usage) : '—'} /></div> : null}
    </button>
  )
}

function groupNodes(nodes: Node[]): NodeGroupSection[] {
  const groups = new Map<string, NodeGroupSection>()
  for (const node of nodes) {
    const groupID = node.group?.id ?? 'ungrouped'
    const section = groups.get(groupID) ?? { id: groupID, name: node.group?.name ?? '未分组', nodes: [], ungrouped: !node.group }
    section.nodes.push(node)
    groups.set(groupID, section)
  }
  return [...groups.values()].sort((left, right) => {
    if (left.ungrouped !== right.ungrouped) return left.ungrouped ? 1 : -1
    return left.name.localeCompare(right.name, 'zh-CN')
  })
}

function MiniStat({ label, value }: { label: string, value: string }) {
  return <div className="rounded-xl border border-primary/20 bg-surface/70 px-2.5 py-2"><p className="text-[10px] font-black tracking-[0.1em] text-muted-foreground">{label}</p><p className="mt-0.5 text-xs font-black text-foreground">{value}</p></div>
}
