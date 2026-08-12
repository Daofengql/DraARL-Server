import axios, { type AxiosInstance, type AxiosRequestConfig, type AxiosResponse, type InternalAxiosRequestConfig, type AxiosError } from 'axios'

const BASE_URL = import.meta.env.VITE_API_URL || (typeof window !== 'undefined' ? window.location.origin : '')
const WS_TOKEN_CLEAR_PATH = '/api/auth/ws-token/clear'
const AUTH_REFRESH_PATH = '/api/auth/refresh'
const AUTH_REFRESH_LOCK_KEY = 'draarl:auth-refresh-lock'
const AUTH_REFRESH_LOCK_TTL_MS = 35000

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

interface RefreshResponseData {
  token: string
  expires_in?: number
  refresh_token?: string
  refresh_expires_in?: number
}

function resolveApiURL(path: string): string {
  if (!BASE_URL) {
    return path
  }

  try {
    const base = new URL(BASE_URL, window.location.origin)
    return new URL(path, base).toString()
  } catch {
    const trimmedBase = BASE_URL.endsWith('/') ? BASE_URL.slice(0, -1) : BASE_URL
    return `${trimmedBase}${path}`
  }
}

function clearWSTokenCookieSilently() {
  void fetch(resolveApiURL(WS_TOKEN_CLEAR_PATH), {
    method: 'POST',
    credentials: 'include',
    keepalive: true,
  }).catch(() => undefined)
}

function clearLocalAuthState() {
  localStorage.removeItem('token')
  localStorage.removeItem('user')
}

class ApiClient {
  private client: AxiosInstance
  private refreshClient: AxiosInstance
  private refreshPromise: Promise<boolean> | null = null

  constructor() {
    this.client = axios.create({
      baseURL: BASE_URL,
      timeout: 30000,
      withCredentials: true,
      headers: {
        'Content-Type': 'application/json',
      },
    })
    this.refreshClient = axios.create({
      baseURL: BASE_URL,
      timeout: 30000,
      withCredentials: true,
      headers: {
        'Content-Type': 'application/json',
      },
    })

    // 请求拦截器 - 添加 token
    this.client.interceptors.request.use(
      (config: InternalAxiosRequestConfig) => {
        const token = localStorage.getItem('token')
        if (token && config.headers) {
          config.headers.Authorization = `Bearer ${token}`
        }
        return config
      },
      (error: AxiosError) => {
        return Promise.reject(error)
      }
    )

    // 响应拦截器 - 处理错误
    this.client.interceptors.response.use(
      (response: AxiosResponse) => response,
      async (error: AxiosError) => {
        const status = error.response?.status
        const originalConfig = error.config as (InternalAxiosRequestConfig & { _retry?: boolean }) | undefined

        if (status === 401 && originalConfig && !originalConfig._retry && !this.shouldSkipRefresh(originalConfig.url)) {
          originalConfig._retry = true
          const refreshed = await this.tryRefreshToken()
          if (refreshed) {
            const token = localStorage.getItem('token')
            if (token && originalConfig.headers) {
              originalConfig.headers.Authorization = `Bearer ${token}`
            }
            return this.client.request(originalConfig)
          }
        }

        if (status === 401 && !this.shouldSkipRefresh(originalConfig?.url)) {
          clearLocalAuthState()
          clearWSTokenCookieSilently()
          if (window.location.pathname !== '/login') {
            window.location.href = '/login'
          }
        }
        return Promise.reject(error)
      }
    )
  }

  private shouldSkipRefresh(url?: string): boolean {
    if (!url) return false
    const normalized = url.toLowerCase()

    return normalized.includes('/api/auth/login') ||
      normalized.includes('/api/auth/email-login') ||
      normalized.includes('/api/auth/send-code') ||
      normalized.includes('/api/auth/verify-email') ||
      normalized.includes('/api/auth/reset-password') ||
      normalized.includes('/api/auth/register') ||
      normalized.includes('/api/auth/logout') ||
      normalized.includes('/api/sso/exchange') ||
      normalized.includes('/api/auth/refresh')
  }

  private async tryRefreshToken(): Promise<boolean> {
    if (this.refreshPromise) {
      return this.refreshPromise
    }

    const previousToken = localStorage.getItem('token')
    this.refreshPromise = this.refreshAcrossTabs(previousToken).finally(() => {
      this.refreshPromise = null
    })
    return this.refreshPromise
  }

