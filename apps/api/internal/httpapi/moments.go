package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

func validMomentInput(input *content.MomentInput) bool {
	input.CityID = strings.TrimSpace(input.CityID)
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	if len(input.CityID) == 0 || len(input.CityID) > 80 ||
		len(input.Title) == 0 || len(input.Title) > 160 ||
		len(input.Body) > 5000 ||
		(input.PlaceID != "" && !uuidPath.MatchString(input.PlaceID)) {
		return false
	}
	switch input.TimePrecision {
	case "unknown":
		if input.OccurredAt != nil {
			return false
		}
	case "year", "month", "day", "instant":
		if input.OccurredAt == nil || input.OccurredAt.After(time.Now().Add(24*time.Hour)) {
			return false
		}
	default:
		return false
	}
	switch input.LocationPrecision {
	case "none", "city":
		return true
	case "place":
		return input.PlaceID != ""
	default:
		return false
	}
}

func (s *server) createMomentDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	var input content.MomentInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !validMomentInput(&input) {
		respondError(w, http.StatusBadRequest, "invalid_moment_draft")
		return
	}
	moment, err := s.content.CreateMomentDraft(r.Context(), actor.ID, input)
	if errors.Is(err, content.ErrConflict) {
		respondError(w, http.StatusConflict, "city_or_place_unavailable")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": moment})
	}
}

func (s *server) listOwnMoments(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	moments, err := s.content.ListOwnMoments(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": moments})
	}
}

func (s *server) getOwnMoment(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("momentID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_moment_id")
		return
	}
	moment, err := s.content.GetOwnMoment(r.Context(), actor.ID, id)
	if errors.Is(err, content.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": moment})
	}
}

func (s *server) updateMomentDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("momentID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_moment_id")
		return
	}
	var request struct {
		content.MomentInput
		Revision int64 `json:"revision"`
	}
	if !decodeStrictJSON(w, r, &request) {
		return
	}
	if request.Revision < 1 || !validMomentInput(&request.MomentInput) {
		respondError(w, http.StatusBadRequest, "invalid_moment_draft")
		return
	}
	moment, err := s.content.UpdateMomentDraft(
		r.Context(), actor.ID, id, request.Revision, request.MomentInput)
	if errors.Is(err, content.ErrConflict) {
		respondError(w, http.StatusConflict, "draft_conflict")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": moment})
	}
}

func (s *server) withdrawMoment(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("momentID")
	revision, parseErr := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if !uuidPath.MatchString(id) || parseErr != nil || revision < 1 {
		respondError(w, http.StatusBadRequest, "invalid_moment_revision")
		return
	}
	err = s.content.WithdrawMoment(r.Context(), actor.ID, id, revision)
	if errors.Is(err, content.ErrConflict) {
		respondError(w, http.StatusConflict, "draft_conflict")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
