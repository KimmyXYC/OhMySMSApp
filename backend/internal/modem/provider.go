package modem

import (
	"context"
	"errors"
)

// ErrModemResetUnsupported 表示该 modem 的插件不支持 Reset 操作
// （典型例子：Huawei MBIM 等固件 DBus 会返回 "Unsupported"）。
// HTTP 层以此判定返回 501 Not Implemented。
var ErrModemResetUnsupported = errors.New("modem reset not supported by this modem")

// Provider 抽象 ModemManager 的能力，允许在开发/测试环境替换为 Mock。
type Provider interface {
	// Start 连接后端（DBus / 模拟），执行初始 reconcile，
	// 启动信号订阅，并阻塞运行直到 ctx 取消。
	Start(ctx context.Context) error

	// Events 返回只读事件通道；生产者为 Provider，消费者为 Runner。
	// Provider 被 Start 之前调用 Events 允许（用于 Runner 提前 subscribe）。
	Events() <-chan Event

	// ListModems 返回所有当前已知 modem 的快照（拷贝），顺序不保证。
	ListModems() []ModemState

	// GetModem 通过 DeviceID 查找 modem 快照。
	GetModem(deviceID string) (ModemState, bool)

	// ListSMS 列出某 modem 当前可见（DBus 上存在的）所有 SMS。
	ListSMS(deviceID string) ([]SMSRecord, error)

	// SendSMS 发送短信，返回 DBus object path 作为 ext_id。
	SendSMS(ctx context.Context, deviceID, to, text string) (extID string, err error)

	// DeleteSMS 从 modem 存储中删除一条 SMS。
	DeleteSMS(ctx context.Context, deviceID, extID string) error

	// InitiateUSSD 发起 USSD 会话。Initiate 本身阻塞，返回值为首个网络回复。
	InitiateUSSD(ctx context.Context, deviceID, command string) (sessionID string, reply string, err error)

	// RespondUSSD 在 USER_RESPONSE 状态下发送用户响应。
	RespondUSSD(ctx context.Context, sessionID, response string) (reply string, err error)

	// CancelUSSD 终止 USSD 会话。
	CancelUSSD(ctx context.Context, sessionID string) error

	// ResetModem 调用 MM Modem.Reset() 让 modem 软复位。
	// MM Reset 是异步的：调用会立即返回，随后 modem 在 DBus 上会消失并重新出现。
	// 若底层插件不支持（例如部分 Huawei MBIM 固件），返回 ErrModemResetUnsupported。
	ResetModem(ctx context.Context, deviceID string) error
}
