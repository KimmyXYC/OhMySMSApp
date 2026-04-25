// Package esim 提供 sticker eUICC 的发现与 profile 切换能力。
//
// 实现方式：通过外部进程 `lpac` 走 SGP.22 ES10x 接口与 eUICC 通信。
// lpac 的 transport 复用 modem 暴露的 cdc-wdm 字符设备（QMI 优先，MBIM 其次）。
// 操作期间必须用 ModemManager.InhibitDevice 暂停 MM 对 modem 的占用，
// 否则 lpac 与 MM 会争夺 APDU 通道。
//
// 设计要点：
//   - 单 Service 模式（不走 modem.Provider 抽象）。
//   - 所有写操作（enable/disable/nickname）都要先 inhibit MM，操作完 uninhibit。
//   - 数据库就是事实来源（card + profile 表），lpac 输出做幂等 upsert。
//   - 自动发现：订阅 modem.Runner 事件，对首次 registered 且有 SIM 的 modem
//     在 cooldown 内只跑一次 chip info，避免对模块产生不必要的干扰。
package esim

import (
	"errors"
	"time"
)

// 公共错误。HTTP 层据此映射 status code。
var (
	// ErrLPACUnavailable lpac 二进制找不到 / exec 失败 → 503。
	ErrLPACUnavailable = errors.New("lpac binary unavailable")
	// ErrTransportUnsupported modem 没有 QMI/MBIM 端口 → 400。
	ErrTransportUnsupported = errors.New("modem has no qmi or mbim transport")
	// ErrInhibitFailed 调用 ModemManager InhibitDevice 失败 → 500。
	ErrInhibitFailed = errors.New("modem manager inhibit failed")
	// ErrLPACError lpac 返回非 0 code → 500，详情见 LPACError.Detail。
	ErrLPACError = errors.New("lpac command failed")
	// ErrNoChangeNeeded profile 已经是请求的状态 → 409。
	ErrNoChangeNeeded = errors.New("profile already in requested state")
	// ErrCardNotFound → 404。
	ErrCardNotFound = errors.New("esim card not found")
	// ErrProfileNotFound → 404。
	ErrProfileNotFound = errors.New("esim profile not found")
	// ErrModemNotBound 卡未绑定到任何 modem → 409。
	ErrModemNotBound = errors.New("esim card is not bound to any modem")
	// ErrModemOffline modem 不在线 → 409。
	ErrModemOffline = errors.New("modem currently offline")
)

// LPACError 携带 lpac 退出原因，方便上层日志/审计。
type LPACError struct {
	ExitCode int
	Detail   string
	Stderr   string
}

func (e *LPACError) Error() string {
	if e.Stderr != "" {
		// 截断 stderr 避免日志过长；保留前 300 字符通常够定位
		s := e.Stderr
		if len(s) > 300 {
			s = s[:300] + "..."
		}
		return "lpac: " + e.Detail + " | stderr=" + s
	}
	return "lpac: " + e.Detail
}
func (e *LPACError) Unwrap() error { return ErrLPACError }

// ESimCard 是 esim_cards 表的对外结构。
type ESimCard struct {
	ID             int64   `json:"id"`
	EID            string  `json:"eid"`
	Vendor         string  `json:"vendor"` // 5ber / 9esim / unknown
	VendorDisplay  string  `json:"vendor_display"`
	Nickname       *string `json:"nickname"`
	Notes          *string `json:"notes"`
	EUICCFirmware  *string `json:"euicc_firmware"`
	ProfileVersion *string `json:"profile_version"`
	FreeNVM        *int64  `json:"free_nvm"`
	ModemID        *int64  `json:"modem_id"`
	ModemDeviceID  *string `json:"modem_device_id"` // 便于前端不再二次查询
	ModemModel     *string `json:"modem_model"`
	Transport      *string `json:"transport"`           // qmi / mbim
	ActiveICCID    *string `json:"active_iccid"`        // 当前 enabled profile 的 ICCID
	ActiveName     *string `json:"active_profile_name"` // 当前 enabled profile 的展示名
	LastSeenAt     *string `json:"last_seen_at"`
	CreatedAt      string  `json:"created_at"`
}

// ESimProfile 是 esim_profiles 表的对外结构。
type ESimProfile struct {
	ID              int64   `json:"id"`
	CardID          int64   `json:"card_id"`
	ICCID           string  `json:"iccid"`
	ISDPAid         *string `json:"isdp_aid"`
	State           string  `json:"state"` // enabled / disabled
	Nickname        *string `json:"nickname"`
	ServiceProvider *string `json:"service_provider"`
	ProfileName     *string `json:"profile_name"`
	ProfileClass    *string `json:"profile_class"`
	LastRefreshedAt *string `json:"last_refreshed_at"`
}

// ESimCardDetail 携带 profiles 列表，用于详情页。
type ESimCardDetail struct {
	ESimCard
	Profiles []ESimProfile `json:"profiles"`
}

// chipInfo 对应 lpac chip info 的 data。仅解析我们关心的字段，其余忽略。
// 注：lpac 的 chip info 输出 schema 在不同版本间变化较多。
// 为减少耦合，我们只直接读 EID 字段，其它（profileVersion / freeNVM）
// 走宽松解析（见 parser.go）。
type chipInfo struct {
	EID string `json:"eidValue"`
}

// 辅助：profile state 常量。
const (
	ProfileStateEnabled  = "enabled"
	ProfileStateDisabled = "disabled"
)

// 辅助：后台自动发现冷却最小值（防止配置失误）。
const minDiscoverCooldown = 5 * time.Minute