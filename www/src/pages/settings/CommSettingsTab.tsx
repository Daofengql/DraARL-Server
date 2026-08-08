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
    <TabPanel value={value} index={4}>
      <Box sx={{ px: 2, maxWidth: 600 }}>
        <Card><CardContent>
          <Typography variant="h6" gutterBottom>通信设置</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>配置音频记录保存策略</Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
            <Box>
              <Typography variant="subtitle1" sx={{ mb: 1, display: 'flex', alignItems: 'center', gap: 1 }}>
                <PhoneInTalk color="primary" fontSize="small" />音频记录
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                开启后将自动记录每次通信的音频数据到MinIO
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
            <Divider />
            <TextField
              label="数据保留天数" type="number" fullWidth value={config.retention_days}
              onChange={(event) => setConfig({ ...config, retention_days: parseInt(event.target.value) || 0 })}
              helperText="超过此天数的记录将被自动删除" inputProps={{ min: 1, max: 365 }}
              disabled={!config.enabled}
            />
            <TextField
              label="最小录制阈值（毫秒）" type="number" fullWidth value={config.min_duration_ms}
              onChange={(event) => setConfig({ ...config, min_duration_ms: parseInt(event.target.value) || 0 })}
              helperText="少于此时长的音频不会上传到MinIO" inputProps={{ min: 0, step: 100 }}
              disabled={!config.enabled}
            />
            <TextField
              label="最大录制时长（秒）" type="number" fullWidth value={config.max_duration_sec}
              onChange={(event) => setConfig({ ...config, max_duration_sec: parseInt(event.target.value) || 0 })}
              helperText="0表示不限制，通信超过此时长将自动断开" inputProps={{ min: 0 }}
              disabled={!config.enabled}
            />
            <TextField
              label="批量上传间隔（秒）" type="number" fullWidth value={config.batch_upload_sec}
              onChange={(event) => setConfig({ ...config, batch_upload_sec: parseInt(event.target.value) || 10 })}
              helperText="音频数据批量上传到MinIO的间隔时间" inputProps={{ min: 1, max: 300 }}
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
