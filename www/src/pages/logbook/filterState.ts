import type { LogbookFilters } from './types'

export function createEmptyLogbookFilters(): LogbookFilters {
  return {
    startTime: '',
    endTime: '',
    callsign: '',
    frequency: '',
    mode: '',
    username: '',
  }
}

export function hasActiveLogbookFilters(filters: LogbookFilters): boolean {
  return Object.values(filters).some(Boolean)
}
