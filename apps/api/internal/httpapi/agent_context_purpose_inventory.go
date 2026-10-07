package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"net/http"
	"reflect"
	"time"
)

// Inventory is metadata, but its original Personal Agent must remain current
// at the response boundary. Reuse the existing native owner/session resolver.
type contextInventoryAgentResolver interface {
	ResolveOwnContextAgent(context.Context, agentprofile.PrivateAccess) (agentcognitive.AgentReference, error)
}

func taskContextInventoryNil(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}
func (s *server) listOwnContextPurposes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Context().Err() != nil {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		contextBuilderHTTPFailure(w, acb.ErrDenied)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return
	}
	if !contextPurposeNoBody(w, r) {
		return
	}
	port, ok := s.catalog.(acb.PurposeInventoryStore)
	sessions, sok := s.access.(socialnow.HumanSessionStore)
	agents, aok := s.catalog.(contextInventoryAgentResolver)
	if !ok || !sok || !aok || taskContextInventoryNil(port) || taskContextInventoryNil(s.access) || taskContextInventoryNil(sessions) || taskContextInventoryNil(agents) {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return
	}
	if e != nil {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		contextBuilderHTTPFailure(w, acb.ErrDenied)
		return
	}
	out, e := port.ListOwnContextPurposes(r.Context(), a)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	if acb.ValidatePurposeInventory(out) != nil || out.Owner != p {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil || len(raw)+1 > acb.PurposeInventoryMaxBytes {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	if e = sessions.ValidateHumanSocialResponse(r.Context(), digest, actor); e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			respondError(w, 401, "unauthorized")
		} else {
			contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		}
		return
	}
	current, e := agents.ResolveOwnContextAgent(r.Context(), a)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	if current.Principal != out.Owner || current.AgentID != out.AgentID || current.Role != agentruntime.PersonalAgent {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	if r.Context().Err() != nil || !time.Now().UTC().Before(out.ValidUntil) {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
