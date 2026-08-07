import assert from 'node:assert/strict'
import test from 'node:test'

import { buildLogbookListURL } from '../src/pages/logbook/apiQuery.ts'

test('serializes pagination and active filters for ordinary users', () => {
  assert.equal(
    buildLogbookListURL({ page: 2, page_size: 20, callsign: 'BA1AA', mode: 'FM' }),
    '/api/logbooks?page=2&page_size=20&callsign=BA1AA&mode=FM',
  )
})

test('uses the admin endpoint and omits empty filters', () => {
  assert.equal(
    buildLogbookListURL({ page: 1, page_size: 10, callsign: '', frequency: 0 }, true),
    '/api/admin/logbooks?page=1&page_size=10',
  )
})
