/**
 * 消息列表组件
 */

import React, { useEffect, useRef, forwardRef, useState, useCallback } from 'react'
import {
  Box,
  Typography,
  Avatar,
  Paper,
  IconButton,
  Skeleton,
  useTheme,
} from '@mui/material'
import PlayIcon from '@mui/icons-material/PlayArrow'
import PauseIcon from '@mui/icons-material/Pause'
import type { RadioMessage } from '../../../types/radio'
import { userService } from '../../../services'
import { opusPlayer } from '../../../utils/opusDecoder'

// 用户信息缓存（全局）
const userInfoCache = new Map<string, { avatar?: string; nickname?: string }>()

interface MessageListProps {
  messages: RadioMessage[]
  currentCallsign: string
  currentSSID: number  // 添加 SSID 用于精确判断
  loading?: boolean
  currentUser?: any    // 当前登录用户信息
  hasMore?: boolean    // 是否还有更多历史消息
  isLoadingMore?: boolean  // 是否正在加载更多
  onLoadMore?: () => void  // 加载更多回调
}

// 样式
const useStyles = () => ({
  root: {
    flex: 1,
    overflow: 'auto',
    p: 2,
    display: 'flex',
    flexDirection: 'column',
    gap: 1.5,
  },
  messageWrapper: {
    display: 'flex',
    gap: 1,
    maxWidth: '80%',
  },
  messageWrapperSelf: {
    marginLeft: 'auto',
    flexDirection: 'row-reverse',
  },
  avatar: {
    width: 40,
    height: 40,
    bgcolor: 'primary.main',
    flexShrink: 0,
  },
  messageBubble: {
    p: 1.5,
    borderRadius: 2,
    maxWidth: '100%',
  },
  messageBubbleOther: {
    bgcolor: 'action.hover',
    borderTopLeftRadius: 0,
  },
  messageBubbleSelf: {
    bgcolor: 'primary.main',
    color: 'primary.contrastText',
    borderTopRightRadius: 0,
  },
  messageHeader: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    mb: 0.5,
  },
  messageContent: {
    wordBreak: 'break-word',
  },
  messageFooter: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'flex-end',
    gap: 0.5,
    mt: 0.5,
    opacity: 0.7,
  },
  voiceMessage: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    minWidth: 150,
  },
  voiceWaveform: {
    flex: 1,
    height: 24,
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
  },
  voiceBar: {
    width: 3,
    bgcolor: 'currentColor',
    borderRadius: 1,
  },
  emptyState: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 2,
    color: 'text.secondary',
  },
  senderInfo: {
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
  },
  callsignChip: {
    fontSize: '0.75rem',
    fontWeight: 'bold',
    opacity: 0.9,
  },
  nickname: {
    fontSize: '0.7rem',
    opacity: 0.7,
  },
  // 时间分割线样式
  timeDivider: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    py: 2,
  },
  timeDividerText: {
    fontSize: '0.75rem',
    color: 'text.secondary',
    bgcolor: 'background.default',
    px: 2,
    borderRadius: 1,
  },
})

