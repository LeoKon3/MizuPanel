import { useCallback, useEffect, useRef, useState } from 'react'
import { getActiveAlerts, getApplicationServices, getAutomationRuns, getK8sClusters, getUptimeMonitors } from '../../api/client'
import type { OverviewSourceState } from '../../types'

const pollInterval = 30_000
const timeoutMs = 15_000
const loadAlerts = async (signal: AbortSignal) => (await getActiveAlerts(signal)).history ?? []
const loadMonitors = async (signal: AbortSignal) => (await getUptimeMonitors(signal)).monitors ?? []
const loadClusters = async (signal: AbortSignal) => (await getK8sClusters(signal)).clusters ?? []
const loadServices = (signal: AbortSignal) => getApplicationServices(signal)
const loadRuns = (signal: AbortSignal) => {
  const now = Date.now()
  return getAutomationRuns({ from: new Date(now - 86400_000).toISOString(), to: new Date(now).toISOString(), limit: 5 }, signal)
}

// Each source owns its poll and cancellation. One failed/hung dependency must not
// hide other sources, become an empty success, or overwrite a newer snapshot.
export function useOverviewSource<T>(load: (signal: AbortSignal) => Promise<T>) {
  const [state, setState] = useState<OverviewSourceState<T>>({ loading: true })
  const active = useRef(false)
  const sequence = useRef(0)
  const controller = useRef<AbortController | undefined>(undefined)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const refresh = useCallback(async () => {
    clearTimeout(timer.current)
    const id = ++sequence.current
    controller.current?.abort()
    const current = new AbortController()
    controller.current = current
    if (!active.current) return
    setState(previous => ({ ...previous, loading: true }))
    let timeout: ReturnType<typeof setTimeout> | undefined
    let abort: (() => void) | undefined
    try {
      const expired = new Promise<never>((_, reject) => {
        abort = () => reject(new DOMException('请求已取消', 'AbortError'))
        current.signal.addEventListener('abort', abort, { once: true })
        timeout = setTimeout(() => { reject(new Error('请求超时')); current.abort() }, timeoutMs)
      })
      const data = await Promise.race([load(current.signal), expired])
      if (!active.current || id !== sequence.current) return
      setState({ data, loading: false, updatedAt: Date.now() })
      return true
    } catch (error) {
      if (!active.current || id !== sequence.current) return
      setState(previous => ({ ...previous, loading: false, error: error instanceof Error ? error.message : '读取失败' }))
      return false
    } finally {
      clearTimeout(timeout)
      if (abort) current.signal.removeEventListener('abort', abort)
      if (active.current && id === sequence.current) timer.current = setTimeout(() => void refresh(), pollInterval)
    }
  }, [load])
  useEffect(() => {
    active.current = true
    void refresh()
    return () => { active.current = false; sequence.current += 1; clearTimeout(timer.current); controller.current?.abort() }
  }, [refresh])
  return { ...state, refresh }
}

export function useOverviewSources() {
  const alerts = useOverviewSource(loadAlerts)
  const monitors = useOverviewSource(loadMonitors)
  const services = useOverviewSource(loadServices)
  const clusters = useOverviewSource(loadClusters)
  const runs = useOverviewSource(loadRuns)
  const refresh = () => Promise.all([alerts.refresh(), monitors.refresh(), services.refresh(), clusters.refresh(), runs.refresh()])
  return { alerts, monitors, services, clusters, runs, refresh }
}
