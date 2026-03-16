import { useState, useEffect, useCallback } from 'react'
import {
  Box,
  Typography,
  Button,
  Paper,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Menu,
  MenuItem,
  Breadcrumbs,
  Chip,
  Alert,
  Snackbar,
  LinearProgress,
  Card,
  CardContent,
  Grid,
} from '@mui/material'
import {
  Folder as FolderIcon,
  InsertDriveFile as FileIcon,
  CreateNewFolder as CreateFolderIcon,
  Upload as UploadIcon,
  Delete as DeleteIcon,
  Edit as EditIcon,
  MoreVert as MoreIcon,
  NavigateNext as NavigateNextIcon,
  Description as DescriptionIcon,
  Image as ImageIcon,
  VideoFile as VideoIcon,
  AudioFile as AudioIcon,
  PictureAsPdf as PdfIcon,
  GridOn as GridIcon,
  ViewList as ListIcon,
  Download as DownloadIcon,
  DriveFileMove as MoveIcon,
  Refresh as RefreshIcon,
} from '@mui/icons-material'
import { ConfirmDialog } from '../../components/common'
import {
  getAssets,
  createFolder,
  uploadFile,
  updateAsset,
  deleteAsset,
  type Asset,
  formatFileSize,
  getFileIcon,
} from '../../services/asset'

type ViewMode = 'list' | 'grid'

