package httpapi

import (
	"encoding/json"
	"errors"
	ncs "github.com/birdtie/birdtie/apps/api/internal/nowcontextselection"
	"io"
	"net/http"
)

func (s *server) nowSelectionAccess(w http.ResponseWriter, r *http.Request) (ncs.Access, bool) {
	actor, digest, e := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, e) {
		return ncs.Access{}, false
	}
	a := ncs.Access{Actor: actor, Digest: digest}
	if !a.Valid() || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 || len(r.Header.Values("X-Birdtie-Business-Workspace")) != 0 {
		respondError(w, 403, "person_workspace_required")
		return a, false
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.nowContextSelection == nil {
		respondError(w, 503, "now_context_selection_unavailable")
		return a, false
	}
	return a, true
}
func nowSelectionFailure(w http.ResponseWriter, e error) {
	code, label := 503, "now_context_selection_unavailable"
	switch {
	case errors.Is(e, ncs.ErrInvalid):
		code, label = 400, "invalid_now_context_selection"
	case errors.Is(e, ncs.ErrDenied):
		code, label = 403, "now_context_selection_denied"
	case errors.Is(e, ncs.ErrConflict):
		code, label = 409, "now_context_selection_changed"
	}
	respondError(w, code, label)
}
func selectionNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, 400, "invalid_now_context_selection")
		return false
	}
	return true
}
func (s *server) getNowContextSelectionOptions(w http.ResponseWriter, r *http.Request) {
	if !selectionNoQuery(w, r) {
		return
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 2))
		if e != nil || len(raw) != 0 {
			respondError(w, 400, "invalid_now_context_selection")
			return
		}
	}
	a, ok := s.nowSelectionAccess(w, r)
	if !ok {
		return
	}
	v, e := s.nowContextSelection.ReadOptions(r.Context(), a)
	if e != nil {
		nowSelectionFailure(w, e)
		return
	}
	if ncs.ValidateOptions(v.Response, a.Actor.ID) != nil {
		nowSelectionFailure(w, ncs.ErrUnavailable)
		return
	}
	body, e := json.Marshal(map[string]any{"data": v.Response})
	if e != nil {
		nowSelectionFailure(w, ncs.ErrUnavailable)
		return
	}
	if e = s.nowContextSelection.RevalidateOptions(r.Context(), a, v); e != nil {
		nowSelectionFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}
func (s *server) resolveNowContextSelection(w http.ResponseWriter, r *http.Request) {
	if !selectionNoQuery(w, r) {
		return
	}
	a, ok := s.nowSelectionAccess(w, r)
	if !ok {
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16000))
	if e != nil {
		nowSelectionFailure(w, ncs.ErrInvalid)
		return
	}
	in, e := ncs.Decode(raw)
	if e != nil {
		nowSelectionFailure(w, e)
		return
	}
	v, e := s.nowContextSelection.Resolve(r.Context(), a, in)
	if e != nil {
		nowSelectionFailure(w, e)
		return
	}
	if ncs.ValidateEnvelope(v.Response.Envelope, a.Actor.ID) != nil || ncs.ValidateOption(v.Response.Choice) != nil || v.Response.Choice.OptionID != in.OptionID {
		nowSelectionFailure(w, ncs.ErrUnavailable)
		return
	}
	body, e := json.Marshal(map[string]any{"data": v.Response})
	if e != nil {
		nowSelectionFailure(w, ncs.ErrUnavailable)
		return
	}
	if e = s.nowContextSelection.RevalidateSelection(r.Context(), a, v); e != nil {
		nowSelectionFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}
