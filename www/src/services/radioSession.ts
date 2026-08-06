import { apiClient } from './api'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export type GhostTransport = 'udp' | 'websocket' | 'edge'

export interface AdminRadioSession {
  session_id: string
  client_instance_hint: string
  owner_id: number
	username: string
	callsign: string
	dev_model: number
  ssid: number
  transport: GhostTransport
  online_since: string
  last_activity: string
  tx_group_id: number
  rx_group_ids: number[]
  disable_send: boolean
  disable_recv: boolean
}

export const radioSessionService = {
  async listAdmin(): Promise<AdminRadioSession[]> {
    const response = await apiClient.get<BackendResponse<AdminRadioSession[]>>('/api/admin/radio/sessions')
    return response.data || []
  },

  async disconnectAdmin(sessionID: string): Promise<void> {
    await apiClient.delete<BackendResponse<unknown>>(`/api/admin/radio/sessions/${encodeURIComponent(sessionID)}`)
  },
}
