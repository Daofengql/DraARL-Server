import {
  Button,
  Card,
  CardContent,
  CardHeader,
  Grid,
  Stack,
  TextField,
} from '@mui/material'
import Save from '@mui/icons-material/Save'
import type { BleProvisionServerConfig } from '../../../services/bleProvision'

interface ProvisionDraarlFormProps {
  value: BleProvisionServerConfig
  disabled: boolean
  loading: boolean
  onChange: (value: BleProvisionServerConfig) => void
  onSave: () => void
}

export function createServerFormValue(config?: Partial<BleProvisionServerConfig>): BleProvisionServerConfig {
  return {
    callsign: String(config?.callsign || ''),
    nodeSsid: Number(config?.nodeSsid ?? 0),
    udpHost: String(config?.udpHost || ''),
    udpPort: Number(config?.udpPort ?? 0),
    httpApiBaseUrl: String(config?.httpApiBaseUrl || ''),
    account: String(config?.account || ''),
    deviceAuthPassword: String(config?.deviceAuthPassword || ''),
  }
}

export function ProvisionDraarlForm({
  value,
  disabled,
  loading,
  onChange,
  onSave,
}: ProvisionDraarlFormProps) {
  const handleFieldChange = <K extends keyof BleProvisionServerConfig>(key: K, fieldValue: BleProvisionServerConfig[K]) => {
    onChange({
      ...value,
      [key]: fieldValue,
    })
  }

  return (
    <Card variant="outlined">
      <CardHeader
        title="DraARL"
        subheader="保留设备端现有的 DraARL 服务器配置读写交互。"
        action={
          <Stack direction="row" spacing={1}>
            <Button
              variant="contained"
              size="small"
              startIcon={<Save />}
              onClick={onSave}
              disabled={disabled || loading}
            >
              保存 DraARL
            </Button>
          </Stack>
        }
      />
      <CardContent sx={{ pt: 0 }}>
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 3 }}>
            <TextField
              fullWidth
              label="Callsign"
              value={value.callsign}
              onChange={(event) => handleFieldChange('callsign', event.target.value)}
              disabled={disabled}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 3 }}>
            <TextField
              fullWidth
              label="Node SSID"
              type="number"
              value={value.nodeSsid}
              onChange={(event) => handleFieldChange('nodeSsid', Number(event.target.value || 0))}
              disabled={disabled}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 3 }}>
            <TextField
              fullWidth
              label="UDP Host"
              value={value.udpHost}
              onChange={(event) => handleFieldChange('udpHost', event.target.value)}
              disabled={disabled}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 3 }}>
            <TextField
              fullWidth
              label="UDP Port"
              type="number"
              value={value.udpPort}
              onChange={(event) => handleFieldChange('udpPort', Number(event.target.value || 0))}
              disabled={disabled}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <TextField
              fullWidth
              label="HTTP API BaseURL"
              value={value.httpApiBaseUrl}
              onChange={(event) => handleFieldChange('httpApiBaseUrl', event.target.value)}
              disabled={disabled}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <TextField
              fullWidth
              label="账号"
              value={value.account}
              onChange={(event) => handleFieldChange('account', event.target.value)}
              disabled={disabled}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <TextField
              fullWidth
              label="设备认证密码"
              type="password"
              value={value.deviceAuthPassword}
              onChange={(event) => handleFieldChange('deviceAuthPassword', event.target.value)}
              disabled={disabled}
            />
          </Grid>
        </Grid>
      </CardContent>
    </Card>
  )
}
