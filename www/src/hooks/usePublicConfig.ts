import { useState, useEffect } from 'react'
import { apiClient } from '../services'

export interface PublicConfig {
  icp: { icp: string }
  systemInfo: {
    name: string
    nameshorthand: string
    logo_url: string
    language: string
  }
}

const DEFAULT_CONFIG: PublicConfig = {
  icp: { icp: '' },
  systemInfo: {
    name: '麟云业余无线电链路平台',
    nameshorthand: 'DraARL',
    logo_url: '',
    language: 'zh',
  },
}

export function usePublicConfig() {
  const [config, setConfig] = useState<PublicConfig>(DEFAULT_CONFIG)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchConfig = async () => {
      try {
        const res = await apiClient.get<any>('/api/config/public')
        if (res.code === 200 && res.data) {
          setConfig(res.data)
        }
      } catch (err) {
        console.error('Failed to fetch public config:', err)
      } finally {
        setLoading(false)
      }
    }
    fetchConfig()

    // 监听配置更新事件
    const handleConfigUpdate = () => {
      fetchConfig()
    }
    window.addEventListener('config-updated', handleConfigUpdate)
    return () => {
      window.removeEventListener('config-updated', handleConfigUpdate)
    }
  }, [])

  return { config, loading }
}
