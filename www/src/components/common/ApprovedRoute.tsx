import { Navigate } from 'react-router-dom'
import { authService } from '../../services'

interface ApprovedRouteProps {
  children: React.ReactNode
}

// ApprovedRoute 审核状态路由守卫
// 用于限制未审核用户访问某些页面（如设备管理、群组管理、在线收发等）
// 未审核用户会被重定向到仪表盘页面
export function ApprovedRoute({ children }: ApprovedRouteProps) {
  const isAuthenticated = authService.isAuthenticated()
  const isApproved = authService.isApproved()

  // 未登录则跳转到登录页
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  // 未审核通过则跳转到仪表盘
  if (!isApproved) {
    return <Navigate to="/dashboard" replace />
  }

  return <>{children}</>
}
