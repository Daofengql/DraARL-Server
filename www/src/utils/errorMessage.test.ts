import { expect, it } from 'vitest'
import { getErrorMessage, getRetryAfterSeconds } from './errorMessage'

it.each([null, undefined, 'failure', {}, { response: { status: 403 } }])('does not delay unrelated errors %j', error => {
  expect(getRetryAfterSeconds(error)).toBe(0)
})

it.each([
  [{ status: 429 }, 5],
  [{ status: 429, headers: { 'retry-after': '2.1' } }, 3],
  [{ status: 429, data: { data: { retry_after: 17 } } }, 17],
  [{ status: 429, headers: { 'retry-after': 'invalid' } }, 5],
  [{ status: 429, headers: { 'retry-after': '-2' } }, 1],
  [{ status: 429, headers: { 'retry-after': '99999' } }, 3600],
])('uses the server cooldown with safe bounds %j', (response, seconds) => {
  expect(getRetryAfterSeconds({ response })).toBe(seconds)
})

it.each([null, undefined, 'failure', {}, { message: ' ' }, { response: { data: { message: 123 } } }])('uses fallback for invalid errors %j', (error) => {
  expect(getErrorMessage(error, '操作失败')).toBe('操作失败')
})
it.each(['SQLSTATE: 23000', 'foreign key constraint fails', 'panic: secret', 'dial tcp 127.0.0.1', 'Error 1452 (23000)'])('hides diagnostics %s', (message) => {
  expect(getErrorMessage({ message }, '操作失败')).toBe('操作失败')
})
it('provides specific actionable network feedback', () => {
  for (const code of ['ECONNABORTED', 'ETIMEDOUT']) expect(getErrorMessage({ code }, '')).toContain('超时')
  expect(getErrorMessage({ code: 'ERR_NETWORK' }, '')).toContain('网络')
  expect(getErrorMessage({ response: { status: 429 } }, '')).toContain('频繁')
  expect(getErrorMessage({ response: { status: 500, data: { message: 'private' } } }, '操作失败')).toBe('操作失败')
  expect(getErrorMessage({ response: { status: 403, data: { message: '没有权限' } } }, '')).toBe('没有权限')
  expect(getErrorMessage(new Error('请选择设备'), '')).toBe('请选择设备')
})
