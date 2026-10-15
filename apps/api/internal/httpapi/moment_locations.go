package httpapi

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"net/http"
	"time"
)

func (s *server) registerMomentLocationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/moments/{momentID}/locations", func(w http.ResponseWriter, r *http.Request) { s.getMomentLocations(w, r, true) })
	mux.HandleFunc("GET /v1/moments/{momentID}/locations", func(w http.ResponseWriter, r *http.Request) { s.getMomentLocations(w, r, false) })
}
func (s *server) getMomentLocations(w http.ResponseWriter, r *http.Request, own bool) {
	w.Header().Set("Cache-Control", "no-store")
	if !humanControlPath(w, r, "momentID") || !humanControlNoBody(w, r) {
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		publicMomentFailure(w, content.ErrInvalid)
		return
	}
	access := content.PublicMomentAccess{}
	if own || len(r.Header.Values("Authorization")) != 0 {
		next, a, d, ok := s.humanControlActor(w, r)
		if !ok {
			return
		}
		r = next
		access = content.PublicMomentAccess{Actor: a, SessionDigest: d}
	}
	store, ok := s.content.(content.MomentLocationsStore)
	if !ok || store == nil {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	receipt, e := store.CaptureMomentLocations(r.Context(), access, r.PathValue("momentID"), own)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	// Encode before final native source/Session verification; no later auth wait.
	view := receipt.View
	if r.Header.Get("X-Birdtie-Map-Locations") == "locations-v2" {
		view.SchemaVersion = "moment-locations-v2"
	} else {
		if own {
			items := []content.MomentLocationResolved{}
			for _, v := range view.Items.([]content.MomentLocationResolved) {
				if v.Kind != "REFERENCE" {
					v.Position = len(items)
					items = append(items, v)
				}
			}
			view.Items = items
		} else {
			items := []content.PublicMomentLocation{}
			for _, v := range view.Items.([]content.PublicMomentLocation) {
				if v.Kind != "REFERENCE" {
					v.DisplayOrder = len(items)
					items = append(items, v)
				}
			}
			view.Items = items
		}
	}
	raw, e := json.Marshal(map[string]any{"data": view})
	if e != nil {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	current, e := store.FinalizeMomentLocations(r.Context(), access, receipt)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if current.SchemaVersion != content.MomentLocationsSchemaVersion || current.MomentID != r.PathValue("momentID") || !current.ValidUntil.After(time.Now().UTC()) || r.Context().Err() != nil {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
