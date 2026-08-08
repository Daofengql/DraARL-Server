import { useCallback, useEffect, useRef, useState } from 'react'
import {
  getSiteConfigs,
  saveAccessDiscovery,
  saveAPRS,
  saveCommSettings,
  saveOpenAI,
  saveRegistration,
  saveSMTP,
  saveSystemInfo,
} from './api'
import { createDefaultSiteConfigs } from './types'
import type { SiteMessage } from './types'
import { validateAccessDiscovery, validateAPRS } from './validation'

export function useSiteConfig() {
  const defaults = createDefaultSiteConfigs()
  const [systemInfo, setSystemInfo] = useState(defaults.systemInfo)
  const [accessDiscovery, setAccessDiscovery] = useState(defaults.accessDiscovery)
  const [aprs, setAPRS] = useState(defaults.aprs)
  const [openai, setOpenAI] = useState(defaults.openai)
  const [commSettings, setCommSettings] = useState(defaults.commSettings)
  const [registration, setRegistration] = useState(defaults.registration)
  const [smtp, setSMTP] = useState(defaults.smtp)
  const [message, setMessage] = useState<SiteMessage | null>(null)
  const [loading, setLoading] = useState(false)
  const messageTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const showMessage = useCallback((type: SiteMessage['type'], text: string) => {
    setMessage({ type, text })
    if (messageTimer.current) clearTimeout(messageTimer.current)
    messageTimer.current = setTimeout(() => setMessage(null), 3000)
  }, [])

  useEffect(() => () => {
    if (messageTimer.current) clearTimeout(messageTimer.current)
  }, [])

  const loadConfigs = useCallback(async () => {
    try {
      const configs = await getSiteConfigs()
      setSystemInfo(configs.systemInfo)
      setAccessDiscovery(configs.accessDiscovery)
      setAPRS(configs.aprs)
      setOpenAI(configs.openai)
      setCommSettings(configs.commSettings)
      setRegistration(configs.registration)
      setSMTP(configs.smtp)
    } catch (error) {
      console.error('Failed to load configs:', error)
      showMessage('error', '加载配置失败')
    }
  }, [showMessage])

  useEffect(() => {
    void loadConfigs()
  }, [loadConfigs])

  const runSave = useCallback(async (
    action: () => Promise<unknown>,
    successText: string,
    errorText: string,
    onSuccess?: () => void,
  ) => {
    setLoading(true)
    try {
      await action()
      showMessage('success', successText)
      onSuccess?.()
    } catch {
      showMessage('error', errorText)
    } finally {
      setLoading(false)
    }
  }, [showMessage])

  const handleSaveSystemInfo = () => runSave(
    () => saveSystemInfo(systemInfo),
    '系统信息保存成功',
    '保存系统信息失败',
    () => window.dispatchEvent(new CustomEvent('config-updated')),
  )

  const handleSaveAccessDiscovery = async () => {
    const error = validateAccessDiscovery(accessDiscovery)
    if (error) return showMessage('error', error)
    await runSave(async () => {
      const saved = await saveAccessDiscovery(accessDiscovery)
      if (saved) setAccessDiscovery(saved)
    }, '接入点配置保存成功', '保存接入点配置失败')
  }

  const handleSaveAPRS = async () => {
    const error = validateAPRS(aprs)
    if (error) return showMessage('error', error)
    await runSave(() => saveAPRS(aprs), 'APRS配置保存成功', '保存APRS配置失败')
  }

  const handleSaveOpenAI = () => runSave(() => saveOpenAI(openai), 'OpenAI配置保存成功', '保存OpenAI配置失败')
  const handleSaveCommSettings = () => runSave(
    () => saveCommSettings(commSettings), '通信设置保存成功', '保存通信设置失败',
  )
  const handleSaveRegistration = () => runSave(
    () => saveRegistration(registration),
    '注册设置保存成功',
    '保存注册设置失败',
    () => window.dispatchEvent(new CustomEvent('config-updated')),
  )
  const handleSaveSMTP = () => runSave(() => saveSMTP(smtp), 'SMTP配置保存成功', '保存SMTP配置失败')

  return {
    systemInfo, setSystemInfo,
    accessDiscovery, setAccessDiscovery,
    aprs, setAPRS,
    openai, setOpenAI,
    commSettings, setCommSettings,
    registration, setRegistration,
    smtp, setSMTP,
    message, setMessage, showMessage,
    loading, loadConfigs,
    handleSaveSystemInfo, handleSaveAccessDiscovery, handleSaveAPRS, handleSaveOpenAI,
    handleSaveCommSettings, handleSaveRegistration, handleSaveSMTP,
  }
}
