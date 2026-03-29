import { useState, useCallback, useMemo, useEffect } from 'react'
import { useLocation } from 'react-router-dom'
import { useTheme } from '@mui/material/styles'
import useMediaQuery from '@mui/material/useMediaQuery'
import {
  Box,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TablePagination,
  Typography,
  Card,
  CardContent,
  IconButton,
  Checkbox,
  Button,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Grid,
  Switch,
  FormControlLabel,
  Tooltip,
  Snackbar,
  Alert,
  Autocomplete,
  Stack,
  Menu,
  ListItemIcon,
  ListItemText,
  CircularProgress,
  List,
  ListItem,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import Refresh from '@mui/icons-material/Refresh'
import FileDownload from '@mui/icons-material/FileDownload'
import Visibility from '@mui/icons-material/Visibility'
import LinkIcon from '@mui/icons-material/Link'
import LinkOffIcon from '@mui/icons-material/LinkOff'
import Search from '@mui/icons-material/Search'
import Clear from '@mui/icons-material/Clear'
import Person from '@mui/icons-material/Person'
import Settings from '@mui/icons-material/Settings'
import DragIndicator from '@mui/icons-material/DragIndicator'
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { PageHeader } from '../../components/common/PageHeader'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { UserDetailPopover } from '../../components/UserDetailPopover'
import { RegionCascader } from '../../components/common/RegionCascader'
import { apiClient } from '../../services/api'
import { authService } from '../../services/auth'
import { relayService } from '../../services/relay'
import type { User, Relay } from '../../types'

// 通联日志数据类型
interface LogbookEntry {
  id: number
  user_id?: number
  username?: string
  my_callsign: string  // 我方呼号（冗余存储，支持客席发射）
  // 时间（数据库存储UTC，前端负责BJT转换）
  time_utc: string
  // 频率
  tx_frequency: number // MHz
  rx_frequency: number // MHz
  // 分区
  cq_zone: number
  itu_zone: number
  // 通信模式
  mode: string
  // 对方信息
  callsign: string
  their_rst: string
  their_power?: number // W
  their_qth?: string
  their_radio?: string
  their_antenna?: string
  // 我方信息
  my_rst: string
  my_power?: number // W
  my_qth?: string
  my_radio?: string
  my_antenna?: string
  // 备注
  notes?: string
  created_at?: string
  updated_at?: string
}

// API 响应类型
interface LogbookListResponse {
  code: number
  message: string
  data: {
    total: number
    items: LogbookEntry[]
    page: number
    page_size: number
  }
}

interface LogbookResponse {
  code: number
  message: string
  data: LogbookEntry
}

// 电台预设类型
interface RadioPreset {
  id: number
  user_id: number
  name: string
  radio: string
  antenna: string
  power: number | null
  qth: string
  sort_order: number
  created_at: string
  updated_at: string
}

interface RadioPresetListResponse {
  code: number
  message: string
  data: RadioPreset[]
}

interface RadioPresetResponse {
  code: number
  message: string
  data: RadioPreset
}

// 时间转换工具函数
// UTC 转 BJT：UTC + 8小时 = BJT
const utcToBjt = (utcTime: string): string => {
  if (!utcTime) return ''
  try {
    // 添加 'Z' 后缀强制解析为 UTC 时间
    const date = new Date(utcTime.replace(' ', 'T') + 'Z')
    // BJT = UTC + 8
    const bjtDate = new Date(date.getTime() + 8 * 60 * 60 * 1000)
    return bjtDate.toISOString().slice(0, 19).replace('T', ' ')
  } catch {
    return utcTime
  }
}

// BJT 转 UTC：BJT - 8小时 = UTC
// 输入的 BJT 时间会被当作本地时间解析（因为 datetime-local 返回的就是本地时间格式）
// 如果用户在 UTC+8 时区，本地时间就是 BJT，直接 toISOString() 就得到 UTC
const bjtToUtc = (bjtTime: string): string => {
  if (!bjtTime) return ''
  try {
    // 解析为本地时间（datetime-local 返回的就是本地时间格式）
    const date = new Date(bjtTime.replace(' ', 'T'))
    // toISOString() 自动转换为 UTC
    return date.toISOString().slice(0, 19).replace('T', ' ')
  } catch {
    return bjtTime
  }
}

// 获取当前UTC时间
const getCurrentUtcTime = (): string => {
  // 返回带秒的格式：YYYY-MM-DD HH:MM:SS
  return new Date().toISOString().slice(0, 19).replace('T', ' ')
}

// 获取当前BJT时间
const getCurrentBjtTime = (): string => {
  const now = new Date()
  const bjtDate = new Date(now.getTime() + 8 * 60 * 60 * 1000)
  return bjtDate.toISOString().slice(0, 19).replace('T', ' ')
}

// 通信模式选项
const MODE_OPTIONS = [
  'FM', 'AM', 'SSB', 'USB', 'LSB', 'CW', 'FT8', 'FT4', 'RTTY', 'PSK31',
  'DMR', 'D-Star', 'YSF', 'P25', 'NXDN', 'AX.25', 'SSTV', 'DV'
]

// API 调用函数
const logbookApi = {
  // 获取列表
  getList: async (params: {
    page?: number
    page_size?: number
    start_time?: string
    end_time?: string
    callsign?: string
    frequency?: number
    mode?: string
    user_id?: number
    username?: string
  }, isAdmin: boolean = false): Promise<LogbookListResponse> => {
    const queryParams = new URLSearchParams()
    if (params.page) queryParams.set('page', String(params.page))
    if (params.page_size) queryParams.set('page_size', String(params.page_size))
    if (params.start_time) queryParams.set('start_time', params.start_time)
    if (params.end_time) queryParams.set('end_time', params.end_time)
    if (params.callsign) queryParams.set('callsign', params.callsign)
    if (params.frequency) queryParams.set('frequency', String(params.frequency))
    if (params.mode) queryParams.set('mode', params.mode)
    if (params.user_id) queryParams.set('user_id', String(params.user_id))
    if (params.username) queryParams.set('username', params.username)

    const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
    const response = await apiClient.get(`${basePath}?${queryParams.toString()}`)
    return response
  },

  // 获取单条
  getOne: async (id: number, isAdmin: boolean = false): Promise<LogbookResponse> => {
    const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
    const response = await apiClient.get(`${basePath}/${id}`)
    return response
  },

  // 创建
  create: async (data: Omit<LogbookEntry, 'id'>): Promise<LogbookResponse> => {
    const response = await apiClient.post('/api/logbooks', data)
    return response
  },

  // 更新
  update: async (id: number, data: Partial<LogbookEntry>, isAdmin: boolean = false): Promise<LogbookResponse> => {
    const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
    const response = await apiClient.put(`${basePath}/${id}`, data)
    return response
  },

  // 删除单条
  delete: async (id: number, isAdmin: boolean = false): Promise<{ code: number; message: string }> => {
    const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
    const response = await apiClient.delete(`${basePath}/${id}`)
    return response
  },

  // 批量删除
  batchDelete: async (ids: number[], isAdmin: boolean = false): Promise<{ code: number; message: string }> => {
    const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
    const response = await apiClient.delete(`${basePath}/batch`, { data: { ids } })
    return response
  },
}

export function LogbookPage() {
  const location = useLocation()
  const isAdminPage = location.pathname.startsWith('/admin/')

  // 数据状态
  const [entries, setEntries] = useState<LogbookEntry[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1) // API 使用 1-based 分页
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [loading, setLoading] = useState(true)

  // 选择状态
  const [selectedIds, setSelectedIds] = useState<number[]>([])

  // 弹窗状态
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [viewDialogOpen, setViewDialogOpen] = useState(false)
  const [detailDialogOpen, setDetailDialogOpen] = useState(false)
  const [currentEntry, setCurrentEntry] = useState<LogbookEntry | null>(null)

  // 删除确认
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<number | number[] | null>(null)

  // 导出菜单
  const [exportAnchorEl, setExportAnchorEl] = useState<null | HTMLElement>(null)

  // 用户详情弹窗（管理员页面用）
  const [userDetailAnchorEl, setUserDetailAnchorEl] = useState<HTMLElement | null>(null)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)

  // 消息提示
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({
    open: false,
    message: '',
    severity: 'success',
  })

  // 电台预设
  const [presets, setPresets] = useState<RadioPreset[]>([])
  const [presetDialogOpen, setPresetDialogOpen] = useState(false)

  // 时间显示模式
  const [timeDisplayMode, setTimeDisplayMode] = useState<'bjt' | 'utc'>('bjt')

  // 搜索筛选状态
  const [searchFilters, setSearchFilters] = useState<{
    startTime: string
    endTime: string
    callsign: string
    frequency: string
    mode: string
    username: string
  }>({
    startTime: '',
    endTime: '',
    callsign: '',
    frequency: '',
    mode: '',
    username: '',
  })

  // 已应用的筛选条件（只有点击搜索按钮后才会更新）
  const [appliedFilters, setAppliedFilters] = useState<{
    startTime: string
    endTime: string
    callsign: string
    frequency: string
    mode: string
    username: string
  }>({
    startTime: '',
    endTime: '',
    callsign: '',
    frequency: '',
    mode: '',
    username: '',
  })

  // 筛选后的数据（服务端筛选，这里只做展示）
  const filteredEntries = useMemo(() => {
    return entries
  }, [entries])

  // 清除搜索筛选
  const clearSearchFilters = () => {
    setSearchFilters({
      startTime: '',
      endTime: '',
      callsign: '',
      frequency: '',
      mode: '',
      username: '',
    })
    setAppliedFilters({
      startTime: '',
      endTime: '',
      callsign: '',
      frequency: '',
      mode: '',
      username: '',
    })
  }

  // 应用搜索筛选
  const applySearchFilters = () => {
    setAppliedFilters({ ...searchFilters })
    setPage(1) // 重置到第一页
  }

  // 是否有活动的筛选条件（输入框中）
  const hasInputFilters = searchFilters.startTime || searchFilters.endTime ||
    searchFilters.callsign || searchFilters.frequency || searchFilters.mode || searchFilters.username

  // 是否有已应用的筛选条件
  const hasActiveFilters = appliedFilters.startTime || appliedFilters.endTime ||
    appliedFilters.callsign || appliedFilters.frequency || appliedFilters.mode || appliedFilters.username

  // 加载电台预设
  const loadPresets = useCallback(async () => {
    try {
      const response = await apiClient.get<RadioPresetListResponse>('/api/user/radio-presets')
      if (response.code === 200) {
        setPresets(response.data || [])
      }
    } catch (error) {
      console.error('加载电台预设失败:', error)
    }
  }, [])

  // 加载数据
  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const params: {
        page: number
        page_size: number
        start_time?: string
        end_time?: string
        callsign?: string
        frequency?: number
        mode?: string
        username?: string
      } = {
        page,
        page_size: rowsPerPage,
      }

      // 添加筛选条件（时间需要转换为 UTC）- 使用 appliedFilters
      if (appliedFilters.startTime) {
        params.start_time = bjtToUtc(appliedFilters.startTime)
      }
      if (appliedFilters.endTime) {
        params.end_time = bjtToUtc(appliedFilters.endTime)
      }
      if (appliedFilters.callsign) {
        params.callsign = appliedFilters.callsign
      }
      if (appliedFilters.frequency) {
        params.frequency = parseFloat(appliedFilters.frequency)
      }
      if (appliedFilters.mode) {
        params.mode = appliedFilters.mode
      }
      // 仅管理员页面支持用户名搜索
      if (isAdminPage && appliedFilters.username) {
        params.username = appliedFilters.username
      }

      const response = await logbookApi.getList(params, isAdminPage)
      if (response.code >= 200 && response.code < 300) {
        setEntries(response.data.items)
        setTotal(response.data.total)
      } else {
        setSnackbar({ open: true, message: response.message || '加载失败', severity: 'error' })
      }
    } catch (error) {
      console.error('加载通联日志失败:', error)
      setSnackbar({ open: true, message: '加载失败', severity: 'error' })
    } finally {
      setLoading(false)
    }
  }, [page, rowsPerPage, appliedFilters, isAdminPage])

  // 初始加载和已应用筛选条件变化时重新加载
  useEffect(() => {
    loadData()
  }, [loadData])

  // 初始加载预设
  useEffect(() => {
    loadPresets()
  }, [loadPresets])

  // 全选
  const handleSelectAll = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.checked) {
      setSelectedIds(filteredEntries.map(e => e.id))
    } else {
      setSelectedIds([])
    }
  }

  // 单选
  const handleSelect = (id: number) => {
    setSelectedIds(prev =>
      prev.includes(id) ? prev.filter(i => i !== id) : [...prev, id]
    )
  }

  // 刷新数据
  const handleRefresh = () => {
    loadData()
  }

  // 打开用户详情弹窗
  const handleOpenUserDetail = async (event: React.MouseEvent<HTMLElement>, userId: number) => {
    event.stopPropagation()
    const anchorEl = event.currentTarget
    try {
      const response = await apiClient.get(`/api/users/${userId}`)
      if (response.code >= 200 && response.code < 300 && response.data) {
        setSelectedUser(response.data)
        setUserDetailAnchorEl(anchorEl)
      }
    } catch (error) {
      console.error('获取用户信息失败:', error)
    }
  }

  // 关闭用户详情弹窗
  const handleCloseUserDetail = () => {
    setUserDetailAnchorEl(null)
    setSelectedUser(null)
  }

  // 打开新增弹窗
  const handleAdd = () => {
    setCurrentEntry(null)
    setAddDialogOpen(true)
  }

  // 打开编辑弹窗
  const handleEdit = (entry: LogbookEntry) => {
    setCurrentEntry(entry)
    setEditDialogOpen(true)
  }

  // 查看详情
  const handleView = (entry: LogbookEntry) => {
    setCurrentEntry(entry)
    setDetailDialogOpen(true)
  }

  // 删除单条
  const handleDelete = (id: number) => {
    setDeleteTarget(id)
    setDeleteConfirmOpen(true)
  }

  // 批量删除
  const handleBatchDelete = () => {
    if (selectedIds.length === 0) return
    setDeleteTarget(selectedIds)
    setDeleteConfirmOpen(true)
  }

  // 确认删除
  const confirmDelete = async () => {
    if (deleteTarget) {
      const ids = Array.isArray(deleteTarget) ? deleteTarget : [deleteTarget]
      try {
        if (ids.length === 1) {
          const response = await logbookApi.delete(ids[0], isAdminPage)
          if (response.code < 200 || response.code >= 300) {
            setSnackbar({ open: true, message: response.message || '删除失败', severity: 'error' })
            return
          }
        } else {
          const response = await logbookApi.batchDelete(ids, isAdminPage)
          if (response.code < 200 || response.code >= 300) {
            setSnackbar({ open: true, message: response.message || '删除失败', severity: 'error' })
            return
          }
        }
        setSelectedIds([])
        setSnackbar({ open: true, message: `成功删除 ${ids.length} 条记录`, severity: 'success' })
        loadData()
      } catch (error) {
        console.error('删除通联记录失败:', error)
        setSnackbar({ open: true, message: '删除失败', severity: 'error' })
      }
    }
    setDeleteConfirmOpen(false)
    setDeleteTarget(null)
  }

  // 导出菜单
  const handleExportClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    setExportAnchorEl(event.currentTarget)
  }

  const handleExportClose = () => {
    setExportAnchorEl(null)
  }

  // 导出 CSV
  const exportCSV = () => {
    const dataToExport = selectedIds.length > 0
      ? entries.filter(e => selectedIds.includes(e.id))
      : entries

    const headers = ['时间', '频率(MHz)', '模式', '对方呼号', 'RST(收/发)', 'CQ分区', 'ITU分区', 'QTH', '备注']
    const rows = dataToExport.map(e => [
      timeDisplayMode === 'bjt' ? utcToBjt(e.time_utc) : e.time_utc,
      e.tx_frequency,
      e.mode,
      e.callsign,
      `${e.their_rst}/${e.my_rst}`,
      e.cq_zone,
      e.itu_zone,
      e.their_qth || '',
      e.notes || '',
    ])

    const csvContent = [
      headers.join(','),
      ...rows.map(r => r.map(cell => `"${cell}"`).join(','))
    ].join('\n')

    const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `logbook_${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)

    setSnackbar({ open: true, message: `成功导出 ${dataToExport.length} 条记录`, severity: 'success' })
    handleExportClose()
  }

  // 导出 XLS (简单实现，使用 HTML table 格式)
  const exportXLS = () => {
    const dataToExport = selectedIds.length > 0
      ? entries.filter(e => selectedIds.includes(e.id))
      : entries

    const headers = ['时间', '频率(MHz)', '模式', '对方呼号', 'RST(收/发)', 'CQ分区', 'ITU分区', 'QTH', '备注']

    let tableHTML = '<html xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">'
    tableHTML += '<head><meta charset="UTF-8"></head><body><table border="1">'
    tableHTML += '<tr>' + headers.map(h => `<th>${h}</th>`).join('') + '</tr>'

    dataToExport.forEach(e => {
      tableHTML += '<tr>'
      tableHTML += `<td>${timeDisplayMode === 'bjt' ? utcToBjt(e.time_utc) : e.time_utc}</td>`
      tableHTML += `<td>${e.tx_frequency}</td>`
      tableHTML += `<td>${e.mode}</td>`
      tableHTML += `<td>${e.callsign}</td>`
      tableHTML += `<td>${e.their_rst}/${e.my_rst}</td>`
      tableHTML += `<td>${e.cq_zone}</td>`
      tableHTML += `<td>${e.itu_zone}</td>`
      tableHTML += `<td>${e.their_qth || ''}</td>`
      tableHTML += `<td>${e.notes || ''}</td>`
      tableHTML += '</tr>'
    })

    tableHTML += '</table></body></html>'

    const blob = new Blob([tableHTML], { type: 'application/vnd.ms-excel;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `logbook_${new Date().toISOString().slice(0, 10)}.xls`
    a.click()
    URL.revokeObjectURL(url)

    setSnackbar({ open: true, message: `成功导出 ${dataToExport.length} 条记录`, severity: 'success' })
    handleExportClose()
  }

  // 取消选择
  const clearSelection = () => {
    setSelectedIds([])
  }

  // 格式化频率显示
  const formatFrequency = (entry: LogbookEntry) => {
    const isSame = entry.tx_frequency === entry.rx_frequency
    if (isSame) {
      return entry.tx_frequency.toFixed(4)
    }
    return `${entry.tx_frequency.toFixed(4)} / ${entry.rx_frequency.toFixed(4)}`
  }

  // 获取时间显示
  const getTimeDisplay = (entry: LogbookEntry) => {
    return timeDisplayMode === 'bjt' ? utcToBjt(entry.time_utc) : entry.time_utc
  }

  return (
    <Box>
      <PageHeader
        title="通联日志"
        subtitle={isAdminPage ? '管理所有用户的通联记录' : '记录您的业余无线电通联'}
        actions={
          <Stack direction="row" spacing={1}>
            {!isAdminPage && (
              <Button
                variant="contained"
                startIcon={<Add />}
                onClick={handleAdd}
              >
                新增记录
              </Button>
            )}
            <IconButton onClick={handleRefresh} disabled={loading} color="primary">
              <Refresh />
            </IconButton>
          </Stack>
        }
      />

      {/* 搜索筛选栏 */}
      <Card sx={{ mb: 2 }}>
        <CardContent>
          <Grid container spacing={2} alignItems="center">
            {/* 时间区间搜索 */}
            <Grid size={{ xs: 12, sm: 6, md: 3 }}>
              <TextField
                fullWidth
                label="开始时间"
                type="datetime-local"
                size="small"
                value={searchFilters.startTime}
                onChange={(e) => setSearchFilters(prev => ({ ...prev, startTime: e.target.value }))}
                slotProps={{ inputLabel: { shrink: true } }}
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 6, md: 3 }}>
              <TextField
                fullWidth
                label="结束时间"
                type="datetime-local"
                size="small"
                value={searchFilters.endTime}
                onChange={(e) => setSearchFilters(prev => ({ ...prev, endTime: e.target.value }))}
                slotProps={{ inputLabel: { shrink: true } }}
              />
            </Grid>

            {/* 对方呼号搜索 */}
            <Grid size={{ xs: 12, sm: 6, md: 2 }}>
              <TextField
                fullWidth
                label="对方呼号"
                size="small"
                value={searchFilters.callsign}
                onChange={(e) => setSearchFilters(prev => ({ ...prev, callsign: e.target.value }))}
                placeholder="例如: BH1ABC"
                InputProps={{
                  startAdornment: <Search fontSize="small" sx={{ mr: 0.5, color: 'text.secondary' }} />,
                }}
              />
            </Grid>

            {/* 频率搜索 */}
            <Grid size={{ xs: 12, sm: 6, md: 2 }}>
              <TextField
                fullWidth
                label="频率 (MHz)"
                size="small"
                type="number"
                value={searchFilters.frequency}
                onChange={(e) => setSearchFilters(prev => ({ ...prev, frequency: e.target.value }))}
                placeholder="例如: 438.5"
                inputProps={{ step: 0.001 }}
              />
            </Grid>

            {/* 模式搜索 */}
            <Grid size={{ xs: 12, sm: 6, md: 2 }}>
              <FormControl fullWidth size="small">
                <InputLabel>模式</InputLabel>
                <Select
                  value={searchFilters.mode}
                  label="模式"
                  onChange={(e) => setSearchFilters(prev => ({ ...prev, mode: e.target.value }))}
                >
                  <MenuItem value="">全部</MenuItem>
                  {MODE_OPTIONS.map(mode => (
                    <MenuItem key={mode} value={mode}>{mode}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>

            {/* 用户名搜索（仅管理员页面） */}
            {isAdminPage && (
              <Grid size={{ xs: 12, sm: 6, md: 2 }}>
                <TextField
                  fullWidth
                  label="所属用户"
                  size="small"
                  value={searchFilters.username}
                  onChange={(e) => setSearchFilters(prev => ({ ...prev, username: e.target.value }))}
                  placeholder="输入用户名搜索"
                  InputProps={{
                    startAdornment: <Search fontSize="small" sx={{ mr: 0.5, color: 'text.secondary' }} />,
                  }}
                />
              </Grid>
            )}
          </Grid>

          {/* 操作按钮行 */}
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', mt: 2 }}>
            <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
              {/* 搜索按钮 */}
              <Button
                size="small"
                variant="contained"
                startIcon={<Search />}
                onClick={applySearchFilters}
                disabled={loading}
              >
                搜索
              </Button>

              {/* 时间显示模式切换 */}
              <Chip
                label="BJT"
                color={timeDisplayMode === 'bjt' ? 'primary' : 'default'}
                onClick={() => setTimeDisplayMode('bjt')}
                size="small"
              />
              <Chip
                label="UTC"
                color={timeDisplayMode === 'utc' ? 'primary' : 'default'}
                onClick={() => setTimeDisplayMode('utc')}
                size="small"
              />

              {/* 清除筛选按钮 */}
              {hasActiveFilters && (
                <Button
                  size="small"
                  variant="outlined"
                  startIcon={<Clear />}
                  onClick={clearSearchFilters}
                >
                  清除筛选
                </Button>
              )}

              {/* 筛选结果统计 */}
              {hasActiveFilters && (
                <Typography variant="body2" color="text.secondary">
                  找到 {total} 条记录
                </Typography>
              )}
            </Box>

            {/* 导出按钮 */}
            <Button
              variant="outlined"
              startIcon={<FileDownload />}
              onClick={handleExportClick}
              disabled={filteredEntries.length === 0}
            >
              导出 {selectedIds.length > 0 && `(${selectedIds.length})`}
            </Button>
          </Box>
        </CardContent>
      </Card>

      {/* 批量操作栏 */}
      {selectedIds.length > 0 && (
        <Card sx={{ mb: 2, bgcolor: 'primary.light', color: 'primary.contrastText' }}>
          <CardContent sx={{ py: 1.5 }}>
            <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
              <Typography variant="body2">
                已选择 {selectedIds.length} 项
              </Typography>
              <Button
                size="small"
                variant="contained"
                color="error"
                startIcon={<Delete />}
                onClick={handleBatchDelete}
              >
                批量删除
              </Button>
              <Button
                size="small"
                variant="outlined"
                color="inherit"
                onClick={clearSelection}
              >
                取消选择
              </Button>
            </Box>
          </CardContent>
        </Card>
      )}

      {/* 表格 */}
      <TableContainer component={Paper}>
        <Table sx={{ minWidth: 900 }}>
          <TableHead>
            <TableRow>
              <TableCell padding="checkbox">
                <Checkbox
                  indeterminate={selectedIds.length > 0 && selectedIds.length < filteredEntries.length}
                  checked={filteredEntries.length > 0 && selectedIds.length === filteredEntries.length}
                  onChange={handleSelectAll}
                />
              </TableCell>
              <TableCell>时间</TableCell>
              <TableCell>频率 (MHz)</TableCell>
              <TableCell>模式</TableCell>
              <TableCell>对方呼号</TableCell>
              <TableCell>RST (收/发)</TableCell>
              <TableCell>CQ/ITU</TableCell>
              <TableCell>QTH</TableCell>
              {isAdminPage && <TableCell>所属用户</TableCell>}
              <TableCell align="center">操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={isAdminPage ? 10 : 9} align="center" sx={{ py: 4 }}>
                  加载中...
                </TableCell>
              </TableRow>
            ) : filteredEntries.length === 0 ? (
              <TableRow>
                <TableCell colSpan={isAdminPage ? 10 : 9} align="center" sx={{ py: 4 }}>
                  <Typography color="text.secondary">
                    {hasActiveFilters ? '没有找到符合条件的记录' : (isAdminPage ? '暂无通联记录' : '暂无通联记录，点击"新增记录"添加您的第一条通联')}
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              filteredEntries.map((entry) => (
                  <TableRow
                    key={entry.id}
                    hover
                    selected={selectedIds.includes(entry.id)}
                  >
                    <TableCell padding="checkbox">
                      <Checkbox
                        checked={selectedIds.includes(entry.id)}
                        onChange={() => handleSelect(entry.id)}
                      />
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" noWrap>
                        {getTimeDisplay(entry)}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {timeDisplayMode === 'bjt' ? 'BJT' : 'UTC'}
                      </Typography>
                    </TableCell>
                    <TableCell>{formatFrequency(entry)}</TableCell>
                    <TableCell>
                      <Chip label={entry.mode} size="small" variant="outlined" />
                    </TableCell>
                    <TableCell>
                      <Typography fontWeight="medium">{entry.callsign}</Typography>
                    </TableCell>
                    <TableCell>{entry.their_rst}/{entry.my_rst}</TableCell>
                    <TableCell>{entry.cq_zone}/{entry.itu_zone}</TableCell>
                    <TableCell>{entry.their_qth || '-'}</TableCell>
                    {isAdminPage && (
                      <TableCell>
                        <Box
                          onClick={(e) => entry.user_id && handleOpenUserDetail(e, entry.user_id)}
                          sx={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: 0.5,
                            cursor: entry.user_id ? 'pointer' : 'default',
                            '&:hover': entry.user_id ? {
                              color: 'primary.main',
                              textDecoration: 'underline',
                            } : {},
                          }}
                        >
                          <Person fontSize="small" />
                          <Typography variant="body2">
                            {entry.username || '-'}
                          </Typography>
                        </Box>
                      </TableCell>
                    )}
                    <TableCell align="center">
                      <Stack direction="row" spacing={0.5} justifyContent="center">
                        <Tooltip title="查看详情">
                          <IconButton size="small" onClick={() => handleView(entry)}>
                            <Visibility fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="编辑">
                          <IconButton size="small" onClick={() => handleEdit(entry)} color="primary">
                            <Edit fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="删除">
                          <IconButton size="small" onClick={() => handleDelete(entry.id)} color="error">
                            <Delete fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Stack>
                    </TableCell>
                  </TableRow>
                ))
            )}
          </TableBody>
        </Table>
        <TablePagination
          component="div"
          count={total}
          page={page - 1}
          onPageChange={(_, newPage) => setPage(newPage + 1)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(e) => {
            setRowsPerPage(parseInt(e.target.value, 10))
            setPage(1)
          }}
          labelRowsPerPage="每页行数"
          labelDisplayedRows={({ from, to, count }) => `${from}-${to} 共 ${count} 条`}
          rowsPerPageOptions={[5, 10, 25, 50]}
        />
      </TableContainer>

      {/* 导出菜单 */}
      <Menu
        anchorEl={exportAnchorEl}
        open={Boolean(exportAnchorEl)}
        onClose={handleExportClose}
      >
        <MenuItem onClick={exportCSV}>
          <ListItemIcon><FileDownload fontSize="small" /></ListItemIcon>
          <ListItemText>导出 CSV</ListItemText>
        </MenuItem>
        <MenuItem onClick={exportXLS}>
          <ListItemIcon><FileDownload fontSize="small" /></ListItemIcon>
          <ListItemText>导出 XLS</ListItemText>
        </MenuItem>
      </Menu>

      {/* 新增记录弹窗 */}
      <LogbookFormDialog
        open={addDialogOpen}
        onClose={() => setAddDialogOpen(false)}
        onSave={async (entry) => {
          try {
            const response = await logbookApi.create(entry)
            if (response.code >= 200 && response.code < 300) {
              setAddDialogOpen(false)
              setSnackbar({ open: true, message: '添加成功', severity: 'success' })
              loadData()
            } else {
              setSnackbar({ open: true, message: response.message || '添加失败', severity: 'error' })
            }
          } catch (error) {
            console.error('添加通联记录失败:', error)
            setSnackbar({ open: true, message: '添加失败', severity: 'error' })
          }
        }}
        title="新增通联记录"
        presets={presets}
        onManagePresets={() => setPresetDialogOpen(true)}
        isAdminPage={isAdminPage}
      />

      {/* 编辑记录弹窗 */}
      <LogbookFormDialog
        open={editDialogOpen}
        onClose={() => setEditDialogOpen(false)}
        onSave={async (entry) => {
          if (currentEntry) {
            try {
              const response = await logbookApi.update(currentEntry.id, entry, isAdminPage)
              if (response.code >= 200 && response.code < 300) {
                setEditDialogOpen(false)
                setSnackbar({ open: true, message: '保存成功', severity: 'success' })
                loadData()
              } else {
                setSnackbar({ open: true, message: response.message || '保存失败', severity: 'error' })
              }
            } catch (error) {
              console.error('保存通联记录失败:', error)
              setSnackbar({ open: true, message: '保存失败', severity: 'error' })
            }
          }
        }}
        initialData={currentEntry}
        title="编辑通联记录"
        presets={presets}
        onManagePresets={() => setPresetDialogOpen(true)}
        isAdminPage={isAdminPage}
      />

      {/* 详情弹窗 */}
      <LogbookDetailDialog
        open={detailDialogOpen}
        onClose={() => setDetailDialogOpen(false)}
        entry={currentEntry}
        timeDisplayMode={timeDisplayMode}
      />

      {/* 预设管理弹窗 */}
      <PresetManageDialog
        open={presetDialogOpen}
        onClose={() => setPresetDialogOpen(false)}
        onRefresh={loadPresets}
      />

      {/* 删除确认 */}
      <ConfirmDialog
        isOpen={deleteConfirmOpen}
        title="确认删除"
        message={
          Array.isArray(deleteTarget)
            ? `确定要删除选中的 ${deleteTarget.length} 条记录吗？此操作不可撤销。`
            : '确定要删除这条记录吗？此操作不可撤销。'
        }
        onConfirm={confirmDelete}
        onCancel={() => {
          setDeleteConfirmOpen(false)
          setDeleteTarget(null)
        }}
        confirmText="删除"
        type="danger"
      />

      {/* 消息提示 */}
      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert
          severity={snackbar.severity}
          onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        >
          {snackbar.message}
        </Alert>
      </Snackbar>

      {/* 用户详情弹窗（管理员页面用） */}
      {isAdminPage && (
        <UserDetailPopover
          open={Boolean(userDetailAnchorEl)}
          anchorEl={userDetailAnchorEl}
          onClose={handleCloseUserDetail}
          user={selectedUser}
        />
      )}
    </Box>
  )
}

// 表单弹窗组件
interface LogbookFormDialogProps {
  open: boolean
  onClose: () => void
  onSave: (entry: Omit<LogbookEntry, 'id'>) => void
  initialData?: LogbookEntry | null
  title: string
  presets: RadioPreset[]
  onManagePresets: () => void
  isAdminPage: boolean
}



function LogbookFormDialog({ open, onClose, onSave, initialData, title, presets, onManagePresets, isAdminPage }: LogbookFormDialogProps) {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))

  const [formData, setFormData] = useState<Partial<LogbookEntry>>(() =>
    initialData || {
      my_callsign: '',
      time_utc: getCurrentUtcTime(),
      tx_frequency: 0,
      rx_frequency: 0,
      cq_zone: 24,
      itu_zone: 44,
      mode: 'FM',
      callsign: '',
      their_rst: '59',
      their_power: undefined,
      their_qth: '',
      their_radio: '',
      their_antenna: '',
      my_rst: '59',
      my_power: undefined,
      my_qth: '',
      my_radio: '',
      my_antenna: '',
      notes: '',
    }
  )

  const [timeMode, setTimeMode] = useState<'bjt' | 'utc'>('bjt')
  const [isRepeater, setIsRepeater] = useState(false) // 是否中继模式
  const [isSameFrequency, setIsSameFrequency] = useState(true) // 是否同频
  const [hasSubmitted, setHasSubmitted] = useState(false) // 是否尝试过提交

  // 中继台搜索相关状态
  const [relayLocation, setRelayLocation] = useState('')
  const [relayOptions, setRelayOptions] = useState<Relay[]>([])
  const [relaySearching, setRelaySearching] = useState(false)

  // 重置表单 - 打开时默认使用当前时间
  const resetForm = useCallback(() => {
    setHasSubmitted(false)
    if (initialData) {
      setFormData(initialData)
      setIsRepeater(initialData.tx_frequency !== initialData.rx_frequency)
    } else {
      // 获取当前用户的呼号和地址作为默认值
      const currentUser = authService.getStoredUser()
      setFormData({
        my_callsign: currentUser?.callsign || '',
        my_qth: currentUser?.address || '',
        time_utc: getCurrentUtcTime(),
        tx_frequency: 0,
        rx_frequency: 0,
        cq_zone: 24,
        itu_zone: 44,
        mode: 'FM',
        callsign: '',
        their_rst: '59',
        their_power: undefined,
        their_qth: '',
        their_radio: '',
        their_antenna: '',
        my_rst: '59',
        my_power: undefined,
        my_radio: '',
        my_antenna: '',
        notes: '',
      })
      setIsRepeater(false)
    }
  }, [initialData])

  // 打开弹窗时重置
  useEffect(() => {
    if (open) {
      resetForm()
    }
  }, [open, resetForm])

  // 处理时间变化（根据显示模式自动转换）
  const handleTimeChange = (value: string, mode: 'bjt' | 'utc') => {
    if (mode === 'bjt') {
      // 输入的是 BJT，转换为 UTC 存储
      setFormData(prev => ({
        ...prev,
        time_utc: bjtToUtc(value),
      }))
    } else {
      // 输入的是 UTC，直接存储
      setFormData(prev => ({
        ...prev,
        time_utc: value,
      }))
    }
  }

  // 获取当前显示的时间值
  const getDisplayTime = () => {
    if (!formData.time_utc) return ''
    return timeMode === 'bjt' ? utcToBjt(formData.time_utc) : formData.time_utc
  }

  // 使用当前时间
  const useCurrentTime = () => {
    // 数据库始终存储UTC时间
    // 直接获取当前UTC时间存储即可
    // 显示时会根据时区模式自动转换
    setFormData(prev => ({
      ...prev,
      time_utc: getCurrentUtcTime(),
    }))
  }

  // 处理同频/中继切换
  const handleFreqModeChange = (same: boolean) => {
    setIsRepeater(!same)
    if (same) {
      setFormData(prev => ({
        ...prev,
        rx_frequency: prev.tx_frequency,
      }))
    }
  }

  // 处理发射频率变化
  const handleTxFrequencyChange = (value: number) => {
    setFormData(prev => ({
      ...prev,
      tx_frequency: value,
      rx_frequency: !isRepeater ? value : prev.rx_frequency,
    }))
  }

  // 保存
  const handleSave = () => {
    setHasSubmitted(true)
    // 验证必填字段
    if (!formData.my_callsign || !formData.callsign || !formData.tx_frequency || !formData.mode) {
      return
    }

    onSave({
      my_callsign: formData.my_callsign || '',
      time_utc: formData.time_utc || getCurrentUtcTime(),
      tx_frequency: formData.tx_frequency || 0,
      rx_frequency: formData.rx_frequency || formData.tx_frequency || 0,
      cq_zone: formData.cq_zone || 24,
      itu_zone: formData.itu_zone || 44,
      mode: formData.mode || 'FM',
      callsign: formData.callsign || '',
      their_rst: formData.their_rst || '59',
      their_power: formData.their_power,
      their_qth: formData.their_qth || '',
      their_radio: formData.their_radio || '',
      their_antenna: formData.their_antenna || '',
      my_rst: formData.my_rst || '59',
      my_power: formData.my_power,
      my_qth: formData.my_qth || '',
      my_radio: formData.my_radio || '',
      my_antenna: formData.my_antenna || '',
      notes: formData.notes || '',
    })
  }

  // 搜索中继台
  const handleSearchRelays = async () => {
    const locationParts = relayLocation.split(' ').filter(Boolean)
    if (locationParts.length < 2) {
      return
    }

    setRelaySearching(true)
    try {
      const relays = await relayService.publicSearch(relayLocation)
      setRelayOptions(relays)
    } catch (error) {
      console.error('搜索中继台失败:', error)
      setRelayOptions([])
    } finally {
      setRelaySearching(false)
    }
  }

  // 快速填充中继台
  const handleRepeaterSelect = (relay: Relay | null) => {
    if (relay) {
      setIsRepeater(true)
      // 中继台存���的频率已经是MHz单位，直接使用
      // up_freq: 中继台上行（中继台接收），down_freq: 中继台下行（中继台发射）
      // 用户设备：发射频率 = 中继台上行，接收频率 = 中继台下行
      const txFreq = relay.up_freq ? parseFloat(relay.up_freq) : 0
      const rxFreq = relay.down_freq ? parseFloat(relay.down_freq) : 0
      // 如果收发频率不同，关闭同频开关
      if (txFreq !== rxFreq) {
        setIsSameFrequency(false)
      }
      setFormData(prev => ({
        ...prev,
        tx_frequency: txFreq,
        rx_frequency: rxFreq,
      }))
    }
  }

  // 快速填充我方设备
  const handleMyRadioSelect = (preset: RadioPreset | null) => {
    if (preset) {
      setFormData(prev => ({
        ...prev,
        my_radio: preset.radio,
        my_antenna: preset.antenna,
        my_qth: preset.qth || prev.my_qth,
        my_power: preset.power ?? prev.my_power,
      }))
    }
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="lg"
      fullWidth
      fullScreen={isMobile}
    >
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {title}
      </DialogTitle>
      <DialogContent dividers sx={{ p: { xs: 1.5, sm: 3 } }}>
        <Grid container spacing={{ xs: 1.5, sm: 2.5 }}>
          {/* 通联时间 */}
          <Grid size={12}>
            <Paper variant="outlined" sx={{ p: { xs: 1.5, sm: 2 }, bgcolor: 'grey.50' }}>
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1.5, flexWrap: 'wrap', gap: 1 }}>
                <Typography variant="subtitle2" color="text.secondary">
                  通联时间
                </Typography>
                <Button
                  size="small"
                  variant="outlined"
                  onClick={useCurrentTime}
                  startIcon={<Refresh fontSize="small" />}
                >
                  当前时间
                </Button>
              </Box>
              <Grid container spacing={{ xs: 1, sm: 2 }}>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <TextField
                    fullWidth
                    label="日期"
                    type="date"
                    size="small"
                    value={getDisplayTime().slice(0, 10) || ''}
                    onChange={(e) => {
                      const currentTime = getDisplayTime()
                      // 保留时间部分（时分秒）
                      const timePart = currentTime?.slice(11) || '00:00:00'
                      const newTime = e.target.value + ' ' + timePart
                      handleTimeChange(newTime, timeMode)
                    }}
                    slotProps={{ inputLabel: { shrink: true } }}
                  />
                </Grid>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <TextField
                    fullWidth
                    label="时间"
                    type="time"
                    size="small"
                    value={getDisplayTime().slice(11, 16) || ''}
                    onChange={(e) => {
                      const currentTime = getDisplayTime()
                      // 保留秒数部分（:SS），如果原时间没有秒则使用 :00
                      const secondsPart = currentTime?.length >= 19 ? currentTime.slice(14, 19) : ':00'
                      const newTime = (currentTime?.slice(0, 10) || new Date().toISOString().slice(0, 10)) + ' ' + e.target.value + secondsPart
                      handleTimeChange(newTime, timeMode)
                    }}
                    slotProps={{ inputLabel: { shrink: true } }}
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 4, md: 3 }}>
                  <FormControl fullWidth size="small">
                    <InputLabel>时区</InputLabel>
                    <Select
                      value={timeMode}
                      label="时区"
                      onChange={(e) => setTimeMode(e.target.value as 'bjt' | 'utc')}
                    >
                      <MenuItem value="bjt">BJT (北京时间)</MenuItem>
                      <MenuItem value="utc">UTC (协调世界时)</MenuItem>
                    </Select>
                  </FormControl>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 频率设置 */}
          <Grid size={12}>
            <Paper variant="outlined" sx={{ p: { xs: 1.5, sm: 2 }, bgcolor: 'grey.50' }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 1.5, flexWrap: 'wrap' }}>
                <Typography variant="subtitle2" color="text.secondary">
                  频率设置
                </Typography>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={isSameFrequency}
                        onChange={(e) => {
                          const same = e.target.checked
                          setIsSameFrequency(same)
                          if (same) {
                            setFormData(prev => ({ ...prev, rx_frequency: prev.tx_frequency }))
                          }
                        }}
                        size="small"
                      />
                    }
                    label={
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                        {isSameFrequency ? <LinkIcon fontSize="small" /> : <LinkOffIcon fontSize="small" />}
                        <Typography variant="caption">
                          {isSameFrequency ? '同频' : '异频'}
                        </Typography>
                      </Box>
                    }
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={isRepeater}
                        onChange={(e) => setIsRepeater(e.target.checked)}
                        size="small"
                      />
                    }
                    label={<Typography variant="caption">中继</Typography>}
                  />
                </Box>
              </Box>

              {/* 中继台快速选择 - 仅中继模式显示 */}
              {isRepeater && (
                <Box sx={{ mb: 2 }}>
                  <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, gap: 1, alignItems: { sm: 'flex-end' }, mb: 2 }}>
                    <Box sx={{ flex: 1, minWidth: 0 }}>
                      <RegionCascader
                        value={relayLocation}
                        onChange={setRelayLocation}
                        label="选择地区搜索中继台"
                        size="small"
                      />
                    </Box>
                    <Button
                      variant="outlined"
                      size="small"
                      onClick={handleSearchRelays}
                      disabled={relaySearching}
                      startIcon={relaySearching ? <CircularProgress size={16} color="inherit" /> : <Search fontSize="small" />}
                      sx={{ minWidth: 80, height: 40 }}
                    >
                      {relaySearching ? '搜索中...' : '搜索'}
                    </Button>
                  </Box>
                  {relayOptions.length > 0 && (
                    <Autocomplete
                      size="small"
                      options={relayOptions}
                      getOptionLabel={(option) => option.name}
                      onChange={(_, value) => handleRepeaterSelect(value)}
                      renderInput={(params) => (
                        <TextField
                          {...params}
                          label="选择中继台"
                          placeholder="选择中继台自动填入频率..."
                        />
                      )}
                      renderOption={(props, option) => {
                        const txFreq = option.up_freq || '-'
                        const rxFreq = option.down_freq || '-'
                        return (
                          <li {...props} key={option.id}>
                            <Box>
                              <Typography variant="body2">{option.name}</Typography>
                              <Typography variant="caption" color="text.secondary">
                                发: {txFreq} MHz / 收: {rxFreq} MHz
                                {option.location && ` · ${option.location}`}
                              </Typography>
                            </Box>
                          </li>
                        )
                      }}
                      noOptionsText="暂无中继台数据"
                    />
                  )}
                </Box>
              )}

              <Grid container spacing={{ xs: 1, sm: 2 }}>
                <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                  <TextField
                    fullWidth
                    required
                    size="small"
                    label={!isRepeater ? "频率 (MHz)" : "发射频率 (MHz)"}
                    type="number"
                    value={formData.tx_frequency || ''}
                    onChange={(e) => handleTxFrequencyChange(parseFloat(e.target.value) || 0)}
                    inputProps={{ step: 0.001 }}
                    error={hasSubmitted && !formData.tx_frequency}
                    helperText={hasSubmitted && !formData.tx_frequency ? '必填' : ''}
                  />
                </Grid>
                {!isSameFrequency && (
                  <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                    <TextField
                      fullWidth
                      label="接收频率 (MHz)"
                      type="number"
                      size="small"
                      value={formData.rx_frequency || ''}
                      onChange={(e) => setFormData(prev => ({ ...prev, rx_frequency: parseFloat(e.target.value) || 0 }))}
                      inputProps={{ step: 0.001 }}
                    />
                  </Grid>
                )}
              </Grid>
            </Paper>
          </Grid>

          {/* 无线电信息 */}
          <Grid size={12}>
            <Paper variant="outlined" sx={{ p: { xs: 1.5, sm: 2 }, bgcolor: 'grey.50' }}>
              <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1.5 }}>
                无线电信息
              </Typography>
              <Grid container spacing={{ xs: 1, sm: 2 }}>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <FormControl fullWidth size="small">
                    <InputLabel>通信模式</InputLabel>
                    <Select
                      value={formData.mode || 'FM'}
                      label="通信模式"
                      onChange={(e) => setFormData(prev => ({ ...prev, mode: e.target.value }))}
                    >
                      {MODE_OPTIONS.map(mode => (
                        <MenuItem key={mode} value={mode}>{mode}</MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                </Grid>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <TextField
                    fullWidth
                    label="CQ 分区"
                    type="number"
                    size="small"
                    value={formData.cq_zone || ''}
                    onChange={(e) => setFormData(prev => ({ ...prev, cq_zone: parseInt(e.target.value) || 1 }))}
                    inputProps={{ min: 1, max: 40 }}
                  />
                </Grid>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <TextField
                    fullWidth
                    label="ITU 分区"
                    type="number"
                    size="small"
                    value={formData.itu_zone || ''}
                    onChange={(e) => setFormData(prev => ({ ...prev, itu_zone: parseInt(e.target.value) || 1 }))}
                    inputProps={{ min: 1, max: 90 }}
                  />
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 双方信息 - 两列布局 */}
          <Grid size={12}>
            <Grid container spacing={{ xs: 1, sm: 2 }}>
              {/* 对方信息 */}
              <Grid size={{ xs: 12, md: 6 }}>
                <Paper
                  variant="outlined"
                  sx={{
                    p: { xs: 1.5, sm: 2 },
                    height: '100%',
                    borderColor: 'primary.light',
                    bgcolor: 'primary.50',
                  }}
                >
                  <Typography variant="subtitle2" color="primary.main" sx={{ mb: 1.5, fontWeight: 600 }}>
                    对方信息
                  </Typography>
                  <Grid container spacing={{ xs: 1, sm: 1.5 }}>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        required
                        label="对方呼号"
                        size="small"
                        value={formData.callsign || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, callsign: e.target.value.toUpperCase() }))}
                        placeholder="例如: BH1ABC"
                        error={hasSubmitted && !formData.callsign}
                        helperText={hasSubmitted && !formData.callsign ? '必填' : ''}
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="QTH (位置)"
                        size="small"
                        value={formData.their_qth || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_qth: e.target.value }))}
                        placeholder="例如: 北京"
                      />
                    </Grid>
                    <Grid size={{ xs: 6, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="收信报告 (RST)"
                        size="small"
                        value={formData.their_rst || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_rst: e.target.value }))}
                        placeholder="59 / 599"
                      />
                    </Grid>
                    <Grid size={{ xs: 6, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="功率 (W)"
                        type="number"
                        size="small"
                        value={formData.their_power || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_power: parseInt(e.target.value) || undefined }))}
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="设备型号"
                        size="small"
                        value={formData.their_radio || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_radio: e.target.value }))}
                        placeholder="例如: IC-9700"
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="天馈"
                        size="small"
                        value={formData.their_antenna || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_antenna: e.target.value }))}
                        placeholder="例如: 八木"
                      />
                    </Grid>
                  </Grid>
                </Paper>
              </Grid>

              {/* 我方信息 */}
              <Grid size={{ xs: 12, md: 6 }}>
                <Paper
                  variant="outlined"
                  sx={{
                    p: { xs: 1.5, sm: 2 },
                    height: '100%',
                    borderColor: 'secondary.light',
                    bgcolor: 'secondary.50',
                  }}
                >
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1.5, gap: 1 }}>
                    <Typography variant="subtitle2" color="secondary.main" sx={{ fontWeight: 600 }}>
                      我方信息
                    </Typography>
                    {!isAdminPage && (
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                        <Autocomplete
                          size="small"
                          options={presets}
                          getOptionLabel={(option) => option.name}
                          onChange={(_, value) => handleMyRadioSelect(value)}
                          sx={{ width: 140 }}
                          renderInput={(params) => (
                            <TextField
                              {...params}
                              label="快速选择"
                              size="small"
                            />
                          )}
                          renderOption={(props, option) => (
                            <li {...props} key={option.id}>
                              <Box>
                                <Typography variant="body2">{option.name}</Typography>
                                <Typography variant="caption" color="text.secondary">
                                  {option.radio} / {option.antenna}
                                </Typography>
                              </Box>
                            </li>
                          )}
                        />
                        <Tooltip title="管理预设">
                          <IconButton size="small" onClick={onManagePresets}>
                            <Settings fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Box>
                    )}
                  </Box>
                  <Grid container spacing={{ xs: 1, sm: 1.5 }}>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        required
                        label="我方呼号"
                        size="small"
                        value={formData.my_callsign || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_callsign: e.target.value }))}
                        placeholder="例如: BG7XXX"
                        error={hasSubmitted && !formData.my_callsign}
                        helperText={hasSubmitted && !formData.my_callsign ? '必填' : ''}
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="QTH (位置)"
                        size="small"
                        value={formData.my_qth || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_qth: e.target.value }))}
                        placeholder="例如: 广州"
                      />
                    </Grid>
                    <Grid size={{ xs: 6, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="发信报告 (RST)"
                        size="small"
                        value={formData.my_rst || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_rst: e.target.value }))}
                        placeholder="59 / 599"
                      />
                    </Grid>
                    <Grid size={{ xs: 6, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="功率 (W)"
                        type="number"
                        size="small"
                        value={formData.my_power || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_power: parseInt(e.target.value) || undefined }))}
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="设备型号"
                        size="small"
                        value={formData.my_radio || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_radio: e.target.value }))}
                        placeholder="例如: FT-991A"
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="天馈"
                        size="small"
                        value={formData.my_antenna || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_antenna: e.target.value }))}
                        placeholder="例如: GP"
                      />
                    </Grid>
                  </Grid>
                </Paper>
              </Grid>
            </Grid>
          </Grid>

          {/* 备注 */}
          <Grid size={12}>
            <TextField
              fullWidth
              label="备注"
              size="small"
              multiline
              rows={2}
              value={formData.notes || ''}
              onChange={(e) => setFormData(prev => ({ ...prev, notes: e.target.value }))}
              placeholder="记录通联的详细信息..."
            />
          </Grid>
        </Grid>
      </DialogContent>
      <DialogActions sx={{ px: { xs: 2, sm: 3 }, py: 2 }}>
        <Button onClick={onClose}>取消</Button>
        <Button variant="contained" onClick={handleSave}>
          保存
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// 详情弹窗组件
interface LogbookDetailDialogProps {
  open: boolean
  onClose: () => void
  entry: LogbookEntry | null
  timeDisplayMode: 'bjt' | 'utc'
}

function LogbookDetailDialog({ open, onClose, entry, timeDisplayMode }: LogbookDetailDialogProps) {
  if (!entry) return null

  const timeLabel = timeDisplayMode === 'bjt' ? '北京时间 (BJT)' : '协调世界时 (UTC)'
  const timeValue = timeDisplayMode === 'bjt' ? utcToBjt(entry.time_utc) : entry.time_utc
  const isSameFrequency = entry.tx_frequency === entry.rx_frequency

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>
        通联详情
        <Chip label={entry.callsign} color="primary" size="small" sx={{ ml: 2 }} />
      </DialogTitle>
      <DialogContent dividers>
        <Grid container spacing={2}>
          {/* 基本信息 */}
          <Grid size={12}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              基本信息
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Grid container spacing={1}>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">{timeLabel}</Typography>
                  <Typography variant="body2">{timeValue}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">通信模式</Typography>
                  <Typography variant="body2"><Chip label={entry.mode} size="small" /></Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">发射频率</Typography>
                  <Typography variant="body2">{entry.tx_frequency} MHz</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">接收频率</Typography>
                  <Typography variant="body2">
                    {isSameFrequency ? '同频' : `${entry.rx_frequency} MHz`}
                  </Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">CQ 分区</Typography>
                  <Typography variant="body2">{entry.cq_zone}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">ITU 分区</Typography>
                  <Typography variant="body2">{entry.itu_zone}</Typography>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 信号报告 */}
          <Grid size={12}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              信号报告
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Grid container spacing={1}>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">收信报告 (对方)</Typography>
                  <Typography variant="body2" fontWeight="medium">{entry.their_rst}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">发信报告 (我方)</Typography>
                  <Typography variant="body2" fontWeight="medium">{entry.my_rst}</Typography>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 对方信息 */}
          <Grid size={12}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              对方信息
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Grid container spacing={1}>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">QTH</Typography>
                  <Typography variant="body2">{entry.their_qth || '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">功率</Typography>
                  <Typography variant="body2">{entry.their_power ? `${entry.their_power} W` : '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">设备</Typography>
                  <Typography variant="body2">{entry.their_radio || '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">天馈</Typography>
                  <Typography variant="body2">{entry.their_antenna || '-'}</Typography>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 我方信息 */}
          <Grid size={12}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              我方信息
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Grid container spacing={1}>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">QTH</Typography>
                  <Typography variant="body2">{entry.my_qth || '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">功率</Typography>
                  <Typography variant="body2">{entry.my_power ? `${entry.my_power} W` : '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">设备</Typography>
                  <Typography variant="body2">{entry.my_radio || '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">天馈</Typography>
                  <Typography variant="body2">{entry.my_antenna || '-'}</Typography>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 备注 */}
          {entry.notes && (
            <Grid size={12}>
              <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                备注
              </Typography>
              <Paper variant="outlined" sx={{ p: 2 }}>
                <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>
                  {entry.notes}
                </Typography>
              </Paper>
            </Grid>
          )}
        </Grid>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>关闭</Button>
      </DialogActions>
    </Dialog>
  )
}

// 预设管理对话框属性
interface PresetManageDialogProps {
  open: boolean
  onClose: () => void
  onRefresh: () => void
}

// 可排序的预设列表项
interface SortablePresetItemProps {
  preset: RadioPreset
  onEdit: (preset: RadioPreset) => void
  onDelete: (id: number) => void
}

function SortablePresetItem({ preset, onEdit, onDelete }: SortablePresetItemProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: preset.id })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    backgroundColor: isDragging ? 'action.hover' : 'transparent',
  }

  return (
    <ListItem
      ref={setNodeRef}
      style={style}
      secondaryAction={
        <Box>
          <IconButton size="small" onClick={() => onEdit(preset)}>
            <Edit fontSize="small" />
          </IconButton>
          <IconButton size="small" color="error" onClick={() => onDelete(preset.id)}>
            <Delete fontSize="small" />
          </IconButton>
        </Box>
      }
    >
      <Box {...attributes} {...listeners} sx={{ cursor: 'grab', mr: 1, display: 'flex', alignItems: 'center' }}>
        <DragIndicator color="action" />
      </Box>
      <ListItemText
        primary={preset.name}
        secondary={
          <Typography variant="body2" color="text.secondary">
            {preset.radio} / {preset.antenna}
            {preset.power && ` / ${preset.power}W`}
            {preset.qth && ` / ${preset.qth}`}
          </Typography>
        }
      />
    </ListItem>
  )
}

// 预设管理对话框
function PresetManageDialog({ open, onClose, onRefresh }: PresetManageDialogProps) {
  const [presets, setPresets] = useState<RadioPreset[]>([])
  const [loading, setLoading] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [editingPreset, setEditingPreset] = useState<RadioPreset | null>(null)
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({ open: false, message: '', severity: 'success' })
  const [formData, setFormData] = useState({
    name: '',
    radio: '',
    antenna: '',
    power: '' as number | '',
    qth: ''
  })

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  )

  const loadPresets = useCallback(async () => {
    setLoading(true)
    try {
      const response = await apiClient.get<RadioPresetListResponse>('/api/user/radio-presets')
      if (response.code >= 200 && response.code < 300 && response.data) {
        setPresets(response.data)
      }
    } catch (error) {
      console.error('加载预设失败:', error)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) {
      loadPresets()
    }
  }, [open, loadPresets])

  const handleAdd = () => {
    setEditingPreset(null)
    setFormData({ name: '', radio: '', antenna: '', power: '', qth: '' })
    setEditDialogOpen(true)
  }

  const handleEdit = (preset: RadioPreset) => {
    setEditingPreset(preset)
    setFormData({
      name: preset.name,
      radio: preset.radio,
      antenna: preset.antenna,
      power: preset.power ?? '',
      qth: preset.qth || ''
    })
    setEditDialogOpen(true)
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定要删除这个预设吗？')) return

    try {
      const response = await apiClient.delete(`/api/user/radio-presets/${id}`)
      if (response.code >= 200 && response.code < 300) {
        setSnackbar({ open: true, message: '删除成功', severity: 'success' })
        loadPresets()
        onRefresh()
      } else {
        setSnackbar({ open: true, message: response.message || '删除失败', severity: 'error' })
      }
    } catch (error) {
      console.error('删除预设失败:', error)
      setSnackbar({ open: true, message: '删除失败', severity: 'error' })
    }
  }

  const handleSave = async () => {
    if (!formData.name || !formData.radio || !formData.antenna) {
      setSnackbar({ open: true, message: '请填写必填项', severity: 'error' })
      return
    }

    try {
      const data = {
        name: formData.name,
        radio: formData.radio,
        antenna: formData.antenna,
        power: formData.power || null,
        qth: formData.qth || null
      }

      let response
      if (editingPreset) {
        response = await apiClient.put(`/api/user/radio-presets/${editingPreset.id}`, data)
      } else {
        response = await apiClient.post('/api/user/radio-presets', data)
      }

      if (response.code >= 200 && response.code < 300) {
        setSnackbar({ open: true, message: editingPreset ? '保存成功' : '添加成功', severity: 'success' })
        setEditDialogOpen(false)
        loadPresets()
        onRefresh()
      } else {
        setSnackbar({ open: true, message: response.message || '操作失败', severity: 'error' })
      }
    } catch (error) {
      console.error('保存预设失败:', error)
      setSnackbar({ open: true, message: '操作失败', severity: 'error' })
    }
  }

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event

    if (over && active.id !== over.id) {
      const oldIndex = presets.findIndex(p => p.id === active.id)
      const newIndex = presets.findIndex(p => p.id === over.id)

      const newPresets = arrayMove(presets, oldIndex, newIndex)
      setPresets(newPresets)

      // 保存排序到后端
      try {
        const orders = newPresets.map((p, index) => ({ id: p.id, order: index }))
        await apiClient.put('/api/user/radio-presets/reorder', { orders })
        onRefresh()
      } catch (error) {
        console.error('保存排序失败:', error)
        setSnackbar({ open: true, message: '保存排序失败', severity: 'error' })
        loadPresets() // 恢复原顺序
      }
    }
  }

  return (
    <>
      <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
        <DialogTitle>
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <Typography variant="h6">管理电台预设</Typography>
            <Button startIcon={<Add />} onClick={handleAdd} variant="contained" size="small">
              添加预设
            </Button>
          </Box>
        </DialogTitle>
        <DialogContent>
          {loading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
              <CircularProgress />
            </Box>
          ) : presets.length === 0 ? (
            <Box sx={{ textAlign: 'center', py: 4, color: 'text.secondary' }}>
              <Typography>暂无预设，点击上方按钮添加</Typography>
            </Box>
          ) : (
            <DndContext
              sensors={sensors}
              collisionDetection={closestCenter}
              onDragEnd={handleDragEnd}
            >
              <SortableContext
                items={presets.map(p => p.id)}
                strategy={verticalListSortingStrategy}
              >
                <List>
                  {presets.map((preset) => (
                    <SortablePresetItem
                      key={preset.id}
                      preset={preset}
                      onEdit={handleEdit}
                      onDelete={handleDelete}
                    />
                  ))}
                </List>
              </SortableContext>
            </DndContext>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>关闭</Button>
        </DialogActions>
      </Dialog>

      {/* 添加/编辑预设弹窗 */}
      <Dialog open={editDialogOpen} onClose={() => setEditDialogOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>{editingPreset ? '编辑预设' : '添加预设'}</DialogTitle>
        <DialogContent>
          <Box sx={{ pt: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              fullWidth
              required
              label="预设名称"
              size="small"
              value={formData.name}
              onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
              placeholder="例如: 家里台"
            />
            <TextField
              fullWidth
              required
              label="电台设备"
              size="small"
              value={formData.radio}
              onChange={(e) => setFormData(prev => ({ ...prev, radio: e.target.value }))}
              placeholder="例如: IC-7300"
            />
            <TextField
              fullWidth
              required
              label="天线"
              size="small"
              value={formData.antenna}
              onChange={(e) => setFormData(prev => ({ ...prev, antenna: e.target.value }))}
              placeholder="例如: DP天线"
            />
            <TextField
              fullWidth
              label="功率 (W)"
              size="small"
              type="number"
              value={formData.power}
              onChange={(e) => setFormData(prev => ({ ...prev, power: e.target.value ? Number(e.target.value) : '' }))}
              placeholder="例如: 100"
            />
            <TextField
              fullWidth
              label="QTH"
              size="small"
              value={formData.qth}
              onChange={(e) => setFormData(prev => ({ ...prev, qth: e.target.value }))}
              placeholder="例如: 广东省广州市"
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditDialogOpen(false)}>取消</Button>
          <Button onClick={handleSave} variant="contained">保存</Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </>
  )
}
