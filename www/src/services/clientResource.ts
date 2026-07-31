import { apiClient } from './api'
import { directUpload } from './storageUpload'

export type ClientResourceReleaseStatus = 'draft' | 'published' | 'withdrawn'
export type ClientResourceChannel = 'stable' | 'beta'

export interface ClientResource {
  id: number
  resource_key: string
  name: string
  category?: string
  description?: string
  required: boolean
  enabled: boolean
  current_stable_version?: string
  current_beta_version?: string
  created_by: number
  create_time: string
  update_time: string
}

export interface ClientResourceSummary {
  id: number
  resource_key: string
  name: string
  category?: string
  required: boolean
}

export interface ClientResourceArtifactTarget {
  platform: string
  arch: string
  min_os_version?: string
  min_android_api?: number
}

export interface ClientResourceArtifact {
  id: number
  release_id: number
  format: string
  runtime: string
  variant: string
  build_number?: string
  file_name: string
  file_size: number
  sha256?: string
  content_signature?: string
  signature_algorithm?: string
  external_url?: string
  storage_key?: string
  metadata?: Record<string, unknown>
  targets: ClientResourceArtifactTarget[]
}

export interface ClientResourceRelease {
  id: number
  resource_id: number
  resource: ClientResourceSummary
  version: string
  channel: ClientResourceChannel
  title: string
  changelog: string
  status: ClientResourceReleaseStatus
  force_update: boolean
  min_client_version?: string
  published_at?: string
  created_by: number
  create_time: string
  update_time: string
  artifacts: ClientResourceArtifact[]
}

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export interface ClientResourceListResult {
  items: ClientResource[]
  total: number
  page: number
  page_size: number
}

export interface ClientResourceReleaseListResult {
  items: ClientResourceRelease[]
  total: number
  page: number
  page_size: number
}

export interface ClientResourceStagingItem {
  object_key: string
  file_name: string
  size: number
  content_type?: string
}

export interface ClientResourceStagingListResult {
  items: ClientResourceStagingItem[]
  total: number
}

export interface ClientResourceStagingRetryResult extends ClientResourceStagingItem {
  upload_token: string
  expires_at: string
}

export interface ClientResourceStorageAuditResult {
  prefix: string
  scanned_objects: number
  scanned_bytes: number
  referenced_objects: number
  referenced_bytes: number
  unreferenced_objects: { key: string; size: number }[]
  missing_references: string[]
}

export interface ClientResourceStorageAuditResponse {
  generated_at: string
  prefixes: ClientResourceStorageAuditResult[]
  totals: {
    scanned_objects: number
    scanned_bytes: number
    referenced_objects: number
    referenced_bytes: number
    unreferenced_objects: number
    unreferenced_bytes: number
    missing_references: number
  }
}

function queryString(params?: Record<string, unknown>): string {
  const qs = new URLSearchParams()
  for (const [key, value] of Object.entries(params || {})) {
    if (value !== undefined && value !== '') qs.set(key, String(value))
  }
  return qs.size ? `?${qs}` : ''
}

function requireData<T>(response: BackendResponse<T>, fallback: string): T {
  if (response.code !== 200 || response.data === undefined) throw new Error(response.message || fallback)
  return response.data
}

export async function listClientResources(params?: {
  resource_key?: string
  name?: string
  category?: string
  enabled?: boolean | ''
  page?: number
  page_size?: number
}): Promise<ClientResourceListResult> {
  const response = await apiClient.get<BackendResponse<ClientResourceListResult>>(`/api/client-resources${queryString(params)}`)
  return requireData(response, '获取客户端资源失败')
}

export async function listClientResourceStaging(): Promise<ClientResourceStagingListResult> {
  const response = await apiClient.get<BackendResponse<ClientResourceStagingListResult>>('/api/client-resources/staging')
  return requireData(response, '获取待完成上传失败')
}

export async function retryClientResourceStaging(objectKey: string): Promise<ClientResourceStagingRetryResult> {
  const response = await apiClient.post<BackendResponse<ClientResourceStagingRetryResult>>('/api/client-resources/staging/retry', { object_key: objectKey })
  return requireData(response, '生成重试凭证失败')
}

export async function auditClientResourceStorage(): Promise<ClientResourceStorageAuditResponse> {
  const response = await apiClient.get<BackendResponse<ClientResourceStorageAuditResponse>>('/api/storage/audit')
  return requireData(response, '存储审计失败')
}

export async function getClientResource(id: number): Promise<ClientResource> {
  const response = await apiClient.get<BackendResponse<ClientResource>>(`/api/client-resources/${id}`)
  return requireData(response, '获取客户端资源失败')
}

