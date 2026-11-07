import type { BleProvisionConfig, BleProvisionServerConfig, BleProvisionWifiConfig } from '../services/bleProvision'

// Firmware may redact passwords on readback. Verify the public configuration
// fields and rely on the firmware RPC acknowledgement for secret fields.
export function verifyWifiReadback(expected: BleProvisionWifiConfig, actual: BleProvisionConfig['wifi']): void {
  if (expected.ssid !== actual.ssid || expected.dhcp !== actual.dhcp || (!expected.dhcp && ['ip', 'gateway', 'subnet', 'dns1', 'dns2'].some((key) => expected[key as keyof BleProvisionWifiConfig] !== actual[key as keyof BleProvisionWifiConfig]))) {
    throw new Error('设备已接收保存请求，但回读的 WiFi 配置不一致，请重新读取并核对')
  }
}

export function verifyServerReadback(expected: BleProvisionServerConfig, actual: BleProvisionConfig['server']): void {
  const keys: (keyof BleProvisionServerConfig)[] = ['callsign', 'nodeSsid', 'udpHost', 'udpPort', 'httpApiBaseUrl', 'account']
  if (keys.some((key) => expected[key] !== actual[key])) throw new Error('设备已接收保存请求，但回读的 DraARL 配置不一致，请重新读取并核对')
}
