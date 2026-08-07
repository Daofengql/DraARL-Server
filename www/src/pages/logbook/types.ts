// 通联日志数据类型
export interface LogbookEntry {
  id: number
  user_id?: number
  username?: string
  my_callsign: string  // 我方呼号（冗余存储，支持客席发射）
  // 时间（数据库存储UTC，前端负责BJT转换）
  time_utc: string
  // 频率
  tx_frequency: number // MHz
  rx_frequency: number // MHz
  // 分区
  cq_zone: number
  itu_zone: number
  // 通信模式
  mode: string
  // 对方信息
  callsign: string
  their_rst: string
  their_power?: number // W
  their_qth?: string
  their_radio?: string
  their_antenna?: string
  // 我方信息
  my_rst: string
  my_power?: number // W
  my_qth?: string
  my_radio?: string
  my_antenna?: string
  // 备注
  notes?: string
  created_at?: string
  updated_at?: string
}

// API 响应类型
export interface LogbookListResponse {
  code: number
  message: string
  data: {
    total: number
    items: LogbookEntry[]
    page: number
    page_size: number
  }
}

export interface LogbookResponse {
  code: number
  message: string
  data: LogbookEntry
}

// 电台预设类型
export interface RadioPreset {
  id: number
  user_id: number
  name: string
  radio: string
  antenna: string
  power: number | null
  qth: string
  sort_order: number
  created_at: string
  updated_at: string
}

export interface RadioPresetListResponse {
  code: number
  message: string
  data: RadioPreset[]
}

export interface LogbookFilters {
  startTime: string
  endTime: string
  callsign: string
  frequency: string
  mode: string
  username: string
}

export interface LogbookSnackbar {
  open: boolean
  message: string
  severity: 'success' | 'error'
}

// 时间转换工具函数
