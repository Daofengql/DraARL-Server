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
import { authService } from '../../services'

interface HeaderProps {
  onMenuClick: () => void
}

export function Header({ onMenuClick }: HeaderProps) {
  const navigate = useNavigate()
  const [user, setUser] = useState(authService.getStoredUser())

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
        <Typography variant="h6" noWrap component="div" sx={{ fontWeight: 600, color: 'primary.main' }}>
          NRLLink 管理平台
        </Typography>
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
