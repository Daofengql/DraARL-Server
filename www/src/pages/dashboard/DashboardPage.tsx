import { useEffect, useState, useRef } from 'react'
import {
  Box,
  Card,
  CardContent,
  Typography,
  Paper,
  LinearProgress,
  Stack,
  Skeleton,
  Alert,
  Chip,
  useTheme,
  useMediaQuery,
  Button,
  Snackbar,
} from '@mui/material'
import Refresh from '@mui/icons-material/Refresh'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import { authService } from '../../services'
import { platformService } from '../../services/platform'
import { deviceService } from '../../services/device'
import { commStatsService } from '../../services/commStats'
import type { DailyCommStats } from '../../types'
import { BlueprintStatCard as StatCard } from '../../components/common/BlueprintStatCard'
import { SITE_CONFIG } from '../../config/site'
import { useConfig } from '../../contexts/ConfigContext'
import {
  CheckIcon,
  DashboardIcon,
  DeviceIcon,
  GroupIcon,
  RadioIcon,
  RecordIcon,
  TimerIcon,
} from '../../components/icons/BlueprintIcons'

// 格式化时长
function formatDuration(ms: number): string {
  if (ms === 0) return '0秒'
  const seconds = Math.floor(ms / 1000)
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = seconds % 60

  const parts: string[] = []
  if (hours > 0) parts.push(`${hours}小时`)
  if (minutes > 0) parts.push(`${minutes}分钟`)
  if (secs > 0 || parts.length === 0) parts.push(`${secs}秒`)

  return parts.join(' ')
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
      <Skeleton variant="rectangular" height={200} />
    </Stack>
  )
}

