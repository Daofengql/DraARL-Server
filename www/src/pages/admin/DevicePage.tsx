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
} from '@mui/material'
import {
  Add,
  Edit,
  Delete,
  Search,
  CheckCircle,
  Cancel,
  Refresh,
} from '@mui/icons-material'
import { deviceService } from '../../services/device'

interface Device {
  id: number
  name: string
  callsign: string
  ssid: number
  dev_model: number
  group_id: number
  is_online: boolean
  status: number
  priority?: number
  qth?: string
  online_time?: string
  create_time?: string
  update_time?: string
}

interface Group {
  id: number
  name: string
  description?: string
}

export function AdminDevicePage() {
  const [devices, setDevices] = useState<Device[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [total, setTotal] = useState(0)
  const [searchKeyword, setSearchKeyword] = useState('')

  // 对话框状态
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [editingDevice, setEditingDevice] = useState<Device | null>(null)
  const [deletingDevice, setDeletingDevice] = useState<Device | null>(null)

  // 表单状态
  const [formData, setFormData] = useState({
    name: '',
    callsign: '',
    ssid: 0,
    dev_model: 1,
    group_id: 0,
    priority: 0,
    qth: '',
  })

  const fetchDevices = async () => {
    setLoading(true)
    try {
      const result = await deviceService.getList({
        page: page + 1,
        page_size: rowsPerPage,
        keyword: searchKeyword || undefined,
      })
      setDevices(result.items)
      setTotal(result.total)
    } catch (err) {
      setError('获取设备列表失败')
    } finally {
      setLoading(false)
    }
  }

  const fetchGroups = async () => {
    try {
      // 这里需要添加获取群组列表的服务
      // const result = await groupService.list()
      // setGroups(result)
      // 临时数据
      setGroups([
        { id: 1, name: '默认群组', description: '系统默认群组' },
      ])
    } catch (err) {
      console.error('获取群组列表失败', err)
    }
  }

  useEffect(() => {
    fetchDevices()
    fetchGroups()
  }, [page, rowsPerPage])

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      if (page === 0) {
        fetchDevices()
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
    setEditingDevice(null)
    setFormData({
      name: '',
      callsign: '',
      ssid: 0,
      dev_model: 1,
      group_id: 0,
      priority: 0,
      qth: '',
    })
    setDialogOpen(true)
  }

  const handleOpenEdit = (device: Device) => {
    setEditingDevice(device)
    setFormData({
      name: device.name,
      callsign: device.callsign,
      ssid: device.ssid,
      dev_model: device.dev_model,
      group_id: device.group_id,
      priority: device.priority || 0,
      qth: device.qth || '',
    })
    setDialogOpen(true)
  }

  const handleOpenDelete = (device: Device) => {
    setDeletingDevice(device)
    setDeleteDialogOpen(true)
  }

  const handleSave = async () => {
    try {
      if (editingDevice) {
        await deviceService.update(editingDevice.id, formData)
      } else {
        await deviceService.create(formData)
      }
      setDialogOpen(false)
      fetchDevices()
    } catch (err) {
      setError(editingDevice ? '更新设备失败' : '创建设备失败')
    }
  }

  const handleDelete = async () => {
    if (!deletingDevice) return
    try {
      await deviceService.delete(deletingDevice.id)
      setDeleteDialogOpen(false)
      fetchDevices()
    } catch (err) {
      setError('删除设备失败')
    }
  }

  const getStatusLabel = (status: number) => {
    switch (status) {
      case 1:
        return <Chip label="正常" color="success" size="small" />
      case 0:
        return <Chip label="禁用" color="default" size="small" />
      case -1:
        return <Chip label="故障" color="error" size="small" />
      default:
        return <Chip label="未知" color="default" size="small" />
    }
  }

  const getModelLabel = (model: number) => {
    switch (model) {
      case 1:
        return 'APRS'
      case 2:
        return 'NRL1'
      case 3:
        return 'NRL2'
      default:
        return `型号${model}`
    }
  }

  return (
    <Stack spacing={3}>
      {/* 页面标题 */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography variant="h5" fontWeight={600}>
          设备管理
        </Typography>
        <Box sx={{ display: 'flex', gap: 2 }}>
          <Button
            startIcon={<Refresh />}
            onClick={fetchDevices}
            variant="outlined"
          >
            刷新
          </Button>
          <Button
            startIcon={<Add />}
            onClick={handleOpenAdd}
            variant="contained"
          >
            添加设备
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
            placeholder="搜索设备名称、呼号..."
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

      {/* 设备列表 */}
      <Card>
        <TableContainer>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>ID</TableCell>
                <TableCell>设备名称</TableCell>
                <TableCell>呼号</TableCell>
                <TableCell>SSID</TableCell>
                <TableCell>型号</TableCell>
                <TableCell>群组</TableCell>
                <TableCell>状态</TableCell>
                <TableCell>在线</TableCell>
                <TableCell>优先级</TableCell>
                <TableCell align="right">操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={10} align="center">
                    <Typography color="text.secondary">加载中...</Typography>
                  </TableCell>
                </TableRow>
              ) : devices.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={10} align="center">
                    <Typography color="text.secondary">暂无设备</Typography>
                  </TableCell>
                </TableRow>
              ) : (
                devices.map((device) => (
                  <TableRow key={device.id} hover>
                    <TableCell>{device.id}</TableCell>
                    <TableCell>{device.name}</TableCell>
                    <TableCell>{device.callsign}</TableCell>
                    <TableCell>{device.ssid}</TableCell>
                    <TableCell>{getModelLabel(device.dev_model)}</TableCell>
                    <TableCell>{device.group_id}</TableCell>
                    <TableCell>{getStatusLabel(device.status)}</TableCell>
                    <TableCell>
                      {device.is_online ? (
                        <Chip
                          label="在线"
                          color="success"
                          size="small"
                          icon={<CheckCircle />}
                        />
                      ) : (
                        <Chip
                          label="离线"
                          color="default"
                          size="small"
                          icon={<Cancel />}
                        />
                      )}
                    </TableCell>
                    <TableCell>{device.priority ?? '-'}</TableCell>
                    <TableCell align="right">
                      <Tooltip title="编辑">
                        <IconButton
                          size="small"
                          onClick={() => handleOpenEdit(device)}
                        >
                          <Edit fontSize="small" />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title="删除">
                        <IconButton
                          size="small"
                          color="error"
                          onClick={() => handleOpenDelete(device)}
                        >
                          <Delete fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))
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
        <DialogTitle>{editingDevice ? '编辑设备' : '添加设备'}</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField
              label="设备名称"
              fullWidth
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            />
            <TextField
              label="呼号"
              fullWidth
              value={formData.callsign}
              onChange={(e) => setFormData({ ...formData, callsign: e.target.value })}
            />
            <TextField
              label="SSID"
              type="number"
              fullWidth
              value={formData.ssid}
              onChange={(e) => setFormData({ ...formData, ssid: parseInt(e.target.value) || 0 })}
            />
            <FormControl fullWidth>
              <InputLabel>设备型号</InputLabel>
              <Select
                value={formData.dev_model}
                label="设备型号"
                onChange={(e) => setFormData({ ...formData, dev_model: e.target.value as number })}
              >
                <MenuItem value={1}>APRS</MenuItem>
                <MenuItem value={2}>NRL1</MenuItem>
                <MenuItem value={3}>NRL2</MenuItem>
              </Select>
            </FormControl>
            <FormControl fullWidth>
              <InputLabel>所属群组</InputLabel>
              <Select
                value={formData.group_id}
                label="所属群组"
                onChange={(e) => setFormData({ ...formData, group_id: e.target.value as number })}
              >
                <MenuItem value={0}>无群组</MenuItem>
                {groups.map((g) => (
                  <MenuItem key={g.id} value={g.id}>
                    {g.name}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <TextField
              label="优先级"
              type="number"
              fullWidth
              value={formData.priority}
              onChange={(e) => setFormData({ ...formData, priority: parseInt(e.target.value) || 0 })}
            />
            <TextField
              label="位置 (QTH)"
              fullWidth
              value={formData.qth}
              onChange={(e) => setFormData({ ...formData, qth: e.target.value })}
              placeholder="例: PM00abcd"
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
            确定要删除设备 <strong>{deletingDevice?.name}</strong> 吗？此操作不可撤销。
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
