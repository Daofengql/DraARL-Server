import { TextField, InputAdornment, IconButton, CircularProgress } from '@mui/material'
import Search from '@mui/icons-material/Search'
import Clear from '@mui/icons-material/Clear'

interface SearchBarProps {
  value: string
  onChange: (value: string) => void
  onSearch: () => void
  placeholder?: string
  loading?: boolean
  size?: 'small' | 'medium'
  fullWidth?: boolean
  sx?: object
}

export function SearchBar({
  value,
  onChange,
  onSearch,
  placeholder = '搜索...',
  loading = false,
  size = 'small',
  fullWidth = false,
  sx,
}: SearchBarProps) {
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      onSearch()
    }
  }

  const handleClear = () => {
    onChange('')
  }

  return (
    <TextField
      size={size}
      fullWidth={fullWidth}
      placeholder={placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={handleKeyDown}
      sx={sx}
      slotProps={{
        input: {
          startAdornment: (
            <InputAdornment position="start">
              {loading ? <CircularProgress size={20} /> : <Search />}
            </InputAdornment>
          ),
          endAdornment: value ? (
            <InputAdornment position="end">
              <IconButton size="small" onClick={handleClear} edge="end">
                <Clear fontSize="small" />
              </IconButton>
            </InputAdornment>
          ) : null,
        },
      }}
    />
  )
}
