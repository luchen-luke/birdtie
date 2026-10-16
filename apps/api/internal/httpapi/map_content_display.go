package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func validMapMediaMode(v url.Values) bool {
	_, present := v["mediaMode"]
	return !present || len(v["mediaMode"]) == 1 && v.Get("mediaMode") == "lazy"
}

func (s *server) mapContentLazyPort(w http.ResponseWriter) (content.HumanMapContentLazyStore, bool) {
	p, ok := s.content.(content.HumanMapContentLazyStore)
	if !ok || nilHumanSelfReviewPort(p) {
		momentControlFailure(w, content.ErrUnavailable)
		return nil, false
	}
	return p, true
}

func (s *server) readMapContentDisplayDescriptor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	mid, id := r.PathValue("momentID"), r.PathValue("mediaID")
	v, e := mapContentQueryValues(r, "sourceVersion")
	if e != nil || len(v) != 1 || !content.MomentControlHex(v.Get("sourceVersion")) || !identity.HumanControlUUID(mid) || !identity.HumanControlUUID(id) || r.Header.Get("Range") != "" {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, referenceActorRequest(r))
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.mapContentLazyPort(w)
	if !ok {
		return
	}
	value, e := p.ReadHumanMapContentDisplayDescriptor(r.Context(), d, a, mid, id, v.Get("sourceVersion"))
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !value.Valid(time.Now().UTC()) || value.ResponseProof == nil || value.ViewerAccountID != a.ID || value.MomentID != mid || value.Media.ID != id || value.OriginalSourceVersion != v.Get("sourceVersion") {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": value}, value.ResponseProof, true)
}
