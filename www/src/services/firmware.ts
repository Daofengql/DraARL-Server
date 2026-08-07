import { apiClient } from './api'
import { directUpload } from './storageUpload'

export interface FirmwareRelease {
  id: number
  dev_model: number
  version: string
  changelog: string
  file_name: string
  file_size: number
  file_hash: string
  download_mode: 'presigned' | 'proxy'
  is_latest: boolean
  created_by: number
  create_time: string
  download_url?: string
}

interface FirmwareListResponse {
  items: FirmwareRelease[]
  total: number
  page: number
  page_size: number
}

interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export async function listFirmware(params?: {
  dev_model?: number
  page?: number
  page_size?: number
}): Promise<FirmwareListResponse> {
  const qs = new URLSearchParams()
  if (params?.dev_model) qs.set('dev_model', params.dev_model.toString())
  if (params?.page) qs.set('page', params.page.toString())
  if (params?.page_size) qs.set('page_size', params.page_size.toString())
  const url = qs.toString() ? `/api/firmware?${qs.toString()}` : '/api/firmware'
  const res = await apiClient.get<ApiResponse<FirmwareListResponse>>(url)
  if (res.code !== 200) throw new Error(res.message || '获取固件列表失败')
  return res.data
}

export async function uploadFirmware(data: {
  file: File
  dev_model: number
  version: string
  changelog?: string
  download_mode?: 'presigned' | 'proxy'
  onProgress?: (percent: number) => void
}): Promise<FirmwareRelease> {
  const uploaded = await directUpload(data.file, 'firmware', data.onProgress)
  const res = await apiClient.post<ApiResponse<FirmwareRelease>>('/api/firmware/complete', {
    dev_model: data.dev_model,
    version: data.version,
    changelog: data.changelog || undefined,
    object_key: uploaded.object_key,
    file_name: data.file.name,
    upload_token: uploaded.upload_token,
    download_mode: data.download_mode || 'presigned',
  })
  if (res.code !== 200) throw new Error(res.message || '上传固件失败')
  data.onProgress?.(100)
  return res.data
}

export async function deleteFirmware(id: number): Promise<void> {
  const res = await apiClient.delete<ApiResponse<void>>(`/api/firmware/${id}`)
  if (res.code !== 200) throw new Error(res.message || '删除固件失败')
}

export async function getLatestFirmware(devModel: number): Promise<FirmwareRelease | null> {
  const res = await apiClient.get<ApiResponse<FirmwareRelease>>(`/api/public/firmware/latest?dev_model=${devModel}`)
  if (res.code === 404) return null
  if (res.code !== 200) throw new Error(res.message || '获取固件信息失败')
  return res.data
}
