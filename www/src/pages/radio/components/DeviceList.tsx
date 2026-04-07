/**
 * 设备列表组件
 */

import React, { useState, useEffect } from 'react'
import {
  Box,
  Typography,
  IconButton,
  List,
  ListItem,
  ListItemAvatar,
  ListItemText,
  Avatar,
  Chip,
  Divider,
  Skeleton,
  useTheme,
} from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import PersonIcon from '@mui/icons-material/Person'
import HeadsetIcon from '@mui/icons-material/Headset'
import ComputerIcon from '@mui/icons-material/Computer'
import PhoneIcon from '@mui/icons-material/Phone'
import TabletIcon from '@mui/icons-material/Tablet'
import ChatBubbleIcon from '@mui/icons-material/ChatBubble'
import AndroidIcon from '@mui/icons-material/Android'
import PhoneIphoneIcon from '@mui/icons-material/PhoneIphone'
import DesktopWindowsIcon from '@mui/icons-material/DesktopWindows'
import LaptopMacIcon from '@mui/icons-material/LaptopMac'
import LanguageIcon from '@mui/icons-material/Language'
import SettingsInputAntennaIcon from '@mui/icons-material/SettingsInputAntenna'
import { apiClient } from '../../../services'
import { DeviceModel } from '../../../types/radio'
import type { OnlineDevice } from '../../../types/radio'

interface DeviceListProps {
  groupId: number
  onClose: () => void
}

// 设备型号图标映射
const getDeviceIcon = (devModel: number) => {
  switch (devModel) {
    case 100: // 微信小程序
      return <ChatBubbleIcon />
    case 101: // Android
      return <AndroidIcon />
    case 102: // iOS
      return <PhoneIphoneIcon />
    case 103: // Windows
      return <DesktopWindowsIcon />
    case 104: // macOS
      return <LaptopMacIcon />
    case 105: // 浏览器
      return <LanguageIcon />
    case 106: // 互联设备
    case 107: // ESP32
    case 110: // 南山对讲桥接器
    case 111: // HT对讲桥接器
    case 112: // 涛涛对讲桥接器
      return <SettingsInputAntennaIcon />
    default:
      return <HeadsetIcon />
  }
}

// 设备型号名称映射
const getDeviceModelName = (devModel: number) => {
  switch (devModel) {
    case 100:
      return '微信小程序'
    case 101:
      return 'Android'
    case 102:
      return 'iOS'
    case 103:
      return 'Windows'
    case 104:
      return 'macOS'
    case 105:
      return '浏览器'
    case 106:
      return '互联设备'
    case 107:
      return 'ESP32 链路台/手咪'
    case 110:
      return '南山对讲桥接器'
    case 111:
      return 'HT 对讲桥接器'
    case 112:
      return '涛涛对讲桥接器'
    default:
      return '未知设备'
  }
}

// 头像颜色
const getAvatarColor = (callsign: string) => {
  const colors = [
    '#f44336',
    '#9c27b0',
    '#673ab7',
    '#3f51b5',
    '#009688',
    '#ff5722',
    '#795548',
    '#607d8b',
  ]
  let hash = 0
  for (let i = 0; i < callsign.length; i++) {
    hash = callsign.charCodeAt(i) + ((hash << 5) - hash)
  }
  return colors[Math.abs(hash) % colors.length]
}

