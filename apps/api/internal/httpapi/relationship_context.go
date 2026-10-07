package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"net/http"
)

func (s *server) relationshipActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", false
	}
	w.Header().Set("Cache-Control", "no-store")
	if actor.AccountType != "person" || r.Header.Get("X-Birdtie-Organization-Workspace") != "" {
		respondError(w, 403, "personal_workspace_required")
		return "", false
	}
	if r.URL.RawQuery != "" {
		respondError(w, 400, "owner_override_not_allowed")
		return "", false
	}
	if s.relationshipContext == nil {
		respondError(w, 503, "relationship_context_unavailable")
		return "", false
	}
	return actor.ID, true
}
func relationshipError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, relationshipcontext.ErrNotFound) {
		respondError(w, 404, "not_found")
	} else if errors.Is(err, relationshipcontext.ErrAgentUnavailable) {
		respondError(w, 403, "agent_unavailable")
	} else {
		serverError(w, err)
	}
	return true
}
func (s *server) ownRelationshipConsent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.relationshipActor(w, r)
	if !ok {
		return
	}
	out, err := s.relationshipContext.OwnRelationshipConsent(r.Context(), actor)
	if !relationshipError(w, err) {
		respond(w, 200, map[string]any{"data": out})
	}
}
func (s *server) setRelationshipConsent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.relationshipActor(w, r)
	if !ok {
		return
	}
	var raw struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeStrictJSON(w, r, &raw) {
		return
	}
	if raw.Enabled == nil {
		respondError(w, 400, "explicit_consent_required")
		return
	}
	out, err := s.relationshipContext.SetRelationshipConsent(r.Context(), actor, relationshipcontext.Consent{Enabled: *raw.Enabled})
	if !relationshipError(w, err) {
		respond(w, 200, map[string]any{"data": out})
	}
}
func (s *server) ownRelationshipContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.relationshipActor(w, r)
	if !ok {
		return
	}
	// Ownership is session-derived; the Store independently checks the live Agent.
	policy := agentruntime.ForType(actorref.Person)
	if !policy.Allows(agentruntime.RelationshipContextRead) || !policy.DecideContext(agentruntime.AccessRequest{Scope: agentruntime.Private, OwnerID: actor, ViewerID: actor, PrincipalID: actor, AuthorityVerified: true}).Allowed {
		respondError(w, 403, "agent_context_forbidden")
		return
	}
	out, err := s.relationshipContext.OwnRelationshipContext(r.Context(), actor)
	if !relationshipError(w, err) {
		respond(w, 200, map[string]any{"data": out})
	}
}
