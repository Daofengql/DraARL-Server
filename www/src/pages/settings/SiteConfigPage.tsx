import { useState, useEffect, useRef } from 'react'
import {
  Box,
  Paper,
  Typography,
  TextField,
  Button,
  Alert,
  Tab,
  Tabs,
  Card,
  CardContent,
  Divider,
  InputAdornment,
  IconButton,
  Chip,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TablePagination,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Switch,
  FormControlLabel,
} from '@mui/material'
import Save from '@mui/icons-material/Save'
import Public from '@mui/icons-material/Public'
import Refresh from '@mui/icons-material/Refresh'
import Terminal from '@mui/icons-material/Terminal'
import CloudUpload from '@mui/icons-material/CloudUpload'
import Delete from '@mui/icons-material/Delete'
import Search from '@mui/icons-material/Search'
import Info from '@mui/icons-material/Info'
import PhoneInTalk from '@mui/icons-material/PhoneInTalk'
import { apiClient } from '../../services/api'
import { logService } from '../../services'
import type { OperatorLog } from '../../types'

interface APRSLogEntry {
  timestamp: string
  message: string
}

interface TabPanelProps {
  children?: React.ReactNode
  index: number
  value: number
}

function TabPanel({ children, value, index }: TabPanelProps) {
  return (
    <div role="tabpanel" hidden={value !== index}>
      {value === index && <Box sx={{ py: 3 }}>{children}</Box>}
    </div>
  )
}

// 系统信息配置
interface SystemInfoConfig {
  name: string
  nameshorthand: string
  logo_url: string
  language: string
  icp: string
}

// APRS配置
interface APRSConfig {
  aprs_server_host: string
  aprs_server_port: string
  self_address: string
  self_port: string
  callsign: string
  ssid: string
  latitude: number
  longitude: number
  altitude: string
}

// OpenAI配置
interface OpenAIConfig {
  base_url: string
  api_key: string
  engine: string
}

// 通信设置配置
interface CommSettingsConfig {
  enabled: boolean
  retention_days: number
  min_duration_ms: number
  max_duration_sec: number
  batch_upload_sec: number
}

// SMTP配置
interface SMTPConfig {
  host: string
  port: number
  use_ssl: boolean
  sender_name: string
  sender_email: string
  password: string
}

// 操作日志事件类型
const EVENT_TYPES = [
  { value: '', label: '全部' },
  { value: 'login', label: '登录' },
  { value: 'logout', label: '登出' },
  { value: 'login_failed', label: '登录失败' },
  { value: 'register', label: '注册' },
  { value: 'user_create', label: '创建用户' },
  { value: 'user_update', label: '更新用户' },
  { value: 'user_delete', label: '删除用户' },
  { value: 'user_status', label: '用户状态变更' },
  { value: 'user_approval', label: '用户审批' },
  { value: 'password_reset', label: '重置密码' },
  { value: 'password_change', label: '修改密码' },
  { value: 'profile_update', label: '更新个人资料' },
  { value: 'group_create', label: '创建群组' },
  { value: 'group_update', label: '更新群组' },
  { value: 'group_delete', label: '删除群组' },
  { value: 'group_join', label: '加入群组' },
  { value: 'group_leave', label: '离开群组' },
  { value: 'group_device_status', label: '群组设备状态' },
  { value: 'device_kick', label: '踢出设备' },
  { value: 'virtual_group_create', label: '创建虚拟互联组' },
  { value: 'virtual_group_update', label: '更新虚拟互联组' },
  { value: 'virtual_group_delete', label: '删除虚拟互联组' },
  { value: 'group_link_add', label: '添加群组互联' },
  { value: 'group_link_remove', label: '移除群组互联' },
  { value: 'asset_create', label: '创建资源' },
  { value: 'asset_upload', label: '上传资源' },
  { value: 'asset_update', label: '更新资源' },
  { value: 'asset_delete', label: '删除资源' },
  { value: 'config_update', label: '配置更新' },
  { value: 'comm_settings_update', label: '通信配置更新' },
  { value: 'comm_record_delete', label: '删除通信记录' },
  { value: 'cache_clear_all', label: '清空缓存' },
  { value: 'cache_metrics_reset', label: '重置缓存指标' },
  { value: 'system', label: '系统' },
]

// 事件类型颜色映射
const EVENT_TYPE_COLORS: Record<string, any> = {
  login: 'info',
  logout: 'default',
  login_failed: 'error',
  register: 'success',
  user_create: 'success',
  user_update: 'warning',
  user_delete: 'error',
  user_status: 'secondary',
  user_approval: 'primary',
  password_reset: 'error',
  password_change: 'warning',
  profile_update: 'info',
  group_create: 'success',
  group_update: 'warning',
  group_delete: 'error',
  group_join: 'info',
  group_leave: 'default',
  group_device_status: 'secondary',
  device_kick: 'warning',
  virtual_group_create: 'success',
  virtual_group_update: 'warning',
  virtual_group_delete: 'error',
  group_link_add: 'info',
  group_link_remove: 'warning',
  asset_create: 'success',
  asset_upload: 'success',
  asset_update: 'warning',
  asset_delete: 'error',
  config_update: 'secondary',
  comm_settings_update: 'secondary',
  comm_record_delete: 'warning',
  cache_clear_all: 'warning',
  cache_metrics_reset: 'info',
  system: 'primary',
}

