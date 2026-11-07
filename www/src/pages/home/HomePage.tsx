import { Box, Typography, Button, Stack, Link, alpha } from '@mui/material'
import ArrowForward from '@mui/icons-material/ArrowForward'
import NorthEast from '@mui/icons-material/NorthEast'
import { Link as RouterLink } from 'react-router-dom'
import { usePageTitle } from '../../hooks/usePageTitle'
import { useConfig } from '../../contexts/ConfigContext'
import { SITE_CONFIG } from '../../config/site'
import { PublicPageLayout } from '../../components/layout'
import { NetworkBlueprint } from '../../components/home/NetworkBlueprint'
import { authService } from '../../services'
import { DeviceIcon, GroupIcon, RecordIcon } from '../../components/icons/BlueprintIcons'

const features = [
  { number: '01', icon: <DeviceIcon />, title: '设备接入', text: '管理设备和接入配置，查看设备在线状态。', path: '/devices' },
  { number: '02', icon: <GroupIcon />, title: '群组协作', text: '组织群组与互联关系，让通信按需连接。', path: '/groups' },
  { number: '03', icon: <RecordIcon />, title: '通联记录', text: '查看平台通信记录，整理自己的通联日志。', path: '/comm-records/platform' },
]

export function HomePage() {
  const { config } = useConfig()
  const authenticated = authService.isAuthenticated()

  // 同步页面标题
  usePageTitle()

  const systemName = config?.systemInfo?.name || SITE_CONFIG.NAME

  return (
    <PublicPageLayout maxWidth={false} centered={false}>
      <Box sx={{ maxWidth: 1280, mx: 'auto', width: '100%' }}>
        <Box component="section" aria-labelledby="home-title" sx={{
          display: 'grid',
          gridTemplateColumns: { xs: 'minmax(0, 1fr)', md: 'minmax(0, 1fr) minmax(0, 1.05fr)' },
          alignItems: 'center',
          gap: { xs: 5, md: 6, lg: 10 },
          py: { xs: 4, md: 8 },
        }}>
          <Box sx={{ minWidth: 0 }}>
            <Stack direction="row" spacing={1.5} alignItems="center" sx={{ mb: 3 }}>
              <Box aria-hidden="true" sx={{ width: 24, height: 2, bgcolor: 'secondary.main' }} />
              <Typography variant="caption" color="secondary.main" sx={{ letterSpacing: '0.06em', overflowWrap: 'anywhere' }}>
                {systemName} / RADIO NETWORK
              </Typography>
            </Stack>
            <Typography id="home-title" component="h1" sx={{
              fontSize: { xs: '2.25rem', sm: '3rem', lg: '3.75rem' },
              lineHeight: 1.35, fontWeight: 700, letterSpacing: '-0.045em', mb: 3,
            }}>
              连接设备，<br />
              <Box component="span" sx={{ color: 'secondary.main' }}>让通联更有序。</Box>
            </Typography>
            <Typography color="text.secondary" sx={{ maxWidth: 480, mb: 4, fontSize: { xs: '0.9375rem', sm: '1rem' }, lineHeight: 1.9, textWrap: 'pretty' }}>
              面向业余无线电的通信管理平台。<br />
              从设备接入、群组互联，到在线收发与通联记录，在一处完成。
            </Typography>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} sx={{ alignItems: { xs: 'stretch', sm: 'center' } }}>
              <Button component={RouterLink} to={authenticated ? '/dashboard' : '/login'} variant="contained" size="large" endIcon={<ArrowForward />}>
                {authenticated ? '进入控制台' : '登录控制台'}
              </Button>
              <Button component={RouterLink} to={authenticated ? '/docs' : '/register'} variant="outlined" size="large" sx={{ color: 'text.primary' }}>
                {authenticated ? '查看文档' : '注册账号'}
              </Button>
            </Stack>
            <Stack direction="row" spacing={2} sx={{ mt: 4, flexWrap: 'wrap', rowGap: 1 }}>
              {['DraARLv1', 'UDP', 'WebSocket'].map((label) => (
                <Typography key={label} variant="caption" color="text.secondary" sx={{ borderLeft: '1px solid', borderColor: 'divider', pl: 1.5 }}>
                  {label}
                </Typography>
              ))}
            </Stack>
          </Box>
          <NetworkBlueprint />
        </Box>

        <Box component="section" aria-labelledby="workspace-title" sx={{
          display: 'grid',
          gridTemplateColumns: { xs: 'minmax(0, 1fr)', md: 'minmax(0, 0.8fr) minmax(0, 1.5fr)' },
          gap: { xs: 3, md: 8 }, borderTop: '1px solid', borderColor: 'divider',
          py: { xs: 4, md: 6 },
        }}>
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 2 }}>工作空间 / WORKSPACE</Typography>
            <Typography id="workspace-title" component="h2" variant="h2" sx={{ mb: 2 }}>
              你的电台网络，<br />清晰可见。
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ maxWidth: '28ch', mb: 3 }}>
              设备、群组与记录各归其位，让每次操作都有明确入口。
            </Typography>
            <Link component={RouterLink} to="/docs" color="secondary.main" sx={{ display: 'inline-flex', alignItems: 'center', gap: 1, fontSize: '0.875rem' }}>
              阅读使用文档 <NorthEast sx={{ fontSize: 16 }} />
            </Link>
          </Box>
          <Box sx={{ minWidth: 0 }}>
            {features.map((feature) => (
              <Box key={feature.number} component={RouterLink} to={feature.path} sx={{
                display: 'grid', gridTemplateColumns: '28px minmax(0, 1fr) 20px',
                columnGap: { xs: 1.5, sm: 3 }, alignItems: 'center',
                color: 'text.primary', textDecoration: 'none',
                borderBottom: '1px solid', borderColor: 'divider',
                py: 2.5, px: { xs: 0, sm: 1.5 },
                '&:first-of-type': { borderTop: '1px solid', borderColor: 'divider' },
                '&:hover': { bgcolor: (theme) => alpha(theme.palette.secondary.main, 0.06), '& .feature-arrow': { color: 'primary.main' } },
                transition: 'background-color 150ms',
              }}>
                <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'start', pt: 0.5 }}>{feature.number}</Typography>
                <Box sx={{ minWidth: 0 }}>
                  <Stack direction="row" spacing={1.5} alignItems="center" sx={{ mb: 0.5 }}>
                    <Box sx={{ color: 'secondary.main', display: 'flex', '& svg': { fontSize: 20 } }}>{feature.icon}</Box>
                    <Typography component="h3" variant="h6">{feature.title}</Typography>
                  </Stack>
                  <Typography variant="body2" color="text.secondary">{feature.text}</Typography>
                </Box>
                <NorthEast className="feature-arrow" sx={{ fontSize: 18, color: 'text.secondary' }} />
              </Box>
            ))}
          </Box>
        </Box>
        <Stack component="footer" direction={{ xs: 'column', sm: 'row' }} spacing={2} justifyContent="space-between" sx={{ py: 3, borderTop: '1px solid', borderColor: 'divider' }}>
          <Typography variant="caption" color="text.secondary">DraARL / 业余无线电通信管理平台</Typography>
          <Stack direction="row" spacing={3}>
            <Link component={RouterLink} to="/relays" color="text.secondary" variant="caption">中继查询</Link>
            <Link component={RouterLink} to="/tools" color="text.secondary" variant="caption">在线工具</Link>
            <Link component={RouterLink} to="/about" color="text.secondary" variant="caption">关于平台</Link>
          </Stack>
        </Stack>
      </Box>
    </PublicPageLayout>
  )
}
