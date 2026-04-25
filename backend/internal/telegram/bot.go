package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/esim"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// botAPI 是我们用到的 tgbotapi 子集，便于测试打桩。
type botAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	StopReceivingUpdates()
}

// bot 是运行时单例。每次 Reload 会销毁旧实例、创建新实例。
type bot struct {
	api         botAPI
	chatID      int64
	pushSMS     bool
	provider    modem.Provider
	runner      *modem.Runner
	store       *modem.Store
	audit       *audit.Service
	esim        *esim.Service // 可为 nil
	log         *slog.Logger
	sessions    *sessionStore
	rateLimiter *rateLimiter

	// 运行时
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// SIM 物理插拔跟踪：deviceID -> 上次见到的 SIM 快照。
	// 通过比对 EventModemUpdated 携带的 ModemState.SIM 变化推送 SIM 插入/拔出/替换通知；
	// 这是因为 mmprovider 不会单独发 SimRemoved 事件（SIM 被拔出时是把 ModemState.SIM 置空、
	// 然后发 ModemUpdated），所以 bot 自己维护跨事件的 diff 状态。
	simStateMu      sync.Mutex
	lastSIMByDevice map[string]simSnapshot
}

// newBot 创建 bot（含与 Telegram API 的连接）。token 为空返回 error。
func newBot(parent context.Context,
	token string, chatID int64, pushSMS bool,
	provider modem.Provider, runner *modem.Runner, store *modem.Store,
	auditSvc *audit.Service, esimSvc *esim.Service, log *slog.Logger,
) (*bot, error) {
	if token == "" {
		return nil, fmt.Errorf("empty token")
	}
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("connect telegram: %w", err)
	}
	return newBotWithAPI(parent, api, chatID, pushSMS, provider, runner, store, auditSvc, esimSvc, log), nil
}

// newBotWithAPI 允许测试注入 fake api。
func newBotWithAPI(parent context.Context,
	api botAPI, chatID int64, pushSMS bool,
	provider modem.Provider, runner *modem.Runner, store *modem.Store,
	auditSvc *audit.Service, esimSvc *esim.Service, log *slog.Logger,
) *bot {
	ctx, cancel := context.WithCancel(parent)
	return &bot{
		api:             api,
		chatID:          chatID,
		pushSMS:         pushSMS,
		provider:        provider,
		runner:          runner,
		store:           store,
		audit:           auditSvc,
		esim:            esimSvc,
		log:             log,
		sessions:        newSessionStore(5 * time.Minute),
		rateLimiter:     newRateLimiter(100 * time.Millisecond),
		ctx:             ctx,
		cancel:          cancel,
		lastSIMByDevice: make(map[string]simSnapshot),
	}
}

// auditActor 返回 "telegram:<chatID>" 形式的 actor 字符串。
func (b *bot) auditActor() string {
	return "telegram:" + strconv.FormatInt(b.chatID, 10)
}

// logAudit 写一条 telegram 侧的审计日志。audit 为 nil 时静默。
func (b *bot) logAudit(e audit.Entry) {
	if b.audit == nil {
		return
	}
	if e.Actor == "" {
		e.Actor = b.auditActor()
	}
	b.audit.Log(b.bgCtx(), e)
}

// run 启动命令循环与推送订阅，阻塞直到 ctx 取消或致命错误。
func (b *bot) run() {
	// 推送订阅
	if b.runner != nil {
		events, unsub := b.runner.Subscribe(128)
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			defer unsub()
			b.pushLoop(events)
		}()
	}

	// 命令 long polling
	uc := tgbotapi.NewUpdate(0)
	uc.Timeout = 30
	updates := b.api.GetUpdatesChan(uc)

	// session TTL purge ticker
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-b.ctx.Done():
				return
			case <-t.C:
				b.sessions.Purge()
			}
		}
	}()

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-b.ctx.Done():
				return
			case upd, ok := <-updates:
				if !ok {
					return
				}
				b.handleUpdate(upd)
			}
		}
	}()
}

