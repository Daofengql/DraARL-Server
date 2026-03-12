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
} from '@mui/material'
import { useNavigate, useLocation } from 'react-router-dom'
import {
  Dashboard,
  Devices,
  Group,
  Person,
  AdminPanelSettings,
  ExitToApp,
} from '@mui/icons-material'
import { authService } from '../../services'

const DRAWER_WIDTH = 240

interface SidebarProps extends DrawerProps {
  onClose?: () => void
}

interface MenuItem {
  path: string
  label: string
  icon: React.ReactNode
}

// 普通用户菜单项（管理员和普通用户都可见）
const menuItems: MenuItem[] = [
  { path: '/', label: '仪表盘', icon: <Dashboard /> },
  { path: '/devices', label: '设备管理', icon: <Devices /> },
  { path: '/groups', label: '群组管理', icon: <Group /> },
  { path: '/profile', label: '个人中心', icon: <Person /> },
]

export function Sidebar({ onClose, open, variant = 'permanent', ...props }: SidebarProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const isAdmin = authService.isAdmin()

  const handleNavigate = (path: string) => {
    navigate(path)
    if (variant === 'temporary' && onClose) {
      onClose()
    }
  }

  const handleLogout = () => {
    authService.logout()
    navigate('/login')
  }

  const handleAdminPanel = () => {
    navigate('/admin')
    if (variant === 'temporary' && onClose) {
      onClose()
    }
  }

  const isActive = (path: string) => {
    return location.pathname === path || location.pathname.startsWith(path + '/')
  }

  const drawerContent = (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ p: 2 }}>
        <Typography variant="h6" sx={{ fontWeight: 600, color: 'primary.main' }}>
          NRLLink
        </Typography>
      </Box>
      <List sx={{ flex: 1, py: 1 }}>
        {menuItems.map((item) => (
          <ListItem key={item.path} disablePadding sx={{ mb: 0.5 }}>
            <ListItemButton
              selected={isActive(item.path)}
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
        ))}
      </List>
      <Box sx={{ p: 2, borderTop: '1px solid', borderColor: 'divider' }}>
        {isAdmin && (
          <ListItemButton
            onClick={handleAdminPanel}
            sx={{ borderRadius: 2, mb: 1, color: 'secondary.main' }}
          >
            <ListItemIcon sx={{ minWidth: 40, color: 'secondary.main' }}>
              <AdminPanelSettings />
            </ListItemIcon>
            <ListItemText primary="后台管理" />
          </ListItemButton>
        )}
        <ListItemButton
          onClick={handleLogout}
          sx={{ borderRadius: 2, color: 'error.main' }}
        >
          <ListItemIcon sx={{ minWidth: 40, color: 'error.main' }}>
            <ExitToApp />
          </ListItemIcon>
          <ListItemText primary="退出登录" />
        </ListItemButton>
      </Box>
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
