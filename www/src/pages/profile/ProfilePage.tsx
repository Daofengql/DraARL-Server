import { useEffect, useState, useRef } from 'react'
import {
  Box,
  Paper,
  Typography,
  Button,
  Avatar,
  Divider,
  Alert,
  Card,
  CardContent,
  Tab,
  Tabs,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Chip,
  List,
  ListItem,
  ListItemText,
  FormControlLabel,
  Switch,
  FormControl,
  RadioGroup,
  FormLabel,
  Radio,
  IconButton,
} from '@mui/material'
import Person from '@mui/icons-material/Person'
import Phone from '@mui/icons-material/Phone'
import LocationOn from '@mui/icons-material/LocationOn'
import Edit from '@mui/icons-material/Edit'
import Lock from '@mui/icons-material/Lock'
import Email from '@mui/icons-material/Email'
import Badge from '@mui/icons-material/Badge'
import Save from '@mui/icons-material/Save'
import Upload from '@mui/icons-material/Upload'
import CloudUpload from '@mui/icons-material/CloudUpload'
import Cake from '@mui/icons-material/Cake'
import Wifi from '@mui/icons-material/Wifi'
import Notifications from '@mui/icons-material/Notifications'
import AccessTime from '@mui/icons-material/AccessTime'
import CheckCircle from '@mui/icons-material/CheckCircle'
import Pending from '@mui/icons-material/Pending'
import Cancel from '@mui/icons-material/Cancel'
import CameraAlt from '@mui/icons-material/CameraAlt'
import { authService } from '../../services'
import { AvatarCropDialog } from '../../components/AvatarCropDialog'
import type { User, CertificateResponse, OperatorCertificate } from '../../types'

interface TabPanelProps {
  children?: React.ReactNode
  index: number
  value: number
}

function TabPanel({ children, value, index }: TabPanelProps) {
  return (
    <div role="tabpanel" hidden={value !== index}>
      {value === index && <Box sx={{ py: 3 }}>{children}</Box>}
    </div>
  )
}

// 审核状态组件
const ApprovalStatusChip = ({ status, reviewNote }: { status?: number; reviewNote?: string }) => {
  if (status === 1) {
    return <Chip icon={<CheckCircle />} label="已审核通过" size="small" color="success" />
  }
  if (status === 2) {
    return (
      <Box>
        <Chip icon={<Cancel />} label="审核未通过" size="small" color="error" />
        {reviewNote && <Typography variant="caption" sx={{ ml: 1, color: 'error.main' }}>{reviewNote}</Typography>}
      </Box>
    )
  }
  return <Chip icon={<Pending />} label="待审核" size="small" color="warning" />
}

