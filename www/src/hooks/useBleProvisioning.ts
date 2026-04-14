import { useEffect, useRef, useState } from 'react'
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
  const clientRef = useRef<BleProvisioningClient | null>(null)
  const [status, setStatus] = useState<BleProvisionStatus>(EMPTY_STATUS)

  const ensureClient = () => {
    if (!clientRef.current) {
      clientRef.current = new BleProvisioningClient({
        onStatusChange: setStatus,
        onDisconnect: () => setStatus(EMPTY_STATUS),
      })
    }
    return clientRef.current
  }

  useEffect(() => {
    return () => {
      const client = clientRef.current
      clientRef.current = null
      if (client) {
        void client.disconnect(false)
      }
    }
  }, [])

  return {
    supported: ensureClient().supported,
    status,
    connect: async () => {
      const client = ensureClient()
      await client.connect()
      return client.getStatus()
    },
    disconnect: async () => {
      const client = clientRef.current
      if (!client) {
        return
      }
      await client.disconnect()
    },
    refreshStatus: async () => ensureClient().refreshStatus(),
    authenticate: async (dynamicCode: string) => ensureClient().authenticate(dynamicCode),
    loadConfig: async (): Promise<BleProvisionConfig> => ensureClient().loadConfig(),
    scanWifi: async (): Promise<{ networks: BleProvisionWifiNetwork[]; partial: boolean; scanInProgress: boolean }> =>
      ensureClient().scanWifi(),
    saveWifi: async (config: BleProvisionWifiConfig) => ensureClient().saveWifi(config),
    saveServer: async (config: BleProvisionServerConfig) => ensureClient().saveServer(config),
    createEmptyConfig: (): BleProvisionConfig => createEmptyProvisionConfig(),
  }
}
