export interface BatchResult<T> { succeeded: T[]; failed: { item: T; error: unknown }[] }

// Limit concurrent requests and retain individual failures for safe retries.
export async function runBatch<T>(items: readonly T[], action: (item: T) => Promise<unknown>, concurrency = 4): Promise<BatchResult<T>> {
  const result: BatchResult<T> = { succeeded: [], failed: [] }
  let next = 0
  const worker = async () => {
    while (next < items.length) {
      const item = items[next++]
      try { await action(item); result.succeeded.push(item) }
      catch (error) { result.failed.push({ item, error }) }
    }
  }
  await Promise.all(Array.from({ length: Math.min(items.length, Math.max(1, concurrency)) }, worker))
  return result
}
