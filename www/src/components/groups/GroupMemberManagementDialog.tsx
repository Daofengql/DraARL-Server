import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material'
import Close from '@mui/icons-material/Close'
import PersonRemove from '@mui/icons-material/PersonRemove'
import Refresh from '@mui/icons-material/Refresh'
import { groupService } from '../../services/group'
import type { Group, GroupMember } from '../../types'
import { getErrorMessage } from '../../utils/errorMessage'
import { ConfirmDialog } from '../common/ConfirmDialog'

interface GroupMemberManagementDialogProps {
  open: boolean
  group: Group | null
  onClose: () => void
  onChanged?: () => void
}

function formatJoinTime(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

export function GroupMemberManagementDialog({
  open,
  group,
  onClose,
  onChanged,
}: GroupMemberManagementDialogProps) {
  const [members, setMembers] = useState<GroupMember[]>([])
  const [loading, setLoading] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [error, setError] = useState('')
  const [memberToRemove, setMemberToRemove] = useState<GroupMember | null>(null)

  const loadMembers = useCallback(async () => {
    if (!group) return
    setLoading(true)
    setError('')
    try {
      const result = await groupService.getMembers(group.id)
      setMembers(result.items)
    } catch (loadError) {
      setError(getErrorMessage(loadError, '加载群组成员失败'))
    } finally {
      setLoading(false)
    }
  }, [group])

  useEffect(() => {
    if (!open) return
    void loadMembers()
  }, [open, loadMembers])

  const removeMember = async () => {
    if (!group || !memberToRemove) return
    const member = memberToRemove
    setMemberToRemove(null)
    setRemoving(true)
    setError('')
    try {
      await groupService.removeMember(group.id, member.user_id)
      setMembers((current) => current.filter((item) => item.user_id !== member.user_id))
      onChanged?.()
    } catch (removeError) {
      setError(getErrorMessage(removeError, '移除群组成员失败'))
      await loadMembers()
    } finally {
      setRemoving(false)
    }
  }

  return (
    <>
      <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
        <DialogTitle sx={{ pr: 7 }}>
          <Typography component="span" variant="h6">成员管理</Typography>
          <Typography component="span" variant="body2" color="text.secondary" sx={{ ml: 1 }}>
            {group ? `${group.name} · ${members.length} 人` : ''}
          </Typography>
          <Tooltip title="关闭">
            <IconButton onClick={onClose} sx={{ position: 'absolute', right: 12, top: 10 }}>
              <Close />
            </IconButton>
          </Tooltip>
        </DialogTitle>

        <DialogContent dividers sx={{ p: 0 }}>
          <Box sx={{ px: 2, py: 1.5, borderBottom: 1, borderColor: 'divider' }}>
            <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
              <Tooltip title="刷新成员列表">
                <span>
                  <IconButton onClick={() => void loadMembers()} disabled={loading || removing}>
                    <Refresh />
                  </IconButton>
                </span>
              </Tooltip>
            </Box>
          </Box>

          {error && <Alert severity="error" onClose={() => setError('')} sx={{ m: 2 }}>{error}</Alert>}

          <TableContainer sx={{ maxHeight: 'min(56vh, 560px)' }}>
            <Table stickyHeader size="small" sx={{ minWidth: 680 }}>
              <TableHead>
                <TableRow>
                  <TableCell>成员</TableCell>
                  <TableCell width={150}>呼号</TableCell>
                  <TableCell width={110}>组内设备</TableCell>
                  <TableCell width={190}>加入时间</TableCell>
                  <TableCell align="right" width={90}>操作</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow><TableCell colSpan={5} align="center" sx={{ py: 5 }}>加载中...</TableCell></TableRow>
                ) : members.length === 0 ? (
                  <TableRow><TableCell colSpan={5} align="center" sx={{ py: 5 }}>暂无成员</TableCell></TableRow>
                ) : members.map((member) => {
                  const displayName = member.nickname || member.username || `用户 ${member.user_id}`
                  const isOwner = member.user_id === group?.ower_id
                  return (
                    <TableRow key={member.id} hover>
                      <TableCell>
                        <Stack spacing={0.25}>
                          <Typography variant="body2" fontWeight={500}>{displayName}</Typography>
                          {member.nickname && member.username && (
                            <Typography variant="caption" color="text.secondary">{member.username}</Typography>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>{member.callsign || '-'}</TableCell>
                      <TableCell>{member.device_count ?? 0}</TableCell>
                      <TableCell>{formatJoinTime(member.join_time)}</TableCell>
                      <TableCell align="right">
                        <Tooltip title={isOwner ? '不能移除群组创建者' : '移除成员'}>
                          <span>
                            <IconButton
                              size="small"
                              color="error"
                              disabled={removing || isOwner}
                              onClick={() => setMemberToRemove(member)}
                            >
                              <PersonRemove fontSize="small" />
                            </IconButton>
                          </span>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </TableContainer>
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>关闭</Button>
        </DialogActions>
      </Dialog>

      <ConfirmDialog
        isOpen={Boolean(memberToRemove)}
        title="移除成员"
        message={`确定要移除“${memberToRemove?.nickname || memberToRemove?.username || ''}”吗？该成员的组内普通设备将迁移到默认群组，幽灵客户端也会立即停止收发本群组。`}
        confirmText="移除"
        type="danger"
        onConfirm={() => void removeMember()}
        onCancel={() => setMemberToRemove(null)}
      />
    </>
  )
}
