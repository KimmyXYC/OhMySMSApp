// Package audit 负责记录写操作审计日志。
//
// 约定：
//   - actor：凡是通过 web 来的写操作，格式 "web:<username>"（无 username 时用 "web:anonymous"）；
//     凡是通过 Telegram Bot 来的，"telegram:<chatID>"；系统自发的 "system"。
//   - action：形如 "auth.login"、"sms.send"、"ussd.start"、"modem.reset"、"modem.nickname"、
//     "settings.telegram.update"、"settings.telegram.test"、"auth.password"。
//   - target：具体对象标识（sms id、device_id、chat id 等），无则空串。
//   - payload：JSON 字符串；调用方负责脱敏（见 Insert/LogAction 的 payload 构造方）。
//   - result："ok" / "error"；error 填错误文本（有时等同于 err.Error()）。
//
// 写入失败只做 warn log，不阻塞业务流程——审计日志不应影响主要功能。
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"
)

// Service 封装审计日志写入。无查询接口；查询走 Store.ListAudit（见 httpapi/settings.go 附近）。
type Service struct {
	db  *sql.DB
	log *slog.Logger
}

// New 构造 audit.Service。db 不可为 nil。
func New(db *sql.DB, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: db, log: log}
}

// Entry 是一条审计日志的结构化入参。Payload 是任意可序列化值；
// 调用方负责确保其中不含敏感字段（完整 password / bot_token / body 全文等）。
type Entry struct {
	Actor   string
	Action  string
	Target  string
	Payload any
	Result  string // "ok" / "error"
	Err     string // 失败时的简要错误文本
}

// Log 写一条审计记录。上下文超时/取消会被透传。
// 永远不返回 error——内部只 warn log，避免扰乱业务路径。
func (s *Service) Log(ctx context.Context, e Entry) {
	if s == nil || s.db == nil {
		return
	}
	if e.Result == "" {
		if e.Err == "" {
			e.Result = "ok"
		} else {
			e.Result = "error"
		}
	}
	var payload string
	if e.Payload != nil {
		if raw, err := json.Marshal(e.Payload); err == nil {
			payload = string(raw)
		}
	}
	var errArg any
	if e.Err != "" {
		errArg = e.Err
	}
	var targetArg any
	if e.Target != "" {
		targetArg = e.Target
	}
	var payloadArg any
	if payload != "" {
		payloadArg = payload
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO audit_log(actor, action, target, payload, result, error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Actor, e.Action, targetArg, payloadArg, e.Result, errArg,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		s.log.Warn("audit log write failed",
			"action", e.Action, "actor", e.Actor, "err", err)
	}
}

// Row 是 audit_log 一行的只读表示，供 HTTP 查询。
type Row struct {
	ID        int64           `json:"id"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Target    *string         `json:"target"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Result    string          `json:"result"`
	Error     *string         `json:"error,omitempty"`
	CreatedAt string          `json:"created_at"`
}

// ListFilter 查询过滤器。
type ListFilter struct {
	Actor  string
	Action string
	Limit  int
	Offset int
}

// List 查询审计日志，按 id DESC 排序。
func (s *Service) List(ctx context.Context, f ListFilter) ([]Row, int, error) {
	if s == nil || s.db == nil {
		return nil, 0, nil
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	where := "1=1"
	args := []any{}
	if f.Actor != "" {
		where += " AND actor = ?"
		args = append(args, f.Actor)
	}
	if f.Action != "" {
		where += " AND action = ?"
		args = append(args, f.Action)
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	argsPaged := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, actor, action, target, payload, result, error, created_at
		 FROM audit_log WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		argsPaged...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var payload sql.NullString
		if err := rows.Scan(&r.ID, &r.Actor, &r.Action, &r.Target, &payload,
			&r.Result, &r.Error, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		if payload.Valid && payload.String != "" {
			r.Payload = json.RawMessage(payload.String)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}
