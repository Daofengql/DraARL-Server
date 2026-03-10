// API 基础配置
const API_BASE = '/api'

// 请求配置
interface RequestConfig {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  headers?: Record<string, string>
  body?: any
}

// API 响应
interface ApiResponse<T = any> {
  code: number
  message: string
  data?: T
}

// 获取 token
function getToken(): string | null {
  return localStorage.getItem('token')
}

// 设置 token
function setToken(token: string): void {
  localStorage.setItem('token', token)
}

// 移除 token
function removeToken(): void {
  localStorage.removeItem('token')
}

// 通用请求函数
async function request<T = any>(url: string, config: RequestConfig = {}): Promise<ApiResponse<T>> {
  const token = getToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...config.headers,
  }

  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const response = await fetch(`${API_BASE}${url}`, {
    method: config.method || 'GET',
    headers,
    body: config.body ? JSON.stringify(config.body) : undefined,
  })

  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`)
  }

  return response.json()
}

export { request, getToken, setToken, removeToken }
export type { ApiResponse, RequestConfig }
