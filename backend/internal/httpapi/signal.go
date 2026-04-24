// signal.go —— /api/signal 路由。
package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

func registerSignal(r chi.Router, deps Deps) {
	r.Get("/signal/{device_id}/history", func(w http.ResponseWriter, req *http.Request) {
		deviceID := chi.URLParam(req, "device_id")
		m, err := deps.Store.GetModemByDeviceID(req.Context(), deviceID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "modem not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		limit := 60
		if v := req.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		var since time.Time
		if v := req.URL.Query().Get("since"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				since = t
			}
		}
		rows, err := deps.Store.ListSignalHistory(req.Context(), m.ID, since, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": rows,
			"total": len(rows),
		})
	})
}
