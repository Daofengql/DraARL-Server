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
  Chip,
  Alert,
  TextField,
  IconButton,
  Tab,
  Tabs,
} from '@mui/material'
import {
  CheckCircle,
  Cancel,
  Refresh,
  Person,
  Close,
} from '@mui/icons-material'
import { approvalService } from '../../services'
import type { CertificateApproval } from '../../types'

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

export function CertificateApprovalsPage() {
  const [tabValue, setTabValue] = useState(0)
  const [pendingCerts, setPendingCerts] = useState<CertificateApproval[]>([])
  const [approvedCerts, setApprovedCerts] = useState<CertificateApproval[]>([])
  const [rejectedCerts, setRejectedCerts] = useState<CertificateApproval[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  // 分页状态
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)

  // 审批对话框
  const [approveDialogOpen, setApproveDialogOpen] = useState(false)
  const [selectedCert, setSelectedCert] = useState<CertificateApproval | null>(null)
  const [note, setNote] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // 图片预览对话框
  const [imagePreviewOpen, setImagePreviewOpen] = useState(false)
  const [previewImageUrl, setPreviewImageUrl] = useState<string | null>(null)
  const [imageScale, setImageScale] = useState(1)

  useEffect(() => {
    loadAllTabData()
  }, [page, rowsPerPage])

  useEffect(() => {
    loadTabData(tabValue)
  }, [tabValue])

  const loadAllTabData = async () => {
    setLoading(true)
    setError('')
    try {
      const [pendingData, approvedData, rejectedData] = await Promise.all([
        approvalService.getCertificateApprovals(page + 1, rowsPerPage, 0),
        approvalService.getCertificateApprovals(page + 1, rowsPerPage, 1),
        approvalService.getCertificateApprovals(page + 1, rowsPerPage, 2),
      ])
      setPendingCerts(pendingData.items || [])
      setApprovedCerts(approvedData.items || [])
      setRejectedCerts(rejectedData.items || [])
    } catch (err: any) {
      console.error('Failed to load certificate approvals:', err)
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
      const data = await approvalService.getCertificateApprovals(page + 1, rowsPerPage, status)

      if (status === 0) {
        setPendingCerts(data.items || [])
      } else if (status === 1) {
        setApprovedCerts(data.items || [])
      } else {
        setRejectedCerts(data.items || [])
      }
    } catch (err: any) {
      console.error('Failed to load certificate approvals:', err)
      setError(err.response?.data?.message || '加载数据失败')
    } finally {
      setLoading(false)
    }
  }

  const loadCertificateApprovals = async () => {
    await loadTabData(tabValue)
  }

  const handleOpenApproveDialog = (cert: CertificateApproval) => {
    setSelectedCert(cert)
    setNote(cert.review_note || '')
    setApproveDialogOpen(true)
  }

  const handleCloseApproveDialog = () => {
    setApproveDialogOpen(false)
    setSelectedCert(null)
    setNote('')
  }

  const handleApprove = async () => {
    if (!selectedCert) return

    setSubmitting(true)
    try {
      await approvalService.approveCertificate(selectedCert.id, { status: 1, note })
      showMessage('success', '操作证已通过审核')
      handleCloseApproveDialog()
      loadCertificateApprovals()
    } catch (err: any) {
      showMessage('error', err.response?.data?.message || '操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleReject = async () => {
    if (!selectedCert) return

    if (!note.trim()) {
      showMessage('error', '请填写拒绝原因')
      return
    }

    setSubmitting(true)
    try {
      await approvalService.approveCertificate(selectedCert.id, { status: 2, note })
      showMessage('success', '操作证已拒绝')
      handleCloseApproveDialog()
      loadCertificateApprovals()
    } catch (err: any) {
      showMessage('error', err.response?.data?.message || '操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  const currentTabCerts = tabValue === 0 ? pendingCerts : tabValue === 1 ? approvedCerts : rejectedCerts

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4">操作证审批</Typography>
        <Button
          variant="outlined"
          startIcon={<Refresh />}
          onClick={loadCertificateApprovals}
          disabled={loading}
        >
          刷新
        </Button>
      </Box>

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
          <Tab label={`待审核 (${pendingCerts.length})`} />
          <Tab label={`已通过 (${approvedCerts.length})`} />
          <Tab label={`已拒绝 (${rejectedCerts.length})`} />
        </Tabs>

        <TabPanel value={tabValue} index={0}>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>ID</TableCell>
                  <TableCell>用户名</TableCell>
                  <TableCell>呼号</TableCell>
                  <TableCell>类型</TableCell>
                  <TableCell>文件名</TableCell>
                  <TableCell>操作证图片</TableCell>
                  <TableCell>上传时间</TableCell>
                  <TableCell>操作</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={8} align="center">
                      加载中...
                    </TableCell>
                  </TableRow>
                ) : currentTabCerts.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} align="center">
                      暂无数据
                    </TableCell>
                  </TableRow>
                ) : (
                  currentTabCerts.map((cert) => (
                    <TableRow key={cert.id} hover>
                      <TableCell>{cert.id}</TableCell>
                      <TableCell>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                          <Person color="primary" fontSize="small" />
                          {cert.username}
                        </Box>
                      </TableCell>
                      <TableCell>{cert.callsign || '-'}</TableCell>
                      <TableCell>
                        {cert.is_update ? (
                          <Chip label="更新" size="small" color="info" variant="outlined" />
                        ) : (
                          <Chip label="首次" size="small" color="success" variant="outlined" />
                        )}
                      </TableCell>
                      <TableCell>{cert.file_name}</TableCell>
                      <TableCell>
                        {cert.file_url ? (
                          <Box
                            component="img"
                            src={cert.file_url}
                            alt="操作证"
                            onClick={() => {
                              setPreviewImageUrl(cert.file_url!)
                              setImageScale(1)
                              setImagePreviewOpen(true)
                            }}
                            sx={{
                              width: 60,
                              height: 60,
                              objectFit: 'cover',
                              borderRadius: 1,
                              cursor: 'pointer',
                              border: '1px solid',
                              borderColor: 'divider',
                            }}
                          />
                        ) : (
                          <Chip label="无预览" size="small" variant="outlined" />
                        )}
                      </TableCell>
                      <TableCell>
                        {cert.upload_time
                          ? new Date(cert.upload_time).toLocaleString('zh-CN')
                          : '-'}
                      </TableCell>
                      <TableCell>
                        <Box sx={{ display: 'flex', gap: 0.5 }}>
                          <Button
                            size="small"
                            variant="outlined"
                            color="error"
                            startIcon={<Cancel />}
                            onClick={() => handleOpenApproveDialog(cert)}
                          >
                            拒绝
                          </Button>
                          <Button
                            size="small"
                            variant="contained"
                            color="success"
                            startIcon={<CheckCircle />}
                            onClick={() => handleOpenApproveDialog(cert)}
                          >
                            通过
                          </Button>
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

        <TabPanel value={tabValue} index={1}>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>ID</TableCell>
                  <TableCell>用户名</TableCell>
                  <TableCell>呼号</TableCell>
                  <TableCell>类型</TableCell>
                  <TableCell>文件名</TableCell>
                  <TableCell>审核时间</TableCell>
                  <TableCell>审核人</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center">
                      加载中...
                    </TableCell>
                  </TableRow>
                ) : currentTabCerts.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center">
                      暂无数据
                    </TableCell>
                  </TableRow>
                ) : (
                  currentTabCerts.map((cert) => (
                    <TableRow key={cert.id} hover>
                      <TableCell>{cert.id}</TableCell>
                      <TableCell>{cert.username}</TableCell>
                      <TableCell>{cert.callsign || '-'}</TableCell>
                      <TableCell>
                        {cert.is_update ? (
                          <Chip label="更新" size="small" color="info" variant="outlined" />
                        ) : (
                          <Chip label="首次" size="small" color="success" variant="outlined" />
                        )}
                      </TableCell>
                      <TableCell>{cert.file_name}</TableCell>
                      <TableCell>
                        {cert.review_time
                          ? new Date(cert.review_time).toLocaleString('zh-CN')
                          : '-'}
                      </TableCell>
                      <TableCell>{cert.reviewer_id || '-'}</TableCell>
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
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>ID</TableCell>
                  <TableCell>用户名</TableCell>
                  <TableCell>呼号</TableCell>
                  <TableCell>类型</TableCell>
                  <TableCell>拒绝原因</TableCell>
                  <TableCell>审核时间</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={6} align="center">
                      加载中...
                    </TableCell>
                  </TableRow>
                ) : currentTabCerts.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} align="center">
                      暂无数据
                    </TableCell>
                  </TableRow>
                ) : (
                  currentTabCerts.map((cert) => (
                    <TableRow key={cert.id} hover>
                      <TableCell>{cert.id}</TableCell>
                      <TableCell>{cert.username}</TableCell>
                      <TableCell>{cert.callsign || '-'}</TableCell>
                      <TableCell>
                        {cert.is_update ? (
                          <Chip label="更新" size="small" color="info" variant="outlined" />
                        ) : (
                          <Chip label="首次" size="small" color="success" variant="outlined" />
                        )}
                      </TableCell>
                      <TableCell>
                        <Typography variant="body2" color="error">
                          {cert.review_note || '未填写'}
                        </Typography>
                      </TableCell>
                      <TableCell>
                        {cert.review_time
                          ? new Date(cert.review_time).toLocaleString('zh-CN')
                          : '-'}
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

      {/* 审批对话框 */}
      <Dialog open={approveDialogOpen} onClose={handleCloseApproveDialog} maxWidth="sm" fullWidth>
        <DialogTitle>审批操作证 - {selectedCert?.username}</DialogTitle>
        <DialogContent>
          {selectedCert && (
            <Box sx={{ mt: 2 }}>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                呼号: {selectedCert.callsign || '-'}
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                类型: {selectedCert.is_update ? '更新操作证' : '首次上传'}
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                文件名: {selectedCert.file_name}
              </Typography>
              {selectedCert.file_url && (
                <Box sx={{ mt: 2, mb: 2 }}>
                  <Typography variant="body2" color="text.secondary" gutterBottom>
                    操作证预览:
                  </Typography>
                  <Box
                    component="img"
                    src={selectedCert.file_url}
                    alt="操作证"
                    onClick={() => {
                      setPreviewImageUrl(selectedCert.file_url!)
                      setImageScale(1)
                      setImagePreviewOpen(true)
                    }}
                    sx={{
                      width: '100%',
                      maxHeight: 200,
                      objectFit: 'contain',
                      borderRadius: 1,
                      cursor: 'pointer',
                      border: '1px solid',
                      borderColor: 'divider',
                    }}
                  />
                </Box>
              )}
              {selectedCert.review_note && selectedCert.status !== 0 && (
                <Alert severity={selectedCert.status === 2 ? 'error' : 'info'} sx={{ mt: 2 }}>
                  <Typography variant="body2">
                    <strong>之前的审核备注:</strong> {selectedCert.review_note}
                  </Typography>
                </Alert>
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
                sx={{ mt: 2 }}
              />
            </Box>
          )}
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
      <Dialog
        open={imagePreviewOpen}
        onClose={() => {
          setImagePreviewOpen(false)
          setImageScale(1)
        }}
        maxWidth="lg"
        fullWidth
        PaperProps={{
          sx: {
            bgcolor: 'rgba(0, 0, 0, 0.9)',
          },
        }}
      >
        <DialogTitle
          sx={{
            color: 'white',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
          }}
        >
          <Typography>操作证预览</Typography>
          <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
            <Typography variant="caption" sx={{ color: 'grey.400' }}>
              滚轮缩放 • {Math.round(imageScale * 100)}%
            </Typography>
            <IconButton
              size="small"
              onClick={() => setImagePreviewOpen(false)}
              sx={{ color: 'white' }}
            >
              <Close />
            </IconButton>
          </Box>
        </DialogTitle>
        <DialogContent
          sx={{
            bgcolor: 'transparent',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            overflow: 'hidden',
          }}
        >
          <Box
            component="img"
            src={previewImageUrl || ''}
            alt="操作证预览"
            onWheel={(e) => {
              e.preventDefault()
              const delta = e.deltaY > 0 ? -0.1 : 0.1
              setImageScale((prev) => Math.max(0.1, Math.min(5, prev + delta)))
            }}
            sx={{
              maxWidth: '100%',
              maxHeight: '70vh',
              objectFit: 'contain',
              transform: `scale(${imageScale})`,
              transition: 'transform 0.1s',
              cursor: 'zoom-in',
            }}
          />
          <Box sx={{ display: 'flex', gap: 2, mt: 2 }}>
            <Button
              size="small"
              variant="outlined"
              sx={{ color: 'white', borderColor: 'white' }}
              onClick={() => setImageScale((prev) => Math.max(0.1, prev - 0.2))}
              disabled={imageScale <= 0.1}
            >
              缩小
            </Button>
            <Button
              size="small"
              variant="outlined"
              sx={{ color: 'white', borderColor: 'white' }}
              onClick={() => setImageScale(1)}
            >
              重置
            </Button>
            <Button
              size="small"
              variant="outlined"
              sx={{ color: 'white', borderColor: 'white' }}
              onClick={() => setImageScale((prev) => Math.min(5, prev + 0.2))}
              disabled={imageScale >= 5}
            >
              放大
            </Button>
          </Box>
        </DialogContent>
      </Dialog>
    </Box>
  )
}
