/**
 * 消息缓存服务
 * 使用 IndexedDB 存储聊天消息
 */

import type { CachedMessage } from '../../types/radio'

const DB_NAME = 'nrllink-radio'
const DB_VERSION = 1
const STORE_NAME = 'messages'

// 配置
const MAX_MESSAGES_PER_GROUP = 500
const MAX_STORAGE_DAYS = 7
const MAX_TOTAL_STORAGE = 100 * 1024 * 1024 // 100MB

class MessageCacheDB {
  private db: IDBDatabase | null = null
  private initPromise: Promise<IDBDatabase> | null = null

  /**
   * 初始化数据库
   */
  async init(): Promise<IDBDatabase> {
    // 如果已经初始化，直接返回
    if (this.db) return this.db

    // 如果正在初始化，等待完成
    if (this.initPromise) return this.initPromise

    this.initPromise = new Promise((resolve, reject) => {
      const request = indexedDB.open(DB_NAME, DB_VERSION)

      request.onerror = () => {
        console.error('[MessageCache] Failed to open database:', request.error)
        reject(request.error)
      }

      request.onsuccess = () => {
        this.db = request.result
        console.log('[MessageCache] Database opened')
        resolve(this.db)
      }

      request.onupgradeneeded = (event) => {
        const db = (event.target as IDBOpenDBRequest).result

        // 创建消息存储
        if (!db.objectStoreNames.contains(STORE_NAME)) {
          const store = db.createObjectStore(STORE_NAME, { keyPath: 'id' })

          // 创建索引
          store.createIndex('groupId', 'groupId', { unique: false })
          store.createIndex('timestamp', 'timestamp', { unique: false })
          store.createIndex('callsign', 'callsign', { unique: false })
          store.createIndex('groupId_timestamp', ['groupId', 'timestamp'], { unique: false })
        }
      }
    })

    return this.initPromise
  }

  /**
   * 添加消息
   */
  async addMessage(message: CachedMessage): Promise<void> {
    const db = await this.init()

    return new Promise((resolve, reject) => {
      const transaction = db.transaction([STORE_NAME], 'readwrite')
      const store = transaction.objectStore(STORE_NAME)

      const request = store.add(message)

      request.onsuccess = () => {
        // 检查是否需要清理旧消息
        this.cleanupIfNeeded(message.groupId)
        resolve()
      }

      request.onerror = () => {
        console.error('[MessageCache] Failed to add message:', request.error)
        reject(request.error)
      }
    })
  }

  /**
   * 获取群组消息
   */
  async getMessagesByGroup(groupId: number, limit: number = 100): Promise<CachedMessage[]> {
    const db = await this.init()

    return new Promise((resolve, reject) => {
      const transaction = db.transaction([STORE_NAME], 'readonly')
      const store = transaction.objectStore(STORE_NAME)
      const index = store.index('groupId_timestamp')

      // 使用游标从最新的消息开始获取
      const range = IDBKeyRange.bound([groupId, 0], [groupId, Date.now()])
      const request = index.openCursor(range, 'prev')

      const messages: CachedMessage[] = []

      request.onsuccess = (event) => {
        const cursor = (event.target as IDBRequest).result
        if (cursor && messages.length < limit) {
          messages.push(cursor.value)
          cursor.continue()
        } else {
          // 反转数组，使消息按时间正序排列
          resolve(messages.reverse())
        }
      }

      request.onerror = () => {
        console.error('[MessageCache] Failed to get messages:', request.error)
        reject(request.error)
      }
    })
  }

  /**
   * 获取最近的消息
   */
  async getRecentMessages(limit: number = 50): Promise<CachedMessage[]> {
    const db = await this.init()

    return new Promise((resolve, reject) => {
      const transaction = db.transaction([STORE_NAME], 'readonly')
      const store = transaction.objectStore(STORE_NAME)
      const index = store.index('timestamp')

      const request = index.openCursor(null, 'prev')

      const messages: CachedMessage[] = []

      request.onsuccess = (event) => {
        const cursor = (event.target as IDBRequest).result
        if (cursor && messages.length < limit) {
          messages.push(cursor.value)
          cursor.continue()
        } else {
          resolve(messages.reverse())
        }
      }

      request.onerror = () => {
        console.error('[MessageCache] Failed to get recent messages:', request.error)
        reject(request.error)
      }
    })
  }

  /**
   * 删除消息
   */
  async deleteMessage(id: string): Promise<void> {
    const db = await this.init()

    return new Promise((resolve, reject) => {
      const transaction = db.transaction([STORE_NAME], 'readwrite')
      const store = transaction.objectStore(STORE_NAME)

      const request = store.delete(id)

      request.onsuccess = () => resolve()
      request.onerror = () => reject(request.error)
    })
  }

  /**
   * 清空群组消息
   */
  async clearGroupMessages(groupId: number): Promise<void> {
    const db = await this.init()

    return new Promise((resolve, reject) => {
      const transaction = db.transaction([STORE_NAME], 'readwrite')
      const store = transaction.objectStore(STORE_NAME)
      const index = store.index('groupId')

      const range = IDBKeyRange.only(groupId)
      const request = index.openCursor(range)

      request.onsuccess = (event) => {
        const cursor = (event.target as IDBRequest).result
        if (cursor) {
          cursor.delete()
          cursor.continue()
        } else {
          resolve()
        }
      }

      request.onerror = () => reject(request.error)
    })
  }

