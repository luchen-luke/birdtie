package httpapi

import (
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/oidcauth"
)

func (s *server) oidcStatus(w http.ResponseWriter, _ *http.Request) {
	respond(w, http.StatusOK, map[string]any{"data": map[string]bool{
		"configured": s.oidc != nil,
	}})
}

func (s *server) startOIDC(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		respondError(w, http.StatusServiceUnavailable, "oidc_not_configured")
		return
	}
	target, err := s.oidc.Start(r.Context(), r.URL.Query().Get("challenge"))
	if errors.Is(err, oidcauth.ErrInvalidFlow) {
		respondError(w, http.StatusBadRequest, "invalid_pkce_challenge")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	noCredentialCache(w)
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *server) completeOIDC(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		respondError(w, http.StatusServiceUnavailable, "oidc_not_configured")
		return
	}
	if r.URL.Query().Get("error") != "" {
		noCredentialCache(w)
		http.Redirect(w, r, s.oidc.FailureRedirect(), http.StatusSeeOther)
		return
	}
	target, err := s.oidc.Complete(
		r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if errors.Is(err, oidcauth.ErrInvalidFlow) {
		noCredentialCache(w)
		http.Redirect(w, r, s.oidc.FailureRedirect(), http.StatusSeeOther)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	noCredentialCache(w)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *server) exchangeOIDC(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		respondError(w, http.StatusServiceUnavailable, "oidc_not_configured")
		return
	}
	var input struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	token, err := s.oidc.Exchange(r.Context(), input.Code, input.Verifier)
	if errors.Is(err, oidcauth.ErrInvalidFlow) {
		respondError(w, http.StatusUnauthorized, "invalid_exchange_code")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	noCredentialCache(w)
	respond(w, http.StatusOK, map[string]any{"data": map[string]any{
		"accessToken":        token,
		"tokenType":          "Bearer",
		"expiresInSeconds":   28800,
		"idleTimeoutSeconds": 1800,
	}})
}

func noCredentialCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
