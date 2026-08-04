/**
 * Radio 服务
 * 整合 WebSocket、音频、消息缓存和群组管理
 */

import { RadioWebSocket, getRadioWebSocket, closeRadioWebSocket } from './radio/websocket'
import { AudioCapture, MultiChannelAudioMixer, getAudioCapture, getAudioMixer, destroyAudioInstances } from './radio/opus'
import { groupManagerService, toRadioGroup } from './radio/groupManager'
import { apiClient } from './api'
import { PacketType, defaultRadioUserConfig } from '../types/radio'
import type {
  WSConnectionState,
  VoiceState,
  RadioMessage,
  RadioGroup,
  RadioUserConfig,
  DraARLPacket,
  OnlineDevice,
  RadioSessionRouting,
  RadioSpeaker,
} from '../types/radio'
import { OpusDecoder } from 'opus-decoder'

// 消息序列号（用于确保同一毫秒内的消息 ID 唯一）
let messageSequence = 0

// 辅助函数：生成消息 ID（本地使用，不再存储到 IndexedDB）
function generateMessageId(groupId: number, timestamp: number, callsign: string): string {
  const seq = messageSequence++
  return `${groupId}_${timestamp}_${callsign}_${seq}`
}

// Opus 配置
const OPUS_SAMPLE_RATE = 16000
const OPUS_CHANNELS = 1

// 将 Float32 PCM 转换为 WAV Blob
function pcmToWav(float32Data: Float32Array, sampleRate: number, channels: number): Blob {
  // 转换为 Int16 PCM
  const int16Data = new Int16Array(float32Data.length)
  for (let i = 0; i < float32Data.length; i++) {
    const s = Math.max(-1, Math.min(1, float32Data[i]))
    int16Data[i] = s < 0 ? s * 0x8000 : s * 0x7FFF
  }

  // 创建 WAV 文件
  const byteRate = sampleRate * channels * 2
  const blockAlign = channels * 2
  const dataSize = int16Data.length * 2
  const bufferSize = 44 + dataSize

  const buffer = new ArrayBuffer(bufferSize)
  const view = new DataView(buffer)

  // RIFF header
  writeString(view, 0, 'RIFF')
  view.setUint32(4, bufferSize - 8, true)
  writeString(view, 8, 'WAVE')

  // fmt chunk
  writeString(view, 12, 'fmt ')
  view.setUint32(16, 16, true) // chunk size
  view.setUint16(20, 1, true) // PCM format
  view.setUint16(22, channels, true)
  view.setUint32(24, sampleRate, true)
  view.setUint32(28, byteRate, true)
  view.setUint16(32, blockAlign, true)
  view.setUint16(34, 16, true) // bits per sample

  // data chunk
  writeString(view, 36, 'data')
  view.setUint32(40, dataSize, true)

  // 写入 PCM 数据
  const int8Data = new Uint8Array(buffer, 44)
  for (let i = 0; i < int16Data.length; i++) {
    int8Data[i * 2] = int16Data[i] & 0xFF
    int8Data[i * 2 + 1] = (int16Data[i] >> 8) & 0xFF
  }

  return new Blob([buffer], { type: 'audio/wav' })
}

function writeString(view: DataView, offset: number, str: string): void {
  for (let i = 0; i < str.length; i++) {
    view.setUint8(offset + i, str.charCodeAt(i))
  }
}

// 解析合并帧格式
// 格式：[Frame1 Length(2B, 大端序)][Frame1 Data][Frame2 Length(2B)][Frame2 Data]
// 兼容单帧格式（无长度前缀）
function parseMergedFrames(data: Uint8Array): Uint8Array[] {
  const frames: Uint8Array[] = []
  let offset = 0

  // 检查是否是合并帧格式
  // 如果第一个字节的值小于 0x80，很可能是长度前缀（Opus 帧通常以 0x80+ 开头）
  while (offset + 2 <= data.length) {
    const frameLength = (data[offset] << 8) | data[offset + 1]

    // 安全检查：帧长度必须合理
    if (frameLength === 0 || frameLength > 1000 || offset + 2 + frameLength > data.length) {
      // 不是合并帧格式，当作单帧处理
      if (offset === 0) {
        return [data]
      }
      break
    }

    // 提取帧数据
    frames.push(data.slice(offset + 2, offset + 2 + frameLength))
    offset += 2 + frameLength
  }

  // 如果没有解析出任何帧，返回原始数据作为单帧
  if (frames.length === 0) {
    return [data]
  }

  return frames
}

