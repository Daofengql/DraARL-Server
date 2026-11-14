const APP_VERSION =
  (typeof import.meta.env.VITE_APP_VERSION === 'string' ? import.meta.env.VITE_APP_VERSION.trim() : '') || 'dev'

const DOCS_URL =
  (typeof import.meta.env.VITE_DOCS_URL === 'string' ? import.meta.env.VITE_DOCS_URL.trim() : '') ||
  'https://daofengql.github.io/DraARL-Server/'

/**
 * 站点默认配置常量
 * 构建时统一的默认值，运行时可被数据库配置覆盖
 */
export const SITE_CONFIG = {
  /** 站点名称 */
  NAME: '麟链互联',
  /** 站点短名称/简称 */
  SHORT_NAME: 'DraARL',
  /** 系统版本 */
  VERSION: APP_VERSION,
  /** 协议版本 */
  PROTOCOL_VERSION: 'DraARLv1',
  /** 正式文档站地址；私有部署可通过 VITE_DOCS_URL 覆盖 */
  DOCS_URL,
} as const

/** 缓存键：站点配置 */
export const CACHE_KEY_SITE_CONFIG = 'draarl_site_config'

/**
 * 从 localStorage 获取缓存的站点名称
 * 用于解决 SPA 路由切换时的 title 闪烁问题
 */
export function getCachedSiteName(): string | null {
  try {
    const cached = localStorage.getItem(CACHE_KEY_SITE_CONFIG)
    if (cached) {
      const parsed = JSON.parse(cached)
      return parsed.name || null
    }
  } catch {
    // ignore
  }
  return null
}

/**
 * 缓存站点配置到 localStorage
 */
export function cacheSiteConfig(config: { name: string; shortName: string }): void {
  try {
    localStorage.setItem(CACHE_KEY_SITE_CONFIG, JSON.stringify(config))
  } catch {
    // ignore
  }
}
