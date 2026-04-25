package esim

import (
	"strings"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// transportInfo 描述 lpac 与 modem 通信使用的字符设备。
type transportInfo struct {
	Kind   string // "qmi" / "mbim"
	Device string // 绝对设备路径，例如 "/dev/cdc-wdm3"
}

// resolveTransport 检查 modem 行的 qmi_port / mbim_port，返回合适的 transport。
// 优先 QMI，因 lpac 的 QMI 驱动更稳定（5ber/9eSIM 均实测通过）。
// 任意 port 字段已经携带前缀 "/dev/" 时不重复添加。
func resolveTransport(m *modem.ModemRow) (transportInfo, bool) {
	if m == nil {
		return transportInfo{}, false
	}
	if p := strDeref(m.QMIPort); p != "" {
		return transportInfo{Kind: "qmi", Device: ensureDevPath(p)}, true
	}
	if p := strDeref(m.MBIMPort); p != "" {
		return transportInfo{Kind: "mbim", Device: ensureDevPath(p)}, true
	}
	return transportInfo{}, false
}

func ensureDevPath(p string) string {
	if strings.HasPrefix(p, "/dev/") || strings.HasPrefix(p, "/") {
		return p
	}
	return "/dev/" + p
}

func strDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
