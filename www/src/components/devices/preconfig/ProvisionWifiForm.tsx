import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  FormControl,
  Grid,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
} from '@mui/material'
import Save from '@mui/icons-material/Save'
import type { BleProvisionWifiConfig, BleProvisionWifiNetwork } from '../../../services/bleProvision'

export const MANUAL_SSID_VALUE = '__manual__'

export interface ProvisionWifiFormValue extends BleProvisionWifiConfig {
  ssidMode: string
  manualSsid: string
}

interface ProvisionWifiFormProps {
  value: ProvisionWifiFormValue
  networks: BleProvisionWifiNetwork[]
  disabled: boolean
  loading: boolean
  helperText: string
  onChange: (value: ProvisionWifiFormValue) => void
  onSave: () => void
}

export function createWifiFormValue(config?: Partial<BleProvisionWifiConfig>): ProvisionWifiFormValue {
  const ssid = String(config?.ssid || '')
  return {
    ssid,
    password: String(config?.password || ''),
    dhcp: config?.dhcp !== false,
    ip: String(config?.ip || ''),
    gateway: String(config?.gateway || ''),
    subnet: String(config?.subnet || ''),
    dns1: String(config?.dns1 || ''),
    dns2: String(config?.dns2 || ''),
    ssidMode: ssid ? MANUAL_SSID_VALUE : '',
    manualSsid: ssid,
  }
}

export function resolveWifiFormValue(value: ProvisionWifiFormValue): BleProvisionWifiConfig {
  return {
    ssid: value.ssidMode === MANUAL_SSID_VALUE ? value.manualSsid.trim() : value.ssidMode.trim(),
    password: value.password,
    dhcp: value.dhcp,
    ip: value.ip.trim(),
    gateway: value.gateway.trim(),
    subnet: value.subnet.trim(),
    dns1: value.dns1.trim(),
    dns2: value.dns2.trim(),
  }
}

export function syncWifiFormWithNetworks(
  value: ProvisionWifiFormValue,
  networks: BleProvisionWifiNetwork[]
): ProvisionWifiFormValue {
  const ssid = value.ssidMode === MANUAL_SSID_VALUE ? value.manualSsid.trim() : value.ssidMode.trim()
  const hasPreset = networks.some((item) => item.ssid === ssid)

  return {
    ...value,
    ssidMode: ssid && hasPreset ? ssid : MANUAL_SSID_VALUE,
    manualSsid: ssid,
  }
}

export function ProvisionWifiForm({
  value,
  networks,
  disabled,
  loading,
  helperText,
  onChange,
  onSave,
}: ProvisionWifiFormProps) {
  const handleFieldChange = <K extends keyof ProvisionWifiFormValue>(key: K, fieldValue: ProvisionWifiFormValue[K]) => {
    onChange({
      ...value,
      [key]: fieldValue,
    })
  }

  const staticDisabled = disabled || value.dhcp

  return (
    <Card variant="outlined">
      <CardHeader
        title="WiFi"
        subheader="WiFi 为必填项，请直接填写网络名称与密码。"
        action={
          <Stack direction="row" spacing={1}>
            <Button
              variant="contained"
              size="small"
              startIcon={<Save />}
              onClick={onSave}
              disabled={disabled || loading}
            >
              保存 WiFi
            </Button>
          </Stack>
        }
      />
      <CardContent sx={{ pt: 0 }}>
        <Stack spacing={2}>
          <Alert severity="info">{helperText}</Alert>

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 4 }}>
              <Stack spacing={2}>
                <TextField
                  fullWidth
                  label="SSID"
                  value={value.manualSsid}
                  onChange={(event) => {
                    handleFieldChange('ssidMode', MANUAL_SSID_VALUE)
                    handleFieldChange('manualSsid', event.target.value)
                  }}
                  disabled={disabled}
                  helperText="请输入 2.4G WiFi 名称"
                />
                <TextField
                  fullWidth
                  label="PASSWORD"
                  type="password"
                  value={value.password}
                  onChange={(event) => handleFieldChange('password', event.target.value)}
                  disabled={disabled}
                />
                <FormControl fullWidth size="small">
                  <Typography variant="body2" color="text.secondary" sx={{ mb: 0.75 }}>
                    MODEL
                  </Typography>
                  <Select
                    value={value.dhcp ? 'dhcp' : 'static'}
                    disabled={disabled}
                    onChange={(event) => handleFieldChange('dhcp', event.target.value === 'dhcp')}
                  >
                    <MenuItem value="dhcp">DHCP</MenuItem>
                    <MenuItem value="static">STATIC</MenuItem>
                  </Select>
                </FormControl>
              </Stack>
            </Grid>
            <Grid size={{ xs: 12, md: 8 }}>
              <Box sx={{ opacity: staticDisabled ? 0.6 : 1 }}>
                <Grid container spacing={2}>
                  <Grid size={{ xs: 12, md: 4 }}>
                    <TextField
                      fullWidth
                      label="IP"
                      value={value.ip}
                      onChange={(event) => handleFieldChange('ip', event.target.value)}
                      disabled={staticDisabled}
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 4 }}>
                    <TextField
                      fullWidth
                      label="网关"
                      value={value.gateway}
                      onChange={(event) => handleFieldChange('gateway', event.target.value)}
                      disabled={staticDisabled}
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 4 }}>
                    <TextField
                      fullWidth
                      label="子网掩码"
                      value={value.subnet}
                      onChange={(event) => handleFieldChange('subnet', event.target.value)}
                      disabled={staticDisabled}
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 6 }}>
                    <TextField
                      fullWidth
                      label="DNS1"
                      value={value.dns1}
                      onChange={(event) => handleFieldChange('dns1', event.target.value)}
                      disabled={staticDisabled}
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 6 }}>
                    <TextField
                      fullWidth
                      label="DNS2"
                      value={value.dns2}
                      onChange={(event) => handleFieldChange('dns2', event.target.value)}
                      disabled={staticDisabled}
                    />
                  </Grid>
                </Grid>
              </Box>
            </Grid>
          </Grid>
        </Stack>
      </CardContent>
    </Card>
  )
}
