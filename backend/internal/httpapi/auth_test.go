package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/auth"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/db"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// setupAuth 构造一个带预设密码的 Service + 绑定到 router，供 /api/auth/password 测试使用。
// 初始密码固定为 "initpw123"。返回的 token 由 username=admin 签发。
func setupAuth(t *testing.T) (*httptest.Server, *auth.Service, string) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "auth.db")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	secret, _ := auth.GenerateSecret()
	hash, err := auth.HashPassword("initpw123")
	if err != nil {
		t.Fatal(err)
	}
	authSvc, err := auth.New(auth.Config{
		Secret:         []byte(secret),
		Username:       "admin",
		PasswordBcrypt: hash,
		TokenTTL:       time.Hour,
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, _ := authSvc.Issue("admin")

	provider := modem.NewMockProvider(log)
	store := modem.NewStore(conn)
	runner := modem.NewRunner(provider, store, log)
	runCtx, runCancel := context.WithCancel(context.Background())
	t.Cleanup(runCancel)
	go func() { _ = runner.Run(runCtx) }()

	h := NewRouter(Deps{
		Version:     "test",
		Modem:       provider,
		ModemRunner: runner,
		Store:       store,
		Auth:        authSvc,
		Server:      config.ServerConfig{},
		Telegram:    config.TelegramConfig{},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, authSvc, tok
}

func postJSON(t *testing.T, srv *httptest.Server, tok, path string, body any) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestChangePasswordSuccess_HTTP(t *testing.T) {
	srv, svc, tok := setupAuth(t)
	resp := postJSON(t, srv, tok, "/api/auth/password", map[string]string{
		"current_password": "initpw123",
		"new_password":     "newpass!!",
	})
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	// 新密码能走 CheckCredentials
	if err := svc.CheckCredentials("admin", "newpass!!"); err != nil {
		t.Fatalf("new password must work: %v", err)
	}
	// 旧密码失败
	if err := svc.CheckCredentials("admin", "initpw123"); err == nil {
		t.Fatal("old password must fail")
	}
}

func TestChangePasswordWrongCurrent_HTTP(t *testing.T) {
	srv, _, tok := setupAuth(t)
	resp := postJSON(t, srv, tok, "/api/auth/password", map[string]string{
		"current_password": "wrong",
		"new_password":     "newpass!!",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["code"] != "invalid_current_password" {
		t.Fatalf("unexpected code: %q", body["code"])
	}
}

func TestChangePasswordTooShort_HTTP(t *testing.T) {
	srv, _, tok := setupAuth(t)
	for _, bad := range []string{"", "12345"} {
		resp := postJSON(t, srv, tok, "/api/auth/password", map[string]string{
			"current_password": "initpw123",
			"new_password":     bad,
		})
		if resp.StatusCode != http.StatusBadRequest {
			resp.Body.Close()
			t.Fatalf("bad=%q expected 400, got %d", bad, resp.StatusCode)
		}
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if body["code"] != "password_too_short" {
			t.Fatalf("bad=%q unexpected code: %q", bad, body["code"])
		}
	}
}

func TestChangePasswordUnauthenticated_HTTP(t *testing.T) {
	srv, _, _ := setupAuth(t)
	// 不带 Bearer token
	resp := postJSON(t, srv, "", "/api/auth/password", map[string]string{
		"current_password": "initpw123",
		"new_password":     "newpass!!",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", resp.StatusCode)
	}
}
