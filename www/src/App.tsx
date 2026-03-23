import { lazy, Suspense } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { MainLayout } from './components/layout'
import { ProtectedRoute, AdminRoute, ApprovedRoute, ToastContainer, PageLoader } from './components/common'
import { AdminLayout } from './components/layout/AdminLayout'
import { authService } from './services'
import { ConfigProvider } from './contexts/ConfigContext'

// 路由懒加载 - 按页面分割代码
const LoginPage = lazy(() => import('./pages/auth/LoginPage').then(m => ({ default: m.LoginPage })))
const RegisterPage = lazy(() => import('./pages/auth/RegisterPage').then(m => ({ default: m.RegisterPage })))
const ForgotPasswordPage = lazy(() => import('./pages/auth/ForgotPasswordPage').then(m => ({ default: m.ForgotPasswordPage })))
const SSOCallbackPage = lazy(() => import('./pages/auth/SSOCallbackPage').then(m => ({ default: m.SSOCallbackPage })))
const HomePage = lazy(() => import('./pages/home/HomePage').then(m => ({ default: m.HomePage })))
const DashboardPage = lazy(() => import('./pages/dashboard/DashboardPage').then(m => ({ default: m.DashboardPage })))
const DevicesPage = lazy(() => import('./pages/devices/DevicesPage').then(m => ({ default: m.DevicesPage })))
const GroupsPage = lazy(() => import('./pages/groups/GroupsPage').then(m => ({ default: m.GroupsPage })))
const UsersPage = lazy(() => import('./pages/users/UsersPage').then(m => ({ default: m.UsersPage })))
const ApprovalsPage = lazy(() => import('./pages/users/ApprovalsPage').then(m => ({ default: m.ApprovalsPage })))
const CertificateApprovalsPage = lazy(() => import('./pages/certificates/CertificateApprovalsPage').then(m => ({ default: m.CertificateApprovalsPage })))
const RelaysPage = lazy(() => import('./pages/relays/RelaysPage').then(m => ({ default: m.RelaysPage })))
const ServersPage = lazy(() => import('./pages/servers/ServersPage').then(m => ({ default: m.ServersPage })))
const ProfilePage = lazy(() => import('./pages/profile/ProfilePage').then(m => ({ default: m.ProfilePage })))
const SiteConfigPage = lazy(() => import('./pages/settings/SiteConfigPage').then(m => ({ default: m.SiteConfigPage })))
const CommRecordsPage = lazy(() => import('./pages/comm-records/CommRecordsPage').then(m => ({ default: m.CommRecordsPage })))
const NotFoundPage = lazy(() => import('./pages/not-found/NotFoundPage').then(m => ({ default: m.NotFoundPage })))
const DocsPage = lazy(() => import('./pages/docs/DocsPage').then(m => ({ default: m.DocsPage })))
const RadioPage = lazy(() => import('./pages/radio/RadioPage').then(m => ({ default: m.RadioPage })))
const AdminDashboardPage = lazy(() => import('./pages/admin/DashboardPage').then(m => ({ default: m.AdminDashboardPage })))
const AdminDevicePage = lazy(() => import('./pages/admin/DevicePage').then(m => ({ default: m.AdminDevicePage })))
const AdminGroupPage = lazy(() => import('./pages/admin/GroupPage').then(m => ({ default: m.AdminGroupPage })))
const GroupLinkPage = lazy(() => import('./pages/admin/GroupLinkPage').then(m => ({ default: m.GroupLinkPage })))
const AssetPage = lazy(() => import('./pages/admin/AssetPage').then(m => ({ default: m.AssetPage })))

// 加载指示器包装组件
const PageSuspense: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <Suspense fallback={<PageLoader />}>{children}</Suspense>
)

