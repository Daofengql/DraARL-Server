import { useState, useEffect, type FormEvent } from 'react'
import {
  Box, Paper, Typography, Table, TableBody, TableCell, TableContainer,
  TableHead, TableRow, Chip, IconButton, Dialog, DialogTitle, DialogContent,
  DialogActions, Button, TextField, Stack, Alert, Tooltip, TablePagination,
  FormControl, InputLabel, Select, MenuItem, FormControlLabel, Switch,
} from '@mui/material'
import Delete from '@mui/icons-material/Delete'
import Refresh from '@mui/icons-material/Refresh'
import Upload from '@mui/icons-material/Upload'
import SystemUpdate from '@mui/icons-material/SystemUpdate'
import { listFirmware, uploadFirmware, deleteFirmware } from '../../services/firmware'
import type { FirmwareRelease } from '../../services/firmware'
import { DEVICE_MODELS } from '../../utils/deviceModel'
import { getDevModelName } from '../../utils/deviceModel'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

// 固件白名单型号（与后端 protocol.FirmwareSupportedDevModels 一致）
const FIRMWARE_MODELS = DEVICE_MODELS.filter(m => [1, 2].includes(m.value))

const SEMVER_RE = /^\d+\.\d+\.\d+(-[\w.]+)?$/

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(2) + ' MB'
}

