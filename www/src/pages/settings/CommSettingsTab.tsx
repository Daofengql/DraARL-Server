import type { Dispatch, SetStateAction } from 'react'
import {
  Box, Button, Card, CardContent, Divider, FormControlLabel, Switch, TextField, Typography,
} from '@mui/material'
import PhoneInTalk from '@mui/icons-material/PhoneInTalk'
import Save from '@mui/icons-material/Save'
import { TabPanel } from '../../components/common/TabPanel'
import type { CommSettingsConfig } from './types'

interface CommSettingsTabProps {
  value: number
  config: CommSettingsConfig
  setConfig: Dispatch<SetStateAction<CommSettingsConfig>>
  loading: boolean
  onSave: () => void
}

export function CommSettingsTab({ value, config, setConfig, loading, onSave }: CommSettingsTabProps) {
  return (
    <TabPanel value={value} index={3}>
      <Box sx={{ px: 2, maxWidth: 600 }}>
        <Card><CardContent>
          <Typography component="h2" variant="h6" gutterBottom>通信设置</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>配置音频记录保存策略</Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
            <Box>
              <Typography variant="subtitle1" sx={{ mb: 1, display: 'flex', alignItems: 'center', gap: 1 }}>
                <PhoneInTalk color="primary" fontSize="small" />音频记录
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                开启后将自动记录每次通信的音频数据到配置的存储服务
              </Typography>
              <FormControlLabel
                control={<Switch
                  checked={config.enabled}
                  onChange={(event) => setConfig({ ...config, enabled: event.target.checked })}
                  color="primary"
                />}
                label={config.enabled ? '已启用' : '已禁用'}
              />
            </Box>
            <Divider />
            <TextField
              label="数据保留天数" type="number" fullWidth value={config.retention_days}
              onChange={(event) => setConfig({ ...config, retention_days: Number(event.target.value) })}
              helperText="超过此天数的记录将被自动删除" inputProps={{ min: 1, max: 365 }}
              disabled={!config.enabled}
            />
            <TextField
              label="最小录制阈值（毫秒）" type="number" fullWidth value={config.min_duration_ms}
              onChange={(event) => setConfig({ ...config, min_duration_ms: Number(event.target.value) })}
              helperText="按实际音频时长判断，少于此值不保存，等于此值会保存；0 表示不过滤" inputProps={{ min: 0, step: 1 }}
              disabled={!config.enabled}
            />
            <TextField
              label="最大录制时长（秒）" type="number" fullWidth value={config.max_duration_sec}
              onChange={(event) => setConfig({ ...config, max_duration_sec: Number(event.target.value) })}
              helperText="0 表示不限制，达到此音频时长后分段保存，通话继续" inputProps={{ min: 0 }}
              disabled={!config.enabled}
            />
            <TextField
              label="批量上传间隔（秒）" type="number" fullWidth value={config.batch_upload_sec}
              onChange={(event) => setConfig({ ...config, batch_upload_sec: Number(event.target.value) })}
              helperText="音频数据批量上传到存储服务的间隔时间" inputProps={{ min: 1, max: 300 }}
              disabled={!config.enabled}
            />
          </Box>
          <Divider sx={{ my: 3 }} />
          <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
            <Button variant="contained" startIcon={<Save />} onClick={onSave} disabled={loading}>保存</Button>
          </Box>
        </CardContent></Card>
      </Box>
    </TabPanel>
  )
}
