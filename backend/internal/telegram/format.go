// Package telegram 提供 Telegram Bot 子系统。
//
// 包结构：
//   - controller.go — 对外生命周期入口（main.go 与 httpapi 调用）
//   - bot.go        — Bot 实例（封装 API、update 分发）
//   - commands.go   — 命令处理
//   - push.go       — runner 事件订阅与推送
//   - session.go    — 多轮交互会话
//   - format.go     — MarkdownV2 工具与消息模板
package telegram

import (
	"fmt"
	"strings"
	"time"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// escapeMarkdownV2 根据 Telegram Bot API 规则转义 MarkdownV2 特殊字符。
// 文档：https://core.telegram.org/bots/api#markdownv2-style
// 需要转义：_ * [ ] ( ) ~ ` > # + - = | { } . !
func escapeMarkdownV2(s string) string {
	specials := "_*[]()~`>#+-=|{}.!\\"
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		if strings.ContainsRune(specials, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeCode 对 MarkdownV2 代码块/代码片段内容转义：仅需转义 ` 与 \。
func escapeCode(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}

// formatSMSNotification 生成新短信推送消息（MarkdownV2）。
// modemLabel 形如 "EC20F"；simLabel 形如 "giffgaff" 或 ICCID 前后截断。
func formatSMSNotification(rec modem.SMSRecord, modemLabel, simLabel, deviceID string, modemIndex int) string {
	var b strings.Builder
	b.WriteString("📩 *新短信*\n")
	b.WriteString("来自: `")
	b.WriteString(escapeCode(rec.Peer))
	b.WriteString("`\n")
	if simLabel != "" {
		b.WriteString("SIM: ")
		b.WriteString(escapeMarkdownV2(simLabel))
		b.WriteString("\n")
	}
	if modemLabel != "" || deviceID != "" {
		b.WriteString("模块: ")
		if modemLabel != "" {
			b.WriteString(escapeMarkdownV2(modemLabel))
		}
		if modemIndex >= 0 {
			b.WriteString(escapeMarkdownV2(fmt.Sprintf(" [#%d]", modemIndex)))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	// 正文用 code block 保留原样（但仍要转义 ` 和 \）
	b.WriteString("```\n")
	b.WriteString(escapeCode(rec.Text))
	b.WriteString("\n```")
	return b.String()
}

// lastN 返回字符串末尾 n 个字符。不足 n 直接返回原串。
// 这里以 rune 计数，避免中间截断多字节字符；ICCID/IMEI 一般是 ASCII，但 ICCID 末位可能含
// X/F 等，所以用 rune 安全。
func lastN(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// modemDisplayName 选择 modem 的显示名：优先 nickname，其次 model，最后 deviceID。
func modemDisplayName(nickname, model, deviceID string) string {
	if nickname != "" {
		return nickname
	}
	if model != "" {
		return model
	}
	return deviceID
}

// formatModemOnline 生成 modem 上线推送消息（MarkdownV2）。
//
//	🟢 *模块上线*
//	<nickname or model>
//	IMEI: …<last4>
//	SIM: <msisdn> (<operator>)        // 仅当有 SIM
//	信号: 📶 <pct>% <tech>             // 仅当有信号
func formatModemOnline(st modem.ModemState, nickname string) string {
	var b strings.Builder
	b.WriteString("🟢 *模块上线*\n")
	name := modemDisplayName(nickname, st.Model, st.DeviceID)
	b.WriteString(escapeMarkdownV2(name))
	b.WriteString("\n")
	if st.IMEI != "" {
		b.WriteString(escapeMarkdownV2("IMEI: …" + lastN(st.IMEI, 4)))
		b.WriteString("\n")
	}
	if st.SIM != nil {
		simLine := "SIM: "
		num := ""
		if st.SIM.MSISDN != "" {
			num = st.SIM.MSISDN
		} else if len(st.OwnNumbers) > 0 {
			num = st.OwnNumbers[0]
		}
		op := nonEmpty(st.SIM.OperatorName, st.OperatorName)
		switch {
		case num != "" && op != "":
			simLine += num + " (" + op + ")"
		case num != "":
			simLine += num
		case op != "":
			simLine += "…" + lastN(st.SIM.ICCID, 4) + " (" + op + ")"
		default:
			simLine += "…" + lastN(st.SIM.ICCID, 4)
		}
		b.WriteString(escapeMarkdownV2(simLine))
		b.WriteString("\n")
	}
	if st.SignalRecent {
		tech := ""
		if len(st.AccessTech) > 0 {
			tech = " " + strings.ToUpper(strings.Join(st.AccessTech, ","))
		}
		b.WriteString(escapeMarkdownV2(fmt.Sprintf("信号: 📶 %d%%%s", st.SignalQuality, tech)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatModemOffline 生成 modem 离线推送消息。
//
//	🔴 *模块离线*
//	<name>
//	IMEI: …<last4>
func formatModemOffline(st modem.ModemState, nickname string) string {
	var b strings.Builder
	b.WriteString("🔴 *模块离线*\n")
	name := modemDisplayName(nickname, st.Model, st.DeviceID)
	b.WriteString(escapeMarkdownV2(name))
	b.WriteString("\n")
	if st.IMEI != "" {
		b.WriteString(escapeMarkdownV2("IMEI: …" + lastN(st.IMEI, 4)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatSIMInserted 生成 SIM 插入推送消息。
//
//	💳 *SIM 插入*
//	<modem name>
//	ICCID: …<last5>
//	号码: <msisdn>          // 可选
//	运营商: <name>          // 可选
func formatSIMInserted(st modem.ModemState, nickname string) string {
	var b strings.Builder
	b.WriteString("💳 *SIM 插入*\n")
	name := modemDisplayName(nickname, st.Model, st.DeviceID)
	b.WriteString(escapeMarkdownV2(name))
	b.WriteString("\n")
	if st.SIM != nil {
		if st.SIM.ICCID != "" {
			b.WriteString(escapeMarkdownV2("ICCID: …" + lastN(st.SIM.ICCID, 5)))
			b.WriteString("\n")
		}
		num := st.SIM.MSISDN
		if num == "" && len(st.OwnNumbers) > 0 {
			num = st.OwnNumbers[0]
		}
		if num != "" {
			b.WriteString(escapeMarkdownV2("号码: " + num))
			b.WriteString("\n")
		}
		op := nonEmpty(st.SIM.OperatorName, st.OperatorName)
		if op != "" {
			b.WriteString(escapeMarkdownV2("运营商: " + op))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatSIMRemoved 生成 SIM 拔出推送消息。
// 当前 ModemState 已经没有 SIM，所以前一次的 ICCID/operator 由 caller 传入。
//
//	🚫 *SIM 拔出*
//	<modem name>
//	(原 SIM: …<last5> · <operator>)
func formatSIMRemoved(st modem.ModemState, nickname, prevICCID, prevOperator string) string {
	var b strings.Builder
	b.WriteString("🚫 *SIM 拔出*\n")
	name := modemDisplayName(nickname, st.Model, st.DeviceID)
	b.WriteString(escapeMarkdownV2(name))
	b.WriteString("\n")
	if prevICCID != "" || prevOperator != "" {
		parts := []string{}
		if prevICCID != "" {
			parts = append(parts, "…"+lastN(prevICCID, 5))
		}
		if prevOperator != "" {
			parts = append(parts, prevOperator)
		}
		b.WriteString(escapeMarkdownV2("(原 SIM: " + strings.Join(parts, " · ") + ")"))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatSIMReplaced 生成 SIM 替换推送消息。
//
//	🔄 *SIM 替换*
//	<modem name>
//	原: …<prev5> → 新: …<new5>
//	运营商: <new operator>     // 可选
func formatSIMReplaced(st modem.ModemState, nickname, prevICCID string) string {
	var b strings.Builder
	b.WriteString("🔄 *SIM 替换*\n")
	name := modemDisplayName(nickname, st.Model, st.DeviceID)
	b.WriteString(escapeMarkdownV2(name))
	b.WriteString("\n")
	newICCID := ""
	if st.SIM != nil {
		newICCID = st.SIM.ICCID
	}
	b.WriteString(escapeMarkdownV2(fmt.Sprintf("原: …%s → 新: …%s",
		lastN(prevICCID, 5), lastN(newICCID, 5))))
	b.WriteString("\n")
	if st.SIM != nil {
		op := nonEmpty(st.SIM.OperatorName, st.OperatorName)
		if op != "" {
			b.WriteString(escapeMarkdownV2("运营商: " + op))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatModemOverview 生成 /status 概览消息。
func formatModemOverview(modems []modem.ModemState) string {
	if len(modems) == 0 {
		return "当前没有检测到任何 modem\\."
	}
	var b strings.Builder
	b.WriteString("*Modem 列表*\n")
	for i, m := range modems {
		b.WriteString(escapeMarkdownV2(fmt.Sprintf("\n[#%d] ", i)))
		b.WriteString("*")
		b.WriteString(escapeMarkdownV2(nonEmpty(m.Model, "unknown")))
		b.WriteString("*\n")
		b.WriteString(escapeMarkdownV2("state: " + string(m.State)))
		b.WriteString("\n")
		if m.IMEI != "" {
			b.WriteString(escapeMarkdownV2("imei:  " + m.IMEI))
			b.WriteString("\n")
		}
		if m.OperatorName != "" || m.OperatorID != "" {
			b.WriteString(escapeMarkdownV2(fmt.Sprintf("op:    %s (%s)", m.OperatorName, m.OperatorID)))
			b.WriteString("\n")
		}
		if m.SignalRecent {
			b.WriteString(escapeMarkdownV2(fmt.Sprintf("signal: %d%%", m.SignalQuality)))
			b.WriteString("\n")
		}
		if m.SIM != nil {
			simLabel := nonEmpty(m.SIM.OperatorName, m.SIM.ICCID)
			b.WriteString(escapeMarkdownV2("sim:   " + simLabel))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// formatSignalOverview 只列出 modem 的信号强度。
func formatSignalOverview(modems []modem.ModemState) string {
	if len(modems) == 0 {
		return "当前没有 modem\\."
	}
	var b strings.Builder
	b.WriteString("*信号强度*\n")
	for i, m := range modems {
		label := nonEmpty(m.Model, m.DeviceID)
		if !m.SignalRecent {
			b.WriteString(escapeMarkdownV2(fmt.Sprintf("\n[#%d] %s: n/a", i, label)))
			continue
		}
		at := "n/a"
		if len(m.AccessTech) > 0 {
			at = strings.Join(m.AccessTech, ",")
		}
		b.WriteString(escapeMarkdownV2(fmt.Sprintf("\n[#%d] %s: %d%% (%s)", i, label, m.SignalQuality, at)))
	}
	return b.String()
}

// formatSIMsOverview /sims。
func formatSIMsOverview(sims []modem.SimRow, modemsByID map[int64]modem.ModemRow, bindings map[int64]int64) string {
	if len(sims) == 0 {
		return "当前没有 SIM\\."
	}
	var b strings.Builder
	b.WriteString("*SIM 列表*\n")
	for i, s := range sims {
		b.WriteString(escapeMarkdownV2(fmt.Sprintf("\n[#%d] ", i)))
		b.WriteString("*")
		b.WriteString(escapeMarkdownV2(s.ICCID))
		b.WriteString("*\n")
		if s.IMSI != nil && *s.IMSI != "" {
			b.WriteString(escapeMarkdownV2("imsi: " + *s.IMSI))
			b.WriteString("\n")
		}
		if s.OperatorName != nil && *s.OperatorName != "" {
			b.WriteString(escapeMarkdownV2("op:   " + *s.OperatorName))
			b.WriteString("\n")
		}
		// 绑定哪个 modem
		for mid, sid := range bindings {
			if sid == s.ID {
				if m, ok := modemsByID[mid]; ok {
					lab := ""
					if m.Model != nil {
						lab = *m.Model
					} else {
						lab = m.DeviceID
					}
					b.WriteString(escapeMarkdownV2("bind: " + lab))
					b.WriteString("\n")
				}
			}
		}
	}
	return b.String()
}

// formatRecentSMS /recent 格式化。
func formatRecentSMS(rows []modem.SMSRow) string {
	if len(rows) == 0 {
		return "最近没有短信\\."
	}
	var b strings.Builder
	b.WriteString("*最近短信*\n")
	for _, r := range rows {
		dir := "→"
		if r.Direction == "inbound" {
			dir = "←"
		}
		ts := r.TsCreated
		if r.TsReceived != nil && *r.TsReceived != "" {
			ts = *r.TsReceived
		}
		tsShort := shortTime(ts)
		b.WriteString("\n")
		b.WriteString(escapeMarkdownV2(fmt.Sprintf("%s  %s  %s", tsShort, dir, r.Peer)))
		b.WriteString("\n`")
		b.WriteString(escapeCode(truncate(r.Body, 200)))
		b.WriteString("`\n")
	}
	return b.String()
}

// formatStart /start 欢迎。
func formatStart(chatID int64, modemCnt, simCnt int) string {
	var b strings.Builder
	b.WriteString("👋 *ohmysmsapp bot*\n\n")
	b.WriteString(escapeMarkdownV2(fmt.Sprintf("已绑定 chat_id: %d\n", chatID)))
	b.WriteString(escapeMarkdownV2(fmt.Sprintf("Modem: %d 个\n", modemCnt)))
	b.WriteString(escapeMarkdownV2(fmt.Sprintf("SIM:   %d 张\n", simCnt)))
	b.WriteString("\n使用 /help 查看命令\\.")
	return b.String()
}

// formatHelp /help。
func formatHelp() string {
	lines := []string{
		"*可用命令*",
		"",
		"/status — 模块概览",
		"/sims — SIM 列表",
		"/signal — 信号强度",
		"/recent [N] — 最近 N 条短信（默认 10）",
		"/send — 发送短信（向导）",
		"/ussd — 发起 USSD（向导）",
		"/esim — 列出 eSIM 卡",
		"/cancel — 取消当前向导",
		"/help — 本帮助",
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		if strings.HasPrefix(l, "*") {
			b.WriteString(l) // 已经是 Markdown
			continue
		}
		b.WriteString(escapeMarkdownV2(l))
	}
	return b.String()
}

// ---------------- helpers ----------------

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// shortTime 尝试解析 RFC3339 并只保留到分钟。解析失败就返回原字符串。
func shortTime(ts string) string {
	if ts == "" {
		return "-"
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Local().Format("01-02 15:04")
	}
	// SQLite "YYYY-MM-DD HH:MM:SS"
	if t, err := time.Parse("2006-01-02 15:04:05", ts); err == nil {
		return t.Local().Format("01-02 15:04")
	}
	return ts
}
