/**
 * 设备列表组件
 */

import React, { useCallback, useEffect, useState } from 'react'
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
} from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import HeadsetIcon from '@mui/icons-material/Headset'
import ComputerIcon from '@mui/icons-material/Computer'
import { apiClient } from '../../../services'
import type { OnlineDevice } from '../../../types/radio'
import { getDevModelIcon, getDevModelName } from '../../../utils/deviceModel'
import { getErrorMessage } from '../../../utils/errorMessage'

interface DeviceListProps {
  groupId: number
  onClose: () => void
}

interface GroupDevicesResponse {
  data?: OnlineDevice[]
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
  const [devices, setDevices] = useState<OnlineDevice[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // 加载设备列表
  const loadDevices = useCallback(async () => {
    try {
      setLoading(true)
      const response = await apiClient.get<GroupDevicesResponse>(`/api/groups/${groupId}/devices`)
      const data = response.data || []
      setDevices(data)
      setError(null)
    } catch (err) {
      console.error('Failed to load devices:', err)
      setError(getErrorMessage(err, '加载设备列表失败'))
    } finally {
      setLoading(false)
    }
  }, [groupId])

  useEffect(() => {
    loadDevices()

    // 定时刷新
    const interval = setInterval(loadDevices, 10000)

    return () => clearInterval(interval)
  }, [loadDevices])

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
                          {getDevModelIcon(device.devModel) || <HeadsetIcon />}
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
                              {getDevModelName(device.devModel)}
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
                  幽灵客户端 ({ghostDevices.length})
                </Typography>
                <List disablePadding>
                  {ghostDevices.map((device) => (
                    <ListItem key={device.id} disableGutters sx={{ mb: 1 }}>
                      <ListItemAvatar>
                        <Avatar sx={{ bgcolor: getAvatarColor(device.callsign) }}>
                          {getDevModelIcon(device.devModel) || <ComputerIcon />}
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
                            {device.nickname || getDevModelName(device.devModel)}
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
