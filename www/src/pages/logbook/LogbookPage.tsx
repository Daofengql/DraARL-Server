import { useState } from 'react'
import { useLocation } from 'react-router-dom'
import {
  Box,
  IconButton,
  Button,
  MenuItem,
  Snackbar,
  Alert,
  Stack,
  Menu,
  ListItemIcon,
  ListItemText,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import Refresh from '@mui/icons-material/Refresh'
import FileDownload from '@mui/icons-material/FileDownload'
import { PageHeader } from '../../components/common/PageHeader'
import { ConfirmDialog } from '../../components/common/ConfirmDialog'
import { UserDetailPopover } from '../../components/UserDetailPopover'
import { apiClient } from '../../services/api'
import type { User } from '../../types'

import { logbookApi } from './api'
import { utcToBjt } from './time'
import type { LogbookEntry } from './types'
import { LogbookFormDialog } from './LogbookFormDialog'
import { LogbookDetailDialog } from './LogbookDetailDialog'
import { PresetManageDialog } from './PresetManageDialog'
import { useLogbookData } from './useLogbookData'
import { LogbookFilters } from './LogbookFilters'
import { LogbookTable } from './LogbookTable'

export function LogbookPage() {
  const location = useLocation()
  const isAdminPage = location.pathname.startsWith('/admin/')

  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [detailDialogOpen, setDetailDialogOpen] = useState(false)
  const [currentEntry, setCurrentEntry] = useState<LogbookEntry | null>(null)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<number | number[] | null>(null)
  const [exportAnchorEl, setExportAnchorEl] = useState<null | HTMLElement>(null)
  const [userDetailAnchorEl, setUserDetailAnchorEl] = useState<HTMLElement | null>(null)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [presetDialogOpen, setPresetDialogOpen] = useState(false)
  const [timeDisplayMode, setTimeDisplayMode] = useState<'bjt' | 'utc'>('bjt')

  const {
    entries,
    filteredEntries,
    total,
    page,
    setPage,
    rowsPerPage,
    setRowsPerPage,
    loading,
    selectedIds,
    setSelectedIds,
    presets,
    loadPresets,
    snackbar,
    setSnackbar,
    searchFilters,
    setSearchFilters,
    clearSearchFilters,
    applySearchFilters,
    hasActiveFilters,
    loadData,
    handleSelectAll,
    handleSelect,
    handleRefresh,
  } = useLogbookData(isAdminPage)

  // 打开用户详情弹窗
  const handleOpenUserDetail = async (event: React.MouseEvent<HTMLElement>, userId: number) => {
    event.stopPropagation()
    const anchorEl = event.currentTarget
    try {
      const response = await apiClient.get(`/api/users/${userId}`)
      if (response.code >= 200 && response.code < 300 && response.data) {
        setSelectedUser(response.data)
        setUserDetailAnchorEl(anchorEl)
      }
    } catch (error) {
      console.error('获取用户信息失败:', error)
    }
  }

  // 关闭用户详情弹窗
  const handleCloseUserDetail = () => {
    setUserDetailAnchorEl(null)
    setSelectedUser(null)
  }

  // 打开新增弹窗
  const handleAdd = () => {
    setCurrentEntry(null)
    setAddDialogOpen(true)
  }

  // 打开编辑弹窗
  const handleEdit = (entry: LogbookEntry) => {
    setCurrentEntry(entry)
    setEditDialogOpen(true)
  }

  // 查看详情
  const handleView = (entry: LogbookEntry) => {
    setCurrentEntry(entry)
    setDetailDialogOpen(true)
  }

  // 删除单条
  const handleDelete = (id: number) => {
    setDeleteTarget(id)
    setDeleteConfirmOpen(true)
  }

  // 批量删除
  const handleBatchDelete = () => {
    if (selectedIds.length === 0) return
    setDeleteTarget(selectedIds)
    setDeleteConfirmOpen(true)
  }

  // 确认删除
  const confirmDelete = async () => {
    if (deleteTarget) {
      const ids = Array.isArray(deleteTarget) ? deleteTarget : [deleteTarget]
      try {
        if (ids.length === 1) {
          const response = await logbookApi.delete(ids[0], isAdminPage)
          if (response.code < 200 || response.code >= 300) {
            setSnackbar({ open: true, message: response.message || '删除失败', severity: 'error' })
            return
          }
        } else {
          const response = await logbookApi.batchDelete(ids, isAdminPage)
          if (response.code < 200 || response.code >= 300) {
            setSnackbar({ open: true, message: response.message || '删除失败', severity: 'error' })
            return
          }
        }
        setSelectedIds([])
        setSnackbar({ open: true, message: `成功删除 ${ids.length} 条记录`, severity: 'success' })
        loadData()
      } catch (error) {
        console.error('删除通联记录失败:', error)
        setSnackbar({ open: true, message: '删除失败', severity: 'error' })
      }
    }
    setDeleteConfirmOpen(false)
    setDeleteTarget(null)
  }

  // 导出菜单
  const handleExportClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    setExportAnchorEl(event.currentTarget)
  }

  const handleExportClose = () => {
    setExportAnchorEl(null)
  }

  // 导出 CSV
  const exportCSV = () => {
    const dataToExport = selectedIds.length > 0
      ? entries.filter(e => selectedIds.includes(e.id))
      : entries

    const headers = ['时间', '频率(MHz)', '模式', '对方呼号', 'RST(收/发)', 'CQ分区', 'ITU分区', 'QTH', '备注']
    const rows = dataToExport.map(e => [
      timeDisplayMode === 'bjt' ? utcToBjt(e.time_utc) : e.time_utc,
      e.tx_frequency,
      e.mode,
      e.callsign,
      `${e.their_rst}/${e.my_rst}`,
      e.cq_zone,
      e.itu_zone,
      e.their_qth || '',
      e.notes || '',
    ])

    const csvContent = [
      headers.join(','),
      ...rows.map(r => r.map(cell => `"${cell}"`).join(','))
    ].join('\n')

    const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `logbook_${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)

    setSnackbar({ open: true, message: `成功导出 ${dataToExport.length} 条记录`, severity: 'success' })
    handleExportClose()
  }

  // 导出 XLS (简单实现，使用 HTML table 格式)
  const exportXLS = () => {
    const dataToExport = selectedIds.length > 0
      ? entries.filter(e => selectedIds.includes(e.id))
      : entries

    const headers = ['时间', '频率(MHz)', '模式', '对方呼号', 'RST(收/发)', 'CQ分区', 'ITU分区', 'QTH', '备注']

    let tableHTML = '<html xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">'
    tableHTML += '<head><meta charset="UTF-8"></head><body><table border="1">'
    tableHTML += '<tr>' + headers.map(h => `<th>${h}</th>`).join('') + '</tr>'

    dataToExport.forEach(e => {
      tableHTML += '<tr>'
      tableHTML += `<td>${timeDisplayMode === 'bjt' ? utcToBjt(e.time_utc) : e.time_utc}</td>`
      tableHTML += `<td>${e.tx_frequency}</td>`
      tableHTML += `<td>${e.mode}</td>`
      tableHTML += `<td>${e.callsign}</td>`
      tableHTML += `<td>${e.their_rst}/${e.my_rst}</td>`
      tableHTML += `<td>${e.cq_zone}</td>`
      tableHTML += `<td>${e.itu_zone}</td>`
      tableHTML += `<td>${e.their_qth || ''}</td>`
      tableHTML += `<td>${e.notes || ''}</td>`
      tableHTML += '</tr>'
    })

    tableHTML += '</table></body></html>'

    const blob = new Blob([tableHTML], { type: 'application/vnd.ms-excel;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `logbook_${new Date().toISOString().slice(0, 10)}.xls`
    a.click()
    URL.revokeObjectURL(url)

    setSnackbar({ open: true, message: `成功导出 ${dataToExport.length} 条记录`, severity: 'success' })
    handleExportClose()
  }

  // 取消选择
  const clearSelection = () => {
    setSelectedIds([])
  }

  return (
    <Box>
      <PageHeader
        title="通联日志"
        subtitle={isAdminPage ? '管理所有用户的通联记录' : '记录您的业余无线电通联'}
        actions={
          <Stack direction="row" spacing={1}>
            {!isAdminPage && (
              <Button
                variant="contained"
                startIcon={<Add />}
                onClick={handleAdd}
              >
                新增记录
              </Button>
            )}
            <IconButton onClick={handleRefresh} disabled={loading} color="primary">
              <Refresh />
            </IconButton>
          </Stack>
        }
      />

      <LogbookFilters
        searchFilters={searchFilters}
        setSearchFilters={setSearchFilters}
        isAdminPage={isAdminPage}
        loading={loading}
        applySearchFilters={applySearchFilters}
        timeDisplayMode={timeDisplayMode}
        setTimeDisplayMode={setTimeDisplayMode}
        hasActiveFilters={Boolean(hasActiveFilters)}
        clearSearchFilters={clearSearchFilters}
        total={total}
        filteredCount={filteredEntries.length}
        selectedCount={selectedIds.length}
        handleExportClick={handleExportClick}
      />

      <LogbookTable
        filteredEntries={filteredEntries}
        total={total}
        page={page}
        setPage={setPage}
        rowsPerPage={rowsPerPage}
        setRowsPerPage={setRowsPerPage}
        loading={loading}
        selectedIds={selectedIds}
        handleSelectAll={handleSelectAll}
        handleSelect={handleSelect}
        handleBatchDelete={handleBatchDelete}
        clearSelection={clearSelection}
        isAdminPage={isAdminPage}
        hasActiveFilters={Boolean(hasActiveFilters)}
        timeDisplayMode={timeDisplayMode}
        handleOpenUserDetail={handleOpenUserDetail}
        handleView={handleView}
        handleEdit={handleEdit}
        handleDelete={handleDelete}
      />

      {/* 导出菜单 */}
      <Menu
        anchorEl={exportAnchorEl}
        open={Boolean(exportAnchorEl)}
        onClose={handleExportClose}
      >
        <MenuItem onClick={exportCSV}>
          <ListItemIcon><FileDownload fontSize="small" /></ListItemIcon>
          <ListItemText>导出 CSV</ListItemText>
        </MenuItem>
        <MenuItem onClick={exportXLS}>
          <ListItemIcon><FileDownload fontSize="small" /></ListItemIcon>
          <ListItemText>导出 XLS</ListItemText>
        </MenuItem>
      </Menu>

      {/* 新增记录弹窗 */}
      <LogbookFormDialog
        open={addDialogOpen}
        onClose={() => setAddDialogOpen(false)}
        onSave={async (entry) => {
          try {
            const response = await logbookApi.create(entry)
            if (response.code >= 200 && response.code < 300) {
              setAddDialogOpen(false)
              setSnackbar({ open: true, message: '添加成功', severity: 'success' })
              loadData()
            } else {
              setSnackbar({ open: true, message: response.message || '添加失败', severity: 'error' })
            }
          } catch (error) {
            console.error('添加通联记录失败:', error)
            setSnackbar({ open: true, message: '添加失败', severity: 'error' })
          }
        }}
        title="新增通联记录"
        presets={presets}
        onManagePresets={() => setPresetDialogOpen(true)}
        isAdminPage={isAdminPage}
      />

      {/* 编辑记录弹窗 */}
      <LogbookFormDialog
        open={editDialogOpen}
        onClose={() => setEditDialogOpen(false)}
        onSave={async (entry) => {
          if (currentEntry) {
            try {
              const response = await logbookApi.update(currentEntry.id, entry, isAdminPage)
              if (response.code >= 200 && response.code < 300) {
                setEditDialogOpen(false)
                setSnackbar({ open: true, message: '保存成功', severity: 'success' })
                loadData()
              } else {
                setSnackbar({ open: true, message: response.message || '保存失败', severity: 'error' })
              }
            } catch (error) {
              console.error('保存通联记录失败:', error)
              setSnackbar({ open: true, message: '保存失败', severity: 'error' })
            }
          }
        }}
        initialData={currentEntry}
        title="编辑通联记录"
        presets={presets}
        onManagePresets={() => setPresetDialogOpen(true)}
        isAdminPage={isAdminPage}
      />

      {/* 详情弹窗 */}
      <LogbookDetailDialog
        open={detailDialogOpen}
        onClose={() => setDetailDialogOpen(false)}
        entry={currentEntry}
        timeDisplayMode={timeDisplayMode}
      />

      {/* 预设管理弹窗 */}
      <PresetManageDialog
        open={presetDialogOpen}
        onClose={() => setPresetDialogOpen(false)}
        onRefresh={loadPresets}
      />

      {/* 删除确认 */}
      <ConfirmDialog
        isOpen={deleteConfirmOpen}
        title="确认删除"
        message={
          Array.isArray(deleteTarget)
            ? `确定要删除选中的 ${deleteTarget.length} 条记录吗？此操作不可撤销。`
            : '确定要删除这条记录吗？此操作不可撤销。'
        }
        onConfirm={confirmDelete}
        onCancel={() => {
          setDeleteConfirmOpen(false)
          setDeleteTarget(null)
        }}
        confirmText="删除"
        type="danger"
      />

      {/* 消息提示 */}
      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert
          severity={snackbar.severity}
          onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        >
          {snackbar.message}
        </Alert>
      </Snackbar>

      {/* 用户详情弹窗（管理员页面用） */}
      {isAdminPage && (
        <UserDetailPopover
          open={Boolean(userDetailAnchorEl)}
          anchorEl={userDetailAnchorEl}
          onClose={handleCloseUserDetail}
          user={selectedUser}
        />
      )}
    </Box>
  )
}

// 表单弹窗组件