export function AssetPage() {
  const [assets, setAssets] = useState<Asset[]>([])
  const [currentFolderId, setCurrentFolderId] = useState<number | null>(null)
  const [breadcrumbs, setBreadcrumbs] = useState<{ id: number | null; name: string }[]>([{ id: null, name: '根目录' }])
  const [loading, setLoading] = useState(false)
  const [viewMode, setViewMode] = useState<ViewMode>('list')

  // 对话框状态
  const [createFolderDialogOpen, setCreateFolderDialogOpen] = useState(false)
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false)
  const [renameDialogOpen, setRenameDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [moveDialogOpen, setMoveDialogOpen] = useState(false)
  const [remarkDialogOpen, setRemarkDialogOpen] = useState(false)

  // 表单数据
  const [newFolderName, setNewFolderName] = useState('')
  const [newFolderRemark, setNewFolderRemark] = useState('')
  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [uploadFileName, setUploadFileName] = useState('')
  const [uploadFileRemark, setUploadFileRemark] = useState('')
  const [renameValue, setRenameValue] = useState('')
  const [remarkValue, setRemarkValue] = useState('')

  // 当前操作的资源
  const [selectedAsset, setSelectedAsset] = useState<Asset | null>(null)
  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null)

  // 消息提示
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({
    open: false,
    message: '',
    severity: 'success',
  })

  // 所有文件夹（用于移动对话框）
  const [allFolders, setAllFolders] = useState<Asset[]>([])
  const [targetFolderId, setTargetFolderId] = useState<number | null | undefined>(undefined)

  // 加载资源列表
  const loadAssets = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getAssets(currentFolderId)
      setAssets(data)
    } catch (err: any) {
      setSnackbar({ open: true, message: err.message || '加载资源失败', severity: 'error' })
    } finally {
      setLoading(false)
    }
  }, [currentFolderId])

  useEffect(() => {
    loadAssets()
  }, [loadAssets])

  // 加载所有文件夹（用于移动）
  const loadAllFolders = async () => {
    try {
      const loadFolderRecursive = async (parentId: number | null): Promise<Asset[]> => {
        const items = await getAssets(parentId)
        let folders: Asset[] = items.filter(item => item.type === 'folder')
        for (const folder of folders) {
          const subFolders = await loadFolderRecursive(folder.id)
          folders = [...folders, ...subFolders]
        }
        return folders
      }
      const folders = await loadFolderRecursive(null)
      setAllFolders(folders)
    } catch (err) {
      console.error('加载文件夹列表失败', err)
    }
  }

  // 导航到文件夹
  const navigateToFolder = (folder: Asset) => {
    setCurrentFolderId(folder.id)
    setBreadcrumbs([...breadcrumbs, { id: folder.id, name: folder.name }])
  }

  // 面包屑导航
  const handleBreadcrumbClick = (index: number) => {
    const newBreadcrumbs = breadcrumbs.slice(0, index + 1)
    setBreadcrumbs(newBreadcrumbs)
    setCurrentFolderId(newBreadcrumbs[index].id)
  }

  // 打开菜单
  const handleMenuOpen = (event: React.MouseEvent<HTMLElement>, asset: Asset) => {
    event.stopPropagation()
    setAnchorEl(event.currentTarget)
    setSelectedAsset(asset)
  }

  // 关闭菜单
  const handleMenuClose = () => {
    setAnchorEl(null)
  }

  // 创建文件夹
  const handleCreateFolder = async () => {
    if (!newFolderName.trim()) {
      setSnackbar({ open: true, message: '请输入文件夹名称', severity: 'error' })
      return
    }
    try {
      await createFolder({
        name: newFolderName.trim(),
        parent_id: currentFolderId,
        remark: newFolderRemark.trim() || undefined,
      })
      setSnackbar({ open: true, message: '创建成功', severity: 'success' })
      setCreateFolderDialogOpen(false)
      setNewFolderName('')
      setNewFolderRemark('')
      loadAssets()
    } catch (err: any) {
      setSnackbar({ open: true, message: err.message || '创建失败', severity: 'error' })
    }
  }

  // 上传文件
  const handleUploadFile = async () => {
    if (!selectedFile) {
      setSnackbar({ open: true, message: '请选择文件', severity: 'error' })
      return
    }
    try {
      await uploadFile(selectedFile, currentFolderId!, uploadFileName.trim() || undefined, uploadFileRemark.trim() || undefined)
      setSnackbar({ open: true, message: '上传成功', severity: 'success' })
      setUploadDialogOpen(false)
      setSelectedFile(null)
      setUploadFileName('')
      setUploadFileRemark('')
      loadAssets()
    } catch (err: any) {
      setSnackbar({ open: true, message: err.message || '上传失败', severity: 'error' })
    }
  }

  // 重命名
  const handleRename = async () => {
    if (!renameValue.trim() || !selectedAsset) return
    try {
      await updateAsset(selectedAsset.id, { name: renameValue.trim() })
      setSnackbar({ open: true, message: '重命名成功', severity: 'success' })
      setRenameDialogOpen(false)
      setSelectedAsset(null)
      loadAssets()
    } catch (err: any) {
      setSnackbar({ open: true, message: err.message || '重命名失败', severity: 'error' })
    }
  }

  // 编辑备注
  const handleUpdateRemark = async () => {
    if (!selectedAsset) return
    try {
      await updateAsset(selectedAsset.id, { remark: remarkValue.trim() })
      setSnackbar({ open: true, message: '更新成功', severity: 'success' })
      setRemarkDialogOpen(false)
      setSelectedAsset(null)
      loadAssets()
    } catch (err: any) {
      setSnackbar({ open: true, message: err.message || '更新失败', severity: 'error' })
    }
  }

  // 删除
  const handleDelete = async () => {
    if (!selectedAsset) return
    try {
      await deleteAsset(selectedAsset.id)
      setSnackbar({ open: true, message: '删除成功', severity: 'success' })
      setDeleteDialogOpen(false)
      setSelectedAsset(null)
      loadAssets()
    } catch (err: any) {
      setSnackbar({ open: true, message: err.message || '删除失败', severity: 'error' })
    }
  }

  // 移动
  const handleMove = async () => {
    if (!selectedAsset) return
    try {
      // 调用移动 API
      const res = await fetch(`/api/admin/assets/${selectedAsset.id}/move`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem('token')}`,
        },
        body: JSON.stringify({ target_parent_id: targetFolderId === undefined ? null : targetFolderId }),
      })
      const data = await res.json()
      if (data.code !== 200) {
        throw new Error(data.message || '移动失败')
      }
      setSnackbar({ open: true, message: '移动成功', severity: 'success' })
      setMoveDialogOpen(false)
      setSelectedAsset(null)
      setTargetFolderId(undefined)
      loadAssets()
    } catch (err: any) {
      setSnackbar({ open: true, message: err.message || '移动失败', severity: 'error' })
    }
  }

  // 获取文件图标
  const renderFileIcon = (asset: Asset) => {
    if (asset.type === 'folder') {
      return <FolderIcon sx={{ color: 'primary.main', fontSize: 28 }} />
    }
    const iconType = getFileIcon(asset.mime_type)
    switch (iconType) {
      case 'image':
        return <ImageIcon sx={{ color: 'success.main', fontSize: 28 }} />
      case 'video':
        return <VideoIcon sx={{ color: 'error.main', fontSize: 28 }} />
      case 'audio':
        return <AudioIcon sx={{ color: 'secondary.main', fontSize: 28 }} />
      case 'pdf':
        return <PdfIcon sx={{ color: 'error.main', fontSize: 28 }} />
      default:
        return <DescriptionIcon sx={{ color: 'grey.500', fontSize: 28 }} />
    }
  }

  // 渲染列表项
  const renderListItem = (asset: Asset) => (
    <ListItem
      key={asset.id}
      secondaryAction={
        <IconButton edge="end" onClick={(e) => handleMenuOpen(e, asset)}>
          <MoreIcon />
        </IconButton>
      }
      disablePadding
      sx={{ mb: 0.5, bgcolor: 'background.paper', borderRadius: 1 }}
    >
      <ListItemButton
        onClick={() => asset.type === 'folder' && navigateToFolder(asset)}
        sx={{ py: 1 }}
      >
        <ListItemIcon>{renderFileIcon(asset)}</ListItemIcon>
        <ListItemText
          primary={asset.name}
          secondary={
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 0.5 }}>
              {asset.type === 'folder' ? (
                <Typography variant="caption" color="text.secondary">
                  {asset.folder_count || 0} 个文件夹 · {asset.file_count || 0} 个文件
                </Typography>
              ) : (
                <>
                  <Typography variant="caption" color="text.secondary">
                    {formatFileSize(asset.size)}
                  </Typography>
                  {asset.mime_type && (
                    <Chip label={asset.mime_type.split('/')[1]} size="small" sx={{ height: 20, fontSize: '0.7rem' }} />
                  )}
                </>
              )}
            </Box>
          }
        />
      </ListItemButton>
    </ListItem>
  )

  // 渲染网格卡片
  const renderGridItem = (asset: Asset) => (
    <Grid key={asset.id} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
      <Card
        sx={{
          cursor: asset.type === 'folder' ? 'pointer' : 'default',
          '&:hover': { boxShadow: 4 },
          position: 'relative',
        }}
        onClick={() => asset.type === 'folder' && navigateToFolder(asset)}
      >
        <CardContent sx={{ textAlign: 'center', py: 3 }}>
          <Box sx={{ mb: 1 }}>{renderFileIcon(asset)}</Box>
          <Typography variant="subtitle2" noWrap title={asset.name}>
            {asset.name}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {asset.type === 'folder'
              ? `${asset.folder_count || 0} 文件夹 · ${asset.file_count || 0} 文件`
              : formatFileSize(asset.size)}
          </Typography>
        </CardContent>
        <IconButton
          size="small"
          sx={{ position: 'absolute', top: 4, right: 4 }}
          onClick={(e) => {
            e.stopPropagation()
            handleMenuOpen(e, asset)
          }}
        >
          <MoreIcon fontSize="small" />
        </IconButton>
      </Card>
    </Grid>
  )

  return (
    <Box>
      {/* 标题栏 */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h5" sx={{ fontWeight: 600 }}>
          资源管理
        </Typography>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            variant="outlined"
            startIcon={<RefreshIcon />}
            onClick={() => loadAssets()}
            disabled={loading}
          >
            刷新
          </Button>
          <Button
            variant={viewMode === 'list' ? 'contained' : 'outlined'}
            size="small"
            onClick={() => setViewMode('list')}
          >
            <ListIcon />
          </Button>
          <Button
            variant={viewMode === 'grid' ? 'contained' : 'outlined'}
            size="small"
            onClick={() => setViewMode('grid')}
          >
            <GridIcon />
          </Button>
        </Box>
      </Box>

      {/* 面包屑导航 */}
      <Paper sx={{ p: 2, mb: 2, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Breadcrumbs separator={<NavigateNextIcon fontSize="small" />}>
          {breadcrumbs.map((crumb, index) => (
            <Chip
              key={crumb.id ?? 'root'}
              label={crumb.name}
              onClick={() => handleBreadcrumbClick(index)}
              sx={{ cursor: 'pointer' }}
              color={index === breadcrumbs.length - 1 ? 'primary' : 'default'}
            />
          ))}
        </Breadcrumbs>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            variant="contained"
            startIcon={<CreateFolderIcon />}
            onClick={() => setCreateFolderDialogOpen(true)}
            size="small"
          >
            新建文件夹
          </Button>
          <Button
            variant="contained"
            startIcon={<UploadIcon />}
            onClick={() => {
              if (currentFolderId === null) {
                setSnackbar({ open: true, message: '请先选择一个文件夹再上传文件', severity: 'error' })
                return
              }
              setUploadDialogOpen(true)
            }}
            size="small"
          >
            上传文件
          </Button>
        </Box>
      </Paper>

      {/* 加载状态 */}
      {loading && <LinearProgress sx={{ mb: 2 }} />}

      {/* 资源列表 */}
      <Paper sx={{ p: 2 }}>
        {assets.length === 0 ? (
          <Box sx={{ textAlign: 'center', py: 6, color: 'text.secondary' }}>
            <FolderIcon sx={{ fontSize: 64, opacity: 0.3 }} />
            <Typography sx={{ mt: 2 }}>当前目录为空</Typography>
            <Typography variant="caption">点击上方按钮创建文件夹或上传文件</Typography>
          </Box>
        ) : viewMode === 'list' ? (
          <List sx={{ p: 0 }}>{assets.map(renderListItem)}</List>
        ) : (
          <Grid container spacing={2}>
            {assets.map(renderGridItem)}
          </Grid>
        )}
      </Paper>

      {/* 右键菜单 */}
      <Menu anchorEl={anchorEl} open={Boolean(anchorEl)} onClose={handleMenuClose}>
        {selectedAsset?.type === 'file' && (
          <MenuItem
            onClick={() => {
              handleMenuClose()
              if (selectedAsset?.download_url) {
                window.open(selectedAsset.download_url, '_blank')
              }
            }}
          >
            <ListItemIcon>
              <DownloadIcon fontSize="small" />
            </ListItemIcon>
            <ListItemText>下载</ListItemText>
          </MenuItem>
        )}
        <MenuItem
          onClick={() => {
            handleMenuClose()
            setRenameValue(selectedAsset?.name || '')
            setRenameDialogOpen(true)
          }}
        >
          <ListItemIcon>
            <EditIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText>重命名</ListItemText>
        </MenuItem>
        <MenuItem
          onClick={() => {
            handleMenuClose()
            setRemarkValue(selectedAsset?.remark || '')
            setRemarkDialogOpen(true)
          }}
        >
          <ListItemIcon>
            <DescriptionIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText>编辑备注</ListItemText>
        </MenuItem>
        <MenuItem
          onClick={async () => {
            handleMenuClose()
            await loadAllFolders()
            setTargetFolderId(selectedAsset?.parent_id)
            setMoveDialogOpen(true)
          }}
        >
          <ListItemIcon>
            <MoveIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText>移动</ListItemText>
        </MenuItem>
        <MenuItem
          onClick={() => {
            handleMenuClose()
            setDeleteDialogOpen(true)
          }}
          sx={{ color: 'error.main' }}
        >
          <ListItemIcon>
            <DeleteIcon fontSize="small" color="error" />
          </ListItemIcon>
          <ListItemText>删除</ListItemText>
        </MenuItem>
      </Menu>

      {/* 创建文件夹对话框 */}
      <Dialog open={createFolderDialogOpen} onClose={() => setCreateFolderDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>新建文件夹</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="文件夹名称"
            fullWidth
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            sx={{ mb: 2 }}
          />
          <TextField
            margin="dense"
            label="备注（可选）"
            fullWidth
            multiline
            rows={2}
            value={newFolderRemark}
            onChange={(e) => setNewFolderRemark(e.target.value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreateFolderDialogOpen(false)}>取消</Button>
          <Button variant="contained" onClick={handleCreateFolder}>
            创建
          </Button>
        </DialogActions>
      </Dialog>

      {/* 上传文件对话框 */}
      <Dialog open={uploadDialogOpen} onClose={() => setUploadDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>上传文件</DialogTitle>
        <DialogContent>
          <Button variant="outlined" component="label" fullWidth sx={{ py: 3, mb: 2 }}>
            {selectedFile ? selectedFile.name : '选择文件'}
            <input
              type="file"
              hidden
              onChange={(e) => {
                const file = e.target.files?.[0]
                if (file) {
                  setSelectedFile(file)
                  setUploadFileName(file.name.replace(/\.[^/.]+$/, ''))
                }
              }}
            />
          </Button>
          <TextField
            margin="dense"
            label="显示名称（可选）"
            fullWidth
            value={uploadFileName}
            onChange={(e) => setUploadFileName(e.target.value)}
            sx={{ mb: 2 }}
            helperText="不填写则使用原文件名"
          />
          <TextField
            margin="dense"
            label="备注（可选）"
            fullWidth
            multiline
            rows={2}
            value={uploadFileRemark}
            onChange={(e) => setUploadFileRemark(e.target.value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setUploadDialogOpen(false)}>取消</Button>
          <Button variant="contained" onClick={handleUploadFile} disabled={!selectedFile}>
            上传
          </Button>
        </DialogActions>
      </Dialog>

      {/* 重命名对话框 */}
      <Dialog open={renameDialogOpen} onClose={() => setRenameDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>重命名</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="名称"
            fullWidth
            value={renameValue}
            onChange={(e) => setRenameValue(e.target.value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRenameDialogOpen(false)}>取消</Button>
          <Button variant="contained" onClick={handleRename}>
            保存
          </Button>
        </DialogActions>
      </Dialog>

      {/* 编辑备注对话框 */}
      <Dialog open={remarkDialogOpen} onClose={() => setRemarkDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>编辑备注</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="备注"
            fullWidth
            multiline
            rows={3}
            value={remarkValue}
            onChange={(e) => setRemarkValue(e.target.value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRemarkDialogOpen(false)}>取消</Button>
          <Button variant="contained" onClick={handleUpdateRemark}>
            保存
          </Button>
        </DialogActions>
      </Dialog>

      {/* 移动对话框 */}
      <Dialog open={moveDialogOpen} onClose={() => setMoveDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>移动到</DialogTitle>
        <DialogContent>
          <TextField
            select
            margin="dense"
            label="目标文件夹"
            fullWidth
            value={targetFolderId === null ? '' : targetFolderId === undefined ? '' : targetFolderId}
            onChange={(e) => setTargetFolderId(e.target.value === '' ? null : Number(e.target.value))}
          >
            <MenuItem value="">根目录</MenuItem>
            {allFolders
              .filter(f => f.id !== selectedAsset?.id)
              .map(f => (
                <MenuItem key={f.id} value={f.id}>
                  {f.name}
                </MenuItem>
              ))}
          </TextField>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setMoveDialogOpen(false)}>取消</Button>
          <Button variant="contained" onClick={handleMove}>
            移动
          </Button>
        </DialogActions>
      </Dialog>

      {/* 删除确认对话框 */}
      <ConfirmDialog
        isOpen={deleteDialogOpen}
        title="确认删除"
        type="danger"
        message={
          selectedAsset?.type === 'folder'
            ? `确定要删除文件夹 "${selectedAsset?.name}" 吗？文件夹内的所有内容也会被删除。`
            : `确定要删除文件 "${selectedAsset?.name}" 吗？`
        }
        onConfirm={handleDelete}
        onCancel={() => setDeleteDialogOpen(false)}
      />

      {/* 消息提示 */}
      <Snackbar
        open={snackbar.open}
        autoHideDuration={4000}
        onClose={() => setSnackbar({ ...snackbar, open: false })}
        anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} onClose={() => setSnackbar({ ...snackbar, open: false })}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </Box>
  )
}

export default AssetPage
