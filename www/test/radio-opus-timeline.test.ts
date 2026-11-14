import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveMixerStartTime } from '../src/services/radio/opus.ts'

test('keeps the existing mixer timeline while buffered audio remains', () => {
  assert.equal(resolveMixerStartTime(0.2, 0.125), 0.2)
})

test('adds startup buffering only after the mixer timeline underruns', () => {
  assert.equal(resolveMixerStartTime(0, 1), 1.16)
  assert.ok(Math.abs(resolveMixerStartTime(1.2, 1.205) - 1.365) < Number.EPSILON * 2)
  assert.equal(resolveMixerStartTime(0, 1, 0.24), 1.24)
})
