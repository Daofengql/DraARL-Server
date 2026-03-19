import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { CircularProgress, Typography, Box } from '@mui/material'
import { authService } from '../../services'
import { usePageTitle } from '../../hooks/usePageTitle'

export function SSOCallbackPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [error, setError] = useState('')

  // 同步页面标题
  usePageTitle()

  useEffect(() => {
    // 新流程：后端直接重定向过来，带有 token 和 user 参数
    const token = searchParams.get('token')
    const userStr = searchParams.get('user')
    const ssoError = searchParams.get('sso_error')

    // 处理错误
    if (ssoError) {
      setError(ssoError)
      setTimeout(() => navigate('/login'), 3000)
      return
    }

    // 处理成功回调
    if (token && userStr) {
      try {
        const user = JSON.parse(decodeURIComponent(userStr))
        authService.saveAuth(token, user)
        navigate('/dashboard')
        return
      } catch (e) {
        setError('解析用户信息失败')
        setTimeout(() => navigate('/login'), 3000)
        return
      }
    }

    // 如果没有必要参数，跳转登录页
    if (!token && !ssoError) {
      navigate('/login')
    }
  }, [searchParams, navigate])

  if (error) {
    return (
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: '100vh',
        }}
      >
        <Typography color="error">{error}</Typography>
        <Typography variant="body2" sx={{ mt: 1 }}>
          3秒后跳转登录页...
        </Typography>
      </Box>
    )
  }

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: '100vh',
      }}
    >
      <CircularProgress />
      <Typography sx={{ mt: 2 }}>正在登录...</Typography>
    </Box>
  )
}
