import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useDebouncedValue } from './useDebouncedValue'
import { useSaveShortcut } from './useSaveShortcut'

afterEach(() => vi.useRealTimers())
it('debounces rapid changes and clears pending work on unmount', () => {
  vi.useFakeTimers()
  const { result, rerender, unmount } = renderHook(({ value }) => useDebouncedValue(value, 300), { initialProps: { value: 'a' } })
  rerender({ value: 'ab' }); act(() => vi.advanceTimersByTime(200)); rerender({ value: 'abc' })
  act(() => vi.advanceTimersByTime(299)); expect(result.current).toBe('a')
  act(() => vi.advanceTimersByTime(1)); expect(result.current).toBe('abc')
  rerender({ value: 'abcd' }); unmount(); expect(vi.getTimerCount()).toBe(0)
})
it('saves with Ctrl/Cmd+S only when enabled and removes listeners', () => {
  const save = vi.fn()
  const { rerender, unmount } = renderHook(({ enabled }) => useSaveShortcut(enabled, save), { initialProps: { enabled: false } })
  const press = (extra = {}) => { const event = new KeyboardEvent('keydown', { key: 's', ctrlKey: true, cancelable: true, ...extra }); window.dispatchEvent(event); return event.defaultPrevented }
  expect(press()).toBe(false)
  rerender({ enabled: true })
  expect(press()).toBe(true); expect(press({ ctrlKey: false, metaKey: true })).toBe(true)
  press({ repeat: true }); press({ ctrlKey: false })
  expect(save).toHaveBeenCalledTimes(2)
  unmount(); expect(press()).toBe(false)
})
