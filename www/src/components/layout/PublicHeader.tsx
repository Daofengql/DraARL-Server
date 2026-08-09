import { useState, useEffect } from 'react'
import { Box, Typography, Button, AppBar, Toolbar, Avatar, IconButton, Menu, MenuItem } from '@mui/material'
import Login from '@mui/icons-material/Login'
import PersonAdd from '@mui/icons-material/PersonAdd'
import Dashboard from '@mui/icons-material/Dashboard'
import MenuBook from '@mui/icons-material/MenuBook'
import Info from '@mui/icons-material/Info'
import Forum from '@mui/icons-material/Forum'
import Home from '@mui/icons-material/Home'
import Build from '@mui/icons-material/Build'
import SettingsInputAntenna from '@mui/icons-material/SettingsInputAntenna'
import MenuIcon from '@mui/icons-material/Menu'
import { useNavigate, useLocation } from 'react-router-dom'
import { useConfig } from '../../contexts/ConfigContext'
import { SITE_CONFIG } from '../../config/site'
import { authService } from '../../services'

interface PublicHeaderProps {
  /** 是否显示菜单按钮（用于移动端侧边栏） */
  onMenuClick?: () => void
}

export function PublicHeader({ onMenuClick }: PublicHeaderProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const { config } = useConfig()
  const [user, setUser] = useState(authService.getStoredUser())
  const isAuthenticated = authService.isAuthenticated()
  const [navMenuAnchor, setNavMenuAnchor] = useState<null | HTMLElement>(null)

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

  const logoUrl = config?.systemInfo?.logo_url || ''
  const systemName = config?.systemInfo?.name || SITE_CONFIG.NAME
  const displayName = user?.nickname || user?.username || ''

  // 判断是否在控制台页面（包括用户控制台和管理员后台）
  const isInConsole = location.pathname.startsWith('/dashboard') ||
                       location.pathname.startsWith('/profile') ||
                       location.pathname.startsWith('/devices') ||
                       location.pathname.startsWith('/groups') ||
                       location.pathname.startsWith('/comm-records') ||
                       location.pathname.startsWith('/radio') ||
                       location.pathname.startsWith('/admin')

  // 导航项
  const navItems = [
    { label: '首页', path: '/', icon: <Home /> },
    { label: '中继查询', path: '/relays', icon: <SettingsInputAntenna /> },
    { label: '工具', path: '/tools', icon: <Build /> },
    { label: '技术交流', path: '/forum', icon: <Forum /> },
    { label: '文档', path: '/docs', icon: <MenuBook /> },
    { label: '关于', path: '/about', icon: <Info /> },
  ]

  const isActive = (path: string) => {
    return location.pathname === path
  }

  const handleNavMenuOpen = (event: React.MouseEvent<HTMLElement>) => {
    setNavMenuAnchor(event.currentTarget)
  }

  const handleNavMenuClose = () => {
    setNavMenuAnchor(null)
  }

  const handleNavClick = (path: string) => {
    navigate(path)
    handleNavMenuClose()
  }

  return (
    <AppBar
      position="fixed"
      elevation={0}
      sx={{
        bgcolor: '#ffffff',
        color: 'text.primary',
        borderBottom: '1px solid',
        borderColor: 'grey.200',
        zIndex: (theme) => theme.zIndex.drawer + 1,
      }}
    >
      <Toolbar sx={{ height: 64, px: { xs: 1.5, sm: 3 }, gap: { xs: 1, sm: 2 } }}>
        {/* 移动端侧边栏菜单按钮 - 仅在有侧边栏时显示，放在左侧 */}
        {onMenuClick && (
          <IconButton
            onClick={onMenuClick}
            sx={{
              display: { xs: 'flex', sm: 'none' },
              color: 'text.secondary',
              p: 1,
            }}
          >
            <MenuIcon />
          </IconButton>
        )}

        {/* Logo */}
        <Box
          onClick={() => navigate('/')}
          sx={{
            display: 'flex',
            alignItems: 'center',
            cursor: 'pointer',
            flexShrink: 0,
          }}
        >
          {logoUrl ? (
            <Box
              component="img"
              src={logoUrl}
              alt="Logo"
              sx={{ height: { xs: 36, sm: 48 }, maxWidth: { xs: 120, sm: 200 }, objectFit: 'contain' }}
            />
          ) : (
            <Typography
              variant="h6"
              sx={{
                fontWeight: 600,
                color: 'primary.main',
                fontSize: { xs: '1rem', sm: '1.25rem' },
                whiteSpace: 'nowrap',
              }}
            >
              {systemName}
            </Typography>
          )}
        </Box>

        {/* 导航链接 - 仅桌面端显示 */}
        <Box sx={{ display: { xs: 'none', sm: 'flex' }, gap: 0.5, ml: 2 }}>
          {navItems.map((item) => (
            <Button
              key={item.path}
              startIcon={item.icon}
              onClick={() => navigate(item.path)}
              sx={{
                textTransform: 'none',
                color: isActive(item.path) ? 'primary.main' : 'text.secondary',
                bgcolor: isActive(item.path) ? 'primary.50' : 'transparent',
                '&:hover': {
                  bgcolor: isActive(item.path) ? 'primary.100' : 'action.hover',
                  color: 'primary.main',
                },
              }}
            >
              {item.label}
            </Button>
          ))}
        </Box>

        <Box sx={{ flexGrow: 1 }} />

        {/* 右侧操作区 */}
        {isAuthenticated ? (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: { xs: 1, sm: 2 } }}>
            {/* 控制台按钮 - 仅在非控制台页面显示 */}
            {!isInConsole && (
              <Button
                variant="contained"
                startIcon={<Dashboard />}
                onClick={() => navigate('/dashboard')}
                sx={{
                  textTransform: 'none',
                  display: { xs: 'none', sm: 'inline-flex' },
                }}
              >
                控制台
              </Button>
            )}
            {/* 移动端控制台图标按钮 */}
            {!isInConsole && (
              <IconButton
                onClick={() => navigate('/dashboard')}
                sx={{ display: { xs: 'flex', sm: 'none' }, color: 'primary.main' }}
              >
                <Dashboard />
              </IconButton>
            )}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Avatar
                src={user?.avatar_thumb || user?.avatar}
                alt={displayName}
                sx={{ width: 32, height: 32 }}
              >
                {displayName?.charAt(0).toUpperCase() || '?'}
              </Avatar>
              <Typography
                variant="body2"
                sx={{
                  fontWeight: 500,
                  display: { xs: 'none', md: 'block' },
                  maxWidth: 120,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {displayName}
              </Typography>
            </Box>
            {/* 移动端导航菜单按钮 - 放在右上角 */}
            <IconButton
              onClick={handleNavMenuOpen}
              sx={{ display: { xs: 'flex', sm: 'none' }, color: 'text.secondary', p: 1 }}
            >
              <MenuIcon />
            </IconButton>
          </Box>
        ) : (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Button
              variant="text"
              startIcon={<Login />}
              onClick={() => navigate('/login')}
              sx={{
                textTransform: 'none',
                display: { xs: 'none', sm: 'inline-flex' },
              }}
            >
              登录
            </Button>
            <Button
              variant="contained"
              onClick={() => navigate('/login')}
              sx={{
                textTransform: 'none',
                display: { xs: 'inline-flex', sm: 'none' },
                minWidth: 'auto',
                px: 1.5,
              }}
            >
              登录
            </Button>
            <Button
              variant="contained"
              startIcon={<PersonAdd />}
              onClick={() => navigate('/register')}
              sx={{
                textTransform: 'none',
                display: { xs: 'none', sm: 'inline-flex' },
              }}
            >
              注册
            </Button>
            {/* 移动端导航菜单按钮 */}
            <IconButton
              onClick={handleNavMenuOpen}
              sx={{ display: { xs: 'flex', sm: 'none' }, color: 'text.secondary', p: 1 }}
            >
              <MenuIcon />
            </IconButton>
          </Box>
        )}
      </Toolbar>

      {/* 移动端导航菜单 */}
      <Menu
        anchorEl={navMenuAnchor}
        open={Boolean(navMenuAnchor)}
        onClose={handleNavMenuClose}
        sx={{ display: { xs: 'block', sm: 'none' } }}
      >
        {navItems.map((item) => (
          <MenuItem
            key={item.path}
            onClick={() => handleNavClick(item.path)}
            selected={isActive(item.path)}
            sx={{ gap: 1.5 }}
          >
            {item.icon}
            {item.label}
          </MenuItem>
        ))}
      </Menu>
    </AppBar>
  )
}
