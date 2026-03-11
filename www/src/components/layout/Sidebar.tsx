import {
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Box,
  Typography,
  Drawer,
  type DrawerProps,
  Collapse,
} from '@mui/material'
import { useNavigate, useLocation } from 'react-router-dom'
import {
  Dashboard,
  Devices,
  Group,
  People,
  Radio,
  Dns,
  Description,
  TaskAlt,
  ExpandLess,
  ExpandMore,
  Verified,
  Settings,
} from '@mui/icons-material'
import { useState } from 'react'
import { authService } from '../../services'

const DRAWER_WIDTH = 240

interface SidebarProps extends DrawerProps {
  onClose?: () => void
}

interface MenuItem {
  path: string
  label: string
  icon: React.ReactNode
  adminOnly: boolean
  children?: MenuItem[]
}

// 定义所有菜单项，包含角色信息和子菜单
const menuItems: MenuItem[] = [
  { path: '/', label: '仪表盘', icon: <Dashboard />, adminOnly: false },
  { path: '/devices', label: '设备管理', icon: <Devices />, adminOnly: false },
  { path: '/groups', label: '群组管理', icon: <Group />, adminOnly: false },
  {
    path: '/users',
    label: '用户管理',
    icon: <People />,
    adminOnly: true,
    children: [
      { path: '/users', label: '用户列表', icon: <People />, adminOnly: true },
      { path: '/approvals', label: '用户审批', icon: <TaskAlt />, adminOnly: true },
      { path: '/certificate-approvals', label: '操作证审批', icon: <Verified />, adminOnly: true },
    ],
  },
  { path: '/relays', label: '中继台', icon: <Radio />, adminOnly: true },
  { path: '/servers', label: '服务器', icon: <Dns />, adminOnly: true },
  { path: '/logs', label: '操作日志', icon: <Description />, adminOnly: true },
  { path: '/settings', label: '站点配置', icon: <Settings />, adminOnly: true },
]

export function Sidebar({ onClose, open, variant = 'permanent', ...props }: SidebarProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const isAdmin = authService.isAdmin()
  const [expandedMenu, setExpandedMenu] = useState<string | null>(null)

  // 过滤菜单项：只显示用户有权限访问的菜单
  const visibleMenuItems = menuItems.filter(item => !item.adminOnly || isAdmin)

  const handleNavigate = (path: string) => {
    navigate(path)
    if (variant === 'temporary' && onClose) {
      onClose()
    }
  }

  const handleToggleMenu = (path: string) => {
    setExpandedMenu(expandedMenu === path ? null : path)
  }

  const isMenuActive = (item: MenuItem): boolean => {
    if (item.children) {
      return item.children.some(child => location.pathname === child.path || location.pathname.startsWith(child.path + '/'))
    }
    return location.pathname === item.path || location.pathname.startsWith(item.path + '/')
  }

  const renderMenuItem = (item: MenuItem) => {
    const hasChildren = item.children && item.children.length > 0
    const isExpanded = expandedMenu === item.path
    const isActive = isMenuActive(item)

    if (hasChildren) {
      const visibleChildren = item.children!.filter(child => !child.adminOnly || isAdmin)
      return (
        <Box key={item.path}>
          <ListItem disablePadding sx={{ mb: 0.5 }}>
            <ListItemButton
              onClick={() => handleToggleMenu(item.path)}
              sx={{
                mx: 1,
                borderRadius: 2,
                ...(isActive && {
                  bgcolor: 'primary.50',
                  '&:hover': { bgcolor: 'primary.100' },
                  '& .MuiListItemIcon-root': { color: 'primary.main' },
                }),
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
              {isExpanded ? <ExpandLess /> : <ExpandMore />}
            </ListItemButton>
          </ListItem>
          <Collapse in={isExpanded} timeout="auto" unmountOnExit>
            <List disablePadding sx={{ pl: 2 }}>
              {visibleChildren.map((child) => (
                <ListItem key={child.path} disablePadding sx={{ mb: 0.5 }}>
                  <ListItemButton
                    selected={location.pathname === child.path}
                    onClick={() => handleNavigate(child.path)}
                    sx={{
                      mx: 1,
                      borderRadius: 2,
                      '&.Mui-selected': {
                        bgcolor: 'primary.100',
                        '&:hover': { bgcolor: 'primary.200' },
                      },
                      '&:hover': { bgcolor: 'action.hover' },
                    }}
                  >
                    <ListItemIcon sx={{ minWidth: 32 }}>
                      {child.icon}
                    </ListItemIcon>
                    <ListItemText
                      primary={child.label}
                      sx={{
                        '& .MuiTypography-root': { fontSize: '0.875rem' },
                      }}
                    />
                  </ListItemButton>
                </ListItem>
              ))}
            </List>
          </Collapse>
        </Box>
      )
    }

    return (
      <ListItem key={item.path} disablePadding sx={{ mb: 0.5 }}>
        <ListItemButton
          selected={isActive}
          onClick={() => handleNavigate(item.path)}
          sx={{
            mx: 1,
            borderRadius: 2,
            '&.Mui-selected': {
              bgcolor: 'primary.50',
              '&:hover': { bgcolor: 'primary.100' },
              '& .MuiListItemIcon-root': { color: 'primary.main' },
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
    )
  }

  const drawerContent = (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ p: 2 }}>
        <Typography variant="h6" sx={{ fontWeight: 600, color: 'primary.main' }}>
          NRLLink
        </Typography>
      </Box>
      <List sx={{ flex: 1, py: 1 }}>
        {visibleMenuItems.map(renderMenuItem)}
      </List>
    </Box>
  )

  if (variant === 'temporary') {
    return (
      <Drawer
        variant="temporary"
        open={open}
        onClose={onClose}
        ModalProps={{ keepMounted: true }}
        sx={{
          width: DRAWER_WIDTH,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            boxSizing: 'border-box',
            width: DRAWER_WIDTH,
          },
        }}
        {...props}
      >
        {drawerContent}
      </Drawer>
    )
  }

  return (
    <Drawer
      variant="permanent"
      sx={{
        width: DRAWER_WIDTH,
        flexShrink: 0,
        whiteSpace: 'nowrap',
        '& .MuiDrawer-paper': {
          width: DRAWER_WIDTH,
          boxSizing: 'border-box',
          top: 0,
          height: '100vh',
          zIndex: (theme) => theme.zIndex.drawer - 1,
          borderRight: '1px solid',
          borderColor: 'grey.200',
        },
      }}
      {...props}
    >
      {drawerContent}
    </Drawer>
  )
}

export { DRAWER_WIDTH }
