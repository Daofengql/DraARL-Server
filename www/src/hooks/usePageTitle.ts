import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'
import { usePublicConfig } from './usePublicConfig'

// 路由到标题后缀的映射
const routeTitleMap: Record<string, string> = {
  '/login': '登录',
  '/register': '注册',
  '/dashboard': '仪表盘',
  '/devices': '我的设备',
  '/groups': '我的群组',
  '/profile': '个人中心',
  '/comm-records': '通信记录',
  '/docs': '技术支持',
  '/admin/dashboard': '仪表盘',
  '/admin/users': '用户管理',
  '/admin/approvals': '用户审批',
  '/admin/certificate-approvals': '操作证审批',
  '/admin/devices': '设备管理',
  '/admin/relays': '中继台',
  '/admin/servers': '服务器',
  '/admin/groups': '群组管理',
  '/admin/group-links': '互联管理',
  '/admin/comm-records': '通信记录',
  '/admin/assets': '资源管理',
  '/admin/settings': '站点配置',
}

/**
 * 根据当前路由自动更新页面标题
 * 在 Layout 组件中使用此 hook，确保 SPA 路由切换时标题同步更新
 */
export function usePageTitle() {
  const location = useLocation()
  const { config } = usePublicConfig()

  useEffect(() => {
    const path = location.pathname
    const siteName = config.systemInfo.name || '麟云链路'

    // 查找匹配的标题后缀
    let titleSuffix = ''

    // 先尝试精确匹配
    if (routeTitleMap[path]) {
      titleSuffix = routeTitleMap[path]
    } else {
      // 尝试前缀匹配（用于动态路由等）
      const matchedKey = Object.keys(routeTitleMap).find(key =>
        path.startsWith(key + '/') || path === key
      )
      if (matchedKey) {
        titleSuffix = routeTitleMap[matchedKey]
      } else if (path.startsWith('/admin/')) {
        // 管理后台的默认标题
        titleSuffix = '管理后台'
      }
    }

    // 设置标题
    document.title = titleSuffix ? `${siteName} - ${titleSuffix}` : siteName
  }, [location.pathname, config.systemInfo.name])
}
