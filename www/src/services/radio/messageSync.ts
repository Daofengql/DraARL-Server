/**
 * 频道消息同步服务。
 * 使用稳定游标读取完整频道历史，实时消息只保留在服务端尚未落库的尾部。
 */

import { apiClient } from '../api'
import type { RadioMessage } from '../../types/radio'

const PAGE_SIZE = 20

interface MessageSenderResponse {
  user_id: number | null
  username: string
  callsign: string
  nickname: string
  ssid: number
  dev_model: number
  is_ghost: boolean
}

interface ChannelMessageResponse {
  id: number
  message_type: 'voice' | 'text'
  source_group_id: number
  source_group_name: string
  requested_group_id: number
  sender: MessageSenderResponse
  sent_at: string
  end_time?: string
  duration_ms: number
  text_content?: string
  audio_url?: string
  audio_size?: number
  audio_format?: string
  status: number
}

interface ChannelMessagesApiResponse {
  code: number
  message: string
  data: {
    messages: ChannelMessageResponse[]
    next_cursor: string
    has_more: boolean
    server_time: string
  }
}

interface CurrentUser {
  id?: number
  callsign: string
  ssid: number
  username: string
}

interface PageState {
  nextCursor: string
  hasMore: boolean
}

function toRadioMessage(record: ChannelMessageResponse, currentUser?: CurrentUser): RadioMessage {
  const sender = record.sender
  const sameUserId = currentUser?.id != null && sender.user_id === currentUser.id
  const sameIdentity = Boolean(currentUser?.username && currentUser?.callsign) &&
    sender.username.toLowerCase() === currentUser!.username.toLowerCase() &&
    sender.callsign.toUpperCase() === currentUser!.callsign.toUpperCase() &&
    sender.ssid === currentUser!.ssid

  return {
    id: `db-${record.id}`,
    type: record.message_type,
    groupId: record.source_group_id,
    requestedGroupId: record.requested_group_id,
    groupName: record.source_group_name || undefined,
    senderId: sender.user_id ?? `${sender.username}-${sender.ssid}`,
    senderCallsign: sender.callsign || 'Unknown',
    senderSSID: sender.ssid,
    senderUsername: sender.username || undefined,
    senderNickname: sender.nickname || undefined,
    content: record.message_type === 'text' ? (record.text_content || '') : (record.audio_url || ''),
    duration: record.duration_ms,
    timestamp: new Date(record.sent_at).getTime(),
    isSelf: sameUserId || sameIdentity,
    isPlayed: true,
  }
}

class MessageSyncService {
  private pageStates = new Map<number, PageState>()

  hasMore(groupId: number): boolean {
    return this.pageStates.get(groupId)?.hasMore ?? true
  }

  private async fetchPage(
    groupId: number,
    currentUser?: CurrentUser,
    cursor?: string,
  ): Promise<{ messages: RadioMessage[]; nextCursor: string; hasMore: boolean }> {
    const params = new URLSearchParams({ limit: String(PAGE_SIZE), message_type: 'all' })
    if (cursor) params.set('cursor', cursor)

    const response = await apiClient.get<ChannelMessagesApiResponse>(
      `/api/groups/${groupId}/messages?${params.toString()}`,
    )
    if (response.code !== 200 || !Array.isArray(response.data?.messages)) {
      throw new Error(response.message || '频道消息加载失败')
    }

    return {
      messages: response.data.messages
        .map(message => toRadioMessage(message, currentUser))
        .sort((left, right) => left.timestamp - right.timestamp),
      nextCursor: response.data.next_cursor || '',
      hasMore: Boolean(response.data.has_more),
    }
  }

  async fetchLatestMessages(groupId: number, currentUser?: CurrentUser): Promise<RadioMessage[]> {
    try {
      const page = await this.fetchPage(groupId, currentUser)
      if (!this.pageStates.has(groupId)) {
        this.pageStates.set(groupId, {
          nextCursor: page.nextCursor,
          hasMore: page.hasMore,
        })
      }
      return page.messages
    } catch (error) {
      console.error('[MessageSync] Failed to fetch latest messages:', error)
      return []
    }
  }

  async loadMoreMessages(groupId: number, currentUser?: CurrentUser): Promise<RadioMessage[]> {
    let state = this.pageStates.get(groupId)
    if (!state) {
      await this.fetchLatestMessages(groupId, currentUser)
      state = this.pageStates.get(groupId)
    }
    if (!state?.hasMore || !state.nextCursor) return []

    try {
      const page = await this.fetchPage(groupId, currentUser, state.nextCursor)
      this.pageStates.set(groupId, {
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
      })
      return page.messages
    } catch (error) {
      console.error('[MessageSync] Failed to load older messages:', error)
      return []
    }
  }

  async syncMessages(
    groupId: number,
    currentMessages: RadioMessage[],
    currentUser?: CurrentUser,
  ): Promise<RadioMessage[]> {
    const latest = await this.fetchLatestMessages(groupId, currentUser)
    if (latest.length === 0) return currentMessages

    const persisted = new Map<string, RadioMessage>()
    for (const message of currentMessages) {
      if (message.id.startsWith('db-')) persisted.set(message.id, message)
    }
    for (const message of latest) persisted.set(message.id, message)

    const newestPersistedTime = Math.max(...latest.map(message => message.timestamp))
    const pendingRealtime = currentMessages.filter(message =>
      !message.id.startsWith('db-') && message.timestamp > newestPersistedTime,
    )

    return [...persisted.values(), ...pendingRealtime]
      .sort((left, right) => left.timestamp - right.timestamp)
  }

  resetGroupState(groupId: number): void {
    this.pageStates.delete(groupId)
  }
}

export const messageSyncService = new MessageSyncService()
