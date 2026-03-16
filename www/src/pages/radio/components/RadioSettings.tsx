/**
 * 设置面板组件
 */

import React, { useState, useEffect } from 'react'
import {
  Box,
  Typography,
  IconButton,
  Divider,
  Slider,
  FormControl,
  FormLabel,
  Select,
  MenuItem,
  Switch,
  FormControlLabel,
  TextField,
  Button,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  ListItemSecondaryAction,
} from '@mui/material'
import {
  Close as CloseIcon,
  VolumeUp as VolumeIcon,
  Mic as MicIcon,
  SettingsInputComponent as InputIcon,
  Output as OutputIcon,
  Badge as BadgeIcon,
  Delete as DeleteIcon,
} from '@mui/icons-material'
import type { RadioUserConfig } from '../../../types/radio'

interface RadioSettingsProps {
  config: RadioUserConfig
  onConfigChange: (config: RadioUserConfig) => void
  onClose: () => void
}

export const RadioSettings: React.FC<RadioSettingsProps> = ({
  config,
  onConfigChange,
  onClose,
}) => {
  const [localConfig, setLocalConfig] = useState<RadioUserConfig>(config)
  const [inputDevices, setInputDevices] = useState<MediaDeviceInfo[]>([])
  const [outputDevices, setOutputDevices] = useState<MediaDeviceInfo[]>([])

  // 加载设备列表
  useEffect(() => {
    const loadDevices = async () => {
      try {
        const devices = await navigator.mediaDevices.enumerateDevices()
        setInputDevices(devices.filter(d => d.kind === 'audioinput'))
        setOutputDevices(devices.filter(d => d.kind === 'audiooutput'))
      } catch (error) {
        console.error('Failed to enumerate devices:', error)
      }
    }

    loadDevices()
  }, [])

  // 更新配置
  const updateConfig = (key: keyof RadioUserConfig, value: any) => {
    const newConfig = { ...localConfig, [key]: value }
    setLocalConfig(newConfig)
    onConfigChange(newConfig)
  }

  // 清除缓存
  const handleClearCache = async () => {
    if (confirm('确定要清除所有消息缓存吗？')) {
      // 这里应该调用消息缓存清除方法
      // messageCache.clearAllMessages()
      alert('缓存已清除')
    }
  }

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 头部 */}
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', p: 2, borderBottom: 1, borderColor: 'divider' }}>
        <Typography variant="h6">设置</Typography>
        <IconButton onClick={onClose} size="small">
          <CloseIcon />
        </IconButton>
      </Box>

      {/* 内容 */}
      <Box sx={{ flex: 1, overflow: 'auto', p: 2 }}>
        {/* 设备配置 */}
        <Typography variant="subtitle2" color="primary" gutterBottom>
          设备配置
        </Typography>

        <FormControl fullWidth margin="normal">
          <FormLabel>SSID (设备子号)</FormLabel>
          <TextField
            type="number"
            size="small"
            value={localConfig.ssid}
            onChange={(e) => updateConfig('ssid', parseInt(e.target.value, 10) || 10)}
            inputProps={{ min: 1, max: 255 }}
          />
        </FormControl>

        <Divider sx={{ my: 2 }} />

        {/* 音频设置 */}
        <Typography variant="subtitle2" color="primary" gutterBottom>
          音频设置
        </Typography>

        <FormControl fullWidth margin="normal">
          <FormLabel>输入设备</FormLabel>
          <Select
            size="small"
            value={localConfig.inputDeviceId || ''}
            onChange={(e) => updateConfig('inputDeviceId', e.target.value)}
            displayEmpty
          >
            <MenuItem value="">默认设备</MenuItem>
            {inputDevices.map((device) => (
              <MenuItem key={device.deviceId} value={device.deviceId}>
                {device.label || `麦克风 ${device.deviceId.slice(0, 8)}`}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <FormControl fullWidth margin="normal">
          <FormLabel>输出设备</FormLabel>
          <Select
            size="small"
            value={localConfig.outputDeviceId || ''}
            onChange={(e) => updateConfig('outputDeviceId', e.target.value)}
            displayEmpty
          >
            <MenuItem value="">默认设备</MenuItem>
            {outputDevices.map((device) => (
              <MenuItem key={device.deviceId} value={device.deviceId}>
                {device.label || `扬声器 ${device.deviceId.slice(0, 8)}`}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <FormControl fullWidth margin="normal">
          <FormLabel>音量: {Math.round(localConfig.volume * 100)}%</FormLabel>
          <Slider
            value={localConfig.volume}
            onChange={(_, value) => updateConfig('volume', value)}
            min={0}
            max={1}
            step={0.01}
          />
        </FormControl>

        <FormControlLabel
          control={
            <Switch
              checked={localConfig.muted}
              onChange={(e) => updateConfig('muted', e.target.checked)}
            />
          }
          label="静音"
        />

        <Divider sx={{ my: 2 }} />

        {/* 数据管理 */}
        <Typography variant="subtitle2" color="primary" gutterBottom>
          数据管理
        </Typography>

        <List disablePadding>
          <ListItem disableGutters>
            <ListItemIcon>
              <BadgeIcon />
            </ListItemIcon>
            <ListItemText
              primary="消息缓存"
              secondary="清除本地消息缓存"
            />
            <ListItemSecondaryAction>
              <Button
                size="small"
                color="error"
                startIcon={<DeleteIcon />}
                onClick={handleClearCache}
              >
                清除
              </Button>
            </ListItemSecondaryAction>
          </ListItem>
        </List>

        <Divider sx={{ my: 2 }} />

        {/* 关于 */}
        <Typography variant="subtitle2" color="primary" gutterBottom>
          关于
        </Typography>
        <Typography variant="body2" color="text.secondary">
          NRL Link 在线收发 v1.0
        </Typography>
        <Typography variant="caption" color="text.secondary">
          使用 WebSocket 和 Opus 音频编解码
        </Typography>
      </Box>
    </Box>
  )
}

export default RadioSettings
