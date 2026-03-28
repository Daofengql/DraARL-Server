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
  Button,
  useTheme,
  useMediaQuery,
} from '@mui/material'
import type { SelectChangeEvent } from '@mui/material'
import MicIcon from '@mui/icons-material/Mic'
import MicOffIcon from '@mui/icons-material/MicOff'
import VolumeUpIcon from '@mui/icons-material/VolumeUp'
import VolumeOffIcon from '@mui/icons-material/VolumeOff'
import SendIcon from '@mui/icons-material/Send'
import GroupIcon from '@mui/icons-material/Group'
import HeadsetIcon from '@mui/icons-material/Headset'
import KeyboardIcon from '@mui/icons-material/Keyboard'
import RecordIcon from '@mui/icons-material/FiberManualRecord'
import CloseIcon from '@mui/icons-material/Close'

import { useAuth } from '../../hooks/useAuth'
import {
  RadioService,
  getRadioService,
  destroyRadioService,
} from '../../services/radioService'
import { messageSyncService } from '../../services/radio/messageSync'
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
import { GroupSelector } from './components/GroupSelector'
import { DeviceList } from './components/DeviceList'

// 样式
const useStyles = () => ({
  root: {
    // 使用固定高度填满视口，减去顶部导航栏 64px
    height: 'calc(100vh - 64px)',
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
  const [deviceListOpen, setDeviceListOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isPTTDown, setIsPTTDown] = useState(false)
  const [connectionConflict, setConnectionConflict] = useState(false) // 【新增】连接冲突状态
  const [audioPermissionNeeded, setAudioPermissionNeeded] = useState(false) // 音频权限提示
  const [isLoadingMore, setIsLoadingMore] = useState(false) // 加载更多状态

  // 配置
  const [config, setConfig] = useState<RadioUserConfig>(radioService.getConfig())

  // Refs
  const messageListRef = useRef<HTMLDivElement>(null)
  const messagesRef = useRef<RadioMessage[]>(messages)
  const isPlayingVoiceRef = useRef(false)
  const pendingSyncMessagesRef = useRef<RadioMessage[] | null>(null)

  // 保持 messagesRef 与 messages 同步
  useEffect(() => {
    messagesRef.current = messages
  }, [messages])

  // 供 MessageList 回调 - 播放状态变化
  const handleVoicePlayStateChange = useCallback((playing: boolean) => {
    isPlayingVoiceRef.current = playing
    // 播放结束后，应用挂起的同步结果
    if (!playing && pendingSyncMessagesRef.current) {
      setMessages(pendingSyncMessagesRef.current)
      pendingSyncMessagesRef.current = null
    }
  }, [])

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

        // 【新增】处理连接冲突事件
        radioService.on('conflict', () => {
          setConnectionConflict(true)
          setError('您的账号已在其他页面建立了电台连接，请先断开其他页面的连接')
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

    // 检查音频权限状态（浏览器自动博放策略）
    const checkAudioPermission = () => {
      // 创建临时 AudioContext 检查状态
      const audioContext = new (window.AudioContext || (window as any).webkitAudioContext)()
      if (audioContext.state === 'suspended') {
        setAudioPermissionNeeded(true)
      }
      audioContext.close()
    }
    checkAudioPermission()

    return () => {
      // 清理
      radioService.disconnect()
    }
  }, [user, token])

  // 激活音频权限
  const handleActivateAudio = useCallback(async () => {
    try {
      // 请求麦克风权限并激活 AudioContext
      await navigator.mediaDevices.getUserMedia({ audio: true })
      setAudioPermissionNeeded(false)
    } catch (error) {
      console.error('Failed to activate audio:', error)
      setError('无法获取音频权限，请检查浏览器设置')
    }
  }, [])

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

  // 【消息同步】每 15 秒从后端同步消息（斩杀线策略）
  useEffect(() => {
    if (connectionState !== 'online') return

    const syncMessages = async () => {
      try {
        // 传递当前用户信息，用于判断 isSelf
        const currentUser = user?.callsign ? {
          username: user.username,
          callsign: user.callsign,
          ssid: 105  // 网页设备固定 SSID=105
        } : undefined

        // 使用 ref 获取最新的消息列表（避免闭包捕获过期值）
        const currentMessages = messagesRef.current
        const merged = await messageSyncService.syncMessages(currentGroupId, currentMessages, currentUser)

        // 用 ID 集合比较，避免 Blob 序列化问题
        const hasChanges = (() => {
          if (merged.length !== currentMessages.length) return true
          const currentIds = new Set(currentMessages.map(m => m.id))
          return merged.some(m => !currentIds.has(m.id))
        })()

        if (!hasChanges) return

        if (isPlayingVoiceRef.current) {
          // 正在播放：挂起同步结果，等播放结束后再应用
          pendingSyncMessagesRef.current = merged
        } else {
          setMessages(merged)
        }
      } catch (error) {
        console.error('[RadioPage] Failed to sync messages:', error)
      }
    }

    // 首次立即同步
    syncMessages()

    // 每 15 秒同步一次
    const interval = setInterval(syncMessages, 15000)

    return () => {
      clearInterval(interval)
    }
  }, [connectionState, currentGroupId, radioService, user])

  // 【加载更多历史消息】
  const handleLoadMore = useCallback(async () => {
    if (isLoadingMore || !messageSyncService.hasMore(currentGroupId)) return

    setIsLoadingMore(true)
    try {
      const currentUser = user?.callsign ? {
        username: user.username,
        callsign: user.callsign,
        ssid: 105
      } : undefined

      const olderMessages = await messageSyncService.loadMoreMessages(currentGroupId, currentUser)
      if (olderMessages.length > 0) {
        // 将旧消息插入到前面
        setMessages(prev => [...olderMessages, ...prev])
      }
    } catch (error) {
      console.error('[RadioPage] Failed to load more messages:', error)
    } finally {
      setIsLoadingMore(false)
    }
  }, [currentGroupId, isLoadingMore, user])

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
      // 重置新群组的分页状态
      messageSyncService.resetGroupState(groupId)
    }
  }

  // 发送文本消息
  const handleSendText = () => {
    if (!textInput.trim()) return

    // 限制文本长度（后端 audio_path 是 varchar(255)，按字节限制 250）
    // 中文字符占 3 字节，这里限制 80 个字符确保不超过后端限制
    const maxLen = 80
    let text = textInput.trim()
    if (text.length > maxLen) {
      text = text.slice(0, maxLen)
    }

    radioService.sendTextMessage(text)
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
          {renderConnectionStatus()}
        </Box>
      </Box>

      {/* 音频权限提示 */}
      {audioPermissionNeeded && (
        <Alert
          severity="info"
          sx={{ alignItems: 'center' }}
          action={
            <Button color="inherit" size="small" onClick={handleActivateAudio}>
              点击激活
            </Button>
          }
        >
          <Typography variant="body2">
            🔊 点击"激活"以启用音频功能
          </Typography>
        </Alert>
      )}

      {/* 错误提示 */}
      {error && (
        <Alert severity="error" onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 【新增】连接冲突警告 */}
      {connectionConflict && (
        <Alert
          severity="warning"
          icon={<HeadsetIcon />}
          sx={{ alignItems: 'center' }}
        >
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
            <Typography variant="body2">
              您的账号已在其他页面建立了电台连接，请先断开其他页面的连接
            </Typography>
          </Box>
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
          hasMore={messageSyncService.hasMore(currentGroupId)}
          isLoadingMore={isLoadingMore}
          onLoadMore={handleLoadMore}
          onVoicePlayStateChange={handleVoicePlayStateChange}
        />
      </Box>

      {/* 接收状态显示 */}
      {voiceState === 'receiving' && currentSpeaker && (
        <Box sx={styles.visualizer}>
          <Typography variant="body2" color="primary">
            🔴 {currentSpeaker.callsign}-{currentSpeaker.ssid}
          </Typography>
        </Box>
      )}

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
                onChange={(e) => {
                  // 限制输入长度（80个字符，对应后端 250 字节限制）
                  const value = e.target.value
                  if (value.length <= 80) {
                    setTextInput(value)
                  }
                }}
                onKeyPress={(e) => e.key === 'Enter' && handleSendText()}
                disabled={connectionState !== 'online'}
                inputProps={{ maxLength: 80 }}
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
