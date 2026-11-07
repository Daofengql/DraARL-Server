import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Box, List, ListItem, ListItemButton, ListItemIcon, ListItemText, Drawer, Typography, Collapse, Divider, Link } from '@mui/material'
import ArrowBack from '@mui/icons-material/ArrowBack'
import ExpandMore from '@mui/icons-material/ExpandMore'
import ExpandLess from '@mui/icons-material/ExpandLess'
import { useState, useEffect } from 'react'
import { authService } from '../../services'
import { PublicHeader } from './PublicHeader'
import { usePageTitle } from '../../hooks/usePageTitle'
import { useConfig } from '../../contexts/ConfigContext'
import { blueprintGridSx } from '../../theme/blueprint'
import {
  CheckIcon,
  DashboardIcon,
  DeviceIcon,
  DocsIcon,
  DownloadIcon,
  FolderIcon,
  GroupIcon,
  HubIcon,
  LinkIcon as BlueprintLinkIcon,
  LogoutIcon,
  RadioIcon,
  RecordIcon,
  SettingsIcon,
  UsersIcon,
} from '../icons/BlueprintIcons'

const DRAWER_WIDTH = 240

interface MenuItem {
  path: string
  label: string
  icon: React.ReactNode
  children?: MenuItem[]
}

// 管理员菜单项
const adminMenuItems: MenuItem[] = [
  { path: '/admin/dashboard', label: '系统数据', icon: <DashboardIcon /> },
  {
    path: '/admin/users',
    label: '用户管理',
    icon: <UsersIcon />,
    children: [
      { path: '/admin/users', label: '账号管理', icon: <UsersIcon /> },
      { path: '/admin/approvals', label: '用户审批', icon: <CheckIcon /> },
      { path: '/admin/certificate-approvals', label: '操作证审批', icon: <CheckIcon /> },
    ]
  },
  {
    path: '/admin/devices',
    label: '设备管理',
    icon: <DeviceIcon />,
    children: [
      { path: '/admin/devices', label: '客户端', icon: <DeviceIcon /> },
      { path: '/admin/radio-sessions', label: '移动端', icon: <HubIcon /> },
      { path: '/admin/relays', label: '中继台', icon: <RadioIcon /> },
    ]
  },
  {
    path: '/admin/client-resources',
    label: '资源分发',
    icon: <DownloadIcon />,
    children: [
      { path: '/admin/client-resources', label: '通用资源', icon: <DownloadIcon /> },
      { path: '/admin/firmware', label: '设备固件', icon: <DeviceIcon /> },
      { path: '/admin/assets', label: '公开资源', icon: <FolderIcon /> },
    ]
  },
  {
    path: '/admin/groups',
    label: '群组管理',
    icon: <GroupIcon />,
    children: [
      { path: '/admin/groups', label: '普通群组', icon: <GroupIcon /> },
      { path: '/admin/group-links', label: '互联管理', icon: <BlueprintLinkIcon /> },
      { path: '/admin/inter-center-links', label: '中心互联', icon: <HubIcon /> },
    ]
  },
  {
    path: '/admin/comm-records',
    label: '通信记录',
    icon: <RecordIcon />,
    children: [
      { path: '/admin/comm-records/platform', label: '平台发信记录', icon: <RecordIcon /> },
      { path: '/admin/comm-records/logbook', label: '通联日志', icon: <DocsIcon /> },
    ]
  },
  { path: '/admin/settings', label: '站点配置', icon: <SettingsIcon /> },
]

