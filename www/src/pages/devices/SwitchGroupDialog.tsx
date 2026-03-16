import { useState } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  ListItemIcon,
  Typography,
  Box,
  Stack,
  Chip,
  TextField,
  Alert,
  Tabs,
  Tab,
} from '@mui/material'
import {
  Lock,
  LockOpen,
  CheckCircle,
  Search,
} from '@mui/icons-material'
import type { Device, Group } from '../../types'
import { groupService } from '../../services/group'

interface SwitchGroupDialogProps {
  open: boolean
  onClose: () => void
  device: Device
  groups: Group[]
  onSwitch: (groupId: number, password?: string) => void
}

// 群组类型
const GROUP_TYPE_PUBLIC = 1
const GROUP_TYPE_PRIVATE = 2

function TabPanel({ children, value, index }: { children: React.ReactNode; value: number; index: number }) {
  return (
    <div role="tabpanel" hidden={value !== index}>
      {value === index && <Box sx={{ py: 2 }}>{children}</Box>}
    </div>
  )
}

export function SwitchGroupDialog({
  open,
  onClose,
  device,
  groups: initialGroups,
  onSwitch,
}: SwitchGroupDialogProps) {
  const [tabValue, setTabValue] = useState(0)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [searchResults, setSearchResults] = useState<Group[]>([])
  const [searchLoading, setSearchLoading] = useState(false)
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false)
  const [selectedGroup, setSelectedGroup] = useState<Group | null>(null)
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')

  // 当前群组
  const currentGroup = initialGroups.find((g) => g.id === device.group_id)

  // 分类群组
  const publicGroups = initialGroups.filter((g) => g.type === GROUP_TYPE_PUBLIC)
  const joinedPrivateGroups = initialGroups.filter(
    (g) => g.type === GROUP_TYPE_PRIVATE && g.is_joined
  )

  // 搜索群组
  const handleSearch = async () => {
    if (!searchKeyword.trim()) {
      setError('请输入搜索关键词')
      return
    }
    setSearchLoading(true)
    setError('')
    try {
      const result = await groupService.search({
        keyword: searchKeyword,
        page: 1,
        page_size: 10,
      })
      // 过滤掉当前群组
      setSearchResults(result.items.filter((g) => g.id !== device.group_id))
    } catch (err) {
      setError('搜索群组失败')
    } finally {
      setSearchLoading(false)
    }
  }

  // 选择群组并切换
  const handleSelectGroup = (group: Group) => {
    if (group.id === device.group_id) return

    // 公开群组或已加入的私有群组直接切换
    if (group.type === GROUP_TYPE_PUBLIC || group.is_joined) {
      onSwitch(group.id)
    } else {
      // 私有群组需要密码
      setSelectedGroup(group)
      setPasswordDialogOpen(true)
    }
  }

  // 确认切换（带密码）
  const handleConfirmSwitch = () => {
    if (!selectedGroup) return
    if (selectedGroup.type === GROUP_TYPE_PRIVATE && !password) {
      setError('请输入密码')
      return
    }
    onSwitch(selectedGroup.id, password)
    setPasswordDialogOpen(false)
    setSelectedGroup(null)
    setPassword('')
    setError('')
  }

  // 获取群组图标
  const getGroupIcon = (group: Group) => {
    if (group.type === GROUP_TYPE_PRIVATE) {
      return <Lock color="secondary" fontSize="small" />
    }
    return <LockOpen color="primary" fontSize="small" />
  }

  // 渲染群组列表项
  const renderGroupItem = (group: Group) => {
    const isCurrent = group.id === device.group_id

    return (
      <ListItem
        key={group.id}
        disablePadding
        secondaryAction={
          isCurrent ? (
            <Chip label="当前" size="small" color="primary" variant="outlined" />
          ) : group.is_joined && group.type === GROUP_TYPE_PRIVATE ? (
            <CheckCircle color="success" fontSize="small" />
          ) : null
        }
      >
        <ListItemButton
          onClick={() => handleSelectGroup(group)}
          disabled={isCurrent}
          selected={isCurrent}
        >
          <ListItemIcon>
            {getGroupIcon(group)}
          </ListItemIcon>
          <ListItemText
            primary={group.name}
            secondary={
              <Stack direction="row" alignItems="center" spacing={1}>
                <Typography variant="body2" color="text.secondary">
                  ID: {group.id}
                </Typography>
                {group.type === GROUP_TYPE_PRIVATE && group.ower_callsign && (
                  <>
                    <span>·</span>
                    <Typography variant="body2" color="text.secondary">
                      创建者: {group.ower_callsign}
                    </Typography>
                  </>
                )}
                {group.online_count !== undefined && group.total_count !== undefined && (
                  <>
                    <span>·</span>
                    <Typography variant="body2" color="text.secondary">
                      在线: {group.online_count}/{group.total_count}
                    </Typography>
                  </>
                )}
              </Stack>
            }
          />
        </ListItemButton>
      </ListItem>
    )
  }

  return (
    <>
      <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
        <DialogTitle>
          切换设备群组
        </DialogTitle>
        <DialogContent>
          <Box sx={{ mb: 2 }}>
            <Typography variant="body2" color="text.secondary">
              设备: <strong>{device.name}</strong> ({device.callsign}-{device.ssid})
            </Typography>
            {currentGroup && (
              <Typography variant="body2" color="text.secondary">
                当前群组: <strong>{currentGroup.name}</strong>
              </Typography>
            )}
          </Box>

          <Tabs value={tabValue} onChange={(_, v) => setTabValue(v)} sx={{ borderBottom: 1, borderColor: 'divider' }}>
            <Tab label="已验证群组" />
            <Tab label="公开群组" />
            <Tab label="搜索群组" />
          </Tabs>

          {error && (
            <Alert severity="error" sx={{ my: 2 }} onClose={() => setError('')}>
              {error}
            </Alert>
          )}

          {/* 已验证群组（私有） */}
          <TabPanel value={tabValue} index={0}>
            {joinedPrivateGroups.length === 0 ? (
              <Box sx={{ textAlign: 'center', py: 4 }}>
                <Typography color="text.secondary">
                  暂无已加入的私有群组
                </Typography>
              </Box>
            ) : (
              <List>
                {joinedPrivateGroups.map(renderGroupItem)}
              </List>
            )}
          </TabPanel>

          {/* 公开群组 */}
          <TabPanel value={tabValue} index={1}>
            {publicGroups.length === 0 ? (
              <Box sx={{ textAlign: 'center', py: 4 }}>
                <Typography color="text.secondary">
                  暂无公开群组
                </Typography>
              </Box>
            ) : (
              <List>
                {publicGroups.map(renderGroupItem)}
              </List>
            )}
          </TabPanel>

          {/* 搜索群组 */}
          <TabPanel value={tabValue} index={2}>
            <Stack spacing={2}>
              <Box sx={{ display: 'flex', gap: 1 }}>
                <TextField
                  fullWidth
                  size="small"
                  placeholder="输入群组ID或名称搜索"
                  value={searchKeyword}
                  onChange={(e) => setSearchKeyword(e.target.value)}
                  onKeyPress={(e) => e.key === 'Enter' && handleSearch()}
                />
                <Button
                  variant="outlined"
                  startIcon={<Search />}
                  onClick={handleSearch}
                  disabled={searchLoading}
                >
                  搜索
                </Button>
              </Box>

              {searchResults.length > 0 && (
                <List>
                  {searchResults.map(renderGroupItem)}
                </List>
              )}

              {searchKeyword && searchResults.length === 0 && !searchLoading && (
                <Box sx={{ textAlign: 'center', py: 4 }}>
                  <Typography color="text.secondary">
                    未找到匹配的群组
                  </Typography>
                </Box>
              )}
            </Stack>
          </TabPanel>
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>取消</Button>
        </DialogActions>
      </Dialog>

      {/* 密码验证对话框 */}
      <Dialog open={passwordDialogOpen} onClose={() => setPasswordDialogOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>验证密码</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <Typography variant="body2" color="text.secondary">
              群组 <strong>{selectedGroup?.name}</strong> 需要密码验证
            </Typography>
            <TextField
              fullWidth
              type="password"
              label="密码"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyPress={(e) => e.key === 'Enter' && handleConfirmSwitch()}
              autoFocus
              error={!!error}
              helperText={error}
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPasswordDialogOpen(false)}>取消</Button>
          <Button onClick={handleConfirmSwitch} variant="contained">
            确认切换
          </Button>
        </DialogActions>
      </Dialog>
    </>
  )
}
