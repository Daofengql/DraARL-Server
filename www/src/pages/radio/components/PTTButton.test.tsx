import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { PTTButton } from './PTTButton'

it('supports hold/release by keyboard, blur and unmount without repeated transmission', () => {
  const down = vi.fn(); const up = vi.fn()
  const props = { isPressed: false, onMouseDown: down, onMouseUp: up, onMouseLeave: vi.fn(), onTouchStart: vi.fn(), onTouchEnd: vi.fn() }
  const { unmount } = render(<PTTButton {...props} />)
  const button = screen.getByRole('button', { name: '按住说话' })
  expect(button).toHaveAttribute('aria-pressed', 'false')
  fireEvent.keyDown(button, { key: ' ' }); fireEvent.keyDown(button, { key: ' ', repeat: true })
  expect(down).toHaveBeenCalledTimes(1)
  fireEvent.keyUp(button, { key: ' ' }); expect(up).toHaveBeenCalledTimes(1)
  fireEvent.keyDown(button, { key: 'Enter' }); fireEvent.blur(button)
  expect(up).toHaveBeenCalledTimes(2)
  fireEvent.keyDown(button, { key: 'Enter' }); unmount()
  expect(up).toHaveBeenCalledTimes(3)
})
it('disables transmission and releases an active keyboard press when disabled', () => {
  const down = vi.fn(); const up = vi.fn()
  const props = { isPressed: false, onMouseDown: down, onMouseUp: up, onMouseLeave: vi.fn(), onTouchStart: vi.fn(), onTouchEnd: vi.fn() }
  const { rerender } = render(<PTTButton {...props} />)
  const button = screen.getByRole('button', { name: '按住说话' })
  fireEvent.keyDown(button, { key: 'Enter' })
  rerender(<PTTButton {...props} disabled />)
  expect(up).toHaveBeenCalledTimes(1)
  expect(button).toBeDisabled()
  fireEvent.keyDown(button, { key: 'Enter' })
  expect(down).toHaveBeenCalledTimes(1)
})
