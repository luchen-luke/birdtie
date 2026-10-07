package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func entityActionCondition(w http.ResponseWriter, r *http.Request, ref ea.Ref, kind string) (*ea.BoundCondition, bool) {
	v, u, o := r.Header.Values("X-Birdtie-Action-Version"), r.Header.Values("X-Birdtie-Action-Until"), r.Header.Values("X-Birdtie-Action-Operation")
	if len(v) == 0 && len(u) == 0 && len(o) == 0 {
		return nil, true
	}
	if len(v) != 1 || len(u) != 1 || len(o) != 1 || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		entityActionFailure(w, ea.ErrInvalid)
		return nil, false
	}
	until, e := time.Parse(time.RFC3339Nano, u[0])
	b := ea.BoundCondition{SourceVersion: v[0], ValidUntil: until, Kind: kind, Operation: o[0]}
	if e != nil || !b.Valid(ref) {
		entityActionFailure(w, ea.ErrInvalid)
		return nil, false
	}
	return &b, true
}

func entityActionFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(e, ea.ErrInvalid):
		respondError(w, 400, "invalid_entity_action_request")
	case errors.Is(e, ea.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(e, ea.ErrChanged):
		respondError(w, 409, "entity_action_source_changed")
	default:
		respondError(w, 503, "entity_actions_unavailable")
	}
}
func (s *server) getEntityActions(w http.ResponseWriter, r *http.Request) {
	s.respondEntityActions(w, r, false)
}
func (s *server) getPublicEntityActions(w http.ResponseWriter, r *http.Request) {
	if len(r.Header.Values("Authorization")) != 0 {
		entityActionFailure(w, identity.ErrUnauthorized)
		return
	}
	s.respondEntityActions(w, r, true)
}
func (s *server) respondEntityActions(w http.ResponseWriter, r *http.Request, public bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || r.TransferEncoding != nil || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		entityActionFailure(w, ea.ErrInvalid)
		return
	}
	ref := ea.Ref{Type: r.PathValue("entityType"), ID: r.PathValue("entityID")}
	if !ref.Valid() {
		entityActionFailure(w, ea.ErrInvalid)
		return
	}
	access := ea.Access{Public: public}
	if public {
		if ref.Type != "place" && ref.Type != "activity" {
			entityActionFailure(w, ea.ErrInvalid)
			return
		}
	} else {
		a, digest, e := s.humanSocialActor(r, true)
		if e != nil {
			entityActionFailure(w, e)
			return
		}
		if a.AccountType != "person" {
			respondError(w, 403, "person_account_required")
			return
		}
		access = ea.Access{Actor: a, SessionDigest: digest}
	}
	native, ok := s.catalog.(ea.Store)
	if !ok {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	receipt, e := native.ReadEntityActions(r.Context(), access, ref)
	if e != nil {
		entityActionFailure(w, e)
		return
	}
	if !receipt.View.Valid() || receipt.View.Entity != ref {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	// Encode first, then perform the native current-source check after all pool,
	// row and session waits. No independent auth/source read follows that check.
	data, e := json.Marshal(map[string]any{"data": receipt.View})
	if e != nil {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	if e = native.RevalidateEntityActions(r.Context(), access, ref, receipt); e != nil {
		entityActionFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
