package httpapi

import (
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type agentProfileVisibilityStore interface {
	ReadOwnAgentProfileVisibility(context.Context, agentprofile.PrivateAccess) (agentprofile.VisibilityRecord, error)
	ReplaceOwnAgentProfileVisibility(context.Context, agentprofile.PrivateAccess, agentprofile.ReplaceVisibilityInput) (agentprofile.VisibilityRecord, error)
	ReadAgentProfileFields(context.Context, [32]byte, string) (agentprofile.ProjectedRecord, error)
}

func profileVisibilityFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentprofile.ErrInvalid):
		respondError(w, http.StatusBadRequest, "invalid_profile_field_visibility")
	case errors.Is(err, agentprofile.ErrConflict):
		respondError(w, http.StatusConflict, "profile_field_visibility_version_conflict")
	case errors.Is(err, agentprofile.ErrForbidden):
		respondError(w, http.StatusForbidden, "profile_field_visibility_forbidden")
	case errors.Is(err, agentprofile.ErrNotFound):
		respondError(w, http.StatusNotFound, "not_found")
	default:
		log.Printf("request_id=%s profile_field_visibility_error category=unavailable", w.Header().Get("X-Request-ID"))
		respondError(w, http.StatusServiceUnavailable, "profile_field_visibility_unavailable")
	}
}

// Human privacy control. This gateway does not issue model/Memory permissions.
func (s *server) ownProfileVisibilityAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		profileVisibilityFailure(w, agentprofile.ErrForbidden)
		return agentprofile.PrivateAccess{}, false
	}
	if s.profileVisibility == nil || s.access == nil {
		profileVisibilityFailure(w, agentprofile.ErrUnavailable)
		return agentprofile.PrivateAccess{}, false
	}
	actor, digest, err := s.actor(r, true)
	if errors.Is(err, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return agentprofile.PrivateAccess{}, false
	}
	if err != nil {
		profileVisibilityFailure(w, err)
		return agentprofile.PrivateAccess{}, false
	}
	principal, err := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if err != nil || principal.Type != actorref.Person {
		profileVisibilityFailure(w, agentprofile.ErrForbidden)
		return agentprofile.PrivateAccess{}, false
	}
	if r.URL.RawQuery != "" {
		profileVisibilityFailure(w, agentprofile.ErrInvalid)
		return agentprofile.PrivateAccess{}, false
	}
	access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		profileVisibilityFailure(w, agentprofile.ErrForbidden)
		return agentprofile.PrivateAccess{}, false
	}
	return access, true
}

func visibilityResponse(w http.ResponseWriter, access agentprofile.PrivateAccess, record agentprofile.VisibilityRecord) {
	if agentprofile.ValidateVisibilityRecord(record) != nil || record.Profile.OwnerID != access.WorkspacePrincipal.ID {
		profileVisibilityFailure(w, agentprofile.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": record})
}

func profileVisibilityEmptyBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 {
		profileVisibilityFailure(w, agentprofile.ErrInvalid)
		return false
	}
	return true
}

func (s *server) ownProfileVisibility(w http.ResponseWriter, r *http.Request) {
	access, ok := s.ownProfileVisibilityAccess(w, r)
	if !ok || !profileVisibilityEmptyBody(w, r) {
		return
	}
	record, err := s.profileVisibility.ReadOwnAgentProfileVisibility(r.Context(), access)
	if err != nil {
		profileVisibilityFailure(w, err)
		return
	}
	visibilityResponse(w, access, record)
}

func (s *server) replaceProfileVisibility(w http.ResponseWriter, r *http.Request) {
	access, ok := s.ownProfileVisibilityAccess(w, r)
	if !ok {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	if r.Body == nil {
		profileVisibilityFailure(w, agentprofile.ErrInvalid)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, agentprofile.MaxPrivateBodyBytes+1))
	if err != nil || len(body) > agentprofile.MaxPrivateBodyBytes {
		respondError(w, http.StatusRequestEntityTooLarge, "profile_field_visibility_body_too_large")
		return
	}
	input, err := agentprofile.DecodeReplaceVisibilityInput(body)
	if err != nil {
		profileVisibilityFailure(w, err)
		return
	}
	record, err := s.profileVisibility.ReplaceOwnAgentProfileVisibility(r.Context(), access, input)
	if err != nil {
		profileVisibilityFailure(w, err)
		return
	}
	visibilityResponse(w, access, record)
}

func (s *server) profileFields(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		profileVisibilityFailure(w, agentprofile.ErrForbidden)
		return
	}
	if s.profileVisibility == nil || s.access == nil {
		profileVisibilityFailure(w, agentprofile.ErrUnavailable)
		return
	}
	id := r.PathValue("accountID")
	principal, err := actorref.ParsePrincipal("PERSON", id)
	if err != nil || principal.ID == "00000000-0000-0000-0000-000000000000" || r.URL.RawQuery != "" {
		profileVisibilityFailure(w, agentprofile.ErrInvalid)
		return
	}
	if !profileVisibilityEmptyBody(w, r) {
		return
	}
	_, digest, err := s.actor(r, false)
	if errors.Is(err, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err != nil {
		profileVisibilityFailure(w, err)
		return
	}
	record, err := s.profileVisibility.ReadAgentProfileFields(r.Context(), digest, principal.ID)
	if err != nil {
		profileVisibilityFailure(w, err)
		return
	}
	if agentprofile.ValidateProjectedRecord(record) != nil || record.AccountID != principal.ID {
		profileVisibilityFailure(w, agentprofile.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": record})
}
