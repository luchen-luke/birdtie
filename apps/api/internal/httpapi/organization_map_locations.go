package httpapi

import (
	"errors"
	"net/http"

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
	a, p, id, ok := s.orgControlAccess(w, r)
	if !ok || !orgControlNoBody(w, r) {
		return
	}
	out, e := p.ReadMapControl(r.Context(), a, id)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	if out.Canonical == nil {
		orgControlFailure(w, organization.ErrNotFound)
		return
	}
	orgControlRespond(w, r, p, a, id, out.Proof, out.ExpiresAt, out.Canonical)
}

func (s *server) submitOrganizationMapLocation(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.memberActor(w, r); !ok {
		return
	}
	if organizationPath(w, r) == "" {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondError(w, 409, "organization_control_required")
}

func (s *server) hideOrganizationMapLocation(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.memberActor(w, r); !ok {
		return
	}
	if organizationPath(w, r) == "" {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondError(w, 409, "organization_control_required")
}

func (s *server) reviewOrganizationMapLocation(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.memberActor(w, r); !ok {
		return
	}
	if organizationPath(w, r) == "" {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondError(w, 409, "organization_control_required")
}
