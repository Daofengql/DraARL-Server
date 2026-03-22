import Circle from '@mui/icons-material/Circle'
import type { SxProps } from '@mui/material'

interface OnlineIndicatorProps {
  online: boolean
  size?: number
  sx?: SxProps
}

export function OnlineIndicator({ online, size = 12, sx }: OnlineIndicatorProps) {
  return (
    <Circle
      sx={{
        fontSize: size,
        color: online ? 'success.main' : 'text.disabled',
        ...sx,
      }}
    />
  )
}
