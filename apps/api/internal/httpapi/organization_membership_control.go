package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	org "github.com/birdtie/birdtie/apps/api/internal/organization"
	"io"
	"net/http"
	"reflect"
	"time"
)

func orgControlNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if e != nil || len(b) != 0 {
		orgControlFailure(w, org.ErrControlInvalid)
		return false
	}
	return true
}

func orgControlFailure(w http.ResponseWriter, e error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(e, org.ErrControlInvalid) || errors.Is(e, org.ErrInvalidTarget):
		respondError(w, 400, "invalid_organization_control")
	case errors.Is(e, org.ErrForbidden):
		respondError(w, 403, "organization_control_forbidden")
	case errors.Is(e, org.ErrControlChanged) || errors.Is(e, org.ErrConflict):
		respondError(w, 409, "organization_control_changed")
	case errors.Is(e, org.ErrNotFound):
		respondError(w, 404, "organization_control_not_found")
	default:
		respondError(w, 503, "organization_control_unavailable")
	}
}
func (s *server) orgControlAccess(w http.ResponseWriter, r *http.Request) (org.HumanAccess, org.HumanControlStore, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("organizationID")
	if !org.ControlID(id) || r.URL.RawQuery != "" || r.URL.ForceQuery {
		orgControlFailure(w, org.ErrControlInvalid)
		return org.HumanAccess{}, nil, id, false
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		orgControlFailure(w, org.ErrForbidden)
		return org.HumanAccess{}, nil, id, false
	}
	actor, d, e := s.actor(r, true)
	if e != nil {
		orgControlFailure(w, e)
		return org.HumanAccess{}, nil, id, false
	}
	if actor.AccountType != "person" {
		orgControlFailure(w, org.ErrForbidden)
		return org.HumanAccess{}, nil, id, false
	}
	p, ok := s.access.(org.HumanControlStore)
	if !ok || nilHumanSelfReviewPort(p) {
		orgControlFailure(w, org.ErrControlUnavailable)
		return org.HumanAccess{}, nil, id, false
	}
	return org.HumanAccess{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}, p, id, true
}
func decodeOrgControl(w http.ResponseWriter, r *http.Request, commit bool) (org.ControlInput, string, error) {
	raw, e := entityCoverBody(w, r, 8192)
	if e != nil {
		return org.ControlInput{}, "", org.ErrControlInvalid
	}
	all := map[string]bool{"action": true, "operationId": true, "expectedAuthorityVersion": true, "userAccountId": true, "membershipId": true, "role": true, "expectedMemberVersion": true, "cityId": true, "latitude": true, "longitude": true, "proposalId": true, "expectedProposalVersion": true, "expectedCanonicalRevision": true, "approval": commit}
	obj, e := entityCoverObject(raw, all)
	if e != nil {
		return org.ControlInput{}, "", org.ErrControlInvalid
	}
	var in org.ControlInput
	var action string
	if e = json.Unmarshal(obj["action"], &action); e != nil {
		return in, "", org.ErrControlInvalid
	}
	keys := []string{"action", "operationId", "expectedAuthorityVersion"}
	switch action {
	case "INVITE":
		keys = append(keys, "userAccountId", "role", "expectedMemberVersion")
	case "ACCEPT", "REVOKE":
		keys = append(keys, "membershipId", "expectedMemberVersion")
	case "ROLE":
		keys = append(keys, "membershipId", "expectedMemberVersion", "role")
	case "PROPOSE":
		keys = append(keys, "cityId", "latitude", "longitude", "expectedCanonicalRevision")
	case "APPROVE_POINT", "REJECT_POINT":
		keys = append(keys, "proposalId", "expectedProposalVersion", "expectedCanonicalRevision")
	case "WITHDRAW_POINT":
		keys = append(keys, "proposalId", "expectedProposalVersion")
	case "HIDE_POINT":
		keys = append(keys, "expectedCanonicalRevision")
	default:
		return in, "", org.ErrControlInvalid
	}
	if commit {
		keys = append(keys, "approval")
	}
	if len(obj) != len(keys) {
		return in, "", org.ErrControlInvalid
	}
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			return in, "", org.ErrControlInvalid
		}
	}
	approval := ""
	if commit {
		if e = json.Unmarshal(obj["approval"], &approval); e != nil || len(approval) < 1 || len(approval) > 512 {
			return in, "", org.ErrControlInvalid
		}
		delete(obj, "approval")
	}
	b, _ := json.Marshal(obj)
	if e = json.Unmarshal(b, &in); e != nil {
		return in, "", org.ErrControlInvalid
	}
	in, e = org.NormalizeControl(in)
	return in, approval, e
}
func orgControlRespond(w http.ResponseWriter, r *http.Request, p org.HumanControlStore, a org.HumanAccess, id, proof string, until time.Time, data any) {
	b, e := json.Marshal(map[string]any{"data": data})
	if e != nil {
		orgControlFailure(w, org.ErrControlUnavailable)
		return
	}
	if e = p.ValidateOrganizationControlResponse(r.Context(), a, id, proof, until); e != nil {
		orgControlFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}
func (s *server) readOrganizationMembersControl(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.orgControlAccess(w, r)
	if !ok || !orgControlNoBody(w, r) {
		return
	}
	out, e := p.ReadMembersControl(r.Context(), a, id)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	orgControlRespond(w, r, p, a, id, out.Proof, out.ExpiresAt, out)
}
func (s *server) previewOrganizationControl(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.orgControlAccess(w, r)
	if !ok {
		return
	}
	in, _, e := decodeOrgControl(w, r, false)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	out, e := p.PreviewOrganizationControl(r.Context(), a, id, in)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	orgControlRespond(w, r, p, a, id, out.Proof, out.ExpiresAt, out)
}
func (s *server) applyOrganizationControl(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.orgControlAccess(w, r)
	if !ok {
		return
	}
	in, approval, e := decodeOrgControl(w, r, true)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	out, e := p.ApplyOrganizationControl(r.Context(), a, id, in, approval)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	orgControlRespond(w, r, p, a, id, out.Proof, time.Time{}, out)
}
func (s *server) readOrganizationControlOperation(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.orgControlAccess(w, r)
	if !ok || !orgControlNoBody(w, r) {
		return
	}
	op := r.PathValue("operationID")
	if !org.ControlID(op) {
		orgControlFailure(w, org.ErrControlInvalid)
		return
	}
	out, e := p.ReadOrganizationControlOperation(r.Context(), a, id, op)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	orgControlRespond(w, r, p, a, id, out.Proof, time.Time{}, out)
}
func (s *server) stopOrganizationControlOperation(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.orgControlAccess(w, r)
	if !ok {
		return
	}
	op := r.PathValue("operationID")
	if !org.ControlID(op) {
		orgControlFailure(w, org.ErrControlInvalid)
		return
	}
	raw, e := entityCoverBody(w, r, 512)
	if e != nil {
		orgControlFailure(w, org.ErrControlInvalid)
		return
	}
	obj, e := entityCoverObject(raw, map[string]bool{"expectedAuthorityVersion": true})
	var v int64
	if e != nil || len(obj) != 1 || json.Unmarshal(obj["expectedAuthorityVersion"], &v) != nil || v < 1 || v > org.MaxControlVersion {
		orgControlFailure(w, org.ErrControlInvalid)
		return
	}
	out, e := p.StopOrganizationControlOperation(r.Context(), a, id, op, v)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	orgControlRespond(w, r, p, a, id, out.Proof, time.Time{}, out)
}
func (s *server) registerOrganizationControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/members/control", s.readOrganizationMembersControl)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/map-location/control", s.readOrganizationMapControl)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/control/preview", s.previewOrganizationControl)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/control", s.applyOrganizationControl)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/control/operations/{operationID}", s.readOrganizationControlOperation)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/control/operations/{operationID}/stop", s.stopOrganizationControlOperation)
}

