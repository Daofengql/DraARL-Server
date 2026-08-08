import assert from 'node:assert/strict'
import test from 'node:test'

import { validateBrandResource } from '../src/pages/settings/brandResources.ts'
import { normalizeSiteConfigs, serializeSystemInfo } from '../src/pages/settings/configNormalization.ts'
import { getEventTypeLabel } from '../src/pages/settings/eventTypes.ts'
import { buildOperatorLogParams } from '../src/pages/settings/operatorLogQuery.ts'

test('normalizes missing config responses to stable defaults', () => {
  const configs = normalizeSiteConfigs({})
  assert.equal(configs.systemInfo.language, 'zh')
  assert.equal(configs.accessDiscovery.center.udp_port, 60050)
  assert.equal(configs.commSettings.retention_days, 30)
  assert.equal(configs.smtp.port, 465)
})

test('normalizes config entries and serializes the system payload without favicon', () => {
  const configs = normalizeSiteConfigs({
    system: { code: 200, data: [
      { key: 'system.name', value: 'DraARL' },
      { key: 'system.nameshorthand', value: 'DRA' },
      { key: 'system.logo_url', value: '/logo.png' },
      { key: 'system.favicon_url', value: '/favicon.ico' },
      { key: 'system.language', value: '' },
    ] },
    icp: { code: 200, data: [{ key: 'web.icp', value: '闽ICP备1号' }] },
  })
  assert.equal(configs.systemInfo.language, 'zh')
  assert.deepEqual(serializeSystemInfo(configs.systemInfo), {
    system: { name: 'DraARL', nameshorthand: 'DRA', logo_url: '/logo.png', language: 'zh' },
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
  assert.equal(validateBrandResource('logo', { size: 10, type: 'text/plain' }), '请选择图片文件')
  assert.equal(validateBrandResource('favicon', { size: 10, type: 'image/svg+xml' }), null)
  assert.equal(validateBrandResource('favicon', { size: 10, type: 'image/jpeg' }), '请选择 .ico, .png 或 .svg 格式的文件')
})
