/**
 * WebSocket 客户端服务
 * 实现与后端 DraARLv1 协议的通信
 */

import { PacketType, DeviceModel } from '../../types/radio'
import type { DraARLPacket, WSConnectionState, WSConfig } from '../../types/radio'
import { defaultWSConfig } from '../../types/radio'

// 协议常量
const DRAARL_VERSION = 'DraA'
const HEADER_SIZE = 90

export class RadioWebSocket {
  private ws: WebSocket | null = null
  private config: WSConfig
  private state: WSConnectionState = 'disconnected'
  private reconnectAttempts = 0
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null

  // 智能重连相关
  private connectionStartTime = 0
  private pendingReconnect = false
  private lastReceiveTime = 0
  private lastSendTime = 0
  private isReceiving = false
  private isSending = false
  private voiceEndTimer: ReturnType<typeof setTimeout> | null = null

  // 回调函数
  private onStateChange: ((state: WSConnectionState) => void) | null = null
  private onPacket: ((packet: DraARLPacket, rawData: ArrayBuffer) => void) | null = null
  private onError: ((error: string) => void) | null = null

  // 用户信息
  private token: string = ''
  private ssid: number = 10
  private username: string = ''
  private callsign: string = ''

  constructor(config: Partial<WSConfig> = {}) {
    this.config = { ...defaultWSConfig, ...config }
  }

  // 设置回调
  setOnStateChange(callback: (state: WSConnectionState) => void) {
    this.onStateChange = callback
  }

  setOnPacket(callback: (packet: DraARLPacket, rawData: ArrayBuffer) => void) {
    this.onPacket = callback
  }

  setOnError(callback: (error: string) => void) {
    this.onError = callback
  }

  // 设置用户信息
  setUserInfo(token: string, ssid: number, username: string, callsign: string) {
    this.token = token
    this.ssid = ssid
    this.username = username
    this.callsign = callsign
  }

  // 获取当前状态
  getState(): WSConnectionState {
    return this.state
  }

  // 连接
  async connect(): Promise<void> {
    if (this.ws && (this.state === 'connecting' || this.state === 'online')) {
      return
    }

    this.setState('connecting')
    this.connectionStartTime = Date.now()
    this.pendingReconnect = false

    return new Promise((resolve, reject) => {
      try {
        // 构建 WebSocket URL
        let url = this.config.url
        if (this.token) {
          url += `?token=${encodeURIComponent(this.token)}&ssid=${this.ssid}`
        }

        this.ws = new WebSocket(url)
        this.ws.binaryType = 'arraybuffer'

        this.ws.onopen = () => {
          console.log('[WS] Connected')
          this.reconnectAttempts = 0
          this.setState('authenticating')

          // 如果有 token，等待服务器确认
          // 如果没有 token，等待心跳包进行设备认证
          if (this.token) {
            // JWT 认证，服务器会自动处理
            this.setState('online')
            this.startHeartbeat()
            resolve()
          }
          // 没有 token 的情况下，需要发送心跳包进行设备认证
          // 这种情况下由调用方处理
        }

        this.ws.onmessage = (event) => {
          if (event.data instanceof ArrayBuffer) {
            this.handleBinaryMessage(event.data)
          }
        }

        this.ws.onerror = (error) => {
          console.error('[WS] Error:', error)
          const errorMsg = 'WebSocket 连接错误'
          if (this.onError) this.onError(errorMsg)
          reject(new Error(errorMsg))
        }

        this.ws.onclose = (event) => {
          console.log('[WS] Closed:', event.code, event.reason)
          this.stopHeartbeat()
          this.setState('disconnected')

          // 如果不是正常关闭，尝试重连
          if (event.code !== 1000 && event.code !== 1001) {
            this.scheduleReconnect()
          }
        }

      } catch (error) {
        const errorMsg = error instanceof Error ? error.message : '连接失败'
        if (this.onError) this.onError(errorMsg)
        reject(new Error(errorMsg))
      }
    })
  }

  // 断开连接
  disconnect() {
    this.stopHeartbeat()
    this.clearReconnectTimer()
    this.clearVoiceEndTimer()

    if (this.ws) {
      this.ws.close(1000, 'User disconnect')
      this.ws = null
    }

    this.setState('disconnected')
  }

  // 发送数据包
  send(packet: ArrayBuffer): boolean {
    if (!this.ws || this.state !== 'online') {
      console.warn('[WS] Cannot send: not connected')
      return false
    }

    try {
      this.ws.send(packet)
      this.lastSendTime = Date.now()
      return true
    } catch (error) {
      console.error('[WS] Send error:', error)
      return false
    }
  }

  // 发送心跳包
  sendHeartbeat(gpsData?: { lat: number; lon: number; alt: number }): boolean {
    const packet = this.buildHeartbeatPacket(gpsData)
    return this.send(packet)
  }

  // 发送语音包
  sendVoice(opusData: Uint8Array): boolean {
    this.isSending = true
    this.lastSendTime = Date.now()
    const packet = this.buildVoicePacket(opusData)
    return this.send(packet)
  }

