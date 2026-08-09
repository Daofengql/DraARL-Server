import { apiClient } from '../../services/api'
import { logService } from '../../services'
import type { OperatorLog } from '../../types'
import { normalizeSiteConfigs, serializeSystemInfo } from './configNormalization'
import { buildOperatorLogParams } from './operatorLogQuery'
import type { OperatorLogQuery } from './operatorLogQuery'
import type {
  AccessDiscoveryConfig,
  APRSConfig,
  APRSLogEntry,
  BackendResponse,
  CommSettingsConfig,
  ConfigEntry,
  OpenAIConfig,
  RegistrationConfig,
  SiteConfigs,
  SMTPConfig,
  SystemInfoConfig,
} from './types'
import type { BrandResourceKind } from './brandResources'

export async function getSiteConfigs(): Promise<SiteConfigs> {
  const [icp, system, accessDiscovery, aprs, openai, commSettings, registration, smtp] = await Promise.all([
    apiClient.get<BackendResponse<ConfigEntry[]>>('/api/config/category/icp'),
    apiClient.get<BackendResponse<ConfigEntry[]>>('/api/config/category/system'),
    apiClient.get<BackendResponse<AccessDiscoveryConfig>>('/api/config/access-discovery'),
    apiClient.get<BackendResponse<APRSConfig>>('/api/config/aprs'),
    apiClient.get<BackendResponse<OpenAIConfig>>('/api/config/openai'),
    apiClient.get<BackendResponse<CommSettingsConfig>>('/api/config/comm-settings'),
    apiClient.get<BackendResponse<RegistrationConfig>>('/api/config/registration'),
    apiClient.get<BackendResponse<SMTPConfig>>('/api/config/smtp'),
  ])

  return normalizeSiteConfigs({ icp, system, accessDiscovery, aprs, openai, commSettings, registration, smtp })
}

export async function saveSystemInfo(config: SystemInfoConfig): Promise<void> {
  const payload = serializeSystemInfo(config)
  await apiClient.put('/api/config/system', payload.system)
  await apiClient.put('/api/config/icp', payload.icp)
}

export async function saveAccessDiscovery(config: AccessDiscoveryConfig): Promise<AccessDiscoveryConfig | undefined> {
  const response = await apiClient.put<BackendResponse<AccessDiscoveryConfig>>('/api/config/access-discovery', config)
  return response.code === 200 ? response.data : undefined
}

export function saveAPRS(config: APRSConfig) {
  return apiClient.put('/api/config/aprs', config)
}

export function saveOpenAI(config: OpenAIConfig) {
  return apiClient.put('/api/config/openai', config)
}

export function saveCommSettings(config: CommSettingsConfig) {
  return apiClient.put('/api/config/comm-settings', config)
}

export function saveRegistration(config: RegistrationConfig) {
  return apiClient.put('/api/config/registration', config)
}

export function saveSMTP(config: SMTPConfig) {
  return apiClient.put('/api/config/smtp', config)
}

export async function getAPRSLogs(): Promise<APRSLogEntry[]> {
  const response = await apiClient.get<BackendResponse<APRSLogEntry[]>>('/api/config/aprs/logs')
  return response.code === 200 && response.data ? response.data : []
}

export async function uploadBrandResource(kind: BrandResourceKind, file: File): Promise<boolean> {
  const formData = new FormData()
  formData.append('file', file)
  const response = await apiClient.post<BackendResponse<{ file_url: string }>>(`/api/upload/${kind}`, formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return response.code === 200 && Boolean(response.data?.file_url)
}

export function deleteBrandResource(kind: BrandResourceKind) {
  return apiClient.delete(`/api/config/${kind}`)
}

export async function getOperatorLogs(query: OperatorLogQuery): Promise<{ items: OperatorLog[]; total: number }> {
  const data = await logService.getList(buildOperatorLogParams(query))
  const items = data.items || data
  const normalizedItems = Array.isArray(items) ? items : []
  return {
    items: normalizedItems,
    total: data.total || normalizedItems.length,
  }
}
