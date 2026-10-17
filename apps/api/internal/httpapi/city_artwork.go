package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/birdtie/birdtie/apps/api/internal/cityartwork"
)

func cityArtworkFailure(w http.ResponseWriter, e error) {
	if errors.Is(e, cityartwork.ErrNotFound) {
		respondError(w, http.StatusNotFound, "city_artwork_not_found")
		return
	}
	respondError(w, http.StatusServiceUnavailable, "city_artwork_unavailable")
}

func (s *server) readCityArtwork(w http.ResponseWriter, r *http.Request, descriptor bool) {
	// Revalidate on every request; a cached derivative has no publication power.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	city, version := r.PathValue("cityID"), r.PathValue("version")
	if !cityartwork.ValidCity(city) || !cityartwork.ValidVersion(version) || r.URL.RawQuery != "" || r.Header.Get("Range") != "" {
		respondError(w, http.StatusBadRequest, "invalid_city_artwork_request")
		return
	}
	p, ok := s.catalog.(cityartwork.Store)
	if !ok {
		cityArtworkFailure(w, cityartwork.ErrUnavailable)
		return
	}
	v, e := p.ReadCityArtwork(r.Context(), city, version)
	if e != nil {
		cityArtworkFailure(w, e)
		return
	}
	if !v.Valid() || v.Descriptor.CityID != city || v.Descriptor.Version != version {
		cityArtworkFailure(w, cityartwork.ErrUnavailable)
		return
	}
	if e = p.ValidateCityArtwork(r.Context(), city, version); e != nil {
		cityArtworkFailure(w, e)
		return
	}
	if descriptor {
		respond(w, http.StatusOK, map[string]any{"data": v.Descriptor})
		return
	}
	w.Header().Set("Content-Type", v.Descriptor.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(v.Bytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(v.Bytes)
}

func (s *server) getCityArtworkDescriptor(w http.ResponseWriter, r *http.Request) {
	s.readCityArtwork(w, r, true)
}
func (s *server) getCityArtworkContent(w http.ResponseWriter, r *http.Request) {
	s.readCityArtwork(w, r, false)
}
