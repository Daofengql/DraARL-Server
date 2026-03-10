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
    <Box sx={{ display: 'flex', minHeight: '100vh', bgcolor: 'background.default' }}>
      <Header onMenuClick={handleDrawerToggle} />

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

      {/* 主内容区 */}
      <Box
        component="main"
        sx={{
          flexGrow: 1,
          width: { sm: `calc(100% - ${DRAWER_WIDTH}px)` },
          mt: { xs: 8, sm: 0 }, // 移动端保留顶部边距，桌面端不需要
          ml: { sm: `${DRAWER_WIDTH}px` }, // 桌面端右边距
        }}
      >
        <Box sx={{ p: 3, mt: { sm: 8 } }}>
          {children || <Outlet />}
        </Box>
      </Box>
    </Box>
  )
}
