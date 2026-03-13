import { apiClient } from './api'
import type {
  Group,
  Device,
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
  callsign: string
  password?: string
  allow_callsign_ssid: string
  ower_id: number
  ower_callsign: string
  devlist: string
  master_server: number
  slave_server: number
  status: number
  create_time: string
  update_time: string
  note: string
}

// 标准化群组数据
const normalizeGroup = (g: BackendGroup): Group => ({
  id: g.id,
  name: g.name,
  type: g.type,
  callsign: g.callsign,
  password: g.password ?? '',
  allow_callsign_ssid: g.allow_callsign_ssid,
  ower_id: g.ower_id,
  ower_callsign: g.ower_callsign,
  devlist: g.devlist,
  master_server: g.master_server,
  slave_server: g.slave_server,
  status: g.status,
  note: g.note,
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
    return { items, total: res.data?.total || items.length, page: params?.page || 1, page_size: params?.page_size || 10 }
  },

  // 获取群组列表（兼容旧接口）
  async list(): Promise<Group[]> {
    const res = await apiClient.get<BackendResponse<{ items: BackendGroup[] }>>('/api/groups')
    return (res.data?.items || []).map(normalizeGroup)
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
      callsign: data.callsign,
      password: data.password,
      allow_callsign_ssid: data.allow_callsign_ssid,
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
      callsign: data.callsign,
      password: data.password,
      allow_callsign_ssid: data.allow_callsign_ssid,
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
}
