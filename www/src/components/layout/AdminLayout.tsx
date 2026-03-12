import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Box, AppBar, Toolbar, Typography, IconButton, List, ListItem, ListItemButton, ListItemIcon, ListItemText, Drawer } from '@mui/material'
import { Menu, Dashboard, People, TaskAlt, Verified, Radio, Dns, Settings, ArrowBack, Logout, Devices, Group } from '@mui/icons-material'
import { useState } from 'react'
import { authService } from '../../services'

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
  { path: '/admin/devices', label: '设备管理', icon: <Devices /> },
  { path: '/admin/groups', label: '群组管理', icon: <Group /> },
  { path: '/admin/relays', label: '中继台', icon: <Radio /> },
  { path: '/admin/servers', label: '服务器', icon: <Dns /> },
  { path: '/admin/settings', label: '站点配置', icon: <Settings /> },
]

export function AdminLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)

  const handleDrawerToggle = () => {
    setMobileOpen(!mobileOpen)
  }

  const handleNavigate = (path: string) => {
    navigate(path)
    setMobileOpen(false)
  }

  const handleLogout = () => {
    authService.logout()
    navigate('/login')
  }

  const handleBackToMain = () => {
    navigate('/')
  }

  const isActive = (path: string) => {
    return location.pathname === path || location.pathname.startsWith(path + '/')
  }

  const isParentActive = (item: MenuItem) => {
    if (!item.children) return false
    return item.children.some(child => location.pathname === child.path || location.pathname.startsWith(child.path + '/'))
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
        {adminMenuItems.map((item) => (
          <Box key={item.path}>
            {/* 父菜单 */}
            <ListItem disablePadding sx={{ mb: 0.5 }}>
              <ListItemButton
                selected={isActive(item.path)}
                onClick={() => handleNavigate(item.path)}
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
              </ListItemButton>
            </ListItem>

            {/* 子菜单 - 只在父菜单相关时显示 */}
            {(isActive(item.path) || isParentActive(item)) && item.children && (
              <Box sx={{ pl: 2, mb: 0.5 }}>
                {item.children.map((child, index) => (
                  <ListItem
                    key={`${item.path}-${index}`}
                    disablePadding
                  >
                    <ListItemButton
                      selected={location.pathname === child.path || location.pathname.startsWith(child.path + '/')}
                      onClick={() => handleNavigate(child.path)}
                      sx={{
                        mx: 1,
                        borderRadius: 2,
                        bgcolor: '#F5F5F5',
                        '&.Mui-selected': {
                          bgcolor: '#E3F2FD',
                          '&:hover': { bgcolor: '#BBDEFB' },
                          '& .MuiListItemIcon-root': { color: '#1565C0' },
                        },
                        '&:hover': { bgcolor: 'grey.200' },
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
            )}
          </Box>
        ))}
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
            <Logout />
          </ListItemIcon>
          <ListItemText primary="退出登录" />
        </ListItemButton>
      </Box>
    </Box>
  )

  return (
    <Box sx={{ display: 'flex', height: '100vh' }}>
      {/* 顶部导航栏 */}
      <AppBar
        position="fixed"
        sx={{
          width: { sm: `calc(100% - ${DRAWER_WIDTH}px)` },
          ml: { sm: `${DRAWER_WIDTH}px` },
          bgcolor: '#1565C0',
        }}
      >
        <Toolbar>
          <IconButton
            color="inherit"
            aria-label="open drawer"
            edge="start"
            onClick={handleDrawerToggle}
            sx={{ mr: 2, display: { sm: 'none' } }}
          >
            <Menu />
          </IconButton>
          <Typography variant="h6" noWrap component="div" sx={{ flexGrow: 1 }}>
            后台管理系统
          </Typography>
        </Toolbar>
      </AppBar>

      {/* 侧边栏 */}
      <Box
        component="nav"
        sx={{ width: { sm: DRAWER_WIDTH }, flexShrink: { sm: 0 } }}
      >
        {/* 移动端抽屉 */}
        <Drawer
          variant="temporary"
          open={mobileOpen}
          onClose={handleDrawerToggle}
          ModalProps={{ keepMounted: true }}
          sx={{
            display: { xs: 'block', sm: 'none' },
            '& .MuiDrawer-paper': {
              boxSizing: 'border-box',
              width: DRAWER_WIDTH,
            },
          }}
        >
          {drawer}
        </Drawer>
        {/* 桌面端抽屉 */}
        <Drawer
          variant="permanent"
          sx={{
            display: { xs: 'none', sm: 'block' },
            '& .MuiDrawer-paper': {
              boxSizing: 'border-box',
              width: DRAWER_WIDTH,
            },
          }}
          open
        >
          {drawer}
        </Drawer>
      </Box>

      {/* 主内容区域 */}
      <Box
        component="main"
        sx={{
          flexGrow: 1,
          p: 3,
          width: { sm: `calc(100% - ${DRAWER_WIDTH}px)` },
          mt: 8,
          bgcolor: 'grey.50',
          minHeight: '100vh',
        }}
      >
        <Outlet />
      </Box>
    </Box>
  )
}

export { DRAWER_WIDTH }
