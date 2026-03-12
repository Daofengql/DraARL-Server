import { useEffect, useState } from 'react'
import {
  Box,
  Card,
  CardContent,
  Typography,
  Paper,
  LinearProgress,
  Stack,
  Skeleton,
} from '@mui/material'
import {
  Devices,
  CheckCircle,
  Group,
  People,
  Radio,
  Dashboard as DashboardIcon,
  Person,
  Public,
} from '@mui/icons-material'
import { authService } from '../../services'
import { platformService } from '../../services/platform'
import { deviceService } from '../../services/device'
import { apiClient } from '../../services'

const DEFAULT_SITE_NAME = 'DraARL 麟云业余无线电链路平台'
const SYSTEM_NAME = 'DraARL 麟链'
const SYSTEM_VERSION = 'v1.0.0'
const PROTOCOL_VERSION = 'DraARLv1'

interface StatCardProps {
  title: string
  value: number | string
  icon: React.ReactNode
  color: 'primary' | 'success' | 'info' | 'warning'
}

function StatCard({ title, value, icon, color }: StatCardProps) {
  const colorConfig = {
    primary: { bg: 'primary.50', color: 'primary.main' },
    success: { bg: 'success.50', color: 'success.main' },
    info: { bg: 'info.50', color: 'info.main' },
    warning: { bg: 'warning.50', color: 'warning.main' },
  }

  const config = colorConfig[color]

  return (
    <Card>
      <CardContent>
        <Stack direction="row" justifyContent="space-between" alignItems="center">
          <Box>
            <Typography variant="body2" color="text.secondary">
              {title}
            </Typography>
            <Typography variant="h4" fontWeight={700} mt={1}>
              {typeof value === 'number' ? value.toLocaleString() : value}
            </Typography>
          </Box>
          <Box
            sx={{
              p: 2,
              borderRadius: 2,
              bgcolor: config.bg,
              color: config.color,
              display: 'flex',
            }}
          >
            {icon}
          </Box>
        </Stack>
      </CardContent>
    </Card>
  )
}

// 骨架屏
function DashboardSkeleton() {
  return (
    <Stack spacing={3}>
      <Skeleton variant="rectangular" height={80} />
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
          gap: 2,
        }}
      >
        {[1, 2, 3, 4].map((i) => (
          <Card key={i}>
            <CardContent>
              <Skeleton variant="text" width={80} sx={{ mb: 2 }} />
              <Skeleton variant="text" width={100} height={40} />
            </CardContent>
          </Card>
        ))}
      </Box>
    </Stack>
  )
}

