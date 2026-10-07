package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func businessConsoleFailure(w http.ResponseWriter, e error) {
	status, code, message := 503, "business_console_unavailable", "暂时无法核实商家状态，请重新读取后检查结果。"
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "请重新登录。"
		w.Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(e, businessconsole.ErrForbidden):
		status, code, message = 403, "business_console_forbidden", "当前身份没有这家商家的管理或审核权限。"
	case errors.Is(e, businessconsole.ErrInvalid):
		status, code, message = 400, "invalid_business_console_input", "请检查内容、来源与有效期。"
	case errors.Is(e, businessconsole.ErrConflict):
		status, code, message = 409, "business_console_version_conflict", "内容或权限已变化，请重新读取并确认。"
	case errors.Is(e, businessconsole.ErrNotFound):
		status, code, message = 404, "business_console_not_found", "未找到当前可管理的商家资料。"
	}
	respond(w, status, map[string]any{"error": code, "message": message})
}
func (s *server) businessConsoleAccess(w http.ResponseWriter, r *http.Request, list bool) (businessconsole.Access, businessconsole.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	var a businessconsole.Access
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 || len(r.Header.Values("X-Birdtie-Business-Workspace")) != 0 {
		businessConsoleFailure(w, businessconsole.ErrInvalid)
		return a, nil, false
	}
	if !list {
		a.BusinessID = r.PathValue("businessID")
		if !businessconsole.ValidID(a.BusinessID) {
			businessConsoleFailure(w, businessconsole.ErrInvalid)
			return a, nil, false
		}
	}
	store, ok := s.catalog.(businessconsole.Store)
	if !ok {
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
	if e = businessconsole.ValidateAccess(a, !list); e != nil {
		businessConsoleFailure(w, e)
		return a, nil, false
	}
	return a, store, true
}
func businessConsoleBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		respond(w, 415, map[string]any{"error": "json_required", "message": "请提交JSON格式。"})
		return nil, false
	}
	if r.Body == nil {
		businessConsoleFailure(w, businessconsole.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, businessconsole.MaxBodyBytes+1))
	if e != nil || len(raw) > businessconsole.MaxBodyBytes {
		businessConsoleFailure(w, businessconsole.ErrInvalid)
		return nil, false
	}
	return raw, true
}
func businessConsoleNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) > 0 {
			businessConsoleFailure(w, businessconsole.ErrInvalid)
			return false
		}
	}
	return true
}

// Encode before the final authoritative read. If a revoke, workspace or source
// version changes while waiting/encoding, the former result is not emitted.
// A committed mutation whose response is withheld remains an unknown outcome;
// clients must inspect the current resource instead of blindly resubmitting.
func businessConsoleResult(w http.ResponseWriter, r *http.Request, a businessconsole.Access, store businessconsole.Store, result any) {
	encoded, e := json.Marshal(map[string]any{"data": result})
	if e != nil {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	var current any
	if a.BusinessID == "" {
		current, e = store.ListBusinessConsoles(r.Context(), a)
	} else {
		var c businessconsole.Console
		c, e = store.ReadBusinessConsole(r.Context(), a)
		if e == nil {
			switch expected := result.(type) {
			case businessconsole.Console:
				current = c
			case businessconsole.Claim:
				if c.Claim != nil {
					current = *c.Claim
				}
			case businessconsole.Profile:
				if c.Profile != nil {
					current = *c.Profile
				}
			case businessconsole.Venue:
				for _, v := range c.Venues {
					if v.PlaceID == expected.PlaceID {
						current = v
						break
					}
				}
			default:
				e = businessconsole.ErrUnavailable
			}
		}
	}
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	if !reflect.DeepEqual(current, result) {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	// Decode and compare the exact current material too; no custom Marshaler
	// may smuggle a stale/private second representation into a response.
	currentJSON, e := json.Marshal(map[string]any{"data": current})
	if e != nil || !bytes.Equal(encoded, currentJSON) {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	if _, e = store.ValidateBusinessAccess(r.Context(), a); e != nil {
		businessConsoleFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		businessConsoleFailure(w, businessconsole.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}
func (s *server) listBusinessConsoles(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, true)
	if !ok || !businessConsoleNoBody(w, r) {
		return
	}
	out, e := store.ListBusinessConsoles(r.Context(), a)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	if out == nil {
		out = []businessconsole.Business{}
	}
	businessConsoleResult(w, r, a, store, out)
}
func (s *server) getBusinessConsole(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, false)
	if !ok || !businessConsoleNoBody(w, r) {
		return
	}
	out, e := store.ReadBusinessConsole(r.Context(), a)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessConsoleResult(w, r, a, store, out)
}
func (s *server) submitBusinessClaim(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, false)
	if !ok {
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	in, e := businessconsole.DecodeClaim(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	out, e := store.SubmitBusinessClaim(r.Context(), a, in)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessConsoleResult(w, r, a, store, out)
}
func (s *server) reviewBusinessClaim(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, false)
	if !ok {
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	in, e := businessconsole.DecodeReview(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	out, e := store.ReviewBusinessClaim(r.Context(), a, in)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessConsoleResult(w, r, a, store, out)
}
func (s *server) putBusinessProfile(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, false)
	if !ok {
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	in, e := businessconsole.DecodeProfile(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	out, e := store.PutBusinessProfile(r.Context(), a, in)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessConsoleResult(w, r, a, store, out)
}
func (s *server) reviewBusinessProfile(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, false)
	if !ok {
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	in, e := businessconsole.DecodeReview(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	out, e := store.ReviewBusinessProfile(r.Context(), a, in)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessConsoleResult(w, r, a, store, out)
}
func (s *server) putBusinessVenueFacts(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, false)
	if !ok {
		return
	}
	place := r.PathValue("placeID")
	if !businessconsole.ValidID(place) {
		businessConsoleFailure(w, businessconsole.ErrInvalid)
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	in, e := businessconsole.DecodeVenue(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	out, e := store.PutBusinessVenueFacts(r.Context(), a, place, in)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessConsoleResult(w, r, a, store, out)
}
func (s *server) reviewBusinessVenueFacts(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, false)
	if !ok {
		return
	}
	place := r.PathValue("placeID")
	if !businessconsole.ValidID(place) {
		businessConsoleFailure(w, businessconsole.ErrInvalid)
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	in, e := businessconsole.DecodeReview(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	out, e := store.ReviewBusinessVenueFacts(r.Context(), a, place, in)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessConsoleResult(w, r, a, store, out)
}
func (s *server) changeBusinessMember(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.businessConsoleAccess(w, r, false)
	if !ok {
		return
	}
	raw, ok := businessConsoleBody(w, r)
	if !ok {
		return
	}
	in, e := businessconsole.DecodeMember(raw)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	out, e := store.ChangeBusinessMember(r.Context(), a, in)
	if e != nil {
		businessConsoleFailure(w, e)
		return
	}
	businessConsoleResult(w, r, a, store, out)
}