// 将 Opus 帧数组解码为 WAV Blob
// 支持合并帧格式：自动解析并解码
async function opusFramesToWav(frames: Uint8Array[]): Promise<Blob> {
  if (frames.length === 0) {
    throw new Error('No frames to decode')
  }

  // 初始化解码器
  const decoder = new OpusDecoder({
    sampleRate: OPUS_SAMPLE_RATE,
    channels: OPUS_CHANNELS,
  })
  await decoder.ready

  try {
    // 解码所有帧（先解析合并帧格式，再解码子帧）
    const decodedFrames: Float32Array[] = []
    for (const frameOrMerged of frames) {
      // 解析合并帧格式（兼容单帧）
      const subFrames = parseMergedFrames(frameOrMerged)
      for (const frame of subFrames) {
        try {
          const decoded = decoder.decodeFrame(frame)
          decodedFrames.push(decoded.channelData[0])
        } catch {
          // 静默忽略解码失败的帧
        }
      }
    }
    if (decodedFrames.length === 0) {
      throw new Error('No frames decoded successfully')
    }

    // 合并所有解码后的 PCM 数据
    const totalSamples = decodedFrames.reduce((sum, frame) => sum + frame.length, 0)
    const mergedPcm = new Float32Array(totalSamples)
    let offset = 0
    for (const frame of decodedFrames) {
      mergedPcm.set(frame, offset)
      offset += frame.length
    }

    // 转换为 WAV
    return pcmToWav(mergedPcm, OPUS_SAMPLE_RATE, OPUS_CHANNELS)
  } finally {
    decoder.free()
  }
}

// 回调类型
export type ConnectionStateCallback = (state: WSConnectionState) => void
export type VoiceStateCallback = (state: VoiceState, callsign?: string) => void
export type MessageCallback = (message: RadioMessage) => void
export type DeviceListCallback = (devices: OnlineDevice[]) => void
export type ErrorCallback = (error: string) => void
export type RoutingCallback = (routing: RadioSessionRouting) => void

// 事件类型
export type RadioEventType =
  | 'connectionStateChange'
  | 'voiceStateChange'
  | 'message'
  | 'deviceListUpdate'
  | 'error'
  | 'speakersChange'
  | 'routingChange'

export interface RadioEventHandlers {
  connectionStateChange?: ConnectionStateCallback
  voiceStateChange?: VoiceStateCallback
  message?: MessageCallback
  deviceListUpdate?: DeviceListCallback
  error?: ErrorCallback
  speakersChange?: (speakers: RadioSpeaker[]) => void
  routingChange?: RoutingCallback
}

interface IncomingVoiceStream {
  key: string
  groupId: number
  callsign: string
  ssid: number
  username: string
  chunks: Uint8Array[]
  startedAt: number
  uiTimer: ReturnType<typeof setTimeout> | null
  commitTimer: ReturnType<typeof setTimeout> | null
}

/**
 * Radio 服务类
 */
export class RadioService {
  // WebSocket
  private ws: RadioWebSocket | null = null

  // 音频
  private audioCapture: AudioCapture | null = null
  private audioMixer: MultiChannelAudioMixer | null = null

  // 配置
  private config: RadioUserConfig

  // 状态
  private connectionState: WSConnectionState = 'disconnected'
  private voiceState: VoiceState = 'idle'
  private routing: RadioSessionRouting = {
    sessionId: '',
    clientInstanceId: '',
    txGroupId: 999,
    rxGroupIds: [999],
  }

  // 用户信息
  private token: string = ''
  private username: string = ''
  private callsign: string = ''
  // JWT 认证设备 SSID 固定为 105（与 DevModel 一致）
  private readonly ssid: number = 105

  // 事件处理器
  private handlers: RadioEventHandlers = {}

  // 设备列表
  private onlineDevices: OnlineDevice[] = []

  private incomingVoiceStreams = new Map<string, IncomingVoiceStream>()
  private activeSpeakers = new Map<string, RadioSpeaker>()

  // 发送语音缓存（用于记录自己发送的语音）
  private sendingVoiceChunks: Uint8Array[] = []
  private sendingVoiceStartTime: number = 0

