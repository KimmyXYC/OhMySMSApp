// esim.go —— /api/esim 路由。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/esim"
)

func registerESIM(r chi.Router, deps Deps) {
	if deps.ESIM == nil {
		return
	}
	svc := deps.ESIM

	r.Get("/esim/cards", func(w http.ResponseWriter, req *http.Request) {
		cards, err := svc.ListCards(req.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": cards,
			"total": len(cards),
		})
	})

	r.Get("/esim/cards/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
			return
		}
		detail, err := svc.GetCard(req.Context(), id)
		if err != nil {
			writeESIMError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})

	r.Get("/esim/cards/{id}/profiles", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
			return
		}
		profs, err := svc.ListProfiles(req.Context(), id)
		if err != nil {
			writeESIMError(w, err)
			return
		}
		if profs == nil {
			profs = []esim.ESimProfile{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": profs,
			"total": len(profs),
		})
	})

	r.Post("/esim/cards/{id}/discover", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
			return
		}
		detail, err := svc.Discover(req.Context(), id)
		actor := actorFromRequest(req)
		if err != nil {
			logAudit(req.Context(), deps, audit.Entry{
				Actor:  actor,
				Action: "esim.card.discover",
				Target: chi.URLParam(req, "id"),
				Result: "error",
				Err:    err.Error(),
			})
			writeESIMError(w, err)
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:   actor,
			Action:  "esim.card.discover",
			Target:  detail.EID,
			Payload: map[string]any{"card_id": detail.ID, "eid": detail.EID, "profiles": len(detail.Profiles)},
			Result:  "ok",
		})
		writeJSON(w, http.StatusOK, detail)
	})

	r.Post("/esim/cards/{id}/profiles", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
			return
		}
		var body struct {
			ActivationCode   string `json:"activation_code"`
			SMDPAddress      string `json:"smdp_address"`
			MatchingID       string `json:"matching_id"`
			ConfirmationCode string `json:"confirmation_code"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		detail, payload, err := svc.AddProfile(req.Context(), id, esim.AddProfileRequest{
			ActivationCode:   body.ActivationCode,
			SMDPAddress:      body.SMDPAddress,
			MatchingID:       body.MatchingID,
			ConfirmationCode: body.ConfirmationCode,
		})
		actor := actorFromRequest(req)
		entry := audit.Entry{
			Actor:   actor,
			Action:  "esim.profile.add",
			Target:  chi.URLParam(req, "id"),
			Payload: payload,
		}
		if err != nil {
			entry.Result = "error"
			entry.Err = err.Error()
			logAudit(req.Context(), deps, entry)
			writeESIMError(w, err)
			return
		}
		entry.Target = detail.EID
		entry.Result = "ok"
		logAudit(req.Context(), deps, entry)
		writeJSON(w, http.StatusCreated, detail)
	})

	r.Put("/esim/cards/{id}/nickname", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
			return
		}
		var body struct {
			Nickname string `json:"nickname"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		nickname := strings.TrimSpace(body.Nickname)
		if err := svc.SetCardNickname(req.Context(), id, nickname); err != nil {
			writeESIMError(w, err)
			return
		}
		detail, err := svc.GetCard(req.Context(), id)
		if err != nil {
			writeESIMError(w, err)
			return
		}
		logAudit(req.Context(), deps, audit.Entry{
			Actor:   actorFromRequest(req),
			Action:  "esim.card.nickname",
			Target:  detail.EID,
			Payload: map[string]any{"card_id": id, "nickname": nickname},
			Result:  "ok",
		})
		writeJSON(w, http.StatusOK, detail)
	})

	r.Post("/esim/profiles/{iccid}/enable", func(w http.ResponseWriter, req *http.Request) {
		iccid := chi.URLParam(req, "iccid")
		payload, err := svc.EnableProfile(req.Context(), iccid)
		actor := actorFromRequest(req)
		entry := audit.Entry{
			Actor:   actor,
			Action:  "esim.profile.enable",
			Target:  iccid,
			Payload: payload,
		}
		if err != nil {
			entry.Result = "error"
			entry.Err = err.Error()
			logAudit(req.Context(), deps, entry)
			writeESIMError(w, err)
			return
		}
		entry.Result = "ok"
		logAudit(req.Context(), deps, entry)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"message": "profile enable submitted",
			"iccid":   iccid,
		})
	})

	r.Post("/esim/profiles/{iccid}/disable", func(w http.ResponseWriter, req *http.Request) {
		iccid := chi.URLParam(req, "iccid")
		payload, err := svc.DisableProfile(req.Context(), iccid)
		actor := actorFromRequest(req)
		entry := audit.Entry{
			Actor:   actor,
			Action:  "esim.profile.disable",
			Target:  iccid,
			Payload: payload,
		}
		if err != nil {
			entry.Result = "error"
			entry.Err = err.Error()
			logAudit(req.Context(), deps, entry)
			writeESIMError(w, err)
			return
		}
		entry.Result = "ok"
		logAudit(req.Context(), deps, entry)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"message": "profile disable submitted",
			"iccid":   iccid,
		})
	})

	r.Put("/esim/profiles/{iccid}/nickname", func(w http.ResponseWriter, req *http.Request) {
		iccid := chi.URLParam(req, "iccid")
		var body struct {
			Nickname string `json:"nickname"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		nickname := strings.TrimSpace(body.Nickname)
		if nickname == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "nickname must not be empty")
			return
		}
		// lpac profile nickname 不允许空名（清空在 SGP.22 上没有标准方式）
		payload, err := svc.SetProfileNickname(req.Context(), iccid, nickname)
		actor := actorFromRequest(req)
		entry := audit.Entry{
			Actor:   actor,
			Action:  "esim.profile.nickname",
			Target:  iccid,
			Payload: payload,
		}
		if err != nil {
			entry.Result = "error"
			entry.Err = err.Error()
			logAudit(req.Context(), deps, entry)
			writeESIMError(w, err)
			return
		}
		entry.Result = "ok"
		logAudit(req.Context(), deps, entry)
		writeJSON(w, http.StatusOK, map[string]any{
			"message":  "profile nickname updated",
			"iccid":    iccid,
			"nickname": nickname,
		})
	})

	r.Post("/esim/profiles/{iccid}/delete", func(w http.ResponseWriter, req *http.Request) {
		iccid := chi.URLParam(req, "iccid")
		var body struct {
			ConfirmName string `json:"confirm_name"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
			return
		}
		payload, err := svc.DeleteProfile(req.Context(), iccid, strings.TrimSpace(body.ConfirmName))
		actor := actorFromRequest(req)
		entry := audit.Entry{
			Actor:   actor,
			Action:  "esim.profile.delete",
			Target:  iccid,
			Payload: payload,
		}
		if err != nil {
			entry.Result = "error"
			entry.Err = err.Error()
			logAudit(req.Context(), deps, entry)
			writeESIMError(w, err)
			return
		}
		entry.Result = "ok"
		logAudit(req.Context(), deps, entry)
		writeJSON(w, http.StatusOK, map[string]any{
			"message": "profile deleted",
			"iccid":   iccid,
		})
	})
}

