import {
  ListItem,
  ListItemButton,
  ListItemText,
  ListItemIcon,
  Typography,
  Stack,
  Chip,
} from '@mui/material'
import Lock from '@mui/icons-material/Lock'
import LockOpen from '@mui/icons-material/LockOpen'
import CheckCircle from '@mui/icons-material/CheckCircle'
import type { Group } from '../../../types'

// 群组类型常量
export const GROUP_TYPE_PUBLIC = 1
export const GROUP_TYPE_PRIVATE = 2

interface GroupListItemProps {
  group: Group
  isCurrent?: boolean
  isJoined?: boolean
  disabled?: boolean
  showOnlineCount?: boolean
  onClick: (group: Group) => void
}

export function GroupListItem({
  group,
  isCurrent = false,
  isJoined = false,
  disabled = false,
  showOnlineCount = true,
  onClick,
}: GroupListItemProps) {
  const getGroupIcon = () => {
    if (group.type === GROUP_TYPE_PRIVATE) {
      return <Lock color="secondary" fontSize="small" />
    }
    return <LockOpen color="primary" fontSize="small" />
  }

  return (
    <ListItem
      disablePadding
      secondaryAction={
        isCurrent ? (
          <Chip label="当前" size="small" color="primary" variant="outlined" />
        ) : isJoined && group.type === GROUP_TYPE_PRIVATE ? (
          <CheckCircle color="success" fontSize="small" />
        ) : null
      }
    >
      <ListItemButton
        onClick={() => onClick(group)}
        disabled={disabled || isCurrent}
        selected={isCurrent}
      >
        <ListItemIcon>
          {getGroupIcon()}
        </ListItemIcon>
        <ListItemText
          primary={group.name}
          secondary={
            <Stack direction="row" alignItems="center" spacing={1}>
              <Typography variant="body2" color="text.secondary">
                ID: {group.id}
              </Typography>
              {group.type === GROUP_TYPE_PRIVATE && group.ower_callsign && (
                <>
                  <span>·</span>
                  <Typography variant="body2" color="text.secondary">
                    创建者: {group.ower_callsign}
                  </Typography>
                </>
              )}
              {showOnlineCount && group.online_count !== undefined && group.total_count !== undefined && (
                <>
                  <span>·</span>
                  <Typography variant="body2" color="text.secondary">
                    在线: {group.online_count}/{group.total_count}
                  </Typography>
                </>
              )}
            </Stack>
          }
        />
      </ListItemButton>
    </ListItem>
  )
}
