import { expect, it, vi } from 'vitest'
import { loadRadioDevices, toOnlineDevice, type RadioDeviceResponse } from './deviceList'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../api', () => ({ apiClient: { get } }))

const device: RadioDeviceResponse = {
  key: 'udp-ghost-session-a', id: 0, username: 'tester', callsign: 'BG7TEST',
  ssid: 101, dev_model: 101, group_id: 1001, is_ghost: true,
  disable_send: true, disable_recv: false, session_id: 'session-a', last_activity: '2026-11-14T00:00:00Z',
}

it('maps live session fields and keeps a stable server key', () => {
  expect(toOnlineDevice(device)).toMatchObject({
    key: 'udp-ghost-session-a', id: 0, devModel: 101, groupId: 1001,
    isGhost: true, isOnline: true, disableSend: true, disableRecv: false,
    sessionId: 'session-a', lastPacketTime: '2026-11-14T00:00:00Z',
  })
  expect(toOnlineDevice({ ...device, key: 'ws-session-b', session_id: 'session-b' }).key)
    .not.toBe(toOnlineDevice(device).key)
})

it('loads the radio endpoint with cancellation instead of the management endpoint', async () => {
  const controller = new AbortController()
  get.mockResolvedValueOnce({ data: [device] })
  expect(await loadRadioDevices(1001, controller.signal)).toEqual([toOnlineDevice(device)])
  expect(get).toHaveBeenCalledWith('/api/radio/groups/1001/devices', { signal: controller.signal })
})
