import { act, render } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { Toast } from './Toast'

afterEach(() => {
  vi.useRealTimers()
})

describe('Toast', () => {
  test('does not restart its lifetime when the parent recreates onClose', () => {
    vi.useFakeTimers()
    const firstClose = vi.fn()
    const secondClose = vi.fn()
    const view = render(<Toast message="saved" type="success" duration={3000} onClose={firstClose} />)

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    view.rerender(<Toast message="saved" type="success" duration={3000} onClose={secondClose} />)

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(firstClose).not.toHaveBeenCalled()
    expect(secondClose).toHaveBeenCalledTimes(1)
  })

  test('starts a new lifetime when the displayed toast changes', () => {
    vi.useFakeTimers()
    const onClose = vi.fn()
    const view = render(<Toast message="first" type="success" duration={3000} onClose={onClose} />)

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    view.rerender(<Toast message="second" type="error" duration={3000} onClose={onClose} />)

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(onClose).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
