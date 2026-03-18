import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Box, List, ListItem, ListItemButton, ListItemIcon, ListItemText, Drawer, Typography, Collapse, Divider, Link } from '@mui/material'
import Dashboard from '@mui/icons-material/Dashboard'
import People from '@mui/icons-material/People'
import TaskAlt from '@mui/icons-material/TaskAlt'
import Verified from '@mui/icons-material/Verified'
import Radio from '@mui/icons-material/Radio'
import Dns from '@mui/icons-material/Dns'
import Settings from '@mui/icons-material/Settings'
import ArrowBack from '@mui/icons-material/ArrowBack'
import ExitToApp from '@mui/icons-material/ExitToApp'
import Devices from '@mui/icons-material/Devices'
import Group from '@mui/icons-material/Group'
import Mic from '@mui/icons-material/Mic'
import ExpandMore from '@mui/icons-material/ExpandMore'
import ExpandLess from '@mui/icons-material/ExpandLess'
import LinkIcon from '@mui/icons-material/Link'
import Folder from '@mui/icons-material/Folder'
import { useState, useEffect } from 'react'
import { authService, apiClient } from '../../services'
import { Header } from './Header'
import { usePageTitle } from '../../hooks/usePageTitle'

const DRAWER_WIDTH = 240

interface MenuItem {
  path: string
  label: string
  icon: React.ReactNode
  children?: MenuItem[]
}

// 管理员菜单项
const adminMenuItems: MenuItem[] = [
  { path: '/admin/dashboard', label: '系统数据', icon: <Dashboard /> },
  {
    path: '/admin/users',
    label: '用户管理',
    icon: <People />,
    children: [
      { path: '/admin/users', label: '账号管理', icon: <People /> },
      { path: '/admin/approvals', label: '用户审批', icon: <Verified /> },
      { path: '/admin/certificate-approvals', label: '操作证审批', icon: <TaskAlt /> },
    ]
  },
  {
    path: '/admin/devices',
    label: '设备管理',
    icon: <Devices />,
    children: [
      { path: '/admin/devices', label: '客户端', icon: <Devices /> },
      { path: '/admin/relays', label: '中继台', icon: <Radio /> },
      { path: '/admin/servers', label: '服务器', icon: <Dns /> },
    ]
  },
  {
    path: '/admin/groups',
    label: '群组管理',
    icon: <Group />,
    children: [
      { path: '/admin/groups', label: '普通群组', icon: <Group /> },
      { path: '/admin/group-links', label: '互联管理', icon: <LinkIcon /> },
    ]
  },
  { path: '/admin/comm-records', label: '通信记录', icon: <Mic /> },
  { path: '/admin/assets', label: '资源管理', icon: <Folder /> },
  { path: '/admin/settings', label: '站点配置', icon: <Settings /> },
]

