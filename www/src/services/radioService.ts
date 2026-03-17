/**
 * Radio 服务
 * 整合 WebSocket、音频、消息缓存和群组管理
 */

import { RadioWebSocket, getRadioWebSocket, closeRadioWebSocket } from './radio/websocket'
import { AudioCapture, AudioPlayer, getAudioCapture, getAudioPlayer, destroyAudioInstances } from './radio/opus'
import { messageCache, toCachedMessage, toRadioMessage, generateMessageId } from './radio/messageCache'
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
} from '../types/radio'
import { OpusDecoder } from 'opus-decoder'

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

// 将 Opus 帧数组解码为 WAV Blob
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
    // 解码所有帧
    const decodedFrames: Float32Array[] = []
    for (const frame of frames) {
      try {
        const decoded = decoder.decodeFrame(frame)
        decodedFrames.push(decoded.channelData[0])
      } catch (e) {
        // 静默忽略解码失败的帧
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

// 事件类型
export type RadioEventType =
  | 'connectionStateChange'
  | 'voiceStateChange'
  | 'message'
  | 'deviceListUpdate'
  | 'error'
  | 'speaking'
  | 'speakingEnd'
  | 'conflict' // 【新增】连接冲突事件

export interface RadioEventHandlers {
  connectionStateChange?: ConnectionStateCallback
  voiceStateChange?: VoiceStateCallback
  message?: MessageCallback
  deviceListUpdate?: DeviceListCallback
  error?: ErrorCallback
  speaking?: (callsign: string, ssid: number) => void
  speakingEnd?: (callsign: string, ssid: number) => void
  conflict?: () => void // 【新增】连接冲突回调
}

/**
 * Radio 服务类
 */
export class RadioService {
  // WebSocket
  private ws: RadioWebSocket | null = null

  // 音频
  private audioCapture: AudioCapture | null = null
  private audioPlayer: AudioPlayer | null = null

  // 配置
  private config: RadioUserConfig

  // 状态
  private connectionState: WSConnectionState = 'disconnected'
  private voiceState: VoiceState = 'idle'
  private currentGroupId: number = 999

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

  // 当前说话人
  private currentSpeaker: { callsign: string; ssid: number } | null = null

  // 语音结束检测
  private voiceEndTimer: ReturnType<typeof setTimeout> | null = null

  // 语音消息缓存（用于记录接收的语音）
  private voiceChunks: Uint8Array[] = []
  private voiceStartTime: number = 0
  private currentVoiceCallsign: string = ''
  private currentVoiceSSID: number = 0
  private currentVoiceUsername: string = '' // 发送方用户名/昵称

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
      this.currentGroupId = lastGroupId
      this.config.defaultGroupId = lastGroupId
    }

    // 初始化 WebSocket
    this.ws = getRadioWebSocket()
    this.ws.setUserInfo(token, this.ssid, username, callsign)

    this.ws.setOnStateChange((state) => {
      this.connectionState = state
      this.emit('connectionStateChange', state)

      // 【核心修复】WS 重连后自动同步群组到服务端
      // 无论初次连接还是断线重连，只要状态变为 online，就调用 API 确保服务端群组同步
      if (state === 'online' && this.currentGroupId > 0) {
        this.syncGroupToServer(this.currentGroupId)
      }
    })

    this.ws.setOnPacket((packet, rawData) => {
      this.handlePacket(packet, rawData)
    })

    this.ws.setOnError((error) => {
      this.emit('error', error)
    })

    // 【新增】设置连接冲突回调
    this.ws.setOnConflict(() => {
      this.emit('conflict')
    })

    // 初始化音频
    this.audioCapture = getAudioCapture()
    this.audioPlayer = getAudioPlayer()

    this.audioCapture.onData((opusData) => {
      this.sendVoiceData(opusData)
    })

    this.audioCapture.onStateChange((state) => {
      if (state === 'capturing') {
        this.setVoiceState('sending')
      } else {
        this.setVoiceState('idle')
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
    this.currentSpeaker = null
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
    this.audioPlayer = null
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
    return this.currentGroupId
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
  getCurrentSpeaker(): { callsign: string; ssid: number } | null {
    return this.currentSpeaker
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

    this.setVoiceState('idle')
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
        id: generateMessageId(this.currentGroupId, this.sendingVoiceStartTime, this.callsign),
        type: 'voice',
        groupId: this.currentGroupId,
        senderId: `ghost-${this.ssid}`,
        senderCallsign: this.callsign,
        senderSSID: this.ssid,
        content: voiceBlob,
        duration: duration,
        timestamp: this.sendingVoiceStartTime,
        isSelf: true,
        isPlayed: true, // 自己发送的语音默认已播放
      }

      // 添加到缓存
      messageCache.addMessage(toCachedMessage(radioMessage))

      // 触发事件（通知 UI 更新）
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

    // 添加到本地缓存
    const radioMessage: RadioMessage = {
      id: generateMessageId(this.currentGroupId, Date.now(), this.callsign),
      type: 'text',
      groupId: this.currentGroupId,
      senderId: `ghost-${this.ssid}`,
      senderCallsign: this.callsign,
      senderSSID: this.ssid,
      content: message,
      timestamp: Date.now(),
      isSelf: true,
    }

    messageCache.addMessage(toCachedMessage(radioMessage))
    this.emit('message', radioMessage)
  }

  /**
   * 切换群组
   * 【关键修复】幽灵设备切换群组需要调用 HTTP API 来同步更新内存状态
   * 这会同时更新 WSDevice.GroupID 和 GhostDevice.GroupID，实现跨协议通信
   */
  async switchGroup(groupId: number): Promise<boolean> {
    if (!this.ws || this.connectionState !== 'online') {
      this.emit('error', '未连接')
      return false
    }

    // 【核心修改】调用 HTTP API 而不是 WebSocket Config 包
    // 这样后端可以同步更新幽灵设备的内存群组状态
    try {
      const response = await apiClient.put<{ code: number; message: string }>(`/api/radio/group`, {
        group_id: groupId,
      })

      if (response.code === 200) {
        // 更新本地状态
        this.currentGroupId = groupId
        this.config.defaultGroupId = groupId
        this.saveConfig()

        // 加载新群组的历史消息
        await this.loadHistoryMessages()

        return true
      }
      this.emit('error', response.message || '切换群组失败')
      return false
    } catch (error) {
      console.error('[RadioService] Failed to switch group:', error)
      this.emit('error', '切换群组失败')
      return false
    }
  }

  /**
   * 设置音量
   */
  setVolume(volume: number): void {
    this.config.volume = volume
    if (this.audioPlayer) {
      this.audioPlayer.setVolume(volume)
    }
    this.saveConfig()
  }

  /**
   * 设置静音
   */
  setMuted(muted: boolean): void {
    this.config.muted = muted
    if (this.audioPlayer) {
      if (muted) {
        this.audioPlayer.mute()
      } else {
        this.audioPlayer.unmute()
      }
    }
    this.saveConfig()
  }

  /**
   * 设置 SSID
   * @deprecated JWT 认证设备 SSID 固定为 105，此方法不再有效
   */
  setSSID(ssid: number): void {
    console.warn('[RadioService] setSSID is deprecated: JWT devices use fixed SSID 105')
  }

  /**
   * 获取配置
   */
  getConfig(): RadioUserConfig {
    return { ...this.config }
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
   */
  async getHistoryMessages(groupId?: number): Promise<RadioMessage[]> {
    const targetGroupId = groupId || this.currentGroupId
    const cached = await messageCache.getMessagesByGroup(targetGroupId)
    return cached.map(toRadioMessage)
  }

  /**
   * 彻底清空当前所有的消息缓存 (包括数据库和内存)
   * @returns 是否成功
   */
  public async clearAllMessageCache(): Promise<boolean> {
    try {
      // 1. 清空底层 IndexedDB 数据库
      await messageCache.clearAllMessages()

      // 2. 清空 Service 内部的语音缓存
      this.voiceChunks = []
      this.sendingVoiceChunks = []
      this.currentVoiceCallsign = ''
      this.currentVoiceSSID = 0
      this.currentVoiceUsername = ''

      return true
    } catch (error) {
      return false
    }
  }

  // ==================== 私有方法 ====================

  /**
   * 【核心修复】同步群组到服务端
   * 在 WS 重连后调用，确保后端的游离 WS 实例被拉回到用户期望的群组
   */
  private async syncGroupToServer(groupId: number): Promise<void> {
    try {
      await apiClient.put<{ code: number; message: string }>(`/api/radio/group`, {
        group_id: groupId,
      })
      // 静默处理同步结果
    } catch {
      // 静默处理同步失败
    }
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

      case PacketType.SERVER_VOICE:
        // 服务器互联语音
        this.handleServerVoicePacket(packet, rawData)
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
  private handleHeartbeat(packet: DraARLPacket): void {
    // 心跳包中可能包含服务器状态信息
    // 可以用于更新在线设备列表等
  }

  /**
   * 处理语音包
   */
  private handleVoicePacket(packet: DraARLPacket, rawData: ArrayBuffer): void {
    // 更新说话人
    this.updateSpeaker(packet.callsign, packet.ssid)

    // 设置语音状态
    this.setVoiceState('receiving')

    // 收集语音数据用于消息记录
    if (packet.data && packet.data.length > 0) {
      // 如果是新说话人，重置缓存
      if (this.currentVoiceCallsign !== packet.callsign || this.currentVoiceSSID !== packet.ssid) {
        this.voiceChunks = []
        this.voiceStartTime = Date.now()
        this.currentVoiceCallsign = packet.callsign
        this.currentVoiceSSID = packet.ssid
        this.currentVoiceUsername = packet.username || ''
      }
      // 收集语音数据
      this.voiceChunks.push(new Uint8Array(packet.data))
    }

    // 播放音频
    if (this.audioPlayer && !this.config.muted) {
      const opusData = packet.data
      if (opusData && opusData.length > 0) {
        this.audioPlayer.play(opusData)
      }
    }

    // 重置语音结束检测
    this.resetVoiceEndTimer()
  }

  /**
   * 处理服务器互联语音包
   */
  private handleServerVoicePacket(packet: DraARLPacket, rawData: ArrayBuffer): void {
    // 服务器互联语音包的 DATA 区域包含原始发送方信息
    // 前 32 字节：原始用户名
    // 32-64 字节：原始呼号
    // 64-68 字节：原始 IP
    // 68+ 字节：语音数据

    if (packet.data && packet.data.length >= 68) {
      // 解析原始发送方信息
      const originalUsername = new TextDecoder()
        .decode(packet.data.slice(0, 32))
        .replace(/\0/g, '')
      const originalCallsign = new TextDecoder()
        .decode(packet.data.slice(32, 64))
        .replace(/\0/g, '')

      // 使用原始呼号作为说话人
      this.updateSpeaker(originalCallsign, packet.ssid)

      this.setVoiceState('receiving')

      // 收集语音数据用于消息记录
      const voiceData = packet.data.slice(68)
      if (voiceData.length > 0) {
        // 如果是新说话人，重置缓存
        if (this.currentVoiceCallsign !== originalCallsign || this.currentVoiceSSID !== packet.ssid) {
          this.voiceChunks = []
          this.voiceStartTime = Date.now()
          this.currentVoiceCallsign = originalCallsign
          this.currentVoiceSSID = packet.ssid
          this.currentVoiceUsername = originalUsername
        }
        // 收集语音数据
        this.voiceChunks.push(new Uint8Array(voiceData))
      }

      if (this.audioPlayer && !this.config.muted) {
        if (voiceData.length > 0) {
          this.audioPlayer.play(voiceData)
        }
      }

      this.resetVoiceEndTimer()
    }
  }

  /**
   * 处理文本消息包
   */
  private handleTextPacket(packet: DraARLPacket): void {
    if (!packet.data || packet.data.length === 0) return

    const message = new TextDecoder().decode(packet.data)

    const radioMessage: RadioMessage = {
      id: generateMessageId(this.currentGroupId, Date.now(), packet.callsign),
      type: 'text',
      groupId: this.currentGroupId,
      senderId: packet.username || packet.callsign,
      senderCallsign: packet.callsign,
      senderSSID: packet.ssid,
      content: message,
      timestamp: Date.now(),
      isSelf: false,
    }

    // 添加到缓存
    messageCache.addMessage(toCachedMessage(radioMessage))

    // 触发事件
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
   * 更新说话人
   */
  private updateSpeaker(callsign: string, ssid: number): void {
    const newSpeaker = { callsign, ssid }
    const isDifferent = !this.currentSpeaker ||
      this.currentSpeaker.callsign !== callsign ||
      this.currentSpeaker.ssid !== ssid

    if (isDifferent) {
      // 旧说话人结束
      if (this.currentSpeaker) {
        this.emit('speakingEnd', this.currentSpeaker.callsign, this.currentSpeaker.ssid)
      }

      // 新说话人开始 - 重置解码器和播放队列，避免旧状态干扰
      // 【关键修复】：resetDecoder() 会重置 Opus 解码器的内部状态，
      // 解决 WebSocket 重连后"重音和卡顿"的问题
      if (this.audioPlayer) {
        this.audioPlayer.resetDecoder()
      }

      // 新说话人开始
      this.currentSpeaker = newSpeaker
      this.emit('speaking', callsign, ssid)
    }
  }

  /**
   * 设置语音状态
   */
  private setVoiceState(state: VoiceState): void {
    if (this.voiceState !== state) {
      this.voiceState = state
      this.emit('voiceStateChange', state, this.currentSpeaker?.callsign)
    }
  }

  /**
   * 重置语音结束检测
   */
  private resetVoiceEndTimer(): void {
    if (this.voiceEndTimer) {
      clearTimeout(this.voiceEndTimer)
    }

    this.voiceEndTimer = setTimeout(async () => {
      // 保存语音消息到缓存
      try {
        await this.saveVoiceMessage()
      } catch (error) {
        console.error('[RadioService] Failed to save voice message:', error)
      }

      if (this.currentSpeaker) {
        this.emit('speakingEnd', this.currentSpeaker.callsign, this.currentSpeaker.ssid)
        this.currentSpeaker = null
      }
      this.setVoiceState('idle')
    }, 200)
  }

  /**
   * 保存语音消息到缓存（仅保存接收的语音，自己发送的由 saveSendingVoiceMessage 处理）
   */
  private async saveVoiceMessage(): Promise<void> {
    // 检查是否有语音数据
    if (this.voiceChunks.length === 0) {
      return
    }
    if (!this.currentVoiceCallsign) {
      return
    }

    // 【关键修复】如果是自己发送的语音，跳过保存
    if (this.currentVoiceCallsign === this.callsign && this.currentVoiceSSID === this.ssid) {
      this.voiceChunks = []
      this.currentVoiceCallsign = ''
      this.currentVoiceSSID = 0
      this.currentVoiceUsername = ''
      return
    }

    // 计算语音时长
    const duration = Date.now() - this.voiceStartTime

    try {
      // 将 Opus 帧转换为 WAV 格式
      const voiceBlob = await opusFramesToWav(this.voiceChunks)

      // 创建消息
      const isSelf = this.currentVoiceCallsign === this.callsign && this.currentVoiceSSID === this.ssid
      const radioMessage: RadioMessage = {
        id: generateMessageId(this.currentGroupId, this.voiceStartTime, this.currentVoiceCallsign),
        type: 'voice',
        groupId: this.currentGroupId,
        senderId: isSelf ? `ghost-${this.ssid}` : `${this.currentVoiceCallsign}-${this.currentVoiceSSID}`,
        senderCallsign: this.currentVoiceCallsign,
        senderSSID: this.currentVoiceSSID,
        senderNickname: this.currentVoiceUsername || undefined,
        content: voiceBlob,
        duration: duration,
        timestamp: this.voiceStartTime,
        isSelf: isSelf,
        isPlayed: true, // 接收的语音默认已播放（实时听到）
      }

      // 添加到缓存
      messageCache.addMessage(toCachedMessage(radioMessage))

      // 触发事件（通知 UI 更新）
      this.emit('message', radioMessage)
    } catch (error) {
      console.error('[RadioService] Failed to save voice message:', error)
    } finally {
      // 清空缓存
      this.voiceChunks = []
      this.currentVoiceCallsign = ''
      this.currentVoiceSSID = 0
      this.currentVoiceUsername = ''
    }
  }

  /**
   * 加载配置
   */
  private loadConfig(): void {
    try {
      const saved = localStorage.getItem('radio-config')
      if (saved) {
        this.config = { ...defaultRadioUserConfig, ...JSON.parse(saved) }
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
          this.currentGroupId = defaultGroup.id
        }
      }

      // 如果没有默认群组，使用第一个可用群组
      if (!this.currentGroupId && groups.length > 0) {
        this.currentGroupId = groups[0].id
      }
    } catch (error) {
      console.error('[RadioService] Failed to load groups:', error)
    }
  }

  /**
   * 加载历史消息
   */
  private async loadHistoryMessages(): Promise<void> {
    try {
      const messages = await messageCache.getMessagesByGroup(this.currentGroupId)
      messages.forEach(msg => {
        this.emit('message', toRadioMessage(msg))
      })
    } catch (error) {
      console.error('[RadioService] Failed to load history messages:', error)
    }
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
