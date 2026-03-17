/**
 * 认证 Hook
 * 提供响应式的用户认证状态
 */

import { useState, useEffect, useCallback } from 'react'
import { authService } from '../services'
import type { User } from '../types'

interface UseAuthReturn {
  user: User | null
  token: string | null
  isAuthenticated: boolean
  isAdmin: boolean
  isApproved: boolean
  refresh: () => void
}

export function useAuth(): UseAuthReturn {
  const [user, setUser] = useState<User | null>(null)
  const [token, setToken] = useState<string | null>(null)

  // 刷新认证状态
  const refresh = useCallback(() => {
    setUser(authService.getStoredUser())
    setToken(authService.getToken())
  }, [])

  // 初始化时加载认证状态
  useEffect(() => {
    refresh()

    // 监听用户信息更新事件
    const handleUserUpdate = () => {
      refresh()
    }

    window.addEventListener('user-updated', handleUserUpdate)

    // 监听 storage 事件（其他标签页的更改）
    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === 'user' || e.key === 'token') {
        refresh()
      }
    }

    window.addEventListener('storage', handleStorageChange)

    return () => {
      window.removeEventListener('user-updated', handleUserUpdate)
      window.removeEventListener('storage', handleStorageChange)
    }
  }, [refresh])

  return {
    user,
    token,
    isAuthenticated: !!token,
    isAdmin: authService.isAdmin(),
    isApproved: authService.isApproved(),
    refresh,
  }
}

export default useAuth
