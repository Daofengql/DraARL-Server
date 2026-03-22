import { useEffect, useRef } from 'react'
import { FormControl, InputLabel, Select, MenuItem, IconButton, Tooltip, CircularProgress } from '@mui/material'
import Refresh from '@mui/icons-material/Refresh'

interface AutoRefreshProps {
  value: number // 0=关闭, n=秒数
  onChange: (seconds: number) => void
  onRefresh: () => void
  loading?: boolean
  label?: string
  size?: 'small' | 'medium'
}

const REFRESH_OPTIONS = [
  { value: 0, label: '关闭' },
  { value: 10, label: '10秒' },
  { value: 30, label: '30秒' },
  { value: 60, label: '1分钟' },
  { value: 300, label: '5分钟' },
]

export function AutoRefresh({
  value,
  onChange,
  onRefresh,
  loading = false,
  label = '自动刷新',
  size = 'small',
}: AutoRefreshProps) {
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }

    if (value > 0) {
      timerRef.current = setInterval(() => {
        onRefresh()
      }, value * 1000)
    }

    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current)
      }
    }
  }, [value, onRefresh])

  return (
    <>
      <FormControl size={size} sx={{ minWidth: 100 }}>
        <InputLabel>{label}</InputLabel>
        <Select
          value={value}
          label={label}
          onChange={(e) => onChange(Number(e.target.value))}
        >
          {REFRESH_OPTIONS.map((option) => (
            <MenuItem key={option.value} value={option.value}>
              {option.label}
            </MenuItem>
          ))}
        </Select>
      </FormControl>
      <Tooltip title="立即刷新">
        <IconButton onClick={onRefresh} disabled={loading} size={size}>
          {loading ? <CircularProgress size={20} /> : <Refresh />}
        </IconButton>
      </Tooltip>
    </>
  )
}
