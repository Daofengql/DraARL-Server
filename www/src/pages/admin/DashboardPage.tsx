import { useEffect, useState } from 'react'
import {
  Box,
  Button,
  Card,
  CardContent,
  Typography,
  Paper,
  LinearProgress,
  Stack,
  Skeleton,
  useTheme,
  alpha,
  useMediaQuery,
} from '@mui/material'
import Devices from '@mui/icons-material/Devices'
import CheckCircle from '@mui/icons-material/CheckCircle'
import Group from '@mui/icons-material/Group'
import People from '@mui/icons-material/People'
import Radio from '@mui/icons-material/Radio'
import DashboardIcon from '@mui/icons-material/Dashboard'
import RecordVoiceOver from '@mui/icons-material/RecordVoiceOver'
import Storage from '@mui/icons-material/Storage'
import Timer from '@mui/icons-material/Timer'
import Memory from '@mui/icons-material/Memory'
import Speed from '@mui/icons-material/Speed'
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
import { platformService } from '../../services/platform'
import { commStatsService } from '../../services/commStats'
import { systemService } from '../../services/system'
import type { SystemOverview } from '../../services/system'
import type { DailyCommStats } from '../../types'
import { SITE_CONFIG } from '../../config/site'
import { useConfig } from '../../contexts/ConfigContext'

interface StatCardProps {
  title: string
  value: number | string
  icon: React.ReactNode
  color: 'primary' | 'success' | 'info' | 'warning'
}

