/**
 * 群组选择器组件
 */

import React, { useState, useEffect } from 'react'
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
import { Group as GroupIcon } from '@mui/icons-material'
import type { RadioGroup } from '../../../types/radio'

interface GroupSelectorProps {
  groups: RadioGroup[]
  currentGroupId: number
  onChange: (groupId: number) => void
  disabled?: boolean
}

export const GroupSelector: React.FC<GroupSelectorProps> = ({
  groups,
  currentGroupId,
  onChange,
  disabled = false,
}) => {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))

  const handleChange = (event: SelectChangeEvent) => {
    onChange(parseInt(event.target.value, 10))
  }

  const currentGroup = groups.find(g => g.id === currentGroupId)

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      <GroupIcon color="action" />

      <FormControl size="small" sx={{ minWidth: isMobile ? 120 : 200 }}>
        <InputLabel id="group-selector-label">
          {isMobile ? '群组' : '当前群组'}
        </InputLabel>
        <Select
          labelId="group-selector-label"
          value={currentGroupId.toString()}
          label={isMobile ? '群组' : '当前群组'}
          onChange={handleChange}
          disabled={disabled}
        >
          {groups.map((group) => (
            <MenuItem key={group.id} value={group.id}>
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
                <Typography noWrap sx={{ maxWidth: 150 }}>
                  {group.name}
                </Typography>
                <Chip
                  size="small"
                  label={group.onlineCount}
                  sx={{ ml: 1, height: 20, fontSize: '0.7rem' }}
                />
              </Box>
            </MenuItem>
          ))}
        </Select>
      </FormControl>

      {currentGroup && (
        <Chip
          size="small"
          label={`${currentGroup.onlineCount} 在线`}
          color="primary"
          variant="outlined"
        />
      )}
    </Box>
  )
}

export default GroupSelector
