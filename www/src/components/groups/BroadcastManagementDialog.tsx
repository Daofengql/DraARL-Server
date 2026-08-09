import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControl,
  FormControlLabel,
  IconButton,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  Switch,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TablePagination,
  TableRow,
  Tabs,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import AudioFile from '@mui/icons-material/AudioFile'
import Delete from '@mui/icons-material/Delete'
import Edit from '@mui/icons-material/Edit'
import PlayArrow from '@mui/icons-material/PlayArrow'
import Refresh from '@mui/icons-material/Refresh'
import Stop from '@mui/icons-material/Stop'
import Upload from '@mui/icons-material/Upload'
import {
  broadcastService,
  type BroadcastAudio,
  type BroadcastRun,
  type BroadcastRunStatus,
  type BroadcastSchedule,
  type BroadcastScheduleInput,
  type BroadcastScheduleType,
} from '../../services/broadcast'
import type { Group } from '../../types'
import { getErrorMessage } from '../../utils/errorMessage'
import { ConfirmDialog } from '../common/ConfirmDialog'

interface BroadcastManagementDialogProps {
  open: boolean
  group: Group | null
  onClose: () => void
}

interface ScheduleFormState {
  audioId: string
  name: string
  scheduleType: BroadcastScheduleType
  timezone: string
  scheduledAt: string
  localTime: string
  weekdayMask: number
  enabled: boolean
}

const WEEKDAYS = [
  { label: '周一', value: 1 },
  { label: '周二', value: 2 },
  { label: '周三', value: 3 },
  { label: '周四', value: 4 },
  { label: '周五', value: 5 },
  { label: '周六', value: 6 },
  { label: '周日', value: 0 },
]

const TIMEZONES = Array.from(new Set([
  Intl.DateTimeFormat().resolvedOptions().timeZone,
  'Asia/Shanghai',
  'UTC',
  'Asia/Hong_Kong',
  'Asia/Tokyo',
  'Europe/London',
  'America/New_York',
  'America/Los_Angeles',
])).filter(Boolean)

