import { useState } from 'react'
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
  Button,
  Typography,
  Chip,
  Alert,
  CircularProgress,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  IconButton,
} from '@mui/material'
import Search from '@mui/icons-material/Search'
import SettingsInputAntenna from '@mui/icons-material/SettingsInputAntenna'
import LocationOn from '@mui/icons-material/LocationOn'
import Visibility from '@mui/icons-material/Visibility'
import { PublicPageLayout } from '../../components/layout'
import { relayService } from '../../services'
import type { Relay } from '../../types'
import { RegionCascader } from '../../components/common/RegionCascader'

export function RelaySearchPage() {
  const [relays, setRelays] = useState<Relay[]>([])
  const [location, setLocation] = useState('')
  const [loading, setLoading] = useState(false)
  const [searched, setSearched] = useState(false)
  const [error, setError] = useState('')
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [noteDialog, setNoteDialog] = useState<{ open: boolean; title: string; note: string }>({
    open: false,
    title: '',
    note: '',
  })

  const handleSearch = async () => {
    const locationParts = location.split(' ').filter(Boolean)
    if (locationParts.length < 2) {
      setError('请至少选择到市级别进行查询')
      return
    }

    setLoading(true)
    setError('')
    setSearched(true)
    try {
      const data = await relayService.publicSearch(location)
      setRelays(data)
      setPage(0)
    } catch (err: any) {
      setError(err.response?.data?.message || '查询失败，请稍后重试')
      setRelays([])
    } finally {
      setLoading(false)
    }
  }

  const paginatedRelays = relays.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  )

  return (
    <PublicPageLayout maxWidth="lg">
      <Paper elevation={3} sx={{ p: 3 }}>
        {/* 标题 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 3 }}>
          <SettingsInputAntenna color="primary" sx={{ fontSize: 32 }} />
          <Typography variant="h4">中继台查询</Typography>
        </Box>

        {/* 搜索区域 */}
        <Paper variant="outlined" sx={{ p: 2, mb: 3 }}>
          <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, gap: 2, alignItems: { sm: 'flex-end' } }}>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <RegionCascader
                value={location}
                onChange={setLocation}
                label="选择地区"
                size="medium"
                fullWidth
                helperText="请选择省/市进行查询，至少需要选择到市级别"
              />
            </Box>
            <Button
              variant="contained"
              startIcon={loading ? <CircularProgress size={20} color="inherit" /> : <Search />}
              onClick={handleSearch}
              disabled={loading}
              sx={{ minWidth: 120, height: 56 }}
            >
              {loading ? '查询中...' : '查询'}
            </Button>
          </Box>
        </Paper>

        {/* 错误提示 */}
        {error && (
          <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>
            {error}
          </Alert>
        )}

        {/* 结果区域 */}
        {searched && !loading && (
          <>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
              <LocationOn color="action" fontSize="small" />
              <Typography variant="body2" color="text.secondary">
                查询地区：{location}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ ml: 2 }}>
                共找到 {relays.length} 个中继台
              </Typography>
            </Box>

            {relays.length > 0 ? (
              <TableContainer component={Paper} variant="outlined" sx={{ overflow: 'auto' }}>
                <Table sx={{ minWidth: 700 }}>
                  <TableHead>
                    <TableRow>
                      <TableCell>名称</TableCell>
                      <TableCell>接收频率</TableCell>
                      <TableCell>发射频率</TableCell>
                      <TableCell>接收亚音</TableCell>
                      <TableCell>发射亚音</TableCell>
                      <TableCell>位置</TableCell>
                      <TableCell>状态</TableCell>
                      <TableCell>备注</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {paginatedRelays.map((relay) => (
                      <TableRow key={relay.id} hover>
                        <TableCell>
                          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <SettingsInputAntenna color="primary" fontSize="small" />
                            {relay.name}
                          </Box>
                        </TableCell>
                        <TableCell>{relay.down_freq || '-'}</TableCell>
                        <TableCell>{relay.up_freq || '-'}</TableCell>
                        <TableCell>{relay.receive_ctcss || '-'}</TableCell>
                        <TableCell>{relay.send_ctcss || '-'}</TableCell>
                        <TableCell>{relay.location || '-'}</TableCell>
                        <TableCell>
                          <Chip
                            label={relay.status === 1 ? '在线' : '离线'}
                            color={relay.status === 1 ? 'success' : 'default'}
                            size="small"
                          />
                        </TableCell>
                        <TableCell>
                          {relay.note ? (
                            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                              <Typography
                                variant="body2"
                                sx={{
                                  maxWidth: 150,
                                  overflow: 'hidden',
                                  textOverflow: 'ellipsis',
                                  whiteSpace: 'nowrap',
                                }}
                              >
                                {relay.note}
                              </Typography>
                              <IconButton
                                size="small"
                                onClick={() => setNoteDialog({ open: true, title: relay.name, note: relay.note || '' })}
                                sx={{ p: 0.5 }}
                              >
                                <Visibility fontSize="small" />
                              </IconButton>
                            </Box>
                          ) : '-'}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <TablePagination
                  component="div"
                  count={relays.length}
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
            ) : (
              <Paper variant="outlined" sx={{ p: 4, textAlign: 'center' }}>
                <Typography color="text.secondary">
                  该地区暂无中继台数据
                </Typography>
              </Paper>
            )}
          </>
        )}

        {/* 初始提示 */}
        {!searched && !loading && (
          <Paper variant="outlined" sx={{ p: 4, textAlign: 'center' }}>
            <LocationOn sx={{ fontSize: 48, color: 'text.disabled', mb: 2 }} />
            <Typography color="text.secondary">
              请选择地区（至少到市级别）查询中继台信息
            </Typography>
          </Paper>
        )}
      </Paper>

      {/* 备注详情弹窗 */}
      <Dialog
        open={noteDialog.open}
        onClose={() => setNoteDialog({ open: false, title: '', note: '' })}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{noteDialog.title} - 备注详情</DialogTitle>
        <DialogContent>
          <Typography variant="body1" sx={{ whiteSpace: 'pre-wrap' }}>
            {noteDialog.note}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setNoteDialog({ open: false, title: '', note: '' })}>
            关闭
          </Button>
        </DialogActions>
      </Dialog>
    </PublicPageLayout>
  )
}
