import { useState } from 'react'
import { Box } from '@mui/material'
import { Header } from './Header'
import { Sidebar, DRAWER_WIDTH } from './Sidebar'
import { Footer } from './Footer'
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
          display: 'flex',
          flexDirection: 'column',
          flexGrow: 1,
          width: { sm: `calc(100% - ${DRAWER_WIDTH}px)` },
          mt: { xs: 8, sm: 0 },
        }}
      >
        <Box sx={{ p: 3, mt: { sm: 8 }, minHeight: 'calc(100vh - 64px)' }}>
          {children || <Outlet />}
        </Box>
        <Footer />
      </Box>
    </Box>
  )
}
