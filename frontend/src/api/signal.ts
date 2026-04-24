import { getClient } from './client'
import type { SignalRow, ListResponse } from '@/types/api'

/** 获取信号历史 */
export function getSignalHistory(deviceId: string, limit = 120, since?: string) {
  const params: Record<string, string | number> = { limit }
  if (since) params.since = since
  return getClient().get<ListResponse<SignalRow>>(`/signal/${encodeURIComponent(deviceId)}/history`, { params })
}
