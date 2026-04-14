import { getDevModelName } from '../../../utils/deviceModel'

export interface ProvisionDeviceProfile {
  key: string
  devModel: number
  label: string
  description: string
  supportsWifi: boolean
  supportsDraarl: boolean
}

export const PROVISION_DEVICE_PROFILES: ProvisionDeviceProfile[] = [
  {
    key: 'devmodel1',
    devModel: 1,
    label: getDevModelName(1),
    description: '支持通过 BLE 完成 WiFi 与 DraARL 预配置',
    supportsWifi: true,
    supportsDraarl: true,
  },
]

export function getProvisionDeviceProfile(key: string | null | undefined) {
  return PROVISION_DEVICE_PROFILES.find((item) => item.key === key) || null
}
