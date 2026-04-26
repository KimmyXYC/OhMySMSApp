// settings.go —— /api/settings/telegram 路由。
//
// 存储策略：
//   - 运行时的 telegram 配置优先以 settings 表中 key="telegram" 为准；
//     没有则回退到 config.yaml 的值（只读）。
//   - GET 响应 *绝不* 回显 bot_token。只回 has_token/chat_id/push_chat_id/push_message_thread_id/push_sms。
//   - PUT 可以更新任一字段；bot_token="" 视为不修改（除非带 clear:true）。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/telegram"
)

type telegramDTO struct {
	HasToken            bool   `json:"has_token"`
	ChatID              int64  `json:"chat_id"`
	PushChatID          int64  `json:"push_chat_id"`
	PushMessageThreadID int    `json:"push_message_thread_id"`
	PushSMS             bool   `json:"push_sms"`
	Source              string `json:"source"` // "config" | "settings"
}

type telegramPut struct {
	BotToken            *string `json:"bot_token,omitempty"`
	ChatID              *int64  `json:"chat_id,omitempty"`
	PushChatID          *int64  `json:"push_chat_id,omitempty"`
	PushMessageThreadID *int    `json:"push_message_thread_id,omitempty"`
	PushSMS             *bool   `json:"push_sms,omitempty"`
	Clear               bool    `json:"clear,omitempty"` // 为 true 时清空 bot_token
}

const telegramSettingsKey = "telegram"

func registerSettings(r chi.Router, deps Deps) {
	r.Get("/settings/telegram", func(w http.ResponseWriter, req *http.Request) {
		cur, source := loadTelegram(req.Context(), deps)
		writeJSON(w, http.StatusOK, telegramDTO{
			HasToken:            cur.BotToken != "",
			ChatID:              cur.ChatID,
			PushChatID:          cur.PushChatID,
			PushMessageThreadID: cur.PushMessageThreadID,
			PushSMS:             cur.PushSMS,
			Source:              source,
		})
	})

	r.Put("/settings/telegram", func(w http.ResponseWriter, req *http.Request) {
		var body telegramPut
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		cur, _ := loadTelegram(req.Context(), deps)
		if body.BotToken != nil && *body.BotToken != "" {
			cur.BotToken = *body.BotToken
		}
		if body.Clear {
			cur.BotToken = ""
		}
		if body.ChatID != nil {
			cur.ChatID = *body.ChatID
		}
		if body.PushChatID != nil {
			cur.PushChatID = *body.PushChatID
		}
		if body.PushMessageThreadID != nil {
			cur.PushMessageThreadID = *body.PushMessageThreadID
		}
		if body.PushSMS != nil {
			cur.PushSMS = *body.PushSMS
		}
		raw, err := json.Marshal(cur)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "serialize_error", err.Error())
			return
		}
		if err := deps.Store.PutSetting(req.Context(), telegramSettingsKey, string(raw)); err != nil {
			logAudit(req.Context(), deps, audit.Entry{
				Actor:  actorFromRequest(req),
				Action: "settings.telegram.update",
				Result: "error",
				Err:    err.Error(),
			})
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		// 审计 payload 不含 bot_token；只记元数据变化。
		logAudit(req.Context(), deps, audit.Entry{
			Actor:  actorFromRequest(req),
			Action: "settings.telegram.update",
			Payload: map[string]any{
				"has_token":              cur.BotToken != "",
				"chat_id":                cur.ChatID,
				"push_chat_id":           cur.PushChatID,
				"push_message_thread_id": cur.PushMessageThreadID,
				"push_sms":               cur.PushSMS,
				"cleared":                body.Clear,
			},
			Result: "ok",
		})
		// 热重载：token/chat_id/push_chat_id/push_message_thread_id/push_sms 任一变更都重启 bot。
		if deps.TelegramCtl != nil {
			if err := deps.TelegramCtl.Reload(req.Context(), cur); err != nil {
				// 记录但不阻塞 PUT 成功返回——settings 已保存
				// （日志由 Reload 内部 log 输出）
				_ = err
			}
		}
		writeJSON(w, http.StatusOK, telegramDTO{
			HasToken:            cur.BotToken != "",
			ChatID:              cur.ChatID,
			PushChatID:          cur.PushChatID,
			PushMessageThreadID: cur.PushMessageThreadID,
			PushSMS:             cur.PushSMS,
			Source:              "settings",
		})
	})

	// POST /settings/telegram/test —— 发送一条测试消息到短信推送目的地。
	// Body: {"text":"可选"}；空文本也可用，仅发默认 "测试消息" 提示。
	// 412：bot 未配置；500：Telegram API 返回错误；200：成功。
	r.Post("/settings/telegram/test", func(w http.ResponseWriter, req *http.Request) {
		var body telegramTestRequest
		if req.ContentLength > 0 {
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
				return
			}
		}
		if deps.TelegramCtl == nil {
			writeError(w, http.StatusPreconditionFailed, "not_configured",
				"telegram not configured")
			return
		}
		if err := deps.TelegramCtl.TestPush(req.Context(), body.Text); err != nil {
			if errors.Is(err, telegram.ErrBotNotConfigured) {
				logAudit(req.Context(), deps, audit.Entry{
					Actor:  actorFromRequest(req),
					Action: "settings.telegram.test",
					Result: "error",
					Err:    "not_configured",
				})
				writeError(w, http.StatusPreconditionFailed, "not_configured",
					"telegram not configured")
				return
			}
			logAudit(req.Context(), deps, audit.Entry{
				Actor:  actorFromRequest(req),
				Action: "settings.telegram.test",
				Result: "error",
				Err:    err.Error(),
			})
			writeError(w, http.StatusInternalServerError, "send_failed", err.Error())
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:   actorFromRequest(req),
			Action:  "settings.telegram.test",
			Payload: map[string]any{"text_len": len(body.Text)},
			Result:  "ok",
		})
		writeJSON(w, http.StatusOK, map[string]string{"message": "sent"})
	})
}

type telegramTestRequest struct {
	Text string `json:"text"`
}

// loadTelegram 从 settings 表加载；缺省回退到 config.yaml 里的值。
func loadTelegram(ctx context.Context, deps Deps) (config.TelegramConfig, string) {
	raw, err := deps.Store.GetSetting(ctx, telegramSettingsKey)
	if err == nil && raw != "" {
		var cfg config.TelegramConfig
		if json.Unmarshal([]byte(raw), &cfg) == nil {
			return cfg, "settings"
		}
	}
	return deps.Telegram, "config"
}
