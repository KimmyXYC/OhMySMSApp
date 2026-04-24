// auth.go —— /api/auth/* 路由。
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

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
			writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
			return
		}
		tok, exp, err := deps.Auth.Issue(body.Username)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "token_issue_failed", err.Error())
			return
		}
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
}
