import type { RadioPreset } from './types'

export function buildPresetOrders(presets: RadioPreset[]): Array<{ id: number; order: number }> {
  return presets.map((preset, order) => ({ id: preset.id, order }))
}
