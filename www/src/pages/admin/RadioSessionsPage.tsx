import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Box,
  Chip,
  FormControl,
  IconButton,
  InputLabel,
  MenuItem,
  Paper,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TablePagination,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material'
import LinkOff from '@mui/icons-material/LinkOff'
import { AutoRefresh } from '../../components/common/AutoRefresh'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { PageHeader } from '../../components/common/PageHeader'
import { SearchBar } from '../../components/common/SearchBar'
import { toast } from '../../components/common/Toast'
import { radioSessionService } from '../../services'
import type { AdminRadioSession, GhostTransport } from '../../services'
import { getDevModelName } from '../../utils/deviceModel'

type TransportFilter = 'all' | GhostTransport

const transportNames: Record<GhostTransport, string> = {
  udp: 'UDP',
  websocket: 'WebSocket',
  edge: '边缘节点',
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function shortSessionID(value: string) {
  return value.length > 8 ? value.slice(0, 8) : value
}

function getErrorMessage(error: unknown, fallback: string) {
  const responseMessage = (error as { response?: { data?: { message?: string } } })?.response?.data?.message
  return responseMessage || (error instanceof Error ? error.message : '') || fallback
}

export function RadioSessionsPage() {
  const [sessions, setSessions] = useState<AdminRadioSession[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [keyword, setKeyword] = useState('')
	const [transportFilter, setTransportFilter] = useState<TransportFilter>('all')
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [autoRefresh, setAutoRefresh] = useState(10)
  const [disconnecting, setDisconnecting] = useState<AdminRadioSession | null>(null)
  const [disconnectBusy, setDisconnectBusy] = useState(false)

  const loadSessions = useCallback(async () => {
    setLoading(true)
    try {
      setSessions(await radioSessionService.listAdmin())
      setError('')
    } catch (err) {
      setError(getErrorMessage(err, '获取幽灵会话失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadSessions()
  }, [loadSessions])

  const filteredSessions = useMemo(() => {
		const normalized = keyword.trim().toLowerCase()
		return sessions.filter((session) => {
			if (transportFilter !== 'all' && session.transport !== transportFilter) return false
      if (!normalized) return true
      return [
        session.username,
        session.callsign,
        session.client_instance_hint,
        shortSessionID(session.session_id),
        String(session.owner_id),
      ].some((value) => value.toLowerCase().includes(normalized))
    })
	}, [keyword, sessions, transportFilter])

  const visibleSessions = useMemo(
    () => filteredSessions.slice(page * rowsPerPage, page * rowsPerPage + rowsPerPage),
    [filteredSessions, page, rowsPerPage],
  )

  useEffect(() => {
    if (page > 0 && page * rowsPerPage >= filteredSessions.length) {
      setPage(Math.max(0, Math.ceil(filteredSessions.length / rowsPerPage) - 1))
    }
  }, [filteredSessions.length, page, rowsPerPage])

  const confirmDisconnect = async () => {
    if (!disconnecting) return
    setDisconnectBusy(true)
    try {
      await radioSessionService.disconnectAdmin(disconnecting.session_id)
      toast.success('幽灵会话已断开')
      setDisconnecting(null)
      await loadSessions()
    } catch (err) {
      setError(getErrorMessage(err, '断开幽灵会话失败'))
    } finally {
      setDisconnectBusy(false)
    }
  }

	return (
    <Box>
      <PageHeader
        title="幽灵会话"
        actions={
          <AutoRefresh
            value={autoRefresh}
            onChange={setAutoRefresh}
            onRefresh={loadSessions}
            loading={loading}
          />
        }
      />

      {error && <Alert severity="error" onClose={() => setError('')} sx={{ mb: 2 }}>{error}</Alert>}

		<Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ mb: 2 }}>
			<Typography variant="body2">在线 {sessions.length}</Typography>
		</Stack>

      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Stack
          direction={{ xs: 'column', md: 'row' }}
          spacing={1.5}
          sx={{ p: 2, borderBottom: '1px solid', borderColor: 'divider' }}
        >
          <SearchBar
            value={keyword}
            onChange={(value) => { setKeyword(value); setPage(0) }}
            onSearch={() => setPage(0)}
            placeholder="账号、呼号或会话 ID"
            sx={{ flex: 1, minWidth: { md: 280 } }}
          />
			<FormControl size="small" sx={{ minWidth: 140 }}>
            <InputLabel>连接方式</InputLabel>
            <Select
              value={transportFilter}
              label="连接方式"
              onChange={(event) => { setTransportFilter(event.target.value as TransportFilter); setPage(0) }}
            >
              <MenuItem value="all">全部</MenuItem>
              <MenuItem value="udp">UDP</MenuItem>
              <MenuItem value="websocket">WebSocket</MenuItem>
              <MenuItem value="edge">边缘节点</MenuItem>
            </Select>
          </FormControl>
        </Stack>

        <TableContainer sx={{ overflowX: 'auto' }}>
          <Table size="small" sx={{ minWidth: 1060 }}>
            <TableHead>
              <TableRow>
                <TableCell>账号</TableCell>
                <TableCell>客户端</TableCell>
                <TableCell>连接</TableCell>
                <TableCell>路由</TableCell>
                <TableCell>活动时间</TableCell>
                <TableCell>状态</TableCell>
                <TableCell align="right">操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {visibleSessions.map((session) => (
                <TableRow key={session.session_id} hover>
                  <TableCell>
                    <Typography variant="body2" fontWeight={600}>{session.callsign || session.username}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {session.username} · #{session.owner_id}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2">{getDevModelName(session.dev_model)}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {session.client_instance_hint} · {shortSessionID(session.session_id)}
                    </Typography>
                  </TableCell>
					<TableCell>
						<Chip size="small" label={transportNames[session.transport] || session.transport} variant="outlined" />
					</TableCell>
                  <TableCell>
                    <Stack spacing={0.75}>
                      <Chip size="small" color="primary" variant="outlined" label={`发送 ${session.tx_group_id}`} sx={{ width: 'fit-content' }} />
                      <Stack direction="row" spacing={0.5} useFlexGap flexWrap="wrap">
                        {session.rx_group_ids.map((groupID) => (
                          <Chip key={groupID} size="small" label={`收听 ${groupID}`} />
                        ))}
                      </Stack>
                    </Stack>
                  </TableCell>
                  <TableCell>
                    <Typography variant="caption" display="block">上线 {formatTime(session.online_since)}</Typography>
                    <Typography variant="caption" color="text.secondary">活动 {formatTime(session.last_activity)}</Typography>
                  </TableCell>
                  <TableCell>
                    <Stack direction="row" spacing={0.5}>
                      <Chip size="small" label={session.disable_send ? '禁发' : '可发'} color={session.disable_send ? 'error' : 'default'} />
                      <Chip size="small" label={session.disable_recv ? '禁收' : '可收'} color={session.disable_recv ? 'error' : 'default'} />
                    </Stack>
                  </TableCell>
                  <TableCell align="right">
                    <Tooltip title="断开会话">
                      <span>
                        <IconButton
                          color="error"
                          size="small"
                          disabled={disconnectBusy}
                          onClick={() => setDisconnecting(session)}
                        >
                          <LinkOff fontSize="small" />
                        </IconButton>
                      </span>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ))}
              {!loading && visibleSessions.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} align="center" sx={{ py: 5, color: 'text.secondary' }}>
                    没有匹配的在线会话
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
        <TablePagination
          component="div"
          count={filteredSessions.length}
          page={page}
          onPageChange={(_, nextPage) => setPage(nextPage)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(event) => { setRowsPerPage(Number(event.target.value)); setPage(0) }}
          rowsPerPageOptions={[10, 25, 50]}
          labelRowsPerPage="每页"
        />
      </Paper>

      <ConfirmDialog
        isOpen={disconnecting !== null}
        title="断开幽灵会话"
        message={disconnecting ? `确认断开 ${disconnecting.callsign || disconnecting.username} 的会话 ${shortSessionID(disconnecting.session_id)}？` : ''}
        confirmText={disconnectBusy ? '处理中' : '断开'}
        onConfirm={() => { if (!disconnectBusy) void confirmDisconnect() }}
        onCancel={() => { if (!disconnectBusy) setDisconnecting(null) }}
        type="danger"
      />
    </Box>
  )
}