export function ProfilePage() {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [tabValue, setTabValue] = useState(0)

  // 操作证相关状态
  const [certificate, setCertificate] = useState<CertificateResponse>({ active_cert: null, pending_cert: null })
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false)
  const [uploadPreview, setUploadPreview] = useState<string | null>(null)
  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [isDragging, setIsDragging] = useState(false)
  const [uploadingCert, setUploadingCert] = useState(false)
  const [certCallsign, setCertCallsign] = useState('') // 操作证上传时的呼号输入

  // 编辑资料对话框
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [editForm, setEditForm] = useState({
    nickname: '',
    callsign: '',
    phone: '',
    address: '',
    introduction: '',
    avatar: '',
    birthday: '',
    dmrid: '',
    mdcid: '',
    alarm_msg: false,
    sex: 0,
  })
  const [uploadingAvatar, setUploadingAvatar] = useState(false)

  // 头像上传
  const avatarInputRef = useRef<HTMLInputElement>(null)
  const [cropDialogOpen, setCropDialogOpen] = useState(false)
  const [selectedAvatarImage, setSelectedAvatarImage] = useState<string | null>(null)

  // 修改密码对话框
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false)
  const [passwordForm, setPasswordForm] = useState({
    old_password: '',
    new_password: '',
    confirm_password: '',
  })

  useEffect(() => {
    loadUserInfo()
    loadCertificate()
  }, [])

  const loadUserInfo = async () => {
    setLoading(true)
    try {
      const freshUser = await authService.getMe()
      setUser(freshUser)
    } catch (err) {
      console.error('Failed to load user info:', err)
      showMessage('error', '加载用户信息失败')
    } finally {
      setLoading(false)
    }
  }

  const loadCertificate = async () => {
    try {
      const cert = await authService.getOperatorCertificate()
      setCertificate(cert)
    } catch (err) {
      console.error('Failed to load certificate:', err)
    }
  }

  const showMessage = (type: 'success' | 'error', text: string) => {
    setMessage({ type, text })
    setTimeout(() => setMessage(null), 3000)
  }

  const handleOpenEditDialog = () => {
    if (user) {
      setEditForm({
        nickname: user.nickname || '',
        callsign: user.callsign || '',
        phone: user.phone || '',
        address: user.address || '',
        introduction: user.introduction || '',
        avatar: user.avatar || '',
        birthday: user.birthday || '',
        dmrid: user.dmrid?.toString() || '',
        mdcid: user.mdcid || '',
        alarm_msg: user.alarm_msg || false,
        sex: user.sex || 0,
      })
    }
    setEditDialogOpen(true)
  }

  const handleCloseEditDialog = () => {
    setEditDialogOpen(false)
  }

  // 头像上传处理 - 先打开裁切对话框
  const handleAvatarSelect = (file: File) => {
    if (!file.type.startsWith('image/')) {
      showMessage('error', '请选择图片文件')
      return
    }
    if (file.size > 10 * 1024 * 1024) {
      showMessage('error', '图片大小不能超过10MB')
      return
    }

    // 创建预览并打开裁切对话框
    const reader = new FileReader()
    reader.onloadend = () => {
      setSelectedAvatarImage(reader.result as string)
      setCropDialogOpen(true)
    }
    reader.readAsDataURL(file)
  }

  // 裁切确认后上传
  const handleCroppedAvatarUpload = async (croppedBlob: Blob) => {
    setCropDialogOpen(false)
    setUploadingAvatar(true)

    try {
      // 将 Blob 转换为 File
      const file = new File([croppedBlob], 'avatar.jpg', { type: 'image/jpeg' })

      await authService.uploadFile(file, 'avatar')
      // 后端已经更新了头像，重新获取用户信息
      const updatedUser = await authService.getMe()
      setUser(updatedUser)
      authService.saveAuth(authService.getToken()!, updatedUser)
      showMessage('success', '头像更新成功')
    } catch (err: any) {
      showMessage('error', err.response?.data?.message || '上传失败')
    } finally {
      setUploadingAvatar(false)
      setSelectedAvatarImage(null)
    }
  }

  const handleCloseCropDialog = () => {
    setCropDialogOpen(false)
    setSelectedAvatarImage(null)
  }

  const handleSaveProfile = async () => {
    try {
      const updateData: Partial<User> = {
        nickname: editForm.nickname,
        callsign: editForm.callsign,
        phone: editForm.phone,
        address: editForm.address,
        introduction: editForm.introduction,
        avatar: editForm.avatar,
        birthday: editForm.birthday,
        dmrid: editForm.dmrid ? parseInt(editForm.dmrid, 10) : undefined,
        mdcid: editForm.mdcid,
        alarm_msg: editForm.alarm_msg,
      }
      updateData.sex = editForm.sex

      const updatedUser = await authService.updateProfile(updateData)
      setUser(updatedUser)
      authService.saveAuth(authService.getToken()!, updatedUser)
      setEditDialogOpen(false)
      showMessage('success', '资料更新成功')
    } catch (err: any) {
      showMessage('error', err.response?.data?.message || '更新失败')
    }
  }

  const handleChangePassword = async () => {
    if (passwordForm.new_password !== passwordForm.confirm_password) {
      showMessage('error', '两次输入的密码不一致')
      return
    }

    if (passwordForm.new_password.length < 6) {
      showMessage('error', '密码长度至少6位')
      return
    }

    try {
      await authService.changeOwnPassword({
        old_password: passwordForm.old_password,
        new_password: passwordForm.new_password,
      })
      setPasswordDialogOpen(false)
      setPasswordForm({ old_password: '', new_password: '', confirm_password: '' })
      showMessage('success', '密码修改成功')
    } catch (err: any) {
      showMessage('error', err.response?.data?.message || '密码修改失败')
    }
  }

  // 证书相关处理函数
  const handleOpenUploadDialog = () => {
    // 优先显示待审核/被拒绝的证书，否则显示已通过的证书
    const certToShow = certificate.pending_cert || (certificate.active_cert ? certificate.active_cert : null)
    setUploadPreview(certToShow?.file_url || null)
    // 初始化呼号输入为当前用户的呼号
    setCertCallsign(user?.callsign || '')
    setUploadDialogOpen(true)
  }

  const handleCloseUploadDialog = () => {
    setUploadDialogOpen(false)
    setUploadPreview(null)
    setSelectedFile(null)
    setCertCallsign('')
  }

  const handleFileSelect = (file: File | null) => {
    if (!file) return

    // 检查文件类型
    if (!file.type.startsWith('image/') && file.type !== 'application/pdf') {
      showMessage('error', '请选择图片或PDF文件')
      return
    }

    // 检查文件大小 (10MB)
    if (file.size > 10 * 1024 * 1024) {
      showMessage('error', '文件大小不能超过10MB')
      return
    }

    setSelectedFile(file)

    // 如果是图片，创建预览
    if (file.type.startsWith('image/')) {
      const reader = new FileReader()
      reader.onloadend = () => {
        setUploadPreview(reader.result as string)
      }
      reader.readAsDataURL(file)
    } else {
      setUploadPreview(null)
    }
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
    const file = e.dataTransfer.files[0]
    handleFileSelect(file)
  }

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }

  const handleDragLeave = () => {
    setIsDragging(false)
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0] || null
    handleFileSelect(file)
  }

  const handleUploadCertificate = async () => {
    // 判断呼号是否发生实质性修改
    const isCallsignChanged = certCallsign && certCallsign !== user?.callsign

    // 如果没选新文件，且呼号也没变，则拦截
    if (!selectedFile && !isCallsignChanged) {
      showMessage('error', '请选择新文件或修改呼号')
      return
    }

    setUploadingCert(true)
    try {
      // 传递 selectedFile (可能为 undefined) 和 certCallsign 给 service
      await authService.uploadOperatorCertificate(selectedFile || undefined, certCallsign)

      // 上传成功后重新加载证书和用户信息
      await Promise.all([loadCertificate(), loadUserInfo()])
      setUploadDialogOpen(false)
      setUploadPreview(null)
      setSelectedFile(null)
      // 清空输入框，防止下次打开残留
      setCertCallsign('')
      showMessage('success', '提交成功，请等待管理员审核')
    } catch (err: any) {
      showMessage('error', err.response?.data?.message || '上传失败')
    } finally {
      setUploadingCert(false)
    }
  }

  const displayName = user?.nickname || user?.username || ''

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '50vh' }}>
        <Typography>加载中...</Typography>
      </Box>
    )
  }

  return (
    <Box>
      <Typography variant="h4" sx={{ mb: 3, fontWeight: 600 }}>
        个人中心
      </Typography>

      {message && (
        <Alert
          severity={message.type}
          sx={{ mb: 2 }}
          onClose={() => setMessage(null)}
        >
          {message.text}
        </Alert>
      )}

      <Paper>
        <Tabs
          value={tabValue}
          onChange={(_, newValue) => setTabValue(newValue)}
          sx={{ borderBottom: 1, borderColor: 'divider', px: 2 }}
        >
          <Tab label="基本信息" />
          <Tab label="账号安全" />
        </Tabs>

        {/* 基本信息标签页 */}
        <TabPanel value={tabValue} index={0}>
          <Box sx={{ px: 2 }}>
            {/* 头部卡片 */}
            <Card sx={{ mb: 3 }}>
              <CardContent>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 3, flexWrap: 'wrap' }}>
                  <Box sx={{ position: 'relative' }}>
                    <Avatar
                      src={user?.avatar_thumb || user?.avatar}
                      alt={displayName}
                      sx={{ width: 100, height: 100, bgcolor: 'primary.main', fontSize: '2.5rem' }}
                    >
                      {displayName.charAt(0).toUpperCase()}
                    </Avatar>
                    <input
                      ref={avatarInputRef}
                      type="file"
                      accept="image/*"
                      style={{ display: 'none' }}
                      onChange={(e) => {
                        const file = e.target.files?.[0]
                        if (file) handleAvatarSelect(file)
                      }}
                    />
                    <IconButton
                      sx={{
                        position: 'absolute',
                        bottom: -4,
                        right: -4,
                        bgcolor: 'background.paper',
                        border: '1px solid',
                        borderColor: 'divider',
                      }}
                      size="small"
                      onClick={() => avatarInputRef.current?.click()}
                      disabled={uploadingAvatar}
                    >
                      <CameraAlt fontSize="small" />
                    </IconButton>
                  </Box>
                  <Box sx={{ flex: 1, minWidth: 200 }}>
                    <Typography variant="h5" fontWeight={600}>
                      {displayName}
                    </Typography>
                    <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                      @{user?.username}
                    </Typography>
                    <Box sx={{ display: 'flex', gap: 1, mt: 1, flexWrap: 'wrap' }}>
                      <Chip
                        label={user?.role === 'admin' ? '管理员' : '普通用户'}
                        size="small"
                        color={user?.role === 'admin' ? 'secondary' : 'default'}
                      />
                      <Chip
                        label={user?.status === 1 ? '正常' : '已禁用'}
                        size="small"
                        color={user?.status === 1 ? 'success' : 'error'}
                      />
                      <ApprovalStatusChip status={user?.approval_status} reviewNote={user?.review_note} />
                    </Box>
                  </Box>
                  <Button
                    variant="outlined"
                    startIcon={<Edit />}
                    onClick={handleOpenEditDialog}
                  >
                    编辑资料
                  </Button>
                </Box>
              </CardContent>
            </Card>

            {/* 信息列表和证书卡片 */}
            <Box
              sx={{
                display: 'grid',
                gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' },
                gap: 3,
              }}
            >
              {/* 左侧：信息列表 */}
              <Card>
                <CardContent>
                  <Typography variant="h6" gutterBottom>
                    个人信息
                  </Typography>
                  <Divider sx={{ mb: 2 }} />
                  <List disablePadding>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        <Person fontSize="small" sx={{ mr: 1 }} />
                        用户名
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.username || '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        <Badge fontSize="small" sx={{ mr: 1 }} />
                        昵称
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.nickname || '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        <Email fontSize="small" sx={{ mr: 1 }} />
                        呼号
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.callsign || '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        <Wifi fontSize="small" sx={{ mr: 1 }} />
                        DMR ID
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.dmrid || '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        MDC ID
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.mdcid || '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        <Phone fontSize="small" sx={{ mr: 1 }} />
                        手机号码
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.phone || '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        <LocationOn fontSize="small" sx={{ mr: 1 }} />
                        地址
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.address || '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        <Cake fontSize="small" sx={{ mr: 1 }} />
                        生日
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.birthday || '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        性别
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.sex === 1 ? '男' : user?.sex === 2 ? '女' : '-'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        <Notifications fontSize="small" sx={{ mr: 1 }} />
                        消息提醒
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.alarm_msg ? '已开启' : '已关闭'}</ListItemText>
                    </ListItem>
                    <ListItem divider>
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        个人简介
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>{user?.introduction || '-'}</ListItemText>
                    </ListItem>
                    <ListItem>
                      <Box sx={{ minWidth: 120, color: 'text.secondary' }}>注册时间</Box>
                      <ListItemText>
                        {user?.created_at ? new Date(user.created_at).toLocaleString('zh-CN') : '-'}
                      </ListItemText>
                    </ListItem>
                  </List>
                </CardContent>
              </Card>

              {/* 右侧：操作证卡片 */}
              <Card>
                <CardContent>
                  <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
                    <Typography variant="h6">
                      操作证书
                    </Typography>
                    {/* 操作证独立状态显示 */}
                    {/* 如果有 pending_cert，显示其状态；否则显示 active_cert 的状态 */}
                    {(certificate.pending_cert || certificate.active_cert) ? (
                      (() => {
                        const certToShow = certificate.pending_cert || certificate.active_cert!
                        if (certToShow!.status === 1) {
                          return <Chip icon={<CheckCircle />} label="已审核通过" size="small" color="success" />
                        }
                        if (certToShow!.status === 2) {
                          return <Chip icon={<Cancel />} label="审核未通过" size="small" color="error" />
                        }
                        return <Chip icon={<Pending />} label="待审核" size="small" color="warning" />
                      })()
                    ) : (
                      <Chip label="未上传" size="small" />
                    )}
                  </Box>
                  <Divider sx={{ mb: 2 }} />
                  {/* 显示拒绝原因（只有 pending_cert 被拒绝时才显示） */}
                  {certificate.pending_cert?.status === 2 && certificate.pending_cert?.review_note && (
                    <Alert severity="error" sx={{ mb: 2 }}>
                      <Typography variant="body2">拒绝原因: {certificate.pending_cert.review_note}</Typography>
                    </Alert>
                  )}
                  {/* 显示待审核提示 */}
                  {certificate.pending_cert?.status === 0 && (
                    <Alert severity="info" sx={{ mb: 2 }}>
                      <Typography variant="body2">操作证待审核，请耐心等待管理员审核</Typography>
                    </Alert>
                  )}
                  {/* 被拒绝时提示可以查看已通过的旧版本 */}
                  {certificate.pending_cert?.status === 2 && certificate.active_cert && (
                    <Alert severity="success" sx={{ mb: 2 }}>
                      <Typography variant="body2">下方显示的是之前已通过的操作证</Typography>
                    </Alert>
                  )}
                  <Box sx={{ display: 'flex', justifyContent: 'center' }}>
                    <Box
                      sx={{
                        width: 400,
                        height: 400,
                        bgcolor: 'grey.100',
                        borderRadius: 1,
                        overflow: 'hidden',
                        position: 'relative',
                      }}
                    >
                      {uploadPreview ? (
                        <Box
                          component="img"
                          src={uploadPreview}
                          alt="预览"
                          sx={{
                            width: '100%',
                            height: '100%',
                            objectFit: 'contain',
                          }}
                        />
                      ) : (() => {
                        // 决定显示哪个操作证：
                        // - 如果有 active_cert（已通过），优先显示
                        // - 如果有 pending_cert 且状态为待审核，显示 pending_cert
                        // - 如果 pending_cert 被拒绝且有 active_cert，显示 active_cert（旧版本）
                        // - 否则显示 pending_cert
                        let certToShow: OperatorCertificate | null = null
                        if (certificate.active_cert) {
                          certToShow = certificate.active_cert
                        } else if (certificate.pending_cert) {
                          certToShow = certificate.pending_cert
                        }

                        return certToShow?.file_url ? (
                          <Box
                            component="img"
                            src={certToShow.file_url}
                            alt="操作证"
                            sx={{
                              width: '100%',
                              height: '100%',
                              objectFit: 'contain',
                            }}
                          />
                        ) : (
                          <Box
                            sx={{
                              width: '100%',
                              height: '100%',
                              display: 'flex',
                              flexDirection: 'column',
                              alignItems: 'center',
                              justifyContent: 'center',
                              color: 'text.secondary',
                            }}
                          >
                            <CloudUpload sx={{ fontSize: 48, mb: 1, opacity: 0.5 }} />
                            <Typography variant="body2">
                              暂无操作证
                            </Typography>
                          </Box>
                        )
                      })()}
                    </Box>
                  </Box>
                  <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 2, mb: 1 }}>
                    请上传您的业余电台操作证，用于身份验证
                  </Typography>
                  <Alert severity="info" sx={{ mb: 2 }}>
                    <Typography variant="caption">
                      上传操作证时可设置呼号，设置后会重置审核状态需要等待管理员重新审核
                    </Typography>
                  </Alert>
                  <Button
                    variant={(certificate.pending_cert || certificate.active_cert) ? 'outlined' : 'contained'}
                    fullWidth
                    startIcon={<Upload />}
                    onClick={handleOpenUploadDialog}
                  >
                    {certificate.pending_cert?.status === 0
                      ? '审核中...（可重新上传）'
                      : certificate.pending_cert?.status === 2
                        ? '重新上传'
                        : certificate.active_cert
                          ? '更新操作证'
                          : '上传操作证'}
                  </Button>
                </CardContent>
              </Card>
            </Box>
          </Box>
        </TabPanel>

        {/* 账号安全标签页 */}
        <TabPanel value={tabValue} index={1}>
          <Box sx={{ px: 2, maxWidth: 600 }}>
            <Card>
              <CardContent>
                <Typography variant="h6" gutterBottom>
                  修改密码
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                  定期修改密码可以保护您的账号安全
                </Typography>
                <Button
                  variant="contained"
                  startIcon={<Lock />}
                  onClick={() => setPasswordDialogOpen(true)}
                >
                  修改密码
                </Button>

                <Divider sx={{ my: 4 }} />

                <Typography variant="h6" gutterBottom>
                  账号信息
                </Typography>
                <List disablePadding>
                  <ListItem divider>
                    <Box sx={{ minWidth: 120, color: 'text.secondary' }}>用户ID</Box>
                    <ListItemText>{user?.id}</ListItemText>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 120, color: 'text.secondary' }}>用户名</Box>
                    <ListItemText>{user?.username}</ListItemText>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 120, color: 'text.secondary' }}>角色</Box>
                    <ListItemText>{user?.role === 'admin' ? '管理员' : '普通用户'}</ListItemText>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 120, color: 'text.secondary' }}>状态</Box>
                    <ListItemText>
                      <Chip
                        label={user?.status === 1 ? '正常' : '已禁用'}
                        size="small"
                        color={user?.status === 1 ? 'success' : 'error'}
                      />
                    </ListItemText>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 120, color: 'text.secondary' }}>审核状态</Box>
                    <ListItemText>
                      <ApprovalStatusChip status={user?.approval_status} reviewNote={user?.review_note} />
                    </ListItemText>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 120, color: 'text.secondary', display: 'flex', alignItems: 'center' }}>
                      <AccessTime fontSize="small" sx={{ mr: 1 }} />
                      最后登录
                    </Box>
                    <ListItemText>
                      {user?.last_login_time
                        ? new Date(user.last_login_time).toLocaleString('zh-CN')
                        : '-'}
                    </ListItemText>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 120, color: 'text.secondary' }}>登录IP</Box>
                    <ListItemText>{user?.last_login_ip || '-'}</ListItemText>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 120, color: 'text.secondary' }}>登录错误次数</Box>
                    <ListItemText>
                      <Chip
                        label={user?.login_err_times || 0}
                        size="small"
                        color={user?.login_err_times && user.login_err_times > 0 ? 'warning' : 'default'}
                      />
                    </ListItemText>
                  </ListItem>
                  <ListItem>
                    <Box sx={{ minWidth: 120, color: 'text.secondary' }}>注册时间</Box>
                    <ListItemText>
                      {user?.created_at
                        ? new Date(user.created_at).toLocaleString('zh-CN')
                        : '-'}
                    </ListItemText>
                  </ListItem>
                </List>
              </CardContent>
            </Card>
          </Box>
        </TabPanel>
      </Paper>

      {/* 编辑资料对话框 */}
      <Dialog open={editDialogOpen} onClose={handleCloseEditDialog} maxWidth="sm" fullWidth>
        <DialogTitle>编辑个人资料</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label="昵称"
              fullWidth
              value={editForm.nickname}
              onChange={(e) => setEditForm({ ...editForm, nickname: e.target.value })}
            />
            <TextField
              label="手机号码"
              fullWidth
              value={editForm.phone}
              onChange={(e) => setEditForm({ ...editForm, phone: e.target.value })}
            />
            <TextField
              label="地址"
              fullWidth
              value={editForm.address}
              onChange={(e) => setEditForm({ ...editForm, address: e.target.value })}
            />
            <TextField
              label="生日"
              fullWidth
              type="date"
              InputLabelProps={{ shrink: true }}
              value={editForm.birthday}
              onChange={(e) => setEditForm({ ...editForm, birthday: e.target.value })}
            />
            <FormControl component="fieldset">
              <FormLabel component="legend">性别</FormLabel>
              <RadioGroup
                row
                value={editForm.sex === 1 ? 'male' : editForm.sex === 2 ? 'female' : 'secret'}
                onChange={(e) => {
                  switch (e.target.value) {
                    case 'male':
                      setEditForm({ ...editForm, sex: 1 })
                      break
                    case 'female':
                      setEditForm({ ...editForm, sex: 2 })
                      break
                    case 'secret':
                    default:
                      setEditForm({ ...editForm, sex: 0 })
                  }
                }}
              >
                <FormControlLabel value="male" control={<Radio />} label="男" />
                <FormControlLabel value="female" control={<Radio />} label="女" />
                <FormControlLabel value="secret" control={<Radio />} label="保密" />
              </RadioGroup>
            </FormControl>
            <TextField
              label="DMR ID"
              fullWidth
              type="number"
              value={editForm.dmrid}
              onChange={(e) => setEditForm({ ...editForm, dmrid: e.target.value })}
            />
            <TextField
              label="MDC ID"
              fullWidth
              value={editForm.mdcid}
              onChange={(e) => setEditForm({ ...editForm, mdcid: e.target.value })}
            />
            <FormControlLabel
              control={
                <Switch
                  checked={editForm.alarm_msg}
                  onChange={(e) => setEditForm({ ...editForm, alarm_msg: e.target.checked })}
                />
              }
              label="开启消息提醒"
            />
            <TextField
              label="个人简介"
              fullWidth
              multiline
              rows={3}
              value={editForm.introduction}
              onChange={(e) => setEditForm({ ...editForm, introduction: e.target.value })}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseEditDialog}>取消</Button>
          <Button onClick={handleSaveProfile} variant="contained" startIcon={<Save />}>
            保存
          </Button>
        </DialogActions>
      </Dialog>

      {/* 修改密码对话框 */}
      <Dialog open={passwordDialogOpen} onClose={() => setPasswordDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>修改密码</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label="当前密码"
              type="password"
              fullWidth
              value={passwordForm.old_password}
              onChange={(e) => setPasswordForm({ ...passwordForm, old_password: e.target.value })}
            />
            <TextField
              label="新密码"
              type="password"
              fullWidth
              value={passwordForm.new_password}
              onChange={(e) => setPasswordForm({ ...passwordForm, new_password: e.target.value })}
              helperText="密码长度至少6位"
            />
            <TextField
              label="确认新密码"
              type="password"
              fullWidth
              value={passwordForm.confirm_password}
              onChange={(e) => setPasswordForm({ ...passwordForm, confirm_password: e.target.value })}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPasswordDialogOpen(false)}>取消</Button>
          <Button onClick={handleChangePassword} variant="contained" startIcon={<Save />}>
            确认修改
          </Button>
        </DialogActions>
      </Dialog>

      {/* 上传操作证对话框 */}
      <Dialog open={uploadDialogOpen} onClose={handleCloseUploadDialog} maxWidth="sm" fullWidth>
        <DialogTitle>上传操作证</DialogTitle>
        <DialogContent>
          <Box sx={{ mt: 1 }}>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              上传操作证时可以设置您的呼号。设置呼号后会重置审核状态，需要等待管理员重新审核。
            </Typography>
            {/* 呼号输入 */}
            <TextField
              label="呼号"
              fullWidth
              value={certCallsign}
              onChange={(e) => setCertCallsign(e.target.value.toUpperCase())}
              placeholder="例如: BG0ABC"
              sx={{ mb: 2 }}
              helperText="可选，留空则不修改呼号"
            />
            <Box
              onDrop={handleDrop}
              onDragOver={handleDragOver}
              onDragLeave={handleDragLeave}
              sx={{
                border: '2px dashed',
                borderColor: isDragging ? 'primary.main' : 'grey.300',
                borderRadius: 2,
                p: 3,
                textAlign: 'center',
                cursor: 'pointer',
                bgcolor: isDragging ? 'action.hover' : 'background.paper',
                transition: 'all 0.2s',
              }}
              onClick={() => document.getElementById('certificate-input')?.click()}
            >
              {uploadPreview ? (
                <Box
                  component="img"
                  src={uploadPreview}
                  alt="预览"
                  sx={{
                    maxWidth: '100%',
                    maxHeight: 300,
                    objectFit: 'contain',
                  }}
                />
              ) : selectedFile?.type === 'application/pdf' ? (
                <Box>
                  <CloudUpload sx={{ fontSize: 48, color: 'text.secondary', mb: 1 }} />
                  <Typography variant="body1" gutterBottom>
                    {selectedFile.name}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    PDF文件已选择
                  </Typography>
                </Box>
              ) : (
                <Box>
                  <CloudUpload sx={{ fontSize: 48, color: 'text.secondary', mb: 1 }} />
                  <Typography variant="body1" gutterBottom>
                    拖拽文件到此处或点击上传
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    支持 JPG、PNG、PDF 格式，最大 10MB
                  </Typography>
                </Box>
              )}
              <input
                id="certificate-input"
                type="file"
                accept="image/*,.pdf"
                onChange={handleInputChange}
                style={{ display: 'none' }}
              />
            </Box>
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseUploadDialog}>取消</Button>
          <Button
            onClick={handleUploadCertificate}
            variant="contained"
            startIcon={<Upload />}
            disabled={(!selectedFile && !(certCallsign && certCallsign !== user?.callsign)) || uploadingCert}
          >
            {uploadingCert ? '提交中...' : '提交'}
          </Button>
        </DialogActions>
      </Dialog>

      {/* 头像裁切对话框 */}
      <AvatarCropDialog
        open={cropDialogOpen}
        imageSrc={selectedAvatarImage || ''}
        onClose={handleCloseCropDialog}
        onConfirm={handleCroppedAvatarUpload}
      />
    </Box>
  )
}
