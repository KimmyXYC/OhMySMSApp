// audit_test.go —— 验证写操作会落入 audit_log，/api/audit 能查询。
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

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/auth"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/db"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// setupWithAudit 与 setup 类似但带 audit.Service，便于校验审计落库。
func setupWithAudit(t *testing.T) (*httptest.Server, *audit.Service, string) {
	t.Helper()
	tmp := t.TempDir()
	conn, err := db.Open(context.Background(), filepath.Join(tmp, "a.db"))
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
	time.Sleep(150 * time.Millisecond)

	secret, _ := auth.GenerateSecret()
	authSvc, _ := auth.New(auth.Config{
		Secret: []byte(secret), Username: "admin", TokenTTL: time.Hour,
	}, log)
	tok, _, _ := authSvc.Issue("admin")

	auditSvc := audit.New(conn, log)

	h := NewRouter(Deps{
		Version: "test", Modem: provider, ModemRunner: runner, Store: store,
		Auth:   authSvc,
		Audit:  auditSvc,
		Server: config.ServerConfig{}, Telegram: config.TelegramConfig{},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, auditSvc, tok
}

// TestAudit_SMSSendLogged 验证成功发送短信会写一条 ok 的 sms.send 记录，
// 且 payload 只含 peer + body_len（不含完整 body）。
func TestAudit_SMSSendLogged(t *testing.T) {
	srv, auditSvc, tok := setupWithAudit(t)

	body, _ := json.Marshal(map[string]string{
		"device_id": "mock-device-quectel-0001",
		"peer":      "+1001",
		"body":      "hello audit",
	})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/sms/send", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("send status=%d", resp.StatusCode)
	}

	rows, _, err := auditSvc.List(context.Background(), audit.ListFilter{Action: "sms.send"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 sms.send audit row, got %d", len(rows))
	}
	r := rows[0]
	if r.Actor != "web:admin" {
		t.Errorf("actor=%q", r.Actor)
	}
	if r.Result != "ok" {
		t.Errorf("result=%q", r.Result)
	}
	var p map[string]any
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p["peer"] != "+1001" {
		t.Errorf("payload peer=%v", p["peer"])
	}
	if _, ok := p["body_len"]; !ok {
		t.Errorf("payload missing body_len: %v", p)
	}
	if _, leaked := p["body"]; leaked {
		t.Errorf("payload should not contain full body: %v", p)
	}
}

// TestAudit_LoginFailureLogged 验证失败登录会写 error 记录。
func TestAudit_LoginFailureLogged(t *testing.T) {
	srv, auditSvc, _ := setupWithAudit(t)

	body, _ := json.Marshal(map[string]string{
		"username": "bob", "password": "x",
	})
	resp, err := http.Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	rows, _, err := auditSvc.List(context.Background(), audit.ListFilter{Action: "auth.login"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 auth.login audit row, got %d", len(rows))
	}
	if rows[0].Result != "error" {
		t.Errorf("result=%q, want error", rows[0].Result)
	}
	if rows[0].Actor != "web:bob" {
		t.Errorf("actor=%q", rows[0].Actor)
	}
}

// TestAudit_ListEndpoint 验证 GET /api/audit 能返回数据。
func TestAudit_ListEndpoint(t *testing.T) {
	srv, auditSvc, tok := setupWithAudit(t)
	ctx := context.Background()
	auditSvc.Log(ctx, audit.Entry{Actor: "system", Action: "test.ping", Result: "ok"})

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/audit?action=test.ping", nil)
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
		Items []audit.Row `json:"items"`
		Total int         `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Total != 1 || len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got total=%d len=%d", out.Total, len(out.Items))
	}
	if out.Items[0].Action != "test.ping" {
		t.Errorf("action=%q", out.Items[0].Action)
	}
}
