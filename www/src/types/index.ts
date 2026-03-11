// 用户相关类型
export interface User {
  id: number
  username: string
  nickname?: string
  callsign?: string
  role: string
  roles?: string[]
  status?: number
  avatar?: string
  address?: string
  phone?: string
  introduction?: string
  sex?: number
  birthday?: string
  isAdmin?: boolean
  created_at?: string
  updated_at?: string
  // 新增字段
  dmrid?: number
  mdcid?: string
  alarm_msg?: boolean
  last_login_time?: string
  last_login_ip?: string
  login_err_times?: number
}

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  user: User
}

export interface RegisterRequest {
  username: string
  password: string
  callsign?: string
}

// 设备相���类型
export interface Device {
  id: number
  name: string
  callsign: string
  ssid: number
  dev_model: number
  model: number // 前端兼容字段
  group_id: number
  is_online: boolean
  online: boolean // 前端兼容字段
  status: number
  priority?: number
  qth?: string
  online_time?: string
  last_heartbeat?: string // 前端兼容字段
  create_time?: string
  created_at?: string // 前端兼容字段
  update_time?: string
  updated_at?: string // 前端兼容字段
}

export interface DeviceQTH {
  id: number
  device_id: number
  qth: string
  latitude?: number
  longitude?: number
  altitude?: number
}

// 群组相关类型
export interface Group {
  id: number
  name: string
  type: number
  callsign?: string
  password?: string
  allow_callsign_ssid?: string
  ower_id?: number
  ower_callsign?: string
  devlist?: string
  master_server?: number
  slave_server?: number
  status?: number
  note?: string
  devices?: Device[]
  master_server_str?: string
  slave_servers?: string[]
  create_time?: string
  created_at?: string // 前端兼容字段
  update_time?: string
  updated_at?: string // 前端兼容字段
}

// 中继台相关类型
export interface Relay {
  id: number
  name: string
  tx_frequency: number
  rx_frequency: number
  ctcss?: number
  owner?: string
  location?: string
  description?: string
  status?: number
  created_at?: string
  updated_at?: string
}

// 服务器相关类型
export interface Server {
  id: number
  name: string
  type: number
  ip: string
  port: number
  status?: number
  location?: string
  description?: string
  created_at?: string
  updated_at?: string
}

// 操作日志相关类型
export interface OperatorLog {
  id: number
  timestamp: string
  content: string
  event_type: string
  operator?: string
  user_id?: number
}

export interface LogStats {
  total: number
  today: number
  by_type?: Record<string, number>
}

// 平台信息相关类型
export interface PlatformInfo {
  name: string
  version: string
  description?: string
}

export interface PlatformStats {
  total_devices: number
  online_devices: number
  total_users: number
  total_groups: number
}

// 通用响应类型
export interface ApiResponse<T = any> {
  code: number
  message: string
  data?: T
}

export interface ListResponse<T = any> {
  items: T[]
  total: number
  page: number
  page_size: number
}

// 路由菜单项类型
export interface MenuItem {
  id: string
  label: string
  icon?: string
  path?: string
  children?: MenuItem[]
  roles?: string[]
}
