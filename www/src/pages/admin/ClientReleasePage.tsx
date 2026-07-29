import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Box, Button, Checkbox, Chip, Dialog, DialogActions, DialogContent,
  DialogTitle, FormControl, FormControlLabel, InputLabel, LinearProgress,
  MenuItem, Paper, Select, Stack, Table, TableBody, TableCell, TableContainer,
  TableHead, TablePagination, TableRow, TextField, Tooltip, Typography,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import CloudUpload from '@mui/icons-material/CloudUpload'
import Delete from '@mui/icons-material/Delete'
import Info from '@mui/icons-material/Info'
import Publish from '@mui/icons-material/Publish'
import Refresh from '@mui/icons-material/Refresh'
import Undo from '@mui/icons-material/Undo'
import type {
  ClientArch, ClientPackageType, ClientPlatform, ClientRelease, ClientReleaseChannel,
  ClientReleaseStatus,
} from '../../services/clientRelease'
import {
  completeClientReleaseArtifact, createClientRelease, deleteClientRelease, getClientRelease,
  listClientReleases, publishClientRelease, withdrawClientRelease,
} from '../../services/clientRelease'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

const SEMVER_RE = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/
const FIELD_LIMITS = {
  appID: 100,
  version: 64,
  title: 255,
  changelog: 1 << 20,
  buildNumber: 64,
  externalURL: 1024,
  signature: 65535,
  signatureAlgorithm: 64,
} as const

type ArtifactDraft = {
  platform: ClientPlatform
  arch: ClientArch
  packageType: ClientPackageType
  file: File | null
  externalURL: string
  buildNumber: string
  minOSVersion: string
  minAndroidAPI: string
  signature: string
  signatureAlgorithm: string
}

type ConfirmReleaseAction = 'publish' | 'withdraw' | null

const emptyArtifact = (): ArtifactDraft => ({
  platform: 'android',
  arch: 'arm64',
  packageType: 'apk',
  file: null,
  externalURL: '',
  buildNumber: '',
  minOSVersion: '',
  minAndroidAPI: '',
  signature: '',
  signatureAlgorithm: '',
})

function formatFileSize(bytes: number): string {
  if (!bytes) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}

function statusChip(status: ClientReleaseStatus) {
  const config: Record<ClientReleaseStatus, { label: string, color: 'default' | 'success' | 'warning' }> = {
    draft: { label: '草稿', color: 'default' },
    published: { label: '已发布', color: 'success' },
    withdrawn: { label: '已撤回', color: 'warning' },
  }
  const value = config[status]
  return <Chip size="small" label={value.label} color={value.color} variant={status === 'draft' ? 'outlined' : 'filled'} />
}

function packageTypesFor(platform: ClientPlatform): ClientPackageType[] {
  if (platform === 'android') return ['apk']
  if (platform === 'windows') return ['exe', 'msix']
  if (platform === 'macos') return ['dmg', 'pkg']
  return ['app_store', 'ipa']
}

function packageFileAccept(packageType: ClientPackageType): string | undefined {
  const values: Partial<Record<ClientPackageType, string>> = {
    apk: '.apk,application/vnd.android.package-archive',
    exe: '.exe,application/vnd.microsoft.portable-executable,application/x-msdownload',
    msix: '.msix,application/vnd.ms-appx',
    dmg: '.dmg,application/x-apple-diskimage',
    pkg: '.pkg,application/x-newton-compatible-pkg',
    ipa: '.ipa',
  }
  return values[packageType]
}

function fileMatchesPackageType(file: File, packageType: ClientPackageType): boolean {
  return file.name.toLowerCase().endsWith(`.${packageType}`)
}

function isHTTPSURL(value: string): boolean {
  try {
    const parsed = new URL(value.trim())
    return parsed.protocol === 'https:' && !!parsed.host
  } catch {
    return false
  }
}