  private async refreshAcrossTabs(previousToken: string | null): Promise<boolean> {
    if (typeof navigator !== 'undefined' && 'locks' in navigator) {
      const locks = navigator.locks
      let result = false
      await locks.request('draarl-auth-refresh', async () => {
        if (localStorage.getItem('token') !== previousToken) {
          window.dispatchEvent(new CustomEvent('user-updated'))
          result = true
          return
        }
        result = await this.refreshWithServer()
      })
      return result
    }

    const owner = this.createRefreshLockOwner()
    const acquired = await this.acquireRefreshLock(owner, previousToken)
    if (!acquired) {
      return localStorage.getItem('token') !== previousToken
    }

    try {
      // Another tab may have completed the rotation while this tab was
      // waiting for the lock. Reuse its access token instead of rotating again.
      if (localStorage.getItem('token') !== previousToken) {
        window.dispatchEvent(new CustomEvent('user-updated'))
        return true
      }

      return await this.refreshWithServer()
    } catch {
      return false
    } finally {
      this.releaseRefreshLock(owner)
    }
  }

  private async refreshWithServer(): Promise<boolean> {
    try {
      const response = await this.refreshClient.post<BackendResponse<RefreshResponseData>>(AUTH_REFRESH_PATH, {})
      const token = response.data?.data?.token
      if (!token) return false
      localStorage.setItem('token', token)
      window.dispatchEvent(new CustomEvent('user-updated'))
      return true
    } catch {
      return false
    }
  }

  private createRefreshLockOwner(): string {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID()
    }
    return `${Date.now()}-${Math.random().toString(36).slice(2)}`
  }

  private readRefreshLock(): { owner: string; expiresAt: number } | null {
    try {
      const raw = localStorage.getItem(AUTH_REFRESH_LOCK_KEY)
      if (!raw) return null
      const parsed = JSON.parse(raw) as { owner?: unknown; expiresAt?: unknown }
      if (typeof parsed.owner !== 'string' || typeof parsed.expiresAt !== 'number') return null
      if (parsed.expiresAt <= Date.now()) return null
      return { owner: parsed.owner, expiresAt: parsed.expiresAt }
    } catch {
      return null
    }
  }

  private async acquireRefreshLock(owner: string, previousToken: string | null): Promise<boolean> {
    const deadline = Date.now() + AUTH_REFRESH_LOCK_TTL_MS - 1000
    while (Date.now() < deadline) {
      if (localStorage.getItem('token') !== previousToken) {
        window.dispatchEvent(new CustomEvent('user-updated'))
        return false
      }

      const current = this.readRefreshLock()
      if (current && current.owner !== owner) {
        await this.delay(50)
        continue
      }

      try {
        localStorage.setItem(AUTH_REFRESH_LOCK_KEY, JSON.stringify({
          owner,
          expiresAt: Date.now() + AUTH_REFRESH_LOCK_TTL_MS,
        }))
        const confirmed = this.readRefreshLock()
        if (confirmed?.owner === owner) return true
      } catch {
        // Private browsing or restricted storage: fall back to local locking.
        return true
      }
      await this.delay(25)
    }
    return false
  }

  private releaseRefreshLock(owner: string): void {
    try {
      if (this.readRefreshLock()?.owner === owner) {
        localStorage.removeItem(AUTH_REFRESH_LOCK_KEY)
      }
    } catch {
      // Ignore storage cleanup failures; the short lease will expire.
    }
  }

  private delay(ms: number): Promise<void> {
    return new Promise(resolve => window.setTimeout(resolve, ms))
  }

  async get<T = any>(url: string, config?: AxiosRequestConfig): Promise<T> {
    const response: AxiosResponse<T> = await this.client.get<T>(url, config)
    return response.data
  }

  async post<T = any>(url: string, data?: any, config?: AxiosRequestConfig): Promise<T> {
    const response: AxiosResponse<T> = await this.client.post<T>(url, data, config)
    return response.data
  }

  async put<T = any>(url: string, data?: any, config?: AxiosRequestConfig): Promise<T> {
    const response: AxiosResponse<T> = await this.client.put<T>(url, data, config)
    return response.data
  }

  async patch<T = any>(url: string, data?: any, config?: AxiosRequestConfig): Promise<T> {
    const response: AxiosResponse<T> = await this.client.patch<T>(url, data, config)
    return response.data
  }

  async delete<T = any>(url: string, config?: AxiosRequestConfig): Promise<T> {
    const response: AxiosResponse<T> = await this.client.delete<T>(url, config)
    return response.data
  }

  async postFormData<T = any>(url: string, formData: FormData, config?: AxiosRequestConfig): Promise<T> {
    const response: AxiosResponse<T> = await this.client.post<T>(url, formData, {
      ...config,
      headers: {
        ...config?.headers,
        'Content-Type': 'multipart/form-data',
      },
    })
    return response.data
  }
}

export const apiClient = new ApiClient()
