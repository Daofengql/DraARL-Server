/**
 * 消息列表组件 - 性能优化版
 * 优化点：
 * 1. 使用 React.memo 避免不必要的重渲染
 * 2. 使用 useMemo/useCallback 缓存计算结果
 * 3. 移除 forceUpdate 反模式，使用状态管理
 * 4. 限制缓存大小防止内存泄漏
 */

import React, { useEffect, useRef, forwardRef, useState, useCallback, useMemo, memo } from 'react'
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

// 缓存配置
const MAX_CACHE_SIZE = 500 // 最大缓存用户数

interface MessageListProps {
  messages: RadioMessage[]
  currentCallsign: string
  currentSSID: number
  loading?: boolean
  currentUser?: any
  hasMore?: boolean
  isLoadingMore?: boolean
  onLoadMore?: () => void
}

// ==========================================
// 性能优化：带大小限制的用户信息缓存
// ==========================================
class UserInfoCache {
  private cache = new Map<string, { avatar?: string; nickname?: string }>()
  private loading = new Set<string>()

  get(key: string) {
    return this.cache.get(key)
  }

  has(key: string) {
    return this.cache.has(key)
  }

  isLoading(key: string) {
    return this.loading.has(key)
  }

  setLoading(key: string) {
    this.loading.add(key)
  }

  set(key: string, value: { avatar?: string; nickname?: string }) {
    // LRU 淘汰策略：如果超过最大缓存数，删除最早的条目
    if (this.cache.size >= MAX_CACHE_SIZE) {
      const firstKey = this.cache.keys().next().value
      if (firstKey) {
        this.cache.delete(firstKey)
      }
    }
    this.cache.set(key, value)
    this.loading.delete(key)
  }

  clear() {
    this.cache.clear()
    this.loading.clear()
  }
}

// 全局缓存实例（限制大小防止内存泄漏）
const userInfoCache = new UserInfoCache()

// ==========================================
// 样式定义（提取到组件外部避免重复创建）
// ==========================================
const useStaticStyles = () => {
  const theme = useTheme()
  return useMemo(() => ({
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
      bgcolor: theme.palette.primary.main,
      flexShrink: 0,
    },
    messageBubble: {
      p: 1.5,
      borderRadius: 2,
      maxWidth: '100%',
    },
    messageBubbleOther: {
      bgcolor: theme.palette.action.hover,
      borderTopLeftRadius: 0,
    },
    messageBubbleSelf: {
      bgcolor: theme.palette.primary.main,
      color: theme.palette.primary.contrastText,
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
  }), [theme])
}

// ==========================================
// 语音消息组件（使用 memo 优化）
// ==========================================
interface VoiceMessageProps {
  duration: number
  isPlayed: boolean
  isSelf: boolean
  audioData?: Blob | string
}

