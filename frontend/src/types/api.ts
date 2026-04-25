// ─── types/api.ts ───
// 前后端共享类型，严格对齐后端蛇形字段命名

// ───────────── 通用 ─────────────

/** 通用列表响应包装 */
export interface ListResponse<T> {
  items: T[]
  total: number
  limit?: number
  offset?: number
}

/** API 错误体 */
export interface ApiError {
  error: string
  code?: string
}

// ───────────── Auth ─────────────

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  expires_at: string
  user: { username: string }
}

// ───────────── Modem (DB Row) ─────────────

/** ModemRow — 来自 GET /api/modems */
export interface ModemRow {
  id: number
  device_id: string
  dbus_path: string | null
  manufacturer: string | null
  model: string | null
  firmware: string | null
  imei: string | null
  nickname?: string | null
  plugin: string | null
  primary_port: string | null
  at_ports: string[] | null
  qmi_port: string | null
  mbim_port: string | null
  usb_path: string | null
  present: boolean
  first_seen_at: string
  last_seen_at: string
  sim?: SimRow | null
  signal?: SignalRow | null
}

// ───────────── ModemState (WS 推送快照) ─────────────

export interface Port {
  name: string
  type: string
}

/** ModemState — WS modem.added / modem.updated 的 data */
export interface ModemState {
  device_id: string
  dbus_path: string
  manufacturer: string
  model: string
  revision: string
  hardware_revision: string
  plugin: string
  imei: string
  primary_port: string
  ports: Port[]
  usb_path: string
  state: string
  failed_reason: string
  power_state: string
  access_tech: string[]
  signal_quality: number
  signal_recent: boolean
  registration: string
  operator_id: string
  operator_name: string
  own_numbers: string[]
  has_sim: boolean
  sim?: SimState | null
  has_ussd: boolean
  has_signal: boolean
  has_messaging: boolean
  supported_storages: string[]
}

/** SimState — 来自 WS sim.updated */
export interface SimState {
  dbus_path: string
  iccid: string
  imsi: string
  eid: string
  operator_id: string
  operator_name: string
  active: boolean
  emergency_numbers: string[]
  sim_type: string
}

// ───────────── SIM (DB Row) ─────────────

export interface SimRow {
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

// ───────────── SMS ─────────────

/** SMSRow — 来自 DB (GET /api/sms) */
export interface SMSRow {
  id: number
  sim_id: number | null
  modem_id: number | null
  direction: 'inbound' | 'outbound'
  state: string
  peer: string
  body: string
  ext_id: string | null
  ts_received: string | null
  ts_created: string
  ts_sent: string | null
  error_message: string | null
  pushed_to_tg: boolean
}

/** ThreadRow — 来自 GET /api/sms/threads */
export interface ThreadRow {
  peer: string
  sim_id: number | null
  last_text: string
  last_time: string
  count: number
  direction: string
  state: string
}

/** SMSRecord — WS sms.received 的 sms 字段 (来自 provider) */
export interface SMSRecord {
  ext_id: string
  direction: string
  state: string
  peer: string
  text: string
  smsc: string
  timestamp: string
  storage: string
  delivery_state: number
}

export interface SmsSendRequest {
  device_id: string
  peer: string
  body: string
}

// ───────────── USSD ─────────────

export interface UssdInitRequest {
  device_id: string
  command: string
}

/** USSD HTTP 响应 */
export interface UssdResponse {
  session_id: string
  reply: string
  state: string
  network_request?: string
  network_notification?: string
  device_id?: string
  session_row_id?: number
  started_at?: string
}

/** USSDRow — 来自 DB (GET /api/ussd/sessions) */
export interface USSDRow {
  id: number
  sim_id: number | null
  modem_id: number | null
  initial_request: string
  state: string
  transcript: UssdTurn[]
  started_at: string
  ended_at: string | null
}

export interface UssdTurn {
  dir: string // "out" | "in" | "notification" | "request" | "response"
  ts: string
  text: string
}

/** USSDState — WS ussd.state 的 data.ussd */
export interface USSDState {
  session_id: string
  device_id: string
  state: string
  last_request: string
  last_response: string
  network_request: string
  network_notification: string
}

// ───────────── Signal ─────────────

/** SignalRow — 来自 DB */
export interface SignalRow {
  id: number
  modem_id: number
  sim_id: number | null
  quality_pct: number | null
  rssi_dbm: number | null
  rsrp_dbm: number | null
  rsrq_db: number | null
  snr_db: number | null
  access_tech: string | null
  registration: string | null
  operator_id: string | null
  operator_name: string | null
  sampled_at: string
}

/** SignalSample — WS signal.sample 的 data (来自 provider) */
export interface SignalSample {
  device_id: string
  quality_pct: number
  rssi_dbm: number | null
  rsrp_dbm: number | null
  rsrq_db: number | null
  snr_db: number | null
  access_tech: string
  registration: string
  operator_id: string
  operator_name: string
  sampled_at: string
}

// ───────────── Settings ─────────────

export interface TelegramSettings {
  has_token: boolean
  chat_id: number
  push_sms: boolean
  source: string
}

export interface TelegramPutRequest {
  bot_token?: string
  chat_id?: number
  push_sms?: boolean
}

// ───────────── eSIM ─────────────

/** ESimCard — 来自 GET /api/esim/cards */
export interface ESimCard {
  id: number
  eid: string                        // 32 hex chars
  vendor: string                     // "5ber" | "9esim" | "unknown"
  vendor_display: string             // 例如 "5ber.eSIM"
  nickname: string | null            // 用户自定义备注（存 DB）
  notes: string | null
  euicc_firmware: string | null
  profile_version: string | null
  free_nvm: number | null            // bytes, eUICC 剩余空间
  modem_id: number | null
  modem_device_id: string | null
  modem_model: string | null         // 当前承载 modem 的 model
  transport: string | null           // "qmi" | "mbim" | null
  active_iccid: string | null        // 当前激活 profile 的 ICCID
  active_profile_name: string | null
  last_seen_at: string | null
  created_at: string
}

/** ESimCardDetail — 来自 GET /api/esim/cards/{id} */
export interface ESimCardDetail extends ESimCard {
  profiles: ESimProfile[]
}

/** ESimProfile — 来自 GET /api/esim/cards/{id}/profiles */
export interface ESimProfile {
  id: number
  card_id: number
  iccid: string
  isdp_aid: string
  state: 'enabled' | 'disabled'
  nickname: string | null            // 写入 eUICC 卡片
  service_provider: string | null    // 运营商名
  profile_name: string | null
  profile_class: string | null       // "operational" / "test" / "provisioning"
  last_refreshed_at: string
}

export interface ESimAddProfileRequest {
  activation_code?: string
  smdp_address?: string
  matching_id?: string
  confirmation_code?: string
}

// ───────────── WebSocket ─────────────

/** WS 消息信封 (server → client) */
export interface WsMessage {
  type: string
  data: any
  ts: string
}
