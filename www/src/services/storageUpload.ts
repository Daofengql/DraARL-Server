import { apiClient } from './api'

export type PresignFileType = 'assets' | 'firmware' | 'operator_cert'

export interface PresignPutResult {
  mode: 's3' | 'local' | string
  upload_url: string
  method: string
  headers?: Record<string, string>
  object_key: string
  expires_at: string
  max_size: number
  content_type: string
  upload_token: string
}

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export async function presignPut(params: {
  file_type: PresignFileType
  file_name: string
  size: number
  content_type?: string
}): Promise<PresignPutResult> {
  const res = await apiClient.post<BackendResponse<PresignPutResult>>('/api/storage/presign-put', params)
  if (res.code !== 200 || !res.data) {
    throw new Error(res.message || '获取上传凭证失败')
  }
  return res.data
}

/** 使用 XMLHttpRequest 直传，支持进度回调 */
export function putToSignedURL(
  uploadURL: string,
  file: File | Blob,
  headers: Record<string, string> | undefined,
  onProgress?: (percent: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', uploadURL, true)
    if (headers) {
      for (const [k, v] of Object.entries(headers)) {
        if (v) xhr.setRequestHeader(k, v)
      }
    }
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) {
        onProgress(Math.round((e.loaded / e.total) * 100))
      }
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve()
      } else {
        reject(new Error(`直传失败: HTTP ${xhr.status}`))
      }
    }
    xhr.onerror = () => reject(new Error('直传网络错误'))
    xhr.onabort = () => reject(new Error('直传已取消'))
    xhr.send(file)
  })
}

/**
 * 直传：presign → PUT → 返回 object_key。
 * 资源/固件/操作证固定走直传，失败直接抛错，不再降级 multipart。
 */
export async function directUpload(
  file: File,
  fileType: PresignFileType,
  onProgress?: (percent: number) => void,
): Promise<{ object_key: string; content_type: string; size: number; upload_token: string }> {
  const contentType = file.type || 'application/octet-stream'
  const presign = await presignPut({
    file_type: fileType,
    file_name: file.name,
    size: file.size,
    content_type: contentType,
  })

  await putToSignedURL(presign.upload_url, file, presign.headers, onProgress)
  return {
    object_key: presign.object_key,
    content_type: presign.content_type || contentType,
    size: file.size,
    upload_token: presign.upload_token,
  }
}
