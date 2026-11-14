import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DeviceList } from './DeviceList'
import type { OnlineDevice } from '../../../types/radio'

const { load } = vi.hoisted(() => ({ load: vi.fn() }))
vi.mock('../../../services/radio/deviceList', () => ({ loadRadioDevices: load }))
vi.mock('../../../utils/deviceModel', () => ({ getDevModelIcon: () => null, getDevModelName: () => 'Test' }))
const device = (callsign: string): OnlineDevice => ({
  key: callsign, id: 0, username: 'test', callsign, ssid: 101, devModel: 101,
  groupId: 1001, isOnline: true, isGhost: true, disableSend: false, disableRecv: false,
})
beforeEach(() => { vi.useFakeTimers(); load.mockReset() })
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks() })

it('discards an old group response even when cancellation is ignored by the transport', async () => {
  let finishOld!: (devices: OnlineDevice[]) => void
  load.mockImplementationOnce(() => new Promise<OnlineDevice[]>(resolve => { finishOld = resolve }))
  const view = render(<DeviceList groupId={1001} onClose={() => {}} />)
  const oldSignal = load.mock.calls[0][1] as AbortSignal
  load.mockResolvedValueOnce([device('NEW')])
  await act(async () => { view.rerender(<DeviceList groupId={1002} onClose={() => {}} />) })
  expect(oldSignal.aborted).toBe(true)
  await act(async () => { finishOld([device('OLD')]) })
  expect(screen.getByText('NEW-101')).toBeInTheDocument()
  expect(screen.queryByText('OLD-101')).not.toBeInTheDocument()
})

it('keeps the current list on refresh failure and pauses polling while hidden', async () => {
  load.mockResolvedValueOnce([device('CURRENT')]).mockRejectedValue(new Error('temporary failure'))
  await act(async () => { render(<DeviceList groupId={1001} onClose={() => {}} />) })
  await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
  expect(screen.getByText('CURRENT-101')).toBeInTheDocument()
  expect(screen.getByText('temporary failure')).toBeInTheDocument()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
  await act(async () => { await vi.advanceTimersByTimeAsync(30_000) })
  expect(load).toHaveBeenCalledTimes(2)
})
