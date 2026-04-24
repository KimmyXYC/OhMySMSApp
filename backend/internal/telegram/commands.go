package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// dispatchCommand 命令路由。
func (b *bot) dispatchCommand(msg *tgbotapi.Message, cmd, args string) {
	switch cmd {
	case "start":
		b.cmdStart()
	case "help":
		_, _ = b.sendText(formatHelp())
	case "status":
		b.cmdStatus()
	case "sims":
		b.cmdSims()
	case "signal":
		b.cmdSignal()
	case "recent":
		b.cmdRecent(args)
	case "send":
		b.cmdSend()
	case "ussd":
		b.cmdUSSD()
	case "cancel":
		b.cmdCancel()
	default:
		_, _ = b.sendText(escapeMarkdownV2("未知命令: /" + cmd))
	}
}

// /start
func (b *bot) cmdStart() {
	modems := b.provider.ListModems()
	sims, _ := b.store.ListSIMs(b.bgCtx())
	_, _ = b.sendText(formatStart(b.chatID, len(modems), len(sims)))
}

// /status
func (b *bot) cmdStatus() {
	modems := b.provider.ListModems()
	_, _ = b.sendText(formatModemOverview(modems))
}

// /signal
func (b *bot) cmdSignal() {
	modems := b.provider.ListModems()
	_, _ = b.sendText(formatSignalOverview(modems))
}

// /sims
func (b *bot) cmdSims() {
	ctx := b.bgCtx()
	sims, err := b.store.ListSIMs(ctx)
	if err != nil {
		_, _ = b.sendText(escapeMarkdownV2("读取 SIM 失败: " + err.Error()))
		return
	}
	modems, err := b.store.ListModems(ctx)
	if err != nil {
		_, _ = b.sendText(escapeMarkdownV2("读取 modem 失败: " + err.Error()))
		return
	}
	modemByID := make(map[int64]modem.ModemRow, len(modems))
	bindings := make(map[int64]int64, len(modems))
	for _, m := range modems {
		modemByID[m.ID] = m
		if m.SIM != nil {
			bindings[m.ID] = m.SIM.ID
		}
	}
	_, _ = b.sendText(formatSIMsOverview(sims, modemByID, bindings))
}

// /recent [N]
func (b *bot) cmdRecent(args string) {
	n := 10
	if args != "" {
		if v, err := strconv.Atoi(args); err == nil && v > 0 {
			n = v
		}
	}
	if n > 50 {
		n = 50
	}
	rows, _, err := b.store.ListSMS(b.bgCtx(), modem.SMSFilter{Limit: n})
	if err != nil {
		_, _ = b.sendText(escapeMarkdownV2("查询失败: " + err.Error()))
		return
	}
	_, _ = b.sendText(formatRecentSMS(rows))
}

// /cancel
func (b *bot) cmdCancel() {
	sess := b.sessions.Get(b.chatID)
	if sess == nil {
		_, _ = b.sendText(escapeMarkdownV2("没有进行中的会话"))
		return
	}
	b.sessions.Delete(b.chatID)
	_, _ = b.sendText(escapeMarkdownV2("已取消"))
}

// /send —— 进入向导：选 modem → 号码 → 文本 → 确认 → 发送
func (b *bot) cmdSend() {
	modems := b.provider.ListModems()
	if len(modems) == 0 {
		_, _ = b.sendText(escapeMarkdownV2("当前没有可用 modem"))
		return
	}
	b.sessions.Set(b.chatID, &Session{
		Kind: SessionSendSMS,
		Step: StepAwaitModem,
	})
	b.promptPickModem(modems, "send")
}

// /ussd
func (b *bot) cmdUSSD() {
	modems := b.provider.ListModems()
	withUSSD := make([]modem.ModemState, 0, len(modems))
	for _, m := range modems {
		if m.HasUSSD {
			withUSSD = append(withUSSD, m)
		}
	}
	if len(withUSSD) == 0 {
		_, _ = b.sendText(escapeMarkdownV2("当前没有支持 USSD 的 modem"))
		return
	}
	b.sessions.Set(b.chatID, &Session{
		Kind: SessionUSSD,
		Step: StepAwaitModem,
	})
	b.promptPickModem(withUSSD, "ussd")
}

