// ─── types/api.ts ───
// 前后端共享类型定义，与 backend/migrations/0001_init.sql 对齐

/** 4G Modem 模块 */
export interface Modem {
  id: number
  device_id: string
  dbus_path: string | null
  manufacturer: string | null
  model: string | null
  firmware: string | null
  imei: string | null
  plugin: string | null
  primary_port: string | null
  at_ports: string[] | null
  qmi_port: string | null
  mbim_port: string | null
  usb_path: string | null
  present: boolean
  first_seen_at: string
  last_seen_at: string
  // 运行时关联（API 可能 join 或 nest）
  sim?: Sim | null
  signal?: SignalSample | null
}

/** SIM 卡 / eSIM Profile */
export interface Sim {
  id: number
  iccid: string
  imsi: string | null
  msisdn: string | null
  operator_id: string | null
  operator_name: string | null
  card_type: 'psim' | 'sticker_esim' | 'embedded_esim'
  esim_card_id: number | null
  esim_profile_active: boolean
  esim_profile_nickname: string | null
  first_seen_at: string
  last_seen_at: string
}

/** Sticker eSIM 物理卡片 */
export interface ESimCard {
  id: number
  eid: string | null
  vendor: string | null
  nickname: string | null
  notes: string | null
  created_at: string
  // 运行时嵌套
  profiles?: Sim[]
}

/** 短信 */
export interface Sms {
  id: number
  sim_id: number | null
  modem_id: number | null
  direction: 'inbound' | 'outbound'
  state: 'received' | 'sending' | 'sent' | 'failed' | 'stored'
  peer: string
  body: string
  ext_id: string | null
  ts_received: string | null
  ts_created: string
  ts_sent: string | null
  error_message: string | null
  pushed_to_tg: boolean
}

/** 短信会话（按 peer 聚合） */
export interface SmsThread {
  peer: string
  sim_id: number
  last_message: Sms
  unread_count: number
  messages: Sms[]
}

/** USSD 会话 */
export interface UssdSession {
  id: number
  sim_id: number | null
  modem_id: number | null
  initial_request: string
  state: 'active' | 'user_response' | 'terminated' | 'failed'
  transcript: UssdTurn[]
  started_at: string
  ended_at: string | null
}

export interface UssdTurn {
  dir: 'request' | 'response'
  ts: string
  text: string
}

/** 信号采样 */
export interface SignalSample {
  id: number
  modem_id: number
  sim_id: number | null
  quality_pct: number | null
  rssi_dbm: number | null
  rsrp_dbm: number | null
  rsrq_db: number | null
  access_tech: 'lte' | 'umts' | 'gsm' | '5gnr' | string | null
  registration: 'home' | 'roaming' | 'searching' | 'denied' | string | null
  operator_id: string | null
  operator_name: string | null
  sampled_at: string
}

/** Modem ↔ Sim 绑定 */
export interface ModemSimBinding {
  modem_id: number
  sim_id: number
  bound_at: string
}

/** 通用分页响应 */
export interface Paginated<T> {
  items: T[]
  total: number
  page: number
  per_page: number
}

/** 认证 */
export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
}

/** USSD 请求 */
export interface UssdInitRequest {
  modem_id: number
  command: string
}

export interface UssdReplyRequest {
  session_id: number
  response: string
}

/** 发送短信请求 */
export interface SmsSendRequest {
  modem_id: number
  peer: string
  body: string
}

/** eSIM Profile 操作 */
export interface ESimProfileAction {
  esim_card_id: number
  iccid: string
  action: 'enable' | 'disable'
}

export interface ESimSetNickname {
  esim_card_id: number
  iccid: string
  nickname: string
}

/** WebSocket 消息信封 */
export interface WsMessage<T = unknown> {
  type:
    | 'sms.new'
    | 'signal.update'
    | 'modem.added'
    | 'modem.removed'
    | 'modem.updated'
    | 'sim.updated'
    | 'ussd.update'
  payload: T
  ts: string
}

/** API 错误体 */
export interface ApiError {
  error: string
  detail?: string
}