  constructor() {
    this.config = { ...defaultRadioUserConfig }
    this.loadConfig()
  }

  /**
   * 初始化服务
   * @param token JWT Token
   * @param username 用户名
   * @param callsign 呼号
   * @param lastGroupId 用户上次选中的群组 ID（从登录响应中获取）
   */
  async init(token: string, username: string, callsign: string, lastGroupId?: number): Promise<void> {
    this.token = token
    this.username = username
    this.callsign = callsign
    // ssid 固定为 105，不再从 config 读取

    // 【核心修复】优先使用服务端返回的 lastGroupId
    // 这样可以确保跨设备/跨会话的群组偏好一致
    if (lastGroupId && lastGroupId > 0) {
      this.routing.txGroupId = lastGroupId
      this.routing.rxGroupIds = [lastGroupId]
      this.config.defaultGroupId = lastGroupId
    }

    // 初始化 WebSocket
    this.ws = getRadioWebSocket()
    this.ws.setUserInfo(token, this.ssid, username, callsign)

    this.ws.setOnStateChange((state) => {
      this.connectionState = state
      this.emit('connectionStateChange', state)

    })

    this.ws.setOnRoutingChange((routing) => {
      this.applyRouting({
        sessionId: routing.sessionId,
        clientInstanceId: routing.clientInstanceId,
        txGroupId: routing.txGroupId,
        rxGroupIds: routing.rxGroupIds,
      })
    })

    this.ws.setOnPacket((packet, rawData) => {
      this.handlePacket(packet, rawData)
    })

    this.ws.setOnError((error) => {
      this.emit('error', error)
    })

    // 初始化音频
    this.audioCapture = getAudioCapture()
    this.audioMixer = getAudioMixer()
    this.audioMixer.setVolume(this.config.volume)
    if (this.config.muted) this.audioMixer.mute()
    for (const [groupId, volume] of Object.entries(this.config.channelVolumes)) {
      this.audioMixer.setChannelVolume(Number(groupId), volume)
    }

    this.audioCapture.onData((opusData) => {
      this.sendVoiceData(opusData)
    })

    this.audioCapture.onStateChange((state) => {
      if (state === 'capturing') {
        this.setVoiceState('sending')
      } else {
        this.setVoiceState(this.activeSpeakers.size > 0 ? 'receiving' : 'idle')
        if (this.ws) {
          // 发送语音结束标记
        }
      }
    })

    // 加载群组列表
    await this.loadGroups()

    // 加载历史消息
    await this.loadHistoryMessages()
  }

  /**
   * 连接
   */
  async connect(): Promise<void> {
    if (!this.ws) {
      throw new Error('Radio service not initialized')
    }

    await this.ws.connect()
  }

  /**
   * 断开连接
   */
  disconnect(): void {
    if (this.ws) {
      this.ws.disconnect()
    }

    this.connectionState = 'disconnected'
    this.voiceState = 'idle'
    this.clearIncomingVoiceStreams()
  }

  /**
   * 销毁服务
   */
  destroy(): void {
    this.disconnect()
    this.saveConfig()

    if (this.audioCapture) {
      this.audioCapture.stop()
    }

    destroyAudioInstances()
    closeRadioWebSocket()

    this.ws = null
    this.audioCapture = null
    this.audioMixer = null
  }

  /**
   * 设置事件处理器
   */
  on<K extends keyof RadioEventHandlers>(event: K, handler: NonNullable<RadioEventHandlers[K]>): void {
    (this.handlers as any)[event] = handler
  }

  /**
   * 移除事件处理器
   */
  off<K extends keyof RadioEventHandlers>(event: K): void {
    delete (this.handlers as any)[event]
  }

  /**
   * 触发事件
   */
  private emit<K extends keyof RadioEventHandlers>(
    event: K,
    ...args: any[]
  ): void {
    const handler = (this.handlers as any)[event]
    if (handler) {
      ;(handler as any)(...args)
    }
  }

  /**
   * 获取连接状态
   */
  getConnectionState(): WSConnectionState {
    return this.connectionState
  }

  /**
   * 获取语音状态
   */
  getVoiceState(): VoiceState {
    return this.voiceState
  }

  /**
   * 获取当前群组 ID
   */
  getCurrentGroupId(): number {
    return this.routing.txGroupId
  }

