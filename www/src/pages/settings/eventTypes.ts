import type { ChipProps } from '@mui/material'

export const EVENT_TYPES = [
  { value: '', label: '全部' },
  { value: 'login', label: '登录' },
  { value: 'logout', label: '登出' },
  { value: 'admin_switch_login', label: '管理员切换登录' },
  { value: 'login_failed', label: '登录失败' },
  { value: 'register', label: '注册' },
  { value: 'user_create', label: '创建用户' },
  { value: 'user_update', label: '更新用户' },
  { value: 'user_delete', label: '删除用户' },
  { value: 'user_status', label: '用户状态变更' },
  { value: 'user_approval', label: '用户审批' },
  { value: 'password_reset', label: '重置密码' },
  { value: 'password_change', label: '修改密码' },
  { value: 'profile_update', label: '更新个人资料' },
  { value: 'group_create', label: '创建群组' },
  { value: 'group_update', label: '更新群组' },
  { value: 'group_delete', label: '删除群组' },
  { value: 'group_join', label: '加入群组' },
  { value: 'group_leave', label: '离开群组' },
  { value: 'group_device_status', label: '群组设备状态' },
  { value: 'device_kick', label: '踢出设备' },
  { value: 'virtual_group_create', label: '创建虚拟互联组' },
  { value: 'virtual_group_update', label: '更新虚拟互联组' },
  { value: 'virtual_group_delete', label: '删除虚拟互联组' },
  { value: 'group_link_add', label: '添加群组互联' },
  { value: 'group_link_remove', label: '移除群组互联' },
  { value: 'asset_create', label: '创建资源' },
  { value: 'asset_upload', label: '上传资源' },
  { value: 'asset_update', label: '更新资源' },
  { value: 'asset_delete', label: '删除资源' },
  { value: 'config_update', label: '配置更新' },
  { value: 'comm_settings_update', label: '通信配置更新' },
  { value: 'comm_record_delete', label: '删除通信记录' },
  { value: 'cache_clear_all', label: '清空缓存' },
  { value: 'cache_metrics_reset', label: '重置缓存指标' },
  { value: 'system', label: '系统' },
] as const

export const EVENT_TYPE_COLORS: Record<string, ChipProps['color']> = {
  login: 'info', logout: 'default', admin_switch_login: 'warning', login_failed: 'error',
  register: 'success', user_create: 'success', user_update: 'warning', user_delete: 'error',
  user_status: 'secondary', user_approval: 'primary', password_reset: 'error',
  password_change: 'warning', profile_update: 'info', group_create: 'success',
  group_update: 'warning', group_delete: 'error', group_join: 'info', group_leave: 'default',
  group_device_status: 'secondary', device_kick: 'warning', virtual_group_create: 'success',
  virtual_group_update: 'warning', virtual_group_delete: 'error', group_link_add: 'info',
  group_link_remove: 'warning', asset_create: 'success', asset_upload: 'success',
  asset_update: 'warning', asset_delete: 'error', config_update: 'secondary',
  comm_settings_update: 'secondary', comm_record_delete: 'warning', cache_clear_all: 'warning',
  cache_metrics_reset: 'info', system: 'primary',
}

const EVENT_TYPE_LABELS: Record<string, string> = {
  login: '登录', logout: '登出', admin_switch_login: '管理员切换登录', login_failed: '登录失败',
  register: '注册', user_create: '创建用户', user_update: '更新用户', user_delete: '删除用户',
  user_status: '状态变更', user_approval: '用户审批', password_reset: '重置密码',
  password_change: '修改密码', profile_update: '更新资料', group_create: '创建群组',
  group_update: '更新群组', group_delete: '删除群组', group_join: '加入群组',
  group_leave: '离开群组', group_device_status: '设备状态', device_kick: '踢出设备',
  virtual_group_create: '创建互联组', virtual_group_update: '更新互联组',
  virtual_group_delete: '删除互联组', group_link_add: '添加互联', group_link_remove: '移除互联',
  asset_create: '创建资源', asset_upload: '上传资源', asset_update: '更新资源',
  asset_delete: '删除资源', config_update: '配置更新', comm_settings_update: '通信配置',
  comm_record_delete: '删除记录', cache_clear_all: '清空缓存', cache_metrics_reset: '重置指标',
  system: '系统',
}

export function getEventTypeLabel(eventType: string): string {
  return EVENT_TYPE_LABELS[eventType] || eventType
}

export function formatOperatorTimestamp(timestamp: string): string {
  return new Date(timestamp).toLocaleString('zh-CN')
}
