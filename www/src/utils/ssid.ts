export const DYNAMIC_BIND_SSID_HINT = 'SSID 必须在 1-99 或 106-235 范围内'

export function isValidDynamicBindSSID(ssid: number): boolean {
  return (ssid >= 1 && ssid <= 99) || (ssid >= 106 && ssid <= 235)
}
