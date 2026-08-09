/**
 * 消息同步服务
 * - 初始加载最新 20 条消息
 * - 向上滚动时加载更多历史消息
 * - 每 15 秒同步最新消息
 */

import { apiClient } from '../api'
import type { RadioMessage } from '../../types/radio'

// 每次获取的消息数量
const PAGE_SIZE = 20

// 后端通信记录响应类型
interface CommRecordResponse {
  id: number
  device_id: number
  device_name: string
  dev_model: number
  group_id: number | null
  group_name: string
  user_id: number | null
  username: string       // 登录用户名（用于查询头像）
  nickname: string       // 用户昵称（用于显示）
  start_time: string
  end_time: string
  duration_ms: number
  audio_url?: string
  audio_size: number
  status: number
  msg_type: number  // 0=音频, 1=文本
  text_content: string
}

// API 响应类型
interface CommRecordsApiResponse {
  code: number
  message: string
  data: {
    list: CommRecordResponse[]
    total: number
    page: number
    page_size: number
  }
}

// 当前用户信息（用于判断 isSelf）
interface CurrentUser {
  callsign: string
  ssid: number
  username: string  // 添加 username 用于精确匹配
}

/**
 * 将后端通信记录转换为前端 RadioMessage 格式
 */
function toRadioMessage(record: CommRecordResponse, currentUser?: CurrentUser): RadioMessage {
  // 解析设备名称（格式：BH5UVN-安卓客户端 或 BH5UVN-105）
  const [callsign] = record.device_name.split('-')

  // 对于幽灵设备（device_id=0），SSID 直接使用 dev_model（100-105）
  // 对于物理设备，需要从 device_name 解析
  let ssid = 0
  if (record.device_id === 0 && record.dev_model >= 100 && record.dev_model <= 105) {
    ssid = record.dev_model
  } else {
    // 物理设备：从 device_name 解析 SSID
    const parts = record.device_name.split('-')
    if (parts.length > 1) {
      ssid = parseInt(parts[1]) || 0
    }
  }

  // 判断消息类型
  const isText = record.msg_type === 1

  // 判断是否是自己发送的消息
  // 三重匹配：username + callsign + ssid(105) 都匹配才是自己
  let isSelf = false
  if (currentUser?.username && currentUser?.callsign) {
    const usernameMatch = record.username?.toLowerCase() === currentUser.username.toLowerCase()
    const callsignMatch = callsign.toUpperCase() === currentUser.callsign.toUpperCase()
    const ssidMatch = ssid === 105  // 网页客户端固定 SSID=105
    isSelf = usernameMatch && callsignMatch && ssidMatch
  }

  return {
    id: `db-${record.id}`,
    type: isText ? 'text' : 'voice',
    groupId: record.group_id || 0,
    groupName: record.group_name || undefined,
    senderId: record.device_id || `db-${record.id}`,
    senderCallsign: callsign || 'Unknown',
    senderSSID: ssid,
    senderUsername: record.username || undefined,
    senderNickname: record.nickname || undefined,
    content: isText ? record.text_content : (record.audio_url || ''),
    duration: record.duration_ms,
    timestamp: new Date(record.start_time).getTime(),
    isSelf,
    isPlayed: true,
  }
}

/**
 * 消息同步服务类
 */
class MessageSyncService {
  // 每个群组的分页状态
  private pageStates: Map<number, {
    currentPage: number
    totalCount: number
    hasMore: boolean
  }> = new Map()

  /**
   * 初始化或重置群组的分页状态
   */
  private initGroupState(groupId: number) {
    if (!this.pageStates.has(groupId)) {
      this.pageStates.set(groupId, {
        currentPage: 0,
        totalCount: 0,
        hasMore: true,
      })
    }
  }

  /**
   * 获取群组的分页状态
   */
  getGroupState(groupId: number) {
    this.initGroupState(groupId)
    return this.pageStates.get(groupId)!
  }

  /**
   * 判断是否还有更多消息可加载
   */
  hasMore(groupId: number): boolean {
    const state = this.pageStates.get(groupId)
    return state?.hasMore ?? true
  }

