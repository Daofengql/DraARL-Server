import { Box, Container, Link } from '@mui/material'
import { PublicHeader } from './PublicHeader'
import { useConfig } from '../../contexts/ConfigContext'

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
    <Box sx={{ display: 'flex', flexDirection: 'column', minHeight: '100vh', bgcolor: 'background.default' }}>
      {/* 顶部导航栏 */}
      <PublicHeader />

      {/* 主内容区 */}
      <Box
        component="main"
        sx={{
          flex: 1,
          display: 'flex',
          alignItems: centered ? 'center' : 'flex-start',
          justifyContent: centered ? 'center' : 'flex-start',
          mt: 8,
          p: 3,
        }}
      >
        <Container maxWidth={maxWidth} sx={{ py: 4 }}>
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
            borderColor: 'grey.200',
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
            <Box
              component="img"
              src="//oss-fz.silverdragon.cn/loongapisources/picbed/penglong/2023/07/24/202307240118075832.png"
              alt="备案图标"
              sx={{ height: 18, width: 18 }}
            />
            {icp}
          </Link>
        </Box>
      )}
    </Box>
  )
}
