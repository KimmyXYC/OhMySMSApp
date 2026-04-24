package telegram

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// pushLoop 消费 runner 事件并推送通知。
func (b *bot) pushLoop(events <-chan modem.Event) {
	for {
		select {
		case <-b.ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			b.dispatchEvent(ev)
		}
	}
}

func (b *bot) dispatchEvent(ev modem.Event) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("telegram push panic", "panic", r)
		}
	}()

	switch ev.Kind {
	case modem.EventSMSReceived:
		if !b.pushSMS {
			return
		}
		rec, ok := ev.Payload.(modem.SMSRecord)
		if !ok {
			return
		}
		b.pushSMSReceived(ev.DeviceID, rec)

	case modem.EventModemAdded:
		st, ok := ev.Payload.(modem.ModemState)
		if !ok {
			return
		}
		label := nonEmpty(st.Model, st.DeviceID)
		_, _ = b.sendText("🟢 " + escapeMarkdownV2("Modem 上线: "+label))

	case modem.EventModemRemoved:
		st, _ := ev.Payload.(modem.ModemState)
		label := nonEmpty(st.Model, st.DeviceID)
		_, _ = b.sendText("🔴 " + escapeMarkdownV2("Modem 离线: "+label))

	case modem.EventUSSDStateChanged:
		u, ok := ev.Payload.(modem.USSDState)
		if !ok {
			return
		}
		if u.State != "user_response" {
			return
		}
		// 仅当 bot 有对应的进行中会话时推提示。
		sess := b.sessions.Get(b.chatID)
		if sess == nil || sess.Kind != SessionUSSD || sess.DeviceID != ev.DeviceID {
			return
		}
		b.sessions.Update(b.chatID, func(s *Session) {
			s.USSDSessionID = u.SessionID
			s.Step = StepUSSDAwaitResponse
		})
		prompt := u.NetworkRequest
		if prompt == "" {
			prompt = "请输入 USSD 响应"
		}
		msg := tgbotapi.NewMessage(b.chatID, escapeMarkdownV2(prompt))
		msg.ParseMode = tgbotapi.ModeMarkdownV2
		msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true}
		b.rateLimiter.wait(b.ctx, b.chatID)
		_, _ = b.api.Send(msg)
	}
}

// pushSMSReceived 发送新短信通知 + "回复" 按钮。
func (b *bot) pushSMSReceived(deviceID string, rec modem.SMSRecord) {
	if b.chatID == 0 {
		return
	}
	// 构造标签
	modemLabel := ""
	simLabel := ""
	modemIndex := -1
	modems := b.provider.ListModems()
	for i, m := range modems {
		if m.DeviceID == deviceID {
			modemLabel = nonEmpty(m.Model, m.DeviceID)
			modemIndex = i
			if m.SIM != nil {
				simLabel = nonEmpty(m.SIM.OperatorName, m.SIM.ICCID)
			}
			break
		}
	}

	text := formatSMSNotification(rec, modemLabel, simLabel, deviceID, modemIndex)

	// 回复按钮 callback data。注意 callback data 最长 64 字节，peer 一般不会超。
	data := "replyto:" + deviceID + ":" + rec.Peer
	if len(data) > 60 {
		// 截断兜底：过长就只带 deviceID，reply 流程会要求用户重新输号码
		data = "replyto:" + deviceID + ":"
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ 回复", data),
		),
	)
	if _, err := b.sendWithMarkup(text, markup); err != nil {
		b.log.Warn("telegram push sms failed", "err", err, "peer", rec.Peer)
	}
}
