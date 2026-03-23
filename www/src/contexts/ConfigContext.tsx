import { createContext, useContext, useState, useEffect, useRef, type ReactNode } from 'react'
import { apiClient } from '../services'
import { SITE_CONFIG } from '../config/site'

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

const DEFAULT_CONFIG: PublicConfig = {
  icp: { icp: '' },
  systemInfo: {
    name: SITE_CONFIG.NAME,
    nameshorthand: SITE_CONFIG.SHORT_NAME,
    logo_url: '',
    language: 'zh',
  },
  sso_enabled: false,
  sso_name: 'SSO',
}

interface ConfigContextValue {
  config: PublicConfig
  loading: boolean
  refresh: () => Promise<void>
}

const ConfigContext = createContext<ConfigContextValue | null>(null)

// 全局请求状态，用于防止 StrictMode 双重请求
let pendingRequest: Promise<void> | null = null
let configFetched = false

export function ConfigProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<PublicConfig>(DEFAULT_CONFIG)
  const [loading, setLoading] = useState(true)
  const mounted = useRef(true)

  const fetchConfig = async () => {
    // 如果已经有缓存数据，直接返回
    if (configFetched) {
      return
    }

    // 如果已有正在进行的请求，复用它
    if (pendingRequest) {
      return pendingRequest
    }

    pendingRequest = (async () => {
      try {
        const res = await apiClient.get<{ code: number; data?: PublicConfig }>('/api/config/public')
        if (res.code === 200 && res.data && mounted.current) {
          setConfig(res.data)
          configFetched = true
        }
      } catch (err) {
        console.error('Failed to fetch public config:', err)
      } finally {
        if (mounted.current) {
          setLoading(false)
        }
        pendingRequest = null
      }
    })()

    return pendingRequest
  }

  const refresh = async () => {
    configFetched = false
    pendingRequest = null
    setLoading(true)
    await fetchConfig()
  }

  useEffect(() => {
    fetchConfig()
    return () => {
      mounted.current = false
    }
  }, [])

  // 监听配置更新事件
  useEffect(() => {
    const handleConfigUpdate = () => {
      refresh()
    }
    window.addEventListener('config-updated', handleConfigUpdate)
    return () => {
      window.removeEventListener('config-updated', handleConfigUpdate)
    }
  }, [])

  return (
    <ConfigContext.Provider value={{ config, loading, refresh }}>
      {children}
    </ConfigContext.Provider>
  )
}

export function useConfig() {
  const context = useContext(ConfigContext)
  if (!context) {
    throw new Error('useConfig must be used within a ConfigProvider')
  }
  return context
}
