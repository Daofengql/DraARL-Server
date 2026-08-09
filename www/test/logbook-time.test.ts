import assert from 'node:assert/strict'
import test from 'node:test'

import { bjtToUtc, utcToBjt } from '../src/pages/logbook/time.ts'

test('converts UTC and BJT across date and year boundaries', () => {
  assert.equal(utcToBjt('2025-12-31 18:30:00'), '2026-01-01 02:30:00')
  assert.equal(bjtToUtc('2026-01-01 02:30:00'), '2025-12-31 18:30:00')
})

test('keeps empty and invalid values compatible', () => {
  assert.equal(utcToBjt(''), '')
  assert.equal(bjtToUtc(''), '')
  assert.equal(utcToBjt('not-a-time'), 'not-a-time')
  assert.equal(bjtToUtc('not-a-time'), 'not-a-time')
})
