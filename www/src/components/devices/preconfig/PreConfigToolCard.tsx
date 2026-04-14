import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Divider,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  Grid,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
} from '@mui/material'
import BluetoothSearching from '@mui/icons-material/BluetoothSearching'
import BluetoothDisabled from '@mui/icons-material/BluetoothDisabled'
import Refresh from '@mui/icons-material/Refresh'
import LockOpen from '@mui/icons-material/LockOpen'
import Hardware from '@mui/icons-material/Hardware'
import type { BleProvisionServerConfig, BleProvisionWifiNetwork } from '../../../services/bleProvision'
import { useBleProvisioning } from '../../../hooks/useBleProvisioning'
import { getErrorMessage } from '../../../utils/errorMessage'
import { createServerFormValue, ProvisionDraarlForm } from './ProvisionDraarlForm'
import {
  createWifiFormValue,
  ProvisionWifiForm,
  resolveWifiFormValue,
  syncWifiFormWithNetworks,
  type ProvisionWifiFormValue,
} from './ProvisionWifiForm'
import { getProvisionDeviceProfile, PROVISION_DEVICE_PROFILES } from './deviceProfiles'
import { ProvisionStatusChips } from './ProvisionStatusChips'

interface PreConfigToolCardProps {
  open: boolean
  onClose: () => void
}

type BusyAction =
  | 'connect'
  | 'disconnect'
  | 'refresh'
  | 'auth'
  | 'loadConfig'
  | 'saveWifi'
  | 'saveServer'
  | null

const WIFI_SCAN_DEFAULT_HINT = '请直接手动输入 2.4G 网络的 SSID 和密码。'

function isChromeBrowser() {
  if (typeof navigator === 'undefined') {
    return false
  }

  const ua = navigator.userAgent
  const vendor = navigator.vendor || ''
  const isChromiumUA = /Chrome|CriOS|Edg\//.test(ua)
  const isGoogleVendor = vendor.includes('Google') || /Edg\//.test(ua)
  const isExcluded = /Firefox|Safari\//.test(ua) && !/Chrome|CriOS|Edg\//.test(ua)

  return isChromiumUA && isGoogleVendor && !isExcluded
}

