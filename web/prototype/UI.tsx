import { useEffect, useRef, type ReactNode } from 'react'
import { AlertCircle, CircleCheck, Clock3, ServerOff, X } from 'lucide-react'
import type { HostStatus, Scenario } from './model'

export function Status({ status }: { status: HostStatus }) {
  const label = { online: '在线', offline: '离线', stale: '数据过期' }[status]
  return <span className={`host-status status-${status}`}><i aria-hidden="true" />{label}</span>
}

export function StateView({ scenario, onRetry, onReset, subject = '主机列表' }: { scenario: Scenario; onRetry: () => void; onReset: () => void; subject?: string }) {
  const isList = subject === '主机列表'
  if (scenario === 'loading') return <div className="skeleton-rows" role="status" aria-label={`正在加载${subject}`}>
    {[0, 1, 2, 3, 4, 5].map(row => <div className="skeleton-row" key={row}><span /><span /><span /><span /></div>)}
    <p>正在加载{subject}…</p>
  </div>
  return <div className="empty-state" role={scenario === 'error' ? 'alert' : 'status'}>
    {scenario === 'error' ? <AlertCircle /> : <ServerOff />}
    <h2>{scenario === 'error' ? `${subject}暂时无法加载` : isList ? '暂无主机' : '暂无可用快照'}</h2>
    <p>{scenario === 'error' ? '演示连接超时状态，其他区域仍可使用。' : isList ? '添加第一台主机后，这里会显示状态与资源使用。' : '演示当前分区没有数据时的布局，不将缺失值显示为零。'}</p>
    <button className="button" onClick={scenario === 'error' ? onRetry : onReset}>{scenario === 'error' ? '重试' : isList ? '加载演示主机' : '加载演示快照'}</button>
  </div>
}

export function Notice({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'warning' }) {
  return <div className={`notice notice-${tone}`}><Clock3 size={16} aria-hidden="true" /><span>{children}</span></div>
}

export function Dialog({ title, children, onClose, className = '' }: { title: string; children: ReactNode; onClose: () => void; className?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const closeRef = useRef(onClose)
  closeRef.current = onClose
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    const root = ref.current!
    const focusables = () => Array.from(root.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled),select:not(:disabled),a[href],[tabindex="0"]'))
    ;(root.querySelector<HTMLElement>('[data-autofocus]') ?? focusables()[0] ?? root).focus()
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); closeRef.current(); return }
      if (event.key !== 'Tab') return
      const nodes = focusables(), first = nodes[0], last = nodes[nodes.length - 1]
      if (!first) { event.preventDefault(); root.focus(); return }
      if (event.shiftKey && (document.activeElement === first || document.activeElement === root)) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('keydown', key); if (previous?.isConnected) previous.focus() }
  }, [])
  return <div className="modal-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
    <div className={`modal ${className}`} role="dialog" aria-modal="true" aria-label={title} ref={ref} tabIndex={-1}>
      <header className="modal-header"><h2>{title}</h2><button className="icon-button" aria-label={`关闭${title}`} onClick={onClose}><X /></button></header>
      {children}
    </div>
  </div>
}

export function ScopeNote() {
  return <div className="scope-note"><CircleCheck size={15} aria-hidden="true" /><span>独立设计样板 · 所有数据均为模拟，不连接真实主机</span></div>
}
