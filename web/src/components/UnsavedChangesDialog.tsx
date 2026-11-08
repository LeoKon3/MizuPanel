import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

export type LeaveGuard = (leave: () => void) => void

// One guard serves editor close, panel navigation and browser history.
export function useUnsavedChangesGuard(dirty: boolean, discard: () => void, options: { description?: string; zIndex?: number } = {}) {
  const latest = useRef({ dirty, discard })
  latest.current = { dirty, discard }
  const [pending, setPending] = useState<{ leave: () => void }>()
  const request = useCallback<LeaveGuard>((leave) => {
    if (latest.current.dirty) setPending({ leave })
    else leave()
  }, [])
  const dialog = pending ? <UnsavedChangesDialog {...options} onCancel={() => setPending(undefined)} onConfirm={() => {
    const leave = pending.leave
    setPending(undefined)
    latest.current.discard()
    leave()
  }} /> : null
  return { request, dialog }
}

function UnsavedChangesDialog({ onCancel, onConfirm, description, zIndex }: { onCancel: () => void; onConfirm: () => void; description?: string; zIndex?: number }) {
  const root = useRef<HTMLElement>(null)
  const cancel = useRef(onCancel)
  cancel.current = onCancel
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    root.current?.querySelector<HTMLButtonElement>('button')?.focus()
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); cancel.current() }
      if (event.key === 'Tab') {
        const buttons = Array.from(root.current?.querySelectorAll<HTMLButtonElement>('button') ?? [])
        if (event.shiftKey && document.activeElement === buttons[0]) { event.preventDefault(); buttons[buttons.length - 1]?.focus() }
        else if (!event.shiftKey && document.activeElement === buttons[buttons.length - 1]) { event.preventDefault(); buttons[0]?.focus() }
      }
    }
    document.addEventListener('keydown', key, true)
    return () => { document.removeEventListener('keydown', key, true); if (previous?.isConnected) previous.focus() }
  }, [])
  return createPortal(<div className="soft-modal-overlay fixed inset-0 z-[100] flex items-center justify-center p-4" style={zIndex ? { zIndex } : undefined}>
    <section ref={root} role="dialog" aria-modal="true" aria-label="未保存的修改" className="soft-modal-shell w-full max-w-md p-6">
      <h2 className="text-base font-semibold">未保存的修改</h2>
      <p className="my-5 text-sm leading-6 text-muted-foreground">{description || '当前编辑内容尚未保存。离开会丢弃修改，不会将内容保存到其他主机。'}</p>
      <div className="flex justify-end gap-3"><button type="button" onClick={onCancel} className="console-button">继续编辑</button><button type="button" onClick={onConfirm} className="console-button console-primary">丢弃并离开</button></div>
    </section>
  </div>, document.body)
}