// promptPickModem 发送 inline keyboard 让用户选 modem。
// prefix="send" | "ussd" | "reply"
func (b *bot) promptPickModem(modems []modem.ModemState, prefix string) {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(modems))
	for i, m := range modems {
		label := nonEmpty(m.Model, m.DeviceID)
		if m.SIM != nil && m.SIM.OperatorName != "" {
			label += " / " + m.SIM.OperatorName
		}
		data := prefix + ":modem:" + m.DeviceID
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("[#%d] %s", i, label), data),
		)
		rows = append(rows, row)
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	title := "请选择 Modem\\:"
	_, _ = b.sendWithMarkup(title, markup)
}

// handleCallback 处理 inline keyboard 点击。
func (b *bot) handleCallback(cb *tgbotapi.CallbackQuery) {
	if b.chatID != 0 && cb.Message != nil && cb.Message.Chat != nil && cb.Message.Chat.ID != b.chatID {
		return
	}
	// 先 ack 回调（否则客户端转圈）
	_, _ = b.api.Request(tgbotapi.NewCallback(cb.ID, ""))

	parts := strings.Split(cb.Data, ":")
	if len(parts) < 2 {
		return
	}
	prefix := parts[0]

	switch prefix {
	case "send", "ussd", "reply":
		if len(parts) >= 3 && parts[1] == "modem" {
			b.onPickModem(prefix, parts[2])
		} else if len(parts) >= 2 && parts[1] == "confirm" {
			b.onConfirmSend()
		} else if len(parts) >= 2 && parts[1] == "abort" {
			b.cmdCancel()
		}
	case "replyto":
		// 从短信推送按钮进来：replyto:<deviceID>:<peer>
		if len(parts) >= 3 {
			b.onReplyTo(parts[1], strings.Join(parts[2:], ":"))
		}
	}
}

// onPickModem 用户在 send/ussd 向导第一步选了 modem。
func (b *bot) onPickModem(prefix, deviceID string) {
	sess := b.sessions.Get(b.chatID)
	if sess == nil {
		_, _ = b.sendText(escapeMarkdownV2("会话已过期，请重新开始"))
		return
	}
	b.sessions.Update(b.chatID, func(s *Session) {
		s.DeviceID = deviceID
	})
	switch prefix {
	case "send":
		b.sessions.Update(b.chatID, func(s *Session) { s.Step = StepAwaitPeer })
		// ForceReply 让用户方便直接回复
		msg := tgbotapi.NewMessage(b.chatID, escapeMarkdownV2("请输入对端号码（例如 +1234567890）："))
		msg.ParseMode = tgbotapi.ModeMarkdownV2
		msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: false}
		b.rateLimiter.wait(b.ctx, b.chatID)
		_, _ = b.api.Send(msg)
	case "ussd":
		b.sessions.Update(b.chatID, func(s *Session) { s.Step = StepAwaitText })
		msg := tgbotapi.NewMessage(b.chatID, escapeMarkdownV2("请输入 USSD 指令（例如 *100#）："))
		msg.ParseMode = tgbotapi.ModeMarkdownV2
		msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true}
		b.rateLimiter.wait(b.ctx, b.chatID)
		_, _ = b.api.Send(msg)
	}
}

// onReplyTo 从短信通知按钮进入 reply 流程。
func (b *bot) onReplyTo(deviceID, peer string) {
	b.sessions.Set(b.chatID, &Session{
		Kind:     SessionReply,
		Step:     StepAwaitText,
		DeviceID: deviceID,
		Peer:     peer,
	})
	msg := tgbotapi.NewMessage(b.chatID,
		escapeMarkdownV2(fmt.Sprintf("回复 %s，请输入文本：", peer)))
	msg.ParseMode = tgbotapi.ModeMarkdownV2
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true}
	b.rateLimiter.wait(b.ctx, b.chatID)
	_, _ = b.api.Send(msg)
}

// onConfirmSend 确认按钮。
func (b *bot) onConfirmSend() {
	sess := b.sessions.Get(b.chatID)
	if sess == nil {
		_, _ = b.sendText(escapeMarkdownV2("会话已过期"))
		return
	}
	switch sess.Kind {
	case SessionSendSMS, SessionReply:
		b.executeSend(sess)
	case SessionUSSD:
		b.executeUSSD(sess)
	}
}