  // 发送文本消息
  sendTextMessage(message: string): boolean {
    const packet = this.buildTextPacket(message)
    return this.send(packet)
  }

  // 发送群组切换
  sendGroupChange(groupId: number): boolean {
    const packet = this.buildConfigPacket(groupId)
    return this.send(packet)
  }

  // 语音发送结束
  voiceSendEnd() {
    this.isSending = false
    this.checkPendingReconnect()
  }

  // 设置状态
  private setState(state: WSConnectionState) {
    if (this.state !== state) {
      this.state = state
      if (this.onStateChange) {
        this.onStateChange(state)
      }
    }
  }

  // 处理二进制消息
  private handleBinaryMessage(data: ArrayBuffer) {
    this.lastReceiveTime = Date.now()

    try {
      const packet = this.decodePacket(data)
      if (!packet) {
        console.warn('[WS] Failed to decode packet')
        return
      }

      // 处理心跳响应
      if (packet.type === PacketType.HEARTBEAT) {
        // 如果服务器填充了呼号，更新本地信息
        if (packet.callsign && !this.callsign) {
          this.callsign = packet.callsign
        }

        // 如果是在认证中状态，切换到在线
        if (this.state === 'authenticating') {
          this.setState('online')
          this.startHeartbeat()
        }
        return
      }

      // 处理语音包
      if (packet.type === PacketType.OPUS_16K || packet.type === PacketType.SERVER_VOICE) {
        this.isReceiving = true
        this.clearVoiceEndTimer()

        // 设置语音结束检测
        this.voiceEndTimer = setTimeout(() => {
          this.isReceiving = false
          this.checkPendingReconnect()
        }, this.config.voiceEndTimeout)
      }

      // 回调处理
      if (this.onPacket) {
        this.onPacket(packet, data)
      }

    } catch (error) {
      console.error('[WS] Handle message error:', error)
    }
  }

  // 解码数据包
  private decodePacket(data: ArrayBuffer): DraARLPacket | null {
    if (data.byteLength < HEADER_SIZE) {
      return null
    }

    const view = new DataView(data)
    const bytes = new Uint8Array(data)

    // 验证版本
    const version = String.fromCharCode(bytes[0], bytes[1], bytes[2], bytes[3])
    if (version !== DRAARL_VERSION) {
      return null
    }

    // 解析各字段
    const length = view.getUint16(4, false) // big-endian
    const username = this.decodeString(bytes, 6, 32)
    const devicePassword = this.decodeString(bytes, 38, 10)
    const type = bytes[48]
    const devModel = bytes[49]
    const ssid = bytes[50]
    const dmrid = (bytes[51] << 16) | (bytes[52] << 8) | bytes[53]
    const callsign = this.decodeString(bytes, 54, 32)
    const reserved = bytes.slice(86, 90)
    const packetData = bytes.slice(HEADER_SIZE)

    return {
      version,
      length,
      username,
      devicePassword,
      type,
      devModel,
      ssid,
      dmrid,
      callsign,
      reserved,
      data: packetData,
    }
  }

  // 编码字符串
  private encodeString(str: string, length: number): Uint8Array {
    const result = new Uint8Array(length)
    const encoder = new TextEncoder()
    const encoded = encoder.encode(str)
    result.set(encoded.slice(0, length))
    return result
  }

  // 解码字符串
  private decodeString(bytes: Uint8Array, start: number, length: number): string {
    const slice = bytes.slice(start, start + length)
    // 找到第一个 null 字节
    let end = length
    for (let i = 0; i < length; i++) {
      if (slice[i] === 0) {
        end = i
        break
      }
    }
    return new TextDecoder().decode(slice.slice(0, end))
  }

  // 构建心跳包
  private buildHeartbeatPacket(gpsData?: { lat: number; lon: number; alt: number }): ArrayBuffer {
    // 如果有 GPS 数据，添加到 DATA 区域
    let dataLength = 0
    if (gpsData) {
      dataLength = 24 // 3 * 8 bytes (double)
    }

    const buffer = new ArrayBuffer(HEADER_SIZE + dataLength)
    const view = new DataView(buffer)
    const bytes = new Uint8Array(buffer)

    // Version
    const versionBytes = new TextEncoder().encode(DRAARL_VERSION)
    bytes.set(versionBytes, 0)

    // Length
    view.setUint16(4, HEADER_SIZE + dataLength, false)

    // Username
    bytes.set(this.encodeString(this.username, 32), 6)

    // DevicePassword (空，因为使用 JWT 认证)
    // bytes.set(this.encodeString('', 10), 38)

    // Type
    bytes[48] = PacketType.HEARTBEAT

    // DevModel
    bytes[49] = DeviceModel.BROWSER

    // SSID
    bytes[50] = this.ssid

    // DMRID
    // bytes[51-53] = 0

    // CallSign
    bytes.set(this.encodeString(this.callsign, 32), 54)

    // Reserved
    // bytes[86-89] = 0

    // GPS Data
    if (gpsData) {
      view.setFloat64(90, gpsData.lat, false)
      view.setFloat64(98, gpsData.lon, false)
      view.setFloat64(106, gpsData.alt, false)
    }

    return buffer
  }