  getRouting(): RadioSessionRouting {
    return { ...this.routing, rxGroupIds: [...this.routing.rxGroupIds] }
  }

  /**
   * 获取在线设备列表
   */
  getOnlineDevices(): OnlineDevice[] {
    return this.onlineDevices
  }

  /**
   * 获取当前说话人
   */
  getActiveSpeakers(): RadioSpeaker[] {
    return Array.from(this.activeSpeakers.values())
  }

  async activateAudio(): Promise<void> {
    await this.audioMixer?.init()
  }

  /**
   * 开始语音发送
   */
  async startVoice(): Promise<void> {
    if (this.voiceState !== 'idle') return
    if (!this.audioCapture) return

    // 初始化发送语音缓存
    this.sendingVoiceChunks = []
    this.sendingVoiceStartTime = Date.now()

    try {
      await this.audioCapture.start()
    } catch (error) {
      console.error('[RadioService] Failed to start voice:', error)
      this.emit('error', '无法启动麦克风')
    }
  }

  /**
   * 停止语音发送
   */
  stopVoice(): void {
    if (this.audioCapture) {
      this.audioCapture.stop()
    }

    if (this.ws) {
      this.ws.voiceSendEnd()
    }

    // 保存发送的语音消息
    this.saveSendingVoiceMessage()

    this.setVoiceState(this.activeSpeakers.size > 0 ? 'receiving' : 'idle')
  }

  /**
   * 保存发送的语音消息到缓存
   */
  private async saveSendingVoiceMessage(): Promise<void> {
    // 检查是否有语音数据
    if (this.sendingVoiceChunks.length === 0) return

    // 计算语音时长
    const duration = Date.now() - this.sendingVoiceStartTime

    try {
      // 将 Opus 帧转换为 WAV 格式
      const voiceBlob = await opusFramesToWav(this.sendingVoiceChunks)

      // 创建消息
      const radioMessage: RadioMessage = {
        id: generateMessageId(this.routing.txGroupId, this.sendingVoiceStartTime, this.callsign),
        type: 'voice',
        groupId: this.routing.txGroupId,
        groupName: groupManagerService.getCachedGroup(this.routing.txGroupId)?.name,
        senderId: `ghost-${this.ssid}`,
        senderCallsign: this.callsign,
        senderSSID: this.ssid,
        content: voiceBlob,
        duration: duration,
        timestamp: this.sendingVoiceStartTime,
        isSelf: true,
        isPlayed: true, // 自己发送的语音默认已播放
      }

      // 触发事件（通知 UI 更新）- 不再存储到 IndexedDB
      this.emit('message', radioMessage)
    } catch (error) {
      console.error('[RadioService] Failed to save voice message:', error)
    }

    // 清空缓存
    this.sendingVoiceChunks = []
  }

  /**
   * 发送文本消息
   */
  sendTextMessage(message: string): void {
    if (!this.ws || this.connectionState !== 'online') {
      this.emit('error', '未连接')
      return
    }

    this.ws.sendTextMessage(message)

    // 直接触发事件，不再存储到 IndexedDB
    const radioMessage: RadioMessage = {
      id: generateMessageId(this.routing.txGroupId, Date.now(), this.callsign),
      type: 'text',
      groupId: this.routing.txGroupId,
      groupName: groupManagerService.getCachedGroup(this.routing.txGroupId)?.name,
      senderId: `ghost-${this.ssid}`,
      senderCallsign: this.callsign,
      senderSSID: this.ssid,
      content: message,
      timestamp: Date.now(),
      isSelf: true,
    }

    this.emit('message', radioMessage)
  }

  async switchGroup(groupId: number): Promise<boolean> {
    return this.updateRouting(groupId, this.routing.rxGroupIds)
  }

  async setReceiveGroups(groupIds: number[]): Promise<boolean> {
    return this.updateRouting(this.routing.txGroupId, groupIds)
  }

