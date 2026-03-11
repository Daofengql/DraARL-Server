import { useEffect, useState } from 'react'
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
} from '@mui/material'
import {
  Person,
  Phone,
  LocationOn,
  Edit,
  Lock,
  Email,
  Badge,
  Save,
  Upload,
  CloudUpload,
  Cake,
  Wifi,
  Notifications,
  AccessTime,
} from '@mui/icons-material'
import { authService } from '../../services'
import type { User } from '../../types'

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

export function ProfilePage() {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [tabValue, setTabValue] = useState(0)

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

  // 修改密码对话框
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false)
  const [passwordForm, setPasswordForm] = useState({
    old_password: '',
    new_password: '',
    confirm_password: '',
  })

  // 证书相关状态
  const [certificateImage, setCertificateImage] = useState<string | null>(null)
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false)
  const [uploadPreview, setUploadPreview] = useState<string | null>(null)
  const [isDragging, setIsDragging] = useState(false)

  useEffect(() => {
    loadUserInfo()
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

  const handleSaveProfile = async () => {
    try {
      // 转换 dmrid 为数字
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
      // 性别字段处理
      // - 0 = 保密
      // - 1 = 男
      // - 2 = 女
      updateData.sex = editForm.sex

      const updatedUser = await authService.updateProfile(updateData)
      setUser(updatedUser)
      // 更新本地存储
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
    setUploadPreview(certificateImage)
    setUploadDialogOpen(true)
  }

  const handleCloseUploadDialog = () => {
    setUploadDialogOpen(false)
    setUploadPreview(null)
  }

  const handleFileSelect = (file: File | null) => {
    if (!file) return

    // 检查文件类型
    if (!file.type.startsWith('image/')) {
      showMessage('error', '请选择图片文件')
      return
    }

    // 检查文件大小 (5MB)
    if (file.size > 5 * 1024 * 1024) {
      showMessage('error', '图片大小不能超过5MB')
      return
    }

    // 创建预览
    const reader = new FileReader()
    reader.onloadend = () => {
      setUploadPreview(reader.result as string)
    }
    reader.readAsDataURL(file)
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

  const handleUploadCertificate = () => {
    // TODO: 调用上传接口
    setCertificateImage(uploadPreview)
    setUploadDialogOpen(false)
    showMessage('success', '证书上传成功')
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
                  <Avatar
                    src={user?.avatar}
                    alt={displayName}
                    sx={{ width: 100, height: 100, bgcolor: 'primary.main', fontSize: '2.5rem' }}
                  >
                    {displayName.charAt(0).toUpperCase()}
                  </Avatar>
                  <Box sx={{ flex: 1, minWidth: 200 }}>
                    <Typography variant="h5" fontWeight={600}>
                      {displayName}
                    </Typography>
                    <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                      @{user?.username}
                    </Typography>
                    <Box sx={{ display: 'flex', gap: 1, mt: 1 }}>
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
                      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 100, color: 'text.secondary' }}>
                        注册时间
                      </Box>
                      <ListItemText sx={{ ml: 2 }}>
                        {user?.created_at ? new Date(user.created_at).toLocaleString('zh-CN') : '-'}
                      </ListItemText>
                    </ListItem>
                  </List>
                </CardContent>
              </Card>

              {/* 右侧：证书卡片 */}
              <Card>
                <CardContent>
                  <Typography variant="h6" gutterBottom>
                    操作证书
                  </Typography>
                  <Divider sx={{ mb: 2 }} />
                  <Box
                    sx={{
                      width: '100%',
                      height: 200,
                      bgcolor: 'grey.100',
                      borderRadius: 1,
                      overflow: 'hidden',
                      position: 'relative',
                    }}
                  >
                    {certificateImage ? (
                      <Box
                        component="img"
                        src={certificateImage}
                        alt="证书"
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
                          暂无证书
                        </Typography>
                      </Box>
                    )}
                  </Box>
                  <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 2, mb: 2 }}>
                    证书用于设备认证和通信加密
                  </Typography>
                  <Button
                    variant={certificateImage ? 'outlined' : 'contained'}
                    fullWidth
                    startIcon={<Upload />}
                    onClick={handleOpenUploadDialog}
                  >
                    {certificateImage ? '更新证书' : '上传证书'}
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
              label="呼号"
              fullWidth
              value={editForm.callsign}
              onChange={(e) => setEditForm({ ...editForm, callsign: e.target.value })}
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
              label="头像URL"
              fullWidth
              value={editForm.avatar}
              onChange={(e) => setEditForm({ ...editForm, avatar: e.target.value })}
              placeholder="https://"
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

      {/* 上传证书对话框 */}
      <Dialog open={uploadDialogOpen} onClose={handleCloseUploadDialog} maxWidth="sm" fullWidth>
        <DialogTitle>上传证书</DialogTitle>
        <DialogContent>
          <Box sx={{ mt: 1 }}>
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
              ) : (
                <Box>
                  <CloudUpload sx={{ fontSize: 48, color: 'text.secondary', mb: 1 }} />
                  <Typography variant="body1" gutterBottom>
                    拖拽图片到此处或点击上传
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    支持 JPG、PNG 格式，最大 5MB
                  </Typography>
                </Box>
              )}
              <input
                id="certificate-input"
                type="file"
                accept="image/*"
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
            disabled={!uploadPreview}
          >
            上传
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
