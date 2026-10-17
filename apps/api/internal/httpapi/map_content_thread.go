package httpapi

import (
	"net/http"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) registerMapContentThreadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/map-content/moments/{momentID}/thread", s.readMapContentThread)
}

func (s *server) readMapContentThread(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id := r.PathValue("momentID")
	query, err := mapContentQueryValues(r)
	if err != nil || len(query) != 0 || r.URL.ForceQuery || !identity.HumanControlUUID(id) {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.content.(content.HumanMapContentThreadStore)
	if !ok || nilHumanSelfReviewPort(p) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	v, err := p.ReadHumanMapContentThread(r.Context(), d, a, id)
	if err != nil {
		momentControlFailure(w, err)
		return
	}
	if !v.Valid(time.Now().UTC()) || v.ResponseProof == nil || v.ViewerAccountID != a.ID || v.SeedMomentID != id {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
