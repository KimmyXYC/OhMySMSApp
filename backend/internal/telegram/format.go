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
