import { afterEach, describe, expect, it } from 'vitest'
import { parseAPIDate, setAPITimezone } from './apiTime'

afterEach(() => setAPITimezone(''))
describe('legacy API wallclock contract', () => {
  it.each([
    ['UTC', '2026-11-07 16:28:36', '2026-11-07T16:28:36.000Z'],
    ['+08:00', '2026-11-07 16:28:36', '2026-11-07T08:28:36.000Z'],
    ['America/New_York', '2026-07-07 12:00:00', '2026-07-07T16:00:00.000Z'],
    ['America/New_York', '2026-11-07 12:00:00', '2026-11-07T17:00:00.000Z'],
  ])('interprets %s', (zone, source, expected) => {
    setAPITimezone(zone)
    expect(parseAPIDate(source).toISOString()).toBe(expected)
  })
  it('preserves explicit offsets and copies Date inputs', () => {
    expect(parseAPIDate('2026-11-07T16:28:36+08:00', 'UTC').toISOString()).toBe('2026-11-07T08:28:36.000Z')
    const original = new Date(100)
    expect(parseAPIDate(original)).not.toBe(original)
    expect(+parseAPIDate(original)).toBe(100)
    expect(+parseAPIDate(123)).toBe(123)
  })
  it('ignores malformed headers and tolerates unsupported timezone names', () => {
    setAPITimezone('UTC')
    setAPITimezone(undefined)
    expect(parseAPIDate('2026-01-01 00:00:00').toISOString()).toBe('2026-01-01T00:00:00.000Z')
    expect(+parseAPIDate('2026-01-01 00:00:00', 'Invalid/Zone')).toBe(+new Date('2026-01-01T00:00:00'))
    expect(Number.isNaN(+parseAPIDate('invalid'))).toBe(true)
  })
})
