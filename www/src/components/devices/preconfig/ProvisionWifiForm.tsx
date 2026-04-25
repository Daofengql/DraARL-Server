import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Grid,
  MenuItem,
  Stack,
  TextField,
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

          <Grid container spacing={2} alignItems="flex-start">
            <Grid size={{ xs: 12, sm: 4 }}>
              <TextField
                fullWidth
                select
                size="small"
                label="MODEL"
                value={value.dhcp ? 'dhcp' : 'static'}
                disabled={disabled}
                onChange={(event) => handleFieldChange('dhcp', event.target.value === 'dhcp')}
              >
                <MenuItem value="dhcp">DHCP</MenuItem>
                <MenuItem value="static">STATIC</MenuItem>
              </TextField>
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <TextField
                fullWidth
                size="small"
                label="SSID"
                placeholder="请输入 2.4G WiFi 名称"
                value={value.manualSsid}
                onChange={(event) => {
                  handleFieldChange('ssidMode', MANUAL_SSID_VALUE)
                  handleFieldChange('manualSsid', event.target.value)
                }}
                disabled={disabled}
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <TextField
                fullWidth
                size="small"
                label="PASSWORD"
                type="password"
                value={value.password}
                onChange={(event) => handleFieldChange('password', event.target.value)}
                disabled={disabled}
              />
            </Grid>

            {!value.dhcp && (
              <>
                <Grid size={{ xs: 12, sm: 4 }}>
                  <TextField
                    fullWidth
                    size="small"
                    label="IP"
                    value={value.ip}
                    onChange={(event) => handleFieldChange('ip', event.target.value)}
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 4 }}>
                  <TextField
                    fullWidth
                    size="small"
                    label="网关"
                    value={value.gateway}
                    onChange={(event) => handleFieldChange('gateway', event.target.value)}
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 4 }}>
                  <TextField
                    fullWidth
                    size="small"
                    label="子网掩码"
                    value={value.subnet}
                    onChange={(event) => handleFieldChange('subnet', event.target.value)}
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 6 }}>
                  <TextField
                    fullWidth
                    size="small"
                    label="DNS1"
                    value={value.dns1}
                    onChange={(event) => handleFieldChange('dns1', event.target.value)}
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 6 }}>
                  <TextField
                    fullWidth
                    size="small"
                    label="DNS2"
                    value={value.dns2}
                    onChange={(event) => handleFieldChange('dns2', event.target.value)}
                    disabled={disabled}
                  />
                </Grid>
              </>
            )}
          </Grid>
        </Stack>
      </CardContent>
    </Card>
  )
}
