import { expect, it } from 'vitest'
import { verifyWifiReadback, verifyServerReadback } from './provisionVerification'
import { createWifiFormValue } from '../components/devices/preconfig/ProvisionWifiForm'
import { createServerFormValue } from '../components/devices/preconfig/ProvisionDraarlForm'

it('accepts redacted WiFi passwords and ignores inactive static fields with DHCP', () => {
  const wifi = createWifiFormValue({ ssid: 'test', password: 'secret' })
  expect(() => verifyWifiReadback(wifi, { ...wifi, password: '', ip: 'old-value' })).not.toThrow()
  expect(() => verifyWifiReadback(wifi, { ...wifi, ssid: 'other' })).toThrow('回读')
  expect(() => verifyWifiReadback(wifi, { ...wifi, dhcp: false })).toThrow('回读')
})
it('requires every active static network field to match', () => {
  const wifi = createWifiFormValue({ ssid: 'test', dhcp: false, ip: '10.0.0.2', gateway: '10.0.0.1', subnet: '255.255.255.0', dns1: '1.1.1.1', dns2: '8.8.8.8' })
  expect(() => verifyWifiReadback(wifi, wifi)).not.toThrow()
  for (const field of ['ip', 'gateway', 'subnet', 'dns1', 'dns2']) expect(() => verifyWifiReadback(wifi, { ...wifi, [field]: 'changed' })).toThrow('回读')
})
it('checks server routing and identity while accepting redacted device secrets', () => {
  const server = createServerFormValue({ callsign: 'BH5UVN', nodeSsid: 1, udpHost: 'test.invalid', udpPort: 60050, account: 'admin', deviceAuthPassword: 'secret' })
  expect(() => verifyServerReadback(server, { ...server, deviceAuthPassword: '' })).not.toThrow()
  for (const field of ['callsign', 'nodeSsid', 'udpHost', 'udpPort', 'httpApiBaseUrl', 'account']) expect(() => verifyServerReadback(server, { ...server, [field]: 'changed' })).toThrow('回读')
})
