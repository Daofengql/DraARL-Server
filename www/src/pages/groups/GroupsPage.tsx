import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
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
} from '@mui/material'
import {
  Add,
  Search,
  LockOpen,
  Lock,
  CheckCircle,
  People,
  Logout,
  ArrowForwardIos,
} from '@mui/icons-material'
import { groupService } from '../../services'
import type { Group } from '../../types'

const GROUP_TYPE_PUBLIC = 1
const GROUP_TYPE_PRIVATE = 2

export function GroupsPage() {
  const navigate = useNavigate()
  const [groups, setGroups] = useState<Group[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 对话框状态控制
  const [dialogOpen, setDialogOpen] = useState(false)
  const [searchDialogOpen, setSearchDialogOpen] = useState(false)
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false)

  // 选中的群组与表单状态
  const [joiningGroup, setJoiningGroup] = useState<Group | null>(null)
  const [searchResults, setSearchResults] = useState<Group[]>([])
  const [searchLoading, setSearchLoading] = useState(false)
  const [searchKeywordInput, setSearchKeywordInput] = useState('')
  const [joinPassword, setJoinPassword] = useState('')
  const [formData, setFormData] = useState({
    name: '',
    type: 1,
    callsign: '',
    password: '',
  })

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
    if (!confirm(`确定要退出群组 "${group.name}" 吗？这会将您在该群组的设备移至默认群组。`)) return
    try {
      await groupService.leave(group.id)
      loadGroups()
    } catch (err: any) {
      setError(err.response?.data?.message || '退出群组失败')
    }
  }

  const handleSave = async () => {
    if (!formData.name) {
      setError('请输入群组名称')
      return
    }
    if (formData.type === GROUP_TYPE_PRIVATE && !formData.password) {
      setError('私有群组必须设置密码')
      return
    }
    try {
      await groupService.create(formData)
      setDialogOpen(false)
      loadGroups()
    } catch (err: any) {
      setError(err.response?.data?.message || '创建失败')
    }
  }

  const handleEnterGroup = (id: number) => {
    navigate(`/groups/${id}`)
  }

  const publicGroups = groups.filter(g => g.type === GROUP_TYPE_PUBLIC)
  const privateGroups = groups.filter(g => g.type === GROUP_TYPE_PRIVATE && g.is_joined)

  // 渲染群组表格行
  const renderGroupRow = (group: Group) => (
    <TableRow key={group.id} hover>
      <TableCell>
        <Stack direction="row" alignItems="center" spacing={1}>
          {group.type === GROUP_TYPE_PRIVATE ? <Lock color="secondary" fontSize="small" /> : <LockOpen color="primary" fontSize="small" />}
          <Typography fontWeight={500}>{group.name}</Typography>
          {group.type === GROUP_TYPE_PRIVATE && group.is_joined && (
            <CheckCircle color="success" sx={{ fontSize: 16 }} />
          )}
        </Stack>
      </TableCell>
      <TableCell>
        {group.type === GROUP_TYPE_PRIVATE
          ? (group.ower_callsign || '-')
          : (group.callsign || '-')
        }
      </TableCell>
      <TableCell>
        <Stack direction="row" alignItems="center" spacing={0.5}>
          <People fontSize="small" />
          <span>{group.online_count || 0}/{group.total_count || 0}</span>
        </Stack>
      </TableCell>
      <TableCell align="right">
        {group.type === GROUP_TYPE_PRIVATE && group.is_joined && (
          <IconButton
            size="small"
            color="error"
            onClick={() => handleLeaveGroup(group)}
            sx={{ mr: 1 }}
          >
            <Logout fontSize="small" />
          </IconButton>
        )}
        <Button
          size="small"
          variant="contained"
          endIcon={<ArrowForwardIos sx={{ fontSize: '12px !important' }} />}
          onClick={() => handleEnterGroup(group.id)}
        >
          进入
        </Button>
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
            搜索/加入群组
          </Button>
          <Button variant="contained" startIcon={<Add />} onClick={() => setDialogOpen(true)}>
            新建群组
          </Button>
        </Stack>
      </Box>

      {error && <Alert severity="error" sx={{ mb: 2, flexShrink: 0 }} onClose={() => setError('')}>{error}</Alert>}

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
                <TableCell>群组名称</TableCell>
                <TableCell>呼号</TableCell>
                <TableCell>设备数</TableCell>
                <TableCell align="right">操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={4} align="center">加载中...</TableCell></TableRow>
              ) : publicGroups.length === 0 ? (
                <TableRow><TableCell colSpan={4} align="center">暂无公开群组</TableCell></TableRow>
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
            <Typography variant="subtitle1" fontWeight={600}>已加入的私有群组</Typography>
            <Typography variant="body2" color="text.secondary">({privateGroups.length} 个)</Typography>
          </Stack>
        </Box>
        <TableContainer sx={{ flex: 1 }}>
          <Table stickyHeader>
            <TableHead>
              <TableRow>
                <TableCell>群组名称</TableCell>
                <TableCell>创建者</TableCell>
                <TableCell>设备数</TableCell>
                <TableCell align="right">操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={4} align="center">加载中...</TableCell></TableRow>
              ) : privateGroups.length === 0 ? (
                <TableRow><TableCell colSpan={4} align="center">暂未加入任何私有群组</TableCell></TableRow>
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
                          加入群组
                        </Button>
                      ) : group.is_joined ? (
                        <Stack direction="row" spacing={0.5} alignItems="center" sx={{ color: 'success.main' }}>
                          <CheckCircle fontSize="small" />
                          <span>已加入</span>
                        </Stack>
                      ) : (
                        <Button size="small" variant="contained" onClick={() => handleEnterGroup(group.id)}>进入</Button>
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

      {/* 新建群组对话框 */}
      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>新建群组</DialogTitle>
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
            {formData.type === GROUP_TYPE_PRIVATE && (
              <TextField label="加入密码" fullWidth type="password" required value={formData.password} onChange={(e) => setFormData({ ...formData, password: e.target.value })} />
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialogOpen(false)}>取消</Button>
          <Button onClick={handleSave} variant="contained">确认创建</Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
