import { Alert } from '@mui/material'
import type { RadioConfigForm } from '../../../utils/radioConfig'
import { resolveFrequencyCardProfile } from '../../../utils/radioConfig'
import { SA818FrequencyCard } from './SA818FrequencyCard'

interface FrequencyConfigCardContainerProps {
  devModel: number
  cardId?: string | null
  value: RadioConfigForm
  onChange: (next: RadioConfigForm) => void
}

export function FrequencyConfigCardContainer({
  devModel,
  cardId,
  value,
  onChange,
}: FrequencyConfigCardContainerProps) {
  const profile = resolveFrequencyCardProfile(devModel, cardId)

  if (!profile) {
    return (
      <Alert severity="info">
        当前设备型号尚未命中频率卡片映射，已安全降级为不显示频率配置表单。
      </Alert>
    )
  }

  switch (profile.cardId) {
    case 'sa818-radio-v1':
      return <SA818FrequencyCard value={value} onChange={onChange} />
    default:
      return (
        <Alert severity="info">
          当前设备型号尚未命中频率卡片映射，已安全降级为不显示频率配置表单。
        </Alert>
      )
  }
}
