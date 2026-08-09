import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box,
  Card,
  CardContent,
  TextField,
  Button,
  Typography,
  Alert,
  InputAdornment,
  IconButton,
  Link as MuiLink,
} from '@mui/material'
import Radio from '@mui/icons-material/Radio'
import Visibility from '@mui/icons-material/Visibility'
import VisibilityOff from '@mui/icons-material/VisibilityOff'
import { captchaService, emailAuthService } from '../../services'
import { usePublicConfig } from '../../hooks/usePublicConfig'
import { usePageTitle } from '../../hooks/usePageTitle'
import { PublicPageLayout } from '../../components/layout'

export function ForgotPasswordPage() {
  const navigate = useNavigate()
  const { config } = usePublicConfig()
  const [step, setStep] = useState(1) // 1: 输入邮箱, 2: 验证码, 3: 设置新密码
  const [email, setEmail] = useState('')
  const [captchaId, setCaptchaId] = useState('')
  const [captchaCode, setCaptchaCode] = useState('')
  const [captchaImage, setCaptchaImage] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [code, setCode] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [countdown, setCountdown] = useState(0)

  usePageTitle()

  // 自动加载图片验证码
  useEffect(() => {
    getCaptcha()
  }, [])

  // 获取图片验证码
  const getCaptcha = async () => {
    try {
      const res = await captchaService.getCaptcha()
      setCaptchaId(res.captcha_id)
      setCaptchaImage(res.captcha_image)
    } catch {
      setError('获取验证码失败')
    }
  }

  // 发送邮箱验证码
  const handleSendCode = async () => {
    if (!email) {
      setError('请输入邮箱地址')
      return
    }
    if (!captchaCode) {
      setError('请输入图片验证码')
      return
    }

    setLoading(true)
    setError('')
    try {
      const res = await emailAuthService.sendCode({
        email,
        purpose: 'reset_password',
        captcha_id: captchaId,
        captcha_code: captchaCode,
      })
      setSessionId(res.session_id)
      setStep(2)
      // 开始倒计时
      setCountdown(60)
      const timer = setInterval(() => {
        setCountdown((prev) => {
          if (prev <= 1) {
            clearInterval(timer)
            return 0
          }
          return prev - 1
        })
      }, 1000)
    } catch (err: any) {
      setError(err.response?.data?.message || '发送验证码失败')
      getCaptcha() // 刷新验证码
    } finally {
      setLoading(false)
    }
  }

  // 验证验证码
  const handleVerifyCode = async () => {
    if (!code) {
      setError('请输入验证码')
      return
    }

    setLoading(true)
    setError('')
    try {
      // 直接进入第三步设置密码
      setStep(3)
    } finally {
      setLoading(false)
    }
  }

  // 重置密码
  const handleResetPassword = async () => {
    if (!newPassword) {
      setError('请输入新密码')
      return
    }
    if (newPassword.length < 6) {
      setError('密码长度至少6位')
      return
    }
    if (newPassword !== confirmPassword) {
      setError('两次输入的密码不一致')
      return
    }

    setLoading(true)
    setError('')
    try {
      await emailAuthService.resetPassword({
        session_id: sessionId,
        code,
        new_password: newPassword,
      })
      navigate('/login', { state: { message: '密码重置成功，请使用新密码登录' } })
    } catch (err: any) {
      setError(err.response?.data?.message || '密码重置失败')
    } finally {
      setLoading(false)
    }
  }

  const logoUrl = config.systemInfo.logo_url
  const siteName = config.systemInfo.name || 'DraARL'
  const siteShorthand = config.systemInfo.nameshorthand || 'DraARL'

  return (
    <PublicPageLayout>
      <Card elevation={3}>
        <CardContent sx={{ p: 4 }}>
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
                找回密码
              </Typography>
            </Box>

            {error && (
              <Alert severity="error" sx={{ mb: 3 }}>
                {error}
              </Alert>
            )}

            {/* 第一步：输入邮箱 */}
            {step === 1 && (
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                <TextField
                  fullWidth
                  label="邮箱地址"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                  autoFocus
                />
                <Box sx={{ display: 'flex', flexDirection: { xs: 'column', sm: 'row' }, gap: 1, alignItems: { xs: 'stretch', sm: 'center' } }}>
                  <TextField
                    fullWidth
                    label="图片验证码"
                    value={captchaCode}
                    onChange={(e) => setCaptchaCode(e.target.value)}
                    required
                    sx={{ flex: 1, minWidth: 180 }}
                  />
                  <Box
                    component="img"
                    src={captchaImage}
                    alt="验证码"
                    onClick={getCaptcha}
                    sx={{
                      height: 64,
                      width: { xs: '100%', sm: 180 },
                      maxWidth: 180,
                      cursor: 'pointer',
                      borderRadius: 1,
                      bgcolor: 'action.hover',
                      objectFit: 'contain',
                    }}
                  />
                </Box>
                <Button
                  fullWidth
                  variant="contained"
                  size="large"
                  onClick={handleSendCode}
                  disabled={loading}
                  sx={{ mt: 2 }}
                >
                  {loading ? '发送中...' : '发送验证码'}
                </Button>
              </Box>
            )}

            {/* 第二步：输入验证码 */}
            {step === 2 && (
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                <Alert severity="info">
                  验证码已发送至 {email}，请查收邮件
                </Alert>
                <TextField
                  fullWidth
                  label="邮箱验证码"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  required
                  autoFocus
                />
                <Button
                  fullWidth
                  variant="contained"
                  size="large"
                  onClick={handleVerifyCode}
                  disabled={loading}
                  sx={{ mt: 2 }}
                >
                  {loading ? '验证中...' : '验证'}
                </Button>
                <Button
                  fullWidth
                  variant="text"
                  onClick={() => {
                    setStep(1)
                    getCaptcha()
                  }}
                  disabled={countdown > 0}
                >
                  {countdown > 0 ? `重新发送 (${countdown}s)` : '重新发送验证码'}
                </Button>
              </Box>
            )}

            {/* 第三步：设置新密码 */}
            {step === 3 && (
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                <TextField
                  fullWidth
                  label="新密码"
                  type={showPassword ? 'text' : 'password'}
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  required
                  autoFocus
                  InputProps={{
                    endAdornment: (
                      <InputAdornment position="end">
                        <IconButton onClick={() => setShowPassword(!showPassword)} edge="end">
                          {showPassword ? <VisibilityOff /> : <Visibility />}
                        </IconButton>
                      </InputAdornment>
                    ),
                  }}
                />
                <TextField
                  fullWidth
                  label="确认密码"
                  type={showPassword ? 'text' : 'password'}
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  required
                />
                <Button
                  fullWidth
                  variant="contained"
                  size="large"
                  onClick={handleResetPassword}
                  disabled={loading}
                  sx={{ mt: 2 }}
                >
                  {loading ? '重置中...' : '重置密码'}
                </Button>
              </Box>
            )}

            <Box sx={{ textAlign: 'center', mt: 3 }}>
              <MuiLink
                component="button"
                type="button"
                variant="body2"
                onClick={() => navigate('/login')}
                sx={{ cursor: 'pointer' }}
              >
                返回登录
              </MuiLink>
            </Box>
          </CardContent>
        </Card>
    </PublicPageLayout>
  )
}
