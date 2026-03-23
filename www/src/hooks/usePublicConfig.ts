import { useConfig } from '../contexts/ConfigContext'

export interface PublicConfig {
  icp: { icp: string }
  systemInfo: {
    name: string
    nameshorthand: string
    logo_url: string
    language: string
  }
  sso_enabled: boolean
  sso_name: string
}

/**
 * usePublicConfig - 获取公开配置的 Hook
 * 现在是对 ConfigContext 的简单封装，保持向后兼容
 */
export function usePublicConfig() {
  const { config, loading } = useConfig()
  return { config, loading }
}
