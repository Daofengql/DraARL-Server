import { apiClient } from './api'
import type {
  Server,
  ListResponse,
} from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

// 后端服务器响应格式
interface BackendServer {
  ID: number
  Name: string
  ServerType: number
  JoinKey: string
  IPAddr: string
  UDPPort: string
  Status: number
  OwerID: number
  OwerCallSign: string
  CreateTime: string
  UpdateTime: string
  Note: string
}

// 标准化服务器数据
const normalizeServer = (s: BackendServer): Server => ({
  id: s.ID,
  name: s.Name,
  type: s.ServerType,
  ip: s.IPAddr,
  port: parseInt(s.UDPPort) || 0,
  status: s.Status,
  location: s.OwerCallSign,
  description: s.Note,
  created_at: s.CreateTime,
  updated_at: s.UpdateTime,
})

export const serverService = {
  // 获取服务器列表
  async getList(params?: {
    page?: number
    page_size?: number
    keyword?: string
  }): Promise<ListResponse<Server>> {
    const res = await apiClient.get<BackendResponse<{ items: BackendServer[] }>>('/api/servers', { params })
    const items = (res.data?.items || []).map(normalizeServer)
    return { items, total: items.length, page: params?.page || 1, page_size: params?.page_size || 10 }
  },

  // 获取服务器列表（兼容旧接口）
  async list(): Promise<Server[]> {
    const res = await apiClient.get<BackendResponse<{ items: BackendServer[] }>>('/api/servers')
    return (res.data?.items || []).map(normalizeServer)
  },

  // 获取服务器详情
  async get(id: number): Promise<Server> {
    const res = await apiClient.get<BackendResponse<BackendServer>>(`/api/servers/${id}`)
    return normalizeServer(res.data!)
  },

  // 创建服务器
  async create(data: Partial<Server>): Promise<Server> {
    const backendData: Partial<BackendServer> = {
      ID: data.id,
      Name: data.name,
      ServerType: data.type,
      JoinKey: '',
      IPAddr: data.ip,
      UDPPort: String(data.port),
      Status: data.status,
      OwerID: 0,
      OwerCallSign: data.location,
      CreateTime: data.created_at,
      UpdateTime: data.updated_at,
      Note: data.description,
    }
    const res = await apiClient.post<BackendResponse<BackendServer>>('/api/server/create', backendData)
    return normalizeServer(res.data!)
  },

  // 更新服务器
  async update(data: Partial<Server>): Promise<Server> {
    const backendData: Partial<BackendServer> = {
      ID: data.id,
      Name: data.name,
      ServerType: data.type,
      JoinKey: '',
      IPAddr: data.ip,
      UDPPort: String(data.port),
      Status: data.status,
      OwerID: 0,
      OwerCallSign: data.location,
      CreateTime: data.created_at,
      UpdateTime: data.updated_at,
      Note: data.description,
    }
    const res = await apiClient.post<BackendResponse<BackendServer>>('/api/server/update', backendData)
    return normalizeServer(res.data!)
  },

  // 删除服务器
  async delete(id: number): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/server/delete', { id })
  },
}
