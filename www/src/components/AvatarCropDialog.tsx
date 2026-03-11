import { useState, useCallback, useEffect } from 'react'
import Cropper from 'react-easy-crop'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Slider,
  Typography,
  IconButton,
} from '@mui/material'
import ZoomIn from '@mui/icons-material/ZoomIn'
import ZoomOut from '@mui/icons-material/ZoomOut'
import RotateLeft from '@mui/icons-material/RotateLeft'
import RotateRight from '@mui/icons-material/RotateRight'

interface AvatarCropDialogProps {
  open: boolean
  imageSrc: string
  onClose: () => void
  onConfirm: (croppedImageBlob: Blob) => void
}

const CROP_AREA_SIZE = { width: 300, height: 300 }

// 将裁切区域转换为实际的图片数据
const createImage = (url: string): Promise<HTMLImageElement> =>
  new Promise((resolve, reject) => {
    const image = new Image()
    image.addEventListener('load', () => resolve(image))
    image.addEventListener('error', (error) => reject(error))
    image.src = url
  })

export function AvatarCropDialog({ open, imageSrc, onClose, onConfirm }: AvatarCropDialogProps) {
  const [crop, setCrop] = useState({ x: 0, y: 0 })
  const [zoom, setZoom] = useState(1)
  const [rotation, setRotation] = useState(0)
  const [croppedAreaPixels, setCroppedAreaPixels] = useState<any>(null)
  const [processing, setProcessing] = useState(false)

  const onCropComplete = useCallback((_croppedArea: any, croppedAreaPixels: any) => {
    setCroppedAreaPixels(croppedAreaPixels)
  }, [])

  // 生成裁切后的图片
  const generateCroppedImage = useCallback(async () => {
    setProcessing(true)
    try {
      const image = await createImage(imageSrc)
      const canvas = document.createElement('canvas')
      const ctx = canvas.getContext('2d')

      if (!ctx) {
        throw new Error('No 2d context')
      }

      // 设置输出尺寸为裁切区域大小
      canvas.width = croppedAreaPixels.width
      canvas.height = croppedAreaPixels.height

      // 旋转角度（弧度）
      const rotRad = (rotation * Math.PI) / 180

      // 绘制裁切后的图片
      ctx.translate(canvas.width / 2, canvas.height / 2)
      ctx.rotate(rotRad)
      ctx.translate(-canvas.width / 2, -canvas.height / 2)

      ctx.drawImage(
        image,
        croppedAreaPixels.x,
        croppedAreaPixels.y,
        croppedAreaPixels.width,
        croppedAreaPixels.height,
        0,
        0,
        croppedAreaPixels.width,
        croppedAreaPixels.height
      )

      // 转换为 Blob
      const blob = await new Promise<Blob>((resolve) => {
        canvas.toBlob((blob) => {
          if (blob) {
            resolve(blob)
          } else {
            resolve(new Blob([], { type: 'image/jpeg' }))
          }
        }, 'image/jpeg', 0.95)
      })

      onConfirm(blob)
    } catch (error) {
      console.error('Error generating cropped image:', error)
    } finally {
      setProcessing(false)
    }
  }, [imageSrc, croppedAreaPixels, rotation, onConfirm])

  const handleConfirm = () => {
    generateCroppedImage()
  }

  const handleRotate = (direction: 'left' | 'right') => {
    if (direction === 'left') {
      setRotation((r) => r - 90)
    } else {
      setRotation((r) => r + 90)
    }
  }

  // 重置状态
  useEffect(() => {
    if (!open) {
      setCrop({ x: 0, y: 0 })
      setZoom(1)
      setRotation(0)
      setCroppedAreaPixels(null)
    }
  }, [open])

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>裁切头像</DialogTitle>
      <DialogContent>
        <Box
          sx={{
            position: 'relative',
            width: '100%',
            height: 350,
            bgcolor: '#333',
            borderRadius: 1,
            overflow: 'hidden',
          }}
        >
          <Cropper
            image={imageSrc}
            crop={crop}
            zoom={zoom}
            rotation={rotation}
            aspect={1}
            cropShape="round"
            showGrid={false}
            onCropChange={setCrop}
            onZoomChange={setZoom}
            onCropComplete={onCropComplete}
            cropSize={CROP_AREA_SIZE}
            style={{
              containerStyle: {
                width: '100%',
                height: '100%',
              },
            }}
          />
        </Box>

        <Box sx={{ mt: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2 }}>
            <ZoomOut fontSize="small" />
            <Slider
              value={zoom}
              min={0.5}
              max={3}
              step={0.1}
              onChange={(_, value) => setZoom(value as number)}
              sx={{ flex: 1 }}
            />
            <ZoomIn fontSize="small" />
          </Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', textAlign: 'center' }}>
            缩放: {Math.round(zoom * 100)}%
          </Typography>
        </Box>

        <Box sx={{ display: 'flex', justifyContent: 'center', gap: 1, mt: 2 }}>
          <IconButton onClick={() => handleRotate('left')} size="small">
            <RotateLeft />
          </IconButton>
          <IconButton onClick={() => handleRotate('right')} size="small">
            <RotateRight />
          </IconButton>
        </Box>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', textAlign: 'center', mt: 1 }}>
          旋转图片
        </Typography>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={processing}>
          取消
        </Button>
        <Button onClick={handleConfirm} variant="contained" disabled={processing || !croppedAreaPixels}>
          {processing ? '处理中...' : '确认'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}
