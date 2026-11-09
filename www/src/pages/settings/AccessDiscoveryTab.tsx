import type { Dispatch, SetStateAction } from 'react'
import {
  Box, Button, Card, CardContent, TextField, Typography,
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
  const setEndpoint = (changes: Partial<AccessDiscoveryConfig>) => {
    setConfig({ ...config, ...changes })
  }

  return (
    <TabPanel value={value} index={1}>
      <Box sx={{ px: 2, maxWidth: 760 }}>
        <Card>
          <CardContent>
            <Typography component="h2" variant="h6" gutterBottom>设备接入点发现</Typography>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
              <Typography variant="body2" color="text.secondary">
                设备配置本站 HTTP API 地址后，会查询本站节点信息并连接 UDP。公网地址或端口有映射时，请填写设备实际可访问的入口。
              </Typography>
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
                <TextField
                  label="节点 ID" value={config.node_id}
                  onChange={(event) => setEndpoint({ node_id: event.target.value })}
                />
                <TextField
                  label="节点 IP／域名" value={config.udp_host}
                  onChange={(event) => setEndpoint({ udp_host: event.target.value })}
                  placeholder="radio.example.com" helperText="留空时使用设备查询 HTTP API 的主机名"
                />
                <TextField
                  label="节点 UDP 端口" type="number" value={config.udp_port}
                  onChange={(event) => setEndpoint({ udp_port: Number(event.target.value) })}
                  inputProps={{ min: 1, max: 65535 }}
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
