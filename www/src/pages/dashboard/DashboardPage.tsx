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
  ToggleButton,
  ToggleButtonGroup,
  Alert,
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

export function DashboardPage() {
  const [stats, setStats] = useState({
    total_devices: 0,
    online_devices: 0,
    total_groups: 0,
    total_users: 0,
  })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const user = authService.getStoredUser()
  const isAdmin = user?.isAdmin || user?.role === 'admin'
  // 普通用户默认看"我的数据"，管理员默认看"系统数据"
  const [viewType, setViewType] = useState<'system' | 'my'>(() => isAdmin ? 'system' : 'my')
  const [platformInfo, setPlatformInfo] = useState({ name: 'NRLLink', version: '1.0.0' })

  const fetchSystemStats = async () => {
    try {
      const [statsData, infoData] = await Promise.all([
        platformService.getTotalStats(),
        platformService.getInfo(),
      ])
      setStats({
        total_devices: statsData.total_devices || 0,
        online_devices: statsData.online_devices || 0,
        total_groups: statsData.total_groups || 0,
        total_users: statsData.total_users || 0,
      })
      setPlatformInfo(infoData)
    } catch (err) {
      setError('获取统计数据失败')
    } finally {
      setLoading(false)
    }
  }

  const fetchMyStats = async () => {
    try {
      // 获取与用户呼号匹配的设备
      const allDevices = await deviceService.list()
      const myDevices = user?.callsign
        ? allDevices.filter(d => d.callsign === user.callsign)
        : []

      // 获取所有群组和用户统计
      const [statsData, infoData] = await Promise.all([
        platformService.getTotalStats(),
        platformService.getInfo(),
      ])

      setStats({
        total_devices: myDevices.length,
        online_devices: myDevices.filter(d => d.is_online || d.online).length,
        total_groups: statsData.total_groups || 0,
        total_users: statsData.total_users || 0,
      })
      setPlatformInfo(infoData)
    } catch (err) {
      setError('获取统计数据失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (viewType === 'system' || !user?.callsign) {
      fetchSystemStats()
    } else {
      fetchMyStats()
    }
  }, [viewType])

  const displayName = user?.nickname || user?.username || '用户'
  const isMyView = viewType === 'my'

  if (loading) {
    return <DashboardSkeleton />
  }

  return (
    <Stack spacing={3}>
      {/* 欢迎信息卡片 */}
      <Card
        sx={{
          background: (theme) => `linear-gradient(135deg, ${theme.palette.primary.main} 0%, ${theme.palette.primary.dark} 100%)`,
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
                  欢迎回来，{displayName}！
                </Typography>
                <Typography variant="body2" sx={{ color: 'rgba(255,255,255,0.8)', mt: 0.5 }}>
                  {platformInfo.name} 业余无线电互联平台
                </Typography>
              </Box>
            </Stack>
            {isAdmin && (
              <ToggleButtonGroup
                value={viewType}
                exclusive
                onChange={(_, value) => value && setViewType(value)}
                sx={{ bgcolor: 'rgba(255,255,255,0.2)' }}
              >
                <ToggleButton value="system" sx={{ color: 'white', '&.Mui-selected': { bgcolor: 'rgba(255,255,255,0.3)' } }}>
                  <Public sx={{ fontSize: 16, mr: 0.5 }} />
                  系统数据
                </ToggleButton>
                <ToggleButton value="my" sx={{ color: 'white', '&.Mui-selected': { bgcolor: 'rgba(255,255,255,0.3)' } }}>
                  <Person sx={{ fontSize: 16, mr: 0.5 }} />
                  我的数据
                </ToggleButton>
              </ToggleButtonGroup>
            )}
          </Stack>
        </CardContent>
      </Card>

      {error && (
        <Alert severity="error" onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 统计卡片 */}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: isMyView
            ? { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(3, 1fr)' }
            : { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
          gap: 2,
        }}
      >
        <StatCard
          title={isMyView ? '我的设备' : '总设备数'}
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
        {!isMyView && (
          <StatCard
            title="用户数量"
            value={stats.total_users}
            icon={<People />}
            color="warning"
          />
        )}
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
                {platformInfo.name || 'NRLLink'}
              </Typography>
            </Box>
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                系统版本
              </Typography>
              <Typography variant="body2" fontWeight={500}>
                {platformInfo.version || '1.0.0'}
              </Typography>
            </Box>
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                协议版本
              </Typography>
              <Typography variant="body2" fontWeight={500}>
                NRL2
              </Typography>
            </Box>
          </Stack>
        </Paper>
      </Box>
    </Stack>
  )
}
