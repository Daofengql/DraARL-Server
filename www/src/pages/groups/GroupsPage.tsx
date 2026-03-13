import { useEffect, useState } from 'react'
// import { useNavigate } from 'react-router-dom' // 移除了冗余的路由跳转
import {
  Box,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
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
  InputAdornment,
  Tooltip,
  FormControlLabel,
  Switch,
  Chip,
} from '@mui/material'
import {
  Add,
  Search,
  LockOpen,
  Lock,
  CheckCircle,
  People,
  Logout,
  Settings,
  Person,
  Edit,
  Delete,
} from '@mui/icons-material'
import { groupService, userService } from '../../services'
import type { Group, User } from '../../types'
import { UserDetailPopover } from '../../components/UserDetailPopover'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

const GROUP_TYPE_PUBLIC = 1
const GROUP_TYPE_PRIVATE = 2

export function GroupsPage() {
  // const navigate = useNavigate() // 移除了冗余的路由跳转
  const [groups, setGroups] = useState<Group[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 对话框状态��制
  const [dialogOpen, setDialogOpen] = useState(false)
  const [searchDialogOpen, setSearchDialogOpen] = useState(false)
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)

  // 用户详情弹窗状态
  const [userDetailAnchorEl, setUserDetailAnchorEl] = useState<HTMLElement | null>(null)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [loadingUser, setLoadingUser] = useState(false)

  // 选中的群组与表单状态
  const [editingGroup, setEditingGroup] = useState<Group | null>(null) // 新增：当前正在编辑的群组
  const [joiningGroup, setJoiningGroup] = useState<Group | null>(null)
  const [deletingGroup, setDeletingGroup] = useState<Group | null>(null)
  const [searchResults, setSearchResults] = useState<Group[]>([])
  const [searchLoading, setSearchLoading] = useState(false)
  const [searchKeywordInput, setSearchKeywordInput] = useState('')
  const [joinPassword, setJoinPassword] = useState('')
  const [formData, setFormData] = useState({
    name: '',
    type: 1,
    callsign: '',
    password: '',
    status: 1,
  })

  // 确认对话框状态
  const [confirmDialog, setConfirmDialog] = useState<{
    open: boolean
    title: string
    message: string
    type: 'danger' | 'warning' | 'info'
    onConfirm: () => void
  }>({ open: false, title: '', message: '', type: 'info', onConfirm: () => {} })

  useEffect(() => {
    loadGroups()
  }, [])

  const loadGroups = async () => {
    setLoading(true)
    try {
      const data = await groupService.list()
      setGroups(data)
    } catch (err) {
      console.error('Failed to load groups:', err)
      setError('加载群组列表失败')
    } finally {
      setLoading(false)
    }
  }

  // 打开用户详��（通过 API 获取公开信息）
  const handleOpenUserDetail = async (event: React.MouseEvent<HTMLElement>, userId: number) => {
    event.stopPropagation()
    // 先保存 currentTarget，因为异步操作后 event 对象会被重用
    const target = event.currentTarget
    setLoadingUser(true)
    try {
      const user = await userService.getPublicInfo(userId)
      console.log('User info loaded:', user)
      setSelectedUser(user)
      setUserDetailAnchorEl(target)
      console.log('AnchorEl set:', target)
    } catch (err) {
      console.error('Failed to load user info:', err)
      setError('获取用户信息失败')
    } finally {
      setLoadingUser(false)
    }
  }

  // 关闭用户详情
  const handleCloseUserDetail = () => {
    setUserDetailAnchorEl(null)
    setSelectedUser(null)
  }

  const handleSearchGroups = async () => {
    if (!searchKeywordInput.trim()) {
      setError('请输入搜索关键词')
      return
    }
    setSearchLoading(true)
    try {
      const result = await groupService.search({
        keyword: searchKeywordInput,
        page: 1,
        page_size: 10,
      })
      setSearchResults(result.items)
    } catch (err) {
      console.error('Search failed:', err)
      setError('搜索群组失败')
    } finally {
      setSearchLoading(false)
    }
  }

  const handleJoinGroup = async () => {
    if (!joiningGroup || !joinPassword) {
      setError('请输入密码')
      return
    }
    try {
      await groupService.join(joiningGroup.id, joinPassword)
      setPasswordDialogOpen(false)
      setJoinPassword('')
      setJoiningGroup(null)
      setSearchDialogOpen(false)
      setError('')
      loadGroups()
    } catch (err: any) {
      setError(err.response?.data?.message || '加入群组失败，请检查密码')
    }
  }

  const handleLeaveGroup = async (group: Group) => {
    setConfirmDialog({
      open: true,
      title: '退出群组',
      message: `确定要退出群组 "${group.name}" 吗？这会将您在该群组的设备移至默认群组。`,
      type: 'warning',
      onConfirm: async () => {
        try {
          await groupService.leave(group.id)
          loadGroups()
        } catch (err: any) {
          setError(err.response?.data?.message || '退出群组失败')
        }
      },
    })
  }

  // 打开新建弹窗
  const handleOpenAdd = () => {
    setEditingGroup(null)
    setFormData({ name: '', type: 1, callsign: '', password: '', status: 1 })
    setDialogOpen(true)
  }

  // 打开编辑弹窗
  const handleOpenEdit = (group: Group) => {
    setEditingGroup(group)
    setFormData({
      name: group.name,
      type: group.type,
      callsign: group.callsign || '',
      password: '', // 编辑时不强制回显密码
      status: group.status ?? 1,
    })
    setDialogOpen(true)
  }

  const handleSave = async () => {
    if (!formData.name) {
      setError('请输入群组名称')
      return
    }
    // 如果是私有群组，且是新建模式（或强制要求修改密码），则校验密码
    if (formData.type === GROUP_TYPE_PRIVATE && !formData.password && !editingGroup) {
      setError('私有群组必须设置密码')
      return
    }
    try {
      if (editingGroup) {
        await groupService.update(editingGroup.id, formData)
      } else {
        await groupService.create(formData)
      }
      setDialogOpen(false)
      loadGroups()
    } catch (err: any) {
      setError(err.response?.data?.message || '保存失败')
    }
  }

  const handleOpenDelete = (group: Group) => {
    setDeletingGroup(group)
    setDeleteDialogOpen(true)
  }

  const handleDelete = async () => {
    if (!deletingGroup) return
    try {
      await groupService.delete(deletingGroup.id)
      setDeleteDialogOpen(false)
      setDeletingGroup(null)
      loadGroups()
    } catch (err: any) {
      setError(err.response?.data?.message || '删除失败')
    }
  }

  // 切换群组状态（启用/禁用）
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
          loadGroups()
        } catch (err: any) {
          setError(err.response?.data?.message || `${actionText}失败`)
        }
      },
    })
  }

  const getStatusLabel = (group: Group) => {
    // 如果是群组所有者，显示可切换的 Switch
    if (group.is_owner) {
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
    // 非所有者显示只读的 Chip
    switch (group.status) {
      case 1:
        return <Chip label="启用" size="small" color="success" />
      case 0:
        return <Chip label="禁用" size="small" color="default" />
      default:
        return <Chip label="未知" size="small" color="default" />
    }
  }

  const publicGroups = groups.filter(g => g.type === GROUP_TYPE_PUBLIC)
  const privateGroups = groups.filter(g => g.type === GROUP_TYPE_PRIVATE && g.is_joined)

  // 渲染群组表格行
  const renderGroupRow = (group: Group) => (
    <TableRow key={group.id} hover sx={{ opacity: group.status === 0 ? 0.5 : 1 }}>
      <TableCell width={60}>{group.id}</TableCell>
      <TableCell>
        <Stack direction="row" alignItems="center" spacing={1}>
          {group.type === GROUP_TYPE_PRIVATE ? <Lock color="secondary" fontSize="small" /> : <LockOpen color="primary" fontSize="small" />}
          <Typography fontWeight={500}>{group.name}</Typography>
          {group.status === 0 && (
            <Chip label="已禁用" size="small" color="error" sx={{ fontSize: '0.7rem', height: 20 }} />
          )}
        </Stack>
      </TableCell>
      <TableCell>{group.callsign || '-'}</TableCell>
      <TableCell>
        {group.ower_id ? (
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
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
              {group.ower_callsign || '-'}
            </Typography>
          </Box>
        ) : (
          <Typography variant="body2" color="text.disabled">-</Typography>
        )}
      </TableCell>
      <TableCell>
        <Stack direction="row" alignItems="center" spacing={0.5}>
          <People fontSize="small" />
          <span>{group.online_count || 0}/{group.total_count || 0}</span>
        </Stack>
      </TableCell>
      <TableCell>{getStatusLabel(group)}</TableCell>
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
        {group.type === GROUP_TYPE_PRIVATE && group.is_joined && (
          <IconButton
            size="small"
            color="error"
            onClick={() => handleLeaveGroup(group)}
            sx={{ mr: 0.5 }}
            title="退出群组"
          >
            <Logout fontSize="small" />
          </IconButton>
        )}
        {group.is_owner && (
          <>
            <Tooltip title="编辑">
              <IconButton size="small" onClick={() => handleOpenEdit(group)}>
                <Edit fontSize="small" />
              </IconButton>
            </Tooltip>
            <Tooltip title="删除">
              <IconButton size="small" color="error" onClick={() => handleOpenDelete(group)}>
                <Delete fontSize="small" />
              </IconButton>
            </Tooltip>
          </>
        )}
      </TableCell>
    </TableRow>
  )

  return (
    <Box sx={{ height: 'calc(100vh - 120px)', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexShrink: 0 }}>
        <Typography variant="h5" fontWeight={600}>我的群组</Typography>
        <Stack direction="row" spacing={2}>
          <Button
            variant="outlined"
            startIcon={<Search />}
            onClick={() => setSearchDialogOpen(true)}
          >
            搜索群组
          </Button>
          <Button variant="contained" startIcon={<Add />} onClick={handleOpenAdd}>
            新建群组
          </Button>
        </Stack>
      </Box>

      {error && <Alert severity="error" sx={{ mb: 2, flexShrink: 0 }} onClose={() => setError('')}>{error}</Alert>}

      {/* 公开群组表格 */}
      <Paper variant="outlined" sx={{ flex: 1, display: 'flex', flexDirection: 'column', mb: 1, overflow: 'hidden' }}>
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

      {/* 私有群组表格 */}
      <Paper variant="outlined" sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <Box sx={{ bgcolor: 'secondary.50', px: 2, py: 1, borderBottom: 1, borderColor: 'divider' }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Lock color="secondary" fontSize="small" />
            <Typography variant="subtitle1" fontWeight={600}>已加入的私有群组</Typography>
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
                <TableRow><TableCell colSpan={8} align="center">暂未加入任何私有群组</TableCell></TableRow>
              ) : (
                privateGroups.map(renderGroupRow)
              )}
            </TableBody>
          </Table>
        </TableContainer>
      </Paper>

      {/* 搜索/加入群组对话框 */}
      <Dialog open={searchDialogOpen} onClose={() => setSearchDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>搜索群组</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField
              fullWidth
              placeholder="输入群组ID或名称搜索..."
              value={searchKeywordInput}
              onChange={(e) => setSearchKeywordInput(e.target.value)}
              onKeyPress={(e) => e.key === 'Enter' && handleSearchGroups()}
              InputProps={{
                endAdornment: (
                  <InputAdornment position="end">
                    <IconButton onClick={handleSearchGroups} disabled={searchLoading}>
                      <Search />
                    </IconButton>
                  </InputAdornment>
                ),
              }}
            />
            {searchResults.length > 0 && (
              <Stack spacing={1}>
                {searchResults.map((group) => (
                  <Paper key={group.id} variant="outlined" sx={{ p: 2 }}>
                    <Stack direction="row" justifyContent="space-between" alignItems="center">
                      <Stack>
                        <Stack direction="row" alignItems="center" spacing={1}>
                          {group.type === GROUP_TYPE_PRIVATE ? <Lock color="secondary" fontSize="small"/> : <LockOpen color="primary" fontSize="small"/>}
                          <Typography fontWeight={500}>{group.name}</Typography>
                        </Stack>
                        <Typography variant="body2" color="text.secondary">
                          ID: {group.id} · 创建者: {group.ower_callsign || '-'}
                        </Typography>
                      </Stack>
                      {group.type === GROUP_TYPE_PRIVATE && !group.is_joined ? (
                        <Button size="small" variant="outlined" onClick={() => { setJoiningGroup(group); setPasswordDialogOpen(true); }}>
                          验证加入
                        </Button>
                      ) : group.is_joined ? (
                        <Stack direction="row" spacing={0.5} alignItems="center" sx={{ color: 'success.main' }}>
                          <CheckCircle fontSize="small" />
                          <span>已加入</span>
                        </Stack>
                      ) : (
                        <Stack direction="row" spacing={0.5} alignItems="center" sx={{ color: 'primary.main' }}>
                          <LockOpen fontSize="small" />
                          <span>公开</span>
                        </Stack>
                      )}
                    </Stack>
                  </Paper>
                ))}
              </Stack>
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setSearchDialogOpen(false)}>关闭</Button>
        </DialogActions>
      </Dialog>

      {/* 密码输入对话框 */}
      <Dialog open={passwordDialogOpen} onClose={() => setPasswordDialogOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>验证密码</DialogTitle>
        <DialogContent>
          <Typography variant="body2" sx={{ mb: 2, mt: 1 }}>请输入 "{joiningGroup?.name}" 的验证密码：</Typography>
          <TextField
            fullWidth
            type="password"
            placeholder="••••••••"
            value={joinPassword}
            onChange={(e) => setJoinPassword(e.target.value)}
            onKeyPress={(e) => e.key === 'Enter' && handleJoinGroup()}
            autoFocus
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPasswordDialogOpen(false)}>取消</Button>
          <Button onClick={handleJoinGroup} variant="contained">确认加入</Button>
        </DialogActions>
      </Dialog>

      {/* 新建/编辑群组对话框 */}
      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{editingGroup ? '群组设置' : '新建群组'}</DialogTitle>
        <DialogContent>
          <Stack spacing={3} sx={{ mt: 1 }}>
            <TextField label="群组名称" fullWidth required value={formData.name} onChange={(e) => setFormData({ ...formData, name: e.target.value })} />
            <FormControl fullWidth>
              <InputLabel>可见性与验证方式</InputLabel>
              <Select value={formData.type} label="可见性与验证方式" onChange={(e) => setFormData({ ...formData, type: e.target.value as number })}>
                <MenuItem value={1}><Stack direction="row" spacing={1}><LockOpen fontSize="small" /><span>公开群组 (所有人可见且无需密码)</span></Stack></MenuItem>
                <MenuItem value={2}><Stack direction="row" spacing={1}><Lock fontSize="small" /><span>私有群组 (需搜索并验证密码)</span></Stack></MenuItem>
              </Select>
            </FormControl>
            <TextField label="呼号标识（可选）" fullWidth value={formData.callsign} onChange={(e) => setFormData({ ...formData, callsign: e.target.value })} />
            {/* 只在私有群组时显示密码框 */}
            {formData.type === GROUP_TYPE_PRIVATE && (
              <TextField
                label={editingGroup ? "重置密码（留空则不修改）" : "加入密码"}
                fullWidth
                type="password"
                required={!editingGroup}
                value={formData.password}
                onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              />
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialogOpen(false)}>取消</Button>
          <Button onClick={handleSave} variant="contained">确认保存</Button>
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
