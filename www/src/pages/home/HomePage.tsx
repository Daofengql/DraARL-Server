import { Box, Typography, Button } from '@mui/material'
import Login from '@mui/icons-material/Login'
import PersonAdd from '@mui/icons-material/PersonAdd'
import { useNavigate } from 'react-router-dom'
import { usePageTitle } from '../../hooks/usePageTitle'
import { useConfig } from '../../contexts/ConfigContext'
import { SITE_CONFIG } from '../../config/site'
import { PublicPageLayout } from '../../components/layout'

export function HomePage() {
  const navigate = useNavigate()
  const { config } = useConfig()

  // 同步页面标题
  usePageTitle()

  const systemName = config?.systemInfo?.name || SITE_CONFIG.NAME

  return (
    <PublicPageLayout maxWidth="md">
      <Box sx={{ textAlign: 'center', py: 6 }}>
        <Typography
          variant="h3"
          sx={{
            fontWeight: 700,
            color: 'text.primary',
            mb: 2,
            fontSize: { xs: '2rem', sm: '2.5rem', md: '3rem' },
          }}
        >
          欢迎使用 {systemName}
        </Typography>
        <Typography
          variant="h6"
          color="text.secondary"
          sx={{ mb: 4, fontWeight: 400, maxWidth: 600, mx: 'auto' }}
        >
          业余无线电通信管理平台
        </Typography>
        <Box sx={{ display: 'flex', gap: 2, justifyContent: 'center', flexWrap: 'wrap' }}>
          <Button
            variant="contained"
            size="large"
            startIcon={<Login />}
            onClick={() => navigate('/login')}
            sx={{ px: 4, py: 1.5, textTransform: 'none', fontSize: '1rem' }}
          >
            立即登录
          </Button>
          <Button
            variant="outlined"
            size="large"
            startIcon={<PersonAdd />}
            onClick={() => navigate('/register')}
            sx={{ px: 4, py: 1.5, textTransform: 'none', fontSize: '1rem' }}
          >
            注册账号
          </Button>
        </Box>
      </Box>
    </PublicPageLayout>
  )
}
