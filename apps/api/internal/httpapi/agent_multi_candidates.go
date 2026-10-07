package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"strings"
)

func multiCandidateHTTPError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, acr.ErrInvalid):
		respondError(w, 400, "invalid_multi_candidate")
	case errors.Is(e, acr.ErrDenied):
		respondError(w, 403, "multi_candidate_denied")
	case errors.Is(e, acr.ErrExpired), errors.Is(e, acr.ErrConflict), errors.Is(e, acr.ErrBusy):
		respondError(w, 409, "multi_candidate_changed")
	default:
		respondError(w, 503, "multi_candidate_unavailable")
	}
}
func (s *server) multiCandidateAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, acr.Gateway, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		multiCandidateHTTPError(w, acr.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return agentprofile.PrivateAccess{}, nil, false
	}
	port := s.multiCandidates
	if port == nil || s.access == nil {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return agentprofile.PrivateAccess{}, nil, false
	}
	if e != nil {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		multiCandidateHTTPError(w, acr.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	return a, port, true
}
func multiCandidateBody(w http.ResponseWriter, r *http.Request, keys ...string) ([]byte, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !strings.EqualFold(media, "application/json") {
		respondError(w, 415, "json_required")
		return nil, false
	}
	if r.Body == nil {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if e != nil {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return nil, false
	}
	if _, e = aep.StrictObject(raw, keys...); e != nil {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return nil, false
	}
	return raw, true
}
func multiCandidateEmpty(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) > 0 {
			multiCandidateHTTPError(w, acr.ErrInvalid)
			return false
		}
	}
	return true
}
func (s *server) previewOwnMultiCandidate(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.multiCandidateAccess(w, r)
	if !ok {
		return
	}
	raw, ok := multiCandidateBody(w, r, "analysisGrantIds", "retainUntil")
	if !ok {
		return
	}
	var sel acr.Selection
	if json.Unmarshal(raw, &sel) != nil {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	var e error
	sel, e = acr.Normalize(sel)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	out, e := port.PreviewOwnMultiCandidate(r.Context(), a, sel)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	if acr.ValidatePreview(out) != nil || out.State != "CURRENT_REVIEW" || out.Owner != a.WorkspacePrincipal || !sameMultiCandidateSelection(out.Selection, sel) {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func sameMultiCandidateSelection(a, b acr.Selection) bool {
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
func (s *server) readOwnMultiCandidatePreview(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.multiCandidateAccess(w, r)
	if !ok || !multiCandidateEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !aep.ValidID(id) {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := port.ReadOwnMultiCandidatePreview(r.Context(), a, id)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	if acr.ValidatePreview(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) approveOwnMultiCandidate(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.multiCandidateAccess(w, r)
	if !ok || !multiCandidateEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !aep.ValidID(id) {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := port.ApproveOwnMultiCandidate(r.Context(), a, id)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	if acr.ValidateGrant(out) != nil || out.PreviewID != id || out.Owner != a.WorkspacePrincipal || out.RevokedAt != nil || !out.ExpiresAt.After(out.ObservedAt) {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) readOwnMultiCandidateGrant(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.multiCandidateAccess(w, r)
	if !ok || !multiCandidateEmpty(w, r) {
		return
	}
	id := r.PathValue("grantID")
	if !aep.ValidID(id) {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := port.ReadOwnMultiCandidateGrant(r.Context(), a, id)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	if acr.ValidateGrant(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) revokeOwnMultiCandidate(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.multiCandidateAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("grantID")
	if !aep.ValidID(id) {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	raw, ok := multiCandidateBody(w, r, "expectedRevision")
	if !ok {
		return
	}
	var in struct {
		Revision int64 `json:"expectedRevision"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Revision < 1 || in.Revision == int64(^uint64(0)>>1) {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := port.RevokeOwnMultiCandidate(r.Context(), a, id, in.Revision)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	if acr.ValidateGrant(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal || out.Revision != in.Revision+1 || out.RevokedAt == nil {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}

func (s *server) stageOwnMultiCandidate(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.multiCandidateAccess(w, r)
	if !ok {
		return
	}
	raw, ok := multiCandidateBody(w, r, "retentionGrantId")
	if !ok {
		return
	}
	var in struct {
		ID string `json:"retentionGrantId"`
	}
	if json.Unmarshal(raw, &in) != nil || !aep.ValidID(in.ID) {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := p.StageOwnMultiCandidate(r.Context(), a, in.ID)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	if acr.ValidateReceipt(out) != nil || out.Owner != a.WorkspacePrincipal || out.RetentionGrantID != in.ID || !out.Committed {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) readOwnMultiCandidateReceipt(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.multiCandidateAccess(w, r)
	if !ok || !multiCandidateEmpty(w, r) {
		return
	}
	id := r.PathValue("grantID")
	if !aep.ValidID(id) {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	out, e := p.ReadOwnMultiCandidateReceipt(r.Context(), a, id)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	if acr.ValidateReceipt(out) != nil || out.Owner != a.WorkspacePrincipal || out.RetentionGrantID != id {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}

func (s *server) readOwnMultiCandidatePreviewReceipt(w http.ResponseWriter, r *http.Request) {
	a, base, ok := s.multiCandidateAccess(w, r)
	if !ok || !multiCandidateEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !aep.ValidID(id) {
		multiCandidateHTTPError(w, acr.ErrInvalid)
		return
	}
	port, ok := base.(acr.PreviewReceiptStore)
	if !ok || port == nil {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	out, e := port.ReadOwnMultiCandidatePreviewReceipt(r.Context(), a, id)
	if e != nil {
		multiCandidateHTTPError(w, e)
		return
	}
	if acr.ValidatePreviewReceipt(out) != nil || out.PreviewID != id || out.Owner != a.WorkspacePrincipal {
		multiCandidateHTTPError(w, acr.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
