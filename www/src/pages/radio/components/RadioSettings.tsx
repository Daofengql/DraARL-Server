/**
 * 设置面板组件
 */

import React from 'react'
import {
  Box,
  Typography,
  IconButton,
  Divider,
  Button,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  ListItemSecondaryAction,
} from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import BadgeIcon from '@mui/icons-material/Badge'
import DeleteIcon from '@mui/icons-material/Delete'
import type { RadioUserConfig } from '../../../types/radio'

interface RadioSettingsProps {
  config: RadioUserConfig
  onConfigChange: (config: RadioUserConfig) => void
  onClose: () => void
  // 清除缓存的回调函数（由父组件提供，负责同时清理数据库和内存）
  onRequestClearCache?: () => Promise<void>
}

export const RadioSettings: React.FC<RadioSettingsProps> = ({
  onClose,
  onRequestClearCache,
}) => {
  // 清除缓存
  const handleClearCache = async () => {
    if (confirm('确定要彻底清除所有聊天记录和语音缓存吗？（此操作不可逆）')) {
      if (onRequestClearCache) {
        try {
          // 等待父组件完成彻底清理（含数据库和内存）
          await onRequestClearCache()
          alert('缓存已彻底清除！')
        } catch (error) {
          console.error('清除缓存失败:', error)
          alert('清除缓存时发生错误')
        }
      } else {
        // 兼容旧版本：如果没有传入 onRequestClearCache，仅显示提示
        alert('请在主页面进行缓存清理')
      }
    }
  }

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 头部 */}
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', p: 2, borderBottom: 1, borderColor: 'divider' }}>
        <Typography variant="h6">设置</Typography>
        <IconButton onClick={onClose} size="small">
          <CloseIcon />
        </IconButton>
      </Box>

      {/* 内容 */}
      <Box sx={{ flex: 1, overflow: 'auto', p: 2 }}>
        {/* 数据管理 */}
        <Typography variant="subtitle2" color="primary" gutterBottom>
          数据管理
        </Typography>

        <List disablePadding>
          <ListItem disableGutters>
            <ListItemIcon>
              <BadgeIcon />
            </ListItemIcon>
            <ListItemText
              primary="消息缓存"
              secondary="清除本地消息缓存"
            />
            <ListItemSecondaryAction>
              <Button
                size="small"
                color="error"
                startIcon={<DeleteIcon />}
                onClick={handleClearCache}
              >
                清除
              </Button>
            </ListItemSecondaryAction>
          </ListItem>
        </List>

        <Divider sx={{ my: 2 }} />

        {/* 关于 */}
        <Typography variant="subtitle2" color="primary" gutterBottom>
          关于
        </Typography>
        <Typography variant="body2" color="text.secondary">
          DraARL Link 在线收发 v1.0
        </Typography>
        <Typography variant="caption" color="text.secondary">
          使用 WebSocket 和 Opus 音频编解码
        </Typography>
      </Box>
    </Box>
  )
}

export default RadioSettings
