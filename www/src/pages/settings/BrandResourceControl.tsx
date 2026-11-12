import { useRef, useState } from 'react'
import { Box, Button, IconButton, Typography } from '@mui/material'
import CloudUpload from '@mui/icons-material/CloudUpload'
import Delete from '@mui/icons-material/Delete'
import { deleteBrandResource, uploadBrandResource } from './api'
import { validateBrandResource } from './brandResources'
import type { BrandResourceKind } from './brandResources'
import type { SiteMessage } from './types'
import { brandPreviewURL } from '../../utils/siteBranding'

interface BrandResourceControlProps {
  kind: BrandResourceKind
  value: string
  theme?: 'light' | 'dark'
  onChanged: () => Promise<void>
  showMessage: (type: SiteMessage['type'], text: string) => void
}

const COPY = {
  logo: {
    label: '站点Logo', alt: 'Logo预览', prompt: '点击上传Logo图片',
    caption: '支持PNG、JPG、GIF格式，最大5MB', accept: 'image/*', maxWidth: '100%', maxHeight: 150,
  },
  favicon: {
    label: '站点Favicon', alt: 'Favicon预览', prompt: '点击上传Favicon',
    caption: '支持 ICO、PNG 格式，最大1MB',
    accept: '.ico,.png,image/x-icon,image/png', maxWidth: 64, maxHeight: 64,
  },
} as const

export function BrandResourceControl({
  kind,
  value,
  theme = 'light',
  onChanged,
  showMessage,
}: BrandResourceControlProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [failedURL, setFailedURL] = useState('')
  const [retry, setRetry] = useState(0)
  const copy = COPY[kind]
  const displayName = kind === 'logo' ? `${theme === 'dark' ? '暗色' : '亮色'}主题 Logo` : 'Favicon'
  const previewURL = value ? `${brandPreviewURL(value, kind, theme)}&retry=${retry}` : ''
  const previewTextColor = kind === 'logo' ? (theme === 'dark' ? '#b8c9da' : '#526477') : 'text.secondary'

  const handleFileChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file || busy) return

    const validationError = validateBrandResource(kind, file)
    if (validationError) {
      showMessage('error', validationError)
      event.target.value = ''
      return
    }

    try {
      setBusy(true)
      if (await uploadBrandResource(kind, file, theme)) {
        await onChanged()
        showMessage('success', `${displayName}上传成功`)
        window.dispatchEvent(new CustomEvent('config-updated'))
      }
    } catch (error) {
      console.error(`Failed to upload ${kind}:`, error)
      showMessage('error', `${displayName}上传失败`)
    } finally {
      setBusy(false)
      if (inputRef.current) inputRef.current.value = ''
    }
  }

  const handleDelete = async () => {
    try {
      setBusy(true)
      await deleteBrandResource(kind, theme)
      await onChanged()
      showMessage('success', `${displayName}删除成功`)
      window.dispatchEvent(new CustomEvent('config-updated'))
    } catch (error) {
      console.error(`Failed to delete ${kind}:`, error)
      showMessage('error', `${displayName}删除失败`)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Box>
      <Typography component="h3" variant="subtitle2" sx={{ mb: 1, color: 'text.secondary' }}>
        {kind === 'logo' ? displayName : copy.label}
      </Typography>
      <Box
        sx={{
          border: '1px dashed', borderColor: 'divider', borderRadius: 2, p: 2,
          textAlign: 'center', bgcolor: kind === 'logo' ? (theme === 'dark' ? '#0b1724' : '#f5f8fc') : 'background.paper', cursor: busy ? 'wait' : 'pointer',
          '&:hover': { borderColor: 'primary.main' },
        }}
        onClick={() => { if (!busy) inputRef.current?.click() }}
      >
        <input
          ref={inputRef}
          type="file"
          accept={copy.accept}
          disabled={busy}
          aria-label={`上传${displayName}`}
          onChange={handleFileChange}
          style={{ display: 'none' }}
        />
        {value ? (
          <Box sx={{ position: 'relative', display: 'inline-block' }}>
            {failedURL === previewURL ? (
              <Box>
                <Typography color="error" variant="body2">预览加载失败，已保存的图片仍保留</Typography>
                <Button size="small" onClick={(event) => { event.stopPropagation(); setRetry((current) => current + 1) }}>重试预览</Button>
              </Box>
            ) : <Box
              component="img"
              src={previewURL}
              alt={copy.alt}
              sx={{ maxWidth: copy.maxWidth, maxHeight: copy.maxHeight, objectFit: 'contain' }}
              onError={() => setFailedURL(previewURL)}
            />}
            <IconButton
              size="small"
              disabled={busy}
              aria-label={`删除${displayName}`}
              sx={{
                position: 'absolute', top: -8, right: -8, bgcolor: 'background.paper',
                '&:hover': { bgcolor: 'error.light' },
              }}
              onClick={(event) => {
                event.stopPropagation()
                void handleDelete()
              }}
            >
              <Delete fontSize="small" color="error" />
            </IconButton>
          </Box>
        ) : (
          <Box sx={{ py: kind === 'logo' ? 3 : 2 }}>
            <CloudUpload sx={{ fontSize: kind === 'logo' ? 48 : 32, color: previewTextColor, mb: 1 }} />
            <Typography variant="body2" color={previewTextColor}>{busy ? '正在保存...' : copy.prompt}</Typography>
            <Typography variant="caption" color={previewTextColor}>{copy.caption}</Typography>
            {kind === 'logo' && theme === 'dark' && <Typography variant="caption" color={previewTextColor} display="block">未设置时使用亮色主题 Logo</Typography>}
          </Box>
        )}
      </Box>
    </Box>
  )
}
