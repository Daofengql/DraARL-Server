import { useCallback, useEffect, useState } from 'react'
import {
  BleProvisioningClient,
  createEmptyProvisionConfig,
  type BleProvisionConfig,
  type BleProvisionServerConfig,
  type BleProvisionStatus,
  type BleProvisionWifiConfig,
  type BleProvisionWifiNetwork,
} from '../services/bleProvision'

const EMPTY_STATUS: BleProvisionStatus = {
  connected: false,
  deviceName: '',
  wifiState: '未知',
  bleState: '未知',
  authenticated: false,
  rssi: null,
}

export function useBleProvisioning() {
  const [status, setStatus] = useState<BleProvisionStatus>(EMPTY_STATUS)
  const [client] = useState(() => new BleProvisioningClient({
    onStatusChange: setStatus,
    onDisconnect: () => setStatus(EMPTY_STATUS),
  }))

  useEffect(() => {
    return () => {
      void client.disconnect(false)
    }
  }, [client])

  const disconnect = useCallback(async () => {
    await client.disconnect()
  }, [client])

  return {
    supported: client.supported,
    status,
    connect: async () => {
      await client.connect()
      return client.getStatus()
    },
    disconnect,
    refreshStatus: async () => client.refreshStatus(),
    authenticate: async (dynamicCode: string) => client.authenticate(dynamicCode),
    loadConfig: async (): Promise<BleProvisionConfig> => client.loadConfig(),
    scanWifi: async (): Promise<{ networks: BleProvisionWifiNetwork[]; partial: boolean; scanInProgress: boolean }> =>
      client.scanWifi(),
    saveWifi: async (config: BleProvisionWifiConfig) => client.saveWifi(config),
    saveServer: async (config: BleProvisionServerConfig) => client.saveServer(config),
    createEmptyConfig: (): BleProvisionConfig => createEmptyProvisionConfig(),
  }
}