export const DeviceList: React.FC<DeviceListProps> = ({ groupId, onClose }) => {
  const theme = useTheme()
  const [devices, setDevices] = useState<OnlineDevice[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // 加载设备列表
  const loadDevices = async () => {
    try {
      setLoading(true)
      const response = await apiClient.get<any>(`/api/groups/${groupId}/devices`)
      const data = response.data || []
      setDevices(data)
      setError(null)
    } catch (err) {
      console.error('Failed to load devices:', err)
      setError('加载设备列表失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadDevices()

    // 定时刷新
    const interval = setInterval(loadDevices, 10000)

    return () => clearInterval(interval)
  }, [groupId])

  // 分组设备：普通设备 vs 幽灵设备
  const normalDevices = devices.filter(d => !d.isGhost)
  const ghostDevices = devices.filter(d => d.isGhost)

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 头部 */}
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', p: 2, borderBottom: 1, borderColor: 'divider' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography variant="h6">在线设备</Typography>
          <Chip size="small" label={devices.length} color="primary" />
        </Box>
        <IconButton onClick={onClose} size="small">
          <CloseIcon />
        </IconButton>
      </Box>

      {/* 内容 */}
      <Box sx={{ flex: 1, overflow: 'auto', p: 2 }}>
        {error && (
          <Typography color="error" align="center" sx={{ py: 2 }}>
            {error}
          </Typography>
        )}

        {loading && devices.length === 0 ? (
          // 加载骨架屏
          <>
            {[1, 2, 3].map((i) => (
              <ListItem key={i} disableGutters>
                <ListItemAvatar>
                  <Skeleton variant="circular" width={40} height={40} />
                </ListItemAvatar>
                <ListItemText
                  primary={<Skeleton variant="text" width={120} />}
                  secondary={<Skeleton variant="text" width={80} />}
                />
              </ListItem>
            ))}
          </>
        ) : (
          <>
            {/* 普通设备 */}
            {normalDevices.length > 0 && (
              <>
                <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1 }}>
                  硬件设备 ({normalDevices.length})
                </Typography>
                <List disablePadding>
                  {normalDevices.map((device) => (
                    <ListItem key={device.id} disableGutters sx={{ mb: 1 }}>
                      <ListItemAvatar>
                        <Avatar sx={{ bgcolor: getAvatarColor(device.callsign) }}>
                          {getDeviceIcon(device.devModel)}
                        </Avatar>
                      </ListItemAvatar>
                      <ListItemText
                        primary={
                          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <Typography variant="body1">
                              {device.callsign}-{device.ssid}
                            </Typography>
                            {device.disableSend && (
                              <Chip size="small" label="禁发" color="warning" />
                            )}
                            {device.disableRecv && (
                              <Chip size="small" label="禁收" color="warning" />
                            )}
                          </Box>
                        }
                        secondary={
                          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <Typography variant="caption">
                              {getDeviceModelName(device.devModel)}
                            </Typography>
                            {device.nickname && (
                              <Typography variant="caption" color="text.secondary">
                                · {device.nickname}
                              </Typography>
                            )}
                          </Box>
                        }
                      />
                    </ListItem>
                  ))}
                </List>
              </>
            )}

            {/* 幽灵设备 */}
            {ghostDevices.length > 0 && (
              <>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1 }}>
                  浏览器客户端 ({ghostDevices.length})
                </Typography>
                <List disablePadding>
                  {ghostDevices.map((device) => (
                    <ListItem key={device.id} disableGutters sx={{ mb: 1 }}>
                      <ListItemAvatar>
                        <Avatar sx={{ bgcolor: getAvatarColor(device.callsign) }}>
                          <ComputerIcon />
                        </Avatar>
                      </ListItemAvatar>
                      <ListItemText
                        primary={
                          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <Typography variant="body1">
                              {device.callsign}-{device.ssid}
                            </Typography>
                            <Chip size="small" label="幽灵" variant="outlined" />
                          </Box>
                        }
                        secondary={
                          <Typography variant="caption">
                            {device.nickname || 'Web 客户端'}
                          </Typography>
                        }
                      />
                    </ListItem>
                  ))}
                </List>
              </>
            )}

            {/* 空状态 */}
            {devices.length === 0 && !loading && (
              <Typography variant="body2" color="text.secondary" align="center" sx={{ py: 4 }}>
                暂无在线设备
              </Typography>
            )}
          </>
        )}
      </Box>
    </Box>
  )
}

export default DeviceList
