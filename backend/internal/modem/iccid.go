package modem

import "strings"

// NormalizeICCID 把一条 ICCID 规范化为"无 padding"的标准形式。
//
// 背景：GSM SIM 的 ICCID 在 EF_ICCID 中以 nibble swap 存储，长度 10 字节 = 20 nibble。
// 真实 ICCID 标准长度 19 位（含 Luhn 校验），剩余 1 位以填充字符 'F' 表示。
// ModemManager 通过 AT+CRSM 直接读出 nibble，因此其 SimIdentifier 经常包含
// 末尾的 'F' padding（见现场观察：`894921007608614852F`、20 位 `...FF` 也可能）。
// 而 lpac 输出的 ICCID（来自 eUICC ISD-R 的 ProfileInfo）通常已经去掉了 padding，
// 只有 19 位（或更少，若 ICCID 本身就是 18 位的）。
//
// 为了让 `sims.iccid`（来自 MM）与 `esim_profiles.iccid`（来自 lpac）能正确关联，
// 全链路统一在入库前调用 NormalizeICCID。规则：
//   - 去掉首尾空白
//   - 仅保留十六进制字符不做改写（不修正大小写中的真数字）
//   - 末尾若是连续的 'F'/'f' padding 字符就剥掉。这里"padding"判定保守一些：
//     只在长度 > 18 时剥末尾的 F；这样 18 位"恰好末位是 F"的怪异 ICCID 不会被误伤
//     （ICCID 末位是 Luhn 校验码 0–9，理论上不会出现 F）。
//
// 不做：Luhn 校验、长度补齐、字符集校验。失败时原样返回。
func NormalizeICCID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// 仅当长度大于 18 时剥末尾 F/f padding。
	for len(s) > 18 {
		last := s[len(s)-1]
		if last == 'F' || last == 'f' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}
