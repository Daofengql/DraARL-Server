import { useState, useEffect } from 'react'
import {
  Box,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Tabs,
  Tab,
  Grid,
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormControlLabel,
  Switch,
  Divider,
  Typography,
  Autocomplete,
  CircularProgress,
  Alert,
  Snackbar,
} from '@mui/material'
import { deviceService, type DeviceConfig } from '../../services/device'
import { DEVICE_MODELS } from '../../utils/deviceModel'

// 预设频率类型
interface FreqPreset {
  name: string
  txFreq?: string
  rxFreq?: string
  txCtcss?: string
  rxCtcss?: string
  squelch?: number
  sameFreq?: boolean
  power?: 'high' | 'medium' | 'low'
  bandwidth?: 'wide' | 'narrow'
}

// 预设频率列表（后续从API获取）
const FREQ_PRESETS: FreqPreset[] = []

interface ParamConfigDialogProps {
  open: boolean
  deviceId: number | undefined
  deviceName: string
  deviceModel: number
  isOnline: boolean
  onClose: () => void
  onDeviceUpdated?: () => void
}

// 内部使用的频率参数（MHz 单位）
interface FreqParams {
  txFreq: string    // MHz
  rxFreq: string    // MHz
  txCtcss: string   // Hz
  rxCtcss: string   // Hz
  squelch: number   // 0-9
  sameFreq: boolean
  power: 'high' | 'medium' | 'low'
  bandwidth: 'wide' | 'narrow'
}

// 将 Hz 转换为 MHz 显示
const hzToMHz = (hz: string): string => {
  if (!hz || hz === '0') return ''
  const num = parseInt(hz, 10)
  if (isNaN(num)) return ''
  return (num / 1_000_000).toFixed(6).replace(/\.?0+$/, '')
}

// 将 MHz 转换为 Hz 存储
const mHzToHz = (mhz: string): string => {
  if (!mhz) return '0'
  const num = parseFloat(mhz)
  if (isNaN(num)) return '0'
  return Math.round(num * 1_000_000).toString()
}

// 将功率等级转换
const powerToLevel = (power: string): string => {
  switch (power) {
    case 'low': return '1'
    case 'medium': return '2'
    case 'high': return '3'
    default: return '3'
  }
}

const levelToPower = (level: string): 'high' | 'medium' | 'low' => {
  switch (level) {
    case '1': return 'low'
    case '2': return 'medium'
    case '3': return 'high'
    default: return 'high'
  }
}

// 将带宽转换
const bandwidthToLevel = (bandwidth: string): string => {
  return bandwidth === 'narrow' ? '1' : '2'
}

const levelToBandwidth = (level: string): 'wide' | 'narrow' => {
  return level === '1' ? 'narrow' : 'wide'
}

