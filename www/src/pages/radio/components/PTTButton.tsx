/**
 * PTT 按钮组件
 */

import React from 'react'
import {
  Button,
  Typography,
  Box,
  useTheme,
  useMediaQuery,
  keyframes,
} from '@mui/material'
import { Mic as MicIcon, MicOff as MicOffIcon } from '@mui/icons-material'

interface PTTButtonProps {
  isPressed: boolean
  onMouseDown: () => void
  onMouseUp: () => void
  onMouseLeave: () => void
  onTouchStart: () => void
  onTouchEnd: () => void
  disabled?: boolean
}

// 脉冲动画
const pulse = keyframes`
  0% {
    box-shadow: 0 0 0 0 rgba(244, 67, 54, 0.4);
  }
  70% {
    box-shadow: 0 0 0 10px rgba(244, 67, 54, 0);
  }
  100% {
    box-shadow: 0 0 0 0 rgba(244, 67, 54, 0);
  }
`

// 呼吸动画
const breathe = keyframes`
  0%, 100% {
    opacity: 0.7;
  }
  50% {
    opacity: 1;
  }
`

export const PTTButton: React.FC<PTTButtonProps> = ({
  isPressed,
  onMouseDown,
  onMouseUp,
  onMouseLeave,
  onTouchStart,
  onTouchEnd,
  disabled = false,
}) => {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))

  return (
    <Button
      variant="contained"
      size="large"
      disabled={disabled}
      onMouseDown={onMouseDown}
      onMouseUp={onMouseUp}
      onMouseLeave={onMouseLeave}
      onTouchStart={onTouchStart}
      onTouchEnd={onTouchEnd}
      sx={{
        minWidth: isMobile ? 100 : 140,
        minHeight: isMobile ? 48 : 56,
        borderRadius: 3,
        bgcolor: isPressed ? 'error.main' : 'primary.main',
        color: 'white',
        transition: 'all 0.15s ease',
        transform: isPressed ? 'scale(0.95)' : 'scale(1)',
        animation: isPressed ? `${pulse} 1s infinite` : 'none',
        '&:hover': {
          bgcolor: isPressed ? 'error.dark' : 'primary.dark',
        },
        '&:active': {
          transform: 'scale(0.95)',
        },
        '&.Mui-disabled': {
          bgcolor: 'action.disabledBackground',
          color: 'action.disabled',
        },
      }}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 0.5 }}>
        {isPressed ? (
          <>
            <MicIcon sx={{ fontSize: isMobile ? 24 : 32 }} />
            <Typography variant="caption" fontWeight="bold">
              发送中
            </Typography>
          </>
        ) : disabled ? (
          <>
            <MicOffIcon sx={{ fontSize: isMobile ? 24 : 32 }} />
            <Typography variant="caption">
              {isMobile ? 'PTT' : '按住说话'}
            </Typography>
          </>
        ) : (
          <>
            <MicIcon sx={{ fontSize: isMobile ? 24 : 32 }} />
            <Typography variant="caption">
              {isMobile ? 'PTT' : '按住说话'}
            </Typography>
          </>
        )}
      </Box>
    </Button>
  )
}

export default PTTButton
