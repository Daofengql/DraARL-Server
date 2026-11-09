import { useCallback, useEffect, useRef, useState } from 'react'
import { isAxiosError } from 'axios'
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Grid,
  Snackbar,
  Tab,
  Tabs,
  TextField,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material'
import { deviceService, type DeviceConfig } from '../../services/device'
import {
  getDevModelName,
  getDeviceConfigTabs,
  supportsAudioConfig,
  supportsBoardIOPolarityConfig,
} from '../../utils/deviceModel'
import {
  bandwidthToLevel,
  buildToneSelection,
  ADC_VOLUME_DEFAULT,
  DAC_VOLUME_DEFAULT,
  getDefaultRadioConfig,
  hzToMHz,
  levelToBandwidth,
  levelToPower,
  mhzToHz,
  normalizeSquelchLevel,
  normalizeAdcGainDb,
  normalizeAudioVolume,
  normalizeActiveHigh,
  powerToLevel,
  toneSelectionToLegacyValue,
  toneSelectionToToneValue,
  type RadioConfigForm,
} from '../../utils/radioConfig'
import { getErrorMessage } from '../../utils/errorMessage'
import { FrequencyConfigCardContainer } from './frequency/FrequencyConfigCardContainer'
import { AudioConfigCard } from './system/AudioConfigCard'

interface ParamConfigDialogProps {
  open: boolean
  deviceId: number | undefined
  deviceName: string
  deviceModel: number
  isOnline: boolean
  onClose: () => void
  onDeviceUpdated?: () => void
}

