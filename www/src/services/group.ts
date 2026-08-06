import { apiClient } from './api'
import type {
  Group,
  Device,
  GroupMember,
  ListResponse,
} from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

// 后端群组响应格式
interface BackendGroup {
  id: number
  name: string
  type: number
  password?: string
  ower_id: number
  ower_callsign: string
  devlist: string
  master_server: number
  slave_server: number
  status: number
  create_time: string
  update_time: string
  note: string
  // 扩展字段
  is_joined?: boolean
  is_owner?: boolean
  online_count?: number
  total_count?: number
  require_password?: boolean
}

// 标准化群组数据
const normalizeGroup = (g: BackendGroup): Group => ({
  id: g.id,
  name: g.name,
  type: g.type,
  password: g.password ?? '',
  ower_id: g.ower_id,
  ower_callsign: g.ower_callsign,
  devlist: g.devlist,
  master_server: g.master_server,
  slave_server: g.slave_server,
  status: g.status,
  note: g.note,
  is_joined: g.is_joined,
  is_owner: g.is_owner,
  online_count: g.online_count,
  total_count: g.total_count,
  require_password: g.require_password,
  create_time: g.create_time,
  created_at: g.create_time, // 前端兼容
  update_time: g.update_time,
  updated_at: g.update_time, // 前端兼容
})

export const groupService = {
  // 获取群组列表
  async getList(params?: {
    page?: number
    page_size?: number
    keyword?: string
  }): Promise<ListResponse<Group>> {
    const res = await apiClient.get<BackendResponse<{ items: BackendGroup[]; total?: number }>>('/api/groups', { params })
    const items = (res.data?.items || []).map(normalizeGroup)
    return { items, total: res.data?.total ?? items.length, page: params?.page || 1, page_size: params?.page_size || 20 }
  },

  // 管理员获取所有非虚拟群组（包含其他用户的私有群组）
  async getAdminList(params?: {
    page?: number
    page_size?: number
    keyword?: string
  }): Promise<ListResponse<Group>> {
    const res = await apiClient.get<BackendResponse<{ items: BackendGroup[]; total?: number }>>('/api/admin/groups', { params })
    const items = (res.data?.items || []).map(normalizeGroup)
    return { items, total: res.data?.total ?? items.length, page: params?.page || 1, page_size: params?.page_size || 100 }
  },

  // 自动翻页拉取完整群组集合，供下拉框和后台引用数据使用。
  async listAll(options?: { admin?: boolean; keyword?: string }): Promise<Group[]> {
    const pageSize = 100
    const result: Group[] = []
    let page = 1
    let total = Number.POSITIVE_INFINITY

    while (result.length < total) {
      const current = options?.admin
        ? await this.getAdminList({ page, page_size: pageSize, keyword: options.keyword })
        : await this.getList({ page, page_size: pageSize, keyword: options?.keyword })
      result.push(...current.items)
      total = current.total
      if (current.items.length === 0 || current.items.length < pageSize) break
      page += 1
    }
    return result
  },

  // 获取群组列表（兼容旧接口）
  async list(): Promise<Group[]> {
    return this.listAll()
  },

  // 获取群组详情
  async get(id: number): Promise<Group> {
    const res = await apiClient.get<BackendResponse<BackendGroup>>(`/api/groups/${id}`)
    return normalizeGroup(res.data!)
  },

  // 获取群组设备
  async getDevices(id: number): Promise<Device[]> {
    const res = await apiClient.get<BackendResponse<{ items: Device[] }>>(`/api/groups/${id}/devices`)
    return res.data?.items || []
  },

  // 创建群组
  async create(data: Partial<Group>): Promise<Group> {
    const backendData: Partial<BackendGroup> = {
      id: data.id,
      name: data.name,
      type: data.type,
      password: data.password,
      ower_id: data.ower_id,
      ower_callsign: data.ower_callsign,
      devlist: data.devlist,
      master_server: data.master_server,
      slave_server: data.slave_server,
      status: data.status,
      note: data.note,
      create_time: data.created_at ?? data.create_time ?? new Date().toISOString(),
      update_time: data.updated_at ?? data.update_time ?? new Date().toISOString(),
    }
    const res = await apiClient.post<BackendResponse<BackendGroup>>('/api/groups', backendData)
    return normalizeGroup(res.data!)
  },

  // 更新群组
  async update(id: number, data: Partial<Group>): Promise<Group> {
    const backendData: Partial<BackendGroup> = {
      id: data.id,
      name: data.name,
      type: data.type,
      password: data.password,
      ower_id: data.ower_id,
      ower_callsign: data.ower_callsign,
      devlist: data.devlist,
      master_server: data.master_server,
      slave_server: data.slave_server,
      status: data.status,
      note: data.note,
      create_time: data.created_at ?? data.create_time,
      update_time: data.updated_at ?? data.update_time,
    }
    const res = await apiClient.put<BackendResponse<BackendGroup>>(`/api/groups/${id}`, backendData)
    return normalizeGroup(res.data!)
  },

  // 删除群组
  async delete(id: number): Promise<void> {
    await apiClient.delete<BackendResponse<unknown>>(`/api/groups/${id}`)
  },

  // 搜索群组（支持私有群组）
  async search(params: {
    keyword: string
    page?: number
    page_size?: number
  }): Promise<ListResponse<Group>> {
    const res = await apiClient.post<BackendResponse<{ items: BackendGroup[]; total: number }>>('/api/groups/search', params)
    const items = (res.data?.items || []).map(normalizeGroup)
    return { items, total: res.data?.total || items.length, page: params.page || 1, page_size: params.page_size || 10 }
  },

  // 加入群组（验证密码）
  async join(id: number, password: string): Promise<{
    group_id: number
    is_verified: boolean
    join_time: string
  }> {
    const res = await apiClient.post<BackendResponse<{
      group_id: number
      is_verified: boolean
      join_time: string
    }>>(`/api/groups/${id}/join`, { password })
    return res.data!
  },

  // 获取群组成员列表
  async getMembers(id: number): Promise<ListResponse<GroupMember>> {
    const res = await apiClient.get<BackendResponse<{ items: GroupMember[]; total: number }>>(`/api/groups/${id}/members`)
    return {
      items: res.data?.items || [],
      total: res.data?.total || 0,
      page: 1,
      page_size: 10
    }
  },

  async removeMember(groupId: number, userId: number): Promise<{
    group_id: number
    user_id: number
    moved_device_count: number
  }> {
    const res = await apiClient.delete<BackendResponse<{
      group_id: number
      user_id: number
      moved_device_count: number
    }>>(`/api/groups/${groupId}/members/${userId}`)
    return res.data!
  },

  // 踢出设备
  async kickDevice(groupId: number, deviceId: number): Promise<void> {
    await apiClient.delete<BackendResponse<unknown>>(`/api/groups/${groupId}/devices/${deviceId}`)
  },

  async updateDeviceCommControl(
    groupId: number,
    deviceId: number,
    data: { disable_send?: boolean; disable_recv?: boolean },
  ): Promise<{ device_id: number; group_id: number; disable_send: boolean; disable_recv: boolean }> {
    const res = await apiClient.put<BackendResponse<{
      device_id: number
      group_id: number
      disable_send: boolean
      disable_recv: boolean
    }>>(`/api/groups/${groupId}/devices/${deviceId}/comm-control`, data)
    return res.data!
  },

  // 离开群组
  async leave(id: number): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>(`/api/groups/${id}/leave`, {})
  },
}
