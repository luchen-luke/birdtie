package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

func (s *server) listJourneyDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	if !s.captureCollectionHTTP(w, r, d, a, false) {
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || r.URL.ForceQuery || len(q) > 1 || len(q) == 1 && (len(q["cursor"]) != 1 || !content.ValidJourneyDiscoveryCursor(q.Get("cursor"))) {
		momentFailure(w, content.ErrInvalid)
		return
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) != 0 {
			momentFailure(w, content.ErrInvalid)
			return
		}
	}
	g, ok := s.content.(content.JourneyDiscoveryStore)
	if !ok || g == nil || (reflect.ValueOf(g).Kind() == reflect.Pointer && reflect.ValueOf(g).IsNil()) {
		momentFailure(w, content.ErrUnavailable)
		return
	}
	page, e := g.ListJourneyDiscovery(r.Context(), d, a, q.Get("cursor"))
	if e != nil {
		momentFailure(w, e)
		return
	}
	if content.ValidateJourneyDiscovery(page, a.ID) != nil {
		momentFailure(w, content.ErrUnavailable)
		return
	}
	// Freeze nested stops before any authentication wait or collaborator call.
	raw, e := json.Marshal(page)
	if e != nil {
		momentFailure(w, content.ErrUnavailable)
		return
	}
	var frozen content.JourneyDiscoveryPage
	if json.Unmarshal(raw, &frozen) != nil {
		momentFailure(w, content.ErrUnavailable)
		return
	}
	frozen.ResponseProof = page.ResponseProof
	fresh, freshD, e := s.actor(r, true)
	if e != nil {
		momentFailure(w, e)
		return
	}
	if fresh != a || freshD != d {
		momentFailure(w, content.ErrConflict)
		return
	}
	if e = validateCollectionHTTP(r); e != nil {
		momentFailure(w, e)
		return
	}
	if e = g.RevalidateJourneyDiscovery(r.Context(), d, a, frozen); e != nil {
		momentFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(append([]byte(`{"data":`), raw...), '}', '\n'))
}
