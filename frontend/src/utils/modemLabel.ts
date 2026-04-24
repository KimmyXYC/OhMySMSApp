import type { ModemRow, ModemState } from '@/types/api'

/**
 * 生成 modem 显示名称：优先 nickname，fallback 到 model + 末4位 IMEI
 */
export function modemLabel(m: ModemRow | ModemState): string {
  if ('nickname' in m && m.nickname?.trim()) return m.nickname
  const shortImei = m.imei ? `#${m.imei.slice(-4)}` : ''
  return [m.model || m.manufacturer || 'Unknown', shortImei].filter(Boolean).join(' ')
}
