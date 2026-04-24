// Package httpapi 提供 REST 路由。阶段 3 会在此注册 SMS/USSD/modem 等路由，
// 当前仅提供 /healthz 与 /api/version 让骨架可跑。
package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps 聚合 HTTP 层依赖。后续会加入 services、authService 等。
type Deps struct {
	Version string
	WebRoot string // 静态站点根目录；空则不托管
}

// NewRouter 组装路由。
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30_000_000_000)) // 30s

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(api chi.Router) {
		api.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"version": deps.Version})
		})
		// TODO(stage-3): /auth, /modems, /sims, /sms, /ussd, /esim 路由
	})

	// WebSocket 端点占位，阶段 3 实现。
	r.Get("/ws", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "websocket not implemented yet", http.StatusNotImplemented)
	})

	// 托管静态前端（SPA），非 /api 与 /ws 的路径落到 index.html
	if deps.WebRoot != "" {
		fs := http.FileServer(http.Dir(deps.WebRoot))
		r.Handle("/assets/*", fs)
		r.Handle("/favicon.ico", fs)
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			// SPA fallback
			http.ServeFile(w, req, filepath.Join(deps.WebRoot, "index.html"))
		})
	}
	return r
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
