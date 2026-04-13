import { createElement, type ReactNode } from 'react'
import Devices from '@mui/icons-material/Devices'
import ChatBubble from '@mui/icons-material/ChatBubble'
import Android from '@mui/icons-material/Android'
import PhoneIphone from '@mui/icons-material/PhoneIphone'
import DesktopWindows from '@mui/icons-material/DesktopWindows'
import LaptopMac from '@mui/icons-material/LaptopMac'
import Language from '@mui/icons-material/Language'
import SettingsInputAntenna from '@mui/icons-material/SettingsInputAntenna'

export type DeviceConfigTabKey = 'freq' | 'system' | 'platform'

type DeviceModelOption = {
  value: number
  label: string
  icon: typeof Devices
  selectable?: boolean
}

const DEVICE_MODEL_DEFINITIONS: DeviceModelOption[] = [
  { value: 0, label: '未知设备', icon: Devices, selectable: false },
  { value: 1, label: 'ESP32 链路盒子（1W 射频版）', icon: SettingsInputAntenna, selectable: true },
  { value: 2, label: 'ESP32 链路盒子（无射频版）', icon: Devices, selectable: true },
  { value: 100, label: '微信小程序', icon: ChatBubble, selectable: true },
  { value: 101, label: 'Android 客户端', icon: Android, selectable: true },
  { value: 102, label: 'iOS 客户端', icon: PhoneIphone, selectable: true },
  { value: 103, label: 'Windows 客户端', icon: DesktopWindows, selectable: true },
  { value: 104, label: 'macOS 客户端', icon: LaptopMac, selectable: true },
  { value: 105, label: '浏览器客户端', icon: Language, selectable: true },
  { value: 106, label: '互联设备（历史）', icon: SettingsInputAntenna, selectable: false },
  { value: 107, label: 'ESP32 链路台/手咪（历史）', icon: SettingsInputAntenna, selectable: false },
  { value: 110, label: '南山对讲桥接器', icon: SettingsInputAntenna, selectable: true },
  { value: 111, label: 'HT 对讲桥接器', icon: SettingsInputAntenna, selectable: true },
  { value: 112, label: '涛涛对讲桥接器', icon: SettingsInputAntenna, selectable: true },
  { value: 113, label: 'NRL2 桥接器', icon: SettingsInputAntenna, selectable: true },
]

export const ALL_DEVICE_MODELS = DEVICE_MODEL_DEFINITIONS
export const DEVICE_MODELS = DEVICE_MODEL_DEFINITIONS.filter((model) => model.selectable)
export type DeviceModelValue = (typeof DEVICE_MODELS)[number]['value']

const UNKNOWN_MODEL: DeviceModelOption = { value: -1, label: '未知设备', icon: Devices, selectable: false }

export function getDeviceInfo(devModel: number): DeviceModelOption {
  if (devModel >= 114 && devModel <= 150) {
    return { value: devModel, label: `互联网桥软件 (${devModel})`, icon: SettingsInputAntenna, selectable: false }
  }
  if (devModel >= 3 && devModel <= 99) {
    return { value: devModel, label: `互联产品 (${devModel})`, icon: SettingsInputAntenna, selectable: false }
  }
  if (devModel >= 151 && devModel <= 255) {
    return { value: devModel, label: `待定型号 (${devModel})`, icon: SettingsInputAntenna, selectable: false }
  }
  return ALL_DEVICE_MODELS.find((model) => model.value === devModel) || UNKNOWN_MODEL
}

export function getDevModelName(devModel: number): string {
  return getDeviceInfo(devModel).label
}

export function getDevModelIcon(devModel: number): ReactNode {
  const IconComponent = getDeviceInfo(devModel).icon
  return createElement(IconComponent, { fontSize: 'small' })
}

export function formatDeviceDisplayName(deviceName: string, devModel: number): string {
  if (isGhostDevice(devModel)) {
    const [callsign] = deviceName.split('-')
    return `${callsign}-${getDevModelName(devModel)}`
  }
  return deviceName
}

export function isGhostDevice(devModel: number): boolean {
  return devModel >= 100 && devModel <= 105
}

export function isLegacyDeviceModel(devModel: number): boolean {
  return devModel === 106 || devModel === 107
}

export function isPlatformOnlyDeviceModel(devModel: number): boolean {
  return devModel >= 110 && devModel <= 150
}

export function supportsFrequencyConfig(devModel: number): boolean {
  if (isPlatformOnlyDeviceModel(devModel)) {
    return false
  }
  return devModel === 1 || devModel === 106 || devModel === 107
}

export function supportsSystemConfig(devModel: number): boolean {
  return !isPlatformOnlyDeviceModel(devModel)
}

export function getDeviceConfigTabs(devModel: number): Array<{ key: DeviceConfigTabKey; label: string }> {
  if (isPlatformOnlyDeviceModel(devModel)) {
    return [{ key: 'platform', label: '平台设置' }]
  }

  if (devModel === 2) {
    return [
      { key: 'system', label: '系统设置' },
      { key: 'platform', label: '平台设置' },
    ]
  }

  const tabs: Array<{ key: DeviceConfigTabKey; label: string }> = []
  if (supportsFrequencyConfig(devModel)) {
    tabs.push({ key: 'freq', label: '频率设置' })
  }
  if (supportsSystemConfig(devModel)) {
    tabs.push({ key: 'system', label: '系统设置' })
  }
  tabs.push({ key: 'platform', label: '平台设置' })
  return tabs
}
