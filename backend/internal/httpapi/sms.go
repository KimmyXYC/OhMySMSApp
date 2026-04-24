// sms.go —— /api/sms 路由：列表 / 会话 / 发送 / 删除。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

type smsSendRequest struct {
	DeviceID string `json:"device_id"` // 优先
	ModemID  int64  `json:"modem_id"`  // 次选；用 DB id 映射到 device_id
	Peer     string `json:"peer"`
	Body     string `json:"body"`
}

func registerSMS(r chi.Router, deps Deps) {
	r.Get("/sms", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		f := modem.SMSFilter{
			DeviceID:  q.Get("device_id"),
			Direction: q.Get("direction"),
			Peer:      q.Get("peer"),
		}
		if v := q.Get("sim_id"); v != "" {
			f.SimID, _ = strconv.ParseInt(v, 10, 64)
		}
		if v := q.Get("modem_id"); v != "" {
			f.ModemID, _ = strconv.ParseInt(v, 10, 64)
		}
		if v := q.Get("limit"); v != "" {
			f.Limit, _ = strconv.Atoi(v)
		}
		if v := q.Get("offset"); v != "" {
			f.Offset, _ = strconv.Atoi(v)
		}
		if v := q.Get("since"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				f.Since = t
			}
		}

		rows, total, err := deps.Store.ListSMS(req.Context(), f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": rows,
			"total": total,
		})
	})

	r.Get("/sms/threads", func(w http.ResponseWriter, req *http.Request) {
		var simID int64
		if v := req.URL.Query().Get("sim_id"); v != "" {
			simID, _ = strconv.ParseInt(v, 10, 64)
		}
		rows, err := deps.Store.ListSMSThreads(req.Context(), simID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": rows,
			"total": len(rows),
		})
	})

	r.Post("/sms/send", func(w http.ResponseWriter, req *http.Request) {
		var body smsSendRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		peer := body.Peer
		text := body.Body
		if peer == "" || text == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "peer and body are required")
			return
		}

		// 解析 device_id（优先）或 modem_id
		deviceID := body.DeviceID
		if deviceID == "" && body.ModemID > 0 {
			m, err := deps.Store.GetModemByID(req.Context(), body.ModemID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					writeError(w, http.StatusNotFound, "not_found", "modem not found")
					return
				}
				writeError(w, http.StatusInternalServerError, "db_error", err.Error())
				return
			}
			deviceID = m.DeviceID
		}
		if deviceID == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "device_id or modem_id required")
			return
		}

		extID, err := deps.Modem.SendSMS(req.Context(), deviceID, peer, text)
		if err != nil {
			logAudit(req.Context(), deps, audit.Entry{
				Actor:  actorFromRequest(req),
				Action: "sms.send",
				Target: deviceID,
				Payload: map[string]any{
					"peer":     peer,
					"body_len": utf8.RuneCountInString(text),
				},
				Result: "error",
				Err:    err.Error(),
			})
			writeError(w, http.StatusInternalServerError, "send_failed", err.Error())
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:  actorFromRequest(req),
			Action: "sms.send",
			Target: deviceID,
			Payload: map[string]any{
				"peer":     peer,
				"body_len": utf8.RuneCountInString(text),
				"ext_id":   extID,
			},
			Result: "ok",
		})

		// 构造返回：最佳 effort 查询刚刚被 Runner 写入的行（可能还没入库）。
		// 先尝试根据 ext_id 查一下；拿不到就返回一个最小 shape。
		time.Sleep(30 * time.Millisecond) // 给 runner 一点时间（mock 会走 event）

		// 用 ext_id 精确查
		if row := findSMSByExtID(req.Context(), deps, extID); row != nil {
			writeJSON(w, http.StatusOK, row)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ext_id":    extID,
			"direction": "outbound",
			"state":     "sent",
			"peer":      peer,
			"body":      text,
		})
	})

	r.Delete("/sms/{id}", func(w http.ResponseWriter, req *http.Request) {
		idStr := chi.URLParam(req, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid sms id")
			return
		}
		// 先取行（拿 ext_id + modem device）以便告知 provider 删除
		rec, err := deps.Store.GetSMSByID(req.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "sms not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		// 尝试删 provider 侧（best effort）
		if rec.ExtID != nil && rec.ModemID != nil {
			m, err := deps.Store.GetModemByID(req.Context(), *rec.ModemID)
			if err == nil && m != nil {
				_ = deps.Modem.DeleteSMS(req.Context(), m.DeviceID, *rec.ExtID)
			}
		}
		if err := deps.Store.DeleteSMSByID(req.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:  actorFromRequest(req),
			Action: "sms.delete",
			Target: strconv.FormatInt(id, 10),
			Payload: map[string]any{
				"peer":      rec.Peer,
				"direction": rec.Direction,
			},
			Result: "ok",
		})
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
}

// findSMSByExtID 根据 ext_id 查最近一条匹配。
func findSMSByExtID(ctx context.Context, deps Deps, extID string) *modem.SMSRow {
	rows, _, err := deps.Store.ListSMS(ctx, modem.SMSFilter{Limit: 10})
	if err != nil {
		return nil
	}
	for i := range rows {
		if rows[i].ExtID != nil && *rows[i].ExtID == extID {
			return &rows[i]
		}
	}
	return nil
}
