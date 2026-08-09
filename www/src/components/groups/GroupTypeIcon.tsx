import LockOpen from '@mui/icons-material/LockOpen'
import Lock from '@mui/icons-material/Lock'
import type { SxProps } from '@mui/material'

// 群组类型常量
export const GROUP_TYPE_PUBLIC = 1
export const GROUP_TYPE_PRIVATE = 2

interface GroupTypeIconProps {
  type: number
  fontSize?: 'small' | 'medium' | 'large' | 'inherit'
  sx?: SxProps
}

export function GroupTypeIcon({ type, fontSize = 'small', sx }: GroupTypeIconProps) {
  if (type === GROUP_TYPE_PRIVATE) {
    return <Lock color="secondary" fontSize={fontSize} sx={sx} />
  }
  return <LockOpen color="primary" fontSize={fontSize} sx={sx} />
}