export function FirmwarePage() {
  const [items, setItems] = useState<FirmwareRelease[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(20)
  const [filterModel, setFilterModel] = useState<number>(0)

  // 上传弹窗
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [formModel, setFormModel] = useState<number>(1)
  const [formVersion, setFormVersion] = useState('')
  const [formChangelog, setFormChangelog] = useState('')
  const [formFile, setFormFile] = useState<File | null>(null)
  const [versionError, setVersionError] = useState<string | null>(null)

  // 删除确认
  const [deleteTarget, setDeleteTarget] = useState<FirmwareRelease | null>(null)

  const fetchData = async () => {
    setLoading(true)
    setError(null)
    try {
      const result = await listFirmware({
        dev_model: filterModel || undefined,
        page: page + 1,
        page_size: rowsPerPage,
      })
      setItems(result.items || [])
      setTotal(result.total)
    } catch (err: any) {
      setError(err.message || '获取固件列表失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchData() }, [page, rowsPerPage, filterModel])

  const handleUpload = async (e: FormEvent) => {
    e.preventDefault()
    if (!formFile) { setError('请选择固件文件'); return }
    if (!SEMVER_RE.test(formVersion)) { setVersionError('版本号格式无效，如 1.0.0 或 1.0.0-beta.1'); return }

    setUploading(true)
    setError(null)
    try {
      await uploadFirmware({
        file: formFile,
        dev_model: formModel,
        version: formVersion,
        changelog: formChangelog || undefined,
      })
      setSuccess('固件上传成功')
      setUploadOpen(false)
      resetForm()
      fetchData()
    } catch (err: any) {
      setError(err.message || '上传失败')
    } finally {
      setUploading(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await deleteFirmware(deleteTarget.id)
      setSuccess('固件已删除')
      setDeleteTarget(null)
      fetchData()
    } catch (err: any) {
      setError(err.message || '删除失败')
    }
  }

  const resetForm = () => {
    setFormModel(1)
    setFormVersion('')
    setFormChangelog('')
    setFormFile(null)
    setVersionError(null)
  }

  const dismissSuccess = () => setSuccess(null)

  return (
    <Box>
      <Typography variant="h5" sx={{ mb: 3, fontWeight: 600 }}>固件管理</Typography>

      {success && <Alert severity="success" onClose={dismissSuccess} sx={{ mb: 2 }}>{success}</Alert>}
      {error && <Alert severity="error" onClose={() => setError(null)} sx={{ mb: 2 }}>{error}</Alert>}

      {/* 工具栏 */}
      <Stack direction="row" spacing={2} sx={{ mb: 2 }} alignItems="center">
        <FormControl size="small" sx={{ minWidth: 200 }}>
          <InputLabel>设备型号筛选</InputLabel>
          <Select
            value={filterModel}
            label="设备型号筛选"
            onChange={(e) => { setFilterModel(e.target.value as number); setPage(0) }}
          >
            <MenuItem value={0}>全部型号</MenuItem>
            {FIRMWARE_MODELS.map(m => (
              <MenuItem key={m.value} value={m.value}>{m.label}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <Box sx={{ flex: 1 }} />
        <Button variant="contained" startIcon={<Upload />} onClick={() => { resetForm(); setUploadOpen(true) }}>
          上传固件
        </Button>
        <IconButton onClick={fetchData} disabled={loading}>
          <Refresh />
        </IconButton>
      </Stack>

      {/* 表格 */}
      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>ID</TableCell>
              <TableCell>设备型号</TableCell>
              <TableCell>版本号</TableCell>
              <TableCell>文件名</TableCell>
              <TableCell>文件大小</TableCell>
              <TableCell>SHA-256</TableCell>
              <TableCell>状态</TableCell>
              <TableCell>上传时间</TableCell>
              <TableCell align="right">操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading && items.length === 0 && (
              <TableRow>
                <TableCell colSpan={9} align="center" sx={{ py: 4 }}>
                  <Typography color="text.secondary">加载中...</Typography>
                </TableCell>
              </TableRow>
            )}
            {!loading && items.length === 0 && (
              <TableRow>
                <TableCell colSpan={9} align="center" sx={{ py: 4 }}>
                  <Typography color="text.secondary">暂无固件记录</Typography>
                </TableCell>
              </TableRow>
            )}
            {items.map(fw => (
              <TableRow key={fw.id} hover>
                <TableCell>{fw.id}</TableCell>
                <TableCell>{getDevModelName(fw.dev_model)}</TableCell>
                <TableCell><Typography fontFamily="monospace" fontSize="0.875rem">{fw.version}</Typography></TableCell>
                <TableCell>{fw.file_name}</TableCell>
                <TableCell>{formatFileSize(fw.file_size)}</TableCell>
                <TableCell>
                  <Tooltip title={fw.file_hash}>
                    <Typography fontFamily="monospace" fontSize="0.75rem" sx={{ maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {fw.file_hash}
                    </Typography>
                  </Tooltip>
                </TableCell>
                <TableCell>
                  {fw.is_latest ? (
                    <Chip label="最新" color="success" size="small" icon={<SystemUpdate sx={{ fontSize: 16 }} />} />
                  ) : (
                    <Chip label="历史" size="small" variant="outlined" />
                  )}
                </TableCell>
                <TableCell>{new Date(fw.create_time).toLocaleString()}</TableCell>
                <TableCell align="right">
                  <Tooltip title="删除">
                    <IconButton size="small" color="error" onClick={() => setDeleteTarget(fw)}>
                      <Delete fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <TablePagination
          component="div"
          count={total}
          page={page}
          onPageChange={(_, p) => setPage(p)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(e) => { setRowsPerPage(parseInt(e.target.value, 10)); setPage(0) }}
          rowsPerPageOptions={[10, 20, 50]}
        />
      </TableContainer>

      {/* 上传弹窗 */}
      <Dialog open={uploadOpen} onClose={() => setUploadOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>上传固件</DialogTitle>
        <form onSubmit={handleUpload}>
          <DialogContent>
            <Stack spacing={2} sx={{ mt: 1 }}>
              <FormControl size="small" fullWidth required>
                <InputLabel>设备型号</InputLabel>
                <Select value={formModel} label="设备型号" onChange={(e) => setFormModel(e.target.value as number)}>
                  {FIRMWARE_MODELS.map(m => (
                    <MenuItem key={m.value} value={m.value}>{m.label}</MenuItem>
                  ))}
                </Select>
              </FormControl>

              <TextField
                label="版本号"
                placeholder="如 1.0.0 或 1.0.0-beta.1"
                value={formVersion}
                onChange={(e) => {
                  setFormVersion(e.target.value)
                  setVersionError(SEMVER_RE.test(e.target.value) ? null : '版本号格式无效，如 1.0.0 或 1.0.0-beta.1')
                }}
                error={!!versionError}
                helperText={versionError || 'semver 格式: MAJOR.MINOR.PATCH[-prerelease]'}
                size="small"
                fullWidth
                required
              />

              <TextField
                label="更新日志"
                value={formChangelog}
                onChange={(e) => setFormChangelog(e.target.value)}
                size="small"
                fullWidth
                multiline
                rows={3}
              />

              <Button variant="outlined" component="label" startIcon={<Upload />}>
                {formFile ? formFile.name : '选择固件文件'}
                <input type="file" hidden onChange={(e) => setFormFile(e.target.files?.[0] || null)} />
              </Button>
              {formFile && (
                <Typography variant="caption" color="text.secondary">
                  大小: {formatFileSize(formFile.size)}
                  {formFile.size > 16 * 1024 * 1024 && (
                    <Typography component="span" color="error" sx={{ ml: 1 }}>超过 16MB 限制</Typography>
                  )}
                </Typography>
              )}
            </Stack>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setUploadOpen(false)}>取消</Button>
            <Button
              type="submit"
              variant="contained"
              disabled={uploading || !formFile || !formVersion || !!versionError || (formFile?.size ?? 0) > 16 * 1024 * 1024}
            >
              {uploading ? '上传中...' : '上传'}
            </Button>
          </DialogActions>
        </form>
      </Dialog>

      {/* 删除确认 */}
      <ConfirmDialog
        isOpen={!!deleteTarget}
        title="删除固件"
        message={`确定要删除 ${deleteTarget ? getDevModelName(deleteTarget.dev_model) : ''} 固件 v${deleteTarget?.version} 吗？此操作不可恢复。`}
        type="danger"
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </Box>
  )
}
