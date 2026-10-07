package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	apc "github.com/birdtie/birdtie/apps/api/internal/agentprofilecompletion"
)

const profileCompletionPath = "/v1/me/agent-profile-completion"

// The existing self-only auth boundary remains the sole invocation context.
// Catalog's native Store implements this human port; no runtime/provider option
// or independent consent grant is introduced. Root registers exact routes.
func (s *server) profileCompletionAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, apc.HumanStore, bool) {
	a, ok := s.privateProfileAccess(w, r)
	if !ok {
		return agentprofile.PrivateAccess{}, nil, false
	}
	store, ok := s.privateProfiles.(apc.HumanStore)
	if !ok || store == nil {
		profileCompletionFailure(w, agentprofile.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	return a, store, true
}
func profileCompletionFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, agentprofile.ErrInvalid):
		respondError(w, http.StatusBadRequest, "invalid_profile_completion")
	case errors.Is(e, agentprofile.ErrForbidden):
		respondError(w, http.StatusForbidden, "profile_completion_forbidden")
	case errors.Is(e, agentprofile.ErrNotFound):
		respondError(w, http.StatusNotFound, "profile_completion_not_found")
	case errors.Is(e, agentprofile.ErrConflict), errors.Is(e, apc.ErrExpired):
		respondError(w, http.StatusConflict, "profile_completion_changed_or_expired")
	default:
		respondError(w, http.StatusServiceUnavailable, "profile_completion_unavailable")
	}
}
func completionGetEmpty(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			respondError(w, http.StatusBadRequest, "profile_completion_get_body_not_supported")
			return false
		}
	}
	return true
}
func completionBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	contentType, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !strings.EqualFold(contentType, "application/json") {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return nil, false
	}
	if r.Body == nil {
		profileCompletionFailure(w, agentprofile.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, apc.MaxBodyBytes+1))
	if e != nil || len(raw) > apc.MaxBodyBytes {
		respondError(w, http.StatusRequestEntityTooLarge, "profile_completion_body_too_large")
		return nil, false
	}
	return raw, true
}
func (s *server) getOwnProfileCompletionSuggestions(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.profileCompletionAccess(w, r)
	if !ok || !completionGetEmpty(w, r) {
		return
	}
	v, e := store.ReadOwnProfileCompletionSuggestions(r.Context(), a)
	if e != nil {
		profileCompletionFailure(w, e)
		return
	}
	if apc.ValidateSuggestions(v) != nil || v.Owner.ID != a.WorkspacePrincipal.ID {
		profileCompletionFailure(w, agentprofile.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, v)
}
func (s *server) previewOwnProfileCompletion(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.profileCompletionAccess(w, r)
	if !ok {
		return
	}
	raw, ok := completionBody(w, r)
	if !ok {
		return
	}
	input, e := apc.DecodePreviewInput(raw)
	if e != nil {
		profileCompletionFailure(w, e)
		return
	}
	v, e := store.PreviewOwnProfileCompletion(r.Context(), a, input)
	if e != nil {
		profileCompletionFailure(w, e)
		return
	}
	if apc.ValidatePreview(v) != nil || v.Owner.ID != a.WorkspacePrincipal.ID || v.ID != input.PreviewID || v.Source.MemoryID != input.MemoryID || v.Source.MemoryVersion != input.MemoryVersion || v.ExpectedProfileVersion != input.ExpectedProfileVersion {
		profileCompletionFailure(w, agentprofile.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, v)
}
func completionReceiptResponse(w http.ResponseWriter, a agentprofile.PrivateAccess, id string, v apc.Receipt, e error) {
	if e != nil {
		profileCompletionFailure(w, e)
		return
	}
	if apc.ValidateReceipt(v) != nil || v.Owner.ID != a.WorkspacePrincipal.ID || v.ID != id {
		profileCompletionFailure(w, agentprofile.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, v)
}
func (s *server) getOwnProfileCompletion(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.profileCompletionAccess(w, r)
	if !ok || !completionGetEmpty(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !apc.ValidID(id) {
		profileCompletionFailure(w, agentprofile.ErrInvalid)
		return
	}
	v, e := store.ReadOwnProfileCompletion(r.Context(), a, id)
	completionReceiptResponse(w, a, id, v, e)
}
func (s *server) acceptOwnProfileCompletion(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.profileCompletionAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("previewID")
	if !apc.ValidID(id) {
		profileCompletionFailure(w, agentprofile.ErrInvalid)
		return
	}
	raw, ok := completionBody(w, r)
	if !ok {
		return
	}
	input, e := apc.DecodeAcceptInput(raw)
	if e != nil {
		profileCompletionFailure(w, e)
		return
	}
	v, e := store.AcceptOwnProfileCompletion(r.Context(), a, id, input)
	if e == nil && (v.State != "COMMITTED" || v.PlanDigest != input.PlanDigest) {
		profileCompletionFailure(w, agentprofile.ErrUnavailable)
		return
	}
	completionReceiptResponse(w, a, id, v, e)
}