  /**
   * 获取最新消息（第一页）
   * 用于初始加载和定时同步
   */
  async fetchLatestMessages(groupId: number, currentUser?: CurrentUser): Promise<{
    messages: RadioMessage[]
    total: number
  }> {
    this.initGroupState(groupId)

    try {
      const response = await apiClient.get<CommRecordsApiResponse>(
        `/api/comm-records?group_id=${groupId}&page=1&page_size=${PAGE_SIZE}`
      )

      if (response.code !== 200 || !response.data?.list) {
        console.warn('[MessageSync] API returned error:', response.message)
        return { messages: [], total: 0 }
      }

      const messages = response.data.list
        .map(record => toRadioMessage(record, currentUser))
        .sort((a, b) => a.timestamp - b.timestamp)

      // 更新分页状态
      const total = response.data.total
      this.pageStates.set(groupId, {
        currentPage: 1,
        totalCount: total,
        hasMore: total > PAGE_SIZE,
      })

      return { messages, total }
    } catch (error) {
      console.error('[MessageSync] Failed to fetch latest messages:', error)
      return { messages: [], total: 0 }
    }
  }

  /**
   * 加载更多历史消息
   * 用于向上滚动时加载
   */
  async loadMoreMessages(groupId: number, currentUser?: CurrentUser): Promise<RadioMessage[]> {
    const state = this.getGroupState(groupId)

    if (!state.hasMore) {
      return []
    }

    const nextPage = state.currentPage + 1

    try {
      const response = await apiClient.get<CommRecordsApiResponse>(
        `/api/comm-records?group_id=${groupId}&page=${nextPage}&page_size=${PAGE_SIZE}`
      )

      if (response.code !== 200 || !response.data?.list) {
        console.warn('[MessageSync] API returned error:', response.message)
        return []
      }

      const messages = response.data.list
        .map(record => toRadioMessage(record, currentUser))
        .sort((a, b) => a.timestamp - b.timestamp)

      // 更新分页状态
      const newTotal = response.data.total
      const loadedCount = response.data.list.length
      const totalLoaded = nextPage * PAGE_SIZE + loadedCount

      this.pageStates.set(groupId, {
        currentPage: nextPage,
        totalCount: newTotal,
        hasMore: totalLoaded < newTotal,
      })

      return messages
    } catch (error) {
      console.error('[MessageSync] Failed to load more messages:', error)
      return []
    }
  }

  /**
   * 同步最新消息（用于定时同步）
   * 只获取第一页，与内存中的实时消息合并
   */
  async syncMessages(
    groupId: number,
    currentMessages: RadioMessage[],
    currentUser?: CurrentUser
  ): Promise<RadioMessage[]> {
    // 获取最新消息（第一页）
    const { messages: latestDbMessages } = await this.fetchLatestMessages(groupId, currentUser)

    if (latestDbMessages.length === 0) {
      return currentMessages
    }

    // 斩杀线 = 数据库最新消息时间
    const cutoffTime = latestDbMessages[latestDbMessages.length - 1].timestamp

    // 过滤实时消息：只保留 > 斩杀线 的
    const newRealtime = currentMessages.filter(m => m.timestamp > cutoffTime)

    // 去重：移除实时消息中已存在于数据库的
    const dbIds = new Set(latestDbMessages.map(m => m.id))
    const uniqueRealtime = newRealtime.filter(m => {
      if (m.id.startsWith('db-')) {
        return !dbIds.has(m.id)
      }
      return true
    })

    // 合并并按时间排序
    return [...latestDbMessages, ...uniqueRealtime].sort((a, b) => a.timestamp - b.timestamp)
  }

  /**
   * 重置群组状态（切换群组时调用）
   */
  resetGroupState(groupId: number) {
    this.pageStates.delete(groupId)
  }

  /**
   * 获取群组总消息数
   */
  getTotalCount(groupId: number): number {
    const state = this.pageStates.get(groupId)
    return state?.totalCount ?? 0
  }
}

// 导出单例
export const messageSyncService = new MessageSyncService()
