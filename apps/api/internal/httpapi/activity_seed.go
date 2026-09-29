package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
)

func validActivityCandidate(input *cityseed.ActivityInput) bool {
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.HostLabel = strings.TrimSpace(input.HostLabel)
	input.TimeZone = strings.TrimSpace(input.TimeZone)
	input.SourceLabel = strings.TrimSpace(input.SourceLabel)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	input.RightsNote = strings.TrimSpace(input.RightsNote)
	if input.PlaceID != "" && !uuidPath.MatchString(input.PlaceID) {
		return false
	}
	source, err := url.Parse(input.SourceURL)
	if err != nil || source.Scheme != "https" || source.Host == "" || source.User != nil {
		return false
	}
	if _, err := time.LoadLocation(input.TimeZone); err != nil {
		return false
	}
	now := time.Now()
	return len(input.Title) >= 1 && len(input.Title) <= 160 &&
		len(input.Summary) <= 3000 && len(input.HostLabel) >= 1 &&
		len(input.HostLabel) <= 160 && len(input.TimeZone) <= 80 &&
		len(input.SourceLabel) >= 1 && len(input.SourceLabel) <= 120 &&
		len(input.SourceURL) <= 1000 && len(input.RightsNote) >= 10 &&
		len(input.RightsNote) <= 1000 && !input.StartsAt.IsZero() &&
		input.EndsAt.After(input.StartsAt) &&
		input.EndsAt.Sub(input.StartsAt) <= 31*24*time.Hour &&
		input.ExpiresAt.After(now.Add(time.Hour)) &&
		input.ExpiresAt.Before(now.Add(365*24*time.Hour))
}

func (s *server) submitActivityCandidate(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	if cityID == "" || len(cityID) > 80 {
		respondError(w, http.StatusBadRequest, "invalid_city_id")
		return
	}
	var input cityseed.ActivityInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !validActivityCandidate(&input) {
		respondError(w, http.StatusBadRequest, "invalid_activity_candidate")
		return
	}
	candidate, err := s.seed.SubmitActivity(r.Context(), actor.ID, cityID, input)
	switch {
	case errors.Is(err, cityseed.ErrForbidden):
		respondError(w, http.StatusForbidden, "city_editor_required")
	case errors.Is(err, cityseed.ErrConflict):
		respondError(w, http.StatusConflict, "place_unavailable")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, http.StatusCreated, map[string]any{"data": candidate})
	}
}

func (s *server) listActivityCandidates(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	candidates, err := s.seed.ListActivityCandidates(
		r.Context(), actor.ID, r.PathValue("cityID"))
	if errors.Is(err, cityseed.ErrForbidden) {
		respondError(w, http.StatusForbidden, "city_editor_required")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": candidates})
	}
}

func (s *server) reviewActivityCandidate(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	candidateID := r.PathValue("candidateID")
	if !uuidPath.MatchString(candidateID) {
		respondError(w, http.StatusBadRequest, "invalid_candidate_id")
		return
	}
	var input cityseed.ActivityReviewInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.Note = strings.TrimSpace(input.Note)
	if len(input.Note) < 10 || len(input.Note) > 1000 ||
		(input.Decision != "publish" && input.Decision != "reject") {
		respondError(w, http.StatusBadRequest, "invalid_review")
		return
	}
	candidate, err := s.seed.ReviewActivity(r.Context(), actor.ID, candidateID, input)
	switch {
	case errors.Is(err, cityseed.ErrForbidden):
		respondError(w, http.StatusForbidden, "reviewer_required")
	case errors.Is(err, cityseed.ErrConflict):
		respondError(w, http.StatusConflict, "review_conflict")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, http.StatusOK, map[string]any{"data": candidate})
	}
}