  async updateRouting(txGroupId: number, rxGroupIds: number[]): Promise<boolean> {
    if (!this.ws || this.connectionState !== 'online') {
      this.emit('error', '未连接')
      return false
    }
    if (!this.routing.sessionId) {
      this.emit('error', '在线会话尚未就绪')
      return false
    }

    try {
      const normalizedRx = Array.from(new Set([...rxGroupIds, txGroupId])).filter(groupId => groupId > 0)
      const response = await apiClient.put<{
        code: number
        message: string
        data: {
          session_id: string
          client_instance_id: string
          tx_group_id: number
          rx_group_ids: number[]
        }
      }>(`/api/radio/sessions/${this.routing.sessionId}/routing`, {
        tx_group_id: txGroupId,
        rx_group_ids: normalizedRx,
      })

      if (response.code !== 200 || !response.data) throw new Error(response.message || '频道路由更新失败')
      this.applyRouting({
        sessionId: response.data.session_id,
        clientInstanceId: response.data.client_instance_id || this.routing.clientInstanceId,
        txGroupId: response.data.tx_group_id,
        rxGroupIds: response.data.rx_group_ids,
      })
      return true
    } catch (error) {
      console.error('[RadioService] Failed to update routing:', error)
      const message = (error as { response?: { data?: { message?: string } }; message?: string })
        .response?.data?.message || (error as Error).message || '频道路由更新失败'
      this.emit('error', message)
      return false
    }
  }

  /**
   * 设置音量
   */
  setVolume(volume: number): void {
    this.config.volume = volume
    this.audioMixer?.setVolume(volume)
    this.saveConfig()
  }

  /**
   * 设置静音
   */
  setMuted(muted: boolean): void {
    this.config.muted = muted
    if (this.audioMixer) {
      if (muted) {
        this.audioMixer.mute()
      } else {
        this.audioMixer.unmute()
      }
    }
    this.saveConfig()
  }

  /**
   * 设置 SSID
   * @deprecated JWT 认证设备 SSID 固定为 105，此方法不再有效
   */
  setSSID(_ssid: number): void {
    console.warn('[RadioService] setSSID is deprecated: JWT devices use fixed SSID 105')
  }

  /**
   * 获取配置
   */
  getConfig(): RadioUserConfig {
    return { ...this.config, channelVolumes: { ...this.config.channelVolumes } }
  }

  setChannelVolume(groupId: number, volume: number): void {
    const normalized = Math.max(0, Math.min(1, volume))
    this.config.channelVolumes[String(groupId)] = normalized
    this.audioMixer?.setChannelVolume(groupId, normalized)
    this.saveConfig()
  }

  /**
   * 获取群组列表
   */
  async getGroups(): Promise<RadioGroup[]> {
    const groups = await groupManagerService.getGroups()
    return groups.map(toRadioGroup)
  }

  /**
   * 刷新群组统计（从后端获取最新的在线设备数）
   * 此方法会更新本地缓存中的 onlineCount
   */
  async refreshGroupStats(): Promise<RadioGroup[]> {
    const groups = await groupManagerService.refreshGroupStats()
    return groups.map(toRadioGroup)
  }

  /**
   * 获取历史消息
   * @deprecated 不再从本地缓存获取，改由页面组件调用 messageSyncService
   */
  async getHistoryMessages(_groupId?: number): Promise<RadioMessage[]> {
    // 不再从 IndexedDB 获取，返回空数组
    // 历史消息由页面组件通过 messageSyncService 获取
    return []
  }

  /**
   * 清空当前会话的内存缓存
   * @returns 是否成功
   */
  public async clearAllMessageCache(): Promise<boolean> {
    try {
      this.sendingVoiceChunks = []
      this.clearIncomingVoiceStreams()

      return true
    } catch {
      return false
    }
  }

  // ==================== 私有方法 ====================

  private applyRouting(routing: RadioSessionRouting): void {
    const rxGroupIds = Array.from(new Set([...routing.rxGroupIds, routing.txGroupId]))
      .filter(groupId => Number.isInteger(groupId) && groupId > 0)
    const previousRx = new Set(this.routing.rxGroupIds)
    const nextRx = new Set(rxGroupIds)
    for (const groupId of previousRx) {
      if (!nextRx.has(groupId)) {
        this.removeIncomingGroup(groupId)
        this.audioMixer?.removeChannel(groupId)
      }
    }

    this.routing = { ...routing, rxGroupIds }
    this.config.defaultGroupId = routing.txGroupId
    this.saveConfig()
    this.emit('routingChange', this.getRouting())
  }

