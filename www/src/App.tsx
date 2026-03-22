import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { MainLayout } from './components/layout'
import { ProtectedRoute, AdminRoute, ApprovedRoute, ToastContainer } from './components/common'
import { AdminLayout } from './components/layout/AdminLayout'
import { authService } from './services'

// 静态导入页面组件
import { LoginPage } from './pages/auth/LoginPage'
import { RegisterPage } from './pages/auth/RegisterPage'
import { ForgotPasswordPage } from './pages/auth/ForgotPasswordPage'
import { SSOCallbackPage } from './pages/auth/SSOCallbackPage'
import { HomePage } from './pages/home/HomePage'
import { DashboardPage } from './pages/dashboard/DashboardPage'
import { DevicesPage } from './pages/devices/DevicesPage'
import { GroupsPage } from './pages/groups/GroupsPage'
import { UsersPage } from './pages/users/UsersPage'
import { ApprovalsPage } from './pages/users/ApprovalsPage'
import { CertificateApprovalsPage } from './pages/certificates/CertificateApprovalsPage'
import { RelaysPage } from './pages/relays/RelaysPage'
import { ServersPage } from './pages/servers/ServersPage'
import { ProfilePage } from './pages/profile/ProfilePage'
import { SiteConfigPage } from './pages/settings/SiteConfigPage'
import { CommRecordsPage } from './pages/comm-records/CommRecordsPage'
import { NotFoundPage } from './pages/not-found/NotFoundPage'
import { DocsPage } from './pages/docs/DocsPage'
import { RadioPage } from './pages/radio/RadioPage'
import { AdminDashboardPage } from './pages/admin/DashboardPage'
import { AdminDevicePage } from './pages/admin/DevicePage'
import { AdminGroupPage } from './pages/admin/GroupPage'
import { GroupLinkPage } from './pages/admin/GroupLinkPage'
import { AssetPage } from './pages/admin/AssetPage'

function App() {
  // 检查是否已登录
  const isAuthenticated = authService.isAuthenticated()

  return (
    <BrowserRouter>
      <Routes>
        {/* 公开路由 - 首页 */}
        <Route path="/" element={<HomePage />} />

        {/* 公开路由 - 登录/注册 */}
        <Route
          path="/login"
          element={!isAuthenticated ? <LoginPage /> : <Navigate to="/dashboard" replace />}
        />
        <Route
          path="/register"
          element={!isAuthenticated ? <RegisterPage /> : <Navigate to="/dashboard" replace />}
        />
        <Route path="/sso/callback" element={<SSOCallbackPage />} />
        <Route path="/forgot-password" element={<ForgotPasswordPage />} />

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
          <Route path="dashboard" element={<DashboardPage />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route path="docs" element={<DocsPage />} />

          {/* 需要审核通过才能访问的页面 */}
          <Route
            path="devices"
            element={
              <ApprovedRoute>
                <DevicesPage />
              </ApprovedRoute>
            }
          />
          <Route
            path="groups"
            element={
              <ApprovedRoute>
                <GroupsPage />
              </ApprovedRoute>
            }
          />
          <Route
            path="comm-records"
            element={
              <ApprovedRoute>
                <CommRecordsPage />
              </ApprovedRoute>
            }
          />
          <Route
            path="radio"
            element={
              <ApprovedRoute>
                <RadioPage />
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
          <Route path="dashboard" element={<AdminDashboardPage />} />
          <Route path="users" element={<UsersPage />} />
          <Route path="devices" element={<AdminDevicePage />} />
          <Route path="groups" element={<AdminGroupPage />} />
          <Route path="group-links" element={<GroupLinkPage />} />
          <Route path="approvals" element={<ApprovalsPage />} />
          <Route path="certificate-approvals" element={<CertificateApprovalsPage />} />
          <Route path="relays" element={<RelaysPage />} />
          <Route path="servers" element={<ServersPage />} />
          <Route path="comm-records" element={<CommRecordsPage />} />
          <Route path="assets" element={<AssetPage />} />
          <Route path="settings" element={<SiteConfigPage />} />
        </Route>

        {/* 404 - 已登录用户显示带布局的404页面 */}
        <Route
          path="*"
          element={
            isAuthenticated ? (
              <MainLayout>
                <NotFoundPage />
              </MainLayout>
            ) : (
              <NotFoundPage />
            )
          }
        />
      </Routes>

      {/* 全局 Toast 通知 */}
      <ToastContainer />
    </BrowserRouter>
  )
}

export default App