// 语音消息组件 - 使用 Web Audio API 播放（支持 Blob 和 URL）
const VoiceMessage: React.FC<{
  duration: number
  isPlayed: boolean
  isSelf: boolean
  audioData?: Blob | string  // 支持 Blob (WAV) 或 URL (Raw Opus)
}> = ({ duration, isPlayed, isSelf, audioData }) => {
  const [isPlaying, setIsPlaying] = React.useState(false)
  const [progress, setProgress] = React.useState(0)
  const audioContextRef = React.useRef<AudioContext | null>(null)
  const sourceNodeRef = React.useRef<AudioBufferSourceNode | null>(null)
  const startTimeRef = React.useRef<number>(0)
  const animationFrameRef = React.useRef<number>(0)
  const styles = useStyles()

  // 播放 Blob 格式音频（WAV，来自实时语音）
  const playBlobAudio = async (blob: Blob) => {
    const arrayBuffer = await blob.arrayBuffer()

    // 创建 AudioContext
    if (!audioContextRef.current || audioContextRef.current.state === 'closed') {
      audioContextRef.current = new AudioContext()
    }
    const ctx = audioContextRef.current

    // 使用 AudioContext 解码 WAV 数据
    const audioBuffer = await ctx.decodeAudioData(arrayBuffer)

    // 创建并播放 AudioBufferSourceNode
    const source = ctx.createBufferSource()
    source.buffer = audioBuffer
    source.connect(ctx.destination)

    sourceNodeRef.current = source
    startTimeRef.current = ctx.currentTime
    setIsPlaying(true)

    // 更新进度
    const updateProgress = () => {
      if (sourceNodeRef.current && audioContextRef.current) {
        const elapsed = audioContextRef.current.currentTime - startTimeRef.current
        const prog = Math.min(elapsed / audioBuffer.duration, 1)
        setProgress(prog)
        if (prog < 1) {
          animationFrameRef.current = requestAnimationFrame(updateProgress)
        }
      }
    }
    animationFrameRef.current = requestAnimationFrame(updateProgress)

    source.onended = () => {
      setIsPlaying(false)
      setProgress(0)
      sourceNodeRef.current = null
    }

    source.start()
  }

  // 播放 URL 格式音频（Raw Opus，来自数据库）
  const playUrlAudio = async (url: string) => {
    // 确保 URL 是完整路径
    const audioUrl = url.startsWith('http') ? url : `/api/minio/${url}`

    await opusPlayer.play(audioUrl, () => {
      setIsPlaying(false)
      setProgress(0)
    })
    setIsPlaying(true)

    // 模拟进度更新（opusPlayer 不提供进度回调）
    const durationSec = duration / 1000
    const startTime = Date.now()
    const updateProgress = () => {
      if (opusPlayer.getIsPlaying()) {
        const elapsed = (Date.now() - startTime) / 1000
        const prog = Math.min(elapsed / durationSec, 1)
        setProgress(prog)
        if (prog < 1) {
          animationFrameRef.current = requestAnimationFrame(updateProgress)
        }
      }
    }
    animationFrameRef.current = requestAnimationFrame(updateProgress)
  }

  const handlePlayPause = async () => {
    if (!audioData) return

    if (isPlaying) {
      // 停止播放
      if (typeof audioData === 'string') {
        opusPlayer.stop()
      } else if (sourceNodeRef.current) {
        sourceNodeRef.current.stop()
        sourceNodeRef.current = null
      }
      if (animationFrameRef.current) {
        cancelAnimationFrame(animationFrameRef.current)
      }
      setIsPlaying(false)
      setProgress(0)
    } else {
      try {
        if (typeof audioData === 'string') {
          // URL 格式：使用 opusPlayer 播放 Raw Opus
          await playUrlAudio(audioData)
        } else {
          // Blob 格式：使用 AudioContext 播放 WAV
          await playBlobAudio(audioData)
        }
      } catch (error) {
        console.error('Failed to play audio:', error)
        setIsPlaying(false)
      }
    }
  }

  // 清理
  React.useEffect(() => {
    return () => {
      // 停止 opusPlayer
      opusPlayer.stop()
      // 停止 AudioContext
      if (sourceNodeRef.current) {
        try {
          sourceNodeRef.current.stop()
        } catch (e) {
          // 忽略
        }
      }
      if (animationFrameRef.current) {
        cancelAnimationFrame(animationFrameRef.current)
      }
      if (audioContextRef.current) {
        try {
          audioContextRef.current.close()
        } catch (e) {
          // 忽略
        }
      }
    }
  }, [])

  // 生成随机波形
  const bars = React.useMemo(() =>
    Array.from({ length: 20 }, () => Math.random() * 16 + 4),
  [])

  return (
    <Box sx={styles.voiceMessage}>
      <IconButton
        size="small"
        onClick={handlePlayPause}
        sx={{ color: isSelf ? 'inherit' : 'primary.main' }}
      >
        {isPlaying ? <PauseIcon /> : <PlayIcon />}
      </IconButton>

      <Box sx={styles.voiceWaveform}>
        {bars.map((height, index) => (
          <Box
            key={index}
            sx={{
              ...styles.voiceBar,
              height,
              opacity: isPlayed || isPlaying ? 1 : 0.5,
              transform: index / bars.length < progress ? 'scaleY(1.2)' : 'scaleY(1)',
              transition: 'transform 0.1s',
            }}
          />
        ))}
      </Box>

      <Typography variant="caption" sx={{ opacity: 0.7 }}>
        {(duration / 1000).toFixed(1)}s
      </Typography>
    </Box>
  )
}

