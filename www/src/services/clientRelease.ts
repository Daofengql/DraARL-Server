import { apiClient } from './api'
import { directUpload } from './storageUpload'

export type ClientReleaseStatus = 'draft' | 'published' | 'withdrawn'
export type ClientReleaseChannel = 'stable' | 'beta'
export type ClientPlatform = 'android' | 'windows' | 'macos' | 'ios'
export type ClientArch = 'armv7' | 'arm64' | 'x86_64' | 'universal'
export type ClientPackageType = 'apk' | 'exe' | 'msix' | 'dmg' | 'pkg' | 'app_store' | 'ipa'

export interface ClientReleaseArtifact {
  id: number
  release_id: number
  platform: ClientPlatform
  arch: ClientArch
  android_abi?: string
  package_type: ClientPackageType
  build_number?: string
  min_os_version?: string
  min_android_api?: number
  file_name: string
  file_size: number
  sha256?: string
  signature?: string
  signature_algorithm?: string
  external_url?: string
  download_url?: string
  url_expires_at?: string
}

export interface ClientRelease {
  id: number
  app_id: string
  version: string
  channel: ClientReleaseChannel
  title: string
  changelog: string
  status: ClientReleaseStatus
  force_update: boolean
  min_supported_version?: string
  published_at?: string
  created_by: number
  create_time: string
  artifacts: ClientReleaseArtifact[]
}

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export interface ClientReleaseListResult {
  items: ClientRelease[]
  total: number
  page: number
  page_size: number
}

export async function listClientReleases(params?: {
  app_id?: string
  version?: string
  channel?: ClientReleaseChannel
  status?: ClientReleaseStatus
  platform?: ClientPlatform
  arch?: ClientArch
  page?: number
  page_size?: number
}): Promise<ClientReleaseListResult> {
  const qs = new URLSearchParams()
  for (const [key, value] of Object.entries(params || {})) {
    if (value !== undefined && value !== '') qs.set(key, String(value))
  }
  const res = await apiClient.get<BackendResponse<ClientReleaseListResult>>(`/api/client-releases${qs.size ? `?${qs}` : ''}`)
  if (res.code !== 200 || !res.data) throw new Error(res.message || '获取客户端发布列表失败')
  return res.data
}

export async function getClientRelease(id: number): Promise<ClientRelease> {
  const res = await apiClient.get<BackendResponse<ClientRelease>>(`/api/client-releases/${id}`)
  if (res.code !== 200 || !res.data) throw new Error(res.message || '获取客户端发布失败')
  return res.data
}

export async function createClientRelease(data: {
  app_id: string
  version: string
  channel: ClientReleaseChannel
  title?: string
  changelog?: string
  force_update?: boolean
  min_supported_version?: string
}): Promise<ClientRelease> {
  const res = await apiClient.post<BackendResponse<ClientRelease>>('/api/client-releases', data)
  if (res.code !== 200 || !res.data) throw new Error(res.message || '创建发布草稿失败')
  return res.data
}

export async function completeClientReleaseArtifact(data: {
  release_id: number
  platform: ClientPlatform
  arch: ClientArch
  package_type: ClientPackageType
  file?: File
  external_url?: string
  build_number?: string
  min_os_version?: string
  min_android_api?: number
  signature?: string
  signature_algorithm?: string
  onProgress?: (percent: number) => void
}): Promise<ClientRelease> {
  const payload: Record<string, unknown> = {
    platform: data.platform,
    arch: data.arch,
    package_type: data.package_type,
    build_number: data.build_number || undefined,
    min_os_version: data.min_os_version || undefined,
    min_android_api: data.min_android_api || undefined,
    signature: data.signature || undefined,
    signature_algorithm: data.signature_algorithm || undefined,
  }
  if (data.package_type === 'app_store') {
    payload.external_url = data.external_url || undefined
    payload.file_name = 'App Store / TestFlight'
  } else {
    if (!data.file) throw new Error('请选择安装包文件')
    const uploaded = await directUpload(data.file, 'client_package', data.onProgress)
    payload.object_key = uploaded.object_key
    payload.upload_token = uploaded.upload_token
    payload.file_name = data.file.name
  }
  const res = await apiClient.post<BackendResponse<ClientRelease>>(`/api/client-releases/${data.release_id}/artifacts/complete`, payload)
  if (res.code !== 200 || !res.data) throw new Error(res.message || '完成安装包上传失败')
  data.onProgress?.(100)
  return res.data
}

export async function publishClientRelease(id: number): Promise<ClientRelease> {
  const res = await apiClient.post<BackendResponse<ClientRelease>>(`/api/client-releases/${id}/publish`, {})
  if (res.code !== 200 || !res.data) throw new Error(res.message || '发布客户端版本失败')
  return res.data
}

export async function withdrawClientRelease(id: number): Promise<ClientRelease> {
  const res = await apiClient.post<BackendResponse<ClientRelease>>(`/api/client-releases/${id}/withdraw`, {})
  if (res.code !== 200 || !res.data) throw new Error(res.message || '撤回客户端版本失败')
  return res.data
}

export async function deleteClientRelease(id: number): Promise<void> {
  const res = await apiClient.delete<BackendResponse<void>>(`/api/client-releases/${id}`)
  if (res.code !== 200) throw new Error(res.message || '删除发布草稿失败')
}
