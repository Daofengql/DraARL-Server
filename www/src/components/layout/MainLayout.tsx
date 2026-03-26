import { useState, useEffect } from 'react'
import { Box } from '@mui/material'
import { PublicHeader } from './PublicHeader'
import { Sidebar, DRAWER_WIDTH } from './Sidebar'
import { Outlet } from 'react-router-dom'
import { usePageTitle } from '../../hooks/usePageTitle'
import { authService } from '../../services'

interface MainLayoutProps {
  children?: React.ReactNode
}

export function MainLayout({ children }: MainLayoutProps) {
  const [mobileOpen, setMobileOpen] = useState(false)

  // 同步页面标题
  usePageTitle()

  // 页面加载时刷新用户信息，确保审核状态等是最新的
  useEffect(() => {
    authService.refreshUserInfo()
  }, [])

  const handleDrawerToggle = () => {
    setMobileOpen(!mobileOpen)
  }

  return (
    /* 外层容器：设置为纵向排列，最小高度占满全屏 */
    <Box sx={{ display: 'flex', flexDirection: 'column', minHeight: '100vh', bgcolor: 'background.default' }}>

      {/* 顶部导航栏 */}
      <PublicHeader onMenuClick={handleDrawerToggle} />

      {/* 中间核心区域：包含侧边栏和主内容，flex: 1 会自动撑开剩余空间 */}
      <Box sx={{ display: 'flex', flex: 1 }}>
        {/* 移动端侧边栏 */}
        <Sidebar
          variant="temporary"
          open={mobileOpen}
          onClose={handleDrawerToggle}
          sx={{ display: { xs: 'block', sm: 'none' } }}
          ModalProps={{ keepMounted: true }}
        />

        {/* 桌面端侧边栏 */}
        <Sidebar
          variant="permanent"
          sx={{ display: { xs: 'none', sm: 'block' } }}
        />

        {/* 主内容区：宽度计算需扣除侧边栏宽度 */}
        <Box
          component="main"
          sx={{
            display: 'flex',
            flexDirection: 'column',
            flexGrow: 1,
            width: { sm: `calc(100% - ${DRAWER_WIDTH}px)` },
            mt: { xs: 8, sm: 8 },
            overflowX: 'hidden',
          }}
        >
          <Box sx={{ p: { xs: 2, sm: 3 }, flex: 1, overflowX: 'hidden' }}>
            {children || <Outlet />}
          </Box>
        </Box>
      </Box>
    </Box>
  )
}
