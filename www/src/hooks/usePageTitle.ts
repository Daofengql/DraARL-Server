import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router-dom'
import { usePublicConfig } from './usePublicConfig'
import { SITE_CONFIG, getCachedSiteName, cacheSiteConfig } from '../config/site'

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
 *
 * 使用 localStorage 缓存站点名称，解决移动端路由切换时的 title 闪烁问题
 */
export function usePageTitle() {
  const location = useLocation()
  const { config } = usePublicConfig()
  const prevTitleRef = useRef<string>('')

  useEffect(() => {
    const path = location.pathname

    // 优先使用缓存的站点名称，避免闪烁
    const cachedName = getCachedSiteName()
    const siteName = config.systemInfo.name || cachedName || SITE_CONFIG.NAME

    // 如果配置已加载且与缓存不同，更新缓存
    if (config.systemInfo.name && config.systemInfo.name !== cachedName) {
      cacheSiteConfig({
        name: config.systemInfo.name,
        shortName: config.systemInfo.nameshorthand || SITE_CONFIG.SHORT_NAME,
      })
    }

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
    const newTitle = titleSuffix ? `${siteName} - ${titleSuffix}` : siteName

    // 只有标题真正改变时才更新，避免不必要的 DOM 操作
    if (prevTitleRef.current !== newTitle) {
      document.title = newTitle
      prevTitleRef.current = newTitle
    }
  }, [location.pathname, config.systemInfo.name, config.systemInfo.nameshorthand])
}
