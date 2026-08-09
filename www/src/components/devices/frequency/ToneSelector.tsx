import {
  Box,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Typography,
} from '@mui/material'
import type { ToneSelection } from '../../../utils/radioConfig'
import { TONE_MODE_OPTIONS, getToneValueOptions } from '../../../utils/radioConfig'

interface ToneSelectorProps {
  label: string
  value: ToneSelection
  onChange: (next: ToneSelection) => void
}

export function ToneSelector({ label, value, onChange }: ToneSelectorProps) {
  const valueOptions = getToneValueOptions(value.mode)

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      <Typography variant="subtitle2">{label}</Typography>
      <FormControl fullWidth size="small">
        <InputLabel>亚音类型</InputLabel>
        <Select
          value={value.mode}
          label="亚音类型"
          onChange={(event) => {
            const nextMode = event.target.value as ToneSelection['mode']
            const nextValues = getToneValueOptions(nextMode)
            onChange({
              mode: nextMode,
              value: nextMode === 'off' ? '0' : (nextValues[0] || '0'),
            })
          }}
        >
          {TONE_MODE_OPTIONS.map((option) => (
            <MenuItem key={option.value} value={option.value}>
              {option.label}
            </MenuItem>
          ))}
        </Select>
      </FormControl>

      <FormControl fullWidth size="small" disabled={value.mode === 'off'}>
        <InputLabel>亚音值</InputLabel>
        <Select
          value={value.mode === 'off' ? '' : value.value}
          label="亚音值"
          onChange={(event) => onChange({ mode: value.mode, value: String(event.target.value) })}
        >
          <MenuItem value="" disabled>
            {value.mode === 'off' ? 'OFF' : '请选择'}
          </MenuItem>
          {valueOptions.map((option) => (
            <MenuItem key={option} value={option}>
              {option}
            </MenuItem>
          ))}
        </Select>
      </FormControl>
    </Box>
  )
}
