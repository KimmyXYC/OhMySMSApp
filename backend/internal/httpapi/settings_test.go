package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/auth"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/db"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/telegram"
)

// mockTelegramCtl 实现 TelegramController 接口，供测试注入。
type mockTelegramCtl struct {
	testPushErr atomic.Pointer[error]
	testCalls   atomic.Int32
	lastText    atomic.Value // string
}

func (m *mockTelegramCtl) Reload(_ context.Context, _ config.TelegramConfig) error {
	return nil
}

func (m *mockTelegramCtl) TestPush(_ context.Context, text string) error {
	m.testCalls.Add(1)
	m.lastText.Store(text)
	if p := m.testPushErr.Load(); p != nil {
		return *p
	}
	return nil
}

func setupSettings(t *testing.T, ctl TelegramController) (*httptest.Server, string) {
	t.Helper()
	tmp := t.TempDir()
	conn, err := db.Open(context.Background(), filepath.Join(tmp, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	provider := modem.NewMockProvider(log)
	store := modem.NewStore(conn)
	runner := modem.NewRunner(provider, store, log)
	runCtx, runCancel := context.WithCancel(context.Background())
	t.Cleanup(runCancel)
	go func() { _ = runner.Run(runCtx) }()
	time.Sleep(100 * time.Millisecond)

	secret, _ := auth.GenerateSecret()
	authSvc, _ := auth.New(auth.Config{
		Secret: []byte(secret), Username: "admin", TokenTTL: time.Hour,
	}, log)
	tok, _, _ := authSvc.Issue("admin")

	h := NewRouter(Deps{
		Version: "test", Modem: provider, ModemRunner: runner, Store: store,
		Auth: authSvc, Server: config.ServerConfig{}, Telegram: config.TelegramConfig{},
		TelegramCtl: ctl,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, tok
}

func TestTelegramTest_Success(t *testing.T) {
	ctl := &mockTelegramCtl{}
	srv, tok := setupSettings(t, ctl)

	body, _ := json.Marshal(map[string]string{"text": "hello"})
	req, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/api/settings/telegram/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var got map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["message"] != "sent" {
		t.Fatalf("unexpected body: %+v", got)
	}
	if ctl.testCalls.Load() != 1 {
		t.Fatalf("expected 1 TestPush call, got %d", ctl.testCalls.Load())
	}
	if s, _ := ctl.lastText.Load().(string); s != "hello" {
		t.Fatalf("text not forwarded: %q", s)
	}
}

func TestTelegramTest_NotConfigured(t *testing.T) {
	ctl := &mockTelegramCtl{}
	notCfg := error(telegram.ErrBotNotConfigured)
	ctl.testPushErr.Store(&notCfg)
	srv, tok := setupSettings(t, ctl)

	body, _ := json.Marshal(map[string]string{"text": "x"})
	req, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/api/settings/telegram/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", resp.StatusCode)
	}
	var got map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["code"] != "not_configured" {
		t.Fatalf("unexpected code: %+v", got)
	}
}

func TestTelegramTest_NotConfigured_NilCtl(t *testing.T) {
	// TelegramCtl 本身未注入：也视为 412
	srv, tok := setupSettings(t, nil)

	body, _ := json.Marshal(map[string]string{"text": "x"})
	req, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/api/settings/telegram/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", resp.StatusCode)
	}
}

func TestTelegramTest_SendFailed(t *testing.T) {
	ctl := &mockTelegramCtl{}
	fail := errors.New("telegram api 401 Forbidden")
	ctl.testPushErr.Store(&fail)
	srv, tok := setupSettings(t, ctl)

	body, _ := json.Marshal(map[string]string{"text": "x"})
	req, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/api/settings/telegram/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
	var got map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["code"] != "send_failed" {
		t.Fatalf("unexpected code: %+v", got)
	}
}

func TestTelegramTest_EmptyBodyAllowed(t *testing.T) {
	// 空 body 也应当被允许，直接调 TestPush("")
	ctl := &mockTelegramCtl{}
	srv, tok := setupSettings(t, ctl)

	req, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/api/settings/telegram/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ctl.testCalls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", ctl.testCalls.Load())
	}
}