// The old invitation array remains compatible, with the same source seal and
// exact initial Session generation as the new private control reads.
func (s *server) listHumanOrganizationInvitations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || !orgControlNoBody(w, r) {
		orgControlFailure(w, org.ErrControlInvalid)
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		orgControlFailure(w, org.ErrForbidden)
		return
	}
	actor, d, e := s.actor(r, true)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	if actor.AccountType != "person" {
		orgControlFailure(w, org.ErrForbidden)
		return
	}
	p, ok := s.access.(org.HumanControlStore)
	if !ok {
		orgControlFailure(w, org.ErrControlUnavailable)
		return
	}
	a := org.HumanAccess{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}
	out, e := p.ReadInvitationsControl(r.Context(), a)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	b, e := json.Marshal(map[string]any{"data": out.Items})
	if e != nil {
		orgControlFailure(w, org.ErrControlUnavailable)
		return
	}
	for id, proof := range out.Proofs {
		if e = p.ValidateOrganizationControlResponse(r.Context(), a, id, proof, out.ExpiresAt); e != nil {
			orgControlFailure(w, e)
			return
		}
	}
	fresh, e := p.ReadInvitationsControl(r.Context(), a)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	if !reflect.DeepEqual(out.Items, fresh.Items) || !reflect.DeepEqual(out.Proofs, fresh.Proofs) {
		orgControlFailure(w, org.ErrControlChanged)
		return
	}
	if e = p.ValidateOrganizationControlSession(r.Context(), a, out.SessionProof); e != nil {
		orgControlFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}
