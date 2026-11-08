import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, Search, X } from 'lucide-react'
import type { Node } from '../types'

export function NodeSwitcher({ nodes, selectedID, onSelect, onClose }: { nodes: Node[]; selectedID?: string; onSelect: (node: Node) => void; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const input = useRef<HTMLInputElement>(null)
  const root = useRef<HTMLElement>(null)
  const close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    input.current?.focus()
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close.current()
      if (event.key === 'Tab') {
        const elements = Array.from(root.current?.querySelectorAll<HTMLElement>('button,input') ?? [])
        if (event.shiftKey && document.activeElement === elements[0]) { event.preventDefault(); elements[elements.length - 1]?.focus() }
        else if (!event.shiftKey && document.activeElement === elements[elements.length - 1]) { event.preventDefault(); elements[0]?.focus() }
      }
    }
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('keydown', key); if (previous?.isConnected) previous.focus() }
  }, [])
  const filtered = nodes.filter(node => `${node.name} ${node.hostname} ${node.ip}`.toLowerCase().includes(query.trim().toLowerCase())).sort((a, b) => (a.name || a.hostname).localeCompare(b.name || b.hostname, 'zh-CN', { numeric: true }) || a.id.localeCompare(b.id))
  return createPortal(<div className="soft-modal-overlay fixed inset-0 z-[70] flex items-center justify-center p-4" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
    <section ref={root} role="dialog" aria-modal="true" aria-label="切换主机" className="soft-modal-shell w-full max-w-md p-4">
      <div className="mb-4 flex items-center justify-between"><h2 className="text-base font-semibold">切换主机</h2><button className="console-icon-button" aria-label="关闭主机切换" onClick={onClose}><X size={16} /></button></div>
      <label className="console-search"><Search size={15} /><input ref={input} aria-label="查找主机" placeholder="输入主机名称或 IP" value={query} onChange={event => setQuery(event.target.value)} /></label>
      <div className="mt-3 max-h-80 overflow-auto">{filtered.map(node => <button type="button" key={node.id} className="node-switch-option" onClick={() => onSelect(node)} aria-label={`切换到 ${node.name || node.hostname}`}><span className="min-w-0 flex-1 text-left"><strong className="block truncate text-sm font-medium">{node.name || node.hostname}</strong><span className="text-xs text-muted-foreground">{node.ip || '—'}</span></span><span className="text-xs text-muted-foreground">{node.status === 'online' ? '在线' : '离线'}</span>{node.id === selectedID && <Check size={15} />}</button>)}{!filtered.length && <p className="py-6 text-center text-sm text-muted-foreground">没有匹配的主机</p>}</div>
    </section>
  </div>, document.body)
}
