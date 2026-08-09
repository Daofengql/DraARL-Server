import { useState } from 'react'
import { Tooltip } from '@mui/material'
import ContentCopy from '@mui/icons-material/ContentCopy'
import Check from '@mui/icons-material/Check'

interface CopyButtonProps {
  text: string
  children?: React.ReactNode
}

export function CopyButton({ text, children }: CopyButtonProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('复制失败:', err)
    }
  }

  return (
    <Tooltip title={copied ? '已复制' : '复制'}>
      <span onClick={(e) => { e.stopPropagation(); handleCopy() }}>
        {children}
      </span>
    </Tooltip>
  )
}

export function CopyIconButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('复制失败:', err)
    }
  }

  return (
    <Tooltip title={copied ? '已复制' : '复制'}>
      <span onClick={(e) => { e.stopPropagation(); handleCopy() }}>
        {copied ? <Check fontSize="small" color="success" /> : <ContentCopy fontSize="small" />}
      </span>
    </Tooltip>
  )
}
