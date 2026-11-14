import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MultiChannelAudioMixer } from './opus'

const { decoded, readiness } = vi.hoisted(() => ({
  decoded: vi.fn(), readiness: { current: Promise.resolve() },
}))
vi.mock('opus-decoder', () => ({ OpusDecoder: class {
  ready = readiness.current
  decodeFrame(data: Uint8Array) { decoded(data); return { channelData: [new Float32Array(1920)] } }
  free() {}
} }))

const sources: { start: ReturnType<typeof vi.fn>; stop: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }[] = []
class AudioContextMock {
  currentTime = 1
  state = 'running'
  destination = {}
  createGain = () => ({ gain: { value: 1 }, connect() {}, disconnect() {} })
  createDynamicsCompressor = () => ({ connect() {}, disconnect() {} })
  createBuffer = (_channels: number, samples: number, sampleRate: number) => ({
    duration: samples / sampleRate, copyToChannel() {},
  })
  createBufferSource = () => {
    const source = { buffer: null, onended: null, connect() {}, start: vi.fn(), stop: vi.fn(), disconnect: vi.fn() }
    sources.push(source)
    return source
  }
  close = vi.fn()
}
let mixer: MultiChannelAudioMixer
beforeEach(() => {
  vi.stubGlobal('AudioContext', AudioContextMock)
  decoded.mockClear()
  readiness.current = Promise.resolve()
  sources.length = 0
  mixer = new MultiChannelAudioMixer()
})
afterEach(() => { mixer.destroy(); vi.unstubAllGlobals() })
const play = (value = 1) => mixer.play('source', 1001, new Uint8Array([0x80, value]))
const context = () => (mixer as unknown as { audioContext: AudioContextMock }).audioContext

it('absorbs a 140ms delivery pause and counts/rebuffers an actual underrun', async () => {
  await play()
  expect(sources[0].start).toHaveBeenCalledWith(1.16)
  context().currentTime = 1.26
  await play()
  expect(sources[1].start.mock.calls[0][0]).toBeCloseTo(1.28)
  expect(mixer.getPlaybackStats().late).toBe(0)
  context().currentTime = 1.41
  await play()
  expect(sources[2].start.mock.calls[0][0]).toBeCloseTo(1.57)
  expect(mixer.getPlaybackStats().late).toBe(1)
})

it('replaces stale scheduled playback with the newest packet after a burst', async () => {
  for (let value = 0; value < 7; value++) await play(value)
  expect(sources[0].stop).toHaveBeenCalledTimes(1)
  expect(sources.at(-1)!.start.mock.calls[0][0]).toBeLessThan(1.6)
  expect(decoded.mock.calls.at(-1)![0][1]).toBe(6)
  expect(mixer.getPlaybackStats().droppedBacklog).toBeGreaterThan(0)
})

it('bounds packets waiting for decoder initialization and keeps the newest five', async () => {
  let ready!: () => void
  readiness.current = new Promise<void>(resolve => { ready = resolve })
  await mixer.init()
  const pending = Array.from({ length: 12 }, (_, value) => play(value))
  await Promise.resolve()
  ready()
  await Promise.all(pending)
  expect(decoded.mock.calls.map(call => call[0][1])).toEqual([0, 7, 8, 9, 10, 11])
  expect(mixer.getPlaybackStats().droppedBacklog).toBeGreaterThanOrEqual(6)
})

it('does not play buffered packets after the stream is removed', async () => {
  let ready!: () => void
  readiness.current = new Promise<void>(resolve => { ready = resolve })
  const pending = play()
  await Promise.resolve()
  mixer.resetStream('source')
  ready()
  await pending
  expect(sources).toHaveLength(0)
})
