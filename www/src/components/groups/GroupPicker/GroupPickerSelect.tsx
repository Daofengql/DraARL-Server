import {
  FormControl,
  Select,
  MenuItem,
  InputLabel,
  Box,
  Typography,
  Chip,
  useTheme,
  useMediaQuery,
} from '@mui/material'
import type { SelectChangeEvent } from '@mui/material'
import GroupIcon from '@mui/icons-material/Group'
import Lock from '@mui/icons-material/Lock'
import LockOpen from '@mui/icons-material/LockOpen'
import type { Group } from '../../../types'
import { GROUP_TYPE_PRIVATE } from './GroupListItem'

interface GroupPickerSelectProps {
  groups: Group[]
  currentGroupId: number
  onChange: (groupId: number) => void
  disabled?: boolean
  size?: 'small' | 'medium'
  showOnlineCount?: boolean
  label?: string
}

export function GroupPickerSelect({
  groups,
  currentGroupId,
  onChange,
  disabled = false,
  size = 'small',
  showOnlineCount = true,
  label,
}: GroupPickerSelectProps) {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))

  const handleChange = (event: SelectChangeEvent) => {
    onChange(parseInt(event.target.value, 10))
  }

  const currentGroup = groups.find((g) => g.id === currentGroupId)

  const getGroupIcon = (group: Group) => {
    if (group.type === GROUP_TYPE_PRIVATE) {
      return <Lock fontSize="small" />
    }
    return <LockOpen fontSize="small" />
  }

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      <GroupIcon color="action" />

      <FormControl size={size} sx={{ minWidth: isMobile ? 120 : 200 }}>
        <InputLabel id="group-picker-label">
          {label || (isMobile ? '群组' : '当前群组')}
        </InputLabel>
        <Select
          labelId="group-picker-label"
          value={currentGroupId.toString()}
          label={label || (isMobile ? '群组' : '当前群组')}
          onChange={handleChange}
          disabled={disabled}
        >
          {groups.map((group) => (
            <MenuItem key={group.id} value={group.id}>
              <Box
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  width: '100%',
                  gap: 1,
                }}
              >
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                  {getGroupIcon(group)}
                  <Typography noWrap sx={{ maxWidth: 150 }}>
                    {group.name}
                  </Typography>
                </Box>
                {showOnlineCount && group.online_count !== undefined && (
                  <Chip
                    size="small"
                    label={group.online_count}
                    sx={{ height: 20, fontSize: '0.7rem' }}
                  />
                )}
              </Box>
            </MenuItem>
          ))}
        </Select>
      </FormControl>

      {currentGroup && showOnlineCount && currentGroup.online_count !== undefined && (
        <Chip
          size="small"
          label={`${currentGroup.online_count} 在线`}
          color="primary"
          variant="outlined"
        />
      )}
    </Box>
  )
}