// 事件类型显示标签映射（用于表格中显示更友好的标签）
const EVENT_TYPE_LABELS: Record<string, string> = {
  login: '登录',
  logout: '登出',
  login_failed: '登录失败',
  register: '注册',
  user_create: '创建用户',
  user_update: '更新用户',
  user_delete: '删除用户',
  user_status: '状态变更',
  user_approval: '用户审批',
  password_reset: '重置密码',
  password_change: '修改密码',
  profile_update: '更新资料',
  group_create: '创建群组',
  group_update: '更新群组',
  group_delete: '删除群组',
  group_join: '加入群组',
  group_leave: '离开群组',
  group_device_status: '设备状态',
  device_kick: '踢出设备',
  virtual_group_create: '创建互联组',
  virtual_group_update: '更新互联组',
  virtual_group_delete: '删除互联组',
  group_link_add: '添加互联',
  group_link_remove: '移除互联',
  asset_create: '创建资源',
  asset_upload: '上传资源',
  asset_update: '更新资源',
  asset_delete: '删除资源',
  config_update: '配置更新',
  comm_settings_update: '通信配置',
  comm_record_delete: '删除记录',
  cache_clear_all: '清空缓存',
  cache_metrics_reset: '重置指标',
  system: '系统',
}

// 获取事件类型的显示标签
function getEventTypeLabel(eventType: string): string {
  return EVENT_TYPE_LABELS[eventType] || eventType
}

