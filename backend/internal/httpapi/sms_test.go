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

// setup 启一个内嵌 httptest server，用 MockProvider + 临时 sqlite。
func setup(t *testing.T) (*httptest.Server, *auth.Service, string) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := db.Open(ctx, dbPath)
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

	// 等 runner 处理初始 events（mock 启动时 emit 两条 ModemAdded）
	time.Sleep(200 * time.Millisecond)

	secret, _ := auth.GenerateSecret()
	authSvc, err := auth.New(auth.Config{
		Secret:   []byte(secret),
		Username: "admin",
		TokenTTL: time.Hour,
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, _ := authSvc.Issue("admin")

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

func TestSMSSend(t *testing.T) {
	srv, _, tok := setup(t)

	body := map[string]string{
		"device_id": "mock-device-quectel-0001",
		"peer":      "+100200",
		"body":      "hello from test",
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/sms/send", bytes.NewReader(raw))
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

	// 等 runner 落库
	time.Sleep(200 * time.Millisecond)

	// 再 list 一下，应能看到这条 outbound
	req2, _ := http.NewRequest(http.MethodGet,
		srv.URL+"/api/sms?device_id=mock-device-quectel-0001&direction=outbound", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("list status=%d", resp2.StatusCode)
	}
	var out struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Total == 0 {
		t.Fatalf("expected at least 1 sms, got 0")
	}
	found := false
	for _, it := range out.Items {
		if it["peer"] == "+100200" && it["body"] == "hello from test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sent sms not found in list: %+v", out)
	}
}

func TestSMSUnauthorized(t *testing.T) {
	srv, _, _ := setup(t)

	resp, err := http.Get(srv.URL + "/api/sms")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLogin(t *testing.T) {
	srv, _, _ := setup(t)

	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "anything-dev-mode",
	})
	resp, err := http.Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Token == "" {
		t.Fatal("no token")
	}

	// wrong username
	body, _ = json.Marshal(map[string]string{"username": "bob", "password": "x"})
	resp2, err := http.Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 401 {
		t.Fatalf("wrong user: status=%d", resp2.StatusCode)
	}
}

func TestModemsList(t *testing.T) {
	srv, _, tok := setup(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/modems", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var out struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Total < 2 {
		t.Fatalf("expected 2 modems, got %d; items=%+v", out.Total, out.Items)
	}
}
