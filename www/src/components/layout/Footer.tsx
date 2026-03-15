import { useState, useEffect } from 'react'
import { Box, Typography, Link } from '@mui/material'
import { apiClient } from '../../services'

interface PublicConfig {
  icp: { icp: string }
}

export function Footer() {
  const [icp, setIcp] = useState('')

  useEffect(() => {
    const fetchPublicConfig = async () => {
      try {
        const res = await apiClient.get<any>('/api/config/public')
        if (res.code === 200 && res.data?.icp?.icp) {
          setIcp(res.data.icp.icp)
        }
      } catch (err) {
        console.error('Failed to fetch public config:', err)
      }
    }
    fetchPublicConfig()

    // 监听配置更新事件
    const handleConfigUpdate = () => {
      fetchPublicConfig()
    }
    window.addEventListener('config-updated', handleConfigUpdate)
    return () => {
      window.removeEventListener('config-updated', handleConfigUpdate)
    }
  }, [])

  if (!icp) {
    return null
  }

  return (
    <Box
      component="footer"
      sx={{
        py: 2,
        px: 2,
        bgcolor: '#1a1a1a',
        textAlign: 'center',
        mt: 'auto',
      }}
    >
      <Link
        href="http://beian.miit.gov.cn/"
        target="_blank"
        rel="noopener noreferrer"
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 1,
          color: '#999',
          textDecoration: 'none',
          '&:hover': {
            color: '#ccc',
          },
        }}
      >
        <Box
          component="img"
          src="//oss-fz.silverdragon.cn/loongapisources/picbed/penglong/2023/07/24/202307240118075832.png"
          alt="备案图标"
          sx={{
            height: 16,
            width: 16,
          }}
        />
        <Typography variant="caption" sx={{ color: 'inherit' }}>
          {icp}
        </Typography>
      </Link>
    </Box>
  )
}
