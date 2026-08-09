import { useRef } from 'react'
import { Box, IconButton, Typography } from '@mui/material'
import CloudUpload from '@mui/icons-material/CloudUpload'
import Delete from '@mui/icons-material/Delete'
import { deleteBrandResource, uploadBrandResource } from './api'
import { validateBrandResource } from './brandResources'
import type { BrandResourceKind } from './brandResources'
import type { SiteMessage } from './types'

interface BrandResourceControlProps {
  kind: BrandResourceKind
  value: string
  onChanged: () => Promise<void>
  onPreviewError: () => void
  showMessage: (type: SiteMessage['type'], text: string) => void
}

const COPY = {
  logo: {
    label: '站点Logo', alt: 'Logo预览', prompt: '点击上传Logo图片',
    caption: '支持PNG、JPG、GIF格式，最大5MB', accept: 'image/*', maxWidth: '100%', maxHeight: 150,
  },
  favicon: {
    label: '站点Favicon', alt: 'Favicon预览', prompt: '点击上传Favicon',
    caption: '支持 .ico, .png, .svg 格式，最大1MB',
    accept: '.ico,.png,.svg,image/x-icon,image/png,image/svg+xml', maxWidth: 64, maxHeight: 64,
  },
} as const

export function BrandResourceControl({
  kind,
  value,
  onChanged,
  onPreviewError,
  showMessage,
}: BrandResourceControlProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const copy = COPY[kind]
  const displayName = kind === 'logo' ? 'Logo' : 'Favicon'

  const handleFileChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    const validationError = validateBrandResource(kind, file)
    if (validationError) {
      showMessage('error', validationError)
      event.target.value = ''
      return
    }

    try {
      if (await uploadBrandResource(kind, file)) {
        await onChanged()
        showMessage('success', `${displayName}上传成功`)
        window.dispatchEvent(new CustomEvent('config-updated'))
      }
    } catch (error) {
      console.error(`Failed to upload ${kind}:`, error)
      showMessage('error', `${displayName}上传失败`)
    } finally {
      if (inputRef.current) inputRef.current.value = ''
    }
  }

  const handleDelete = async () => {
    try {
      await deleteBrandResource(kind)
      await onChanged()
      showMessage('success', `${displayName}删除成功`)
      window.dispatchEvent(new CustomEvent('config-updated'))
    } catch (error) {
      console.error(`Failed to delete ${kind}:`, error)
      showMessage('error', `${displayName}删除失败`)
    }
  }

  return (
    <Box>
      <Typography variant="subtitle2" sx={{ mb: 1, color: 'text.secondary' }}>
        {copy.label}
      </Typography>
      <Box
        sx={{
          border: '1px dashed', borderColor: 'divider', borderRadius: 2, p: 2,
          textAlign: 'center', bgcolor: 'background.paper', cursor: 'pointer',
          '&:hover': { bgcolor: 'action.hover' },
        }}
        onClick={() => inputRef.current?.click()}
      >
        <input
          ref={inputRef}
          type="file"
          accept={copy.accept}
          onChange={handleFileChange}
          style={{ display: 'none' }}
        />
        {value ? (
          <Box sx={{ position: 'relative', display: 'inline-block' }}>
            <Box
              component="img"
              src={value}
              alt={copy.alt}
              sx={{ maxWidth: copy.maxWidth, maxHeight: copy.maxHeight, objectFit: 'contain' }}
              onError={(event) => {
                event.currentTarget.src = ''
                onPreviewError()
              }}
            />
            <IconButton
              size="small"
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
            <CloudUpload sx={{ fontSize: kind === 'logo' ? 48 : 32, color: 'text.secondary', mb: 1 }} />
            <Typography variant="body2" color="text.secondary">{copy.prompt}</Typography>
            <Typography variant="caption" color="text.disabled">{copy.caption}</Typography>
          </Box>
        )}
      </Box>
    </Box>
  )
}
