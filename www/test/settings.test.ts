import assert from 'node:assert/strict'
import test from 'node:test'

import { validateBrandResource } from '../src/pages/settings/brandResources.ts'
import { normalizeSiteConfigs, serializeSystemInfo } from '../src/pages/settings/configNormalization.ts'
import { getEventTypeLabel } from '../src/pages/settings/eventTypes.ts'
import { buildOperatorLogParams } from '../src/pages/settings/operatorLogQuery.ts'
import { validateCommSettings } from '../src/pages/settings/validation.ts'
import { createDefaultSiteConfigs } from '../src/pages/settings/types.ts'
import { brandPreviewURL, selectSiteLogo } from '../src/utils/siteBranding.ts'

test('normalizes missing config responses to stable defaults', () => {
  const configs = normalizeSiteConfigs({})
  assert.equal(configs.systemInfo.language, 'zh')
  assert.equal(configs.accessDiscovery.udp_port, 60050)
  assert.equal(configs.commSettings.retention_days, 30)
  assert.equal(configs.smtp.port, 465)
})

test('preserves both theme logos and favicon when saving system information', () => {
  const configs = normalizeSiteConfigs({
    system: { code: 200, data: [
      { key: 'system.name', value: 'DraARL' },
      { key: 'system.nameshorthand', value: 'DRA' },
      { key: 'system.logo_url', value: '/logo.png' },
      { key: 'system.logo_dark_url', value: 'uploads/logo/dark.png' },
      { key: 'system.favicon_url', value: '/favicon.ico' },
      { key: 'system.language', value: '' },
    ] },
    icp: { code: 200, data: [{ key: 'web.icp', value: '闽ICP备1号' }] },
  })
  assert.equal(configs.systemInfo.language, 'zh')
  assert.deepEqual(serializeSystemInfo(configs.systemInfo), {
    system: { name: 'DraARL', nameshorthand: 'DRA', logo_url: '/logo.png', logo_dark_url: 'uploads/logo/dark.png', favicon_url: '/favicon.ico', language: 'zh' },
    icp: { icp: '闽ICP备1号' },
  })
})

test('serializes operator log page and event filters', () => {
  assert.deepEqual(buildOperatorLogParams({ page: 2, pageSize: 25, eventType: 'login' }), {
    page: 3,
    page_size: 25,
    event_type: 'login',
  })
  assert.deepEqual(buildOperatorLogParams({ page: 0, pageSize: 10, eventType: '' }), {
    page: 1,
    page_size: 10,
    event_type: undefined,
  })
  assert.equal(getEventTypeLabel('user_delete'), '删除用户')
  assert.equal(getEventTypeLabel('future_event'), 'future_event')
})

test('validates brand resource size and media type by resource kind', () => {
  assert.equal(validateBrandResource('logo', { size: 5 * 1024 * 1024, type: 'image/jpeg' }), null)
  assert.equal(validateBrandResource('logo', { size: 5 * 1024 * 1024 + 1, type: 'image/jpeg' }), 'Logo文件大小不能超过5MB')
  assert.equal(validateBrandResource('logo', { size: 10, type: 'text/plain' }), '请选择 PNG、JPG 或 GIF 图片')
  assert.equal(validateBrandResource('favicon', { size: 10, type: 'image/png' }), null)
  assert.equal(validateBrandResource('favicon', { size: 10, type: 'image/svg+xml' }), '请选择 .ico 或 .png 格式的文件')
})

test('selects the current theme logo and falls back when one is unset', () => {
  const info = { logo_url: '/logo.png?v=1', logo_dark_url: '/logo-dark.png?v=2' }
  assert.equal(selectSiteLogo(info, 'light'), info.logo_url)
  assert.equal(selectSiteLogo(info, 'dark'), info.logo_dark_url)
  assert.equal(selectSiteLogo({ logo_url: info.logo_url }, 'dark'), info.logo_url)
  assert.equal(selectSiteLogo({ logo_dark_url: info.logo_dark_url }, 'light'), info.logo_dark_url)
  assert.match(brandPreviewURL('uploads/logo/current.png', 'logo'), /^\/logo.png\?v=/)
  assert.match(brandPreviewURL('uploads/logo/current.png', 'logo', 'dark'), /^\/logo-dark.png\?v=/)
  assert.match(brandPreviewURL('uploads/favicon/current.ico', 'favicon'), /^\/favicon.ico\?v=/)
  assert.notEqual(brandPreviewURL('old', 'logo'), brandPreviewURL('new', 'logo'))
})

test('validates recording thresholds, zero limits and integer values', () => {
  const config = createDefaultSiteConfigs().commSettings
  assert.equal(validateCommSettings(config), null)
  assert.equal(validateCommSettings({ ...config, min_duration_ms: 1001, max_duration_sec: 1 }), '最小录制阈值不能超过最大录制时长')
  assert.equal(validateCommSettings({ ...config, min_duration_ms: 1000, max_duration_sec: 1 }), null)
  assert.equal(validateCommSettings({ ...config, min_duration_ms: 0, max_duration_sec: 0 }), null)
  assert.equal(validateCommSettings({ ...config, min_duration_ms: 1000000, max_duration_sec: 0 }), null)
  assert.ok(validateCommSettings({ ...config, min_duration_ms: -1 }))
  assert.ok(validateCommSettings({ ...config, min_duration_ms: 0.5 }))
  assert.ok(validateCommSettings({ ...config, batch_upload_sec: 0 }))
})