// stop 优雅停止：取消 ctx，停 long polling，等 goroutine 退出。
func (b *bot) stop() {
	b.cancel()
	b.api.StopReceivingUpdates()
	// 等 goroutine 退出；加一个超时兜底。
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		b.log.Warn("telegram bot stop timeout")
	}
}

// handleUpdate 分发一条 update。
func (b *bot) handleUpdate(upd tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("telegram handler panic", "panic", r)
		}
	}()

	// Callback（inline keyboard 点击）
	if upd.CallbackQuery != nil {
		b.handleCallback(upd.CallbackQuery)
		return
	}

	if upd.Message == nil {
		return
	}
	msg := upd.Message

	// 鉴权：只处理绑定的 chat_id
	if b.chatID != 0 && msg.Chat != nil && msg.Chat.ID != b.chatID {
		b.log.Warn("telegram message from unauthorized chat",
			"chat_id", msg.Chat.ID, "text", truncate(msg.Text, 40))
		return
	}

	// 命令？
	if msg.IsCommand() {
		cmd := msg.Command()
		args := strings.TrimSpace(msg.CommandArguments())
		b.log.Info("tg command", "chat_id", msg.Chat.ID, "cmd", cmd, "args", truncate(args, 40))
		b.dispatchCommand(msg, cmd, args)
		return
	}

	// 非命令 — 尝试作为当前 session 的输入
	if msg.Text != "" {
		b.handleSessionInput(msg)
	}
}

// sendText 发送一条 MarkdownV2 文本。失败只 log。
func (b *bot) sendText(text string) (tgbotapi.Message, error) {
	if b.chatID == 0 {
		return tgbotapi.Message{}, fmt.Errorf("no chat_id bound")
	}
	b.rateLimiter.wait(b.ctx, b.chatID)
	msg := tgbotapi.NewMessage(b.chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdownV2
	m, err := b.api.Send(msg)
	if err != nil {
		b.log.Warn("telegram send failed", "err", err)
		// 退化尝试一次：去掉 ParseMode（防止 escape 出 bug 时完全失联）
		msg2 := tgbotapi.NewMessage(b.chatID, stripMarkdownV2(text))
		if m2, err2 := b.api.Send(msg2); err2 == nil {
			return m2, nil
		}
	}
	return m, err
}

// sendWithMarkup 发送带 InlineKeyboard 的消息。
func (b *bot) sendWithMarkup(text string, markup any) (tgbotapi.Message, error) {
	if b.chatID == 0 {
		return tgbotapi.Message{}, fmt.Errorf("no chat_id bound")
	}
	b.rateLimiter.wait(b.ctx, b.chatID)
	msg := tgbotapi.NewMessage(b.chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdownV2
	if markup != nil {
		msg.ReplyMarkup = markup
	}
	return b.api.Send(msg)
}

// stripMarkdownV2 极简退化：去掉反斜杠 escape 字符。仅做为失败回退。
func stripMarkdownV2(s string) string {
	return strings.ReplaceAll(s, "\\", "")
}

// rateLimiter 简单按 chatID 间隔 100ms 限速。
type rateLimiter struct {
	mu   sync.Mutex
	last map[int64]time.Time
	gap  time.Duration
}

func newRateLimiter(gap time.Duration) *rateLimiter {
	return &rateLimiter{last: make(map[int64]time.Time), gap: gap}
}

// wait 阻塞直到距上次对该 chatID 发送已过 gap，或 ctx 取消。
func (r *rateLimiter) wait(ctx context.Context, chatID int64) {
	r.mu.Lock()
	last := r.last[chatID]
	now := time.Now()
	dur := r.gap - now.Sub(last)
	if dur <= 0 {
		r.last[chatID] = now
		r.mu.Unlock()
		return
	}
	r.last[chatID] = now.Add(dur) // 预订这次发送时点
	r.mu.Unlock()
	t := time.NewTimer(dur)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
