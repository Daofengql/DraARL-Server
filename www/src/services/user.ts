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
  // 获取用户列表（管理员专用）
  async getList(params?: {
    page?: number
    page_size?: number
    keyword?: string
    role?: string
  }): Promise<ListResponse<User>> {
    const query = params ? {
      ...params,
      limit: params.page_size,
      page_size: undefined,
    } : undefined
    const res = await apiClient.get<BackendResponse<ListResponse<User>>>('/api/users', { params: query })
    return res.data || { items: [], total: 0, page: 1, page_size: 10 }
  },

  // 自动翻页获取全部用户，供后台列表及用户引用字段使用。
  async listAll(): Promise<User[]> {
    const pageSize = 100
    const result: User[] = []
    let page = 1
    let total = Number.POSITIVE_INFINITY

    while (result.length < total) {
      const current = await this.getList({ page, page_size: pageSize })
      result.push(...current.items)
      total = current.total
      if (current.items.length === 0 || current.items.length < pageSize) break
      page += 1
    }
    return result
  },

  // 管理员按需揭示指定用户的设备准入密码。
  async getDevicePassword(id: number): Promise<{ device_password: string; is_new: boolean }> {
    const res = await apiClient.get<BackendResponse<{ device_password: string; is_new: boolean }>>(
      `/api/admin/users/${id}/device-password`
    )
    return res.data!
  },

  // 获取用户公开信息（任何登录用户可访问）
  async getPublicInfo(id: number): Promise<User> {
    const res = await apiClient.get<{ code: number; message: string; data?: User }>(`/api/users/${id}/public`)
    console.log('getPublicInfo raw response:', res)
    console.log('getPublicInfo data:', res.data)
    return res.data!
  },

  // 通过用户名获取用户公开信息（任何登录用户可访问）
  async getPublicInfoByName(username: string): Promise<User> {
    const res = await apiClient.get<{ code: number; message: string; data?: User }>(`/api/users/name/${encodeURIComponent(username)}/public`)
    return res.data!
  },

  // 获取用户详情
  async get(id: number): Promise<User> {
    const res = await apiClient.get<{ code: number; message: string; data?: User }>(`/api/users/${id}`)
    return res.data!
  },

  // 更新用户
  async update(id: number, data: Partial<User>): Promise<User> {
    const res = await apiClient.put<{ code: number; message: string; data?: User }>(`/api/users/${id}`, data)
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
