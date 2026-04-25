// modems.go —— /api/modems 路由。
package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

func registerModems(r chi.Router, deps Deps) {
	r.Get("/modems", func(w http.ResponseWriter, req *http.Request) {
		rows, err := deps.Store.ListModems(req.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": rows,
			"total": len(rows),
		})
	})

	r.Get("/modems/{device_id}", func(w http.ResponseWriter, req *http.Request) {
		dev := chi.URLParam(req, "device_id")
		m, err := deps.Store.GetModemByDeviceID(req.Context(), dev)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "modem not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, m)
	})

	r.Delete("/modems/{device_id}", func(w http.ResponseWriter, req *http.Request) {
		dev := chi.URLParam(req, "device_id")
		err := deps.Store.DeleteOfflineModem(req.Context(), dev)
		actor := actorFromRequest(req)
		entry := audit.Entry{Actor: actor, Action: "modem.delete", Target: dev}
		if err != nil {
			entry.Result = "error"
			entry.Err = err.Error()
			logAudit(req.Context(), deps, entry)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				writeError(w, http.StatusNotFound, "not_found", "modem not found")
			case errors.Is(err, modem.ErrModemInUse):
				writeError(w, http.StatusConflict, "modem_in_use", err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			}
			return
		}
		entry.Result = "ok"
		logAudit(req.Context(), deps, entry)
		writeJSON(w, http.StatusOK, map[string]string{"message": "modem deleted"})
	})

	// POST /modems/{device_id}/reset —— 异步软复位 modem。
	//  202 + {"message":"reset requested"} 成功提交
	//  501 + {"error":"modem reset not supported","code":"reset_unsupported"} 插件/固件不支持
	//  404 modem 不存在
	//  500 其他底层错误（DBus 断开等）
	r.Post("/modems/{device_id}/reset", func(w http.ResponseWriter, req *http.Request) {
		dev := chi.URLParam(req, "device_id")
		if _, ok := deps.Modem.GetModem(dev); !ok {
			writeError(w, http.StatusNotFound, "not_found", "modem not found")
			return
		}
		if err := deps.Modem.ResetModem(req.Context(), dev); err != nil {
			if errors.Is(err, modem.ErrModemResetUnsupported) {
				logAudit(req.Context(), deps, audit.Entry{
					Actor:  actorFromRequest(req),
					Action: "modem.reset",
					Target: dev,
					Result: "error",
					Err:    "unsupported",
				})
				writeError(w, http.StatusNotImplemented, "reset_unsupported",
					"modem reset not supported")
				return
			}
			logAudit(req.Context(), deps, audit.Entry{
				Actor:  actorFromRequest(req),
				Action: "modem.reset",
				Target: dev,
				Result: "error",
				Err:    err.Error(),
			})
			writeError(w, http.StatusInternalServerError, "reset_failed", err.Error())
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:  actorFromRequest(req),
			Action: "modem.reset",
			Target: dev,
			Result: "ok",
		})
		writeJSON(w, http.StatusAccepted, map[string]string{"message": "reset requested"})
	})

	// PUT /modems/{device_id}/nickname —— 设置用户备注（nickname）。
	// Body: {"nickname":"..."}；空串视作清空。
	// 成功返回 200 + 更新后的 ModemRow。
	r.Put("/modems/{device_id}/nickname", func(w http.ResponseWriter, req *http.Request) {
		dev := chi.URLParam(req, "device_id")
		var body struct {
			Nickname string `json:"nickname"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		nickname := strings.TrimSpace(body.Nickname)
		if err := deps.Store.SetModemNickname(req.Context(), dev, nickname); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "modem not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		updated, err := deps.Store.GetModemByDeviceID(req.Context(), dev)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:   actorFromRequest(req),
			Action:  "modem.nickname",
			Target:  dev,
			Payload: map[string]any{"nickname": nickname},
			Result:  "ok",
		})
		writeJSON(w, http.StatusOK, updated)
	})
}
