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
  People,
  Radio,
  Dns,
  Description,
} from '@mui/icons-material'

const DRAWER_WIDTH = 240

interface SidebarProps extends DrawerProps {
  onClose?: () => void
}

const menuItems = [
  { path: '/', label: '仪表盘', icon: <Dashboard /> },
  { path: '/devices', label: '设备管理', icon: <Devices /> },
  { path: '/groups', label: '群组管理', icon: <Group /> },
  { path: '/users', label: '用户管理', icon: <People /> },
  { path: '/relays', label: '中继台', icon: <Radio /> },
  { path: '/servers', label: '服务器', icon: <Dns /> },
  { path: '/logs', label: '操作日志', icon: <Description /> },
]

export function Sidebar({ onClose, open, variant = 'permanent', ...props }: SidebarProps) {
  const navigate = useNavigate()
  const location = useLocation()

  const handleNavigate = (path: string) => {
    navigate(path)
    if (variant === 'temporary' && onClose) {
      onClose()
    }
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
              selected={location.pathname === item.path || location.pathname.startsWith(item.path + '/')}
              onClick={() => handleNavigate(item.path)}
              sx={{
                mx: 1,
                borderRadius: 2,
                '&.Mui-selected': {
                  bgcolor: 'primary.50',
                  '&:hover': {
                    bgcolor: 'primary.100',
                  },
                  '& .MuiListItemIcon-root': {
                    color: 'primary.main',
                  },
                },
                '&:hover': {
                  bgcolor: 'action.hover',
                },
              }}
            >
              <ListItemIcon sx={{ minWidth: 40 }}>
                {item.icon}
              </ListItemIcon>
              <ListItemText
                primary={item.label}
                sx={{
                  '& .MuiTypography-root': {
                    fontWeight: 500,
                  },
                }}
              />
            </ListItemButton>
          </ListItem>
        ))}
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