export function ParamConfigDialog({ open, deviceId, deviceName, deviceModel, isOnline, onClose, onDeviceUpdated }: ParamConfigDialogProps) {
  const [tabValue, setTabValue] = useState(0)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' | 'info' }>({ open: false, message: '', severity: 'info' })
  const [freqParams, setFreqParams] = useState<FreqParams>({
    txFreq: '',
    rxFreq: '',
    txCtcss: '',
    rxCtcss: '',
    squelch: 0,
    sameFreq: true,
    power: 'high',
    bandwidth: 'wide',
  })

  // 平台设置 - 设备编辑
  const [platformFormData, setPlatformFormData] = useState({
    name: '',
    model: 1,
  })

  // 加载设备配置
  useEffect(() => {
    if (open && deviceId) {
      loadConfig()
      // 初始化平台设置表单
      setPlatformFormData({
        name: deviceName,
        model: deviceModel,
      })
    }
  }, [open, deviceId, deviceName, deviceModel])

  const loadConfig = async () => {
    if (!deviceId) return
    setLoading(true)
    try {
      const config = await deviceService.getConfig(deviceId)
      // 转换配置到本地状态
      const rxFreqMHz = hzToMHz(config.rx_freq || '0')
      const txFreqMHz = hzToMHz(config.tx_freq || '0')
      setFreqParams({
        rxFreq: rxFreqMHz,
        txFreq: txFreqMHz,
        rxCtcss: config.rx_ctcss || '',
        txCtcss: config.tx_ctcss || '',
        squelch: parseInt(config.sql_level || '0', 10),
        sameFreq: rxFreqMHz === txFreqMHz || !rxFreqMHz || !txFreqMHz,
        power: levelToPower(config.power_level || '3'),
        bandwidth: levelToBandwidth(config.tx_bandwidth || '2'),
      })
    } catch (error) {
      console.error('加载配置失败:', error)
      setSnackbar({ open: true, message: '加载配置失败', severity: 'error' })
    } finally {
      setLoading(false)
    }
  }

  const handleSaveAndSync = async () => {
    if (!deviceId) return
    setSaving(true)
    try {
      // 构建配置对象（使用 Hz 单位）
      const config: Partial<DeviceConfig> = {
        tx_freq: mHzToHz(freqParams.txFreq),
        rx_freq: freqParams.sameFreq ? mHzToHz(freqParams.txFreq) : mHzToHz(freqParams.rxFreq),
        tx_ctcss: freqParams.txCtcss || '0',
        rx_ctcss: freqParams.rxCtcss || '0',
        sql_level: freqParams.squelch.toString(),
        power_level: powerToLevel(freqParams.power),
        tx_bandwidth: bandwidthToLevel(freqParams.bandwidth),
      }

      // 保存到数据库
      await deviceService.updateConfig(deviceId, config)

      // 如果设备在线，立即同步
      if (isOnline) {
        const result = await deviceService.syncConfig(deviceId)
        setSnackbar({ open: true, message: result.message || '配置已下发到设备', severity: 'success' })
      } else {
        setSnackbar({ open: true, message: '配置已保存，设备上线后将自动同步', severity: 'success' })
      }

      onClose()
    } catch (error) {
      console.error('保存配置失败:', error)
      setSnackbar({ open: true, message: '保存配置失败', severity: 'error' })
    } finally {
      setSaving(false)
    }
  }

  // 保存平台设置（设备名称和型号）
  const handleSavePlatform = async () => {
    if (!deviceId) return
    if (!platformFormData.name.trim()) {
      setSnackbar({ open: true, message: '请输入设备名称', severity: 'error' })
      return
    }

    setSaving(true)
    try {
      await deviceService.update(deviceId, {
        name: platformFormData.name,
        model: platformFormData.model,
      })
      setSnackbar({ open: true, message: '设备信息保存成功', severity: 'success' })
      onDeviceUpdated?.()
      onClose()
    } catch (error: any) {
      console.error('保存设备信息失败:', error)
      setSnackbar({ open: true, message: error.response?.data?.message || '保存失败', severity: 'error' })
    } finally {
      setSaving(false)
    }
  }

  const handleClose = () => {
    onClose()
  }

  // 根据 tab 获取标题
  const getTabTitle = () => {
    switch (tabValue) {
      case 0: return '频率设置'
      case 1: return '系统设置'
      case 2: return '平台设置'
      default: return '参数配置'
    }
  }

  return (
    <Dialog
      open={open}
      onClose={(_, reason) => reason === 'backdropClick' ? null : handleClose()}
      maxWidth="md"
      fullWidth
    >
      <DialogTitle sx={{ pb: 0 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span>{getTabTitle()} - {deviceName}</span>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {loading && <CircularProgress size={20} />}
            {isOnline ? (
              <Typography variant="caption" color="success.main">● 在线</Typography>
            ) : (
              <Typography variant="caption" color="text.secondary">○ 离线</Typography>
            )}
          </Box>
        </Box>
      </DialogTitle>
      <Box sx={{ borderBottom: 1, borderColor: 'divider', px: 3 }}>
        <Tabs value={tabValue} onChange={(_, v) => setTabValue(v)}>
          <Tab label="频率设置" />
          <Tab label="系统设置" />
          <Tab label="平台设置" />
        </Tabs>
      </Box>
      <DialogContent>
        <Box sx={{ py: 2 }}>
          {!isOnline && (
            <Alert severity="info" sx={{ mb: 2 }}>
              设备当前离线，配置将保存到服务器，设备上线后自动同步。
            </Alert>
          )}
          {tabValue === 0 && (
            <Grid container spacing={3}>
              {/* 快速填入 */}
              <Grid size={12}>
                <Autocomplete
                  fullWidth
                  options={FREQ_PRESETS}
                  getOptionLabel={(option) => option.name}
                  onChange={(_, value) => {
                    if (value) {
                      setFreqParams({
                        txFreq: value.txFreq || '',
                        rxFreq: value.rxFreq || '',
                        txCtcss: value.txCtcss || '',
                        rxCtcss: value.rxCtcss || '',
                        squelch: value.squelch ?? 0,
                        sameFreq: value.sameFreq ?? true,
                        power: value.power || 'high',
                        bandwidth: value.bandwidth || 'wide',
                      })
                    }
                  }}
                  renderInput={(params) => (
                    <TextField
                      {...params}
                      label="快速填入预设"
                      placeholder="搜索预设频率..."
                    />
                  )}
                  noOptionsText="暂无预设数据"
                />
              </Grid>

              <Grid size={12}><Divider /></Grid>

              {/* 频率设置 */}
              <Grid size={6}>
                <TextField
                  fullWidth
                  label="发射频率 (MHz)"
                  value={freqParams.txFreq}
                  onChange={(e) => {
                    const val = e.target.value
                    setFreqParams(prev => ({
                      ...prev,
                      txFreq: val,
                      rxFreq: prev.sameFreq ? val : prev.rxFreq,
                    }))
                  }}
                  placeholder="例如: 439.500"
                />
              </Grid>
              <Grid size={6}>
                <TextField
                  fullWidth
                  label="接收频率 (MHz)"
                  value={freqParams.sameFreq ? freqParams.txFreq : freqParams.rxFreq}
                  onChange={(e) => setFreqParams(prev => ({ ...prev, rxFreq: e.target.value }))}
                  disabled={freqParams.sameFreq}
                  placeholder="例如: 439.500"
                />
              </Grid>

              {/* 收发同频开关 */}
              <Grid size={12}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={freqParams.sameFreq}
                      onChange={(e) => setFreqParams(prev => ({
                        ...prev,
                        sameFreq: e.target.checked,
                        rxFreq: e.target.checked ? prev.txFreq : prev.rxFreq,
                      }))}
                    />
                  }
                  label="收发同频"
                />
              </Grid>

              <Grid size={12}><Divider /></Grid>

              {/* 亚音设置 */}
              <Grid size={6}>
                <TextField
                  fullWidth
                  label="发送亚音 (CTCSS Hz)"
                  value={freqParams.txCtcss}
                  onChange={(e) => setFreqParams(prev => ({ ...prev, txCtcss: e.target.value }))}
                  placeholder="例如: 88.5"
                  helperText="留空或 0 表示关闭"
                />
              </Grid>
              <Grid size={6}>
                <TextField
                  fullWidth
                  label="接收亚音 (CTCSS Hz)"
                  value={freqParams.rxCtcss}
                  onChange={(e) => setFreqParams(prev => ({ ...prev, rxCtcss: e.target.value }))}
                  placeholder="例如: 88.5"
                  helperText="留空或 0 表示关闭"
                />
              </Grid>

              <Grid size={12}><Divider /></Grid>

              {/* SQL静噪、功率、带宽 */}
              <Grid size={4}>
                <TextField
                  fullWidth
                  label="SQL 静噪等级"
                  type="number"
                  value={freqParams.squelch}
                  onChange={(e) => setFreqParams(prev => ({ ...prev, squelch: Number(e.target.value) }))}
                  slotProps={{ htmlInput: { min: 0, max: 9 } }}
                  helperText="0-9 级"
                />
              </Grid>
              <Grid size={4}>
                <FormControl fullWidth>
                  <InputLabel>发射功率</InputLabel>
                  <Select
                    value={freqParams.power}
                    label="发射功率"
                    onChange={(e) => setFreqParams(prev => ({ ...prev, power: e.target.value as 'high' | 'medium' | 'low' }))}
                  >
                    <MenuItem value="high">高功率</MenuItem>
                    <MenuItem value="medium">中功率</MenuItem>
                    <MenuItem value="low">低功率</MenuItem>
                  </Select>
                </FormControl>
              </Grid>
              <Grid size={4}>
                <FormControl fullWidth>
                  <InputLabel>发射带宽</InputLabel>
                  <Select
                    value={freqParams.bandwidth}
                    label="发射带宽"
                    onChange={(e) => setFreqParams(prev => ({ ...prev, bandwidth: e.target.value as 'wide' | 'narrow' }))}
                  >
                    <MenuItem value="wide">宽带 (25kHz)</MenuItem>
                    <MenuItem value="narrow">窄带 (12.5kHz)</MenuItem>
                  </Select>
                </FormControl>
              </Grid>
            </Grid>
          )}
          {tabValue === 1 && (
            <Typography color="text.secondary" align="center" sx={{ py: 4 }}>
              系统设置功能开发中，敬请期待...
            </Typography>
          )}
          {tabValue === 2 && (
            <Grid container spacing={3}>
              <Grid size={12}>
                <TextField
                  fullWidth
                  label="设备名称"
                  value={platformFormData.name}
                  onChange={(e) => setPlatformFormData(prev => ({ ...prev, name: e.target.value }))}
                  placeholder="请输入设备名称"
                />
              </Grid>
              <Grid size={12}>
                <FormControl fullWidth>
                  <InputLabel>设备型号</InputLabel>
                  <Select
                    value={platformFormData.model}
                    label="设备型号"
                    onChange={(e) => setPlatformFormData(prev => ({ ...prev, model: e.target.value as number }))}
                  >
                    {DEVICE_MODELS.map((model) => (
                      <MenuItem key={model.value} value={model.value}>
                        {model.label}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </Grid>
            </Grid>
          )}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose}>取消</Button>
        {tabValue === 2 ? (
          // 平台设置的保存按钮
          <Button
            variant="contained"
            onClick={handleSavePlatform}
            disabled={saving || loading}
            startIcon={saving ? <CircularProgress size={20} /> : null}
          >
            {saving ? '保存中...' : '保存'}
          </Button>
        ) : (
          // 频率设置/系统设置的保存按钮
          <Button
            variant="contained"
            onClick={handleSaveAndSync}
            disabled={saving || loading}
            startIcon={saving ? <CircularProgress size={20} /> : null}
          >
            {saving ? '保存中...' : (isOnline ? '保存并同步' : '保存配置')}
          </Button>
        )}
      </DialogActions>

      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </Dialog>
  )
}
