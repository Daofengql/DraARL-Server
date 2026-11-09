interface ResponseMessagePayload {
  response?: {
    status?: number
    headers?: Record<string, unknown>
    data?: {
      message?: string
      data?: { retry_after?: number }
    }
  }
  message?: string
  code?: string
}

export function getRetryAfterSeconds(error: unknown): number {
  if (!error || typeof error !== 'object') return 0
  const response = (error as ResponseMessagePayload).response
  if (response?.status !== 429) return 0
  const seconds = Number(response.headers?.['retry-after'] ?? response.data?.data?.retry_after ?? 5)
  return Number.isFinite(seconds) ? Math.min(3600, Math.max(1, Math.ceil(seconds))) : 5
}

export function getErrorMessage(error: unknown, fallback: string): string {
  if (!error || typeof error !== 'object') {
    return fallback
  }

  const maybeError = error as ResponseMessagePayload
  if (maybeError.code === 'ECONNABORTED' || maybeError.code === 'ETIMEDOUT') return '请求超时，请稍后重试'
  if (maybeError.code === 'ERR_NETWORK') return '网络连接失败，请检查网络后重试'
  const status = maybeError.response?.status
  if (status === 429) return '操作过于频繁，请稍后重试'
  const message = maybeError.response?.data?.message || maybeError.message
  if (typeof message !== 'string' || !message.trim()) return fallback
  // Avoid exposing SQL, stack traces, filesystem paths or transport errors.
  if (/sqlstate|foreign key|constraint fails|panic:|stack trace|dial tcp|ECONN|node_modules|Error \d+ \(/i.test(message)) return fallback
  if (status && status >= 500) return fallback
  return message
}
