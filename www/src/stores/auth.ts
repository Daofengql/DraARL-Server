import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { request, getToken, setToken, removeToken } from '../api'

interface User {
  id: number
  username: string
  nickname: string
  isAdmin: boolean
  status: number
}

interface LoginData {
  username: string
  password: string
}

interface RegisterData {
  username: string
  password: string
  nickname?: string
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const user = ref<User | null>(null)
  const loading = ref(false)

  const isAuthenticated = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.isAdmin ?? false)

  // 登录
  async function login(data: LoginData) {
    loading.value = true
    try {
      const response = await request<{ token: string; user: User }>('/auth/login', {
        method: 'POST',
        body: data,
      })

      if (response.code === 200 && response.data) {
        token.value = response.data.token
        user.value = response.data.user
        setToken(response.data.token)
        return { success: true }
      }

      return { success: false, message: response.message }
    } catch (error: any) {
      return { success: false, message: error.message || '登录失败' }
    } finally {
      loading.value = false
    }
  }

  // 注册
  async function register(data: RegisterData) {
    loading.value = true
    try {
      const response = await request<{ id: number; username: string; nickname: string }>('/auth/register', {
        method: 'POST',
        body: data,
      })

      if (response.code === 201 || response.code === 200) {
        return { success: true, data: response.data }
      }

      return { success: false, message: response.message }
    } catch (error: any) {
      return { success: false, message: error.message || '注册失败' }
    } finally {
      loading.value = false
    }
  }

  // 获取当前用户信息
  async function fetchUser() {
    if (!token.value) return

    try {
      const response = await request<User>('/me')
      if (response.code === 200 && response.data) {
        user.value = response.data
      }
    } catch (error) {
      // Token 可能过期，清除本地数据
      logout()
    }
  }

  // 登出
  async function logout() {
    try {
      await request('/auth/logout', { method: 'POST' })
    } catch (error) {
      // 忽略错误
    } finally {
      token.value = null
      user.value = null
      removeToken()
    }
  }

  return {
    token,
    user,
    loading,
    isAuthenticated,
    isAdmin,
    login,
    register,
    fetchUser,
    logout,
  }
})
