import { afterEach, expect, it, vi } from 'vitest'
import { RadioService } from './radioService'
import { PacketType, type DraARLPacket, type RadioSessionRouting } from '../types/radio'

vi.mock('./radio/websocket', () => ({ closeRadioWebSocket: vi.fn() }))
vi.mock('./radio/opus', () => ({ destroyAudioInstances: vi.fn() }))
vi.mock('./radio/groupManager', () => ({
  groupManagerService: { getCachedGroup: () => undefined },
}))
vi.mock('./api', () => ({ apiClient: {} }))
vi.mock('opus-decoder', () => ({
  OpusDecoder: class {
    ready = Promise.resolve()
    decodeFrame() { return { channelData: [new Float32Array(960)] } }
    free() {}
  },
}))

const services: RadioService[] = []
afterEach(() => {
  services.forEach(service => service.destroy())
  services.length = 0
  vi.useRealTimers()
  localStorage.clear()
})

function linkedSource() {
  vi.useFakeTimers()
  const service = new RadioService()
  services.push(service)
  const input = service as unknown as {
    applyRouting: (routing: RadioSessionRouting) => void
    handlePacket: (packet: DraARLPacket, data: ArrayBuffer) => void
  }
  input.applyRouting({ sessionId: 'listener-a', clientInstanceId: 'web-a', txGroupId: 1001, rxGroupIds: [1001] })
  const packet: DraARLPacket = {
    version: 'DRA1', length: 93, username: 'source-b', devicePassword: '',
    type: PacketType.OPUS_16K, devModel: 105, ssid: 105, dmrid: 0,
    callsign: 'BG7ABC', reserved: new Uint8Array(4), sourceGroupId: 1002,
    data: new Uint8Array([0x80, 1, 2]),
  }
  return { service, input, packet }
}

it('plays a linked group source and preserves its username in live and completed voice messages', async () => {
  const { service, input, packet } = linkedSource()
  const onMessage = vi.fn()
  service.on('message', onMessage)
  input.handlePacket(packet, new ArrayBuffer(0))
  expect(service.getActiveSpeakers()).toEqual([
    expect.objectContaining({ groupId: 1002, username: 'source-b', callsign: 'BG7ABC' }),
  ])
  await vi.advanceTimersByTimeAsync(1500)
  expect(onMessage).toHaveBeenCalledWith(expect.objectContaining({
    type: 'voice', groupId: 1002, senderUsername: 'source-b', content: expect.any(Blob),
  }))
})

it('retains the source username for linked text and drops a packet without a source group', () => {
  const { service, input, packet } = linkedSource()
  const onMessage = vi.fn()
  service.on('message', onMessage)
  input.handlePacket({ ...packet, type: PacketType.TEXT_MESSAGE, data: new TextEncoder().encode('hello') }, new ArrayBuffer(0))
  expect(onMessage).toHaveBeenCalledWith(expect.objectContaining({
    type: 'text', groupId: 1002, senderUsername: 'source-b', content: 'hello',
  }))
  input.handlePacket({ ...packet, sourceGroupId: 0 }, new ArrayBuffer(0))
  expect(service.getActiveSpeakers()).toHaveLength(0)
})

it('restores the speaker when audio resumes before the stream is committed', async () => {
  const { service, input, packet } = linkedSource()
  const onSpeakers = vi.fn()
  const onState = vi.fn()
  const onMessage = vi.fn()
  service.on('speakersChange', onSpeakers)
  service.on('voiceStateChange', onState)
  service.on('message', onMessage)
  input.handlePacket(packet, new ArrayBuffer(0))
  await vi.advanceTimersByTimeAsync(800)
  expect(service.getActiveSpeakers()).toHaveLength(0)
  input.handlePacket(packet, new ArrayBuffer(0))
  expect(service.getActiveSpeakers()).toEqual([
    expect.objectContaining({ callsign: 'BG7ABC', ssid: 105, groupId: 1002 }),
  ])
  expect(onSpeakers).toHaveBeenCalledTimes(3)
  expect(onState).toHaveBeenLastCalledWith('receiving', 'BG7ABC')
  expect(onMessage).not.toHaveBeenCalled()
  await vi.advanceTimersByTimeAsync(1500)
  expect(onMessage).toHaveBeenCalledTimes(1)
})

it('clears buffered linked audio when unsubscribing from the delivery channel', async () => {
  const { service, input, packet } = linkedSource()
  const onMessage = vi.fn()
  service.on('message', onMessage)
  input.handlePacket(packet, new ArrayBuffer(0))
  input.applyRouting({ sessionId: 'listener-a', clientInstanceId: 'web-a', txGroupId: 1003, rxGroupIds: [1003] })
  expect(service.getActiveSpeakers()).toHaveLength(0)
  await vi.advanceTimersByTimeAsync(1500)
  expect(onMessage).not.toHaveBeenCalled()
})
