package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// ErrBotNotConfigured 表示尚未配置 Telegram Bot（token 为空或未启动）。
// HTTP 层用来返回 412 Precondition Failed。
var ErrBotNotConfigured = errors.New("telegram bot not configured")

// Controller 管理 Bot 的生命周期，支持配置热更新。
// 线程安全：Start / Reload / Stop 可并发调用（内部加锁串行化）。
type Controller struct {
	provider modem.Provider
	runner   *modem.Runner
	store    *modem.Store
	log      *slog.Logger

	parent context.Context

	mu      sync.Mutex
	current *bot
}

// NewController 构造 controller。provider/runner/store 不可为 nil。
func NewController(provider modem.Provider, runner *modem.Runner, store *modem.Store, log *slog.Logger) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{
		provider: provider,
		runner:   runner,
		store:    store,
		log:      log,
	}
}

// Start 启动 Bot（如果有 token），订阅 runner 事件。
// token 为空则不启动，返回 nil。
// parent ctx 用作所有 bot goroutine 的根 ctx。
func (c *Controller) Start(parent context.Context, cfg config.TelegramConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.parent = parent
	return c.startLocked(cfg)
}

// Reload 用新配置重启。token 变更 / chat_id 变更 / pushSMS 变更都会触发。
// 若 parent ctx 尚未被 Start 设置过，会使用 context.Background()。
func (c *Controller) Reload(_ context.Context, cfg config.TelegramConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 停止旧实例
	if c.current != nil {
		c.current.stop()
		c.current = nil
	}
	parent := c.parent
	if parent == nil {
		parent = context.Background()
	}
	c.parent = parent
	return c.startLocked(cfg)
}

// Stop 停止 Bot 并清理订阅。可安全多次调用。
func (c *Controller) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != nil {
		c.current.stop()
		c.current = nil
	}
	return nil
}

// startLocked 必须在持锁下调用。
func (c *Controller) startLocked(cfg config.TelegramConfig) error {
	if cfg.BotToken == "" {
		c.log.Info("telegram bot disabled: no token")
		return nil
	}
	b, err := newBot(c.parent, cfg.BotToken, cfg.ChatID, cfg.PushSMS,
		c.provider, c.runner, c.store, c.log)
	if err != nil {
		c.log.Warn("telegram bot start failed", "err", err)
		return err
	}
	b.run()
	c.current = b
	c.log.Info("telegram bot started", "chat_id", cfg.ChatID, "push_sms", cfg.PushSMS)
	return nil
}

// Running 返回当前是否有活跃 bot（供 /healthz / 测试查询）。
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current != nil
}

// TestPush 向当前绑定的 chat_id 发送一条测试消息。
// 若 Bot 未启用（无 token 或 chat_id=0），返回 ErrBotNotConfigured。
// 文本会被包裹为：📡 测试消息：<text>（来自 ohmysmsd）。
// text 可为空——那样就只发前缀提示。
func (c *Controller) TestPush(ctx context.Context, text string) error {
	c.mu.Lock()
	b := c.current
	c.mu.Unlock()
	if b == nil {
		return ErrBotNotConfigured
	}
	if b.chatID == 0 {
		return ErrBotNotConfigured
	}
	text = strings.TrimSpace(text)
	body := "📡 测试消息：" + text + "（来自 ohmysmsd）"
	// 使用纯文本（不走 MarkdownV2）避免用户输入触发 escape 失败。
	msg := tgbotapi.NewMessage(b.chatID, body)
	b.rateLimiter.wait(ctx, b.chatID)
	if _, err := b.api.Send(msg); err != nil {
		return err
	}
	return nil
}
