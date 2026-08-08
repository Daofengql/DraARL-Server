import { useCallback, useEffect, useState } from 'react'
import {
  Box, Button, Card, CardContent, Chip, FormControl, InputLabel, MenuItem, Paper, Select,
  Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography,
} from '@mui/material'
import Search from '@mui/icons-material/Search'
import type { OperatorLog } from '../../types'
import { TabPanel } from '../../components/common/TabPanel'
import { getOperatorLogs } from './api'
import { EVENT_TYPE_COLORS, EVENT_TYPES, formatOperatorTimestamp, getEventTypeLabel } from './eventTypes'

export function OperatorLogsTab({ value }: { value: number }) {
  const [logs, setLogs] = useState<OperatorLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [eventType, setEventType] = useState('')
  const [loading, setLoading] = useState(false)

  const loadLogs = useCallback(async () => {
    setLoading(true)
    try {
      const result = await getOperatorLogs({ page, pageSize: rowsPerPage, eventType })
      setLogs(result.items)
      setTotal(result.total)
    } catch (error) {
      console.error('Failed to load logs:', error)
    } finally {
      setLoading(false)
    }
  }, [eventType, page, rowsPerPage])

  useEffect(() => {
    if (value === 7) void loadLogs()
  }, [loadLogs, value])

  return (
    <TabPanel value={value} index={7}>
      <Box sx={{ px: 2 }}>
        <Card><CardContent>
          <Typography variant="h6" gutterBottom>操作日志</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>查看系统操作日志记录</Typography>
          <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, gap: 2, mb: 2 }}>
            <TextField
              placeholder="搜索日志内容" value={searchKeyword}
              onChange={(event) => setSearchKeyword(event.target.value)}
              onKeyPress={(event) => event.key === 'Enter' && void loadLogs()}
              size="small" fullWidth sx={{ maxWidth: { sm: 300 } }}
            />
            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>事件类型</InputLabel>
              <Select
                value={eventType} label="事件类型"
                onChange={(event) => {
                  setEventType(event.target.value)
                  setPage(0)
                }}
              >
                {EVENT_TYPES.map((type) => <MenuItem key={type.value} value={type.value}>{type.label}</MenuItem>)}
              </Select>
            </FormControl>
            <Button variant="outlined" size="small" startIcon={<Search />} onClick={() => void loadLogs()}>搜索</Button>
          </Box>

          <TableContainer component={Paper} variant="outlined" sx={{ overflow: 'auto' }}>
            <Table sx={{ minWidth: 500 }}>
              <TableHead><TableRow>
                <TableCell>ID</TableCell><TableCell>时间</TableCell><TableCell>操作者</TableCell>
                <TableCell>事件类型</TableCell><TableCell>内容</TableCell>
              </TableRow></TableHead>
              <TableBody>
                {loading ? (
                  <TableRow><TableCell colSpan={5} align="center">加载中...</TableCell></TableRow>
                ) : logs.length === 0 ? (
                  <TableRow><TableCell colSpan={5} align="center">暂无数据</TableCell></TableRow>
                ) : logs.map((log) => (
                  <TableRow key={log.id} hover>
                    <TableCell>{log.id}</TableCell>
                    <TableCell>{formatOperatorTimestamp(log.timestamp)}</TableCell>
                    <TableCell>{log.operator || '-'}</TableCell>
                    <TableCell>
                      {log.event_type && (
                        <Chip
                          label={getEventTypeLabel(log.event_type)} size="small"
                          color={EVENT_TYPE_COLORS[log.event_type] || 'default'} variant="outlined"
                        />
                      )}
                    </TableCell>
                    <TableCell>{log.content}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <TablePagination
              component="div" count={total} page={page} onPageChange={(_, newPage) => setPage(newPage)}
              rowsPerPage={rowsPerPage}
              onRowsPerPageChange={(event) => {
                setRowsPerPage(parseInt(event.target.value, 10))
                setPage(0)
              }}
              labelRowsPerPage="每页行数"
              labelDisplayedRows={({ from, to, count }) => `${from}-${to} 共 ${count}`}
            />
          </TableContainer>
        </CardContent></Card>
      </Box>
    </TabPanel>
  )
}
