// audit.go —— /api/audit 查询端点 + audit helper。
package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/auth"
)

func registerAudit(r chi.Router, deps Deps) {
	r.Get("/audit", func(w http.ResponseWriter, req *http.Request) {
		if deps.Audit == nil {
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "total": 0})
			return
		}
		q := req.URL.Query()
		f := audit.ListFilter{
			Actor:  q.Get("actor"),
			Action: q.Get("action"),
		}
		if v := q.Get("limit"); v != "" {
			f.Limit, _ = strconv.Atoi(v)
		}
		if v := q.Get("offset"); v != "" {
			f.Offset, _ = strconv.Atoi(v)
		}
		rows, total, err := deps.Audit.List(req.Context(), f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if rows == nil {
			rows = []audit.Row{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":  rows,
			"total":  total,
			"limit":  f.Limit,
			"offset": f.Offset,
		})
	})
}

// actorFromRequest 从 request 上下文里取 JWT claims 的 subject 构造审计 actor。
// 没有 claims（未鉴权接口 / 匿名）返回 "web:anonymous"。
func actorFromRequest(req *http.Request) string {
	if c, ok := auth.ClaimsFromContext(req.Context()); ok && c.Subject != "" {
		return "web:" + c.Subject
	}
	return "web:anonymous"
}

// logAudit 是一个小 helper：deps.Audit 为 nil 时静默。
func logAudit(ctx context.Context, deps Deps, e audit.Entry) {
	if deps.Audit == nil {
		return
	}
	deps.Audit.Log(ctx, e)
}