export function AdminDashboardPage() {
  const [stats, setStats] = useState({
    total_devices: 0,
    online_devices: 0,
    total_groups: 0,
    total_users: 0,
  })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [platformInfo, setPlatformInfo] = useState({ name: '', version: SYSTEM_VERSION })
  const [systemConfig, setSystemConfig] = useState<any>(null)

  const fetchSystemStats = async () => {
    try {
      const [statsData, infoData, publicConfig] = await Promise.all([
        platformService.getTotalStats(),
        platformService.getInfo(),
        apiClient.get<any>('/api/config/public'),
      ])
      setStats({
        total_devices: statsData.total_devices || 0,
        online_devices: statsData.online_devices || 0,
        total_groups: statsData.total_groups || 0,
        total_users: statsData.total_users || 0,
      })
      setPlatformInfo(infoData)
      if (publicConfig.code === 200 && publicConfig.data) {
        setSystemConfig(publicConfig.data)
      }
    } catch (err) {
      setError('获取统计数据失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchSystemStats()
  }, [])

  // 站点名称：欢迎卡片使用配置的站点名称或默认值
  const siteName = systemConfig?.systemInfo?.name || DEFAULT_SITE_NAME

  if (loading) {
    return <DashboardSkeleton />
  }

  return (
    <Stack spacing={3}>
      {/* 欢迎信息卡片 */}
      <Card
        sx={{
          background: (theme) => `linear-gradient(135deg, #1565C0 0%, #0D47A1 100%)`,
          color: 'white',
          border: 'none',
          boxShadow: 3,
        }}
      >
        <CardContent>
          <Stack direction="row" alignItems="center" spacing={2} justifyContent="space-between">
            <Stack direction="row" alignItems="center" spacing={2}>
              <DashboardIcon sx={{ fontSize: 40 }} />
              <Box>
                <Typography variant="h5" fontWeight={600}>
                  后台管理系统
                </Typography>
                <Typography variant="body2" sx={{ color: 'rgba(255,255,255,0.8)', mt: 0.5 }}>
                  {siteName} - 系统数据
                </Typography>
              </Box>
            </Stack>
          </Stack>
        </CardContent>
      </Card>

      {error && (
        <Box>
          <Typography variant="body1" color="error">
            {error}
          </Typography>
        </Box>
      )}

      {/* 统计卡片 */}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
          gap: 2,
        }}
      >
        <StatCard
          title="总设备数"
          value={stats.total_devices}
          icon={<Devices />}
          color="primary"
        />
        <StatCard
          title="在线设备"
          value={stats.online_devices}
          icon={<CheckCircle />}
          color="success"
        />
        <StatCard
          title="群组数量"
          value={stats.total_groups}
          icon={<Group />}
          color="info"
        />
        <StatCard
          title="用户数量"
          value={stats.total_users}
          icon={<People />}
          color="warning"
        />
      </Box>

      {/* 详细信息面板 */}
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 3 }}>
        {/* 系统状态 */}
        <Paper variant="outlined" sx={{ p: 3 }}>
          <Stack direction="row" alignItems="center" spacing={1} mb={3}>
            <Radio color="primary" />
            <Typography variant="h6" fontWeight={600}>
              系统状态
            </Typography>
          </Stack>
          <Stack spacing={2}>
            <Box>
              <Stack direction="row" justifyContent="space-between" mb={1}>
                <Typography variant="body2" color="text.secondary">
                  设备在线率
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {stats.total_devices > 0
                    ? `${Math.round((stats.online_devices / stats.total_devices) * 100)}%`
                    : '0%'}
                </Typography>
              </Stack>
              <LinearProgress
                variant="determinate"
                value={stats.total_devices > 0 ? (stats.online_devices / stats.total_devices) * 100 : 0}
                sx={{
                  height: 8,
                  borderRadius: 4,
                  bgcolor: 'grey.200',
                  '& .MuiLinearProgress-bar': {
                    bgcolor:
                      stats.total_devices > 0 && stats.online_devices / stats.total_devices > 0.8
                        ? 'success.main'
                        : stats.online_devices / stats.total_devices > 0.5
                          ? 'warning.main'
                          : 'error.main',
                  },
                }}
              />
            </Box>
            <Box sx={{ pt: 1 }}>
              <Stack direction="row" justifyContent="space-between">
                <Typography variant="body2" color="text.secondary">
                  当前在线
                </Typography>
                <Typography variant="body1" fontWeight={500} color="success.main">
                  {stats.online_devices} / {stats.total_devices}
                </Typography>
              </Stack>
            </Box>
          </Stack>
        </Paper>

        {/* 系统信息 */}
        <Paper variant="outlined" sx={{ p: 3 }}>
          <Stack direction="row" alignItems="center" spacing={1} mb={3}>
            <Devices color="primary" />
            <Typography variant="h6" fontWeight={600}>
              系统信息
            </Typography>
          </Stack>
          <Stack spacing={2}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                系统名称
              </Typography>
              <Typography variant="body2" fontWeight={500}>
                {SYSTEM_NAME}
              </Typography>
            </Box>
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                系统版本
              </Typography>
              <Typography variant="body2" fontWeight={500}>
                {SYSTEM_VERSION}
              </Typography>
            </Box>
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                协议版本
              </Typography>
              <Typography variant="body2" fontWeight={500}>
                {PROTOCOL_VERSION}
              </Typography>
            </Box>
          </Stack>
        </Paper>
      </Box>
    </Stack>
  )
}
