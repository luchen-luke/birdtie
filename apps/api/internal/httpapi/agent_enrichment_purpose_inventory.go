package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

func enrichmentInventoryNil(v any) bool {
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

func (s *server) listOwnEnrichmentPurposes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Context().Err() != nil {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		enrichmentPurposeHTTPError(w, aep.ErrDenied)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return
	}
	if !enrichmentPurposeEmpty(w, r) {
		return
	}
	port, ok := s.catalog.(aep.InventoryStore)
	sessions, sessionOK := s.access.(socialnow.HumanSessionStore)
	agents, agentOK := s.catalog.(contextInventoryAgentResolver)
	if !ok || !sessionOK || !agentOK || enrichmentInventoryNil(port) || enrichmentInventoryNil(s.access) || enrichmentInventoryNil(sessions) || enrichmentInventoryNil(agents) {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return
	}
	if e != nil {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		enrichmentPurposeHTTPError(w, aep.ErrDenied)
		return
	}
	out, e := port.ListOwnEnrichmentPurposes(r.Context(), a)
	if e != nil {
		enrichmentPurposeHTTPError(w, e)
		return
	}
	if aep.ValidateInventory(out) != nil || out.Owner != p {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil || len(raw)+1 > aep.InventoryMaxBytes {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	if e = sessions.ValidateHumanSocialResponse(r.Context(), digest, actor); e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			respondError(w, 401, "unauthorized")
		} else {
			enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		}
		return
	}
	current, e := agents.ResolveOwnContextAgent(r.Context(), a)
	if e != nil {
		if errors.Is(e, acb.ErrDenied) {
			enrichmentPurposeHTTPError(w, aep.ErrDenied)
		} else {
			enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		}
		return
	}
	if current.Principal != out.Owner || current.AgentID != out.AgentID || current.Role != agentruntime.PersonalAgent {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	if r.Context().Err() != nil || !time.Now().UTC().Before(out.ValidUntil) {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}
