package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

func mapLocationError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, organization.ErrForbidden):
		respondError(w, http.StatusForbidden, "organization_map_forbidden")
	case errors.Is(err, organization.ErrNotFound):
		respondError(w, http.StatusNotFound, "organization_map_not_found")
	case errors.Is(err, organization.ErrConflict):
		respondError(w, http.StatusConflict, "organization_map_conflict")
	default:
		serverError(w, err)
	}
	return true
}

func (s *server) listPublicOrganizationMapPins(w http.ResponseWriter, r *http.Request) {
	if s.mapLocations == nil {
		respondError(w, http.StatusServiceUnavailable, "organization_map_unavailable")
		return
	}
	cityID := r.PathValue("cityID")
	if _, err := s.catalog.GetCity(r.Context(), cityID); handleReadError(w, err) {
		return
	}
	pins, err := s.mapLocations.ListPublicMapPins(r.Context(), cityID)
	if mapLocationError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": pins})
}

func (s *server) mapLocationActor(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	actorID, ok := s.memberActor(w, r)
	if !ok {
		return "", "", false
	}
	orgID := organizationPath(w, r)
	if orgID == "" || s.mapLocations == nil {
		return "", "", false
	}
	return actorID, orgID, true
}

func (s *server) getOrganizationMapLocation(w http.ResponseWriter, r *http.Request) {
	actorID, orgID, ok := s.mapLocationActor(w, r)
	if !ok {
		return
	}
	v, err := s.mapLocations.GetMapLocation(r.Context(), actorID, orgID)
	if mapLocationError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": v})
}

func (s *server) submitOrganizationMapLocation(w http.ResponseWriter, r *http.Request) {
	actorID, orgID, ok := s.mapLocationActor(w, r)
	if !ok {
		return
	}
	var input struct {
		CityID    string  `json:"cityId"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if input.CityID == "" || math.IsNaN(input.Latitude) || math.IsInf(input.Latitude, 0) ||
		math.IsNaN(input.Longitude) || math.IsInf(input.Longitude, 0) ||
		input.Latitude < -90 || input.Latitude > 90 || input.Longitude < -180 || input.Longitude > 180 {
		respondError(w, http.StatusBadRequest, "invalid_organization_map_point")
		return
	}
	if _, err := s.catalog.GetCity(r.Context(), input.CityID); handleReadError(w, err) {
		return
	}
	v, err := s.mapLocations.SubmitMapLocation(r.Context(), actorID, orgID, input.CityID, input.Latitude, input.Longitude)
	if mapLocationError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": v})
}

func (s *server) hideOrganizationMapLocation(w http.ResponseWriter, r *http.Request) {
	actorID, orgID, ok := s.mapLocationActor(w, r)
	if !ok {
		return
	}
	if mapLocationError(w, s.mapLocations.HideMapLocation(r.Context(), actorID, orgID)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) reviewOrganizationMapLocation(w http.ResponseWriter, r *http.Request) {
	actorID, orgID, ok := s.mapLocationActor(w, r)
	if !ok {
		return
	}
	var input struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.Note = strings.TrimSpace(input.Note)
	if (input.Decision != "approve" && input.Decision != "reject") || len([]rune(input.Note)) > 1000 {
		respondError(w, http.StatusBadRequest, "invalid_organization_map_review")
		return
	}
	if mapLocationError(w, s.mapLocations.ReviewMapLocation(r.Context(), actorID, orgID, input.Decision, input.Note)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
