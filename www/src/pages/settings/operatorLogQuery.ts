export interface OperatorLogQuery {
  page: number
  pageSize: number
  eventType: string
}

export function buildOperatorLogParams(query: OperatorLogQuery) {
  return {
    page: query.page + 1,
    page_size: query.pageSize,
    event_type: query.eventType || undefined,
  }
}
