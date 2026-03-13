import React, { useState, useEffect } from 'react'
import {
  Box,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
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
  Typography,
} from '@mui/material'
import {
  Add,
  Edit,
  Delete,
  Search,
  Refresh,
  ExpandMore,
  LockOpen,
  Lock,
  PersonOff,
  Person,
  Circle,
} from '@mui/icons-material'
import { groupService } from '../../services/group'
import { userService } from '../../services'
import type { Group, Device, User } from '../../types'
import { UserDetailPopover } from '../../components/UserDetailPopover'

const GROUP_TYPE_PUBLIC = 1
const GROUP_TYPE_PRIVATE = 2

export function AdminGroupPage() {
  const [groups, setGroups] = useState<Group[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [total, setTotal] = useState(0)
  const [searchKeyword, setSearchKeyword] = useState('')

  // 群组设备缓存
  const [groupDevices, setGroupDevices] = useState<Record<number, Device[]>>({})
  const [expandedGroupId, setExpandedGroupId] = useState<number | null>(null)

  // 对话框状态
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<Group | null>(null)
  const [deletingGroup, setDeletingGroup] = useState<Group | null>(null)

  // 用户详情弹窗状态
  const [userDetailAnchorEl, setUserDetailAnchorEl] = useState<HTMLElement | null>(null)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)

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
    try {
      const devices = await groupService.getDevices(groupId)
      setGroupDevices((prev) => ({ ...prev, [groupId]: devices }))
    } catch (err) {
      console.error('获取群组设备失败', err)
    }
  }

  // 获取用户信息（用于显示��组创建者详情）
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

  useEffect(() => {
    fetchGroups()
    loadUsers()
  }, [page, rowsPerPage])

  const loadUsers = async () => {
    try {
      const data = await userService.getList()
      setUsers(data.items || data)
    } catch (err) {
      console.error('Failed to load users:', err)
    }
  }

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

  const handleSearch = () => {
    setPage(0)
    fetchGroups()
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
      callsign: group.callsign || '',
      password: '', // 编辑时不强制回显密码
      allow_callsign_ssid: group.allow_callsign_ssid || '',
      note: group.note || '',
      status: group.status ?? 1,
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

  const getStatusLabel = (status?: number) => {
    switch (status) {
      case 1:
        return <Chip label="启用" color="success" size="small" />
      case 0:
        return <Chip label="禁用" color="default" size="small" />
      default:
        return <Chip label="未知" color="default" size="small" />
    }
  }

  // 更新设备禁发/禁收状态
  const handleUpdateDeviceStatus = async (groupId: number, deviceId: number, disableSend: boolean, disableRecv: boolean) => {
    try {
      await groupService.updateDevice(groupId, deviceId, { disable_send: disableSend, disable_recv: disableRecv })
      // 刷新设备列表
      setGroupDevices((prev) => {
        const newCache = { ...prev }
        delete newCache[groupId]
        return newCache
      })
      fetchGroupDevices(groupId)
    } catch (err) {
      setError('更新设备状态失败')
    }
  }

  // 踢出设备
  const handleKickDevice = async (groupId: number, deviceId: number, deviceName: string) => {
    if (!confirm(`确定要将设备"${deviceName}"踢出群组吗？`)) return
    try {
      await groupService.kickDevice(groupId, deviceId)
      // 刷新设备列表
      setGroupDevices((prev) => {
        const newCache = { ...prev }
        delete newCache[groupId]
        return newCache
      })
      fetchGroupDevices(groupId)
    } catch (err) {
      setError('踢出设备失败')
    }
  }

  // 渲染群组表格行
  const renderGroupRow = (group: Group) => {
    const devices = groupDevices[group.id] || []
    const devCount = group.total_count || (group.devlist ? group.devlist.split(',').filter(Boolean).length : 0)
    const isExpanded = expandedGroupId === group.id
    const isPrivate = group.type === GROUP_TYPE_PRIVATE

    return (
      <React.Fragment key={group.id}>
        <TableRow hover>
          <TableCell width={60}>{group.id}</TableCell>
          <TableCell>
            <Stack direction="row" alignItems="center" spacing={1}>
              {isPrivate ? (
                <Lock color="secondary" fontSize="small" sx={{ fontSize: 16 }} />
              ) : (
                <LockOpen color="primary" fontSize="small" sx={{ fontSize: 16 }} />
              )}
              <Typography fontWeight={500}>{group.name}</Typography>
            </Stack>
          </TableCell>
          <TableCell>
            {group.callsign || '-'}
          </TableCell>
          <TableCell>
            {group.ower_id ? (
              <Stack
                direction="row"
                alignItems="center"
                spacing={1}
                sx={{
                  cursor: 'pointer',
                  '&:hover .owner-text': {
                    color: 'primary.main',
                    textDecoration: 'underline',
                  },
                }}
                onClick={(e) => handleOpenUserDetail(e, group.ower_id!)}
              >
                <Person color="primary" fontSize="small" />
                <Typography className="owner-text" variant="body2">
                  {getUserInfo(group.ower_id)?.username || getUserInfo(group.ower_id)?.callsign || group.ower_callsign || '-'}
                </Typography>
              </Stack>
            ) : (
              group.ower_name || group.ower_callsign || '-'
            )}
          </TableCell>
          <TableCell>
            <Stack direction="row" alignItems="center" spacing={1}>
              <Typography>
                {group.online_count ?? 0}/{group.total_count ?? devCount}
              </Typography>
              <IconButton
                size="small"
                onClick={() => {
                  if (isExpanded) {
                    setExpandedGroupId(null)
                  } else {
                    setExpandedGroupId(group.id)
                    fetchGroupDevices(group.id)
                  }
                }}
              >
                <ExpandMore fontSize="small" sx={{ transform: isExpanded ? 'rotate(180deg)' : 'none', transition: 'transform 0.2s' }} />
              </IconButton>
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
          <TableCell align="right" width={120}>
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
        {isExpanded && (
          <TableRow>
            <TableCell colSpan={8} sx={{ pb: 2, pt: 1, bgcolor: 'grey.50' }}>
              <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1 }}>
                设备列表 ({devices.length})
              </Typography>
              {devices.length === 0 ? (
                <Typography variant="body2" color="text.secondary">暂无设备</Typography>
              ) : (
                <Stack spacing={1}>
                  {devices.map((device) => (
                    <Stack
                      key={device.id}
                      direction="row"
                      alignItems="center"
                      justifyContent="space-between"
                      sx={{
                        p: 1.5,
                        bgcolor: 'background.paper',
                        borderRadius: 1,
                      }}
                    >
                      <Stack direction="row" alignItems="center" spacing={2}>
                        {/* 在线状态圆点 */}
                        <Circle
                          sx={{
                            fontSize: 12,
                            color: device.is_online ? 'success.main' : 'text.disabled',
                          }}
                        />
                        <Typography variant="body2" fontWeight={500}>
                          {device.name}
                        </Typography>
                        <Typography variant="body2" color="text.secondary">
                          {device.callsign}-{device.ssid}
                        </Typography>
                      </Stack>
                      <Stack direction="row" alignItems="center" spacing={1}>
                        {/* 发送控制按钮 */}
                        <Tooltip title={device.disable_send ? '点击启用发送' : '点击禁用发送'}>
                          <Button
                            size="small"
                            variant={device.disable_send ? 'outlined' : 'contained'}
                            color={device.disable_send ? 'error' : 'success'}
                            onClick={() => handleUpdateDeviceStatus(group.id, device.id!, !(device.disable_send ?? false), device.disable_recv ?? false)}
                            sx={{ minWidth: 56, fontSize: '0.75rem' }}
                          >
                            发送
                          </Button>
                        </Tooltip>

                        {/* 接收控制按钮 */}
                        <Tooltip title={device.disable_recv ? '点击启用接收' : '点击禁用接收'}>
                          <Button
                            size="small"
                            variant={device.disable_recv ? 'outlined' : 'contained'}
                            color={device.disable_recv ? 'error' : 'success'}
                            onClick={() => handleUpdateDeviceStatus(group.id, device.id!, device.disable_send ?? false, !(device.disable_recv ?? false))}
                            sx={{ minWidth: 56, fontSize: '0.75rem' }}
                          >
                            接收
                          </Button>
                        </Tooltip>

                        {/* 踢出设备按钮 */}
                        <Tooltip title="踢出设备">
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => handleKickDevice(group.id, device.id, device.name)}
                          >
                            <PersonOff fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Stack>
                    </Stack>
                  ))}
                </Stack>
              )}
            </TableCell>
          </TableRow>
        )}
      </React.Fragment>
    )
  }

  // 分类群组
  const publicGroups = groups.filter(g => g.type === GROUP_TYPE_PUBLIC)
  const privateGroups = groups.filter(g => g.type === GROUP_TYPE_PRIVATE)

  return (
    <Box sx={{ height: 'calc(100vh - 120px)', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexShrink: 0 }}>
        <Typography variant="h4">群组管理</Typography>
        <Stack direction="row" spacing={2}>
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
        </Stack>
      </Box>

      {error && (
        <Alert severity="error" sx={{ mb: 2, flexShrink: 0 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 搜索栏 */}
      <Paper sx={{ mb: 2, flexShrink: 0 }}>
        <Box sx={{ display: 'flex', gap: 2, p: 2 }}>
          <TextField
            placeholder="搜索群组名称、呼号..."
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

      {/* 公开群组表格 - 占 2/3 */}
      <Paper variant="outlined" sx={{ flex: 2, display: 'flex', flexDirection: 'column', mb: 1, overflow: 'hidden' }}>
        <Box sx={{ bgcolor: 'primary.50', px: 2, py: 1, borderBottom: 1, borderColor: 'divider' }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <LockOpen color="primary" fontSize="small" />
            <Typography variant="subtitle1" fontWeight={600}>公开群组</Typography>
            <Typography variant="body2" color="text.secondary">({publicGroups.length} 个)</Typography>
          </Stack>
        </Box>
        <TableContainer sx={{ flex: 1 }}>
          <Table stickyHeader>
            <TableHead>
              <TableRow>
                <TableCell width={60}>ID</TableCell>
                <TableCell>群组名称</TableCell>
                <TableCell>呼号</TableCell>
                <TableCell width={100}>拥有者</TableCell>
                <TableCell width={120}>设备数量</TableCell>
                <TableCell width={100}>状态</TableCell>
                <TableCell>备注</TableCell>
                <TableCell align="right" width={120}>操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={8} align="center">加载中...</TableCell></TableRow>
              ) : publicGroups.length === 0 ? (
                <TableRow><TableCell colSpan={8} align="center">暂无公开群组</TableCell></TableRow>
              ) : (
                publicGroups.map(renderGroupRow)
              )}
            </TableBody>
          </Table>
        </TableContainer>
      </Paper>

      {/* 私有群组表格 - 占 1/3 */}
      <Paper variant="outlined" sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <Box sx={{ bgcolor: 'secondary.50', px: 2, py: 1, borderBottom: 1, borderColor: 'divider' }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Lock color="secondary" fontSize="small" />
            <Typography variant="subtitle1" fontWeight={600}>私有群组</Typography>
            <Typography variant="body2" color="text.secondary">({privateGroups.length} 个)</Typography>
          </Stack>
        </Box>
        <TableContainer sx={{ flex: 1 }}>
          <Table stickyHeader>
            <TableHead>
              <TableRow>
                <TableCell width={60}>ID</TableCell>
                <TableCell>群组名称</TableCell>
                <TableCell>呼号</TableCell>
                <TableCell width={100}>拥有者</TableCell>
                <TableCell width={120}>设备数量</TableCell>
                <TableCell width={100}>状态</TableCell>
                <TableCell>备注</TableCell>
                <TableCell align="right" width={120}>操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={8} align="center">加载中...</TableCell></TableRow>
              ) : privateGroups.length === 0 ? (
                <TableRow><TableCell colSpan={8} align="center">暂无私有群组</TableCell></TableRow>
              ) : (
                privateGroups.map(renderGroupRow)
              )}
            </TableBody>
          </Table>
        </TableContainer>
      </Paper>

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
                <MenuItem value={1}>
                  <Stack direction="row" alignItems="center" spacing={1}>
                    <LockOpen fontSize="small" />
                    <span>公开群组</span>
                  </Stack>
                </MenuItem>
                <MenuItem value={2}>
                  <Stack direction="row" alignItems="center" spacing={1}>
                    <Lock fontSize="small" />
                    <span>私有群组</span>
                  </Stack>
                </MenuItem>
              </Select>
            </FormControl>
            <TextField
              label="呼号"
              fullWidth
              value={formData.callsign}
              onChange={(e) => setFormData({ ...formData, callsign: e.target.value })}
            />

            {/* 修改点：只有私有群组才显示密码输入框 */}
            {formData.type === GROUP_TYPE_PRIVATE && (
              <TextField
                label={editingGroup ? "新密码 (留空则不修改)" : "密码"}
                fullWidth
                type="password"
                value={formData.password}
                onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              />
            )}

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