  /**
   * 处理收到的数据包
   */
  private handlePacket(packet: DraARLPacket, rawData: ArrayBuffer): void {
    switch (packet.type) {
      case PacketType.HEARTBEAT:
        // 心跳响应，更新设备列表
        this.handleHeartbeat(packet)
        break

      case PacketType.OPUS_16K:
        // 语音包
        this.handleVoicePacket(packet, rawData)
        break

      case PacketType.TEXT_MESSAGE:
        // 文本消息
        this.handleTextPacket(packet)
        break

      default:
        // 忽略其他类型
        break
    }
  }

  /**
   * 处理心跳包
   */
  private handleHeartbeat(_packet: DraARLPacket): void {
    // 心跳包中可能包含服务器状态信息
    // 可以用于更新在线设备列表等
  }

  /**
   * 处理语音包
   */
  private handleVoicePacket(packet: DraARLPacket, _rawData: ArrayBuffer): void {
    if (!packet.data?.length) return
    const groupId = packet.sourceGroupId || this.routing.txGroupId
    const streamKey = `${groupId}:${packet.username || packet.callsign}:${packet.ssid}`
    let stream = this.incomingVoiceStreams.get(streamKey)
    if (!stream) {
      stream = {
        key: streamKey,
        groupId,
        callsign: packet.callsign,
        ssid: packet.ssid,
        username: packet.username || '',
        chunks: [],
        startedAt: Date.now(),
        uiTimer: null,
        commitTimer: null,
      }
      this.incomingVoiceStreams.set(streamKey, stream)
      this.activeSpeakers.set(streamKey, {
        key: streamKey,
        groupId,
        callsign: packet.callsign,
        ssid: packet.ssid,
        username: packet.username || undefined,
      })
      this.emitSpeakersChange()
    }

    stream.chunks.push(new Uint8Array(packet.data))
    void this.audioMixer?.play(streamKey, groupId, packet.data)
    this.setVoiceState('receiving')
    this.resetIncomingVoiceTimers(stream)
  }

  /**
   * 处理文本消息包
   */
  private handleTextPacket(packet: DraARLPacket): void {
    if (!packet.data || packet.data.length === 0) return

    const message = new TextDecoder().decode(packet.data)

    const groupId = packet.sourceGroupId || this.routing.txGroupId
    const radioMessage: RadioMessage = {
      id: generateMessageId(groupId, Date.now(), packet.callsign),
      type: 'text',
      groupId,
      groupName: groupManagerService.getCachedGroup(groupId)?.name,
      senderId: packet.username || packet.callsign,
      senderCallsign: packet.callsign,
      senderSSID: packet.ssid,
      content: message,
      timestamp: Date.now(),
      isSelf: packet.username === this.username && packet.callsign === this.callsign && packet.ssid === this.ssid,
    }

    // 直接触发事件，不再存储到 IndexedDB
    this.emit('message', radioMessage)
  }

  /**
   * 发送语音数据
   */
  private sendVoiceData(opusData: Uint8Array): void {
    if (this.ws && this.connectionState === 'online') {
      // 收集发送的语音数据
      this.sendingVoiceChunks.push(new Uint8Array(opusData))
      // 发送到服务器
      this.ws.sendVoice(opusData)
    }
  }

  /**
   * 设置语音状态
   */
  private setVoiceState(state: VoiceState): void {
    if (this.voiceState !== state) {
      this.voiceState = state
      this.emit('voiceStateChange', state, this.getActiveSpeakers()[0]?.callsign)
    }
  }

  private resetIncomingVoiceTimers(stream: IncomingVoiceStream): void {
    if (stream.uiTimer) clearTimeout(stream.uiTimer)
    if (stream.commitTimer) clearTimeout(stream.commitTimer)
    stream.uiTimer = setTimeout(() => {
      this.activeSpeakers.delete(stream.key)
      this.emitSpeakersChange()
      if (this.activeSpeakers.size === 0 && this.voiceState !== 'sending') this.setVoiceState('idle')
    }, 600)
    stream.commitTimer = setTimeout(() => {
      this.incomingVoiceStreams.delete(stream.key)
      this.audioMixer?.resetStream(stream.key)
      void this.saveIncomingVoiceStream(stream)
    }, 1500)
  }

