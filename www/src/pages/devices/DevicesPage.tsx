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
  Chip,
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
} from '@mui/material'
import {
  Add,
  Edit,
  Delete,
  Search,
  Radio,
  RadioButtonUnchecked,
} from '@mui/icons-material'
import { deviceService, groupService } from '../../services'
import type { Device, Group } from '../../types'

const DEVICE_MODELS = [
  { value: 0, label: '微信小程序' },
  { value: 1, label: 'Android' },
  { value: 2, label: 'iOS' },
  { value: 3, label: 'Windows' },
  { value: 4, label: '浏览器' },
  { value: 5, label: '服务器' },
  { value: 6, label: 'BM网关' },
  { value: 7, label: 'DMR网关' },
  { value: 8, label: 'YSF网关' },
  { value: 9, label: 'P25网关' },
  { value: 10, label: 'NXDN网关' },
  { value: 11, label: 'MMDVM' },
]

export function DevicesPage() {
  const [devices, setDevices] = useState<Device[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingDevice, setEditingDevice] = useState<Device | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    callsign: '',
    ssid: 0,
    model: 1,
    group_id: 0,
  })

  useEffect(() => {
    loadDevices()
    loadGroups()
  }, [page, rowsPerPage])

  const loadDevices = async () => {
    setLoading(true)
    try {
      const data = await deviceService.list()
      setDevices(data)
    } catch (err) {
      console.error('Failed to load devices:', err)
    } finally {
      setLoading(false)
    }
  }

  const loadGroups = async () => {
    try {
      const data = await groupService.list()
      setGroups(data)
    } catch (err) {
      console.error('Failed to load groups:', err)
    }
  }

  const handleSearch = async () => {
    setLoading(true)
    try {
      const data = await deviceService.list()
      const filtered = data.filter(
        (d) =>
          d.name.toLowerCase().includes(searchKeyword.toLowerCase()) ||
          d.callsign.toLowerCase().includes(searchKeyword.toLowerCase())
      )
      setDevices(filtered)
    } catch (err) {
      console.error('Search failed:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleOpenDialog = (device?: Device) => {
    if (device) {
      setEditingDevice(device)
      setFormData({
        name: device.name,
        callsign: device.callsign,
        ssid: device.ssid,
        model: device.model,
        group_id: device.group_id,
      })
    } else {
      setEditingDevice(null)
      setFormData({
        name: '',
        callsign: '',
        ssid: 0,
        model: 1,
        group_id: 0,
      })
    }
    setDialogOpen(true)
    setError('')
  }

  const handleCloseDialog = () => {
    setDialogOpen(false)
    setEditingDevice(null)
  }

  const handleSave = async () => {
    try {
      if (editingDevice) {
        await deviceService.update(editingDevice.id, formData)
      } else {
        await deviceService.create(formData)
      }
      handleCloseDialog()
      loadDevices()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
    }
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定要删除这个设备吗？')) return
    try {
      await deviceService.delete(id)
      loadDevices()
    } catch (err: any) {
      setError(err.response?.data?.message || '删除失败')
    }
  }

  const filteredDevices = devices.filter(
    (d) =>
      !searchKeyword ||
      d.name.toLowerCase().includes(searchKeyword.toLowerCase()) ||
      d.callsign.toLowerCase().includes(searchKeyword.toLowerCase())
  )

  const paginatedDevices = filteredDevices.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  )

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4">设备管理</Typography>
        <Button variant="contained" startIcon={<Add />} onClick={() => handleOpenDialog()}>
          添加设备
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
            placeholder="搜索设备名称或呼号"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            onKeyPress={(e) => e.key === 'Enter' && handleSearch()}
            size="small"
            sx={{ flexGrow: 1 }}
          />
          <Button variant="outlined" startIcon={<Search />} onClick={handleSearch}>
            搜索
          </Button>
        </Box>
      </Paper>

      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>状态</TableCell>
              <TableCell>名称</TableCell>
              <TableCell>呼号</TableCell>
              <TableCell>SSID</TableCell>
              <TableCell>型号</TableCell>
              <TableCell>群组</TableCell>
              <TableCell>操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={7} align="center">
                  加载中...
                </TableCell>
              </TableRow>
            ) : paginatedDevices.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} align="center">
                  暂无数据
                </TableCell>
              </TableRow>
            ) : (
              paginatedDevices.map((device) => (
                <TableRow key={device.id} hover>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      {device.online ? (
                        <Radio color="success" fontSize="small" />
                      ) : (
                        <RadioButtonUnchecked color="disabled" fontSize="small" />
                      )}
                      <Chip
                        label={device.online ? '在线' : '离线'}
                        size="small"
                        color={device.online ? 'success' : 'default'}
                      />
                    </Box>
                  </TableCell>
                  <TableCell>{device.name}</TableCell>
                  <TableCell>{device.callsign}</TableCell>
                  <TableCell>{device.ssid}</TableCell>
                  <TableCell>
                    {DEVICE_MODELS.find((m) => m.value === device.model)?.label || '未知'}
                  </TableCell>
                  <TableCell>{groups.find((g) => g.id === device.group_id)?.name || '-'}</TableCell>
                  <TableCell>
                    <IconButton size="small" onClick={() => handleOpenDialog(device)}>
                      <Edit fontSize="small" />
                    </IconButton>
                    <IconButton size="small" color="error" onClick={() => handleDelete(device.id)}>
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
          count={filteredDevices.length}
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
        <DialogTitle>{editingDevice ? '编辑设备' : '添加设备'}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
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
                value={formData.model}
                label="设备型号"
                onChange={(e) => setFormData({ ...formData, model: e.target.value as number })}
              >
                {DEVICE_MODELS.map((model) => (
                  <MenuItem key={model.value} value={model.value}>
                    {model.label}
                  </MenuItem>
                ))}
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
                {groups.map((group) => (
                  <MenuItem key={group.id} value={group.id}>
                    {group.name}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
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
