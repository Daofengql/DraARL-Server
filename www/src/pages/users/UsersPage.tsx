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
  Chip,
  Tooltip,
  Card,
  CardContent,
  List,
  ListItem,
  ListItemText,
  Divider,
  Avatar,
  Popover,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import Search from '@mui/icons-material/Search'
import Person from '@mui/icons-material/Person'
import Block from '@mui/icons-material/Block'
import CheckCircle from '@mui/icons-material/CheckCircle'
import Phone from '@mui/icons-material/Phone'
import Cake from '@mui/icons-material/Cake'
import LocationOn from '@mui/icons-material/LocationOn'
import Badge from '@mui/icons-material/Badge'
import CalendarToday from '@mui/icons-material/CalendarToday'
import { userService } from '../../services'
import { authService } from '../../services'
import type { User } from '../../types'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

const USER_ROLES = [
  { value: 'admin', label: '管理员' },
  { value: 'user', label: '普通用户' },
]

// 获取当前登录用户ID
const getCurrentUserId = (): number => {
  const user = authService.getStoredUser()
  return user?.id || 0
}

// 检查是否是主管理员（ID=1）
const isSuperAdmin = (): boolean => {
  return getCurrentUserId() === 1
}

export function UsersPage() {
  const [users, setUsers] = useState<User[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingUser, setEditingUser] = useState<User | null>(null)
  const [detailAnchorEl, setDetailAnchorEl] = useState<HTMLElement | null>(null)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [formData, setFormData] = useState({
    username: '',
    callsign: '',
    password: '',
    role: 'user',
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
    loadUsers()
  }, [])

  const loadUsers = async () => {
    setLoading(true)
    try {
      const data = await userService.getList()
      setUsers(data.items || data)
    } catch (err) {
      console.error('Failed to load users:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleOpenDialog = (user?: User) => {
    if (user) {
      setEditingUser(user)
      setFormData({
        username: user.username,
        callsign: user.callsign || '',
        password: '',
        role: user.role,
      })
    } else {
      setEditingUser(null)
      setFormData({
        username: '',
        callsign: '',
        password: '',
        role: 'user',
      })
    }
    setDialogOpen(true)
    setError('')
  }

  const handleCloseDialog = () => {
    setDialogOpen(false)
    setEditingUser(null)
  }

  const handleSave = async () => {
    try {
      if (editingUser) {
        const updateData: any = {
          username: formData.username,
          callsign: formData.callsign,
        }
        // 只有主管理员可以修改角色
        if (isSuperAdmin()) {
          updateData.role = formData.role
        }
        if (formData.password) {
          await userService.changePassword(editingUser.id, {
            old_password: '',
            new_password: formData.password,
          })
        }
        await userService.update(editingUser.id, updateData)
      } else {
        // 创建新用户时，默认角色为 user
        const createData = {
          username: formData.username,
          callsign: formData.callsign,
          password: formData.password,
          role: 'user', // 默认创建普通用户
        }
        await userService.create(createData)
      }
      handleCloseDialog()
      loadUsers()
    } catch (err: any) {
      setError(err.response?.data?.message || '操作失败')
    }
  }

  const handleDelete = async (id: number) => {
    // 不允许删除ID为1的主管理员
    if (id === 1) {
      setError('主管理员不能被删除')
      return
    }
    const user = users.find(u => u.id === id)
    setConfirmDialog({
      open: true,
      title: '删除用户',
      message: `确定要删除用户 "${user?.username || id}" 吗？`,
      type: 'danger',
      onConfirm: async () => {
        try {
          await userService.delete(id)
          loadUsers()
        } catch (err: any) {
          setError(err.response?.data?.message || '删除失败')
        }
      },
    })
  }

  const handleToggleStatus = async (user: User) => {
    // 不允许禁用ID为1的主管理员
    if (user.id === 1) {
      setError('主管理员不能被禁用')
      return
    }

    const newStatus = user.status === 1 ? 0 : 1
    const action = newStatus === 1 ? '启用' : '禁用'

    setConfirmDialog({
      open: true,
      title: `${action}用户`,
      message: `确定要${action}用户 ${user.username} 吗？`,
      type: newStatus === 1 ? 'info' : 'warning',
      onConfirm: async () => {
        try {
          await userService.updateStatus(user.id, newStatus)
          loadUsers()
        } catch (err: any) {
          setError(err.response?.data?.message || `${action}失败`)
        }
      },
    })
  }

  const handleOpenUserDetail = (event: React.MouseEvent<HTMLElement>, user: User) => {
    setSelectedUser(user)
    setDetailAnchorEl(event.currentTarget)
  }

  const handleCloseUserDetail = () => {
    setDetailAnchorEl(null)
    setSelectedUser(null)
  }

  const formatDate = (dateStr?: string) => {
    if (!dateStr) return '-'
    return new Date(dateStr).toLocaleDateString('zh-CN')
  }

  const getSexLabel = (sex: number) => {
    switch (sex) {
      case 1:
        return '男'
      case 2:
        return '女'
      default:
        return '未设置'
    }
  }

  const filteredUsers = users.filter(
    (u) =>
      !searchKeyword ||
      u.username.toLowerCase().includes(searchKeyword.toLowerCase()) ||
      (u.callsign && u.callsign.toLowerCase().includes(searchKeyword.toLowerCase()))
  )

  const paginatedUsers = filteredUsers.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  )

  const currentUserId = getCurrentUserId()

  return (
    <Box>
      <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'center' }, gap: 2, mb: 3 }}>
        <Typography variant="h4" fontWeight={600}>用户管理</Typography>
        <Button variant="contained" size="small" startIcon={<Add />} onClick={() => handleOpenDialog()}>
          添加
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
            placeholder="搜索用户名或呼号"
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

      <TableContainer component={Paper} sx={{ overflow: 'auto' }}>
        <Table sx={{ minWidth: 600 }}>
          <TableHead>
            <TableRow>
              <TableCell>ID</TableCell>
              <TableCell>用户名</TableCell>
              <TableCell>呼号</TableCell>
              <TableCell>角色</TableCell>
              <TableCell>状态</TableCell>
              <TableCell>创建时间</TableCell>
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
            ) : paginatedUsers.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} align="center">
                  暂无数据
                </TableCell>
              </TableRow>
            ) : (
              paginatedUsers.map((user) => (
                <TableRow key={user.id} hover>
                  <TableCell>{user.id}</TableCell>
                  <TableCell>
                    <Box
                      sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 1,
                        cursor: 'pointer',
                        '&:hover': {
                          '& .username-text': {
                            color: 'primary.main',
                            textDecoration: 'underline',
                          },
                        },
                      }}
                      onClick={(e) => handleOpenUserDetail(e, user)}
                    >
                      <Person color="primary" fontSize="small" />
                      <Typography className="username-text" variant="body2">
                        {user.username}
                      </Typography>
                      {user.id === currentUserId && (
                        <Chip label="当前用户" size="small" variant="outlined" sx={{ ml: 1 }} />
                      )}
                    </Box>
                  </TableCell>
                  <TableCell>{user.callsign || '-'}</TableCell>
                  <TableCell>
                    <Chip
                      label={user.role === 'admin' ? '管理员' : '普通用户'}
                      size="small"
                      color={user.role === 'admin' ? 'secondary' : 'default'}
                    />
                  </TableCell>
                  <TableCell>
                    <Chip
                      label={user.status === 1 ? '正常' : '已禁用'}
                      size="small"
                      color={user.status === 1 ? 'success' : 'error'}
                    />
                  </TableCell>
                  <TableCell>
                    {user.created_at
                      ? new Date(user.created_at).toLocaleDateString('zh-CN')
                      : '-'}
                  </TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', gap: 0.5 }}>
                      <Tooltip title="编辑">
                        <IconButton size="small" onClick={() => handleOpenDialog(user)}>
                          <Edit fontSize="small" />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title={user.status === 1 ? '禁用用户' : '启用用户'}>
                        <IconButton
                          size="small"
                          onClick={() => handleToggleStatus(user)}
                          color={user.status === 1 ? 'warning' : 'success'}
                          disabled={user.id === 1}
                        >
                          {user.status === 1 ? <Block fontSize="small" /> : <CheckCircle fontSize="small" />}
                        </IconButton>
                      </Tooltip>
                      <Tooltip title="删除">
                        <IconButton
                          size="small"
                          color="error"
                          onClick={() => handleDelete(user.id)}
                          disabled={user.id === 1}
                        >
                          <Delete fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </Box>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        <TablePagination
          component="div"
          count={filteredUsers.length}
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
        <DialogTitle>{editingUser ? '编辑用户' : '添加用户'}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label="用户名"
              fullWidth
              value={formData.username}
              onChange={(e) => setFormData({ ...formData, username: e.target.value })}
            />
            <TextField
              label="呼号（可选）"
              fullWidth
              value={formData.callsign}
              onChange={(e) => setFormData({ ...formData, callsign: e.target.value })}
            />
            {/* 只有主管理员可以设置用户角色 */}
            {isSuperAdmin() && (
              <FormControl fullWidth>
                <InputLabel>角色</InputLabel>
                <Select
                  value={formData.role}
                  label="角色"
                  onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                >
                  {USER_ROLES.map((role) => (
                    <MenuItem key={role.value} value={role.value}>
                      {role.label}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
            )}
            <TextField
              label={editingUser ? '新密码（留空则不修改）' : '密码'}
              type="password"
              fullWidth
              value={formData.password}
              onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              required={!editingUser}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog}>取消</Button>
          <Button onClick={handleSave} variant="contained">
            保存
          </Button>
        </DialogActions>
      </Dialog>

      {/* 用户详情弹窗 */}
      <Popover
        open={Boolean(detailAnchorEl)}
        anchorEl={detailAnchorEl}
        onClose={handleCloseUserDetail}
        anchorOrigin={{
          vertical: 'bottom',
          horizontal: 'left',
        }}
        transformOrigin={{
          vertical: 'top',
          horizontal: 'left',
        }}
        slotProps={{
          paper: {
            sx: { width: 400, maxHeight: 600, overflow: 'auto' },
          },
        }}
      >
        {selectedUser && (
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2 }}>
                <Avatar
                  src={selectedUser.avatar_thumb || selectedUser.avatar}
                  sx={{ width: 64, height: 64 }}
                >
                  {selectedUser.username?.charAt(0).toUpperCase()}
                </Avatar>
                <Box>
                  <Typography variant="h6">{selectedUser.username}</Typography>
                  <Typography variant="body2" color="text.secondary">
                    ID: {selectedUser.id}
                  </Typography>
                  <Box sx={{ display: 'flex', gap: 0.5, mt: 0.5 }}>
                    <Chip
                      label={selectedUser.role === 'admin' ? '管理员' : '普通用户'}
                      size="small"
                      color={selectedUser.role === 'admin' ? 'secondary' : 'default'}
                    />
                    <Chip
                      label={selectedUser.status === 1 ? '正常' : '已禁用'}
                      size="small"
                      color={selectedUser.status === 1 ? 'success' : 'error'}
                    />
                  </Box>
                </Box>
              </Box>

              <Divider sx={{ mb: 2 }} />

              <List disablePadding dense>
                {selectedUser.callsign && (
                  <ListItem divider>
                    <Badge sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                    <ListItemText
                      primary="呼号"
                      secondary={selectedUser.callsign}
                    />
                  </ListItem>
                )}
                {selectedUser.phone && (
                  <ListItem divider>
                    <Phone sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                    <ListItemText
                      primary="手机号"
                      secondary={selectedUser.phone}
                    />
                  </ListItem>
                )}
                {selectedUser.address && (
                  <ListItem divider>
                    <LocationOn sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                    <ListItemText
                      primary="地址"
                      secondary={selectedUser.address}
                    />
                  </ListItem>
                )}
                {selectedUser.birthday && (
                  <ListItem divider>
                    <Cake sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                    <ListItemText
                      primary="生日"
                      secondary={selectedUser.birthday}
                    />
                  </ListItem>
                )}
                {selectedUser.sex !== undefined && selectedUser.sex !== 0 && (
                  <ListItem divider>
                    <Person sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                    <ListItemText
                      primary="性别"
                      secondary={getSexLabel(selectedUser.sex)}
                    />
                  </ListItem>
                )}
                <ListItem divider>
                  <CalendarToday sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                  <ListItemText
                    primary="注册时间"
                    secondary={formatDate(selectedUser.created_at)}
                  />
                </ListItem>
                {selectedUser.last_login_time && (
                  <ListItem divider>
                    <CheckCircle sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                    <ListItemText
                      primary="最后登录"
                      secondary={new Date(selectedUser.last_login_time).toLocaleString('zh-CN')}
                    />
                  </ListItem>
                )}
                {selectedUser.dmrid && selectedUser.dmrid > 0 && (
                  <ListItem divider>
                    <Badge sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                    <ListItemText
                      primary="DMR ID"
                      secondary={selectedUser.dmrid}
                    />
                  </ListItem>
                )}
                {selectedUser.introduction && (
                  <ListItem>
                    <Typography variant="body2" color="text.secondary">
                      简介: {selectedUser.introduction}
                    </Typography>
                  </ListItem>
                )}
              </List>
            </CardContent>
          </Card>
        )}
      </Popover>

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
    </Box>
  )
}
