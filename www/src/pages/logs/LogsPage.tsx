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
  { value: 'create', label: '创建' },
  { value: 'update', label: '更新' },
  { value: 'delete', label: '删除' },
]

const EVENT_TYPE_COLORS: Record<string, any> = {
  login: 'info',
  logout: 'default',
  create: 'success',
  update: 'warning',
  delete: 'error',
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
