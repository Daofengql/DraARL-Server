import type { Dispatch, SetStateAction } from 'react'
import { Alert, Box, Button, Card, CardContent, Divider, FormControlLabel, Switch, Typography } from '@mui/material'
import Save from '@mui/icons-material/Save'
import { TabPanel } from '../../components/common/TabPanel'
import type { RegistrationConfig } from './types'

interface RegistrationTabProps {
  value: number
  config: RegistrationConfig
  setConfig: Dispatch<SetStateAction<RegistrationConfig>>
  loading: boolean
  onSave: () => void
}

export function RegistrationTab({ value, config, setConfig, loading, onSave }: RegistrationTabProps) {
  return (
    <TabPanel value={value} index={6}>
      <Box sx={{ px: 2, maxWidth: 600 }}>
        <Card><CardContent>
          <Typography variant="h6" gutterBottom>注册设置</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
            配置用户注册流程中的验证要求
          </Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <FormControlLabel
              control={<Switch
                checked={config.require_email_verification}
                onChange={(event) => setConfig({ ...config, require_email_verification: event.target.checked })}
              />}
              label="注册时必须验证邮箱"
            />
            <Alert severity="info">
              关闭后，用户仍需填写邮箱地址，但注册流程不会要求发送和校验邮箱验证码。
            </Alert>
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
