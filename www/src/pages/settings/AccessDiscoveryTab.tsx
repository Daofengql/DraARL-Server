import type { Dispatch, SetStateAction } from 'react'
import {
  Box, Button, Card, CardContent, Divider, FormControlLabel, InputAdornment, Switch, TextField, Typography,
} from '@mui/material'
import Save from '@mui/icons-material/Save'
import { TabPanel } from '../../components/common/TabPanel'
import type { AccessDiscoveryConfig } from './types'

interface AccessDiscoveryTabProps {
  value: number
  config: AccessDiscoveryConfig
  setConfig: Dispatch<SetStateAction<AccessDiscoveryConfig>>
  loading: boolean
  onSave: () => void
}

export function AccessDiscoveryTab({ value, config, setConfig, loading, onSave }: AccessDiscoveryTabProps) {
  const setCenter = (changes: Partial<AccessDiscoveryConfig['center']>) => {
    setConfig({ ...config, center: { ...config.center, ...changes } })
  }

  return (
    <TabPanel value={value} index={1}>
      <Box sx={{ px: 2, maxWidth: 760 }}>
        <Card>
          <CardContent>
            <Typography variant="h6" gutterBottom>设备接入点发现</Typography>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, minmax(0, 1fr))' }, gap: 2 }}>
                <TextField
                  label="发现凭证有效期" type="number" value={config.token_ttl_seconds}
                  onChange={(event) => setConfig({ ...config, token_ttl_seconds: Number(event.target.value) })}
                  inputProps={{ min: 1, max: 300 }}
                  InputProps={{ endAdornment: <InputAdornment position="end">秒</InputAdornment> }}
                />
                <TextField
                  label="边缘健康有效期" type="number" value={config.edge_health_ttl_seconds}
                  onChange={(event) => setConfig({ ...config, edge_health_ttl_seconds: Number(event.target.value) })}
                  inputProps={{ min: 1, max: 300 }}
                  InputProps={{ endAdornment: <InputAdornment position="end">秒</InputAdornment> }}
                />
                <TextField
                  label="客户端缓存时间" type="number" value={config.cache_max_age_seconds}
                  onChange={(event) => setConfig({ ...config, cache_max_age_seconds: Number(event.target.value) })}
                  inputProps={{ min: 1, max: 30 }}
                  InputProps={{ endAdornment: <InputAdornment position="end">秒</InputAdornment> }}
                />
              </Box>

              <Divider />
              <FormControlLabel
                control={<Switch checked={config.center.enabled} onChange={(event) => setCenter({ enabled: event.target.checked })} />}
                label="发布中心直连入口"
              />
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
                <TextField
                  label="公开 ID" value={config.center.public_id}
                  onChange={(event) => setCenter({ public_id: event.target.value })}
                  disabled={!config.center.enabled}
                />
                <TextField
                  label="显示名称" value={config.center.display_name}
                  onChange={(event) => setCenter({ display_name: event.target.value })}
                  disabled={!config.center.enabled}
                />
                <TextField
                  label="公网 UDP 地址" value={config.center.udp_host}
                  onChange={(event) => setCenter({ udp_host: event.target.value })}
                  placeholder="radio.example.com" disabled={!config.center.enabled}
                />
                <TextField
                  label="公网 UDP 端口" type="number" value={config.center.udp_port}
                  onChange={(event) => setCenter({ udp_port: Number(event.target.value) })}
                  inputProps={{ min: 1, max: 65535 }} disabled={!config.center.enabled}
                />
                <TextField
                  label="地区" value={config.center.region}
                  onChange={(event) => setCenter({ region: event.target.value })}
                  placeholder="福建省 福州市" disabled={!config.center.enabled}
                />
                <TextField
                  label="网络标签" value={config.center.network}
                  onChange={(event) => setCenter({ network: event.target.value })}
                  disabled={!config.center.enabled}
                />
                <TextField
                  label="优先级" type="number" value={config.center.priority}
                  onChange={(event) => setCenter({ priority: Number(event.target.value) })}
                  disabled={!config.center.enabled}
                />
              </Box>
              <Box>
                <Button variant="contained" startIcon={<Save />} onClick={onSave} disabled={loading}>
                  保存接入点配置
                </Button>
              </Box>
            </Box>
          </CardContent>
        </Card>
      </Box>
    </TabPanel>
  )
}
