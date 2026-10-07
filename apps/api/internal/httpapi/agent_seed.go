package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentseed"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"strings"
)

func (s *server) seedActor(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "personal_seed_required")
		return identity.Actor{}, [32]byte{}, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, 400, "seed_query_not_supported")
		return identity.Actor{}, [32]byte{}, false
	}
	if s.access == nil || s.agentSeeds == nil {
		respondError(w, 503, "seed_unavailable")
		return identity.Actor{}, [32]byte{}, false
	}
	actor, digest, e := s.actor(r, true)
	if authFailed(w, e) {
		return identity.Actor{}, [32]byte{}, false
	}
	if actor.AccountType != "person" {
		respondError(w, 403, "personal_seed_required")
		return identity.Actor{}, [32]byte{}, false
	}
	return actor, digest, true
}
func seedFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, agentseed.ErrInvalid):
		respondError(w, 400, "invalid_seed")
	case errors.Is(e, agentseed.ErrForbidden):
		respondError(w, 403, "seed_forbidden")
	case errors.Is(e, agentseed.ErrConflict):
		respondError(w, 409, "seed_source_conflict")
	default:
		respondError(w, 503, "seed_unavailable")
	}
}
func (s *server) getOwnAgentSeed(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.seedActor(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) > 0 {
			respondError(w, 400, "seed_get_body_not_supported")
			return
		}
	}
	v, e := s.agentSeeds.ReadOwnAgentSeed(r.Context(), digest, actor)
	if e != nil {
		seedFailure(w, e)
		return
	}
	if v.OwnerID != actor.ID || v.SchemaVersion != agentseed.Schema || !agentseed.SnapshotValid(v.Snapshot) {
		seedFailure(w, agentseed.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) saveOwnAgentSeed(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.seedActor(w, r)
	if !ok {
		return
	}
	typ, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !strings.EqualFold(typ, "application/json") {
		respondError(w, 415, "json_required")
		return
	}
	if r.Body == nil {
		respondError(w, 400, "invalid_seed")
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, agentseed.MaxBody+1))
	if e != nil {
		seedFailure(w, agentseed.ErrUnavailable)
		return
	}
	if len(raw) > agentseed.MaxBody {
		respondError(w, 413, "seed_too_large")
		return
	}
	input, e := agentseed.Decode(raw)
	if e != nil {
		seedFailure(w, e)
		return
	}
	v, e := s.agentSeeds.SaveOwnAgentSeed(r.Context(), digest, actor, input)
	if e != nil {
		seedFailure(w, e)
		return
	}
	if v.OwnerID != actor.ID || v.SchemaVersion != agentseed.Schema || !agentseed.SnapshotValid(v.Snapshot) {
		seedFailure(w, agentseed.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