export function ParamConfigDialog({
  open,
  deviceId,
  deviceName,
  deviceModel: reportedDeviceModel,
  isOnline,
  onClose,
  onDeviceUpdated,
}: ParamConfigDialogProps) {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))
  const defaultRadioConfig = useRef(getDefaultRadioConfig())
  const loadRequestRef = useRef(0)
  const [tabValue, setTabValue] = useState(0)
  const [deviceModel, setDeviceModel] = useState(reportedDeviceModel)
  const [loading, setLoading] = useState(false)
  const [configLoaded, setConfigLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' | 'info' }>({
    open: false,
    message: '',
    severity: 'info',
  })
  const [radioConfig, setRadioConfig] = useState<RadioConfigForm>(defaultRadioConfig.current)
  const [platformFormData, setPlatformFormData] = useState({
    name: '',
  })

  const tabs = getDeviceConfigTabs(deviceModel)
  const currentTab = tabs[tabValue]?.key ?? 'platform'

  useEffect(() => {
    if (tabValue >= tabs.length) {
      setTabValue(0)
    }
  }, [tabValue, tabs.length])

  const loadConfig = useCallback(async (currentDeviceId: number, requestId: number) => {
    setLoading(true)
    setConfigLoaded(false)
    try {
      const snapshot = await deviceService.getConfigSnapshot(currentDeviceId)
      if (loadRequestRef.current !== requestId) {
        return
      }
      if (snapshot.devModel !== undefined) setDeviceModel(snapshot.devModel)
      const config = snapshot.configs
      const rxFreqMHz = hzToMHz(config.rx_freq)
      const txFreqMHz = hzToMHz(config.tx_freq)
      setRadioConfig({
        txFreq: txFreqMHz,
        rxFreq: rxFreqMHz,
        txTone: buildToneSelection({
          mode: config.tx_tone_mode,
          value: config.tx_tone_value,
        }),
        rxTone: buildToneSelection({
          mode: config.rx_tone_mode,
          value: config.rx_tone_value,
        }),
        squelch: normalizeSquelchLevel(Number(config.sql_level || '0')),
        sameFreq: !rxFreqMHz || !txFreqMHz || rxFreqMHz === txFreqMHz,
        power: levelToPower(config.power_level),
        bandwidth: levelToBandwidth(config.tx_bandwidth),
        adcGainDb: normalizeAdcGainDb(Number(config.adc_gain_db)),
        adcVolume: normalizeAudioVolume(Number(config.adc_volume), ADC_VOLUME_DEFAULT),
        dacVolume: normalizeAudioVolume(Number(config.dac_volume), DAC_VOLUME_DEFAULT),
        sqlActiveHigh: normalizeActiveHigh(config.sql_active_high),
        pttActiveHigh: normalizeActiveHigh(config.ptt_active_high),
      })
      setConfigLoaded(true)
    } catch (error) {
      if (loadRequestRef.current !== requestId) {
        return
      }
      console.error('加载配置失败:', error)
      setRadioConfig(defaultRadioConfig.current)
      setSnackbar({ open: true, message: getErrorMessage(error, '加载配置失败'), severity: 'error' })
    } finally {
      if (loadRequestRef.current === requestId) {
        setLoading(false)
      }
    }
  }, [])

  useEffect(() => {
    if (!open) {
      loadRequestRef.current += 1
      setLoading(false)
      setSaving(false)
      setTabValue(0)
      setRadioConfig(defaultRadioConfig.current)
      setPlatformFormData({
        name: '',
      })
      return
    }

    setPlatformFormData({
      name: deviceName,
    })
    setDeviceModel(reportedDeviceModel)
    setRadioConfig(defaultRadioConfig.current)

    if (deviceId) {
      const requestId = loadRequestRef.current + 1
      loadRequestRef.current = requestId
      void loadConfig(deviceId, requestId)
    } else {
      setLoading(false)
    }
  }, [open, deviceId, deviceName, reportedDeviceModel, loadConfig])

  const saveAndSyncConfig = async (config: Partial<DeviceConfig>) => {
    if (!deviceId || !configLoaded || loading) return
    setSaving(true)
    try {
      await deviceService.updateConfig(deviceId, config)

      if (isOnline) {
        const result = await deviceService.syncConfig(deviceId)
        setSnackbar({ open: true, message: result.message || '配置已下发到设备', severity: 'success' })
      } else {
        setSnackbar({ open: true, message: '配置已保存，设备上线后将自动同步', severity: 'success' })
      }

      onClose()
    } catch (error) {
      console.error('保存配置失败:', error)
      setSnackbar({ open: true, message: getErrorMessage(error, '保存配置失败'), severity: 'error' })
      if (isAxiosError(error) && error.response?.status === 409) {
        const requestId = ++loadRequestRef.current
        setTabValue(0)
        await loadConfig(deviceId, requestId)
      }
    } finally {
      setSaving(false)
    }
  }

  const handleSaveFrequencyAndSync = async () => {
    await saveAndSyncConfig({
      tx_freq: mhzToHz(radioConfig.txFreq),
      rx_freq: radioConfig.sameFreq ? mhzToHz(radioConfig.txFreq) : mhzToHz(radioConfig.rxFreq),
      tx_tone_mode: radioConfig.txTone.mode,
      tx_tone_value: toneSelectionToToneValue(radioConfig.txTone),
      rx_tone_mode: radioConfig.rxTone.mode,
      rx_tone_value: toneSelectionToToneValue(radioConfig.rxTone),
      tx_ctcss: toneSelectionToLegacyValue(radioConfig.txTone),
      rx_ctcss: toneSelectionToLegacyValue(radioConfig.rxTone),
      sql_level: String(normalizeSquelchLevel(radioConfig.squelch)),
      power_level: powerToLevel(radioConfig.power),
      tx_bandwidth: bandwidthToLevel(radioConfig.bandwidth),
    })
  }

  const handleSaveAudioAndSync = async () => {
    await saveAndSyncConfig({
      adc_gain_db: String(normalizeAdcGainDb(radioConfig.adcGainDb)),
      adc_volume: String(normalizeAudioVolume(radioConfig.adcVolume, ADC_VOLUME_DEFAULT)),
      dac_volume: String(normalizeAudioVolume(radioConfig.dacVolume, DAC_VOLUME_DEFAULT)),
      ...(supportsBoardIOPolarityConfig(deviceModel) ? {
        sql_active_high: radioConfig.sqlActiveHigh ? '1' : '0',
        ptt_active_high: radioConfig.pttActiveHigh ? '1' : '0',
      } : {}),
    })
  }

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
      })
      setSnackbar({ open: true, message: '设备信息保存成功', severity: 'success' })
      onDeviceUpdated?.()
      onClose()
    } catch (error) {
      console.error('保存设备信息失败:', error)
      setSnackbar({ open: true, message: getErrorMessage(error, '保存失败'), severity: 'error' })
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <Dialog
        open={open}
        onClose={(_, reason) => (reason === 'backdropClick' ? null : onClose())}
        maxWidth="md"
        fullWidth
        fullScreen={isMobile}
      >
      <DialogTitle sx={{ pb: 0 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span>{tabs[tabValue]?.label || '参数配置'} - {deviceName}</span>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexShrink: 0 }}>
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
        <Tabs value={tabValue} onChange={(_, value) => setTabValue(value)} variant="fullWidth" aria-label="设备配置分类" sx={{ '& .MuiTab-root': { minWidth: 0, px: 1 } }}>
          {tabs.map((tab) => (
            <Tab key={tab.key} label={tab.label} />
          ))}
        </Tabs>
      </Box>

      <DialogContent>
        <Box sx={{ py: 2 }}>
          {!loading && !configLoaded && currentTab !== 'platform' && (
            <Alert severity="error" sx={{ mb: 2 }} action={
              <Button color="inherit" onClick={() => {
                if (deviceId) void loadConfig(deviceId, ++loadRequestRef.current)
              }}>重新加载</Button>
            }>未能读取设备配置，请重新加载后保存。</Alert>
          )}
          {!isOnline && (
            <Alert severity="info" sx={{ mb: 2 }}>
              设备当前离线，配置将保存到服务器，设备上线后自动同步。
            </Alert>
          )}

          {currentTab === 'freq' && (
            <FrequencyConfigCardContainer
              devModel={deviceModel}
              value={radioConfig}
              onChange={setRadioConfig}
            />
          )}

          {currentTab === 'system' && (
            supportsAudioConfig(deviceModel) ? (
              <AudioConfigCard
                value={radioConfig}
                onChange={setRadioConfig}
                showIOPolarity={supportsBoardIOPolarityConfig(deviceModel)}
              />
            ) : (
              <Alert severity="info">当前设备型号暂无可配置的系统参数。</Alert>
            )
          )}

          {currentTab === 'platform' && (
            <Grid container spacing={3}>
              <Grid size={12}>
                <TextField
                  fullWidth
                  label="设备名称"
                  value={platformFormData.name}
                  onChange={(event) => setPlatformFormData((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder="请输入设备名称"
                />
              </Grid>
              <Grid size={12}>
                <TextField
                  fullWidth
                  label="设备型号（只读）"
                  value={getDevModelName(deviceModel)}
                  slotProps={{ input: { readOnly: true } }}
                />
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                  设备型号由客户端上报，Web 端不可修改。
                </Typography>
              </Grid>
            </Grid>
          )}
        </Box>
      </DialogContent>

      <DialogActions>
        <Button onClick={onClose}>取消</Button>
        {currentTab === 'platform' && (
          <Button
            variant="contained"
            onClick={handleSavePlatform}
            disabled={saving || loading}
            startIcon={saving ? <CircularProgress size={20} /> : null}
          >
            {saving ? '保存中...' : '保存'}
          </Button>
        )}
        {currentTab === 'freq' && (
          <Button
            variant="contained"
            onClick={handleSaveFrequencyAndSync}
            disabled={saving || loading || !configLoaded}
            startIcon={saving ? <CircularProgress size={20} /> : null}
          >
            {saving ? '保存中...' : (isOnline ? '保存并同步' : '保存配置')}
          </Button>
        )}
        {currentTab === 'system' && supportsAudioConfig(deviceModel) && (
          <Button
            variant="contained"
            onClick={handleSaveAudioAndSync}
            disabled={saving || loading || !configLoaded}
            startIcon={saving ? <CircularProgress size={20} /> : null}
          >
            {saving ? '保存中...' : (isOnline ? '保存并同步' : '保存配置')}
          </Button>
        )}
      </DialogActions>

      </Dialog>

      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </>
  )
}
