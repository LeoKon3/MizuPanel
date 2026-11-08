import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'
import { useOverviewSource } from './useOverviewSources'

function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { resolve, promise } }
afterEach(() => { cleanup(); vi.useRealTimers() })

describe('independent overview polling', () => {
  test('times out and cancels a hung read then retries without overlapping', async () => {
    vi.useFakeTimers()
    const load = vi.fn<(signal: AbortSignal) => Promise<string[]>>().mockImplementation(() => new Promise(() => {}))
    const { result, unmount } = renderHook(() => useOverviewSource(load))
    expect(load).toHaveBeenCalledTimes(1)
    const signal = load.mock.calls[0][0]
    await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
    expect(signal.aborted).toBe(true)
    expect(result.current).toMatchObject({ loading: false, error: '请求超时' })
    expect(result.current.data).toBeUndefined()
    load.mockResolvedValue(['recovered'])
    await act(async () => { await vi.advanceTimersByTimeAsync(30_000) })
    expect(load).toHaveBeenCalledTimes(2)
    expect(result.current).toMatchObject({ data: ['recovered'], loading: false })
    expect(result.current.error).toBeUndefined()
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  test('an explicitly refreshed response wins over the older promise', async () => {
    const old = deferred<string[]>()
    const load = vi.fn<(signal: AbortSignal) => Promise<string[]>>().mockReturnValueOnce(old.promise).mockResolvedValue(['new'])
    const { result } = renderHook(() => useOverviewSource(load))
    await act(async () => { await result.current.refresh() })
    expect(load.mock.calls[0][0].aborted).toBe(true)
    await act(async () => { old.resolve(['old']) })
    expect(result.current.data).toEqual(['new'])
  })

  test('unmount aborts pending network work and clears its timeout', async () => {
    vi.useFakeTimers()
    const load = vi.fn<(signal: AbortSignal) => Promise<string[]>>().mockImplementation(() => new Promise(() => {}))
    const { unmount } = renderHook(() => useOverviewSource(load))
    unmount()
    await act(async () => { await Promise.resolve() })
    expect(load.mock.calls[0][0].aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })
})
