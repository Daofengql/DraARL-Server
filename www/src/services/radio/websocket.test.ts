import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { RadioWebSocket } from './websocket'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../api', () => ({ apiClient: { post } }))

class MockWebSocket {
  static sockets: MockWebSocket[] = []
  binaryType = ''
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onerror: ((event: unknown) => void) | null = null
  onclose: ((event: { code: number; reason: string }) => void) | null = null
  send = vi.fn()
  close = vi.fn((code = 1000, reason = '') => this.onclose?.({ code, reason }))
  constructor(_url: string) { MockWebSocket.sockets.push(this) }
  authenticate() {
    this.onopen?.()
    this.onmessage?.({ data: JSON.stringify({ type: 'auth_success', data: {
      session_id: 'session-a', tx_group_id: 1001, rx_group_ids: [1001],
    } }) })
  }
}

let client: RadioWebSocket
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('WebSocket', MockWebSocket)
  MockWebSocket.sockets = []
  post.mockReset().mockResolvedValue({})
  client = new RadioWebSocket({ url: 'ws://localhost/ws', reconnectInterval: 100 })
  client.setUserInfo('token', 105, 'tester', 'BG7TEST')
})
afterEach(() => {
  client.disconnect()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  localStorage.clear()
})
const latest = () => MockWebSocket.sockets.at(-1)!

it('retries a failed cookie sync and authenticates on the next connection', async () => {
  post.mockRejectedValueOnce(new Error('network unavailable'))
  await expect(client.connect()).rejects.toThrow('network unavailable')
  expect(client.getState()).toBe('reconnecting')
  await vi.advanceTimersByTimeAsync(100)
  latest().authenticate()
  expect(post).toHaveBeenCalledTimes(2)
  expect(client.getState()).toBe('online')
})

it('does not retry a 401 cookie sync, including wake events', async () => {
  post.mockRejectedValueOnce({ response: { status: 401 } })
  await expect(client.connect()).rejects.toThrow('WS 认证信息同步失败')
  window.dispatchEvent(new Event('online'))
  await vi.advanceTimersByTimeAsync(60_000)
  expect(post).toHaveBeenCalledTimes(1)
})

it('retries an authentication timeout even though it closes with code 1000', async () => {
  const connecting = client.connect().catch(error => error)
  await vi.advanceTimersByTimeAsync(10_000)
  expect(await connecting).toBeInstanceOf(Error)
  expect(MockWebSocket.sockets[0].close).toHaveBeenCalledWith(1000, 'authentication_timeout')
  await vi.advanceTimersByTimeAsync(100)
  latest().authenticate()
  expect(client.getState()).toBe('online')
})

it('keeps slow retries after six consecutive failures and wakes immediately', async () => {
  const connecting = client.connect().catch(() => {})
  await vi.advanceTimersByTimeAsync(0)
  for (let attempt = 0; attempt < 6; attempt++) {
    latest().onclose?.({ code: 1006, reason: 'network unavailable' })
    await vi.advanceTimersByTimeAsync(attempt < 5 ? 100 * 1.5 ** attempt : 30_000)
  }
  await connecting
  expect(MockWebSocket.sockets).toHaveLength(7)
  latest().onclose?.({ code: 1006, reason: '' })
  window.dispatchEvent(new Event('online'))
  await vi.advanceTimersByTimeAsync(0)
  expect(MockWebSocket.sockets).toHaveLength(8)
  latest().authenticate()
  expect(client.getState()).toBe('online')
})

it.each([1008, 1013])('does not retry server policy close %s', async code => {
  const connecting = client.connect().catch(() => {})
  await vi.advanceTimersByTimeAsync(0)
  latest().onclose?.({ code, reason: 'policy rejected' })
  await connecting
  window.dispatchEvent(new Event('online'))
  document.dispatchEvent(new Event('visibilitychange'))
  await vi.advanceTimersByTimeAsync(60_000)
  expect(MockWebSocket.sockets).toHaveLength(1)
})

it('ignores all events from an old connection after a planned reconnect', async () => {
  const connecting = client.connect()
  await vi.advanceTimersByTimeAsync(0)
  const old = latest()
  old.authenticate()
  await connecting
  old.close.mockImplementation(() => {})
  const internal = client as unknown as { doReconnect: () => void }
  internal.doReconnect()
  await vi.advanceTimersByTimeAsync(0)
  const current = latest()
  current.authenticate()
  old.onopen?.()
  old.onmessage?.({ data: JSON.stringify({ type: 'auth_success', data: {
    session_id: 'stale', tx_group_id: 9999, rx_group_ids: [9999],
  } }) })
  old.onerror?.('stale error')
  old.onclose?.({ code: 1006, reason: 'stale close' })
  expect(client.getState()).toBe('online')
  expect(client.getRouting()?.sessionId).toBe('session-a')
  await vi.advanceTimersByTimeAsync(10_000)
  expect(current.send).toHaveBeenCalledTimes(1)
})

it('does not open a socket when disconnected during cookie sync', async () => {
  let finish!: () => void
  post.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
  const connecting = client.connect()
  client.disconnect()
  finish()
  await connecting
  expect(MockWebSocket.sockets).toHaveLength(0)
  expect(client.getState()).toBe('disconnected')
})