  /**
   * 清空所有消息
   */
  async clearAllMessages(): Promise<void> {
    const db = await this.init()

    return new Promise((resolve, reject) => {
      const transaction = db.transaction([STORE_NAME], 'readwrite')
      const store = transaction.objectStore(STORE_NAME)

      const request = store.clear()

      request.onsuccess = () => resolve()
      request.onerror = () => reject(request.error)
    })
  }

  /**
   * 获取存储统计
   */
  async getStorageStats(): Promise<{ count: number; size: number }> {
    const db = await this.init()

    return new Promise((resolve, reject) => {
      const transaction = db.transaction([STORE_NAME], 'readonly')
      const store = transaction.objectStore(STORE_NAME)

      const countRequest = store.count()

      countRequest.onsuccess = () => {
        const count = countRequest.result

        // 估算存储大小（不准确，但可用于监控）
        // 实际大小需要遍历所有记录计算
        const estimatedSize = count * 1024 // 假设平均每条消息 1KB

        resolve({ count, size: estimatedSize })
      }

      countRequest.onerror = () => reject(countRequest.error)
    })
  }

  /**
   * 清理旧消息
   */
  private async cleanupIfNeeded(groupId: number): Promise<void> {
    try {
      const db = await this.init()

      // 1. 检查并删除过期消息
      const cutoffTime = Date.now() - MAX_STORAGE_DAYS * 24 * 60 * 60 * 1000

      const transaction = db.transaction([STORE_NAME], 'readwrite')
      const store = transaction.objectStore(STORE_NAME)
      const timestampIndex = store.index('timestamp')

      const range = IDBKeyRange.upperBound(cutoffTime)
      const cursorRequest = timestampIndex.openCursor(range)

      cursorRequest.onsuccess = (event) => {
        const cursor = (event.target as IDBRequest).result
        if (cursor) {
          cursor.delete()
          cursor.continue()
        }
      }

      // 2. 检查群组消息数量
      const groupIndex = store.index('groupId')
      const countRequest = groupIndex.count(IDBKeyRange.only(groupId))

      countRequest.onsuccess = () => {
        if (countRequest.result > MAX_MESSAGES_PER_GROUP) {
          // 删除最旧的消息
          this.deleteOldestMessages(groupId, countRequest.result - MAX_MESSAGES_PER_GROUP)
        }
      }

    } catch (error) {
      console.error('[MessageCache] Cleanup failed:', error)
    }
  }

  /**
   * 删除最旧的消息
   */
  private async deleteOldestMessages(groupId: number, count: number): Promise<void> {
    const db = await this.init()

    const transaction = db.transaction([STORE_NAME], 'readwrite')
    const store = transaction.objectStore(STORE_NAME)
    const index = store.index('groupId_timestamp')

    const range = IDBKeyRange.bound([groupId, 0], [groupId, Date.now()])
    const cursorRequest = index.openCursor(range)

    let deleted = 0

    cursorRequest.onsuccess = (event) => {
      const cursor = (event.target as IDBRequest).result
      if (cursor && deleted < count) {
        cursor.delete()
        deleted++
        cursor.continue()
      }
    }
  }

  /**
   * 标记消息为已播放
   */
  async markAsPlayed(id: string): Promise<void> {
    const db = await this.init()

    return new Promise((resolve, reject) => {
      const transaction = db.transaction([STORE_NAME], 'readwrite')
      const store = transaction.objectStore(STORE_NAME)

      const getRequest = store.get(id)

      getRequest.onsuccess = () => {
        const message = getRequest.result
        if (message) {
          message.isPlayed = true
          store.put(message)
        }
        resolve()
      }

      getRequest.onerror = () => reject(getRequest.error)
    })
  }
}

// 导出单例
export const messageCache = new MessageCacheDB()

// 消息序列号（用于确保同一毫秒内的消息 ID 唯一）
let messageSequence = 0

// 辅助函数：生成消息 ID
export function generateMessageId(groupId: number, timestamp: number, callsign: string): string {
  const seq = messageSequence++
  return `${groupId}_${timestamp}_${callsign}_${seq}`
}

// 辅助函数：将 RadioMessage 转换为 CachedMessage
export function toCachedMessage(msg: any): CachedMessage {
  return {
    id: msg.id || generateMessageId(msg.groupId, msg.timestamp, msg.senderCallsign),
    groupId: msg.groupId,
    callsign: msg.senderCallsign,
    ssid: msg.senderSSID,
    nickname: msg.senderNickname,
    avatar: msg.senderAvatar,
    type: msg.type,
    content: msg.content,
    duration: msg.duration,
    timestamp: msg.timestamp,
    isSelf: msg.isSelf,
    isPlayed: msg.isPlayed || false,
  }
}

// 辅助函数：将 CachedMessage 转换为 RadioMessage
export function toRadioMessage(cached: CachedMessage): any {
  return {
    id: cached.id,
    type: cached.type,
    groupId: cached.groupId,
    senderCallsign: cached.callsign,
    senderSSID: cached.ssid,
    senderNickname: cached.nickname,
    senderAvatar: cached.avatar,
    content: cached.content,
    duration: cached.duration,
    timestamp: cached.timestamp,
    isSelf: cached.isSelf,
    isPlayed: cached.isPlayed,
  }
}
