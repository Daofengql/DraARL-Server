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

// 后端中继台响应格式
interface BackendRelay {
  ID: number
  Name: string
  TXFrequency: number
  RXFrequency: number
  CTCSS?: number
  Owner: string
  Location: string
  Description: string
  Status: number
  CreateTime: string
  UpdateTime: string
}

// 标准化中继台数据
const normalizeRelay = (r: BackendRelay): Relay => ({
  id: r.ID,
  name: r.Name,
  tx_frequency: r.TXFrequency,
  rx_frequency: r.RXFrequency,
  ctcss: r.CTCSS,
  owner: r.Owner,
  location: r.Location,
  description: r.Description,
  status: r.Status,
  created_at: r.CreateTime,
  updated_at: r.UpdateTime,
})

export const relayService = {
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
    const backendData: Partial<BackendRelay> = {
      ID: data.id,
      Name: data.name,
      TXFrequency: data.tx_frequency,
      RXFrequency: data.rx_frequency,
      CTCSS: data.ctcss,
      Owner: data.owner,
      Location: data.location,
      Description: data.description,
      Status: data.status,
      CreateTime: data.created_at,
      UpdateTime: data.updated_at,
    }
    const res = await apiClient.post<BackendResponse<BackendRelay>>('/api/relay/create', backendData)
    return normalizeRelay(res.data!)
  },

  // 更新中继台
  async update(data: Partial<Relay>): Promise<Relay> {
    const backendData: Partial<BackendRelay> = {
      ID: data.id,
      Name: data.name,
      TXFrequency: data.tx_frequency,
      RXFrequency: data.rx_frequency,
      CTCSS: data.ctcss,
      Owner: data.owner,
      Location: data.location,
      Description: data.description,
      Status: data.status,
      CreateTime: data.created_at,
      UpdateTime: data.updated_at,
    }
    const res = await apiClient.post<BackendResponse<BackendRelay>>('/api/relay/update', backendData)
    return normalizeRelay(res.data!)
  },

  // 删除中继台
  async delete(id: number): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/relay/delete', { id })
  },
}
