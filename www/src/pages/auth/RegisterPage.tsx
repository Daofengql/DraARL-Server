import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Container,
  Box,
  Card,
  CardContent,
  TextField,
  Button,
  Typography,
  Alert,
  Link,
  Stepper,
  Step,
  StepLabel,
  IconButton,
} from '@mui/material'
import { Radio, CheckCircle, ContentCopy } from '@mui/icons-material'
import { authService } from '../../services'
import { usePublicConfig } from '../../hooks/usePublicConfig'
import { usePageTitle } from '../../hooks/usePageTitle'

const steps = ['基本信息', '联系方式', '设置密码']

interface FormData {
  username: string
  callsign: string
  phone: string
  password: string
  confirmPassword: string
  nickname: string
}

interface FormErrors {
  username?: string
  callsign?: string
  phone?: string
  password?: string
  confirmPassword?: string
}

export function RegisterPage() {
  const navigate = useNavigate()
  const { config } = usePublicConfig()
  const [activeStep, setActiveStep] = useState(0)

  // 同步页面标题
  usePageTitle()

  const [formData, setFormData] = useState<FormData>({
    username: '',
    callsign: '',
    phone: '',
    password: '',
    confirmPassword: '',
    nickname: '',
  })
  const [errors, setErrors] = useState<FormErrors>({})
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [registerSuccess, setRegisterSuccess] = useState(false)
  const [devicePassword, setDevicePassword] = useState('')

  const logoUrl = config.systemInfo.logo_url
  const siteName = config.systemInfo.name || 'DraARL'
  const siteShorthand = config.systemInfo.nameshorthand || 'DraARL'
  const icp = config.icp?.icp || ''

  // 验证呼号格式（字母开头，后跟字母数字，3-10个字符）
  const validateCallsign = (callsign: string): boolean => {
    return /^[A-Z][A-Z0-9]{2,9}$/i.test(callsign)
  }

  // 验证手机号格式（中国大陆手机号）
  const validatePhone = (phone: string): boolean => {
    return /^1[3-9]\d{9}$/.test(phone)
  }

  // 验证用户名格式
  const validateUsername = (username: string): boolean => {
    return /^[a-zA-Z0-9_]{3,20}$/.test(username)
  }

  // 验证密码强度
  const validatePassword = (password: string): boolean => {
    return password.length >= 6
  }

  const validateStep = (step: number): boolean => {
    const newErrors: FormErrors = {}
    let isValid = true

    if (step === 0) {
      // 验证基本信息
      if (!formData.username) {
        newErrors.username = '请输入用户名'
        isValid = false
      } else if (!validateUsername(formData.username)) {
        newErrors.username = '用户名必须是3-20个字符，只能包含字母、数字和下划线'
        isValid = false
      }

      if (!formData.callsign) {
        newErrors.callsign = '请输入呼号'
        isValid = false
      } else if (!validateCallsign(formData.callsign)) {
        newErrors.callsign = '呼号格式不正确，应以字母开头，3-10个字符'
        isValid = false
      }
    } else if (step === 1) {
      // 验证联系方式
      if (!formData.phone) {
        newErrors.phone = '请输入手机号'
        isValid = false
      } else if (!validatePhone(formData.phone)) {
        newErrors.phone = '手机号格式不正确'
        isValid = false
      }
    } else if (step === 2) {
      // 验证密码
      if (!formData.password) {
        newErrors.password = '请输入密码'
        isValid = false
      } else if (!validatePassword(formData.password)) {
        newErrors.password = '密码长度至少6位'
        isValid = false
      }

      if (!formData.confirmPassword) {
        newErrors.confirmPassword = '请确认密码'
        isValid = false
      } else if (formData.password !== formData.confirmPassword) {
        newErrors.confirmPassword = '两次输入的密码不一致'
        isValid = false
      }
    }

    setErrors(newErrors)
    return isValid
  }

  const handleNext = () => {
    setError('')
    if (validateStep(activeStep)) {
      setActiveStep((prev) => prev + 1)
    }
  }

  const handleBack = () => {
    setActiveStep((prev) => prev - 1)
    setErrors({})
  }

  const handleSubmit = async () => {
    setError('')

    if (!validateStep(2)) {
      return
    }

    setLoading(true)

    try {
      const result = await authService.register({
        username: formData.username,
        callsign: formData.callsign,
        phone: formData.phone,
        password: formData.password,
        nickname: formData.nickname || formData.username,
      })
      // 保存设备密码并显示成功页面
      if (result?.device_password) {
        setDevicePassword(result.device_password)
      }
      setRegisterSuccess(true)
    } catch (err: any) {
      setError(err.response?.data?.message || '注册失败')
    } finally {
      setLoading(false)
    }
  }

  const handleCopyPassword = () => {
    if (devicePassword) {
      navigator.clipboard.writeText(devicePassword)
    }
  }

  const renderStepContent = () => {
    switch (activeStep) {
      case 0:
        return (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
            <TextField
              fullWidth
              label="用户名"
              value={formData.username}
              onChange={(e) => setFormData({ ...formData, username: e.target.value })}
              error={!!errors.username}
              helperText={errors.username}
              required
              autoFocus
            />
            <TextField
              fullWidth
              label="呼号"
              value={formData.callsign}
              onChange={(e) => {
                const value = e.target.value.toUpperCase()
                setFormData({ ...formData, callsign: value })
              }}
              error={!!errors.callsign}
              helperText={errors.callsign || '例如: BG1ABC'}
              required
              inputProps={{ maxLength: 10 }}
            />
            <TextField
              fullWidth
              label="昵称（可选）"
              value={formData.nickname}
              onChange={(e) => setFormData({ ...formData, nickname: e.target.value })}
              helperText="默认使用用户名"
            />
          </Box>
        )
      case 1:
        return (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
            <TextField
              fullWidth
              label="手机号"
              value={formData.phone}
              onChange={(e) => setFormData({ ...formData, phone: e.target.value })}
              error={!!errors.phone}
              helperText={errors.phone || '用于身份验证和联系'}
              required
              inputProps={{ maxLength: 11 }}
            />
            <Alert severity="info">
              <Typography variant="body2">
                请确保手机号真实有效，我们需要通过操作证来验证您的业余无线电资格。
              </Typography>
            </Alert>
          </Box>
        )
      case 2:
        return (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
            <TextField
              fullWidth
              label="密码"
              type="password"
              value={formData.password}
              onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              error={!!errors.password}
              helperText={errors.password || '密码长度至少6位'}
              required
            />
            <TextField
              fullWidth
              label="确认密码"
              type="password"
              value={formData.confirmPassword}
              onChange={(e) => setFormData({ ...formData, confirmPassword: e.target.value })}
              error={!!errors.confirmPassword}
              helperText={errors.confirmPassword}
              required
            />
          </Box>
        )
      default:
        return null
    }
  }

  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: (theme) => theme.palette.background.default,
        py: 4,
      }}
    >
      <Container maxWidth="sm">
        <Card elevation={3}>
          <CardContent sx={{ p: 4 }}>
            {registerSuccess ? (
              // 注册成功页面
              <Box sx={{ textAlign: 'center' }}>
                <CheckCircle sx={{ fontSize: 64, color: 'success.main', mb: 2 }} />
                <Typography variant="h4" gutterBottom>
                  注册成功
                </Typography>
                <Typography variant="body1" color="text.secondary" sx={{ mb: 3 }}>
                  您的账号已创建成功，请等待管理员审核。
                </Typography>

                <Alert severity="warning" sx={{ mb: 3, textAlign: 'left' }}>
                  <Typography variant="subtitle2" gutterBottom>
                    设备准入密码（请立即保存）
                  </Typography>
                  <Box sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 1,
                    bgcolor: 'grey.100',
                    p: 1.5,
                    borderRadius: 1,
                    fontFamily: 'monospace',
                    fontSize: '1.2rem',
                  }}>
                    <strong>{devicePassword}</strong>
                    <IconButton size="small" onClick={handleCopyPassword}>
                      <ContentCopy fontSize="small" />
                    </IconButton>
                  </Box>
                  <Typography variant="caption" display="block" sx={{ mt: 1 }}>
                    此密码用于 DraARLv1 协议设备认证，仅显示一次，请务必保存！
                  </Typography>
                </Alert>

                <Alert severity="info" sx={{ mb: 3, textAlign: 'left' }}>
                  <Typography variant="body2">
                    审核通过后，您可以在"设备管理"页面查看或重新生成设备密码。
                  </Typography>
                </Alert>

                <Button
                  variant="contained"
                  size="large"
                  onClick={() => navigate('/login')}
                  sx={{ mt: 2 }}
                >
                  前往登录
                </Button>
              </Box>
            ) : (
              // 注册表单
              <>
                <Box sx={{ textAlign: 'center', mb: 4 }}>
                  {logoUrl ? (
                    <Box
                      component="img"
                      src={logoUrl}
                      alt={siteName}
                      onClick={() => navigate('/')}
                      sx={{
                        height: 80,
                        mb: 1.5,
                        objectFit: 'contain',
                        cursor: 'pointer',
                      }}
                      onError={(e) => {
                        (e.target as HTMLImageElement).style.display = 'none'
                      }}
                    />
                  ) : (
                    <Radio
                      sx={{ fontSize: 64, color: 'primary.main', mb: 1, cursor: 'pointer' }}
                      onClick={() => navigate('/')}
                    />
                  )}
                  <Typography variant="h6" component="h1" gutterBottom sx={{ fontWeight: 500 }}>
                    {siteShorthand}
                  </Typography>
                  <Typography variant="body1" color="text.secondary">
                    {siteName} - 创建新账号
                  </Typography>
                </Box>

                <Stepper activeStep={activeStep} sx={{ mb: 4 }}>
                  {steps.map((label) => (
                    <Step key={label}>
                      <StepLabel>{label}</StepLabel>
                    </Step>
                  ))}
                </Stepper>

                {error && (
                  <Alert severity="error" sx={{ mb: 3 }}>
                    {error}
                  </Alert>
                )}

                <Box sx={{ mb: 4 }}>
                  {renderStepContent()}
                </Box>

                <Box sx={{ display: 'flex', justifyContent: 'space-between', mt: 2 }}>
                  <Button
                    disabled={activeStep === 0}
                    onClick={handleBack}
                  >
                    上一步
                  </Button>
                  {activeStep === steps.length - 1 ? (
                    <Button
                      variant="contained"
                      onClick={handleSubmit}
                      disabled={loading}
                      startIcon={<CheckCircle />}
                    >
                      {loading ? '注册中...' : '完成注册'}
                    </Button>
                  ) : (
                    <Button
                      variant="contained"
                      onClick={handleNext}
                    >
                      下一步
                    </Button>
                  )}
                </Box>

                <Box sx={{ textAlign: 'center', mt: 3 }}>
                  <Link
                    component="button"
                    type="button"
                    variant="body2"
                    onClick={() => navigate('/login')}
                  >
                    已有账号？返回登录
                  </Link>
                </Box>
              </>
            )}
          </CardContent>
        </Card>

        {icp && (
          <Box sx={{ textAlign: 'center', mt: 2 }}>
            <Link
              href="http://beian.miit.gov.cn/"
              target="_blank"
              rel="noopener noreferrer"
              sx={{
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: 0.5,
                color: 'text.secondary',
                textDecoration: 'none',
                fontSize: '0.875rem',
                '&:hover': { color: 'text.primary' },
              }}
            >
              <Box
                component="img"
                src="//oss-fz.silverdragon.cn/loongapisources/picbed/penglong/2023/07/24/202307240118075832.png"
                alt="备案图标"
                sx={{ height: 18, width: 18 }}
              />
              {icp}
            </Link>
          </Box>
        )}
      </Container>
    </Box>
  )
}
