import type { Dispatch, MouseEvent, SetStateAction } from 'react'
import {
  Box, Button, Card, CardContent, Chip, FormControl, Grid, InputLabel, MenuItem,
  Select, TextField, Typography,
} from '@mui/material'
import Clear from '@mui/icons-material/Clear'
import FileDownload from '@mui/icons-material/FileDownload'
import Search from '@mui/icons-material/Search'
import { MODE_OPTIONS } from './time'
import type { LogbookFilters as LogbookFilterValues } from './types'

interface LogbookFiltersProps {
  searchFilters: LogbookFilterValues
  setSearchFilters: Dispatch<SetStateAction<LogbookFilterValues>>
  isAdminPage: boolean
  loading: boolean
  applySearchFilters: () => void
  timeDisplayMode: 'bjt' | 'utc'
  setTimeDisplayMode: Dispatch<SetStateAction<'bjt' | 'utc'>>
  hasActiveFilters: boolean
  clearSearchFilters: () => void
  total: number
  filteredCount: number
  selectedCount: number
  handleExportClick: (event: MouseEvent<HTMLButtonElement>) => void
}

export function LogbookFilters({
  searchFilters,
  setSearchFilters,
  isAdminPage,
  loading,
  applySearchFilters,
  timeDisplayMode,
  setTimeDisplayMode,
  hasActiveFilters,
  clearSearchFilters,
  total,
  filteredCount,
  selectedCount,
  handleExportClick,
}: LogbookFiltersProps) {
  return (
    <>      {/* 搜索筛选栏 */}
      <Card sx={{ mb: 2 }}>
        <CardContent>
          <Grid container spacing={2} alignItems="center">
            {/* 时间区间搜索 */}
            <Grid size={{ xs: 12, sm: 6, md: 3 }}>
              <TextField
                fullWidth
                label="开始时间"
                type="datetime-local"
                size="small"
                value={searchFilters.startTime}
                onChange={(e) => setSearchFilters(prev => ({ ...prev, startTime: e.target.value }))}
                slotProps={{ inputLabel: { shrink: true } }}
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 6, md: 3 }}>
              <TextField
                fullWidth
                label="结束时间"
                type="datetime-local"
                size="small"
                value={searchFilters.endTime}
                onChange={(e) => setSearchFilters(prev => ({ ...prev, endTime: e.target.value }))}
                slotProps={{ inputLabel: { shrink: true } }}
              />
            </Grid>

            {/* 对方呼号搜索 */}
            <Grid size={{ xs: 12, sm: 6, md: 2 }}>
              <TextField
                fullWidth
                label="对方呼号"
                size="small"
                value={searchFilters.callsign}
                onChange={(e) => setSearchFilters(prev => ({ ...prev, callsign: e.target.value }))}
                placeholder="例如: BH1ABC"
                InputProps={{
                  startAdornment: <Search fontSize="small" sx={{ mr: 0.5, color: 'text.secondary' }} />,
                }}
              />
            </Grid>

            {/* 频率搜索 */}
            <Grid size={{ xs: 12, sm: 6, md: 2 }}>
              <TextField
                fullWidth
                label="频率 (MHz)"
                size="small"
                type="number"
                value={searchFilters.frequency}
                onChange={(e) => setSearchFilters(prev => ({ ...prev, frequency: e.target.value }))}
                placeholder="例如: 438.5"
                inputProps={{ step: 0.001 }}
              />
            </Grid>

            {/* 模式搜索 */}
            <Grid size={{ xs: 12, sm: 6, md: 2 }}>
              <FormControl fullWidth size="small">
                <InputLabel>模式</InputLabel>
                <Select
                  value={searchFilters.mode}
                  label="模式"
                  onChange={(e) => setSearchFilters(prev => ({ ...prev, mode: e.target.value }))}
                >
                  <MenuItem value="">全部</MenuItem>
                  {MODE_OPTIONS.map(mode => (
                    <MenuItem key={mode} value={mode}>{mode}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>

            {/* 用户名搜索（仅管理员页面） */}
            {isAdminPage && (
              <Grid size={{ xs: 12, sm: 6, md: 2 }}>
                <TextField
                  fullWidth
                  label="所属用户"
                  size="small"
                  value={searchFilters.username}
                  onChange={(e) => setSearchFilters(prev => ({ ...prev, username: e.target.value }))}
                  placeholder="输入用户名搜索"
                  InputProps={{
                    startAdornment: <Search fontSize="small" sx={{ mr: 0.5, color: 'text.secondary' }} />,
                  }}
                />
              </Grid>
            )}
          </Grid>

          {/* 操作按钮行 */}
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', mt: 2 }}>
            <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
              {/* 搜索按钮 */}
              <Button
                size="small"
                variant="contained"
                startIcon={<Search />}
                onClick={applySearchFilters}
                disabled={loading}
              >
                搜索
              </Button>

              {/* 时间显示模式切换 */}
              <Chip
                label="BJT"
                color={timeDisplayMode === 'bjt' ? 'primary' : 'default'}
                onClick={() => setTimeDisplayMode('bjt')}
                size="small"
              />
              <Chip
                label="UTC"
                color={timeDisplayMode === 'utc' ? 'primary' : 'default'}
                onClick={() => setTimeDisplayMode('utc')}
                size="small"
              />

              {/* 清除筛选按钮 */}
              {hasActiveFilters && (
                <Button
                  size="small"
                  variant="outlined"
                  startIcon={<Clear />}
                  onClick={clearSearchFilters}
                >
                  清除筛选
                </Button>
              )}

              {/* 筛选结果统计 */}
              {hasActiveFilters && (
                <Typography variant="body2" color="text.secondary">
                  找到 {total} 条记录
                </Typography>
              )}
            </Box>

            {/* 导出按钮 */}
            <Button
              variant="outlined"
              startIcon={<FileDownload />}
              onClick={handleExportClick}
              disabled={filteredCount === 0}
            >
              导出 {selectedCount > 0 && `(${selectedCount})`}
            </Button>
          </Box>
        </CardContent>
      </Card>

    </>
  )
}
