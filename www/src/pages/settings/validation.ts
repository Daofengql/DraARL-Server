import type { APRSConfig, AccessDiscoveryConfig } from './types'

export function validateAPRS(config: APRSConfig): string | null {
  if (config.longitude < -180 || config.longitude > 180) return '经度必须在 -180 到 180 之间'
  if (config.latitude < -90 || config.latitude > 90) return '纬度必须在 -90 到 90 之间'
  return null
}

export function validateAccessDiscovery(config: AccessDiscoveryConfig): string | null {
  if (config.token_ttl_seconds < 1 || config.token_ttl_seconds > 300) {
    return '发现凭证有效期必须在 1-300 秒之间'
  }
  if (config.edge_health_ttl_seconds < 1 || config.edge_health_ttl_seconds > 300) {
    return '边缘健康有效期必须在 1-300 秒之间'
  }
  if (config.cache_max_age_seconds < 1 || config.cache_max_age_seconds > 30) {
    return '客户端缓存时间必须在 1-30 秒之间'
  }
  if (config.center.enabled && config.center.udp_host.trim() === '') {
    return '启用中心直连时必须填写公网 UDP 地址'
  }
  if (config.center.udp_port < 1 || config.center.udp_port > 65535) {
    return '中心公网 UDP 端口必须在 1-65535 之间'
  }
  return null
}
