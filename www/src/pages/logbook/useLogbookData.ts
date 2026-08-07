import { useCallback, useEffect, useState, type ChangeEvent } from 'react'

import { apiClient } from '../../services/api'
import { logbookApi } from './api'
import { createEmptyLogbookFilters, hasActiveLogbookFilters } from './filterState'
import { bjtToUtc } from './time'
import type {
  LogbookEntry,
  LogbookFilters,
  LogbookSnackbar,
  RadioPreset,
  RadioPresetListResponse,
} from './types'

export function useLogbookData(isAdminPage: boolean) {
  const [entries, setEntries] = useState<LogbookEntry[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [rowsPerPage, setRowsPerPage] = useState(10)
  const [loading, setLoading] = useState(true)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [presets, setPresets] = useState<RadioPreset[]>([])
  const [searchFilters, setSearchFilters] = useState<LogbookFilters>(createEmptyLogbookFilters)
  const [appliedFilters, setAppliedFilters] = useState<LogbookFilters>(createEmptyLogbookFilters)
  const [snackbar, setSnackbar] = useState<LogbookSnackbar>({
    open: false,
    message: '',
    severity: 'success',
  })

  const loadPresets = useCallback(async () => {
    try {
      const response = await apiClient.get<RadioPresetListResponse>('/api/user/radio-presets')
      if (response.code === 200) {
        setPresets(response.data || [])
      }
    } catch (error) {
      console.error('加载电台预设失败:', error)
    }
  }, [])

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const params = {
        page,
        page_size: rowsPerPage,
        ...(appliedFilters.startTime && { start_time: bjtToUtc(appliedFilters.startTime) }),
        ...(appliedFilters.endTime && { end_time: bjtToUtc(appliedFilters.endTime) }),
        ...(appliedFilters.callsign && { callsign: appliedFilters.callsign }),
        ...(appliedFilters.frequency && { frequency: parseFloat(appliedFilters.frequency) }),
        ...(appliedFilters.mode && { mode: appliedFilters.mode }),
        ...(isAdminPage && appliedFilters.username && { username: appliedFilters.username }),
      }
      const response = await logbookApi.getList(params, isAdminPage)
      if (response.code >= 200 && response.code < 300) {
        setEntries(response.data.items)
        setTotal(response.data.total)
      } else {
        setSnackbar({ open: true, message: response.message || '加载失败', severity: 'error' })
      }
    } catch (error) {
      console.error('加载通联日志失败:', error)
      setSnackbar({ open: true, message: '加载失败', severity: 'error' })
    } finally {
      setLoading(false)
    }
  }, [page, rowsPerPage, appliedFilters, isAdminPage])

  useEffect(() => {
    void loadData()
  }, [loadData])

  useEffect(() => {
    void loadPresets()
  }, [loadPresets])

  const clearSearchFilters = () => {
    setSearchFilters(createEmptyLogbookFilters())
    setAppliedFilters(createEmptyLogbookFilters())
  }

  const applySearchFilters = () => {
    setAppliedFilters({ ...searchFilters })
    setPage(1)
  }

  const handleSelectAll = (event: ChangeEvent<HTMLInputElement>) => {
    setSelectedIds(event.target.checked ? entries.map((entry) => entry.id) : [])
  }

  const handleSelect = (id: number) => {
    setSelectedIds((current) =>
      current.includes(id) ? current.filter((entryID) => entryID !== id) : [...current, id],
    )
  }

  return {
    entries,
    filteredEntries: entries,
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
    hasActiveFilters: hasActiveLogbookFilters(appliedFilters),
    loadData,
    handleSelectAll,
    handleSelect,
    handleRefresh: loadData,
  }
}
