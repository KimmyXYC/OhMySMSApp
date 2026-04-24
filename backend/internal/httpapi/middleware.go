// middleware.go —— CORS 中间件。
package httpapi

import (
	"net/http"
	"path"
	"strings"
)

// corsMiddleware 根据 allowed 白名单放行 Origin。
//
// 行为：
//   - 空白名单 → 不添加 CORS 头（同源场景，浏览器天然允许）
//   - 白名单含 "*" → 完全放开（开发/受信内网；Access-Control-Allow-Credentials 依然回 true
//     并回显具体 Origin，避免 `*` 与 credentials 的规范冲突）
//   - 其它 → 按 glob 模式匹配（与 ws.Hub 的 OriginPatterns 行为对齐），支持 `*`、`?`
//     例如 `http://localhost:*`、`https://*.example.com`
//
// 命中匹配时回显完整 CORS 响应头，包括 Access-Control-Allow-Credentials。
// Preflight（OPTIONS）请求直接回 204（已带 CORS 头）。
func corsMiddleware(allowed []string) func(http.Handler) http.Handler {
	patterns := make([]string, 0, len(allowed))
	wildcard := false
	for _, o := range allowed {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if o == "*" {
			wildcard = true
			continue
		}
		patterns = append(patterns, o)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (wildcard || matchOrigin(patterns, origin)) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// matchOrigin 按 glob 语法匹配任意一个模式。
// 精确匹配在 glob 下是退化情况（无通配符），一并覆盖。
func matchOrigin(patterns []string, origin string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, origin); ok {
			return true
		}
	}
	return false
}