export function SiteConfigPage() {
  const [tabValue, setTabValue] = useState(0)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [loading, setLoading] = useState(false)
  const [uploadingLogo, setUploadingLogo] = useState(false)

  // 系统信息配置
  const [systemInfo, setSystemInfo] = useState<SystemInfoConfig>({
    name: '',
    nameshorthand: '',
    logo_url: '',
    language: 'zh',
    icp: '',
  })

  // APRS配置
  const [aprs, setAPRS] = useState<APRSConfig>({
    aprs_server_host: '',
    aprs_server_port: '',
    self_address: '',
    self_port: '',
    callsign: '',
    ssid: '',
    latitude: 0,
    longitude: 0,
    altitude: '',
  })

  // OpenAI配置
  const [openai, setOpenAI] = useState<OpenAIConfig>({
    base_url: '',
    api_key: '',
    engine: '',
  })

  // 通信设置配置
  const [commSettings, setCommSettings] = useState<CommSettingsConfig>({
    enabled: false,
    retention_days: 30,
    min_duration_ms: 500,
    max_duration_sec: 300,
    batch_upload_sec: 10,
  })

  // SMTP配置
  const [smtp, setSMTP] = useState<SMTPConfig>({
    host: 'smtp.qq.com',
    port: 465,
    use_ssl: true,
    sender_name: '',
    sender_email: '',
    password: '',
  })

  // APRS日志
  const [aprsLogs, setAPRSLogs] = useState<APRSLogEntry[]>([])
  const [aprsLogsLoading, setAprsLogsLoading] = useState(false)
  const [configCardHeight, setConfigCardHeight] = useState<number | null>(null)
  const configCardRef = useRef<HTMLDivElement>(null)

  // 操作日志状态
  const [opLogs, setOpLogs] = useState<OperatorLog[]>([])
  const [opTotal, setOpTotal] = useState(0)
  const [logPage, setLogPage] = useState(0)
  const [logRowsPerPage, setLogRowsPerPage] = useState(10)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [eventType, setEventType] = useState('')
  const [opLogsLoading, setOpLogsLoading] = useState(false)

  useEffect(() => {
    loadConfigs()
  }, [])

  const loadConfigs = async () => {
    try {
      // 并行获取所有配置
      const [icpRes, systemRes, aprsRes, openaiRes, commSettingsRes, smtpRes] = await Promise.all([
        apiClient.get<any>('/api/config/category/icp'),
        apiClient.get<any>('/api/config/category/system'),
        apiClient.get<any>('/api/config/aprs'),
        apiClient.get<any>('/api/config/openai'),
        apiClient.get<any>('/api/config/comm-settings'),
        apiClient.get<any>('/api/config/smtp'),
      ])

      // 解析系统信息配置（包含ICP）
      if (systemRes.code === 200 && systemRes.data && systemRes.data.length > 0) {
        const newSystemInfo: SystemInfoConfig = {
          name: systemRes.data.find((c: any) => c.key === 'system.name')?.value || '',
          nameshorthand: systemRes.data.find((c: any) => c.key === 'system.nameshorthand')?.value || '',
          logo_url: systemRes.data.find((c: any) => c.key === 'system.logo_url')?.value || '',
          language: systemRes.data.find((c: any) => c.key === 'system.language')?.value || 'zh',
          icp: icpRes.code === 200 && icpRes.data && icpRes.data.length > 0
            ? icpRes.data.find((c: any) => c.key === 'web.icp')?.value || ''
            : '',
        }
        setSystemInfo(newSystemInfo)
      }

      // 解析APRS配置
      if (aprsRes.code === 200 && aprsRes.data) {
        setAPRS(aprsRes.data)
      }

      // 解析OpenAI配置
      if (openaiRes.code === 200 && openaiRes.data) {
        setOpenAI(openaiRes.data)
      }

      // 解析通信设置配置
      if (commSettingsRes.code === 200 && commSettingsRes.data) {
        setCommSettings(commSettingsRes.data)
      }

      // 解析SMTP配置
      if (smtpRes.code === 200 && smtpRes.data) {
        setSMTP(smtpRes.data)
      }
    } catch (err) {
      console.error('Failed to load configs:', err)
      showMessage('error', '加载配置失败')
    }
  }

  const showMessage = (type: 'success' | 'error', text: string) => {
    setMessage({ type, text })
    setTimeout(() => setMessage(null), 3000)
  }

  const handleSaveSystemInfo = async () => {
    setLoading(true)
    try {
      // 保存系统信息
      await apiClient.put('/api/config/system', {
        name: systemInfo.name,
        nameshorthand: systemInfo.nameshorthand,
        logo_url: systemInfo.logo_url,
        language: systemInfo.language,
      })
      // 保存ICP备案号
      await apiClient.put('/api/config/icp', { icp: systemInfo.icp })
      showMessage('success', '系统信息保存成功')
      // 通知Header刷新
      window.dispatchEvent(new CustomEvent('config-updated'))
    } catch (err) {
      showMessage('error', '保存系统信息失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSaveAPRS = async () => {
    // 校验经纬度范围
    if (aprs.longitude < -180 || aprs.longitude > 180) {
      showMessage('error', '经度必须在 -180 到 180 之间')
      return
    }
    if (aprs.latitude < -90 || aprs.latitude > 90) {
      showMessage('error', '纬度必须在 -90 到 90 之间')
      return
    }

    setLoading(true)
    try {
      await apiClient.put('/api/config/aprs', aprs)
      showMessage('success', 'APRS配置保存成功')
    } catch (err) {
      showMessage('error', '保存APRS配置失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSaveOpenAI = async () => {
    setLoading(true)
    try {
      await apiClient.put('/api/config/openai', openai)
      showMessage('success', 'OpenAI配置保存成功')
    } catch (err) {
      showMessage('error', '保存OpenAI配置失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSaveCommSettings = async () => {
    setLoading(true)
    try {
      await apiClient.put('/api/config/comm-settings', commSettings)
      showMessage('success', '通信设置保存成功')
    } catch (err) {
      showMessage('error', '保存通信设置失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSaveSMTP = async () => {
    setLoading(true)
    try {
      await apiClient.put('/api/config/smtp', smtp)
      showMessage('success', 'SMTP配置保存成功')
    } catch (err) {
      showMessage('error', '保存SMTP配置失败')
    } finally {
      setLoading(false)
    }
  }

  const loadAPRSLogs = async () => {
    setAprsLogsLoading(true)
    try {
      const res = await apiClient.get<any>('/api/config/aprs/logs')
      if (res.code === 200 && res.data) {
        setAPRSLogs(res.data)
      }
    } catch (err) {
      console.error('Failed to load APRS logs:', err)
    } finally {
      setAprsLogsLoading(false)
    }
  }

  // Logo上传处理
  const logoInputRef = useRef<HTMLInputElement>(null)

  const handleLogoUploadClick = () => {
    logoInputRef.current?.click()
  }

  const handleLogoFileChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    // 验证文件大小 (限制5MB)
    const maxSize = 5 * 1024 * 1024
    if (file.size > maxSize) {
      showMessage('error', 'Logo文件大小不能超过5MB')
      return
    }

    // 验证文件类型
    if (!file.type.startsWith('image/')) {
      showMessage('error', '请选择图片文件')
      return
    }

    setUploadingLogo(true)
    try {
      const formData = new FormData()
      formData.append('file', file)

      const res = await apiClient.post<any>('/api/upload/logo', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      })

      if (res.code === 200 && res.data?.file_url) {
        // 重新加载配置以获取最新的 logo URL
        await loadConfigs()
        showMessage('success', 'Logo上传成功')
        // 通知Header刷新
        window.dispatchEvent(new CustomEvent('config-updated'))
      }
    } catch (err) {
      console.error('Failed to upload logo:', err)
      showMessage('error', 'Logo上传失败')
    } finally {
      setUploadingLogo(false)
      // 重置input
      if (logoInputRef.current) {
        logoInputRef.current.value = ''
      }
    }
  }

  const handleLogoDelete = async () => {
    try {
      await apiClient.delete('/api/config/logo')
      await loadConfigs()
      showMessage('success', 'Logo删除成功')
      // 通知Header刷新
      window.dispatchEvent(new CustomEvent('config-updated'))
    } catch (err) {
      console.error('Failed to delete logo:', err)
      showMessage('error', 'Logo删除失败')
    }
  }

  // 加载APRS日志当切换到APRS标签页时
  useEffect(() => {
    if (tabValue === 1) {
      loadAPRSLogs()
      // 每10秒刷新一次日志
      const interval = setInterval(loadAPRSLogs, 10000)
      return () => clearInterval(interval)
    }
  }, [tabValue])

  // 加载操作日志当切换到操作日志标签页时
  useEffect(() => {
    if (tabValue === 4) {
      loadOpLogs()
    }
  }, [tabValue, logPage, logRowsPerPage, eventType])

  // 加载操作日志
  const loadOpLogs = async () => {
    setOpLogsLoading(true)
    try {
      const data = await logService.getList({
        page: logPage + 1,
        page_size: logRowsPerPage,
        event_type: eventType || undefined,
      })
      const items = data.items || data
      setOpLogs(Array.isArray(items) ? items : [])
      setOpTotal(data.total || (Array.isArray(items) ? items.length : 0))
    } catch (err) {
      console.error('Failed to load logs:', err)
    } finally {
      setOpLogsLoading(false)
    }
  }

  // 同步两个卡片的高度
  useEffect(() => {
    if (tabValue === 1 && configCardRef.current) {
      const updateHeight = () => {
        if (configCardRef.current) {
          setConfigCardHeight(configCardRef.current.offsetHeight)
        }
      }

      // 初始高度
      updateHeight()

      // 监听窗口大小变化
      const resizeObserver = new ResizeObserver(updateHeight)
      resizeObserver.observe(configCardRef.current)

      return () => resizeObserver.disconnect()
    }
  }, [tabValue])

  return (
    <Box>
      <Typography variant="h4" sx={{ mb: 3, fontWeight: 600 }}>
        站点配置
      </Typography>

      {message && (
        <Alert
          severity={message.type}
          sx={{ mb: 2 }}
          onClose={() => setMessage(null)}
        >
          {message.text}
        </Alert>
      )}

      <Paper>
        <Tabs
          value={tabValue}
          onChange={(_, newValue) => setTabValue(newValue)}
          sx={{ borderBottom: 1, borderColor: 'divider', px: 2 }}
        >
          <Tab label="系统信息" />
          <Tab label="APRS" />
          <Tab label="OpenAI" />
          <Tab label="通信设置" />
          <Tab label="SMTP配置" />
          <Tab label="操作日志" />
        </Tabs>

        {/* 系统信息标签页 */}
        <TabPanel value={tabValue} index={0}>
          <Box sx={{ px: 2, maxWidth: 600 }}>
            <Card>
              <CardContent>
                <Typography variant="h6" gutterBottom>
                  系统基本信息
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                  配置站点的基本显示信息
                </Typography>

                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <TextField
                    label="站点名称"
                    fullWidth
                    value={systemInfo.name}
                    onChange={(e) => setSystemInfo({ ...systemInfo, name: e.target.value })}
                    placeholder="例如：DraARL-福建"
                  />

                  <TextField
                    label="站点简称"
                    fullWidth
                    value={systemInfo.nameshorthand}
                    onChange={(e) => setSystemInfo({ ...systemInfo, nameshorthand: e.target.value })}
                    placeholder="例如：DraARL-Fujian"
                  />

                  {/* Logo上传组件 */}
                  <Box>
                    <Typography variant="subtitle2" sx={{ mb: 1, color: 'text.secondary' }}>
                      站点Logo
                    </Typography>
                    <Box
                      sx={{
                        border: '1px dashed',
                        borderColor: 'divider',
                        borderRadius: 2,
                        p: 2,
                        textAlign: 'center',
                        bgcolor: 'background.paper',
                        cursor: 'pointer',
                        '&:hover': { bgcolor: 'action.hover' },
                      }}
                      onClick={handleLogoUploadClick}
                    >
                      <input
                        ref={logoInputRef}
                        type="file"
                        accept="image/*"
                        onChange={handleLogoFileChange}
                        style={{ display: 'none' }}
                      />
                      {systemInfo.logo_url ? (
                        <Box sx={{ position: 'relative', display: 'inline-block' }}>
                          <Box
                            component="img"
                            src={systemInfo.logo_url}
                            alt="Logo预览"
                            sx={{
                              maxWidth: '100%',
                              maxHeight: 150,
                              objectFit: 'contain',
                            }}
                            onError={(e) => {
                              (e.target as HTMLImageElement).src = ''
                              setSystemInfo({ ...systemInfo, logo_url: '' })
                            }}
                          />
                          <IconButton
                            size="small"
                            sx={{
                              position: 'absolute',
                              top: -8,
                              right: -8,
                              bgcolor: 'background.paper',
                              '&:hover': { bgcolor: 'error.light' },
                            }}
                            onClick={(e) => {
                              e.stopPropagation()
                              handleLogoDelete()
                            }}
                          >
                            <Delete fontSize="small" color="error" />
                          </IconButton>
                        </Box>
                      ) : (
                        <Box sx={{ py: 3 }}>
                          <CloudUpload sx={{ fontSize: 48, color: 'text.secondary', mb: 1 }} />
                          <Typography variant="body2" color="text.secondary">
                            点击上传Logo图片
                          </Typography>
                          <Typography variant="caption" color="text.disabled">
                            支持PNG、JPG、GIF格式，最大5MB
                          </Typography>
                        </Box>
                      )}
                    </Box>
                  </Box>

                  <TextField
                    label="语言"
                    fullWidth
                    value={systemInfo.language}
                    onChange={(e) => setSystemInfo({ ...systemInfo, language: e.target.value })}
                    select
                    SelectProps={{ native: true }}
                  >
                    <option value="zh">中文</option>
                    <option value="en">English</option>
                  </TextField>

                  <Divider sx={{ my: 2 }} />

                  <TextField
                    label="ICP备案号"
                    fullWidth
                    value={systemInfo.icp}
                    onChange={(e) => setSystemInfo({ ...systemInfo, icp: e.target.value })}
                    placeholder="例如：闽ICP备12345678号"
                    InputProps={{
                      startAdornment: (
                        <InputAdornment position="start">
                          <Public />
                        </InputAdornment>
                      ),
                    }}
                  />
                </Box>

                <Divider sx={{ my: 3 }} />

                <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
                  <Button
                    variant="contained"
                    startIcon={<Save />}
                    onClick={handleSaveSystemInfo}
                    disabled={loading}
                  >
                    保存
                  </Button>
                </Box>
              </CardContent>
            </Card>
          </Box>
        </TabPanel>

        {/* APRS标签页 */}
        <TabPanel value={tabValue} index={1}>
          <Box sx={{ px: 2 }}>
            <Box sx={{ display: 'flex', flexDirection: { xs: 'column', md: 'row' }, gap: 2, alignItems: { xs: 'stretch', md: 'flex-start' } }}>
              {/* APRS配置卡片 */}
              <Box sx={{ flex: { xs: '1 1 auto', md: '0 1 50%' } }}>
                <Card ref={configCardRef}>
                  <CardContent>
                    <Typography variant="h6" gutterBottom>
                      APRS配置
                    </Typography>
                    <Alert severity="info" sx={{ mb: 2 }}>
                      此配置用于服务器的APRS上报，将服务器信息（如在线设备数、服务器地址等）上报到APRS网络，而非设备的APRS配置。
                    </Alert>
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                      配置APRS服务器连接信息
                    </Typography>

                    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                      {/* APRS服务器地址:端口 */}
                      <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, alignItems: { xs: 'stretch', sm: 'center' }, gap: 1 }}>
                        <TextField
                          label="APRS服务器"
                          fullWidth
                          value={aprs.aprs_server_host}
                          onChange={(e) => setAPRS({ ...aprs, aprs_server_host: e.target.value })}
                          placeholder="china.aprs2.net"
                        />
                        <Typography sx={{ fontSize: '1.2rem', fontWeight: 'bold', color: 'text.secondary', display: { xs: 'none', sm: 'block' }, minWidth: '20px', textAlign: 'center' }}>:</Typography>
                        <TextField
                          label="端口"
                          sx={{ width: { xs: '100%', sm: '100px' } }}
                          value={aprs.aprs_server_port}
                          onChange={(e) => setAPRS({ ...aprs, aprs_server_port: e.target.value })}
                          placeholder="14580"
                        />
                      </Box>

                      {/* 本机地址:端口 */}
                      <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, alignItems: { xs: 'stretch', sm: 'center' }, gap: 1 }}>
                        <TextField
                          label="本机地址"
                          fullWidth
                          value={aprs.self_address}
                          onChange={(e) => setAPRS({ ...aprs, self_address: e.target.value })}
                          placeholder="yourdomain.com"
                        />
                        <Typography sx={{ fontSize: '1.2rem', fontWeight: 'bold', color: 'text.secondary', display: { xs: 'none', sm: 'block' }, minWidth: '20px', textAlign: 'center' }}>:</Typography>
                        <TextField
                          label="端口"
                          sx={{ width: { xs: '100%', sm: '100px' } }}
                          value={aprs.self_port}
                          onChange={(e) => setAPRS({ ...aprs, self_port: e.target.value })}
                          placeholder="60050"
                        />
                      </Box>

                      {/* 呼号-SSID */}
                      <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, alignItems: { xs: 'stretch', sm: 'center' }, gap: 1 }}>
                        <TextField
                          label="呼号"
                          fullWidth
                          value={aprs.callsign}
                          onChange={(e) => setAPRS({ ...aprs, callsign: e.target.value })}
                          placeholder="BH0AAA"
                        />
                        <Typography sx={{ fontSize: '1.2rem', fontWeight: 'bold', color: 'text.secondary', display: { xs: 'none', sm: 'block' }, minWidth: '20px', textAlign: 'center' }}>-</Typography>
                        <TextField
                          label="SSID"
                          sx={{ width: { xs: '100%', sm: '100px' } }}
                          value={aprs.ssid}
                          onChange={(e) => setAPRS({ ...aprs, ssid: e.target.value })}
                          placeholder="10"
                        />
                      </Box>

                      {/* 经度,纬度,海拔 */}
                      <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, alignItems: { xs: 'stretch', sm: 'center' }, gap: 1 }}>
                        <TextField
                          label="经度"
                          fullWidth
                          type="number"
                          inputProps={{ step: 0.000001, min: -180, max: 180 }}
                          value={aprs.longitude || ''}
                          onChange={(e) => setAPRS({ ...aprs, longitude: parseFloat(e.target.value) || 0 })}
                          placeholder="0.000000"
                          helperText="-180 到 180"
                          error={aprs.longitude < -180 || aprs.longitude > 180}
                        />
                        <Typography sx={{ fontSize: '1.2rem', fontWeight: 'bold', color: 'text.secondary', display: { xs: 'none', sm: 'block' }, minWidth: '20px', textAlign: 'center' }}>,</Typography>
                        <TextField
                          label="纬度"
                          fullWidth
                          type="number"
                          inputProps={{ step: 0.000001, min: -90, max: 90 }}
                          value={aprs.latitude || ''}
                          onChange={(e) => setAPRS({ ...aprs, latitude: parseFloat(e.target.value) || 0 })}
                          placeholder="0.000000"
                          helperText="-90 到 90"
                          error={aprs.latitude < -90 || aprs.latitude > 90}
                        />
                        <Typography sx={{ fontSize: '1.2rem', fontWeight: 'bold', color: 'text.secondary', display: { xs: 'none', sm: 'block' }, minWidth: '20px', textAlign: 'center' }}>,</Typography>
                        <TextField
                          label="海拔(m)"
                          sx={{ width: { xs: '100%', sm: '120px' } }}
                          value={aprs.altitude}
                          onChange={(e) => setAPRS({ ...aprs, altitude: e.target.value })}
                          placeholder="000000"
                        />
                      </Box>
                    </Box>

                    <Divider sx={{ my: 3 }} />

                    <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
                      <Button
                        variant="contained"
                        startIcon={<Save />}
                        onClick={handleSaveAPRS}
                        disabled={loading}
                      >
                        保存
                      </Button>
                    </Box>
                  </CardContent>
                </Card>
              </Box>

              {/* APRS日志卡片 */}
              <Box sx={{ flex: { xs: '1 1 auto', md: '0 1 50%' }, display: 'flex', minHeight: 0 }}>
                <Card sx={{ width: '100%', display: 'flex', flexDirection: 'column', height: configCardHeight || 'auto', minHeight: 0 }}>
                  <CardContent sx={{ flexGrow: 1, display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' }}>
                    <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                        <Terminal color="primary" />
                        <Typography variant="h6">
                          APRS日志
                        </Typography>
                      </Box>
                      <IconButton
                        size="small"
                        onClick={loadAPRSLogs}
                        disabled={aprsLogsLoading}
                      >
                        <Refresh />
                      </IconButton>
                    </Box>

                    <Box
                      sx={{
                        flexGrow: 1,
                        bgcolor: '#1e1e1e',
                        borderRadius: 1,
                        p: 2,
                        overflow: 'auto',
                        fontFamily: 'monospace',
                        fontSize: '0.875rem',
                        minHeight: 0,
                      }}
                    >
                      {aprsLogs.length === 0 ? (
                        <Typography variant="body2" sx={{ color: '#888' }}>
                          暂无日志
                        </Typography>
                      ) : (
                        aprsLogs.map((log, index) => (
                          <Box
                            key={index}
                            sx={{
                              mb: 0.5,
                              color: '#d4d4d4',
                              '&:hover': { bgcolor: 'rgba(255,255,255,0.05)' },
                              px: 0.5,
                              py: 0.25,
                              borderRadius: 0.5,
                            }}
                          >
                            <Typography
                              variant="body2"
                              component="span"
                              sx={{ color: '#569cd6', mr: 1 }}
                            >
                              [{log.timestamp}]
                            </Typography>
                            <Typography variant="body2" component="span">
                              {log.message}
                            </Typography>
                          </Box>
                        ))
                      )}
                    </Box>

                    <Box sx={{ mt: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <Chip
                        size="small"
                        label={`共 ${aprsLogs.length} 条`}
                        color="default"
                        variant="outlined"
                      />
                      <Typography variant="caption" color="text.secondary">
                        每10秒自动刷新
                      </Typography>
                    </Box>
                  </CardContent>
                </Card>
              </Box>
            </Box>
          </Box>
        </TabPanel>

        {/* OpenAI标签页 */}
        <TabPanel value={tabValue} index={2}>
          <Box sx={{ px: 2, maxWidth: 600 }}>
            <Card>
              <CardContent>
                <Typography variant="h6" gutterBottom>
                  OpenAI配置
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                  配置OpenAI API连接信息
                </Typography>

                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <TextField
                    label="Base URL"
                    fullWidth
                    value={openai.base_url}
                    onChange={(e) => setOpenAI({ ...openai, base_url: e.target.value })}
                    placeholder="https://api.openai.com/v1"
                  />

                  <TextField
                    label="API Key"
                    fullWidth
                    type="password"
                    value={openai.api_key}
                    onChange={(e) => setOpenAI({ ...openai, api_key: e.target.value })}
                    placeholder="sk-..."
                  />

                  <TextField
                    label="Engine/Model"
                    fullWidth
                    value={openai.engine}
                    onChange={(e) => setOpenAI({ ...openai, engine: e.target.value })}
                    placeholder="gpt-4"
                  />
                </Box>

                <Divider sx={{ my: 3 }} />

                <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
                  <Button
                    variant="contained"
                    startIcon={<Save />}
                    onClick={handleSaveOpenAI}
                    disabled={loading}
                  >
                    保存
                  </Button>
                </Box>
              </CardContent>
            </Card>
          </Box>
        </TabPanel>

        {/* 通信设置标签页 */}
        <TabPanel value={tabValue} index={3}>
          <Box sx={{ px: 2, maxWidth: 600 }}>
            <Card>
              <CardContent>
                <Typography variant="h6" gutterBottom>
                  通信设置
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                  配置音频记录保存策略
                </Typography>

                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                  {/* 启用音频记录 */}
                  <Box>
                    <Typography variant="subtitle1" sx={{ mb: 1, display: 'flex', alignItems: 'center', gap: 1 }}>
                      <PhoneInTalk color="primary" fontSize="small" />
                      音频记录
                    </Typography>
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                      开启后将自动记录每次通信的音频数据到MinIO
                    </Typography>
                    <FormControlLabel
                      control={
                        <Switch
                          checked={commSettings.enabled}
                          onChange={(e) => setCommSettings({ ...commSettings, enabled: e.target.checked })}
                          color="primary"
                        />
                      }
                      label={commSettings.enabled ? '已启用' : '已禁用'}
                    />
                  </Box>

                  <Divider />

                  <Divider />

                  <TextField
                    label="数据保留天数"
                    type="number"
                    fullWidth
                    value={commSettings.retention_days}
                    onChange={(e) => setCommSettings({ ...commSettings, retention_days: parseInt(e.target.value) || 0 })}
                    helperText="超过此天数的记录将被自动删除"
                    inputProps={{ min: 1, max: 365 }}
                    disabled={!commSettings.enabled}
                  />

                  <TextField
                    label="最小录制阈值（毫秒）"
                    type="number"
                    fullWidth
                    value={commSettings.min_duration_ms}
                    onChange={(e) => setCommSettings({ ...commSettings, min_duration_ms: parseInt(e.target.value) || 0 })}
                    helperText="少于此时长的音频不会上传到MinIO"
                    inputProps={{ min: 0, step: 100 }}
                    disabled={!commSettings.enabled}
                  />

				  <TextField
                    label="最大录制时长（秒）"
                    type="number"
                    fullWidth
                    value={commSettings.max_duration_sec}
                    onChange={(e) => setCommSettings({ ...commSettings, max_duration_sec: parseInt(e.target.value) || 0 })}
                    helperText="0表示不限制，通信超过此时长将自动断开"
                    inputProps={{ min: 0 }}
                    disabled={!commSettings.enabled}
                  />

				  <TextField
                    label="批量上传间隔（秒）"
                    type="number"
                    fullWidth
                    value={commSettings.batch_upload_sec}
                    onChange={(e) => setCommSettings({ ...commSettings, batch_upload_sec: parseInt(e.target.value) || 10 })}
                    helperText="音频数据批量上传到MinIO的间隔时间"
                    inputProps={{ min: 1, max: 300 }}
                    disabled={!commSettings.enabled}
                  />
                </Box>

                <Divider sx={{ my: 3 }} />

                <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
                  <Button
                    variant="contained"
                    startIcon={<Save />}
                    onClick={handleSaveCommSettings}
                    disabled={loading}
                  >
                    保存
                  </Button>
                </Box>
              </CardContent>
            </Card>
          </Box>
        </TabPanel>

        {/* SMTP配置标签页 */}
        <TabPanel value={tabValue} index={4}>
          <Box sx={{ px: 2, maxWidth: 600 }}>
            <Card>
              <CardContent>
                <Typography variant="h6" gutterBottom>
                  SMTP邮件配置
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                  配置SMTP服务器用于发送验证码邮件
                </Typography>

                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <TextField
                    label="SMTP服务器地址"
                    fullWidth
                    value={smtp.host}
                    onChange={(e) => setSMTP({ ...smtp, host: e.target.value })}
                    placeholder="例如：smtp.qq.com"
                  />

                  <TextField
                    label="SMTP端口"
                    type="number"
                    fullWidth
                    value={smtp.port}
                    onChange={(e) => setSMTP({ ...smtp, port: parseInt(e.target.value) || 465 })}
                    placeholder="465"
                    inputProps={{ min: 1, max: 65535 }}
                  />

                  <FormControlLabel
                    control={
                      <Switch
                        checked={smtp.use_ssl}
                        onChange={(e) => setSMTP({ ...smtp, use_ssl: e.target.checked })}
                      />
                    }
                    label="使用SSL加密"
                  />

                  <Divider sx={{ my: 1 }} />

                  <TextField
                    label="发件人昵称"
                    fullWidth
                    value={smtp.sender_name}
                    onChange={(e) => setSMTP({ ...smtp, sender_name: e.target.value })}
                    placeholder="例如：NRL火链"
                  />

                  <TextField
                    label="发件人邮箱"
                    fullWidth
                    type="email"
                    value={smtp.sender_email}
                    onChange={(e) => setSMTP({ ...smtp, sender_email: e.target.value })}
                    placeholder="例如：noreply@example.com"
                  />

                  <TextField
                    label="邮箱授权码"
                    fullWidth
                    type="password"
                    value={smtp.password}
                    onChange={(e) => setSMTP({ ...smtp, password: e.target.value })}
                    placeholder="邮箱SMTP授权码（非登录密码）"
                    helperText="请使用邮箱的SMTP授权码，而非登录密码"
                  />
                </Box>

                <Divider sx={{ my: 3 }} />

                <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
                  <Button
                    variant="contained"
                    startIcon={<Save />}
                    onClick={handleSaveSMTP}
                    disabled={loading}
                  >
                    保存
                  </Button>
                </Box>
              </CardContent>
            </Card>
          </Box>
        </TabPanel>

        {/* 操作日志标签页 */}
        <TabPanel value={tabValue} index={5}>
          <Box sx={{ px: 2 }}>
            <Card>
              <CardContent>
                <Typography variant="h6" gutterBottom>
                  操作日志
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                  查看系统操作日志记录
                </Typography>

                <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, gap: 2, mb: 2 }}>
                  <TextField
                    placeholder="搜索日志内容"
                    value={searchKeyword}
                    onChange={(e) => setSearchKeyword(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && loadOpLogs()}
                    size="small"
                    fullWidth
                    sx={{ maxWidth: { sm: 300 } }}
                  />
                  <FormControl size="small" sx={{ minWidth: 120 }}>
                    <InputLabel>事件类型</InputLabel>
                    <Select
                      value={eventType}
                      label="事件类型"
                      onChange={(e) => {
                        setEventType(e.target.value)
                        setLogPage(0)
                      }}
                    >
                      {EVENT_TYPES.map((type) => (
                        <MenuItem key={type.value} value={type.value}>
                          {type.label}
                        </MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                  <Button variant="outlined" size="small" startIcon={<Search />} onClick={loadOpLogs}>
                    搜索
                  </Button>
                </Box>

                <TableContainer component={Paper} variant="outlined" sx={{ overflow: 'auto' }}>
                  <Table sx={{ minWidth: 500 }}>
                    <TableHead>
                      <TableRow>
                        <TableCell>ID</TableCell>
                        <TableCell>时间</TableCell>
                        <TableCell>操作者</TableCell>
                        <TableCell>事件类型</TableCell>
                        <TableCell>内容</TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {opLogsLoading ? (
                        <TableRow>
                          <TableCell colSpan={5} align="center">
                            加载中...
                          </TableCell>
                        </TableRow>
                      ) : opLogs.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={5} align="center">
                            暂无数据
                          </TableCell>
                        </TableRow>
                      ) : (
                        opLogs.map((log) => (
                          <TableRow key={log.id} hover>
                            <TableCell>{log.id}</TableCell>
                            <TableCell>{formatTimestamp(log.timestamp)}</TableCell>
                            <TableCell>{log.operator || '-'}</TableCell>
                            <TableCell>
                              {log.event_type && (
                                <Chip
                                  label={getEventTypeLabel(log.event_type)}
                                  size="small"
                                  color={EVENT_TYPE_COLORS[log.event_type] || 'default'}
                                  variant="outlined"
                                />
                              )}
                            </TableCell>
                            <TableCell>{log.content}</TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                  <TablePagination
                    component="div"
                    count={opTotal}
                    page={logPage}
                    onPageChange={(_, newPage) => setLogPage(newPage)}
                    rowsPerPage={logRowsPerPage}
                    onRowsPerPageChange={(e) => {
                      setLogRowsPerPage(parseInt(e.target.value, 10))
                      setLogPage(0)
                    }}
                    labelRowsPerPage="每页行数"
                    labelDisplayedRows={({ from, to, count }) => `${from}-${to} 共 ${count}`}
                  />
                </TableContainer>
              </CardContent>
            </Card>
          </Box>
        </TabPanel>
      </Paper>
    </Box>
  )

  function formatTimestamp(timestamp: string) {
    return new Date(timestamp).toLocaleString('zh-CN')
  }
}
