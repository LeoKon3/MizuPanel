import { useCallback, useEffect, useRef } from 'react'
import type { LeaveGuard } from '../components/UnsavedChangesDialog'

// An editor decides whether to leave; this hook owns URL/history restoration.
export function usePanelNavigation(onChange: () => void) {
  const guard = useRef<LeaveGuard | undefined>(undefined)
  const index = useRef(0)
  const restoring = useRef(false)
  const guardedPath = useRef('')
  const afterRestore = useRef<(() => void) | undefined>(undefined)
  const change = useRef(onChange)
  change.current = onChange
  const registerGuard = useCallback((next: LeaveGuard | undefined) => {
    guard.current = next
    if (next) guardedPath.current = `${window.location.pathname}${window.location.search}`
  }, [])
  const commit = useCallback((path: string) => {
    if (`${window.location.pathname}${window.location.search}` !== path) {
      index.current += 1
      window.history.pushState({ ...window.history.state, panelIndex: index.current }, '', path)
    }
    change.current()
  }, [])
  const navigate = useCallback((path: string) => {
    const leave = () => {
      if (restoring.current) afterRestore.current = () => commit(path)
      else commit(path)
    }
    if (guard.current) guard.current(leave)
    else leave()
  }, [commit])
  useEffect(() => {
    index.current = typeof window.history.state?.panelIndex === 'number' ? window.history.state.panelIndex : 0
    window.history.replaceState({ ...window.history.state, panelIndex: index.current }, '')
    const handlePop = (event: PopStateEvent) => {
      if (restoring.current) {
        restoring.current = false
        const pending = afterRestore.current
        afterRestore.current = undefined
        pending?.()
        return
      }
      const target = `${window.location.pathname}${window.location.search}`
      const nextIndex = typeof event.state?.panelIndex === 'number' ? event.state.panelIndex : index.current - 1
      if (!guard.current) { index.current = nextIndex; change.current(); return }
      const delta = index.current - nextIndex
      if (delta) { restoring.current = true; window.history.go(delta) }
      else window.history.replaceState(window.history.state, '', guardedPath.current)
      guard.current(() => {
        if (restoring.current) afterRestore.current = () => commit(target)
        else commit(target)
      })
    }
    window.addEventListener('popstate', handlePop)
    return () => {
      afterRestore.current = undefined
      window.removeEventListener('popstate', handlePop)
    }
  }, [commit])
  return { navigate, registerGuard }
}
