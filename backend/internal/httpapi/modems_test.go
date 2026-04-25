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
)

// resetProviderWrap 包装 MockProvider，允许把 ResetModem 覆写为定制行为。
type resetProviderWrap struct {
	*modem.MockProvider
	resetErr atomic.Pointer[error] // 每次调用前设置；nil = 透传给 mock
}

func (w *resetProviderWrap) ResetModem(ctx context.Context, deviceID string) error {
	if p := w.resetErr.Load(); p != nil {
		return *p
	}
	return w.MockProvider.ResetModem(ctx, deviceID)
}

func setupWithProvider(t *testing.T, provider modem.Provider) (*httptest.Server, string) {
	srv, tok, _ := setupWithProviderAndStore(t, provider)
	return srv, tok
}

func setupWithProviderAndStore(t *testing.T, provider modem.Provider) (*httptest.Server, string, *modem.Store) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "m.db")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	store := modem.NewStore(conn)
	runner := modem.NewRunner(provider, store, log)
	runCtx, runCancel := context.WithCancel(context.Background())
	t.Cleanup(runCancel)
	go func() { _ = runner.Run(runCtx) }()
	// 等待 mock 的 ModemAdded 事件被 runner 落库
	time.Sleep(200 * time.Millisecond)

	secret, _ := auth.GenerateSecret()
	authSvc, err := auth.New(auth.Config{
		Secret: []byte(secret), Username: "admin", TokenTTL: time.Hour,
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, _ := authSvc.Issue("admin")

	h := NewRouter(Deps{
		Version: "test", Modem: provider, ModemRunner: runner, Store: store,
		Auth: authSvc, Server: config.ServerConfig{}, Telegram: config.TelegramConfig{},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, tok, store
}

const mockDev = "mock-device-quectel-0001"

func TestResetModem_Success(t *testing.T) {
	provider := modem.NewMockProvider(nil)
	srv, tok := setupWithProvider(t, provider)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/modems/"+mockDev+"/reset", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["message"] != "reset requested" {
		t.Fatalf("unexpected message: %+v", body)
	}
}

func TestResetModem_Unsupported(t *testing.T) {
	mock := modem.NewMockProvider(nil)
	wrap := &resetProviderWrap{MockProvider: mock}
	e := modem.ErrModemResetUnsupported
	wrap.resetErr.Store(&e)
	srv, tok := setupWithProvider(t, wrap)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/modems/"+mockDev+"/reset", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["code"] != "reset_unsupported" {
		t.Fatalf("unexpected code: %+v", body)
	}
}

func TestResetModem_GenericError(t *testing.T) {
	mock := modem.NewMockProvider(nil)
	wrap := &resetProviderWrap{MockProvider: mock}
	e := errors.New("dbus: connection refused")
	wrap.resetErr.Store(&e)
	srv, tok := setupWithProvider(t, wrap)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/modems/"+mockDev+"/reset", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["code"] != "reset_failed" {
		t.Fatalf("unexpected code: %+v", body)
	}
}

func TestResetModem_NotFound(t *testing.T) {
	provider := modem.NewMockProvider(nil)
	srv, tok := setupWithProvider(t, provider)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/modems/not-a-device/reset", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestModemNickname_SetAndClear(t *testing.T) {
	provider := modem.NewMockProvider(nil)
	srv, tok := setupWithProvider(t, provider)

	// 设置备注
	raw, _ := json.Marshal(map[string]string{"nickname": "  office-sim  "})
	req, _ := http.NewRequest(http.MethodPut,
		srv.URL+"/api/modems/"+mockDev+"/nickname", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("put nickname status=%d", resp.StatusCode)
	}
	var row map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row["nickname"] != "office-sim" {
		t.Fatalf("expected trimmed nickname, got %v", row["nickname"])
	}

	// GET 也能看到
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/modems/"+mockDev, nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var got map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&got)
	if got["nickname"] != "office-sim" {
		t.Fatalf("GET should return nickname, got %v", got["nickname"])
	}

	// 清空：空字符串
	raw2, _ := json.Marshal(map[string]string{"nickname": ""})
	req3, _ := http.NewRequest(http.MethodPut,
		srv.URL+"/api/modems/"+mockDev+"/nickname", bytes.NewReader(raw2))
	req3.Header.Set("Authorization", "Bearer "+tok)
	req3.Header.Set("Content-Type", "application/json")
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Fatalf("clear nickname status=%d", resp3.StatusCode)
	}
	var cleared map[string]any
	_ = json.NewDecoder(resp3.Body).Decode(&cleared)
	// 清空后 nickname 应为 nil
	if cleared["nickname"] != nil {
		t.Fatalf("expected nickname cleared, got %v", cleared["nickname"])
	}
}

func TestModemNickname_NotFound(t *testing.T) {
	provider := modem.NewMockProvider(nil)
	srv, tok := setupWithProvider(t, provider)

	raw, _ := json.Marshal(map[string]string{"nickname": "x"})
	req, _ := http.NewRequest(http.MethodPut,
		srv.URL+"/api/modems/nonexistent/nickname", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteOfflineModemAPI(t *testing.T) {
	provider := modem.NewMockProvider(nil)
	srv, tok, store := setupWithProviderAndStore(t, provider)
	ctx := context.Background()

	// 在线 mock 模块不能删除。
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/modems/"+mockDev, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for online modem, got %d", resp.StatusCode)
	}

	if err := store.MarkModemAbsent(ctx, mockDev); err != nil {
		t.Fatal(err)
	}
	req2, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/modems/"+mockDev, nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 deleting offline modem, got %d", resp2.StatusCode)
	}
}
