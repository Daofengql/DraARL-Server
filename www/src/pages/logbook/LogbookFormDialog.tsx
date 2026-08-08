import { useState, useCallback, useEffect } from 'react'
import { useTheme } from '@mui/material/styles'
import useMediaQuery from '@mui/material/useMediaQuery'
import {
  Box, Paper, Typography, IconButton, Button, Dialog, DialogTitle, DialogContent,
  DialogActions, TextField, FormControl, InputLabel, Select, MenuItem, Grid,
  Switch, FormControlLabel, Tooltip, Autocomplete, CircularProgress,
} from '@mui/material'
import Refresh from '@mui/icons-material/Refresh'
import LinkIcon from '@mui/icons-material/Link'
import LinkOffIcon from '@mui/icons-material/LinkOff'
import Search from '@mui/icons-material/Search'
import Settings from '@mui/icons-material/Settings'
import { RegionCascader } from '../../components/common/RegionCascader'
import { authService } from '../../services/auth'
import { relayService } from '../../services/relay'
import type { Relay } from '../../types'
import { bjtToUtc, getCurrentUtcTime, utcToBjt, MODE_OPTIONS } from './time'
import type { LogbookEntry, RadioPreset } from './types'
interface LogbookFormDialogProps {
  open: boolean
  onClose: () => void
  onSave: (entry: Omit<LogbookEntry, 'id'>) => void
  initialData?: LogbookEntry | null
  title: string
  presets: RadioPreset[]
  onManagePresets: () => void
  isAdminPage: boolean
}