const VoiceMessage = memo(function VoiceMessage({ duration, isPlayed, isSelf, audioData }: VoiceMessageProps) {
  const [isPlaying, setIsPlaying] = useState(false)
  const [progress, setProgress] = useState(0)
  const audioContextRef = useRef<AudioContext | null>(null)
  const sourceNodeRef = useRef<AudioBufferSourceNode | null>(null)
  const startTimeRef = useRef<number>(0)
  const animationFrameRef = useRef<number>(0)
  const styles = useStaticStyles()

  // 生成随机波形（使用 useMemo 缓存）
  const bars = useMemo(() =>
    Array.from({ length: 20 }, () => Math.random() * 16 + 4),
  [])

  const playBlobAudio = useCallback(async (blob: Blob) => {
    const arrayBuffer = await blob.arrayBuffer()

    if (!audioContextRef.current || audioContextRef.current.state === 'closed') {
      audioContextRef.current = new AudioContext()
    }
    const ctx = audioContextRef.current

    const audioBuffer = await ctx.decodeAudioData(arrayBuffer)
    const source = ctx.createBufferSource()
    source.buffer = audioBuffer
    source.connect(ctx.destination)

    sourceNodeRef.current = source
    startTimeRef.current = ctx.currentTime
    setIsPlaying(true)

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
  }, [])

  const playUrlAudio = useCallback(async (url: string) => {
    const audioUrl = url.startsWith('http') ? url : `/api/minio/${url}`

    await opusPlayer.play(audioUrl, () => {
      setIsPlaying(false)
      setProgress(0)
    })
    setIsPlaying(true)

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
  }, [duration])

  const handlePlayPause = useCallback(async () => {
    if (!audioData) return

    if (isPlaying) {
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
          await playUrlAudio(audioData)
        } else {
          await playBlobAudio(audioData)
        }
      } catch (error) {
        console.error('Failed to play audio:', error)
        setIsPlaying(false)
      }
    }
  }, [audioData, isPlaying, playBlobAudio, playUrlAudio])

  // 清理
  useEffect(() => {
    return () => {
      opusPlayer.stop()
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
})

// ==========================================
// 单条消息组件（使用 memo 优化）
// ==========================================
interface MessageItemProps {
  message: RadioMessage
  isSelf: boolean
  avatarUrl?: string
  nickname?: string
  showTimeDivider: boolean
  prevMessage?: RadioMessage
  getAvatarColor: (callsign: string) => string
  formatTime: (timestamp: number) => string
  formatTimeDivider: (timestamp: number) => string
  needsTimeDivider: (currentMsg: RadioMessage, prevMsg?: RadioMessage) => boolean
  styles: ReturnType<typeof useStaticStyles>
}

const MessageItem = memo(function MessageItem({
  message,
  isSelf,
  avatarUrl,
  nickname,
  showTimeDivider,
  formatTime,
  formatTimeDivider,
  styles,
  getAvatarColor,
}: MessageItemProps) {
  return (
    <>
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

          {/* 自己发的消息也显示昵称 */}
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
    </>
  )
})

// ==========================================
// 主组件
// ==========================================
export const MessageList = forwardRef<HTMLDivElement, MessageListProps>(
  ({ messages, currentCallsign, currentSSID, loading, currentUser, hasMore, isLoadingMore, onLoadMore }, ref) => {
    const styles = useStaticStyles()
    const scrollRef = useRef<HTMLDivElement>(null)

    // 使用状态触发重渲染（替代 forceUpdate）
    const [cacheVersion, setCacheVersion] = useState(0)

    const prevScrollHeightRef = useRef<number>(0)
    const isInitialLoadRef = useRef(true)
    const prevLastMsgTimeRef = useRef(0)

    // 滚动检测
    const handleScroll = useCallback(() => {
      if (!scrollRef.current || !onLoadMore || isLoadingMore || !hasMore) return

      if (scrollRef.current.scrollTop < 100) {
        prevScrollHeightRef.current = scrollRef.current.scrollHeight
        onLoadMore()
      }
    }, [onLoadMore, isLoadingMore, hasMore])

    // 加载更多后恢复滚动位置
    useEffect(() => {
      if (scrollRef.current && prevScrollHeightRef.current > 0) {
        const newScrollHeight = scrollRef.current.scrollHeight
        const scrollDiff = newScrollHeight - prevScrollHeightRef.current
        scrollRef.current.scrollTop = scrollDiff
        prevScrollHeightRef.current = 0
      }
    }, [messages.length])

    // 异步加载用户头像（使用缓存版本触发重渲染）
    const loadUserAvatar = useCallback(async (username: string | number) => {
      const key = String(username).trim()

      if (userInfoCache.has(key) || userInfoCache.isLoading(key)) {
        return
      }

      userInfoCache.setLoading(key)

      if (key.startsWith('ghost-') || /^.+-\d+$/.test(key)) {
        userInfoCache.set(key, {})
        return
      }

      try {
        const user = await userService.getPublicInfoByName(key)
        userInfoCache.set(key, {
          avatar: user.avatar_thumb || user.avatar,
          nickname: user.nickname,
        })
        // 使用状态更新触发重渲染
        setCacheVersion(v => v + 1)
      } catch (error) {
        userInfoCache.set(key, {})
      }
    }, [])

    // 当消息变化时，加载未缓存的用户头像
    useEffect(() => {
      messages.forEach(msg => {
        if (!msg.senderAvatar) {
          const usernameToFetch = (msg as any).senderUsername || msg.senderNickname || msg.senderId
          if (usernameToFetch) {
            const key = String(usernameToFetch).trim()
            if (!userInfoCache.has(key) && !userInfoCache.isLoading(key)) {
              loadUserAvatar(usernameToFetch)
            }
          }
        }
      })
    }, [messages, loadUserAvatar])

    // 当消息被清空时，重置标记
    useEffect(() => {
      if (messages.length === 0) {
        isInitialLoadRef.current = true
        prevLastMsgTimeRef.current = 0
      }
    }, [messages.length])

    // 自动滚动到底部
    useEffect(() => {
      if (scrollRef.current) {
        if (isInitialLoadRef.current) {
          scrollRef.current.scrollTop = scrollRef.current.scrollHeight
          isInitialLoadRef.current = false
          if (messages.length > 0) {
            prevLastMsgTimeRef.current = messages[messages.length - 1].timestamp
          }
          return
        }

        const lastMsgTime = messages.length > 0 ? messages[messages.length - 1].timestamp : 0
        const hasNewMessage = lastMsgTime > prevLastMsgTimeRef.current
        prevLastMsgTimeRef.current = lastMsgTime

        if (hasNewMessage) {
          const { scrollTop, scrollHeight, clientHeight } = scrollRef.current
          const isNearBottom = scrollHeight - scrollTop - clientHeight < 150
          if (isNearBottom) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight
          }
        }
      }
    }, [messages])

    // 格式化时间（使用 useCallback 缓存）
    const formatTime = useCallback((timestamp: number) => {
      const date = new Date(timestamp)
      return date.toLocaleTimeString('zh-CN', {
        hour: '2-digit',
        minute: '2-digit',
      })
    }, [])

    const formatTimeDivider = useCallback((timestamp: number) => {
      const date = new Date(timestamp)
      const year = date.getFullYear()
      const month = String(date.getMonth() + 1).padStart(2, '0')
      const day = String(date.getDate()).padStart(2, '0')
      const hour = String(date.getHours()).padStart(2, '0')
      const minute = String(date.getMinutes()).padStart(2, '0')
      return `${year}-${month}-${day}  ${hour}:${minute}`
    }, [])

    // 判断是否需要时间分割
    const needsTimeDivider = useCallback((currentMsg: RadioMessage, prevMsg?: RadioMessage): boolean => {
      if (!prevMsg) return true
      const diff = currentMsg.timestamp - prevMsg.timestamp
      return diff >= 10 * 60 * 1000
    }, [])

    // 获取头像颜色（使用 useCallback 缓存）
    const getAvatarColor = useCallback((callsign: string) => {
      const colors = [
        '#1976d2',
        '#9c27b0',
        '#f44336',
        '#673ab7',
        '#3f51b5',
        '#009688',
        '#ff5722',
        '#795548',
      ]
      let hash = 0
      for (let i = 0; i < callsign.length; i++) {
        hash = callsign.charCodeAt(i) + ((hash << 5) - hash)
      }
      return colors[Math.abs(hash) % colors.length]
    }, [])

    // 预计算消息项数据（使用 useMemo 优化，包含 cacheVersion 触发更新）
    const messageItems = useMemo(() => {
      return messages.map((message, index) => {
        const isMatchCallsign = String(message.senderCallsign).toUpperCase() === String(currentCallsign).toUpperCase()
        const isMatchSSID = String(message.senderSSID) === String(currentSSID)
        const isSelf = (isMatchCallsign && isMatchSSID) || message.isSelf === true

        const usernameForCache = String((message as any).senderUsername || message.senderNickname || message.senderId).trim()
        const cachedInfo = userInfoCache.get(usernameForCache)

        const selfAvatar = currentUser?.avatar_thumb || currentUser?.avatar
        const avatarUrl = isSelf && selfAvatar
          ? selfAvatar
          : (message.senderAvatar || cachedInfo?.avatar)

        const nickname = isSelf && currentUser?.nickname
          ? currentUser.nickname
          : (cachedInfo?.nickname || message.senderNickname)

        const prevMessage = index > 0 ? messages[index - 1] : undefined
        const showTimeDivider = needsTimeDivider(message, prevMessage)

        return {
          message,
          isSelf,
          avatarUrl,
          nickname,
          showTimeDivider,
          prevMessage,
        }
      })
    }, [messages, currentCallsign, currentSSID, currentUser, needsTimeDivider, cacheVersion])

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
        {messageItems.map((item, index) => (
          <MessageItem
            key={item.message.id || `${item.message.timestamp}-${index}`}
            message={item.message}
            isSelf={item.isSelf}
            avatarUrl={item.avatarUrl}
            nickname={item.nickname}
            showTimeDivider={item.showTimeDivider}
            prevMessage={item.prevMessage}
            getAvatarColor={getAvatarColor}
            formatTime={formatTime}
            formatTimeDivider={formatTimeDivider}
            needsTimeDivider={needsTimeDivider}
            styles={styles}
          />
        ))}
      </Box>
    )
  }
)

MessageList.displayName = 'MessageList'

export default MessageList
