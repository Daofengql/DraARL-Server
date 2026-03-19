import { useEffect, useState } from 'react'
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
  TextField,
  Button,
  Typography,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Chip,
} from '@mui/material'
import Search from '@mui/icons-material/Search'
import Description from '@mui/icons-material/Description'
import { logService } from '../../services'
import type { OperatorLog } from '../../types'

const EVENT_TYPES = [
  { value: '', label: '全部' },
  { value: 'login', label: '登录' },
  { value: 'logout', label: '登出' },
  { value: 'login_failed', label: '登录失败' },
  { value: 'register', label: '注册' },
  { value: 'user_create', label: '创建用户' },
  { value: 'user_update', label: '更新用户' },
  { value: 'user_delete', label: '删除用户' },
  { value: 'user_status', label: '用户状态变更' },
  { value: 'user_approval', label: '用户审批' },
  { value: 'password_reset', label: '重置密码' },
  { value: 'password_change', label: '修改密码' },
  { value: 'profile_update', label: '更新资料' },
  { value: 'group_create', label: '创建群组' },
  { value: 'group_update', label: '更新群组' },
  { value: 'group_delete', label: '删除群组' },
  { value: 'group_join', label: '加入群组' },
  { value: 'group_leave', label: '离开群组' },
  { value: 'group_device_status', label: '群组设备状态' },
  { value: 'device_kick', label: '踢出设备' },
  { value: 'virtual_group_create', label: '创建虚拟互联组' },
  { value: 'virtual_group_update', label: '更新虚拟互联组' },
  { value: 'virtual_group_delete', label: '删除虚拟互联组' },
  { value: 'group_link_add', label: '添加群组互联' },
  { value: 'group_link_remove', label: '移除群组互联' },
  { value: 'asset_create', label: '创建资源' },
  { value: 'asset_upload', label: '上传资源' },
  { value: 'asset_update', label: '更新资源' },
  { value: 'asset_delete', label: '删除资源' },
  { value: 'config_update', label: '配置更新' },
  { value: 'comm_settings_update', label: '通信配置更新' },
  { value: 'comm_record_delete', label: '删除通信记录' },
  { value: 'cache_clear_all', label: '清空缓存' },
  { value: 'cache_metrics_reset', label: '重置缓存指标' },
  { value: 'system', label: '系统' },
]

const EVENT_TYPE_COLORS: Record<string, any> = {
  login: 'info',
  logout: 'default',
  login_failed: 'error',
  register: 'success',
  user_create: 'success',
  user_update: 'warning',
  user_delete: 'error',
  user_status: 'secondary',
  user_approval: 'primary',
  password_reset: 'warning',
  password_change: 'warning',
  profile_update: 'info',
  group_create: 'success',
  group_update: 'warning',
  group_delete: 'error',
  group_join: 'info',
  group_leave: 'default',
  group_device_status: 'secondary',
  device_kick: 'warning',
  virtual_group_create: 'success',
  virtual_group_update: 'warning',
  virtual_group_delete: 'error',
  group_link_add: 'info',
  group_link_remove: 'warning',
  asset_create: 'success',
  asset_upload: 'success',
  asset_update: 'warning',
  asset_delete: 'error',
  config_update: 'secondary',
  comm_settings_update: 'secondary',
  comm_record_delete: 'warning',
  cache_clear_all: 'warning',
  cache_metrics_reset: 'info',
  system: 'primary',
}

export function LogsPage() {
  const [logs, setLogs] = useState<OperatorLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [eventType, setEventType] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    loadLogs()
  }, [page, rowsPerPage, eventType])

  const loadLogs = async () => {
    setLoading(true)
    try {
      const data = await logService.getList({
        page: page + 1,
        page_size: rowsPerPage,
        event_type: eventType || undefined,
      })
      const items = data.items || data
      setLogs(Array.isArray(items) ? items : [])
      setTotal(data.total || (Array.isArray(items) ? items.length : 0))
    } catch (err) {
      console.error('Failed to load logs:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleSearch = () => {
    setPage(0)
    loadLogs()
  }

  const formatTimestamp = (timestamp: string) => {
    return new Date(timestamp).toLocaleString('zh-CN')
  }

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        操作日志
      </Typography>

      <Paper sx={{ mb: 2 }}>
        <Box sx={{ display: 'flex', gap: 2, p: 2, flexWrap: 'wrap' }}>
          <TextField
            placeholder="搜索日志内容"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            onKeyPress={(e) => e.key === 'Enter' && handleSearch()}
            size="small"
            sx={{ flexGrow: 1, minWidth: 200 }}
          />
          <FormControl size="small" sx={{ minWidth: 120 }}>
            <InputLabel>事件类型</InputLabel>
            <Select
              value={eventType}
              label="事件类型"
              onChange={(e) => setEventType(e.target.value)}
            >
              {EVENT_TYPES.map((type) => (
                <MenuItem key={type.value} value={type.value}>
                  {type.label}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
          <Button variant="outlined" startIcon={<Search />} onClick={handleSearch}>
            搜索
          </Button>
        </Box>
      </Paper>

      <TableContainer component={Paper} sx={{ overflow: 'auto' }}>
        <Table sx={{ minWidth: 600 }}>
          <TableHead>
            <TableRow>
              <TableCell>ID</TableCell>
              <TableCell>时间</TableCell>
              <TableCell>操作者</TableCell>
              <TableCell>事件类型</TableCell>
              <TableCell>内容</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={5} align="center">
                  加载中...
                </TableCell>
              </TableRow>
            ) : logs.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} align="center">
                  暂无数据
                </TableCell>
              </TableRow>
            ) : (
              logs.map((log) => (
                <TableRow key={log.id} hover>
                  <TableCell>{log.id}</TableCell>
                  <TableCell>{formatTimestamp(log.timestamp)}</TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Description fontSize="small" color="action" />
                      {log.operator || '-'}
                    </Box>
                  </TableCell>
                  <TableCell>
                    {log.event_type && (
                      <Chip
                        label={log.event_type}
                        size="small"
                        color={EVENT_TYPE_COLORS[log.event_type] || 'default'}
                        variant="outlined"
                      />
                    )}
                  </TableCell>
                  <TableCell>{log.content}</TableCell>
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
          labelDisplayedRows={({ from, to, count }) => `${from}-${to} 共 ${count}`}
        />
      </TableContainer>
    </Box>
  )
}
