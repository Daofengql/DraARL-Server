import { isValidElement } from 'react'
import { expect, it } from 'vitest'
import { ALL_DEVICE_MODELS, DEVICE_MODELS, getDeviceInfo, getDevModelName, getDevModelIcon, formatDeviceDisplayName, isGhostDevice, isPlatformOnlyDeviceModel, supportsFrequencyConfig, supportsAudioConfig, supportsBoardIOPolarityConfig, supportsSystemConfig, getDeviceConfigTabs } from './deviceModel'

it('displays every known model while keeping legacy models unavailable for creation', () => {
  for (const model of ALL_DEVICE_MODELS) {
    expect(getDeviceInfo(model.value)).toBe(model)
    expect(getDevModelName(model.value)).toBe(model.label)
    expect(isValidElement(getDevModelIcon(model.value))).toBe(true)
  }
  expect(DEVICE_MODELS.some(({ value }) => value === 106 || value === 107 || value === 0)).toBe(false)
  expect(getDevModelName(3)).toBe('互联产品 (3)')
  expect(getDevModelName(99)).toBe('互联产品 (99)')
  expect(getDevModelName(151)).toBe('待定型号 (151)')
  expect(getDevModelName(255)).toBe('待定型号 (255)')
  expect(getDevModelName(-1)).toBe('未知设备')
  expect(getDevModelName(150)).toBe('未知设备')
})
it('matches hardware capabilities and configuration tabs', () => {
  for (const model of [0, 1, 2, 3, 99, 100, 105, 106, 151, 236, 239, 255]) {
    const platform = model >= 236 && model <= 239
    expect(isPlatformOnlyDeviceModel(model)).toBe(platform)
    expect(supportsFrequencyConfig(model)).toBe(model === 1)
    expect(supportsAudioConfig(model)).toBe(model === 1 || model === 2)
    expect(supportsBoardIOPolarityConfig(model)).toBe(model === 2)
    expect(supportsSystemConfig(model)).toBe(!platform)
    expect(isGhostDevice(model)).toBe(model >= 100 && model <= 105)
    expect(getDeviceConfigTabs(model).map(({ key }) => key)).toEqual(platform ? ['platform'] : model === 1 ? ['freq', 'system', 'platform'] : ['system', 'platform'])
  }
  expect(formatDeviceDisplayName('BH5UVN-7', 105)).toBe('BH5UVN-浏览器客户端')
  expect(formatDeviceDisplayName('BH5UVN-7', 1)).toBe('BH5UVN-7')
})
