/**
 * 在线收发页面
 */

import React, { useEffect, useState, useRef, useCallback } from 'react'
import {
  Box,
  Typography,
  IconButton,
  TextField,
  Drawer,
  Alert,
  Button,
} from '@mui/material'
import MicIcon from '@mui/icons-material/Mic'
import VolumeUpIcon from '@mui/icons-material/VolumeUp'
import VolumeOffIcon from '@mui/icons-material/VolumeOff'
import SendIcon from '@mui/icons-material/Send'
import KeyboardIcon from '@mui/icons-material/Keyboard'
import RecordIcon from '@mui/icons-material/FiberManualRecord'

import { useAuth } from '../../hooks/useAuth'
import {
  getRadioService,
} from '../../services/radioService'
import { messageSyncService } from '../../services/radio/messageSync'
import { RadioTabCoordinator } from '../../services/radio/tabCoordinator'
import type {
  WSConnectionState,
  VoiceState,
  RadioMessage,
  RadioGroup,
  RadioUserConfig,
  RadioSessionRouting,
  RadioSpeaker,
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
    alignItems: 'stretch',
    flexDirection: 'column',
    gap: 0.75,
    p: 1,
    borderBottom: 1,
    borderColor: 'divider',
    bgcolor: 'background.paper',
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    minWidth: 0,
    flex: 1,
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
  const styles = useStyles()

  // 认证
  const { user, token } = useAuth()

  // Radio 服务
  const [radioService] = useState(() => getRadioService())

  // 状态
  const [connectionState, setConnectionState] = useState<WSConnectionState>('disconnected')
  const [voiceState, setVoiceState] = useState<VoiceState>('idle')
  const [activeSpeakers, setActiveSpeakers] = useState<RadioSpeaker[]>([])

  // 数据
  const [groups, setGroups] = useState<RadioGroup[]>([])
  const [routing, setRouting] = useState<RadioSessionRouting>(() => radioService.getRouting())
  const [activeGroupId, setActiveGroupId] = useState(() => radioService.getRouting().txGroupId)
  const [messages, setMessages] = useState<RadioMessage[]>([])

  // UI 状态
  const [inputMode, setInputMode] = useState<'voice' | 'text'>('voice')
  const [textInput, setTextInput] = useState('')
  const [deviceListOpen, setDeviceListOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isPTTDown, setIsPTTDown] = useState(false)
  const [audioPermissionNeeded, setAudioPermissionNeeded] = useState(false) // 音频权限提示
  const [isLoadingMore, setIsLoadingMore] = useState(false) // 加载更多状态
  const [routingUpdating, setRoutingUpdating] = useState(false)
  const [takenOver, setTakenOver] = useState(false)

  // 配置
  const [config, setConfig] = useState<RadioUserConfig>(radioService.getConfig())

  // Refs
  const messageListRef = useRef<HTMLDivElement>(null)
  const activeGroupIdRef = useRef(activeGroupId)
  const messagesByViewRef = useRef<Map<number, RadioMessage[]>>(new Map())
  const isPlayingVoiceRef = useRef(false)
  const pendingSyncMessagesRef = useRef<{ groupId: number; messages: RadioMessage[] } | null>(null)
  const ownsRadioTabRef = useRef(true)

  useEffect(() => {
    activeGroupIdRef.current = activeGroupId
  }, [activeGroupId])

  // 供 MessageList 回调 - 播放状态变化
  const handleVoicePlayStateChange = useCallback((playing: boolean) => {
    isPlayingVoiceRef.current = playing
    // 播放结束后，应用挂起的同步结果
    if (!playing && pendingSyncMessagesRef.current) {
      const pending = pendingSyncMessagesRef.current
      messagesByViewRef.current.set(pending.groupId, pending.messages)
      if (activeGroupIdRef.current === pending.groupId) setMessages(pending.messages)
      pendingSyncMessagesRef.current = null
    }
  }, [])

  // 初始化
  useEffect(() => {
    if (!user || !token) return

    const coordinator = new RadioTabCoordinator()
    ownsRadioTabRef.current = true
    coordinator.start(() => {
      ownsRadioTabRef.current = false
      setTakenOver(true)
      setIsPTTDown(false)
      radioService.destroy()
      setConnectionState('disconnected')
      setVoiceState('idle')
      setActiveSpeakers([])
    })

    const initRadio = async () => {
      try {
        // 设置事件处理器
        radioService.on('connectionStateChange', (state) => {
          setConnectionState(state)
        })

        radioService.on('voiceStateChange', (state, _callsign) => {
          setVoiceState(state)
        })

        radioService.on('message', (message) => {
          const current = messagesByViewRef.current.get(message.groupId) || []
          if (current.some(existing => existing.id === message.id)) return
          const updated = [...current, message].sort((left, right) => left.timestamp - right.timestamp)
          messagesByViewRef.current.set(message.groupId, updated)
          if (activeGroupIdRef.current === message.groupId) setMessages(updated)
        })

        radioService.on('speakersChange', setActiveSpeakers)

        radioService.on('routingChange', nextRouting => {
          setRouting(nextRouting)
          setConfig(radioService.getConfig())
          activeGroupIdRef.current = nextRouting.txGroupId
          setActiveGroupId(nextRouting.txGroupId)
          setMessages(messagesByViewRef.current.get(nextRouting.txGroupId) || [])
        })

        radioService.on('error', (errorMsg) => {
          setError(errorMsg)
          setTimeout(() => setError(null), 5000)
        })

        // 初始化服务（传入用户上次选中的群组 ID，确保跨设备同步）
        await radioService.init(token, user!.username, user!.callsign || '', user!.last_group_id)
        if (!ownsRadioTabRef.current) return

        // 加载群组
        const groupList = await radioService.getGroups()
        setGroups(groupList)

        // 连接
        await radioService.connect()
        if (!ownsRadioTabRef.current) return
        const connectedRouting = radioService.getRouting()
        setRouting(connectedRouting)
        activeGroupIdRef.current = connectedRouting.txGroupId
        setActiveGroupId(connectedRouting.txGroupId)
        setMessages(messagesByViewRef.current.get(connectedRouting.txGroupId) || [])

      } catch (error) {
        if (!ownsRadioTabRef.current) return
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
      ownsRadioTabRef.current = false
      coordinator.stop()
      radioService.disconnect()
    }
  }, [user, token, radioService])

  // 激活音频权限
  const handleActivateAudio = useCallback(async () => {
    try {
      await radioService.activateAudio()
      setAudioPermissionNeeded(false)
    } catch (error) {
      console.error('Failed to activate audio:', error)
      setError('无法获取音频权限，请检查浏览器设置')
    }
  }, [radioService])

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
    if (connectionState !== 'online' || activeGroupId === 0) return

    const syncMessages = async () => {
      try {
        // 传递当前用户信息，用于判断 isSelf
        const currentUser = user?.callsign ? {
          username: user.username,
          callsign: user.callsign,
          ssid: 105,
          id: user.id,
        } : undefined

        const currentMessages = messagesByViewRef.current.get(activeGroupId) || []
        const merged = await messageSyncService.syncMessages(activeGroupId, currentMessages, currentUser)

        // 用 ID 集合比较，避免 Blob 序列化问题
        const hasChanges = (() => {
          if (merged.length !== currentMessages.length) return true
          const currentIds = new Set(currentMessages.map(m => m.id))
          return merged.some(m => !currentIds.has(m.id))
        })()

        if (!hasChanges) return

        if (isPlayingVoiceRef.current) {
          // 正在播放：挂起同步结果，等播放结束后再应用
          pendingSyncMessagesRef.current = { groupId: activeGroupId, messages: merged }
        } else {
          messagesByViewRef.current.set(activeGroupId, merged)
          if (activeGroupIdRef.current === activeGroupId) setMessages(merged)
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
  }, [connectionState, activeGroupId, user])

  // 【加载更多历史消息】
  const handleLoadMore = useCallback(async () => {
    if (activeGroupId === 0 || isLoadingMore || !messageSyncService.hasMore(activeGroupId)) return

    setIsLoadingMore(true)
    try {
      const currentUser = user?.callsign ? {
        username: user.username,
        callsign: user.callsign,
        ssid: 105,
        id: user.id,
      } : undefined

      const olderMessages = await messageSyncService.loadMoreMessages(activeGroupId, currentUser)
      if (olderMessages.length > 0) {
        const current = messagesByViewRef.current.get(activeGroupId) || []
        const existingIds = new Set(current.map(message => message.id))
        const merged = [...olderMessages.filter(message => !existingIds.has(message.id)), ...current]
        messagesByViewRef.current.set(activeGroupId, merged)
        if (activeGroupIdRef.current === activeGroupId) setMessages(merged)
      }
    } catch (error) {
      console.error('[RadioPage] Failed to load more messages:', error)
    } finally {
      setIsLoadingMore(false)
    }
  }, [activeGroupId, isLoadingMore, user])

  // PTT 按下
  const handlePTTDown = useCallback(() => {
    if (!ownsRadioTabRef.current || takenOver) return
    if (connectionState !== 'online') return
    if (voiceState !== 'idle') return

    setIsPTTDown(true)
    radioService.startVoice()
  }, [connectionState, voiceState, radioService, takenOver])

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

  const handleTxGroupChange = async (groupId: number) => {
    if (!ownsRadioTabRef.current || takenOver) return
    setRoutingUpdating(true)
    try {
      await radioService.switchGroup(groupId)
    } finally {
      setRoutingUpdating(false)
    }
  }

  const handleReceiveGroupsChange = async (groupIds: number[]) => {
    if (!ownsRadioTabRef.current || takenOver) return
    setRoutingUpdating(true)
    try {
      await radioService.setReceiveGroups(groupIds)
    } finally {
      setRoutingUpdating(false)
    }
  }

  const handleChannelVolumeChange = (groupId: number, volume: number) => {
    radioService.setChannelVolume(groupId, volume)
    setConfig(radioService.getConfig())
  }

  // 发送文本消息
  const handleSendText = () => {
    if (!ownsRadioTabRef.current || takenOver) return
    if (!textInput.trim()) return

    const text = textInput.trim()
    if (radioService.sendTextMessage(text)) {
      setTextInput('')
    }
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

  // 渲染连接状态
  const renderConnectionStatus = () => (
    <Box sx={styles.connectionStatus}>
      <Box sx={{ ...styles.statusDot, bgcolor: stateColors[connectionState] }} />
      <Typography variant="body2" color="text.secondary">
        {stateTexts[connectionState]}
      </Typography>
    </Box>
  )

  // 渲染说话指示器
  const renderSpeakingIndicator = () => {
    if (activeSpeakers.length === 0) return null

    return (
      <Box sx={{ display: 'flex', gap: 0.75, overflowX: 'auto', pb: 0.25 }}>
        {activeSpeakers.map(speaker => (
          <Box key={speaker.key} sx={{ ...styles.speakingIndicator, flexShrink: 0 }}>
            <RecordIcon sx={{ fontSize: 12 }} />
            <Typography variant="body2">
              {groups.find(group => group.id === speaker.groupId)?.name || `#${speaker.groupId}`}
              {' · '}{speaker.callsign}-{speaker.ssid}
            </Typography>
          </Box>
        ))}
      </Box>
    )
  }

  return (
    <Box sx={styles.root}>
      {/* 头部 */}
      <Box sx={styles.header}>
        <Box sx={{ display: 'flex', justifyContent: 'flex-end', minWidth: 0 }}>
          {renderConnectionStatus()}
        </Box>
        <GroupSelector
          groups={groups}
          txGroupId={routing.txGroupId}
          rxGroupIds={routing.rxGroupIds}
          channelVolumes={config.channelVolumes}
          onTxChange={handleTxGroupChange}
          onRxChange={handleReceiveGroupsChange}
          onChannelVolumeChange={handleChannelVolumeChange}
          disabled={connectionState !== 'online'}
          updating={routingUpdating}
        />
        {renderSpeakingIndicator()}
        {takenOver && (
          <Alert severity="warning" sx={{ mt: 0.5 }}>
            已在其他页面接管
          </Alert>
        )}
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

      {/* 消息列表 */}
      <Box sx={styles.messageArea}>
        <MessageList
          ref={messageListRef}
          messages={messages}
          currentCallsign={user?.callsign || ''}
          currentSSID={105}
          currentUser={user}
          hasMore={activeGroupId > 0 && messageSyncService.hasMore(activeGroupId)}
          isLoadingMore={isLoadingMore}
          onLoadMore={handleLoadMore}
          onVoicePlayStateChange={handleVoicePlayStateChange}
        />
      </Box>

      {/* 接收状态显示 */}
      {voiceState === 'receiving' && activeSpeakers.length > 0 && (
        <Box sx={styles.visualizer}>
          <Typography variant="body2" color="primary">
            {activeSpeakers.length} 路语音正在混音
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
                onChange={(e) => setTextInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.nativeEvent.isComposing) handleSendText()
                }}
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

      {/* 设备列表抽屉 */}
      <Drawer
        anchor="right"
        open={deviceListOpen}
        onClose={() => setDeviceListOpen(false)}
      >
        <Box sx={styles.settingsDrawer}>
          <DeviceList
            groupId={activeGroupId || routing.txGroupId}
            onClose={() => setDeviceListOpen(false)}
          />
        </Box>
      </Drawer>
    </Box>
  )
}

export default RadioPage
