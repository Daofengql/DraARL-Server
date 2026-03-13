import { apiClient } from './api'
import type {
  User,
  LoginRequest,
  RegisterRequest,
  FileUploadResponse,
  CertificateResponse,
  OperatorCertificateUpload,
} from '../types'

interface LoginResponse {
  token: string
  user: User
}

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export const authService = {
  // 用户登录
  async login(data: LoginRequest): Promise<LoginResponse> {
    const res = await apiClient.post<BackendResponse<LoginResponse>>('/api/auth/login', data)
    return res.data!
  },

  // 用户登出
  async logout(): Promise<void> {
    return apiClient.post('/api/auth/logout')
  },

  // 用户注册
  async register(data: RegisterRequest): Promise<void> {
    return apiClient.post('/api/auth/register', data)
  },

  // 获取当前用户信息
  async getMe(): Promise<User> {
    const res = await apiClient.get<BackendResponse<User>>('/api/me')
    return res.data!
  },

  // 更新个人资料
  async updateProfile(data: Partial<User>): Promise<User> {
    const res = await apiClient.put<BackendResponse<User>>('/api/me', data)
    return res.data!
  },

  // 修改自己的密码
  async changeOwnPassword(data: { old_password: string; new_password: string }): Promise<void> {
    await apiClient.put('/api/me/password', data)
  },

  // 上传文件（通用，用于头像等）
  async uploadFile(file: File, fileType: string): Promise<FileUploadResponse> {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('file_type', fileType)
    const res = await apiClient.post<BackendResponse<FileUploadResponse>>('/api/upload/file', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
    return res.data!
  },

  // 上传操作证（file 可选，用于纯呼号更新场景）
  async uploadOperatorCertificate(file?: File, callsign?: string): Promise<OperatorCertificateUpload> {
    const formData = new FormData()
    if (file) {
      formData.append('file', file)
    }
    if (callsign) {
      formData.append('callsign', callsign)
    }
    const res = await apiClient.post<BackendResponse<OperatorCertificateUpload>>('/api/upload/operator-certificate', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
    return res.data!
  },

  // 获取操作证信息（返回 active_cert 和 pending_cert）
  async getOperatorCertificate(): Promise<CertificateResponse> {
    const res = await apiClient.get<BackendResponse<CertificateResponse>>('/api/operator-certificate')
    return res.data || { active_cert: null, pending_cert: null }
  },

  // 获取操作证临时访问URL
  async getOperatorCertificateUrl(id: number): Promise<{ url: string; expires_in: number }> {
    const res = await apiClient.get<BackendResponse<{ url: string; expires_in: number }>>(`/api/operator-certificate/${id}/url`)
    return res.data!
  },

  // 保存认证信息
  saveAuth(token: string, user: User) {
    localStorage.setItem('token', token)
    localStorage.setItem('user', JSON.stringify(user))
    // 触发自定义事件，通知其他组件用户信息已更新
    window.dispatchEvent(new CustomEvent('user-updated'))
  },

  // 清除认证信息
  clearAuth() {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
  },

  // 获取存储的用户信息
  getStoredUser(): User | null {
    const userStr = localStorage.getItem('user')
    if (userStr) {
      try {
        return JSON.parse(userStr)
      } catch {
        return null
      }
    }
    return null
  },

  // 获取存储的 token
  getToken(): string | null {
    return localStorage.getItem('token')
  },

  // 检查是否已登录
  isAuthenticated(): boolean {
    return !!this.getToken()
  },

  // 检查是否是管理员
  isAdmin(): boolean {
    const user = this.getStoredUser()
    if (!user) return false

    // 检查 isAdmin 字段
    if (user.isAdmin === true) return true

    // 检查 roles 数组是否包含 admin
    if (user.roles && Array.isArray(user.roles)) {
      return user.roles.includes('admin')
    }

    // 检查 role 字段
    if (user.role === 'admin') return true

    return false
  },

  // 检查用户是否已审核通过
  isApproved(): boolean {
    const user = this.getStoredUser()
    if (!user) return false
    // 管理员总是可以通过
    if (this.isAdmin()) return true
    // 检查审核状态：1=已通过
    return user.approval_status === 1
  },
}
