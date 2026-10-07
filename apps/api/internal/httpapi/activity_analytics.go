package httpapi

import (
	"errors"
	"log"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/analytics"
)

func (s *server) recordActivityView(w http.ResponseWriter, r *http.Request) {
	if s.analytics == nil {
		respondError(w, http.StatusServiceUnavailable, "analytics_unavailable")
		return
	}
	id := r.PathValue("activityID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return
	}
	var input struct {
		EventType   string `json:"eventType"`
		EntrySource string `json:"entrySource"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !analytics.ValidViewEvent(input.EventType, input.EntrySource) {
		respondError(w, http.StatusBadRequest, "invalid_analytics_event")
		return
	}
	err := s.analytics.RecordActivityEvent(r.Context(), id, input.EventType, input.EntrySource, "")
	if errors.Is(err, analytics.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) organizationActivityMetrics(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	id := r.PathValue("organizationID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_organization_id")
		return
	}
	if s.analytics == nil {
		respondError(w, http.StatusServiceUnavailable, "analytics_unavailable")
		return
	}
	items, err := s.analytics.OrganizationActivityMetrics(r.Context(), actor.ID, id)
	if errors.Is(err, analytics.ErrForbidden) {
		respondError(w, http.StatusForbidden, "organization_admin_required")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": items})
	}
}

func (s *server) recordConversion(r *http.Request, activityID, kind, source, key string) {
	if s.analytics == nil {
		return
	}
	if err := s.analytics.RecordActivityEvent(r.Context(), activityID, kind, source, key); err != nil {
		log.Printf("activity analytics conversion failed: %v", err)
	}
}

func conversionSource(r *http.Request, fallback string) string {
	source := r.Header.Get("X-Birdtie-Entry-Source")
	if analytics.ValidSource(source) { return source }
	return fallback
}
