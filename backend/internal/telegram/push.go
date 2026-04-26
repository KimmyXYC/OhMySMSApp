package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// simSnapshot 记录某 deviceID 上次看到的 SIM 关键信息（用于 diff）。
type simSnapshot struct {
	ICCID    string
	Operator string
}

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
		if b.cancelPendingOffline(ev.DeviceID, st) {
			return
		}
		nickname := b.lookupNickname(ev.DeviceID)
		// 初始化 SIM 状态记录，避免下一次 ModemUpdated 误报为"插入"。
		b.recordSIMSnapshot(ev.DeviceID, st)
		_, _ = b.sendPushText(formatModemOnline(st, nickname))

	case modem.EventModemRemoved:
		st, _ := ev.Payload.(modem.ModemState)
		b.scheduleOffline(ev.DeviceID, st)

	case modem.EventModemUpdated:
		st, ok := ev.Payload.(modem.ModemState)
		if !ok {
			return
		}
		if b.hasPendingOffline(ev.DeviceID) {
			b.recordSIMSnapshot(ev.DeviceID, st)
			return
		}
		b.handleSIMDiff(ev.DeviceID, st)

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

func (b *bot) scheduleOffline(deviceID string, st modem.ModemState) {
	b.simStateMu.Lock()
	if old := b.pendingOffline[deviceID]; old != nil {
		old.Stop()
	}
	grace := b.offlineGrace
	if grace <= 0 {
		grace = 20 * time.Second
	}
	timer := time.AfterFunc(grace, func() {
		select {
		case <-b.ctx.Done():
			return
		default:
		}
		b.simStateMu.Lock()
		delete(b.pendingOffline, deviceID)
		b.simStateMu.Unlock()
		nickname := b.lookupNickname(deviceID)
		b.clearSIMSnapshot(deviceID)
		_, _ = b.sendPushText(formatModemOffline(st, nickname))
	})
	b.pendingOffline[deviceID] = timer
	b.simStateMu.Unlock()
}

func (b *bot) cancelPendingOffline(deviceID string, st modem.ModemState) bool {
	b.simStateMu.Lock()
	timer := b.pendingOffline[deviceID]
	if timer != nil {
		timer.Stop()
		delete(b.pendingOffline, deviceID)
	}
	b.simStateMu.Unlock()
	if timer != nil {
		b.recordSIMSnapshot(deviceID, st)
		return true
	}
	return false
}

func (b *bot) hasPendingOffline(deviceID string) bool {
	b.simStateMu.Lock()
	defer b.simStateMu.Unlock()
	return b.pendingOffline[deviceID] != nil
}

// lookupNickname 从 store 查询 modem 的 nickname。无 store / 找不到 / 出错 → 返回 ""。
// 加 1s 超时以避免阻塞 push 链路。
func (b *bot) lookupNickname(deviceID string) string {
	if b == nil || b.store == nil || deviceID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(b.bgCtx(), time.Second)
	defer cancel()
	row, err := b.store.GetModemByDeviceID(ctx, deviceID)
	if err != nil {
		// sql.ErrNoRows 等都是预期可能（modem 还没入库），静默返回 "".
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			b.log.Debug("telegram lookupNickname miss", "device", deviceID, "err", err)
		}
		return ""
	}
	if row == nil || row.Nickname == nil {
		return ""
	}
	return *row.Nickname
}

// currentSIM 返回 (ICCID, OperatorName)；SIM 为 nil 时返回 ("", "")。
func currentSIM(st modem.ModemState) (string, string) {
	if st.SIM == nil {
		return "", ""
	}
	return st.SIM.ICCID, nonEmpty(st.SIM.OperatorName, st.OperatorName)
}

// recordSIMSnapshot 写入 deviceID 当前的 SIM 快照（用于 diff 起点）。
func (b *bot) recordSIMSnapshot(deviceID string, st modem.ModemState) {
	iccid, op := currentSIM(st)
	b.simStateMu.Lock()
	defer b.simStateMu.Unlock()
	if b.lastSIMByDevice == nil {
		b.lastSIMByDevice = make(map[string]simSnapshot)
	}
	b.lastSIMByDevice[deviceID] = simSnapshot{ICCID: iccid, Operator: op}
}

// clearSIMSnapshot 在 ModemRemoved 时清除记录。
func (b *bot) clearSIMSnapshot(deviceID string) {
	b.simStateMu.Lock()
	defer b.simStateMu.Unlock()
	delete(b.lastSIMByDevice, deviceID)
}

