import type { ChangeEvent, MouseEvent } from 'react'
import {
  Box, Button, Card, CardContent, Checkbox, Chip, IconButton, Paper, Stack,
  Table, TableBody, TableCell, TableContainer, TableHead, TablePagination,
  TableRow, Tooltip, Typography,
} from '@mui/material'
import Delete from '@mui/icons-material/Delete'
import Edit from '@mui/icons-material/Edit'
import Person from '@mui/icons-material/Person'
import Visibility from '@mui/icons-material/Visibility'
import { utcToBjt } from './time'
import type { LogbookEntry } from './types'

interface LogbookTableProps {
  filteredEntries: LogbookEntry[]
  total: number
  page: number
  setPage: (page: number) => void
  rowsPerPage: number
  setRowsPerPage: (rows: number) => void
  loading: boolean
  selectedIds: number[]
  handleSelectAll: (event: ChangeEvent<HTMLInputElement>) => void
  handleSelect: (id: number) => void
  handleBatchDelete: () => void
  clearSelection: () => void
  isAdminPage: boolean
  hasActiveFilters: boolean
  timeDisplayMode: 'bjt' | 'utc'
  handleOpenUserDetail: (event: MouseEvent<HTMLElement>, userID: number) => void
  handleView: (entry: LogbookEntry) => void
  handleEdit: (entry: LogbookEntry) => void
  handleDelete: (id: number) => void
}

export function LogbookTable({
  filteredEntries,
  total,
  page,
  setPage,
  rowsPerPage,
  setRowsPerPage,
  loading,
  selectedIds,
  handleSelectAll,
  handleSelect,
  handleBatchDelete,
  clearSelection,
  isAdminPage,
  hasActiveFilters,
  timeDisplayMode,
  handleOpenUserDetail,
  handleView,
  handleEdit,
  handleDelete,
}: LogbookTableProps) {
  const formatFrequency = (entry: LogbookEntry) => {
    if (entry.tx_frequency === entry.rx_frequency) {
      return entry.tx_frequency.toFixed(4)
    }
    return `${entry.tx_frequency.toFixed(4)} / ${entry.rx_frequency.toFixed(4)}`
  }

  const getTimeDisplay = (entry: LogbookEntry) =>
    timeDisplayMode === 'bjt' ? utcToBjt(entry.time_utc) : entry.time_utc

  return (
    <>      {/* 批量操作栏 */}
      {selectedIds.length > 0 && (
        <Card sx={{ mb: 2, bgcolor: 'primary.light', color: 'primary.contrastText' }}>
          <CardContent sx={{ py: 1.5 }}>
            <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
              <Typography variant="body2">
                已选择 {selectedIds.length} 项
              </Typography>
              <Button
                size="small"
                variant="contained"
                color="error"
                startIcon={<Delete />}
                onClick={handleBatchDelete}
              >
                批量删除
              </Button>
              <Button
                size="small"
                variant="outlined"
                color="inherit"
                onClick={clearSelection}
              >
                取消选择
              </Button>
            </Box>
          </CardContent>
        </Card>
      )}

      {/* 表格 */}
      <TableContainer component={Paper}>
        <Table sx={{ minWidth: 900 }}>
          <TableHead>
            <TableRow>
              <TableCell padding="checkbox">
                <Checkbox
                  indeterminate={selectedIds.length > 0 && selectedIds.length < filteredEntries.length}
                  checked={filteredEntries.length > 0 && selectedIds.length === filteredEntries.length}
                  onChange={handleSelectAll}
                />
              </TableCell>
              <TableCell>时间</TableCell>
              <TableCell>频率 (MHz)</TableCell>
              <TableCell>模式</TableCell>
              <TableCell>对方呼号</TableCell>
              <TableCell>RST (收/发)</TableCell>
              <TableCell>CQ/ITU</TableCell>
              <TableCell>QTH</TableCell>
              {isAdminPage && <TableCell>所属用户</TableCell>}
              <TableCell align="center">操作</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={isAdminPage ? 10 : 9} align="center" sx={{ py: 4 }}>
                  加载中...
                </TableCell>
              </TableRow>
            ) : filteredEntries.length === 0 ? (
              <TableRow>
                <TableCell colSpan={isAdminPage ? 10 : 9} align="center" sx={{ py: 4 }}>
                  <Typography color="text.secondary">
                    {hasActiveFilters ? '没有找到符合条件的记录' : (isAdminPage ? '暂无通联记录' : '暂无通联记录，点击"新增记录"添加您的第一条通联')}
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              filteredEntries.map((entry) => (
                  <TableRow
                    key={entry.id}
                    hover
                    selected={selectedIds.includes(entry.id)}
                  >
                    <TableCell padding="checkbox">
                      <Checkbox
                        checked={selectedIds.includes(entry.id)}
                        onChange={() => handleSelect(entry.id)}
                      />
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" noWrap>
                        {getTimeDisplay(entry)}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {timeDisplayMode === 'bjt' ? 'BJT' : 'UTC'}
                      </Typography>
                    </TableCell>
                    <TableCell>{formatFrequency(entry)}</TableCell>
                    <TableCell>
                      <Chip label={entry.mode} size="small" variant="outlined" />
                    </TableCell>
                    <TableCell>
                      <Typography fontWeight="medium">{entry.callsign}</Typography>
                    </TableCell>
                    <TableCell>{entry.their_rst}/{entry.my_rst}</TableCell>
                    <TableCell>{entry.cq_zone}/{entry.itu_zone}</TableCell>
                    <TableCell>{entry.their_qth || '-'}</TableCell>
                    {isAdminPage && (
                      <TableCell>
                        <Box
                          onClick={(e) => entry.user_id && handleOpenUserDetail(e, entry.user_id)}
                          sx={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: 0.5,
                            cursor: entry.user_id ? 'pointer' : 'default',
                            '&:hover': entry.user_id ? {
                              color: 'primary.main',
                              textDecoration: 'underline',
                            } : {},
                          }}
                        >
                          <Person fontSize="small" />
                          <Typography variant="body2">
                            {entry.username || '-'}
                          </Typography>
                        </Box>
                      </TableCell>
                    )}
                    <TableCell align="center">
                      <Stack direction="row" spacing={0.5} justifyContent="center">
                        <Tooltip title="查看详情">
                          <IconButton size="small" onClick={() => handleView(entry)}>
                            <Visibility fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="编辑">
                          <IconButton size="small" onClick={() => handleEdit(entry)} color="primary">
                            <Edit fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="删除">
                          <IconButton size="small" onClick={() => handleDelete(entry.id)} color="error">
                            <Delete fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Stack>
                    </TableCell>
                  </TableRow>
                ))
            )}
          </TableBody>
        </Table>
        <TablePagination
          component="div"
          count={total}
          page={page - 1}
          onPageChange={(_, newPage) => setPage(newPage + 1)}
          rowsPerPage={rowsPerPage}
          onRowsPerPageChange={(e) => {
            setRowsPerPage(parseInt(e.target.value, 10))
            setPage(1)
          }}
          labelRowsPerPage="每页行数"
          labelDisplayedRows={({ from, to, count }) => `${from}-${to} 共 ${count} 条`}
          rowsPerPageOptions={[5, 10, 25, 50]}
        />
      </TableContainer>

    </>
  )
}