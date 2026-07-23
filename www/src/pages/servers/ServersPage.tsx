import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControl,
  FormControlLabel,
  IconButton,
  InputLabel,
  Menu,
  MenuItem,
  Paper,
  Select,
  Stack,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TablePagination,
  TableRow,
  TextField,
  Tooltip,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import ContentCopy from '@mui/icons-material/ContentCopy'
import Edit from '@mui/icons-material/Edit'
import Key from '@mui/icons-material/Key'
import KeyOff from '@mui/icons-material/KeyOff'
import LinkOff from '@mui/icons-material/LinkOff'
import MoreVert from '@mui/icons-material/MoreVert'
import Visibility from '@mui/icons-material/Visibility'
import { edgeNodeService } from '../../services'
import type {
  EdgeNode,
  EdgeNodeCredentialResult,
  EdgeNodeUpdate,
  MetricRate,
  MetricsSnapshot,
  NodeProtectionSnapshot,
} from '../../services/server'
import { AutoRefresh } from '../../components/common/AutoRefresh'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { PageHeader } from '../../components/common/PageHeader'
import { RegionCascader, isChineseAdministrativeRegion } from '../../components/common/RegionCascader'
import { SearchBar } from '../../components/common/SearchBar'

const integerFormatter = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 0 })
const decimalFormatter = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 })

const protectionRows: Array<{ key: keyof NodeProtectionSnapshot; label: string }> = [
  { key: 'data_soft_limit_events', label: '数据软限额事件' },
  { key: 'data_hard_limit_drops', label: '数据硬限额丢弃' },
  { key: 'data_queue_drops', label: '数据队列丢弃' },
  { key: 'data_stale_drops', label: '过期队列丢弃' },
  { key: 'control_soft_limit_events', label: '控制软限额事件' },
  { key: 'control_hard_limit_drops', label: '控制硬限额丢弃' },
  { key: 'device_auth_limit_drops', label: '设备认证限额丢弃' },
  { key: 'session_limit_rejects', label: '会话上限拒绝' },
  { key: 'invalid_auth_tags', label: '无效认证标签' },
  { key: 'identity_rejects', label: '身份拒绝' },
  { key: 'expired_drops', label: '过期包丢弃' },
  { key: 'replay_drops', label: '重放包丢弃' },
  { key: 'unbound_address_drops', label: '未绑定地址丢弃' },
  { key: 'data_bind_rejects', label: '数据面绑定拒绝' },
]

const emptyEditForm: EdgeNodeUpdate = {
  display_name: '',
  note: '',
  status: 1,
  public_access_enabled: false,
  public_udp_host: '',
  public_udp_port: 60050,
  public_region: '',
  public_network: '',
  public_priority: 100,
}

function formatCount(value = 0) {
  return integerFormatter.format(value)
}

function formatPPS(value = 0) {
  return `${decimalFormatter.format(value)} pps`
}

function formatBytes(value = 0) {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let scaled = Math.max(0, value)
  let unit = 0
  while (scaled >= 1024 && unit < units.length - 1) {
    scaled /= 1024
    unit += 1
  }
  return `${decimalFormatter.format(scaled)} ${units[unit]}`
}

function formatBytesRate(value = 0) {
  return `${formatBytes(value)}/s`
}

function formatBitRate(bytesPerSecond = 0) {
  const units = ['bit/s', 'kbit/s', 'Mbit/s', 'Gbit/s', 'Tbit/s']
  let scaled = Math.max(0, bytesPerSecond * 8)
  let unit = 0
  while (scaled >= 1000 && unit < units.length - 1) {
    scaled /= 1000
    unit += 1
  }
  return `${decimalFormatter.format(scaled)} ${units[unit]}`
}

function formatTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function shortNodeID(nodeID: string) {
  return nodeID.length > 12 ? `${nodeID.slice(0, 12)}...` : nodeID
}

function getErrorMessage(error: unknown, fallback: string) {
  const responseMessage = (error as { response?: { data?: { message?: string } } })?.response?.data?.message
  const directMessage = error instanceof Error ? error.message : ''
  return responseMessage || directMessage || fallback
}

