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
  Divider,
} from '@mui/material'
import Radio from '@mui/icons-material/Radio'
import { authService, ssoService } from '../../services'
import { usePublicConfig } from '../../hooks/usePublicConfig'
import { usePageTitle } from '../../hooks/usePageTitle'

export function LoginPage() {
  const navigate = useNavigate()
  const { config } = usePublicConfig()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // 同步页面标题
  usePageTitle()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const response = await authService.login({ username, password })
      authService.saveAuth(response.token, response.user)
      navigate('/dashboard')
    } catch (err: any) {
      setError(err.response?.data?.message || '登录失败，���检查用户名和密码')
    } finally {
      setLoading(false)
    }
  }

  const handleSSOLogin = async () => {
    try {
      setLoading(true)
      const res = await ssoService.getLoginURL()
      window.location.href = res.url
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

            <form onSubmit={handleSubmit}>
              <TextField
                fullWidth
                label="用户名"
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

            <Box sx={{ textAlign: 'center', mt: 2 }}>
              <Link
                component="button"
                type="button"
                variant="body2"
                onClick={() => navigate('/register')}
              >
                没有账号？立即注册
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
    </Box>
  )
}
