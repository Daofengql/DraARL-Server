import { useEffect, useState, useCallback } from 'react'
import { useLocation } from 'react-router-dom'
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
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Typography,
  Card,
  CardContent,
  IconButton,
  CircularProgress,
  Snackbar,
  Alert,
} from '@mui/material'
import { PlayArrow, Stop, Devices, Group, Download, Refresh } from '@mui/icons-material'
import { apiClient } from '../../services/api'
import { opusPlayer, getWavBlobFromOpusUrl } from '../../utils/opusDecoder'

interface CommRecord {
  id: number
  device_id: number
  device_name: string
  group_id?: number
  group_name?: string
  user_id?: number
  username?: string
  start_time: string
  end_time: string
  duration_ms: number
  audio_path?: string
  audio_url?: string
  audio_size?: number
  status: number
}

export function CommRecordsPage() {
  const location = useLocation()
  const [records, setRecords] = useState<CommRecord[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [loading, setLoading] = useState(false)
  const [filterUserId, setFilterUserId] = useState<number | null>(null)
  const [filterDeviceId, setFilterDeviceId] = useState<number | null>(null)
  const [filterGroupId, setFilterGroupId] = useState<number | null>(null)
  const [deviceList, setDeviceList] = useState<{ id: number; name: string }[]>([])
  const [userList, setUserList] = useState<{ id: number; username: string }[]>([])
  const [groupList, setGroupList] = useState<{ id: number; name: string }[]>([])
  const [playingId, setPlayingId] = useState<number | null>(null)
  const [loadingId, setLoadingId] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  // 判断是否在后台管理页面
  const isAdminPage = location.pathname.startsWith('/admin/')
  const showUserFilter = isAdminPage

  useEffect(() => {
    loadRecords()
    if (showUserFilter) {
      loadUsers()
    }
    loadDevices()
    loadGroups()
  }, [page, rowsPerPage, filterUserId, filterDeviceId, filterGroupId])

  // 组件卸载时停止播放
  useEffect(() => {
    return () => {
      opusPlayer.stop()
    }
  }, [])

  const loadRecords = async () => {
    setLoading(true)
    try {
      const params: Record<string, unknown> = {
        page: page + 1,
        page_size: rowsPerPage,
      }
      if (filterUserId) params.user_id = filterUserId
      if (filterDeviceId) params.device_id = filterDeviceId
      if (filterGroupId) params.group_id = filterGroupId

      const res = await apiClient.get<any>('/api/comm-records', { params })
      if (res.code === 200) {
        const items = res.data?.list || res.data?.items || res.data?.data || res.data || []
        setRecords(Array.isArray(items) ? items : [])
        setTotal(res.data?.total || res.data?.count || 0)
      }
    } catch (err) {
      console.error('Failed to load comm records:', err)
      setRecords([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }

  const loadUsers = async () => {
    try {
      const res = await apiClient.get<any>('/api/users')
      if (res.code === 200) {
        const users = res.data?.items || res.data?.data || res.data || []
        setUserList(Array.isArray(users) ? users : [])
      }
    } catch (err) {
      console.error('Failed to load users:', err)
      setUserList([])
    }
  }

  const loadDevices = async () => {
    try {
      const res = await apiClient.get<any>('/api/devices')
      if (res.code === 200) {
        const devices = res.data?.items || res.data?.data || res.data || []
        setDeviceList(Array.isArray(devices) ? devices : [])
      }
    } catch (err) {
      console.error('Failed to load devices:', err)
      setDeviceList([])
    }
  }

  const loadGroups = async () => {
    try {
      const res = await apiClient.get<any>('/api/groups')
      if (res.code === 200) {
        const groups = res.data?.items || res.data?.data || res.data || []
        setGroupList(Array.isArray(groups) ? groups : [])
      }
    } catch (err) {
      console.error('Failed to load groups:', err)
      setGroupList([])
    }
  }

  const formatDuration = (ms: number) => {
    if (ms < 1000) return `${ms}ms`
    const seconds = Math.floor(ms / 1000)
    const minutes = Math.floor(seconds / 60)
    const remainingSeconds = seconds % 60
    if (minutes > 0) {
      return `${minutes}分${remainingSeconds}秒`
    }
    return `${seconds}秒`
  }

  const formatTime = (timeStr: string) => {
    return new Date(timeStr).toLocaleString('zh-CN')
  }

  const formatFileSize = (bytes?: number) => {
    if (!bytes) return ''
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  }

  // 播放/停止音频（支持 Raw Opus 格式）
  const handlePlay = useCallback(async (record: CommRecord) => {
    // 如果正在播放同一个，则停止
    if (playingId === record.id) {
      opusPlayer.stop()
      setPlayingId(null)
      return
    }

    // 停止之前的播放
    opusPlayer.stop()
    setLoadingId(record.id)
    setError(null)

    try {
      if (!record.audio_url && !record.audio_path) {
        throw new Error('无音频数据')
      }

      // 构建 URL
      const audioUrl = record.audio_url || `/api/minio/${record.audio_path}`

      // 使用 Opus 播放器播放
      await opusPlayer.play(audioUrl, () => {
        setPlayingId(null)
      })

      setPlayingId(record.id)
    } catch (err) {
      console.error('播放失败:', err)
      setError(`播放失败: ${err instanceof Error ? err.message : '未知错误'}`)
      setPlayingId(null)
    } finally {
      setLoadingId(null)
    }
  }, [playingId])

  // 下载音频（转换为 WAV 格式）
  const handleDownload = useCallback(async (record: CommRecord) => {
    if (!record.audio_url && !record.audio_path) {
      setError('无音频数据')
      return
    }

    try {
      const audioUrl = record.audio_url || `/api/minio/${record.audio_path}`

      // 解码并转换为 WAV
      const wavBlob = await getWavBlobFromOpusUrl(audioUrl)

      // 创建下载链接
      const url = URL.createObjectURL(wavBlob)
      const a = document.createElement('a')
      a.href = url
      a.download = `comm_${record.device_name}_${new Date(record.start_time).toISOString().slice(0, 19).replace(/[:-]/g, '')}.wav`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    } catch (err) {
      console.error('下载失败:', err)
      setError(`下载失败: ${err instanceof Error ? err.message : '未知错误'}`)
    }
  }, [])

  return (
    <Box>
      <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'center' }, gap: 2, mb: 3 }}>
        <Typography variant="h4" sx={{ fontWeight: 600 }}>
          通信记录
        </Typography>
        <IconButton
          onClick={() => loadRecords()}
          disabled={loading}
          title="刷新"
          color="primary"
        >
          <Refresh />
        </IconButton>
      </Box>

      {/* 筛选栏 */}
      <Card sx={{ mb: 2 }}>
        <CardContent>
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', alignItems: 'center' }}>
            <FormControl size="small" sx={{ minWidth: { xs: 120, sm: 150 } }}>
              <InputLabel>设备筛选</InputLabel>
              <Select
                value={filterDeviceId || ''}
                label="设备筛选"
                onChange={(e) => {
                  setFilterDeviceId(e.target.value ? Number(e.target.value) : null)
                  setPage(0)
                }}
              >
                <MenuItem value="">全部设备</MenuItem>
                {deviceList.map((device) => (
                  <MenuItem key={device.id} value={device.id}>
                    {device.name}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>

            <FormControl size="small" sx={{ minWidth: { xs: 120, sm: 150 } }}>
              <InputLabel>群组筛选</InputLabel>
              <Select
                value={filterGroupId || ''}
                label="群组筛选"
                onChange={(e) => {
                  setFilterGroupId(e.target.value ? Number(e.target.value) : null)
                  setPage(0)
                }}
              >
                <MenuItem value="">全部群组</MenuItem>
                {groupList.map((group) => (
                  <MenuItem key={group.id} value={group.id}>
                    {group.name}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>

            {showUserFilter && (
              <FormControl size="small" sx={{ minWidth: { xs: 120, sm: 150 } }}>
                <InputLabel>用户筛选</InputLabel>
                <Select
                  value={filterUserId || ''}
                  label="用户筛选"
                  onChange={(e) => {
                    setFilterUserId(e.target.value ? Number(e.target.value) : null)
                    setPage(0)
                  }}
                >
                  <MenuItem value="">全部用户</MenuItem>
                  {userList.map((user) => (
                    <MenuItem key={user.id} value={user.id}>
                      {user.username}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
            )}
          </Box>
        </CardContent>
      </Card>

      {/* 通信记录表格 */}
      <TableContainer component={Paper} sx={{ overflow: 'auto' }}>
        <Table sx={{ minWidth: 700 }}>
          <TableHead>
            <TableRow>
              <TableCell>ID</TableCell>
              <TableCell>通信时间</TableCell>
              <TableCell>设备</TableCell>
              <TableCell>群组</TableCell>
              {showUserFilter && <TableCell>用户</TableCell>}
              <TableCell>通信时长</TableCell>
              <TableCell>音频</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={7} align="center">
                  加载中...
                </TableCell>
              </TableRow>
            ) : records.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} align="center">
                  暂无记录
                </TableCell>
              </TableRow>
            ) : (
              records.map((record) => (
                <TableRow key={record.id} hover>
                  <TableCell>{record.id}</TableCell>
                  <TableCell>{formatTime(record.start_time)}</TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Devices color="primary" fontSize="small" />
                      {record.device_name}
                    </Box>
                  </TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Group color="action" fontSize="small" />
                      {record.group_name || '-'}
                    </Box>
                  </TableCell>
                  {showUserFilter && <TableCell>{record.username || '-'}</TableCell>}
                  <TableCell>{formatDuration(record.duration_ms)}</TableCell>
                  <TableCell>
                    {(record.audio_url || record.audio_path) ? (
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                        <IconButton
                          size="small"
                          onClick={() => handlePlay(record)}
                          color={playingId === record.id ? 'error' : 'primary'}
                          disabled={loadingId !== null && loadingId !== record.id}
                        >
                          {loadingId === record.id ? (
                            <CircularProgress size={20} />
                          ) : playingId === record.id ? (
                            <Stop />
                          ) : (
                            <PlayArrow />
                          )}
                        </IconButton>
                        <IconButton
                          size="small"
                          onClick={() => handleDownload(record)}
                          color="default"
                          title="下载 WAV"
                        >
                          <Download />
                        </IconButton>
                        <Typography variant="caption" color="text.secondary">
                          {formatFileSize(record.audio_size)}
                        </Typography>
                      </Box>
                    ) : (
                      <Typography variant="caption" color="text.secondary">
                        无音频
                      </Typography>
                    )}
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
          labelDisplayedRows={({ from, to, count }) => `${from}-${to} 共 ${count}`}
        />
      </TableContainer>

      {/* 错误提示 */}
      <Snackbar
        open={!!error}
        autoHideDuration={4000}
        onClose={() => setError(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity="error" onClose={() => setError(null)}>
          {error}
        </Alert>
      </Snackbar>
    </Box>
  )
}
