package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
)

func systemPushFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, sp.ErrInvalid):
		respondError(w, 400, "invalid_system_push")
	case errors.Is(e, sp.ErrConflict):
		respondError(w, 409, "system_push_changed")
	case errors.Is(e, sp.ErrNotFound):
		respondError(w, 404, "system_push_unavailable")
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(e, sp.ErrDisabled):
		respondError(w, 503, "system_push_disabled")
	default:
		respondError(w, 503, "system_push_service_unavailable")
	}
}
func (s *server) systemPushAccess(w http.ResponseWriter, r *http.Request, installation bool, open bool) (sp.Access, bool) {
	w.Header().Set("Cache-Control", "no-store")
	var a sp.Access
	if nilHumanSelfReviewPort(s.access) || nilHumanSelfReviewPort(s.systemPush) {
		systemPushFailure(w, sp.ErrUnavailable)
		return a, false
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "personal_identity_required")
		return a, false
	}
	if !open && (r.URL.RawQuery != "" || r.URL.ForceQuery) {
		systemPushFailure(w, sp.ErrInvalid)
		return a, false
	}
	actor, digest, e := s.actor(r, true)
	if authFailed(w, e) {
		return a, false
	}
	if actor.AccountType != "person" {
		respondError(w, 403, "personal_identity_required")
		return a, false
	}
	a.ProfileAccess = identity.ProfileAccess{Actor: actor, SessionDigest: digest}
	if installation || open {
		iid := r.PathValue("installationID")
		if open {
			if len(r.Header.Values("X-Birdtie-Push-Installation")) != 1 {
				systemPushFailure(w, sp.ErrInvalid)
				return a, false
			}
			iid = r.Header.Get("X-Birdtie-Push-Installation")
		}
		if !sp.ValidID(iid) || len(r.Header.Values("X-Birdtie-Push-Installation-Proof")) != 1 {
			systemPushFailure(w, sp.ErrInvalid)
			return a, false
		}
		proof, e := sp.ProofDigest(r.Header.Get("X-Birdtie-Push-Installation-Proof"))
		if e != nil {
			systemPushFailure(w, e)
			return a, false
		}
		a.InstallationID = iid
		a.ProofDigest = proof
		if !open && len(r.Header.Values("X-Birdtie-Push-Installation")) != 0 {
			systemPushFailure(w, sp.ErrInvalid)
			return a, false
		}
	}
	return a, true
}
func systemPushBody(w http.ResponseWriter, r *http.Request, keys ...string) (map[string]json.RawMessage, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		respondError(w, 415, "json_required")
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if e != nil {
		systemPushFailure(w, sp.ErrInvalid)
		return nil, false
	}
	defer clear(raw)
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		systemPushFailure(w, sp.ErrInvalid)
		return nil, false
	}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, e := d.Token()
		k, ok := key.(string)
		if e != nil || !ok || !allowed[k] || fields[k] != nil {
			systemPushFailure(w, sp.ErrInvalid)
			return nil, false
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || strings.TrimSpace(string(v)) == "null" {
			systemPushFailure(w, sp.ErrInvalid)
			return nil, false
		}
		fields[k] = v
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(fields) != len(keys) {
		systemPushFailure(w, sp.ErrInvalid)
		return nil, false
	}
	if _, e = d.Token(); e != io.EOF {
		systemPushFailure(w, sp.ErrInvalid)
		return nil, false
	}
	return fields, true
}
func clearSystemPushFields(fields map[string]json.RawMessage) {
	for _, v := range fields {
		clear(v)
	}
}
func systemPushResponse(w http.ResponseWriter, r *http.Request, value any, validate func() error) {
	raw, e := json.Marshal(map[string]any{"data": value})
	if e != nil {
		systemPushFailure(w, sp.ErrUnavailable)
		return
	}
	if e = validate(); e != nil {
		systemPushFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		systemPushFailure(w, sp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) systemPushOptions(w http.ResponseWriter, r *http.Request) {
	a, ok := s.systemPushAccess(w, r, false, false)
	if !ok || !messagePolicyNoBody(w, r) {
		return
	}
	v, e := s.systemPush.ReadOptions(r.Context(), a.ProfileAccess)
	if e != nil {
		systemPushFailure(w, e)
		return
	}
	if !sp.ValidOptions(v) {
		systemPushFailure(w, sp.ErrUnavailable)
		return
	}
	systemPushResponse(w, r, v, func() error { return s.systemPush.ValidateOptions(r.Context(), a.ProfileAccess, v) })
}
func (s *server) getSystemPushInstallation(w http.ResponseWriter, r *http.Request) {
	a, ok := s.systemPushAccess(w, r, true, false)
	if !ok || !messagePolicyNoBody(w, r) {
		return
	}
	v, e := s.systemPush.ReadInstallation(r.Context(), a)
	if e != nil {
		systemPushFailure(w, e)
		return
	}
	if !sp.ValidInstallation(v) {
		systemPushFailure(w, sp.ErrUnavailable)
		return
	}
	systemPushResponse(w, r, v, func() error { return s.systemPush.ValidateInstallation(r.Context(), a, v) })
}

func (s *server) systemPushCheckAddress(w http.ResponseWriter, r *http.Request) {
	a, ok := s.systemPushAccess(w, r, true, false)
	if !ok {
		return
	}
	f, ok := systemPushBody(w, r, "schema", "expectedVersion", "registrationKind", "address")
	if !ok {
		return
	}
	defer clearSystemPushFields(f)
	var in sp.AddressCheckInput
	var address string
	if json.Unmarshal(f["schema"], &in.Schema) != nil || json.Unmarshal(f["expectedVersion"], &in.ExpectedVersion) != nil || json.Unmarshal(f["registrationKind"], &in.RegistrationKind) != nil || json.Unmarshal(f["address"], &address) != nil {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	in.Address = sp.Address(address)
	if sp.ValidateAddressCheck(in) != nil {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	v, e := s.systemPush.CheckAddress(r.Context(), a, in)
	if e != nil {
		systemPushFailure(w, e)
		return
	}
	if !sp.ValidAddressMatch(v) || v.InstallationID != a.InstallationID || v.Version != in.ExpectedVersion {
		systemPushFailure(w, sp.ErrUnavailable)
		return
	}
	systemPushResponse(w, r, v, func() error { return s.systemPush.ValidateAddressMatch(r.Context(), a, in, v) })
}
func (s *server) systemPushOperationResponse(w http.ResponseWriter, r *http.Request, a sp.Access, v sp.Operation) {
	if !sp.ValidOperation(v) {
		systemPushFailure(w, sp.ErrUnavailable)
		return
	}
	systemPushResponse(w, r, v, func() error { return s.systemPush.ValidateOperation(r.Context(), a, v) })
}
func (s *server) putSystemPushInstallation(w http.ResponseWriter, r *http.Request) {
	a, ok := s.systemPushAccess(w, r, true, false)
	if !ok {
		return
	}
	f, ok := systemPushBody(w, r, "schema", "operationId", "expectedVersion", "registrationKind", "platform", "address", "permission")
	if !ok {
		return
	}
	defer clearSystemPushFields(f)
	var in sp.RegistrationInput
	var address string
	if json.Unmarshal(f["schema"], &in.Schema) != nil || json.Unmarshal(f["operationId"], &in.OperationID) != nil || json.Unmarshal(f["expectedVersion"], &in.ExpectedVersion) != nil || json.Unmarshal(f["registrationKind"], &in.RegistrationKind) != nil || json.Unmarshal(f["platform"], &in.Platform) != nil || json.Unmarshal(f["address"], &address) != nil || json.Unmarshal(f["permission"], &in.Permission) != nil {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	in.Address = sp.Address(address)
	if sp.ValidateRegistration(in) != nil {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	v, e := s.systemPush.RegisterInstallation(r.Context(), a, in)
	if e != nil {
		systemPushFailure(w, e)
		return
	}
	s.systemPushOperationResponse(w, r, a, v)
}
func (s *server) deleteSystemPushInstallation(w http.ResponseWriter, r *http.Request) {
	a, ok := s.systemPushAccess(w, r, true, false)
	if !ok {
		return
	}
	f, ok := systemPushBody(w, r, "schema", "operationId", "expectedVersion")
	if !ok {
		return
	}
	var in sp.RevokeInput
	if json.Unmarshal(f["schema"], &in.Schema) != nil || json.Unmarshal(f["operationId"], &in.OperationID) != nil || json.Unmarshal(f["expectedVersion"], &in.ExpectedVersion) != nil || sp.ValidateRevoke(in) != nil {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	v, e := s.systemPush.RevokeInstallation(r.Context(), a, in)
	if e != nil {
		systemPushFailure(w, e)
		return
	}
	s.systemPushOperationResponse(w, r, a, v)
}
func (s *server) getSystemPushOperation(w http.ResponseWriter, r *http.Request) {
	a, ok := s.systemPushAccess(w, r, true, false)
	if !ok || !messagePolicyNoBody(w, r) {
		return
	}
	op := r.PathValue("operationID")
	if !sp.ValidID(op) {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	v, e := s.systemPush.ReadOperation(r.Context(), a, op)
	if e != nil {
		systemPushFailure(w, e)
		return
	}
	s.systemPushOperationResponse(w, r, a, v)
}
func (s *server) resolveSystemPushEvent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.systemPushAccess(w, r, false, true)
	if !ok || !messagePolicyNoBody(w, r) {
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q) != 2 || len(q["bindingId"]) != 1 || len(q["bindingVersion"]) != 1 {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	version, e := strconv.ParseInt(q.Get("bindingVersion"), 10, 64)
	if e != nil || strconv.FormatInt(version, 10) != q.Get("bindingVersion") {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	in := sp.OpenInput{EventID: r.PathValue("eventID"), BindingID: q.Get("bindingId"), BindingVersion: version}
	if !sp.ValidOpen(in) {
		systemPushFailure(w, sp.ErrInvalid)
		return
	}
	v, e := s.systemPush.ResolveEvent(r.Context(), a, in)
	if e != nil {
		systemPushFailure(w, e)
		return
	}
	if v.Schema != "system-push-open-v1" || v.EventID != in.EventID || v.BindingID != in.BindingID || v.BindingVersion != in.BindingVersion || !sp.ValidID(v.Item.ID) {
		systemPushFailure(w, sp.ErrUnavailable)
		return
	}
	systemPushResponse(w, r, v, func() error { return s.systemPush.ValidateEvent(r.Context(), a, in, v) })
}
