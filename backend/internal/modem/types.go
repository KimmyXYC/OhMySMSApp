// Package modem 提供 ModemManager DBus 集成层。
//
// 设计要点：
//   - Provider 抽象：MM 实现 + Mock 实现，便于开发机本地运行与单测
//   - DeviceIdentifier 为 modem 稳定 key（DBus path 易变）
//   - 内存状态快照 + DB 持久化双写：内存负责快速读取，DB 负责历史/审计
//   - 事件通过单一 channel 扇出给下游（WS hub / Telegram / DB 写入 Runner）
package modem

import (
	"errors"
	"time"
)

var (
	ErrModemInUse = errors.New("modem is online and cannot be deleted")
	ErrSIMInUse   = errors.New("sim is currently in use and cannot be deleted")
)

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
	Name string `json:"name"` // 例如 "cdc-wdm0"、"ttyUSB2"
	Type string `json:"type"` // "net" / "at" / "qmi" / "mbim" / "gps" / "ignored" / "unknown" ...
}

// ModemState 是一个 modem 的完整内存快照。来源于 MM 的多个接口合并：
// Modem / Modem3gpp / Messaging / Signal 等。
type ModemState struct {
	DeviceID string `json:"device_id"` // Modem.DeviceIdentifier (主键)
	DBusPath string `json:"dbus_path"` // 当前 DBus path（易变）

	Manufacturer     string `json:"manufacturer"`
	Model            string `json:"model"`
	Revision         string `json:"revision"`
	HardwareRevision string `json:"hardware_revision"`
	Plugin           string `json:"plugin"`
	IMEI             string `json:"imei"`
	PrimaryPort      string `json:"primary_port"`
	Ports            []Port `json:"ports"`
	USBPath          string `json:"usb_path"` // Physdev，sysfs 路径

	State        ModemStateEnum `json:"state"`
	FailedReason string         `json:"failed_reason"` // sim-missing / sim-error / ...
	PowerState   string         `json:"power_state"`   // off / low / on / unknown

	AccessTech    []string `json:"access_tech"`    // 解码后的技术列表，例如 ["lte"]
	SignalQuality int      `json:"signal_quality"` // 0-100
	SignalRecent  bool     `json:"signal_recent"`

	Registration string   `json:"registration"` // home / roaming / searching / denied / unknown / ...
	OperatorID   string   `json:"operator_id"`  // MCCMNC
	OperatorName string   `json:"operator_name"`
	OwnNumbers   []string `json:"own_numbers"`

	HasSim bool      `json:"has_sim"`
	SIM    *SimState `json:"sim,omitempty"` // 当前插入的 SIM

	// 能力探测：基于 GetManagedObjects 返回的接口列表。
	HasUSSD      bool `json:"has_ussd"`
	HasSignal    bool `json:"has_signal"`
	HasMessaging bool `json:"has_messaging"`

	SupportedStorages []string `json:"supported_storages"` // 例如 ["sm","me"]
}

// SimState 对应 MM Sim 接口。
type SimState struct {
	DBusPath         string   `json:"dbus_path"`
	ICCID            string   `json:"iccid"`
	IMSI             string   `json:"imsi"`
	EID              string   `json:"eid"`
	MSISDN           string   `json:"msisdn"` // 手机号；MM Sim 接口无此字段，由 modem 层 OwnNumbers 合并得到
	OperatorID       string   `json:"operator_id"`
	OperatorName     string   `json:"operator_name"`
	Active           bool     `json:"active"`
	EmergencyNumbers []string `json:"emergency_numbers"`
	SimType          string   `json:"sim_type"` // physical / esim / unknown
}

// SMSRecord 对应 MM Sms 对象的一条短信。
type SMSRecord struct {
	ExtID         string    `json:"ext_id"`    // DBus object path，作为外部唯一 id
	Direction     string    `json:"direction"` // inbound / outbound
	State         string    `json:"state"`     // unknown / stored / receiving / received / sending / sent
	Peer          string    `json:"peer"`
	Text          string    `json:"text"`
	SMSC          string    `json:"smsc"`
	Timestamp     time.Time `json:"timestamp"` // 来自 SMS Center 的时间
	Storage       string    `json:"storage"`
	DeliveryState uint32    `json:"delivery_state"`
}

// USSDState 表示一次 USSD 会话的当前状态。
type USSDState struct {
	SessionID           string `json:"session_id"` // 约定 = DeviceID，MM 侧无显式 session id
	DeviceID            string `json:"device_id"`
	State               string `json:"state"` // idle / active / user_response / unknown
	LastRequest         string `json:"last_request"`
	LastResponse        string `json:"last_response"`
	NetworkRequest      string `json:"network_request"`
	NetworkNotification string `json:"network_notification"`
}

// SignalSample 是一次 signal 采样（用于阶段 7 图表）。
type SignalSample struct {
	DeviceID     string    `json:"device_id"`
	QualityPct   int       `json:"quality_pct"`
	RSSIdBm      *int      `json:"rssi_dbm"`
	RSRPdBm      *int      `json:"rsrp_dbm"`
	RSRQdB       *int      `json:"rsrq_db"`
	SNRdB        *float64  `json:"snr_db"`
	AccessTech   string    `json:"access_tech"`
	Registration string    `json:"registration"`
	OperatorID   string    `json:"operator_id"`
	OperatorName string    `json:"operator_name"`
	SampledAt    time.Time `json:"sampled_at"`
}
