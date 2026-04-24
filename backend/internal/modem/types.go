// Package modem 提供 ModemManager DBus 集成层。
//
// 设计要点：
//   - Provider 抽象：MM 实现 + Mock 实现，便于开发机本地运行与单测
//   - DeviceIdentifier 为 modem 稳定 key（DBus path 易变）
//   - 内存状态快照 + DB 持久化双写：内存负责快速读取，DB 负责历史/审计
//   - 事件通过单一 channel 扇出给下游（WS hub / Telegram / DB 写入 Runner）
package modem

import "time"

// EventKind 事件类型枚举。
type EventKind string

const (
	EventModemAdded       EventKind = "modem_added"
	EventModemRemoved     EventKind = "modem_removed"
	EventModemUpdated     EventKind = "modem_updated"
	EventSimUpdated       EventKind = "sim_updated"
	EventSignalSampled    EventKind = "signal_sampled"
	EventSMSReceived      EventKind = "sms_received"
	EventSMSStateChanged  EventKind = "sms_state_changed"
	EventUSSDStateChanged EventKind = "ussd_state_changed"
)

// Event 是 Provider → 下游的统一事件载荷。
//
// Payload 的具体类型取决于 Kind：
//   - ModemAdded/ModemRemoved/ModemUpdated  → ModemState
//   - SimUpdated                            → SimState
//   - SignalSampled                         → SignalSample
//   - SMSReceived/SMSStateChanged           → SMSRecord
//   - UssdStateChanged                      → USSDState
type Event struct {
	Kind     EventKind
	DeviceID string
	Payload  any
	At       time.Time
}

// ModemStateEnum 对应 MMModemState（字符串化后的值）。
// 原始值是 int32，FAILED=-1，详见 docs/modemmanager-dbus-ref.md §C。
type ModemStateEnum string

const (
	ModemStateFailed        ModemStateEnum = "failed"
	ModemStateUnknown       ModemStateEnum = "unknown"
	ModemStateInitializing  ModemStateEnum = "initializing"
	ModemStateLocked        ModemStateEnum = "locked"
	ModemStateDisabled      ModemStateEnum = "disabled"
	ModemStateDisabling     ModemStateEnum = "disabling"
	ModemStateEnabling      ModemStateEnum = "enabling"
	ModemStateEnabled       ModemStateEnum = "enabled"
	ModemStateSearching     ModemStateEnum = "searching"
	ModemStateRegistered    ModemStateEnum = "registered"
	ModemStateDisconnecting ModemStateEnum = "disconnecting"
	ModemStateConnecting    ModemStateEnum = "connecting"
	ModemStateConnected     ModemStateEnum = "connected"
)

// Port 表示 modem 暴露的一个端口。Type 是字符串化的 MMModemPortType。
type Port struct {
	Name string // 例如 "cdc-wdm0"、"ttyUSB2"
	Type string // "net" / "at" / "qmi" / "mbim" / "gps" / "ignored" / "unknown" ...
}

// ModemState 是一个 modem 的完整内存快照。来源于 MM 的多个接口合并：
// Modem / Modem3gpp / Messaging / Signal 等。
type ModemState struct {
	DeviceID string // Modem.DeviceIdentifier (主键)
	DBusPath string // 当前 DBus path（易变）

	Manufacturer     string
	Model            string
	Revision         string
	HardwareRevision string
	Plugin           string
	IMEI             string
	PrimaryPort      string
	Ports            []Port
	USBPath          string // Physdev，sysfs 路径

	State        ModemStateEnum
	FailedReason string // sim-missing / sim-error / ...
	PowerState   string // off / low / on / unknown

	AccessTech    []string // 解码后的技术列表，例如 ["lte"]
	SignalQuality int      // 0-100
	SignalRecent  bool

	Registration string // home / roaming / searching / denied / unknown / ...
	OperatorID   string // MCCMNC
	OperatorName string
	OwnNumbers   []string

	HasSim bool
	SIM    *SimState // 当前插入的 SIM

	// 能力探测：基于 GetManagedObjects 返回的接口列表。
	HasUSSD      bool
	HasSignal    bool
	HasMessaging bool

	SupportedStorages []string // 例如 ["sm","me"]
}

// SimState 对应 MM Sim 接口。
type SimState struct {
	DBusPath         string
	ICCID            string
	IMSI             string
	EID              string
	OperatorID       string
	OperatorName     string
	Active           bool
	EmergencyNumbers []string
	SimType          string // physical / esim / unknown
}

// SMSRecord 对应 MM Sms 对象的一条短信。
type SMSRecord struct {
	ExtID         string // DBus object path，作为外部唯一 id
	Direction     string // inbound / outbound
	State         string // unknown / stored / receiving / received / sending / sent
	Peer          string
	Text          string
	SMSC          string
	Timestamp     time.Time // 来自 SMS Center 的时间
	Storage       string
	DeliveryState uint32
}

// USSDState 表示一次 USSD 会话的当前状态。
type USSDState struct {
	SessionID           string // 约定 = DeviceID，MM 侧无显式 session id
	DeviceID            string
	State               string // idle / active / user_response / unknown
	LastRequest         string
	LastResponse        string
	NetworkRequest      string
	NetworkNotification string
}

// SignalSample 是一次 signal 采样（用于阶段 7 图表）。
type SignalSample struct {
	DeviceID     string
	QualityPct   int
	RSSIdBm      *int
	RSRPdBm      *int
	RSRQdB       *int
	SNRdB        *float64
	AccessTech   string
	Registration string
	OperatorID   string
	OperatorName string
	SampledAt    time.Time
}
