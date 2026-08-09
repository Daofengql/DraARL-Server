import { Chip, Stack } from '@mui/material'
import type { BleProvisionStatus } from '../../../services/bleProvision'

interface ProvisionStatusChipsProps {
  status: BleProvisionStatus
}

function getWifiColor(state: string): 'default' | 'success' | 'warning' | 'error' {
  if (state === '已连接') return 'success'
  if (state === '连接中') return 'warning'
  if (state === '连接失败') return 'error'
  return 'default'
}

export function ProvisionStatusChips({ status }: ProvisionStatusChipsProps) {
  return (
    <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
      <Chip
        size="small"
        color={status.connected ? 'success' : 'default'}
        label={`设备: ${status.connected ? status.deviceName || '已连接' : '未连接'}`}
      />
      <Chip
        size="small"
        color={getWifiColor(status.wifiState)}
        label={`WiFi: ${status.wifiState}${status.rssi !== null ? ` (${status.rssi} dBm)` : ''}`}
      />
      <Chip size="small" color={status.connected ? 'success' : 'default'} label={`BLE: ${status.bleState}`} />
      <Chip size="small" color={status.authenticated ? 'success' : 'default'} label={`认证: ${status.authenticated ? '已通过' : '未完成'}`} />
    </Stack>
  )
}
