export interface LogbookListParams {
  page?: number
  page_size?: number
  start_time?: string
  end_time?: string
  callsign?: string
  frequency?: number
  mode?: string
  user_id?: number
  username?: string
}

export function buildLogbookListURL(params: LogbookListParams, isAdmin: boolean = false): string {
  const queryParams = new URLSearchParams()
  if (params.page) queryParams.set('page', String(params.page))
  if (params.page_size) queryParams.set('page_size', String(params.page_size))
  if (params.start_time) queryParams.set('start_time', params.start_time)
  if (params.end_time) queryParams.set('end_time', params.end_time)
  if (params.callsign) queryParams.set('callsign', params.callsign)
  if (params.frequency) queryParams.set('frequency', String(params.frequency))
  if (params.mode) queryParams.set('mode', params.mode)
  if (params.user_id) queryParams.set('user_id', String(params.user_id))
  if (params.username) queryParams.set('username', params.username)

  const basePath = isAdmin ? '/api/admin/logbooks' : '/api/logbooks'
  const query = queryParams.toString()
  return query ? `${basePath}?${query}` : basePath
}

export function logbookBatchDeletePath(isAdmin: boolean): string {
  return isAdmin ? '/api/admin/logbooks/batch' : '/api/logbooks/batch'
}
