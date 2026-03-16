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

export interface RadioEventHandlers {
  connectionStateChange?: ConnectionStateCallback
  voiceStateChange?: VoiceStateCallback
  message?: MessageCallback
  deviceListUpdate?: DeviceListCallback
  error?: ErrorCallback
  speaking?: (callsign: string, ssid: number) => void
  speakingEnd?: (callsign: string, ssid: number) => void
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
  private ssid: number = 10

  // 事件处理器
  private handlers: RadioEventHandlers = {}

  // 设备列表
  private onlineDevices: OnlineDevice[] = []

  // 当前说话人
  private currentSpeaker: { callsign: string; ssid: number } | null = null

  // 语音结束检测
  private voiceEndTimer: ReturnType<typeof setTimeout> | null = null

  constructor() {
    this.config = { ...defaultRadioUserConfig }
    this.loadConfig()
  }

  /**
   * 初始化服务
   */
  async init(token: string, username: string, callsign: string): Promise<void> {
    this.token = token
    this.username = username
    this.callsign = callsign
    this.ssid = this.config.ssid

    // 初始化 WebSocket
    this.ws = getRadioWebSocket()
    this.ws.setUserInfo(token, this.ssid, username, callsign)

    this.ws.setOnStateChange((state) => {
      this.connectionState = state
      this.emit('connectionStateChange', state)
    })

    this.ws.setOnPacket((packet, rawData) => {
      this.handlePacket(packet, rawData)
    })

    this.ws.setOnError((error) => {
      this.emit('error', error)
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

    this.setVoiceState('idle')
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
   */
  setSSID(ssid: number): void {
    this.ssid = ssid
    this.config.ssid = ssid
    if (this.ws) {
      this.ws.setUserInfo(this.token, ssid, this.username, this.callsign)
    }
    this.saveConfig()
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
   * 获取历史消息
   */
  async getHistoryMessages(groupId?: number): Promise<RadioMessage[]> {
    const targetGroupId = groupId || this.currentGroupId
    const cached = await messageCache.getMessagesByGroup(targetGroupId)
    return cached.map(toRadioMessage)
  }

  // ==================== 私有方法 ====================

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
      const originalCallsign = new TextDecoder()
        .decode(packet.data.slice(32, 64))
        .replace(/\0/g, '')

      // 使用原始呼号作为说话人
      this.updateSpeaker(originalCallsign, packet.ssid)

      this.setVoiceState('receiving')

      if (this.audioPlayer && !this.config.muted) {
        const voiceData = packet.data.slice(68)
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

    this.voiceEndTimer = setTimeout(() => {
      if (this.currentSpeaker) {
        this.emit('speakingEnd', this.currentSpeaker.callsign, this.currentSpeaker.ssid)
        this.currentSpeaker = null
      }
      this.setVoiceState('idle')
    }, 200)
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