  private async saveIncomingVoiceStream(stream: IncomingVoiceStream): Promise<void> {
    if (stream.chunks.length === 0) return

    try {
      const voiceBlob = await opusFramesToWav(stream.chunks)
      const isSelf = stream.username === this.username && stream.callsign === this.callsign && stream.ssid === this.ssid
      const radioMessage: RadioMessage = {
        id: generateMessageId(stream.groupId, stream.startedAt, stream.callsign),
        type: 'voice',
        groupId: stream.groupId,
        groupName: groupManagerService.getCachedGroup(stream.groupId)?.name,
        senderId: `${stream.callsign}-${stream.ssid}`,
        senderCallsign: stream.callsign,
        senderSSID: stream.ssid,
        senderUsername: stream.username || undefined,
        content: voiceBlob,
        duration: Date.now() - stream.startedAt,
        timestamp: stream.startedAt,
        isSelf,
        isPlayed: true,
      }
      this.emit('message', radioMessage)
    } catch (error) {
      console.error('[RadioService] Failed to save voice message:', error)
    }
  }

  private emitSpeakersChange(): void {
    this.emit('speakersChange', this.getActiveSpeakers())
  }

  private removeIncomingGroup(groupId: number): void {
    for (const [streamKey, stream] of this.incomingVoiceStreams) {
      if (stream.groupId !== groupId) continue
      if (stream.uiTimer) clearTimeout(stream.uiTimer)
      if (stream.commitTimer) clearTimeout(stream.commitTimer)
      this.incomingVoiceStreams.delete(streamKey)
      this.activeSpeakers.delete(streamKey)
      this.audioMixer?.resetStream(streamKey)
    }
    this.emitSpeakersChange()
  }

  private clearIncomingVoiceStreams(): void {
    for (const stream of this.incomingVoiceStreams.values()) {
      if (stream.uiTimer) clearTimeout(stream.uiTimer)
      if (stream.commitTimer) clearTimeout(stream.commitTimer)
      this.audioMixer?.resetStream(stream.key)
    }
    this.incomingVoiceStreams.clear()
    this.activeSpeakers.clear()
    this.emitSpeakersChange()
  }

  /**
   * 加载配置
   */
  private loadConfig(): void {
    try {
      const saved = localStorage.getItem('radio-config')
      if (saved) {
        const parsed = JSON.parse(saved) as Partial<RadioUserConfig>
        this.config = {
          ...defaultRadioUserConfig,
          ...parsed,
          channelVolumes: parsed.channelVolumes && typeof parsed.channelVolumes === 'object'
            ? { ...parsed.channelVolumes }
            : {},
        }
      }
    } catch (error) {
      console.error('[RadioService] Failed to load config:', error)
    }
  }

  /**
   * 保存配置
   */
  private saveConfig(): void {
    try {
      localStorage.setItem('radio-config', JSON.stringify(this.config))
    } catch (error) {
      console.error('[RadioService] Failed to save config:', error)
    }
  }

  /**
   * 加载群组列表
   */
  private async loadGroups(): Promise<void> {
    try {
      const groups = await groupManagerService.getGroups()

      // 查找默认群组
      if (this.config.defaultGroupId) {
        const defaultGroup = groups.find(g => g.id === this.config.defaultGroupId)
        if (defaultGroup) {
          this.routing.txGroupId = defaultGroup.id
          this.routing.rxGroupIds = [defaultGroup.id]
        }
      }

      // 如果没有默认群组，使用第一个可用群组
      if (!this.routing.txGroupId && groups.length > 0) {
        this.routing.txGroupId = groups[0].id
        this.routing.rxGroupIds = [groups[0].id]
      }
    } catch (error) {
      console.error('[RadioService] Failed to load groups:', error)
    }
  }

  /**
   * 加载历史消息
   * @deprecated 不再从 IndexedDB 加载，改由页面组件调用 messageSyncService
   */
  private async loadHistoryMessages(): Promise<void> {
    // 不再从 IndexedDB 加载历史消息
    // 历史消息由页面组件通过 messageSyncService 获取
    return Promise.resolve()
  }
}

// 单例
let radioServiceInstance: RadioService | null = null

export function getRadioService(): RadioService {
  if (!radioServiceInstance) {
    radioServiceInstance = new RadioService()
  }
  return radioServiceInstance
}

export function destroyRadioService(): void {
  if (radioServiceInstance) {
    radioServiceInstance.destroy()
    radioServiceInstance = null
  }
}
