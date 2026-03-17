/**
 * 在线收发页面
 */

import React, { useEffect, useState, useRef, useCallback } from 'react'
import {
  Box,
  Typography,
  IconButton,
  Select,
  MenuItem,
  FormControl,
  Chip,
  Avatar,
  Paper,
  TextField,
  Fab,
  Slider,
  Tooltip,
  Drawer,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Divider,
  CircularProgress,
  Alert,
  useTheme,
  useMediaQuery,
} from '@mui/material'
import type { SelectChangeEvent } from '@mui/material'
import {
  Mic as MicIcon,
  MicOff as MicOffIcon,
  VolumeUp as VolumeUpIcon,
  VolumeOff as VolumeOffIcon,
  Settings as SettingsIcon,
  Send as SendIcon,
  Group as GroupIcon,
  Headset as HeadsetIcon,
  Keyboard as KeyboardIcon,
  FiberManualRecord as RecordIcon,
  Close as CloseIcon,
} from '@mui/icons-material'

import { useAuth } from '../../hooks/useAuth'
import {
  RadioService,
  getRadioService,
  destroyRadioService,
} from '../../services/radioService'
import type {
  WSConnectionState,
  VoiceState,
  RadioMessage,
  RadioGroup,
  RadioUserConfig,
} from '../../types/radio'

// 子组件
import { MessageList } from './components/MessageList'
import { PTTButton } from './components/PTTButton'
import { AudioVisualizer } from './components/AudioVisualizer'
import { GroupSelector } from './components/GroupSelector'
import { RadioSettings } from './components/RadioSettings'
import { DeviceList } from './components/DeviceList'

// 样式
const useStyles = () => ({
  root: {
    // 使用固定高度计算，突破父容器的 padding 限制
    height: 'calc(100vh - 64px - 48px)', // 64px header + 24px padding (上下各 12px)
    margin: { xs: -2, sm: -3 }, // 抵消父容器的 padding
    display: 'flex',
    flexDirection: 'column',
    bgcolor: 'background.default',
    overflow: 'hidden', // 防止整体滚动
  },
  header: {
    flexShrink: 0, // 固定高度，不压缩
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    p: 1.5,
    borderBottom: 1,
    borderColor: 'divider',
    bgcolor: 'background.paper',
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
  },
  headerRight: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
  },
  connectionStatus: {
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
  },
  statusDot: {
    width: 8,
    height: 8,
    borderRadius: '50%',
  },
  messageArea: {
    flex: 1,
    minHeight: 0, // 关键：允许 flex 子元素收缩
    overflow: 'hidden',
    display: 'flex',
    flexDirection: 'column',
  },
  visualizer: {
    flexShrink: 0, // 固定高度，不压缩
    height: 48,
    borderBottom: 1,
    borderColor: 'divider',
    display: 'flex',
    alignItems: 'center',
    px: 2,
    gap: 2,
  },
  inputArea: {
    flexShrink: 0, // 固定高度，不压缩
    p: 2,
    borderTop: 1,
    borderColor: 'divider',
    bgcolor: 'background.paper',
  },
  inputRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
  },
  textInput: {
    flex: 1,
  },
  pttButton: {
    flex: 1, // 语音模式下 PTT 按钮全宽
    minHeight: 56,
  },
  settingsDrawer: {
    width: 320,
  },
  speakingIndicator: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    px: 1,
    py: 0.5,
    borderRadius: 2,
    bgcolor: 'primary.main',
    color: 'primary.contrastText',
    animation: 'pulse 1.5s infinite',
  },
})

// 状态颜色
const stateColors: Record<WSConnectionState, string> = {
  disconnected: '#9e9e9e',
  connecting: '#ff9800',
  authenticating: '#ff9800',
  online: '#4caf50',
  reconnecting: '#ff9800',
}

// 状态文本
const stateTexts: Record<WSConnectionState, string> = {
  disconnected: '已断开',
  connecting: '连接中',
  authenticating: '认证中',
  online: '已连接',
  reconnecting: '重连中',
}

