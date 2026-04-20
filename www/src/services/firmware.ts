import { apiClient } from './api'

export interface FirmwareRelease {
  id: number
  dev_model: number
  version: string
  changelog: string
  file_name: string
  file_size: number
  file_hash: string
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
}): Promise<FirmwareRelease> {
  const formData = new FormData()
  formData.append('file', data.file)
  formData.append('dev_model', data.dev_model.toString())
  formData.append('version', data.version)
  if (data.changelog) formData.append('changelog', data.changelog)
  const res = await apiClient.postFormData<ApiResponse<FirmwareRelease>>('/api/firmware', formData)
  if (res.code !== 200) throw new Error(res.message || '上传固件失败')
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
