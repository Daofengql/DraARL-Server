/**
 * 群组管理服务
 */

import { apiClient } from '../api'
import type { RadioGroup } from '../../types/radio'

export interface GroupInfo {
  id: number
  name: string
  callsign?: string
  type: number
  status: number
  ownerId?: number
  note?: string
  createTime?: string
  updateTime?: string
}

export interface GroupWithOnline extends GroupInfo {
  onlineCount: number
  deviceCount: number
}

class GroupManagerService {
  private groupCache: Map<number, GroupWithOnline> = new Map()
  private cacheTime: number = 0
  private cacheTimeout: number = 10000 // 10秒缓存

  /**
   * 获取群组列表
   */
  async getGroups(): Promise<GroupWithOnline[]> {
    // 检查缓存
    if (Date.now() - this.cacheTime < this.cacheTimeout && this.groupCache.size > 0) {
      return Array.from(this.groupCache.values())
    }

    try {
      const response = await apiClient.get<any>('/api/groups')
      const groups = response.data || []

      // 更新缓存
      this.groupCache.clear()
      groups.forEach((group: GroupWithOnline) => {
        this.groupCache.set(group.id, group)
      })
      this.cacheTime = Date.now()

      return groups
    } catch (error) {
      console.error('[GroupManager] Failed to get groups:', error)
      // 返回缓存的数据
      return Array.from(this.groupCache.values())
    }
  }

  /**
   * 获取单个群组
   */
  async getGroup(groupId: number): Promise<GroupWithOnline | null> {
    // 先检查缓存
    const cached = this.groupCache.get(groupId)
    if (cached && Date.now() - this.cacheTime < this.cacheTimeout) {
      return cached
    }

    try {
      const response = await apiClient.get<any>(`/api/groups/${groupId}`)
      const group = response.data

      if (group) {
        this.groupCache.set(groupId, group)
      }

      return group || null
    } catch (error) {
      console.error('[GroupManager] Failed to get group:', error)
      return this.groupCache.get(groupId) || null
    }
  }

  /**
   * 获取群组在线设备数
   */
  async getGroupOnlineCount(groupId: number): Promise<number> {
    try {
      const response = await apiClient.get<any>(`/api/groups/${groupId}/devices`)
      const devices = response.data || []
      return devices.filter((d: any) => d.isOnline).length
    } catch (error) {
      console.error('[GroupManager] Failed to get online count:', error)
      return 0
    }
  }

  /**
   * 获取群组设备列表
   */
  async getGroupDevices(groupId: number): Promise<any[]> {
    try {
      const response = await apiClient.get<any>(`/api/groups/${groupId}/devices`)
      return response.data || []
    } catch (error) {
      console.error('[GroupManager] Failed to get group devices:', error)
      return []
    }
  }

  /**
   * 加入群组
   */
  async joinGroup(groupId: number): Promise<boolean> {
    try {
      await apiClient.post(`/api/groups/${groupId}/join`)
      // 清除缓存以刷新数据
      this.invalidateCache()
      return true
    } catch (error) {
      console.error('[GroupManager] Failed to join group:', error)
      return false
    }
  }

  /**
   * 离开群组
   */
  async leaveGroup(groupId: number): Promise<boolean> {
    try {
      await apiClient.post(`/api/groups/${groupId}/leave`)
      this.invalidateCache()
      return true
    } catch (error) {
      console.error('[GroupManager] Failed to leave group:', error)
      return false
    }
  }

  /**
   * 搜索群组
   */
  async searchGroups(query: string): Promise<GroupWithOnline[]> {
    try {
      const response = await apiClient.post<any>('/api/groups/search', { query })
      return response.data || []
    } catch (error) {
      console.error('[GroupManager] Failed to search groups:', error)
      return []
    }
  }

  /**
   * 清除缓存
   */
  invalidateCache(): void {
    this.cacheTime = 0
    this.groupCache.clear()
  }

  /**
   * 获取缓存的群组（同步方法）
   */
  getCachedGroup(groupId: number): GroupWithOnline | undefined {
    return this.groupCache.get(groupId)
  }

  /**
   * 获取所有缓存的群组（同步方法）
   */
  getCachedGroups(): GroupWithOnline[] {
    return Array.from(this.groupCache.values())
  }

  /**
   * 更新群组在线设备数（本地更新）
   */
  updateOnlineCount(groupId: number, count: number): void {
    const group = this.groupCache.get(groupId)
    if (group) {
      group.onlineCount = count
    }
  }
}

// 导出单例
export const groupManagerService = new GroupManagerService()

// 转换为 RadioGroup 格式
export function toRadioGroup(group: GroupWithOnline): RadioGroup {
  return {
    id: group.id,
    name: group.name,
    callsign: group.callsign,
    type: group.type,
    status: group.status,
    onlineCount: group.onlineCount,
    isDefault: group.id === 999, // 公共群组为默认
  }
}