export const MessageList = forwardRef<HTMLDivElement, MessageListProps>(
  ({ messages, currentCallsign, currentSSID, loading, currentUser, hasMore, isLoadingMore, onLoadMore }, ref) => {
    const theme = useTheme()
    const styles = useStyles()
    const scrollRef = useRef<HTMLDivElement>(null)

    // 用户头像状态（用于触发重渲染）
    const [, forceUpdate] = useState({})

    // 记录滚动位置，用于加载更多后恢复
    const prevScrollHeightRef = useRef<number>(0)

    // 标记是否是首次加载（用于区分"首次加载"和"新消息到达"）
    const isInitialLoadRef = useRef(true)

    // 滚动检测：当滚动到顶部时加载更多
    const handleScroll = useCallback(() => {
      if (!scrollRef.current || !onLoadMore || isLoadingMore || !hasMore) return

      // 当滚动到顶部附近（距离顶部小于 100px）时触发加载
      if (scrollRef.current.scrollTop < 100) {
        // 记录当前滚动高度，用于加载后恢复位置
        prevScrollHeightRef.current = scrollRef.current.scrollHeight
        onLoadMore()
      }
    }, [onLoadMore, isLoadingMore, hasMore])

    // 加载更多后恢复滚动位置（防止跳动）
    useEffect(() => {
      if (scrollRef.current && prevScrollHeightRef.current > 0) {
        const newScrollHeight = scrollRef.current.scrollHeight
        const scrollDiff = newScrollHeight - prevScrollHeightRef.current
        scrollRef.current.scrollTop = scrollDiff
        prevScrollHeightRef.current = 0
      }
    }, [messages.length])

    // 异步加载用户头像
    const loadUserAvatar = useCallback(async (username: string | number) => {
      // 【核心修复】补充 trim() 防止不可见空格绕过规则
      const key = String(username).trim()

      // 已缓存则跳过
      if (userInfoCache.has(key)) {
        return
      }

      // 标记为加载中（防止重复请求）
      userInfoCache.set(key, {})

      // 如果是 ghost-xxx 或 callsign-ssid 格式，直接跳过 API 调用
      if (key.startsWith('ghost-') || /^.+-\d+$/.test(key)) {
        return
      }

      try {
        const user = await userService.getPublicInfoByName(key)
        userInfoCache.set(key, {
          avatar: user.avatar_thumb || user.avatar,
          nickname: user.nickname,
        })
        // 触发重渲染
        forceUpdate({})
      } catch (error) {
        // 静默失败，缓存空对象避免重复请求
        userInfoCache.set(key, {})
      }
    }, [])

    // 当消息变化时，加载未缓存的用户头像
    useEffect(() => {
      messages.forEach(msg => {
        if (!msg.senderAvatar) {
          // 【核心修复】提取真正的 username
          // 真实的登录用户名(如 admin)往往被解析到了 senderNickname 或 senderUsername 中
          // 优先使用这些字段来请求头像，而不是用 BH5UVN-2 (senderId)
          const usernameToFetch = (msg as any).senderUsername || msg.senderNickname || msg.senderId
          if (usernameToFetch) {
            const cached = userInfoCache.get(String(usernameToFetch).trim())
            if (!cached) {
              loadUserAvatar(usernameToFetch)
            }
          }
        }
      })
    }, [messages, loadUserAvatar])

    // 当消息被清空时（如切换群组），重置首次加载标记
    useEffect(() => {
      if (messages.length === 0) {
        isInitialLoadRef.current = true
        prevLastMsgTimeRef.current = 0
      }
    }, [messages.length])

    // 记录上一次最新消息的时间戳，用于判断是否是新消息
    const prevLastMsgTimeRef = useRef(0)

    // 自动滚动到底部
    useEffect(() => {
      if (scrollRef.current) {
        // 首次加载时，始终滚动到底部
        if (isInitialLoadRef.current) {
          scrollRef.current.scrollTop = scrollRef.current.scrollHeight
          isInitialLoadRef.current = false
          // 记录最新消息时间
          if (messages.length > 0) {
            prevLastMsgTimeRef.current = messages[messages.length - 1].timestamp
          }
          return
        }

        // 检查是否有新消息（最新消息时间戳更新）
        const lastMsgTime = messages.length > 0 ? messages[messages.length - 1].timestamp : 0
        const hasNewMessage = lastMsgTime > prevLastMsgTimeRef.current
        prevLastMsgTimeRef.current = lastMsgTime

        // 新消息到达时，只有用户在底部附近才自动滚动
        if (hasNewMessage) {
          const { scrollTop, scrollHeight, clientHeight } = scrollRef.current
          const isNearBottom = scrollHeight - scrollTop - clientHeight < 150
          if (isNearBottom) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight
          }
          return
        }

        // 其他情况（如消息同步更新、加载历史消息）：保持当前位置
        // 加载历史消息时，滚动位置由 handleScroll 和 useEffect 恢复逻辑处理
      }
    }, [messages])

    // 格式化时间
    const formatTime = (timestamp: number) => {
      const date = new Date(timestamp)
      return date.toLocaleTimeString('zh-CN', {
        hour: '2-digit',
        minute: '2-digit',
      })
    }

    // 格式化时间分割显示（年月日 时:分）
    const formatTimeDivider = (timestamp: number) => {
      const date = new Date(timestamp)
      const year = date.getFullYear()
      const month = String(date.getMonth() + 1).padStart(2, '0')
      const day = String(date.getDate()).padStart(2, '0')
      const hour = String(date.getHours()).padStart(2, '0')
      const minute = String(date.getMinutes()).padStart(2, '0')
      return `${year}-${month}-${day}  ${hour}:${minute}`
    }

    // 判断是否需要时间分割（10分钟间隔）
    const needsTimeDivider = (currentMsg: RadioMessage, prevMsg?: RadioMessage): boolean => {
      if (!prevMsg) return true // 第一条消息总是显示时间分割
      const diff = currentMsg.timestamp - prevMsg.timestamp
      return diff >= 10 * 60 * 1000 // 10分钟，单位毫秒
    }

    // 获取头像颜色
    const getAvatarColor = (callsign: string) => {
      const colors = [
        theme.palette.primary.main,
        theme.palette.secondary.main,
        '#f44336',
        '#9c27b0',
        '#673ab7',
        '#3f51b5',
        '#009688',
        '#ff5722',
      ]
      let hash = 0
      for (let i = 0; i < callsign.length; i++) {
        hash = callsign.charCodeAt(i) + ((hash << 5) - hash)
      }
      return colors[Math.abs(hash) % colors.length]
    }

    // 加载骨架屏
    if (loading) {
      return (
        <Box sx={styles.root}>
          {[1, 2, 3].map((i) => (
            <Box key={i} sx={{ ...styles.messageWrapper, maxWidth: '60%' }}>
              <Skeleton variant="circular" width={36} height={36} />
              <Box sx={{ flex: 1 }}>
                <Skeleton variant="text" width={100} />
                <Skeleton variant="rectangular" height={60} sx={{ borderRadius: 2 }} />
              </Box>
            </Box>
          ))}
        </Box>
      )
    }

    // 空状态
    if (messages.length === 0) {
      return (
        <Box sx={styles.emptyState}>
          <Typography variant="h6">暂无消息</Typography>
          <Typography variant="body2">
            按 PTT 开始通话或发送文字消息
          </Typography>
        </Box>
      )
    }

    return (
      <Box ref={scrollRef} sx={styles.root} onScroll={handleScroll}>
        {/* 加载更多指示器 */}
        {hasMore && (
          <Box sx={{ textAlign: 'center', py: 1 }}>
            {isLoadingMore ? (
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 1 }}>
                <Typography variant="caption" color="text.secondary">
                  加载中...
                </Typography>
              </Box>
            ) : (
              <Typography variant="caption" color="text.secondary" sx={{ cursor: 'pointer' }} onClick={onLoadMore}>
                向上滚动加载更多
              </Typography>
            )}
          </Box>
        )}
        {/* 没有更多消息提示 */}
        {!hasMore && messages.length > 0 && (
          <Box sx={{ textAlign: 'center', py: 1 }}>
            <Typography variant="caption" color="text.secondary">
              没有更多消息了
            </Typography>
          </Box>
        )}
        {messages.map((message, index) => {
          // 【核心修复】增强己方消息的判断逻辑，防止类型不匹配（如 "10" === 10 为 false）
          const isMatchCallsign = String(message.senderCallsign).toUpperCase() === String(currentCallsign).toUpperCase()
          const isMatchSSID = String(message.senderSSID) === String(currentSSID)
          const isSelf = (isMatchCallsign && isMatchSSID) || message.isSelf === true

          // --- 【核心修复】替换这里的获取逻辑 ---
          // 获取缓存的用户头像
          const usernameForCache = String((message as any).senderUsername || message.senderNickname || message.senderId).trim()
          const cachedInfo = userInfoCache.get(usernameForCache)

          // 如果是己方消息，直接从 currentUser 提取头像，否则用缓存
          const selfAvatar = currentUser?.avatar_thumb || currentUser?.avatar
          const avatarUrl = isSelf && selfAvatar
            ? selfAvatar
            : (message.senderAvatar || cachedInfo?.avatar)

          // 如果是己方消息，直接从 currentUser 提取昵称，否则用缓存
          const nickname = isSelf && currentUser?.nickname
            ? currentUser.nickname
            : (cachedInfo?.nickname || message.senderNickname)
          // ------------------------------------

          // 判断是否需要显示时间分割线
          const prevMessage = index > 0 ? messages[index - 1] : undefined
          const showTimeDivider = needsTimeDivider(message, prevMessage)

          return (
            <React.Fragment key={message.id}>
              {/* 时间分割线 */}
              {showTimeDivider && (
                <Box sx={styles.timeDivider}>
                  <Typography sx={styles.timeDividerText}>
                    {formatTimeDivider(message.timestamp)}
                  </Typography>
                </Box>
              )}

              {/* 消息 */}
              <Box
                sx={{
                  ...styles.messageWrapper,
                  ...(isSelf && styles.messageWrapperSelf),
                }}
              >
                {/* 头像 */}
                {!isSelf && (
                  <Avatar
                    src={avatarUrl}
                    sx={{
                      ...styles.avatar,
                      bgcolor: avatarUrl ? undefined : getAvatarColor(message.senderCallsign),
                    }}
                  >
                    {!avatarUrl && message.senderCallsign.charAt(0)}
                  </Avatar>
                )}

                {/* 消息气泡 */}
                <Paper
                  elevation={0}
                  sx={{
                    ...styles.messageBubble,
                    ...(isSelf ? styles.messageBubbleSelf : styles.messageBubbleOther),
                  }}
                >
                  {/* 头部 - 显示发送方信息 */}
                  {!isSelf && (
                    <Box sx={styles.messageHeader}>
                      <Box sx={styles.senderInfo}>
                        <Typography variant="subtitle2" sx={styles.callsignChip}>
                          {message.senderCallsign}-{message.senderSSID}
                        </Typography>
                        {nickname && (
                          <Typography variant="caption" sx={styles.nickname}>
                            ({nickname})
                          </Typography>
                        )}
                      </Box>
                    </Box>
                  )}

                  {/* --- 【核心修复】自己发的消息也显示昵称 --- */}
                  {isSelf && (
                    <Box sx={{ ...styles.messageHeader, justifyContent: 'flex-end' }}>
                      <Box sx={styles.senderInfo}>
                        {nickname && (
                          <Typography variant="caption" sx={styles.nickname}>
                            ({nickname})
                          </Typography>
                        )}
                        <Typography variant="caption" sx={{ opacity: 0.8 }}>
                          {message.senderCallsign}-{message.senderSSID}
                        </Typography>
                      </Box>
                    </Box>
                  )}
                  {/* ------------------------------------------------ */}

                  {/* 内容 */}
                  <Box sx={styles.messageContent}>
                    {message.type === 'text' ? (
                      <Typography variant="body2">{message.content as string}</Typography>
                    ) : (
                      <VoiceMessage
                        duration={message.duration || 0}
                        isPlayed={message.isPlayed || false}
                        isSelf={isSelf}
                        audioData={message.content as Blob}
                      />
                    )}
                  </Box>

                  {/* 底部 */}
                  <Box sx={styles.messageFooter}>
                    <Typography variant="caption">
                      {formatTime(message.timestamp)}
                    </Typography>
                  </Box>
                </Paper>

                {/* 自己的头像 */}
                {isSelf && (
                  <Avatar
                    src={avatarUrl}
                    sx={{
                      ...styles.avatar,
                      bgcolor: avatarUrl ? undefined : getAvatarColor(message.senderCallsign),
                    }}
                  >
                    {!avatarUrl && message.senderCallsign.charAt(0)}
                  </Avatar>
                )}
              </Box>
            </React.Fragment>
          )
        })}
      </Box>
    )
  }
)

MessageList.displayName = 'MessageList'

export default MessageList
