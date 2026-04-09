import { useEffect, useState } from 'react'
import {
  Autocomplete,
  Box,
  Button,
  CircularProgress,
  Divider,
  FormControl,
  FormControlLabel,
  InputLabel,
  MenuItem,
  Paper,
  Select,
  Switch,
  TextField,
  Typography,
} from '@mui/material'
import { relayService } from '../../../services/relay'
import type { Relay } from '../../../types'
import { RegionCascader } from '../../common/RegionCascader'
import { ToneSelector } from './ToneSelector'
import {
  POWER_OPTIONS,
  SQL_LEVEL_OPTIONS,
  buildToneSelection,
  formatToneDisplay,
  normalizeSquelchLevel,
  type RadioConfigForm,
} from '../../../utils/radioConfig'
import { getErrorMessage } from '../../../utils/errorMessage'

interface RelayPreset {
  id: number
  name: string
  txFreq: string
  rxFreq: string
  txToneRaw: string
  rxToneRaw: string
  sameFreq: boolean
}

interface SA818FrequencyCardProps {
  value: RadioConfigForm
  onChange: (next: RadioConfigForm) => void
}

const relayToPreset = (relay: Relay): RelayPreset => ({
  id: relay.id,
  name: relay.name,
  txFreq: relay.up_freq || '',
  rxFreq: relay.down_freq || '',
  txToneRaw: relay.send_ctcss || '',
  rxToneRaw: relay.receive_ctcss || '',
  sameFreq: relay.up_freq === relay.down_freq,
})

export function SA818FrequencyCard({ value, onChange }: SA818FrequencyCardProps) {
  const [relayLocation, setRelayLocation] = useState('')
  const [relayPresets, setRelayPresets] = useState<RelayPreset[]>([])
  const [selectedRelay, setSelectedRelay] = useState<RelayPreset | null>(null)
  const [relaySearching, setRelaySearching] = useState(false)
  const [relayError, setRelayError] = useState('')

  useEffect(() => {
    setRelayPresets([])
    setSelectedRelay(null)
    setRelayError('')
  }, [relayLocation])

  const handleSearchRelays = async () => {
    const locationParts = relayLocation.split(' ').filter(Boolean)
    if (locationParts.length < 2) {
      setRelayError('请至少选择到市级别')
      return
    }

    setRelaySearching(true)
    setRelayError('')
    try {
      const relays = await relayService.publicSearch(relayLocation)
      setRelayPresets(relays.map(relayToPreset))
      setSelectedRelay(null)
      if (relays.length === 0) {
        setRelayError('该地区暂无中继台数据')
      }
    } catch (error) {
      console.error('搜索中继台失败:', error)
      setRelayError(getErrorMessage(error, '搜索中继台失败'))
    } finally {
      setRelaySearching(false)
    }
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
      <Paper variant="outlined" sx={{ p: 2, bgcolor: 'grey.50' }}>
        <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1.5 }}>
          中继台预设填入
        </Typography>
        <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, gap: 2, alignItems: { sm: 'flex-end' } }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <RegionCascader
              value={relayLocation}
              onChange={setRelayLocation}
              label="选择地区"
              size="small"
            />
          </Box>
          <Button
            variant="outlined"
            size="small"
            onClick={handleSearchRelays}
            disabled={relaySearching}
            startIcon={relaySearching ? <CircularProgress size={16} color="inherit" /> : null}
            sx={{ minWidth: 80, height: 40 }}
          >
            {relaySearching ? '搜索中...' : '搜索'}
          </Button>
        </Box>
        {relayError && (
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
            {relayError}
          </Typography>
        )}
        {relayPresets.length > 0 && (
          <Autocomplete
            fullWidth
            size="small"
            sx={{ mt: 2 }}
            options={relayPresets}
            value={selectedRelay}
            getOptionLabel={(option) => option.name}
            isOptionEqualToValue={(option, selected) => option.id === selected.id}
            onChange={(_, preset) => {
              setSelectedRelay(preset)
              if (!preset) return
              onChange({
                ...value,
                txFreq: preset.txFreq || '',
                rxFreq: preset.rxFreq || '',
                txTone: buildToneSelection({ legacy: preset.txToneRaw }),
                rxTone: buildToneSelection({ legacy: preset.rxToneRaw }),
                sameFreq: preset.sameFreq,
              })
            }}
            renderInput={(params) => (
              <TextField
                {...params}
                label="选择中继台"
                placeholder="选择后自动填入参数"
              />
            )}
            renderOption={(props, option) => (
              <li {...props} key={option.id}>
                <Box>
                  <Typography variant="body2">{option.name}</Typography>
                  <Typography variant="caption" color="text.secondary">
                    发: {option.txFreq} MHz / 收: {option.rxFreq} MHz / 发亚音: {formatToneDisplay(option.txToneRaw)} / 收亚音: {formatToneDisplay(option.rxToneRaw)}
                  </Typography>
                </Box>
              </li>
            )}
            noOptionsText="暂无中继台数据"
          />
        )}
      </Paper>

      <Divider />

      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
        <TextField
          fullWidth
          label="发射频率 (MHz)"
          value={value.txFreq}
          onChange={(event) => {
            const nextTxFreq = event.target.value
            onChange({
              ...value,
              txFreq: nextTxFreq,
              rxFreq: value.sameFreq ? nextTxFreq : value.rxFreq,
            })
          }}
          placeholder="例如: 439.500"
        />
        <TextField
          fullWidth
          label="接收频率 (MHz)"
          value={value.sameFreq ? value.txFreq : value.rxFreq}
          onChange={(event) => onChange({ ...value, rxFreq: event.target.value })}
          disabled={value.sameFreq}
          placeholder="例如: 439.500"
        />
      </Box>

      <FormControlLabel
        control={(
          <Switch
            checked={value.sameFreq}
            onChange={(event) => onChange({
              ...value,
              sameFreq: event.target.checked,
              rxFreq: event.target.checked ? value.txFreq : value.rxFreq,
            })}
          />
        )}
        label="收发同频"
      />

      <Divider />

      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
        <ToneSelector
          label="发送亚音"
          value={value.txTone}
          onChange={(nextTone) => onChange({ ...value, txTone: nextTone })}
        />
        <ToneSelector
          label="接收亚音"
          value={value.rxTone}
          onChange={(nextTone) => onChange({ ...value, rxTone: nextTone })}
        />
      </Box>

      <Divider />

      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
        <FormControl fullWidth size="small">
          <InputLabel>SQL 等级</InputLabel>
          <Select
            value={String(value.squelch)}
            label="SQL 等级"
            onChange={(event) => onChange({
              ...value,
              squelch: normalizeSquelchLevel(Number(event.target.value)),
            })}
          >
            {SQL_LEVEL_OPTIONS.map((level) => (
              <MenuItem key={level} value={String(level)}>
                {level}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <FormControl fullWidth size="small">
          <InputLabel>发射功率</InputLabel>
          <Select
            value={value.power}
            label="发射功率"
            onChange={(event) => onChange({
              ...value,
              power: event.target.value as RadioConfigForm['power'],
            })}
          >
            {POWER_OPTIONS.map((option) => (
              <MenuItem key={option.value} value={option.value}>
                {option.label}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>
    </Box>
  )
}
