package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"strings"
)

func (s *server) registerDevEmailRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/auth/dev-email/status", s.devEmailStatus)
	mux.HandleFunc("POST /v1/auth/dev-email/code", s.requestDevEmailCode)
	mux.HandleFunc("POST /v1/auth/dev-email/verify", s.verifyDevEmailCode)
}
func (s *server) devEmailPort() (devauth.EmailStore, bool) {
	p, ok := s.devPhone.(devauth.EmailStore)
	return p, ok && !nilHumanSelfReviewPort(p)
}
func (s *server) devEmailStatus(w http.ResponseWriter, _ *http.Request) {
	_, ok := s.devEmailPort()
	noCredentialCache(w)
	respond(w, http.StatusOK, map[string]any{"data": map[string]bool{"enabled": s.devPhoneEnabled && ok}})
}
func (s *server) devEmailReady(w http.ResponseWriter, r *http.Request) (devauth.EmailStore, bool) {
	if !s.devPhoneEnabled {
		respondError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusBadRequest, "invalid_email_login")
		return nil, false
	}
	p, ok := s.devEmailPort()
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "email_login_unavailable")
	}
	return p, ok
}
func (s *server) requestDevEmailCode(w http.ResponseWriter, r *http.Request) {
	p, ok := s.devEmailReady(w, r)
	if !ok {
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	email, ok := devauth.NormalizeEmail(input.Email)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid_email")
		return
	}
	e := p.RequestDevEmailChallenge(r.Context(), devauth.EmailDigest(email))
	if errors.Is(e, devauth.ErrRateLimited) {
		respondError(w, http.StatusTooManyRequests, "rate_limited")
	} else if errors.Is(e, devauth.ErrDisabled) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if e != nil {
		serverError(w, e)
	} else {
		noCredentialCache(w)
		respond(w, http.StatusOK, map[string]any{"data": map[string]int{"expiresInSeconds": 300}})
	}
}
func (s *server) verifyDevEmailCode(w http.ResponseWriter, r *http.Request) {
	p, ok := s.devEmailReady(w, r)
	if !ok {
		return
	}
	var input struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	email, ok := devauth.NormalizeEmail(input.Email)
	code := strings.TrimSpace(input.Code)
	if !ok || len(code) != 6 {
		respondError(w, http.StatusBadRequest, "invalid_email_or_code")
		return
	}
	token, digest, e := identity.NewToken()
	if e != nil {
		serverError(w, e)
		return
	}
	e = p.VerifyDevEmailChallenge(r.Context(), devauth.EmailDigest(email), code == devauth.Code, digest)
	switch {
	case errors.Is(e, devauth.ErrInvalid):
		respondError(w, http.StatusUnauthorized, "invalid_code")
	case errors.Is(e, devauth.ErrRateLimited):
		respondError(w, http.StatusTooManyRequests, "rate_limited")
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, http.StatusForbidden, "account_unavailable")
	case errors.Is(e, devauth.ErrDisabled):
		respondError(w, http.StatusNotFound, "not_found")
	case e != nil:
		serverError(w, e)
	default:
		noCredentialCache(w)
		respond(w, http.StatusOK, map[string]any{"data": map[string]any{"accessToken": token, "tokenType": "Bearer", "expiresInSeconds": 28800, "idleTimeoutSeconds": 1800, "credentialKind": "email"}})
	}
}
