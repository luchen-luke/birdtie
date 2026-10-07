package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"strings"
)

func enrichmentPurposeHTTPError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, aep.ErrInvalid):
		respondError(w, 400, "invalid_enrichment_purpose")
	case errors.Is(e, aep.ErrDenied):
		respondError(w, 403, "enrichment_purpose_denied")
	case errors.Is(e, aep.ErrExpired), errors.Is(e, aep.ErrConflict):
		respondError(w, 409, "enrichment_purpose_changed")
	default:
		respondError(w, 503, "enrichment_purpose_unavailable")
	}
}
func (s *server) enrichmentPurposeAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, aep.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		enrichmentPurposeHTTPError(w, aep.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return agentprofile.PrivateAccess{}, nil, false
	}
	port, ok := s.catalog.(aep.Store)
	if !ok || port == nil || s.access == nil {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return agentprofile.PrivateAccess{}, nil, false
	}
	if e != nil {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		enrichmentPurposeHTTPError(w, aep.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	return a, port, true
}
func enrichmentPurposeBody(w http.ResponseWriter, r *http.Request, keys ...string) ([]byte, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !strings.EqualFold(media, "application/json") {
		respondError(w, 415, "json_required")
		return nil, false
	}
	if r.Body == nil {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if e != nil {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return nil, false
	}
	if _, e = aep.StrictObject(raw, keys...); e != nil {
		enrichmentPurposeHTTPError(w, e)
		return nil, false
	}
	return raw, true
}
func enrichmentPurposeEmpty(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) > 0 {
			enrichmentPurposeHTTPError(w, aep.ErrInvalid)
			return false
		}
	}
	return true
}
func (s *server) previewOwnEnrichmentPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.enrichmentPurposeAccess(w, r)
	if !ok {
		return
	}
	raw, ok := enrichmentPurposeBody(w, r, "taskId", "momentId", "momentRevision", "fields", "deadlineAt")
	if !ok {
		return
	}
	var sel aep.Selection
	if json.Unmarshal(raw, &sel) != nil {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return
	}
	var e error
	sel, e = aep.Normalize(sel)
	if e != nil {
		enrichmentPurposeHTTPError(w, e)
		return
	}
	out, e := port.PreviewOwnEnrichmentPurpose(r.Context(), a, sel)
	if e != nil {
		enrichmentPurposeHTTPError(w, e)
		return
	}
	if aep.ValidatePreview(out) != nil || out.State != "CURRENT_REVIEW" || out.Owner != a.WorkspacePrincipal || !sameEnrichmentSelection(out.Selection, sel) {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func sameEnrichmentSelection(a, b aep.Selection) bool {
	x, e := aep.Normalize(a)
	if e != nil {
		return false
	}
	y, e := aep.Normalize(b)
	if e != nil {
		return false
	}
	jx, _ := json.Marshal(x)
	jy, _ := json.Marshal(y)
	return string(jx) == string(jy)
}
func (s *server) getOwnEnrichmentPurposePreview(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.enrichmentPurposeAccess(w, r)
	if !ok || !enrichmentPurposeEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !aep.ValidID(id) {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return
	}
	out, e := port.ReadOwnEnrichmentPurposePreview(r.Context(), a, id)
	if e != nil {
		enrichmentPurposeHTTPError(w, e)
		return
	}
	if aep.ValidatePreview(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) approveOwnEnrichmentPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.enrichmentPurposeAccess(w, r)
	if !ok || !enrichmentPurposeEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !aep.ValidID(id) {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return
	}
	out, e := port.ApproveOwnEnrichmentPurpose(r.Context(), a, id)
	if e != nil {
		enrichmentPurposeHTTPError(w, e)
		return
	}
	if aep.ValidateGrant(out) != nil || out.PreviewID != id || out.Owner != a.WorkspacePrincipal || out.RevokedAt != nil || !out.ExpiresAt.After(out.ObservedAt) {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) getOwnEnrichmentPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.enrichmentPurposeAccess(w, r)
	if !ok || !enrichmentPurposeEmpty(w, r) {
		return
	}
	id := r.PathValue("grantID")
	if !aep.ValidID(id) {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return
	}
	out, e := port.ReadOwnEnrichmentPurpose(r.Context(), a, id)
	if e != nil {
		enrichmentPurposeHTTPError(w, e)
		return
	}
	if aep.ValidateGrant(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) revokeOwnEnrichmentPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.enrichmentPurposeAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("grantID")
	if !aep.ValidID(id) {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return
	}
	raw, ok := enrichmentPurposeBody(w, r, "expectedRevision")
	if !ok {
		return
	}
	var in struct {
		Revision int64 `json:"expectedRevision"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Revision < 1 || in.Revision == int64(^uint64(0)>>1) {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return
	}
	out, e := port.RevokeOwnEnrichmentPurpose(r.Context(), a, id, in.Revision)
	if e != nil {
		enrichmentPurposeHTTPError(w, e)
		return
	}
	if aep.ValidateGrant(out) != nil || out.ID != id || out.Owner != a.WorkspacePrincipal || out.Revision != in.Revision+1 || out.RevokedAt == nil {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}

func (s *server) getOwnEnrichmentPurposePreviewReceipt(w http.ResponseWriter, r *http.Request) {
	a, base, ok := s.enrichmentPurposeAccess(w, r)
	if !ok || !enrichmentPurposeEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !aep.ValidID(id) {
		enrichmentPurposeHTTPError(w, aep.ErrInvalid)
		return
	}
	port, ok := base.(aep.PreviewReceiptStore)
	if !ok || port == nil {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	out, e := port.ReadOwnEnrichmentPurposePreviewReceipt(r.Context(), a, id)
	if e != nil {
		enrichmentPurposeHTTPError(w, e)
		return
	}
	if aep.ValidatePreviewReceipt(out, aep.Purpose) != nil || out.PreviewID != id || out.Owner != a.WorkspacePrincipal {
		enrichmentPurposeHTTPError(w, aep.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