export function LogbookFormDialog({ open, onClose, onSave, initialData, title, presets, onManagePresets, isAdminPage }: LogbookFormDialogProps) {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))

  const [formData, setFormData] = useState<Partial<LogbookEntry>>(() =>
    initialData || {
      my_callsign: '',
      time_utc: getCurrentUtcTime(),
      tx_frequency: 0,
      rx_frequency: 0,
      cq_zone: 24,
      itu_zone: 44,
      mode: 'FM',
      callsign: '',
      their_rst: '59',
      their_power: undefined,
      their_qth: '',
      their_radio: '',
      their_antenna: '',
      my_rst: '59',
      my_power: undefined,
      my_qth: '',
      my_radio: '',
      my_antenna: '',
      notes: '',
    }
  )

  const [timeMode, setTimeMode] = useState<'bjt' | 'utc'>('bjt')
  const [isRepeater, setIsRepeater] = useState(false) // 是否中继模式
  const [isSameFrequency, setIsSameFrequency] = useState(true) // 是否同频
  const [hasSubmitted, setHasSubmitted] = useState(false) // 是否尝试过提交

  // 中继台搜索相关状态
  const [relayLocation, setRelayLocation] = useState('')
  const [relayOptions, setRelayOptions] = useState<Relay[]>([])
  const [relaySearching, setRelaySearching] = useState(false)

  // 重置表单 - 打开时默认使用当前时间
  const resetForm = useCallback(() => {
    setHasSubmitted(false)
    if (initialData) {
      setFormData(initialData)
      setIsRepeater(initialData.tx_frequency !== initialData.rx_frequency)
    } else {
      // 获取当前用户的呼号和地址作为默认值
      const currentUser = authService.getStoredUser()
      setFormData({
        my_callsign: currentUser?.callsign || '',
        my_qth: currentUser?.address || '',
        time_utc: getCurrentUtcTime(),
        tx_frequency: 0,
        rx_frequency: 0,
        cq_zone: 24,
        itu_zone: 44,
        mode: 'FM',
        callsign: '',
        their_rst: '59',
        their_power: undefined,
        their_qth: '',
        their_radio: '',
        their_antenna: '',
        my_rst: '59',
        my_power: undefined,
        my_radio: '',
        my_antenna: '',
        notes: '',
      })
      setIsRepeater(false)
    }
  }, [initialData])

  // 打开弹窗时重置
  useEffect(() => {
    if (open) {
      resetForm()
    }
  }, [open, resetForm])

  // 处理时间变化（根据显示模式自动转换）
  const handleTimeChange = (value: string, mode: 'bjt' | 'utc') => {
    if (mode === 'bjt') {
      // 输入的是 BJT，转换为 UTC 存储
      setFormData(prev => ({
        ...prev,
        time_utc: bjtToUtc(value),
      }))
    } else {
      // 输入的是 UTC，直接存储
      setFormData(prev => ({
        ...prev,
        time_utc: value,
      }))
    }
  }

  // 获取当前显示的时间值
  const getDisplayTime = () => {
    if (!formData.time_utc) return ''
    return timeMode === 'bjt' ? utcToBjt(formData.time_utc) : formData.time_utc
  }

  // 使用当前时间
  const useCurrentTime = () => {
    // 数据库始终存储UTC时间
    // 直接获取当前UTC时间存储即可
    // 显示时会根据时区模式自动转换
    setFormData(prev => ({
      ...prev,
      time_utc: getCurrentUtcTime(),
    }))
  }

  // 处理发射频率变化
  const handleTxFrequencyChange = (value: number) => {
    setFormData(prev => ({
      ...prev,
      tx_frequency: value,
      rx_frequency: !isRepeater ? value : prev.rx_frequency,
    }))
  }

  // 保存
  const handleSave = () => {
    setHasSubmitted(true)
    // 验证必填字段
    if (!formData.my_callsign || !formData.callsign || !formData.tx_frequency || !formData.mode) {
      return
    }

    onSave({
      my_callsign: formData.my_callsign || '',
      time_utc: formData.time_utc || getCurrentUtcTime(),
      tx_frequency: formData.tx_frequency || 0,
      rx_frequency: formData.rx_frequency || formData.tx_frequency || 0,
      cq_zone: formData.cq_zone || 24,
      itu_zone: formData.itu_zone || 44,
      mode: formData.mode || 'FM',
      callsign: formData.callsign || '',
      their_rst: formData.their_rst || '59',
      their_power: formData.their_power,
      their_qth: formData.their_qth || '',
      their_radio: formData.their_radio || '',
      their_antenna: formData.their_antenna || '',
      my_rst: formData.my_rst || '59',
      my_power: formData.my_power,
      my_qth: formData.my_qth || '',
      my_radio: formData.my_radio || '',
      my_antenna: formData.my_antenna || '',
      notes: formData.notes || '',
    })
  }

  // 搜索中继台
  const handleSearchRelays = async () => {
    const locationParts = relayLocation.split(' ').filter(Boolean)
    if (locationParts.length < 2) {
      return
    }

    setRelaySearching(true)
    try {
      const relays = await relayService.publicSearch(relayLocation)
      setRelayOptions(relays)
    } catch (error) {
      console.error('搜索中继台失败:', error)
      setRelayOptions([])
    } finally {
      setRelaySearching(false)
    }
  }

  // 快速填充中继台
  const handleRepeaterSelect = (relay: Relay | null) => {
    if (relay) {
      setIsRepeater(true)
      // 中继台存储的频率已经是MHz单位，直接使用
      // up_freq: 中继台上行（中继台接收），down_freq: 中继台下行（中继台发射）
      // 用户设备：发射频率 = 中继台上行，接收频率 = 中继台下行
      const txFreq = relay.up_freq ? parseFloat(relay.up_freq) : 0
      const rxFreq = relay.down_freq ? parseFloat(relay.down_freq) : 0
      // 如果收发频率不同，关闭同频开关
      if (txFreq !== rxFreq) {
        setIsSameFrequency(false)
      }
      setFormData(prev => ({
        ...prev,
        tx_frequency: txFreq,
        rx_frequency: rxFreq,
      }))
    }
  }

  // 快速填充我方设备
  const handleMyRadioSelect = (preset: RadioPreset | null) => {
    if (preset) {
      setFormData(prev => ({
        ...prev,
        my_radio: preset.radio,
        my_antenna: preset.antenna,
        my_qth: preset.qth || prev.my_qth,
        my_power: preset.power ?? prev.my_power,
      }))
    }
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="lg"
      fullWidth
      fullScreen={isMobile}
    >
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {title}
      </DialogTitle>
      <DialogContent dividers sx={{ p: { xs: 1.5, sm: 3 } }}>
        <Grid container spacing={{ xs: 1.5, sm: 2.5 }}>
          {/* 通联时间 */}
          <Grid size={12}>
            <Paper variant="outlined" sx={{ p: { xs: 1.5, sm: 2 }, bgcolor: 'grey.50' }}>
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1.5, flexWrap: 'wrap', gap: 1 }}>
                <Typography variant="subtitle2" color="text.secondary">
                  通联时间
                </Typography>
                <Button
                  size="small"
                  variant="outlined"
                  onClick={useCurrentTime}
                  startIcon={<Refresh fontSize="small" />}
                >
                  当前时间
                </Button>
              </Box>
              <Grid container spacing={{ xs: 1, sm: 2 }}>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <TextField
                    fullWidth
                    label="日期"
                    type="date"
                    size="small"
                    value={getDisplayTime().slice(0, 10) || ''}
                    onChange={(e) => {
                      const currentTime = getDisplayTime()
                      // 保留时间部分（时分秒）
                      const timePart = currentTime?.slice(11) || '00:00:00'
                      const newTime = e.target.value + ' ' + timePart
                      handleTimeChange(newTime, timeMode)
                    }}
                    slotProps={{ inputLabel: { shrink: true } }}
                  />
                </Grid>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <TextField
                    fullWidth
                    label="时间"
                    type="time"
                    size="small"
                    value={getDisplayTime().slice(11, 16) || ''}
                    onChange={(e) => {
                      const currentTime = getDisplayTime()
                      // 保留秒数部分（:SS），如果原时间没有秒则使用 :00
                      const secondsPart = currentTime?.length >= 19 ? currentTime.slice(14, 19) : ':00'
                      const newTime = (currentTime?.slice(0, 10) || new Date().toISOString().slice(0, 10)) + ' ' + e.target.value + secondsPart
                      handleTimeChange(newTime, timeMode)
                    }}
                    slotProps={{ inputLabel: { shrink: true } }}
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 4, md: 3 }}>
                  <FormControl fullWidth size="small">
                    <InputLabel>时区</InputLabel>
                    <Select
                      value={timeMode}
                      label="时区"
                      onChange={(e) => setTimeMode(e.target.value as 'bjt' | 'utc')}
                    >
                      <MenuItem value="bjt">BJT (北京时间)</MenuItem>
                      <MenuItem value="utc">UTC (协调世界时)</MenuItem>
                    </Select>
                  </FormControl>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 频率设置 */}
          <Grid size={12}>
            <Paper variant="outlined" sx={{ p: { xs: 1.5, sm: 2 }, bgcolor: 'grey.50' }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 1.5, flexWrap: 'wrap' }}>
                <Typography variant="subtitle2" color="text.secondary">
                  频率设置
                </Typography>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={isSameFrequency}
                        onChange={(e) => {
                          const same = e.target.checked
                          setIsSameFrequency(same)
                          if (same) {
                            setFormData(prev => ({ ...prev, rx_frequency: prev.tx_frequency }))
                          }
                        }}
                        size="small"
                      />
                    }
                    label={
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                        {isSameFrequency ? <LinkIcon fontSize="small" /> : <LinkOffIcon fontSize="small" />}
                        <Typography variant="caption">
                          {isSameFrequency ? '同频' : '异频'}
                        </Typography>
                      </Box>
                    }
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={isRepeater}
                        onChange={(e) => setIsRepeater(e.target.checked)}
                        size="small"
                      />
                    }
                    label={<Typography variant="caption">中继</Typography>}
                  />
                </Box>
              </Box>

              {/* 中继台快速选择 - 仅中继模式显示 */}
              {isRepeater && (
                <Box sx={{ mb: 2 }}>
                  <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, gap: 1, alignItems: { sm: 'flex-end' }, mb: 2 }}>
                    <Box sx={{ flex: 1, minWidth: 0 }}>
                      <RegionCascader
                        value={relayLocation}
                        onChange={setRelayLocation}
                        label="选择地区搜索中继台"
                        size="small"
                      />
                    </Box>
                    <Button
                      variant="outlined"
                      size="small"
                      onClick={handleSearchRelays}
                      disabled={relaySearching}
                      startIcon={relaySearching ? <CircularProgress size={16} color="inherit" /> : <Search fontSize="small" />}
                      sx={{ minWidth: 80, height: 40 }}
                    >
                      {relaySearching ? '搜索中...' : '搜索'}
                    </Button>
                  </Box>
                  {relayOptions.length > 0 && (
                    <Autocomplete
                      size="small"
                      options={relayOptions}
                      getOptionLabel={(option) => option.name}
                      onChange={(_, value) => handleRepeaterSelect(value)}
                      renderInput={(params) => (
                        <TextField
                          {...params}
                          label="选择中继台"
                          placeholder="选择中继台自动填入频率..."
                        />
                      )}
                      renderOption={(props, option) => {
                        const txFreq = option.up_freq || '-'
                        const rxFreq = option.down_freq || '-'
                        return (
                          <li {...props} key={option.id}>
                            <Box>
                              <Typography variant="body2">{option.name}</Typography>
                              <Typography variant="caption" color="text.secondary">
                                发: {txFreq} MHz / 收: {rxFreq} MHz
                                {option.location && ` · ${option.location}`}
                              </Typography>
                            </Box>
                          </li>
                        )
                      }}
                      noOptionsText="暂无中继台数据"
                    />
                  )}
                </Box>
              )}

              <Grid container spacing={{ xs: 1, sm: 2 }}>
                <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                  <TextField
                    fullWidth
                    required
                    size="small"
                    label={!isRepeater ? "频率 (MHz)" : "发射频率 (MHz)"}
                    type="number"
                    value={formData.tx_frequency || ''}
                    onChange={(e) => handleTxFrequencyChange(parseFloat(e.target.value) || 0)}
                    inputProps={{ step: 0.001 }}
                    error={hasSubmitted && !formData.tx_frequency}
                    helperText={hasSubmitted && !formData.tx_frequency ? '必填' : ''}
                  />
                </Grid>
                {!isSameFrequency && (
                  <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                    <TextField
                      fullWidth
                      label="接收频率 (MHz)"
                      type="number"
                      size="small"
                      value={formData.rx_frequency || ''}
                      onChange={(e) => setFormData(prev => ({ ...prev, rx_frequency: parseFloat(e.target.value) || 0 }))}
                      inputProps={{ step: 0.001 }}
                    />
                  </Grid>
                )}
              </Grid>
            </Paper>
          </Grid>

          {/* 无线电信息 */}
          <Grid size={12}>
            <Paper variant="outlined" sx={{ p: { xs: 1.5, sm: 2 }, bgcolor: 'grey.50' }}>
              <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1.5 }}>
                无线电信息
              </Typography>
              <Grid container spacing={{ xs: 1, sm: 2 }}>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <FormControl fullWidth size="small">
                    <InputLabel>通信模式</InputLabel>
                    <Select
                      value={formData.mode || 'FM'}
                      label="通信模式"
                      onChange={(e) => setFormData(prev => ({ ...prev, mode: e.target.value }))}
                    >
                      {MODE_OPTIONS.map(mode => (
                        <MenuItem key={mode} value={mode}>{mode}</MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                </Grid>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <TextField
                    fullWidth
                    label="CQ 分区"
                    type="number"
                    size="small"
                    value={formData.cq_zone || ''}
                    onChange={(e) => setFormData(prev => ({ ...prev, cq_zone: parseInt(e.target.value) || 1 }))}
                    inputProps={{ min: 1, max: 40 }}
                  />
                </Grid>
                <Grid size={{ xs: 6, sm: 4, md: 3 }}>
                  <TextField
                    fullWidth
                    label="ITU 分区"
                    type="number"
                    size="small"
                    value={formData.itu_zone || ''}
                    onChange={(e) => setFormData(prev => ({ ...prev, itu_zone: parseInt(e.target.value) || 1 }))}
                    inputProps={{ min: 1, max: 90 }}
                  />
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 双方信息 - 两列布局 */}
          <Grid size={12}>
            <Grid container spacing={{ xs: 1, sm: 2 }}>
              {/* 对方信息 */}
              <Grid size={{ xs: 12, md: 6 }}>
                <Paper
                  variant="outlined"
                  sx={{
                    p: { xs: 1.5, sm: 2 },
                    height: '100%',
                    borderColor: 'primary.light',
                    bgcolor: 'primary.50',
                  }}
                >
                  <Typography variant="subtitle2" color="primary.main" sx={{ mb: 1.5, fontWeight: 600 }}>
                    对方信息
                  </Typography>
                  <Grid container spacing={{ xs: 1, sm: 1.5 }}>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        required
                        label="对方呼号"
                        size="small"
                        value={formData.callsign || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, callsign: e.target.value.toUpperCase() }))}
                        placeholder="例如: BH1ABC"
                        error={hasSubmitted && !formData.callsign}
                        helperText={hasSubmitted && !formData.callsign ? '必填' : ''}
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="QTH (位置)"
                        size="small"
                        value={formData.their_qth || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_qth: e.target.value }))}
                        placeholder="例如: 北京"
                      />
                    </Grid>
                    <Grid size={{ xs: 6, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="收信报告 (RST)"
                        size="small"
                        value={formData.their_rst || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_rst: e.target.value }))}
                        placeholder="59 / 599"
                      />
                    </Grid>
                    <Grid size={{ xs: 6, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="功率 (W)"
                        type="number"
                        size="small"
                        value={formData.their_power || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_power: parseInt(e.target.value) || undefined }))}
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="设备型号"
                        size="small"
                        value={formData.their_radio || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_radio: e.target.value }))}
                        placeholder="例如: IC-9700"
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="天馈"
                        size="small"
                        value={formData.their_antenna || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, their_antenna: e.target.value }))}
                        placeholder="例如: 八木"
                      />
                    </Grid>
                  </Grid>
                </Paper>
              </Grid>

              {/* 我方信息 */}
              <Grid size={{ xs: 12, md: 6 }}>
                <Paper
                  variant="outlined"
                  sx={{
                    p: { xs: 1.5, sm: 2 },
                    height: '100%',
                    borderColor: 'secondary.light',
                    bgcolor: 'secondary.50',
                  }}
                >
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1.5, gap: 1 }}>
                    <Typography variant="subtitle2" color="secondary.main" sx={{ fontWeight: 600 }}>
                      我方信息
                    </Typography>
                    {!isAdminPage && (
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                        <Autocomplete
                          size="small"
                          options={presets}
                          getOptionLabel={(option) => option.name}
                          onChange={(_, value) => handleMyRadioSelect(value)}
                          sx={{ width: 140 }}
                          renderInput={(params) => (
                            <TextField
                              {...params}
                              label="快速选择"
                              size="small"
                            />
                          )}
                          renderOption={(props, option) => (
                            <li {...props} key={option.id}>
                              <Box>
                                <Typography variant="body2">{option.name}</Typography>
                                <Typography variant="caption" color="text.secondary">
                                  {option.radio} / {option.antenna}
                                </Typography>
                              </Box>
                            </li>
                          )}
                        />
                        <Tooltip title="管理预设">
                          <IconButton size="small" onClick={onManagePresets}>
                            <Settings fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Box>
                    )}
                  </Box>
                  <Grid container spacing={{ xs: 1, sm: 1.5 }}>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        required
                        label="我方呼号"
                        size="small"
                        value={formData.my_callsign || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_callsign: e.target.value }))}
                        placeholder="例如: BG7XXX"
                        error={hasSubmitted && !formData.my_callsign}
                        helperText={hasSubmitted && !formData.my_callsign ? '必填' : ''}
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="QTH (位置)"
                        size="small"
                        value={formData.my_qth || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_qth: e.target.value }))}
                        placeholder="例如: 广州"
                      />
                    </Grid>
                    <Grid size={{ xs: 6, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="发信报告 (RST)"
                        size="small"
                        value={formData.my_rst || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_rst: e.target.value }))}
                        placeholder="59 / 599"
                      />
                    </Grid>
                    <Grid size={{ xs: 6, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="功率 (W)"
                        type="number"
                        size="small"
                        value={formData.my_power || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_power: parseInt(e.target.value) || undefined }))}
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="设备型号"
                        size="small"
                        value={formData.my_radio || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_radio: e.target.value }))}
                        placeholder="例如: FT-991A"
                      />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 6 }}>
                      <TextField
                        fullWidth
                        label="天馈"
                        size="small"
                        value={formData.my_antenna || ''}
                        onChange={(e) => setFormData(prev => ({ ...prev, my_antenna: e.target.value }))}
                        placeholder="例如: GP"
                      />
                    </Grid>
                  </Grid>
                </Paper>
              </Grid>
            </Grid>
          </Grid>

          {/* 备注 */}
          <Grid size={12}>
            <TextField
              fullWidth
              label="备注"
              size="small"
              multiline
              rows={2}
              value={formData.notes || ''}
              onChange={(e) => setFormData(prev => ({ ...prev, notes: e.target.value }))}
              placeholder="记录通联的详细信息..."
            />
          </Grid>
        </Grid>
      </DialogContent>
      <DialogActions sx={{ px: { xs: 2, sm: 3 }, py: 2 }}>
        <Button onClick={onClose}>取消</Button>
        <Button variant="contained" onClick={handleSave}>
          保存
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// 详情弹窗组件
