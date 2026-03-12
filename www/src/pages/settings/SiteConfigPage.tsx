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
} from '@mui/material'
import {
  Save,
  Public,
  Refresh,
  Terminal,
  CloudUpload,
  Delete,
  Search,
} from '@mui/icons-material'
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
  passcode: number
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

// 操作日志
const EVENT_TYPES = [
  { value: '', label: '全部' },
  { value: 'login', label: '登录' },
  { value: 'logout', label: '登出' },
  { value: 'create', label: '创建' },
  { value: 'update', label: '更新' },
  { value: 'delete', label: '删除' },
]

const EVENT_TYPE_COLORS: Record<string, any> = {
  login: 'info',
  logout: 'default',
  create: 'success',
  update: 'warning',
  delete: 'error',
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
    passcode: 0,
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
      const [icpRes, systemRes, aprsRes, openaiRes] = await Promise.all([
        apiClient.get<any>('/api/config/category/icp'),
        apiClient.get<any>('/api/config/category/system'),
        apiClient.get<any>('/api/config/aprs'),
        apiClient.get<any>('/api/config/openai'),
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
    } catch (err) {
      showMessage('error', '保存系统信息失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSaveAPRS = async () => {
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

  const handleLogoDelete = () => {
    setSystemInfo({ ...systemInfo, logo_url: '' })
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
    if (tabValue === 3) {
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
                    placeholder="例如：NRL-福建开发组"
                  />

                  <TextField
                    label="站点简称"
                    fullWidth
                    value={systemInfo.nameshorthand}
                    onChange={(e) => setSystemInfo({ ...systemInfo, nameshorthand: e.target.value })}
                    placeholder="例如：NRL-Fujian"
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
                      <TextField
                        label="APRS服务器地址"
                        fullWidth
                        value={aprs.aprs_server_host}
                        onChange={(e) => setAPRS({ ...aprs, aprs_server_host: e.target.value })}
                        placeholder="china.aprs2.net"
                      />

                      <TextField
                        label="APRS服务器端口"
                        fullWidth
                        value={aprs.aprs_server_port}
                        onChange={(e) => setAPRS({ ...aprs, aprs_server_port: e.target.value })}
                        placeholder="14580"
                      />

                      <TextField
                        label="本机地址"
                        fullWidth
                        value={aprs.self_address}
                        onChange={(e) => setAPRS({ ...aprs, self_address: e.target.value })}
                        placeholder="yourdomain.com"
                      />

                      <TextField
                        label="本机端口"
                        fullWidth
                        value={aprs.self_port}
                        onChange={(e) => setAPRS({ ...aprs, self_port: e.target.value })}
                        placeholder="60050"
                      />

                      <TextField
                        label="呼号"
                        fullWidth
                        value={aprs.callsign}
                        onChange={(e) => setAPRS({ ...aprs, callsign: e.target.value })}
                        placeholder="BH0AAA"
                      />

                      <TextField
                        label="SSID"
                        fullWidth
                        value={aprs.ssid}
                        onChange={(e) => setAPRS({ ...aprs, ssid: e.target.value })}
                        placeholder="10"
                      />

                      <TextField
                        label="Passcode"
                        fullWidth
                        type="number"
                        value={aprs.passcode || ''}
                        onChange={(e) => setAPRS({ ...aprs, passcode: parseInt(e.target.value) || 0 })}
                        placeholder="-1"
                      />

                      <TextField
                        label="纬度"
                        fullWidth
                        type="number"
                        inputProps={{ step: 0.000001 }}
                        value={aprs.latitude || ''}
                        onChange={(e) => setAPRS({ ...aprs, latitude: parseFloat(e.target.value) || 0 })}
                        placeholder="0.000000"
                      />

                      <TextField
                        label="经度"
                        fullWidth
                        type="number"
                        inputProps={{ step: 0.000001 }}
                        value={aprs.longitude || ''}
                        onChange={(e) => setAPRS({ ...aprs, longitude: parseFloat(e.target.value) || 0 })}
                        placeholder="0.000000"
                      />

                      <TextField
                        label="海拔高度"
                        fullWidth
                        value={aprs.altitude}
                        onChange={(e) => setAPRS({ ...aprs, altitude: e.target.value })}
                        placeholder="000000"
                      />
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

              {/* APRS日志卡��� */}
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

        {/* 操作日志标签页 */}
        <TabPanel value={tabValue} index={3}>
          <Box sx={{ px: 2 }}>
            <Card>
              <CardContent>
                <Typography variant="h6" gutterBottom>
                  操作日志
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                  查看系统操作日志记录
                </Typography>

                <Box sx={{ display: 'flex', gap: 2, mb: 2, flexWrap: 'wrap' }}>
                  <TextField
                    placeholder="搜索日志内容"
                    value={searchKeyword}
                    onChange={(e) => setSearchKeyword(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && loadOpLogs()}
                    size="small"
                    sx={{ flexGrow: 1, minWidth: 200 }}
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
                  <Button variant="outlined" startIcon={<Search />} onClick={loadOpLogs}>
                    搜索
                  </Button>
                </Box>

                <TableContainer component={Paper} variant="outlined">
                  <Table>
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
                                  label={log.event_type}
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
