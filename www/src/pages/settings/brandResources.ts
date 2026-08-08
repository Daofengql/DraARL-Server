export type BrandResourceKind = 'logo' | 'favicon'

const FAVICON_TYPES = ['image/x-icon', 'image/vnd.microsoft.icon', 'image/png', 'image/svg+xml']

export function validateBrandResource(kind: BrandResourceKind, file: Pick<File, 'size' | 'type'>): string | null {
  if (kind === 'logo') {
    if (file.size > 5 * 1024 * 1024) return 'Logo文件大小不能超过5MB'
    if (!file.type.startsWith('image/')) return '请选择图片文件'
    return null
  }

  if (file.size > 1024 * 1024) return 'Favicon文件大小不能超过1MB'
  if (!FAVICON_TYPES.includes(file.type)) return '请选择 .ico, .png 或 .svg 格式的文件'
  return null
}
