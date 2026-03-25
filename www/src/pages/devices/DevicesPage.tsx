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
  Alert,
  Stack,
  Tooltip,
  IconButton,
} from '@mui/material'
import Delete from '@mui/icons-material/Delete'
import Lock from '@mui/icons-material/Lock'
import Key from '@mui/icons-material/Key'
import Settings from '@mui/icons-material/Settings'
import AddLink from '@mui/icons-material/AddLink'
import { deviceService, groupService } from '../../services'
import type { Device, Group } from '../../types'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { ParamConfigDialog } from '../../components/devices/ParamConfigDialog'
import { DynamicCodeBindDialog } from '../../components/devices/DynamicCodeBindDialog'
import { DevicePasswordDialog } from '../../components/devices/DevicePasswordDialog'
import { GroupPickerDialog } from '../../components/groups/GroupPicker'
import { PageHeader } from '../../components/common/PageHeader'
import { SearchBar } from '../../components/common/SearchBar'
import { AutoRefresh } from '../../components/common/AutoRefresh'
import { OnlineIndicator } from '../../components/common/OnlineIndicator'
import { SendRecvControl } from '../../components/common/SendRecvControl'
import { getDevModelName } from '../../utils/deviceModel'

const GROUP_TYPE_PRIVATE = 2

export function DevicesPage() {
  const [devices, setDevices] = useState<Device[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 切换群组对话框状态
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

  // 自动刷新状态
  const [autoRefresh, setAutoRefresh] = useState(0)

  // 参数下发对话框状态
  const [paramDialogOpen, setParamDialogOpen] = useState(false)
  const [paramDevice, setParamDevice] = useState<Device | null>(null)

  // 动态码绑定对话框状态
  const [dynamicBindDialogOpen, setDynamicBindDialogOpen] = useState(false)

  // 设备密码对话框状态
  const [devicePasswordDialogOpen, setDevicePasswordDialogOpen] = useState(false)

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
              variant="outlined"
              size="small"
              startIcon={<AddLink />}
              onClick={() => setDynamicBindDialogOpen(true)}
              color="secondary"
            >
              动态码绑定
            </Button>
            <Button
              variant="contained"
              size="small"
              startIcon={<Key />}
              onClick={() => setDevicePasswordDialogOpen(true)}
              color="primary"
            >
              设备密码
            </Button>
          </Stack>
        }
      />

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

      {/* 动态码绑定对话框 */}
      <DynamicCodeBindDialog
        open={dynamicBindDialogOpen}
        onClose={() => setDynamicBindDialogOpen(false)}
      />

      {/* 设备密码对话框 */}
      <DevicePasswordDialog
        open={devicePasswordDialogOpen}
        onClose={() => setDevicePasswordDialogOpen(false)}
      />
    </Box>
  )
}
