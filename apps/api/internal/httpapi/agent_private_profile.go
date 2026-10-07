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

type privateAgentProfileStore interface {
	ReadOwnAgentPrivateProfile(context.Context, agentprofile.PrivateAccess) (agentprofile.PrivateRecord, error)
	ReplaceOwnAgentPrivateProfile(context.Context, agentprofile.PrivateAccess, agentprofile.ReplacePrivateInput) (agentprofile.PrivateRecord, error)
}

// This is a direct human self-review/edit gateway. It is not a model tool,
// Organization delegation, public profile read, inference or Memory permission.
func (s *server) privateProfileAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusForbidden, "personal_profile_required")
		return agentprofile.PrivateAccess{}, false
	}
	if s.access == nil || s.privateProfiles == nil {
		respondError(w, http.StatusServiceUnavailable, "private_profile_unavailable")
		return agentprofile.PrivateAccess{}, false
	}
	actor, digest, err := s.actor(r, true)
	if errors.Is(err, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return agentprofile.PrivateAccess{}, false
	}
	if err != nil {
		privateProfileFailure(w, err)
		return agentprofile.PrivateAccess{}, false
	}
	principal, err := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if err != nil || principal.Type != actorref.Person {
		respondError(w, http.StatusForbidden, "personal_profile_required")
		return agentprofile.PrivateAccess{}, false
	}
	// No owner/Agent/workspace selectors are accepted at this self-only route.
	if r.URL.RawQuery != "" {
		respondError(w, http.StatusBadRequest, "private_profile_query_not_supported")
		return agentprofile.PrivateAccess{}, false
	}
	access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		respondError(w, http.StatusForbidden, "private_profile_forbidden")
		return agentprofile.PrivateAccess{}, false
	}
	return access, true
}

func privateProfileFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentprofile.ErrInvalid):
		respondError(w, http.StatusBadRequest, "invalid_private_profile")
	case errors.Is(err, agentprofile.ErrConflict):
		respondError(w, http.StatusConflict, "private_profile_version_conflict")
	case errors.Is(err, agentprofile.ErrForbidden):
		respondError(w, http.StatusForbidden, "private_profile_forbidden")
	case errors.Is(err, agentprofile.ErrNotFound):
		respondError(w, http.StatusNotFound, "private_profile_not_found")
	default:
		// Never log an underlying PG DETAIL, input, token or private field body.
		log.Printf("request_id=%s private_profile_error category=unavailable", w.Header().Get("X-Request-ID"))
		respondError(w, http.StatusServiceUnavailable, "private_profile_unavailable")
	}
}

func privateProfileResponse(w http.ResponseWriter, access agentprofile.PrivateAccess, profile agentprofile.PrivateRecord) {
	if agentprofile.ValidatePrivateRecord(profile) != nil ||
		profile.Profile.OwnerType != actorref.Person || profile.Profile.OwnerID != access.WorkspacePrincipal.ID {
		privateProfileFailure(w, agentprofile.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": profile})
}

func (s *server) getOwnAgentPrivateProfile(w http.ResponseWriter, r *http.Request) {
	access, ok := s.privateProfileAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			respondError(w, http.StatusBadRequest, "private_profile_get_body_not_supported")
			return
		}
	}
	profile, err := s.privateProfiles.ReadOwnAgentPrivateProfile(r.Context(), access)
	if err != nil {
		privateProfileFailure(w, err)
		return
	}
	privateProfileResponse(w, access, profile)
}

func (s *server) replaceOwnAgentPrivateProfile(w http.ResponseWriter, r *http.Request) {
	access, ok := s.privateProfileAccess(w, r)
	if !ok {
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(contentType, "application/json") {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	// Domain decoding rejects duplicate/unknown/null authority fields as well as
	// invalid values. Full replacement may explicitly clear all private values.
	if r.Body == nil {
		respondError(w, http.StatusBadRequest, "invalid_private_profile")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, agentprofile.MaxPrivateBodyBytes+1))
	if err != nil || len(body) > agentprofile.MaxPrivateBodyBytes {
		respondError(w, http.StatusRequestEntityTooLarge, "private_profile_body_too_large")
		return
	}
	input, err := agentprofile.DecodeReplacePrivateInput(body)
	if err != nil {
		privateProfileFailure(w, err)
		return
	}
	profile, err := s.privateProfiles.ReplaceOwnAgentPrivateProfile(r.Context(), access, input)
	if err != nil {
		privateProfileFailure(w, err)
		return
	}
	privateProfileResponse(w, access, profile)
}
