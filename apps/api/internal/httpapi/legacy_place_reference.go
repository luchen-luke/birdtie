package httpapi

import (
	"errors"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

func (s *server) readLegacyPlaceReference(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	q, e := url.ParseQuery(r.URL.RawQuery)
	ref := lpr.Reference{Kind: q.Get("kind"), RecordID: q.Get("recordId"), PlaceID: r.PathValue("placeID")}
	if e != nil || r.URL.ForceQuery || len(q) != 2 || len(q["kind"]) != 1 || len(q["recordId"]) != 1 || !ref.Valid() {
		respondError(w, 400, "invalid_owned_place_reference")
		return
	}
	if !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.content.(lpr.HumanStore)
	if !ok || port == nil || (reflect.ValueOf(port).Kind() == reflect.Pointer && reflect.ValueOf(port).IsNil()) {
		respondError(w, 503, "owned_place_reference_unavailable")
		return
	}
	v, e := port.ReadHumanLegacyPlaceReference(r.Context(), d, a, ref)
	if errors.Is(e, lpr.ErrNotFound) {
		respondError(w, 404, "owned_place_reference_unavailable")
		return
	}
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !v.Valid(ref, time.Now().UTC()) || v.ResponseProof == nil {
		respondError(w, 503, "owned_place_reference_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) registerLegacyPlaceReferenceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/place-references/{placeID}", s.readLegacyPlaceReference)
}
