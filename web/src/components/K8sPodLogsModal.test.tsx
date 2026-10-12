import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import K8sPodLogsModal from './K8sPodLogsModal'
import { fetchK8sPodLogs } from '../api/k8s'

vi.mock('../api/k8s', () => ({ fetchK8sPodLogs: vi.fn() }))

type Deferred<T> = { promise: Promise<T>; resolve: (value: T) => void }
function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

describe('K8sPodLogsModal request lifecycle', () => {
  beforeEach(() => vi.clearAllMocks())

  test('aborts the previous request and ignores its stale response', async () => {
    const first = deferred<{ success: true; logs: string }>()
    const second = deferred<{ success: true; logs: string }>()
    vi.mocked(fetchK8sPodLogs)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)

    const { rerender } = render(
      <K8sPodLogsModal clusterId="c1" namespace="default" podName="web" open onClose={vi.fn()} />
    )
    await act(async () => {})
    const firstSignal = vi.mocked(fetchK8sPodLogs).mock.calls[0][6]
    expect(firstSignal).toBeInstanceOf(AbortSignal)

    fireEvent.change(screen.getByRole('combobox'), { target: { value: '200' } })
    await act(async () => {})
    expect(firstSignal?.aborted).toBe(true)

    await act(async () => {
      second.resolve({ success: true, logs: 'new log' })
      await second.promise
    })
    expect(screen.getByText('new log')).toBeInTheDocument()

    await act(async () => {
      first.resolve({ success: true, logs: 'stale log' })
      await first.promise
    })
    expect(screen.queryByText('stale log')).not.toBeInTheDocument()
    expect(screen.getByText('new log')).toBeInTheDocument()
  })
})
