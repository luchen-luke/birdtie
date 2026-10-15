package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"net/http"
)

func (s *server) registerMomentMediaContextRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/moments/{momentID}/media-contexts", s.getOwnedMomentMediaContexts)
}
func (s *server) getOwnedMomentMediaContexts(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "momentID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.content.(content.OwnedMomentMediaContextStore)
	if !ok || p == nil {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	v, e := p.ReadOwnedMomentMediaContexts(r.Context(), d, a, r.PathValue("momentID"))
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if v.SchemaVersion != content.OwnedMomentMediaContextSchema || v.MomentID != r.PathValue("momentID") || v.Items == nil || !content.MomentControlHex(v.SourceVersion) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
