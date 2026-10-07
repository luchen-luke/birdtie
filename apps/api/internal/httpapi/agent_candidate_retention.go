package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"strings"
)

func candidateRetentionHTTPError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, acr.ErrInvalid):
		respondError(w, 400, "invalid_candidate_retention")
	case errors.Is(e, acr.ErrDenied):
		respondError(w, 403, "candidate_retention_denied")
	case errors.Is(e, acr.ErrExpired), errors.Is(e, acr.ErrConflict):
		respondError(w, 409, "candidate_retention_changed")
	default:
		respondError(w, 503, "candidate_retention_unavailable")
	}
}
func (s *server) candidateRetentionAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, acr.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		candidateRetentionHTTPError(w, acr.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return agentprofile.PrivateAccess{}, nil, false
	}
	port, ok := s.catalog.(acr.Store)
	if !ok || port == nil || s.access == nil {
		candidateRetentionHTTPError(w, acr.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return agentprofile.PrivateAccess{}, nil, false
	}
	if e != nil {
		candidateRetentionHTTPError(w, acr.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		candidateRetentionHTTPError(w, acr.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	return a, port, true
}
func candidateRetentionBody(w http.ResponseWriter, r *http.Request, keys ...string) ([]byte, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !strings.EqualFold(media, "application/json") {
		respondError(w, 415, "json_required")
		return nil, false
	}
	if r.Body == nil {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if e != nil {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return nil, false
	}
	if _, e = aep.StrictObject(raw, keys...); e != nil {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return nil, false
	}
	return raw, true
}
func candidateRetentionEmpty(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) > 0 {
			candidateRetentionHTTPError(w, acr.ErrInvalid)
			return false
		}
	}
	return true
}
func (s *server) previewOwnCandidateRetention(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.candidateRetentionAccess(w, r)
	if !ok {
		return
	}
	raw, ok := candidateRetentionBody(w, r, "analysisGrantId", "retainUntil")
	if !ok {
		return
	}
	var sel acr.Selection
	if json.Unmarshal(raw, &sel) != nil {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return
	}
	var e error
	sel, e = acr.Normalize(sel)
	if e != nil {
		candidateRetentionHTTPError(w, e)
		return
	}
	out, e := port.PreviewOwnCandidateRetention(r.Context(), a, sel)
	if e != nil {
		candidateRetentionHTTPError(w, e)
		return
	}
	if acr.ValidatePreview(out) != nil || out.State != "CURRENT_REVIEW" || out.Owner != a.WorkspacePrincipal || !sameRetentionSelection(out.Selection, sel) {
		candidateRetentionHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func sameRetentionSelection(a, b acr.Selection) bool {
	x, e := acr.Normalize(a)
	if e != nil {
		return false
	}
	y, e := acr.Normalize(b)
	if e != nil {
		return false
	}
	jx, _ := json.Marshal(x)
	jy, _ := json.Marshal(y)
	return string(jx) == string(jy)
}
func (s *server) getOwnCandidateRetentionPreview(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.candidateRetentionAccess(w, r)
	if !ok || !candidateRetentionEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !aep.ValidID(id) {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := port.ReadOwnCandidateRetentionPreview(r.Context(), a, id)
	if e != nil {
		candidateRetentionHTTPError(w, e)
		return
	}
	if acr.ValidatePreview(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal {
		candidateRetentionHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) approveOwnCandidateRetention(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.candidateRetentionAccess(w, r)
	if !ok || !candidateRetentionEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !aep.ValidID(id) {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := port.ApproveOwnCandidateRetention(r.Context(), a, id)
	if e != nil {
		candidateRetentionHTTPError(w, e)
		return
	}
	if acr.ValidateGrant(out) != nil || out.PreviewID != id || out.Owner != a.WorkspacePrincipal || out.RevokedAt != nil || !out.ExpiresAt.After(out.ObservedAt) {
		candidateRetentionHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) getOwnCandidateRetention(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.candidateRetentionAccess(w, r)
	if !ok || !candidateRetentionEmpty(w, r) {
		return
	}
	id := r.PathValue("grantID")
	if !aep.ValidID(id) {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := port.ReadOwnCandidateRetention(r.Context(), a, id)
	if e != nil {
		candidateRetentionHTTPError(w, e)
		return
	}
	if acr.ValidateGrant(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal {
		candidateRetentionHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) revokeOwnCandidateRetention(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.candidateRetentionAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("grantID")
	if !aep.ValidID(id) {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return
	}
	raw, ok := candidateRetentionBody(w, r, "expectedRevision")
	if !ok {
		return
	}
	var in struct {
		Revision int64 `json:"expectedRevision"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Revision < 1 || in.Revision == int64(^uint64(0)>>1) {
		candidateRetentionHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := port.RevokeOwnCandidateRetention(r.Context(), a, id, in.Revision)
	if e != nil {
		candidateRetentionHTTPError(w, e)
		return
	}
	if acr.ValidateGrant(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal || out.Revision != in.Revision+1 || out.RevokedAt == nil {
		candidateRetentionHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
