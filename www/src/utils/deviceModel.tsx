import Devices from '@mui/icons-material/Devices'
import ChatBubble from '@mui/icons-material/ChatBubble'
import Android from '@mui/icons-material/Android'
import PhoneIphone from '@mui/icons-material/PhoneIphone'
import DesktopWindows from '@mui/icons-material/DesktopWindows'
import LaptopMac from '@mui/icons-material/LaptopMac'
import Language from '@mui/icons-material/Language'
import SettingsInputAntenna from '@mui/icons-material/SettingsInputAntenna'

// 设备型号定义
export const DEVICE_MODELS = [
  { value: 0, label: '未知设备', icon: Devices },
  { value: 100, label: '微信小程序', icon: ChatBubble },
  { value: 101, label: 'Android 客户端', icon: Android },
  { value: 102, label: 'iOS 客户端', icon: PhoneIphone },
  { value: 103, label: 'Windows 客户端', icon: DesktopWindows },
  { value: 104, label: 'macOS 客户端', icon: LaptopMac },
  { value: 105, label: '浏览器客户端', icon: Language },
  { value: 106, label: '互联设备', icon: SettingsInputAntenna },
  { value: 107, label: 'ESP32 链路台/手咪', icon: SettingsInputAntenna },
  { value: 110, label: '南山对讲桥接器', icon: SettingsInputAntenna },
  { value: 111, label: 'HT 对讲桥接器', icon: SettingsInputAntenna },
  { value: 112, label: '涛涛对讲桥接器', icon: SettingsInputAntenna },
] as const

export type DeviceModelValue = (typeof DEVICE_MODELS)[number]['value']

// 获取设备型号信息
export function getDeviceInfo(devModel: number) {
  return DEVICE_MODELS.find((m) => m.value === devModel) || DEVICE_MODELS[0]
}

// 获取设备型号名称
export function getDevModelName(devModel: number): string {
  return getDeviceInfo(devModel).label
}

// 获取设备型号图标组件
export function getDevModelIcon(devModel: number): React.ReactNode {
  const info = getDeviceInfo(devModel)
  const IconComponent = info.icon
  return <IconComponent fontSize="small" />
}

// 格式化设备显示名称（幽灵设备特殊处理）
// 幽灵设备（100-105）：呼号-设备名称（如 BH5UVN-安卓客户端）
// 普通设备：呼号-SSID（如 BH5UVN-1）
export function formatDeviceDisplayName(deviceName: string, devModel: number): string {
  // 检查是否是幽灵设备
  if (devModel >= 100 && devModel <= 105) {
    // 幽灵设备：替换 SSID 为设备名称
    const [callsign] = deviceName.split('-')
    const devModelName = getDevModelName(devModel)
    return `${callsign}-${devModelName}`
  }
  // 普通设备：保持原样
  return deviceName
}

// 检查是否是幽灵设备
export function isGhostDevice(devModel: number): boolean {
  return devModel >= 100 && devModel <= 105
}

export function isPlatformOnlyDeviceModel(devModel: number): boolean {
  return devModel >= 110 && devModel <= 112
}
