import type { APRSConfig, AccessDiscoveryConfig } from './types'

export function validateAPRS(config: APRSConfig): string | null {
  if (config.longitude < -180 || config.longitude > 180) return '经度必须在 -180 到 180 之间'
  if (config.latitude < -90 || config.latitude > 90) return '纬度必须在 -90 到 90 之间'
  return null
}

export function validateAccessDiscovery(config: AccessDiscoveryConfig): string | null {
  if (!/^[a-z0-9](?:[a-z0-9_-]{0,62}[a-z0-9])?$/.test(config.node_id.trim().toLowerCase())) return '节点 ID 应为 1-64 位字母、数字、横线或下划线，首尾为字母或数字'
  if (!Number.isInteger(config.udp_port) || config.udp_port < 1 || config.udp_port > 65535) return '节点 UDP 端口必须在 1-65535 之间'
  return null
}
