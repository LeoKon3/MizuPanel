import { describe, expect, test } from 'vitest'

import { mapWithConcurrency } from './concurrency'

describe('mapWithConcurrency', () => {
  test('preserves result order and never exceeds the limit', async () => {
    let active = 0
    let peak = 0
    const resolvers: Array<() => void> = []
    const resultPromise = mapWithConcurrency([1, 2, 3, 4, 5], 2, async (value) => {
      active += 1
      peak = Math.max(peak, active)
      await new Promise<void>((resolve) => resolvers.push(resolve))
      active -= 1
      return value * 10
    })

    while (resolvers.length < 2) await Promise.resolve()
    resolvers.shift()?.()
    while (resolvers.length < 2) await Promise.resolve()
    resolvers.shift()?.()
    while (resolvers.length < 2) await Promise.resolve()
    resolvers.shift()?.()
    while (resolvers.length < 2) await Promise.resolve()
    resolvers.shift()?.()
    while (resolvers.length < 1) await Promise.resolve()
    resolvers.shift()?.()

    await expect(resultPromise).resolves.toEqual([10, 20, 30, 40, 50])
    expect(peak).toBe(2)
  })
})
