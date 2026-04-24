// Package httpapi 提供 REST 路由。阶段 3 实现：
//   - auth (login / me)
//   - modems / sims
//   - sms（列表 / threads / send / delete）
//   - ussd（start / respond / cancel / sessions）
//   - signal history
//   - settings.telegram（安全读写，不回 bot_token）
//
// 响应约定：
//   - 成功：直接返回资源 JSON；列表用 {"items":[...],"total":N}
//   - 失败：{"error":"...","code":"..."} + 对应 HTTP 状态码
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/auth"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// TelegramController 暴露 Reload + TestPush，避免 httpapi → telegram 反向依赖。
// main.go 把 *telegram.Controller 作为此接口注入；测试可用 mock 替代。
type TelegramController interface {
	Reload(ctx context.Context, cfg config.TelegramConfig) error
	TestPush(ctx context.Context, text string) error
}

// Deps 聚合 HTTP 层依赖。
type Deps struct {
	Version string
	WebRoot string // 静态站点根目录；空则不托管

	// 后端子系统
	Modem       modem.Provider
	ModemRunner *modem.Runner
	Store       *modem.Store
	Auth        *auth.Service
	Audit       *audit.Service // 审计日志服务；可为 nil（不记录）

	// WS handler（/ws 端点）；由 main.go 注入 hub.ServeHTTP
	WSHandler http.Handler

	// 运行期配置的只读快照（CORS / telegram 等）
	Server   config.ServerConfig
	Telegram config.TelegramConfig

	// 可选：PUT /api/settings/telegram 保存后触发热重载；nil 则跳过。
	// 也用于 POST /api/settings/telegram/test 发送测试消息。
	TelegramCtl TelegramController
}

// NewRouter 组装路由。
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(deps.Server.AllowedOrigins))
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(api chi.Router) {
		// 公开
		api.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"version": deps.Version})
		})
		registerAuthPublic(api, deps)

		// 鉴权
		api.Group(func(pr chi.Router) {
			if deps.Auth != nil {
				pr.Use(deps.Auth.RequireAuth)
			}
			registerAuthProtected(pr, deps)
			registerModems(pr, deps)
			registerSims(pr, deps)
			registerSMS(pr, deps)
			registerUSSD(pr, deps)
			registerSignal(pr, deps)
			registerSettings(pr, deps)
			registerAudit(pr, deps)
		})
	})

	// WebSocket 端点（鉴权在 hub 内部做）
	if deps.WSHandler != nil {
		r.Handle("/ws", deps.WSHandler)
	} else {
		r.Get("/ws", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "websocket not wired", http.StatusNotImplemented)
		})
	}

	// 托管静态前端（SPA），非 /api /ws /healthz 的路径落到 index.html
	if deps.WebRoot != "" {
		fs := http.FileServer(http.Dir(deps.WebRoot))
		r.Handle("/assets/*", fs)
		r.Handle("/favicon.ico", fs)
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			http.ServeFile(w, req, filepath.Join(deps.WebRoot, "index.html"))
		})
	}
	return r
}

// writeJSON 把 body 编码为 JSON 写入响应。
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError 写标准错误体。
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
