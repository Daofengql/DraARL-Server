import { apiClient } from './api'
import type {
  User,
  LoginRequest,
  RegisterRequest,
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

  // 保存认证信息
  saveAuth(token: string, user: User) {
    localStorage.setItem('token', token)
    localStorage.setItem('user', JSON.stringify(user))
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
}
