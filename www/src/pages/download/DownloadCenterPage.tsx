import { useState, useEffect } from 'react'
import {
  Box,
  Typography,
  Container,
  Paper,
  Card,
  CardContent,
  CardActionArea,
  Grid,
  Chip,
  IconButton,
  Breadcrumbs,
  Link,
  Skeleton,
  Alert,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Divider,
} from '@mui/material'
import {
  Folder as FolderIcon,
  InsertDriveFile as FileIcon,
  Description as DescriptionIcon,
  Image as ImageIcon,
  VideoFile as VideoIcon,
  AudioFile as AudioIcon,
  PictureAsPdf as PdfIcon,
  Download as DownloadIcon,
  ArrowBack as ArrowBackIcon,
  Home as HomeIcon,
  CloudDownload as CloudDownloadIcon,
} from '@mui/icons-material'
import { MainLayout } from '../../components/layout'
import {
  getAssetTree,
  getFolderFiles,
  getDownloadUrl,
  type Asset,
  type AssetTreeItem,
  formatFileSize,
} from '../../services/asset'

// 获取文件图标
const getFileIcon = (mimeType?: string) => {
  if (!mimeType) return <FileIcon sx={{ color: 'grey.500' }} />
  if (mimeType.startsWith('image/')) return <ImageIcon sx={{ color: 'success.main' }} />
  if (mimeType.startsWith('video/')) return <VideoIcon sx={{ color: 'error.main' }} />
  if (mimeType.startsWith('audio/')) return <AudioIcon sx={{ color: 'secondary.main' }} />
  if (mimeType === 'application/pdf') return <PdfIcon sx={{ color: 'error.main' }} />
  return <DescriptionIcon sx={{ color: 'grey.500' }} />
}

// 颜色配置
const categoryColors = [
  { bg: '#E3F2FD', text: '#1565C0', icon: '#1976D2' },
  { bg: '#F3E5F5', text: '#7B1FA2', icon: '#9C27B0' },
  { bg: '#E8F5E9', text: '#2E7D32', icon: '#43A047' },
  { bg: '#FFF3E0', text: '#E65100', icon: '#FB8C00' },
  { bg: '#FFEBEE', text: '#C62828', icon: '#E53935' },
  { bg: '#E0F7FA', text: '#00838F', icon: '#00ACC1' },
  { bg: '#FFF8E1', text: '#F9A825', icon: '#FBC02D' },
  { bg: '#FBE9E7', text: '#D84315', icon: '#FF5722' },
]

