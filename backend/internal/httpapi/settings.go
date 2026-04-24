// settings.go —— /api/settings/telegram 路由。
//
// 存储策略：
//   - 运行时的 telegram 配置优先以 settings 表中 key="telegram" 为准；
//     没有则回退到 config.yaml 的值（只读）。
//   - GET 响应 *绝不* 回显 bot_token。只回 has_token/chat_id/push_sms。
//   - PUT 可以更新任一字段；bot_token="" 视为不修改（除非带 clear:true）。
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
)

type telegramDTO struct {
	HasToken bool   `json:"has_token"`
	ChatID   int64  `json:"chat_id"`
	PushSMS  bool   `json:"push_sms"`
	Source   string `json:"source"` // "config" | "settings"
}

type telegramPut struct {
	BotToken *string `json:"bot_token,omitempty"`
	ChatID   *int64  `json:"chat_id,omitempty"`
	PushSMS  *bool   `json:"push_sms,omitempty"`
	Clear    bool    `json:"clear,omitempty"` // 为 true 时清空 bot_token
}

const telegramSettingsKey = "telegram"

func registerSettings(r chi.Router, deps Deps) {
	r.Get("/settings/telegram", func(w http.ResponseWriter, req *http.Request) {
		cur, source := loadTelegram(req.Context(), deps)
		writeJSON(w, http.StatusOK, telegramDTO{
			HasToken: cur.BotToken != "",
			ChatID:   cur.ChatID,
			PushSMS:  cur.PushSMS,
			Source:   source,
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
		if body.PushSMS != nil {
			cur.PushSMS = *body.PushSMS
		}
		raw, err := json.Marshal(cur)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "serialize_error", err.Error())
			return
		}
		if err := deps.Store.PutSetting(req.Context(), telegramSettingsKey, string(raw)); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, telegramDTO{
			HasToken: cur.BotToken != "",
			ChatID:   cur.ChatID,
			PushSMS:  cur.PushSMS,
			Source:   "settings",
		})
	})
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