export function AdminLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)
  // 用户管理菜单的展开/折叠状态
  const [userMenuExpanded, setUserMenuExpanded] = useState(false)
  // 设备管理菜单的展开/折叠状态
  const [deviceMenuExpanded, setDeviceMenuExpanded] = useState(false)
  // 群组管理菜单的展开/折叠状态
  const [groupMenuExpanded, setGroupMenuExpanded] = useState(false)
  const [icp, setIcp] = useState('')

  // 同步页面标题
  usePageTitle()

  // 页面加载时刷新用户信息，确保审核状态等是最新的
  useEffect(() => {
    authService.refreshUserInfo()
  }, [])

  useEffect(() => {
    const fetchICP = async () => {
      try {
        const res = await apiClient.get<any>('/api/config/public')
        if (res.code === 200 && res.data?.icp?.icp) {
          setIcp(res.data.icp.icp)
        }
      } catch (err) {
        console.error('Failed to fetch ICP config:', err)
      }
    }
    fetchICP()

    const handleConfigUpdate = () => {
      fetchICP()
    }
    window.addEventListener('config-updated', handleConfigUpdate)
    return () => {
      window.removeEventListener('config-updated', handleConfigUpdate)
    }
  }, [])

  // 当路由变化时，如果焦点不在子菜单上，自动折叠
  useEffect(() => {
    const userPaths = ['/admin/users', '/admin/approvals', '/admin/certificate-approvals']
    const devicePaths = ['/admin/devices', '/admin/relays', '/admin/servers']
    const groupPaths = ['/admin/groups', '/admin/group-links']

    // 如果当前路径不在用户管理子菜单下，折叠
    if (!userPaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/'))) {
      setUserMenuExpanded(false)
    }

    // 如果当前路径不在设备管理子菜单下，折叠
    if (!devicePaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/'))) {
      setDeviceMenuExpanded(false)
    }

    // 如果当前路径不在群组管理子菜单下，折叠
    if (!groupPaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/'))) {
      setGroupMenuExpanded(false)
    }
  }, [location.pathname])

  const handleDrawerToggle = () => {
    setMobileOpen(!mobileOpen)
  }

  const handleNavigate = (path: string) => {
    navigate(path)
    setMobileOpen(false)
  }

  const handleLogout = async () => {
    await authService.logout()
    window.location.href = '/login'
  }

  const handleBackToMain = () => {
    navigate('/dashboard')
  }

  // 切换用户管理菜单展开/折叠
  const toggleUserMenu = () => {
    setUserMenuExpanded(!userMenuExpanded)
  }

  // 切换设备管理菜单展开/折叠
  const toggleDeviceMenu = () => {
    setDeviceMenuExpanded(!deviceMenuExpanded)
  }

  // 切换群组管理菜单展开/折叠
  const toggleGroupMenu = () => {
    setGroupMenuExpanded(!groupMenuExpanded)
  }

  const isActive = (path: string) => {
    return location.pathname === path || location.pathname.startsWith(path + '/')
  }

  // 判断是否有子菜单处于活动状态
  const isParentActive = (item: MenuItem) => {
    if (!item.children) return false
    return item.children.some(child => location.pathname === child.path || location.pathname.startsWith(child.path + '/'))
  }

  // 判断菜单项是否应该高亮：有子菜单的父菜单，只有在直接访问父路径时才高亮
  const shouldHighlight = (item: MenuItem) => {
    if (item.children) {
      // 有子菜单时，只有直接访问父路径才高亮父菜单
      return location.pathname === item.path
    }
    return isActive(item.path)
  }

  const drawer = (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ p: 2, borderBottom: '1px solid', borderColor: 'divider' }}>
        <Typography variant="h6" sx={{ fontWeight: 600, color: '#1565C0' }}>
          后台管理
        </Typography>
        <Typography variant="caption" sx={{ color: 'text.secondary' }}>
          管理员专用
        </Typography>
      </Box>
      <List sx={{ flex: 1, py: 1 }}>
        {adminMenuItems.map((item) => {
          const isUserMenu = item.path === '/admin/users'
          const isDeviceMenu = item.path === '/admin/devices'
          const isGroupMenu = item.path === '/admin/groups'
          const isExpandableMenu = isUserMenu || isDeviceMenu || isGroupMenu
          const getMenuExpanded = () => {
            if (isUserMenu) return userMenuExpanded
            if (isDeviceMenu) return deviceMenuExpanded
            if (isGroupMenu) return groupMenuExpanded
            return false
          }
          const showChildren = isExpandableMenu ? getMenuExpanded() : (isActive(item.path) || isParentActive(item))
          const toggleMenu = () => {
            if (isUserMenu) toggleUserMenu()
            if (isDeviceMenu) toggleDeviceMenu()
            if (isGroupMenu) toggleGroupMenu()
          }

          return (
            <Box key={item.path}>
              {/* 父菜单 */}
              <ListItem disablePadding sx={{ mb: 0.5 }}>
                <ListItemButton
                  selected={shouldHighlight(item)}
                  onClick={() => isExpandableMenu ? toggleMenu() : handleNavigate(item.path)}
                  sx={{
                    mx: 1,
                    borderRadius: 2,
                    '&.Mui-selected': {
                      bgcolor: '#E3F2FD',
                      '&:hover': { bgcolor: '#BBDEFB' },
                      '& .MuiListItemIcon-root': { color: '#1565C0' },
                    },
                    '&:hover': { bgcolor: 'action.hover' },
                  }}
                >
                  <ListItemIcon sx={{ minWidth: 40 }}>
                    {item.icon}
                  </ListItemIcon>
                  <ListItemText
                    primary={item.label}
                    sx={{
                      '& .MuiTypography-root': { fontWeight: 500 },
                    }}
                  />
                  {item.children && (
                    getMenuExpanded() ? <ExpandLess sx={{ ml: 1 }} /> : <ExpandMore sx={{ ml: 1 }} />
                  )}
                </ListItemButton>
              </ListItem>

              {/* 子菜单 - 使用折叠动画 */}
              {item.children && (
                <Collapse in={showChildren} timeout="auto" unmountOnExit>
                  {/* 分隔线 */}
                  <Divider sx={{ mb: 1, borderColor: 'grey.300' }} />
                  <Box sx={{ pl: 2, mb: 1 }}>
                    {item.children.map((child, index) => (
                      <ListItem
                        key={`${item.path}-${index}`}
                        disablePadding
                        sx={{ mb: 0.25 }}
                      >
                        <ListItemButton
                          selected={location.pathname === child.path}
                          onClick={() => handleNavigate(child.path)}
                          sx={{
                            mx: 1,
                            borderRadius: 2,
                            '&.Mui-selected': {
                              bgcolor: '#E3F2FD',
                              '&:hover': { bgcolor: '#BBDEFB' },
                              '& .MuiListItemIcon-root': { color: '#1565C0' },
                            },
                            '&:hover': { bgcolor: 'action.hover' },
                          }}
                        >
                          <ListItemIcon sx={{ minWidth: 40 }}>
                            {child.icon}
                          </ListItemIcon>
                          <ListItemText
                            primary={child.label}
                            sx={{
                              '& .MuiTypography-root': { fontWeight: 500 },
                            }}
                          />
                        </ListItemButton>
                      </ListItem>
                    ))}
                  </Box>
                </Collapse>
              )}
            </Box>
          )
        })}
      </List>
      <Box sx={{ p: 2, borderTop: '1px solid', borderColor: 'divider' }}>
        <ListItemButton
          onClick={handleBackToMain}
          sx={{ borderRadius: 2, mb: 1 }}
        >
          <ListItemIcon sx={{ minWidth: 40 }}>
            <ArrowBack />
          </ListItemIcon>
          <ListItemText primary="返回主界面" />
        </ListItemButton>
        <ListItemButton
          onClick={handleLogout}
          sx={{ borderRadius: 2, color: 'error.main' }}
        >
          <ListItemIcon sx={{ minWidth: 40, color: 'error.main' }}>
            <ExitToApp />
          </ListItemIcon>
          <ListItemText primary="退出登录" />
        </ListItemButton>
        {icp && (
          <>
            <Divider sx={{ my: 1.5 }} />
            <Link
              href="http://beian.miit.gov.cn/"
              target="_blank"
              rel="noopener noreferrer"
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: 0.5,
                color: 'text.secondary',
                textDecoration: 'none',
                fontSize: '0.875rem',
                '&:hover': { color: 'text.primary' },
              }}
            >
              <Box
                component="img"
                src="//oss-fz.silverdragon.cn/loongapisources/picbed/penglong/2023/07/24/202307240118075832.png"
                alt="备案图标"
                sx={{ height: 21, width: 21 }}
              />
              {icp}
            </Link>
          </>
        )}
      </Box>
    </Box>
  )

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', minHeight: '100vh', bgcolor: 'grey.50' }}>
      {/* 顶部导航栏 - 复用前台 Header */}
      <Header onMenuClick={handleDrawerToggle} />

      {/* 中间核心区域 */}
      <Box sx={{ display: 'flex', flex: 1, mt: 8 }}>
        {/* 移动端抽屉 */}
        <Drawer
          variant="temporary"
          open={mobileOpen}
          onClose={handleDrawerToggle}
          ModalProps={{ keepMounted: true }}
          sx={{
            display: { xs: 'block', sm: 'none' },
            width: DRAWER_WIDTH,
            flexShrink: 0,
            '& .MuiDrawer-paper': {
              width: DRAWER_WIDTH,
              boxSizing: 'border-box',
            },
          }}
        >
          {drawer}
        </Drawer>
        {/* 桌面端抽屉 */}
        <Drawer
          variant="permanent"
          sx={{
            width: DRAWER_WIDTH,
            flexShrink: 0,
            whiteSpace: 'nowrap',
            display: { xs: 'none', sm: 'block' },
            '& .MuiDrawer-paper': {
              width: DRAWER_WIDTH,
              boxSizing: 'border-box',
              top: 64,
              height: 'calc(100vh - 64px)',
              zIndex: (theme) => theme.zIndex.drawer - 1,
              borderRight: '1px solid',
              borderColor: 'grey.200',
            },
          }}
          open
        >
          {drawer}
        </Drawer>

        {/* 主内容区域 */}
        <Box
          component="main"
          sx={{
            display: 'flex',
            flexDirection: 'column',
            flexGrow: 1,
            width: { sm: `calc(100% - ${DRAWER_WIDTH}px)` },
            overflowX: 'hidden',
          }}
        >
          <Box sx={{ p: { xs: 2, sm: 3 }, minHeight: '100vh', overflowX: 'hidden' }}>
            <Outlet />
          </Box>
        </Box>
      </Box>
    </Box>
  )
}

export { DRAWER_WIDTH }
