package esim

import "strings"

// vendorFromEID 根据 EID 前缀识别厂商。
//
// EID 是 32 位 EUM ID + EIN（详见 SGP.02 §2.1.6 / SGP.22）。
// 已知前缀（来自 6A 现场实测 + 公开 SGP.02 EUM 列表）：
//   - 89086030...  → 5ber.eSIM（EUM = 5ber 自有 IIN）
//   - 35840574...  → 9eSIM（注：该前缀首位 3 不符合 EID 规范的 89 起始，
//                            实测 lpac 输出原样为此值，按厂商样本兜底识别）
//   - 89044045...  → 9eSIM 备用前缀
//
// 未匹配到返回 "unknown"。
func vendorFromEID(eid string) string {
	e := strings.ToUpper(strings.TrimSpace(eid))
	switch {
	case strings.HasPrefix(e, "89086030"):
		return "5ber"
	case strings.HasPrefix(e, "35840574"), strings.HasPrefix(e, "89044045"):
		return "9esim"
	default:
		return "unknown"
	}
}

// vendorDisplay 给前端用的展示名。
func vendorDisplay(vendor string) string {
	switch vendor {
	case "5ber":
		return "5ber.eSIM"
	case "9esim":
		return "9eSIM"
	default:
		return "Unknown eUICC"
	}
}
