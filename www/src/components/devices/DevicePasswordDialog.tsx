import { useState, useEffect } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  Box,
  Typography,
  IconButton,
  Button,
  CircularProgress,
  Card,
  CardContent,
  Alert,
} from '@mui/material'
import Visibility from '@mui/icons-material/Visibility'
import VisibilityOff from '@mui/icons-material/VisibilityOff'
import Refresh from '@mui/icons-material/Refresh'
import ContentCopy from '@mui/icons-material/ContentCopy'
import Warning from '@mui/icons-material/Warning'
import { authService } from '../../services'

interface DevicePasswordDialogProps {
  open: boolean
  onClose: () => void
}

export function DevicePasswordDialog({ open, onClose }: DevicePasswordDialogProps) {
  const [password, setPassword] = useState<string>('')
  const [showPassword, setShowPassword] = useState(false)
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [confirmRefresh, setConfirmRefresh] = useState(false)

  // 获取当前用户信息（包含账号）
  const currentUser = authService.getStoredUser()

  // 加载当前密码
  const loadPassword = async () => {
    setLoading(true)
    setError('')
    try {
      const result = await authService.getDevicePassword()
      setPassword(result.device_password)
      setShowPassword(false) // 默认隐藏密码
    } catch (err: any) {
      setError(err.response?.data?.message || '获取密码失败')
    } finally {
      setLoading(false)
    }
  }

  // 对话框打开时加载密码
  useEffect(() => {
    if (open) {
      setShowPassword(false)
      setConfirmRefresh(false)
      loadPassword()
    }
  }, [open])

  // 复制密码
  const handleCopy = () => {
    if (password) {
      navigator.clipboard.writeText(password)
    }
  }

  // 点击刷新按钮 - 显示确认提示
  const handleRefreshClick = () => {
    setConfirmRefresh(true)
  }

  // 确认刷新密码
  const handleConfirmRefresh = async () => {
    setConfirmRefresh(false)
    setRefreshing(true)
    setError('')
    try {
      const result = await authService.regenerateDevicePassword()
      setPassword(result.device_password)
      setShowPassword(true) // 刷新后直接显示新密码
    } catch (err: any) {
      setError(err.response?.data?.message || '刷新密码失败')
    } finally {
      setRefreshing(false)
    }
  }

  // 取消刷新
  const handleCancelRefresh = () => {
    setConfirmRefresh(false)
  }

  // 关闭时重置
  const handleClose = () => {
    setPassword('')
    setShowPassword(false)
    setError('')
    setConfirmRefresh(false)
    onClose()
  }

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          设备密码
        </Box>
      </DialogTitle>
      <DialogContent>
        {/* 密码用途说明 */}
        <Alert severity="info" sx={{ mb: 2 }}>
          此密码仅供设备接入使用，不会涉及账号安全。
        </Alert>

        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
            <CircularProgress />
          </Box>
        ) : error ? (
          <Typography color="error">{error}</Typography>
        ) : (
          <Card variant="outlined" sx={{ mb: 2 }}>
            <CardContent>
              <Box sx={{ mb: 2 }}>
                <Typography variant="body2" color="text.secondary" gutterBottom>
                  账号
                </Typography>
                <Typography variant="body1" sx={{ fontFamily: 'monospace', fontSize: '1.1rem' }}>
                  {currentUser?.username || '-'}
                </Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="text.secondary" gutterBottom>
                  设备密码
                </Typography>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography
                    variant="body1"
                    sx={{
                      fontFamily: 'monospace',
                      fontSize: '1.1rem',
                      letterSpacing: showPassword ? '0.5px' : '0.2em',
                    }}
                  >
                    {showPassword ? password : '••••••••'}
                  </Typography>
                  <IconButton size="small" onClick={() => setShowPassword(!showPassword)}>
                    {showPassword ? <VisibilityOff fontSize="small" /> : <Visibility fontSize="small" />}
                  </IconButton>
                  <IconButton size="small" onClick={handleCopy} disabled={!password}>
                    <ContentCopy fontSize="small" />
                  </IconButton>
                </Box>
              </Box>
            </CardContent>
          </Card>
        )}

        {/* 刷新确认提示 */}
        {confirmRefresh && (
          <Alert
            severity="warning"
            icon={<Warning />}
            sx={{ mb: 2 }}
            action={
              <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', flexWrap: 'nowrap', whiteSpace: 'nowrap' }}>
                <Button size="small" color="inherit" onClick={handleCancelRefresh}>
                  取消
                </Button>
                <Button size="small" variant="contained" color="warning" onClick={handleConfirmRefresh}>
                  确认刷新
                </Button>
              </Box>
            }
          >
            <Typography variant="body2">
              <strong>刷新后原密码将立即失效</strong>
              <br />
              已连接的设备下次连接时将无法通过认证，需要使用新密码重新配置。
            </Typography>
          </Alert>
        )}

        <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 1 }}>
          <Button
            variant="outlined"
            color="warning"
            startIcon={refreshing ? <CircularProgress size={16} /> : <Refresh />}
            onClick={handleRefreshClick}
            disabled={loading || refreshing || confirmRefresh}
          >
            刷新
          </Button>
          <Button variant="contained" onClick={handleClose}>
            关闭
          </Button>
        </Box>
      </DialogContent>
    </Dialog>
  )
}
