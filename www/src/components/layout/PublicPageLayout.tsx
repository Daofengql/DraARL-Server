import { Box, Container, Link } from '@mui/material'
import { PublicHeader } from './PublicHeader'
import { useConfig } from '../../contexts/ConfigContext'
import { blueprintGridSx } from '../../theme/blueprint'

interface PublicPageLayoutProps {
  children: React.ReactNode
  /** Container 最大宽度，默认为 'sm'（适合登录/注册表单），文档页面可使用 'lg' */
  maxWidth?: 'xs' | 'sm' | 'md' | 'lg' | 'xl' | false
  /** 是否居中显示内容，默认为 true（适合登录/注册表单），文档页面可设为 false */
  centered?: boolean
}

export function PublicPageLayout({ children, maxWidth = 'sm', centered = true }: PublicPageLayoutProps) {
  const { config } = useConfig()
  const icp = config.icp?.icp || ''

  return (
    <Box sx={[blueprintGridSx, { display: 'flex', flexDirection: 'column', minHeight: '100dvh' }]}>
      {/* 顶部导航栏 */}
      <PublicHeader />

      {/* 主内容区 */}
      <Box
        component="main"
        id="main-content"
        tabIndex={-1}
        sx={{
          flex: 1,
          display: 'flex',
          alignItems: centered ? 'center' : 'flex-start',
          justifyContent: centered ? 'center' : 'flex-start',
          mt: 8,
          p: { xs: 2, sm: 3 },
          minWidth: 0,
          scrollMarginTop: 80,
        }}
      >
        <Container maxWidth={maxWidth} sx={{ py: { xs: 2, sm: 3 }, px: { xs: 0, sm: 1 }, minWidth: 0 }}>
          {children}
        </Container>
      </Box>

      {/* 底部备案信息 */}
      {icp && (
        <Box
          component="footer"
          sx={{
            py: 2,
            textAlign: 'center',
            borderTop: '1px solid',
            borderColor: 'divider',
            bgcolor: 'background.paper',
          }}
        >
          <Link
            href="http://beian.miit.gov.cn/"
            target="_blank"
            rel="noopener noreferrer"
            sx={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 0.5,
              color: 'text.secondary',
              textDecoration: 'none',
              fontSize: '0.875rem',
              '&:hover': { color: 'text.primary' },
            }}
          >
            {icp}
          </Link>
        </Box>
      )}
    </Box>
  )
}
