package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
)

// This source supplies candidate facts to the original editor/reviewer flow.
// It is not a model provider, an approval, or a parallel public Place catalog.
type OpenPlaceAcquirer interface {
	Search(context.Context, agenttool.OpenPlaceQuery) ([]cityseed.SubmitInput, error)
}

func WithOpenPlaceAcquirer(source OpenPlaceAcquirer) Option {
	return func(s *server) { s.openPlaces = source }
}

func (s *server) acquireOpenPlaceCandidates(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "city_editor_required")
		return
	}
	if s.openPlaces == nil || s.seed == nil || s.catalog == nil {
		respondError(w, http.StatusServiceUnavailable, "open_place_source_unavailable")
		return
	}
	var input struct {
		Category string `json:"category"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	switch input.Category {
	case "museum", "restaurant", "cafe", "park":
	default:
		respondError(w, http.StatusBadRequest, "unsupported_place_category")
		return
	}
	cityID := r.PathValue("cityID")
	if len(cityID) == 0 || len(cityID) > 80 {
		respondError(w, http.StatusBadRequest, "invalid_city_id")
		return
	}
	// Read the existing editor boundary before any network dispatch. Submit
	// rechecks the native session, city membership and rights at each write.
	_, err = s.seed.List(r.Context(), actor.ID, cityID)
	if errors.Is(err, cityseed.ErrForbidden) {
		respondError(w, http.StatusForbidden, "city_editor_required")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	city, err := s.catalog.GetCity(r.Context(), cityID)
	if handleReadError(w, err) {
		return
	}
	if city.Map == nil {
		respondError(w, http.StatusUnprocessableEntity, "city_map_scope_unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	facts, err := s.openPlaces.Search(ctx, agenttool.OpenPlaceQuery{
		Latitude: city.Map.Latitude, Longitude: city.Map.Longitude,
		RadiusMeters: 6000, Category: input.Category,
	})
	if err != nil {
		respondError(w, http.StatusBadGateway, "open_place_source_failed")
		return
	}
	if len(facts) > 15 {
		respondError(w, http.StatusBadGateway, "open_place_source_failed")
		return
	}
	// Validate the entire bounded response before the first write. No arbitrary
	// provider URL, coordinates, licence or fact payload is accepted from HTTP.
	for _, fact := range facts {
		if !validCandidate(&fact) {
			respondError(w, http.StatusBadGateway, "open_place_fact_invalid")
			return
		}
		if _, _, err := cityseed.NormalizeSourcePermission(fact, time.Now().UTC()); err != nil {
			respondError(w, http.StatusBadGateway, "open_place_fact_invalid")
			return
		}
	}
	rows := make([]cityseed.Candidate, 0, len(facts))
	duplicates, reusedCurrent, notAttempted := 0, 0, 0
	var unresolved map[string]any
	for i, fact := range facts {
		candidate, err := s.seed.Submit(cityseed.WithHumanSession(ctx, actor.ID, digest), actor.ID, cityID, fact)
		if errors.Is(err, cityseed.ErrDuplicate) {
			duplicates++
			if errors.Is(err, cityseed.ErrSourceAlreadyCurrent) {
				reusedCurrent++
			}
			continue
		}
		if err != nil {
			// Earlier native transactions may have committed. Return their IDs so
			// a partial batch is never presented as rolled back or fully imported.
			notAttempted = len(facts) - i - 1
			outcome, code := "UNKNOWN", "candidate_submit_unconfirmed"
			if errors.Is(err, cityseed.ErrForbidden) {
				outcome, code = "NOT_WRITTEN", "city_editor_required"
			}
			if errors.Is(err, cityseed.ErrSourcePermission) {
				outcome, code = "NOT_WRITTEN", "place_source_permission_unconfirmed"
			}
			unresolved = map[string]any{"provider": fact.ProviderCode, "providerPlaceId": fact.ProviderPlaceID, "outcome": outcome, "code": code}
			break
		}
		rows = append(rows, candidate)
	}
	status := "PENDING_REVIEW"
	if len(facts) > 0 && reusedCurrent == len(facts) {
		status = "REUSED_CURRENT"
	}
	if unresolved != nil {
		status = "PARTIAL_PENDING_REVIEW"
	}
	respond(w, http.StatusOK, map[string]any{"data": rows, "status": status,
		"duplicates": duplicates, "reusedCurrent": reusedCurrent, "notAttempted": notAttempted, "unresolved": unresolved,
		"provider": "openstreetmap", "attribution": "© OpenStreetMap contributors",
		"licenseUrl": "https://www.openstreetmap.org/copyright"})
}
