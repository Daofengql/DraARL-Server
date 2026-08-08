import type { Dispatch, SetStateAction } from 'react'
import {
  Box, Button, Card, CardContent, Divider, FormControlLabel, Switch, TextField, Typography,
} from '@mui/material'
import Save from '@mui/icons-material/Save'
import { TabPanel } from '../../components/common/TabPanel'
import type { SMTPConfig } from './types'

interface SmtpTabProps {
  value: number
  config: SMTPConfig
  setConfig: Dispatch<SetStateAction<SMTPConfig>>
  loading: boolean
  onSave: () => void
}

export function SmtpTab({ value, config, setConfig, loading, onSave }: SmtpTabProps) {
  return (
    <TabPanel value={value} index={5}>
      <Box sx={{ px: 2, maxWidth: 600 }}>
        <Card><CardContent>
          <Typography variant="h6" gutterBottom>SMTP邮件配置</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
            配置SMTP服务器用于发送验证码邮件
          </Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              label="SMTP服务器地址" fullWidth value={config.host}
              onChange={(event) => setConfig({ ...config, host: event.target.value })}
              placeholder="例如：smtp.qq.com"
            />
            <TextField
              label="SMTP端口" type="number" fullWidth value={config.port}
              onChange={(event) => setConfig({ ...config, port: parseInt(event.target.value) || 465 })}
              placeholder="465" inputProps={{ min: 1, max: 65535 }}
            />
            <FormControlLabel
              control={<Switch checked={config.use_ssl} onChange={(event) => setConfig({ ...config, use_ssl: event.target.checked })} />}
              label="使用SSL加密"
            />
            <Divider sx={{ my: 1 }} />
            <TextField
              label="发件人昵称" fullWidth value={config.sender_name}
              onChange={(event) => setConfig({ ...config, sender_name: event.target.value })}
              placeholder="例如：DraARL麟链"
            />
            <TextField
              label="发件人邮箱" fullWidth type="email" value={config.sender_email}
              onChange={(event) => setConfig({ ...config, sender_email: event.target.value })}
              placeholder="例如：noreply@example.com"
            />
            <TextField
              label="邮箱授权码" fullWidth type="password" value={config.password}
              onChange={(event) => setConfig({ ...config, password: event.target.value })}
              placeholder="邮箱SMTP授权码（非登录密码）" helperText="请使用邮箱的SMTP授权码，而非登录密码"
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
