import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Autocomplete, Badge, Box, Button, Checkbox, Chip, Dialog, DialogActions,
  DialogContent, DialogTitle, FormControl, FormControlLabel, IconButton,
  InputLabel, LinearProgress, MenuItem, Paper, Select, Stack, Table, TableBody,
  TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField,
  Tooltip, Typography,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import ArrowBack from '@mui/icons-material/ArrowBack'
import CloudUpload from '@mui/icons-material/CloudUpload'
import Delete from '@mui/icons-material/Delete'
import Edit from '@mui/icons-material/Edit'
import Info from '@mui/icons-material/Info'
import FactCheck from '@mui/icons-material/FactCheck'
import Publish from '@mui/icons-material/Publish'
import Refresh from '@mui/icons-material/Refresh'
import type {
  ClientResource, ClientResourceArtifactTarget, ClientResourceChannel,
  ClientResourceRelease, ClientResourceReleaseStatus, ClientResourceStagingItem,
  ClientResourceStorageAuditResponse,
} from '../../services/clientResource'
import {
  auditClientResourceStorage, completeClientResourceArtifact, createClientResource, createClientResourceRelease,
  deleteClientResource, deleteClientResourceRelease, getClientResourceRelease, listClientResourceReleases,
  listClientResourceStaging, listClientResources, publishClientResourceRelease,
  retryClientResourceStaging, updateClientResource,
} from '../../services/clientResource'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

const SEMVER_RE = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/
const RESOURCE_KEY_RE = /^[a-z0-9][a-z0-9._-]{0,62}(?:\/[a-z0-9][a-z0-9._-]{0,62})*$/
const SERVER_CAPABILITY_RE = /^[a-z][a-z0-9._-]{0,63}$/

const CATEGORY_OPTIONS = ['application', 'model', 'font', 'dictionary', 'data']
const SERVER_CAPABILITY_OPTIONS = ['multi_receive_v1', 'source_group_v1']
const FORMAT_OPTIONS: Record<string, string[]> = {
  application: ['apk', 'exe', 'msix', 'dmg', 'pkg', 'ipa', 'app_store'],
  model: ['onnx', 'tflite', 'safetensors', 'gguf', 'bin'],
  font: ['ttf', 'otf', 'ttc', 'woff', 'woff2'],
  dictionary: ['dict', 'json', 'txt', 'sqlite', 'zip', 'bin'],
  data: ['json', 'zip', 'bin'],
}

type TargetOption = { key: string; platform: string; arch: string; label: string }

const TARGET_OPTIONS: TargetOption[] = [
  { key: 'android/armv7', platform: 'android', arch: 'armv7', label: 'Android / armv7' },
  { key: 'android/arm64', platform: 'android', arch: 'arm64', label: 'Android / arm64' },
  { key: 'android/x86_64', platform: 'android', arch: 'x86_64', label: 'Android / x86_64' },
  { key: 'windows/x86_64', platform: 'windows', arch: 'x86_64', label: 'Windows / x86_64' },
  { key: 'windows/arm64', platform: 'windows', arch: 'arm64', label: 'Windows / arm64' },
  { key: 'macos/x86_64', platform: 'macos', arch: 'x86_64', label: 'macOS / x86_64' },
  { key: 'macos/arm64', platform: 'macos', arch: 'arm64', label: 'macOS / arm64' },
  { key: 'ios/arm64', platform: 'ios', arch: 'arm64', label: 'iOS / arm64' },
  { key: 'linux/x86_64', platform: 'linux', arch: 'x86_64', label: 'Linux / x86_64' },
  { key: 'linux/arm64', platform: 'linux', arch: 'arm64', label: 'Linux / arm64' },
]

type ResourceForm = {
  resourceKey: string
  name: string
  category: string
  description: string
  required: boolean
  enabled: boolean
}

type ReleaseForm = {
  version: string
  channel: ClientResourceChannel
  title: string
  changelog: string
  minClientVersion: string
  minServerVersion: string
  requiredProtocolVersion: string
  requiredCapabilities: string[]
  forceUpdate: boolean
}

type ArtifactForm = {
  format: string
  runtime: string
  variant: string
  buildNumber: string
  fileName: string
  file: File | null
  stagingObjectKey: string
  stagingUploadToken: string
  externalURL: string
  contentSignature: string
  signatureAlgorithm: string
  metadata: string
  targets: ClientResourceArtifactTarget[]
  customPlatform: string
  customArch: string
}

const emptyResourceForm = (): ResourceForm => ({
  resourceKey: '', name: '', category: '', description: '', required: false, enabled: true,
})

const emptyReleaseForm = (): ReleaseForm => ({
  version: '', channel: 'stable', title: '', changelog: '', minClientVersion: '', minServerVersion: '',
  requiredProtocolVersion: '', requiredCapabilities: [], forceUpdate: false,
})

const emptyArtifactForm = (): ArtifactForm => ({
  format: '', runtime: 'default', variant: 'default', buildNumber: '', fileName: '', file: null,
  stagingObjectKey: '', stagingUploadToken: '',
  externalURL: '', contentSignature: '', signatureAlgorithm: '', metadata: '{}',
  targets: [], customPlatform: '', customArch: '',
})

function statusChip(status: ClientResourceReleaseStatus) {
  const values: Record<ClientResourceReleaseStatus, { label: string; color: 'default' | 'success' | 'warning' }> = {
    draft: { label: '草稿', color: 'default' },
    published: { label: '已发布', color: 'success' },
    withdrawn: { label: '已撤回', color: 'warning' },
  }
  const value = values[status]
  return <Chip size="small" label={value.label} color={value.color} variant={status === 'draft' ? 'outlined' : 'filled'} />
}

