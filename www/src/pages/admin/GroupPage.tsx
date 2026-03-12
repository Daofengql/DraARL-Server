import { useState, useEffect } from 'react'
import {
  Box,
  Card,
  CardContent,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Chip,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  Stack,
  Alert,
  Tooltip,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  TablePagination,
  InputAdornment,
  Accordion,
  AccordionSummary,
  AccordionDetails,
} from '@mui/material'
import {
  Add,
  Edit,
  Delete,
  Search,
  Refresh,
  ExpandMore,
  Devices,
} from '@mui/icons-material'
import { groupService } from '../../services/group'
import { deviceService } from '../../services/device'

interface Group {
  id: number
  name: string
  type: number
  callsign: string
  password: string
  allow_callsign_ssid: string
  ower_id: number
  ower_callsign: string
  devlist: string
  master_server: number
  slave_server: number
  status: number
  note: string
  create_time: string
  update_time: string
}

interface Device {
  id: number
  name: string
  callsign: string
  ssid: number
  is_online: boolean
}

export function AdminGroupPage() {
  const [groups, setGroups] = useState<Group[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [total, setTotal] = useState(0)
  const [searchKeyword, setSearchKeyword] = useState('')

  // 群组设备缓存
  const [groupDevices, setGroupDevices] = useState<Record<number, Device[]>>({})
  const [loadingDevices, setLoadingDevices] = useState<Record<number, boolean>>({})

  // 对话框状态
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<Group | null>(null)
  const [deletingGroup, setDeletingGroup] = useState<Group | null>(null)

  // 表单状态
  const [formData, setFormData] = useState({
    name: '',
    type: 1,
    callsign: '',
    password: '',
    allow_callsign_ssid: '',
    note: '',
    status: 1,
  })

  const fetchGroups = async () => {
    setLoading(true)
    try {
      const result = await groupService.getList({
        page: page + 1,
        page_size: rowsPerPage,
        keyword: searchKeyword || undefined,
      })
      setGroups(result.items)
      setTotal(result.total)
    } catch (err) {
      setError('获取群组列表失败')
    } finally {
      setLoading(false)
    }
  }

  const fetchGroupDevices = async (groupId: number) => {
    if (groupDevices[groupId]) return // 已缓存
    setLoadingDevices((prev) => ({ ...prev, [groupId]: true }))
    try {
      const devices = await groupService.getDevices(groupId)
      setGroupDevices((prev) => ({ ...prev, [groupId]: devices }))
    } catch (err) {
      console.error('获取群组设备失败', err)
    } finally {
      setLoadingDevices((prev) => ({ ...prev, [groupId]: false }))
    }
  }

  useEffect(() => {
    fetchGroups()
  }, [page, rowsPerPage])

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      if (page === 0) {
        fetchGroups()
      } else {
        setPage(0)
      }
    }, 500)
    return () => clearTimeout(timeoutId)
  }, [searchKeyword])

  const handleSearch = (value: string) => {
    setSearchKeyword(value)
  }

  const handleOpenAdd = () => {
    setEditingGroup(null)
    setFormData({
      name: '',
      type: 1,
      callsign: '',
      password: '',
      allow_callsign_ssid: '',
      note: '',
      status: 1,
    })
    setDialogOpen(true)
  }

  const handleOpenEdit = (group: Group) => {
    setEditingGroup(group)
    setFormData({
      name: group.name,
      type: group.type,
      callsign: group.callsign,
      password: group.password,
      allow_callsign_ssid: group.allow_callsign_ssid,
      note: group.note || '',
      status: group.status,
    })
    setDialogOpen(true)
  }

  const handleOpenDelete = (group: Group) => {
    setDeletingGroup(group)
    setDeleteDialogOpen(true)
  }

  const handleSave = async () => {
    try {
      if (editingGroup) {
        await groupService.update(editingGroup.id, formData)
      } else {
        await groupService.create(formData)
      }
      setDialogOpen(false)
      fetchGroups()
    } catch (err) {
      setError(editingGroup ? '更新群组失败' : '创建群组失败')
    }
  }

  const handleDelete = async () => {
    if (!deletingGroup) return
    try {
      await groupService.delete(deletingGroup.id)
      setDeleteDialogOpen(false)
      // 清除缓存
      setGroupDevices((prev) => {
        const newCache = { ...prev }
        delete newCache[deletingGroup.id]
        return newCache
      })
      fetchGroups()
    } catch (err) {
      setError('删除群组失败')
    }
  }

  const getTypeLabel = (type: number) => {
    switch (type) {
      case 1:
        return <Chip label="公网" color="primary" size="small" />
      case 2:
        return <Chip label="专网" color="secondary" size="small" />
      case 3:
        return <Chip label="本地" color="info" size="small" />
      default:
        return <Chip label={`类型${type}`} color="default" size="small" />
    }
  }

  const getStatusLabel = (status: number) => {
    switch (status) {
      case 1:
        return <Chip label="启用" color="success" size="small" />
      case 0:
        return <Chip label="禁用" color="default" size="small" />
      default:
        return <Chip label="未知" color="default" size="small" />
    }
  }

  return (
    <Stack spacing={3}>
      {/* 页面标题 */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography variant="h5" fontWeight={600}>
          群组管理
        </Typography>
        <Box sx={{ display: 'flex', gap: 2 }}>
          <Button
            startIcon={<Refresh />}
            onClick={fetchGroups}
            variant="outlined"
          >
            刷新
          </Button>
          <Button
            startIcon={<Add />}
            onClick={handleOpenAdd}
            variant="contained"
          >
            添加群组
          </Button>
        </Box>
      </Box>

      {error && (
        <Alert severity="error" onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 搜索栏 */}
      <Card>
        <CardContent>
          <TextField
            fullWidth
            placeholder="搜索群组名称、呼号..."
            value={searchKeyword}
            onChange={(e) => handleSearch(e.target.value)}
            InputProps={{
              startAdornment: (
                <InputAdornment position="start">
                  <Search color="action" />
                </InputAdornment>
              ),
            }}
          />
        </CardContent>
      </Card>

      {/* 群组列表 */}
      <Card>
        <TableContainer>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>ID</TableCell>
                <TableCell>群组名称</TableCell>
                <TableCell>呼号</TableCell>
                <TableCell>类型</TableCell>
                <TableCell>拥有者</TableCell>
                <TableCell>设备数量</TableCell>
                <TableCell>状态</TableCell>
                <TableCell>备注</TableCell>
                <TableCell align="right">操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={9} align="center">
                    <Typography color="text.secondary">加载中...</Typography>
                  </TableCell>
                </TableRow>
              ) : groups.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={9} align="center">
                    <Typography color="text.secondary">暂无群组</Typography>
                  </TableCell>
                </TableRow>
              ) : (
                groups.map((group) => {
                  const devices = groupDevices[group.id] || []
                  const devCount = group.devlist ? group.devlist.split(',').filter(Boolean).length : 0
                  return (
                    <>
                      <TableRow key={group.id} hover>
                        <TableCell>{group.id}</TableCell>
                        <TableCell>{group.name}</TableCell>
                        <TableCell>{group.callsign || '-'}</TableCell>
                        <TableCell>{getTypeLabel(group.type)}</TableCell>
                        <TableCell>{group.ower_callsign || '-'}</TableCell>
                        <TableCell>
                          <Stack direction="row" alignItems="center" spacing={1}>
                            <Typography>{devCount}</Typography>
                            {devCount > 0 && (
                              <IconButton
                                size="small"
                                onClick={() => fetchGroupDevices(group.id)}
                              >
                                <Devices fontSize="small" />
                              </IconButton>
                            )}
                          </Stack>
                        </TableCell>
                        <TableCell>{getStatusLabel(group.status)}</TableCell>
                        <TableCell>
                          <Typography
                            sx={{
                              maxWidth: 150,
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                              whiteSpace: 'nowrap',
                            }}
                          >
                            {group.note || '-'}
                          </Typography>
                        </TableCell>
                        <TableCell align="right">
                          <Tooltip title="编辑">
                            <IconButton
                              size="small"
                              onClick={() => handleOpenEdit(group)}
                            >
                              <Edit fontSize="small" />
                            </IconButton>
                          </Tooltip>
                          <Tooltip title="删除">
                            <IconButton
                              size="small"
                              color="error"
                              onClick={() => handleOpenDelete(group)}
                            >
                              <Delete fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        </TableCell>
                      </TableRow>

                      {/* 设备详情展开行 */}
                      {devices.length > 0 && (
                        <TableRow>
                          <TableCell colSpan={9} sx={{ pb: 0, pt: 0 }}>
                            <Accordion
                              variant="outlined"
                              sx={{ boxShadow: 'none', '&:before': { display: 'none' } }}
                            >
                              <AccordionSummary expandIcon={<ExpandMore />}>
                                <Typography variant="body2" color="text.secondary">
                                  设备列表 ({devices.length})
                                </Typography>
                              </AccordionSummary>
                              <AccordionDetails>
                                <Stack spacing={1}>
                                  {devices.map((device) => (
                                    <Stack
                                      key={device.id}
                                      direction="row"
                                      alignItems="center"
                                      spacing={2}
                                      sx={{
                                        p: 1,
                                        bgcolor: 'grey.50',
                                        borderRadius: 1,
                                      }}
                                    >
                                      <Typography variant="body2" fontWeight={500}>
                                        {device.name}
                                      </Typography>
                                      <Typography variant="body2" color="text.secondary">
                                        {device.callsign}-{device.ssid}
                                      </Typography>
                                      <Chip
                                        label={device.is_online ? '在线' : '离线'}
                                        size="small"
                                        color={device.is_online ? 'success' : 'default'}
                                      />
                                    </Stack>
                                  ))}
                                </Stack>
                              </AccordionDetails>
                            </Accordion>
                          </TableCell>
                        </TableRow>
                      )}
                    </>
                  )
                })
              )}
            </TableBody>
          </Table>
        </TableContainer>
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
          labelDisplayedRows={({ from, to, count }) =>
            `${from}-${to} 共 ${count !== -1 ? count : `超过 ${to}`} 条`
          }
        />
      </Card>

      {/* 添加/编辑对话框 */}
      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{editingGroup ? '编辑群组' : '添加群组'}</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
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
                <MenuItem value={1}>公网</MenuItem>
                <MenuItem value={2}>专网</MenuItem>
                <MenuItem value={3}>本地</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label="呼号"
              fullWidth
              value={formData.callsign}
              onChange={(e) => setFormData({ ...formData, callsign: e.target.value })}
            />
            <TextField
              label="密码"
              fullWidth
              type="password"
              value={formData.password}
              onChange={(e) => setFormData({ ...formData, password: e.target.value })}
            />
            <TextField
              label="允许呼号SSID"
              fullWidth
              value={formData.allow_callsign_ssid}
              onChange={(e) => setFormData({ ...formData, allow_callsign_ssid: e.target.value })}
              placeholder="例: BG5XXX-0,BG5XXX-1"
            />
            <FormControl fullWidth>
              <InputLabel>状态</InputLabel>
              <Select
                value={formData.status}
                label="状态"
                onChange={(e) => setFormData({ ...formData, status: e.target.value as number })}
              >
                <MenuItem value={1}>启用</MenuItem>
                <MenuItem value={0}>禁用</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label="备注"
              fullWidth
              multiline
              rows={2}
              value={formData.note}
              onChange={(e) => setFormData({ ...formData, note: e.target.value })}
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialogOpen(false)}>取消</Button>
          <Button onClick={handleSave} variant="contained">
            保存
          </Button>
        </DialogActions>
      </Dialog>

      {/* 删除确认对话框 */}
      <Dialog open={deleteDialogOpen} onClose={() => setDeleteDialogOpen(false)}>
        <DialogTitle>确认删除</DialogTitle>
        <DialogContent>
          <Typography>
            确定要删除群组 <strong>{deletingGroup?.name}</strong> 吗？此操作不可撤销。
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)}>取消</Button>
          <Button onClick={handleDelete} color="error" variant="contained">
            删除
          </Button>
        </DialogActions>
      </Dialog>
    </Stack>
  )
}