const RUN_STATUS: Record<BroadcastRunStatus, { label: string; color: 'default' | 'success' | 'warning' | 'error' | 'info' }> = {
  claimed: { label: '等待播放', color: 'info' },
  playing: { label: '播放中', color: 'info' },
  succeeded: { label: '成功', color: 'success' },
  skipped_recent_voice: { label: '最近有语音', color: 'warning' },
  skipped_domain_busy: { label: '话权占用', color: 'warning' },
  skipped_interconnected: { label: '互联策略阻止', color: 'warning' },
  skipped_no_receiver: { label: '无接收者', color: 'warning' },
  skipped_site_disabled: { label: '站点已关闭', color: 'warning' },
  cancelled: { label: '手动停止', color: 'default' },
  cancelled_site_disabled: { label: '站点关闭取消', color: 'default' },
  cancelled_interconnect_enabled: { label: '开启互联取消', color: 'default' },
  failed: { label: '系统失败', color: 'error' },
}

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / 1024 / 1024).toFixed(1)} MiB`
}

function formatDuration(value: number): string {
  return `${(Math.max(value, 0) / 1000).toFixed(1)} 秒`
}

function formatDateTime(value?: string): string {
  if (!value) return '-'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}

function toLocalDateTimeInput(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function initialScheduleForm(): ScheduleFormState {
  return {
    audioId: '',
    name: '',
    scheduleType: 'daily',
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai',
    scheduledAt: '',
    localTime: '08:00',
    weekdayMask: 0,
    enabled: true,
  }
}

function scheduleDescription(schedule: BroadcastSchedule): string {
  if (schedule.schedule_type === 'once') return `一次 · ${formatDateTime(schedule.scheduled_at)}`
  if (schedule.schedule_type === 'daily') return `每天 ${schedule.local_time?.slice(0, 5) || '-'}`
  const days = WEEKDAYS.filter(day => (schedule.weekday_mask || 0) & (1 << day.value)).map(day => day.label).join('、')
  return `${days || '未选择星期'} ${schedule.local_time?.slice(0, 5) || '-'}`
}

export function BroadcastManagementDialog({ open, group, onClose }: BroadcastManagementDialogProps) {
  const [tab, setTab] = useState(0)
  const [audios, setAudios] = useState<BroadcastAudio[]>([])
  const [schedules, setSchedules] = useState<BroadcastSchedule[]>([])
  const [runs, setRuns] = useState<BroadcastRun[]>([])
  const [runTotal, setRunTotal] = useState(0)
  const [runPage, setRunPage] = useState(0)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [uploadName, setUploadName] = useState('')
  const [preview, setPreview] = useState<BroadcastAudio | null>(null)
  const [scheduleDialogOpen, setScheduleDialogOpen] = useState(false)
  const [editingSchedule, setEditingSchedule] = useState<BroadcastSchedule | null>(null)
  const [scheduleForm, setScheduleForm] = useState<ScheduleFormState>(initialScheduleForm)
  const [confirm, setConfirm] = useState<{ open: boolean; title: string; message: string; action: () => void }>({
    open: false,
    title: '',
    message: '',
    action: () => undefined,
  })
  const fileInput = useRef<HTMLInputElement>(null)

  const readyAudios = useMemo(() => audios.filter(audio => audio.status === 'ready'), [audios])
  const audioNames = useMemo(() => new Map(audios.map(audio => [audio.id, audio.name])), [audios])

  const loadAll = async (quiet = false) => {
    if (!group) return
    if (!quiet) setLoading(true)
    try {
      const [audioItems, scheduleItems, runResult] = await Promise.all([
        broadcastService.listAudios(group.id),
        broadcastService.listSchedules(group.id),
        broadcastService.listRuns(group.id, runPage + 1, 20),
      ])
      setAudios(audioItems)
      setSchedules(scheduleItems)
      setRuns(runResult.items)
      setRunTotal(runResult.total)
      setError('')
    } catch (err) {
      if (!quiet) setError(getErrorMessage(err, '读取自动播报数据失败'))
    } finally {
      if (!quiet) setLoading(false)
    }
  }

  useEffect(() => {
    if (!open || !group) return
    setTab(0)
    setRunPage(0)
    setError('')
    setSuccess('')
    setPreview(null)
    void loadAll()
  }, [open, group?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!open || !group) return
    void loadAll(true)
  }, [runPage]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!open || !group) return
    const needsRefresh = audios.some(audio => audio.status === 'processing') || runs.some(run => run.status === 'claimed' || run.status === 'playing')
    if (!needsRefresh) return
    const timer = window.setInterval(() => void loadAll(true), 2000)
    return () => window.clearInterval(timer)
  }, [open, group?.id, audios, runs]) // eslint-disable-line react-hooks/exhaustive-deps

  const showSuccess = (message: string) => {
    setSuccess(message)
    window.setTimeout(() => setSuccess(''), 3000)
  }

  const uploadAudio = async (file?: File) => {
    if (!group || !file) return
    setBusy(true)
    try {
      await broadcastService.uploadAudio(group.id, file, uploadName)
      setUploadName('')
      if (fileInput.current) fileInput.current.value = ''
      showSuccess('音频已进入处理队列')
      await loadAll(true)
    } catch (err) {
      setError(getErrorMessage(err, '上传播报音频失败'))
    } finally {
      setBusy(false)
    }
  }

  const openPreview = async (audio: BroadcastAudio) => {
    if (!group) return
    try {
      setPreview(await broadcastService.getAudio(group.id, audio.id))
    } catch (err) {
      setError(getErrorMessage(err, '生成试听地址失败'))
    }
  }

  const openScheduleEditor = (schedule?: BroadcastSchedule) => {
    setEditingSchedule(schedule || null)
    if (schedule) {
      setScheduleForm({
        audioId: String(schedule.audio_id),
        name: schedule.name,
        scheduleType: schedule.schedule_type,
        timezone: schedule.timezone,
        scheduledAt: toLocalDateTimeInput(schedule.scheduled_at),
        localTime: schedule.local_time?.slice(0, 5) || '08:00',
        weekdayMask: schedule.weekday_mask || 0,
        enabled: schedule.enabled,
      })
    } else {
      setScheduleForm(initialScheduleForm())
    }
    setScheduleDialogOpen(true)
  }

  const saveSchedule = async () => {
    if (!group) return
    const audioId = Number(scheduleForm.audioId)
    if (!audioId || !scheduleForm.name.trim()) {
      setError('请选择音频并填写计划名称')
      return
    }
    if (scheduleForm.scheduleType === 'once' && !scheduleForm.scheduledAt) {
      setError('请选择单次播报时间')
      return
    }
    if (scheduleForm.scheduleType === 'weekly' && scheduleForm.weekdayMask === 0) {
      setError('请至少选择一个星期')
      return
    }
    const input: BroadcastScheduleInput = {
      audio_id: audioId,
      name: scheduleForm.name.trim(),
      schedule_type: scheduleForm.scheduleType,
      timezone: scheduleForm.timezone,
      enabled: scheduleForm.enabled,
    }
    if (scheduleForm.scheduleType === 'once') input.scheduled_at = new Date(scheduleForm.scheduledAt).toISOString()
    if (scheduleForm.scheduleType !== 'once') input.local_time = `${scheduleForm.localTime}:00`
    if (scheduleForm.scheduleType === 'weekly') input.weekday_mask = scheduleForm.weekdayMask

    setBusy(true)
    try {
      if (editingSchedule) await broadcastService.updateSchedule(group.id, editingSchedule.id, input)
      else await broadcastService.createSchedule(group.id, input)
      setScheduleDialogOpen(false)
      showSuccess(editingSchedule ? '计划已更新' : '计划已创建')
      await loadAll(true)
    } catch (err) {
      setError(getErrorMessage(err, '保存播报计划失败'))
    } finally {
      setBusy(false)
    }
  }

  const perform = async (action: () => Promise<void>, successMessage: string) => {
    setBusy(true)
    try {
      await action()
      showSuccess(successMessage)
      await loadAll(true)
    } catch (err) {
      setError(getErrorMessage(err, successMessage))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Dialog open={open} onClose={busy ? undefined : onClose} maxWidth="lg" fullWidth>
        <DialogTitle>
          <Stack direction="row" alignItems="center" spacing={1}>
            <AudioFile color="primary" />
            <Box sx={{ minWidth: 0 }}>
              <Typography variant="h6" noWrap>自动播报 · {group?.name}</Typography>
              <Typography variant="caption" color="text.secondary">实体组 ID {group?.id}</Typography>
            </Box>
          </Stack>
        </DialogTitle>
        <Divider />
        <Tabs value={tab} onChange={(_, value) => setTab(value)} sx={{ px: 2 }}>
          <Tab label={`音频库 (${audios.length})`} />
          <Tab label={`计划 (${schedules.length})`} />
          <Tab label={`执行历史 (${runTotal})`} />
        </Tabs>
        <Divider />
        <DialogContent sx={{ minHeight: 460, p: 2 }}>
          {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>{error}</Alert>}
          {success && <Alert severity="success" sx={{ mb: 2 }} onClose={() => setSuccess('')}>{success}</Alert>}

          {tab === 0 && (
            <Stack spacing={2}>
              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems={{ sm: 'center' }}>
                <TextField
                  size="small"
                  label="音频名称"
                  value={uploadName}
                  onChange={event => setUploadName(event.target.value)}
                  sx={{ minWidth: 240 }}
                />
                <Button component="label" variant="contained" startIcon={<Upload />} disabled={busy}>
                  上传音频
                  <input
                    ref={fileInput}
                    hidden
                    type="file"
                    accept="audio/wav,audio/x-wav,audio/mpeg,audio/ogg,audio/mp4,audio/x-m4a,.wav,.mp3,.ogg,.m4a"
                    onChange={event => void uploadAudio(event.target.files?.[0])}
                  />
                </Button>
                <Tooltip title="刷新">
                  <IconButton onClick={() => void loadAll()} disabled={loading}><Refresh /></IconButton>
                </Tooltip>
              </Stack>
              <TableContainer sx={{ maxHeight: 330 }}>
                <Table stickyHeader size="small" sx={{ minWidth: 820 }}>
                  <TableHead><TableRow>
                    <TableCell>名称</TableCell><TableCell>状态</TableCell><TableCell>时长</TableCell>
                    <TableCell>大小</TableCell><TableCell>引用计划</TableCell><TableCell>处理信息</TableCell><TableCell align="right">操作</TableCell>
                  </TableRow></TableHead>
                  <TableBody>
                    {audios.length === 0 ? <TableRow><TableCell colSpan={7} align="center">暂无音频</TableCell></TableRow> : audios.map(audio => (
                      <TableRow key={audio.id} hover>
                        <TableCell>{audio.name}</TableCell>
                        <TableCell><Chip size="small" label={audio.status === 'ready' ? '可播放' : audio.status === 'processing' ? '处理中' : '处理失败'} color={audio.status === 'ready' ? 'success' : audio.status === 'failed' ? 'error' : 'info'} /></TableCell>
                        <TableCell>{audio.status === 'ready' ? formatDuration(audio.duration_ms) : '-'}</TableCell>
                        <TableCell>{formatBytes(audio.original_size)}</TableCell>
                        <TableCell>{audio.schedule_count}</TableCell>
                        <TableCell><Typography variant="body2" color={audio.status === 'failed' ? 'error' : 'text.secondary'} noWrap sx={{ maxWidth: 220 }}>{audio.error_message || '-'}</Typography></TableCell>
                        <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                          <Tooltip title="试听"><span><IconButton size="small" disabled={audio.status !== 'ready'} onClick={() => void openPreview(audio)}><PlayArrow /></IconButton></span></Tooltip>
                          <Tooltip title={audio.schedule_count ? '仍有计划引用' : '删除'}><span><IconButton size="small" color="error" disabled={audio.schedule_count > 0} onClick={() => setConfirm({ open: true, title: '删除音频', message: `确定删除“${audio.name}”吗？`, action: () => void perform(() => broadcastService.deleteAudio(group!.id, audio.id), '音频已删除') })}><Delete /></IconButton></span></Tooltip>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
              {preview?.preview_url && (
                <Box component="audio" controls autoPlay src={preview.preview_url} sx={{ width: '100%', height: 40 }} />
              )}
            </Stack>
          )}

          {tab === 1 && (
            <Stack spacing={2}>
              <Alert severity="info">
                当前活动通信域连续静默 5 秒才播放；最近有语音则本轮跳过。互联开启时按虚拟组信标策略运行或挂起，播放开始后占用话权直到结束或管理员停止。
              </Alert>
              <Stack direction="row" justifyContent="space-between" alignItems="center">
                <Typography variant="subtitle2">每个计划独立选择音频和触发时刻</Typography>
                <Button variant="contained" size="small" startIcon={<Add />} disabled={readyAudios.length === 0} onClick={() => openScheduleEditor()}>新建计划</Button>
              </Stack>
              <TableContainer sx={{ maxHeight: 315 }}>
                <Table stickyHeader size="small" sx={{ minWidth: 900 }}>
                  <TableHead><TableRow>
                    <TableCell>计划</TableCell><TableCell>音频</TableCell><TableCell>周期</TableCell><TableCell>下次运行</TableCell><TableCell>状态</TableCell><TableCell align="right">操作</TableCell>
                  </TableRow></TableHead>
                  <TableBody>
                    {schedules.length === 0 ? <TableRow><TableCell colSpan={6} align="center">暂无计划</TableCell></TableRow> : schedules.map(schedule => (
                      <TableRow key={schedule.id} hover>
                        <TableCell>{schedule.name}</TableCell>
                        <TableCell>{audioNames.get(schedule.audio_id) || `音频 #${schedule.audio_id}`}</TableCell>
                        <TableCell><Typography variant="body2">{scheduleDescription(schedule)}</Typography><Typography variant="caption" color="text.secondary">{schedule.timezone}</Typography></TableCell>
                        <TableCell>{formatDateTime(schedule.next_run_at)}</TableCell>
                        <TableCell>
                          <Chip size="small" label={!schedule.enabled ? '已停用' : schedule.suspended_reason ? '互联挂起' : schedule.effective_enabled ? '运行中' : '待安排'} color={!schedule.enabled ? 'default' : schedule.suspended_reason ? 'warning' : 'success'} />
                        </TableCell>
                        <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                          <Tooltip title="立即播放"><span><IconButton size="small" disabled={!schedule.enabled || Boolean(schedule.suspended_reason)} onClick={() => void perform(() => broadcastService.runSchedule(group!.id, schedule.id).then(() => undefined), '播报已触发')}><PlayArrow /></IconButton></span></Tooltip>
                          <Tooltip title="编辑"><IconButton size="small" onClick={() => openScheduleEditor(schedule)}><Edit /></IconButton></Tooltip>
                          <Tooltip title="删除"><IconButton size="small" color="error" onClick={() => setConfirm({ open: true, title: '删除计划', message: `确定删除“${schedule.name}”吗？历史执行记录会保留。`, action: () => void perform(() => broadcastService.deleteSchedule(group!.id, schedule.id), '计划已删除') })}><Delete /></IconButton></Tooltip>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </Stack>
          )}

          {tab === 2 && (
            <Stack spacing={1}>
              <TableContainer sx={{ maxHeight: 365 }}>
                <Table stickyHeader size="small" sx={{ minWidth: 940 }}>
                  <TableHead><TableRow>
                    <TableCell>计划 / 音频</TableCell><TableCell>计划时刻</TableCell><TableCell>开始</TableCell><TableCell>状态</TableCell><TableCell>已播放</TableCell><TableCell>包</TableCell><TableCell align="right">操作</TableCell>
                  </TableRow></TableHead>
                  <TableBody>
                    {runs.length === 0 ? <TableRow><TableCell colSpan={7} align="center">暂无执行记录</TableCell></TableRow> : runs.map(run => {
                      const status = RUN_STATUS[run.status] || { label: run.status, color: 'default' as const }
                      const schedule = schedules.find(item => item.id === run.schedule_id)
                      return (
                        <TableRow key={run.id} hover>
                          <TableCell><Typography variant="body2">{schedule?.name || `计划 #${run.schedule_id}`}</Typography><Typography variant="caption" color="text.secondary">{audioNames.get(run.audio_id) || `音频 #${run.audio_id}`}</Typography></TableCell>
                          <TableCell>{formatDateTime(run.scheduled_for)}</TableCell>
                          <TableCell>{formatDateTime(run.started_at)}</TableCell>
                          <TableCell><Tooltip title={run.error_message || ''}><Chip size="small" label={status.label} color={status.color} /></Tooltip></TableCell>
                          <TableCell>{formatDuration(run.played_duration_ms)}</TableCell>
                          <TableCell>{run.sent_packets} / 丢 {run.dropped_packets}</TableCell>
                          <TableCell align="right"><Tooltip title="停止"><span><IconButton size="small" color="error" disabled={run.status !== 'claimed' && run.status !== 'playing'} onClick={() => void perform(() => broadcastService.cancelRun(group!.id, run.id), '停止请求已提交')}><Stop /></IconButton></span></Tooltip></TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              </TableContainer>
              <TablePagination component="div" count={runTotal} page={runPage} rowsPerPage={20} rowsPerPageOptions={[20]} onPageChange={(_, value) => setRunPage(value)} />
            </Stack>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose} disabled={busy}>关闭</Button>
        </DialogActions>
      </Dialog>

      <Dialog open={scheduleDialogOpen} onClose={busy ? undefined : () => setScheduleDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{editingSchedule ? '编辑播报计划' : '新建播报计划'}</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField label="计划名称" value={scheduleForm.name} onChange={event => setScheduleForm(current => ({ ...current, name: event.target.value }))} fullWidth required />
            <FormControl fullWidth required>
              <InputLabel>播放音频</InputLabel>
              <Select label="播放音频" value={scheduleForm.audioId} onChange={event => setScheduleForm(current => ({ ...current, audioId: String(event.target.value) }))}>
                {readyAudios.map(audio => <MenuItem key={audio.id} value={String(audio.id)}>{audio.name} · {formatDuration(audio.duration_ms)}</MenuItem>)}
              </Select>
            </FormControl>
            <FormControl fullWidth>
              <InputLabel>重复方式</InputLabel>
              <Select label="重复方式" value={scheduleForm.scheduleType} onChange={event => setScheduleForm(current => ({ ...current, scheduleType: event.target.value as BroadcastScheduleType }))}>
                <MenuItem value="once">一次</MenuItem><MenuItem value="daily">每天</MenuItem><MenuItem value="weekly">每周</MenuItem>
              </Select>
            </FormControl>
            <FormControl fullWidth>
              <InputLabel>时区</InputLabel>
              <Select label="时区" value={scheduleForm.timezone} onChange={event => setScheduleForm(current => ({ ...current, timezone: String(event.target.value) }))}>
                {TIMEZONES.map(timezone => <MenuItem key={timezone} value={timezone}>{timezone}</MenuItem>)}
              </Select>
            </FormControl>
            {scheduleForm.scheduleType === 'once' ? (
              <TextField label="播放时刻" type="datetime-local" value={scheduleForm.scheduledAt} onChange={event => setScheduleForm(current => ({ ...current, scheduledAt: event.target.value }))} slotProps={{ inputLabel: { shrink: true } }} fullWidth required />
            ) : (
              <TextField label="播放时间" type="time" value={scheduleForm.localTime} onChange={event => setScheduleForm(current => ({ ...current, localTime: event.target.value }))} slotProps={{ inputLabel: { shrink: true }, htmlInput: { step: 60 } }} fullWidth required />
            )}
            {scheduleForm.scheduleType === 'weekly' && (
              <Box>
                <Typography variant="caption" color="text.secondary">星期</Typography>
                <Stack direction="row" flexWrap="wrap" gap={0.75} sx={{ mt: 0.5 }}>
                  {WEEKDAYS.map(day => {
                    const checked = Boolean(scheduleForm.weekdayMask & (1 << day.value))
                    return <Chip key={day.value} label={day.label} color={checked ? 'primary' : 'default'} variant={checked ? 'filled' : 'outlined'} onClick={() => setScheduleForm(current => ({ ...current, weekdayMask: current.weekdayMask ^ (1 << day.value) }))} />
                  })}
                </Stack>
              </Box>
            )}
            <FormControlLabel control={<Switch checked={scheduleForm.enabled} onChange={event => setScheduleForm(current => ({ ...current, enabled: event.target.checked }))} />} label="启用计划" />
          </Stack>
        </DialogContent>
        <DialogActions><Button onClick={() => setScheduleDialogOpen(false)} disabled={busy}>取消</Button><Button variant="contained" onClick={() => void saveSchedule()} disabled={busy}>保存</Button></DialogActions>
      </Dialog>

      <ConfirmDialog
        isOpen={confirm.open}
        title={confirm.title}
        message={confirm.message}
        type="warning"
        onConfirm={() => { confirm.action(); setConfirm(current => ({ ...current, open: false })) }}
        onCancel={() => setConfirm(current => ({ ...current, open: false }))}
      />
    </>
  )
}
