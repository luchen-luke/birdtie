package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentbusiness"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) businessIdentityAccess(w http.ResponseWriter, r *http.Request) (businessconsole.Access, agentbusiness.IdentityStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	a := businessconsole.Access{BusinessID: r.PathValue("businessID")}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 || len(r.Header.Values("X-Birdtie-Business-Workspace")) != 0 || !businessconsole.ValidID(a.BusinessID) {
		businessConsoleFailure(w, businessconsole.ErrInvalid)
		return a, nil, false
	}
	store, ok := s.catalog.(agentbusiness.IdentityStore)
	if !ok || store == nil || (reflect.ValueOf(store).Kind() == reflect.Pointer && reflect.ValueOf(store).IsNil()) {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return a, nil, false
	}
	actor, digest, e := s.organizationAgentActor(r)
	if e == nil && digest == ([32]byte{}) {
		e = identity.ErrUnauthorized
	}
	if e != nil {
		businessConsoleFailure(w, e)
		return a, nil, false
	}
	if actor.AccountType != "person" {
		businessConsoleFailure(w, businessconsole.ErrForbidden)
		return a, nil, false
	}
	a.SessionDigest = digest
	a.ActingPersonID = actor.ID
	if businessconsole.ValidateAccess(a, true) != nil {
		businessConsoleFailure(w, businessconsole.ErrForbidden)
		return a, nil, false
	}
	return a, store, true
}
func (s *server) getBusinessAgentIdentity(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessIdentityAccess(w, r)
	if !ok || !businessConsoleNoBody(w, r) {
		return
	}
	v, e := store.ReadBusinessAgentIdentity(r.Context(), a)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessIdentityResult(w, r, a, store, v)
}
func (s *server) establishBusinessAgentIdentity(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessIdentityAccess(w, r)
	if !ok {
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	input, e := agentbusiness.DecodeEstablishIdentity(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	v, e := store.EstablishBusinessAgentIdentity(r.Context(), a, input)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessIdentityResult(w, r, a, store, v)
}
func businessIdentityResult(w http.ResponseWriter, r *http.Request, a businessconsole.Access, store agentbusiness.IdentityStore, v agentbusiness.BusinessIdentity) {
	if agentbusiness.ValidateBusinessIdentity(v) != nil || v.BusinessID != a.BusinessID {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	// Recheck the same native current source after encoding, including account,
	// membership, claim, actual suspended identity/Profile and the session.
	fresh, e := store.ReadBusinessAgentIdentity(r.Context(), a)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	if agentbusiness.ValidateBusinessIdentity(fresh) != nil || fresh.BusinessID != v.BusinessID || fresh.SourceVersion != v.SourceVersion || !v.ValidUntil.After(fresh.ObservedAt) || !v.ValidUntil.After(time.Now().UTC()) || r.Context().Err() != nil {
		businessConsoleFailure(w, businessconsole.ErrConflict)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}