export function DownloadCenterPage() {
  const [categories, setCategories] = useState<AssetTreeItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // 当前浏览状态
  const [currentFolder, setCurrentFolder] = useState<{ id: number; name: string } | null>(null)
  const [files, setFiles] = useState<Asset[]>([])
  const [filesLoading, setFilesLoading] = useState(false)

  // 下载对话框
  const [downloadDialogOpen, setDownloadDialogOpen] = useState(false)
  const [selectedFile, setSelectedFile] = useState<Asset | null>(null)
  const [downloadUrl, setDownloadUrl] = useState<string | null>(null)
  const [downloading, setDownloading] = useState(false)

  // 加载分类列表
  useEffect(() => {
    const loadCategories = async () => {
      try {
        setLoading(true)
        const data = await getAssetTree()
        setCategories(data)
        setError(null)
      } catch (err: any) {
        setError(err.message || '加载失败')
      } finally {
        setLoading(false)
      }
    }
    loadCategories()
  }, [])

  // 进入文件夹
  const handleEnterFolder = async (folder: AssetTreeItem) => {
    setCurrentFolder({ id: folder.id, name: folder.name })
    setFilesLoading(true)
    try {
      const data = await getFolderFiles(folder.id)
      setFiles(data.files || [])
    } catch (err: any) {
      console.error('加载文件列表失败', err)
      setFiles([])
    } finally {
      setFilesLoading(false)
    }
  }

  // 返回分类列表
  const handleBackToCategories = () => {
    setCurrentFolder(null)
    setFiles([])
  }

  // 下载文件
  const handleDownload = async (file: Asset) => {
    setSelectedFile(file)
    setDownloadDialogOpen(true)
    setDownloading(true)
    try {
      const data = await getDownloadUrl(file.id)
      setDownloadUrl(data.download_url)
    } catch (err: any) {
      console.error('获取下载链接失败', err)
    } finally {
      setDownloading(false)
    }
  }

  // 执行下载
  const executeDownload = () => {
    if (downloadUrl) {
      window.open(downloadUrl, '_blank')
    }
    setDownloadDialogOpen(false)
    setDownloadUrl(null)
    setSelectedFile(null)
  }

  // 渲染加载骨架屏
  const renderSkeletons = () => (
    <Grid container spacing={3}>
      {[1, 2, 3, 4].map((i) => (
        <Grid key={i} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
          <Paper sx={{ p: 3, height: '100%' }}>
            <Skeleton variant="circular" width={48} height={48} sx={{ mb: 2 }} />
            <Skeleton variant="text" width="80%" />
            <Skeleton variant="text" width="60%" />
          </Paper>
        </Grid>
      ))}
    </Grid>
  )

  // 渲染分类卡片
  const renderCategories = () => (
    <Grid container spacing={3}>
      {categories.map((category, index) => {
        const colorConfig = categoryColors[index % categoryColors.length]
        return (
          <Grid key={category.id} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
            <Card
              sx={{
                height: '100%',
                transition: 'transform 0.2s, box-shadow 0.2s',
                '&:hover': {
                  transform: 'translateY(-4px)',
                  boxShadow: 4,
                },
              }}
            >
              <CardActionArea
                onClick={() => handleEnterFolder(category)}
                sx={{ height: '100%', p: 2 }}
              >
                <CardContent sx={{ textAlign: 'center' }}>
                  <Box
                    sx={{
                      width: 64,
                      height: 64,
                      borderRadius: 2,
                      bgcolor: colorConfig.bg,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      mx: 'auto',
                      mb: 2,
                    }}
                  >
                    <FolderIcon sx={{ fontSize: 32, color: colorConfig.icon }} />
                  </Box>
                  <Typography variant="h6" sx={{ fontWeight: 600, mb: 1, color: 'text.primary' }}>
                    {category.name}
                  </Typography>
                  {category.remark && (
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                      {category.remark}
                    </Typography>
                  )}
                  <Chip
                    label={`${category.file_count || 0} 个文件`}
                    size="small"
                    sx={{ bgcolor: colorConfig.bg, color: colorConfig.text }}
                  />
                </CardContent>
              </CardActionArea>
            </Card>
          </Grid>
        )
      })}
    </Grid>
  )

  // 渲染文件列表
  const renderFiles = () => (
    <Box>
      {/* 返回按钮和面包屑 */}
      <Box sx={{ mb: 3, display: 'flex', alignItems: 'center', gap: 2 }}>
        <Button
          startIcon={<ArrowBackIcon />}
          onClick={handleBackToCategories}
          sx={{ color: 'text.secondary' }}
        >
          返回分类
        </Button>
        <Breadcrumbs separator="/">
          <Link
            component="button"
            onClick={handleBackToCategories}
            sx={{ display: 'flex', alignItems: 'center', color: 'text.secondary' }}
          >
            <HomeIcon sx={{ mr: 0.5 }} fontSize="inherit" />
            下载中心
          </Link>
          <Typography color="text.primary" sx={{ display: 'flex', alignItems: 'center' }}>
            <FolderIcon sx={{ mr: 0.5 }} fontSize="inherit" />
            {currentFolder?.name}
          </Typography>
        </Breadcrumbs>
      </Box>

      {/* 文件列表 */}
      <Paper sx={{ p: 2 }}>
        {filesLoading ? (
          <Box sx={{ textAlign: 'center', py: 4 }}>
            <Skeleton variant="rectangular" height={400} />
          </Box>
        ) : files.length === 0 ? (
          <Box sx={{ textAlign: 'center', py: 6, color: 'text.secondary' }}>
            <FolderIcon sx={{ fontSize: 64, opacity: 0.3 }} />
            <Typography sx={{ mt: 2 }}>该分类下暂无文件</Typography>
          </Box>
        ) : (
          <List>
            {files.map((file, index) => (
              <Box key={file.id}>
                {index > 0 && <Divider />}
                <ListItem
                  secondaryAction={
                    <IconButton edge="end" onClick={() => handleDownload(file)}>
                      <DownloadIcon />
                    </IconButton>
                  }
                >
                  <ListItemIcon>{getFileIcon(file.mime_type)}</ListItemIcon>
                  <ListItemText
                    primary={file.name}
                    secondary={
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 0.5 }}>
                        <Typography variant="caption" color="text.secondary">
                          {formatFileSize(file.size)}
                        </Typography>
                        {file.mime_type && (
                          <Chip
                            label={file.mime_type.split('/')[1]?.toUpperCase()}
                            size="small"
                            sx={{ height: 18, fontSize: '0.65rem' }}
                          />
                        )}
                        {file.remark && (
                          <Typography variant="caption" color="text.secondary">
                            · {file.remark}
                          </Typography>
                        )}
                      </Box>
                    }
                  />
                </ListItem>
              </Box>
            ))}
          </List>
        )}
      </Paper>
    </Box>
  )

  return (
    <MainLayout>
      <Container maxWidth="xl" sx={{ py: 4 }}>
        {/* 页面标题 */}
        <Box sx={{ mb: 4, textAlign: 'center' }}>
          <CloudDownloadIcon sx={{ fontSize: 48, color: 'primary.main', mb: 2 }} />
          <Typography variant="h4" sx={{ fontWeight: 700, color: 'text.primary' }}>
            下载中心
          </Typography>
          <Typography variant="body1" color="text.secondary" sx={{ mt: 1 }}>
            获取客户端安装包、技术文档等资源
          </Typography>
        </Box>

        {/* 错误提示 */}
        {error && (
          <Alert severity="error" sx={{ mb: 3 }}>
            {error}
          </Alert>
        )}

        {/* 内容区域 */}
        {loading ? (
          renderSkeletons()
        ) : currentFolder ? (
          renderFiles()
        ) : categories.length === 0 ? (
          <Paper sx={{ p: 6, textAlign: 'center' }}>
            <FolderIcon sx={{ fontSize: 64, color: 'grey.300', mb: 2 }} />
            <Typography variant="h6" color="text.secondary">
              暂无下载资源
            </Typography>
            <Typography variant="body2" color="text.secondary">
              管理员尚未添加任何下载资源，请稍后再来
            </Typography>
          </Paper>
        ) : (
          renderCategories()
        )}

        {/* 下载对话框 */}
        <Dialog open={downloadDialogOpen} onClose={() => setDownloadDialogOpen(false)} maxWidth="sm" fullWidth>
          <DialogTitle>下载文件</DialogTitle>
          <DialogContent>
            {selectedFile && (
              <Box>
                <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
                  {selectedFile.name}
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                  文件大小: {formatFileSize(selectedFile.size)}
                </Typography>
                {selectedFile.remark && (
                  <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                    备注: {selectedFile.remark}
                  </Typography>
                )}
              </Box>
            )}
            {downloading && (
              <Box sx={{ mt: 2, textAlign: 'center' }}>
                <Typography color="text.secondary">正在生成下载链接...</Typography>
              </Box>
            )}
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setDownloadDialogOpen(false)}>取消</Button>
            <Button
              variant="contained"
              onClick={executeDownload}
              disabled={!downloadUrl || downloading}
              startIcon={<DownloadIcon />}
            >
              开始下载
            </Button>
          </DialogActions>
        </Dialog>
      </Container>
    </MainLayout>
  )
}

export default DownloadCenterPage