function App() {
  // 检查是否已登录
  const isAuthenticated = authService.isAuthenticated()

  return (
    <BrowserRouter>
      <ConfigProvider>
        <Routes>
        {/* 公开路由 - 首页 */}
        <Route path="/" element={<PageSuspense><HomePage /></PageSuspense>} />

        {/* 公开路由 - 登录/注册 */}
        <Route
          path="/login"
          element={!isAuthenticated ? <PageSuspense><LoginPage /></PageSuspense> : <Navigate to="/dashboard" replace />}
        />
        <Route
          path="/register"
          element={!isAuthenticated ? <PageSuspense><RegisterPage /></PageSuspense> : <Navigate to="/dashboard" replace />}
        />
        <Route path="/sso/callback" element={<PageSuspense><SSOCallbackPage /></PageSuspense>} />
        <Route path="/forgot-password" element={<PageSuspense><ForgotPasswordPage /></PageSuspense>} />

        {/* 普通用户路由（管理员和用户一样可见） */}
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <MainLayout />
            </ProtectedRoute>
          }
        >
          {/* 无需审核即可访问的页面 */}
          <Route path="dashboard" element={<PageSuspense><DashboardPage /></PageSuspense>} />
          <Route path="profile" element={<PageSuspense><ProfilePage /></PageSuspense>} />
          <Route path="docs" element={<PageSuspense><DocsPage /></PageSuspense>} />

          {/* 需要审核通过才能访问的页面 */}
          <Route
            path="devices"
            element={
              <ApprovedRoute>
                <PageSuspense><DevicesPage /></PageSuspense>
              </ApprovedRoute>
            }
          />
          <Route
            path="groups"
            element={
              <ApprovedRoute>
                <PageSuspense><GroupsPage /></PageSuspense>
              </ApprovedRoute>
            }
          />
          <Route
            path="comm-records"
            element={
              <ApprovedRoute>
                <PageSuspense><CommRecordsPage /></PageSuspense>
              </ApprovedRoute>
            }
          />
          <Route
            path="radio"
            element={
              <ApprovedRoute>
                <PageSuspense><RadioPage /></PageSuspense>
              </ApprovedRoute>
            }
          />
        </Route>

        {/* 管理员专用路由 /admin */}
        <Route
          path="/admin"
          element={
            <ProtectedRoute>
              <AdminRoute>
                <AdminLayout />
              </AdminRoute>
            </ProtectedRoute>
          }
        >
          <Route index element={<Navigate to="/admin/dashboard" replace />} />
          <Route path="dashboard" element={<PageSuspense><AdminDashboardPage /></PageSuspense>} />
          <Route path="users" element={<PageSuspense><UsersPage /></PageSuspense>} />
          <Route path="devices" element={<PageSuspense><AdminDevicePage /></PageSuspense>} />
          <Route path="groups" element={<PageSuspense><AdminGroupPage /></PageSuspense>} />
          <Route path="group-links" element={<PageSuspense><GroupLinkPage /></PageSuspense>} />
          <Route path="approvals" element={<PageSuspense><ApprovalsPage /></PageSuspense>} />
          <Route path="certificate-approvals" element={<PageSuspense><CertificateApprovalsPage /></PageSuspense>} />
          <Route path="relays" element={<PageSuspense><RelaysPage /></PageSuspense>} />
          <Route path="servers" element={<PageSuspense><ServersPage /></PageSuspense>} />
          <Route path="comm-records" element={<PageSuspense><CommRecordsPage /></PageSuspense>} />
          <Route path="assets" element={<PageSuspense><AssetPage /></PageSuspense>} />
          <Route path="settings" element={<PageSuspense><SiteConfigPage /></PageSuspense>} />
        </Route>

        {/* 404 - 已登录用户显示带布局的404页面 */}
        <Route
          path="*"
          element={
            isAuthenticated ? (
              <MainLayout>
                <PageSuspense><NotFoundPage /></PageSuspense>
              </MainLayout>
            ) : (
              <PageSuspense><NotFoundPage /></PageSuspense>
            )
          }
        />
      </Routes>

      {/* 全局 Toast 通知 */}
      <ToastContainer />
      </ConfigProvider>
    </BrowserRouter>
  )
}

export default App
