import type { APRSConfig, AccessDiscoveryConfig, CommSettingsConfig } from './types'

export function validateCommSettings(config: CommSettingsConfig): string | null {
  if (!Number.isInteger(config.retention_days) || config.retention_days < 1 || config.retention_days > 365) return '数据保留天数必须在 1-365 之间'
  if (!Number.isSafeInteger(config.min_duration_ms) || config.min_duration_ms < 0) return '最小录制阈值必须为非负整数'
  if (!Number.isSafeInteger(config.max_duration_sec) || config.max_duration_sec < 0) return '最大录制时长必须为非负整数'
  if (config.max_duration_sec > 0 && config.min_duration_ms > config.max_duration_sec * 1000) return '最小录制阈值不能超过最大录制时长'
  if (!Number.isInteger(config.batch_upload_sec) || config.batch_upload_sec < 1 || config.batch_upload_sec > 300) return '批量上传间隔必须在 1-300 秒之间'
  return null
}

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
