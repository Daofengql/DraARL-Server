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
  ID: number
  Name: string
  Type: number
  CallSign: string
  Password: string
  AllowCallSignSSID: string
  OwerID: number
  OwerCallSign: string
  DevList: string
  MasterServer: number
  SlaveServer: number
  Status: number
  CreateTime: string
  UpdateTime: string
  Note: string
}

// 标准化群组数据
const normalizeGroup = (g: BackendGroup): Group => ({
  id: g.ID,
  name: g.Name,
  type: g.Type,
  callsign: g.CallSign,
  password: g.Password,
  allow_callsign_ssid: g.AllowCallSignSSID,
  ower_id: g.OwerID,
  ower_callsign: g.OwerCallSign,
  devlist: g.DevList,
  master_server: g.MasterServer,
  slave_server: g.SlaveServer,
  status: g.Status,
  note: g.Note,
  create_time: g.CreateTime,
  created_at: g.CreateTime, // 前端兼容
  update_time: g.UpdateTime,
  updated_at: g.UpdateTime, // 前端兼容
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
      ID: data.id,
      Name: data.name,
      Type: data.type,
      CallSign: data.callsign,
      Password: data.password,
      AllowCallSignSSID: data.allow_callsign_ssid,
      OwerID: data.ower_id,
      OwerCallSign: data.ower_callsign,
      DevList: data.devlist,
      MasterServer: data.master_server,
      SlaveServer: data.slave_server,
      Status: data.status,
      CreateTime: data.created_at ?? data.create_time,
      UpdateTime: data.updated_at ?? data.update_time,
      Note: data.note,
    }
    const res = await apiClient.post<BackendResponse<BackendGroup>>('/api/groups', backendData)
    return normalizeGroup(res.data!)
  },

  // 更新群组
  async update(id: number, data: Partial<Group>): Promise<Group> {
    const backendData: Partial<BackendGroup> = {
      ID: data.id,
      Name: data.name,
      Type: data.type,
      CallSign: data.callsign,
      Password: data.password,
      AllowCallSignSSID: data.allow_callsign_ssid,
      OwerID: data.ower_id,
      OwerCallSign: data.ower_callsign,
      DevList: data.devlist,
      MasterServer: data.master_server,
      SlaveServer: data.slave_server,
      Status: data.status,
      CreateTime: data.created_at ?? data.create_time,
      UpdateTime: data.updated_at ?? data.update_time,
      Note: data.note,
    }
    const res = await apiClient.put<BackendResponse<BackendGroup>>(`/api/groups/${id}`, backendData)
    return normalizeGroup(res.data!)
  },

  // 删除群组
  async delete(id: number): Promise<void> {
    await apiClient.delete<BackendResponse<unknown>>(`/api/groups/${id}`)
  },
}
