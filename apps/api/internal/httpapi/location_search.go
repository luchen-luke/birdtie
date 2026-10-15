package httpapi

import (
	"encoding/json"
	lr "github.com/birdtie/birdtie/apps/api/internal/locationreference"
	"net/http"
	"strconv"
)

func (s *server) registerLocationSearchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/location-search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !humanControlNoBody(w, r) {
			return
		}
		q := r.URL.Query()
		for key, values := range q {
			if (key != "q" && key != "limit") || len(values) != 1 {
				http.Error(w, "invalid query", 400)
				return
			}
		}
		var e error
		n := 10
		if q.Get("limit") != "" {
			n, e = strconv.Atoi(q.Get("limit"))
		}
		if e != nil || lr.ValidateQuery(q.Get("q"), n) != nil {
			http.Error(w, "invalid query", 400)
			return
		}
		store, ok := s.content.(lr.Store)
		if !ok {
			http.Error(w, "unavailable", 503)
			return
		}
		v, e := store.SearchLocationReferences(r.Context(), q.Get("q"), n)
		if e != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{"data": v})
	})
}
