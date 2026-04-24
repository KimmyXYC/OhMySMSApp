package modem

import (
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// 本文件集中 MM 枚举 → 字符串的映射以及 DBus Variant 解包辅助。
// 枚举值严格遵循 docs/modemmanager-dbus-ref.md §C。

// 常用 DBus 服务/接口常量。
const (
	mmService = "org.freedesktop.ModemManager1"
	mmRoot    = "/org/freedesktop/ModemManager1"

	ifaceObjectManager = "org.freedesktop.DBus.ObjectManager"
	ifaceDBusProps     = "org.freedesktop.DBus.Properties"

	ifaceModem      = "org.freedesktop.ModemManager1.Modem"
	ifaceModem3gpp  = "org.freedesktop.ModemManager1.Modem.Modem3gpp"
	ifaceUSSD       = "org.freedesktop.ModemManager1.Modem.Modem3gpp.Ussd"
	ifaceMessaging  = "org.freedesktop.ModemManager1.Modem.Messaging"
	ifaceSignal     = "org.freedesktop.ModemManager1.Modem.Signal"
	ifaceSim        = "org.freedesktop.ModemManager1.Sim"
	ifaceSms        = "org.freedesktop.ModemManager1.Sms"
)

// decodeModemState 把 MMModemState (int32) 转为字符串。
func decodeModemState(v int32) ModemStateEnum {
	switch v {
	case -1:
		return ModemStateFailed
	case 0:
		return ModemStateUnknown
	case 1:
		return ModemStateInitializing
	case 2:
		return ModemStateLocked
	case 3:
		return ModemStateDisabled
	case 4:
		return ModemStateDisabling
	case 5:
		return ModemStateEnabling
	case 6:
		return ModemStateEnabled
	case 7:
		return ModemStateSearching
	case 8:
		return ModemStateRegistered
	case 9:
		return ModemStateDisconnecting
	case 10:
		return ModemStateConnecting
	case 11:
		return ModemStateConnected
	default:
		return ModemStateUnknown
	}
}

// decodeFailedReason 对应 MMModemStateFailedReason。
func decodeFailedReason(v uint32) string {
	switch v {
	case 0:
		return ""
	case 1:
		return "unknown"
	case 2:
		return "sim-missing"
	case 3:
		return "sim-error"
	case 4:
		return "unknown-capabilities"
	case 5:
		return "esim-without-profiles"
	default:
		return fmt.Sprintf("reason-%d", v)
	}
}

// decodePowerState 对应 MMModemPowerState。
func decodePowerState(v uint32) string {
	switch v {
	case 1:
		return "off"
	case 2:
		return "low"
	case 3:
		return "on"
	default:
		return "unknown"
	}
}

// decodeRegistrationState 对应 MMModem3gppRegistrationState。
func decodeRegistrationState(v uint32) string {
	switch v {
	case 0:
		return "idle"
	case 1:
		return "home"
	case 2:
		return "searching"
	case 3:
		return "denied"
	case 4:
		return "unknown"
	case 5:
		return "roaming"
	case 6:
		return "home-sms-only"
	case 7:
		return "roaming-sms-only"
	case 8:
		return "emergency-only"
	case 9:
		return "home-csfb-not-preferred"
	case 10:
		return "roaming-csfb-not-preferred"
	case 11:
		return "attached-rlos"
	default:
		return "unknown"
	}
}

// decodeSmsState 对应 MMSmsState。
func decodeSmsState(v uint32) string {
	switch v {
	case 1:
		return "stored"
	case 2:
		return "receiving"
	case 3:
		return "received"
	case 4:
		return "sending"
	case 5:
		return "sent"
	default:
		return "unknown"
	}
}

// decodeSmsStorage 对应 MMSmsStorage。
func decodeSmsStorage(v uint32) string {
	switch v {
	case 1:
		return "sm"
	case 2:
		return "me"
	case 3:
		return "mt"
	case 4:
		return "sr"
	case 5:
		return "bm"
	case 6:
		return "ta"
	default:
		return "unknown"
	}
}

// decodeUSSDState 对应 MMModem3gppUssdSessionState。
func decodeUSSDState(v uint32) string {
	switch v {
	case 1:
		return "idle"
	case 2:
		return "active"
	case 3:
		return "user_response"
	default:
		return "unknown"
	}
}

// decodePortType 对应 MMModemPortType。
func decodePortType(v uint32) string {
	switch v {
	case 2:
		return "net"
	case 3:
		return "at"
	case 4:
		return "qcdm"
	case 5:
		return "gps"
	case 6:
		return "qmi"
	case 7:
		return "mbim"
	case 8:
		return "audio"
	case 9:
		return "ignored"
	case 10:
		return "xmmrpc"
	default:
		return "unknown"
	}
}

// decodeSimType 对应 MMSimType (since 1.20)。
func decodeSimType(v uint32) string {
	switch v {
	case 1:
		return "physical"
	case 2:
		return "esim"
	default:
		return "unknown"
	}
}

// decodeAccessTechnologies 解 AccessTechnologies bitmask（MMModemAccessTechnology）。
// 返回按权重高 → 低排序的字符串列表；最常见的放前面便于 UI 取首个。
func decodeAccessTechnologies(mask uint32) []string {
	if mask == 0 {
		return nil
	}
	// 表项按"最新/最高"优先排列，这样最可靠的技术位于返回数组前面。
	entries := []struct {
		bit  uint32
		name string
	}{
		{0x8000, "5gnr"},
		{0x20000, "lte-nb-iot"},
		{0x10000, "lte-cat-m"},
		{0x4000, "lte"},
		{0x1000, "hsdpa"},
		{0x2000, "hsupa"},
		{0x0800, "hspa"},
		{0x0020, "umts"},
		{0x0010, "edge"},
		{0x0040, "hsdpa"},
		{0x0002, "gsm"},
		{0x0001, "pots"},
	}
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, e := range entries {
		if mask&e.bit != 0 && !seen[e.name] {
			out = append(out, e.name)
			seen[e.name] = true
		}
	}
	return out
}

// getString 从 properties 字典取字符串；key 不存在或类型不符时返回空串。
func getString(props map[string]dbus.Variant, key string) string {
	v, ok := props[key]
	if !ok {
		return ""
	}
	if s, ok := v.Value().(string); ok {
		return s
	}
	return ""
}

// getObjectPath 取 object path；空或类型不符返回空 path。
func getObjectPath(props map[string]dbus.Variant, key string) dbus.ObjectPath {
	v, ok := props[key]
	if !ok {
		return ""
	}
	if p, ok := v.Value().(dbus.ObjectPath); ok {
		return p
	}
	return ""
}

// getUint32 取 uint32，缺失返回 0。
func getUint32(props map[string]dbus.Variant, key string) uint32 {
	v, ok := props[key]
	if !ok {
		return 0
	}
	if u, ok := v.Value().(uint32); ok {
		return u
	}
	return 0
}

// getInt32 取 int32（State 必须用它，因为 FAILED=-1）。
func getInt32(props map[string]dbus.Variant, key string) int32 {
	v, ok := props[key]
	if !ok {
		return 0
	}
	switch x := v.Value().(type) {
	case int32:
		return x
	case int:
		return int32(x)
	}
	return 0
}

// getBool 取 bool，缺失/类型不符返回 false。
func getBool(props map[string]dbus.Variant, key string) bool {
	v, ok := props[key]
	if !ok {
		return false
	}
	if b, ok := v.Value().(bool); ok {
		return b
	}
	return false
}

// getStringSlice 取 as 类型；缺失返回 nil。
func getStringSlice(props map[string]dbus.Variant, key string) []string {
	v, ok := props[key]
	if !ok {
		return nil
	}
	if ss, ok := v.Value().([]string); ok {
		// 拷贝一份，避免共享 DBus 内部 slice
		out := make([]string, len(ss))
		copy(out, ss)
		return out
	}
	return nil
}

// getSignalQuality 解 (ub) struct，返回 (pct, recent)。
func getSignalQuality(props map[string]dbus.Variant) (int, bool) {
	v, ok := props["SignalQuality"]
	if !ok {
		return 0, false
	}
	s, ok := v.Value().([]interface{})
	if !ok || len(s) != 2 {
		return 0, false
	}
	pct, _ := s[0].(uint32)
	recent, _ := s[1].(bool)
	return int(pct), recent
}

// getPorts 解 a(su)。DBus 将其表达为 [][]interface{}。
func getPorts(props map[string]dbus.Variant) []Port {
	v, ok := props["Ports"]
	if !ok {
		return nil
	}
	raw, ok := v.Value().([][]interface{})
	if !ok {
		return nil
	}
	out := make([]Port, 0, len(raw))
	for _, item := range raw {
		if len(item) < 2 {
			continue
		}
		name, _ := item[0].(string)
		typ, _ := item[1].(uint32)
		out = append(out, Port{Name: name, Type: decodePortType(typ)})
	}
	return out
}

// getSupportedStorages 解 SupportedStorages (au)。
func getSupportedStorages(props map[string]dbus.Variant) []string {
	v, ok := props["SupportedStorages"]
	if !ok {
		return nil
	}
	raw, ok := v.Value().([]uint32)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, u := range raw {
		out = append(out, decodeSmsStorage(u))
	}
	return out
}

// signalDictFloat 从 signal 接口的 a{sv} dict 中取 double 值。不存在返回 nil 指针。
func signalDictFloat(v dbus.Variant, key string) *float64 {
	if v.Signature().String() == "" {
		return nil
	}
	dict, ok := v.Value().(map[string]dbus.Variant)
	if !ok {
		return nil
	}
	entry, ok := dict[key]
	if !ok {
		return nil
	}
	if f, ok := entry.Value().(float64); ok {
		return &f
	}
	return nil
}

// normalizeMSISDN 把 MM Modem.OwnNumbers 返回的号码规整成 E.164 风格：
//   - 前后空格裁掉
//   - 全是数字且长度 >= 5（粗略判断）时自动补 "+" 前缀（MM 在部分模块上会丢掉前缀）
//   - 已经带 "+" 或含其他字符（如 "*100#" 短码）则原样返回
func normalizeMSISDN(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s[0] == '+' {
		return s
	}
	allDigits := true
	for _, r := range s {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits && len(s) >= 5 {
		return "+" + s
	}
	return s
}