export async function createClientResource(data: {
  resource_key: string
  name: string
  category?: string
  description?: string
  required?: boolean
  enabled?: boolean
}): Promise<ClientResource> {
  const response = await apiClient.post<BackendResponse<ClientResource>>('/api/client-resources', data)
  return requireData(response, '创建客户端资源失败')
}

export async function updateClientResource(id: number, data: Partial<{
  resource_key: string
  name: string
  category: string
  description: string
  required: boolean
  enabled: boolean
}>): Promise<ClientResource> {
  const response = await apiClient.patch<BackendResponse<ClientResource>>(`/api/client-resources/${id}`, data)
  return requireData(response, '更新客户端资源失败')
}

export async function listClientResourceReleases(resourceId: number, params?: {
  version?: string
  channel?: ClientResourceChannel | ''
  status?: ClientResourceReleaseStatus | ''
  platform?: string
  arch?: string
  page?: number
  page_size?: number
}): Promise<ClientResourceReleaseListResult> {
  const response = await apiClient.get<BackendResponse<ClientResourceReleaseListResult>>(`/api/client-resources/${resourceId}/releases${queryString(params)}`)
  return requireData(response, '获取资源发布失败')
}

export async function getClientResourceRelease(resourceId: number, releaseId: number): Promise<ClientResourceRelease> {
  const response = await apiClient.get<BackendResponse<ClientResourceRelease>>(`/api/client-resources/${resourceId}/releases/${releaseId}`)
  return requireData(response, '获取资源发布失败')
}

export async function createClientResourceRelease(resourceId: number, data: {
  version: string
  channel: ClientResourceChannel
  title?: string
  changelog?: string
  force_update?: boolean
  min_client_version?: string
}): Promise<ClientResourceRelease> {
  const response = await apiClient.post<BackendResponse<ClientResourceRelease>>(`/api/client-resources/${resourceId}/releases`, data)
  return requireData(response, '创建资源发布草稿失败')
}

export async function completeClientResourceArtifact(data: {
  resource_id: number
  release_id: number
  format: string
  runtime?: string
  variant?: string
  build_number?: string
  file?: File
  file_name?: string
  staging_object_key?: string
  staging_upload_token?: string
  external_url?: string
  content_signature?: string
  signature_algorithm?: string
  metadata?: Record<string, unknown>
  targets: ClientResourceArtifactTarget[]
  onProgress?: (percent: number) => void
}): Promise<ClientResourceRelease> {
  const payload: Record<string, unknown> = {
    format: data.format,
    runtime: data.runtime || undefined,
    variant: data.variant || undefined,
    build_number: data.build_number || undefined,
    external_url: data.external_url || undefined,
    content_signature: data.content_signature || undefined,
    signature_algorithm: data.signature_algorithm || undefined,
    metadata: data.metadata,
    targets: data.targets,
  }
  if (data.format === 'app_store') {
    payload.file_name = 'App Store / TestFlight'
  } else if (data.staging_object_key && data.staging_upload_token) {
    payload.object_key = data.staging_object_key
    payload.upload_token = data.staging_upload_token
    payload.file_name = data.file_name || data.staging_object_key.split('/').pop()
  } else {
    if (!data.file) throw new Error('请选择资源文件')
    const uploaded = await directUpload(data.file, 'client_resource', data.onProgress)
    payload.object_key = uploaded.object_key
    payload.upload_token = uploaded.upload_token
    payload.file_name = data.file_name || data.file.name
  }
  const response = await apiClient.post<BackendResponse<ClientResourceRelease>>(
    `/api/client-resources/${data.resource_id}/releases/${data.release_id}/artifacts/complete`,
    payload,
  )
  data.onProgress?.(100)
  return requireData(response, '完成资源文件上传失败')
}

export async function publishClientResourceRelease(resourceId: number, releaseId: number): Promise<ClientResourceRelease> {
  const response = await apiClient.post<BackendResponse<ClientResourceRelease>>(`/api/client-resources/${resourceId}/releases/${releaseId}/publish`, {})
  return requireData(response, '发布资源版本失败')
}

export async function withdrawClientResourceRelease(resourceId: number, releaseId: number): Promise<ClientResourceRelease> {
  const response = await apiClient.post<BackendResponse<ClientResourceRelease>>(`/api/client-resources/${resourceId}/releases/${releaseId}/withdraw`, {})
  return requireData(response, '撤回资源版本失败')
}

export async function deleteClientResourceRelease(resourceId: number, releaseId: number): Promise<void> {
  const response = await apiClient.delete<BackendResponse<void>>(`/api/client-resources/${resourceId}/releases/${releaseId}`)
  if (response.code !== 200) throw new Error(response.message || '删除资源发布草稿失败')
}
