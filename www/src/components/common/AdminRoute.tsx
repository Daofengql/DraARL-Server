import { Navigate } from 'react-router-dom'
import { Box, Typography } from '@mui/material'
import { authService } from '../../services'

interface AdminRouteProps {
  children: React.ReactNode
}

export function AdminRoute({ children }: AdminRouteProps) {
  const isAuthenticated = authService.isAuthenticated()
  const isAdmin = authService.isAdmin()

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  if (!isAdmin) {
    return (
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100vh',
          textAlign: 'center',
          px: 3,
        }}
      >
        <Typography variant="h4" gutterBottom color="error">
          访问被拒绝
        </Typography>
        <Typography variant="body1" color="text.secondary">
          您没有权限访问此页面，该页面需要管理员权限！
        </Typography>
      </Box>
    )
  }

  return <>{children}</>
}
