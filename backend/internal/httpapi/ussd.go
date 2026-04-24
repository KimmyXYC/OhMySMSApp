// ussd.go —— /api/ussd 路由。
package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

type ussdInitReq struct {
	DeviceID string `json:"device_id"`
	ModemID  int64  `json:"modem_id"`
	Command  string `json:"command"`
}

type ussdReplyReq struct {
	Response string `json:"response"`
}

type ussdResponse struct {
	SessionID       string `json:"session_id"`
	Reply           string `json:"reply"`
	State           string `json:"state"`
	NetworkRequest  string `json:"network_request,omitempty"`
	NetworkNotif    string `json:"network_notification,omitempty"`
	DeviceID        string `json:"device_id,omitempty"`
	SessionRowID    int64  `json:"session_row_id,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
}

func registerUSSD(r chi.Router, deps Deps) {
	r.Post("/ussd", func(w http.ResponseWriter, req *http.Request) {
		var body ussdInitReq
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		if body.Command == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "command is required")
			return
		}
		deviceID := body.DeviceID
		if deviceID == "" && body.ModemID > 0 {
			m, err := deps.Store.GetModemByID(req.Context(), body.ModemID)
			if err != nil {
				writeError(w, http.StatusNotFound, "not_found", "modem not found")
				return
			}
			deviceID = m.DeviceID
		}
		if deviceID == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "device_id or modem_id required")
			return
		}

		sid, reply, err := deps.Modem.InitiateUSSD(req.Context(), deviceID, body.Command)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ussd_failed", err.Error())
			return
		}
		// 简化：MM event 的 state 会被 Runner 落库；我们无法精准拿到
		// "是否需要 respond"。provider.InitiateUSSD 的返回值没有带 state，
		// 因此这里返回 state=terminated（最常见）。前端发现真的是 user_response
		// 时会通过 WS 再收到 ussd.state 事件。
		resp := ussdResponse{
			SessionID: sid,
			Reply:     reply,
			State:     "terminated",
			DeviceID:  deviceID,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
		writeJSON(w, http.StatusOK, resp)
	})

	r.Post("/ussd/{session_id}/respond", func(w http.ResponseWriter, req *http.Request) {
		sid := chi.URLParam(req, "session_id")
		var body ussdReplyReq
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		if body.Response == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "response is required")
			return
		}
		reply, err := deps.Modem.RespondUSSD(req.Context(), sid, body.Response)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ussd_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, ussdResponse{
			SessionID: sid, Reply: reply, State: "terminated",
		})
	})

	r.Delete("/ussd/{session_id}", func(w http.ResponseWriter, req *http.Request) {
		sid := chi.URLParam(req, "session_id")
		if err := deps.Modem.CancelUSSD(req.Context(), sid); err != nil {
			writeError(w, http.StatusInternalServerError, "ussd_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/ussd/sessions", func(w http.ResponseWriter, req *http.Request) {
		limit := 100
		if v := req.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		rows, err := deps.Store.ListUSSDSessions(req.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": rows,
			"total": len(rows),
		})
	})

	r.Get("/ussd/sessions/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid session id")
			return
		}
		row, err := deps.Store.GetUSSDSessionByID(req.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "session not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}
