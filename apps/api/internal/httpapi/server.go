package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/oidcauth"
)

type pinger interface {
	Ping(context.Context) error
}

type server struct {
	catalog foundation.PublicCatalog
	access  identity.AccessStore
	seed    cityseed.Store
	content content.MomentStore
	oidc    *oidcauth.Service
	db      pinger
}

var uuidPath = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func New(catalog foundation.PublicCatalog, access identity.AccessStore, seed cityseed.Store, contentStore content.MomentStore, oidc *oidcauth.Service, db pinger, allowedOrigins []string) http.Handler {
	s := &server{catalog: catalog, access: access, seed: seed, content: contentStore, oidc: oidc, db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /v1/cities", s.listCities)
	mux.HandleFunc("GET /v1/cities/{cityID}", s.getCity)
	mux.HandleFunc("GET /v1/cities/{cityID}/places", s.listPlaces)
	mux.HandleFunc("GET /v1/places/{placeID}", s.getPlace)
	mux.HandleFunc("GET /v1/cities/{cityID}/activities", s.listActivities)
	mux.HandleFunc("GET /v1/activities/{activityID}", s.getActivity)
	mux.HandleFunc("GET /v1/cities/{cityID}/activity-candidates", s.listActivityCandidates)
	mux.HandleFunc("POST /v1/cities/{cityID}/activity-candidates", s.submitActivityCandidate)
	mux.HandleFunc("POST /v1/activity-candidates/{candidateID}/review", s.reviewActivityCandidate)
	mux.HandleFunc("GET /v1/me", s.me)
	mux.HandleFunc("GET /v1/me/moments", s.listOwnMoments)
	mux.HandleFunc("POST /v1/me/moments", s.createMomentDraft)
	mux.HandleFunc("GET /v1/me/moments/{momentID}", s.getOwnMoment)
	mux.HandleFunc("PUT /v1/me/moments/{momentID}", s.updateMomentDraft)
	mux.HandleFunc("DELETE /v1/me/moments/{momentID}", s.withdrawMoment)
	mux.HandleFunc("GET /v1/me/blocks", s.listBlocks)
	mux.HandleFunc("POST /v1/me/blocks", s.blockAccount)
	mux.HandleFunc("DELETE /v1/me/blocks/{accountID}", s.unblockAccount)
	mux.HandleFunc("POST /v1/session/logout", s.logout)
	mux.HandleFunc("GET /v1/me/consents", s.listConsents)
	mux.HandleFunc("POST /v1/me/consents", s.grantConsent)
	mux.HandleFunc("DELETE /v1/me/consents/{grantID}", s.revokeConsent)
	mux.HandleFunc("GET /v1/accounts/{accountID}/profile", s.getProfile)
	mux.HandleFunc("GET /v1/cities/{cityID}/place-candidates", s.listPlaceCandidates)
	mux.HandleFunc("POST /v1/cities/{cityID}/place-candidates", s.submitPlaceCandidate)
	mux.HandleFunc("POST /v1/place-candidates/{candidateID}/review", s.reviewPlaceCandidate)
	mux.HandleFunc("GET /v1/auth/oidc/start", s.startOIDC)
	mux.HandleFunc("GET /v1/auth/oidc/status", s.oidcStatus)
	mux.HandleFunc("GET /v1/auth/oidc/callback", s.completeOIDC)
	mux.HandleFunc("POST /v1/auth/oidc/exchange", s.exchangeOIDC)
	return cors(mux, allowedOrigins)
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		respondError(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *server) listCities(w http.ResponseWriter, r *http.Request) {
	cities, err := s.catalog.ListCities(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": cities})
}

func (s *server) getCity(w http.ResponseWriter, r *http.Request) {
	city, err := s.catalog.GetCity(r.Context(), r.PathValue("cityID"))
	if handleReadError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": city})
}

func (s *server) listPlaces(w http.ResponseWriter, r *http.Request) {
	cityID := r.PathValue("cityID")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 240 {
		respondError(w, http.StatusBadRequest, "invalid_place_query")
		return
	}
	_, err := s.catalog.GetCity(r.Context(), cityID)
	if handleReadError(w, err) {
		return
	}
	places, err := s.catalog.ListPlaces(r.Context(), cityID, query)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": places})
}

func (s *server) getPlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("placeID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_place_id")
		return
	}
	place, err := s.catalog.GetPlace(r.Context(), id)
	if handleReadError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": place})
}

func (s *server) listActivities(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	_, err = s.catalog.GetCity(r.Context(), cityID)
	if handleReadError(w, err) {
		return
	}
	activities, err := s.catalog.ListActivities(r.Context(), cityID, actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": activities})
}

func (s *server) getActivity(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("activityID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return
	}
	activity, err := s.catalog.GetActivity(r.Context(), id, actor.ID)
	if handleReadError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": activity})
}

func handleReadError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, foundation.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else {
		serverError(w, err)
	}
	return true
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("foundation read failed: %v", err)
	respondError(w, http.StatusInternalServerError, "internal_error")
}

func respondError(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code}})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func cors(next http.Handler, allowedOrigins []string) http.Handler {
	allowed := make(map[string]bool)
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			} else if r.Method == http.MethodOptions {
				respondError(w, http.StatusForbidden, "origin_not_allowed")
				return
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
