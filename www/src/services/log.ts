import { apiClient } from './api'
import type {
  OperatorLog,
  LogStats,
  ListResponse,
} from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export const logService = {
  // 获取操作日志列表
  async getList(params?: {
    page?: number
    page_size?: number
    event_type?: string
    user_id?: number
    start_time?: string
    end_time?: string
  }): Promise<ListResponse<OperatorLog>> {
    const res = await apiClient.get<BackendResponse<ListResponse<OperatorLog>>>('/api/operatorlog/list', { params })
    return res.data || { items: [], total: 0, page: 1, page_size: 10 }
  },

  // 获取日志统计
  async getStats(): Promise<LogStats> {
    const res = await apiClient.get<BackendResponse<LogStats>>('/api/operatorlog/stats')
    return res.data || { total: 0, today: 0 }
  },
}
