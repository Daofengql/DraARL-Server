import { useCallback, useEffect, useState } from 'react'
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
  Alert,
  Switch,
  FormControlLabel,
  Chip,
  Checkbox,
  Toolbar,
  Tooltip,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import Search from '@mui/icons-material/Search'
import SettingsInputAntenna from '@mui/icons-material/SettingsInputAntenna'
import { relayService } from '../../services'
import type { Relay } from '../../types'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { RegionCascader } from '../../components/common/RegionCascader'
import { ToneSelector } from '../../components/devices/frequency/ToneSelector'
import {
  buildToneSelection,
  formatToneDisplay,
  toneSelectionToRelayValue,
  type ToneSelection,
} from '../../utils/radioConfig'
import { getErrorMessage } from '../../utils/errorMessage'

const initialFormData = {
  id: 0,
  name: '',
  up_freq: '',
  down_freq: '',
  send_ctcss: '',
  receive_ctcss: '',
  ower_callsign: '',
  location: '',
  note: '',
  status: 1,
}

export function RelaysPage() {
  const offTone: ToneSelection = { mode: 'off', value: '0' }
  const [relays, setRelays] = useState<Relay[]>([])
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [searchLocation, setSearchLocation] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRelay, setEditingRelay] = useState<Relay | null>(null)
  const [formData, setFormData] = useState(initialFormData)
  const [receiveTone, setReceiveTone] = useState<ToneSelection>(offTone)
  const [sendTone, setSendTone] = useState<ToneSelection>(offTone)
  const [selectedIds, setSelectedIds] = useState<number[]>([])

  const [statusSwitch, setStatusSwitch] = useState(true)
  const [validateForm, setValidateForm] = useState(false)

  // 确认对话框状态
  const [confirmDialog, setConfirmDialog] = useState<{
    open: boolean
    title: string
    message: string
    type: 'danger' | 'warning' | 'info'
    onConfirm: () => void
  }>({ open: false, title: '', message: '', type: 'info', onConfirm: () => {} })

  const loadRelays = useCallback(async (location?: string) => {
    setLoading(true)
    try {
      // 使用后端按地区搜索（管理员版本，不过滤状态）
      const data = await relayService.list(location)
      setRelays(data)
    } catch (err) {
      console.error('Failed to load relays:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadRelays()
  }, [loadRelays])

  // 当搜索条件变化时重新加载
  const handleSearch = () => {
    setPage(0)
    void loadRelays(searchLocation || undefined)
  }

  const handleClear = () => {
    setSearchKeyword('')
    setSearchLocation('')
    setPage(0)
    // 清除后重新加载全部数据
    relayService.list().then(setRelays).catch(console.error)
  }

  const handleOpenDialog = (relay?: Relay) => {
    if (relay) {
      setEditingRelay(relay)
      setReceiveTone(buildToneSelection({ legacy: relay.receive_ctcss }))
      setSendTone(buildToneSelection({ legacy: relay.send_ctcss }))
      setFormData({
        id: relay.id,
        name: relay.name,
        up_freq: relay.up_freq,
        down_freq: relay.down_freq,
        send_ctcss: relay.send_ctcss,
        receive_ctcss: relay.receive_ctcss,
        ower_callsign: relay.ower_callsign,
        location: relay.location || '',
        note: relay.note,
        status: relay.status,
      })
      setStatusSwitch(relay.status === 1)
    } else {
      setEditingRelay(null)
      setReceiveTone(offTone)
      setSendTone(offTone)
      setFormData(initialFormData)
      setStatusSwitch(true)
    }
    setDialogOpen(true)
    setError('')
    setValidateForm(false)
  }

  const handleCloseDialog = () => {
    setDialogOpen(false)
    setEditingRelay(null)
  }

  const handleSave = async () => {
    setValidateForm(true)
    // 验证必填项
    if (!formData.name.trim()) {
      setError('名称为必填项')
      return
    }
    if (!formData.down_freq.trim()) {
      setError('接收频率为必填项')
      return
    }
    if (!formData.up_freq.trim()) {
      setError('发射频率为必填项')
      return
    }
    const locationParts = formData.location.split(' ').filter(Boolean)
    if (locationParts.length < 2) {
      setError('位置为必填项，至少需要选择到市级别')
      return
    }

    try {
      const data = {
        ...formData,
        status: statusSwitch ? 1 : 0,
      }
      if (editingRelay) {
        await relayService.update(data)
      } else {
        await relayService.create(data)
      }
      handleCloseDialog()
      loadRelays()
    } catch (error) {
      setError(getErrorMessage(error, '操作失败'))
    }
  }

  const handleDelete = async (id: number) => {
    const relay = relays.find(r => r.id === id)
    setConfirmDialog({
      open: true,
      title: '删除中继台',
      message: `确定要删除中继台 "${relay?.name || id}" 吗？`,
      type: 'danger',
      onConfirm: async () => {
        try {
          await relayService.delete(id)
          loadRelays()
        } catch (error) {
          setError(getErrorMessage(error, '删除失败'))
        }
      },
    })
  }

  // 全选/取消全选当前页
  const handleSelectAll = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.checked) {
      setSelectedIds(paginatedRelays.map(r => r.id))
    } else {
      setSelectedIds([])
    }
  }

  // 选择/取消选择单个
  const handleSelect = (id: number) => {
    setSelectedIds(prev =>
      prev.includes(id) ? prev.filter(i => i !== id) : [...prev, id]
    )
  }

  // 批量删除
  const handleBatchDelete = () => {
    if (selectedIds.length === 0) return

    setConfirmDialog({
      open: true,
      title: '批量删除',
      message: `确定要删除选中的 ${selectedIds.length} 个中继台吗？此操作不可恢复。`,
      type: 'danger',
      onConfirm: async () => {
        try {
          await Promise.all(selectedIds.map(id => relayService.delete(id)))
          setSelectedIds([])
          loadRelays()
        } catch (error) {
          setError(getErrorMessage(error, '批量删除失败'))
        }
      },
    })
  }

  const filteredRelays = relays.filter((r) => {
    // 关键字过滤
    const matchKeyword = !searchKeyword ||
      r.name.toLowerCase().includes(searchKeyword.toLowerCase()) ||
      (r.ower_callsign && r.ower_callsign.toLowerCase().includes(searchKeyword.toLowerCase()))

    // 地区过滤（支持任意级别）
    const matchLocation = !searchLocation ||
      r.location.includes(searchLocation)

    return matchKeyword && matchLocation
  })

  const paginatedRelays = filteredRelays.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  )

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4">中继台管理</Typography>
        <Button variant="contained" startIcon={<Add />} onClick={() => handleOpenDialog()}>
          添加中继台
        </Button>
      </Box>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>
          {error}
        </Alert>
      )}

      <Paper sx={{ mb: 2 }}>
        <Box sx={{ display: 'flex', gap: 2, p: 2, alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <Box sx={{ minWidth: 250 }}>
            <RegionCascader
              value={searchLocation}
              onChange={setSearchLocation}
              label="按地区筛选"
              size="small"
              helperText="可选择任意级别地区"
            />
          </Box>
          <TextField
            placeholder="搜索中继台名称或所有者呼号"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            size="small"
            sx={{ flexGrow: 1, minWidth: 200 }}
          />
          <Button
            variant="outlined"
            startIcon={<Search />}
            onClick={handleSearch}
          >
            搜索
          </Button>
          {(searchKeyword || searchLocation) && (
            <Button
              variant="text"
              onClick={handleClear}
            >
              清除
            </Button>
          )}
        </Box>
      </Paper>

      <TableContainer component={Paper} sx={{ overflow: 'auto' }}>
        {selectedIds.length > 0 && (
          <Toolbar sx={{ bgcolor: 'action.selected', gap: 2 }}>
            <Typography sx={{ flex: 1 }} color="inherit">
              已选择 {selectedIds.length} 项
            </Typography>
            <Tooltip title="批量删除">
              <Button
                variant="contained"
                color="error"
                size="small"
                startIcon={<Delete />}
                onClick={handleBatchDelete}
              >
                批量删除
              </Button>
            </Tooltip>
          </Toolbar>
        )}
        <Table sx={{ minWidth: 1000 }}>
          <TableHead>
            <TableRow>
              <TableCell padding="checkbox">
                <Checkbox
                  indeterminate={selectedIds.length > 0 && selectedIds.length < paginatedRelays.length}
                  checked={paginatedRelays.length > 0 && selectedIds.length === paginatedRelays.length}
                  onChange={handleSelectAll}
                />
              </TableCell>
              <TableCell>ID</TableCell>
              <TableCell>名称</TableCell>
              <TableCell>接收频率</TableCell>
              <TableCell>发射频率</TableCell>
              <TableCell>接收亚音</TableCell>
              <TableCell>发射亚音</TableCell>
              <TableCell>所有者呼号</TableCell>
              <TableCell>位置</TableCell>
              <TableCell>状态</TableCell>
              <TableCell>备注</TableCell>
              <TableCell>操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={12} align="center">
                  加载中...
                </TableCell>
              </TableRow>
            ) : paginatedRelays.length === 0 ? (
              <TableRow>
                <TableCell colSpan={12} align="center">
                  暂无数据
                </TableCell>
              </TableRow>
            ) : (
              paginatedRelays.map((relay) => (
                <TableRow key={relay.id} hover selected={selectedIds.includes(relay.id)}>
                  <TableCell padding="checkbox">
                    <Checkbox
                      checked={selectedIds.includes(relay.id)}
                      onChange={() => handleSelect(relay.id)}
                    />
                  </TableCell>
                  <TableCell>{relay.id}</TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <SettingsInputAntenna color="primary" fontSize="small" />
                      {relay.name}
                    </Box>
                  </TableCell>
                  <TableCell>{relay.down_freq || '-'}</TableCell>
                  <TableCell>{relay.up_freq || '-'}</TableCell>
                  <TableCell>{relay.receive_ctcss ? formatToneDisplay(relay.receive_ctcss) : '-'}</TableCell>
                  <TableCell>{relay.send_ctcss ? formatToneDisplay(relay.send_ctcss) : '-'}</TableCell>
                  <TableCell>{relay.ower_callsign || '-'}</TableCell>
                  <TableCell>{relay.location || '-'}</TableCell>
                  <TableCell>
                    <Chip
                      label={relay.status === 1 ? '启用' : '禁用'}
                      color={relay.status === 1 ? 'success' : 'default'}
                      size="small"
                    />
                  </TableCell>
                  <TableCell>{relay.note || '-'}</TableCell>
                  <TableCell>
                    <IconButton size="small" onClick={() => handleOpenDialog(relay)}>
                      <Edit fontSize="small" />
                    </IconButton>
                    <IconButton size="small" color="error" onClick={() => handleDelete(relay.id)}>
                      <Delete fontSize="small" />
                    </IconButton>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        <TablePagination
          component="div"
          count={filteredRelays.length}
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
        <DialogTitle>{editingRelay ? '编辑中继台' : '添加中继台'}</DialogTitle>
        <DialogContent>
          <Alert severity="info" sx={{ mb: 2, mt: 1 }}>
            以下频率和亚音参数为<strong>设备上台时需设置</strong>的参数，而非中继台自身的收发参数。
          </Alert>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              label="名称"
              fullWidth
              required
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            />
            <Box sx={{ display: 'flex', gap: 2 }}>
              <TextField
                label="接收频率"
                sx={{ flex: 1 }}
                placeholder="例如: 439.500"
                value={formData.down_freq}
                onChange={(e) => setFormData({ ...formData, down_freq: e.target.value })}
                InputProps={{ endAdornment: <Typography color="text.secondary">MHz</Typography> }}
              />
              <TextField
                label="发射频率"
                sx={{ flex: 1 }}
                placeholder="例如: 434.500"
                value={formData.up_freq}
                onChange={(e) => setFormData({ ...formData, up_freq: e.target.value })}
                InputProps={{ endAdornment: <Typography color="text.secondary">MHz</Typography> }}
              />
            </Box>
            <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
              <ToneSelector
                label="接收亚音"
                value={receiveTone}
                onChange={(tone) => {
                  setReceiveTone(tone)
                  setFormData((prev) => ({ ...prev, receive_ctcss: toneSelectionToRelayValue(tone) }))
                }}
              />
              <ToneSelector
                label="发射亚音"
                value={sendTone}
                onChange={(tone) => {
                  setSendTone(tone)
                  setFormData((prev) => ({ ...prev, send_ctcss: toneSelectionToRelayValue(tone) }))
                }}
              />
            </Box>
            <TextField
              label="所有者呼号"
              fullWidth
              placeholder="例如: BD7XXX"
              value={formData.ower_callsign}
              onChange={(e) => setFormData({ ...formData, ower_callsign: e.target.value })}
            />
            <RegionCascader
              value={formData.location}
              onChange={(value) => setFormData({ ...formData, location: value })}
              label="位置"
              size="small"
              required
              helperText="至少需要选择到市级别"
              error={validateForm && formData.location.split(' ').filter(Boolean).length < 2}
            />
            <TextField
              label="备注"
              fullWidth
              multiline
              rows={5}
              placeholder="模拟中继可直接填写说明&#10;DMR: CC:1 TG:4600&#10;D-STAR: REF XXXXX C&#10;YSF: FCS XXXXX"
              value={formData.note}
              onChange={(e) => setFormData({ ...formData, note: e.target.value })}
              helperText="数字中继请在备注中标注参数，如 DMR Color Code、Talk Group 等"
            />
            <FormControlLabel
              control={
                <Switch
                  checked={statusSwitch}
                  onChange={(e) => setStatusSwitch(e.target.checked)}
                />
              }
              label={statusSwitch ? '启用' : '禁用'}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog}>取消</Button>
          <Button onClick={handleSave} variant="contained" type="button">
            保存
          </Button>
        </DialogActions>
      </Dialog>

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
