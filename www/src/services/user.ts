import { apiClient } from './api'
import type {
  User,
  ListResponse,
} from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export const userService = {
  // 获取用户列表
  async getList(params?: {
    page?: number
    page_size?: number
    keyword?: string
    role?: string
  }): Promise<ListResponse<User>> {
    const res = await apiClient.get<BackendResponse<ListResponse<User>>>('/api/users', { params })
    return res.data || { items: [], total: 0, page: 1, page_size: 10 }
  },

  // 获取用户详情
  async get(id: number): Promise<User> {
    const res = await apiClient.get<BackendResponse<User>>(`/api/users/${id}`)
    return res.data!
  },

  // 创建用户
  async create(data: Partial<User>): Promise<User> {
    const res = await apiClient.post<BackendResponse<User>>('/api/users', data)
    return res.data!
  },

  // 更新用户
  async update(id: number, data: Partial<User>): Promise<User> {
    const res = await apiClient.put<BackendResponse<User>>(`/api/users/${id}`, data)
    return res.data!
  },

  // 删除用户
  async delete(id: number): Promise<void> {
    await apiClient.delete<BackendResponse<unknown>>(`/api/users/${id}`)
  },

  // 修改密码
  async changePassword(id: number, data: { old_password: string; new_password: string }): Promise<void> {
    await apiClient.put<BackendResponse<unknown>>(`/api/users/${id}/password`, data)
  },

  // 更新用户状态（禁用/启用）
  async updateStatus(id: number, status: number): Promise<void> {
    await apiClient.put<BackendResponse<unknown>>(`/api/users/${id}/status`, { status })
  },
}