// handleSessionInput 非命令文本作为 session 的下一步输入。
func (b *bot) handleSessionInput(msg *tgbotapi.Message) {
	sess := b.sessions.Get(b.chatID)
	if sess == nil {
		return
	}
	txt := strings.TrimSpace(msg.Text)
	switch sess.Step {
	case StepAwaitPeer:
		b.sessions.Update(b.chatID, func(s *Session) {
			s.Peer = txt
			s.Step = StepAwaitText
		})
		m := tgbotapi.NewMessage(b.chatID, escapeMarkdownV2("请输入短信正文："))
		m.ParseMode = tgbotapi.ModeMarkdownV2
		m.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true}
		b.rateLimiter.wait(b.ctx, b.chatID)
		_, _ = b.api.Send(m)

	case StepAwaitText:
		b.sessions.Update(b.chatID, func(s *Session) {
			s.Text = txt
			s.Step = StepConfirm
		})
		if sess.Kind == SessionUSSD {
			// USSD 没 confirm 步骤，直接执行
			b.sessions.Update(b.chatID, func(s *Session) { s.Step = StepConfirm })
			// 读回最新 sess
			cur := b.sessions.Get(b.chatID)
			if cur != nil {
				b.executeUSSD(cur)
			}
			return
		}
		// send/reply 显示摘要 + 确认按钮
		b.promptConfirmSend()

	case StepUSSDAwaitResponse:
		// USSD user_response 态下：把文本发给 provider
		sid := sess.USSDSessionID
		reply, err := b.provider.RespondUSSD(b.bgCtx(), sid, txt)
		if err != nil {
			_, _ = b.sendText(escapeMarkdownV2("USSD 响应失败: " + err.Error()))
			b.sessions.Delete(b.chatID)
			return
		}
		b.sendUSSDReply(reply)
	}
}

// promptConfirmSend 发送确认摘要。
func (b *bot) promptConfirmSend() {
	sess := b.sessions.Get(b.chatID)
	if sess == nil {
		return
	}
	var summary strings.Builder
	summary.WriteString("*准备发送短信*\n")
	summary.WriteString(escapeMarkdownV2("Modem: " + sess.DeviceID + "\n"))
	summary.WriteString(escapeMarkdownV2("To:    " + sess.Peer + "\n"))
	summary.WriteString("\n```\n")
	summary.WriteString(escapeCode(sess.Text))
	summary.WriteString("\n```")

	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ 发送", "send:confirm"),
			tgbotapi.NewInlineKeyboardButtonData("❌ 取消", "send:abort"),
		),
	)
	_, _ = b.sendWithMarkup(summary.String(), markup)
}

// executeSend 调用 provider.SendSMS。
func (b *bot) executeSend(sess *Session) {
	ctx, cancel := context.WithTimeout(b.bgCtx(), 30*time.Second)
	defer cancel()
	_, err := b.provider.SendSMS(ctx, sess.DeviceID, sess.Peer, sess.Text)
	if err != nil {
		_, _ = b.sendText(escapeMarkdownV2("发送失败: " + err.Error()))
		b.sessions.Delete(b.chatID)
		return
	}
	_, _ = b.sendText(escapeMarkdownV2("✅ 已提交发送"))
	b.sessions.Delete(b.chatID)
}

// executeUSSD 调用 provider.InitiateUSSD。
func (b *bot) executeUSSD(sess *Session) {
	ctx, cancel := context.WithTimeout(b.bgCtx(), 30*time.Second)
	defer cancel()
	sid, reply, err := b.provider.InitiateUSSD(ctx, sess.DeviceID, sess.Text)
	if err != nil {
		_, _ = b.sendText(escapeMarkdownV2("USSD 失败: " + err.Error()))
		b.sessions.Delete(b.chatID)
		return
	}
	b.sessions.Update(b.chatID, func(s *Session) {
		s.USSDSessionID = sid
	})
	b.sendUSSDReply(reply)
}

// sendUSSDReply 发送 USSD 回复；若设备随后进入 user_response，我们靠 push 的 UssdStateChanged 触发 ForceReply（见 push.go）。
func (b *bot) sendUSSDReply(reply string) {
	var body strings.Builder
	body.WriteString("📟 *USSD 回复*\n\n```\n")
	body.WriteString(escapeCode(reply))
	body.WriteString("\n```")
	_, _ = b.sendText(body.String())
}

// bgCtx 返回一个和 bot 生命周期绑定的后台 ctx。
func (b *bot) bgCtx() context.Context {
	if b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}
