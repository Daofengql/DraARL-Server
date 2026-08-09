import type { Group } from '../types'

/**
 * 检查是否可以编辑群组
 */
export const canEditGroup = (
  group: Group,
  currentUserId: number | undefined,
  isAdmin: boolean
): boolean => {
  if (isAdmin) return true
  if (!currentUserId) return false
  return group.ower_id === currentUserId
}

/**
 * 检查是否可以删除群组
 */
export const canDeleteGroup = (
  group: Group,
  currentUserId: number | undefined,
  isAdmin: boolean
): boolean => {
  return canEditGroup(group, currentUserId, isAdmin)
}

/**
 * 检查是否可以踢出设备
 */
export const canKickDevice = (
  group: Group,
  currentUserId: number | undefined,
  isAdmin: boolean
): boolean => {
  return canEditGroup(group, currentUserId, isAdmin)
}

/**
 * 检查是否可以设置设备禁发/禁收
 */
export const canSetDeviceStatus = (
  group: Group,
  currentUserId: number | undefined,
  isAdmin: boolean
): boolean => {
  return canEditGroup(group, currentUserId, isAdmin)
}

/**
 * 检查是否可以查看群组设备
 */
export const canViewDevices = (group: Group): boolean => {
  // 公开群组所有人可查看，私有群组需要已加入
  return group.type === 1 || group.is_joined === true
}

/**
 * 检查是否可以离开群组
 */
export const canLeaveGroup = (
  group: Group,
  currentUserId: number | undefined
): boolean => {
  // 只有私有群组且已加入的可以离开
  // 但群组创建者不能离开自己的群组
  if (group.type !== 2 || group.is_joined !== true) {
    return false
  }
  // 如果是群组创建者，不允许离开
  if (currentUserId && group.ower_id === currentUserId) {
    return false
  }
  return true
}

/**
 * 检查是否可以直接加入群组（无需密码）
 */
export const canJoinDirectly = (group: Group): boolean => {
  // 公开群组或已验证的私有群组
  return group.type === 1 || group.is_joined === true
}

/**
 * 获取群组类型标签文本
 */
export const getGroupTypeLabel = (type: number): string => {
  switch (type) {
    case 1:
      return '公开'
    case 2:
      return '私有'
    default:
      return '未知'
  }
}

/**
 * 获取群组类型图标
 */
export const getGroupTypeIcon = (type: number): 'lock-open' | 'lock' => {
  return type === 1 ? 'lock-open' : 'lock'
}

/**
 * 检查群组是否需要密码验证
 */
export const requirePassword = (group: Group): boolean => {
  // 私有群组且未验证需要密码
  return group.type === 2 && !group.is_joined
}
