import { useEffect, useState } from 'react'
import {
  Box,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TablePagination,
  Button,
  Typography,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Card,
  CardContent,
  Chip,
  Alert,
  TextField,
  List,
  ListItem,
  Divider,
  Tab,
  Tabs,
} from '@mui/material'
import CheckCircle from '@mui/icons-material/CheckCircle'
import Cancel from '@mui/icons-material/Cancel'
import Visibility from '@mui/icons-material/Visibility'
import Refresh from '@mui/icons-material/Refresh'
import Description from '@mui/icons-material/Description'
import Person from '@mui/icons-material/Person'
import { approvalService } from '../../services'
import type { PendingApproval } from '../../types'
import { TabPanel } from '../../components/common/TabPanel'
import { ImagePreviewDialog } from '../../components/common/ImagePreviewDialog'
import { PageHeader } from '../../components/common/PageHeader'

// 审核状态组件
const ApprovalStatusBadge = ({ status }: { status: number }) => {
  switch (status) {
    case 0:
      return <Chip label="待审核" size="small" color="warning" />
    case 1:
      return <Chip label="已通过" size="small" color="success" />
    case 2:
      return <Chip label="已拒绝" size="small" color="error" />
    default:
      return <Chip label="未知" size="small" />
  }
}

export function ApprovalsPage() {
  const [tabValue, setTabValue] = useState(0)
  const [pendingUsers, setPendingUsers] = useState<PendingApproval[]>([])
  const [approvedUsers, setApprovedUsers] = useState<PendingApproval[]>([])
  const [rejectedUsers, setRejectedUsers] = useState<PendingApproval[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  // 分页状态
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)

  // 审批对话框
  const [approveDialogOpen, setApproveDialogOpen] = useState(false)
  const [selectedUser, setSelectedUser] = useState<PendingApproval | null>(null)
  const [note, setNote] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // 详情对话框
  const [detailDialogOpen, setDetailDialogOpen] = useState(false)
  // 图片预览对话框
  const [imagePreviewOpen, setImagePreviewOpen] = useState(false)
  const [previewImageUrl, setPreviewImageUrl] = useState<string | null>(null)

  useEffect(() => {
    // 初始化时一次性加载所有状态的数据
    loadAllTabData()
  }, [page, rowsPerPage])

  // 当切换标签时，重新加载对应标签的数据（使用当前页码）
  useEffect(() => {
    loadTabData(tabValue)
  }, [tabValue])

  const loadAllTabData = async () => {
    setLoading(true)
    setError('')
    try {
      // 并行加载所有三个状态的数据
      const [pendingData, approvedData, rejectedData] = await Promise.all([
        approvalService.getPendingApprovals(page + 1, rowsPerPage, 0),
        approvalService.getPendingApprovals(page + 1, rowsPerPage, 1),
        approvalService.getPendingApprovals(page + 1, rowsPerPage, 2),
      ])
      setPendingUsers(pendingData.items || [])
      setApprovedUsers(approvedData.items || [])
      setRejectedUsers(rejectedData.items || [])
    } catch (err: any) {
      console.error('Failed to load approvals:', err)
      setError(err.response?.data?.message || '加载数据失败')
    } finally {
      setLoading(false)
    }
  }

  const showMessage = (type: 'success' | 'error', text: string) => {
    setMessage({ type, text })
    setTimeout(() => setMessage(null), 3000)
  }

  const loadTabData = async (status: number) => {
    setLoading(true)
    setError('')
    try {
      const data = await approvalService.getPendingApprovals(page + 1, rowsPerPage, status)

      if (status === 0) {
        setPendingUsers(data.items || [])
      } else if (status === 1) {
        setApprovedUsers(data.items || [])
      } else {
        setRejectedUsers(data.items || [])
      }
    } catch (err: any) {
      console.error('Failed to load approvals:', err)
      setError(err.response?.data?.message || '加载数据失败')
    } finally {
      setLoading(false)
    }
  }

  const loadPendingApprovals = async () => {
    await loadTabData(tabValue)
  }

  const handleOpenApproveDialog = (user: PendingApproval) => {
    setSelectedUser(user)
    setNote(user.review_note || '')
    setApproveDialogOpen(true)
  }

  const handleCloseApproveDialog = () => {
    setApproveDialogOpen(false)
    setSelectedUser(null)
    setNote('')
  }

  const handleApprove = async () => {
    if (!selectedUser) return

    setSubmitting(true)
    try {
      // 如果用户账户已审核通过，但有待审核的操作证，只审批操作证
      if (selectedUser.approval_status === 1 && selectedUser.cert && selectedUser.cert.status === 0) {
        await approvalService.approveCertificate(selectedUser.cert.id, { status: 1, note })
        showMessage('success', '操作证已通过审核')
      } else {
        // 否则审批用户账户
        await approvalService.approveUser(selectedUser.id, { status: 1, note })
        showMessage('success', '已通过审核')
      }
      handleCloseApproveDialog()
      loadPendingApprovals()
    } catch (err: any) {
      showMessage('error', err.response?.data?.message || '操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleReject = async () => {
    if (!selectedUser) return

    if (!note.trim()) {
      showMessage('error', '请填写拒绝原因')
      return
    }

    setSubmitting(true)
    try {
      // 如果用户账户已审核通过，但有待审核的操作证，只审批操作证
      if (selectedUser.approval_status === 1 && selectedUser.cert && selectedUser.cert.status === 0) {
        await approvalService.approveCertificate(selectedUser.cert.id, { status: 2, note })
        showMessage('success', '操作证已拒绝')
      } else {
        // 否则审批用户账户
        await approvalService.approveUser(selectedUser.id, { status: 2, note })
        showMessage('success', '已拒绝用户')
      }
      handleCloseApproveDialog()
      loadPendingApprovals()
    } catch (err: any) {
      showMessage('error', err.response?.data?.message || '操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleOpenDetail = (user: PendingApproval) => {
    setSelectedUser(user)
    setDetailDialogOpen(true)
  }

  const handleCloseDetail = () => {
    setDetailDialogOpen(false)
    setSelectedUser(null)
  }

  const currentTabUsers = tabValue === 0 ? pendingUsers : tabValue === 1 ? approvedUsers : rejectedUsers

  return (
    <Box>
      <PageHeader
        title="用户审批"
        actions={
          <Button
            variant="outlined"
            startIcon={<Refresh />}
            onClick={loadPendingApprovals}
            disabled={loading}
          >
            刷新
          </Button>
        }
      />

      {message && (
        <Alert
          severity={message.type}
          sx={{ mb: 2 }}
          onClose={() => setMessage(null)}
        >
          {message.text}
        </Alert>
      )}

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>
          {error}
        </Alert>
      )}

      <Paper>
        <Tabs
          value={tabValue}
          onChange={(_, newValue) => {
            setTabValue(newValue)
            setPage(0)
          }}
          sx={{ borderBottom: 1, borderColor: 'divider', px: 2 }}
        >
          <Tab label={`待审核 (${pendingUsers.length})`} />
          <Tab label={`已通过 (${approvedUsers.length})`} />
          <Tab label={`已拒绝 (${rejectedUsers.length})`} />
        </Tabs>

        <TabPanel value={tabValue} index={0}>
          <TableContainer sx={{ overflow: 'auto' }}>
            <Table sx={{ minWidth: 700 }}>
              <TableHead>
                <TableRow>
                  <TableCell>ID</TableCell>
                  <TableCell>用户名</TableCell>
                  <TableCell>呼号</TableCell>
                  <TableCell>手机号</TableCell>
                  <TableCell>操作证</TableCell>
                  <TableCell>注册时间</TableCell>
                  <TableCell>操作</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center">
                      加载中...
                    </TableCell>
                  </TableRow>
                ) : currentTabUsers.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center">
                      暂无数据
                    </TableCell>
                  </TableRow>
                ) : (
                  currentTabUsers.map((user) => (
                    <TableRow key={user.id} hover>
                      <TableCell>{user.id}</TableCell>
                      <TableCell>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                          <Person color="primary" fontSize="small" />
                          {user.username}
                        </Box>
                      </TableCell>
                      <TableCell>{user.callsign || '-'}</TableCell>
                      <TableCell>{user.phone || '-'}</TableCell>
                      <TableCell>
                        {user.has_cert ? (
                          <Chip
                            icon={<Description />}
                            label="已上传"
                            size="small"
                            color="success"
                            variant="outlined"
                          />
                        ) : (
                          <Chip label="未上传" size="small" variant="outlined" />
                        )}
                      </TableCell>
                      <TableCell>
                        {user.created_at
                          ? new Date(user.created_at).toLocaleString('zh-CN')
                          : '-'}
                      </TableCell>
                      <TableCell>
                        <Box sx={{ display: 'flex', gap: 0.5 }}>
                          <Button
                            size="small"
                            variant="outlined"
                            color="info"
                            startIcon={<Visibility />}
                            onClick={() => handleOpenDetail(user)}
                          >
                            查看
                          </Button>
                          {tabValue === 0 && (
                            <>
                              <Button
                                size="small"
                                variant="outlined"
                                color="error"
                                startIcon={<Cancel />}
                                onClick={() => handleOpenApproveDialog(user)}
                              >
                                拒绝
                              </Button>
                              <Button
                                size="small"
                                variant="contained"
                                color="success"
                                startIcon={<CheckCircle />}
                                onClick={() => handleOpenApproveDialog(user)}
                              >
                                通过
                              </Button>
                            </>
                          )}
                        </Box>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableContainer>
          <TablePagination
            component="div"
            count={-1} // 未知总数
            page={page}
            onPageChange={(_, newPage) => setPage(newPage)}
            rowsPerPage={rowsPerPage}
            onRowsPerPageChange={(e) => {
              setRowsPerPage(parseInt(e.target.value, 10))
              setPage(0)
            }}
            labelRowsPerPage="每页行数"
            labelDisplayedRows={() => `第 ${page + 1} 页`}
          />
        </TabPanel>

        <TabPanel value={tabValue} index={1}>
          <TableContainer sx={{ overflow: 'auto' }}>
            <Table sx={{ minWidth: 600 }}>
              <TableHead>
                <TableRow>
                  <TableCell>ID</TableCell>
                  <TableCell>用户名</TableCell>
                  <TableCell>呼号</TableCell>
                  <TableCell>手机号</TableCell>
                  <TableCell>审核时间</TableCell>
                  <TableCell>操作</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={6} align="center">
                      加载中...
                    </TableCell>
                  </TableRow>
                ) : currentTabUsers.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} align="center">
                      暂无数据
                    </TableCell>
                  </TableRow>
                ) : (
                  currentTabUsers.map((user) => (
                    <TableRow key={user.id} hover>
                      <TableCell>{user.id}</TableCell>
                      <TableCell>{user.username}</TableCell>
                      <TableCell>{user.callsign || '-'}</TableCell>
                      <TableCell>{user.phone || '-'}</TableCell>
                      <TableCell>
                        {user.review_time
                          ? new Date(user.review_time).toLocaleString('zh-CN')
                          : '-'}
                      </TableCell>
                      <TableCell>
                        <Button
                          size="small"
                          variant="outlined"
                          color="info"
                          startIcon={<Visibility />}
                          onClick={() => handleOpenDetail(user)}
                        >
                          查看
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableContainer>
          <TablePagination
            component="div"
            count={-1}
            page={page}
            onPageChange={(_, newPage) => setPage(newPage)}
            rowsPerPage={rowsPerPage}
            onRowsPerPageChange={(e) => {
              setRowsPerPage(parseInt(e.target.value, 10))
              setPage(0)
            }}
            labelRowsPerPage="每页行数"
            labelDisplayedRows={() => `第 ${page + 1} 页`}
          />
        </TabPanel>

        <TabPanel value={tabValue} index={2}>
          <TableContainer sx={{ overflow: 'auto' }}>
            <Table sx={{ minWidth: 600 }}>
              <TableHead>
                <TableRow>
                  <TableCell>ID</TableCell>
                  <TableCell>用户名</TableCell>
                  <TableCell>呼号</TableCell>
                  <TableCell>拒绝原因</TableCell>
                  <TableCell>审核时间</TableCell>
                  <TableCell>操作</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={6} align="center">
                      加载中...
                    </TableCell>
                  </TableRow>
                ) : currentTabUsers.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} align="center">
                      暂无数据
                    </TableCell>
                  </TableRow>
                ) : (
                  currentTabUsers.map((user) => (
                    <TableRow key={user.id} hover>
                      <TableCell>{user.id}</TableCell>
                      <TableCell>{user.username}</TableCell>
                      <TableCell>{user.callsign || '-'}</TableCell>
                      <TableCell>
                        <Typography variant="body2" color="error">
                          {user.cert?.review_note || user.review_note || '未填写'}
                        </Typography>
                      </TableCell>
                      <TableCell>
                        {user.review_time
                          ? new Date(user.review_time).toLocaleString('zh-CN')
                          : '-'}
                      </TableCell>
                      <TableCell>
                        <Button
                          size="small"
                          variant="outlined"
                          color="info"
                          startIcon={<Visibility />}
                          onClick={() => handleOpenDetail(user)}
                        >
                          查看
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableContainer>
          <TablePagination
            component="div"
            count={-1}
            page={page}
            onPageChange={(_, newPage) => setPage(newPage)}
            rowsPerPage={rowsPerPage}
            onRowsPerPageChange={(e) => {
              setRowsPerPage(parseInt(e.target.value, 10))
              setPage(0)
            }}
            labelRowsPerPage="每页行数"
            labelDisplayedRows={() => `第 ${page + 1} 页`}
          />
        </TabPanel>
      </Paper>

      {/* 用户详情对话框 */}
      <Dialog open={detailDialogOpen} onClose={handleCloseDetail} maxWidth="md" fullWidth>
        <DialogTitle>用户详情</DialogTitle>
        <DialogContent>
          {selectedUser && (
            <Box sx={{ display: 'flex', flexDirection: { xs: 'column', md: 'row' }, gap: 3, mt: 1 }}>
              {/* 左侧：用户信息 */}
              <Box sx={{ flex: 1 }}>
                <Typography variant="h6" gutterBottom>
                  基本信息
                </Typography>
                <List disablePadding>
                  <ListItem divider>
                    <Box sx={{ minWidth: 100, color: 'text.secondary' }}>用户ID</Box>
                    <Box>{selectedUser.id}</Box>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 100, color: 'text.secondary' }}>用户名</Box>
                    <Box>{selectedUser.username}</Box>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 100, color: 'text.secondary' }}>昵称</Box>
                    <Box>{selectedUser.nickname || '-'}</Box>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 100, color: 'text.secondary' }}>呼号</Box>
                    <Box>{selectedUser.callsign || '-'}</Box>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 100, color: 'text.secondary' }}>手机号</Box>
                    <Box>{selectedUser.phone || '-'}</Box>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 100, color: 'text.secondary' }}>地址</Box>
                    <Box>{selectedUser.address || '-'}</Box>
                  </ListItem>
                  <ListItem divider>
                    <Box sx={{ minWidth: 100, color: 'text.secondary' }}>注册时间</Box>
                    <Box>
                      {selectedUser.created_at
                        ? new Date(selectedUser.created_at).toLocaleString('zh-CN')
                        : '-'}
                    </Box>
                  </ListItem>
                  <ListItem>
                    <Box sx={{ minWidth: 100, color: 'text.secondary' }}>审核状态</Box>
                    <Box>
                      <ApprovalStatusBadge status={selectedUser.approval_status} />
                    </Box>
                  </ListItem>
                </List>
                {selectedUser.review_note && (
                  <Alert severity={selectedUser.approval_status === 2 ? 'error' : 'info'} sx={{ mt: 2 }}>
                    <Typography variant="body2">
                      <strong>审核备注:</strong> {selectedUser.review_note}
                    </Typography>
                  </Alert>
                )}
              </Box>

              {/* 右侧：操作证 */}
              <Box sx={{ flex: 1 }}>
                <Typography variant="h6" gutterBottom>
                  操作证书
                </Typography>
                {/* 只显示最新的过审操作证（status=1） */}
                {selectedUser.certs && selectedUser.certs.length > 0 ? (
                  (() => {
                    // 找到最新的过审操作证（status=1）
                    const approvedCert = selectedUser.certs
                      .filter((cert) => cert.status === 1)
                      .sort((a, b) => b.id - a.id)[0]

                    if (approvedCert) {
                      return (
                        <Card>
                          <CardContent sx={{ pt: 2 }}>
                            <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
                              <Typography variant="body2" fontWeight={500}>
                                过审操作证
                              </Typography>
                              <ApprovalStatusBadge status={1} />
                            </Box>
                            <Typography variant="body2" color="text.secondary" gutterBottom>
                              文件名: {approvedCert.file_name}
                            </Typography>
                            <Typography variant="body2" color="text.secondary" gutterBottom>
                              文件大小: {(approvedCert.file_size / 1024).toFixed(2)} KB
                            </Typography>
                            <Typography variant="body2" color="text.secondary" gutterBottom>
                              上传时间: {approvedCert.upload_time}
                            </Typography>
                            {approvedCert.review_note && (
                              <Alert severity="info" sx={{ mt: 2, py: 1 }}>
                                <Typography variant="body2">
                                  <strong>审核备注:</strong> {approvedCert.review_note}
                                </Typography>
                              </Alert>
                            )}
                            <Divider sx={{ my: 2 }} />
                            {approvedCert.file_url ? (
                              <Box
                                component="img"
                                src={approvedCert.file_url}
                                alt="操作证"
                                onClick={() => {
                                  setPreviewImageUrl(approvedCert.file_url!)
                                  setImagePreviewOpen(true)
                                }}
                                sx={{
                                  width: '100%',
                                  maxHeight: 300,
                                  objectFit: 'contain',
                                  borderRadius: 1,
                                  cursor: 'pointer',
                                  transition: 'transform 0.2s',
                                  '&:hover': {
                                    transform: 'scale(1.02)',
                                    boxShadow: 3,
                                  },
                                }}
                              />
                            ) : (
                              <Box
                                sx={{
                                  width: '100%',
                                  height: 150,
                                  display: 'flex',
                                  alignItems: 'center',
                                  justifyContent: 'center',
                                  bgcolor: 'grey.100',
                                  borderRadius: 1,
                                  color: 'text.secondary',
                                }}
                              >
                                无法预览
                              </Box>
                            )}
                            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1, textAlign: 'center' }}>
                              点击图片可放大查看
                            </Typography>
                          </CardContent>
                        </Card>
                      )
                    }
                    // 没有过审操作证，显示提示
                    return <Alert severity="info">该用户暂无过审的操作证</Alert>
                  })()
                ) : (
                  <Alert severity="warning">该用户未上传操作证</Alert>
                )}
              </Box>
            </Box>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDetail}>关闭</Button>
          {selectedUser && selectedUser.approval_status === 0 && (
            <Button
              variant="contained"
              color="success"
              startIcon={<CheckCircle />}
              onClick={() => {
                handleCloseDetail()
                handleOpenApproveDialog(selectedUser)
              }}
            >
              去审核
            </Button>
          )}
        </DialogActions>
      </Dialog>

      {/* 审批对话框 */}
      <Dialog open={approveDialogOpen} onClose={handleCloseApproveDialog} maxWidth="sm" fullWidth>
        <DialogTitle>
          {selectedUser && selectedUser.approval_status === 1 && selectedUser.cert && selectedUser.cert.status === 0
            ? `审批操作证 - ${selectedUser.username}`
            : `审批用户 - ${selectedUser?.username}`}
        </DialogTitle>
        <DialogContent>
          <Box sx={{ mt: 2 }}>
            {selectedUser && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="body2" color="text.secondary">
                  呼号: {selectedUser.callsign || '-'}
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  手机号: {selectedUser.phone || '-'}
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  操作证: {selectedUser.has_cert ? '已上传' : '未上传'}
                </Typography>
                {selectedUser.approval_status === 1 && selectedUser.cert && selectedUser.cert.status === 0 && (
                  <Alert severity="info" sx={{ mt: 2 }}>
                    该用户账户已审核通过，此操作仅审批操作证
                  </Alert>
                )}
              </Box>
            )}
            <TextField
              fullWidth
              label="审核备注"
              multiline
              rows={3}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="请输入审核意见..."
              helperText="拒绝时必须填写备注"
              required
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseApproveDialog} disabled={submitting}>
            取消
          </Button>
          <Button
            onClick={handleReject}
            color="error"
            startIcon={<Cancel />}
            disabled={submitting}
          >
            拒绝
          </Button>
          <Button
            onClick={handleApprove}
            variant="contained"
            color="success"
            startIcon={<CheckCircle />}
            disabled={submitting}
          >
            通过
          </Button>
        </DialogActions>
      </Dialog>

      {/* 图片预览对话框 */}
      <ImagePreviewDialog
        open={imagePreviewOpen}
        onClose={() => setImagePreviewOpen(false)}
        imageUrl={previewImageUrl}
        title="操作证预览"
      />
    </Box>
  )
}
