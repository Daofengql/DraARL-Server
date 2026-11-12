export type BrandResourceKind = 'logo' | 'favicon'

const FAVICON_TYPES = ['image/x-icon', 'image/vnd.microsoft.icon', 'image/png']

export function validateBrandResource(kind: BrandResourceKind, file: Pick<File, 'size' | 'type'>): string | null {
  if (kind === 'logo') {
    if (file.size > 5 * 1024 * 1024) return 'Logo文件大小不能超过5MB'
    if (!['image/png', 'image/jpeg', 'image/gif'].includes(file.type)) return '请选择 PNG、JPG 或 GIF 图片'
    return null
  }

  if (file.size > 1024 * 1024) return 'Favicon文件大小不能超过1MB'
  if (!FAVICON_TYPES.includes(file.type)) return '请选择 .ico 或 .png 格式的文件'
  return null
}
