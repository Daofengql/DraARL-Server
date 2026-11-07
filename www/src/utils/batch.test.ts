import { expect, it } from 'vitest'
import { runBatch } from './batch'

it('bounds concurrency and retains only failed items for retries', async () => {
  let active = 0; let peak = 0
  const result = await runBatch([1, 2, 3, 4, 5, 6], async (item) => {
    active++; peak = Math.max(peak, active)
    await new Promise((resolve) => setTimeout(resolve, 1))
    active--
    if (item % 2 === 0) throw new Error('rejected')
  }, 2)
  expect(peak).toBe(2)
  expect(result.succeeded.sort()).toEqual([1, 3, 5])
  expect(result.failed.map(({ item }) => item).sort()).toEqual([2, 4, 6])
})
it('accepts empty batches and normalizes a zero concurrency limit', async () => {
  expect(await runBatch([], async () => {})).toEqual({ succeeded: [], failed: [] })
  expect((await runBatch([1], async () => {}, 0)).succeeded).toEqual([1])
})
