import { useState } from 'react'
import { Box } from '@mui/material'
import { Header } from './Header'
import { Sidebar, DRAWER_WIDTH } from './Sidebar'
import { Outlet } from 'react-router-dom'

interface MainLayoutProps {
  children?: React.ReactNode
}

export function MainLayout({ children }: MainLayoutProps) {
  const [mobileOpen, setMobileOpen] = useState(false)

  const handleDrawerToggle = () => {
    setMobileOpen(!mobileOpen)
  }

  return (
    /* 外层容器：设置为纵向排列，最小高度占满全屏 */
    <Box sx={{ display: 'flex', flexDirection: 'column', minHeight: '100vh', bgcolor: 'background.default' }}>

      {/* 顶部导航栏 */}
      <Header onMenuClick={handleDrawerToggle} />

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