function formatFileSize(bytes: number) {
  if (!bytes) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function isHTTPSURL(value: string) {
  try {
    const parsed = new URL(value)
    return parsed.protocol === 'https:' && !!parsed.host
  } catch {
    return false
  }
}

function targetKey(target: ClientResourceArtifactTarget) {
  return `${target.platform}/${target.arch}`
}

export function ClientResourcePage() {
  const [resources, setResources] = useState<ClientResource[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(20)
  const [nameFilter, setNameFilter] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('')
  const [enabledFilter, setEnabledFilter] = useState<boolean | ''>('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)

  const [resourceFormOpen, setResourceFormOpen] = useState(false)
  const [editingResource, setEditingResource] = useState<ClientResource | null>(null)
  const [resourceForm, setResourceForm] = useState<ResourceForm>(emptyResourceForm())
  const [resourceToDelete, setResourceToDelete] = useState<ClientResource | null>(null)

  const [resourceDetailOpen, setResourceDetailOpen] = useState(false)
  const [selectedResource, setSelectedResource] = useState<ClientResource | null>(null)
  const [releases, setReleases] = useState<ClientResourceRelease[]>([])
  const [releaseLoading, setReleaseLoading] = useState(false)
  const [releaseFormVisible, setReleaseFormVisible] = useState(false)
  const [releaseForm, setReleaseForm] = useState<ReleaseForm>(emptyReleaseForm())
  const [selectedRelease, setSelectedRelease] = useState<ClientResourceRelease | null>(null)
  const [publishedEditMode, setPublishedEditMode] = useState(false)

  const [artifactForm, setArtifactForm] = useState<ArtifactForm>(emptyArtifactForm())
  const [uploadProgress, setUploadProgress] = useState(0)
  const [confirmAction, setConfirmAction] = useState<'publish' | 'delete' | null>(null)
  const [stagingItems, setStagingItems] = useState<ClientResourceStagingItem[]>([])
  const [stagingDialogOpen, setStagingDialogOpen] = useState(false)
  const [stagingLoading, setStagingLoading] = useState(false)
  const [auditDialogOpen, setAuditDialogOpen] = useState(false)
  const [auditLoading, setAuditLoading] = useState(false)
  const [audit, setAudit] = useState<ClientResourceStorageAuditResponse | null>(null)

  const fetchResources = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const result = await listClientResources({
        name: nameFilter || undefined,
        category: categoryFilter || undefined,
        enabled: enabledFilter,
        page: page + 1,
        page_size: rowsPerPage,
      })
      setResources(result.items)
      setTotal(result.total)
    } catch (err) {
      setError((err as Error).message || '获取客户端资源失败')
    } finally {
      setLoading(false)
    }
  }, [categoryFilter, enabledFilter, nameFilter, page, rowsPerPage])

  useEffect(() => { void fetchResources() }, [fetchResources])

  const fetchStaging = useCallback(async (showError = false) => {
    try {
      const result = await listClientResourceStaging()
      setStagingItems(result.items)
    } catch (err) {
      setStagingItems([])
      if (showError) setError((err as Error).message || '获取待完成上传失败')
    }
  }, [])

  useEffect(() => { void fetchStaging() }, [fetchStaging])

  const openAudit = async () => {
    setAuditDialogOpen(true)
    setAuditLoading(true)
    setError(null)
    try {
      setAudit(await auditClientResourceStorage())
    } catch (err) {
      setError((err as Error).message || '存储审计失败')
    } finally {
      setAuditLoading(false)
    }
  }

  const adoptStaging = async (item: ClientResourceStagingItem) => {
    if (!selectedRelease || (selectedRelease.status !== 'draft' && selectedRelease.status !== 'published')) {
      setError('请先打开草稿或已发布版本，再选择待完成上传')
      return
    }
    setStagingLoading(true)
    setError(null)
    try {
      const retry = await retryClientResourceStaging(item.object_key)
      const extension = item.file_name.split('.').pop()?.toLowerCase() || ''
      setArtifactForm({
        ...emptyArtifactForm(),
        format: extension,
        fileName: item.file_name,
        stagingObjectKey: retry.object_key,
        stagingUploadToken: retry.upload_token,
      })
      if (selectedRelease.status === 'published') setPublishedEditMode(true)
      setStagingDialogOpen(false)
      setSuccess('已载入待完成上传，可继续填写目标并提交')
    } catch (err) {
      setError((err as Error).message || '生成重试凭证失败')
    } finally {
      setStagingLoading(false)
    }
  }

  const fetchReleases = useCallback(async (resourceId: number) => {
    setReleaseLoading(true)
    try {
      const result = await listClientResourceReleases(resourceId, { page: 1, page_size: 100 })
      setReleases(result.items)
    } catch (err) {
      setError((err as Error).message || '获取资源发布失败')
    } finally {
      setReleaseLoading(false)
    }
  }, [])

  const openResourceCreate = () => {
    setEditingResource(null)
    setResourceForm(emptyResourceForm())
    setResourceFormOpen(true)
  }

  const openResourceEdit = (resource: ClientResource) => {
    setEditingResource(resource)
    setResourceForm({
      resourceKey: resource.resource_key,
      name: resource.name,
      category: resource.category || '',
      description: resource.description || '',
      required: resource.required,
      enabled: resource.enabled,
    })
    setResourceFormOpen(true)
  }

  const saveResource = async () => {
    const resourceKey = resourceForm.resourceKey.trim().toLowerCase()
    if (!RESOURCE_KEY_RE.test(resourceKey) || !resourceForm.name.trim()) {
      setError('资源标识或名称格式无效')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const payload = {
        resource_key: resourceKey,
        name: resourceForm.name.trim(),
        category: resourceForm.category.trim().toLowerCase(),
        description: resourceForm.description.trim(),
        required: resourceForm.required,
        enabled: resourceForm.enabled,
      }
      const saved = editingResource
        ? await updateClientResource(editingResource.id, payload)
        : await createClientResource(payload)
      setResourceFormOpen(false)
      setSuccess(editingResource ? '资源已更新' : '资源已创建')
      if (selectedResource?.id === saved.id) setSelectedResource(saved)
      await fetchResources()
    } catch (err) {
      setError((err as Error).message || '保存资源失败')
    } finally {
      setBusy(false)
    }
  }

  const runResourceDelete = async () => {
    if (!resourceToDelete) return
    const deleting = resourceToDelete
    setResourceToDelete(null)
    setBusy(true)
    setError(null)
    try {
      const result = await deleteClientResource(deleting.id)
      if (selectedResource?.id === deleting.id) {
        setResourceDetailOpen(false)
        setSelectedResource(null)
        setSelectedRelease(null)
      }
      const cleanup = result.object_cleanup_failures > 0
        ? `，${result.object_cleanup_failures} 个对象清理失败，请运行对象审计`
        : ''
      setSuccess(`资源已删除，级联删除 ${result.deleted_releases} 个版本、${result.deleted_artifacts} 个文件${cleanup}`)
      if (resources.length === 1 && page > 0) setPage(page - 1)
      else await fetchResources()
    } catch (err) {
      setError((err as Error).message || '删除客户端资源失败')
    } finally {
      setBusy(false)
    }
  }

  const openResourceDetail = async (resource: ClientResource) => {
    setSelectedResource(resource)
    setSelectedRelease(null)
    setPublishedEditMode(false)
    setReleaseFormVisible(false)
    setResourceDetailOpen(true)
    await fetchReleases(resource.id)
  }

  const createRelease = async () => {
    if (!selectedResource || !SEMVER_RE.test(releaseForm.version.trim())) {
      setError('版本号格式无效')
      return
    }
    if (releaseForm.minClientVersion && !SEMVER_RE.test(releaseForm.minClientVersion.trim())) {
      setError('最低客户端版本格式无效')
      return
    }
    if (releaseForm.minServerVersion && !SEMVER_RE.test(releaseForm.minServerVersion.trim())) {
      setError('最低服务端版本格式无效')
      return
    }
    const requiredProtocolVersion = releaseForm.requiredProtocolVersion === '' ? 0 : Number(releaseForm.requiredProtocolVersion)
    if (!Number.isInteger(requiredProtocolVersion) || requiredProtocolVersion < 0 || requiredProtocolVersion > 65535) {
      setError('所需协议版本必须是 0 到 65535 之间的整数')
      return
    }
    const requiredCapabilities = [...new Set(releaseForm.requiredCapabilities.map((value) => value.trim().toLowerCase()).filter(Boolean))].sort()
    if (requiredCapabilities.length > 32 || requiredCapabilities.some((value) => !SERVER_CAPABILITY_RE.test(value))) {
      setError('所需服务端能力格式无效')
      return
    }
    if (requiredCapabilities.length > 0 && requiredProtocolVersion === 0) {
      setError('声明协议能力时必须同时声明所需协议版本')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const release = await createClientResourceRelease(selectedResource.id, {
        version: releaseForm.version.trim(),
        channel: releaseForm.channel,
        title: releaseForm.title.trim(),
        changelog: releaseForm.changelog.trim(),
        min_client_version: releaseForm.minClientVersion.trim() || undefined,
        min_server_version: releaseForm.minServerVersion.trim() || undefined,
        required_protocol_version: requiredProtocolVersion || undefined,
        required_capabilities: requiredCapabilities.length > 0 ? requiredCapabilities : undefined,
        force_update: releaseForm.forceUpdate,
      })
      setReleaseForm(emptyReleaseForm())
      setReleaseFormVisible(false)
      setSuccess('资源发布草稿已创建')
      await fetchReleases(selectedResource.id)
      setSelectedRelease(release)
    } catch (err) {
      setError((err as Error).message || '创建资源发布失败')
    } finally {
      setBusy(false)
    }
  }

  const openRelease = async (release: ClientResourceRelease) => {
    if (!selectedResource) return
    setReleaseLoading(true)
    try {
      const detail = await getClientResourceRelease(selectedResource.id, release.id)
      setSelectedRelease(detail)
      setPublishedEditMode(false)
      setArtifactForm(emptyArtifactForm())
      setUploadProgress(0)
    } catch (err) {
      setError((err as Error).message || '获取资源发布失败')
    } finally {
      setReleaseLoading(false)
    }
  }

  const selectedTargetOptions = useMemo(() => {
    const keys = new Set(artifactForm.targets.map(targetKey))
    return TARGET_OPTIONS.filter((option) => keys.has(option.key))
  }, [artifactForm.targets])

  const setPresetTargets = (options: TargetOption[]) => {
    const existing = new Map(artifactForm.targets.map((target) => [targetKey(target), target]))
    const targets = options.map((option) => existing.get(option.key) || { platform: option.platform, arch: option.arch })
    const customTargets = artifactForm.targets.filter((target) => !TARGET_OPTIONS.some((option) => option.key === targetKey(target)))
    setArtifactForm({ ...artifactForm, targets: [...targets, ...customTargets] })
  }

  const addCustomTarget = () => {
    const platform = artifactForm.customPlatform.trim().toLowerCase()
    const arch = artifactForm.customArch.trim().toLowerCase()
    if (!platform || !arch || platform === 'universal' || arch === 'universal') {
      setError('请输入明确的平台和架构')
      return
    }
    const key = `${platform}/${arch}`
    if (artifactForm.targets.some((target) => targetKey(target) === key)) return
    setArtifactForm({
      ...artifactForm,
      targets: [...artifactForm.targets, { platform, arch }],
      customPlatform: '',
      customArch: '',
    })
  }

  const updateTarget = (index: number, update: Partial<ClientResourceArtifactTarget>) => {
    const targets = artifactForm.targets.map((target, current) => current === index ? { ...target, ...update } : target)
    setArtifactForm({ ...artifactForm, targets })
  }

  const removeTarget = (index: number) => {
    setArtifactForm({ ...artifactForm, targets: artifactForm.targets.filter((_, current) => current !== index) })
  }

  const completeArtifact = async () => {
    if (!selectedResource || !selectedRelease || !artifactForm.format.trim() || artifactForm.targets.length === 0) {
      setError('请填写文件格式并至少选择一个目标')
      return
    }
    const format = artifactForm.format.trim().toLowerCase()
    if (format === 'app_store') {
      if (!isHTTPSURL(artifactForm.externalURL.trim())) { setError('请输入有效的 HTTPS 商店地址'); return }
    } else if (!artifactForm.file && !artifactForm.stagingObjectKey) {
      setError('请选择资源文件')
      return
    } else if (!artifactForm.fileName.toLowerCase().endsWith(`.${format}`)) {
      setError('文件扩展名与格式不一致')
      return
    }
    if (!!artifactForm.contentSignature.trim() !== !!artifactForm.signatureAlgorithm.trim()) {
      setError('内容签名和签名算法必须同时填写')
      return
    }
    let metadata: Record<string, unknown>
    try {
      metadata = JSON.parse(artifactForm.metadata || '{}') as Record<string, unknown>
      if (!metadata || Array.isArray(metadata) || typeof metadata !== 'object') throw new Error('invalid')
    } catch {
      setError('metadata 必须是 JSON 对象')
      return
    }
    setBusy(true)
    setUploadProgress(0)
    setError(null)
    try {
      const release = await completeClientResourceArtifact({
        resource_id: selectedResource.id,
        release_id: selectedRelease.id,
        format,
        runtime: artifactForm.runtime.trim().toLowerCase(),
        variant: artifactForm.variant.trim().toLowerCase(),
        build_number: artifactForm.buildNumber.trim(),
        file: artifactForm.file || undefined,
        file_name: artifactForm.fileName.trim() || undefined,
        staging_object_key: artifactForm.stagingObjectKey || undefined,
        staging_upload_token: artifactForm.stagingUploadToken || undefined,
        external_url: artifactForm.externalURL.trim() || undefined,
        content_signature: artifactForm.contentSignature.trim() || undefined,
        signature_algorithm: artifactForm.signatureAlgorithm.trim() || undefined,
        metadata,
        targets: artifactForm.targets.map((target) => ({
          ...target,
          min_android_api: target.platform === 'android' && target.min_android_api ? Number(target.min_android_api) : undefined,
          min_os_version: target.min_os_version || undefined,
        })),
        onProgress: setUploadProgress,
      })
      setSelectedRelease(release)
      setArtifactForm(emptyArtifactForm())
      setSuccess('资源文件已添加')
      await fetchReleases(selectedResource.id)
      await fetchStaging()
    } catch (err) {
      setError((err as Error).message || '添加资源文件失败')
    } finally {
      setBusy(false)
    }
  }

  const runReleaseAction = async () => {
    if (!selectedResource || !selectedRelease || !confirmAction) return
    setBusy(true)
    setError(null)
    try {
      if (confirmAction === 'delete') {
        const result = await deleteClientResourceRelease(selectedResource.id, selectedRelease.id)
        setSelectedRelease(null)
        setPublishedEditMode(false)
        const cleanup = result.object_cleanup_failures > 0
          ? `，${result.object_cleanup_failures} 个对象清理失败，请运行对象审计`
          : ''
        setSuccess(`资源版本已删除，删除 ${result.deleted_artifacts} 个文件和 ${result.deleted_objects} 个对象${cleanup}`)
      } else {
        const updated = await publishClientResourceRelease(selectedResource.id, selectedRelease.id)
        setSelectedRelease(updated)
        setSuccess('资源版本已发布')
      }
      setConfirmAction(null)
      await Promise.all([fetchReleases(selectedResource.id), fetchResources()])
    } catch (err) {
      setError((err as Error).message || '资源发布操作失败')
    } finally {
      setBusy(false)
    }
  }

  const formatOptions = FORMAT_OPTIONS[selectedResource?.category || ''] || []
  const releaseAllowsArtifacts = selectedRelease?.status === 'draft' || selectedRelease?.status === 'published'
  const artifactEditorVisible = selectedRelease?.status === 'draft' || (selectedRelease?.status === 'published' && publishedEditMode)
  const releaseContractSummary = selectedRelease ? [
    `min_client_version=${selectedRelease.min_client_version || '-'}`,
    `min_server_version=${selectedRelease.min_server_version || '-'}`,
    `required_protocol_version=${selectedRelease.required_protocol_version || 0}`,
    `required_capabilities=${selectedRelease.required_capabilities?.join(', ') || '-'}`,
  ].join('\n') : ''
  const releaseArtifactSummary = selectedRelease?.artifacts.map((artifact) => [
    `${artifact.format} / ${artifact.runtime} / ${artifact.variant}`,
    `key=${artifact.storage_key || 'external'}`,
    `size=${artifact.file_size} bytes`,
    `sha256=${artifact.sha256 || '-'}`,
    `targets=${artifact.targets.map(targetKey).join(', ')}`,
  ].join('\n')).join('\n\n') || ''
  const releasePublishSummary = [releaseContractSummary, releaseArtifactSummary].filter(Boolean).join('\n\n')
  const releaseDeleteSummary = selectedRelease?.status === 'published'
    ? '删除后该版本会立即从 manifest 消失，并清理其全部文件；已有下载链接也将失效。'
    : '该版本及其全部文件将被永久删除。'

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 1.5, mb: 3 }}>
        <Typography variant="h5" sx={{ fontWeight: 600, flex: 1, minWidth: 220 }}>客户端资源分发</Typography>
        <Button variant="outlined" startIcon={<CloudUpload />} onClick={() => { setStagingDialogOpen(true); void fetchStaging(true) }}><Badge badgeContent={stagingItems.length} color="warning" max={99}>待完成上传</Badge></Button>
        <Tooltip title="只读扫描 immutable final 对象"><Button variant="outlined" startIcon={<FactCheck />} onClick={() => void openAudit()}>对象审计</Button></Tooltip>
        <Tooltip title="刷新"><IconButton onClick={() => void fetchResources()} disabled={loading}><Refresh /></IconButton></Tooltip>
        <Button variant="contained" startIcon={<Add />} onClick={openResourceCreate}>新建资源</Button>
      </Box>

      {success && <Alert severity="success" sx={{ mb: 2 }} onClose={() => setSuccess(null)}>{success}</Alert>}
      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>{error}</Alert>}

      <Stack direction="row" useFlexGap flexWrap="wrap" spacing={1.5} sx={{ mb: 2 }}>
        <TextField size="small" label="名称" value={nameFilter} onChange={(event) => { setNameFilter(event.target.value); setPage(0) }} sx={{ width: { xs: '100%', sm: 220 } }} />
        <Autocomplete freeSolo size="small" options={CATEGORY_OPTIONS} value={categoryFilter} onInputChange={(_, value) => { setCategoryFilter(value); setPage(0) }} renderInput={(params) => <TextField {...params} label="分类" />} sx={{ width: { xs: '100%', sm: 180 } }} />
        <FormControl size="small" sx={{ minWidth: 150 }}>
          <InputLabel>启用状态</InputLabel>
          <Select value={enabledFilter === '' ? '' : String(enabledFilter)} label="启用状态" onChange={(event) => { const value = event.target.value; setEnabledFilter(value === '' ? '' : value === 'true'); setPage(0) }}>
            <MenuItem value="">全部</MenuItem><MenuItem value="true">已启用</MenuItem><MenuItem value="false">已停用</MenuItem>
          </Select>
        </FormControl>
      </Stack>

      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead><TableRow><TableCell>资源</TableCell><TableCell>分类</TableCell><TableCell>stable</TableCell><TableCell>beta</TableCell><TableCell>状态</TableCell><TableCell align="right">操作</TableCell></TableRow></TableHead>
          <TableBody>
            {loading && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 5 }}>加载中...</TableCell></TableRow>}
            {!loading && resources.length === 0 && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 5 }}>暂无客户端资源</TableCell></TableRow>}
            {resources.map((resource) => <TableRow key={resource.id} hover>
              <TableCell><Typography fontWeight={600}>{resource.name}</Typography><Typography variant="caption" fontFamily="monospace" color="text.secondary">{resource.resource_key}</Typography></TableCell>
              <TableCell>{resource.category ? <Chip size="small" label={resource.category} variant="outlined" /> : '-'}</TableCell>
              <TableCell>{resource.current_stable_version || '-'}</TableCell>
              <TableCell>{resource.current_beta_version || '-'}</TableCell>
              <TableCell><Chip size="small" label={resource.enabled ? '已启用' : '已停用'} color={resource.enabled ? 'success' : 'default'} variant={resource.enabled ? 'filled' : 'outlined'} />{resource.required && <Chip size="small" label="必需" color="warning" variant="outlined" sx={{ ml: 0.5 }} />}</TableCell>
              <TableCell align="right"><Tooltip title="编辑"><IconButton size="small" onClick={() => openResourceEdit(resource)}><Edit fontSize="small" /></IconButton></Tooltip><Tooltip title="版本管理"><IconButton size="small" onClick={() => void openResourceDetail(resource)}><Info fontSize="small" /></IconButton></Tooltip><Tooltip title="删除资源"><IconButton size="small" color="error" onClick={() => setResourceToDelete(resource)}><Delete fontSize="small" /></IconButton></Tooltip></TableCell>
            </TableRow>)}
          </TableBody>
        </Table>
        <TablePagination component="div" count={total} page={page} rowsPerPage={rowsPerPage} rowsPerPageOptions={[10, 20, 50, 100]} onPageChange={(_, value) => setPage(value)} onRowsPerPageChange={(event) => { setRowsPerPage(Number(event.target.value)); setPage(0) }} labelRowsPerPage="每页" />
      </TableContainer>

      <Dialog open={resourceFormOpen} onClose={() => !busy && setResourceFormOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{editingResource ? '编辑客户端资源' : '新建客户端资源'}</DialogTitle>
        <DialogContent><Stack spacing={2} sx={{ mt: 1 }}>
          <TextField size="small" label="资源标识" value={resourceForm.resourceKey} onChange={(event) => setResourceForm({ ...resourceForm, resourceKey: event.target.value })} error={!!resourceForm.resourceKey && !RESOURCE_KEY_RE.test(resourceForm.resourceKey.toLowerCase())} required />
          <TextField size="small" label="资源名称" value={resourceForm.name} onChange={(event) => setResourceForm({ ...resourceForm, name: event.target.value })} required />
          <Autocomplete freeSolo size="small" options={CATEGORY_OPTIONS} value={resourceForm.category} onInputChange={(_, value) => setResourceForm({ ...resourceForm, category: value })} renderInput={(params) => <TextField {...params} label="分类" />} />
          <TextField size="small" label="备注" value={resourceForm.description} onChange={(event) => setResourceForm({ ...resourceForm, description: event.target.value })} multiline minRows={3} />
          <Stack direction="row" spacing={2}><FormControlLabel control={<Checkbox checked={resourceForm.required} onChange={(event) => setResourceForm({ ...resourceForm, required: event.target.checked })} />} label="必需资源" /><FormControlLabel control={<Checkbox checked={resourceForm.enabled} onChange={(event) => setResourceForm({ ...resourceForm, enabled: event.target.checked })} />} label="启用" /></Stack>
        </Stack></DialogContent>
        <DialogActions><Button onClick={() => setResourceFormOpen(false)} disabled={busy}>取消</Button><Button variant="contained" onClick={() => void saveResource()} disabled={busy || !resourceForm.name.trim() || !RESOURCE_KEY_RE.test(resourceForm.resourceKey.toLowerCase())}>保存</Button></DialogActions>
      </Dialog>

      <Dialog open={resourceDetailOpen} onClose={() => !busy && setResourceDetailOpen(false)} maxWidth="lg" fullWidth>
        <DialogTitle>
          <Stack direction="row" alignItems="center" spacing={1}>
            {selectedRelease && <IconButton size="small" onClick={() => { setSelectedRelease(null); setPublishedEditMode(false) }}><ArrowBack /></IconButton>}
            <Box sx={{ flex: 1, minWidth: 0 }}><Typography variant="h6" noWrap>{selectedResource?.name || '资源版本'}</Typography><Typography variant="caption" fontFamily="monospace" color="text.secondary">{selectedResource?.resource_key}</Typography></Box>
            {!selectedRelease && <Button startIcon={<Add />} onClick={() => setReleaseFormVisible(!releaseFormVisible)} disabled={!selectedResource?.enabled}>新建版本</Button>}
          </Stack>
        </DialogTitle>
        <DialogContent>
          {!selectedRelease && <Stack spacing={2} sx={{ mt: 1 }}>
            {releaseFormVisible && <Paper variant="outlined" sx={{ p: 2 }}><Stack spacing={1.5}>
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))', lg: 'repeat(4, minmax(0, 1fr))' }, gap: 1.5 }}>
                <TextField size="small" label="版本" value={releaseForm.version} onChange={(event) => setReleaseForm({ ...releaseForm, version: event.target.value })} error={!!releaseForm.version && !SEMVER_RE.test(releaseForm.version)} required fullWidth />
                <FormControl size="small" fullWidth><InputLabel>频道</InputLabel><Select value={releaseForm.channel} label="频道" onChange={(event) => setReleaseForm({ ...releaseForm, channel: event.target.value as ClientResourceChannel })}><MenuItem value="stable">stable</MenuItem><MenuItem value="beta">beta</MenuItem></Select></FormControl>
                <TextField size="small" label="最低客户端版本" value={releaseForm.minClientVersion} onChange={(event) => setReleaseForm({ ...releaseForm, minClientVersion: event.target.value })} error={!!releaseForm.minClientVersion && !SEMVER_RE.test(releaseForm.minClientVersion)} fullWidth />
                <TextField size="small" label="最低服务端版本" value={releaseForm.minServerVersion} onChange={(event) => setReleaseForm({ ...releaseForm, minServerVersion: event.target.value })} error={!!releaseForm.minServerVersion && !SEMVER_RE.test(releaseForm.minServerVersion)} fullWidth />
              </Box>
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'minmax(180px, 1fr) minmax(0, 3fr)' }, gap: 1.5 }}>
                <TextField size="small" type="number" label="所需协议版本" value={releaseForm.requiredProtocolVersion} onChange={(event) => setReleaseForm({ ...releaseForm, requiredProtocolVersion: event.target.value })} slotProps={{ htmlInput: { min: 0, max: 65535, step: 1 } }} fullWidth />
                <Autocomplete multiple freeSolo size="small" options={SERVER_CAPABILITY_OPTIONS} value={releaseForm.requiredCapabilities} onChange={(_, values) => setReleaseForm({ ...releaseForm, requiredCapabilities: values })} renderInput={(params) => <TextField {...params} label="所需服务端能力" />} />
              </Box>
              <TextField size="small" label="标题" value={releaseForm.title} onChange={(event) => setReleaseForm({ ...releaseForm, title: event.target.value })} />
              <TextField size="small" label="更新日志" value={releaseForm.changelog} onChange={(event) => setReleaseForm({ ...releaseForm, changelog: event.target.value })} multiline minRows={3} />
              <Stack direction="row" alignItems="center"><FormControlLabel control={<Checkbox checked={releaseForm.forceUpdate} onChange={(event) => setReleaseForm({ ...releaseForm, forceUpdate: event.target.checked })} />} label="强制更新" /><Box sx={{ flex: 1 }} /><Button onClick={() => setReleaseFormVisible(false)}>取消</Button><Button variant="contained" onClick={() => void createRelease()} disabled={busy || !SEMVER_RE.test(releaseForm.version)}>创建草稿</Button></Stack>
            </Stack></Paper>}
            <TableContainer component={Paper} variant="outlined"><Table size="small" sx={{ minWidth: 680 }}><TableHead><TableRow><TableCell>版本</TableCell><TableCell>频道</TableCell><TableCell>文件</TableCell><TableCell>状态</TableCell><TableCell sx={{ whiteSpace: 'nowrap' }}>发布时间</TableCell><TableCell align="right">操作</TableCell></TableRow></TableHead><TableBody>
              {releaseLoading && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 4 }}>加载中...</TableCell></TableRow>}
              {!releaseLoading && releases.length === 0 && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 4 }}>暂无发布版本</TableCell></TableRow>}
              {releases.map((release) => <TableRow key={release.id} hover><TableCell sx={{ fontFamily: 'monospace' }}>{release.version}</TableCell><TableCell><Chip size="small" label={release.channel} variant="outlined" /></TableCell><TableCell>{release.artifacts.length}</TableCell><TableCell>{statusChip(release.status)}</TableCell><TableCell>{release.published_at ? new Date(release.published_at).toLocaleString() : '-'}</TableCell><TableCell align="right"><Tooltip title="管理"><IconButton size="small" onClick={() => void openRelease(release)}><Info fontSize="small" /></IconButton></Tooltip></TableCell></TableRow>)}
            </TableBody></Table></TableContainer>
          </Stack>}

          {selectedRelease && <Stack spacing={2} sx={{ mt: 1 }}>
            <Stack direction="row" spacing={1} alignItems="center">{statusChip(selectedRelease.status)}<Chip size="small" label={selectedRelease.channel} variant="outlined" /><Typography fontFamily="monospace">{selectedRelease.version}</Typography></Stack>
            <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, minmax(0, 1fr))' }, gap: 1 }}>
              <Typography variant="body2"><Typography component="span" color="text.secondary">最低客户端：</Typography><Typography component="span" fontFamily="monospace">{selectedRelease.min_client_version || '-'}</Typography></Typography>
              <Typography variant="body2"><Typography component="span" color="text.secondary">最低服务端：</Typography><Typography component="span" fontFamily="monospace">{selectedRelease.min_server_version || '-'}</Typography></Typography>
              <Typography variant="body2"><Typography component="span" color="text.secondary">协议版本：</Typography><Typography component="span" fontFamily="monospace">{selectedRelease.required_protocol_version || 0}</Typography></Typography>
            </Box>
            {selectedRelease.required_capabilities && selectedRelease.required_capabilities.length > 0 && <Stack direction="row" spacing={0.75} useFlexGap flexWrap="wrap"><Typography variant="body2" color="text.secondary" sx={{ alignSelf: 'center' }}>所需能力：</Typography>{selectedRelease.required_capabilities.map((capability) => <Chip key={capability} size="small" label={capability} variant="outlined" />)}</Stack>}
            {selectedRelease.changelog && <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>{selectedRelease.changelog}</Typography>}
            {selectedRelease.status === 'draft' && selectedRelease.artifacts.length > 0 && <Alert severity="info">发布前请核对每个 artifact 的完整 final key、大小、SHA-256 和适用目标；发布后对象与版本不可覆盖。</Alert>}
            <TableContainer component={Paper} variant="outlined"><Table size="small" sx={{ minWidth: 800 }}><TableHead><TableRow><TableCell>格式</TableCell><TableCell sx={{ whiteSpace: 'nowrap' }}>runtime / variant</TableCell><TableCell sx={{ whiteSpace: 'nowrap' }}>适用目标</TableCell><TableCell>文件</TableCell><TableCell>大小</TableCell><TableCell>SHA-256</TableCell></TableRow></TableHead><TableBody>
              {selectedRelease.artifacts.length === 0 && <TableRow><TableCell colSpan={6} align="center">暂无文件</TableCell></TableRow>}
              {selectedRelease.artifacts.map((artifact) => <TableRow key={artifact.id}><TableCell>{artifact.format}</TableCell><TableCell>{artifact.runtime} / {artifact.variant}</TableCell><TableCell>{artifact.targets.map((target) => <Chip key={targetKey(target)} size="small" label={`${target.platform}/${target.arch}`} sx={{ mr: 0.5, mb: 0.5 }} />)}</TableCell><TableCell><Typography variant="body2">{artifact.file_name}</Typography>{artifact.storage_key && <Typography variant="caption" fontFamily="monospace" sx={{ display: 'block', wordBreak: 'break-all' }}>{artifact.storage_key}</Typography>}</TableCell><TableCell>{formatFileSize(artifact.file_size)}</TableCell><TableCell><Typography variant="caption" fontFamily="monospace" sx={{ wordBreak: 'break-all' }}>{artifact.sha256 || '-'}</Typography></TableCell></TableRow>)}
            </TableBody></Table></TableContainer>

            {artifactEditorVisible && <Paper variant="outlined" sx={{ p: 2 }}><Stack spacing={1.5}>
              <Typography variant="subtitle2">{selectedRelease.status === 'published' ? '追加资源文件' : '添加资源文件'}</Typography>
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))', lg: 'repeat(4, minmax(0, 1fr))' }, gap: 1.5 }}><Autocomplete freeSolo size="small" options={formatOptions} value={artifactForm.format} onInputChange={(_, value) => setArtifactForm({ ...artifactForm, format: value.toLowerCase(), file: null })} renderInput={(params) => <TextField {...params} label="格式" required />} /><TextField size="small" label="Runtime" value={artifactForm.runtime} onChange={(event) => setArtifactForm({ ...artifactForm, runtime: event.target.value })} /><TextField size="small" label="Variant" value={artifactForm.variant} onChange={(event) => setArtifactForm({ ...artifactForm, variant: event.target.value })} /><TextField size="small" label="构建号" value={artifactForm.buildNumber} onChange={(event) => setArtifactForm({ ...artifactForm, buildNumber: event.target.value })} /></Box>
              {artifactForm.format === 'app_store' ? <TextField size="small" type="url" label="App Store / TestFlight 地址" value={artifactForm.externalURL} onChange={(event) => setArtifactForm({ ...artifactForm, externalURL: event.target.value })} required /> : <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} alignItems={{ xs: 'stretch', sm: 'center' }}><Button component="label" variant="outlined" startIcon={<CloudUpload />} disabled={!artifactForm.format || !!artifactForm.stagingObjectKey} sx={{ flexShrink: 0, minWidth: { sm: 136 } }}>选择文件<input hidden type="file" accept={artifactForm.format ? `.${artifactForm.format}` : undefined} onChange={(event) => { const file = event.target.files?.[0] || null; setArtifactForm({ ...artifactForm, file, fileName: file?.name || '', stagingObjectKey: '', stagingUploadToken: '' }) }} /></Button><TextField size="small" label="文件名" value={artifactForm.fileName} onChange={(event) => setArtifactForm({ ...artifactForm, fileName: event.target.value })} disabled={!!artifactForm.stagingObjectKey} fullWidth /></Stack>}
              {artifactForm.stagingObjectKey && <Alert severity="info" action={<Button size="small" onClick={() => setArtifactForm({ ...artifactForm, stagingObjectKey: '', stagingUploadToken: '', fileName: '', file: null })}>改用本地文件</Button>}>已选择待完成上传：<Typography component="span" fontFamily="monospace">{artifactForm.stagingObjectKey}</Typography></Alert>}
              <Autocomplete multiple size="small" options={TARGET_OPTIONS} value={selectedTargetOptions} isOptionEqualToValue={(option, value) => option.key === value.key} getOptionLabel={(option) => option.label} onChange={(_, options) => setPresetTargets(options)} renderInput={(params) => <TextField {...params} label="适用目标" />} />
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'minmax(0, 1fr) minmax(0, 1fr) 40px' }, gap: 1, alignItems: 'start' }}><TextField size="small" label="自定义平台" value={artifactForm.customPlatform} onChange={(event) => setArtifactForm({ ...artifactForm, customPlatform: event.target.value })} /><TextField size="small" label="自定义架构" value={artifactForm.customArch} onChange={(event) => setArtifactForm({ ...artifactForm, customArch: event.target.value })} /><Tooltip title="添加自定义目标"><IconButton color="primary" onClick={addCustomTarget} aria-label="添加自定义目标" sx={{ width: 40, height: 40, justifySelf: { xs: 'end', sm: 'stretch' }, border: 1, borderColor: 'divider', borderRadius: 1 }}><Add /></IconButton></Tooltip></Box>
              {artifactForm.targets.length > 0 && <TableContainer component={Paper} variant="outlined"><Table size="small" sx={{ minWidth: 680, tableLayout: 'fixed' }}><TableHead><TableRow><TableCell sx={{ width: '15%' }}>平台</TableCell><TableCell sx={{ width: '15%' }}>架构</TableCell><TableCell sx={{ width: '30%', whiteSpace: 'nowrap' }}>最低系统版本</TableCell><TableCell sx={{ width: '30%', whiteSpace: 'nowrap' }}>最低 Android API</TableCell><TableCell sx={{ width: 52 }} /></TableRow></TableHead><TableBody>{artifactForm.targets.map((target, index) => <TableRow key={targetKey(target)}><TableCell>{target.platform}</TableCell><TableCell>{target.arch}</TableCell><TableCell><TextField size="small" value={target.min_os_version || ''} onChange={(event) => updateTarget(index, { min_os_version: event.target.value })} fullWidth /></TableCell><TableCell>{target.platform === 'android' ? <TextField size="small" type="number" value={target.min_android_api || ''} onChange={(event) => updateTarget(index, { min_android_api: Number(event.target.value) || undefined })} slotProps={{ htmlInput: { min: 1, max: 1000 } }} fullWidth /> : '-'}</TableCell><TableCell align="right"><Tooltip title="删除目标"><IconButton size="small" onClick={() => removeTarget(index)} aria-label={`删除 ${target.platform}/${target.arch} 目标`}><Delete fontSize="small" /></IconButton></Tooltip></TableCell></TableRow>)}</TableBody></Table></TableContainer>}
              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5}><TextField size="small" label="签名算法" value={artifactForm.signatureAlgorithm} onChange={(event) => setArtifactForm({ ...artifactForm, signatureAlgorithm: event.target.value })} fullWidth /><TextField size="small" label="内容签名" value={artifactForm.contentSignature} onChange={(event) => setArtifactForm({ ...artifactForm, contentSignature: event.target.value })} fullWidth /></Stack>
              <TextField size="small" label="Metadata JSON" value={artifactForm.metadata} onChange={(event) => setArtifactForm({ ...artifactForm, metadata: event.target.value })} multiline minRows={3} />
              {busy && uploadProgress > 0 && <Box><LinearProgress variant="determinate" value={uploadProgress} /><Typography variant="caption" color="text.secondary">{uploadProgress}%</Typography></Box>}
              <Box><Button variant="contained" startIcon={<CloudUpload />} onClick={() => void completeArtifact()} disabled={busy || !artifactForm.format || artifactForm.targets.length === 0}>验证并添加</Button></Box>
            </Stack></Paper>}
          </Stack>}
        </DialogContent>
        <DialogActions>
          {selectedRelease && <Button color="error" startIcon={<Delete />} onClick={() => setConfirmAction('delete')} disabled={busy}>{selectedRelease.status === 'draft' ? '删除草稿' : '删除版本'}</Button>}
          <Box sx={{ flex: 1 }} />
          {selectedRelease?.status === 'draft' && <Button variant="contained" startIcon={<Publish />} onClick={() => setConfirmAction('publish')} disabled={busy || selectedRelease.artifacts.length === 0}>发布</Button>}
          {selectedRelease?.status === 'published' && <Button variant={publishedEditMode ? 'outlined' : 'contained'} startIcon={<Edit />} onClick={() => setPublishedEditMode(!publishedEditMode)} disabled={busy}>{publishedEditMode ? '结束编辑' : '编辑'}</Button>}
          <Button onClick={() => setResourceDetailOpen(false)} disabled={busy}>关闭</Button>
        </DialogActions>
      </Dialog>

      <Dialog open={stagingDialogOpen} onClose={() => !stagingLoading && setStagingDialogOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>待完成上传</DialogTitle>
        <DialogContent>
          {stagingItems.length === 0 && <Alert severity="info" sx={{ mt: 1 }}>当前管理员没有可继续完成的客户端资源上传。</Alert>}
          {stagingItems.length > 0 && <TableContainer component={Paper} variant="outlined" sx={{ mt: 1 }}><Table size="small"><TableHead><TableRow><TableCell>对象</TableCell><TableCell>大小</TableCell><TableCell>类型</TableCell><TableCell align="right">操作</TableCell></TableRow></TableHead><TableBody>{stagingItems.map((item) => <TableRow key={item.object_key}><TableCell><Typography variant="body2" fontFamily="monospace" sx={{ wordBreak: 'break-all' }}>{item.object_key}</Typography><Typography variant="caption" color="text.secondary">默认文件名：{item.file_name}</Typography></TableCell><TableCell>{formatFileSize(item.size)}</TableCell><TableCell>{item.content_type || '-'}</TableCell><TableCell align="right"><Button size="small" variant="contained" onClick={() => void adoptStaging(item)} disabled={stagingLoading || !releaseAllowsArtifacts}>载入版本</Button></TableCell></TableRow>)}</TableBody></Table></TableContainer>}
        </DialogContent>
        <DialogActions><Button onClick={() => void fetchStaging(true)} disabled={stagingLoading}>刷新</Button><Button onClick={() => setStagingDialogOpen(false)} disabled={stagingLoading}>关闭</Button></DialogActions>
      </Dialog>

      <Dialog open={auditDialogOpen} onClose={() => !auditLoading && setAuditDialogOpen(false)} maxWidth="lg" fullWidth>
        <DialogTitle>对象审计（只读）</DialogTitle>
        <DialogContent>
          {auditLoading && <LinearProgress sx={{ mt: 1 }} />}
          {!auditLoading && audit && <Stack spacing={2} sx={{ mt: 1 }}>
            <Alert severity={audit.totals.unreferenced_objects || audit.totals.missing_references ? 'warning' : 'success'}>
              扫描 {audit.totals.scanned_objects} 个对象，{audit.totals.unreferenced_objects} 个未被数据库引用，{audit.totals.missing_references} 个数据库引用缺少对象。审计不会删除对象。
            </Alert>
            <TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>前缀</TableCell><TableCell>对象数</TableCell><TableCell>总大小</TableCell><TableCell>已引用</TableCell><TableCell>未引用</TableCell><TableCell>缺失引用</TableCell></TableRow></TableHead><TableBody>{audit.prefixes.map((prefix) => <TableRow key={prefix.prefix}><TableCell sx={{ fontFamily: 'monospace' }}>{prefix.prefix}</TableCell><TableCell>{prefix.scanned_objects}</TableCell><TableCell>{formatFileSize(prefix.scanned_bytes)}</TableCell><TableCell>{prefix.referenced_objects}</TableCell><TableCell>{prefix.unreferenced_objects.length}</TableCell><TableCell>{prefix.missing_references.length}</TableCell></TableRow>)}</TableBody></Table></TableContainer>
            {audit.prefixes.some((prefix) => prefix.unreferenced_objects.length > 0 || prefix.missing_references.length > 0) && <Stack spacing={1}>{audit.prefixes.flatMap((prefix) => prefix.unreferenced_objects.map((object) => <Typography key={`orphan-${object.key}`} variant="caption" fontFamily="monospace" sx={{ wordBreak: 'break-all' }}>未引用：{object.key} ({formatFileSize(object.size)})</Typography>).concat(prefix.missing_references.map((key) => <Typography key={`missing-${key}`} variant="caption" fontFamily="monospace" color="error" sx={{ wordBreak: 'break-all' }}>缺失：{key}</Typography>)))}</Stack>}
          </Stack>}
        </DialogContent>
        <DialogActions><Button onClick={() => void openAudit()} disabled={auditLoading}>重新扫描</Button><Button onClick={() => setAuditDialogOpen(false)} disabled={auditLoading}>关闭</Button></DialogActions>
      </Dialog>

      <ConfirmDialog isOpen={confirmAction !== null} title={confirmAction === 'publish' ? '发布资源版本' : '删除资源版本'} message={confirmAction === 'publish' ? `确定发布 ${selectedRelease?.version} 吗？\n\n${releasePublishSummary}` : `确定删除 ${selectedRelease?.version} 吗？\n\n${releaseDeleteSummary}`} confirmText={confirmAction === 'delete' ? '删除版本' : '确认'} type={confirmAction === 'delete' ? 'danger' : 'warning'} onConfirm={() => void runReleaseAction()} onCancel={() => setConfirmAction(null)} />
      <ConfirmDialog isOpen={resourceToDelete !== null} title="删除客户端资源" message={`确定删除“${resourceToDelete?.name || ''}”吗？这会级联删除所有草稿、已发布/已撤回版本、适用目标和对象文件，且无法恢复。`} confirmText="删除资源" type="danger" onConfirm={() => void runResourceDelete()} onCancel={() => !busy && setResourceToDelete(null)} />
    </Box>
  )
}
