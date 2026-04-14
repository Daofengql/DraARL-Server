const SERVICE_UUID = '6d22f67d-7287-4f4e-8548-b362f9b1f001'
const STATUS_UUID = '6d22f67d-7287-4f4e-8548-b362f9b1f002'
const AUTH_UUID = '6d22f67d-7287-4f4e-8548-b362f9b1f003'
const RPC_TX_UUID = '6d22f67d-7287-4f4e-8548-b362f9b1f004'
const RPC_RX_UUID = '6d22f67d-7287-4f4e-8548-b362f9b1f005'

const CHUNK_PAYLOAD = 19
const CHUNK_START = 0x01
const CHUNK_END = 0x02
const RPC_TIMEOUT_MS = 12000
const STATUS_POLL_INTERVAL_MS = 2000

const encoder = new TextEncoder()
const decoder = new TextDecoder()

export interface BleProvisionStatus {
  connected: boolean
  deviceName: string
  wifiState: string
  bleState: string
  authenticated: boolean
  rssi: number | null
}

export interface BleProvisionWifiNetwork {
  ssid: string
  rssi: number
  auth: number
}

export interface BleProvisionWifiConfig {
  ssid: string
  password: string
  dhcp: boolean
  ip: string
  gateway: string
  subnet: string
  dns1: string
  dns2: string
}

export interface BleProvisionServerConfig {
  callsign: string
  nodeSsid: number
  udpHost: string
  udpPort: number
  httpApiBaseUrl: string
  account: string
  deviceAuthPassword: string
}

export interface BleProvisionConfig {
  wifi: BleProvisionWifiConfig
  server: BleProvisionServerConfig
}

interface RpcPending {
  resolve: (value: any) => void
  reject: (reason?: unknown) => void
  timeoutHandle: number
}

interface BleProvisionCallbacks {
  onStatusChange?: (status: BleProvisionStatus) => void
  onDisconnect?: () => void
}

type CharacteristicWriter = {
  writeValueWithResponse?: (value: BufferSource) => Promise<void>
  writeValueWithoutResponse?: (value: BufferSource) => Promise<void>
}

const WIFI_STATE_MAP: Record<string, string> = {
  '0': 'Idle',
  '1': '未配置',
  '2': '连接中',
  '3': '已连接',
  '4': '连接失败',
}

const WIFI_STATE_RPC_MAP: Record<string, string> = {
  idle: 'Idle',
  'no-config': '未配置',
  connecting: '连接中',
  connected: '已连接',
  failed: '连接失败',
}

const BLE_STATE_MAP: Record<string, string> = {
  '0': '已禁用',
  '1': '广播中',
  '2': '已连接',
}

const EMPTY_STATUS: BleProvisionStatus = {
  connected: false,
  deviceName: '',
  wifiState: '未知',
  bleState: '未知',
  authenticated: false,
  rssi: null,
}

function isBluetoothSupported() {
  return typeof navigator !== 'undefined' && 'bluetooth' in navigator
}

function normalizeWifiConfig(data: Record<string, any> | undefined): BleProvisionWifiConfig {
  return {
    ssid: String(data?.ssid || ''),
    password: String(data?.password || ''),
    dhcp: data?.dhcp !== false,
    ip: String(data?.ip || ''),
    gateway: String(data?.gateway || ''),
    subnet: String(data?.subnet || ''),
    dns1: String(data?.dns1 || ''),
    dns2: String(data?.dns2 || ''),
  }
}

function normalizeServerConfig(data: Record<string, any> | undefined): BleProvisionServerConfig {
  return {
    callsign: String(data?.callsign || ''),
    nodeSsid: Number(data?.node_ssid ?? 0),
    udpHost: String(data?.udp_host || ''),
    udpPort: Number(data?.udp_port ?? 0),
    httpApiBaseUrl: String(data?.http_api_base_url || ''),
    account: String(data?.account || ''),
    deviceAuthPassword: String(data?.device_auth_password || ''),
  }
}

function decodeDataView(view: DataView) {
  return decoder.decode(new Uint8Array(view.buffer, view.byteOffset, view.byteLength))
}

