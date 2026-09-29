package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/intent"
)

func validIntentInput(input *intent.Input) bool {
	input.Topic = strings.TrimSpace(input.Topic)
	input.Details = strings.TrimSpace(input.Details)
	input.TimeZone = strings.TrimSpace(input.TimeZone)
	input.CoarseAreaLabel = strings.TrimSpace(input.CoarseAreaLabel)
	input.PublicMapZone = strings.TrimSpace(input.PublicMapZone)
	if input.PublicMapZone != "" && input.PublicMapZone != "city_centre" &&
		input.PublicMapZone != "north" && input.PublicMapZone != "south" &&
		input.PublicMapZone != "east" && input.PublicMapZone != "west" {
		return false
	}
	now := time.Now()
	if !input.Confirmed || len(input.Topic) == 0 || len(input.Topic) > 160 ||
		len(input.Details) > 3000 || len(input.TimeZone) == 0 ||
		len(input.TimeZone) > 80 || len(input.CoarseAreaLabel) == 0 ||
		len(input.CoarseAreaLabel) > 160 ||
		input.AvailableFrom.Before(now.Add(-time.Hour)) ||
		input.AvailableFrom.After(now.Add(30*24*time.Hour)) ||
		!input.AvailableUntil.After(input.AvailableFrom) ||
		input.AvailableUntil.After(now.Add(31*24*time.Hour)) ||
		!input.ExpiresAt.After(now.Add(time.Hour)) ||
		input.ExpiresAt.After(input.AvailableUntil) {
		return false
	}
	_, err := time.LoadLocation(input.TimeZone)
	return err == nil
}

func (s *server) submitIntent(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	if cityID == "" || len(cityID) > 80 {
		respondError(w, http.StatusBadRequest, "invalid_city_id")
		return
	}
	var input intent.Input
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !validIntentInput(&input) {
		respondError(w, http.StatusBadRequest, "invalid_intent")
		return
	}
	record, err := s.intents.SubmitIntent(r.Context(), actor.ID, cityID, input)
	switch {
	case errors.Is(err, intent.ErrForbidden):
		respondError(w, http.StatusForbidden, "city_unavailable")
	case errors.Is(err, intent.ErrConflict):
		respondError(w, http.StatusConflict, "public_profile_or_limit_required")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, http.StatusCreated, map[string]any{"data": record})
	}
}

func (s *server) listOwnIntents(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	records, err := s.intents.ListOwnIntents(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": records})
	}
}

func (s *server) withdrawIntent(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("intentID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_intent_id")
		return
	}
	err = s.intents.WithdrawIntent(r.Context(), actor.ID, id)
	if errors.Is(err, intent.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