// writeESIMError 把 esim.* 错误码映射到 HTTP 状态。
func writeESIMError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, esim.ErrCardNotFound):
		writeError(w, http.StatusNotFound, "card_not_found", err.Error())
	case errors.Is(err, esim.ErrProfileNotFound):
		writeError(w, http.StatusNotFound, "profile_not_found", err.Error())
	case errors.Is(err, esim.ErrLPACUnavailable):
		writeError(w, http.StatusServiceUnavailable, "lpac_unavailable", err.Error())
	case errors.Is(err, esim.ErrTransportUnsupported):
		writeError(w, http.StatusBadRequest, "transport_unsupported", err.Error())
	case errors.Is(err, esim.ErrInhibitFailed):
		writeError(w, http.StatusInternalServerError, "inhibit_failed", err.Error())
	case errors.Is(err, esim.ErrNoChangeNeeded):
		writeError(w, http.StatusConflict, "no_change_needed", err.Error())
	case errors.Is(err, esim.ErrModemNotBound):
		writeError(w, http.StatusConflict, "modem_not_bound", err.Error())
	case errors.Is(err, esim.ErrModemOffline):
		writeError(w, http.StatusConflict, "modem_offline", err.Error())
	case errors.Is(err, esim.ErrProfileActive):
		writeError(w, http.StatusConflict, "profile_active", err.Error())
	case errors.Is(err, esim.ErrInvalidProfileInput):
		writeError(w, http.StatusBadRequest, "invalid_profile_input", err.Error())
	case errors.Is(err, esim.ErrLPACError):
		var lerr *esim.LPACError
		if errors.As(err, &lerr) {
			status := http.StatusInternalServerError
			detail := strings.ToLower(lerr.Detail + " " + lerr.Stderr)
			switch {
			case strings.Contains(detail, "activation") || strings.Contains(detail, "confirmation") || strings.Contains(detail, "invalid") || strings.Contains(detail, "format"):
				status = http.StatusBadRequest
			case strings.Contains(detail, "already") || strings.Contains(detail, "not in disabled") || strings.Contains(detail, "insufficient") || strings.Contains(detail, "no space"):
				status = http.StatusConflict
			case strings.Contains(detail, "network") || strings.Contains(detail, "connect") || strings.Contains(detail, "timeout") || strings.Contains(detail, "smdp") || strings.Contains(detail, "server"):
				status = http.StatusBadGateway
			}
			writeError(w, status, "lpac_error", lerr.Detail)
			return
		}
		writeError(w, http.StatusInternalServerError, "lpac_error", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