async function writeCharacteristic(
  characteristic: BluetoothRemoteGATTCharacteristic & CharacteristicWriter,
  value: BufferSource
) {
  if (typeof characteristic.writeValueWithResponse === 'function') {
    await characteristic.writeValueWithResponse(value)
    return
  }
  if (typeof characteristic.writeValueWithoutResponse === 'function') {
    await characteristic.writeValueWithoutResponse(value)
    return
  }
  throw new Error('当前设备不支持写入 BLE 特征值')
}

export class BleProvisioningClient {
  private device: BluetoothDevice | null = null
  private server: BluetoothRemoteGATTServer | null = null
  private statusChar: BluetoothRemoteGATTCharacteristic | null = null
  private authChar: BluetoothRemoteGATTCharacteristic | null = null
  private rpcTxChar: BluetoothRemoteGATTCharacteristic | null = null
  private rpcRxChar: BluetoothRemoteGATTCharacteristic | null = null
  private nextId = 1
  private pending = new Map<number, RpcPending>()
  private rpcBuffer = ''
  private statusPollTimer: number | null = null
  private callbacks: BleProvisionCallbacks
  private status: BleProvisionStatus = { ...EMPTY_STATUS }

  constructor(callbacks: BleProvisionCallbacks = {}) {
    this.callbacks = callbacks
  }

  getStatus() {
    return { ...this.status }
  }

  get supported() {
    return isBluetoothSupported()
  }

  async connect() {
    if (!isBluetoothSupported()) {
      throw new Error('当前浏览器不支持 Web Bluetooth，请使用 Chromium 内核浏览器并通过 HTTPS 或 localhost 访问')
    }

    await this.disconnect(false)

    this.device = await navigator.bluetooth.requestDevice({
      filters: [{ services: [SERVICE_UUID] }],
      optionalServices: [SERVICE_UUID],
    })
    this.device.addEventListener('gattserverdisconnected', this.handleDisconnect)

    this.server = await this.device.gatt?.connect() ?? null
    if (!this.server) {
      throw new Error('BLE GATT 连接失败')
    }

    const service = await this.server.getPrimaryService(SERVICE_UUID)
    this.statusChar = await service.getCharacteristic(STATUS_UUID)
    this.authChar = await service.getCharacteristic(AUTH_UUID)
    this.rpcTxChar = await service.getCharacteristic(RPC_TX_UUID)
    this.rpcRxChar = await service.getCharacteristic(RPC_RX_UUID)

    await this.statusChar.startNotifications()
    await this.rpcRxChar.startNotifications()
    this.statusChar.addEventListener('characteristicvaluechanged', this.handleStatusNotify)
    this.rpcRxChar.addEventListener('characteristicvaluechanged', this.handleRpcNotify)

    this.status = {
      ...this.status,
      connected: true,
      deviceName: this.device.name || 'Unknown',
      bleState: '已连接',
    }
    this.emitStatus()

    await this.refreshStatus()

    this.statusPollTimer = window.setInterval(() => {
      void this.refreshStatus().catch(() => undefined)
    }, STATUS_POLL_INTERVAL_MS)
  }

  async disconnect(emitCallback = true) {
    if (this.statusPollTimer !== null) {
      window.clearInterval(this.statusPollTimer)
      this.statusPollTimer = null
    }

    if (this.statusChar) {
      this.statusChar.removeEventListener('characteristicvaluechanged', this.handleStatusNotify)
    }
    if (this.rpcRxChar) {
      this.rpcRxChar.removeEventListener('characteristicvaluechanged', this.handleRpcNotify)
    }

    if (this.device) {
      this.device.removeEventListener('gattserverdisconnected', this.handleDisconnect)
    }

    this.rejectAllPending(new Error('设备已断开连接'))

    if (this.device?.gatt?.connected) {
      this.device.gatt.disconnect()
    }

    this.server = null
    this.statusChar = null
    this.authChar = null
    this.rpcTxChar = null
    this.rpcRxChar = null
    this.rpcBuffer = ''
    this.device = null
    this.status = { ...EMPTY_STATUS }
    this.emitStatus()

    if (emitCallback) {
      this.callbacks.onDisconnect?.()
    }
  }

