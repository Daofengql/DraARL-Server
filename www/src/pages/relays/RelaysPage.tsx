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
  IconButton,
  Typography,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Alert,
} from '@mui/material'
import { Add, Edit, Delete, Search, SettingsInputAntenna } from '@mui/icons-material'
import { relayService } from '../../services'
import type { Relay } from '../../types'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

export function RelaysPage() {
  const [relays, setRelays] = useState<Relay[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRelay, setEditingRelay] = useState<Relay | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    tx_frequency: 0,
    rx_frequency: 0,
    ctcss: 0,
    owner: '',
    location: '',
    description: '',
  })

  // 确认对话框状态
  const [confirmDialog, setConfirmDialog] = useState<{
    open: boolean
    title: string
    message: string
    type: 'danger' | 'warning' | 'info'
    onConfirm: () => void
  }>({ open: false, title: '', message: '', type: 'info', onConfirm: () => {} })

  useEffect(() => {
    loadRelays()
  }, [])

  const loadRelays = async () => {
    setLoading(true)
    try {
      const data = await relayService.list()
      setRelays(data)
    } catch (err) {
      console.error('Failed to load relays:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleOpenDialog = (relay?: Relay) => {
    if (relay) {
      setEditingRelay(relay)
      setFormData({
        name: relay.name,
        tx_frequency: relay.tx_frequency,
        rx_frequency: relay.rx_frequency,
        ctcss: relay.ctcss || 0,
        owner: relay.owner || '',
        location: relay.location || '',
        description: relay.description || '',
      })
    } else {
      setEditingRelay(null)
      setFormData({
        name: '',
        tx_frequency: 0,
        rx_frequency: 0,
        ctcss: 0,
        owner: '',
        location: '',
        description: '',
      })
    }
    setDialogOpen(true)
    setError('')
  }

  const handleCloseDialog = () => {
    setDialogOpen(false)
    setEditingRelay(null)
  }

  const handleSave = async () => {
    try {
      if (editingRelay) {
        await relayService.update({ id: editingRelay.id, ...formData })
      } else {
        await relayService.create(formData)
      }
      handleCloseDialog()
      loadRelays()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
    }
  }

  const handleDelete = async (id: number) => {
    const relay = relays.find(r => r.id === id)
    setConfirmDialog({
      open: true,
      title: '删除中继台',
      message: `确定要删除中继台 "${relay?.name || id}" 吗？`,
      type: 'danger',
      onConfirm: async () => {
        try {
          await relayService.delete(id)
          loadRelays()
        } catch (err: any) {
          setError(err.response?.data?.message || '删除失败')
        }
      },
    })
  }

  const filteredRelays = relays.filter(
    (r) =>
      !searchKeyword ||
      r.name.toLowerCase().includes(searchKeyword.toLowerCase()) ||
      (r.location && r.location.toLowerCase().includes(searchKeyword.toLowerCase()))
  )

  const paginatedRelays = filteredRelays.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  )

  const formatFrequency = (freq: number) => {
    return freq > 0 ? `${(freq / 1000000).toFixed(4)} MHz` : '-'
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4">中继台管理</Typography>
        <Button variant="contained" startIcon={<Add />} onClick={() => handleOpenDialog()}>
          添加中继台
        </Button>
      </Box>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>
          {error}
        </Alert>
      )}

      <Paper sx={{ mb: 2 }}>
        <Box sx={{ display: 'flex', gap: 2, p: 2 }}>
          <TextField
            placeholder="搜索中继台名称或位置"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            size="small"
            sx={{ flexGrow: 1 }}
          />
          <Button variant="outlined" startIcon={<Search />}>
            搜索
          </Button>
        </Box>
      </Paper>

      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>ID</TableCell>
              <TableCell>名称</TableCell>
              <TableCell>下行频率</TableCell>
              <TableCell>上行频率</TableCell>
              <TableCell>CTCSS</TableCell>
              <TableCell>位置</TableCell>
              <TableCell>所有者</TableCell>
              <TableCell>操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={8} align="center">
                  加载中...
                </TableCell>
              </TableRow>
            ) : paginatedRelays.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} align="center">
                  暂无数据
                </TableCell>
              </TableRow>
            ) : (
              paginatedRelays.map((relay) => (
                <TableRow key={relay.id} hover>
                  <TableCell>{relay.id}</TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <SettingsInputAntenna color="primary" fontSize="small" />
                      {relay.name}
                    </Box>
                  </TableCell>
                  <TableCell>{formatFrequency(relay.tx_frequency)}</TableCell>
                  <TableCell>{formatFrequency(relay.rx_frequency)}</TableCell>
                  <TableCell>{relay.ctcss ? `${relay.ctcss} Hz` : '-'}</TableCell>
                  <TableCell>{relay.location || '-'}</TableCell>
                  <TableCell>{relay.owner || '-'}</TableCell>
                  <TableCell>
                    <IconButton size="small" onClick={() => handleOpenDialog(relay)}>
                      <Edit fontSize="small" />
                    </IconButton>
                    <IconButton size="small" color="error" onClick={() => handleDelete(relay.id)}>
                      <Delete fontSize="small" />
                    </IconButton>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        <TablePagination
          component="div"
          count={filteredRelays.length}
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

      <Dialog open={dialogOpen} onClose={handleCloseDialog} maxWidth="sm" fullWidth>
        <DialogTitle>{editingRelay ? '编辑中继台' : '添加中继台'}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label="名称"
              fullWidth
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            />
            <TextField
              label="下行频率 (Hz)"
              type="number"
              fullWidth
              value={formData.tx_frequency}
              onChange={(e) => setFormData({ ...formData, tx_frequency: parseInt(e.target.value) || 0 })}
            />
            <TextField
              label="上行频率 (Hz)"
              type="number"
              fullWidth
              value={formData.rx_frequency}
              onChange={(e) => setFormData({ ...formData, rx_frequency: parseInt(e.target.value) || 0 })}
            />
            <TextField
              label="CTCSS (Hz)"
              type="number"
              fullWidth
              value={formData.ctcss}
              onChange={(e) => setFormData({ ...formData, ctcss: parseInt(e.target.value) || 0 })}
            />
            <TextField
              label="所有者"
              fullWidth
              value={formData.owner}
              onChange={(e) => setFormData({ ...formData, owner: e.target.value })}
            />
            <TextField
              label="位置"
              fullWidth
              value={formData.location}
              onChange={(e) => setFormData({ ...formData, location: e.target.value })}
            />
            <TextField
              label="描述"
              fullWidth
              multiline
              rows={2}
              value={formData.description}
              onChange={(e) => setFormData({ ...formData, description: e.target.value })}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog}>取消</Button>
          <Button onClick={handleSave} variant="contained">
            保存
          </Button>
        </DialogActions>
      </Dialog>

      {/* 确认对话框 */}
      <ConfirmDialog
        isOpen={confirmDialog.open}
        title={confirmDialog.title}
        message={confirmDialog.message}
        type={confirmDialog.type}
        onConfirm={() => {
          confirmDialog.onConfirm()
          setConfirmDialog(prev => ({ ...prev, open: false }))
        }}
        onCancel={() => setConfirmDialog(prev => ({ ...prev, open: false }))}
      />
    </Box>
  )
}
