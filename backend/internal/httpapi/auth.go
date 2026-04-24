// auth.go —— /api/auth/* 路由。
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string       `json:"token"`
	ExpiresAt string       `json:"expires_at"`
	User      responseUser `json:"user"`
}

type responseUser struct {
	Username string `json:"username"`
}

// registerAuthPublic 注册无需鉴权的 /auth 端点。
func registerAuthPublic(r chi.Router, deps Deps) {
	r.Post("/auth/login", func(w http.ResponseWriter, req *http.Request) {
		if deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "auth_disabled", "auth not configured")
			return
		}
		var body loginRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		if err := deps.Auth.CheckCredentials(body.Username, body.Password); err != nil {
			logAudit(req.Context(), deps, audit.Entry{
				Actor:   "web:" + body.Username,
				Action:  "auth.login",
				Target:  body.Username,
				Payload: map[string]any{"ip": req.RemoteAddr},
				Result:  "error",
				Err:     err.Error(),
			})
			writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
			return
		}
		tok, exp, err := deps.Auth.Issue(body.Username)
		if err != nil {
			logAudit(req.Context(), deps, audit.Entry{
				Actor:  "web:" + body.Username,
				Action: "auth.login",
				Target: body.Username,
				Result: "error",
				Err:    err.Error(),
			})
			writeError(w, http.StatusInternalServerError, "token_issue_failed", err.Error())
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:   "web:" + body.Username,
			Action:  "auth.login",
			Target:  body.Username,
			Payload: map[string]any{"ip": req.RemoteAddr},
			Result:  "ok",
		})
		writeJSON(w, http.StatusOK, loginResponse{
			Token:     tok,
			ExpiresAt: exp.UTC().Format("2006-01-02T15:04:05Z"),
			User:      responseUser{Username: body.Username},
		})
	})

	// logout 无状态（JWT），只回 ok。
	r.Post("/auth/logout", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

// registerAuthProtected 注册需要鉴权的 /auth 端点。
func registerAuthProtected(r chi.Router, deps Deps) {
	r.Get("/auth/me", func(w http.ResponseWriter, req *http.Request) {
		c, ok := auth.ClaimsFromContext(req.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "no claims")
			return
		}
		writeJSON(w, http.StatusOK, responseUser{Username: c.Subject})
	})

	// POST /auth/password —— 修改当前用户密码。
	// 请求体 { current_password, new_password }；new_password 至少 6 字符。
	// 成功后返回 200 + {"message":"password updated"}；原 token 仍然有效。
	r.Post("/auth/password", func(w http.ResponseWriter, req *http.Request) {
		if deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "auth_disabled", "auth not configured")
			return
		}
		var body changePasswordRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		if strings.TrimSpace(body.NewPassword) == "" {
			writeError(w, http.StatusBadRequest, "password_too_short", "new_password is required")
			return
		}
		if len(strings.TrimSpace(body.NewPassword)) < 6 {
			writeError(w, http.StatusBadRequest, "password_too_short", "new_password must be at least 6 characters")
			return
		}
		if err := deps.Auth.ChangePassword(req.Context(), body.CurrentPassword, body.NewPassword); err != nil {
			// 区分"当前密码错"与其它错误
			if err.Error() == "invalid current password" {
				logAudit(req.Context(), deps, audit.Entry{
					Actor:  actorFromRequest(req),
					Action: "auth.password",
					Result: "error",
					Err:    err.Error(),
				})
				writeError(w, http.StatusUnauthorized, "invalid_current_password", err.Error())
				return
			}
			// 服务层也会二次校验长度，这里再兜一次
			if strings.Contains(err.Error(), "too short") {
				writeError(w, http.StatusBadRequest, "password_too_short", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "change_password_failed", err.Error())
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:  actorFromRequest(req),
			Action: "auth.password",
			Result: "ok",
		})
		writeJSON(w, http.StatusOK, map[string]string{"message": "password updated"})
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
