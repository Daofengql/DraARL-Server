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
  Snackbar,
} from '@mui/material'
import {
  Edit,
  Delete,
  Search,
  Circle,
  Lock,
  Key,
  ContentCopy,
} from '@mui/icons-material'
import { deviceService, groupService, authService } from '../../services'
import type { Device, Group } from '../../types'
import { SwitchGroupDialog } from './SwitchGroupDialog'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

const DEVICE_MODELS = [
  { value: 0, label: '未知设备' },
  { value: 100, label: '微信小程序' },
  { value: 101, label: 'Android 客户端' },
  { value: 102, label: 'iOS 客户端' },
  { value: 103, label: 'Windows 客户端' },
  { value: 105, label: '浏览器客户端' },
  { value: 106, label: '互联设备' },
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

  // 确认对话框状态
  const [confirmDialog, setConfirmDialog] = useState<{
    open: boolean
    title: string
    message: string
    type: 'danger' | 'warning' | 'info'
    onConfirm: () => void
  }>({ open: false, title: '', message: '', type: 'info', onConfirm: () => {} })

  // 设备密码相关状态
  const [regeneratedPassword, setRegeneratedPassword] = useState<string | null>(null)
  const [showDevicePassword, setShowDevicePassword] = useState(false)
  const [generatingDevicePassword, setGeneratingDevicePassword] = useState(false)
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({
    open: false,
    message: '',
    severity: 'success',
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

  // 生成/重新生成设备密码
  const handleRegenerateDevicePassword = async () => {
    setGeneratingDevicePassword(true)
    try {
      const result = await authService.regenerateDevicePassword()
      setRegeneratedPassword(result.device_password)
      setShowDevicePassword(true)
      setSnackbar({ open: true, message: '设备密码已生成', severity: 'success' })
    } catch (err: any) {
      setSnackbar({ open: true, message: err.response?.data?.message || '生成失败', severity: 'error' })
    } finally {
      setGeneratingDevicePassword(false)
    }
  }

  // 复制设备密码
  const handleCopyDevicePassword = () => {
    if (regeneratedPassword) {
      navigator.clipboard.writeText(regeneratedPassword)
      setSnackbar({ open: true, message: '已复制到剪贴板', severity: 'success' })
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
    if (!editingDevice) return
    try {
      await deviceService.update(editingDevice.id, { name: formData.name, model: formData.model })
      handleCloseDialog()
      loadDevices()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
    }
  }

  const handleDelete = async (id: number) => {
    const device = devices.find(d => d.id === id)
    setConfirmDialog({
      open: true,
      title: '删除设备',
      message: `确定要删除设备 "${device?.name || id}" 吗？`,
      type: 'danger',
      onConfirm: async () => {
        try {
          await deviceService.delete(id)
          loadDevices()
        } catch (err: any) {
          setError(err.response?.data?.message || '删除失败')
        }
      },
    })
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
        <Button
          variant="contained"
          size="small"
          startIcon={<Key />}
          onClick={handleRegenerateDevicePassword}
          disabled={generatingDevicePassword}
          color="primary"
        >
          {generatingDevicePassword ? '生成中...' : '重新生成设备密码'}
        </Button>
      </Box>

      {showDevicePassword && regeneratedPassword && (
        <Alert severity="warning" sx={{ mb: 2 }} onClose={() => setShowDevicePassword(false)}>
          <Typography variant="body2">
            新设备密码: <strong style={{ fontSize: '1.1em', letterSpacing: '0.5px' }}>{regeneratedPassword}</strong>
          </Typography>
          <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
            请立即保存，此密码仅显示一次！
          </Typography>
          <Button
            size="small"
            startIcon={<ContentCopy />}
            onClick={handleCopyDevicePassword}
            sx={{ mt: 1 }}
          >
            复制密码
          </Button>
        </Alert>
      )}

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
              <TableCell>设备类型</TableCell>
              <TableCell>呼号及SSID</TableCell>
              <TableCell>所在群组</TableCell>
              <TableCell width={130}>收发控制</TableCell>
              <TableCell align="right">操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow><TableCell colSpan={7} align="center">加载中...</TableCell></TableRow>
            ) : paginatedDevices.length === 0 ? (
              <TableRow><TableCell colSpan={7} align="center">暂无设备数据</TableCell></TableRow>
            ) : (
              paginatedDevices.map((device) => {
                const group = getGroupInfo(device.group_id)
                return (
                  <TableRow key={device.id} hover>
                    {/* 在线���态使用绿圆点或灰圆圈 */}
                    <TableCell>
                      {device.online || device.is_online ?
                        <Circle sx={{ fontSize: 16, color: 'success.main' }} /> :
                        <Circle sx={{ fontSize: 16, color: 'text.disabled' }} />
                      }
                    </TableCell>
                    <TableCell sx={{ fontWeight: 500 }}>{device.name}</TableCell>
                    <TableCell>
                      <Typography variant="body2" color="text.secondary">
                        {DEVICE_MODELS.find(m => m.value === (device.model ?? device.dev_model))?.label || '未知设备'}
                      </Typography>
                    </TableCell>
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
        <DialogTitle>编辑设备</DialogTitle>
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

      {/* 提示消息 */}
      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar({ ...snackbar, open: false })}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} onClose={() => setSnackbar({ ...snackbar, open: false })}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </Box>
  )
}
