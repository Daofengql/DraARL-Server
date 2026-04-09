import { apiClient } from './api'
import type {
  Device,
  DeviceQTH,
  ListResponse,
} from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

// 后端设备响应格式
interface BackendDevice {
  id: number
  name: string
  callsign: string
  ssid: number
  dev_model: number
  group_id: number
  is_online: boolean
  status: number
  priority?: number
  disable_send?: boolean
  disable_recv?: boolean
  qth?: string
  last_online_ip?: string
  last_online_ip_location?: string
  note?: string
  password?: string
  online_time?: string
  owner_id?: number
  owner_name?: string
  owner_callsign?: string
  create_time?: string
  update_time?: string
}

// 标准化设备数据
const normalizeDevice = (d: BackendDevice): Device => ({
  id: d.id,
  name: d.name,
  callsign: d.callsign,
  ssid: d.ssid,
  dev_model: d.dev_model,
  model: d.dev_model, // 前端兼容
  group_id: d.group_id,
  is_online: d.is_online,
  online: d.is_online, // 前端兼容
  status: d.status,
  priority: d.priority,
  disable_send: d.disable_send,
  disable_recv: d.disable_recv,
  qth: d.qth,
  last_online_ip: d.last_online_ip,
  last_online_ip_location: d.last_online_ip_location,
  note: d.note,
  owner_id: d.owner_id,
  owner_name: d.owner_name,
  owner_callsign: d.owner_callsign,
  online_time: d.online_time,
  last_heartbeat: d.online_time,
  create_time: d.create_time,
  created_at: d.create_time, // 前端兼容
  update_time: d.update_time,
  updated_at: d.update_time, // 前端兼容
})

export const deviceService = {
  // 获取设备列表
  async getList(params?: {
    page?: number
    page_size?: number
    keyword?: string
    group_id?: number
  }): Promise<ListResponse<Device>> {
    const res = await apiClient.get<BackendResponse<{ items: BackendDevice[]; total: number }>>('/api/devices', { params })
    const items = (res.data?.items || []).map(normalizeDevice)
    return { items, total: res.data?.total || 0, page: params?.page || 1, page_size: params?.page_size || 10 }
  },

  // 获取设备列表（兼容旧接口）
  async list(): Promise<Device[]> {
    const res = await apiClient.get<BackendResponse<{ items: BackendDevice[] }>>('/api/devices')
    return (res.data?.items || []).map(normalizeDevice)
  },

  // 获取单个设备
  async get(id: number): Promise<Device> {
    const res = await apiClient.get<BackendResponse<BackendDevice>>(`/api/devices/${id}`)
    return normalizeDevice(res.data!)
  },

  // 获取单个设备（兼容旧接口）
  async getById(id: number): Promise<Device> {
    const res = await apiClient.get<BackendResponse<BackendDevice>>('/api/device/get', { params: { id } })
    return normalizeDevice(res.data!)
  },

  // 更新设备
  async update(id: number, data: Partial<Device>): Promise<Device> {
    const backendData: Partial<BackendDevice> = {
      id: data.id,
      name: data.name,
      callsign: data.callsign,
      ssid: data.ssid,
      group_id: data.group_id,
      is_online: data.online ?? data.is_online,
      status: data.status,
      priority: data.priority,
      disable_send: data.disable_send,
      disable_recv: data.disable_recv,
      qth: data.qth,
      note: data.note,
      password: data.password,
      online_time: data.last_heartbeat ?? data.online_time,
      create_time: data.created_at ?? data.create_time,
      update_time: data.updated_at ?? data.update_time,
    }
    const res = await apiClient.put<BackendResponse<BackendDevice>>(`/api/devices/${id}`, backendData)
    return normalizeDevice(res.data!)
  },

  // 删除设备
  async delete(id: number): Promise<void> {
    await apiClient.delete<BackendResponse<unknown>>(`/api/devices/${id}`)
  },

  // 获取设备位置列表
  async getQTHs(): Promise<DeviceQTH[]> {
    const res = await apiClient.get<BackendResponse<{ items: DeviceQTH[] }>>('/api/device/qths')
    return res.data?.items || []
  },

  // 获取设备位置（兼容旧接口）
  async getQTH(id: number): Promise<string> {
    const res = await apiClient.get<BackendResponse<string>>('/api/device/qth', { params: { id } })
    return res.data || ''
  },

  // 修改设备群组
  async changeGroup(data: { device_id: number; group_id: number; password?: string }): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/device/changegroup', data)
  },

  // 执行AT命令
  async executeAT(data: { device_id: number; command: string }): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/device/at', data)
  },

  // 查询设备参数
  async query(data: { device_id: number; param: string }): Promise<any> {
    const res = await apiClient.post<BackendResponse<any>>('/api/device/query', data)
    return res.data
  },

  // 修改设备参数
  async change(data: { device_id: number; params: Record<string, any> }): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/device/change', data)
  },

  // 修改1W模块参数
  async change1W(data: { device_id: number; params: Record<string, any> }): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/device/change1w', data)
  },

  // 修改2W模块参数
  async change2W(data: { device_id: number; params: Record<string, any> }): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/device/change2w', data)
  },

  // 切换设备群组
  async switchGroup(
    deviceId: number,
    groupId: number,
    password?: string
  ): Promise<void> {
    await apiClient.post<BackendResponse<unknown>>('/api/device/changegroup', {
      device_id: deviceId,
      group_id: groupId,
      password: password || ''
    })
  },

  // ============================================================
  // 设备配置同步 API（UDP 普通设备）
  // ============================================================

  // 获取设备配置
  async getConfig(deviceId: number): Promise<Record<string, string>> {
    const res = await apiClient.get<BackendResponse<Record<string, string>>>(`/api/devices/${deviceId}/config`)
    return res.data || {}
  },

  // 更新设备配置
  async updateConfig(deviceId: number, config: Partial<DeviceConfig>): Promise<Record<string, string>> {
    const res = await apiClient.put<BackendResponse<Record<string, string>>>(`/api/devices/${deviceId}/config`, config)
    return res.data || {}
  },

  // 立即同步配置到设备
  async syncConfig(deviceId: number): Promise<{ message: string }> {
    const res = await apiClient.post<BackendResponse<{ message: string }>>(`/api/devices/${deviceId}/config/sync`)
    return res.data || { message: '同步请求已发送' }
  },
}

// 设备配置类型定义
export interface DeviceConfig {
  rx_freq?: string       // 接收频率 (Hz)
  tx_freq?: string       // 发射频率 (Hz)
  rx_ctcss?: string      // 接收亚音旧字段 (Hz, 0=关闭)
  tx_ctcss?: string      // 发射亚音旧字段 (Hz, 0=关闭)
  rx_tone_mode?: string  // 接收亚音类型 (off/ctcss/cdcss_n/cdcss_i)
  rx_tone_value?: string // 接收亚音值 (88.5/023)
  tx_tone_mode?: string  // 发射亚音类型 (off/ctcss/cdcss_n/cdcss_i)
  tx_tone_value?: string // 发射亚音值 (88.5/023)
  sql_level?: string     // 静噪等级 (0-8)
  power_level?: string   // 功率等级 (1=低, 3=高)
  tx_bandwidth?: string  // 发射带宽 (1=窄带, 2=宽带)
}
