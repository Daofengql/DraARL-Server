import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { MainLayout } from './components/layout'
import { ProtectedRoute, AdminRoute, ToastContainer } from './components/common'
import { LoginPage, RegisterPage } from './pages/auth'
import { DashboardPage } from './pages/dashboard'
import { DevicesPage } from './pages/devices'
import { GroupsPage } from './pages/groups'
import { UsersPage, ApprovalsPage } from './pages/users'
import { CertificateApprovalsPage } from './pages/certificates'
import { RelaysPage } from './pages/relays'
import { ServersPage } from './pages/servers'
import { ProfilePage } from './pages/profile'
import { SiteConfigPage } from './pages/settings'
import { CommRecordsPage } from './pages/comm-records'
import { AdminLayout } from './components/layout/AdminLayout'
import { AdminDashboardPage } from './pages/admin/DashboardPage'
import { AdminDevicePage } from './pages/admin/DevicePage'
import { AdminGroupPage } from './pages/admin/GroupPage'
import { authService } from './services'

function App() {
  // 检查是否已登录
  const isAuthenticated = authService.isAuthenticated()
  const isAdmin = authService.isAdmin()

  return (
    <BrowserRouter>
      <Routes>
        {/* 公开路由 */}
        <Route
          path="/login"
          element={!isAuthenticated ? <LoginPage /> : <Navigate to="/" replace />}
        />
        <Route
          path="/register"
          element={!isAuthenticated ? <RegisterPage /> : <Navigate to="/" replace />}
        />

        {/* 普通用户路由（管理员和用户一样可见） */}
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <MainLayout />
            </ProtectedRoute>
          }
        >
          <Route index element={<DashboardPage />} />
          <Route path="devices" element={<DevicesPage />} />
          <Route path="groups" element={<GroupsPage />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route path="comm-records" element={<CommRecordsPage />} />
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
          <Route path="approvals" element={<ApprovalsPage />} />
          <Route path="certificate-approvals" element={<CertificateApprovalsPage />} />
          <Route path="relays" element={<RelaysPage />} />
          <Route path="servers" element={<ServersPage />} />
          <Route path="comm-records" element={<CommRecordsPage />} />
          <Route path="settings" element={<SiteConfigPage />} />
        </Route>

        {/* 404 */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>

      {/* 全局 Toast 通知 */}
      <ToastContainer />
    </BrowserRouter>
  )
}

export default App
