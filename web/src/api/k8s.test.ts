import { afterEach, describe, expect, test, vi } from 'vitest'
import { fetchK8sClusters } from './k8s'
import { setUnauthorizedHandler } from './client'

describe('K8s API transport', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    setUnauthorizedHandler(() => {})
  })

  test('routes K8s 401 responses through the shared unauthorized handler', async () => {
    const unauthorized = vi.fn()
    setUnauthorizedHandler(unauthorized)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('unauthorized', { status: 401 }))

    await expect(fetchK8sClusters()).rejects.toMatchObject({ status: 401 })
    expect(unauthorized).toHaveBeenCalledOnce()
  })
})
