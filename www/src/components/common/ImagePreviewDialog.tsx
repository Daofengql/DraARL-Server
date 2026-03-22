import { useState } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  IconButton,
  Button,
  Box,
  Typography,
} from '@mui/material'
import Close from '@mui/icons-material/Close'

interface ImagePreviewDialogProps {
  open: boolean
  onClose: () => void
  imageUrl: string | null
  title?: string
}

export function ImagePreviewDialog({
  open,
  onClose,
  imageUrl,
  title = '图片预览',
}: ImagePreviewDialogProps) {
  const [scale, setScale] = useState(1)

  const handleWheel = (e: React.WheelEvent) => {
    e.preventDefault()
    const delta = e.deltaY > 0 ? -0.1 : 0.1
    setScale((prev) => Math.max(0.1, Math.min(5, prev + delta)))
  }

  const handleReset = () => setScale(1)
  const handleZoomIn = () => setScale((prev) => Math.min(5, prev + 0.2))
  const handleZoomOut = () => setScale((prev) => Math.max(0.1, prev - 0.2))

  const handleClose = () => {
    setScale(1)
    onClose()
  }

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      maxWidth="lg"
      fullWidth
      PaperProps={{
        sx: { bgcolor: 'rgba(0, 0, 0, 0.9)' },
      }}
    >
      <DialogTitle
        sx={{
          color: 'white',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
        }}
      >
        <Typography>{title}</Typography>
        <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
          <Typography variant="caption" sx={{ color: 'grey.400' }}>
            滚轮缩放 • {Math.round(scale * 100)}%
          </Typography>
          <IconButton size="small" onClick={handleClose} sx={{ color: 'white' }}>
            <Close />
          </IconButton>
        </Box>
      </DialogTitle>
      <DialogContent
        sx={{
          bgcolor: 'transparent',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          overflow: 'hidden',
        }}
      >
        <Box
          component="img"
          src={imageUrl || ''}
          alt="preview"
          onWheel={handleWheel}
          sx={{
            maxWidth: '100%',
            maxHeight: '70vh',
            objectFit: 'contain',
            transform: `scale(${scale})`,
            transition: 'transform 0.1s',
            cursor: 'zoom-in',
          }}
        />
        <Box sx={{ display: 'flex', gap: 2, mt: 2 }}>
          <Button
            size="small"
            variant="outlined"
            sx={{ color: 'white', borderColor: 'white' }}
            onClick={handleZoomOut}
            disabled={scale <= 0.1}
          >
            缩小
          </Button>
          <Button
            size="small"
            variant="outlined"
            sx={{ color: 'white', borderColor: 'white' }}
            onClick={handleReset}
          >
            重置
          </Button>
          <Button
            size="small"
            variant="outlined"
            sx={{ color: 'white', borderColor: 'white' }}
            onClick={handleZoomIn}
            disabled={scale >= 5}
          >
            放大
          </Button>
        </Box>
      </DialogContent>
    </Dialog>
  )
}
