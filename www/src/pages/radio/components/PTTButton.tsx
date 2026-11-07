/**
 * PTT 按钮组件
 */

import React, { useEffect, useRef } from 'react'
import {
  Button,
  Typography,
  Box,
  useTheme,
  useMediaQuery,
} from '@mui/material'
import { MicIcon, MicOffIcon } from '../../../components/icons/BlueprintIcons'

interface PTTButtonProps {
  isPressed: boolean
  onMouseDown: () => void
  onMouseUp: () => void
  onMouseLeave: () => void
  onTouchStart: () => void
  onTouchEnd: () => void
  disabled?: boolean
  fullWidth?: boolean
}

export const PTTButton: React.FC<PTTButtonProps> = ({
  isPressed,
  onMouseDown,
  onMouseUp,
  onMouseLeave,
  onTouchStart,
  onTouchEnd,
  disabled = false,
  fullWidth = false,
}) => {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))
  const keyboardPressed = useRef(false)
  const releaseCallback = useRef(onMouseUp)
  useEffect(() => { releaseCallback.current = onMouseUp }, [onMouseUp])
  const releaseKeyboard = () => {
    if (!keyboardPressed.current) return
    keyboardPressed.current = false
    releaseCallback.current()
  }
  useEffect(() => {
    if (disabled && keyboardPressed.current) {
      keyboardPressed.current = false
      releaseCallback.current()
    }
    return () => {
      if (keyboardPressed.current) {
        keyboardPressed.current = false
        releaseCallback.current()
      }
    }
  }, [disabled])

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
      onTouchCancel={onTouchEnd}
      aria-label={isMobile ? 'PTT 按住说话' : '按住说话'}
      aria-pressed={isPressed}
      onKeyDown={(event) => {
        if (event.key !== ' ' && event.key !== 'Enter') return
        event.preventDefault()
        if (!disabled && !event.repeat && !keyboardPressed.current) {
          keyboardPressed.current = true
          onMouseDown()
        }
      }}
      onKeyUp={(event) => {
        if (event.key !== ' ' && event.key !== 'Enter') return
        event.preventDefault()
        releaseKeyboard()
      }}
      onBlur={releaseKeyboard}
      sx={{
        width: fullWidth ? '100%' : undefined,
        minWidth: fullWidth ? '100%' : (isMobile ? 100 : 140),
        minHeight: isMobile ? 48 : 56,
        borderRadius: 0,
        bgcolor: isPressed ? 'error.main' : 'primary.main',
        color: 'primary.contrastText',
        border: '2px solid',
        borderColor: isPressed ? 'error.main' : 'transparent',
        transition: 'background-color 150ms, border-color 150ms, color 150ms',
        '&:hover': {
          bgcolor: isPressed ? 'error.dark' : 'primary.dark',
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
