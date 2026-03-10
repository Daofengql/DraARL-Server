import { useState } from 'react'
import {
  AppBar,
  Box,
  Toolbar,
  IconButton,
  Typography,
  Menu,
  MenuItem,
  Avatar,
  Divider,
  ListItemIcon,
  ListItemText,
} from '@mui/material'
import {
  Menu as MenuIcon,
  AccountCircle,
  Logout,
  Settings,
} from '@mui/icons-material'
import { useNavigate } from 'react-router-dom'
import { authService } from '../../services'

interface HeaderProps {
  onMenuClick: () => void
}

export function Header({ onMenuClick }: HeaderProps) {
  const navigate = useNavigate()
  const user = authService.getStoredUser()
  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null)

  const handleMenuOpen = (event: React.MouseEvent<HTMLElement>) => {
    setAnchorEl(event.currentTarget)
  }

  const handleMenuClose = () => {
    setAnchorEl(null)
  }

  const handleLogout = async () => {
    await authService.logout()
    authService.clearAuth()
    navigate('/login')
    handleMenuClose()
  }

  // 显示名称：优先使用 nickname，其次 username
  const displayName = user?.nickname || user?.username

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
        <Box sx={{ display: 'flex', alignItems: 'center' }}>
          <Box
            onClick={handleMenuOpen}
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              cursor: 'pointer',
              px: 1,
              py: 0.5,
              borderRadius: 2,
              '&:hover': { bgcolor: 'action.hover' },
            }}
          >
            {user?.avatar ? (
              <Avatar src={user.avatar} alt={displayName} sx={{ width: 32, height: 32 }}>
                {displayName?.charAt(0).toUpperCase() || '?'}
              </Avatar>
            ) : (
              <Avatar sx={{ width: 32, height: 32, bgcolor: 'primary.main', fontWeight: 600 }}>
                {displayName?.charAt(0).toUpperCase() || '?'}
              </Avatar>
            )}
            <Typography variant="body2" sx={{ display: { xs: 'none', sm: 'block' }, fontWeight: 500 }}>
              {displayName || '加载中...'}
            </Typography>
          </Box>

          <Menu
            id="menu-appbar"
            anchorEl={anchorEl}
            anchorOrigin={{
              vertical: 'bottom',
              horizontal: 'right',
            }}
            keepMounted
            transformOrigin={{
              vertical: 'top',
              horizontal: 'right',
            }}
            open={Boolean(anchorEl)}
            onClose={handleMenuClose}
            slotProps={{
              paper: {
                sx: {
                  mt: 1.5,
                  minWidth: 180,
                  border: '1px solid',
                  borderColor: 'grey.200',
                  boxShadow: '0 4px 12px rgba(0,0,0,0.1)',
                },
              },
            }}
          >
            <MenuItem onClick={() => { navigate('/profile'); handleMenuClose() }}>
              <ListItemIcon><AccountCircle fontSize="small" /></ListItemIcon>
              <ListItemText>个人中心</ListItemText>
            </MenuItem>
            <MenuItem onClick={() => { navigate('/settings'); handleMenuClose() }}>
              <ListItemIcon><Settings fontSize="small" /></ListItemIcon>
              <ListItemText>系统设置</ListItemText>
            </MenuItem>
            <Divider sx={{ borderColor: 'grey.200' }} />
            <MenuItem onClick={handleLogout}>
              <ListItemIcon><Logout fontSize="small" /></ListItemIcon>
              <ListItemText>退出登录</ListItemText>
            </MenuItem>
          </Menu>
        </Box>
      </Toolbar>
    </AppBar>
  )
}
