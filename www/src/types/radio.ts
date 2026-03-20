// 在线收发页面相关类型定义

// WebSocket 连接状态
export type WSConnectionState =
  | 'disconnected'   // 已断开
  | 'connecting'     // 连接中
  | 'authenticating' // 认证中
  | 'online'         // 在线
  | 'reconnecting'   // 重连中

// 语音状态
export type VoiceState =
  | 'idle'      // 空闲
  | 'sending'   // 正在发送
  | 'receiving' // 正在接收

// 消息类型
export type MessageType =
  | 'voice' // 语音消息
  | 'text'  // 文本消息

// DraARLv1 数据包类型
export const PacketType = {
  CONTROL: 0,
  HEARTBEAT: 2,
  CONFIG: 3,
  TEXT_MESSAGE: 4,
  OPUS_16K: 5,
  SERVER_VOICE: 6,
  AT_PASSTHROUGH: 7,
} as const

// 设备型号
export const DeviceModel = {
  UNKNOWN: 0,
  WECHAT_MINI: 100,
  ANDROID: 101,
  IOS: 102,
  WINDOWS: 103,
  BROWSER: 105, // 幽灵设备使用
  INTERCONNECT: 106,
} as const

// 在线设备信息
export interface OnlineDevice {
  id: number | string
  username: string
  callsign: string
  ssid: number
  nickname?: string
  devModel: number
  groupId: number
  isOnline: boolean
  isGhost: boolean // 是否是幽灵设备
  disableSend: boolean
  disableRecv: boolean
  lastPacketTime?: string
}

// 消息
export interface RadioMessage {
  id: string
  type: MessageType
  groupId: number

  // 发送者信息
  senderId: number | string
  senderCallsign: string
  senderSSID: number
  senderNickname?: string
  senderAvatar?: string

  // 消息内容
  content: string | Blob // 文本内容或语音 Blob
  duration?: number // 语音时长 (ms)

  // 时间信息
  timestamp: number

  // 状态
  isSelf: boolean // 是否是自己发送的
  isPlayed?: boolean // 语音是否已播放
}

// 群组信息（用于群组选择器）
export interface RadioGroup {
  id: number
  name: string
  callsign?: string
  type: number
  status: number
  onlineCount: number // 在线设备数
  isDefault?: boolean
}

// WebSocket 配置
export interface WSConfig {
  url: string
  reconnectInterval: number // 重连间隔 (ms)
  maxReconnectAttempts: number // 最大重连次数
  heartbeatInterval: number // 心跳间隔 (ms)
  preReconnectTime: number // 预重连时间 (ms)
  voiceEndTimeout: number // 语音结束超时 (ms)
}

// 获取 WebSocket URL
function getDefaultWSUrl(): string {
  // 优先使用环境变量
  const envUrl = import.meta.env.VITE_WS_URL
  if (envUrl) {
    return envUrl
  }
  // 回退到基于当前页面地址的 URL
  return `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`
}

// 默认配置
export const defaultWSConfig: WSConfig = {
  url: getDefaultWSUrl(),
  reconnectInterval: 3000,
  maxReconnectAttempts: 5,
  heartbeatInterval: 10000, // 10秒
  preReconnectTime: 240000, // 240秒
  voiceEndTimeout: 600, // 600ms - 放宽超时以应对移动端 JS 主线程卡顿
}

// DraARLv1 数据包
export interface DraARLPacket {
  version: string
  length: number
  username: string
  devicePassword: string
  type: number
  devModel: number
  ssid: number
  dmrid: number
  callsign: string
  reserved: Uint8Array
  data: Uint8Array
}

// 通信统计
export interface RadioStats {
  onlineDevices: number
  ghostDevices: number
  totalDevices: number
  messagesReceived: number
  messagesSent: number
  voiceTimeReceived: number // ms
  voiceTimeSent: number // ms
}

// 用户配置
export interface RadioUserConfig {
  defaultGroupId: number
  inputDeviceId?: string
  outputDeviceId?: string
  volume: number // 0-1
  muted: boolean
}

// 默认用户配置
export const defaultRadioUserConfig: RadioUserConfig = {
  defaultGroupId: 999, // 公共群组
  volume: 0.8,
  muted: false,
}

// IndexedDB 消息缓存结构
export interface CachedMessage {
  id: string
  groupId: number
  callsign: string
  ssid: number
  nickname?: string
  avatar?: string
  type: 'voice' | 'text'
  content: Blob | string
  duration?: number
  timestamp: number
  isSelf: boolean
  isPlayed: boolean
}
