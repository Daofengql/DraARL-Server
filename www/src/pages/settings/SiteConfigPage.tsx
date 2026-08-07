import { useState } from 'react'
import { Alert, Box, Paper, Tab, Tabs, Typography } from '@mui/material'
import { AccessDiscoveryTab } from './AccessDiscoveryTab'
import { AprsTab } from './AprsTab'
import { CommSettingsTab } from './CommSettingsTab'
import { OpenAITab } from './OpenAITab'
import { OperatorLogsTab } from './OperatorLogsTab'
import { RegistrationTab } from './RegistrationTab'
import { SmtpTab } from './SmtpTab'
import { SystemInfoTab } from './SystemInfoTab'
import { useSiteConfig } from './useSiteConfig'

export function SiteConfigPage() {
  const [tabValue, setTabValue] = useState(0)
  const siteConfig = useSiteConfig()

  return (
    <Box>
      <Typography variant="h4" sx={{ mb: 3, fontWeight: 600 }}>站点配置</Typography>
      {siteConfig.message && (
        <Alert severity={siteConfig.message.type} sx={{ mb: 2 }} onClose={() => siteConfig.setMessage(null)}>
          {siteConfig.message.text}
        </Alert>
      )}

      <Paper>
        <Tabs
          value={tabValue}
          onChange={(_, newValue) => setTabValue(newValue)}
          sx={{ borderBottom: 1, borderColor: 'divider', px: 2 }}
          variant="scrollable"
          scrollButtons="auto"
        >
          <Tab label="系统信息" />
          <Tab label="接入点" />
          <Tab label="APRS" />
          <Tab label="OpenAI" />
          <Tab label="通信设置" />
          <Tab label="SMTP配置" />
          <Tab label="注册设置" />
          <Tab label="操作日志" />
        </Tabs>

        <SystemInfoTab
          value={tabValue}
          config={siteConfig.systemInfo}
          setConfig={siteConfig.setSystemInfo}
          loading={siteConfig.loading}
          onSave={siteConfig.handleSaveSystemInfo}
          reload={siteConfig.loadConfigs}
          showMessage={siteConfig.showMessage}
        />
        <AccessDiscoveryTab
          value={tabValue}
          config={siteConfig.accessDiscovery}
          setConfig={siteConfig.setAccessDiscovery}
          loading={siteConfig.loading}
          onSave={siteConfig.handleSaveAccessDiscovery}
        />
        <AprsTab
          value={tabValue}
          config={siteConfig.aprs}
          setConfig={siteConfig.setAPRS}
          loading={siteConfig.loading}
          onSave={siteConfig.handleSaveAPRS}
          showMessage={siteConfig.showMessage}
        />
        <OpenAITab
          value={tabValue}
          config={siteConfig.openai}
          setConfig={siteConfig.setOpenAI}
          loading={siteConfig.loading}
          onSave={siteConfig.handleSaveOpenAI}
        />
        <CommSettingsTab
          value={tabValue}
          config={siteConfig.commSettings}
          setConfig={siteConfig.setCommSettings}
          loading={siteConfig.loading}
          onSave={siteConfig.handleSaveCommSettings}
        />
        <SmtpTab
          value={tabValue}
          config={siteConfig.smtp}
          setConfig={siteConfig.setSMTP}
          loading={siteConfig.loading}
          onSave={siteConfig.handleSaveSMTP}
        />
        <RegistrationTab
          value={tabValue}
          config={siteConfig.registration}
          setConfig={siteConfig.setRegistration}
          loading={siteConfig.loading}
          onSave={siteConfig.handleSaveRegistration}
        />
        <OperatorLogsTab value={tabValue} />
      </Paper>
    </Box>
  )
}