export function PreConfigToolCard({ open, onClose }: PreConfigToolCardProps) {
  const {
    supported,
    status,
    connect,
    disconnect,
    refreshStatus,
    authenticate,
    loadConfig,
    saveWifi,
    saveServer,
  } = useBleProvisioning()

  const [deviceType, setDeviceType] = useState('')
  const [dynamicCode, setDynamicCode] = useState('')
  const [wifiNetworks, setWifiNetworks] = useState<BleProvisionWifiNetwork[]>([])
  const [wifiForm, setWifiForm] = useState<ProvisionWifiFormValue>(() => createWifiFormValue())
  const [serverForm, setServerForm] = useState<BleProvisionServerConfig>(() => createServerFormValue())
  const [busyAction, setBusyAction] = useState<BusyAction>(null)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [wifiHelperText, setWifiHelperText] = useState(WIFI_SCAN_DEFAULT_HINT)

  const selectedProfile = useMemo(() => getProvisionDeviceProfile(deviceType), [deviceType])
  const preferChrome = isChromeBrowser()

  useEffect(() => {
    if (!open) {
      setDeviceType('')
      setDynamicCode('')
      setWifiNetworks([])
      setWifiForm(createWifiFormValue())
      setServerForm(createServerFormValue())
      setBusyAction(null)
      setError('')
      setSuccess('')
      setWifiHelperText(WIFI_SCAN_DEFAULT_HINT)
      void disconnect().catch(() => undefined)
    }
  }, [open])

  const handleClose = () => {
    onClose()
  }

  const runAction = async (action: BusyAction, fn: () => Promise<void>) => {
    setBusyAction(action)
    setError('')
    setSuccess('')
    try {
      await fn()
    } catch (actionError) {
      setError(getErrorMessage(actionError, '操作失败'))
    } finally {
      setBusyAction(null)
    }
  }

  const applyLoadedConfig = (nextWifiForm: ProvisionWifiFormValue, nextServerForm: BleProvisionServerConfig) => {
    setWifiForm(syncWifiFormWithNetworks(nextWifiForm, wifiNetworks))
    setServerForm(nextServerForm)
  }

  const handleLoadConfig = async () => {
    await runAction('loadConfig', async () => {
      const config = await loadConfig()
      applyLoadedConfig(createWifiFormValue(config.wifi), createServerFormValue(config.server))
      setSuccess('已从设备读取当前配置')
    })
  }

  const handleConnect = async () => {
    await runAction('connect', async () => {
      await connect()
      setSuccess('BLE 设备连接成功')
    })
  }

  const handleAuthenticate = async () => {
    const normalizedCode = dynamicCode.trim()
    if (!/^\d{6}$/.test(normalizedCode)) {
      setError('请输入 6 位动态码')
      return
    }

    await runAction('auth', async () => {
      await authenticate(normalizedCode)
      const config = await loadConfig()
      applyLoadedConfig(createWifiFormValue(config.wifi), createServerFormValue(config.server))
      setSuccess('动态码认证成功，已自动读取设备配置')
    })
  }

  const handleSaveWifi = async () => {
    const payload = resolveWifiFormValue(wifiForm)
    if (!payload.ssid) {
      setError('请先填写 WiFi SSID')
      return
    }
    if (!payload.dhcp && (!payload.ip || !payload.gateway || !payload.subnet)) {
      setError('静态 IP 模式下请至少填写 IP、网关和子网掩码')
      return
    }

    await runAction('saveWifi', async () => {
      await saveWifi(payload)
      setSuccess('WiFi 配置已发送到设备')
    })
  }

  const handleSaveServer = async () => {
    if (!serverForm.account.trim()) {
      setError('请填写 DraARL 账号')
      return
    }
    if (!serverForm.deviceAuthPassword.trim()) {
      setError('请填写设备认证密码')
      return
    }

    await runAction('saveServer', async () => {
      await saveServer(serverForm)
      setSuccess('DraARL 配置已发送到设备')
    })
  }

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      maxWidth="lg"
      fullWidth
      scroll="paper"
    >
      <DialogTitle>预配置工具</DialogTitle>
      <DialogContent dividers>
        <Card variant="outlined" sx={{ borderRadius: 2 }}>
          <CardContent sx={{ p: 3 }}>
            <Stack spacing={3}>
              <Alert severity={preferChrome ? 'info' : 'warning'}>
                请使用 Chrome 浏览器打开本工具。BLE 连接依赖 Chromium 的 Web Bluetooth 能力，请直接填写设备要连接的 2.4G WiFi 信息。
              </Alert>

            <Box sx={{ display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', gap: 2 }}>
              <Box>
                <Typography variant="body2" color="text.secondary">
                  将 ESP32 预配置流程集成到当前前端，只保留 BLE 认证与配置读写能力。
                </Typography>
              </Box>
              <Chip
                icon={<Hardware />}
                label={selectedProfile ? `当前设备类型: ${selectedProfile.label}` : '请先选择设备类型'}
                color={selectedProfile ? 'primary' : 'default'}
                variant={selectedProfile ? 'filled' : 'outlined'}
              />
            </Box>

            {!supported && (
              <Alert severity="warning">
                当前浏览器不支持 Web Bluetooth，请使用 Chromium 内核浏览器，并通过 HTTPS 或 localhost 打开此页面。
              </Alert>
            )}

            {error && <Alert severity="error" onClose={() => setError('')}>{error}</Alert>}
            {success && <Alert severity="success" onClose={() => setSuccess('')}>{success}</Alert>}

            <Grid container spacing={2}>
              <Grid size={{ xs: 12, md: 4 }}>
                <FormControl fullWidth size="small">
                  <Typography variant="body2" color="text.secondary" sx={{ mb: 0.75 }}>
                    设备类型
                  </Typography>
                  <Select
                    value={deviceType}
                    displayEmpty
                    onChange={(event) => {
                      setDeviceType(event.target.value)
                      setError('')
                      setSuccess('')
                    }}
                  >
                    <MenuItem value="" disabled>
                      请选择设备类型
                    </MenuItem>
                    {PROVISION_DEVICE_PROFILES.map((profile) => (
                      <MenuItem key={profile.key} value={profile.key}>
                        {profile.key} · {profile.label}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </Grid>
              <Grid size={{ xs: 12, md: 8 }}>
                <Alert severity="info">
                  {selectedProfile?.description || '当前先开放 devmodel1，即 ESP32 链路盒子（1W 射频版）。'}
                </Alert>
              </Grid>
            </Grid>

            <Divider />

            <Stack spacing={2}>
              <Box sx={{ display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', gap: 2 }}>
                <Box>
                  <Typography variant="subtitle1">BLE 连接与动态码认证</Typography>
                  <Typography variant="body2" color="text.secondary">
                    顶部流程沿用动态码认证，认证通过后可读取并保存配置；WiFi 请直接手动填写。
                  </Typography>
                </Box>
                <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                  <Button
                    variant="contained"
                    size="small"
                    startIcon={<BluetoothSearching />}
                    onClick={handleConnect}
                    disabled={!selectedProfile || !supported || busyAction !== null}
                  >
                    连接设备
                  </Button>
                  <Button
                    variant="outlined"
                    size="small"
                    startIcon={<BluetoothDisabled />}
                    onClick={() => runAction('disconnect', disconnect)}
                    disabled={!status.connected || busyAction !== null}
                  >
                    断开连接
                  </Button>
                  <Button
                    variant="outlined"
                    size="small"
                    startIcon={<Refresh />}
                    onClick={() => runAction('refresh', async () => {
                      await refreshStatus()
                      setSuccess('状态已刷新')
                    })}
                    disabled={!status.connected || busyAction !== null}
                  >
                    刷新状态
                  </Button>
                </Stack>
              </Box>

              <ProvisionStatusChips status={status} />

              <Grid container spacing={2} alignItems="flex-end">
                <Grid size={{ xs: 12, md: 4 }}>
                  <TextField
                    fullWidth
                    label="动态码"
                    placeholder="输入 6 位动态码"
                    value={dynamicCode}
                    onChange={(event) => setDynamicCode(event.target.value.replace(/\D/g, '').slice(0, 6))}
                    disabled={!status.connected || busyAction !== null}
                  />
                </Grid>
                <Grid size={{ xs: 12, md: 8 }}>
                  <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                    <Button
                      variant="contained"
                      startIcon={<LockOpen />}
                      onClick={handleAuthenticate}
                      disabled={!status.connected || busyAction !== null}
                    >
                      提交认证
                    </Button>
                    <Button
                      variant="outlined"
                      onClick={handleLoadConfig}
                      disabled={!status.authenticated || busyAction !== null}
                    >
                      读取配置
                    </Button>
                  </Stack>
                </Grid>
              </Grid>
            </Stack>

            {selectedProfile && (
              <Stack spacing={2}>
                {selectedProfile.supportsWifi && (
                  <ProvisionWifiForm
                    value={wifiForm}
                    networks={wifiNetworks}
                    disabled={!status.authenticated}
                    loading={busyAction === 'saveWifi'}
                    helperText={wifiHelperText}
                    onChange={setWifiForm}
                    onSave={handleSaveWifi}
                  />
                )}

                {selectedProfile.supportsDraarl && (
                  <ProvisionDraarlForm
                    value={serverForm}
                    disabled={!status.authenticated}
                    loading={busyAction === 'saveServer'}
                    onChange={setServerForm}
                    onSave={handleSaveServer}
                  />
                )}
              </Stack>
            )}
            </Stack>
          </CardContent>
        </Card>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose}>关闭</Button>
      </DialogActions>
    </Dialog>
  )
}
