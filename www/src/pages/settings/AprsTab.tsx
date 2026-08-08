import { useCallback, useEffect, useRef, useState } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import {
  Alert, Box, Button, Card, CardContent, Chip, Divider, IconButton, TextField, Typography,
} from '@mui/material'
import MyLocation from '@mui/icons-material/MyLocation'
import Refresh from '@mui/icons-material/Refresh'
import Save from '@mui/icons-material/Save'
import Terminal from '@mui/icons-material/Terminal'
import { TabPanel } from '../../components/common/TabPanel'
import { getAPRSLogs } from './api'
import type { APRSConfig, APRSLogEntry, SiteMessage } from './types'

interface AprsTabProps {
  value: number
  config: APRSConfig
  setConfig: Dispatch<SetStateAction<APRSConfig>>
  loading: boolean
  onSave: () => void
  showMessage: (type: SiteMessage['type'], text: string) => void
}

export function AprsTab({ value, config, setConfig, loading, onSave, showMessage }: AprsTabProps) {
  const [logs, setLogs] = useState<APRSLogEntry[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const [locating, setLocating] = useState(false)
  const [configCardHeight, setConfigCardHeight] = useState<number | null>(null)
  const configCardRef = useRef<HTMLDivElement>(null)

  const loadLogs = useCallback(async () => {
    setLogsLoading(true)
    try {
      setLogs(await getAPRSLogs())
    } catch (error) {
      console.error('Failed to load APRS logs:', error)
    } finally {
      setLogsLoading(false)
    }
  }, [])

  useEffect(() => {
    if (value !== 2) return
    void loadLogs()
    const interval = setInterval(() => void loadLogs(), 10000)
    return () => clearInterval(interval)
  }, [loadLogs, value])

  useEffect(() => {
    if (value !== 2 || !configCardRef.current) return
    const updateHeight = () => {
      if (configCardRef.current) setConfigCardHeight(configCardRef.current.offsetHeight)
    }
    updateHeight()
    const resizeObserver = new ResizeObserver(updateHeight)
    resizeObserver.observe(configCardRef.current)
    return () => resizeObserver.disconnect()
  }, [value])

  const handleGetLocation = () => {
    if (!navigator.geolocation) {
      showMessage('error', '浏览器不支持地理位置功能')
      return
    }

    setLocating(true)
    navigator.geolocation.getCurrentPosition(
      (position) => {
        setConfig((current) => ({
          ...current,
          latitude: position.coords.latitude,
          longitude: position.coords.longitude,
          altitude: position.coords.altitude?.toFixed(1) || current.altitude,
        }))
        showMessage('success', '位置获取成功')
        setLocating(false)
      },
      (error) => {
        let message = '获取位置失败'
        switch (error.code) {
          case error.PERMISSION_DENIED:
            message = '位置权限被拒绝，请在浏览器设置中允许'
            break
          case error.POSITION_UNAVAILABLE:
            message = '位置信息不可用'
            break
          case error.TIMEOUT:
            message = '获取位置超时'
            break
        }
        showMessage('error', message)
        setLocating(false)
      },
      { enableHighAccuracy: true, timeout: 10000 },
    )
  }

  return (
    <TabPanel value={value} index={2}>
      <Box sx={{ px: 2 }}>
        <Box sx={{ display: 'flex', flexDirection: { xs: 'column', md: 'row' }, gap: 2, alignItems: { xs: 'stretch', md: 'flex-start' } }}>
          <Box sx={{ flex: { xs: '1 1 auto', md: '0 1 50%' } }}>
            <Card ref={configCardRef}>
              <CardContent>
                <Typography variant="h6" gutterBottom>APRS配置</Typography>
                <Alert severity="info" sx={{ mb: 2 }}>
                  此配置用于服务器的APRS上报，将服务器信息（如在线设备数、服务器地址等）上报到APRS网络，而非设备的APRS配置。
                </Alert>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                  配置APRS服务器连接信息
                </Typography>

                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <AddressRow separator=":" left={(
                    <TextField
                      label="APRS服务器" fullWidth value={config.aprs_server_host}
                      onChange={(event) => setConfig({ ...config, aprs_server_host: event.target.value })}
                      placeholder="china.aprs2.net"
                    />
                  )} right={(
                    <TextField
                      label="端口" sx={{ width: { xs: '100%', sm: '100px' } }} value={config.aprs_server_port}
                      onChange={(event) => setConfig({ ...config, aprs_server_port: event.target.value })}
                      placeholder="14580"
                    />
                  )} />
                  <AddressRow separator=":" left={(
                    <TextField
                      label="本机地址" fullWidth value={config.self_address}
                      onChange={(event) => setConfig({ ...config, self_address: event.target.value })}
                      placeholder="yourdomain.com"
                    />
                  )} right={(
                    <TextField
                      label="端口" sx={{ width: { xs: '100%', sm: '100px' } }} value={config.self_port}
                      onChange={(event) => setConfig({ ...config, self_port: event.target.value })}
                      placeholder="60050"
                    />
                  )} />
                  <AddressRow separator="-" left={(
                    <TextField
                      label="呼号" fullWidth value={config.callsign}
                      onChange={(event) => setConfig({ ...config, callsign: event.target.value })}
                      placeholder="BH0AAA"
                    />
                  )} right={(
                    <TextField
                      label="SSID" sx={{ width: { xs: '100%', sm: '100px' } }} value={config.ssid}
                      onChange={(event) => setConfig({ ...config, ssid: event.target.value })}
                      placeholder="10"
                    />
                  )} />

                  <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, alignItems: { xs: 'stretch', sm: 'center' }, gap: 1 }}>
                    <TextField
                      label="经度" sx={{ width: { xs: '100%', sm: '150px' } }} type="number"
                      inputProps={{ step: 0.000001, min: -180, max: 180 }} value={config.longitude || ''}
                      onChange={(event) => setConfig({ ...config, longitude: parseFloat(event.target.value) || 0 })}
                      placeholder="0.000000" helperText="-180 到 180"
                      error={config.longitude < -180 || config.longitude > 180}
                    />
                    <Separator>,</Separator>
                    <TextField
                      label="纬度" sx={{ width: { xs: '100%', sm: '150px' } }} type="number"
                      inputProps={{ step: 0.000001, min: -90, max: 90 }} value={config.latitude || ''}
                      onChange={(event) => setConfig({ ...config, latitude: parseFloat(event.target.value) || 0 })}
                      placeholder="0.000000" helperText="-90 到 90"
                      error={config.latitude < -90 || config.latitude > 90}
                    />
                    <Separator>,</Separator>
                    <TextField
                      label="海拔(m)" sx={{ width: { xs: '100%', sm: '120px' } }} value={config.altitude}
                      onChange={(event) => setConfig({ ...config, altitude: event.target.value })}
                      placeholder="000000" helperText=" "
                    />
                    <Button
                      variant="outlined" startIcon={<MyLocation />} onClick={handleGetLocation} disabled={locating}
                      sx={{ minWidth: 'auto', whiteSpace: 'nowrap', mt: '6px' }}
                    >
                      {locating ? '定位中...' : '获取位置'}
                    </Button>
                  </Box>
                </Box>

                <Divider sx={{ my: 3 }} />
                <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
                  <Button variant="contained" startIcon={<Save />} onClick={onSave} disabled={loading}>保存</Button>
                </Box>
              </CardContent>
            </Card>
          </Box>

          <Box sx={{ flex: { xs: '1 1 auto', md: '0 1 50%' }, display: 'flex', minHeight: 0 }}>
            <Card sx={{ width: '100%', display: 'flex', flexDirection: 'column', height: configCardHeight || 'auto', minHeight: 0 }}>
              <CardContent sx={{ flexGrow: 1, display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' }}>
                <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Terminal color="primary" />
                    <Typography variant="h6">APRS日志</Typography>
                  </Box>
                  <IconButton size="small" onClick={() => void loadLogs()} disabled={logsLoading}><Refresh /></IconButton>
                </Box>
                <Box sx={{
                  flexGrow: 1, bgcolor: '#1e1e1e', borderRadius: 1, p: 2, overflow: 'auto',
                  fontFamily: 'monospace', fontSize: '0.875rem', minHeight: 0,
                }}>
                  {logs.length === 0 ? (
                    <Typography variant="body2" sx={{ color: '#888' }}>暂无日志</Typography>
                  ) : logs.map((log, index) => (
                    <Box
                      key={index}
                      sx={{ mb: 0.5, color: '#d4d4d4', '&:hover': { bgcolor: 'rgba(255,255,255,0.05)' }, px: 0.5, py: 0.25, borderRadius: 0.5 }}
                    >
                      <Typography variant="body2" component="span" sx={{ color: '#569cd6', mr: 1 }}>
                        [{log.timestamp}]
                      </Typography>
                      <Typography variant="body2" component="span">{log.message}</Typography>
                    </Box>
                  ))}
                </Box>
                <Box sx={{ mt: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <Chip size="small" label={`共 ${logs.length} 条`} color="default" variant="outlined" />
                  <Typography variant="caption" color="text.secondary">每10秒自动刷新</Typography>
                </Box>
              </CardContent>
            </Card>
          </Box>
        </Box>
      </Box>
    </TabPanel>
  )
}

function Separator({ children }: { children: React.ReactNode }) {
  return (
    <Typography sx={{
      fontSize: '1.2rem', fontWeight: 'bold', color: 'text.secondary',
      display: { xs: 'none', sm: 'block' }, minWidth: '20px', textAlign: 'center',
    }}>
      {children}
    </Typography>
  )
}

function AddressRow({ separator, left, right }: { separator: string; left: React.ReactNode; right: React.ReactNode }) {
  return (
    <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, alignItems: { xs: 'stretch', sm: 'center' }, gap: 1 }}>
      {left}<Separator>{separator}</Separator>{right}
    </Box>
  )
}
