package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	nq "github.com/birdtie/birdtie/apps/api/internal/nowcontextquery"
	"io"
	"mime"
	"net/http"
)

func onlineQueryFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(e, nq.ErrInvalid):
		respondError(w, 400, "invalid_online_query")
	case errors.Is(e, nq.ErrDenied):
		respondError(w, 403, "online_query_denied")
	case errors.Is(e, nq.ErrNotFound):
		respondError(w, 404, "online_source_unavailable")
	case errors.Is(e, nq.ErrConflict):
		respondError(w, 409, "online_source_changed")
	default:
		respondError(w, 503, "online_query_unavailable")
	}
}
func (s *server) onlineQueryAccess(w http.ResponseWriter, r *http.Request) (nq.Access, nq.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		onlineQueryFailure(w, nq.ErrInvalid)
		return nq.Access{}, nil, false
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		onlineQueryFailure(w, nq.ErrDenied)
		return nq.Access{}, nil, false
	}
	actor, digest, e := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, e) {
		return nq.Access{}, nil, false
	}
	a := nq.Access{Digest: digest, Actor: actor}
	if !a.Valid() {
		onlineQueryFailure(w, nq.ErrDenied)
		return a, nil, false
	}
	p, ok := s.catalog.(nq.Store)
	if !ok || p == nil {
		onlineQueryFailure(w, nq.ErrUnavailable)
		return a, nil, false
	}
	return a, p, true
}
func onlineQueryNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) > 0 {
			onlineQueryFailure(w, nq.ErrInvalid)
			return false
		}
	}
	return true
}
func (s *server) listOwnNowOnlineContexts(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.onlineQueryAccess(w, r)
	if !ok || !onlineQueryNoBody(w, r) {
		return
	}
	items, e := p.ListOwnOnlineContexts(r.Context(), a)
	if e != nil {
		onlineQueryFailure(w, e)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": items.Contexts})
	if e != nil {
		onlineQueryFailure(w, nq.ErrUnavailable)
		return
	}
	if e = p.RevalidateOwnOnlineContexts(r.Context(), a, items); e != nil {
		onlineQueryFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		onlineQueryFailure(w, nq.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) createOwnNowOnlineTask(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.onlineQueryAccess(w, r)
	if !ok {
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		respondError(w, 415, "json_required")
		return
	}
	if r.Body == nil {
		onlineQueryFailure(w, nq.ErrInvalid)
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if e != nil {
		onlineQueryFailure(w, nq.ErrInvalid)
		return
	}
	in, e := nq.Decode(raw)
	if e != nil {
		onlineQueryFailure(w, e)
		return
	}
	receipt, e := p.QueryOwnPublicOnline(r.Context(), a, in)
	if e != nil {
		onlineQueryFailure(w, e)
		return
	}
	s.respondNowOnline(w, r, a, p, receipt)
}
func (s *server) getOwnNowOnlineTask(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.onlineQueryAccess(w, r)
	if !ok || !onlineQueryNoBody(w, r) {
		return
	}
	receipt, e := p.RestoreOwnPublicOnline(r.Context(), a, r.PathValue("taskID"))
	if e != nil {
		onlineQueryFailure(w, e)
		return
	}
	s.respondNowOnline(w, r, a, p, receipt)
}
func (s *server) getOwnNowOnlineIntent(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.onlineQueryAccess(w, r)
	if !ok || !onlineQueryNoBody(w, r) {
		return
	}
	receipt, e := p.ReadOwnPublicOnlineIntent(r.Context(), a, r.PathValue("intentID"))
	if e != nil {
		onlineQueryFailure(w, e)
		return
	}
	s.respondNowOnline(w, r, a, p, receipt)
}
func (s *server) respondNowOnline(w http.ResponseWriter, r *http.Request, a nq.Access, p nq.Store, receipt nq.Receipt) {
	raw, e := json.Marshal(map[string]any{"data": receipt.Response})
	if e != nil {
		onlineQueryFailure(w, nq.ErrUnavailable)
		return
	}
	if e = p.RevalidateOwnPublicOnline(r.Context(), a, receipt); e != nil {
		onlineQueryFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		onlineQueryFailure(w, nq.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
