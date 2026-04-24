package audit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/db"
)

// openTestDB 构造一个带完整 schema 的临时 SQLite。
func openTestDB(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	path := t.TempDir() + "/audit.db"
	conn, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return New(conn, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestLogAndList(t *testing.T) {
	ctx := context.Background()
	s := openTestDB(t)

	s.Log(ctx, Entry{
		Actor:   "web:admin",
		Action:  "auth.login",
		Target:  "admin",
		Payload: map[string]any{"ip": "1.2.3.4"},
		Result:  "ok",
	})
	s.Log(ctx, Entry{
		Actor:  "web:admin",
		Action: "sms.send",
		Target: "devA",
		Payload: map[string]any{
			"peer":     "+111",
			"body_len": 5,
		},
	})
	s.Log(ctx, Entry{
		Actor:  "telegram:999",
		Action: "sms.send",
		Target: "devA",
		Err:    "send failed",
	})

	rows, total, err := s.List(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("expected 3 rows, got total=%d len=%d", total, len(rows))
	}
	// 最新的在前
	if rows[0].Action != "sms.send" || rows[0].Actor != "telegram:999" {
		t.Errorf("ordering wrong: %+v", rows[0])
	}
	// error 分支的 result 应该是 "error"
	if rows[0].Result != "error" {
		t.Errorf("expected result=error, got %q", rows[0].Result)
	}

	// 按 actor 过滤
	filtered, _, err := s.List(ctx, ListFilter{Actor: "web:admin"})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("expected 2 rows for web:admin, got %d", len(filtered))
	}
	for _, r := range filtered {
		if r.Actor != "web:admin" {
			t.Errorf("unexpected actor: %q", r.Actor)
		}
	}

	// 按 action 过滤
	filtered2, _, err := s.List(ctx, ListFilter{Action: "sms.send"})
	if err != nil {
		t.Fatalf("list filtered2: %v", err)
	}
	if len(filtered2) != 2 {
		t.Errorf("expected 2 rows for sms.send, got %d", len(filtered2))
	}

	// payload 能反序列化（找一条带 payload 的）
	var okRow Row
	for _, r := range filtered2 {
		if len(r.Payload) > 0 {
			okRow = r
			break
		}
	}
	if len(okRow.Payload) == 0 {
		t.Fatalf("no row with payload found in: %+v", filtered2)
	}
	var p map[string]any
	if err := json.Unmarshal(okRow.Payload, &p); err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	if _, ok := p["peer"]; !ok {
		t.Errorf("missing peer in payload: %+v", p)
	}
}

// TestLog_NilServiceIsSafe 验证 nil-safe：audit.Service = nil 时不 panic。
func TestLog_NilServiceIsSafe(t *testing.T) {
	var s *Service
	s.Log(context.Background(), Entry{Action: "x", Actor: "system"})
}
