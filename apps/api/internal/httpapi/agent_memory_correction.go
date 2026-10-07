package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"io"
	"mime"
	"net/http"
	"reflect"
)

const memoryCorrectionPath = "/v1/me/agent-memory-corrections"

func correctionFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, agentmemory.ErrInvalid):
		respondError(w, 400, "invalid_memory_correction")
	case errors.Is(e, agentmemory.ErrForbidden):
		respondError(w, 403, "memory_correction_forbidden")
	case errors.Is(e, agentmemory.ErrConflict):
		respondError(w, 409, "memory_correction_changed_or_expired")
	case errors.Is(e, agentmemory.ErrNotFound):
		respondError(w, 404, "memory_correction_not_found")
	default:
		respondError(w, 503, "memory_correction_unavailable")
	}
}
func (s *server) memoryCorrectionAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, mc.HumanStore, bool) {
	a, ok := s.privateProfileAccess(w, r)
	if !ok {
		return a, nil, false
	}
	store, ok := s.privateProfiles.(mc.HumanStore)
	if !ok || store == nil {
		correctionFailure(w, agentmemory.ErrUnavailable)
		return a, nil, false
	}
	return a, store, true
}
func correctionBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	typ, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || typ != "application/json" {
		respondError(w, 415, "json_required")
		return nil, false
	}
	if r.Body == nil {
		correctionFailure(w, agentmemory.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, mc.MaxBodyBytes+1))
	if e != nil || len(raw) > mc.MaxBodyBytes {
		respondError(w, 413, "memory_correction_body_too_large")
		return nil, false
	}
	return raw, true
}
func (s *server) previewOwnMemoryCorrection(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.memoryCorrectionAccess(w, r)
	if !ok {
		return
	}
	raw, ok := correctionBody(w, r)
	if !ok {
		return
	}
	in, e := mc.DecodeInput(raw)
	if e != nil {
		correctionFailure(w, e)
		return
	}
	p, e := store.PreviewOwnMemoryCorrection(r.Context(), a, in)
	if e != nil {
		correctionFailure(w, e)
		return
	}
	if mc.ValidatePreview(p) != nil || p.Owner.ID != a.WorkspacePrincipal.ID || p.ID != in.ID || !reflect.DeepEqual(p.Input, in) {
		correctionFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, p)
}
func (s *server) confirmOwnMemoryCorrection(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.memoryCorrectionAccess(w, r)
	if !ok {
		return
	}
	raw, ok := correctionBody(w, r)
	if !ok {
		return
	}
	in, e := mc.DecodeConfirm(raw)
	if e != nil {
		correctionFailure(w, e)
		return
	}
	id := r.PathValue("operationID")
	if !mc.ValidID(id) {
		correctionFailure(w, agentmemory.ErrInvalid)
		return
	}
	v, e := store.ConfirmOwnMemoryCorrection(r.Context(), a, id, in)
	if e != nil {
		correctionFailure(w, e)
		return
	}
	if mc.ValidateReceipt(v) != nil || v.Owner.ID != a.WorkspacePrincipal.ID || v.ID != id || v.PlanDigest != in.PlanDigest {
		correctionFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, v)
}
func (s *server) readOwnMemoryCorrection(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.memoryCorrectionAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) != 0 {
			correctionFailure(w, agentmemory.ErrInvalid)
			return
		}
	}
	id := r.PathValue("operationID")
	if !mc.ValidID(id) {
		correctionFailure(w, agentmemory.ErrInvalid)
		return
	}
	v, e := store.ReadOwnMemoryCorrection(r.Context(), a, id)
	if e != nil {
		correctionFailure(w, e)
		return
	}
	if mc.ValidateReceipt(v) != nil || v.Owner.ID != a.WorkspacePrincipal.ID || v.ID != id {
		correctionFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, v)
}
