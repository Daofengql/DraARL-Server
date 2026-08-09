import type { Dispatch, SetStateAction } from 'react'
import { Box, Button, Card, CardContent, Divider, TextField, Typography } from '@mui/material'
import Save from '@mui/icons-material/Save'
import { TabPanel } from '../../components/common/TabPanel'
import type { OpenAIConfig } from './types'

interface OpenAITabProps {
  value: number
  config: OpenAIConfig
  setConfig: Dispatch<SetStateAction<OpenAIConfig>>
  loading: boolean
  onSave: () => void
}

export function OpenAITab({ value, config, setConfig, loading, onSave }: OpenAITabProps) {
  return (
    <TabPanel value={value} index={3}>
      <Box sx={{ px: 2, maxWidth: 600 }}>
        <Card><CardContent>
          <Typography variant="h6" gutterBottom>OpenAI配置</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>配置OpenAI API连接信息</Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              label="Base URL" fullWidth value={config.base_url}
              onChange={(event) => setConfig({ ...config, base_url: event.target.value })}
              placeholder="https://api.openai.com/v1"
            />
            <TextField
              label="API Key" fullWidth type="password" value={config.api_key}
              onChange={(event) => setConfig({ ...config, api_key: event.target.value })}
              placeholder="sk-..."
            />
            <TextField
              label="Engine/Model" fullWidth value={config.engine}
              onChange={(event) => setConfig({ ...config, engine: event.target.value })}
              placeholder="gpt-4"
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
