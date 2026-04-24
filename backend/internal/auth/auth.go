// Package auth 提供单用户模式下的登录鉴权：
//   - bcrypt 密码校验
//   - HMAC-SHA256 JWT 签发/校验
//   - HTTP 中间件：Authorization: Bearer <token> 或 ?token= 参数（WS 用）
//
// 安全约定：
//   - jwt_secret 为空时 main.go 会随机生成一份放内存并告警（生产须落盘）
//   - password_bcrypt 为空时（开发模式）接受任何密码但打 WARN 日志
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ctxKey 用于把 claims 塞进 request context。
type ctxKey struct{}

// Claims 是 ohmysms JWT 的业务负载。
type Claims struct {
	Subject string // 用户名，= username
	IssuedAt time.Time
	ExpiresAt time.Time
}

// Service 聚合签发/校验/密码校验逻辑。无状态，线程安全。
type Service struct {
	log            *slog.Logger
	secret         []byte
	username       string
	passwordBcrypt string // 允许为空 → 开发模式
	ttl            time.Duration
	devMode        bool // passwordBcrypt 为空时为 true
}

// Config 服务构造参数。
type Config struct {
	Secret         []byte
	Username       string
	PasswordBcrypt string
	TokenTTL       time.Duration
}

// New 构造 Service。secret 若为 nil/空，会返回错误（调用方须先生成）。
func New(cfg Config, log *slog.Logger) (*Service, error) {
	if log == nil {
		log = slog.Default()
	}
	if len(cfg.Secret) == 0 {
		return nil, errors.New("auth: jwt secret is empty")
	}
	if cfg.Username == "" {
		return nil, errors.New("auth: username is empty")
	}
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = 30 * 24 * time.Hour
	}
	dev := cfg.PasswordBcrypt == ""
	if dev {
		log.Warn("auth running in DEV mode: password_bcrypt not set, any password will be accepted — set auth.password_bcrypt in config.yaml for production")
	}
	return &Service{
		log:            log,
		secret:         cfg.Secret,
		username:       cfg.Username,
		passwordBcrypt: cfg.PasswordBcrypt,
		ttl:            cfg.TokenTTL,
		devMode:        dev,
	}, nil
}

// Username 返回配置的唯一用户名（供 /me 接口）。
func (s *Service) Username() string { return s.username }

// TTL 返回 token 有效期。
func (s *Service) TTL() time.Duration { return s.ttl }

// CheckCredentials 验证用户名 + 明文密码。devMode 下任意密码通过，但用户名仍需匹配。
func (s *Service) CheckCredentials(username, password string) error {
	if username != s.username {
		return errors.New("invalid credentials")
	}
	if s.devMode {
		s.log.Warn("dev-mode login accepted", "user", username)
		return nil
	}
	return comparePassword(s.passwordBcrypt, password)
}

// Issue 签发 JWT。
func (s *Service) Issue(username string) (token string, expiresAt time.Time, err error) {
	now := time.Now()
	exp := now.Add(s.ttl)
	tok, err := signHS256(s.secret, map[string]any{
		"sub": username,
		"iat": now.Unix(),
		"exp": exp.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return tok, exp, nil
}

// Validate 解析并校验 token。返回 Claims 或错误。
func (s *Service) Validate(token string) (Claims, error) {
	payload, err := verifyHS256(s.secret, token)
	if err != nil {
		return Claims{}, err
	}
	sub, _ := payload["sub"].(string)
	if sub == "" {
		return Claims{}, errors.New("jwt: missing sub")
	}
	iat, _ := toInt64(payload["iat"])
	exp, _ := toInt64(payload["exp"])
	if exp == 0 {
		return Claims{}, errors.New("jwt: missing exp")
	}
	if time.Now().Unix() >= exp {
		return Claims{}, errors.New("jwt: token expired")
	}
	return Claims{
		Subject:   sub,
		IssuedAt:  time.Unix(iat, 0),
		ExpiresAt: time.Unix(exp, 0),
	}, nil
}

// ClaimsFromContext 返回 RequireAuth 注入的 claims。
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}

// RequireAuth 是 chi 中间件。期望 Authorization: Bearer <jwt> 或 ?token=<jwt>。
// 失败返回 401 JSON: {"error":"...","code":"unauthorized"}。
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := extractToken(r)
		if tok == "" {
			writeUnauthorized(w, "missing token")
			return
		}
		claims, err := s.Validate(tok)
		if err != nil {
			writeUnauthorized(w, err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ValidateQueryToken 用于 WS 升级前鉴权：直接给 ?token= 参数。
func (s *Service) ValidateQueryToken(r *http.Request) (Claims, error) {
	tok := extractToken(r)
	if tok == "" {
		return Claims{}, errors.New("missing token")
	}
	return s.Validate(tok)
}

// ---------- helpers ----------

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		parts := strings.SplitN(h, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
		// 也允许直接给裸 token
		return strings.TrimSpace(h)
	}
	if q := r.URL.Query().Get("token"); q != "" {
		return q
	}
	return ""
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("WWW-Authenticate", `Bearer realm="ohmysms"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"error":%q,"code":"unauthorized"}`, msg)
}

// GenerateSecret 产生 32 字节 hex（共 64 字符）作为 jwt secret。
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}
