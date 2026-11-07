import { useState, useEffect } from 'react'
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
  Collapse,
  type DrawerProps,
} from '@mui/material'
import { useNavigate, useLocation } from 'react-router-dom'
import ExpandMore from '@mui/icons-material/ExpandMore'
import ExpandLess from '@mui/icons-material/ExpandLess'
import { authService } from '../../services'
import { useConfig } from '../../contexts/ConfigContext'
import {
  AdminIcon,
  DashboardIcon,
  DeviceIcon,
  GroupIcon,
  LogIcon,
  LogoutIcon,
  ProfileIcon,
  RadioIcon,
  RecordIcon,
} from '../icons/BlueprintIcons'

const DRAWER_WIDTH = 240

interface SidebarProps extends DrawerProps {
  onClose?: () => void
}

interface MenuItem {
  path: string
  label: string
  icon: React.ReactNode
  requireApproved?: boolean // 是否需要审核通过才显示
  children?: MenuItem[] // 子菜单
}

// 普通用户菜单项
// requireApproved: true 表示需要审核通过才显示
const menuItems: MenuItem[] = [
  { path: '/dashboard', label: '仪表盘', icon: <DashboardIcon /> },
  { path: '/radio', label: '在线收发', icon: <RadioIcon />, requireApproved: true },
  { path: '/devices', label: '设备管理', icon: <DeviceIcon />, requireApproved: true },
  { path: '/groups', label: '群组管理', icon: <GroupIcon />, requireApproved: true },
  { path: '/profile', label: '个人中心', icon: <ProfileIcon /> },
  {
    path: '/comm-records',
    label: '通信记录',
    icon: <RecordIcon />,
    requireApproved: true,
    children: [
      { path: '/comm-records/platform', label: '平台发信记录', icon: <RecordIcon /> },
      { path: '/comm-records/logbook', label: '通联日志', icon: <LogIcon /> },
    ]
  },
]

// 1. 在参数中单独解构出 sx，防止它留在 ...props 中覆盖内部样式
export function Sidebar({ onClose, open, variant = 'permanent', sx, ...props }: SidebarProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const isAdmin = authService.isAdmin()
  const isApproved = authService.isApproved() // 检查用户是否已审核通过
  const { config } = useConfig()
  const icp = config.icp?.icp || ''

  // 通信记录菜单的展开/折叠状态
  const [commRecordsMenuExpanded, setCommRecordsMenuExpanded] = useState(false)

  // 当路由变化时，如果焦点不在子菜单上，自动折叠
  useEffect(() => {
    const commRecordsPaths = ['/comm-records/platform', '/comm-records/logbook']

    // 如果当前路径不在通信记录子菜单下，折叠
    if (!commRecordsPaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/'))) {
      setCommRecordsMenuExpanded(false)
    }
  }, [location.pathname])

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

  // 判断是否有子菜单处于活动状态
  const isParentActive = (item: MenuItem) => {
    if (!item.children) return false
    return item.children.some(child => location.pathname === child.path || location.pathname.startsWith(child.path + '/'))
  }

  // 判断菜单项是否应该高亮：有子菜单的父菜单，只有在直接访问父路径时才高亮
  const shouldHighlight = (item: MenuItem) => {
    if (item.children) {
      return location.pathname === item.path
    }
    return isActive(item.path)
  }

  // 切换通信记录菜单展开/折叠
  const toggleCommRecordsMenu = () => {
    setCommRecordsMenuExpanded(!commRecordsMenuExpanded)
  }

  const drawerContent = (
    <Box component="nav" aria-label="主导航" sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ p: 3, borderBottom: '1px solid', borderColor: 'divider' }}>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>DraARL / CONSOLE</Typography>
        <Typography component="div" variant="h6">我的工作台</Typography>
      </Box>
      <List sx={{ flex: 1, py: 1 }}>
        {menuItems
          .filter((item) => !item.requireApproved || isApproved)
          .map((item) => {
            const isCommRecordsMenu = item.path === '/comm-records'
            const isExpandableMenu = isCommRecordsMenu
            const showChildren = isExpandableMenu ? commRecordsMenuExpanded : (isActive(item.path) || isParentActive(item))
            const toggleMenu = () => {
              if (isCommRecordsMenu) toggleCommRecordsMenu()
            }

            return (
              <Box component="li" key={item.path} sx={{ listStyle: 'none' }}>
                {/* 父菜单 */}
                <ListItem component="div" disablePadding sx={{ mb: 0.5 }}>
                  <ListItemButton
                    selected={shouldHighlight(item)}
                    aria-expanded={item.children ? showChildren : undefined}
                    onClick={() => isExpandableMenu ? toggleMenu() : handleNavigate(item.path)}
                    sx={{
                      mx: 1,
                      borderRadius: 0,
                      '&.Mui-selected': {
                        bgcolor: 'action.selected',
                        '&:hover': { bgcolor: 'action.hover' },
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
                    {item.children && (
                      commRecordsMenuExpanded ? <ExpandLess sx={{ ml: 1 }} /> : <ExpandMore sx={{ ml: 1 }} />
                    )}
                  </ListItemButton>
                </ListItem>

                {/* 子菜单 - 使用折叠动画 */}
                {item.children && (
                  <Collapse in={showChildren} timeout={0} unmountOnExit>
                    <Divider sx={{ mb: 1, mx: 2, borderColor: 'divider' }} />
                    <List disablePadding sx={{ pl: 2, mb: 1 }}>
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
                              borderRadius: 0,
                              '&.Mui-selected': {
                                bgcolor: 'action.selected',
                                '&:hover': { bgcolor: 'action.hover' },
                                '& .MuiListItemIcon-root': { color: 'primary.main' },
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
                    </List>
                  </Collapse>
                )}
              </Box>
            )
          })}
      </List>
      <Box sx={{ p: 2, borderTop: '1px solid', borderColor: 'divider' }}>
        {isAdmin && (
          <ListItemButton
            onClick={handleAdminPanel}
            sx={{ borderRadius: 0, mb: 1, color: 'secondary.main' }}
          >
            <ListItemIcon sx={{ minWidth: 40, color: 'secondary.main' }}>
              <AdminIcon />
            </ListItemIcon>
            <ListItemText primary="后台管理" />
          </ListItemButton>
        )}
        <ListItemButton
          onClick={handleLogout}
          sx={{ borderRadius: 0, color: 'error.main' }}
        >
          <ListItemIcon sx={{ minWidth: 40, color: 'error.main' }}>
            <LogoutIcon />
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
            top: 64,
            height: 'calc(100dvh - 64px)',
            zIndex: (theme) => theme.zIndex.drawer - 1,
            borderRight: '1px solid',
            borderColor: 'divider',
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
