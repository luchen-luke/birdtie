package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
)

func (s *server) getPublicBusiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("businessID")
	if !businessconsole.ValidID(id) || r.URL.RawQuery != "" || r.URL.ForceQuery {
		businessConsoleFailure(w, businessconsole.ErrInvalid)
		return
	}
	if !businessConsoleNoBody(w, r) {
		return
	}
	actor, _, e := s.actor(r, false)
	if authFailed(w, e) {
		return
	}
	store, ok := s.catalog.(supplierprofile.Store)
	if !ok {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	result, e := store.ReadPublicBusiness(r.Context(), id, actor.ID)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": result})
	if e != nil {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	final, _, e := s.actor(r, false)
	if authFailed(w, e) {
		return
	}
	if final != actor || r.Context().Err() != nil {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	// Authenticate may wait for a session update. Re-project disclosure after
	// that wait, with no subsequent authentication write before emission.
	current, e := store.ReadPublicBusiness(r.Context(), id, actor.ID)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	latest, e := json.Marshal(map[string]any{"data": current})
	if e != nil || !reflect.DeepEqual(current, result) || !bytes.Equal(encoded, latest) {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}

func (s *server) getBusinessPublicPermission(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.businessConsoleAccess(w, r, false)
	if !ok || !businessConsoleNoBody(w, r) {
		return
	}
	store, ok := s.catalog.(supplierprofile.Store)
	if !ok {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	out, e := store.ReadBusinessPublicPermission(r.Context(), a)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	s.supplierOwnerResult(w, r, a, store, out)
}
func (s *server) changeBusinessPublicPermission(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.businessConsoleAccess(w, r, false)
	if !ok {
		return
	}
	store, ok := s.catalog.(supplierprofile.Store)
	if !ok {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	in, e := supplierprofile.DecodePermission(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	out, e := store.ChangeBusinessPublicPermission(r.Context(), a, in)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	s.supplierOwnerResult(w, r, a, store, out)
}
func (s *server) supplierOwnerResult(w http.ResponseWriter, r *http.Request, a businessconsole.Access, store supplierprofile.Store, result any) {
	encoded, e := json.Marshal(map[string]any{"data": result})
	if e != nil {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	current, e := store.ReadBusinessPublicPermission(r.Context(), a)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	var latest any = current
	if _, ok := result.(supplierprofile.Permission); ok {
		latest = current.Permission
	}
	latestJSON, e := json.Marshal(map[string]any{"data": latest})
	if e != nil || !reflect.DeepEqual(latest, result) || !bytes.Equal(encoded, latestJSON) || r.Context().Err() != nil {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}
