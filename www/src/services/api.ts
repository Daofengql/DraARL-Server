import axios, { type AxiosInstance, type AxiosRequestConfig, type AxiosResponse, type InternalAxiosRequestConfig, type AxiosError } from 'axios'

const BASE_URL = import.meta.env.VITE_API_URL || (typeof window !== 'undefined' ? window.location.origin : '')
const WS_TOKEN_CLEAR_PATH = '/api/auth/ws-token/clear'
const AUTH_REFRESH_PATH = '/api/auth/refresh'

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

    this.refreshPromise = (async () => {
      try {
        const response = await this.refreshClient.post<BackendResponse<RefreshResponseData>>(AUTH_REFRESH_PATH, {})
        const token = response.data?.data?.token
        if (!token) {
          return false
        }

        localStorage.setItem('token', token)
        window.dispatchEvent(new CustomEvent('user-updated'))
        return true
      } catch {
        return false
      } finally {
        this.refreshPromise = null
      }
    })()

    return this.refreshPromise
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
