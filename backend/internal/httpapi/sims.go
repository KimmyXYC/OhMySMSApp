// sims.go —— /api/sims 路由。
package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func registerSims(r chi.Router, deps Deps) {
	r.Get("/sims", func(w http.ResponseWriter, req *http.Request) {
		rows, err := deps.Store.ListSIMs(req.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": rows,
			"total": len(rows),
		})
	})

	r.Get("/sims/{id}", func(w http.ResponseWriter, req *http.Request) {
		idStr := chi.URLParam(req, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid sim id")
			return
		}
		sim, err := deps.Store.GetSIMByID(req.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "sim not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sim)
	})
}
