import { useState, useEffect } from 'react'
import {
  AppBar,
  Box,
  Toolbar,
  IconButton,
  Typography,
  Avatar,
} from '@mui/material'
import MenuIcon from '@mui/icons-material/Menu'
import { useNavigate } from 'react-router-dom'
import { authService } from '../../services'
import { SITE_CONFIG } from '../../config/site'
import { useConfig } from '../../contexts/ConfigContext'

interface HeaderProps {
  onMenuClick: () => void
}

export function Header({ onMenuClick }: HeaderProps) {
  const navigate = useNavigate()
  const [user, setUser] = useState(authService.getStoredUser())
  const { config: systemConfig } = useConfig()
  const [logoError, setLogoError] = useState(false)

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
  const displayTitle = logoUrl ? '' : (systemName || SITE_CONFIG.NAME)

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
