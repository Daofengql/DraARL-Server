import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DynamicCodeBindDialog } from './DynamicCodeBindDialog'

const service = vi.hoisted(() => ({ bindDevice: vi.fn(), submitDeviceConfig: vi.fn() }))
vi.mock('../../services', () => ({ deviceBindService: service }))
afterEach(() => { vi.useRealTimers(); vi.resetAllMocks() })

it('accepts a pasted code and sends only one request while Enter is repeated', async () => {
  let complete: (value: unknown) => void = () => {}
  service.bindDevice.mockImplementation(() => new Promise(resolve => { complete = resolve }))
  render(<DynamicCodeBindDialog open onClose={() => {}} />)
  const input = screen.getByRole('textbox', { name: '设备绑定码' })
  fireEvent.change(input, { target: { value: '123456' } })
  fireEvent.keyDown(input, { key: 'Enter' })
  fireEvent.keyDown(input, { key: 'Enter' })
  expect(service.bindDevice).toHaveBeenCalledExactlyOnceWith('123456')
  await act(async () => complete({
    device_mac: 'AA:BB:CC:DD:EE:FF', call_sign: 'TEST', available_ssids: [1],
    recommended_ssid: 1, replaceable_devices: [],
  }))
  expect(screen.getByRole('button', { name: '提交配置' })).toBeEnabled()
})

it('preserves the code and prevents retry until the server cooldown ends', async () => {
  vi.useFakeTimers()
  service.bindDevice.mockRejectedValue({ response: { status: 429, headers: { 'retry-after': '2' } } })
  render(<DynamicCodeBindDialog open onClose={() => {}} />)
  const input = screen.getByRole('textbox', { name: '设备绑定码' })
  fireEvent.change(input, { target: { value: '123456' } })
  await act(async () => fireEvent.click(screen.getByRole('button', { name: '绑定设备' })))
  expect(input).toHaveValue('123456')
  expect(screen.getByRole('button', { name: '绑定设备' })).toBeDisabled()
  fireEvent.keyDown(input, { key: 'Enter' })
  expect(service.bindDevice).toHaveBeenCalledOnce()
  act(() => vi.advanceTimersByTime(1000))
  act(() => vi.advanceTimersByTime(1000))
  expect(screen.getByRole('button', { name: '绑定设备' })).toBeEnabled()
})