function addSnapshots(...snapshots: Array<MetricsSnapshot | undefined>): MetricsSnapshot {
  return snapshots.reduce<MetricsSnapshot>((total, snapshot) => ({
    in_packets: total.in_packets + (snapshot?.in_packets || 0),
    in_bytes: total.in_bytes + (snapshot?.in_bytes || 0),
    out_packets: total.out_packets + (snapshot?.out_packets || 0),
    out_bytes: total.out_bytes + (snapshot?.out_bytes || 0),
    drops: total.drops + (snapshot?.drops || 0),
    errors: total.errors + (snapshot?.errors || 0),
  }), { in_packets: 0, in_bytes: 0, out_packets: 0, out_bytes: 0, drops: 0, errors: 0 })
}

function protectionTotal(...snapshots: Array<NodeProtectionSnapshot | undefined>) {
  return snapshots.reduce((total, snapshot) => total + protectionRows.reduce(
    (subtotal, row) => subtotal + (snapshot?.[row.key] || 0),
    0,
  ), 0)
}

function currentConnections(node: EdgeNode) {
  return node.runtime.online
    ? node.runtime.heartbeat.connection_count
    : node.persisted_connection_count
}

function nodeHealth(node: EdgeNode) {
  if (node.status === 0) return { label: '已禁用', color: 'default' as const }
  if (!node.runtime.online) return { label: '离线', color: 'error' as const }
  if (node.runtime.traffic_rates.device.stale) return { label: '心跳异常', color: 'warning' as const }
  return { label: '在线', color: 'success' as const }
}

function routeHealth(node: EdgeNode) {
  if (!node.runtime.online) return { label: '等待上线', color: 'default' as const }
  if (node.runtime.sync_error) return { label: '同步失败', color: 'error' as const }
  if (
    node.runtime.pending_control > 0 ||
    node.runtime.acked_projection_version !== node.runtime.heartbeat.projection_version
  ) {
    return { label: '同步中', color: 'warning' as const }
  }
  return { label: '已同步', color: 'success' as const }
}

function TrafficCell({ rate, stale }: { rate: MetricRate; stale: boolean }) {
  if (stale) {
    return <Typography variant="body2" color="text.secondary">无实时样本</Typography>
  }
  return (
    <Stack spacing={0.4} sx={{ minWidth: 215 }}>
      <Typography variant="body2">
        接收 {formatPPS(rate.in_pps)}
      </Typography>
      <Typography variant="caption" color="text.secondary">
        {formatBytesRate(rate.in_bytes_per_second)} · {formatBitRate(rate.in_bytes_per_second)}
      </Typography>
      <Typography variant="body2">
        发送 {formatPPS(rate.out_pps)}
      </Typography>
      <Typography variant="caption" color="text.secondary">
        {formatBytesRate(rate.out_bytes_per_second)} · {formatBitRate(rate.out_bytes_per_second)}
      </Typography>
    </Stack>
  )
}

function MetricDetailRow({ label, rate, snapshot, stale }: {
  label: string
  rate: MetricRate
  snapshot: MetricsSnapshot
  stale: boolean
}) {
  return (
    <TableRow>
      <TableCell>{label}</TableCell>
      <TableCell>{stale ? '-' : formatPPS(rate.in_pps)}</TableCell>
      <TableCell>{stale ? '-' : formatPPS(rate.out_pps)}</TableCell>
      <TableCell>{stale ? '-' : `${formatBytesRate(rate.in_bytes_per_second)} / ${formatBitRate(rate.in_bytes_per_second)}`}</TableCell>
      <TableCell>{stale ? '-' : `${formatBytesRate(rate.out_bytes_per_second)} / ${formatBitRate(rate.out_bytes_per_second)}`}</TableCell>
      <TableCell>{formatCount(snapshot.in_packets)} / {formatBytes(snapshot.in_bytes)}</TableCell>
      <TableCell>{formatCount(snapshot.out_packets)} / {formatBytes(snapshot.out_bytes)}</TableCell>
      <TableCell>{formatCount(snapshot.drops)} / {formatCount(snapshot.errors)}</TableCell>
    </TableRow>
  )
}

