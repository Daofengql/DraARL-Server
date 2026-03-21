import { useState } from 'react'
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
} from '@mui/material'

// 预设频率类型
interface FreqPreset {
  name: string
  txFreq?: string
  rxFreq?: string
  txCtcss?: string
  rxCtcss?: string
  squelch?: number
  sameFreq?: boolean
  power?: 'high' | 'low'
  bandwidth?: 'wide' | 'narrow'
}

// 预设频率列表（后续从API获取）
const FREQ_PRESETS: FreqPreset[] = []

interface ParamConfigDialogProps {
  open: boolean
  deviceName: string
  onClose: () => void
  onSubmit?: (params: FreqParams) => void
}

export interface FreqParams {
  txFreq: string
  rxFreq: string
  txCtcss: string
  rxCtcss: string
  squelch: number
  sameFreq: boolean
  power: 'high' | 'low'
  bandwidth: 'wide' | 'narrow'
}

export function ParamConfigDialog({ open, deviceName, onClose, onSubmit }: ParamConfigDialogProps) {
  const [tabValue, setTabValue] = useState(0)
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

  const handleSubmit = () => {
    onSubmit?.(freqParams)
    onClose()
  }

  const handleClose = () => {
    onClose()
  }

  return (
    <Dialog
      open={open}
      onClose={(_, reason) => reason === 'backdropClick' ? null : handleClose()}
      maxWidth="md"
      fullWidth
    >
      <DialogTitle sx={{ pb: 0 }}>参数下发 - {deviceName}</DialogTitle>
      <Box sx={{ borderBottom: 1, borderColor: 'divider', px: 3 }}>
        <Tabs value={tabValue} onChange={(_, v) => setTabValue(v)}>
          <Tab label="频率设置" />
          <Tab label="系统设置" />
        </Tabs>
      </Box>
      <DialogContent>
        <Box sx={{ py: 2 }}>
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
                  label="发送亚音 (CTCSS)"
                  value={freqParams.txCtcss}
                  onChange={(e) => setFreqParams(prev => ({ ...prev, txCtcss: e.target.value }))}
                  placeholder="例如: 88.5"
                  helperText="留空表示关闭"
                />
              </Grid>
              <Grid size={6}>
                <TextField
                  fullWidth
                  label="接收亚音 (CTCSS)"
                  value={freqParams.rxCtcss}
                  onChange={(e) => setFreqParams(prev => ({ ...prev, rxCtcss: e.target.value }))}
                  placeholder="例如: 88.5"
                  helperText="留空表示关闭"
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
                    onChange={(e) => setFreqParams(prev => ({ ...prev, power: e.target.value as 'high' | 'low' }))}
                  >
                    <MenuItem value="high">高功率</MenuItem>
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
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose}>取消</Button>
        <Button variant="contained" onClick={handleSubmit}>下发参数</Button>
      </DialogActions>
    </Dialog>
  )
}
