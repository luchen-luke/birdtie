package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/safety"
)

func (s *server) createIncidentReport(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || s.safety == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	var input safety.ReportInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.Details = strings.TrimSpace(input.Details)
	if !safety.ValidTarget(input.TargetType, input.TargetID) || !safety.ValidReason(input.Reason) ||
		(input.TargetID != "" && !uuidPath.MatchString(input.TargetID)) ||
		len([]rune(input.Details)) < 10 || len([]rune(input.Details)) > 1000 {
		respondError(w, http.StatusBadRequest, "invalid_report")
		return
	}
	item, err := s.safety.CreateReport(r.Context(), actor.ID, input)
	if errors.Is(err, safety.ErrTargetNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if errors.Is(err, safety.ErrRateLimited) {
		respondError(w, http.StatusTooManyRequests, "report_rate_limited")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": item})
	}
}

func (s *server) listOwnIncidentReports(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || s.safety == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	items, err := s.safety.ListOwnReports(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}