  async refreshStatus(useRpcFallback = true) {
    let updated = false

    if (this.statusChar) {
      try {
        const value = await this.statusChar.readValue()
        updated = this.parseStatusText(decodeDataView(value))
      } catch {
        updated = false
      }
    }

    if (!useRpcFallback || !this.rpcTxChar || !this.rpcRxChar) {
      return this.getStatus()
    }

    if (!updated || this.device?.gatt?.connected) {
      try {
        const payload = await this.sendRpc('get_status', {}, 3000)
        this.applyStatusFromRpc(payload)
      } catch {
        // Ignore status fallback errors to avoid noisy polling failures.
      }
    }

    return this.getStatus()
  }

  async authenticate(dynamicCode: string) {
    if (!this.authChar) {
      throw new Error('设备尚未连接')
    }
    await writeCharacteristic(this.authChar as BluetoothRemoteGATTCharacteristic & CharacteristicWriter, encoder.encode(dynamicCode))
    const success = await this.waitForAuthentication()
    if (!success) {
      throw new Error('动态码认证失败')
    }
  }

  async loadConfig(): Promise<BleProvisionConfig> {
    const data = await this.sendRpc('get_config')
    return {
      wifi: normalizeWifiConfig(data?.wifi),
      server: normalizeServerConfig(data?.server),
    }
  }

  async scanWifi(): Promise<{ networks: BleProvisionWifiNetwork[]; partial: boolean; scanInProgress: boolean }> {
    const result = await this.sendRpc('scan_wifi', {}, 6000)
    const networks = Array.isArray(result?.networks)
      ? result.networks
        .filter((item: any) => item && item.ssid)
        .map((item: any) => ({
          ssid: String(item.ssid),
          rssi: Number(item.rssi ?? 0),
          auth: Number(item.auth ?? 0),
        }))
      : []

    return {
      networks,
      partial: Boolean(result?.partial),
      scanInProgress: Boolean(result?.scan_in_progress),
    }
  }

  async saveWifi(config: BleProvisionWifiConfig) {
    await this.sendRpc('set_wifi', {
      ssid: config.ssid,
      password: config.password,
      dhcp: config.dhcp,
      ip: config.ip,
      gateway: config.gateway,
      subnet: config.subnet,
      dns1: config.dns1,
      dns2: config.dns2,
    })
  }

  async saveServer(config: BleProvisionServerConfig) {
    await this.sendRpc('set_server', {
      callsign: config.callsign,
      node_ssid: config.nodeSsid,
      udp_host: config.udpHost,
      udp_port: config.udpPort,
      http_api_base_url: config.httpApiBaseUrl,
      account: config.account,
      device_auth_password: config.deviceAuthPassword,
    })
  }

  private emitStatus() {
    this.callbacks.onStatusChange?.(this.getStatus())
  }

  private rejectAllPending(error: Error) {
    for (const [id, pending] of this.pending.entries()) {
      window.clearTimeout(pending.timeoutHandle)
      this.pending.delete(id)
      pending.reject(error)
    }
  }

  private parseStatusText(text: string) {
    const normalized = text.replace(/\0/g, '').trim()
    if (!normalized) {
      return false
    }

    const parts: Record<string, string> = {}
    normalized.split(';').forEach((entry) => {
      if (!entry || entry.length < 2) return
      parts[entry[0]] = entry.slice(1)
    })

    if (parts.w === undefined && parts.b === undefined && parts.a === undefined) {
      return false
    }

    this.status = {
      ...this.status,
      connected: Boolean(this.device?.gatt?.connected),
      deviceName: this.device?.name || this.status.deviceName,
      wifiState: parts.w !== undefined ? (WIFI_STATE_MAP[parts.w] || '未知') : this.status.wifiState,
      bleState: parts.b !== undefined ? (BLE_STATE_MAP[parts.b] || '未知') : this.status.bleState,
      authenticated: parts.a === '1',
      rssi: parts.r !== undefined ? Number(parts.r) : this.status.rssi,
    }
    this.emitStatus()
    return true
  }

  private applyStatusFromRpc(payload: Record<string, any> | undefined) {
    if (!payload || typeof payload !== 'object') {
      return
    }

    this.status = {
      ...this.status,
      connected: Boolean(this.device?.gatt?.connected),
      wifiState: WIFI_STATE_RPC_MAP[String(payload.wifi_state || 'idle')] || this.status.wifiState,
      bleState: BLE_STATE_MAP[String(payload.ble_state ?? 0)] || this.status.bleState,
      authenticated: typeof payload.authenticated === 'boolean' ? payload.authenticated : this.status.authenticated,
      rssi: Number.isFinite(payload.rssi) ? Number(payload.rssi) : this.status.rssi,
    }
    this.emitStatus()
  }

