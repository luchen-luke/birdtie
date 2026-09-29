package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/community"
)

func validCommunityInput(input *community.Input) bool {
	input.Name = strings.TrimSpace(input.Name)
	input.Summary = strings.TrimSpace(input.Summary)
	input.PlaceID = strings.TrimSpace(input.PlaceID)
	input.SourceLabel = strings.TrimSpace(input.SourceLabel)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	input.RightsNote = strings.TrimSpace(input.RightsNote)
	if input.PlaceID != "" && !uuidPath.MatchString(input.PlaceID) {
		return false
	}
	if input.SourceURL != "" {
		source, err := url.Parse(input.SourceURL)
		if err != nil || source.Scheme != "https" || source.Host == "" || source.User != nil {
			return false
		}
	}
	now := time.Now()
	return len(input.Name) >= 1 && len(input.Name) <= 160 &&
		len(input.Summary) <= 3000 && len(input.SourceLabel) <= 120 &&
		(input.SourceURL == "" || input.SourceLabel != "") &&
		len(input.SourceURL) <= 1000 && len(input.RightsNote) <= 1000 &&
		input.ExpiresAt.After(now.Add(time.Hour)) &&
		input.ExpiresAt.Before(now.Add(365*24*time.Hour))
}

func (s *server) submitCommunity(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	if cityID == "" || len(cityID) > 80 {
		respondError(w, http.StatusBadRequest, "invalid_city_id")
		return
	}
	var input community.Input
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !validCommunityInput(&input) {
		respondError(w, http.StatusBadRequest, "invalid_community")
		return
	}
	record, err := s.communities.SubmitCommunity(r.Context(), actor.ID, cityID, input)
	switch {
	case errors.Is(err, community.ErrForbidden):
		respondError(w, http.StatusForbidden, "city_unavailable")
	case errors.Is(err, community.ErrConflict):
		respondError(w, http.StatusConflict, "place_unavailable")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, http.StatusCreated, map[string]any{"data": record})
	}
}

func (s *server) listOwnCommunities(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	records, err := s.communities.ListOwnCommunities(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": records})
}

func (s *server) withdrawCommunity(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("communityID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_community_id")
		return
	}
	err = s.communities.WithdrawCommunity(r.Context(), actor.ID, id)
	if errors.Is(err, community.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