export function AdminLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)
  // 用户管理菜单的展开/折叠状态
  const [userMenuExpanded, setUserMenuExpanded] = useState(false)
  // 设备管理菜单的展开/折叠状态
  const [deviceMenuExpanded, setDeviceMenuExpanded] = useState(false)
  const [resourceMenuExpanded, setResourceMenuExpanded] = useState(false)
  // 群组管理菜单的展开/折叠状态
  const [groupMenuExpanded, setGroupMenuExpanded] = useState(false)
  // 通信记录菜单的展开/折叠状态
  const [commRecordsMenuExpanded, setCommRecordsMenuExpanded] = useState(false)
  const { config } = useConfig()
  const icp = config.icp?.icp || ''

  // 同步页面标题
  usePageTitle()

  // 页面加载时刷新用户信息，确保审核状态等是最新的
  useEffect(() => {
    authService.refreshUserInfo()
  }, [])

  // 当路由变化时，如果焦点不在子菜单上，自动折叠
  useEffect(() => {
    const userPaths = ['/admin/users', '/admin/approvals', '/admin/certificate-approvals']
    const devicePaths = ['/admin/devices', '/admin/radio-sessions', '/admin/relays']
    const resourcePaths = ['/admin/client-resources', '/admin/firmware', '/admin/assets']
    const groupPaths = ['/admin/groups', '/admin/group-links', '/admin/inter-center-links']
    const commRecordsPaths = ['/admin/comm-records/platform', '/admin/comm-records/logbook']

    // 如果当前路径不在用户管理子菜单下，折叠
    if (!userPaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/'))) {
      setUserMenuExpanded(false)
    }

    // 直接打开或刷新子页面时也展开对应菜单，确保当前入口可见。
    setDeviceMenuExpanded(devicePaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/')))
    setResourceMenuExpanded(resourcePaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/')))

    // 如果当前路径不在群组管理子菜单下，折叠
    if (!groupPaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/'))) {
      setGroupMenuExpanded(false)
    }

    // 如果当前路径不在通信记录子菜单下，折叠
    if (!commRecordsPaths.some(path => location.pathname === path || location.pathname.startsWith(path + '/'))) {
      setCommRecordsMenuExpanded(false)
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

  const toggleResourceMenu = () => {
    setResourceMenuExpanded(!resourceMenuExpanded)
  }

  // 切换群组管理菜单展开/折叠
  const toggleGroupMenu = () => {
    setGroupMenuExpanded(!groupMenuExpanded)
  }

  // 切换通信记录菜单展开/折叠
  const toggleCommRecordsMenu = () => {
    setCommRecordsMenuExpanded(!commRecordsMenuExpanded)
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
    <Box component="nav" aria-label="后台导航" sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ p: 2, borderBottom: '1px solid', borderColor: 'divider' }}>
        <Typography variant="caption" color="secondary.main" sx={{ display: 'block', mb: 0.75 }}>
          DraARL / ADMIN
        </Typography>
        <Typography component="div" variant="body2" sx={{ fontWeight: 600 }}>
          后台管理
        </Typography>
      </Box>
      <List sx={{ flex: 1, py: 1 }}>
        {adminMenuItems.map((item) => {
          const isUserMenu = item.path === '/admin/users'
          const isDeviceMenu = item.path === '/admin/devices'
          const isResourceMenu = item.path === '/admin/client-resources'
          const isGroupMenu = item.path === '/admin/groups'
          const isCommRecordsMenu = item.path === '/admin/comm-records'
          const isExpandableMenu = isUserMenu || isDeviceMenu || isResourceMenu || isGroupMenu || isCommRecordsMenu
          const getMenuExpanded = () => {
            if (isUserMenu) return userMenuExpanded
            if (isDeviceMenu) return deviceMenuExpanded
            if (isResourceMenu) return resourceMenuExpanded
            if (isGroupMenu) return groupMenuExpanded
            if (isCommRecordsMenu) return commRecordsMenuExpanded
            return false
          }
          const showChildren = isExpandableMenu ? getMenuExpanded() : (isActive(item.path) || isParentActive(item))
          const toggleMenu = () => {
            if (isUserMenu) toggleUserMenu()
            if (isDeviceMenu) toggleDeviceMenu()
            if (isResourceMenu) toggleResourceMenu()
            if (isGroupMenu) toggleGroupMenu()
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
                    getMenuExpanded() ? <ExpandLess sx={{ ml: 1 }} /> : <ExpandMore sx={{ ml: 1 }} />
                  )}
                </ListItemButton>
              </ListItem>

              {/* 子菜单 - 使用折叠动画 */}
              {item.children && (
                <Collapse in={showChildren} timeout={0} unmountOnExit>
                  {/* 分隔线 */}
                  <Divider sx={{ mb: 1, borderColor: 'divider' }} />
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
        <ListItemButton
          onClick={handleBackToMain}
          sx={{ borderRadius: 0, mb: 1 }}
        >
          <ListItemIcon sx={{ minWidth: 40 }}>
            <ArrowBack />
          </ListItemIcon>
          <ListItemText primary="返回主界面" />
        </ListItemButton>
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

  return (
    <Box sx={[blueprintGridSx, { display: 'flex', flexDirection: 'column', minHeight: '100dvh' }]}>
      {/* 顶部导航栏 - 复用公共 Header */}
      <PublicHeader onMenuClick={handleDrawerToggle} />

      {/* 中间核心区域 */}
      <Box sx={{ display: 'flex', flex: 1, mt: 8 }}>
        {/* 移动端抽屉 */}
        <Drawer
          variant="temporary"
          open={mobileOpen}
          onClose={handleDrawerToggle}
          ModalProps={{ keepMounted: true }}
          sx={{
            display: { xs: 'block', md: 'none' },
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
            display: { xs: 'none', md: 'block' },
            '& .MuiDrawer-paper': {
              width: DRAWER_WIDTH,
              boxSizing: 'border-box',
              top: 64,
              height: 'calc(100dvh - 64px)',
              zIndex: (theme) => theme.zIndex.drawer - 1,
              borderRight: '1px solid',
              borderColor: 'divider',
            },
          }}
          open
        >
          {drawer}
        </Drawer>

        {/* 主内容区域 */}
        <Box
          component="main"
          id="main-content"
          tabIndex={-1}
          sx={{
            display: 'flex',
            flexDirection: 'column',
            flexGrow: 1,
            minWidth: 0,
            scrollMarginTop: 80,
            width: { md: `calc(100% - ${DRAWER_WIDTH}px)` },
            overflowX: 'hidden',
          }}
        >
          <Box sx={{ p: { xs: 2, sm: 3, lg: 4 }, flex: 1, minWidth: 0, width: '100%', maxWidth: 1280, mx: 'auto' }}>
            <Outlet />
          </Box>
        </Box>
      </Box>
    </Box>
  )
}

export { DRAWER_WIDTH }
