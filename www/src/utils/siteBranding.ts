interface SiteBranding {
  logo_url?: string
  logo_dark_url?: string
}

export function selectSiteLogo(info: SiteBranding, mode: 'light' | 'dark'): string {
  return (mode === 'dark' ? info.logo_dark_url || info.logo_url : info.logo_url || info.logo_dark_url) || ''
}

export function brandPreviewURL(value: string, kind: 'logo' | 'favicon', theme: 'light' | 'dark' = 'light'): string {
  if (!value) return ''
  let revision = 2166136261
  for (let index = 0; index < value.length; index++) revision = Math.imul(revision ^ value.charCodeAt(index), 16777619)
  const route = kind === 'favicon' ? '/favicon.ico' : theme === 'dark' ? '/logo-dark.png' : '/logo.png'
  return `${route}?v=${(revision >>> 0).toString(16)}`
}