  // 构建语音包
  private buildVoicePacket(opusData: Uint8Array): ArrayBuffer {
    const buffer = new ArrayBuffer(HEADER_SIZE + opusData.length)
    const view = new DataView(buffer)
    const bytes = new Uint8Array(buffer)

    // Header
    const versionBytes = new TextEncoder().encode(DRAARL_VERSION)
    bytes.set(versionBytes, 0)
    view.setUint16(4, HEADER_SIZE + opusData.length, false)
    bytes.set(this.encodeString(this.username, 32), 6)
    bytes[48] = PacketType.OPUS_16K
    bytes[49] = DeviceModel.BROWSER
    bytes[50] = this.ssid
    bytes.set(this.encodeString(this.callsign, 32), 54)

    // Data
    bytes.set(opusData, HEADER_SIZE)

    return buffer
  }

  // 构建文本消息包
  private buildTextPacket(message: string): ArrayBuffer {
    const messageBytes = new TextEncoder().encode(message)
    const buffer = new ArrayBuffer(HEADER_SIZE + messageBytes.length)
    const view = new DataView(buffer)
    const bytes = new Uint8Array(buffer)

    // Header
    const versionBytes = new TextEncoder().encode(DRAARL_VERSION)
    bytes.set(versionBytes, 0)
    view.setUint16(4, HEADER_SIZE + messageBytes.length, false)
    bytes.set(this.encodeString(this.username, 32), 6)
    bytes[48] = PacketType.TEXT_MESSAGE
    bytes[49] = DeviceModel.BROWSER
    bytes[50] = this.ssid
    bytes.set(this.encodeString(this.callsign, 32), 54)

    // Data
    bytes.set(messageBytes, HEADER_SIZE)

    return buffer
  }

  // 构建配置包（群组切换）
  private buildConfigPacket(groupId: number): ArrayBuffer {
    const buffer = new ArrayBuffer(HEADER_SIZE + 4)
    const view = new DataView(buffer)
    const bytes = new Uint8Array(buffer)

    // Header
    const versionBytes = new TextEncoder().encode(DRAARL_VERSION)
    bytes.set(versionBytes, 0)
    view.setUint16(4, HEADER_SIZE + 4, false)
    bytes.set(this.encodeString(this.username, 32), 6)
    bytes[48] = PacketType.CONFIG
    bytes[49] = DeviceModel.BROWSER
    bytes[50] = this.ssid
    bytes.set(this.encodeString(this.callsign, 32), 54)

    // Data (Group ID - 4 bytes big-endian)
    view.setUint32(90, groupId, false)

    return buffer
  }

  // 启动心跳
  private startHeartbeat() {
    this.stopHeartbeat()
    this.heartbeatTimer = setInterval(() => {
      if (this.state === 'online') {
        this.sendHeartbeat()

        // 检查是否需要预重连
        this.checkPreReconnect()
      }
    }, this.config.heartbeatInterval)
  }

  // 停止心跳
  private stopHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
  }

  // 检查预重连
  private checkPreReconnect() {
    const elapsed = Date.now() - this.connectionStartTime
    if (elapsed >= this.config.preReconnectTime && !this.isReceiving && !this.isSending) {
      this.pendingReconnect = true
      console.log('[WS] Marked for reconnect')
    }
  }

  // 检查待重连
  private checkPendingReconnect() {
    if (this.pendingReconnect && !this.isReceiving && !this.isSending) {
      console.log('[WS] Executing pending reconnect')
      this.doReconnect()
    }
  }

  // 安排重连
  private scheduleReconnect() {
    if (this.reconnectAttempts >= this.config.maxReconnectAttempts) {
      console.log('[WS] Max reconnect attempts reached')
      if (this.onError) this.onError('连接失败，请刷新页面重试')
      return
    }

    this.clearReconnectTimer()

    const delay = this.config.reconnectInterval * Math.pow(1.5, this.reconnectAttempts)
    console.log(`[WS] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts + 1})`)

    this.setState('reconnecting')

    this.reconnectTimer = setTimeout(() => {
      this.reconnectAttempts++
      this.connect().catch(() => {
        // 错误已在 onerror 中处理
      })
    }, delay)
  }

  // 执行重连
  private doReconnect() {
    this.pendingReconnect = false
    this.disconnect()
    this.connect().catch(() => {
      // 错误已处理
    })
  }

  // 清除重连定时器
  private clearReconnectTimer() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
  }

  // 清除语音结束定时器
  private clearVoiceEndTimer() {
    if (this.voiceEndTimer) {
      clearTimeout(this.voiceEndTimer)
      this.voiceEndTimer = null
    }
  }
}

// 单例实例
let radioWSInstance: RadioWebSocket | null = null

export function getRadioWebSocket(): RadioWebSocket {
  if (!radioWSInstance) {
    radioWSInstance = new RadioWebSocket()
  }
  return radioWSInstance
}

export function closeRadioWebSocket() {
  if (radioWSInstance) {
    radioWSInstance.disconnect()
    radioWSInstance = null
  }
}
