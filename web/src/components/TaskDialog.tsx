import { useEffect, useRef, type KeyboardEvent, type ReactNode, type RefObject } from 'react'
import { X } from 'lucide-react'

type TaskDialogProps = {
  ariaLabel: string
  title: string
  description?: string
  children: ReactNode
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg' | 'xl'
  initialFocusRef?: RefObject<HTMLElement | null>
  returnFocusRef?: RefObject<HTMLElement | null>
  fallbackFocusRef?: RefObject<HTMLElement | null>
  closeDisabled?: boolean
  onClose: () => void
}

const focusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

const sizeClasses = {
  sm: 'max-w-md',
  md: 'max-w-2xl',
  lg: 'max-w-4xl',
  xl: 'max-w-6xl'
} as const

export function TaskDialog({ ariaLabel, title, description, children, footer, size = 'md', initialFocusRef, returnFocusRef, fallbackFocusRef, closeDisabled = false, onClose }: TaskDialogProps) {
  const dialogRef = useRef<HTMLElement | null>(null)
  const closeButtonRef = useRef<HTMLButtonElement | null>(null)
  const closeRef = useRef(onClose)
  const closeDisabledRef = useRef(closeDisabled)
  closeRef.current = onClose
  closeDisabledRef.current = closeDisabled

  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return undefined
    const initial = initialFocusRef?.current || getFocusable(dialog)[0] || dialog
    initial.focus()
    return () => {
      const trigger = returnFocusRef?.current
      const fallback = fallbackFocusRef?.current
      const target = trigger?.isConnected ? trigger : fallback?.isConnected ? fallback : undefined
      target?.focus()
    }
  }, [fallbackFocusRef, initialFocusRef, returnFocusRef])

  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === 'Escape') {
      if (closeDisabledRef.current) return
      event.preventDefault()
      event.stopPropagation()
      closeRef.current()
      return
    }
    if (event.key !== 'Tab') return
    const dialog = dialogRef.current
    if (!dialog) return
    const focusable = getFocusable(dialog)
    if (focusable.length === 0) {
      event.preventDefault()
      dialog.focus()
      return
    }
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const active = document.activeElement
    if (event.shiftKey && (active === first || active === dialog || !dialog.contains(active))) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && (active === last || active === dialog || !dialog.contains(active))) {
      event.preventDefault()
      first.focus()
    }
  }

  return (
    <div
      className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-5"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !closeDisabled) onClose()
      }}
    >
      <section
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={ariaLabel}
        tabIndex={-1}
        onKeyDown={handleKeyDown}
        className={`soft-modal-shell flex max-h-[92vh] w-full ${sizeClasses[size]} flex-col outline-none`}
      >
        <header className="soft-modal-header flex shrink-0 items-start justify-between gap-4 border-b px-4 py-4 sm:px-5">
          <div className="min-w-0">
            <h2 className="break-words text-lg font-black text-foreground [overflow-wrap:anywhere]">{title}</h2>
            {description ? <p className="mt-1 break-words text-xs font-semibold leading-5 text-muted-foreground [overflow-wrap:anywhere]">{description}</p> : null}
          </div>
          <button
            ref={closeButtonRef}
            type="button"
            aria-label={`关闭${ariaLabel}`}
            title="关闭"
            onClick={onClose}
            disabled={closeDisabled}
            className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <X size={16} aria-hidden="true" />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4 sm:px-5">{children}</div>
        {footer ? <footer className="soft-modal-footer shrink-0 border-t px-4 py-4 sm:px-5">{footer}</footer> : null}
      </section>
    </div>
  )
}

function getFocusable(dialog: HTMLElement) {
  return Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector))
    .filter((element) => element.getAttribute('aria-hidden') !== 'true' && !element.closest('[hidden]'))
}
