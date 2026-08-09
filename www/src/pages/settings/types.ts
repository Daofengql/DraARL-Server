export interface APRSLogEntry {
  timestamp: string
  message: string
}

export interface SystemInfoConfig {
  name: string
  nameshorthand: string
  logo_url: string
  favicon_url: string
  language: string
  icp: string
}

export interface APRSConfig {
  aprs_server_host: string
  aprs_server_port: string
  self_address: string
  self_port: string
  callsign: string
  ssid: string
  latitude: number
  longitude: number
  altitude: string
}

export interface AccessDiscoveryConfig {
  token_ttl_seconds: number
  edge_health_ttl_seconds: number
  cache_max_age_seconds: number
  center: {
    enabled: boolean
    public_id: string
    display_name: string
    udp_host: string
    udp_port: number
    region: string
    network: string
    priority: number
  }
}

export interface OpenAIConfig {
  base_url: string
  api_key: string
  engine: string
}

export interface CommSettingsConfig {
  enabled: boolean
  retention_days: number
  min_duration_ms: number
  max_duration_sec: number
  batch_upload_sec: number
}

export interface RegistrationConfig {
  require_email_verification: boolean
}

export interface SMTPConfig {
  host: string
  port: number
  use_ssl: boolean
  sender_name: string
  sender_email: string
  password: string
}

export interface SiteConfigs {
  systemInfo: SystemInfoConfig
  accessDiscovery: AccessDiscoveryConfig
  aprs: APRSConfig
  openai: OpenAIConfig
  commSettings: CommSettingsConfig
  registration: RegistrationConfig
  smtp: SMTPConfig
}

export type SiteMessage = { type: 'success' | 'error'; text: string }

export interface ConfigEntry {
  key: string
  value: string
}

export interface BackendResponse<T> {
  code: number
  data?: T
}

export const DEFAULT_SITE_CONFIGS: SiteConfigs = {
  systemInfo: {
    name: '',
    nameshorthand: '',
    logo_url: '',
    favicon_url: '',
    language: 'zh',
    icp: '',
  },
  accessDiscovery: {
    token_ttl_seconds: 300,
    edge_health_ttl_seconds: 20,
    cache_max_age_seconds: 5,
    center: {
      enabled: false,
      public_id: 'center',
      display_name: '中心直连',
      udp_host: '',
      udp_port: 60050,
      region: '',
      network: '',
      priority: 100,
    },
  },
  aprs: {
    aprs_server_host: '',
    aprs_server_port: '',
    self_address: '',
    self_port: '',
    callsign: '',
    ssid: '',
    latitude: 0,
    longitude: 0,
    altitude: '',
  },
  openai: { base_url: '', api_key: '', engine: '' },
  commSettings: {
    enabled: false,
    retention_days: 30,
    min_duration_ms: 500,
    max_duration_sec: 300,
    batch_upload_sec: 10,
  },
  registration: { require_email_verification: true },
  smtp: {
    host: 'smtp.qq.com',
    port: 465,
    use_ssl: true,
    sender_name: '',
    sender_email: '',
    password: '',
  },
}

export function createDefaultSiteConfigs(): SiteConfigs {
  return structuredClone(DEFAULT_SITE_CONFIGS)
}
