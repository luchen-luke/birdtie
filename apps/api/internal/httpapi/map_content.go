package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (s *server) RegisterMapContentRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /v1/me/map-content", s.readMapContent)
	m.HandleFunc("GET /v1/me/map-content/places/{placeID}/moments", s.readMapContentPlace)
	m.HandleFunc("GET /v1/me/map-content/moments/{momentID}", s.readMapContentMoment)
	m.HandleFunc("GET /v1/me/map-content/places/{placeID}/images/{imageID}/content", s.readMapContentImage)
	m.HandleFunc("GET /v1/me/map-content/moments/{momentID}/media/{mediaID}/content", s.readMapContentMedia)
}
func mapContentQueryValues(r *http.Request, keys ...string) (url.Values, error) {
	v, e := referenceQuery(r)
	if e != nil {
		return nil, e
	}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for k := range v {
		if !allowed[k] {
			return nil, content.ErrInvalid
		}
	}
	return v, nil
}
func (s *server) mapContentPort(w http.ResponseWriter) (content.HumanMapContentStore, bool) {
	p, ok := s.content.(content.HumanMapContentStore)
	if !ok || nilHumanSelfReviewPort(p) {
		momentControlFailure(w, content.ErrUnavailable)
		return nil, false
	}
	return p, true
}
func (s *server) readMapContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	v, e := mapContentQueryValues(r, "cityId", "west", "south", "east", "north")
	if e != nil || len(v) != 5 {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	q := content.MapContentQuery{CityID: v.Get("cityId")}
	values := []*float64{&q.Bounds.West, &q.Bounds.South, &q.Bounds.East, &q.Bounds.North}
	for i, k := range []string{"west", "south", "east", "north"} {
		f, e := strconv.ParseFloat(v.Get(k), 64)
		if e != nil {
			momentControlFailure(w, content.ErrInvalid)
			return
		}
		*values[i] = f
	}
	if !q.Valid() {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, referenceActorRequest(r))
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.mapContentPort(w)
	if !ok {
		return
	}
	page, e := p.ReadHumanMapContent(r.Context(), d, a, q)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !page.Valid(time.Now().UTC()) || page.ResponseProof == nil || page.ViewerAccountID != a.ID || page.CityID != q.CityID || page.Bounds != q.Bounds {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": page}, page.ResponseProof, true)
}
func (s *server) readMapContentPlace(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	v, e := mapContentQueryValues(r, "cursor")
	id := r.PathValue("placeID")
	if e != nil || len(v) > 1 || !identity.HumanControlUUID(id) || len(v.Get("cursor")) > 4096 {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, referenceActorRequest(r))
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.mapContentPort(w)
	if !ok {
		return
	}
	page, e := p.ReadHumanMapContentPlace(r.Context(), d, a, id, v.Get("cursor"))
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !page.Valid(time.Now().UTC()) || page.ResponseProof == nil || page.PlaceID != id || page.ViewerAccountID != a.ID {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": page}, page.ResponseProof, true)
}
func (s *server) readMapContentMoment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id := r.PathValue("momentID")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 || !identity.HumanControlUUID(id) {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.mapContentPort(w)
	if !ok {
		return
	}
	v, e := p.ReadHumanMapContentMoment(r.Context(), d, a, id)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !v.Valid(time.Now().UTC()) || v.ResponseProof == nil || v.ViewerAccountID != a.ID || v.ID != id {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) readMapContentImage(w http.ResponseWriter, r *http.Request) {
	s.readMapContentBinary(w, r, "places", r.PathValue("placeID"), r.PathValue("imageID"))
}
func (s *server) readMapContentMedia(w http.ResponseWriter, r *http.Request) {
	s.readMapContentBinary(w, r, "moments", r.PathValue("momentID"), r.PathValue("mediaID"))
}
func (s *server) readMapContentBinary(w http.ResponseWriter, r *http.Request, kind, target, id string) {
	w.Header().Set("Cache-Control", "private, no-store")
	v, e := mapContentQueryValues(r, "sourceVersion", "variant")
	variant := v.Get("variant")
	if e != nil || len(v) < 1 || len(v) > 2 || !content.MomentControlHex(v.Get("sourceVersion")) || !identity.HumanControlUUID(target) || !identity.HumanControlUUID(id) || (variant != "" && variant != "poster") || kind == "places" && variant != "" || r.Header.Get("Range") != "" {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, referenceActorRequest(r))
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.mapContentPort(w)
	if !ok {
		return
	}
	b, e := p.ReadHumanMapContentBinary(r.Context(), d, a, kind, target, id, v.Get("sourceVersion"), variant)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	defer clear(b.Bytes)
	if b.ResponseProof == nil || !b.ValidUntil.After(time.Now().UTC()) || len(b.Bytes) < 1 || len(b.Bytes) > 64*1024*1024 || !content.MomentControlHex(b.SHA256) || mm.DigestBytes(b.Bytes) != b.SHA256 || (b.MIMEType != "image/jpeg" && b.MIMEType != "image/png" && b.MIMEType != "video/mp4") {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	frame, valid, _ := identity.OriginalHumanControlFrame(r.Context(), d, a)
	sessions, ok := s.access.(identity.HumanControlSessionStore)
	if !valid || !ok {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	if e = sessions.ValidateHumanControlSession(r.Context(), d, a, frame); e != nil {
		humanControlFailure(w, e)
		return
	}
	if e = b.ResponseProof.RevalidateHumanControl(r.Context()); e != nil {
		momentControlFailure(w, e)
		return
	}
	if r.Context().Err() != nil || !b.ValidUntil.After(time.Now().UTC()) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", b.MIMEType)
	w.Header().Set("Content-Length", strconv.Itoa(len(b.Bytes)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b.Bytes)
}
