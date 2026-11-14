import { apiClient } from '../api'
import type { OnlineDevice } from '../../types/radio'

export interface RadioDeviceResponse {
  key: string
  id: number
  username: string
  callsign: string
  ssid: number
  nickname?: string
  dev_model: number
  group_id: number
  is_ghost: boolean
  disable_send: boolean
  disable_recv: boolean
  session_id?: string
  last_activity?: string
}

export function toOnlineDevice(device: RadioDeviceResponse): OnlineDevice {
  return {
    key: device.key || device.session_id || `${device.is_ghost ? 'g' : 'd'}-${device.id}`,
    id: device.id,
    username: device.username,
    callsign: device.callsign,
    ssid: device.ssid,
    nickname: device.nickname,
    devModel: device.dev_model,
    groupId: device.group_id,
    isOnline: true,
    isGhost: device.is_ghost,
    disableSend: device.disable_send,
    disableRecv: device.disable_recv,
    sessionId: device.session_id,
    lastPacketTime: device.last_activity,
  }
}

export async function loadRadioDevices(groupId: number, signal: AbortSignal): Promise<OnlineDevice[]> {
  const response = await apiClient.get<{ data: RadioDeviceResponse[] }>(
    `/api/radio/groups/${groupId}/devices`, { signal },
  )
  return response.data.map(toOnlineDevice)
}
