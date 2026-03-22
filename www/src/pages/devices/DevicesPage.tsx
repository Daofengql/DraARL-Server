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
  Button,
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
  IconButton,
  Snackbar,
  TextField,
} from '@mui/material'
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import Lock from '@mui/icons-material/Lock'
import Key from '@mui/icons-material/Key'
import ContentCopy from '@mui/icons-material/ContentCopy'
import Settings from '@mui/icons-material/Settings'
import { deviceService, groupService, authService } from '../../services'
import type { Device, Group } from '../../types'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { ParamConfigDialog } from '../../components/devices/ParamConfigDialog'
import { GroupPickerDialog } from '../../components/groups/GroupPicker'
import { PageHeader } from '../../components/common/PageHeader'
import { SearchBar } from '../../components/common/SearchBar'
import { AutoRefresh } from '../../components/common/AutoRefresh'
import { OnlineIndicator } from '../../components/common/OnlineIndicator'
import { SendRecvControl } from '../../components/common/SendRecvControl'
import { DEVICE_MODELS, getDevModelName } from '../../utils/deviceModel'

const GROUP_TYPE_PRIVATE = 2

export function DevicesPage() {
  const [devices, setDevices] = useState<Device[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 对话框状态
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingDevice, setEditingDevice] = useState<Device | null>(null)
  const [switchDialogOpen, setSwitchDialogOpen] = useState(false)
  const [switchingDevice, setSwitchingDevice] = useState<Device | null>(null)

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

  // 自动刷新状态
  const [autoRefresh, setAutoRefresh] = useState(0)

  // 参数下发对话框状态
  const [paramDialogOpen, setParamDialogOpen] = useState(false)
  const [paramDevice, setParamDevice] = useState<Device | null>(null)

  const [formData, setFormData] = useState({
    name: '',
    model: 1,
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
        model: device.model ?? device.dev_model ?? 1,
      })
    } else {
      setEditingDevice(null)
      setFormData({
        name: '',
        model: 1,
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
    const device = devices.find((d) => d.id === id)
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

  const paginatedDevices = filteredDevices.slice(page * rowsPerPage, page * rowsPerPage + rowsPerPage)

  // 获取群组信息
  const getGroupInfo = (groupId: number) => {
    return groups.find((g) => g.id === groupId)
  }

  return (
    <Box>
      <PageHeader
        title="设备管理"
        actions={
          <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
            <AutoRefresh
              value={autoRefresh}
              onChange={setAutoRefresh}
              onRefresh={loadDevices}
              loading={loading}
            />
            <Button
              variant="contained"
              size="small"
              startIcon={<Key />}
              onClick={handleRegenerateDevicePassword}
              disabled={generatingDevicePassword}
              color="primary"
            >
              {generatingDevicePassword ? '生成中...' : '设备密码'}
            </Button>
          </Stack>
        }
      />

      {showDevicePassword && regeneratedPassword && (
        <Alert severity="warning" sx={{ mb: 2 }} onClose={() => setShowDevicePassword(false)}>
          <Typography variant="body2">
            新设备密码:{' '}
            <strong style={{ fontSize: '1.1em', letterSpacing: '0.5px' }}>{regeneratedPassword}</strong>
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

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>
          {error}
        </Alert>
      )}

      <Paper sx={{ mb: 2, p: 2 }}>
        <SearchBar
          value={searchKeyword}
          onChange={setSearchKeyword}
          onSearch={() => setPage(0)}
          placeholder="搜索设备名称或呼号"
          loading={loading}
          fullWidth
        />
      </Paper>

      <TableContainer component={Paper} variant="outlined" sx={{ overflow: 'auto' }}>
        <Table sx={{ minWidth: 800, tableLayout: 'fixed' }}>
          <TableHead sx={{ bgcolor: 'grey.50' }}>
            <TableRow>
              <TableCell align="center" sx={{ width: 70 }}>在线</TableCell>
              <TableCell align="center">名称</TableCell>
              <TableCell align="center">设备类型</TableCell>
              <TableCell align="center">呼号-SSID</TableCell>
              <TableCell align="center">所在群组</TableCell>
              <TableCell align="center" sx={{ width: 150 }}>收发控制</TableCell>
              <TableCell align="center" sx={{ width: 120 }}>操作</TableCell>
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
                  暂无设备数据
                </TableCell>
              </TableRow>
            ) : (
              paginatedDevices.map((device) => {
                const group = getGroupInfo(device.group_id)
                return (
                  <TableRow key={device.id} hover>
                    <TableCell align="center">
                      <OnlineIndicator online={device.online || device.is_online || false} />
                    </TableCell>
                    <TableCell align="center" sx={{ fontWeight: 500 }}>
                      {device.name}
                    </TableCell>
                    <TableCell align="center">
                      <Typography variant="body2" color="text.secondary">
                        {getDevModelName(device.model ?? device.dev_model ?? 0)}
                      </Typography>
                    </TableCell>
                    <TableCell align="center">
                      {device.callsign}-{device.ssid}
                    </TableCell>
                    <TableCell align="center">
                      <Button
                        size="small"
                        variant="outlined"
                        onClick={() => handleOpenSwitchDialog(device)}
                        endIcon={group?.type === GROUP_TYPE_PRIVATE ? <Lock fontSize="small" /> : undefined}
                      >
                        {group?.name || '无群组'}
                      </Button>
                    </TableCell>
                    <TableCell align="center">
                      <SendRecvControl
                        disableSend={device.disable_send ?? false}
                        disableRecv={device.disable_recv ?? false}
                        onToggleSend={() => handleToggleSend(device)}
                        onToggleRecv={() => handleToggleRecv(device)}
                      />
                    </TableCell>
                    <TableCell align="center">
                      <Stack direction="row" spacing={0.5} justifyContent="center" alignItems="center">
                        <Tooltip title="设置">
                          <IconButton
                            size="small"
                            color="secondary"
                            onClick={() => {
                              setParamDevice(device)
                              setParamDialogOpen(true)
                            }}
                          >
                            <Settings fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="删除设备">
                          <IconButton size="small" color="error" onClick={() => handleDelete(device.id)}>
                            <Delete fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Stack>
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
        <GroupPickerDialog
          open={switchDialogOpen}
          onClose={() => {
            setSwitchDialogOpen(false)
            setSwitchingDevice(null)
          }}
          device={switchingDevice}
          groups={groups}
          currentGroupId={switchingDevice.group_id}
          onSelect={handleSwitchGroup}
          title="切换设备群组"
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
          setConfirmDialog((prev) => ({ ...prev, open: false }))
        }}
        onCancel={() => setConfirmDialog((prev) => ({ ...prev, open: false }))}
      />

      {/* 参数下发对话框 */}
      <ParamConfigDialog
        open={paramDialogOpen}
        deviceId={paramDevice?.id || 0}
        deviceName={paramDevice?.name || ''}
        deviceModel={paramDevice?.model ?? paramDevice?.dev_model ?? 1}
        isOnline={paramDevice?.is_online || false}
        onClose={() => setParamDialogOpen(false)}
        onDeviceUpdated={loadDevices}
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
