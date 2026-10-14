package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	cc "github.com/birdtie/birdtie/apps/api/internal/contactcode"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"strings"
)

func contactCodeFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, cc.ErrInvalid):
		respondError(w, 400, "invalid_contact_code")
	case errors.Is(e, cc.ErrConflict):
		respondError(w, 409, "contact_code_changed")
	case errors.Is(e, cc.ErrNotFound), errors.Is(e, identity.ErrNotFound):
		respondError(w, 404, "contact_code_unavailable")
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	default:
		respondError(w, 503, "contact_code_service_unavailable")
	}
}
func (s *server) contactCodeAccess(w http.ResponseWriter, r *http.Request) (identity.ProfileAccess, cc.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	var a identity.ProfileAccess
	if nilHumanSelfReviewPort(s.access) {
		contactCodeFailure(w, cc.ErrUnavailable)
		return a, nil, false
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "personal_identity_required")
		return a, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		contactCodeFailure(w, cc.ErrInvalid)
		return a, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if authFailed(w, e) {
		return a, nil, false
	}
	if actor.AccountType != "person" {
		respondError(w, 403, "personal_identity_required")
		return a, nil, false
	}
	store, ok := s.access.(cc.Store)
	if !ok || nilHumanSelfReviewPort(store) {
		contactCodeFailure(w, cc.ErrUnavailable)
		return a, nil, false
	}
	return identity.ProfileAccess{Actor: actor, SessionDigest: digest}, store, true
}

// Read one closed object, rejecting duplicate keys and trailing JSON too.
func contactCodeBody(w http.ResponseWriter, r *http.Request, keys ...string) (map[string]json.RawMessage, bool) {
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		respondError(w, 415, "json_required")
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if e != nil {
		contactCodeFailure(w, cc.ErrInvalid)
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		contactCodeFailure(w, cc.ErrInvalid)
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for d.More() {
		key, e := d.Token()
		k, ok := key.(string)
		if e != nil || !ok || !allowed[k] || fields[k] != nil {
			contactCodeFailure(w, cc.ErrInvalid)
			return nil, false
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			contactCodeFailure(w, cc.ErrInvalid)
			return nil, false
		}
		fields[k] = v
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(fields) != len(keys) {
		contactCodeFailure(w, cc.ErrInvalid)
		return nil, false
	}
	if _, e = d.Token(); e != io.EOF {
		contactCodeFailure(w, cc.ErrInvalid)
		return nil, false
	}
	return fields, true
}
func (s *server) contactCodeResponse(w http.ResponseWriter, r *http.Request, a identity.ProfileAccess, p cc.Store, v cc.View) {
	if !cc.ValidView(v) {
		contactCodeFailure(w, cc.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		contactCodeFailure(w, cc.ErrUnavailable)
		return
	}
	if e = p.ValidateOwnContactCode(r.Context(), a, v); e != nil {
		contactCodeFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		contactCodeFailure(w, cc.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) getOwnContactCode(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.contactCodeAccess(w, r)
	if !ok || !messagePolicyNoBody(w, r) {
		return
	}
	v, e := p.ReadOwnContactCode(r.Context(), a)
	if e != nil {
		contactCodeFailure(w, e)
		return
	}
	s.contactCodeResponse(w, r, a, p, v)
}
func (s *server) changeOwnContactCode(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.contactCodeAccess(w, r)
	if !ok {
		return
	}
	f, ok := contactCodeBody(w, r, "operationId", "expectedVersion")
	if !ok {
		return
	}
	var in cc.Input
	if strings.TrimSpace(string(f["expectedVersion"])) == "null" || json.Unmarshal(f["operationId"], &in.OperationID) != nil || json.Unmarshal(f["expectedVersion"], &in.ExpectedVersion) != nil || cc.ValidateInput(in) != nil {
		contactCodeFailure(w, cc.ErrInvalid)
		return
	}
	v, e := p.ChangeOwnContactCode(r.Context(), a, in, r.Method == http.MethodDelete)
	if e != nil {
		contactCodeFailure(w, e)
		return
	}
	s.contactCodeResponse(w, r, a, p, v)
}
func (s *server) resolveContactCode(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.contactCodeAccess(w, r)
	if !ok {
		return
	}
	f, ok := contactCodeBody(w, r, "codeId")
	if !ok {
		return
	}
	var code string
	if json.Unmarshal(f["codeId"], &code) != nil || !cc.ValidID(code) {
		contactCodeFailure(w, cc.ErrInvalid)
		return
	}
	v, e := p.ResolveContactCode(r.Context(), a, code)
	if e != nil {
		contactCodeFailure(w, e)
		return
	}
	if !cc.ValidID(v.AccountID) {
		contactCodeFailure(w, cc.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		contactCodeFailure(w, cc.ErrUnavailable)
		return
	}
	if e = p.ValidateContactResolution(r.Context(), a, code, v); e != nil {
		contactCodeFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		contactCodeFailure(w, cc.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
