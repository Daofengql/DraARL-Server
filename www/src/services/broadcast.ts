import { apiClient } from './api'

export type BroadcastAudioStatus = 'processing' | 'ready' | 'failed'
export type BroadcastScheduleType = 'once' | 'daily' | 'weekly' | 'interval'
export type BroadcastRunStatus =
  | 'claimed'
  | 'playing'
  | 'succeeded'
  | 'skipped_recent_voice'
  | 'skipped_domain_busy'
  | 'skipped_interconnected'
  | 'skipped_no_receiver'
  | 'skipped_site_disabled'
  | 'cancelled'
  | 'cancelled_site_disabled'
  | 'cancelled_interconnect_enabled'
  | 'failed'

export interface BroadcastAudio {
  id: number
  group_id: number
  name: string
  original_mime_type: string
  original_size: number
  playback_size: number
  duration_ms: number
  packet_count: number
  sha256: string
  status: BroadcastAudioStatus
  error_message?: string
  schedule_count: number
  preview_url?: string
  created_at: string
  updated_at: string
}

export interface BroadcastSchedule {
  id: number
  group_id: number
  audio_id: number
  name: string
  schedule_type: BroadcastScheduleType
  timezone: string
  scheduled_at?: string
  local_time?: string
  weekday_mask?: number
  interval_seconds?: number
  interval_start_at?: string
  blackout_start_time?: string
  blackout_end_time?: string
  next_run_at?: string
  enabled: boolean
  suspended_reason?: string
  suspended_by_virtual_group_id?: number
  suspended_at?: string
  effective_enabled: boolean
  created_at: string
  updated_at: string
}

export interface BroadcastRun {
  id: number
  schedule_id: number
  audio_id: number
  source_group_id: number
  scheduled_for: string
  domain_key?: string
  domain_group_ids?: number[]
  status: BroadcastRunStatus
  last_voice_at?: string
  started_at?: string
  ended_at?: string
  played_duration_ms: number
  sent_packets: number
  dropped_packets: number
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
}

export interface BroadcastScheduleInput {
  audio_id: number
  name: string
  schedule_type: BroadcastScheduleType
  timezone: string
  scheduled_at?: string
  local_time?: string
  weekday_mask?: number
  interval_seconds?: number
  interval_start_at?: string
  blackout_start_time?: string
  blackout_end_time?: string
  enabled: boolean
}

export interface BroadcastContext {
  group_id: number
  interconnect_linked: boolean
  interconnect_enabled: boolean
  virtual_group_id?: number
  virtual_group_name?: string
  policy_mode?: 'suspend_all' | 'allow_single_source'
  allowed_source_group_id?: number
  allowed_source_name?: string
  source_allowed: boolean
}

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

function unwrap<T>(response: BackendResponse<T>, fallback: string): T {
  if (response.code < 200 || response.code >= 300 || response.data === undefined) {
    throw new Error(response.message || fallback)
  }
  return response.data
}

export const broadcastService = {
  async getContext(groupId: number): Promise<BroadcastContext> {
    const response = await apiClient.get<BackendResponse<BroadcastContext>>(`/api/groups/${groupId}/broadcast-context`)
    return unwrap(response, '读取自动播报互联状态失败')
  },

  async listAudios(groupId: number): Promise<BroadcastAudio[]> {
    const response = await apiClient.get<BackendResponse<{ items: BroadcastAudio[] }>>(`/api/groups/${groupId}/broadcast-audios`)
    return unwrap(response, '读取播报音频失败').items
  },

  async uploadAudio(groupId: number, file: File, name: string): Promise<BroadcastAudio> {
    const form = new FormData()
    form.append('file', file)
    if (name.trim()) form.append('name', name.trim())
    const response = await apiClient.postFormData<BackendResponse<BroadcastAudio>>(`/api/groups/${groupId}/broadcast-audios`, form)
    return unwrap(response, '上传播报音频失败')
  },

  async getAudio(groupId: number, audioId: number): Promise<BroadcastAudio> {
    const response = await apiClient.get<BackendResponse<BroadcastAudio>>(`/api/groups/${groupId}/broadcast-audios/${audioId}`)
    return unwrap(response, '读取音频详情失败')
  },

  async deleteAudio(groupId: number, audioId: number): Promise<void> {
    const response = await apiClient.delete<BackendResponse<unknown>>(`/api/groups/${groupId}/broadcast-audios/${audioId}`)
    if (response.code < 200 || response.code >= 300) throw new Error(response.message || '删除播报音频失败')
  },

  async listSchedules(groupId: number): Promise<BroadcastSchedule[]> {
    const response = await apiClient.get<BackendResponse<{ items: BroadcastSchedule[] }>>(`/api/groups/${groupId}/broadcast-schedules`)
    return unwrap(response, '读取播报计划失败').items
  },

  async createSchedule(groupId: number, input: BroadcastScheduleInput): Promise<BroadcastSchedule> {
    const response = await apiClient.post<BackendResponse<BroadcastSchedule>>(`/api/groups/${groupId}/broadcast-schedules`, input)
    return unwrap(response, '创建播报计划失败')
  },

  async updateSchedule(groupId: number, scheduleId: number, input: Partial<BroadcastScheduleInput>): Promise<BroadcastSchedule> {
    const response = await apiClient.patch<BackendResponse<BroadcastSchedule>>(`/api/groups/${groupId}/broadcast-schedules/${scheduleId}`, input)
    return unwrap(response, '更新播报计划失败')
  },

  async deleteSchedule(groupId: number, scheduleId: number): Promise<void> {
    const response = await apiClient.delete<BackendResponse<unknown>>(`/api/groups/${groupId}/broadcast-schedules/${scheduleId}`)
    if (response.code < 200 || response.code >= 300) throw new Error(response.message || '删除播报计划失败')
  },

  async runSchedule(groupId: number, scheduleId: number): Promise<BroadcastRun> {
    const response = await apiClient.post<BackendResponse<BroadcastRun>>(`/api/groups/${groupId}/broadcast-schedules/${scheduleId}/run`)
    return unwrap(response, '触发播报失败')
  },

  async listRuns(groupId: number, page = 1, pageSize = 20): Promise<{ items: BroadcastRun[]; total: number }> {
    const response = await apiClient.get<BackendResponse<{ items: BroadcastRun[]; total: number }>>(
      `/api/groups/${groupId}/broadcast-runs?page=${page}&page_size=${pageSize}`,
    )
    return unwrap(response, '读取执行历史失败')
  },

  async cancelRun(groupId: number, runId: number): Promise<void> {
    const response = await apiClient.post<BackendResponse<unknown>>(`/api/groups/${groupId}/broadcast-runs/${runId}/cancel`)
    if (response.code < 200 || response.code >= 300) throw new Error(response.message || '停止播报失败')
  },
}