  private async readAuthState() {
    if (!this.authChar) {
      return ''
    }
    const value = await this.authChar.readValue()
    return decodeDataView(value).replace(/\0/g, '').trim()
  }

  private async waitForAuthentication(timeoutMs = 3000, intervalMs = 150) {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
      await this.refreshStatus(false)
      if (this.status.authenticated) {
        return true
      }

      const authState = await this.readAuthState()
      if (authState === 'ERR') {
        return false
      }

      if (authState === 'OK') {
        try {
          const status = await this.sendRpc('get_status')
          this.applyStatusFromRpc(status)
          if (status?.authenticated) {
            return true
          }
        } catch {
          // Ignore transient RPC errors while waiting for auth state.
        }
      }

      await new Promise((resolve) => window.setTimeout(resolve, intervalMs))
    }

    return this.status.authenticated
  }

  private async sendRpc(cmd: string, data: Record<string, unknown> = {}, timeoutMs = RPC_TIMEOUT_MS) {
    if (!this.rpcTxChar || !this.rpcRxChar) {
      throw new Error('RPC 通道未就绪')
    }
    if (!this.device?.gatt?.connected) {
      throw new Error('设备未连接')
    }

    const id = this.nextId++
    const text = JSON.stringify({ id, cmd, data })
    const bytes = encoder.encode(text)

    const promise = new Promise<any>((resolve, reject) => {
      const timeoutHandle = window.setTimeout(() => {
        if (!this.pending.has(id)) {
          return
        }
        this.pending.delete(id)
        reject(new Error(`RPC 超时: ${cmd}`))
      }, timeoutMs)

      this.pending.set(id, {
        resolve,
        reject,
        timeoutHandle,
      })
    })

    const writer = this.rpcTxChar as BluetoothRemoteGATTCharacteristic & CharacteristicWriter
    for (let offset = 0; offset < bytes.length; offset += CHUNK_PAYLOAD) {
      const chunk = bytes.slice(offset, offset + CHUNK_PAYLOAD)
      const frame = new Uint8Array(chunk.length + 1)
      frame[0] = (offset === 0 ? CHUNK_START : 0) | (offset + CHUNK_PAYLOAD >= bytes.length ? CHUNK_END : 0)
      frame.set(chunk, 1)

      try {
        await writeCharacteristic(writer, frame)
      } catch (error) {
        const pending = this.pending.get(id)
        if (pending) {
          window.clearTimeout(pending.timeoutHandle)
          this.pending.delete(id)
        }
        throw error
      }
    }

    return promise
  }

  private handleStatusNotify = (event: Event) => {
    const target = event.target as BluetoothRemoteGATTCharacteristic | null
    if (!target?.value) {
      return
    }
    this.parseStatusText(decodeDataView(target.value))
  }

  private handleRpcNotify = (event: Event) => {
    const target = event.target as BluetoothRemoteGATTCharacteristic | null
    if (!target?.value) {
      return
    }

    const bytes = new Uint8Array(target.value.buffer, target.value.byteOffset, target.value.byteLength)
    const flags = bytes[0]
    if (flags & CHUNK_START) {
      this.rpcBuffer = ''
    }

    this.rpcBuffer += decoder.decode(bytes.slice(1), { stream: !(flags & CHUNK_END) })

    if (!(flags & CHUNK_END)) {
      return
    }

    const payloadText = this.rpcBuffer
    this.rpcBuffer = ''

    let payload: any
    try {
      payload = JSON.parse(payloadText)
    } catch {
      this.rejectAllPending(new Error('设备返回了非法 JSON，请检查设备端配置内容'))
      return
    }

    const pending = this.pending.get(payload.id)
    if (!pending) {
      return
    }

    window.clearTimeout(pending.timeoutHandle)
    this.pending.delete(payload.id)
    if (payload.ok) {
      pending.resolve(payload.data || {})
      return
    }
    pending.reject(new Error(payload.error || '未知错误'))
  }

  private handleDisconnect = () => {
    void this.disconnect()
  }
}

export function createEmptyProvisionConfig(): BleProvisionConfig {
  return {
    wifi: normalizeWifiConfig(undefined),
    server: normalizeServerConfig(undefined),
  }
}