function architecturesFor(platform: ClientPlatform, packageType?: ClientPackageType): ClientArch[] {
  if (platform === 'android') return ['armv7', 'arm64', 'universal']
  if (platform === 'windows') return ['x86_64', 'universal']
  if (platform === 'macos') return ['arm64', 'x86_64', 'universal']
  if (packageType === 'app_store') return ['universal']
  return ['universal', 'arm64']
}

function defaultArtifactForPlatform(platform: ClientPlatform): ArtifactDraft {
  const draft = emptyArtifact()
  draft.platform = platform
  draft.packageType = packageTypesFor(platform)[0]
  draft.arch = architecturesFor(platform, draft.packageType)[0]
  return draft
}

export function ClientReleasePage() {
  const [items, setItems] = useState<ClientRelease[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(20)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [statusFilter, setStatusFilter] = useState<ClientReleaseStatus | ''>('')
  const [channelFilter, setChannelFilter] = useState<ClientReleaseChannel | ''>('')
  const [appIDFilter, setAppIDFilter] = useState('')
  const [versionFilter, setVersionFilter] = useState('')
  const [platformFilter, setPlatformFilter] = useState<ClientPlatform | ''>('')
  const [archFilter, setArchFilter] = useState<ClientArch | ''>('')
  const [createOpen, setCreateOpen] = useState(false)
  const [detailOpen, setDetailOpen] = useState(false)
  const [selectedRelease, setSelectedRelease] = useState<ClientRelease | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ClientRelease | null>(null)
  const [confirmReleaseAction, setConfirmReleaseAction] = useState<ConfirmReleaseAction>(null)
  const [busy, setBusy] = useState(false)
  const [uploadProgress, setUploadProgress] = useState(0)
  const [releaseForm, setReleaseForm] = useState({
    appID: 'draarl-client', version: '', channel: 'stable' as ClientReleaseChannel,
    title: '', changelog: '', forceUpdate: false, minSupportedVersion: '',
  })
  const [artifactForm, setArtifactForm] = useState<ArtifactDraft>(emptyArtifact)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const result = await listClientReleases({
        app_id: appIDFilter.trim() || undefined,
        version: versionFilter.trim() || undefined,
        status: statusFilter || undefined,
        channel: channelFilter || undefined,
        platform: platformFilter || undefined,
        arch: archFilter || undefined,
        page: page + 1,
        page_size: rowsPerPage,
      })
      setItems(result.items)
      setTotal(result.total)
    } catch (err: any) {
      setError(err.message || '获取客户端发布列表失败')
    } finally {
      setLoading(false)
    }
  }, [appIDFilter, archFilter, channelFilter, page, platformFilter, rowsPerPage, statusFilter, versionFilter])

  useEffect(() => { void fetchData() }, [fetchData])

  const artifactMatrix = useMemo(() => {
    if (!selectedRelease) return ''
    return selectedRelease.artifacts.map(item => `${item.platform}:${item.arch}:${item.package_type}`).join('、') || '尚未添加安装包'
  }, [selectedRelease])

  const resetReleaseForm = () => {
    setReleaseForm({ appID: 'draarl-client', version: '', channel: 'stable', title: '', changelog: '', forceUpdate: false, minSupportedVersion: '' })
  }

  const openDetail = async (release: ClientRelease) => {
    setBusy(true)
    setError(null)
    try {
      const detail = await getClientRelease(release.id)
      setSelectedRelease(detail)
      setArtifactForm(emptyArtifact())
      setDetailOpen(true)
    } catch (err: any) {
      setError(err.message || '获取发布详情失败')
    } finally {
      setBusy(false)
    }
  }

  const handleCreate = async () => {
    if (!releaseForm.appID.trim() || !SEMVER_RE.test(releaseForm.version.trim())) {
      setError('请填写有效的 app_id 和 semver 版本号')
      return
    }
    if (releaseForm.minSupportedVersion && !SEMVER_RE.test(releaseForm.minSupportedVersion.trim())) {
      setError('最低支持版本必须是 semver 格式')
      return
    }
    setBusy(true)
    setError(null)
    setSuccess(null)
    try {
      const release = await createClientRelease({
        app_id: releaseForm.appID.trim(), version: releaseForm.version.trim(), channel: releaseForm.channel,
        title: releaseForm.title.trim() || undefined, changelog: releaseForm.changelog.trim() || undefined,
        force_update: releaseForm.forceUpdate, min_supported_version: releaseForm.minSupportedVersion.trim() || undefined,
      })
      setCreateOpen(false)
      resetReleaseForm()
      setSuccess(`已创建 ${release.version} 草稿，可继续添加平台安装包`)
      await openDetail(release)
      await fetchData()
    } catch (err: any) {
      setError(err.message || '创建发布草稿失败')
    } finally {
      setBusy(false)
    }
  }

  const setArtifactPlatform = (platform: ClientPlatform) => setArtifactForm(defaultArtifactForPlatform(platform))

  const setArtifactPackageType = (packageType: ClientPackageType) => {
    const availableArchitectures = architecturesFor(artifactForm.platform, packageType)
    const arch = availableArchitectures.includes(artifactForm.arch) ? artifactForm.arch : availableArchitectures[0]
    setArtifactForm({ ...artifactForm, packageType, arch, file: null, externalURL: '' })
  }

  const handleCompleteArtifact = async () => {
    if (!selectedRelease) return
    if (artifactForm.packageType === 'app_store') {
      if (!isHTTPSURL(artifactForm.externalURL)) {
        setError('App Store / TestFlight 发布必须填写有效的 HTTPS 地址')
        return
      }
    } else if (!artifactForm.file) {
      setError('请选择安装包文件')
      return
    } else if (!fileMatchesPackageType(artifactForm.file, artifactForm.packageType)) {
      setError(`请选择 .${artifactForm.packageType} 格式的安装包`)
      return
    }
    if (artifactForm.minOSVersion && !SEMVER_RE.test(artifactForm.minOSVersion.trim())) {
      setError('最低系统版本必须是 semver 格式')
      return
    }
    if (!!artifactForm.signature.trim() !== !!artifactForm.signatureAlgorithm.trim()) {
      setError('安装包签名与签名算法必须同时填写')
      return
    }
    const minAndroidAPI = artifactForm.minAndroidAPI ? Number(artifactForm.minAndroidAPI) : undefined
    if (minAndroidAPI !== undefined && (!Number.isInteger(minAndroidAPI) || minAndroidAPI < 1 || minAndroidAPI > 1000)) {
      setError('最低 Android API 必须是 1 到 1000 之间的整数')
      return
    }
    setBusy(true)
    setUploadProgress(0)
    setError(null)
    setSuccess(null)
    try {
      const updated = await completeClientReleaseArtifact({
        release_id: selectedRelease.id, platform: artifactForm.platform, arch: artifactForm.arch,
        package_type: artifactForm.packageType, file: artifactForm.file || undefined,
        external_url: artifactForm.externalURL.trim() || undefined, build_number: artifactForm.buildNumber.trim() || undefined,
        min_os_version: artifactForm.minOSVersion.trim() || undefined, min_android_api: minAndroidAPI,
        signature: artifactForm.signature.trim() || undefined, signature_algorithm: artifactForm.signatureAlgorithm.trim() || undefined,
        onProgress: setUploadProgress,
      })
      setSelectedRelease(updated)
      setArtifactForm(emptyArtifact())
      setSuccess('安装包已校验并加入发布矩阵')
      await fetchData()
    } catch (err: any) {
      setError(err.message || '安装包上传失败')
    } finally {
      setBusy(false)
      setUploadProgress(0)
    }
  }

  const handlePublish = async () => {
    if (!selectedRelease) return
    setConfirmReleaseAction(null)
    setBusy(true)
    setError(null)
    setSuccess(null)
    try {
      const updated = await publishClientRelease(selectedRelease.id)
      setSelectedRelease(updated)
      setSuccess('客户端版本已发布')
      await fetchData()
    } catch (err: any) {
      setError(err.message || '发布失败')
    } finally {
      setBusy(false)
    }
  }

  const handleWithdraw = async () => {
    if (!selectedRelease) return
    setConfirmReleaseAction(null)
    setBusy(true)
    setError(null)
    setSuccess(null)
    try {
      const updated = await withdrawClientRelease(selectedRelease.id)
      setSelectedRelease(updated)
      setSuccess('客户端版本已撤回')
      await fetchData()
    } catch (err: any) {
      setError(err.message || '撤回失败')
    } finally {
      setBusy(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    const target = deleteTarget
    setDeleteTarget(null)
    setBusy(true)
    setError(null)
    setSuccess(null)
    try {
      await deleteClientRelease(target.id)
      if (selectedRelease?.id === target.id) setDetailOpen(false)
      setSuccess('发布草稿已删除')
      await fetchData()
    } catch (err: any) {
      setError(err.message || '删除草稿失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 2, mb: 3 }}>
        <Box sx={{ flex: 1, minWidth: { xs: '100%', sm: 240 } }}>
          <Typography variant="h5" sx={{ fontWeight: 600 }}>客户端发布</Typography>
          <Typography variant="body2" color="text.secondary">按平台和架构维护可验证的安装包版本</Typography>
        </Box>
        <Tooltip title="刷新列表"><Button variant="outlined" onClick={() => void fetchData()} disabled={loading} sx={{ minWidth: 44, px: 1 }}><Refresh /></Button></Tooltip>
        <Button variant="contained" startIcon={<Add />} onClick={() => { resetReleaseForm(); setCreateOpen(true) }}>新建发布草稿</Button>
      </Box>

      {success && <Alert severity="success" sx={{ mb: 2 }} onClose={() => setSuccess(null)}>{success}</Alert>}
      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>{error}</Alert>}

      <Stack direction="row" useFlexGap flexWrap="wrap" spacing={1.5} sx={{ mb: 2 }}>
        <TextField size="small" label="应用 ID" value={appIDFilter} onChange={(event) => { setAppIDFilter(event.target.value); setPage(0) }} sx={{ width: { xs: '100%', sm: 190 } }} />
        <TextField size="small" label="版本" value={versionFilter} onChange={(event) => { setVersionFilter(event.target.value); setPage(0) }} sx={{ width: { xs: '100%', sm: 150 } }} />
        <FormControl size="small" sx={{ minWidth: 140 }}>
          <InputLabel>状态</InputLabel>
          <Select value={statusFilter} label="状态" onChange={(event) => { setStatusFilter(event.target.value as ClientReleaseStatus | ''); setPage(0) }}>
            <MenuItem value="">全部状态</MenuItem><MenuItem value="draft">草稿</MenuItem><MenuItem value="published">已发布</MenuItem><MenuItem value="withdrawn">已撤回</MenuItem>
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 140 }}>
          <InputLabel>频道</InputLabel>
          <Select value={channelFilter} label="频道" onChange={(event) => { setChannelFilter(event.target.value as ClientReleaseChannel | ''); setPage(0) }}>
            <MenuItem value="">全部频道</MenuItem><MenuItem value="stable">stable</MenuItem><MenuItem value="beta">beta</MenuItem>
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 140 }}>
          <InputLabel>平台</InputLabel>
          <Select value={platformFilter} label="平台" onChange={(event) => { setPlatformFilter(event.target.value as ClientPlatform | ''); setPage(0) }}>
            <MenuItem value="">全部平台</MenuItem><MenuItem value="android">Android</MenuItem><MenuItem value="windows">Windows</MenuItem><MenuItem value="macos">macOS</MenuItem><MenuItem value="ios">iOS</MenuItem>
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 140 }}>
          <InputLabel>架构</InputLabel>
          <Select value={archFilter} label="架构" onChange={(event) => { setArchFilter(event.target.value as ClientArch | ''); setPage(0) }}>
            <MenuItem value="">全部架构</MenuItem><MenuItem value="armv7">armv7</MenuItem><MenuItem value="arm64">arm64</MenuItem><MenuItem value="x86_64">x86_64</MenuItem><MenuItem value="universal">universal</MenuItem>
          </Select>
        </FormControl>
      </Stack>

      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead><TableRow><TableCell>应用 / 版本</TableCell><TableCell>频道</TableCell><TableCell>发布矩阵</TableCell><TableCell>状态</TableCell><TableCell>创建时间</TableCell><TableCell align="right">操作</TableCell></TableRow></TableHead>
          <TableBody>
            {loading && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 5 }}>加载中...</TableCell></TableRow>}
            {!loading && items.length === 0 && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 5 }}>暂无客户端发布记录</TableCell></TableRow>}
            {items.map((release) => <TableRow key={release.id} hover>
              <TableCell><Typography fontFamily="monospace">{release.app_id}</Typography><Typography fontFamily="monospace" variant="body2">v{release.version}</Typography></TableCell>
              <TableCell><Chip size="small" label={release.channel} color={release.channel === 'stable' ? 'primary' : 'secondary'} variant="outlined" /></TableCell>
              <TableCell>{release.artifacts.length ? release.artifacts.map((artifact) => <Chip key={artifact.id} size="small" sx={{ mr: 0.5, mb: 0.5 }} label={`${artifact.platform} ${artifact.arch} · ${artifact.package_type}`} />) : <Typography variant="caption" color="text.secondary">尚无安装包</Typography>}</TableCell>
              <TableCell>{statusChip(release.status)}</TableCell>
              <TableCell>{new Date(release.create_time).toLocaleString()}</TableCell>
              <TableCell align="right"><Tooltip title="查看和管理"><Button size="small" onClick={() => void openDetail(release)} sx={{ minWidth: 36, px: 0.75 }}><Info fontSize="small" /></Button></Tooltip></TableCell>
            </TableRow>)}
          </TableBody>
        </Table>
        <TablePagination
          component="div"
          count={total}
          page={page}
          rowsPerPage={rowsPerPage}
          rowsPerPageOptions={[10, 20, 50, 100]}
          onPageChange={(_, nextPage) => setPage(nextPage)}
          onRowsPerPageChange={(event) => { setRowsPerPage(Number(event.target.value)); setPage(0) }}
          labelRowsPerPage="每页"
        />
      </TableContainer>

      <Dialog open={createOpen} onClose={() => !busy && setCreateOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>新建客户端发布草稿</DialogTitle>
        <DialogContent><Stack spacing={2} sx={{ mt: 1 }}>
          <TextField label="应用 ID" size="small" value={releaseForm.appID} onChange={(event) => setReleaseForm({ ...releaseForm, appID: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.appID } }} required />
          <TextField label="版本号" size="small" placeholder="1.2.0 或 1.2.0-beta.1" value={releaseForm.version} onChange={(event) => setReleaseForm({ ...releaseForm, version: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.version } }} required error={!!releaseForm.version && !SEMVER_RE.test(releaseForm.version)} />
          <FormControl size="small"><InputLabel>频道</InputLabel><Select value={releaseForm.channel} label="频道" onChange={(event) => setReleaseForm({ ...releaseForm, channel: event.target.value as ClientReleaseChannel })}><MenuItem value="stable">stable</MenuItem><MenuItem value="beta">beta</MenuItem></Select></FormControl>
          <TextField label="标题" size="small" value={releaseForm.title} onChange={(event) => setReleaseForm({ ...releaseForm, title: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.title } }} />
          <TextField label="最低支持版本" size="small" placeholder="可选，例如 1.0.0" value={releaseForm.minSupportedVersion} onChange={(event) => setReleaseForm({ ...releaseForm, minSupportedVersion: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.version } }} />
          <TextField label="更新日志" size="small" multiline minRows={3} value={releaseForm.changelog} onChange={(event) => setReleaseForm({ ...releaseForm, changelog: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.changelog } }} />
          <FormControlLabel control={<Checkbox checked={releaseForm.forceUpdate} onChange={(event) => setReleaseForm({ ...releaseForm, forceUpdate: event.target.checked })} />} label="标记为强制更新" />
        </Stack></DialogContent>
        <DialogActions><Button onClick={() => setCreateOpen(false)} disabled={busy}>取消</Button><Button variant="contained" onClick={() => void handleCreate()} disabled={busy || !releaseForm.appID || !SEMVER_RE.test(releaseForm.version)}>{busy ? '创建中...' : '创建草稿'}</Button></DialogActions>
      </Dialog>

      <Dialog open={detailOpen} onClose={() => !busy && setDetailOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>{selectedRelease ? `${selectedRelease.app_id} v${selectedRelease.version}` : '发布详情'}</DialogTitle>
        <DialogContent>{selectedRelease && <Stack spacing={2} sx={{ mt: 1 }}>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems={{ sm: 'center' }}><Typography variant="body2">{statusChip(selectedRelease.status)}</Typography><Typography variant="body2" color="text.secondary">{artifactMatrix}</Typography></Stack>
          {selectedRelease.changelog && <Paper variant="outlined" sx={{ p: 1.5 }}><Typography variant="caption" color="text.secondary">更新日志</Typography><Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>{selectedRelease.changelog}</Typography></Paper>}

          <TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>平台</TableCell><TableCell>架构</TableCell><TableCell>类型</TableCell><TableCell>文件</TableCell><TableCell>大小</TableCell><TableCell>SHA-256</TableCell></TableRow></TableHead><TableBody>
            {selectedRelease.artifacts.length === 0 && <TableRow><TableCell colSpan={6} align="center">暂无安装包</TableCell></TableRow>}
            {selectedRelease.artifacts.map((artifact) => <TableRow key={artifact.id}><TableCell>{artifact.platform}</TableCell><TableCell>{artifact.arch}{artifact.android_abi && artifact.android_abi !== 'universal' ? ` (${artifact.android_abi})` : ''}</TableCell><TableCell>{artifact.package_type}</TableCell><TableCell>{artifact.file_name}</TableCell><TableCell>{formatFileSize(artifact.file_size)}</TableCell><TableCell><Typography fontFamily="monospace" variant="caption">{artifact.sha256 ? `${artifact.sha256.slice(0, 16)}...` : '-'}</Typography></TableCell></TableRow>)}
          </TableBody></Table></TableContainer>

          {selectedRelease.status === 'draft' && <Paper variant="outlined" sx={{ p: 2 }}><Typography variant="subtitle2" sx={{ mb: 1.5 }}>添加安装包</Typography><Stack spacing={1.5}>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5}><FormControl size="small" fullWidth><InputLabel>平台</InputLabel><Select value={artifactForm.platform} label="平台" onChange={(event) => setArtifactPlatform(event.target.value as ClientPlatform)}><MenuItem value="android">Android</MenuItem><MenuItem value="windows">Windows</MenuItem><MenuItem value="macos">macOS</MenuItem><MenuItem value="ios">iOS</MenuItem></Select></FormControl><FormControl size="small" fullWidth><InputLabel>架构</InputLabel><Select value={artifactForm.arch} label="架构" onChange={(event) => setArtifactForm({ ...artifactForm, arch: event.target.value as ClientArch })}>{architecturesFor(artifactForm.platform, artifactForm.packageType).map((arch) => <MenuItem key={arch} value={arch}>{arch}</MenuItem>)}</Select></FormControl><FormControl size="small" fullWidth><InputLabel>包类型</InputLabel><Select value={artifactForm.packageType} label="包类型" onChange={(event) => setArtifactPackageType(event.target.value as ClientPackageType)}>{packageTypesFor(artifactForm.platform).map((packageType) => <MenuItem key={packageType} value={packageType}>{packageType}</MenuItem>)}</Select></FormControl></Stack>
            {artifactForm.packageType === 'app_store' ? <TextField label="App Store / TestFlight 地址" size="small" type="url" value={artifactForm.externalURL} onChange={(event) => setArtifactForm({ ...artifactForm, externalURL: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.externalURL } }} required /> : <Button component="label" variant="outlined" startIcon={<CloudUpload />} disabled={busy}>{artifactForm.file ? artifactForm.file.name : '选择安装包文件'}<input hidden type="file" accept={packageFileAccept(artifactForm.packageType)} onChange={(event) => setArtifactForm({ ...artifactForm, file: event.target.files?.[0] || null })} /></Button>}
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5}><TextField label="构建号" size="small" value={artifactForm.buildNumber} onChange={(event) => setArtifactForm({ ...artifactForm, buildNumber: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.buildNumber } }} fullWidth /><TextField label="最低系统版本" size="small" placeholder="可选，例如 12.0.0" value={artifactForm.minOSVersion} onChange={(event) => setArtifactForm({ ...artifactForm, minOSVersion: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.version } }} fullWidth />{artifactForm.platform === 'android' && <TextField label="最低 Android API" size="small" type="number" value={artifactForm.minAndroidAPI} onChange={(event) => setArtifactForm({ ...artifactForm, minAndroidAPI: event.target.value })} slotProps={{ htmlInput: { min: 1, max: 1000, step: 1 } }} sx={{ minWidth: 150 }} />}</Stack>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5}><TextField label="签名算法" size="small" placeholder="例如 ed25519" value={artifactForm.signatureAlgorithm} onChange={(event) => setArtifactForm({ ...artifactForm, signatureAlgorithm: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.signatureAlgorithm } }} fullWidth /><TextField label="安装包签名" size="small" value={artifactForm.signature} onChange={(event) => setArtifactForm({ ...artifactForm, signature: event.target.value })} slotProps={{ htmlInput: { maxLength: FIELD_LIMITS.signature } }} multiline minRows={2} fullWidth /></Stack>
            {busy && uploadProgress > 0 && <Box><LinearProgress variant="determinate" value={uploadProgress} /><Typography variant="caption" color="text.secondary">上传中 {uploadProgress}%</Typography></Box>}
            <Box><Button variant="contained" startIcon={<CloudUpload />} onClick={() => void handleCompleteArtifact()} disabled={busy}>{busy ? '处理中...' : '验证并添加安装包'}</Button></Box>
          </Stack></Paper>}
        </Stack>}</DialogContent>
        <DialogActions>{selectedRelease?.status === 'draft' && <Button color="error" startIcon={<Delete />} onClick={() => setDeleteTarget(selectedRelease)} disabled={busy}>删除草稿</Button>}<Box sx={{ flex: 1 }} />{selectedRelease?.status === 'draft' && <Button variant="contained" startIcon={<Publish />} onClick={() => setConfirmReleaseAction('publish')} disabled={busy || !selectedRelease.artifacts.length}>发布</Button>}{selectedRelease?.status === 'published' && <Button color="warning" variant="contained" startIcon={<Undo />} onClick={() => setConfirmReleaseAction('withdraw')} disabled={busy}>撤回</Button>}<Button onClick={() => setDetailOpen(false)} disabled={busy}>关闭</Button></DialogActions>
      </Dialog>

      <ConfirmDialog isOpen={!!deleteTarget} title="删除客户端发布草稿" message={`确定删除 ${deleteTarget?.app_id} v${deleteTarget?.version} 草稿吗？已上传的安装包也会被移除。`} type="danger" onConfirm={() => void handleDelete()} onCancel={() => setDeleteTarget(null)} />
      <ConfirmDialog
        isOpen={confirmReleaseAction !== null}
        title={confirmReleaseAction === 'publish' ? '发布客户端版本' : '撤回客户端版本'}
        message={confirmReleaseAction === 'publish'
          ? `确定发布 ${selectedRelease?.app_id} v${selectedRelease?.version} 吗？发布后版本和安装包不可修改。`
          : `确定撤回 ${selectedRelease?.app_id} v${selectedRelease?.version} 吗？公共更新查询将回退到上一可用版本。`}
        confirmText={confirmReleaseAction === 'publish' ? '发布' : '撤回'}
        type="warning"
        onConfirm={() => void (confirmReleaseAction === 'publish' ? handlePublish() : handleWithdraw())}
        onCancel={() => setConfirmReleaseAction(null)}
      />
    </Box>
  )
}
