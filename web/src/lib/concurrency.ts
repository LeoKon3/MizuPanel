export async function mapWithConcurrency<Input, Output>(items: Input[], limit: number, worker: (item: Input, index: number) => Promise<Output>): Promise<Output[]> {
  if (!Number.isInteger(limit) || limit < 1) throw new Error('concurrency limit must be a positive integer')
  const results = new Array<Output>(items.length)
  let nextIndex = 0

  const run = async () => {
    while (nextIndex < items.length) {
      const index = nextIndex
      nextIndex += 1
      results[index] = await worker(items[index], index)
    }
  }

  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, run))
  return results
}
