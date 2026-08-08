import { apiClient } from '../../services/api'
import type { LogbookEntry, LogbookListResponse, LogbookResponse } from './types'
import { buildLogbookListURL, logbookBatchDeletePath, type LogbookListParams } from './apiQuery'

export { buildLogbookListURL, type LogbookListParams } from './apiQuery'

// API 调用函数
export const logbookApi = {
  // 获取列表
  getList: async (params: LogbookListParams, isAdmin: boolean = false): Promise<LogbookListResponse> => {
    const response = await apiClient.get(buildLogbookListURL(params, isAdmin))
    return response
  },

  // 获取单条
  getOne: async (id: number, isAdmin: boolean = false): Promise<LogbookResponse> => {
    const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
    const response = await apiClient.get(`${basePath}/${id}`)
    return response
  },

  // 创建
  create: async (data: Omit<LogbookEntry, 'id'>): Promise<LogbookResponse> => {
    const response = await apiClient.post('/api/logbooks', data)
    return response
  },

  // 更新
  update: async (id: number, data: Partial<LogbookEntry>, isAdmin: boolean = false): Promise<LogbookResponse> => {
    const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
    const response = await apiClient.put(`${basePath}/${id}`, data)
    return response
  },

  // 删除单条
  delete: async (id: number, isAdmin: boolean = false): Promise<{ code: number; message: string }> => {
    const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
    const response = await apiClient.delete(`${basePath}/${id}`)
    return response
  },

  // 批量删除
  batchDelete: async (ids: number[], isAdmin: boolean = false): Promise<{ code: number; message: string }> => {
    const response = await apiClient.delete(logbookBatchDeletePath(isAdmin), { data: { ids } })
    return response
  },
}
