package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"net/http"
)

func (s *server) socialContextActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", false
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return "", false
	}
	if s.socialContext == nil {
		respondError(w, http.StatusServiceUnavailable, "social_context_unavailable")
		return "", false
	}
	w.Header().Set("Cache-Control", "no-store")
	return actor.ID, true
}

func socialContextError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, socialcontext.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if errors.Is(err, identity.ErrUnauthorized) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
	} else if errors.Is(err, socialcontext.ErrChanged) {
		respondError(w, http.StatusConflict, "shared_history_changed")
	} else if errors.Is(err, socialcontext.ErrUnavailable) {
		respondError(w, http.StatusServiceUnavailable, "shared_history_unavailable")
	} else {
		serverError(w, err)
	}
	return true
}

func (s *server) ownSocialDisclosure(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.socialContextActor(w, r)
	if !ok {
		return
	}
	out, err := s.socialContext.OwnSocialDisclosure(r.Context(), actor)
	if !socialContextError(w, err) {
		respond(w, http.StatusOK, map[string]any{"data": out})
	}
}

func (s *server) setSocialDisclosure(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.socialContextActor(w, r)
	if !ok {
		return
	}
	// All three fields are required; omitted values never silently revoke consent.
	var raw struct {
		MutualTies        *bool `json:"mutualTies"`
		SharedCommunities *bool `json:"sharedCommunities"`
		SharedActivities  *bool `json:"sharedActivities"`
	}
	if !decodeStrictJSON(w, r, &raw) {
		return
	}
	if raw.MutualTies == nil || raw.SharedCommunities == nil || raw.SharedActivities == nil {
		respondError(w, http.StatusBadRequest, "invalid_social_disclosure")
		return
	}
	out, err := s.socialContext.SetSocialDisclosure(r.Context(), actor, socialcontext.Disclosure{MutualTies: *raw.MutualTies, SharedCommunities: *raw.SharedCommunities, SharedActivities: *raw.SharedActivities})
	if !socialContextError(w, err) {
		respond(w, http.StatusOK, map[string]any{"data": out})
	}
}

func (s *server) sharedSocialContext(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, digest, err := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, http.StatusBadRequest, "invalid_shared_history_query")
		return
	}
	target := r.PathValue("accountID")
	if !uuidPath.MatchString(target) {
		respondError(w, http.StatusBadRequest, "invalid_account_id")
		return
	}
	native, ok := s.socialContext.(socialcontext.CurrentStore)
	if !ok {
		socialContextError(w, socialcontext.ErrUnavailable)
		return
	}
	a := socialcontext.CurrentAccess{ViewerID: actor.ID, TargetID: target, SessionDigest: digest}
	out, check, err := native.SharedSocialContextCurrent(r.Context(), a)
	if socialContextError(w, err) {
		return
	}
	if check == nil || socialcontext.ValidateCurrent(out, a) != nil {
		socialContextError(w, socialcontext.ErrUnavailable)
		return
	}
	raw, err := json.Marshal(map[string]any{"data": out})
	if err != nil {
		serverError(w, err)
		return
	}
	if socialContextError(w, check(r.Context())) {
		return
	}
	if r.Context().Err() != nil {
		socialContextError(w, socialcontext.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
