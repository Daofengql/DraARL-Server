import { useState, useRef, useEffect } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  Box,
  Typography,
  Alert,
  Stepper,
  Step,
  StepLabel,
  CircularProgress,
  IconButton,
} from '@mui/material'
import Visibility from '@mui/icons-material/Visibility'
import VisibilityOff from '@mui/icons-material/VisibilityOff'
import CheckCircle from '@mui/icons-material/CheckCircle'
import { deviceBindService } from '../../services'

interface DynamicCodeBindDialogProps {
  open: boolean
  onClose: () => void
}

const steps = ['输入动态码', '配置设备参数', '完成']

export function DynamicCodeBindDialog({ open, onClose }: DynamicCodeBindDialogProps) {
  const [activeStep, setActiveStep] = useState(0)
  const [dynamicCode, setDynamicCode] = useState(['', '', '', '', '', ''])
  const [ssid, setSsid] = useState('1')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 六个输入框的 ref
  const inputRefs = useRef<(HTMLInputElement | null)[]>([])

  // 当 dialog 打开时，聚焦第一个输入框
  useEffect(() => {
    if (open && inputRefs.current[0]) {
      setTimeout(() => inputRefs.current[0]?.focus(), 100)
    }
  }, [open])

  const handleReset = () => {
    setActiveStep(0)
    setDynamicCode(['', '', '', '', '', ''])
    setSsid('1')
    setLoading(false)
    setError('')
    setBindResult(null)
    setConfigResult(null)
    setShowPassword(false)
  }

  // 处理单个输入框的变化
  const handleCodeChange = (index: number, value: string) => {
    // 只允许数字
    const digit = value.replace(/\D/g, '').slice(-1)

    const newCode = [...dynamicCode]
    newCode[index] = digit
    setDynamicCode(newCode)
    setError('')

    // 如果输入了数字，自动跳到下一个输入框
    if (digit && index < 5) {
      inputRefs.current[index + 1]?.focus()
    }
  }

  // 处理退格键
  const handleKeyDown = (index: number, e: React.KeyboardEvent) => {
    if (e.key === 'Backspace' && !dynamicCode[index] && index > 0) {
      inputRefs.current[index - 1]?.focus()
    }
  }

  // 处理粘贴
  const handlePaste = (e: React.ClipboardEvent) => {
    e.preventDefault()
    const text = e.clipboardData.getData('text').replace(/\D/g, '').slice(0, 6)
    if (text) {
      const newCode = text.split('').concat(['', '', '', '', '', '']).slice(0, 6)
      setDynamicCode(newCode)
      // 聚焦到下一个空输入框或最后一个
      const nextIndex = Math.min(text.length, 5)
      inputRefs.current[nextIndex]?.focus()
    }
  }

  // 获取完整的动态码
  const getFullCode = () => dynamicCode.join('')

  // 绑定结果
  const [bindResult, setBindResult] = useState<{
    device_mac: string
    call_sign: string
  } | null>(null)

  // 配置结果
  const [configResult, setConfigResult] = useState<{
    udp_auth_info: {
      username: string
      device_password: string
    }
    dmr_id: number
  } | null>(null)

  // 显示/隐藏密码
  const [showPassword, setShowPassword] = useState(false)

  const handleClose = () => {
    handleReset()
    onClose()
  }

  // 步骤1：绑定设备
  const handleBindDevice = async () => {
    const code = getFullCode()
    if (code.length !== 6) {
      setError('请输入6位动态码')
      return
    }

    setLoading(true)
    setError('')

    try {
      const result = await deviceBindService.bindDevice(code)
      setBindResult(result)
      setActiveStep(1)
    } catch (err: any) {
      setError(err.response?.data?.message || '绑定失败，请检查动态码是否正确')
    } finally {
      setLoading(false)
    }
  }

  // 验证 SSID 范围 (1-99 或 106-235)
  const isValidSsid = (ssidNum: number) => {
    return (ssidNum >= 1 && ssidNum <= 99) || (ssidNum >= 106 && ssidNum <= 235)
  }

  // 步骤2：提交配置
  const handleSubmitConfig = async () => {
    const ssidNum = parseInt(ssid, 10)
    if (isNaN(ssidNum) || !isValidSsid(ssidNum)) {
      setError('SSID 必须在 1-99 或 106-235 范围内')
      return
    }

    if (!bindResult) {
      setError('设备信息丢失，请重新绑定')
      return
    }

    setLoading(true)
    setError('')

    try {
      const result = await deviceBindService.submitDeviceConfig({
        device_mac: bindResult.device_mac,
        ssid: ssidNum,
      })
      setConfigResult(result)
      setActiveStep(2)
    } catch (err: any) {
      setError(err.response?.data?.message || '配置提交失败')
    } finally {
      setLoading(false)
    }
  }

  // 渲染步骤内容
  const renderStepContent = () => {
    switch (activeStep) {
      case 0:
        return (
          <Box>
            <Alert severity="info" sx={{ mb: 2 }}>
              请在设备上获取6位动态码，然后输入以下输入框完成绑定
            </Alert>
            <Box
              sx={{
                display: 'flex',
                gap: 1,
                justifyContent: 'center',
                mb: 2,
              }}
            >
              {dynamicCode.map((digit, index) => (
                <TextField
                  key={index}
                  inputRef={(el) => (inputRefs.current[index] = el)}
                  value={digit}
                  onChange={(e) => handleCodeChange(index, e.target.value)}
                  onKeyDown={(e) => handleKeyDown(index, e)}
                  onPaste={index === 0 ? handlePaste : undefined}
                  disabled={loading}
                  inputProps={{
                    maxLength: 1,
                    style: {
                      fontSize: '1.5rem',
                      textAlign: 'center',
                      width: '3rem',
                      padding: '0.75rem 0.5rem',
                    },
                  }}
                  sx={{
                    '& .MuiOutlinedInput-root': {
                      justifyContent: 'center',
                    },
                  }}
                />
              ))}
            </Box>
            {error && activeStep === 0 && (
              <Typography color="error" variant="body2" align="center">
                {error}
              </Typography>
            )}
            {!error && (
              <Typography variant="body2" color="text.secondary" align="center">
                动态码有效期为60秒
              </Typography>
            )}
          </Box>
        )

      case 1:
        return (
          <Box>
            <Alert severity="success" sx={{ mb: 2 }}>
              <Typography variant="body2">
                设备 <strong>{bindResult?.device_mac}</strong> 即将完成登录
              </Typography>
              <Typography variant="body2">
                呼叫：<strong>{bindResult?.call_sign}</strong>
              </Typography>
            </Alert>
            <TextField
              label="SSID"
              fullWidth
              type="number"
              value={ssid}
              onChange={(e) => {
                setSsid(e.target.value)
                setError('')
              }}
              disabled={loading}
              error={!!error && activeStep === 1}
              helperText={error && activeStep === 1 ? error : '设备在群组中的唯一标识 (1-99 或 106-235)'}
              slotProps={{
                htmlInput: {
                  min: 1,
                  max: 235,
                },
              }}
            />
          </Box>
        )

      case 2:
        return (
          <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', mb: 2 }}>
              <CheckCircle sx={{ fontSize: 64, color: 'success.main' }} />
            </Box>
            <Typography variant="h6" align="center" gutterBottom>
              配置完成！
            </Typography>
            <Typography variant="body2" color="text.secondary" align="center" sx={{ mb: 2 }}>
              设备将自动获取配置并上线
            </Typography>
            <Alert severity="info" sx={{ mb: 2 }}>
              <Typography variant="body2">
                设备将获得以下认证信息：
              </Typography>
              <Box sx={{ mt: 1, p: 1, bgcolor: 'background.paper', borderRadius: 1 }}>
                <Typography variant="body2">
                  用户名：<strong>{configResult?.udp_auth_info.username}</strong>
                </Typography>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography variant="body2">
                    设备密码：
                  </Typography>
                  <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                    {showPassword ? configResult?.udp_auth_info.device_password : '••••••••'}
                  </Typography>
                  <IconButton size="small" onClick={() => setShowPassword(!showPassword)}>
                    {showPassword ? <VisibilityOff fontSize="small" /> : <Visibility fontSize="small" />}
                  </IconButton>
                </Box>
                <Typography variant="body2">
                  DMR ID：<strong>{configResult?.dmr_id}</strong>
                </Typography>
              </Box>
            </Alert>
          </Box>
        )

      default:
        return null
    }
  }

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth disableRestoreFocus>
      <DialogTitle>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          动态码登录
        </Box>
      </DialogTitle>
      <DialogContent>
        <Stepper activeStep={activeStep} sx={{ mb: 3 }}>
          {steps.map((label) => (
            <Step key={label}>
              <StepLabel>{label}</StepLabel>
            </Step>
          ))}
        </Stepper>
        {renderStepContent()}
      </DialogContent>
      <DialogActions>
        {activeStep === 0 && (
          <>
            <Button onClick={handleClose}>取消</Button>
            <Button
              variant="contained"
              onClick={handleBindDevice}
              disabled={loading || dynamicCode.length !== 6}
            >
              {loading ? <CircularProgress size={24} /> : '绑定设备'}
            </Button>
          </>
        )}
        {activeStep === 1 && (
          <>
            <Button onClick={handleReset}>重新绑定</Button>
            <Button
              variant="contained"
              onClick={handleSubmitConfig}
              disabled={loading}
            >
              {loading ? <CircularProgress size={24} /> : '提交配置'}
            </Button>
          </>
        )}
        {activeStep === 2 && (
          <Button variant="contained" onClick={handleClose}>
            完成
          </Button>
        )}
      </DialogActions>
    </Dialog>
  )
}