// handleSIMDiff 比较 deviceID 当前 SIM 与上次记录，按变化类型推送独立 SIM 通知。
// 若无变化（包括首次见到这个 deviceID）则只更新内部记录、不推送。
//
// ModemAdded 已经初始化过快照，所以这里收到的 prev 都是有定义的；
// 但首次 ModemUpdated 来得早于 ModemAdded（理论上不会发生，但兜底）也安全：
// 视作 prev="" → 与当前对比仍可能触发"插入"，不致 crash。
func (b *bot) handleSIMDiff(deviceID string, st modem.ModemState) {
	curr, currOp := currentSIM(st)

	b.simStateMu.Lock()
	prev, hadPrev := b.lastSIMByDevice[deviceID]
	if b.lastSIMByDevice == nil {
		b.lastSIMByDevice = make(map[string]simSnapshot)
	}
	b.lastSIMByDevice[deviceID] = simSnapshot{ICCID: curr, Operator: currOp}
	b.simStateMu.Unlock()

	// 没有 prev 记录（理论上 ModemAdded 已写过，兜底避免误推）：仅记录，不推送。
	if !hadPrev {
		return
	}
	if prev.ICCID == curr {
		return
	}
	nickname := b.lookupNickname(deviceID)
	switch {
	case prev.ICCID == "" && curr != "":
		_, _ = b.sendPushText(formatSIMInserted(st, nickname))
	case prev.ICCID != "" && curr == "":
		_, _ = b.sendPushText(formatSIMRemoved(st, nickname, prev.ICCID, prev.Operator))
	case prev.ICCID != "" && curr != "" && prev.ICCID != curr:
		_, _ = b.sendPushText(formatSIMReplaced(st, nickname, prev.ICCID))
	}
}

// pushSMSReceived 发送新短信通知，并记录 Telegram message_id → 原短信目标映射。
func (b *bot) pushSMSReceived(deviceID string, rec modem.SMSRecord) {
	pushChatID := b.effectivePushChatID()
	if pushChatID == 0 {
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
	m, err := b.sendMessage(pushChatID, b.pushThread, text, tgbotapi.ModeMarkdownV2, 0, nil)
	if err != nil {
		b.log.Warn("telegram push sms failed", "err", err, "peer", rec.Peer)
		return
	}
	b.recordPushedSMS(pushChatID, m.MessageID, deviceID, rec.Peer)
}

func (b *bot) recordPushedSMS(chatID int64, messageID int, deviceID, peer string) {
	if chatID == 0 || messageID == 0 || deviceID == "" || peer == "" {
		return
	}
	b.pushedSMSMu.Lock()
	defer b.pushedSMSMu.Unlock()
	if b.pushedSMS == nil {
		b.pushedSMS = make(map[pushedSMSKey]pushedSMSTarget)
	}
	now := time.Now()
	for k, v := range b.pushedSMS {
		if now.Sub(v.CreatedAt) > 7*24*time.Hour {
			delete(b.pushedSMS, k)
		}
	}
	b.pushedSMS[pushedSMSKey{ChatID: chatID, MessageID: messageID}] = pushedSMSTarget{
		DeviceID:  deviceID,
		Peer:      peer,
		CreatedAt: now,
	}
}

func (b *bot) lookupPushedSMS(chatID int64, messageID int) (pushedSMSTarget, bool) {
	b.pushedSMSMu.Lock()
	defer b.pushedSMSMu.Unlock()
	target, ok := b.pushedSMS[pushedSMSKey{ChatID: chatID, MessageID: messageID}]
	return target, ok
}

func (b *bot) handleDirectSMSReply(msg *tgbotapi.Message) bool {
	if msg == nil || msg.Chat == nil || msg.ReplyToMessage == nil {
		return false
	}
	if msg.Chat.ID != b.effectivePushChatID() {
		return false
	}
	target, ok := b.lookupPushedSMS(msg.Chat.ID, msg.ReplyToMessage.MessageID)
	if !ok {
		return false
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		_, _ = b.replyPlain(msg, "只支持文本短信回复")
		return true
	}
	b.executeDirectSMSReply(msg, target, text)
	return true
}

func (b *bot) executeDirectSMSReply(msg *tgbotapi.Message, target pushedSMSTarget, text string) {
	ctx, cancel := context.WithTimeout(b.bgCtx(), 30*time.Second)
	defer cancel()
	extID, err := b.provider.SendSMS(ctx, target.DeviceID, target.Peer, text)
	actorChatID := b.chatID
	if msg != nil && msg.Chat != nil {
		actorChatID = msg.Chat.ID
	}
	if err != nil {
		b.logAudit(audit.Entry{
			Actor:  "telegram:" + strconv.FormatInt(actorChatID, 10),
			Action: "sms.send",
			Target: target.DeviceID,
			Payload: map[string]any{
				"peer":     target.Peer,
				"body_len": utf8.RuneCountInString(text),
				"via":      "telegram_direct_reply",
			},
			Result: "error",
			Err:    err.Error(),
		})
		_, _ = b.replyPlain(msg, "发送失败: "+err.Error())
		return
	}
	b.logAudit(audit.Entry{
		Actor:  "telegram:" + strconv.FormatInt(actorChatID, 10),
		Action: "sms.send",
		Target: target.DeviceID,
		Payload: map[string]any{
			"peer":     target.Peer,
			"body_len": utf8.RuneCountInString(text),
			"ext_id":   extID,
			"via":      "telegram_direct_reply",
		},
		Result: "ok",
	})
	_, _ = b.replyPlain(msg, "✅ 已提交发送")
}
