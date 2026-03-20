// 用户相关类型
export interface User {
  id: number
  username: string
  nickname?: string
  callsign?: string
  role: string
  roles?: string[]
  status?: number
  approval_status?: number  // 0=待审核, 1=已通过, 2=已拒绝
  review_note?: string
  avatar?: string
  avatar_thumb?: string  // 头像缩略图
  address?: string
  phone?: string
  email?: string
  email_verified?: boolean
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
  last_group_id?: number  // 用户上次选中的群组 ID（用于跨设备/跨会话同步）
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
  callsign: string
  phone: string
  nickname?: string
  email?: string
  session_id?: string
  email_code?: string
}

// 设备相关类型
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
  disable_send?: boolean  // 禁用发送
  disable_recv?: boolean  // 禁用接收
  note?: string           // 备注
  password?: string       // 设备密码
  group_name?: string     // 所属群组名称（前端扩展）
  owner_id?: number       // 设备所有者ID
  owner_name?: string     // 设备所有者名称
  owner_callsign?: string // 设备所有者呼号
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
  type: number  // 1=公开, 2=私有
  callsign?: string
  password?: string
  allow_callsign_ssid?: string
  ower_id?: number
  ower_callsign?: string
  ower_name?: string     // 群组创建者名称
  devlist?: string
  master_server?: number
  slave_server?: number
  status?: number  // 0=禁用, 1=启用
  is_virtual?: boolean  // 是否为虚拟互联组
  note?: string
  devices?: Device[]

  // 新增字段
  is_joined?: boolean       // 当前用户是否已加入（私有群组）
  is_owner?: boolean        // 当前用户是否是创建者
  online_count?: number     // 在线设备数
  total_count?: number      // 总设备数
  require_password?: boolean // 是否需要密码（私有群组且未验证）

  master_server_str?: string
  slave_servers?: string[]
  create_time?: string
  created_at?: string // 前端兼容字段
  update_time?: string
  updated_at?: string // 前端兼容字段
}

// 群组互联关联类型
export interface GroupLink {
  id: number
  link_group_id: number      // 互联组ID
  target_group_id: number    // 目标群组ID
  target_group_name?: string // 目标群组名称
  target_group_status?: number // 目标群组状态
  created_at: string
  updated_at: string
}

// 虚拟互联组（包含关联信息）
export interface VirtualGroup extends Group {
  target_count?: number  // 关联的目标群组数量
  targets?: GroupLinkTarget[]
}

// 互联组关联的目标群组
export interface GroupLinkTarget {
  id: number
  target_group_id: number
  target_group_name: string
  target_group_status: number
  created_at: string
}

// 群组成员类型
export interface GroupMember {
  id: number
  group_id: number
  user_id: number
  username?: string
  callsign?: string
  is_verified: boolean
  join_time: string
  last_verify: string
  device_count?: number    // 该成员在群组中的设备数
  disable_send: boolean
  disable_recv: boolean
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

// 操作证相关类型
export interface OperatorCertificate {
  id: number
  file_name: string
  file_size: number
  file_type: string
  upload_time: string
  file_url?: string
  status?: number  // 0=待审核, 1=已通过, 2=已拒绝
  review_note?: string
}

// 新增：证书响应类型，包含 active_cert 和 pending_cert
export interface CertificateResponse {
  active_cert: OperatorCertificate | null
  pending_cert: OperatorCertificate | null
}

// 操作证上传响应
export interface OperatorCertificateUpload {
  id: number
  file_name: string
  file_size: number
  upload_time: string
}

export interface FileUploadResponse {
  file_name: string
  file_size: number
  file_type: string
  minio_path: string
  file_url: string
}

// 用户审批相关类型
export interface PendingApproval {
  id: number
  username: string
  nickname?: string
  callsign?: string
  phone?: string
  address?: string
  approval_status: number
  created_at: string
  has_cert: boolean
  cert?: OperatorCertificate
  certs?: OperatorCertificate[]  // 新增：所有操作证列表
  review_time?: string
  reviewer_id?: number
  review_note?: string
}

export interface ApprovalRequest {
  status: number  // 1=通过, 2=拒绝
  note: string
}

// 通信统计相关类型
export interface UserCommStats {
  total_count: number
  total_size: number      // 文件总大小（字节）
  total_duration: number  // 总时长（毫秒）
}

export interface SystemCommStats {
  total_count: number
  total_size: number      // 文件总大小（字节）
  total_duration: number  // 总时长（毫秒）
}

export interface DailyCommStats {
  date: string
  count: number
  duration: number  // 总时长（毫秒）
}

// 操作证审批相关类型
export interface CertificateApproval {
  id: number
  user_id: number
  username: string
  nickname?: string
  callsign?: string
  file_name: string
  file_size: number
  file_type: string
  upload_time: string
  file_url?: string
  status: number  // 0=待审核, 1=已通过, 2=已拒绝
  review_note?: string
  review_time?: string
  reviewer_id?: number
  is_update: boolean  // true=更新(非首次), false=首次
  is_replaced?: boolean // true=被新证替换(但之前是通过), false=未替换或真正被拒绝
}
