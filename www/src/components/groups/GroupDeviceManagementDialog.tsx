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
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import Close from '@mui/icons-material/Close'
import PauseCircleOutline from '@mui/icons-material/PauseCircleOutline'
import PersonOff from '@mui/icons-material/PersonOff'
import Refresh from '@mui/icons-material/Refresh'
import Restore from '@mui/icons-material/Restore'
import { groupService } from '../../services/group'
import type { Device, Group } from '../../types'
import { getErrorMessage } from '../../utils/errorMessage'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { OnlineIndicator } from '../common/OnlineIndicator'

interface GroupDeviceManagementDialogProps {
  open: boolean
  group: Group | null
  onClose: () => void
  onChanged?: () => void
  allowKick?: boolean
}

export function GroupDeviceManagementDialog({
  open,
  group,
  onClose,
  onChanged,
  allowKick = true,
}: GroupDeviceManagementDialogProps) {
  const [devices, setDevices] = useState<Device[]>([])
  const [loading, setLoading] = useState(false)
  const [savingDeviceId, setSavingDeviceId] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [reason, setReason] = useState('')
  const [deviceToKick, setDeviceToKick] = useState<Device | null>(null)
  const busy = savingDeviceId !== null

  const loadDevices = useCallback(async () => {
    if (!group) return
    setLoading(true)
    setError('')
    try {
      setDevices(await groupService.getDevices(group.id))
    } catch (loadError) {
      setError(getErrorMessage(loadError, '加载群组设备失败'))
    } finally {
      setLoading(false)
    }
  }, [group])

  useEffect(() => {
    if (!open) return
    setReason('')
    void loadDevices()
  }, [open, loadDevices])

  const updateCommControl = async (
    device: Device,
    changes: { disable_send?: boolean; disable_recv?: boolean },
  ) => {
    if (!group) return
    setSavingDeviceId(device.id)
    setError('')
    try {
      const updated = await groupService.updateDeviceCommControl(group.id, device.id, {
        ...changes,
        reason: reason.trim() || undefined,
      })
      setDevices((current) => current.map((item) => (
        item.id === device.id
          ? { ...item, disable_send: updated.disable_send, disable_recv: updated.disable_recv }
          : item
      )))
      onChanged?.()
    } catch (updateError) {
      setError(getErrorMessage(updateError, '更新设备收发状态失败'))
      await loadDevices()
    } finally {
      setSavingDeviceId(null)
    }
  }

  const kickDevice = async () => {
    if (!group || !deviceToKick) return
    const device = deviceToKick
    setDeviceToKick(null)
    setSavingDeviceId(device.id)
    setError('')
    try {
      await groupService.kickDevice(group.id, device.id)
      setDevices((current) => current.filter((item) => item.id !== device.id))
      onChanged?.()
    } catch (kickError) {
      setError(getErrorMessage(kickError, '踢出设备失败'))
    } finally {
      setSavingDeviceId(null)
    }
  }

  return (
    <>
      <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
        <DialogTitle sx={{ pr: 7 }}>
          <Typography component="span" variant="h6">设备管理</Typography>
          <Typography component="span" variant="body2" color="text.secondary" sx={{ ml: 1 }}>
            {group ? `${group.name} · ${devices.length} 台` : ''}
          </Typography>
          <Tooltip title="关闭">
            <IconButton onClick={onClose} sx={{ position: 'absolute', right: 12, top: 10 }}>
              <Close />
            </IconButton>
          </Tooltip>
        </DialogTitle>

        <DialogContent dividers sx={{ p: 0 }}>
          <Box sx={{ px: 2, py: 1.5, borderBottom: 1, borderColor: 'divider' }}>
            <Alert severity="info" sx={{ mb: 1.5 }}>
              收发控制作用于设备本身，切换群组后仍然生效；设备所有者排除干扰后可以自行恢复。
            </Alert>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems={{ sm: 'center' }}>
              <TextField
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                label="操作原因（可选）"
                placeholder="例如：疑似持续发射"
                size="small"
                inputProps={{ maxLength: 500 }}
                fullWidth
              />
              <Tooltip title="刷新设备列表">
                <span>
                  <IconButton onClick={() => void loadDevices()} disabled={loading || busy}>
                    <Refresh />
                  </IconButton>
                </span>
              </Tooltip>
            </Stack>
          </Box>

          {error && <Alert severity="error" onClose={() => setError('')} sx={{ m: 2 }}>{error}</Alert>}

          <TableContainer sx={{ maxHeight: 'min(56vh, 560px)' }}>
            <Table stickyHeader size="small" sx={{ minWidth: 720 }}>
              <TableHead>
                <TableRow>
                  <TableCell>设备</TableCell>
                  <TableCell width={150}>呼号-SSID</TableCell>
                  <TableCell width={100}>状态</TableCell>
                  <TableCell align="center" width={110}>禁止发送</TableCell>
                  <TableCell align="center" width={110}>禁止接收</TableCell>
                  <TableCell align="right" width={150}>操作</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow><TableCell colSpan={6} align="center" sx={{ py: 5 }}>加载中...</TableCell></TableRow>
                ) : devices.length === 0 ? (
                  <TableRow><TableCell colSpan={6} align="center" sx={{ py: 5 }}>暂无设备</TableCell></TableRow>
                ) : devices.map((device) => {
                  return (
                    <TableRow key={device.id} hover>
                      <TableCell>
                        <Typography variant="body2" fontWeight={500}>{device.name || `设备 ${device.id}`}</Typography>
                        <Typography variant="caption" color="text.secondary">型号 {device.dev_model ?? device.model ?? 0}</Typography>
                      </TableCell>
                      <TableCell>{device.callsign}-{device.ssid}</TableCell>
                      <TableCell>
                        <Stack direction="row" spacing={0.75} alignItems="center">
                          <OnlineIndicator online={Boolean(device.is_online || device.online)} />
                          <Typography variant="body2">{device.is_online || device.online ? '在线' : '离线'}</Typography>
                        </Stack>
                      </TableCell>
                      <TableCell align="center">
                        <Switch
                          checked={Boolean(device.disable_send)}
                          disabled={busy}
                          onChange={(_, checked) => void updateCommControl(device, { disable_send: checked })}
                          inputProps={{ 'aria-label': `${device.callsign}-${device.ssid} 禁止发送` }}
                        />
                      </TableCell>
                      <TableCell align="center">
                        <Switch
                          checked={Boolean(device.disable_recv)}
                          disabled={busy}
                          onChange={(_, checked) => void updateCommControl(device, { disable_recv: checked })}
                          inputProps={{ 'aria-label': `${device.callsign}-${device.ssid} 禁止接收` }}
                        />
                      </TableCell>
                      <TableCell align="right">
                        <Tooltip title="暂停双向通信">
                          <span>
                            <IconButton
                              size="small"
                              disabled={busy || (Boolean(device.disable_send) && Boolean(device.disable_recv))}
                              onClick={() => void updateCommControl(device, { disable_send: true, disable_recv: true })}
                            >
                              <PauseCircleOutline fontSize="small" />
                            </IconButton>
                          </span>
                        </Tooltip>
                        <Tooltip title="恢复正常通信">
                          <span>
                            <IconButton
                              size="small"
                              color="success"
                              disabled={busy || (!device.disable_send && !device.disable_recv)}
                              onClick={() => void updateCommControl(device, { disable_send: false, disable_recv: false })}
                            >
                              <Restore fontSize="small" />
                            </IconButton>
                          </span>
                        </Tooltip>
                        {allowKick && (
                          <Tooltip title="踢出群组">
                            <span>
                              <IconButton size="small" color="error" disabled={busy} onClick={() => setDeviceToKick(device)}>
                                <PersonOff fontSize="small" />
                              </IconButton>
                            </span>
                          </Tooltip>
                        )}
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
        isOpen={Boolean(deviceToKick)}
        title="踢出设备"
        message={`确定要将设备“${deviceToKick?.name || ''}”移出群组吗？`}
        confirmText="踢出"
        type="danger"
        onConfirm={() => void kickDevice()}
        onCancel={() => setDeviceToKick(null)}
      />
    </>
  )
}
