// sims.go —— /api/sims 路由。
package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
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

	// PUT /sims/{id}/msisdn —— 设置 SIM 本地显示号码覆盖值。
	// Body: {"msisdn":"..."}；空串或全空白视作清空覆盖值。
	// 成功返回 200 + 更新后的 SimRow。该号码仅用于本地展示，不写入 SIM 卡。
	r.Put("/sims/{id}/msisdn", func(w http.ResponseWriter, req *http.Request) {
		idStr := chi.URLParam(req, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid sim id")
			return
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		rawMSISDN, ok := body["msisdn"]
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "msisdn must be provided as a string")
			return
		}
		var msisdn *string
		if err := json.Unmarshal(rawMSISDN, &msisdn); err != nil || msisdn == nil {
			writeError(w, http.StatusBadRequest, "bad_request", "msisdn must be provided as a string")
			return
		}

		if err := deps.Store.SetSIMMSISDNOverride(req.Context(), id, *msisdn); err != nil {
			logAudit(req.Context(), deps, audit.Entry{
				Actor:  actorFromRequest(req),
				Action: "sim.msisdn_override",
				Target: idStr,
				Result: "error",
				Err:    err.Error(),
			})
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "sim not found")
				return
			}
			if errors.Is(err, modem.ErrInvalidMSISDNOverride) {
				writeError(w, http.StatusBadRequest, "bad_request", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		updated, err := deps.Store.GetSIMByID(req.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:  actorFromRequest(req),
			Action: "sim.msisdn_override",
			Target: idStr,
			Payload: map[string]any{
				"msisdn_override":      updated.MSISDNOverride,
				"local_display_number": true,
				"note":                 "local display number only; not written to SIM",
			},
			Result: "ok",
		})
		writeJSON(w, http.StatusOK, updated)
	})

	r.Delete("/sims/{id}", func(w http.ResponseWriter, req *http.Request) {
		idStr := chi.URLParam(req, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid sim id")
			return
		}
		err = deps.Store.DeleteUnusedSIM(req.Context(), id)
		entry := audit.Entry{Actor: actorFromRequest(req), Action: "sim.delete", Target: idStr}
		if err != nil {
			entry.Result = "error"
			entry.Err = err.Error()
			logAudit(req.Context(), deps, entry)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				writeError(w, http.StatusNotFound, "not_found", "sim not found")
			case errors.Is(err, modem.ErrSIMInUse):
				writeError(w, http.StatusConflict, "sim_in_use", err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			}
			return
		}
		entry.Result = "ok"
		logAudit(req.Context(), deps, entry)
		writeJSON(w, http.StatusOK, map[string]string{"message": "sim deleted"})
	})
}
