/**
 * 消息列表组件
 */

import React, { useEffect, useRef, forwardRef } from 'react'
import {
  Box,
  Typography,
  Avatar,
  Paper,
  IconButton,
  Skeleton,
  useTheme,
} from '@mui/material'
import {
  PlayArrow as PlayIcon,
  Pause as PauseIcon,
} from '@mui/icons-material'
import type { RadioMessage } from '../../../types/radio'

interface MessageListProps {
  messages: RadioMessage[]
  currentCallsign: string
  loading?: boolean
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
    width: 36,
    height: 36,
    bgcolor: 'primary.main',
  },
  messageBubble: {
    p: 1.5,
    borderRadius: 2,
    maxWidth: '100%',
  },
  messageBubbleOther: {
    bgcolor: 'background.paper',
    borderTopLeftRadius: 0,
  },
  messageBubbleSelf: {
    bgcolor: 'primary.light',
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
})

// 语音消息组件
const VoiceMessage: React.FC<{
  duration: number
  isPlayed: boolean
  isSelf: boolean
}> = ({ duration, isPlayed, isSelf }) => {
  const [isPlaying, setIsPlaying] = React.useState(false)
  const [progress, setProgress] = React.useState(0)

  const handlePlayPause = () => {
    setIsPlaying(!isPlaying)
    // TODO: 实际播放逻辑
  }

  // 生成随机波形
  const bars = Array.from({ length: 20 }, () => Math.random() * 16 + 4)

  return (
    <Box sx={useStyles().voiceMessage}>
      <IconButton
        size="small"
        onClick={handlePlayPause}
        sx={{ color: isSelf ? 'inherit' : 'primary.main' }}
      >
        {isPlaying ? <PauseIcon /> : <PlayIcon />}
      </IconButton>

      <Box sx={useStyles().voiceWaveform}>
        {bars.map((height, index) => (
          <Box
            key={index}
            sx={{
              ...useStyles().voiceBar,
              height,
              opacity: isPlayed || isPlaying ? 1 : 0.5,
            }}
          />
        ))}
      </Box>

      <Typography variant="caption" color="text.secondary">
        {(duration / 1000).toFixed(1)}s
      </Typography>
    </Box>
  )
}

export const MessageList = forwardRef<HTMLDivElement, MessageListProps>(
  ({ messages, currentCallsign, loading }, ref) => {
    const theme = useTheme()
    const styles = useStyles()
    const scrollRef = useRef<HTMLDivElement>(null)

    // 自动滚动到底部
    useEffect(() => {
      if (scrollRef.current) {
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight
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
      <Box ref={scrollRef} sx={styles.root}>
        {messages.map((message) => {
          const isSelf = message.senderCallsign === currentCallsign

          return (
            <Box
              key={message.id}
              sx={{
                ...styles.messageWrapper,
                ...(isSelf && styles.messageWrapperSelf),
              }}
            >
              {/* 头像 */}
              {!isSelf && (
                <Avatar
                  sx={{
                    ...styles.avatar,
                    bgcolor: getAvatarColor(message.senderCallsign),
                  }}
                >
                  {message.senderCallsign.charAt(0)}
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
                {/* 头部 */}
                {!isSelf && (
                  <Box sx={styles.messageHeader}>
                    <Typography variant="subtitle2" fontWeight="bold">
                      {message.senderCallsign}-{message.senderSSID}
                    </Typography>
                    {message.senderNickname && (
                      <Typography variant="caption" color="text.secondary">
                        {message.senderNickname}
                      </Typography>
                    )}
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
                    />
                  )}
                </Box>

                {/* 底部 */}
                <Box sx={styles.messageFooter}>
                  <Typography variant="caption" color="text.secondary">
                    {formatTime(message.timestamp)}
                  </Typography>
                </Box>
              </Paper>

              {/* 自己的头像 */}
              {isSelf && (
                <Avatar
                  sx={{
                    ...styles.avatar,
                    bgcolor: getAvatarColor(message.senderCallsign),
                  }}
                >
                  {message.senderCallsign.charAt(0)}
                </Avatar>
              )}
            </Box>
          )
        })}
      </Box>
    )
  }
)

MessageList.displayName = 'MessageList'

export default MessageList
