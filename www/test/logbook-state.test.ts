import assert from 'node:assert/strict'
import test from 'node:test'

import { logbookBatchDeletePath } from '../src/pages/logbook/apiQuery.ts'
import { createEmptyLogbookFilters, hasActiveLogbookFilters } from '../src/pages/logbook/filterState.ts'
import { buildPresetOrders } from '../src/pages/logbook/presetOrder.ts'
import type { RadioPreset } from '../src/pages/logbook/types.ts'

test('applies and resets filter state without retaining stale values', () => {
  const empty = createEmptyLogbookFilters()
  assert.equal(hasActiveLogbookFilters(empty), false)
  assert.equal(hasActiveLogbookFilters({ ...empty, callsign: 'BA1AA' }), true)
  assert.deepEqual(createEmptyLogbookFilters(), empty)
})

test('selects the correct batch deletion endpoint', () => {
  assert.equal(logbookBatchDeletePath(false), '/api/logbooks/batch')
  assert.equal(logbookBatchDeletePath(true), '/api/admin/logbooks/batch')
})

test('serializes preset order after drag sorting', () => {
  const presets = [{ id: 9 }, { id: 4 }, { id: 7 }] as RadioPreset[]
  assert.deepEqual(buildPresetOrders(presets), [
    { id: 9, order: 0 },
    { id: 4, order: 1 },
    { id: 7, order: 2 },
  ])
})
