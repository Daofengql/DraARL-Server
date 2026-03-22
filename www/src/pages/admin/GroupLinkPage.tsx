import React, { useState, useEffect } from 'react'
import {
  Box,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Chip,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  Stack,
  Alert,
  Tooltip,
  TablePagination,
  Typography,
  Switch,
  Checkbox,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Divider,
  Card,
  CardHeader,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import Refresh from '@mui/icons-material/Refresh'
import LinkIcon from '@mui/icons-material/Link'
import LinkOff from '@mui/icons-material/LinkOff'
import GroupIcon from '@mui/icons-material/Group'
import { groupLinkService } from '../../services'
import type { VirtualGroup, Group, GroupLinkTarget } from '../../types'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'

export function GroupLinkPage() {
  const [virtualGroups, setVirtualGroups] = useState<VirtualGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(10)

  // 对话框状态
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [linkDialogOpen, setLinkDialogOpen] = useState(false)
  const [selectedVirtualGroup, setSelectedVirtualGroup] = useState<VirtualGroup | null>(null)

  // 表单状态
  const [formData, setFormData] = useState({
    name: '',
    note: '',
    status: 1,
  })

  // 关联群组相关
  const [availableGroups, setAvailableGroups] = useState<Group[]>([])
  const [linkedTargets, setLinkedTargets] = useState<GroupLinkTarget[]>([])
  const [selectedTargets, setSelectedTargets] = useState<number[]>([])
  const [linkWarning, setLinkWarning] = useState<string | null>(null)

  // 确认对话框
  const [confirmDialog, setConfirmDialog] = useState<{
    open: boolean
    title: string
    message: string
    type: 'danger' | 'warning' | 'info'
    onConfirm: () => void
  }>({ open: false, title: '', message: '', type: 'info', onConfirm: () => {} })

  // 加载虚拟互联组列表
  const fetchVirtualGroups = async () => {
    setLoading(true)
    setError(null)
    try {
      const result = await groupLinkService.getVirtualGroups()
      setVirtualGroups(result)
    } catch (err: any) {
      setError(err.message || '获取虚拟互联组列表失败')
    } finally {
      setLoading(false)
    }
  }

  // 加载可用的目标群组
  const fetchAvailableGroups = async () => {
    try {
      const result = await groupLinkService.getAvailableTargetGroups()
      setAvailableGroups(result)
    } catch (err: any) {
      console.error('获取可用目标群组失败:', err)
    }
  }

  // 加载已关联的目标群组
  const fetchLinkedTargets = async (virtualGroupId: number) => {
    try {
      const result = await groupLinkService.getGroupLinkTargets(virtualGroupId)
      setLinkedTargets(result)
    } catch (err: any) {
      console.error('获取已关联群组失败:', err)
      setLinkedTargets([])
    }
  }

  useEffect(() => {
    fetchVirtualGroups()
  }, [])

  // 创建虚拟互联组
  const handleCreate = async () => {
    if (!formData.name.trim()) {
      setError('请输入虚拟互联组名称')
      return
    }
    try {
      await groupLinkService.createVirtualGroup({
        name: formData.name,
        note: formData.note,
        status: formData.status,
      })
      setCreateDialogOpen(false)
      setFormData({ name: '', note: '', status: 1 })
      setSuccess('创建成功')
      fetchVirtualGroups()
      setTimeout(() => setSuccess(null), 3000)
    } catch (err: any) {
      setError(err.message || '创建失败')
    }
  }

  // 更新虚拟互联组
  const handleUpdate = async () => {
    if (!selectedVirtualGroup) return
    if (!formData.name.trim()) {
      setError('请输入虚拟互联组名称')
      return
    }
    try {
      await groupLinkService.updateVirtualGroup(selectedVirtualGroup.id, {
        name: formData.name,
        note: formData.note,
        status: formData.status,
      })
      setEditDialogOpen(false)
      setSelectedVirtualGroup(null)
      setFormData({ name: '', note: '', status: 1 })
      setSuccess('更新成功')
      fetchVirtualGroups()
      setTimeout(() => setSuccess(null), 3000)
    } catch (err: any) {
      setError(err.message || '更新失败')
    }
  }

  // 删除虚拟互联组
  const handleDelete = async (vg: VirtualGroup) => {
    setConfirmDialog({
      open: true,
      title: '删除虚拟互联组',
      message: `确定要删除虚拟互联组 "${vg.name}" 吗？删除后该互联组关联的所有群组将解除互联关系。`,
      type: 'danger',
      onConfirm: async () => {
        try {
          await groupLinkService.deleteVirtualGroup(vg.id)
          setSuccess('删除成功')
          fetchVirtualGroups()
          setTimeout(() => setSuccess(null), 3000)
        } catch (err: any) {
          setError(err.message || '删除失败')
        }
      },
    })
  }

  // 打开编辑对话框
  const handleOpenEdit = (vg: VirtualGroup) => {
    setSelectedVirtualGroup(vg)
    setFormData({
      name: vg.name,
      note: vg.note || '',
      status: vg.status ?? 1,
    })
    setEditDialogOpen(true)
  }

  // 打开关联管理对话框
  const handleOpenLink = async (vg: VirtualGroup) => {
    setSelectedVirtualGroup(vg)
    setLinkWarning(null)
    await Promise.all([fetchAvailableGroups(), fetchLinkedTargets(vg.id)])
    setSelectedTargets([])
    setLinkDialogOpen(true)
  }

  // 添加关联群组
  const handleAddTargets = async () => {
    if (!selectedVirtualGroup || selectedTargets.length === 0) return

    try {
      for (const targetId of selectedTargets) {
        const result = await groupLinkService.addGroupLinkTarget(selectedVirtualGroup.id, targetId)
        if (result.warning) {
          setLinkWarning(result.warning)
        }
      }
      setSelectedTargets([])
      await fetchLinkedTargets(selectedVirtualGroup.id)
      await fetchVirtualGroups()
    } catch (err: any) {
      setError(err.message || '添加关联失败')
    }
  }

  // 移除关联群组
  const handleRemoveTarget = async (targetId: number) => {
    if (!selectedVirtualGroup) return
    try {
      await groupLinkService.removeGroupLinkTarget(selectedVirtualGroup.id, targetId)
      await fetchLinkedTargets(selectedVirtualGroup.id)
      await fetchVirtualGroups()
    } catch (err: any) {
      setError(err.message || '移除关联失败')
    }
  }

  // 切换目标选择
  const handleToggleTarget = (targetId: number) => {
    setSelectedTargets(prev =>
      prev.includes(targetId)
        ? prev.filter(id => id !== targetId)
        : [...prev, targetId]
    )
  }

  // 切换状态
  const handleToggleStatus = async (vg: VirtualGroup) => {
    const newStatus = vg.status === 1 ? 0 : 1
    const actionText = newStatus === 1 ? '启用' : '禁用'
    setConfirmDialog({
      open: true,
      title: `${actionText}虚拟互联组`,
      message: `确定要${actionText}虚拟互联组 "${vg.name}" 吗？`,
      type: newStatus === 1 ? 'info' : 'warning',
      onConfirm: async () => {
        try {
          await groupLinkService.updateVirtualGroup(vg.id, { status: newStatus })
          fetchVirtualGroups()
        } catch (err: any) {
          setError(err.message || `${actionText}失败`)
        }
      },
    })
  }

  // 过滤出未关联的群组
  const unlinkedGroups = availableGroups.filter(
    g => !linkedTargets.some(t => t.target_group_id === g.id)
  )

  return (
    <Box sx={{ height: 'calc(100vh - 120px)', display: 'flex', flexDirection: 'column' }}>
      {/* 标题和操作栏 */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexShrink: 0 }}>
        <Typography variant="h4">互联管理</Typography>
        <Stack direction="row" spacing={2}>
          <Button
            startIcon={<Refresh />}
            onClick={fetchVirtualGroups}
            variant="outlined"
          >
            刷新
          </Button>
          <Button
            startIcon={<Add />}
            onClick={() => {
              setFormData({ name: '', note: '', status: 1 })
              setCreateDialogOpen(true)
            }}
            variant="contained"
          >
            创建互联组
          </Button>
        </Stack>
      </Box>

      {/* 提示信息 */}
      {error && (
        <Alert severity="error" sx={{ mb: 2, flexShrink: 0 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}
      {success && (
        <Alert severity="success" sx={{ mb: 2, flexShrink: 0 }} onClose={() => setSuccess(null)}>
          {success}
        </Alert>
      )}

      {/* 说明卡片 */}
      <Alert severity="info" sx={{ mb: 2, flexShrink: 0 }}>
        <Typography variant="body2">
          <strong>虚拟互联组</strong>：创建一个虚拟群组作为桥梁，将多个实体群组连接起来。
          当任何一个关联群组有语音时，会自动转发到所有其他关联群组。
        </Typography>
      </Alert>

      {/* 虚拟互联组列表 */}
      <Paper variant="outlined" sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <TableContainer sx={{ flex: 1 }}>
          <Table stickyHeader>
            <TableHead>
              <TableRow>
                <TableCell width={60}>ID</TableCell>
                <TableCell>互联组名称</TableCell>
                <TableCell width={120}>关联群组数</TableCell>
                <TableCell width={100}>状态</TableCell>
                <TableCell>备注</TableCell>
                <TableCell width={250} align="right">操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={6} align="center">加载中...</TableCell>
                </TableRow>
              ) : virtualGroups.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} align="center">
                    <Typography color="text.secondary" sx={{ py: 4 }}>
                      暂无虚拟互联组，点击"创建互联组"按钮开始创建
                    </Typography>
                  </TableCell>
                </TableRow>
              ) : (
                virtualGroups
                  .slice(page * rowsPerPage, (page + 1) * rowsPerPage)
                  .map((vg) => (
                    <TableRow key={vg.id} hover>
                      <TableCell>{vg.id}</TableCell>
                      <TableCell>
                        <Stack direction="row" alignItems="center" spacing={1}>
                          <LinkIcon color="primary" fontSize="small" />
                          <Typography fontWeight={500}>{vg.name}</Typography>
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Chip
                          label={`${vg.target_count || 0} 个群组`}
                          size="small"
                          color={(vg.target_count || 0) > 5 ? 'warning' : 'default'}
                        />
                      </TableCell>
                      <TableCell>
                        <Tooltip title={vg.status === 1 ? '点击禁用' : '点击启用'}>
                          <Switch
                            checked={vg.status === 1}
                            onChange={() => handleToggleStatus(vg)}
                            size="small"
                            color={vg.status === 1 ? 'success' : 'default'}
                          />
                        </Tooltip>
                      </TableCell>
                      <TableCell>
                        <Typography
                          sx={{
                            maxWidth: 200,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {vg.note || '-'}
                        </Typography>
                      </TableCell>
                      <TableCell align="right">
                        <Tooltip title="管理关联群组">
                          <IconButton
                            size="small"
                            color="primary"
                            onClick={() => handleOpenLink(vg)}
                          >
                            <LinkIcon />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="编辑">
                          <IconButton
                            size="small"
                            onClick={() => handleOpenEdit(vg)}
                          >
                            <Edit fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="删除">
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => handleDelete(vg)}
                          >
                            <Delete fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  ))
              )}
            </TableBody>
          </Table>
        </TableContainer>
        <TablePagination
          component="div"
          count={virtualGroups.length}
          page={page}
          onPageChange={(_, newPage) => setPage(newPage)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(e) => {
            setRowsPerPage(parseInt(e.target.value, 10))
            setPage(0)
          }}
          labelRowsPerPage="每页行数"
          labelDisplayedRows={({ from, to, count }) => `${from}-${to} 共 ${count} 条`}
        />
      </Paper>

      {/* 创建对话框 */}
      <Dialog open={createDialogOpen} onClose={() => setCreateDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>创建虚拟互联组</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField
              label="互联组名称"
              fullWidth
              required
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              placeholder="例如：全省中继互联"
            />
            <TextField
              label="备注"
              fullWidth
              multiline
              rows={2}
              value={formData.note}
              onChange={(e) => setFormData({ ...formData, note: e.target.value })}
              placeholder="描述该互联组的用途"
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreateDialogOpen(false)}>取消</Button>
          <Button onClick={handleCreate} variant="contained">
            创建
          </Button>
        </DialogActions>
      </Dialog>

      {/* 编辑对话框 */}
      <Dialog open={editDialogOpen} onClose={() => setEditDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>编辑虚拟互联组</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField
              label="互联组名称"
              fullWidth
              required
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            />
            <TextField
              label="备注"
              fullWidth
              multiline
              rows={2}
              value={formData.note}
              onChange={(e) => setFormData({ ...formData, note: e.target.value })}
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditDialogOpen(false)}>取消</Button>
          <Button onClick={handleUpdate} variant="contained">
            保存
          </Button>
        </DialogActions>
      </Dialog>

      {/* 关联管理对话框 */}
      <Dialog
        open={linkDialogOpen}
        onClose={() => setLinkDialogOpen(false)}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle>
          <Stack direction="row" alignItems="center" spacing={1}>
            <LinkIcon />
            <span>管理关联群组 - {selectedVirtualGroup?.name}</span>
          </Stack>
        </DialogTitle>
        <DialogContent>
          {linkWarning && (
            <Alert severity="warning" sx={{ mb: 2 }} onClose={() => setLinkWarning(null)}>
              {linkWarning}
            </Alert>
          )}

          <Stack direction="row" spacing={2} sx={{ height: 400 }}>
            {/* 左侧：已关联的群组 */}
            <Card sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
              <CardHeader
                title={`已关联群组 (${linkedTargets.length})`}
                sx={{ pb: 1 }}
              />
              <Divider />
              <List dense sx={{ flex: 1, overflow: 'auto' }}>
                {linkedTargets.length === 0 ? (
                  <ListItem>
                    <ListItemText
                      secondary="暂无关联的群组"
                      secondaryTypographyProps={{ align: 'center' }}
                    />
                  </ListItem>
                ) : (
                  linkedTargets.map((target) => (
                    <ListItem
                      key={target.id}
                      disablePadding
                      secondaryAction={
                        <IconButton
                          edge="end"
                          size="small"
                          color="error"
                          onClick={() => handleRemoveTarget(target.target_group_id)}
                        >
                          <LinkOff />
                        </IconButton>
                      }
                    >
                      <ListItemButton sx={{ pr: 6 }}>
                        <ListItemIcon>
                          <GroupIcon color="primary" />
                        </ListItemIcon>
                        <ListItemText
                          primary={target.target_group_name}
                          secondary={`ID: ${target.target_group_id}`}
                        />
                      </ListItemButton>
                    </ListItem>
                  ))
                )}
              </List>
            </Card>

            {/* 右侧：可添加的群组 */}
            <Card sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
              <CardHeader
                title={`可添加的群组 (${unlinkedGroups.length})`}
                sx={{ pb: 1 }}
                action={
                  selectedTargets.length > 0 && (
                    <Button
                      size="small"
                      variant="contained"
                      startIcon={<Add />}
                      onClick={handleAddTargets}
                    >
                      添加选中 ({selectedTargets.length})
                    </Button>
                  )
                }
              />
              <Divider />
              <List dense sx={{ flex: 1, overflow: 'auto' }}>
                {unlinkedGroups.length === 0 ? (
                  <ListItem>
                    <ListItemText
                      secondary="没有可添加的群组"
                      secondaryTypographyProps={{ align: 'center' }}
                    />
                  </ListItem>
                ) : (
                  unlinkedGroups.map((group) => (
                    <ListItem
                      key={group.id}
                      disablePadding
                    >
                      <ListItemButton
                        dense
                        onClick={() => handleToggleTarget(group.id)}
                      >
                        <ListItemIcon>
                          <Checkbox
                            edge="start"
                            checked={selectedTargets.includes(group.id)}
                            tabIndex={-1}
                            disableRipple
                            size="small"
                          />
                        </ListItemIcon>
                        <ListItemText
                          primary={group.name}
                          secondary={`ID: ${group.id}`}
                        />
                      </ListItemButton>
                    </ListItem>
                  ))
                )}
              </List>
            </Card>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setLinkDialogOpen(false)}>关闭</Button>
        </DialogActions>
      </Dialog>

      {/* 确认对话框 */}
      <ConfirmDialog
        isOpen={confirmDialog.open}
        title={confirmDialog.title}
        message={confirmDialog.message}
        type={confirmDialog.type}
        onConfirm={() => {
          confirmDialog.onConfirm()
          setConfirmDialog(prev => ({ ...prev, open: false }))
        }}
        onCancel={() => setConfirmDialog(prev => ({ ...prev, open: false }))}
      />
    </Box>
  )
}
