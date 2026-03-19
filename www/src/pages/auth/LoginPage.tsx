import { useState, useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
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
  Divider,
  Snackbar,
  Tabs,
  Tab,
} from '@mui/material'
import Radio from '@mui/icons-material/Radio'
import { authService, ssoService, captchaService, emailAuthService } from '../../services'
import { usePublicConfig } from '../../hooks/usePublicConfig'
import { usePageTitle } from '../../hooks/usePageTitle'

export function LoginPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { config } = usePublicConfig()
  const [loginMode, setLoginMode] = useState(0) // 0: 密码登录, 1: 验证码登录

  // 密码登录状态
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')

  // 验证码登录状态
  const [email, setEmail] = useState('')
  const [captchaId, setCaptchaId] = useState('')
  const [captchaCode, setCaptchaCode] = useState('')
  const [captchaImage, setCaptchaImage] = useState('')
  const [emailCode, setEmailCode] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [countdown, setCountdown] = useState(0)

  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [ssoMessage, setSsoMessage] = useState<{ type: 'success' | 'error'; message: string } | null>(null)

  // 同步页面标题
  usePageTitle()

  // 自动加载图片验证码
  useEffect(() => {
    getCaptcha()
  }, [])

  // 处理 URL 中的 sso_error 参数
  useEffect(() => {
    const ssoError = searchParams.get('sso_error')

    // 检查是否在弹出窗口中（SSO 回调失败时后端重定向到此）
    const isPopup = window.opener && window.opener !== window

    if (ssoError) {
      if (isPopup) {
        // 在弹出窗口中，通过 postMessage 通知父窗口并关闭自己
        window.opener.postMessage(
          {
            type: 'SSO_LOGIN_ERROR',
            error: ssoError,
          },
          window.location.origin
        )
        setTimeout(() => window.close(), 100)
        return
      }

      // 不在弹出窗口中，正常显示错误
      setError(ssoError)
      // 清除 URL 中的错误参数
      window.history.replaceState({}, '', '/login')
    }
  }, [searchParams])

  // 监听来自 SSO 回调窗口的消息
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      // 安全检查：确保消息来自可信源
      if (event.origin !== window.location.origin) return

      const { type, token, user, error: ssoError } = event.data || {}

      if (type === 'SSO_LOGIN_SUCCESS' && token && user) {
        authService.saveAuth(token, user)
        setSsoMessage({ type: 'success', message: 'SSO 登录成功' })
        setTimeout(() => navigate('/dashboard'), 500)
      } else if (type === 'SSO_LOGIN_ERROR' && ssoError) {
        setError(ssoError)
        setLoading(false)
      }
    }

    window.addEventListener('message', handleMessage)
    return () => window.removeEventListener('message', handleMessage)
  }, [navigate])

  // 获取图片验证码
  const getCaptcha = async () => {
    try {
      const res = await captchaService.getCaptcha()
      setCaptchaId(res.captcha_id)
      setCaptchaImage(res.captcha_image)
    } catch (err) {
      // 静默失败
    }
  }

  // 密码登录
  const handlePasswordLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const response = await authService.login({ username, password })
      authService.saveAuth(response.token, response.user)
      navigate('/dashboard')
    } catch (err: any) {
      setError(err.response?.data?.message || '登录失败，请检查用户名和密码')
    } finally {
      setLoading(false)
    }
  }

  // 发送邮箱验证码
  const handleSendEmailCode = async () => {
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
        purpose: 'login',
        captcha_id: captchaId,
        captcha_code: captchaCode,
      })
      setSessionId(res.session_id)
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
      getCaptcha()
    } finally {
      setLoading(false)
    }
  }

  // 邮箱验证码登录
  const handleEmailLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!sessionId) {
      setError('请先获取邮箱验证码')
      return
    }
    if (!emailCode) {
      setError('请输入邮箱验证码')
      return
    }

    setError('')
    setLoading(true)

    try {
      const response = await emailAuthService.emailLogin({
        session_id: sessionId,
        code: emailCode,
      })
      authService.saveAuth(response.token, response.user)
      navigate('/dashboard')
    } catch (err: any) {
      setError(err.response?.data?.message || '登录失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSSOLogin = async () => {
    try {
      setLoading(true)
      setError('')
      const res = await ssoService.getLoginURL()
      // 打开新窗口进行 SSO 登录
      const width = 600
      const height = 700
      const left = window.screenX + (window.outerWidth - width) / 2
      const top = window.screenY + (window.outerHeight - height) / 2
      window.open(
        res.url,
        'SSO Login',
        `width=${width},height=${height},left=${left},top=${top},toolbar=no,menubar=no,resizable=yes`
      )
      // 注意：不在这里设置 loading = false，等待 postMessage 回调
    } catch (err: any) {
      setError(err.response?.data?.message || '获取SSO登录地址失败')
      setLoading(false)
    }
  }

  const logoUrl = config.systemInfo.logo_url
  const siteName = config.systemInfo.name || 'DraARL'
  const siteShorthand = config.systemInfo.nameshorthand || 'DraARL'
  const icp = config.icp?.icp || ''

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
                {siteName}
              </Typography>
            </Box>

            {error && (
              <Alert severity="error" sx={{ mb: 3 }}>
                {error}
              </Alert>
            )}

            {/* 登录方式切换 */}
            <Tabs
              value={loginMode}
              onChange={(_, v) => setLoginMode(v)}
              variant="fullWidth"
              sx={{ mb: 3 }}
            >
              <Tab label="密码登录" />
              <Tab label="验证码登录" />
            </Tabs>

            {/* 密码登录表单 */}
            {loginMode === 0 && (
              <form onSubmit={handlePasswordLogin}>
                <TextField
                  fullWidth
                  label="用户名/邮箱"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  margin="normal"
                  required
                  autoFocus
                />
                <TextField
                  fullWidth
                  label="密码"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  margin="normal"
                  required
                />
                <Button
                  fullWidth
                  type="submit"
                  variant="contained"
                  size="large"
                  sx={{ mt: 3, mb: 2 }}
                  disabled={loading}
                >
                  {loading ? '登录中...' : '登录'}
                </Button>
              </form>
            )}

            {/* 验证码登录表单 */}
            {loginMode === 1 && (
              <form onSubmit={handleEmailLogin}>
                <TextField
                  fullWidth
                  label="邮箱地址"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  margin="normal"
                  required
                  autoFocus
                />
                <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', mt: 2 }}>
                  <TextField
                    label="图片验证码"
                    value={captchaCode}
                    onChange={(e) => setCaptchaCode(e.target.value)}
                    required
                    sx={{ flex: 1 }}
                  />
                  <Box
                    component="img"
                    src={captchaImage}
                    alt="验证码"
                    onClick={getCaptcha}
                    sx={{
                      height: 64,
                      cursor: 'pointer',
                      borderRadius: 1,
                      bgcolor: 'action.hover',
                    }}
                  />
                </Box>
                <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', mt: 2 }}>
                  <TextField
                    label="邮箱验证码"
                    value={emailCode}
                    onChange={(e) => setEmailCode(e.target.value)}
                    required
                    sx={{ flex: 1 }}
                  />
                  <Button
                    variant="outlined"
                    onClick={handleSendEmailCode}
                    disabled={loading || countdown > 0}
                    sx={{ minWidth: 120 }}
                  >
                    {countdown > 0 ? `${countdown}s` : '发送验证码'}
                  </Button>
                </Box>
                <Button
                  fullWidth
                  type="submit"
                  variant="contained"
                  size="large"
                  sx={{ mt: 3, mb: 2 }}
                  disabled={loading}
                >
                  {loading ? '登录中...' : '登录'}
                </Button>
              </form>
            )}

            <Box sx={{ textAlign: 'center', mt: 2, display: 'flex', justifyContent: 'center', gap: 2 }}>
              <Link
                component="button"
                type="button"
                variant="body2"
                onClick={() => navigate('/register')}
              >
                没有账号？立即注册
              </Link>
              <Link
                component="button"
                type="button"
                variant="body2"
                onClick={() => navigate('/forgot-password')}
              >
                忘记密码？
              </Link>
            </Box>

            {config.sso_enabled && (
              <Box sx={{ mt: 3 }}>
                <Divider sx={{ my: 2 }}>或</Divider>
                <Button
                  fullWidth
                  variant="outlined"
                  size="large"
                  onClick={handleSSOLogin}
                  disabled={loading}
                  sx={{
                    background: 'linear-gradient(45deg, #2196f3 30%, #21cbf3 90%)',
                    color: 'white',
                    borderColor: 'transparent',
                    '&:hover': {
                      background: 'linear-gradient(45deg, #1976d2 30%, #1e88e5 90%)',
                      borderColor: 'transparent',
                    },
                  }}
                >
                  使用 {config.sso_name || 'SSO'} 登录
                </Button>
              </Box>
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

      {/* SSO 登录成功提示 */}
      <Snackbar
        open={ssoMessage !== null}
        autoHideDuration={3000}
        onClose={() => setSsoMessage(null)}
        anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
      >
        <Alert severity={ssoMessage?.type} onClose={() => setSsoMessage(null)}>
          {ssoMessage?.message}
        </Alert>
      </Snackbar>
    </Box>
  )
}
