import { useState } from 'react'
import {
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Box,
  Typography,
  Drawer,
  Divider,
  Link,
  type DrawerProps,
} from '@mui/material'
import { useNavigate, useLocation } from 'react-router-dom'
import Dashboard from '@mui/icons-material/Dashboard'
import Devices from '@mui/icons-material/Devices'
import Group from '@mui/icons-material/Group'
import Person from '@mui/icons-material/Person'
import AdminPanelSettings from '@mui/icons-material/AdminPanelSettings'
import ExitToApp from '@mui/icons-material/ExitToApp'
import Mic from '@mui/icons-material/Mic'
import MenuBook from '@mui/icons-material/MenuBook'
import Radio from '@mui/icons-material/Radio'
import { authService } from '../../services'
import { useConfig } from '../../contexts/ConfigContext'

const DRAWER_WIDTH = 240

interface SidebarProps extends DrawerProps {
  onClose?: () => void
}

interface MenuItem {
  path: string
  label: string
  icon: React.ReactNode
  requireApproved?: boolean // 是否需要审核通过才显示
}

// 普通用户菜单项
// requireApproved: true 表示需要审核通过才显示
const menuItems: MenuItem[] = [
  { path: '/dashboard', label: '仪表盘', icon: <Dashboard /> },
  { path: '/radio', label: '在线收发', icon: <Radio />, requireApproved: true },
  { path: '/devices', label: '设备管理', icon: <Devices />, requireApproved: true },
  { path: '/groups', label: '群组管理', icon: <Group />, requireApproved: true },
  { path: '/profile', label: '个人中心', icon: <Person /> },
  { path: '/comm-records', label: '通信记录', icon: <Mic />, requireApproved: true },
  { path: '/docs', label: '技术支持', icon: <MenuBook /> },
]

// 1. 在参数中单独解构出 sx，防止它留在 ...props 中覆盖内部样式
export function Sidebar({ onClose, open, variant = 'permanent', sx, ...props }: SidebarProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const isAdmin = authService.isAdmin()
  const isApproved = authService.isApproved() // 检查用户是否已审核通过
  const { config } = useConfig()
  const icp = config.icp?.icp || ''

  const handleNavigate = (path: string) => {
    navigate(path)
    if (variant === 'temporary' && onClose) {
      onClose()
    }
  }

  const handleLogout = async () => {
    await authService.logout()
    window.location.href = '/login'
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
          DraARL
        </Typography>
      </Box>
      <List sx={{ flex: 1, py: 1 }}>
        {menuItems
          .filter((item) => !item.requireApproved || isApproved)
          .map((item) => (
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

  if (variant === 'temporary') {
    return (
      <Drawer
        variant="temporary"
        open={open}
        onClose={onClose}
        ModalProps={{ keepMounted: true }}
        // 2. 移动端抽屉：使用 MUI 的数组语法合并内部 sx 和外部传入的 sx
        sx={[
          {
            width: DRAWER_WIDTH,
            flexShrink: 0,
            '& .MuiDrawer-paper': {
              boxSizing: 'border-box',
              width: DRAWER_WIDTH,
            },
          },
          ...(Array.isArray(sx) ? sx : [sx ?? {}])
        ]}
        {...props}
      >
        {drawerContent}
      </Drawer>
    )
  }

  return (
    <Drawer
      variant="permanent"
      // 3. 桌面端抽屉：同样使用数组语法合并，确保 width 和传入的 display 规则共存
      sx={[
        {
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
        },
        ...(Array.isArray(sx) ? sx : [sx ?? {}])
      ]}
      {...props}
    >
      {drawerContent}
    </Drawer>
  )
}

export { DRAWER_WIDTH }
