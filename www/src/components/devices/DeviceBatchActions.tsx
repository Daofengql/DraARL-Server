import { useRef, useState } from 'react'
import { Alert, Button, Checkbox, FormControlLabel, Paper, Stack, Typography } from '@mui/material'
import type { Device, Group } from '../../types'
import { deviceService } from '../../services/device'
import { getErrorMessage } from '../../utils/errorMessage'
import { runBatch } from '../../utils/batch'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { GroupPickerDialog } from '../groups/GroupPicker'

interface Props {
  devices: Device[]
  visible: Device[]
  groups: Group[]
  selected: number[]
  onSelection: (ids: number[]) => void
  onBusy: (busy: boolean) => void
  onComplete: () => Promise<void>
}

export function DeviceBatchActions({ devices, visible, groups, selected, onSelection, onBusy, onComplete }: Props) {
  const running = useRef(false)
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [chooseGroup, setChooseGroup] = useState(false)
  const [message, setMessage] = useState('')
  const [failed, setFailed] = useState(false)
  const allVisible = visible.length > 0 && visible.every((device) => selected.includes(device.id))

  const execute = async (action: (device: Device) => Promise<unknown>) => {
    if (running.current) return
    running.current = true
    setBusy(true); onBusy(true); setMessage('')
    try {
      const targets = devices.filter((device) => selected.includes(device.id))
      const result = await runBatch(targets, action)
      setFailed(result.failed.length > 0)
      setMessage(`已完成 ${result.succeeded.length} 台设备${result.failed.length ? `，${result.failed.length} 台失败并保留选中：${getErrorMessage(result.failed[0].error, '请重试')}` : ''}`)
      onSelection(result.failed.map(({ item }) => item.id))
      await onComplete()
    } catch (error) {
      setFailed(true); setMessage(getErrorMessage(error, '刷新设备列表失败，请手动刷新'))
    } finally { running.current = false; setBusy(false); onBusy(false) }
  }

  return <Paper variant="outlined" sx={{ p: 1.5, mb: 2 }} aria-busy={busy}>
    <Stack direction="row" gap={1} alignItems="center" flexWrap="wrap" useFlexGap>
      <FormControlLabel label="选择本页" control={<Checkbox checked={allVisible} indeterminate={!allVisible && visible.some((device) => selected.includes(device.id))} disabled={busy || !visible.length} onChange={(_, checked) => onSelection(checked ? [...new Set([...selected, ...visible.map((device) => device.id)])] : selected.filter((id) => !visible.some((device) => device.id === id)))} />} />
      <Typography variant="body2" role="status">已选 {selected.length} 台</Typography>
      <Button size="small" disabled={busy || !selected.length} onClick={() => onSelection([])}>清除选择</Button>
      <Button size="small" disabled={busy || !selected.length} onClick={() => setChooseGroup(true)}>批量切组</Button>
      <Button size="small" disabled={busy || !selected.length} onClick={() => void execute((device) => deviceService.update(device.id, { status: 2 }))}>批量禁用</Button>
      <Button size="small" disabled={busy || !selected.length} onClick={() => void execute((device) => deviceService.update(device.id, { status: 1 }))}>批量启用</Button>
      <Button size="small" color="error" disabled={busy || !selected.length} onClick={() => setConfirmDelete(true)}>批量删除</Button>
    </Stack>
    {message && <Alert severity={failed ? 'warning' : 'success'} sx={{ mt: 1 }}>{message}</Alert>}
    <ConfirmDialog isOpen={confirmDelete} title="批量删除设备" message={`确定删除选中的 ${selected.length} 台设备吗？此操作无法撤销。`} type="danger" onCancel={() => setConfirmDelete(false)} onConfirm={() => { setConfirmDelete(false); void execute((device) => deviceService.delete(device.id)) }} />
    <GroupPickerDialog open={chooseGroup} groups={groups} title={`为 ${selected.length} 台设备选择群组`} onClose={() => setChooseGroup(false)} onSelect={(id, password) => { setChooseGroup(false); void execute((device) => deviceService.switchGroup(device.id, id, password)) }} />
  </Paper>
}
