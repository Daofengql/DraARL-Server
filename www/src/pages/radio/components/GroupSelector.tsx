import React, { useMemo, useState } from 'react'
import {
  Box,
  Checkbox,
  CircularProgress,
  FormControl,
  IconButton,
  InputLabel,
  ListItemText,
  MenuItem,
  Popover,
  Select,
  Slider,
  Stack,
  Tab,
  Tabs,
  Tooltip,
  Typography,
} from '@mui/material'
import type { SelectChangeEvent } from '@mui/material/Select'
import SendIcon from '@mui/icons-material/Send'
import HeadphonesIcon from '@mui/icons-material/Headphones'
import TuneIcon from '@mui/icons-material/Tune'
import VolumeUpIcon from '@mui/icons-material/VolumeUp'
import type { RadioGroup } from '../../../types/radio'

interface GroupSelectorProps {
  groups: RadioGroup[]
  txGroupId: number
  rxGroupIds: number[]
  activeGroupId: number
  channelVolumes: Record<string, number>
  onTxChange: (groupId: number) => void
  onRxChange: (groupIds: number[]) => void
  onActiveGroupChange: (groupId: number) => void
  onChannelVolumeChange: (groupId: number, volume: number) => void
  disabled?: boolean
  updating?: boolean
}

const MAX_RECEIVE_GROUPS = 16

export const GroupSelector: React.FC<GroupSelectorProps> = ({
  groups,
  txGroupId,
  rxGroupIds,
  activeGroupId,
  channelVolumes,
  onTxChange,
  onRxChange,
  onActiveGroupChange,
  onChannelVolumeChange,
  disabled = false,
  updating = false,
}) => {
  const [mixerAnchor, setMixerAnchor] = useState<HTMLElement | null>(null)
  const groupById = useMemo(() => new Map(groups.map(group => [group.id, group])), [groups])
  const receiveGroups = rxGroupIds
    .map(groupId => groupById.get(groupId))
    .filter((group): group is RadioGroup => Boolean(group))

  const handleReceiveChange = (event: SelectChangeEvent<number[]>) => {
    const value = event.target.value
    const selected = (typeof value === 'string' ? value.split(',') : value)
      .map(Number)
      .filter(groupId => Number.isInteger(groupId) && groupId > 0)
    onRxChange(Array.from(new Set([...selected, txGroupId])))
  }

  return (
    <Box sx={{ minWidth: 0, flex: 1 }}>
      <Stack
        direction={{ xs: 'column', sm: 'row' }}
        spacing={1}
        sx={{ alignItems: { xs: 'stretch', sm: 'center' } }}
      >
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', minWidth: 0, flex: { sm: '0 1 250px' } }}>
          <SendIcon color="action" fontSize="small" />
          <FormControl size="small" sx={{ minWidth: 0, flex: 1 }}>
            <InputLabel id="tx-group-label">发送频道</InputLabel>
            <Select
              labelId="tx-group-label"
              value={txGroupId || ''}
              label="发送频道"
              disabled={disabled || updating}
              onChange={event => onTxChange(Number(event.target.value))}
            >
              {groups.map(group => (
                <MenuItem key={group.id} value={group.id}>{group.name}</MenuItem>
              ))}
            </Select>
          </FormControl>
        </Stack>

        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', minWidth: 0, flex: 1 }}>
          <HeadphonesIcon color="action" fontSize="small" />
          <FormControl size="small" sx={{ minWidth: 0, flex: 1 }}>
            <InputLabel id="rx-groups-label">收听频道</InputLabel>
            <Select<number[]>
              multiple
              labelId="rx-groups-label"
              value={rxGroupIds}
              label="收听频道"
              disabled={disabled || updating}
              onChange={handleReceiveChange}
              renderValue={selected => {
                const names = selected.map(groupId => groupById.get(groupId)?.name || `#${groupId}`)
                return names.length <= 2 ? names.join('、') : `${names.length} 个频道`
              }}
              MenuProps={{ PaperProps: { sx: { maxHeight: 420 } } }}
            >
              {groups.map(group => {
                const checked = rxGroupIds.includes(group.id)
                const requiredByTx = group.id === txGroupId
                const atLimit = rxGroupIds.length >= MAX_RECEIVE_GROUPS && !checked
                return (
                  <MenuItem key={group.id} value={group.id} disabled={requiredByTx || atLimit}>
                    <Checkbox checked={checked} size="small" />
                    <ListItemText primary={group.name} secondary={`${group.onlineCount} 在线`} />
                  </MenuItem>
                )
              })}
            </Select>
          </FormControl>

          {updating && <CircularProgress size={20} />}
          <Tooltip title="频道混音">
            <span>
              <IconButton
                size="small"
                disabled={disabled || rxGroupIds.length === 0}
                onClick={event => setMixerAnchor(event.currentTarget)}
              >
                <TuneIcon />
              </IconButton>
            </span>
          </Tooltip>
        </Stack>
      </Stack>

      <Tabs
        value={activeGroupId}
        onChange={(_, value: number) => onActiveGroupChange(value)}
        variant="scrollable"
        scrollButtons="auto"
        allowScrollButtonsMobile
        sx={{ minHeight: 34, mt: 0.5, '& .MuiTab-root': { minHeight: 34, py: 0, minWidth: 72 } }}
      >
        <Tab value={0} label="实时" />
        {receiveGroups.map(group => (
          <Tab key={group.id} value={group.id} label={group.name} />
        ))}
      </Tabs>

      <Popover
        open={Boolean(mixerAnchor)}
        anchorEl={mixerAnchor}
        onClose={() => setMixerAnchor(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
      >
        <Box sx={{ width: { xs: 280, sm: 340 }, p: 2 }}>
          <Typography variant="subtitle2" sx={{ mb: 1 }}>频道混音</Typography>
          <Stack spacing={1.5}>
            {receiveGroups.map(group => (
              <Box key={group.id}>
                <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                  <VolumeUpIcon color="action" fontSize="small" />
                  <Typography variant="body2" noWrap sx={{ width: 110 }}>{group.name}</Typography>
                  <Slider
                    size="small"
                    aria-label={`${group.name} 音量`}
                    value={Math.round((channelVolumes[String(group.id)] ?? 1) * 100)}
                    onChange={(_, value) => onChannelVolumeChange(group.id, Number(value) / 100)}
                  />
                  <Typography variant="caption" sx={{ width: 32, textAlign: 'right' }}>
                    {Math.round((channelVolumes[String(group.id)] ?? 1) * 100)}
                  </Typography>
                </Stack>
              </Box>
            ))}
          </Stack>
        </Box>
      </Popover>
    </Box>
  )
}

export default GroupSelector
