import { apiClient } from './api'
import type { UserCommStats, SystemCommStats, DailyCommStats } from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export const commStatsService = {
  // 获取当前用户的通信统计
  async getUserStats(): Promise<UserCommStats> {
    const res = await apiClient.get<BackendResponse<UserCommStats>>('/api/comm-records/user-stats')
    return res.data || { total_count: 0, total_size: 0, total_duration: 0 }
  },

  // 获取当前用户近30天通信趋势
  async getUserTrend(): Promise<DailyCommStats[]> {
    const res = await apiClient.get<BackendResponse<DailyCommStats[]>>('/api/comm-records/user-trend')
    return res.data || []
  },

  // 获取系统通信统计（管理员）
  async getSystemStats(): Promise<SystemCommStats> {
    const res = await apiClient.get<BackendResponse<SystemCommStats>>('/api/comm-records/system-stats')
    return res.data || { total_count: 0, total_size: 0, total_duration: 0 }
  },

  // 获取系统近30天通信趋势（管理员）
  async getSystemTrend(): Promise<DailyCommStats[]> {
    const res = await apiClient.get<BackendResponse<DailyCommStats[]>>('/api/comm-records/system-trend')
    return res.data || []
  },
}
