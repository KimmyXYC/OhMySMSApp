// modems.go —— /api/modems 路由。
package httpapi

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
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

	// reset 在阶段 3 先占位，调用 provider 无现成方法；返回 501 但日志可观测。
	r.Post("/modems/{device_id}/reset", func(w http.ResponseWriter, req *http.Request) {
		dev := chi.URLParam(req, "device_id")
		if _, ok := deps.Modem.GetModem(dev); !ok {
			writeError(w, http.StatusNotFound, "not_found", "modem not found")
			return
		}
		// TODO(stage-?): 对接 provider.Reset 或 mmcli --reset
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"modem reset not implemented yet")
	})
}
