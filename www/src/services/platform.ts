import { apiClient } from './api'
import type {
  PlatformInfo,
  PlatformStats,
} from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export const platformService = {
  // 获取平台信息
  async getInfo(): Promise<PlatformInfo> {
    const res = await apiClient.get<BackendResponse<PlatformInfo>>('/api/platform/info')
    return res.data || { name: 'NRLLink', version: '1.0.0' }
  },

  // 获取统计信息
  async getTotalStats(): Promise<PlatformStats> {
    const res = await apiClient.get<BackendResponse<PlatformStats>>('/api/platform/totalstats')
    return res.data || { total_devices: 0, online_devices: 0, total_users: 0, total_groups: 0 }
  },
}
