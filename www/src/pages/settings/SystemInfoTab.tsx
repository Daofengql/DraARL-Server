import type { Dispatch, SetStateAction } from 'react'
import { Box, Button, Card, CardContent, Divider, InputAdornment, TextField, Typography } from '@mui/material'
import Public from '@mui/icons-material/Public'
import Save from '@mui/icons-material/Save'
import { TabPanel } from '../../components/common/TabPanel'
import { BrandResourceControl } from './BrandResourceControl'
import type { SiteMessage, SystemInfoConfig } from './types'

interface SystemInfoTabProps {
  value: number
  config: SystemInfoConfig
  setConfig: Dispatch<SetStateAction<SystemInfoConfig>>
  loading: boolean
  onSave: () => void
  reload: () => Promise<void>
  showMessage: (type: SiteMessage['type'], text: string) => void
}

export function SystemInfoTab({
  value, config, setConfig, loading, onSave, reload, showMessage,
}: SystemInfoTabProps) {
  return (
    <TabPanel value={value} index={0}>
      <Box sx={{ px: 2, maxWidth: 600 }}>
        <Card>
          <CardContent>
            <Typography variant="h6" gutterBottom>系统基本信息</Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
              配置站点的基本显示信息
            </Typography>

            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              <TextField
                label="站点名称" fullWidth value={config.name}
                onChange={(event) => setConfig({ ...config, name: event.target.value })}
                placeholder="例如：DraARL-福建"
              />
              <TextField
                label="站点简称" fullWidth value={config.nameshorthand}
                onChange={(event) => setConfig({ ...config, nameshorthand: event.target.value })}
                placeholder="例如：DraARL-Fujian"
              />
              <BrandResourceControl
                kind="logo" value={config.logo_url} onChanged={reload} showMessage={showMessage}
                onPreviewError={() => setConfig({ ...config, logo_url: '' })}
              />
              <BrandResourceControl
                kind="favicon" value={config.favicon_url} onChanged={reload} showMessage={showMessage}
                onPreviewError={() => setConfig({ ...config, favicon_url: '' })}
              />
              <TextField
                label="语言" fullWidth value={config.language}
                onChange={(event) => setConfig({ ...config, language: event.target.value })}
                select SelectProps={{ native: true }}
              >
                <option value="zh">中文</option>
                <option value="en">English</option>
              </TextField>

              <Divider sx={{ my: 2 }} />
              <TextField
                label="ICP备案号" fullWidth value={config.icp}
                onChange={(event) => setConfig({ ...config, icp: event.target.value })}
                placeholder="例如：闽ICP备12345678号"
                InputProps={{ startAdornment: <InputAdornment position="start"><Public /></InputAdornment> }}
              />
            </Box>

            <Divider sx={{ my: 3 }} />
            <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
              <Button variant="contained" startIcon={<Save />} onClick={onSave} disabled={loading}>保存</Button>
            </Box>
          </CardContent>
        </Card>
      </Box>
    </TabPanel>
  )
}