export const RadioPage: React.FC = () => {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))
  const styles = useStyles()

  // 认证
  const { user, token } = useAuth()

  // Radio 服务
  const [radioService] = useState(() => getRadioService())

  // 状态
  const [connectionState, setConnectionState] = useState<WSConnectionState>('disconnected')
  const [voiceState, setVoiceState] = useState<VoiceState>('idle')
  const [currentSpeaker, setCurrentSpeaker] = useState<{ callsign: string; ssid: number } | null>(null)

  // 数据
  const [groups, setGroups] = useState<RadioGroup[]>([])
  const [currentGroupId, setCurrentGroupId] = useState<number>(999)
  const [messages, setMessages] = useState<RadioMessage[]>([])
  const [onlineDevices, setOnlineDevices] = useState<any[]>([])

  // UI 状态
  const [inputMode, setInputMode] = useState<'voice' | 'text'>('voice')
  const [textInput, setTextInput] = useState('')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [deviceListOpen, setDeviceListOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isPTTDown, setIsPTTDown] = useState(false)

  // 配置
  const [config, setConfig] = useState<RadioUserConfig>(radioService.getConfig())

  // Refs
  const messageListRef = useRef<HTMLDivElement>(null)

  // 初始化
  useEffect(() => {
    if (!user || !token) return

    const initRadio = async () => {
      try {
        // 设置事件处理器
        radioService.on('connectionStateChange', (state) => {
          setConnectionState(state)
        })

        radioService.on('voiceStateChange', (state, callsign) => {
          setVoiceState(state)
        })

        radioService.on('message', (message) => {
          setMessages(prev => [...prev, message])
        })

        radioService.on('speaking', (callsign, ssid) => {
          setCurrentSpeaker({ callsign, ssid })
        })

        radioService.on('speakingEnd', () => {
          setCurrentSpeaker(null)
        })

        radioService.on('error', (errorMsg) => {
          setError(errorMsg)
          setTimeout(() => setError(null), 5000)
        })

        // 初始化服务（传入用户上次选中的群组 ID，确保跨设备同步）
        await radioService.init(token, user!.username, user!.callsign || '', user!.last_group_id)

        // 加载群组
        const groupList = await radioService.getGroups()
        setGroups(groupList)
        setCurrentGroupId(radioService.getCurrentGroupId())

        // 连接
        await radioService.connect()

      } catch (error) {
        console.error('Failed to init radio:', error)
        setError('初始化失败')
      }
    }

    initRadio()

    return () => {
      // 清理
      radioService.disconnect()
    }
  }, [user, token])

  // 【自动刷新】定时刷新群组统计（每 5 秒）
  useEffect(() => {
    if (connectionState !== 'online') return

    const refreshStats = async () => {
      try {
        const updatedGroups = await radioService.refreshGroupStats()
        setGroups(updatedGroups)
      } catch (error) {
        console.error('Failed to refresh group stats:', error)
      }
    }

    // 立即刷新一次
    refreshStats()

    // 每 5 秒刷新一次
    const interval = setInterval(refreshStats, 5000)

    return () => {
      clearInterval(interval)
    }
  }, [connectionState, radioService])

  // PTT 按下
  const handlePTTDown = useCallback(() => {
    if (connectionState !== 'online') return
    if (voiceState !== 'idle') return

    setIsPTTDown(true)
    radioService.startVoice()
  }, [connectionState, voiceState, radioService])

  // PTT 松开
  const handlePTTUp = useCallback(() => {
    if (!isPTTDown) return

    setIsPTTDown(false)
    radioService.stopVoice()
  }, [isPTTDown, radioService])

  // 键盘事件
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.code === 'Space' && !e.repeat && inputMode === 'voice') {
        e.preventDefault()
        handlePTTDown()
      }
    }

    const handleKeyUp = (e: KeyboardEvent) => {
      if (e.code === 'Space' && inputMode === 'voice') {
        e.preventDefault()
        handlePTTUp()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    window.addEventListener('keyup', handleKeyUp)

    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('keyup', handleKeyUp)
    }
  }, [handlePTTDown, handlePTTUp, inputMode])

  // 切换群组
  const handleGroupChange = async (groupId: number) => {
    const success = await radioService.switchGroup(groupId)
    if (success) {
      setCurrentGroupId(groupId)
      setMessages([])
    }
  }

  // 发送文本消息
  const handleSendText = () => {
    if (!textInput.trim()) return

    radioService.sendTextMessage(textInput.trim())
    setTextInput('')
  }

  // 切换输入模式
  const toggleInputMode = () => {
    setInputMode(prev => prev === 'voice' ? 'text' : 'voice')
  }

  // 切换静音
  const toggleMute = () => {
    const newMuted = !config.muted
    radioService.setMuted(newMuted)
    setConfig(radioService.getConfig())
  }

  // 音量变化
  const handleVolumeChange = (_: Event, value: number | number[]) => {
    radioService.setVolume(value as number)
    setConfig(radioService.getConfig())
  }

  // 渲染连接状态
  const renderConnectionStatus = () => (
    <Box sx={styles.connectionStatus}>
      <Box
        sx={{
          ...styles.statusDot,
          bgcolor: stateColors[connectionState],
        }}
      />
      <Typography variant="body2" color="text.secondary">
        {stateTexts[connectionState]}
      </Typography>
    </Box>
  )

  // 渲染说话指示器
  const renderSpeakingIndicator = () => {
    if (!currentSpeaker) return null

    return (
      <Box sx={styles.speakingIndicator}>
        <RecordIcon sx={{ fontSize: 12 }} />
        <Typography variant="body2">
          {currentSpeaker.callsign}-{currentSpeaker.ssid} 正在说话
        </Typography>
      </Box>
    )
  }

  return (
    <Box sx={styles.root}>
      {/* 头部 */}
      <Box sx={styles.header}>
        <Box sx={styles.headerLeft}>
          <GroupSelector
            groups={groups}
            currentGroupId={currentGroupId}
            onChange={handleGroupChange}
            disabled={connectionState !== 'online'}
          />
          {renderSpeakingIndicator()}
        </Box>

        <Box sx={styles.headerRight}>
          <Chip
            icon={<HeadsetIcon />}
            label={onlineDevices.length}
            size="small"
            onClick={() => setDeviceListOpen(true)}
          />
          {renderConnectionStatus()}
          <IconButton onClick={() => setSettingsOpen(true)}>
            <SettingsIcon />
          </IconButton>
        </Box>
      </Box>

      {/* 错误提示 */}
      {error && (
        <Alert severity="error" onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 消息列表 */}
      <Box sx={styles.messageArea}>
        <MessageList
          ref={messageListRef}
          messages={messages}
          currentCallsign={user?.callsign || ''}
          currentSSID={105}
          currentUser={user}
        />
      </Box>

      {/* 音频可视化 */}
      <Box sx={styles.visualizer}>
        <AudioVisualizer
          isActive={voiceState !== 'idle'}
          isSending={voiceState === 'sending'}
        />
        {voiceState === 'receiving' && currentSpeaker && (
          <Typography variant="body2" color="primary">
            🔴 {currentSpeaker.callsign}-{currentSpeaker.ssid}
          </Typography>
        )}
      </Box>

      {/* 输入区域 */}
      <Box sx={styles.inputArea}>
        <Box sx={styles.inputRow}>
          {/* 模式切换 */}
          <IconButton onClick={toggleInputMode} color="primary">
            {inputMode === 'voice' ? <KeyboardIcon /> : <MicIcon />}
          </IconButton>

          {/* 文本输入模式 */}
          {inputMode === 'text' ? (
            <>
              <TextField
                sx={styles.textInput}
                size="small"
                placeholder="输入消息..."
                value={textInput}
                onChange={(e) => setTextInput(e.target.value)}
                onKeyPress={(e) => e.key === 'Enter' && handleSendText()}
                disabled={connectionState !== 'online'}
              />
              <IconButton
                color="primary"
                onClick={handleSendText}
                disabled={!textInput.trim() || connectionState !== 'online'}
              >
                <SendIcon />
              </IconButton>
            </>
          ) : (
            /* PTT 按钮 - 全宽 */
            <Box sx={{ flex: 1, display: 'flex' }}>
              <PTTButton
                isPressed={isPTTDown}
                onMouseDown={handlePTTDown}
                onMouseUp={handlePTTUp}
                onMouseLeave={handlePTTUp}
                onTouchStart={handlePTTDown}
                onTouchEnd={handlePTTUp}
                disabled={connectionState !== 'online' || voiceState === 'receiving'}
                fullWidth
              />
            </Box>
          )}

          {/* 音量控制 */}
          <IconButton onClick={toggleMute} color={config.muted ? 'error' : 'default'}>
            {config.muted ? <VolumeOffIcon /> : <VolumeUpIcon />}
          </IconButton>
        </Box>

        {/* PTT 提示 */}
        {inputMode === 'voice' && (
          <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block', textAlign: 'center' }}>
            按住 PTT 或空格键说话
          </Typography>
        )}
      </Box>

      {/* 设置抽屉 */}
      <Drawer
        anchor="right"
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
      >
        <Box sx={styles.settingsDrawer}>
          <RadioSettings
            config={config}
            onConfigChange={(newConfig) => {
              setConfig(newConfig)
            }}
            onClose={() => setSettingsOpen(false)}
            // 【核心修复】在这里统筹清理逻辑，实现三端同步清理
            onRequestClearCache={async () => {
              // 1. 调用 Service 彻底清空数据库和 Service 的内部内存
              const success = await radioService.clearAllMessageCache()

              if (success) {
                // 2. 清空当前屏幕上的 React State，让画面立刻变空白
                setMessages([])
              } else {
                throw new Error('清理失败')
              }
            }}
          />
        </Box>
      </Drawer>

      {/* 设备列表抽屉 */}
      <Drawer
        anchor="right"
        open={deviceListOpen}
        onClose={() => setDeviceListOpen(false)}
      >
        <Box sx={styles.settingsDrawer}>
          <DeviceList
            groupId={currentGroupId}
            onClose={() => setDeviceListOpen(false)}
          />
        </Box>
      </Drawer>
    </Box>
  )
}

export default RadioPage
