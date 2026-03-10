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
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Alert,
  Chip,
} from '@mui/material'
import { Add, Edit, Delete, Search, Dns } from '@mui/icons-material'
import { serverService } from '../../services'
import type { Server } from '../../types'

const SERVER_TYPES = [
  { value: 0, label: '主服务器' },
  { value: 1, label: '从服务器' },
  { value: 2, label: '中继服务器' },
]

const SERVER_STATUS = [
  { value: 0, label: '离线', color: 'error' as const },
  { value: 1, label: '在线', color: 'success' as const },
  { value: 2, label: '维护中', color: 'warning' as const },
]

export function ServersPage() {
  const [servers, setServers] = useState<Server[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingServer, setEditingServer] = useState<Server | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    type: 0,
    ip: '',
    port: 8080,
    location: '',
    description: '',
  })

  useEffect(() => {
    loadServers()
  }, [])

  const loadServers = async () => {
    setLoading(true)
    try {
      const data = await serverService.list()
      setServers(data)
    } catch (err) {
      console.error('Failed to load servers:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleOpenDialog = (server?: Server) => {
    if (server) {
      setEditingServer(server)
      setFormData({
        name: server.name,
        type: server.type,
        ip: server.ip,
        port: server.port,
        location: server.location || '',
        description: server.description || '',
      })
    } else {
      setEditingServer(null)
      setFormData({
        name: '',
        type: 0,
        ip: '',
        port: 8080,
        location: '',
        description: '',
      })
    }
    setDialogOpen(true)
    setError('')
  }

  const handleCloseDialog = () => {
    setDialogOpen(false)
    setEditingServer(null)
  }

  const handleSave = async () => {
    try {
      if (editingServer) {
        await serverService.update({ id: editingServer.id, ...formData })
      } else {
        await serverService.create(formData)
      }
      handleCloseDialog()
      loadServers()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
    }
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定要删除这个服务器吗？')) return
    try {
      await serverService.delete(id)
      loadServers()
    } catch (err: any) {
      setError(err.response?.data?.message || '删除失败')
    }
  }

  const filteredServers = servers.filter(
    (s) =>
      !searchKeyword ||
      s.name.toLowerCase().includes(searchKeyword.toLowerCase()) ||
      s.ip.includes(searchKeyword)
  )

  const paginatedServers = filteredServers.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  )

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4">服务器管理</Typography>
        <Button variant="contained" startIcon={<Add />} onClick={() => handleOpenDialog()}>
          添加服务器
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
            placeholder="搜索服务器名称或IP"
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
              <TableCell>类型</TableCell>
              <TableCell>IP地址</TableCell>
              <TableCell>端口</TableCell>
              <TableCell>位置</TableCell>
              <TableCell>状态</TableCell>
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
            ) : paginatedServers.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} align="center">
                  暂无数据
                </TableCell>
              </TableRow>
            ) : (
              paginatedServers.map((server) => (
                <TableRow key={server.id} hover>
                  <TableCell>{server.id}</TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Dns color="primary" fontSize="small" />
                      {server.name}
                    </Box>
                  </TableCell>
                  <TableCell>
                    {SERVER_TYPES.find((t) => t.value === server.type)?.label || '未知'}
                  </TableCell>
                  <TableCell>{server.ip}</TableCell>
                  <TableCell>{server.port}</TableCell>
                  <TableCell>{server.location || '-'}</TableCell>
                  <TableCell>
                    {server.status !== undefined && (
                      <Chip
                        label={SERVER_STATUS.find((s) => s.value === server.status)?.label || '未知'}
                        size="small"
                        color={SERVER_STATUS.find((s) => s.value === server.status)?.color || 'default'}
                      />
                    )}
                  </TableCell>
                  <TableCell>
                    <IconButton size="small" onClick={() => handleOpenDialog(server)}>
                      <Edit fontSize="small" />
                    </IconButton>
                    <IconButton size="small" color="error" onClick={() => handleDelete(server.id)}>
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
          count={filteredServers.length}
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
        <DialogTitle>{editingServer ? '编辑服务器' : '添加服务器'}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label="名称"
              fullWidth
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            />
            <FormControl fullWidth>
              <InputLabel>类型</InputLabel>
              <Select
                value={formData.type}
                label="类型"
                onChange={(e) => setFormData({ ...formData, type: e.target.value as number })}
              >
                {SERVER_TYPES.map((type) => (
                  <MenuItem key={type.value} value={type.value}>
                    {type.label}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <TextField
              label="IP地址"
              fullWidth
              value={formData.ip}
              onChange={(e) => setFormData({ ...formData, ip: e.target.value })}
            />
            <TextField
              label="端口"
              type="number"
              fullWidth
              value={formData.port}
              onChange={(e) => setFormData({ ...formData, port: parseInt(e.target.value) || 8080 })}
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
    </Box>
  )
}
