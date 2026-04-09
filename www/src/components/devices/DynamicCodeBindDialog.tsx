import { useState, useRef, useEffect, useCallback } from 'react'
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
import { DYNAMIC_BIND_SSID_HINT, isValidDynamicBindSSID } from '../../utils/ssid'
import { getErrorMessage } from '../../utils/errorMessage'

interface DynamicCodeBindDialogProps {
  open: boolean
  onClose: () => void
}

const steps = ['输入动态码', '配置设备参数', '完成']

export function DynamicCodeBindDialog({ open, onClose }: DynamicCodeBindDialogProps) {
  const requestSeqRef = useRef(0)
  const [activeStep, setActiveStep] = useState(0)
  const [dynamicCode, setDynamicCode] = useState(['', '', '', '', '', ''])
  const [ssid, setSsid] = useState('1')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 六个输入框的 ref
  const inputRefs = useRef<(HTMLInputElement | null)[]>([])

  const handleReset = useCallback(() => {
    requestSeqRef.current += 1
    setActiveStep(0)
    setDynamicCode(['', '', '', '', '', ''])
    setSsid('1')
    setLoading(false)
    setError('')
    setBindResult(null)
    setConfigResult(null)
    setShowPassword(false)
  }, [])

  // 当 dialog 打开时，聚焦第一个输入框
  useEffect(() => {
    if (open && inputRefs.current[0]) {
      setTimeout(() => inputRefs.current[0]?.focus(), 100)
    }
  }, [open])

  useEffect(() => {
    handleReset()
  }, [open, handleReset])

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
  const isCodeComplete = dynamicCode.every((digit) => digit !== '')

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
    const requestSeq = requestSeqRef.current

    try {
      const result = await deviceBindService.bindDevice(code)
      if (requestSeq !== requestSeqRef.current) {
        return
      }
      setBindResult(result)
      setActiveStep(1)
    } catch (error) {
      if (requestSeq !== requestSeqRef.current) {
        return
      }
      setError(getErrorMessage(error, '绑定失败，请检查动态码是否正确'))
    } finally {
      if (requestSeq === requestSeqRef.current) {
        setLoading(false)
      }
    }
  }

  // 步骤2：提交配置
  const handleSubmitConfig = async () => {
    const ssidNum = parseInt(ssid, 10)
    if (isNaN(ssidNum) || !isValidDynamicBindSSID(ssidNum)) {
      setError(DYNAMIC_BIND_SSID_HINT)
      return
    }

    if (!bindResult) {
      setError('设备信息丢失，请重新绑定')
      return
    }

    setLoading(true)
    setError('')
    const requestSeq = requestSeqRef.current

    try {
      const result = await deviceBindService.submitDeviceConfig({
        device_mac: bindResult.device_mac,
        ssid: ssidNum,
      })
      if (requestSeq !== requestSeqRef.current) {
        return
      }
      setConfigResult(result)
      setActiveStep(2)
    } catch (error) {
      if (requestSeq !== requestSeqRef.current) {
        return
      }
      setError(getErrorMessage(error, '配置提交失败'))
    } finally {
      if (requestSeq === requestSeqRef.current) {
        setLoading(false)
      }
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
              helperText={error && activeStep === 1 ? error : `设备在群组中的唯一标识，${DYNAMIC_BIND_SSID_HINT}`}
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
              disabled={loading || !isCodeComplete}
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
