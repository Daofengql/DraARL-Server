import { createDefaultSiteConfigs } from './types.ts'
import type {
  AccessDiscoveryConfig,
  APRSConfig,
  BackendResponse,
  CommSettingsConfig,
  ConfigEntry,
  OpenAIConfig,
  RegistrationConfig,
  SiteConfigs,
  SMTPConfig,
  SystemInfoConfig,
} from './types'

export interface RawSiteConfigResponses {
  icp: BackendResponse<ConfigEntry[]>
  system: BackendResponse<ConfigEntry[]>
  accessDiscovery: BackendResponse<AccessDiscoveryConfig>
  aprs: BackendResponse<APRSConfig>
  openai: BackendResponse<OpenAIConfig>
  commSettings: BackendResponse<CommSettingsConfig>
  registration: BackendResponse<RegistrationConfig>
  smtp: BackendResponse<SMTPConfig>
}

function configValue(entries: ConfigEntry[] | undefined, key: string, fallback = ''): string {
  return entries?.find((entry) => entry.key === key)?.value || fallback
}

export function normalizeSiteConfigs(responses: Partial<RawSiteConfigResponses>): SiteConfigs {
  const configs = createDefaultSiteConfigs()
  const systemEntries = responses.system?.code === 200 ? responses.system.data : undefined
  const icpEntries = responses.icp?.code === 200 ? responses.icp.data : undefined

  if (systemEntries?.length) {
    configs.systemInfo = {
      name: configValue(systemEntries, 'system.name'),
      nameshorthand: configValue(systemEntries, 'system.nameshorthand'),
      logo_url: configValue(systemEntries, 'system.logo_url'),
      favicon_url: configValue(systemEntries, 'system.favicon_url'),
      language: configValue(systemEntries, 'system.language', 'zh'),
      icp: configValue(icpEntries, 'web.icp'),
    }
  }

  if (responses.accessDiscovery?.code === 200 && responses.accessDiscovery.data) {
    configs.accessDiscovery = responses.accessDiscovery.data
  }
  if (responses.aprs?.code === 200 && responses.aprs.data) configs.aprs = responses.aprs.data
  if (responses.openai?.code === 200 && responses.openai.data) configs.openai = responses.openai.data
  if (responses.commSettings?.code === 200 && responses.commSettings.data) {
    configs.commSettings = responses.commSettings.data
  }
  if (responses.registration?.code === 200 && responses.registration.data) {
    configs.registration = responses.registration.data
  }
  if (responses.smtp?.code === 200 && responses.smtp.data) configs.smtp = responses.smtp.data

  return configs
}

export function serializeSystemInfo(config: SystemInfoConfig) {
  return {
    system: {
      name: config.name,
      nameshorthand: config.nameshorthand,
      logo_url: config.logo_url,
      language: config.language,
    },
    icp: { icp: config.icp },
  }
}
