/**
 * 音频可视化组件
 */

import React, { useEffect, useRef, useState } from 'react'
import { Box, useTheme } from '@mui/material'

interface AudioVisualizerProps {
  isActive: boolean
  isSending: boolean
}

export const AudioVisualizer: React.FC<AudioVisualizerProps> = ({
  isActive,
  isSending,
}) => {
  const theme = useTheme()
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const animationRef = useRef<number | null>(null)
  const [bars, setBars] = useState<number[]>(Array(30).fill(2))

  useEffect(() => {
    if (!isActive) {
      // 重置为静止状态
      setBars(Array(30).fill(2))
      return
    }

    const canvas = canvasRef.current
    if (!canvas) return

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    let frame = 0

    const animate = () => {
      frame++

      // 生成随机波形
      const newBars = bars.map((_, i) => {
        const baseHeight = isActive ? 8 : 2
        const variation = isActive ? Math.sin(frame * 0.1 + i * 0.3) * 10 + Math.random() * 8 : 0
        return Math.max(2, baseHeight + variation)
      })
      setBars(newBars)

      animationRef.current = requestAnimationFrame(animate)
    }

    animate()

    return () => {
      if (animationRef.current) {
        cancelAnimationFrame(animationRef.current)
      }
    }
  }, [isActive])

  const color = isSending ? theme.palette.error.main : theme.palette.primary.main

  return (
    <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', gap: 0.5, height: '100%' }}>
      {bars.map((height, index) => (
        <Box
          key={index}
          sx={{
            width: 4,
            height: height,
            bgcolor: color,
            borderRadius: 0.5,
            transition: 'height 0.05s ease',
            opacity: isActive ? 1 : 0.3,
          }}
        />
      ))}
    </Box>
  )
}

export default AudioVisualizer
