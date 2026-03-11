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
import { LogsPage } from './pages/logs'
import { ProfilePage } from './pages/profile'
import { SiteConfigPage } from './pages/settings'
import { authService } from './services'

function App() {
  // 检查是否已登录
  const isAuthenticated = authService.isAuthenticated()

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

        {/* 受保护路由 */}
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <MainLayout />
            </ProtectedRoute>
          }
        >
          {/* 普通用户可访问的路由 */}
          <Route index element={<DashboardPage />} />
          <Route path="devices" element={<DevicesPage />} />
          <Route path="groups" element={<GroupsPage />} />
          <Route path="profile" element={<ProfilePage />} />

          {/* 需要管理员权限的路由 */}
          <Route
            path="users"
            element={
              <AdminRoute>
                <UsersPage />
              </AdminRoute>
            }
          />
          <Route
            path="approvals"
            element={
              <AdminRoute>
                <ApprovalsPage />
              </AdminRoute>
            }
          />
          <Route
            path="certificate-approvals"
            element={
              <AdminRoute>
                <CertificateApprovalsPage />
              </AdminRoute>
            }
          />
          <Route
            path="relays"
            element={
              <AdminRoute>
                <RelaysPage />
              </AdminRoute>
            }
          />
          <Route
            path="servers"
            element={
              <AdminRoute>
                <ServersPage />
              </AdminRoute>
            }
          />
          <Route
            path="logs"
            element={
              <AdminRoute>
                <LogsPage />
              </AdminRoute>
            }
          />
          <Route
            path="settings"
            element={
              <AdminRoute>
                <SiteConfigPage />
              </AdminRoute>
            }
          />
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
