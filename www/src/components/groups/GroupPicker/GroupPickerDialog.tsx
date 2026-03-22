import { useState } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  List,
  Typography,
  Box,
  TextField,
  Alert,
  Tabs,
  Tab,
  Stack,
} from '@mui/material'
import Search from '@mui/icons-material/Search'
import type { Group, Device } from '../../../types'
import { groupService } from '../../../services/group'
import { TabPanel } from '../../common/TabPanel'
import { GroupListItem, GROUP_TYPE_PUBLIC, GROUP_TYPE_PRIVATE } from './GroupListItem'

interface GroupPickerDialogProps {
  open: boolean
  onClose: () => void
  groups: Group[]
  currentGroupId?: number
  device?: Device
  showSearchTab?: boolean
  onSelect: (groupId: number, password?: string) => void
  title?: string
}

export function GroupPickerDialog({
  open,
  onClose,
  groups,
  currentGroupId,
  device,
  showSearchTab = true,
  onSelect,
  title = '选择群组',
}: GroupPickerDialogProps) {
  const [tabValue, setTabValue] = useState(0)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [searchResults, setSearchResults] = useState<Group[]>([])
  const [searchLoading, setSearchLoading] = useState(false)
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false)
  const [selectedGroup, setSelectedGroup] = useState<Group | null>(null)
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')

  // 当前群组
  const currentGroup = currentGroupId ? groups.find((g) => g.id === currentGroupId) : undefined

  // 分类群组
  const publicGroups = groups.filter((g) => g.type === GROUP_TYPE_PUBLIC)
  const joinedPrivateGroups = groups.filter(
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
      setSearchResults(result.items.filter((g) => g.id !== currentGroupId))
    } catch {
      setError('搜索群组失败')
    } finally {
      setSearchLoading(false)
    }
  }

  // 选择群组
  const handleSelectGroup = (group: Group) => {
    if (group.id === currentGroupId) return

    // 公开群组或已加入的私有群组直接切换
    if (group.type === GROUP_TYPE_PUBLIC || group.is_joined) {
      onSelect(group.id)
      onClose()
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
    onSelect(selectedGroup.id, password)
    setPasswordDialogOpen(false)
    setSelectedGroup(null)
    setPassword('')
    setError('')
    onClose()
  }

  // 重置状态
  const handleClose = () => {
    setTabValue(0)
    setSearchKeyword('')
    setSearchResults([])
    setError('')
    onClose()
  }

  return (
    <>
      <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
        <DialogTitle>{title}</DialogTitle>
        <DialogContent>
          {device && (
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
          )}

          <Tabs
            value={tabValue}
            onChange={(_, v) => setTabValue(v)}
            sx={{ borderBottom: 1, borderColor: 'divider' }}
          >
            <Tab label="已验证群组" />
            <Tab label="公开群组" />
            {showSearchTab && <Tab label="搜索群组" />}
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
                {joinedPrivateGroups.map((group) => (
                  <GroupListItem
                    key={group.id}
                    group={group}
                    isCurrent={group.id === currentGroupId}
                    isJoined={group.is_joined}
                    onClick={handleSelectGroup}
                  />
                ))}
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
                {publicGroups.map((group) => (
                  <GroupListItem
                    key={group.id}
                    group={group}
                    isCurrent={group.id === currentGroupId}
                    onClick={handleSelectGroup}
                  />
                ))}
              </List>
            )}
          </TabPanel>

          {/* 搜索群组 */}
          {showSearchTab && (
            <TabPanel value={tabValue} index={2}>
              <Stack spacing={2}>
                <Box sx={{ display: 'flex', gap: 1 }}>
                  <TextField
                    fullWidth
                    size="small"
                    placeholder="输入群组ID或名称搜索"
                    value={searchKeyword}
                    onChange={(e) => setSearchKeyword(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
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
                    {searchResults.map((group) => (
                      <GroupListItem
                        key={group.id}
                        group={group}
                        isCurrent={group.id === currentGroupId}
                        isJoined={group.is_joined}
                        onClick={handleSelectGroup}
                      />
                    ))}
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
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={handleClose}>取消</Button>
        </DialogActions>
      </Dialog>

      {/* 密码验证对话框 */}
      <Dialog
        open={passwordDialogOpen}
        onClose={() => setPasswordDialogOpen(false)}
        maxWidth="xs"
        fullWidth
      >
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
              onKeyDown={(e) => e.key === 'Enter' && handleConfirmSwitch()}
              autoFocus
              error={!!error}
              helperText={error}
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPasswordDialogOpen(false)}>取消</Button>
          <Button onClick={handleConfirmSwitch} variant="contained">
            确认
          </Button>
        </DialogActions>
      </Dialog>
    </>
  )
}
