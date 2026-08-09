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
  Typography,
  Switch,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import Person from '@mui/icons-material/Person'
import SettingsInputAntenna from '@mui/icons-material/SettingsInputAntenna'
import ManageAccounts from '@mui/icons-material/ManageAccounts'
import Campaign from '@mui/icons-material/Campaign'
import { groupService } from '../../services/group'
import { userService } from '../../services'
import type { Group, User } from '../../types'
import { UserDetailPopover } from '../../components/UserDetailPopover'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { PageHeader } from '../../components/common/PageHeader'
import { SearchBar } from '../../components/common/SearchBar'
import { BroadcastManagementDialog, GroupDeviceManagementDialog, GroupMemberManagementDialog, GroupTypeIcon, GROUP_TYPE_PUBLIC, GROUP_TYPE_PRIVATE } from '../../components/groups'

export function AdminGroupPage() {
  const [groups, setGroups] = useState<Group[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchKeyword, setSearchKeyword] = useState('')

  // 对话框状态
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<Group | null>(null)
  const [deletingGroup, setDeletingGroup] = useState<Group | null>(null)
  const [managedGroup, setManagedGroup] = useState<Group | null>(null)
  const [managedMembersGroup, setManagedMembersGroup] = useState<Group | null>(null)
  const [broadcastGroup, setBroadcastGroup] = useState<Group | null>(null)

  // 用户详情弹窗状态
  const [userDetailAnchorEl, setUserDetailAnchorEl] = useState<HTMLElement | null>(null)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)

  // 确认对话框状态
  const [confirmDialog, setConfirmDialog] = useState<{
    open: boolean
    title: string
    message: string
    type: 'danger' | 'warning' | 'info'
    onConfirm: () => void
  }>({ open: false, title: '', message: '', type: 'info', onConfirm: () => {} })

  // 表单状态
  const [formData, setFormData] = useState({
    name: '',
    type: 1,
    password: '',
    note: '',
    status: 1,
  })

  const fetchGroups = async () => {
    setLoading(true)
    try {
      const items = await groupService.listAll({
        admin: true,
        keyword: searchKeyword || undefined,
      })
      setGroups(items)
    } catch {
      setError('获取群组列表失败')
    } finally {
      setLoading(false)
    }
  }

  // 获取用户信息（用于显示群组创建者详情）
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
      } catch (error) {
        console.error('Failed to load user info:', error)
      }
    }
  }

  // 关闭用户详情
  const handleCloseUserDetail = () => {
    setUserDetailAnchorEl(null)
    setSelectedUser(null)
  }

  // Reference data is loaded once; group keyword changes are handled by the debounced effect below.
  useEffect(() => {
    fetchGroups()
    loadUsers()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const loadUsers = async () => {
    try {
      const data = await userService.listAll()
      setUsers(data)
    } catch (error) {
      console.error('Failed to load users:', error)
    }
  }

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      fetchGroups()
    }, 500)
    return () => clearTimeout(timeoutId)
  }, [searchKeyword]) // eslint-disable-line react-hooks/exhaustive-deps

  const handleSearch = () => {
    fetchGroups()
  }

  const handleOpenAdd = () => {
    setEditingGroup(null)
    setFormData({
      name: '',
      type: 1,
      password: '',
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
      password: '', // 编辑时不强制回显密码
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
    } catch {
      setError(editingGroup ? '更新群组失败' : '创建群组失败')
    }
  }

  const handleDelete = async () => {
    if (!deletingGroup) return
    try {
      await groupService.delete(deletingGroup.id)
      setDeleteDialogOpen(false)
      fetchGroups()
    } catch {
      setError('删除群组失败')
    }
  }

  // 快捷切换群组状态
  const handleToggleStatus = async (group: Group) => {
    const newStatus = group.status === 1 ? 0 : 1
    const actionText = newStatus === 1 ? '启用' : '禁用'
    setConfirmDialog({
      open: true,
      title: `${actionText}群组`,
      message: `确定要${actionText}群组 "${group.name}" 吗？`,
      type: newStatus === 1 ? 'info' : 'warning',
      onConfirm: async () => {
        try {
          await groupService.update(group.id, { status: newStatus })
          fetchGroups()
        } catch {
          setError(`${actionText}失败`)
        }
      },
    })
  }

  // 渲染状态列（管理员可切换）
  const renderStatusSwitch = (group: Group) => {
    return (
      <Tooltip title={group.status === 1 ? '点击禁用' : '点击启用'}>
        <Switch
          checked={group.status === 1}
          onChange={() => handleToggleStatus(group)}
          size="small"
          color={group.status === 1 ? 'success' : 'default'}
        />
      </Tooltip>
    )
  }

  // 渲染群组表格行
  const renderGroupRow = (group: Group) => {
    const devCount = group.total_count || (group.devlist ? group.devlist.split(',').filter(Boolean).length : 0)

    return (
      <TableRow key={group.id} hover>
        <TableCell width={60}>{group.id}</TableCell>
        <TableCell>
          <Stack direction="row" alignItems="center" spacing={1}>
            <GroupTypeIcon type={group.type} />
            <Typography fontWeight={500}>{group.name}</Typography>
          </Stack>
        </TableCell>
        <TableCell>
          {group.ower_id ? (
            <Stack direction="row" alignItems="center" spacing={1}>
              <Stack
                direction="row"
                alignItems="center"
                spacing={1}
                sx={{
                  cursor: 'pointer',
                  '&:hover .owner-text': { color: 'primary.main', textDecoration: 'underline' },
                }}
                onClick={(event) => handleOpenUserDetail(event, group.ower_id!)}
              >
                <Person color="primary" fontSize="small" />
                <Typography className="owner-text" variant="body2">
                  {getUserInfo(group.ower_id)?.username || getUserInfo(group.ower_id)?.callsign || group.ower_callsign || '-'}
                </Typography>
              </Stack>
            </Stack>
          ) : (group.ower_name || group.ower_callsign || '-')}
        </TableCell>
        <TableCell>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Typography>{group.online_count ?? 0}/{group.total_count ?? devCount}</Typography>
            <Tooltip title="管理设备">
              <IconButton size="small" onClick={() => setManagedGroup(group)}>
                <SettingsInputAntenna fontSize="small" />
              </IconButton>
            </Tooltip>
          </Stack>
        </TableCell>
        <TableCell>{renderStatusSwitch(group)}</TableCell>
        <TableCell>
          <Typography sx={{ maxWidth: 150, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {group.note || '-'}
          </Typography>
        </TableCell>
        <TableCell align="right" width={120}>
          {!group.is_virtual && (
            <Tooltip title="自动播报">
              <IconButton size="small" onClick={() => setBroadcastGroup(group)}><Campaign fontSize="small" /></IconButton>
            </Tooltip>
          )}
          {group.type === GROUP_TYPE_PRIVATE && (
            <Tooltip title="管理成员">
              <IconButton size="small" onClick={() => setManagedMembersGroup(group)}><ManageAccounts fontSize="small" /></IconButton>
            </Tooltip>
          )}
          <Tooltip title="编辑">
            <IconButton size="small" onClick={() => handleOpenEdit(group)}><Edit fontSize="small" /></IconButton>
          </Tooltip>
          <Tooltip title="删除">
            <IconButton size="small" color="error" onClick={() => handleOpenDelete(group)}><Delete fontSize="small" /></IconButton>
          </Tooltip>
        </TableCell>
      </TableRow>
    )
  }

  // 分类群组
  const publicGroups = groups.filter(g => g.type === GROUP_TYPE_PUBLIC)
  const privateGroups = groups.filter(g => g.type === GROUP_TYPE_PRIVATE)

  return (
    <Box sx={{ height: 'calc(100vh - 120px)', display: 'flex', flexDirection: 'column' }}>
      <PageHeader
        title="群组管理"
        actions={
          <Stack direction="row" spacing={1}>
            <Button
              startIcon={<Add />}
              onClick={handleOpenAdd}
              variant="contained"
              size="small"
            >
              添加群组
            </Button>
          </Stack>
        }
      />

      {error && (
        <Alert severity="error" sx={{ mb: 2, flexShrink: 0 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 搜索栏 */}
      <Paper sx={{ mb: 2, flexShrink: 0, p: 2 }}>
        <SearchBar
          value={searchKeyword}
          onChange={setSearchKeyword}
          onSearch={handleSearch}
          placeholder="搜索群组 ID 或名称..."
          loading={loading}
          fullWidth
        />
      </Paper>

      {/* 公开群组表格 - 占 2/3 */}
      <Paper variant="outlined" sx={{ flex: 2, display: 'flex', flexDirection: 'column', mb: 1, overflow: 'hidden' }}>
        <Box sx={{ bgcolor: 'primary.50', px: 2, py: 1, borderBottom: 1, borderColor: 'divider' }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <GroupTypeIcon type={GROUP_TYPE_PUBLIC} />
            <Typography variant="subtitle1" fontWeight={600}>公开群组</Typography>
            <Typography variant="body2" color="text.secondary">({publicGroups.length} 个)</Typography>
          </Stack>
        </Box>
        <TableContainer sx={{ flex: 1, overflow: 'auto' }}>
          <Table sx={{ minWidth: 900 }}>
            <TableHead>
              <TableRow>
                <TableCell width={60}>ID</TableCell>
                <TableCell>群组名称</TableCell>
                <TableCell width={100}>拥有者</TableCell>
                <TableCell width={120}>设备数量</TableCell>
                <TableCell width={100}>状态</TableCell>
                <TableCell>备注</TableCell>
                <TableCell align="right" width={120}>操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={7} align="center">加载中...</TableCell></TableRow>
              ) : publicGroups.length === 0 ? (
                <TableRow><TableCell colSpan={7} align="center">暂无公开群组</TableCell></TableRow>
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
            <GroupTypeIcon type={GROUP_TYPE_PRIVATE} />
            <Typography variant="subtitle1" fontWeight={600}>私有群组</Typography>
            <Typography variant="body2" color="text.secondary">({privateGroups.length} 个)</Typography>
          </Stack>
        </Box>
        <TableContainer sx={{ flex: 1, overflow: 'auto' }}>
          <Table sx={{ minWidth: 900 }}>
            <TableHead>
              <TableRow>
                <TableCell width={60}>ID</TableCell>
                <TableCell>群组名称</TableCell>
                <TableCell width={100}>拥有者</TableCell>
                <TableCell width={120}>设备数量</TableCell>
                <TableCell width={100}>状态</TableCell>
                <TableCell>备注</TableCell>
                <TableCell align="right" width={120}>操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={7} align="center">加载中...</TableCell></TableRow>
              ) : privateGroups.length === 0 ? (
                <TableRow><TableCell colSpan={7} align="center">暂无私有群组</TableCell></TableRow>
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
                <MenuItem value={GROUP_TYPE_PUBLIC}>
                  <Stack direction="row" alignItems="center" spacing={1}>
                    <GroupTypeIcon type={GROUP_TYPE_PUBLIC} />
                    <span>公开群组</span>
                  </Stack>
                </MenuItem>
                <MenuItem value={GROUP_TYPE_PRIVATE}>
                  <Stack direction="row" alignItems="center" spacing={1}>
                    <GroupTypeIcon type={GROUP_TYPE_PRIVATE} />
                    <span>私有群组</span>
                  </Stack>
                </MenuItem>
              </Select>
            </FormControl>
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

      <GroupDeviceManagementDialog
        open={Boolean(managedGroup)}
        group={managedGroup}
        onClose={() => setManagedGroup(null)}
        onChanged={() => void fetchGroups()}
      />

      <GroupMemberManagementDialog
        open={Boolean(managedMembersGroup)}
        group={managedMembersGroup}
        onClose={() => setManagedMembersGroup(null)}
        onChanged={() => void fetchGroups()}
      />

      <BroadcastManagementDialog
        open={Boolean(broadcastGroup)}
        group={broadcastGroup}
        onClose={() => setBroadcastGroup(null)}
      />

      {/* 用户详情弹窗 */}
      <UserDetailPopover
        open={Boolean(userDetailAnchorEl)}
        anchorEl={userDetailAnchorEl}
        onClose={handleCloseUserDetail}
        user={selectedUser}
      />

      {/* 通用确认对话框 */}
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
    </Box>
  )
}
