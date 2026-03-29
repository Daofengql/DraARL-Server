import { apiClient } from './api'
import type {
  Relay,
  ListResponse,
} from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

// 后端中继台响应格式（与 gormdb.Relay 对应）
interface BackendRelay {
  id: number
  name: string
  up_freq: string
  down_freq: string
  send_ctss: string
  recive_ctss: string
  ower_callsign: string
  location: string
  status: number
  note: string
  create_time?: string
  update_time?: string
}

// 标准化中继台数据（后端 -> 前端）
const normalizeRelay = (r: BackendRelay): Relay => ({
  id: r.id,
  name: r.name,
  up_freq: r.up_freq,
  down_freq: r.down_freq,
  send_ctcss: r.send_ctss,
  receive_ctcss: r.recive_ctss,
  ower_callsign: r.ower_callsign,
  location: r.location,
  status: r.status,
  note: r.note,
  create_time: r.create_time,
  update_time: r.update_time,
})

// 转换前端数据为后端格式（前端 -> 后端）
const toBackendFormat = (data: Partial<Relay>): Partial<BackendRelay> => ({
  id: data.id,
  name: data.name,
  up_freq: data.up_freq,
  down_freq: data.down_freq,
  send_ctss: data.send_ctcss,
  recive_ctss: data.receive_ctcss,
  ower_callsign: data.ower_callsign,
  location: data.location,
  status: data.status,
  note: data.note,
})

export const relayService = {
  // 公开搜索中继台（无需登录）
  async publicSearch(location: string): Promise<Relay[]> {
    const res = await apiClient.get<BackendResponse<{ items: BackendRelay[] }>>('/api/public/relays', {
      params: { location }
    })
    return (res.data?.items || []).map(normalizeRelay)
  },

  // 获取中继台列表
  async getList(params?: {
    page?: number
    page_size?: number
    keyword?: string
  }): Promise<ListResponse<Relay>> {
    const res = await apiClient.get<BackendResponse<{ items: BackendRelay[] }>>('/api/relays', { params })
    const items = (res.data?.items || []).map(normalizeRelay)
    return { items, total: items.length, page: params?.page || 1, page_size: params?.page_size || 10 }
  },

  // 获取中继台列表（兼容旧接口）
  async list(): Promise<Relay[]> {
    const res = await apiClient.get<BackendResponse<{ items: BackendRelay[] }>>('/api/relays')
    return (res.data?.items || []).map(normalizeRelay)
  },

  // 获取中继台详情
  async get(id: number): Promise<Relay> {
    const res = await apiClient.get<BackendResponse<BackendRelay>>(`/api/relays/${id}`)
    return normalizeRelay(res.data!)
  },

  // 创建中继台
  async create(data: Partial<Relay>): Promise<Relay> {
    const res = await apiClient.post<BackendResponse<BackendRelay>>('/api/relay/create', toBackendFormat(data))
    return normalizeRelay(res.data!)
  },

  // 更新中继台
  async update(data: Partial<Relay>): Promise<Relay> {
    const res = await apiClient.post<BackendResponse<BackendRelay>>('/api/relay/update', toBackendFormat(data))
    return normalizeRelay(res.data!)
  },

  // 删除中继台
  async delete(id: number): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/relay/delete', { id })
  },
}
