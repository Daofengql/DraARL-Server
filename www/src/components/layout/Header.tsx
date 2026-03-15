import { useState, useEffect } from 'react'
import {
  AppBar,
  Box,
  Toolbar,
  IconButton,
  Typography,
  Avatar,
  Tooltip,
} from '@mui/material'
import {
  Menu as MenuIcon,
} from '@mui/icons-material'
import { useNavigate } from 'react-router-dom'
import { authService, apiClient } from '../../services'

interface HeaderProps {
  onMenuClick: () => void
}

interface PublicConfig {
  icp: { icp: string }
  systemInfo: {
    name: string
    nameshorthand: string
    logo_url: string
    language: string
  }
}

const DEFAULT_TITLE = 'DraARL 麟云业余无线电链路平台'

export function Header({ onMenuClick }: HeaderProps) {
  const navigate = useNavigate()
  const [user, setUser] = useState(authService.getStoredUser())
  const [systemConfig, setSystemConfig] = useState<PublicConfig | null>(null)
  const [logoError, setLogoError] = useState(false)

  // 获取公开配置（无需认证）
  useEffect(() => {
    const fetchPublicConfig = async () => {
      try {
        const res = await apiClient.get<any>('/api/config/public')
        if (res.code === 200 && res.data) {
          setSystemConfig(res.data)
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

  // 监听用户信息变化
  useEffect(() => {
    const handleUserUpdate = () => {
      setUser(authService.getStoredUser())
    }
    window.addEventListener('user-updated', handleUserUpdate)
    return () => {
      window.removeEventListener('user-updated', handleUserUpdate)
    }
  }, [])

  const displayName = user?.nickname || user?.username || ''

  // 计算显示内容：
  // 1. 如果logo_url存在且不为空，显示Logo
  // 2. 否则，如果system.name存在且不为空，显示系统名称
  // 3. 否则，显示默认标题
  const logoUrl = systemConfig?.systemInfo?.logo_url || ''
  const systemName = systemConfig?.systemInfo?.name || ''
  const displayTitle = logoUrl ? '' : (systemName || DEFAULT_TITLE)

  return (
    <AppBar
      position="fixed"
      elevation={0}
      sx={{
        zIndex: (theme) => theme.zIndex.drawer + 1,
        bgcolor: '#ffffff',
        color: 'text.primary',
        borderBottom: '1px solid',
        borderColor: 'grey.200',
      }}
    >
      <Toolbar disableGutters sx={{ px: { xs: 2, sm: 3 }, height: 64 }}>
        <IconButton
          color="inherit"
          aria-label="open drawer"
          edge="start"
          onClick={onMenuClick}
          sx={{ mr: 2, display: { sm: 'none' } }}
        >
          <MenuIcon />
        </IconButton>
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1.5,
          }}
        >
          {logoUrl && !logoError ? (
            <Box
              component="img"
              src={logoUrl}
              alt="Logo"
              onClick={() => navigate('/')}
              sx={{
                height: 48,
                maxWidth: 240,
                objectFit: 'contain',
                cursor: 'pointer',
              }}
              onError={() => setLogoError(true)}
            />
          ) : (
            <Typography
              variant="h6"
              noWrap
              component="div"
              onClick={() => navigate('/')}
              sx={{ fontWeight: 600, color: 'primary.main', cursor: 'pointer' }}
            >
              {displayTitle}
            </Typography>
          )}
        </Box>
        <Box sx={{ flexGrow: 1 }} />
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <Avatar
            src={user?.avatar_thumb || user?.avatar}
            alt={displayName}
            sx={{ width: 36, height: 36 }}
          >
            {displayName?.charAt(0).toUpperCase() || '?'}
          </Avatar>
          <Typography variant="body2" sx={{ fontWeight: 500, display: { xs: 'none', sm: 'block' } }}>
            {displayName}
          </Typography>
        </Box>
      </Toolbar>
    </AppBar>
  )
}