export function DashboardPage() {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))
  const { config: systemConfig } = useConfig()
  const [stats, setStats] = useState({
    my_devices: 0,
    online_devices: 0,
    total_groups: 0,
    comm_count: 0,
    comm_duration: 0,
  })
  const [commTrend, setCommTrend] = useState<DailyCommStats[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [refreshSuccess, setRefreshSuccess] = useState(false)
  const [cachedUser, setCachedUser] = useState(authService.getStoredUser())
  const fetchingRef = useRef(false) // 防止 StrictMode 双重请求

  // 监听用户信息更新事件
  useEffect(() => {
    const handleUserUpdate = () => {
      setCachedUser(authService.getStoredUser())
    }
    window.addEventListener('user-updated', handleUserUpdate)
    return () => window.removeEventListener('user-updated', handleUserUpdate)
  }, [])

  // 刷新用户状态
  const handleRefreshStatus = async () => {
    setRefreshing(true)
    try {
      const newUser = await authService.refreshUserInfo()
      if (newUser) {
        setCachedUser(newUser)
        setRefreshSuccess(true)
      }
    } finally {
      setRefreshing(false)
    }
  }

  const fetchMyStats = async () => {
    try {
      // 获取与用户呼号匹配的设备
      const allDevices = await deviceService.list()
      const myDevices = cachedUser?.callsign
        ? allDevices.filter(d => d.callsign === cachedUser.callsign)
        : []

      // 获取所有群组和用户统计
      const [statsData, commStatsData, commTrendData] = await Promise.all([
        platformService.getTotalStats(),
        commStatsService.getUserStats(),
        commStatsService.getUserTrend(),
      ])

      setStats({
        my_devices: myDevices.length,
        online_devices: myDevices.filter(d => d.is_online || d.online).length,
        total_groups: statsData.total_groups || 0,
        comm_count: commStatsData.total_count || 0,
        comm_duration: commStatsData.total_duration || 0,
      })
      setCommTrend(commTrendData)
    } catch {
      setError('获取统计数据失败')
    } finally {
      setLoading(false)
    }
  }

  // Load the initial snapshot once; fetchingRef also prevents StrictMode duplicate requests.
  useEffect(() => {
    // 防止 StrictMode 双重请求
    if (fetchingRef.current) return
    fetchingRef.current = true

    fetchMyStats()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const displayName = cachedUser?.nickname || cachedUser?.username || '用户'

  // 站点名称：欢迎卡片使用配置的站点名称或默认值
  const siteName = systemConfig?.systemInfo?.name || SITE_CONFIG.NAME

  // 用户状态标识
  const getUserStatus = () => {
    if (!cachedUser) return { label: '未登录', color: 'default' as const }
    if (cachedUser.role === 'admin') return { label: '管理员', color: 'secondary' as const }
    if (cachedUser.approval_status === 0) return { label: '待审核', color: 'warning' as const }
    if (cachedUser.approval_status === 2) return { label: '已拒绝', color: 'error' as const }
    return { label: '普通用户', color: 'primary' as const }
  }

  const userStatus = getUserStatus()

  if (loading) {
    return <DashboardSkeleton />
  }

  return (
    <Stack spacing={3}>
      {/* 欢迎信息卡片 */}
      <Card
        sx={{
          borderTop: '2px solid',
          borderTopColor: 'secondary.main',
        }}
      >
        <CardContent>
          <Typography variant="caption" color="secondary.main" sx={{ display: 'block', mb: 2 }}>
            DraARL / 我的工作台
          </Typography>
          <Stack direction="row" alignItems="flex-start" spacing={2}>
            <DashboardIcon sx={{ fontSize: 28, color: 'secondary.main', mt: 0.5, display: { xs: 'none', sm: 'block' } }} />
            <Box sx={{ flexGrow: 1, minWidth: 0 }}>
              <Stack direction="row" alignItems="center" useFlexGap flexWrap="wrap" spacing={1.5}>
                <Typography component="h1" variant="h5" fontWeight={600}>
                  欢迎回来，{displayName}！
                </Typography>
                <Chip
                  label={userStatus.label}
                  size="small"
                  color={userStatus.color}
                  variant="outlined"
                />
              </Stack>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                {siteName} · 设备、群组与通信概览
              </Typography>
            </Box>
          </Stack>
        </CardContent>
      </Card>

      {/* 待审核提示 */}
      {cachedUser?.approval_status === 0 && (
        <Alert
          severity="warning"
          action={
            <Button
              color="inherit"
              size="small"
              onClick={handleRefreshStatus}
              disabled={refreshing}
              startIcon={<Refresh />}
            >
              {refreshing ? '刷新中...' : '刷新状态'}
            </Button>
          }
        >
          <Typography variant="body2">
            您的账号正在审核中，部分功能暂不可用。审核通过后点击"刷新状态"即可生效。
          </Typography>
        </Alert>
      )}

      {/* 已拒绝提示 */}
      {cachedUser?.approval_status === 2 && (
        <Alert severity="error">
          <Typography variant="body2">
            您的账号审核未通过。{cachedUser.review_note && `原因：${cachedUser.review_note}`}
          </Typography>
        </Alert>
      )}

      {error && (
        <Alert severity="error" onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 统计卡片 */}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(3, 1fr)' },
          gap: 2,
        }}
      >
        <StatCard
          title="我的设备"
          value={stats.my_devices}
          icon={<DeviceIcon />}
          color="primary"
        />
        <StatCard
          title="在线设备"
          value={stats.online_devices}
          icon={<CheckIcon />}
          color="success"
        />
        <StatCard
          title="群组数量"
          value={stats.total_groups}
          icon={<GroupIcon />}
          color="info"
        />
      </Box>

      {/* 通信统计卡片 */}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)' },
          gap: 2,
        }}
      >
        <StatCard
          title="通信记录"
          value={stats.comm_count}
          icon={<RecordIcon />}
          color="warning"
        />
        <StatCard
          title="通信总时长"
          value={formatDuration(stats.comm_duration)}
          icon={<TimerIcon />}
          color="info"
        />
      </Box>

      {/* 通信趋势图 */}
      <Card>
        <CardContent>
          <Stack direction="row" alignItems="center" spacing={1} mb={2}>
            <RecordIcon color="primary" />
            <Typography component="h2" variant="h6" fontWeight={600}>
              近30天通信趋势
            </Typography>
          </Stack>
          <Box sx={{ width: '100%', height: 300, minHeight: 300 }}>
            {commTrend.length > 0 ? (
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={commTrend} margin={{ top: 5, right: isMobile ? 5 : 60, left: isMobile ? 0 : 0, bottom: 5 }}>
                  <CartesianGrid stroke={theme.palette.divider} strokeDasharray="3 3" />
                  <XAxis
                    dataKey="date"
                    tick={{ fontSize: isMobile ? 10 : 12, fill: theme.palette.text.secondary }}
                    interval="preserveStartEnd"
                    minTickGap={24}
                    tickFormatter={(value) => value ? value.slice(5) : ''}
                  />
                  <YAxis
                    yAxisId="left"
                    tick={{ fontSize: isMobile ? 10 : 12, fill: theme.palette.text.secondary }}
                    allowDecimals={false}
                    width={isMobile ? 35 : 60}
                  />
                  <YAxis
                    yAxisId="right"
                    orientation="right"
                    tick={{ fontSize: isMobile ? 10 : 12, fill: theme.palette.text.secondary }}
                    tickFormatter={(value) => value < 60000 ? `${Number((value / 1000).toFixed(1))}秒` : `${Math.round(value / 60000)}分`}
                    width={isMobile ? 35 : 60}
                  />
                  <Tooltip
                    contentStyle={{ backgroundColor: theme.palette.background.paper, borderColor: theme.palette.divider, color: theme.palette.text.primary }}
                    labelStyle={{ color: theme.palette.text.primary }}
                    labelFormatter={(label) => `日期: ${label}`}
                    formatter={(value, name) => {
                      if (name === '通信时长') {
                        return [formatDuration(value as number), name]
                      }
                      return [value ?? 0, name]
                    }}
                  />
                  <Legend wrapperStyle={{ fontSize: isMobile ? 12 : 14 }} />
                  <Line
                    yAxisId="left"
                    type="monotone"
                    dataKey="count"
                    stroke={theme.palette.primary.main}
                    strokeWidth={2}
                    dot={false}
                    isAnimationActive={false}
                    name="通信次数"
                  />
                  <Line
                    yAxisId="right"
                    type="monotone"
                    dataKey="duration"
                    stroke={theme.palette.success.main}
                    strokeWidth={2}
                    dot={false}
                    isAnimationActive={false}
                    name="通信时长"
                  />
                </LineChart>
              </ResponsiveContainer>
            ) : (
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
                <Typography color="text.secondary">暂无通信记录数据</Typography>
              </Box>
            )}
          </Box>
        </CardContent>
      </Card>

      {/* 详细信息面板 */}
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 3 }}>
        {/* 设备状态 */}
        <Paper variant="outlined" sx={{ p: 3 }}>
          <Stack direction="row" alignItems="center" spacing={1} mb={3}>
            <RadioIcon color="primary" />
            <Typography component="h2" variant="h6" fontWeight={600}>
              我的设备状态
            </Typography>
          </Stack>
          <Stack spacing={2}>
            <Box>
              <Stack direction="row" justifyContent="space-between" mb={1}>
                <Typography variant="body2" color="text.secondary">
                  设备在线率
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {stats.my_devices > 0
                    ? `${Math.round((stats.online_devices / stats.my_devices) * 100)}%`
                    : '0%'}
                </Typography>
              </Stack>
              <LinearProgress aria-label="设备在线率"
                variant="determinate"
                value={stats.my_devices > 0 ? (stats.online_devices / stats.my_devices) * 100 : 0}
                sx={{
                  height: 8,
                  borderRadius: 0,
                  bgcolor: 'action.disabledBackground',
                  '& .MuiLinearProgress-bar': {
                    bgcolor:
                      stats.my_devices > 0 && stats.online_devices / stats.my_devices > 0.8
                        ? 'success.main'
                        : stats.online_devices / stats.my_devices > 0.5
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
                  {stats.online_devices} / {stats.my_devices}
                </Typography>
              </Stack>
            </Box>
          </Stack>
        </Paper>

        {/* 系统信息 */}
        <Paper variant="outlined" sx={{ p: 3 }}>
          <Stack direction="row" alignItems="center" spacing={1} mb={3}>
            <DeviceIcon color="primary" />
            <Typography component="h2" variant="h6" fontWeight={600}>
              系统信息
            </Typography>
          </Stack>
          <Stack spacing={2}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                系统名称
              </Typography>
              <Typography variant="body2" fontWeight={500}>
                {SITE_CONFIG.NAME}
              </Typography>
            </Box>
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                系统版本
              </Typography>
              <Typography variant="body2" fontWeight={500}>
                {SITE_CONFIG.VERSION}
              </Typography>
            </Box>
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                协议版本
              </Typography>
              <Typography variant="body2" fontWeight={500}>
                {SITE_CONFIG.PROTOCOL_VERSION}
              </Typography>
            </Box>
          </Stack>
        </Paper>
      </Box>

      {/* 刷新成功提示 */}
      <Snackbar
        open={refreshSuccess}
        autoHideDuration={3000}
        onClose={() => setRefreshSuccess(false)}
        anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
      >
        <Alert severity="success" onClose={() => setRefreshSuccess(false)}>
          {cachedUser?.approval_status === 1 ? '审核已通过，现在可以使用全部功能！' : '状态已刷新'}
        </Alert>
      </Snackbar>
    </Stack>
  )
}
