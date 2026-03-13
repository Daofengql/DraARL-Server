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
  Stack,
  Tooltip,
} from '@mui/material'
import {
  Add,
  Edit,
  Delete,
  Search,
  Circle,
  LockOpen,
  Lock,
} from '@mui/icons-material'
import { deviceService, groupService } from '../../services'
import type { Device, Group } from '../../types'
import { SwitchGroupDialog } from './SwitchGroupDialog'

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

const GROUP_TYPE_PUBLIC = 1
const GROUP_TYPE_PRIVATE = 2

export function DevicesPage() {
  const [devices, setDevices] = useState<Device[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [switchDialogOpen, setSwitchDialogOpen] = useState(false)
  const [switchingDevice, setSwitchingDevice] = useState<Device | null>(null)
  const [editingDevice, setEditingDevice] = useState<Device | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    callsign: '',
    ssid: 0,
    model: 1,
    group_id: 0,
    disable_send: false,
    disable_recv: false,
  })

  useEffect(() => {
    loadDevices()
    loadGroups()
  }, [])

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

  // 对设备禁发/禁收状态进行直观的圆���渲染（绿灯正常，红灯禁用）
  const renderStatusDots = (device: Device) => (
    <Stack direction="row" spacing={1} alignItems="center">
      {/* 发送控制 */}
      <Tooltip title={device.disable_send ? '点击启用发送' : '点击禁用发送'}>
        <Button
          size="small"
          variant={device.disable_send ? 'outlined' : 'contained'}
          color={device.disable_send ? 'error' : 'success'}
          onClick={() => handleToggleSend(device)}
          sx={{ minWidth: 56, fontSize: '0.75rem' }}
        >
          发送
        </Button>
      </Tooltip>

      {/* 接收控制 */}
      <Tooltip title={device.disable_recv ? '点击启用接收' : '点击禁用接收'}>
        <Button
          size="small"
          variant={device.disable_recv ? 'outlined' : 'contained'}
          color={device.disable_recv ? 'error' : 'success'}
          onClick={() => handleToggleRecv(device)}
          sx={{ minWidth: 56, fontSize: '0.75rem' }}
        >
          接收
        </Button>
      </Tooltip>
    </Stack>
  )

  // 快捷切换禁发状态
  const handleToggleSend = async (device: Device) => {
    try {
      await deviceService.update(device.id, {
        ...device,
        disable_send: !device.disable_send,
      })
      loadDevices()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
    }
  }

  // 快捷切换禁收状态
  const handleToggleRecv = async (device: Device) => {
    try {
      await deviceService.update(device.id, {
        ...device,
        disable_recv: !device.disable_recv,
      })
      loadDevices()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
    }
  }

  const handleOpenSwitchDialog = (device: Device) => {
    setSwitchingDevice(device)
    setSwitchDialogOpen(true)
  }

  const handleSwitchGroup = async (groupId: number, password?: string) => {
    if (!switchingDevice) return
    try {
      await deviceService.switchGroup(switchingDevice.id, groupId, password)
      setSwitchDialogOpen(false)
      setSwitchingDevice(null)
      loadDevices()
    } catch (err: any) {
      setError(err.response?.data?.message || '切换群组失败')
    }
  }

  const handleOpenDialog = (device?: Device) => {
    if (device) {
      setEditingDevice(device)
      setFormData({
        name: device.name,
        callsign: '',
        ssid: 0,
        model: device.model,
        group_id: 0,
        disable_send: false,
        disable_recv: false,
      })
    } else {
      setEditingDevice(null)
      setFormData({
        name: '',
        callsign: '',
        ssid: 0,
        model: 1,
        group_id: 0,
        disable_send: false,
        disable_recv: false,
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
    if (!formData.name.trim()) {
      setError('请输入设备名称')
      return
    }
    try {
      if (editingDevice) {
        await deviceService.update(editingDevice.id, { name: formData.name, model: formData.model })
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

  // 获取群组信息
  const getGroupInfo = (groupId: number) => {
    return groups.find((g) => g.id === groupId)
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h5" fontWeight={600}>设备管理</Typography>
        <Button variant="contained" startIcon={<Add />} onClick={() => handleOpenDialog()}>
          添加设备
        </Button>
      </Box>

      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>{error}</Alert>}

      <Paper sx={{ mb: 2 }}>
        <Box sx={{ display: 'flex', gap: 2, p: 2 }}>
          <TextField
            placeholder="搜索设备名称或呼号"
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

      <TableContainer component={Paper} variant="outlined">
        <Table>
          <TableHead sx={{ bgcolor: 'grey.50' }}>
            <TableRow>
              <TableCell width={80}>在线状态</TableCell>
              <TableCell>名称</TableCell>
              <TableCell>呼号及SSID</TableCell>
              <TableCell>所在群组</TableCell>
              <TableCell width={130}>收发控制</TableCell>
              <TableCell align="right">操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow><TableCell colSpan={6} align="center">加载中...</TableCell></TableRow>
            ) : paginatedDevices.length === 0 ? (
              <TableRow><TableCell colSpan={6} align="center">暂无设备数据</TableCell></TableRow>
            ) : (
              paginatedDevices.map((device) => {
                const group = getGroupInfo(device.group_id)
                return (
                  <TableRow key={device.id} hover>
                    {/* 在线状态使用绿圆点或灰圆圈 */}
                    <TableCell>
                      {device.online || device.is_online ?
                        <Circle sx={{ fontSize: 16, color: 'success.main' }} /> :
                        <Circle sx={{ fontSize: 16, color: 'text.disabled' }} />
                      }
                    </TableCell>
                    <TableCell sx={{ fontWeight: 500 }}>{device.name}</TableCell>
                    <TableCell>{device.callsign}-{device.ssid}</TableCell>
                    <TableCell>
                      {group ? (
                        <Stack direction="row" alignItems="center" spacing={1}>
                          <Typography variant="body2">{group.name}</Typography>
                          {group.type === GROUP_TYPE_PRIVATE && (
                            <Tooltip title="私有群组">
                              <Lock fontSize="small" color="secondary" sx={{ fontSize: 16 }}/>
                            </Tooltip>
                          )}
                        </Stack>
                      ) : (
                        <Typography variant="body2" color="text.disabled">无群组或群组已解散</Typography>
                      )}
                    </TableCell>

                    {/* 按需求渲染状态圆点: 左绿发, 右绿收，可点击切换 */}
                    <TableCell>
                      {renderStatusDots(device)}
                    </TableCell>

                    <TableCell align="right">
                      <Button
                        size="small"
                        variant="outlined"
                        sx={{ mr: 1 }}
                        onClick={() => handleOpenSwitchDialog(device)}
                      >
                        切换群组
                      </Button>
                      <Tooltip title="编辑设备">
                        <IconButton size="small" onClick={() => handleOpenDialog(device)}>
                          <Edit fontSize="small" />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title="删除设备">
                        <IconButton size="small" color="error" onClick={() => handleDelete(device.id)}>
                          <Delete fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                )
              })
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

      {/* 编辑设备对话框 */}
      <Dialog open={dialogOpen} onClose={handleCloseDialog} maxWidth="sm" fullWidth>
        <DialogTitle>{editingDevice ? '编辑设备' : '添加设备'}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label="设备名称"
              fullWidth
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              autoFocus
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
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog}>取消</Button>
          <Button onClick={handleSave} variant="contained">
            保存
          </Button>
        </DialogActions>
      </Dialog>

      {/* 切换群组对话框 */}
      {switchingDevice && (
        <SwitchGroupDialog
          open={switchDialogOpen}
          onClose={() => {
            setSwitchDialogOpen(false)
            setSwitchingDevice(null)
          }}
          device={switchingDevice}
          groups={groups}
          onSwitch={handleSwitchGroup}
        />
      )}
    </Box>
  )
}