export function ServersPage() {
  const [nodes, setNodes] = useState<EdgeNode[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [searchKeyword, setSearchKeyword] = useState('')
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [autoRefresh, setAutoRefresh] = useState(10)
  const [editOpen, setEditOpen] = useState(false)
  const [editingNode, setEditingNode] = useState<EdgeNode | null>(null)
  const [editForm, setEditForm] = useState<EdgeNodeUpdate>(emptyEditForm)
  const [regionScope, setRegionScope] = useState<'china' | 'overseas'>('china')
  const [detailNode, setDetailNode] = useState<EdgeNode | null>(null)
  const [secret, setSecret] = useState<{ title: string; value: string; description: string } | null>(null)
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null)
  const [menuNode, setMenuNode] = useState<EdgeNode | null>(null)
  const [confirm, setConfirm] = useState<{
    open: boolean
    title: string
    message: string
    confirmText: string
    type: 'danger' | 'warning' | 'info'
    action: () => Promise<void>
  } | null>(null)

  const loadNodes = useCallback(async () => {
    setLoading(true)
    try {
      const data = await edgeNodeService.list()
      setNodes(data)
      setDetailNode((current) => current ? data.find((node) => node.id === current.id) || null : null)
    } catch (err) {
      setError(getErrorMessage(err, '获取边缘节点列表失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadNodes()
  }, [loadNodes])

  const filteredNodes = useMemo(() => {
    const keyword = searchKeyword.trim().toLowerCase()
    if (!keyword) return nodes
    return nodes.filter((node) => [
      node.display_name,
      node.node_id,
      node.note,
      node.public_udp_host,
      node.public_region,
    ].some((value) => value?.toLowerCase().includes(keyword)))
  }, [nodes, searchKeyword])

  const paginatedNodes = filteredNodes.slice(page * rowsPerPage, page * rowsPerPage + rowsPerPage)

  const openCreate = () => {
    setEditingNode(null)
    setEditForm(emptyEditForm)
    setRegionScope('china')
    setEditOpen(true)
  }

  const openEdit = (node: EdgeNode) => {
    setEditingNode(node)
    setEditForm({
      display_name: node.display_name,
      note: node.note,
      status: node.status,
      public_access_enabled: node.public_access_enabled,
      public_udp_host: node.public_udp_host,
      public_udp_port: node.public_udp_port || 60050,
      public_region: node.public_region,
      public_network: node.public_network,
      public_priority: node.public_priority,
    })
    setRegionScope(!node.public_region || isChineseAdministrativeRegion(node.public_region) ? 'china' : 'overseas')
    setEditOpen(true)
  }

  const saveNode = async () => {
    if (!editForm.display_name.trim()) {
      setError('节点昵称不能为空')
      return
    }
    const region = editForm.public_region.trim()
    if (!region) {
      setError('节点地区不能为空')
      return
    }
    if (regionScope === 'china' && region.split(/\s+/).length < 2) {
      setError('国内节点地区至少需要选择到市级别')
      return
    }
    setSaving(true)
    try {
      if (editingNode) {
        await edgeNodeService.update(editingNode.id, {
          ...editForm,
          display_name: editForm.display_name.trim(),
          note: editForm.note.trim(),
          public_udp_host: editForm.public_udp_host.trim(),
          public_region: region,
          public_network: editForm.public_network.trim(),
        })
        setNotice('节点设置已更新')
      } else {
        const result = await edgeNodeService.create({
          display_name: editForm.display_name.trim(),
          note: editForm.note.trim(),
          public_region: region,
        })
        setSecret({
          title: '一次性注册 Token',
          value: result.registration_token,
          description: `节点 ${result.node.display_name} 已创建。请将此值写入边缘配置的 Edge.Token；关闭后无法再次查看。`,
        })
      }
      setEditOpen(false)
      await loadNodes()
    } catch (err) {
      setError(getErrorMessage(err, editingNode ? '更新节点失败' : '创建节点失败'))
    } finally {
      setSaving(false)
    }
  }

  const openNodeMenu = (event: React.MouseEvent<HTMLElement>, node: EdgeNode) => {
    setMenuAnchor(event.currentTarget)
    setMenuNode(node)
  }

  const closeNodeMenu = () => {
    setMenuAnchor(null)
    setMenuNode(null)
  }

  const requestRotate = (node: EdgeNode) => {
    closeNodeMenu()
    setConfirm({
      open: true,
      title: '轮换节点凭据',
      message: `确定为“${node.display_name}”签发新凭据吗？旧凭据只在配置的宽限期内有效。`,
      confirmText: '轮换凭据',
      type: 'warning',
      action: async () => {
        const result: EdgeNodeCredentialResult = await edgeNodeService.rotateCredential(node.id)
        setSecret({
          title: '一次性节点凭据',
          value: result.credential,
          description: result.delivered_online
            ? `新凭据已在线下发，凭据代次为 ${result.credential_epoch}。此值仅供应急备份，关闭后无法再次查看。`
            : `节点当前未完成在线接收。请手动更新边缘 identity，凭据代次为 ${result.credential_epoch}；关闭后无法再次查看。`,
        })
      },
    })
  }

  const requestRevoke = (node: EdgeNode) => {
    closeNodeMenu()
    setConfirm({
      open: true,
      title: '吊销节点凭据',
      message: `吊销“${node.display_name}”的全部当前凭据并立即断开连接？节点在重新签发凭据前不能接入。`,
      confirmText: '吊销凭据',
      type: 'danger',
      action: async () => {
        await edgeNodeService.revokeCredential(node.id)
        setNotice('节点凭据已吊销')
      },
    })
  }

  const requestDisconnect = (node: EdgeNode) => {
    closeNodeMenu()
    setConfirm({
      open: true,
      title: '重置节点连接',
      message: `断开“${node.display_name}”的当前会话？节点凭据仍然有效，可以立即重新连接。`,
      confirmText: '重置连接',
      type: 'warning',
      action: async () => {
        const result = await edgeNodeService.disconnect(node.id)
        setNotice(result.disconnected ? '节点连接已断开' : '节点当前没有在线连接')
      },
    })
  }

  const runConfirmedAction = async () => {
    if (!confirm) return
    const action = confirm.action
    setConfirm(null)
    try {
      await action()
      await loadNodes()
    } catch (err) {
      setError(getErrorMessage(err, '节点操作失败'))
    }
  }

  const copySecret = async () => {
    if (!secret) return
    try {
      await navigator.clipboard.writeText(secret.value)
      setNotice('凭据已复制到剪贴板')
    } catch {
      setError('无法访问剪贴板，请手动选择凭据文本')
    }
  }

  return (
    <Box>
      <PageHeader
        title="边缘节点"
        subtitle="管理 Type 0 边缘入口、运行状态和路由投影"
        sx={{
          flexDirection: { xs: 'column', sm: 'row' },
          alignItems: { xs: 'stretch', sm: 'center' },
          gap: 2,
        }}
        actions={
          <Stack direction="row" spacing={1} alignItems="center" justifyContent={{ xs: 'flex-end', sm: 'initial' }}>
            <AutoRefresh value={autoRefresh} onChange={setAutoRefresh} onRefresh={loadNodes} loading={loading} />
            <Button variant="contained" startIcon={<Add />} onClick={openCreate} sx={{ whiteSpace: 'nowrap' }}>注册节点</Button>
          </Stack>
        }
      />

      <Alert severity="info" sx={{ mb: 2 }}>
        流量、速率和带宽均为 DraARL 应用层统计，不代表服务器网卡的总流量。
      </Alert>
      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>{error}</Alert>}
      {notice && <Alert severity="success" sx={{ mb: 2 }} onClose={() => setNotice('')}>{notice}</Alert>}

      <Paper variant="outlined" sx={{ mb: 2, p: 2 }}>
        <SearchBar
          value={searchKeyword}
          onChange={(value) => { setSearchKeyword(value); setPage(0) }}
          onSearch={() => setPage(0)}
          placeholder="搜索节点昵称、NodeID、备注或公开入口"
          loading={loading}
          fullWidth
        />
      </Paper>

      <TableContainer component={Paper} variant="outlined" sx={{ overflow: 'auto' }}>
        <Table sx={{ minWidth: 1510 }}>
          <TableHead sx={{ bgcolor: 'grey.50' }}>
            <TableRow>
              <TableCell sx={{ width: 95 }}>健康</TableCell>
              <TableCell sx={{ minWidth: 180 }}>节点</TableCell>
              <TableCell align="center" sx={{ width: 85 }}>连接数</TableCell>
              <TableCell sx={{ minWidth: 235 }}>设备侧</TableCell>
              <TableCell sx={{ minWidth: 235 }}>互联侧</TableCell>
              <TableCell sx={{ minWidth: 175 }}>异常累计</TableCell>
              <TableCell sx={{ minWidth: 160 }}>路由投影</TableCell>
              <TableCell sx={{ minWidth: 180 }}>公开入口</TableCell>
              <TableCell
                align="center"
                sx={{
                  width: 120,
                  position: { xs: 'static', md: 'sticky' },
                  right: 0,
                  zIndex: 2,
                  bgcolor: 'grey.50',
                }}
              >操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading && nodes.length === 0 ? (
              <TableRow><TableCell colSpan={9} align="center">加载中...</TableCell></TableRow>
            ) : paginatedNodes.length === 0 ? (
              <TableRow><TableCell colSpan={9} align="center">暂无边缘节点</TableCell></TableRow>
            ) : paginatedNodes.map((node) => {
              const health = nodeHealth(node)
              const route = routeHealth(node)
              const metrics = addSnapshots(
                node.runtime.heartbeat.device,
                node.runtime.heartbeat.interconnect,
                node.runtime.center_interconnect,
              )
              const protectedEvents = protectionTotal(
                node.runtime.heartbeat.protection,
                node.runtime.center_protection,
              )
              return (
                <TableRow key={node.id} hover>
                  <TableCell>
                    <Chip size="small" label={health.label} color={health.color} />
                    <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.5 }}>
                      {formatTime(node.runtime.last_heartbeat || node.last_seen_at)}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2" fontWeight={600}>{node.display_name}</Typography>
                    <Tooltip title={node.node_id} placement="top-start">
                      <Typography variant="caption" color="text.secondary" sx={{ fontFamily: 'monospace' }}>
                        {shortNodeID(node.node_id)}
                      </Typography>
                    </Tooltip>
                    {node.note && (
                      <Typography variant="caption" color="text.secondary" display="block" noWrap sx={{ maxWidth: 180 }}>
                        {node.note}
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell align="center">
                    <Typography variant="body1" fontWeight={600}>{formatCount(currentConnections(node))}</Typography>
                    {!node.runtime.online && node.persisted_connection_count > 0 && (
                      <Typography variant="caption" color="text.secondary">最近</Typography>
                    )}
                  </TableCell>
                  <TableCell>
                    <TrafficCell
                      rate={node.runtime.traffic_rates.device.current}
                      stale={node.runtime.traffic_rates.device.stale}
                    />
                  </TableCell>
                  <TableCell>
                    <TrafficCell
                      rate={node.runtime.traffic_rates.edge_interconnect.current}
                      stale={node.runtime.traffic_rates.edge_interconnect.stale}
                    />
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2">丢弃 {formatCount(metrics.drops)}</Typography>
                    <Typography variant="body2">错误 {formatCount(metrics.errors)}</Typography>
                    <Typography variant="body2">保护 {formatCount(protectedEvents)}</Typography>
                  </TableCell>
                  <TableCell>
                    <Chip size="small" label={route.label} color={route.color} />
                    <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.5 }}>
                      ACK {formatCount(node.runtime.acked_projection_version)} / 投影 {formatCount(node.runtime.heartbeat.projection_version)}
                    </Typography>
                    {node.runtime.pending_control > 0 && (
                      <Typography variant="caption" color="warning.main" display="block">
                        待确认 {formatCount(node.runtime.pending_control)}
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell>
                    <Chip
                      size="small"
                      variant="outlined"
                      color={node.public_access_enabled ? 'success' : 'default'}
                      label={node.public_access_enabled ? '已发布' : '未发布'}
                    />
                    {(node.public_udp_host || node.public_udp_port > 0) && (
                      <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.5 }}>
                        {node.public_udp_host || '-'}:{node.public_udp_port || '-'}
                      </Typography>
                    )}
                    {node.public_region && (
                      <Typography variant="caption" color="text.secondary" display="block">
                        {node.public_region}
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell
                    align="center"
                    sx={{
                      position: { xs: 'static', md: 'sticky' },
                      right: 0,
                      zIndex: 1,
                      bgcolor: 'background.paper',
                    }}
                  >
                    <Tooltip title="查看详情">
                      <IconButton size="small" onClick={() => setDetailNode(node)}><Visibility fontSize="small" /></IconButton>
                    </Tooltip>
                    <Tooltip title="编辑节点">
                      <IconButton size="small" color="primary" onClick={() => openEdit(node)}><Edit fontSize="small" /></IconButton>
                    </Tooltip>
                    <Tooltip title="更多操作">
                      <IconButton size="small" onClick={(event) => openNodeMenu(event, node)}><MoreVert fontSize="small" /></IconButton>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        <TablePagination
          component="div"
          count={filteredNodes.length}
          page={page}
          onPageChange={(_, nextPage) => setPage(nextPage)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(event) => { setRowsPerPage(Number(event.target.value)); setPage(0) }}
          labelRowsPerPage="每页行数"
          labelDisplayedRows={({ from, to, count }) => `${from}-${to} 共 ${count}`}
        />
      </TableContainer>

      <Menu anchorEl={menuAnchor} open={Boolean(menuAnchor)} onClose={closeNodeMenu}>
        <MenuItem onClick={() => menuNode && requestRotate(menuNode)}><Key fontSize="small" sx={{ mr: 1.5 }} />轮换凭据</MenuItem>
        <MenuItem onClick={() => menuNode && requestDisconnect(menuNode)} disabled={!menuNode?.runtime.online}>
          <LinkOff fontSize="small" sx={{ mr: 1.5 }} />重置连接
        </MenuItem>
        <MenuItem onClick={() => menuNode && requestRevoke(menuNode)} sx={{ color: 'error.main' }}>
          <KeyOff fontSize="small" sx={{ mr: 1.5 }} />吊销凭据
        </MenuItem>
      </Menu>

      <Dialog open={editOpen} onClose={() => !saving && setEditOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>{editingNode ? '编辑边缘节点' : '注册边缘节点'}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 2, mt: 1 }}>
            <TextField
              label="节点昵称"
              value={editForm.display_name}
              onChange={(event) => setEditForm({ ...editForm, display_name: event.target.value })}
              required
              fullWidth
            />
            {editingNode && (
              <FormControl fullWidth>
                <InputLabel>节点状态</InputLabel>
                <Select
                  label="节点状态"
                  value={editForm.status}
                  onChange={(event) => setEditForm({ ...editForm, status: Number(event.target.value) })}
                >
                  <MenuItem value={1}>启用</MenuItem>
                  <MenuItem value={0}>禁用</MenuItem>
                </Select>
              </FormControl>
            )}
            <Box sx={{ gridColumn: '1 / -1', display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, gap: 1.5, alignItems: { sm: 'flex-start' } }}>
              <ToggleButtonGroup
                value={regionScope}
                exclusive
                size="small"
                onChange={(_, next: 'china' | 'overseas' | null) => {
                  if (!next || next === regionScope) return
                  setRegionScope(next)
                  setEditForm({ ...editForm, public_region: '' })
                }}
                aria-label="节点地区范围"
                sx={{ flexShrink: 0 }}
              >
                <ToggleButton value="china">国内</ToggleButton>
                <ToggleButton value="overseas">海外</ToggleButton>
              </ToggleButtonGroup>
              <Box sx={{ flex: 1, minWidth: 0, width: '100%' }}>
                {regionScope === 'china' ? (
                  <RegionCascader
                    value={editForm.public_region}
                    onChange={(value) => setEditForm({ ...editForm, public_region: value })}
                    label="节点地区"
                    required
                    helperText="至少需要选择到市级别"
                  />
                ) : (
                  <TextField
                    label="国家或地区"
                    value={editForm.public_region}
                    onChange={(event) => setEditForm({ ...editForm, public_region: event.target.value })}
                    placeholder="美国 加利福尼亚州"
                    required
                    fullWidth
                  />
                )}
              </Box>
            </Box>
            <TextField
              label="备注"
              value={editForm.note}
              onChange={(event) => setEditForm({ ...editForm, note: event.target.value })}
              multiline
              rows={2}
              fullWidth
              sx={{ gridColumn: '1 / -1' }}
            />
          </Box>

          {editingNode && (
            <>
              <Divider sx={{ my: 3 }} />
              <Typography variant="subtitle1" fontWeight={600} gutterBottom>公开设备入口</Typography>
              <FormControlLabel
                control={
                  <Switch
                    checked={editForm.public_access_enabled}
                    onChange={(event) => setEditForm({ ...editForm, public_access_enabled: event.target.checked })}
                  />
                }
                label="加入客户端可选入口列表"
              />
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '2fr 1fr 1fr' }, gap: 2, mt: 2 }}>
                <TextField
                  label="公开 UDP 主机"
                  value={editForm.public_udp_host}
                  onChange={(event) => setEditForm({ ...editForm, public_udp_host: event.target.value })}
                  placeholder="edge.example.com"
                  fullWidth
                />
                <TextField
                  label="公开 UDP 端口"
                  type="number"
                  value={editForm.public_udp_port}
                  onChange={(event) => setEditForm({ ...editForm, public_udp_port: Number(event.target.value) })}
                  inputProps={{ min: 1, max: 65535 }}
                  fullWidth
                />
                <TextField
                  label="排序优先级"
                  type="number"
                  value={editForm.public_priority}
                  onChange={(event) => setEditForm({ ...editForm, public_priority: Number(event.target.value) })}
                  helperText="数值越小越靠前"
                  fullWidth
                />
              </Box>
            </>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditOpen(false)} disabled={saving}>取消</Button>
          <Button variant="contained" onClick={() => void saveNode()} disabled={saving}>
            {saving ? '保存中...' : editingNode ? '保存' : '生成注册 Token'}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog open={Boolean(secret)} onClose={() => setSecret(null)} maxWidth="sm" fullWidth>
        <DialogTitle>{secret?.title}</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>此凭据只显示一次，关闭前请妥善保存。</Alert>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>{secret?.description}</Typography>
          <Paper variant="outlined" sx={{ p: 2, bgcolor: 'grey.50', overflow: 'auto' }}>
            <Typography component="pre" sx={{ m: 0, fontFamily: 'monospace', fontSize: '0.82rem', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
              {secret?.value}
            </Typography>
          </Paper>
        </DialogContent>
        <DialogActions>
          <Button startIcon={<ContentCopy />} onClick={() => void copySecret()}>复制凭据</Button>
          <Button variant="contained" onClick={() => setSecret(null)}>已保存并关闭</Button>
        </DialogActions>
      </Dialog>

      <Dialog open={Boolean(detailNode)} onClose={() => setDetailNode(null)} maxWidth="xl" fullWidth>
        <DialogTitle>
          {detailNode?.display_name}
          {detailNode && (
            <Typography component="span" variant="body2" color="text.secondary" sx={{ ml: 1.5, fontFamily: 'monospace' }}>
              {detailNode.node_id}
            </Typography>
          )}
        </DialogTitle>
        {detailNode && (
          <DialogContent>
            <Alert severity="info" sx={{ mb: 2 }}>以下带宽为 DraARL 应用层数据，不是网卡总流量。</Alert>
            <Stack direction={{ xs: 'column', md: 'row' }} spacing={3} sx={{ mb: 2 }}>
              <Box><Typography variant="caption" color="text.secondary">控制连接</Typography><Typography variant="body2">{detailNode.runtime.remote_addr || '-'}</Typography></Box>
              <Box><Typography variant="caption" color="text.secondary">连接时间</Typography><Typography variant="body2">{formatTime(detailNode.runtime.connected_at)}</Typography></Box>
              <Box><Typography variant="caption" color="text.secondary">最后心跳</Typography><Typography variant="body2">{formatTime(detailNode.runtime.last_heartbeat || detailNode.last_seen_at)}</Typography></Box>
              <Box><Typography variant="caption" color="text.secondary">设备连接数</Typography><Typography variant="body2">{formatCount(currentConnections(detailNode))}</Typography></Box>
              <Box><Typography variant="caption" color="text.secondary">凭据代次</Typography><Typography variant="body2">{formatCount(detailNode.credential_epoch)}</Typography></Box>
            </Stack>

            <Typography variant="subtitle1" fontWeight={600} sx={{ mt: 3, mb: 1 }}>流量与累计计数</Typography>
            <TableContainer sx={{ overflow: 'auto' }}>
              <Table size="small" sx={{ minWidth: 1120 }}>
                <TableHead>
                  <TableRow>
                    <TableCell>统计位置</TableCell>
                    <TableCell>接收 PPS</TableCell>
                    <TableCell>发送 PPS</TableCell>
                    <TableCell>接收带宽</TableCell>
                    <TableCell>发送带宽</TableCell>
                    <TableCell>累计接收 包/字节</TableCell>
                    <TableCell>累计发送 包/字节</TableCell>
                    <TableCell>丢弃/错误</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  <MetricDetailRow
                    label="边缘设备侧"
                    rate={detailNode.runtime.traffic_rates.device.current}
                    snapshot={detailNode.runtime.heartbeat.device}
                    stale={detailNode.runtime.traffic_rates.device.stale}
                  />
                  <MetricDetailRow
                    label="边缘互联侧"
                    rate={detailNode.runtime.traffic_rates.edge_interconnect.current}
                    snapshot={detailNode.runtime.heartbeat.interconnect}
                    stale={detailNode.runtime.traffic_rates.edge_interconnect.stale}
                  />
                  <MetricDetailRow
                    label="中心互联侧"
                    rate={detailNode.runtime.traffic_rates.center_interconnect.current}
                    snapshot={detailNode.runtime.center_interconnect}
                    stale={detailNode.runtime.traffic_rates.center_interconnect.stale}
                  />
                </TableBody>
              </Table>
            </TableContainer>

            <Typography variant="subtitle1" fontWeight={600} sx={{ mt: 3, mb: 1 }}>路由投影</Typography>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={3}>
              <Box><Typography variant="caption" color="text.secondary">边缘投影版本</Typography><Typography variant="body2">{formatCount(detailNode.runtime.heartbeat.projection_version)}</Typography></Box>
              <Box><Typography variant="caption" color="text.secondary">中心确认版本</Typography><Typography variant="body2">{formatCount(detailNode.runtime.acked_projection_version)}</Typography></Box>
              <Box><Typography variant="caption" color="text.secondary">待确认控制消息</Typography><Typography variant="body2">{formatCount(detailNode.runtime.pending_control)}</Typography></Box>
              <Box sx={{ flex: 1 }}><Typography variant="caption" color="text.secondary">同步错误</Typography><Typography variant="body2" color={detailNode.runtime.sync_error ? 'error.main' : 'text.primary'}>{detailNode.runtime.sync_error || '无'}</Typography></Box>
            </Stack>

            <Typography variant="subtitle1" fontWeight={600} sx={{ mt: 3, mb: 1 }}>节点资源保护</Typography>
            <TableContainer sx={{ overflow: 'auto' }}>
              <Table size="small" sx={{ maxWidth: 760 }}>
                <TableHead><TableRow><TableCell>事件</TableCell><TableCell align="right">边缘侧</TableCell><TableCell align="right">中心侧</TableCell></TableRow></TableHead>
                <TableBody>
                  {protectionRows.map((row) => (
                    <TableRow key={row.key}>
                      <TableCell>{row.label}</TableCell>
                      <TableCell align="right">{formatCount(detailNode.runtime.heartbeat.protection[row.key])}</TableCell>
                      <TableCell align="right">{formatCount(detailNode.runtime.center_protection[row.key])}</TableCell>
                    </TableRow>
                  ))}
                  <TableRow>
                    <TableCell>当前排队数据</TableCell>
                    <TableCell align="right">{formatCount(detailNode.runtime.heartbeat.protection.queued_data)}</TableCell>
                    <TableCell align="right">{formatCount(detailNode.runtime.center_protection.queued_data)}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </TableContainer>

            <Typography variant="subtitle1" fontWeight={600} sx={{ mt: 3, mb: 1 }}>中心全局保护</Typography>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={3} flexWrap="wrap" useFlexGap>
              <Typography variant="body2">待握手 {formatCount(detailNode.runtime.control_server_protection.pending_handshakes)}</Typography>
              <Typography variant="body2">活动节点 {formatCount(detailNode.runtime.control_server_protection.active_nodes)}</Typography>
              <Typography variant="body2">握手拒绝 {formatCount(detailNode.runtime.control_server_protection.pending_rejected)}</Typography>
              <Typography variant="body2">认证限速 {formatCount(detailNode.runtime.control_server_protection.auth_rate_rejected)}</Typography>
              <Typography variant="body2">认证失败 {formatCount(detailNode.runtime.control_server_protection.auth_failed)}</Typography>
              <Typography variant="body2">节点上限拒绝 {formatCount(detailNode.runtime.control_server_protection.max_nodes_rejected)}</Typography>
              <Typography variant="body2">协议拒绝 {formatCount(detailNode.runtime.control_server_protection.protocol_rejected)}</Typography>
              <Typography variant="body2">未知关键子类型 {formatCount(detailNode.runtime.control_server_protection.unsupported_subtype_drops)}</Typography>
              <Typography variant="body2">未认证 Type 0 {formatCount(detailNode.runtime.datagram_bridge_protection.unauthenticated_type0)}</Typography>
              <Typography variant="body2">无效 Type 0 {formatCount(detailNode.runtime.datagram_bridge_protection.invalid_type0)}</Typography>
              <Typography variant="body2">全局队列丢弃 {formatCount(detailNode.runtime.datagram_bridge_protection.global_queue_drops)}</Typography>
            </Stack>
          </DialogContent>
        )}
        <DialogActions><Button onClick={() => setDetailNode(null)}>关闭</Button></DialogActions>
      </Dialog>

      <ConfirmDialog
        isOpen={Boolean(confirm?.open)}
        title={confirm?.title || ''}
        message={confirm?.message || ''}
        confirmText={confirm?.confirmText}
        type={confirm?.type}
        onConfirm={() => void runConfirmedAction()}
        onCancel={() => setConfirm(null)}
      />
    </Box>
  )
}
