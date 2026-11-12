import { useState, useEffect } from 'react'
import { Box, Typography, Button, AppBar, Toolbar, Avatar, IconButton, Menu, MenuItem, Tooltip } from '@mui/material'
import DarkMode from '@mui/icons-material/DarkMode'
import LightMode from '@mui/icons-material/LightMode'
import MenuIcon from '@mui/icons-material/Menu'
import { useNavigate, useLocation, Link as RouterLink } from 'react-router-dom'
import { useConfig } from '../../contexts/ConfigContext'
import { useSiteLogo } from '../../hooks/useSiteLogo'
import { SITE_CONFIG } from '../../config/site'
import { authService } from '../../services'
import { useColorMode } from '../../contexts/ThemeContext'
import {
  AntennaIcon,
  DashboardIcon,
  DocsIcon,
  ForumIcon,
  HomeIcon,
  InfoIcon,
  LoginIcon,
  RegisterIcon,
  ToolsIcon,
} from '../icons/BlueprintIcons'

interface PublicHeaderProps {
  /** 是否显示菜单按钮（用于移动端侧边栏） */
  onMenuClick?: () => void
}

export function PublicHeader({ onMenuClick }: PublicHeaderProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const { config } = useConfig()
  const { mode, toggleMode } = useColorMode()
  const toggleLabel = mode === 'dark' ? '切换到日间模式' : '切换到黑夜模式'
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

  const logoUrl = useSiteLogo()
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
    { label: '首页', path: '/', icon: <HomeIcon /> },
    { label: '中继查询', path: '/relays', icon: <AntennaIcon /> },
    { label: '工具', path: '/tools', icon: <ToolsIcon /> },
    { label: '技术交流', path: '/forum', icon: <ForumIcon /> },
    { label: '文档', path: '/docs', icon: <DocsIcon /> },
    { label: '关于', path: '/about', icon: <InfoIcon /> },
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
        bgcolor: 'background.paper',
        color: 'text.primary',
        borderBottom: '1px solid',
        borderColor: 'divider',
        zIndex: (theme) => ({ xs: theme.zIndex.appBar, md: theme.zIndex.drawer + 1 }),
      }}
    >
      <Box component="a" href="#main-content" sx={{
        position: 'absolute', left: 16, top: -100, px: 2, py: 1,
        bgcolor: 'primary.main', color: 'primary.contrastText',
        '&:focus': { top: 8 }, zIndex: 1,
      }}>
        跳至主要内容
      </Box>
      <Toolbar sx={{ height: 64, px: { xs: 1.5, sm: 3 }, gap: { xs: 1, sm: 2 } }}>
        {/* 移动端侧边栏菜单按钮 - 仅在有侧边栏时显示，放在左侧 */}
        {onMenuClick && (
          <IconButton
            onClick={onMenuClick}
            aria-label="打开控制台菜单"
            sx={{
              display: { xs: 'flex', md: 'none' },
              color: 'text.secondary',
              p: 1,
            }}
          >
            <MenuIcon />
          </IconButton>
        )}

        {/* Logo */}
        <Box
          component={RouterLink}
          to="/"
          aria-label={systemName + ' 首页'}
          sx={{
            display: 'flex',
            alignItems: 'center',
            textDecoration: 'none',
            flexShrink: 1,
            minWidth: 0,
            maxWidth: { xs: 104, sm: 220 },
            gap: 1.5,
            overflow: 'hidden',
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
            <>
              <Box sx={{ display: { xs: 'none', sm: 'flex' }, alignItems: 'center', justifyContent: 'center', width: 34, height: 34, border: '1px solid', borderColor: 'secondary.main', color: 'secondary.main', flexShrink: 0 }}>
                <AntennaIcon sx={{ fontSize: 21 }} />
              </Box>
              <Typography
                component="div"
                variant="h6"
                sx={{
                  fontWeight: 600,
                  color: 'text.primary',
                  fontSize: { xs: '1rem', sm: '1.25rem' },
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {systemName}
              </Typography>
            </>
          )}
        </Box>

        {/* 导航链接 - 仅桌面端显示 */}
        <Box component="nav" aria-label="站点导航" sx={{ display: { xs: 'none', lg: 'flex' }, gap: 0.5, ml: 2 }}>
          {navItems.map((item) => (
            <Button
              key={item.path}
              startIcon={item.icon}
              component={RouterLink}
              to={item.path}
              aria-current={isActive(item.path) ? 'page' : undefined}
              sx={{
                textTransform: 'none',
                color: isActive(item.path) ? 'primary.main' : 'text.secondary',
                borderBottom: '2px solid',
                borderColor: isActive(item.path) ? 'primary.main' : 'transparent',
                bgcolor: 'transparent',
                '&:hover': {
                  bgcolor: 'action.hover',
                  color: 'primary.main',
                },
              }}
            >
              {item.label}
            </Button>
          ))}
        </Box>

        <Box sx={{ flexGrow: 1 }} />

        <Tooltip title={toggleLabel}>
          <IconButton onClick={toggleMode} aria-label={toggleLabel} sx={{ color: 'text.secondary', flexShrink: 0 }}>
            {mode === 'dark' ? <LightMode /> : <DarkMode />}
          </IconButton>
        </Tooltip>

        {/* 右侧操作区 */}
        {isAuthenticated ? (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: { xs: 1, sm: 2 } }}>
            {/* 控制台按钮 - 仅在非控制台页面显示 */}
            {!isInConsole && (
              <Button
                variant="contained"
                startIcon={<DashboardIcon />}
                onClick={() => navigate('/dashboard')}
                sx={{
                  textTransform: 'none',
                  display: { xs: 'none', lg: 'inline-flex' },
                }}
              >
                控制台
              </Button>
            )}
            {/* 移动端控制台图标按钮 */}
            {!isInConsole && (
              <IconButton
                onClick={() => navigate('/dashboard')}
                aria-label="打开控制台"
                sx={{ display: { xs: 'flex', lg: 'none' }, color: 'primary.main' }}
              >
                <DashboardIcon />
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
              aria-label="打开导航菜单"
              aria-haspopup="menu"
              aria-expanded={Boolean(navMenuAnchor)}
              aria-controls={navMenuAnchor ? 'site-navigation-menu' : undefined}
              sx={{ display: { xs: 'flex', lg: 'none' }, color: 'text.secondary', p: 1 }}
            >
              <MenuIcon />
            </IconButton>
          </Box>
        ) : (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Button
              variant="text"
              startIcon={<LoginIcon />}
              onClick={() => navigate('/login')}
              sx={{
                textTransform: 'none',
                display: { xs: 'none', lg: 'inline-flex' },
              }}
            >
              登录
            </Button>
            <Button
              variant="contained"
              onClick={() => navigate('/login')}
              sx={{
                textTransform: 'none',
                display: { xs: 'inline-flex', lg: 'none' },
                minWidth: 'auto',
                px: 1.5,
              }}
            >
              登录
            </Button>
            <Button
              variant="contained"
              startIcon={<RegisterIcon />}
              onClick={() => navigate('/register')}
              sx={{
                textTransform: 'none',
                display: { xs: 'none', lg: 'inline-flex' },
              }}
            >
              注册
            </Button>
            {/* 移动端导航菜单按钮 */}
            <IconButton
              onClick={handleNavMenuOpen}
              aria-label="打开导航菜单"
              aria-haspopup="menu"
              aria-expanded={Boolean(navMenuAnchor)}
              aria-controls={navMenuAnchor ? 'site-navigation-menu' : undefined}
              sx={{ display: { xs: 'flex', lg: 'none' }, color: 'text.secondary', p: 1 }}
            >
              <MenuIcon />
            </IconButton>
          </Box>
        )}
      </Toolbar>

      {/* 移动端导航菜单 */}
      <Menu
        id="site-navigation-menu"
        anchorEl={navMenuAnchor}
        open={Boolean(navMenuAnchor)}
        onClose={handleNavMenuClose}
        sx={{ display: { xs: 'block', lg: 'none' } }}
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
