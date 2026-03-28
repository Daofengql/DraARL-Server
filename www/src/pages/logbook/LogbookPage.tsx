import { useState, useCallback, useMemo } from 'react'
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
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import Refresh from '@mui/icons-material/Refresh'
import FileDownload from '@mui/icons-material/FileDownload'
import Visibility from '@mui/icons-material/Visibility'
import LinkIcon from '@mui/icons-material/Link'
import LinkOffIcon from '@mui/icons-material/LinkOff'
import { PageHeader } from '../../components/common/PageHeader'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

// 通联日志数据类型
interface LogbookEntry {
  id: number
  user_id?: number
  username?: string
  // 时间
  time_utc: string
  time_bjt: string
  // 频率
  tx_frequency: number // MHz
  rx_frequency: number // MHz
  same_frequency: boolean // 是否同频
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

// 通信模式选项
const MODE_OPTIONS = [
  'FM', 'AM', 'SSB', 'USB', 'LSB', 'CW', 'FT8', 'FT4', 'RTTY', 'PSK31',
  'DMR', 'D-Star', 'YSF', 'P25', 'NXDN', 'AX.25', 'SSTV', 'DV'
]

// 模拟数据
const MOCK_DATA: LogbookEntry[] = [
  {
    id: 1,
    user_id: 1,
    username: 'BG7XXX',
    time_utc: '2024-03-28 08:00:00',
    time_bjt: '2024-03-28 16:00:00',
    tx_frequency: 438.5,
    rx_frequency: 438.5,
    same_frequency: true,
    cq_zone: 24,
    itu_zone: 44,
    mode: 'FM',
    callsign: 'BH1ABC',
    their_rst: '59',
    their_power: 50,
    their_qth: '北京',
    their_radio: 'IC-9700',
    their_antenna: '八木',
    my_rst: '59',
    my_power: 25,
    my_qth: '广州',
    my_radio: 'FT-991A',
    my_antenna: 'GP',
    notes: '通联良好',
  },
  {
    id: 2,
    user_id: 1,
    username: 'BG7XXX',
    time_utc: '2024-03-27 12:30:00',
    time_bjt: '2024-03-27 20:30:00',
    tx_frequency: 14.27,
    rx_frequency: 14.27,
    same_frequency: true,
    cq_zone: 24,
    itu_zone: 44,
    mode: 'SSB',
    callsign: 'JA1ZLC',
    their_rst: '57',
    their_power: 100,
    their_qth: '东京',
    their_radio: 'TS-990',
    their_antenna: 'DP',
    my_rst: '55',
    my_power: 100,
    my_qth: '广州',
    my_radio: 'FT-991A',
    my_antenna: 'DP',
    notes: '传播一般',
  },
]

// 中继台列表（预留）
const REPEATER_OPTIONS = [
  { id: 1, name: '广州438.500', freq: 438.5, offset: -5 },
  { id: 2, name: '深圳439.500', freq: 439.5, offset: -5 },
  { id: 3, name: '北京439.750', freq: 439.75, offset: -5 },
]

export function LogbookPage() {
  const location = useLocation()
  const isAdminPage = location.pathname.startsWith('/admin/')

  // 数据状态
  const [entries, setEntries] = useState<LogbookEntry[]>(MOCK_DATA)
  const [total, setTotal] = useState(MOCK_DATA.length)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [loading, setLoading] = useState(false)

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

  // 消息提示
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({
    open: false,
    message: '',
    severity: 'success',
  })

  // 时间显示模式
  const [timeDisplayMode, setTimeDisplayMode] = useState<'bjt' | 'utc'>('bjt')

  // 全选
  const handleSelectAll = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.checked) {
      setSelectedIds(entries.map(e => e.id))
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
    setLoading(true)
    setTimeout(() => {
      setLoading(false)
      setSnackbar({ open: true, message: '刷新成功', severity: 'success' })
    }, 500)
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
  const confirmDelete = () => {
    if (deleteTarget) {
      const ids = Array.isArray(deleteTarget) ? deleteTarget : [deleteTarget]
      setEntries(prev => prev.filter(e => !ids.includes(e.id)))
      setSelectedIds([])
      setSnackbar({ open: true, message: `成功删除 ${ids.length} 条记录`, severity: 'success' })
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
      timeDisplayMode === 'bjt' ? e.time_bjt : e.time_utc,
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
      tableHTML += `<td>${timeDisplayMode === 'bjt' ? e.time_bjt : e.time_utc}</td>`
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
    if (entry.same_frequency) {
      return entry.tx_frequency.toFixed(4)
    }
    return `${entry.tx_frequency.toFixed(4)} / ${entry.rx_frequency.toFixed(4)}`
  }

  // 获取时间显示
  const getTimeDisplay = (entry: LogbookEntry) => {
    return timeDisplayMode === 'bjt' ? entry.time_bjt : entry.time_utc
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

      {/* 筛选栏和工具栏 */}
      <Card sx={{ mb: 2 }}>
        <CardContent>
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between' }}>
            <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
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
            </Box>

            {/* 导出按钮 */}
            <Button
              variant="outlined"
              startIcon={<FileDownload />}
              onClick={handleExportClick}
              disabled={entries.length === 0}
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
                  indeterminate={selectedIds.length > 0 && selectedIds.length < entries.length}
                  checked={entries.length > 0 && selectedIds.length === entries.length}
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
              <TableCell align="center">操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={9} align="center" sx={{ py: 4 }}>
                  加载中...
                </TableCell>
              </TableRow>
            ) : entries.length === 0 ? (
              <TableRow>
                <TableCell colSpan={9} align="center" sx={{ py: 4 }}>
                  <Typography color="text.secondary">
                    {isAdminPage ? '暂无通联记录' : '暂无通联记录，点击"新增记录"添加您的第一条通联'}
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              entries
                .slice(page * rowsPerPage, page * rowsPerPage + rowsPerPage)
                .map((entry) => (
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
          page={page}
          onPageChange={(_, newPage) => setPage(newPage)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(e) => {
            setRowsPerPage(parseInt(e.target.value, 10))
            setPage(0)
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
        onSave={(entry) => {
          const newEntry = { ...entry, id: Date.now() }
          setEntries(prev => [newEntry, ...prev])
          setTotal(prev => prev + 1)
          setAddDialogOpen(false)
          setSnackbar({ open: true, message: '添加成功', severity: 'success' })
        }}
        title="新增通联记录"
      />

      {/* 编辑记录弹窗 */}
      <LogbookFormDialog
        open={editDialogOpen}
        onClose={() => setEditDialogOpen(false)}
        onSave={(entry) => {
          if (currentEntry) {
            setEntries(prev => prev.map(e =>
              e.id === currentEntry.id ? { ...entry, id: currentEntry.id } : e
            ))
          }
          setEditDialogOpen(false)
          setSnackbar({ open: true, message: '保存成功', severity: 'success' })
        }}
        initialData={currentEntry}
        title="编辑通联记录"
      />

      {/* 详情弹窗 */}
      <LogbookDetailDialog
        open={detailDialogOpen}
        onClose={() => setDetailDialogOpen(false)}
        entry={currentEntry}
        timeDisplayMode={timeDisplayMode}
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
    </Box>
  )
}

// 我方设备预设选项（预留）
const MY_RADIO_OPTIONS = [
  { id: 1, name: 'FT-991A', radio: 'FT-991A', antenna: 'GP', qth: '' },
  { id: 2, name: 'IC-9700', radio: 'IC-9700', antenna: '八木', qth: '' },
  { id: 3, name: 'TS-890', radio: 'TS-890', antenna: 'DP', qth: '' },
]

// 表单弹窗组件
interface LogbookFormDialogProps {
  open: boolean
  onClose: () => void
  onSave: (entry: Omit<LogbookEntry, 'id'>) => void
  initialData?: LogbookEntry | null
  title: string
}

function LogbookFormDialog({ open, onClose, onSave, initialData, title }: LogbookFormDialogProps) {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))

  // 获取当前时间
  const getCurrentTime = useCallback((mode: 'bjt' | 'utc') => {
    const now = new Date()
    if (mode === 'bjt') {
      // BJT = UTC + 8
      const bjtDate = new Date(now.getTime() + 8 * 60 * 60 * 1000)
      return bjtDate.toISOString().slice(0, 16).replace('T', ' ')
    }
    return now.toISOString().slice(0, 16).replace('T', ' ')
  }, [])

  const [formData, setFormData] = useState<Partial<LogbookEntry>>(() =>
    initialData || {
      time_utc: getCurrentTime('utc'),
      time_bjt: getCurrentTime('bjt'),
      tx_frequency: 0,
      rx_frequency: 0,
      same_frequency: true,
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

  // 重置表单 - 打开时默认使用当前时间
  const resetForm = useCallback(() => {
    if (initialData) {
      setFormData(initialData)
      setIsRepeater(!initialData.same_frequency)
    } else {
      setFormData({
        time_utc: getCurrentTime('utc'),
        time_bjt: getCurrentTime('bjt'),
        tx_frequency: 0,
        rx_frequency: 0,
        same_frequency: true,
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
      })
      setIsRepeater(false)
    }
  }, [initialData, getCurrentTime])

  // 打开弹窗时重置
  useState(() => {
    if (open) {
      resetForm()
    }
  })

  // 时间转换 BJT <-> UTC (差8小时)
  const convertTime = (time: string, from: 'bjt' | 'utc', to: 'bjt' | 'utc') => {
    if (!time) return ''
    try {
      const date = new Date(time.replace(' ', 'T'))
      const offset = from === 'bjt' ? -8 : 8
      date.setHours(date.getHours() + offset)
      return date.toISOString().slice(0, 16).replace('T', ' ')
    } catch {
      return time
    }
  }

  // 处理时间变化
  const handleTimeChange = (value: string, mode: 'bjt' | 'utc') => {
    setTimeMode(mode)
    if (mode === 'bjt') {
      setFormData(prev => ({
        ...prev,
        time_bjt: value,
        time_utc: convertTime(value, 'bjt', 'utc'),
      }))
    } else {
      setFormData(prev => ({
        ...prev,
        time_utc: value,
        time_bjt: convertTime(value, 'utc', 'bjt'),
      }))
    }
  }

  // 使用当前时间
  const useCurrentTime = () => {
    handleTimeChange(getCurrentTime(timeMode), timeMode)
  }

  // 处理同频/中继切换
  const handleFreqModeChange = (same: boolean) => {
    setFormData(prev => ({
      ...prev,
      same_frequency: same,
      rx_frequency: same ? prev.tx_frequency : prev.rx_frequency,
    }))
  }

  // 处理频率变化
  const handleTxFrequencyChange = (value: number) => {
    setFormData(prev => ({
      ...prev,
      tx_frequency: value,
      rx_frequency: prev.same_frequency ? value : prev.rx_frequency,
    }))
  }

  // 保存
  const handleSave = () => {
    // 验证必填字段
    if (!formData.callsign || !formData.tx_frequency || !formData.mode) {
      return
    }

    onSave({
      time_utc: formData.time_utc || getCurrentTime('utc'),
      time_bjt: formData.time_bjt || getCurrentTime('bjt'),
      tx_frequency: formData.tx_frequency || 0,
      rx_frequency: formData.rx_frequency || formData.tx_frequency || 0,
      same_frequency: formData.same_frequency ?? true,
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

  // 快速填充中继台
  const handleRepeaterSelect = (repeater: typeof REPEATER_OPTIONS[0] | null) => {
    if (repeater) {
      setFormData(prev => ({
        ...prev,
        tx_frequency: repeater.freq,
        rx_frequency: repeater.freq + repeater.offset * 0.001,
        same_frequency: false,
      }))
    }
  }

  // 快速填充我方设备
  const handleMyRadioSelect = (preset: typeof MY_RADIO_OPTIONS[0] | null) => {
    if (preset) {
      setFormData(prev => ({
        ...prev,
        my_radio: preset.radio,
        my_antenna: preset.antenna,
        my_qth: preset.qth || prev.my_qth,
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
                    value={(timeMode === 'bjt' ? formData.time_bjt : formData.time_utc)?.slice(0, 10) || ''}
                    onChange={(e) => {
                      const currentTime = timeMode === 'bjt' ? formData.time_bjt : formData.time_utc
                      const newTime = e.target.value + ' ' + (currentTime?.slice(11) || '00:00')
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
                    value={(timeMode === 'bjt' ? formData.time_bjt : formData.time_utc)?.slice(11, 16) || ''}
                    onChange={(e) => {
                      const currentTime = timeMode === 'bjt' ? formData.time_bjt : formData.time_utc
                      const newTime = (currentTime?.slice(0, 10) || new Date().toISOString().slice(0, 10)) + ' ' + e.target.value
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
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={formData.same_frequency}
                        onChange={(e) => handleFreqModeChange(e.target.checked)}
                        size="small"
                      />
                    }
                    label={
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                        {formData.same_frequency ? <LinkIcon fontSize="small" /> : <LinkOffIcon fontSize="small" />}
                        <Typography variant="caption">
                          {formData.same_frequency ? '同频' : '异频'}
                        </Typography>
                      </Box>
                    }
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={isRepeater}
                        onChange={(e) => {
                          setIsRepeater(e.target.checked)
                          if (e.target.checked) {
                            handleFreqModeChange(false)
                          }
                        }}
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
                  <Autocomplete
                    size="small"
                    options={REPEATER_OPTIONS}
                    getOptionLabel={(option) => `${option.name} (${option.freq} MHz)`}
                    onChange={(_, value) => handleRepeaterSelect(value)}
                    renderInput={(params) => (
                      <TextField
                        {...params}
                        label="快速选择中继台"
                        placeholder="搜索中继台..."
                      />
                    )}
                    renderOption={(props, option) => (
                      <li {...props} key={option.id}>
                        <Box>
                          <Typography variant="body2">{option.name}</Typography>
                          <Typography variant="caption" color="text.secondary">
                            {option.freq} MHz (差频 {option.offset} MHz)
                          </Typography>
                        </Box>
                      </li>
                    )}
                  />
                </Box>
              )}

              <Grid container spacing={{ xs: 1, sm: 2 }}>
                <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                  <TextField
                    fullWidth
                    label={formData.same_frequency ? "频率 (MHz)" : "发射频率 (MHz)"}
                    type="number"
                    size="small"
                    value={formData.tx_frequency || ''}
                    onChange={(e) => handleTxFrequencyChange(parseFloat(e.target.value) || 0)}
                    inputProps={{ step: 0.001 }}
                  />
                </Grid>
                {!formData.same_frequency && (
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
                        label="对方呼号 *"
                        size="small"
                        value={formData.callsign || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, callsign: e.target.value.toUpperCase() }))}
                        placeholder="例如: BH1ABC"
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
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1.5 }}>
                    <Typography variant="subtitle2" color="secondary.main" sx={{ fontWeight: 600 }}>
                      我方信息
                    </Typography>
                    <Autocomplete
                      size="small"
                      options={MY_RADIO_OPTIONS}
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
                  </Box>
                  <Grid container spacing={{ xs: 1, sm: 1.5 }}>
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
  const timeValue = timeDisplayMode === 'bjt' ? entry.time_bjt : entry.time_utc

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
                    {entry.same_frequency ? '同频' : `${entry.rx_frequency} MHz`}
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
