interface ResponseMessagePayload {
  response?: {
    data?: {
      message?: string
    }
  }
  message?: string
}

export function getErrorMessage(error: unknown, fallback: string): string {
  if (!error || typeof error !== 'object') {
    return fallback
  }

  const maybeError = error as ResponseMessagePayload
  return maybeError.response?.data?.message || maybeError.message || fallback
}
