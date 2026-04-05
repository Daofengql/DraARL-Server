import { useState, useEffect } from 'react'
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
import Person from '@mui/icons-material/Person'
import Settings from '@mui/icons-material/Settings'
import { deviceService, groupService, userService } from '../../services'
import type { Device, Group, User } from '../../types'
import { UserDetailPopover } from '../../components/UserDetailPopover'
import { ParamConfigDialog } from '../../components/devices/ParamConfigDialog'
import { GroupPickerDialog } from '../../components/groups/GroupPicker'
import { PageHeader } from '../../components/common/PageHeader'
import { SearchBar } from '../../components/common/SearchBar'
import { AutoRefresh } from '../../components/common/AutoRefresh'
import { OnlineIndicator } from '../../components/common/OnlineIndicator'
import { SendRecvControl } from '../../components/common/SendRecvControl'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { getDevModelName } from '../../utils/deviceModel'

const GROUP_TYPE_PRIVATE = 2

export function AdminDevicePage() {
  const [devices, setDevices] = useState<Device[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 切换群组对话框状态
  const [switchDialogOpen, setSwitchDialogOpen] = useState(false)
  const [switchingDevice, setSwitchingDevice] = useState<Device | null>(null)

  // 删除确认
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deletingDevice, setDeletingDevice] = useState<Device | null>(null)

  // 用户详情弹窗状态
  const [userDetailAnchorEl, setUserDetailAnchorEl] = useState<HTMLElement | null>(null)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)

  // 自动刷新状态
  const [autoRefresh, setAutoRefresh] = useState(0)

  // 参数下发弹窗状态
  const [paramDialogOpen, setParamDialogOpen] = useState(false)
  const [paramDevice, setParamDevice] = useState<Device | null>(null)

  useEffect(() => {
    loadDevices()
    loadGroups()
    loadUsers()
  }, [])

  const loadDevices = async () => {
    setLoading(true)
    try {
      const result = await deviceService.getList({
        page: page + 1,
        page_size: rowsPerPage,
        keyword: searchKeyword || undefined,
      })
      setDevices(result.items)
    } catch (err) {
      console.error('Failed to load devices:', err)
      setError('获取设备列表失败')
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

  const loadUsers = async () => {
    try {
      const data = await userService.getList()
      setUsers(data.items || data)
    } catch (err) {
      console.error('Failed to load users:', err)
    }
  }

  // 获取用户信息
  const getUserInfo = (userId?: number) => {
    if (!userId) return null
    return users.find((u) => u.id === userId)
  }

  // 打开用户详情
  const handleOpenUserDetail = async (event: React.MouseEvent<HTMLElement>, userIdOrUser: number | User) => {
    if (typeof userIdOrUser === 'object') {
      setSelectedUser(userIdOrUser)
      setUserDetailAnchorEl(event.currentTarget)
      return
    }

    const localUser = getUserInfo(userIdOrUser)
    if (localUser) {
      setSelectedUser(localUser)
      setUserDetailAnchorEl(event.currentTarget)
    } else {
      try {
        const user = await userService.getPublicInfo(userIdOrUser)
        setSelectedUser(user)
        setUserDetailAnchorEl(event.currentTarget)
      } catch (err) {
        console.error('Failed to load user info:', err)
      }
    }
  }

  // 关闭用户详情
  const handleCloseUserDetail = () => {
    setUserDetailAnchorEl(null)
    setSelectedUser(null)
  }

  // 快捷切换禁发状态
  const handleToggleSend = async (device: Device) => {
    try {
      await deviceService.update(device.id, {
        disable_send: !(device.disable_send ?? false),
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
        disable_recv: !(device.disable_recv ?? false),
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

  const handleOpenDelete = (device: Device) => {
    setDeletingDevice(device)
    setDeleteDialogOpen(true)
  }

  const handleDelete = async () => {
    if (!deletingDevice) return
    try {
      await deviceService.delete(deletingDevice.id)
      setDeleteDialogOpen(false)
      setDeletingDevice(null)
      loadDevices()
    } catch (err: any) {
      setError(err.response?.data?.message || '删除失败')
    }
  }

  const handleSearch = () => {
    setPage(0)
    loadDevices()
  }

  // 获取群组信息
  const getGroupInfo = (groupId: number) => {
    return groups.find((g) => g.id === groupId)
  }

  return (
    <Box>
      <PageHeader
        title="设备管理"
        actions={
          <AutoRefresh
            value={autoRefresh}
            onChange={setAutoRefresh}
            onRefresh={loadDevices}
            loading={loading}
          />
        }
      />

      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>{error}</Alert>}

      <Paper sx={{ mb: 2, p: 2 }}>
        <SearchBar
          value={searchKeyword}
          onChange={setSearchKeyword}
          onSearch={handleSearch}
          placeholder="搜索设备名称或呼号"
          loading={loading}
          fullWidth
        />
      </Paper>

      <TableContainer component={Paper} variant="outlined" sx={{ overflow: 'auto' }}>
        <Table sx={{ minWidth: 900, tableLayout: 'fixed' }}>
          <TableHead sx={{ bgcolor: 'grey.50' }}>
            <TableRow>
              <TableCell align="center" sx={{ width: 70 }}>在线</TableCell>
              <TableCell align="center">名称</TableCell>
              <TableCell align="center">设备类型</TableCell>
              <TableCell align="center">呼号-SSID</TableCell>
              <TableCell align="center">所有者</TableCell>
              <TableCell align="center">所在群组</TableCell>
              <TableCell align="center" sx={{ width: 150 }}>收发控制</TableCell>
              <TableCell align="center" sx={{ width: 120 }}>操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow><TableCell colSpan={8} align="center">加载中...</TableCell></TableRow>
            ) : devices.length === 0 ? (
              <TableRow><TableCell colSpan={8} align="center">暂无设备数据</TableCell></TableRow>
            ) : (
              devices.map((device) => {
                const group = getGroupInfo(device.group_id)
                const owner = getUserInfo(device.owner_id)
                return (
                  <TableRow key={device.id} hover>
                    <TableCell align="center">
                      <OnlineIndicator online={device.online || device.is_online || false} />
                    </TableCell>
                    <TableCell align="center" sx={{ fontWeight: 500 }}>{device.name}</TableCell>
                    <TableCell align="center">
                      <Typography variant="body2" color="text.secondary">
                        {getDevModelName(device.model ?? device.dev_model ?? 0)}
                      </Typography>
                    </TableCell>
                    <TableCell align="center">{device.callsign}-{device.ssid}</TableCell>
                    <TableCell align="center">
                      {owner ? (
                        <Box
                          sx={{
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            gap: 1,
                            cursor: 'pointer',
                            '&:hover': {
                              '& .owner-text': {
                                color: 'primary.main',
                                textDecoration: 'underline',
                              },
                            },
                          }}
                          onClick={(e) => handleOpenUserDetail(e, owner)}
                        >
                          <Person color="primary" fontSize="small" />
                          <Typography className="owner-text" variant="body2">
                            {owner.callsign || owner.username}
                          </Typography>
                        </Box>
                      ) : (
                        <Typography variant="body2" color="text.disabled">
                          {device.owner_name || device.owner_callsign || '-'}
                        </Typography>
                      )}
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
                          <IconButton size="small" color="error" onClick={() => handleOpenDelete(device)}>
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
          count={-1}
          page={page}
          onPageChange={(_, newPage) => setPage(newPage)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(e) => {
            setRowsPerPage(parseInt(e.target.value, 10))
            setPage(0)
          }}
          labelRowsPerPage="每页行数"
          labelDisplayedRows={({ from, to }) => `${from}-${to}`}
        />
      </TableContainer>

      {/* 删除确认对话框 */}
      <ConfirmDialog
        isOpen={deleteDialogOpen}
        title="确认删除"
        message={`确定要删除设备 ${deletingDevice?.name} 吗？此操作不可撤销。`}
        confirmText="删除"
        cancelText="取消"
        onConfirm={handleDelete}
        onCancel={() => setDeleteDialogOpen(false)}
        type="danger"
      />

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

      {/* 用户详情弹窗 */}
      <UserDetailPopover
        open={Boolean(userDetailAnchorEl)}
        anchorEl={userDetailAnchorEl}
        onClose={handleCloseUserDetail}
        user={selectedUser}
      />

      {/* 参数下发弹窗 */}
      {paramDevice && (
        <ParamConfigDialog
          open={paramDialogOpen}
          deviceId={paramDevice.id}
          deviceName={paramDevice.name}
          deviceModel={paramDevice.model ?? paramDevice.dev_model ?? 1}
          isOnline={paramDevice.is_online}
          onClose={() => {
            setParamDialogOpen(false)
            setParamDevice(null)
          }}
          onDeviceUpdated={loadDevices}
        />
      )}
    </Box>
  )
}
