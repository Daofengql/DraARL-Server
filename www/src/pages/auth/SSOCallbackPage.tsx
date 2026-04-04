import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { CircularProgress, Typography, Box } from '@mui/material'
import { authService, ssoService } from '../../services'
import { usePageTitle } from '../../hooks/usePageTitle'

export function SSOCallbackPage() {
  const [searchParams] = useSearchParams()
  const [error, setError] = useState('')

  // 同步页面标题
  usePageTitle()

  useEffect(() => {
    // 新流程：后端重定向携带一次性交换码，前端再向后端换取登录态
    const code = searchParams.get('code')
    const ssoError = searchParams.get('sso_error')

    // 立即清理地址栏参数，避免 code/sso_error 留在浏览器历史或被前端埋点采集
    if ((code || ssoError) && window.location.search) {
      window.history.replaceState({}, document.title, window.location.pathname)
    }

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

    // 处理成功回调（交换码换取 token + user）
    if (code) {
      ssoService.exchangeCode(code)
        .then(({ token, user }) => {
          if (isPopup) {
            window.opener.postMessage(
              {
                type: 'SSO_LOGIN_SUCCESS',
                token,
                user,
              },
              window.location.origin
            )
            setTimeout(() => window.close(), 100)
            return
          }

          authService.saveAuth(token, user)
          window.location.href = '/dashboard'
        })
        .catch((err: any) => {
          const message = err?.response?.data?.message || 'SSO 登录数据交换失败'
          if (isPopup) {
            window.opener.postMessage(
              {
                type: 'SSO_LOGIN_ERROR',
                error: message,
              },
              window.location.origin
            )
            setTimeout(() => window.close(), 100)
            return
          }
          setError(message)
          setTimeout(() => (window.location.href = '/login'), 3000)
        })
      return
    }

    // 如果没有必要参数，跳转登录页
    if (!code && !ssoError) {
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
