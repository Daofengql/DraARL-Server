import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { CircularProgress, Typography, Box } from '@mui/material'
import { authService } from '../../services'
import { usePageTitle } from '../../hooks/usePageTitle'

export function SSOCallbackPage() {
  const [searchParams] = useSearchParams()
  const [error, setError] = useState('')

  // 同步页面标题
  usePageTitle()

  useEffect(() => {
    // 新流程：后端直接重定向过来，带有 token 和 user 参数
    const token = searchParams.get('token')
    const userStr = searchParams.get('user')
    const ssoError = searchParams.get('sso_error')

    // 检查是否在弹出窗口中（有 opener）
    const isPopup = window.opener && window.opener !== window

    // 处理错误
    if (ssoError) {
      if (isPopup) {
        // 通过 postMessage 通知父窗口
        window.opener.postMessage(
          {
            type: 'SSO_LOGIN_ERROR',
            error: ssoError,
          },
          window.location.origin
        )
        // 关闭弹出窗口
        setTimeout(() => window.close(), 100)
        return
      } else {
        // 不是弹出窗口，显示错误并跳转
        setError(ssoError)
        setTimeout(() => (window.location.href = '/login'), 3000)
        return
      }
    }

    // 处理成功回调
    if (token && userStr) {
      try {
        const user = JSON.parse(decodeURIComponent(userStr))
        if (isPopup) {
          // 通过 postMessage 通知父窗口
          window.opener.postMessage(
            {
              type: 'SSO_LOGIN_SUCCESS',
              token,
              user,
            },
            window.location.origin
          )
          // 关闭弹出窗口
          setTimeout(() => window.close(), 100)
          return
        } else {
          // 不是弹出窗口，直接保存并跳转
          authService.saveAuth(token, user)
          window.location.href = '/dashboard'
          return
        }
      } catch (e) {
        if (isPopup) {
          window.opener.postMessage(
            {
              type: 'SSO_LOGIN_ERROR',
              error: '解析用户信息失败',
            },
            window.location.origin
          )
          setTimeout(() => window.close(), 100)
          return
        } else {
          setError('解析用户信息失败')
          setTimeout(() => (window.location.href = '/login'), 3000)
          return
        }
      }
    }

    // 如果没有必要参数，跳转登录页
    if (!token && !ssoError) {
      if (isPopup) {
        window.close()
      } else {
        window.location.href = '/login'
      }
    }
  }, [searchParams])

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
