import { useEffect, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  Radio,
  RadioGroup,
  Stack,
  Typography,
} from '@mui/material'
import Campaign from '@mui/icons-material/Campaign'
import { groupLinkService } from '../../services/groupLink'
import type { VirtualGroup } from '../../types'
import { getErrorMessage } from '../../utils/errorMessage'

interface VirtualGroupBroadcastPolicyDialogProps {
  open: boolean
  group: VirtualGroup | null
  onClose: () => void
  onSaved: (group: VirtualGroup) => void
}

export function broadcastPolicySummary(group: VirtualGroup): string {
  if (group.broadcast_policy?.mode === 'allow_single_source') {
    return `仅保留 ${group.allowed_source_name || `实体组 #${group.broadcast_policy.allowed_source_group_id}`}`
  }
  return '全部暂停'
}

export function VirtualGroupBroadcastPolicyDialog({ open, group, onClose, onSaved }: VirtualGroupBroadcastPolicyDialogProps) {
  const [detail, setDetail] = useState<VirtualGroup | null>(null)
  const [selection, setSelection] = useState('suspend_all')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const groupID = group?.id

  useEffect(() => {
    if (!open || !groupID) return
    setLoading(true)
    setError('')
    void groupLinkService.getVirtualGroup(groupID)
      .then(result => {
        setDetail(result)
        const source = result.broadcast_policy?.mode === 'allow_single_source'
          ? String(result.broadcast_policy.allowed_source_group_id || '')
          : 'suspend_all'
        setSelection(source || 'suspend_all')
      })
      .catch(err => setError(getErrorMessage(err, '读取虚拟组信标策略失败')))
      .finally(() => setLoading(false))
  }, [open, groupID])

  const save = async () => {
    if (!detail) return
    setSaving(true)
    setError('')
    try {
      await groupLinkService.updateBroadcastPolicy(detail.id, selection === 'suspend_all'
        ? { mode: 'suspend_all' }
        : { mode: 'allow_single_source', allowed_source_group_id: Number(selection) })
      const refreshed = await groupLinkService.getVirtualGroup(detail.id)
      setDetail(refreshed)
      onSaved(refreshed)
      onClose()
    } catch (err) {
      setError(getErrorMessage(err, '更新虚拟组信标策略失败'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onClose={saving ? undefined : onClose} maxWidth="sm" fullWidth>
      <DialogTitle>
        <Stack direction="row" alignItems="center" spacing={1}>
          <Campaign color="primary" />
          <span>修改信标策略 · {group?.name}</span>
        </Stack>
      </DialogTitle>
      <DialogContent>
        {loading ? (
          <Stack alignItems="center" sx={{ py: 5 }}><CircularProgress size={28} /></Stack>
        ) : (
          <Stack spacing={2} sx={{ mt: 0.5 }}>
            {error && <Alert severity="error" onClose={() => setError('')}>{error}</Alert>}
            <Alert severity={detail?.status === 1 ? 'warning' : 'info'}>
              {detail?.status === 1
                ? '互联当前开启，保存后立即重新裁决成员实体组的自动播报；不允许的当前播放会安全停止。'
                : '互联当前关闭，各实体组自动播报正常运行；本策略会在下次开启互联时直接应用。'}
            </Alert>
            <RadioGroup value={selection} onChange={event => setSelection(event.target.value)}>
              <FormControlLabel
                value="suspend_all"
                control={<Radio />}
                label={<Box><Typography>全部暂停</Typography><Typography variant="caption" color="text.secondary">互联开启期间不运行任何成员组信标</Typography></Box>}
                sx={{ alignItems: 'flex-start', mb: 1 }}
              />
              {(detail?.broadcast_members || []).map(member => {
                const selected = selection === String(member.group_id)
                const allowed = detail?.status === 1 && detail.broadcast_policy?.mode === 'allow_single_source' && detail.broadcast_policy.allowed_source_group_id === member.group_id
                return (
                  <FormControlLabel
                    key={member.group_id}
                    value={String(member.group_id)}
                    control={<Radio />}
                    sx={{ alignItems: 'flex-start', mb: 1 }}
                    label={
                      <Stack direction="row" alignItems="center" spacing={1} flexWrap="wrap">
                        <Box>
                          <Typography>{member.group_name}</Typography>
                          <Typography variant="caption" color="text.secondary">{member.enabled_count} 个启用计划</Typography>
                        </Box>
                        {detail?.status === 1 && <Chip size="small" label={allowed ? '允许运行' : '互联挂起'} color={allowed ? 'success' : 'warning'} />}
                        {selected && member.enabled_count === 0 && <Chip size="small" label="无运行计划" variant="outlined" />}
                      </Stack>
                    }
                  />
                )
              })}
            </RadioGroup>
            {selection !== 'suspend_all' && (
              <Alert severity="warning">
                只保留一个信标源，不会产生多信标冲突；播放期间仍会占用整个互联话权，且真人语音不能抢占。
              </Alert>
            )}
          </Stack>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={saving}>取消</Button>
        <Button variant="contained" onClick={() => void save()} disabled={loading || saving || !detail}>保存策略</Button>
      </DialogActions>
    </Dialog>
  )
}
