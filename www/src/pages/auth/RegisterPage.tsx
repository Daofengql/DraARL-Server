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
} from '@mui/material'
import { Radio, CheckCircle } from '@mui/icons-material'
import { authService } from '../../services'

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
  const [activeStep, setActiveStep] = useState(0)
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
      await authService.register({
        username: formData.username,
        callsign: formData.callsign,
        phone: formData.phone,
        password: formData.password,
        nickname: formData.nickname || formData.username,
      })
      navigate('/login', { state: { message: '注册成功，请等待管理员审核' } })
    } catch (err: any) {
      setError(err.response?.data?.message || '注册失败')
    } finally {
      setLoading(false)
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
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: (theme) => theme.palette.background.default,
        py: 4,
      }}
    >
      <Container maxWidth="sm">
        <Card elevation={3}>
          <CardContent sx={{ p: 4 }}>
            <Box sx={{ textAlign: 'center', mb: 4 }}>
              <Radio sx={{ fontSize: 48, color: 'primary.main', mb: 1 }} />
              <Typography variant="h4" component="h1" gutterBottom>
                DraARL
              </Typography>
              <Typography variant="body2" color="text.secondary">
                创建新账号
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
          </CardContent>
        </Card>
      </Container>
    </Box>
  )
}
