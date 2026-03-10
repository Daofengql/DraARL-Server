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
import { Add, Edit, Delete, Search, Group as GroupIcon } from '@mui/icons-material'
import { groupService } from '../../services'
import type { Group } from '../../types'

const GROUP_TYPES = [
  { value: 0, label: '普通群组' },
  { value: 1, label: '中继互联' },
  { value: 2, label: '会议模式' },
]

export function GroupsPage() {
  const [groups, setGroups] = useState<Group[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<Group | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    type: 0,
    callsign: '',
    password: '',
  })

  useEffect(() => {
    loadGroups()
  }, [])

  const loadGroups = async () => {
    setLoading(true)
    try {
      const data = await groupService.list()
      setGroups(data)
    } catch (err) {
      console.error('Failed to load groups:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleOpenDialog = (group?: Group) => {
    if (group) {
      setEditingGroup(group)
      setFormData({
        name: group.name,
        type: group.type,
        callsign: group.callsign || '',
        password: group.password || '',
      })
    } else {
      setEditingGroup(null)
      setFormData({
        name: '',
        type: 0,
        callsign: '',
        password: '',
      })
    }
    setDialogOpen(true)
    setError('')
  }

  const handleCloseDialog = () => {
    setDialogOpen(false)
    setEditingGroup(null)
  }

  const handleSave = async () => {
    try {
      if (editingGroup) {
        await groupService.update(editingGroup.id, formData)
      } else {
        await groupService.create(formData)
      }
      handleCloseDialog()
      loadGroups()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
    }
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定要删除这个群组吗？')) return
    try {
      await groupService.delete(id)
      loadGroups()
    } catch (err: any) {
      setError(err.response?.data?.message || '删除失败')
    }
  }

  const filteredGroups = groups.filter(
    (g) =>
      !searchKeyword ||
      g.name.toLowerCase().includes(searchKeyword.toLowerCase()) ||
      (g.callsign && g.callsign.toLowerCase().includes(searchKeyword.toLowerCase()))
  )

  const paginatedGroups = filteredGroups.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  )

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4">群组管理</Typography>
        <Button variant="contained" startIcon={<Add />} onClick={() => handleOpenDialog()}>
          添加群组
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
            placeholder="搜索群组名称或呼号"
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
              <TableCell>呼号</TableCell>
              <TableCell>设备数</TableCell>
              <TableCell>操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={6} align="center">
                  加载中...
                </TableCell>
              </TableRow>
            ) : paginatedGroups.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} align="center">
                  暂无数据
                </TableCell>
              </TableRow>
            ) : (
              paginatedGroups.map((group) => (
                <TableRow key={group.id} hover>
                  <TableCell>{group.id}</TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <GroupIcon color="primary" fontSize="small" />
                      {group.name}
                    </Box>
                  </TableCell>
                  <TableCell>
                    <Chip
                      label={GROUP_TYPES.find((t) => t.value === group.type)?.label || '未知'}
                      size="small"
                      variant="outlined"
                    />
                  </TableCell>
                  <TableCell>{group.callsign || '-'}</TableCell>
                  <TableCell>{group.devices?.length || 0}</TableCell>
                  <TableCell>
                    <IconButton size="small" onClick={() => handleOpenDialog(group)}>
                      <Edit fontSize="small" />
                    </IconButton>
                    <IconButton size="small" color="error" onClick={() => handleDelete(group.id)}>
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
          count={filteredGroups.length}
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
        <DialogTitle>{editingGroup ? '编辑群组' : '添加群组'}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label="群组名称"
              fullWidth
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            />
            <FormControl fullWidth>
              <InputLabel>群组类型</InputLabel>
              <Select
                value={formData.type}
                label="群组类型"
                onChange={(e) => setFormData({ ...formData, type: e.target.value as number })}
              >
                {GROUP_TYPES.map((type) => (
                  <MenuItem key={type.value} value={type.value}>
                    {type.label}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <TextField
              label="呼号（可选）"
              fullWidth
              value={formData.callsign}
              onChange={(e) => setFormData({ ...formData, callsign: e.target.value })}
            />
            <TextField
              label="群组密码（可选）"
              fullWidth
              value={formData.password}
              onChange={(e) => setFormData({ ...formData, password: e.target.value })}
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
