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
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import Search from '@mui/icons-material/Search'
import Circle from '@mui/icons-material/Circle'
import Lock from '@mui/icons-material/Lock'
import Person from '@mui/icons-material/Person'
import Refresh from '@mui/icons-material/Refresh'
import { deviceService, groupService, userService } from '../../services'
import type { Device, Group, User } from '../../types'
import { SwitchGroupDialog } from '../devices/SwitchGroupDialog'
import { UserDetailPopover } from '../../components/UserDetailPopover'

const DEVICE_MODELS = [
  { value: 0, label: '未知设备' },
  { value: 100, label: '微信小程序' },
  { value: 101, label: 'Android 客户端' },
  { value: 102, label: 'iOS 客户端' },
  { value: 103, label: 'Windows 客户端' },
  { value: 105, label: '浏览器客户端' },
  { value: 106, label: '互联设备' },
]

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
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [switchDialogOpen, setSwitchDialogOpen] = useState(false)
  const [switchingDevice, setSwitchingDevice] = useState<Device | null>(null)
  const [editingDevice, setEditingDevice] = useState<Device | null>(null)
  const [deletingDevice, setDeletingDevice] = useState<Device | null>(null)

  // 用户详情弹窗状态
  const [userDetailAnchorEl, setUserDetailAnchorEl] = useState<HTMLElement | null>(null)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)

  // 自动刷新状态
  const [autoRefresh, setAutoRefresh] = useState(0) // 0=关闭, 10/30/60=秒数

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
    loadUsers()
  }, [])

  // 自动刷新逻辑
  useEffect(() => {
    if (autoRefresh === 0) return

    const timer = setInterval(() => {
      loadDevices()
    }, autoRefresh * 1000)

    return () => clearInterval(timer)
  }, [autoRefresh, page, rowsPerPage, searchKeyword])

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
    // 如果传入的是 User 对象，直接使用
    if (typeof userIdOrUser === 'object') {
      setSelectedUser(userIdOrUser)
      setUserDetailAnchorEl(event.currentTarget)
      return
    }

    // 如果传入的是 userId，先在本地列表中查找，找不到则调用 API
    const localUser = getUserInfo(userIdOrUser)
    if (localUser) {
      setSelectedUser(localUser)
      setUserDetailAnchorEl(event.currentTarget)
    } else {
      // 调用公开接口获取用户信息
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

  // 对设备禁发/禁收状态进行直观的渲染（绿灯正常，红灯禁用）
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
        ...device,
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

  const handleOpenDialog = (device?: Device) => {
    if (device) {
      setEditingDevice(device)
      setDeletingDevice(null)
      setFormData({
        name: device.name,
        callsign: device.callsign,
        ssid: device.ssid,
        model: device.model ?? device.dev_model ?? 1,
        group_id: device.group_id,
        disable_send: device.disable_send ?? false,
        disable_recv: device.disable_recv ?? false,
      })
    } else {
      setEditingDevice(null)
      setDeletingDevice(null)
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
    if (!editingDevice) {
      return
    }
    try {
      await deviceService.update(editingDevice.id, formData)
      handleCloseDialog()
      loadDevices()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
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

  const filteredDevices = devices.filter(
    (d) =>
      !searchKeyword ||
      d.name.toLowerCase().includes(searchKeyword.toLowerCase()) ||
      d.callsign.toLowerCase().includes(searchKeyword.toLowerCase())
  )

  // 获取群组信息
  const getGroupInfo = (groupId: number) => {
    return groups.find((g) => g.id === groupId)
  }

  const handleSearch = () => {
    setPage(0)
    loadDevices()
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4">设备管理</Typography>
        <Stack direction="row" spacing={1} alignItems="center">
          {/* 自动刷新控制 */}
          <FormControl size="small" sx={{ minWidth: 120 }}>
            <InputLabel>自动刷新</InputLabel>
            <Select
              value={autoRefresh}
              label="自动刷新"
              onChange={(e) => setAutoRefresh(e.target.value as number)}
            >
              <MenuItem value={0}>关闭</MenuItem>
              <MenuItem value={10}>10秒</MenuItem>
              <MenuItem value={30}>30秒</MenuItem>
              <MenuItem value={60}>60秒</MenuItem>
            </Select>
          </FormControl>
          <Button
            variant="outlined"
            size="small"
            startIcon={<Refresh />}
            onClick={loadDevices}
            disabled={loading}
          >
            刷新
          </Button>
        </Stack>
      </Box>

      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>{error}</Alert>}

      <Paper sx={{ mb: 2 }}>
        <Box sx={{ display: 'flex', gap: 2, p: 2 }}>
          <TextField
            placeholder="搜索设备名称或呼号"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
            size="small"
            sx={{ flexGrow: 1 }}
          />
          <Button variant="outlined" startIcon={<Search />} onClick={handleSearch}>
            搜索
          </Button>
        </Box>
      </Paper>

      <TableContainer component={Paper} variant="outlined" sx={{ overflow: 'auto' }}>
        <Table sx={{ minWidth: 900 }}>
          <TableHead sx={{ bgcolor: 'grey.50' }}>
            <TableRow>
              <TableCell width={80}>在线</TableCell>
              <TableCell>名称</TableCell>
              <TableCell>设备类型</TableCell>
              <TableCell>呼号SSID</TableCell>
              <TableCell>所有者</TableCell>
              <TableCell>所在群组</TableCell>
              <TableCell width={130}>收发控制</TableCell>
              <TableCell align="right">操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow><TableCell colSpan={8} align="center">加载中...</TableCell></TableRow>
            ) : filteredDevices.length === 0 ? (
              <TableRow><TableCell colSpan={8} align="center">暂无设备数据</TableCell></TableRow>
            ) : (
              filteredDevices.map((device) => {
                const group = getGroupInfo(device.group_id)
                const owner = getUserInfo(device.owner_id)
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
                    <TableCell>
                      <Typography variant="body2" color="text.secondary">
                        {DEVICE_MODELS.find(m => m.value === (device.model ?? device.dev_model))?.label || '未知设备'}
                      </Typography>
                    </TableCell>
                    <TableCell>{device.callsign}-{device.ssid}</TableCell>
                    <TableCell>
                      {owner ? (
                        <Box
                          sx={{
                            display: 'flex',
                            alignItems: 'center',
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

                    {/* 按需求渲染状态按钮: 左绿发, 右绿收，可点击切换 */}
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
                        <IconButton size="small" color="error" onClick={() => handleOpenDelete(device)}>
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
          count={-1} // 后台分页，前端不确定总数
          page={page}
          onPageChange={(_, newPage) => setPage(newPage)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(e) => {
            setRowsPerPage(parseInt(e.target.value, 10))
            setPage(0)
          }}
          labelRowsPerPage="每页行数"
          labelDisplayedRows={({ from, to, count }) => `${from}-${to} ${count !== -1 ? `共 ${count}` : ''}`}
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
                {groups.map((g) => (
                  <MenuItem key={g.id} value={g.id}>
                    {g.name}
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

      {/* 用户详情弹窗 */}
      <UserDetailPopover
        open={Boolean(userDetailAnchorEl)}
        anchorEl={userDetailAnchorEl}
        onClose={handleCloseUserDetail}
        user={selectedUser}
      />
    </Box>
  )
}
