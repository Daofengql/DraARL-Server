import { useEffect, useState } from 'react'
import {
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Typography,
  Paper,
  LinearProgress,
  Stack,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  useTheme,
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
import Dns from '@mui/icons-material/Dns'
import ArrowForward from '@mui/icons-material/ArrowForward'
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
import { edgeNodeService } from '../../services/server'
import type { EdgeNode } from '../../services/server'
import type { DailyCommStats } from '../../types'
import { SITE_CONFIG } from '../../config/site'
import { useConfig } from '../../contexts/ConfigContext'
import { useNavigate } from 'react-router-dom'

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

function formatPPS(value: number): string {
  return `${value.toLocaleString('zh-CN', { maximumFractionDigits: 1 })} pps`
}

function formatBitRate(bytesPerSecond: number): string {
  const units = ['bit/s', 'kbit/s', 'Mbit/s', 'Gbit/s', 'Tbit/s']
  let value = Math.max(0, bytesPerSecond * 8)
  let unit = 0
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000
    unit += 1
  }
  return `${value.toLocaleString('zh-CN', { maximumFractionDigits: 1 })} ${units[unit]}`
}

function nodeHasIssue(node: EdgeNode): boolean {
  if (node.status === 1 && !node.runtime.online) return true
  if (!node.runtime.online) return false
  return Boolean(node.runtime.sync_error) ||
    node.runtime.pending_control > 0 ||
    node.runtime.acked_projection_version !== node.runtime.heartbeat.projection_version ||
    node.runtime.traffic_rates.device.stale
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
  const navigate = useNavigate()
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
  const [edgeNodes, setEdgeNodes] = useState<EdgeNode[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [serverError, setServerError] = useState<string | null>(null)

  const fetchSystemStats = async () => {
    try {
      const edgeNodesPromise = edgeNodeService.list().catch(() => {
        setServerError('边缘节点状态暂时无法读取')
        return []
      })
      const [statsData, commStatsData, commTrendData, edgeNodeData] = await Promise.all([
        platformService.getTotalStats(),
        commStatsService.getSystemStats(),
        commStatsService.getSystemTrend(),
        edgeNodesPromise,
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
      setEdgeNodes(edgeNodeData)
    } catch {
      setError('获取统计数据失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchSystemStats()
  }, [])

  // 站点名称：欢迎卡片使用配置的站点名称或默认值
  const siteName = systemConfig?.systemInfo?.name || SITE_CONFIG.NAME

  const onlineEdgeNodes = edgeNodes.filter((node) => node.status === 1 && node.runtime.online)
  const edgeConnections = onlineEdgeNodes.reduce(
    (total, node) => total + node.runtime.heartbeat.connection_count,
    0,
  )
  const deviceTraffic = onlineEdgeNodes.reduce((total, node) => ({
    pps: total.pps + node.runtime.traffic_rates.device.current.in_pps + node.runtime.traffic_rates.device.current.out_pps,
    bytes: total.bytes + node.runtime.traffic_rates.device.current.in_bytes_per_second + node.runtime.traffic_rates.device.current.out_bytes_per_second,
  }), { pps: 0, bytes: 0 })
  const interconnectTraffic = onlineEdgeNodes.reduce((total, node) => ({
    pps: total.pps + node.runtime.traffic_rates.edge_interconnect.current.in_pps + node.runtime.traffic_rates.edge_interconnect.current.out_pps,
    bytes: total.bytes + node.runtime.traffic_rates.edge_interconnect.current.in_bytes_per_second + node.runtime.traffic_rates.edge_interconnect.current.out_bytes_per_second,
  }), { pps: 0, bytes: 0 })
  const issueNodeCount = edgeNodes.filter(nodeHasIssue).length

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

      <Paper variant="outlined">
        <Stack
          direction={{ xs: 'column', sm: 'row' }}
          justifyContent="space-between"
          alignItems={{ xs: 'flex-start', sm: 'center' }}
          spacing={1}
          sx={{ px: 3, py: 2 }}
        >
          <Stack direction="row" spacing={1} alignItems="center">
            <Dns color="primary" />
            <Box>
              <Typography variant="h6" fontWeight={600}>边缘节点</Typography>
              <Typography variant="caption" color="text.secondary">DraARL 应用层实时统计</Typography>
            </Box>
          </Stack>
          <Button endIcon={<ArrowForward />} onClick={() => navigate('/admin/servers')}>节点管理</Button>
        </Stack>

        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: 'repeat(2, 1fr)', md: 'repeat(5, 1fr)' },
            borderTop: 1,
            borderBottom: edgeNodes.length > 0 ? 1 : 0,
            borderColor: 'divider',
          }}
        >
          {[
            { label: '在线节点', value: `${onlineEdgeNodes.length} / ${edgeNodes.length}` },
            { label: '边缘设备连接', value: edgeConnections.toLocaleString() },
            { label: '设备侧吞吐', value: `${formatPPS(deviceTraffic.pps)} · ${formatBitRate(deviceTraffic.bytes)}` },
            { label: '互联侧吞吐', value: `${formatPPS(interconnectTraffic.pps)} · ${formatBitRate(interconnectTraffic.bytes)}` },
            { label: '异常节点', value: issueNodeCount.toLocaleString() },
          ].map((item) => (
            <Box key={item.label} sx={{ px: 3, py: 2, minWidth: 0 }}>
              <Typography variant="caption" color="text.secondary">{item.label}</Typography>
              <Typography variant="body1" fontWeight={600} sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{item.value}</Typography>
            </Box>
          ))}
        </Box>

        {serverError ? (
          <Typography color="warning.main" sx={{ px: 3, py: 2 }}>{serverError}</Typography>
        ) : edgeNodes.length === 0 ? (
          <Typography color="text.secondary" sx={{ px: 3, py: 2 }}>尚未注册边缘节点</Typography>
        ) : (
          <TableContainer sx={{ overflow: 'auto' }}>
            <Table size="small" sx={{ minWidth: 760 }}>
              <TableHead>
                <TableRow>
                  <TableCell>节点</TableCell>
                  <TableCell>状态</TableCell>
                  <TableCell align="right">设备连接</TableCell>
                  <TableCell>设备侧</TableCell>
                  <TableCell>互联侧</TableCell>
                  <TableCell>路由投影</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {edgeNodes.slice(0, 5).map((node) => {
                  const issue = nodeHasIssue(node)
                  const deviceRate = node.runtime.traffic_rates.device.current
                  const interconnectRate = node.runtime.traffic_rates.edge_interconnect.current
                  return (
                    <TableRow key={node.id} hover>
                      <TableCell>
                        <Typography variant="body2" fontWeight={500}>{node.display_name}</Typography>
                        {node.public_region && <Typography variant="caption" color="text.secondary">{node.public_region}</Typography>}
                      </TableCell>
                      <TableCell>
                        <Chip
                          size="small"
                          label={node.status === 0 ? '已禁用' : node.runtime.online ? '在线' : '离线'}
                          color={node.status === 0 ? 'default' : node.runtime.online ? 'success' : 'error'}
                        />
                      </TableCell>
                      <TableCell align="right">{node.runtime.online ? node.runtime.heartbeat.connection_count.toLocaleString() : '-'}</TableCell>
                      <TableCell>
                        {node.runtime.online && !node.runtime.traffic_rates.device.stale
                          ? `${formatPPS(deviceRate.in_pps + deviceRate.out_pps)} · ${formatBitRate(deviceRate.in_bytes_per_second + deviceRate.out_bytes_per_second)}`
                          : '-'}
                      </TableCell>
                      <TableCell>
                        {node.runtime.online && !node.runtime.traffic_rates.edge_interconnect.stale
                          ? `${formatPPS(interconnectRate.in_pps + interconnectRate.out_pps)} · ${formatBitRate(interconnectRate.in_bytes_per_second + interconnectRate.out_bytes_per_second)}`
                          : '-'}
                      </TableCell>
                      <TableCell>
                        <Chip size="small" variant="outlined" label={issue ? '需检查' : node.runtime.online ? '已同步' : '等待上线'} color={issue ? 'warning' : node.runtime.online ? 'success' : 'default'} />
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </Paper>

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
                    stroke="#1976d2"
                    strokeWidth={2}
                    dot={false}
                    name="通信次数"
                  />
                  <Line
                    yAxisId="right"
                    type="monotone"
                    dataKey="duration"
                    stroke="#2e7d32"
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
