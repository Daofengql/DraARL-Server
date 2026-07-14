import { Box, Paper, TextField, Typography } from '@mui/material'
import {
  ADC_GAIN_MAX_DB,
  ADC_GAIN_MIN_DB,
  ADC_GAIN_STEP_DB,
  ADC_VOLUME_DEFAULT,
  AUDIO_VOLUME_MAX,
  AUDIO_VOLUME_MIN,
  DAC_VOLUME_DEFAULT,
  normalizeAdcGainDb,
  normalizeAudioVolume,
  type RadioConfigForm,
} from '../../../utils/radioConfig'

interface AudioConfigCardProps {
  value: RadioConfigForm
  onChange: (next: RadioConfigForm) => void
}

export function AudioConfigCard({ value, onChange }: AudioConfigCardProps) {
  const updateAudioNumber = (
    field: 'adcGainDb' | 'adcVolume' | 'dacVolume',
    rawValue: string,
  ) => {
    if (rawValue.trim() === '') {
      return
    }

    const parsed = Number(rawValue)
    if (!Number.isFinite(parsed)) {
      return
    }

    const nextValue = field === 'adcGainDb'
      ? normalizeAdcGainDb(parsed)
      : normalizeAudioVolume(parsed, field === 'adcVolume' ? ADC_VOLUME_DEFAULT : DAC_VOLUME_DEFAULT)
    onChange({ ...value, [field]: nextValue })
  }

  return (
    <Paper variant="outlined" sx={{ p: 2 }}>
      <Typography variant="subtitle2" sx={{ mb: 1.5 }}>
        音频电平
      </Typography>
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(3, minmax(0, 1fr))' }, gap: 2 }}>
        <TextField
          fullWidth
          type="number"
          label="ADC 增益 (dB)"
          value={value.adcGainDb}
          onChange={(event) => updateAudioNumber('adcGainDb', event.target.value)}
          slotProps={{ htmlInput: { min: ADC_GAIN_MIN_DB, max: ADC_GAIN_MAX_DB, step: ADC_GAIN_STEP_DB } }}
          helperText={`${ADC_GAIN_MIN_DB}-${ADC_GAIN_MAX_DB} dB，${ADC_GAIN_STEP_DB} dB 步进`}
        />
        <TextField
          fullWidth
          type="number"
          label="ADC 音量"
          value={value.adcVolume}
          onChange={(event) => updateAudioNumber('adcVolume', event.target.value)}
          slotProps={{ htmlInput: { min: AUDIO_VOLUME_MIN, max: AUDIO_VOLUME_MAX, step: 1 } }}
          helperText={`${AUDIO_VOLUME_MIN}-${AUDIO_VOLUME_MAX}`}
        />
        <TextField
          fullWidth
          type="number"
          label="DAC 音量"
          value={value.dacVolume}
          onChange={(event) => updateAudioNumber('dacVolume', event.target.value)}
          slotProps={{ htmlInput: { min: AUDIO_VOLUME_MIN, max: AUDIO_VOLUME_MAX, step: 1 } }}
          helperText={`${AUDIO_VOLUME_MIN}-${AUDIO_VOLUME_MAX}`}
        />
      </Box>
    </Paper>
  )
}