function StatCard({ title, value, icon, color }: StatCardProps) {
  const theme = useTheme()
  const colorConfig = {
    primary: { bg: alpha(theme.palette.primary.main, 0.12), color: 'primary.main' },
    success: { bg: alpha(theme.palette.success.main, 0.12), color: 'success.main' },
    info: { bg: alpha(theme.palette.info.main, 0.12), color: 'info.main' },
    warning: { bg: alpha(theme.palette.warning.main, 0.12), color: 'warning.main' },
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

// 格式化文件大小
function formatFileSize(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

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

function formatPercent(value: number | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) ? `${value.toFixed(1)}%` : '采集中'
}

function formatSampleTime(value: string): string {
  if (!value) return '暂不可用'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '暂不可用' : date.toLocaleString('zh-CN', { hour12: false })
}

function ResourceCard({
  title,
  icon,
  value,
  percent,
  detail,
  color,
}: {
  title: string
  icon: React.ReactNode
  value: string
  percent: number | undefined
  detail: string
  color: 'primary' | 'success' | 'warning'
}) {
  const theme = useTheme()
  const barColor = color === 'warning' && (percent || 0) >= 80 ? theme.palette.error.main : theme.palette[color].main
  return (
    <Card variant="outlined">
      <CardContent>
        <Stack direction="row" justifyContent="space-between" alignItems="center" spacing={1}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Box sx={{ color: `${color}.main`, display: 'flex' }}>{icon}</Box>
            <Typography variant="body2" color="text.secondary">{title}</Typography>
          </Stack>
          <Typography variant="h6" fontWeight={700}>{value}</Typography>
        </Stack>
        <LinearProgress
          variant={typeof percent === 'number' ? 'determinate' : 'indeterminate'}
          value={percent || 0}
          sx={{ mt: 2, height: 7, borderRadius: 4, '& .MuiLinearProgress-bar': { backgroundColor: barColor } }}
        />
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>{detail}</Typography>
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
      <Skeleton variant="rectangular" height={200} />
    </Stack>
  )
}

export function AdminDashboardPage() {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))
  const { config: systemConfig } = useConfig()
  const [stats, setStats] = useState({
    total_devices: 0,
    online_devices: 0,
    total_groups: 0,
    total_users: 0,
  })
  const [commStats, setCommStats] = useState({
    total_count: 0,
    total_size: 0,
    total_duration: 0,
  })
  const [commTrend, setCommTrend] = useState<DailyCommStats[]>([])
  const [systemOverview, setSystemOverview] = useState<SystemOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [systemError, setSystemError] = useState<string | null>(null)

  const fetchSystemStats = async () => {
    try {
      const systemOverviewPromise = systemService.getOverview().catch(() => {
        setSystemError('服务器负载暂时无法读取')
        return null
      })
      const [statsData, commStatsData, commTrendData, systemData] = await Promise.all([
        platformService.getTotalStats(),
        commStatsService.getSystemStats(),
        commStatsService.getSystemTrend(),
        systemOverviewPromise,
      ])
      setStats({
        total_devices: statsData.total_devices || 0,
        online_devices: statsData.online_devices || 0,
        total_groups: statsData.total_groups || 0,
        total_users: statsData.total_users || 0,
      })
      setCommStats({
        total_count: commStatsData.total_count || 0,
        total_size: commStatsData.total_size || 0,
        total_duration: commStatsData.total_duration || 0,
      })
      setCommTrend(commTrendData)
      setSystemOverview(systemData)
    } catch {
      setError('获取统计数据失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchSystemStats()
  }, [])

  useEffect(() => {
    const refreshTimer = window.setInterval(() => { void fetchSystemStats() }, 15_000)
    return () => window.clearInterval(refreshTimer)
  }, [])

  // 站点名称：欢迎卡片使用配置的站点名称或默认值
  const siteName = systemConfig?.systemInfo?.name || SITE_CONFIG.NAME

  if (loading) {
    return <DashboardSkeleton />
  }

  return (
    <Stack spacing={3}>
      {/* 欢迎信息卡片 */}
      <Card
        sx={{
          background: 'linear-gradient(135deg, #1565C0 0%, #0D47A1 100%)',
          color: 'white',
          border: 'none',
          boxShadow: 3,
        }}
      >
        <CardContent>
          <Stack direction={{ xs: 'column', sm: 'row' }} alignItems={{ xs: 'flex-start', sm: 'center' }} spacing={2} justifyContent="space-between">
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
            <Button
              onClick={() => { void fetchSystemStats() }}
              startIcon={<Refresh />}
              sx={{ color: 'inherit', borderColor: 'rgba(255,255,255,0.55)', whiteSpace: 'nowrap', alignSelf: { xs: 'stretch', sm: 'auto' }, '&:hover': { borderColor: 'white', bgcolor: 'rgba(255,255,255,0.12)' } }}
              variant="outlined"
            >
              刷新数据
            </Button>
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

      {/* 主机运维概览 */}
      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Stack
          direction={{ xs: 'column', sm: 'row' }}
          justifyContent="space-between"
          alignItems={{ xs: 'flex-start', sm: 'center' }}
          spacing={1}
          sx={{ px: 3, py: 2 }}
        >
          <Stack direction="row" spacing={1} alignItems="center">
            <Speed color="primary" />
            <Box>
              <Typography variant="h6" fontWeight={600}>服务器概览</Typography>
              <Typography variant="caption" color="text.secondary">主机资源与 DraARL 进程运行状态</Typography>
            </Box>
          </Stack>
          <Typography variant="caption" color="text.secondary">
            最近采样：{formatSampleTime(systemOverview?.sampled_at || '')}
          </Typography>
        </Stack>

        {systemError || !systemOverview ? (
          <Box sx={{ px: 3, pb: 3 }}>
            <Typography color="warning.main">{systemError || '服务器信息暂不可用'}</Typography>
          </Box>
        ) : (
          <>
            <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, 1fr)' }, gap: 2, px: 3, pb: 3 }}>
              <ResourceCard
                title="CPU 使用率"
                icon={<Speed />}
                value={formatPercent(systemOverview.cpu_usage_percent)}
                percent={systemOverview.cpu_usage_percent}
                detail={`${systemOverview.cpu_cores} 核 · 负载 ${systemOverview.load_average?.[0]?.toFixed(2) || '暂不可用'}`}
                color="primary"
              />
              <ResourceCard
                title="内存使用率"
                icon={<Memory />}
                value={systemOverview.memory.available ? formatPercent(systemOverview.memory.used_percent) : '暂不可用'}
                percent={systemOverview.memory.available ? systemOverview.memory.used_percent : undefined}
                detail={systemOverview.memory.available ? `${formatFileSize(systemOverview.memory.used_bytes)} / ${formatFileSize(systemOverview.memory.total_bytes)}` : '当前平台未提供主机内存数据'}
                color="success"
              />
              <ResourceCard
                title="磁盘使用率"
                icon={<Storage />}
                value={systemOverview.disk.available ? formatPercent(systemOverview.disk.used_percent) : '暂不可用'}
                percent={systemOverview.disk.available ? systemOverview.disk.used_percent : undefined}
                detail={systemOverview.disk.available ? `${formatFileSize(systemOverview.disk.free_bytes)} 可用` : '当前平台未提供磁盘容量数据'}
                color="warning"
              />
            </Box>
            <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'repeat(2, 1fr)', sm: 'repeat(4, 1fr)' }, borderTop: 1, borderColor: 'divider' }}>
              {[
                { label: '主机', value: systemOverview.hostname || '未知' },
                { label: '系统', value: `${systemOverview.os} · ${systemOverview.architecture}` },
                { label: 'DraARL 版本', value: systemOverview.server_version || 'dev' },
                { label: '运行时长', value: formatDuration(systemOverview.uptime_seconds * 1000) },
                { label: 'Go 版本', value: systemOverview.go_version },
                { label: 'Goroutine', value: systemOverview.goroutines.toLocaleString() },
                { label: '进程内存', value: formatFileSize(systemOverview.process.heap_inuse_bytes) },
                { label: 'GC 次数', value: systemOverview.process.num_gc.toLocaleString() },
              ].map((item) => (
                <Box key={item.label} sx={{ px: 3, py: 1.75, minWidth: 0 }}>
                  <Typography variant="caption" color="text.secondary">{item.label}</Typography>
                  <Typography variant="body2" fontWeight={600} sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{item.value}</Typography>
                </Box>
              ))}
            </Box>
          </>
        )}
      </Paper>

      {/* 基础统计卡片 */}
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

      {/* 通信统计卡片 */}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, 1fr)' },
          gap: 2,
        }}
      >
        <StatCard
          title="通信记录数"
          value={commStats.total_count}
          icon={<RecordVoiceOver />}
          color="primary"
        />
        <StatCard
          title="通信总大小"
          value={formatFileSize(commStats.total_size)}
          icon={<Storage />}
          color="info"
        />
        <StatCard
          title="通信总时长"
          value={formatDuration(commStats.total_duration)}
          icon={<Timer />}
          color="success"
        />
      </Box>

      {/* 通信趋势图 */}
      <Card>
        <CardContent>
          <Stack direction="row" alignItems="center" spacing={1} mb={2}>
            <RecordVoiceOver color="primary" />
            <Typography variant="h6" fontWeight={600}>
              近30天平台通信趋势
            </Typography>
          </Stack>
          <Box sx={{ width: '100%', height: 300, minHeight: 300 }}>
            {commTrend.length > 0 ? (
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={commTrend} margin={{ top: 5, right: isMobile ? 5 : 60, left: isMobile ? 0 : 0, bottom: 5 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis
                    dataKey="date"
                    tick={{ fontSize: isMobile ? 10 : 12 }}
                    interval={isMobile ? 'preserveStartEnd' : 0}
                    tickFormatter={(value) => value ? value.slice(5) : ''}
                  />
                  <YAxis
                    yAxisId="left"
                    tick={{ fontSize: isMobile ? 10 : 12 }}
                    allowDecimals={false}
                    width={isMobile ? 35 : 60}
                  />
                  <YAxis
                    yAxisId="right"
                    orientation="right"
                    tick={{ fontSize: isMobile ? 10 : 12 }}
                    tickFormatter={(value) => `${Math.round(value / 60000)}分`}
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
                    name="通信次数"
                  />
                  <Line
                    yAxisId="right"
                    type="monotone"
                    dataKey="duration"
                    stroke={theme.palette.success.main}
                    strokeWidth={2}
                    dot={false}
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
                  bgcolor: 'action.disabledBackground',
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
    </Stack>
  )
}
