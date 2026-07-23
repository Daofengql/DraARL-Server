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

export interface MetricsSnapshot {
  in_packets: number
  in_bytes: number
  out_packets: number
  out_bytes: number
  drops: number
  errors: number
}

export interface MetricRate {
  in_pps: number
  out_pps: number
  in_bytes_per_second: number
  out_bytes_per_second: number
  drops_per_second: number
  errors_per_second: number
}

export interface RateWindow {
  current: MetricRate
  minute_average: MetricRate
  minute_peak: MetricRate
  last_online: MetricRate
  sample_count: number
  stale: boolean
}

export interface NodeProtectionSnapshot {
  data_soft_limit_events: number
  data_hard_limit_drops: number
  data_queue_drops: number
  data_stale_drops: number
  control_soft_limit_events: number
  control_hard_limit_drops: number
  device_auth_limit_drops: number
  session_limit_rejects: number
  invalid_auth_tags: number
  identity_rejects: number
  expired_drops: number
  replay_drops: number
  unbound_address_drops: number
  data_bind_rejects: number
  queued_data: number
}

export interface EdgeNodeRuntime {
  node_id: string
  online: boolean
  remote_addr?: string
  connected_at?: string
  last_heartbeat?: string
  heartbeat: {
    instance_id: string
    sent_at_ms: number
    goroutines: number
    connection_count: number
    device: MetricsSnapshot
    interconnect: MetricsSnapshot
    projection_version: number
    protection: NodeProtectionSnapshot
    receiver_cache: {
      hits: number
      misses: number
      rebuilds: number
      build_ns: number
      max_entries: number
      generation: number
    }
  }
  center_interconnect: MetricsSnapshot
  traffic_rates: {
    device: RateWindow
    edge_interconnect: RateWindow
    center_interconnect: RateWindow
    reset_reason?: string
  }
  acked_projection_version: number
  pending_control: number
  sync_error?: string
  center_protection: NodeProtectionSnapshot
  control_server_protection: {
    pending_handshakes: number
    active_nodes: number
    pending_rejected: number
    auth_rate_rejected: number
    auth_failed: number
    max_nodes_rejected: number
    protocol_rejected: number
    unsupported_subtype_drops: number
  }
  datagram_bridge_protection: {
    unauthenticated_type0: number
    invalid_type0: number
    global_queue_drops: number
  }
}

export interface EdgeNode {
  id: number
  node_id: string
  display_name: string
  note: string
  status: number
  registered_at?: string
  registration_expires_at?: string
  credential_epoch: number
  last_seen_at?: string
  persisted_connection_count: number
  runtime: EdgeNodeRuntime
  create_time: string
  update_time: string
  public_access_id: string
  public_access_enabled: boolean
  public_udp_host: string
  public_udp_port: number
  public_region: string
  public_network: string
  public_priority: number
}

export interface EdgeNodeUpdate {
  display_name: string
  note: string
  status: number
  public_access_enabled: boolean
  public_udp_host: string
  public_udp_port: number
  public_region: string
  public_network: string
  public_priority: number
}

export interface EdgeNodeCredentialResult {
  credential: string
  credential_epoch: number
  previous_valid_until: string
  delivered_online: boolean
  delivery_message_id: number
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

export const edgeNodeService = {
  async list(): Promise<EdgeNode[]> {
    const res = await apiClient.get<BackendResponse<{ items: EdgeNode[] }>>('/api/edge-nodes')
    return res.data?.items || []
  },

  async create(data: { display_name: string; note: string; public_region: string }): Promise<{ node: EdgeNode; registration_token: string }> {
    const res = await apiClient.post<BackendResponse<{ node: EdgeNode; registration_token: string }>>('/api/edge-nodes', data)
    if (!res.data) throw new Error('创建节点后未返回注册凭据')
    return res.data
  },

  async update(id: number, data: EdgeNodeUpdate): Promise<EdgeNode> {
    const res = await apiClient.put<BackendResponse<EdgeNode>>(`/api/edge-nodes/${id}`, data)
    if (!res.data) throw new Error('更新节点后未返回节点信息')
    return res.data
  },

  async rotateCredential(id: number): Promise<EdgeNodeCredentialResult> {
    const res = await apiClient.post<BackendResponse<EdgeNodeCredentialResult>>(`/api/edge-nodes/${id}/rotate-credential`)
    if (!res.data) throw new Error('轮换后未返回新凭据')
    return res.data
  },

  async revokeCredential(id: number): Promise<{ credential_epoch: number; disconnected: boolean }> {
    const res = await apiClient.post<BackendResponse<{ credential_epoch: number; disconnected: boolean }>>(`/api/edge-nodes/${id}/revoke-credential`)
    if (!res.data) throw new Error('吊销凭据失败')
    return res.data
  },

  async disconnect(id: number): Promise<{ disconnected: boolean }> {
    const res = await apiClient.post<BackendResponse<{ disconnected: boolean }>>(`/api/edge-nodes/${id}/disconnect`)
    return res.data || { disconnected: false }
  },
}
