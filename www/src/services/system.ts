import { apiClient } from './api'

export interface SystemOverview {
  hostname: string
  os: string
  architecture: string
  cpu_cores: number
  cpu_usage_percent?: number
  load_average?: number[]
  goroutines: number
  go_version: string
  server_version: string
  build_time: string
  uptime_seconds: number
  memory: {
    available: boolean
    total_bytes: number
    used_bytes: number
    available_bytes: number
    used_percent: number
  }
  disk: {
    available: boolean
    total_bytes: number
    used_bytes: number
    free_bytes: number
    used_percent: number
  }
  process: {
    alloc_bytes: number
    heap_alloc_bytes: number
    heap_inuse_bytes: number
    sys_bytes: number
    num_gc: number
  }
  sampled_at: string
}

export const systemService = {
  async getOverview(): Promise<SystemOverview> {
    const response = await apiClient.get<{ code: number; message: string; data?: SystemOverview }>('/api/system/overview')
    return response.data || {
      hostname: '', os: '', architecture: '', cpu_cores: 0, goroutines: 0,
      go_version: '', server_version: '', build_time: '', uptime_seconds: 0,
      memory: { available: false, total_bytes: 0, used_bytes: 0, available_bytes: 0, used_percent: 0 },
      disk: { available: false, total_bytes: 0, used_bytes: 0, free_bytes: 0, used_percent: 0 },
      process: { alloc_bytes: 0, heap_alloc_bytes: 0, heap_inuse_bytes: 0, sys_bytes: 0, num_gc: 0 },
      sampled_at: '',
    }
  },
}
