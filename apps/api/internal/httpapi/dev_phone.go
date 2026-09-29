package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) devPhoneStatus(w http.ResponseWriter, _ *http.Request) {
	respond(w, http.StatusOK, map[string]any{"data": map[string]bool{
		"enabled": s.devPhoneEnabled,
	}})
}

func (s *server) requestDevPhoneCode(w http.ResponseWriter, r *http.Request) {
	if !s.devPhoneEnabled {
		respondError(w, http.StatusNotFound, "not_found")
		return
	}
	var input struct {
		Phone string `json:"phone"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	phone, ok := devauth.NormalizePhone(input.Phone)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid_phone")
		return
	}
	err := s.devPhone.RequestDevPhoneChallenge(r.Context(), devauth.PhoneDigest(phone))
	if errors.Is(err, devauth.ErrRateLimited) {
		respondError(w, http.StatusTooManyRequests, "rate_limited")
	} else if err != nil {
		serverError(w, err)
	} else {
		noCredentialCache(w)
		respond(w, http.StatusOK, map[string]any{"data": map[string]int{
			"expiresInSeconds": 300,
		}})
	}
}

func (s *server) verifyDevPhoneCode(w http.ResponseWriter, r *http.Request) {
	if !s.devPhoneEnabled {
		respondError(w, http.StatusNotFound, "not_found")
		return
	}
	var input struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	phone, ok := devauth.NormalizePhone(input.Phone)
	code := strings.TrimSpace(input.Code)
	if !ok || len(code) != 6 {
		respondError(w, http.StatusBadRequest, "invalid_phone_or_code")
		return
	}
	token, digest, err := identity.NewToken()
	if err != nil {
		serverError(w, err)
		return
	}
	err = s.devPhone.VerifyDevPhoneChallenge(
		r.Context(), devauth.PhoneDigest(phone), code == devauth.Code, digest)
	switch {
	case errors.Is(err, devauth.ErrInvalid):
		respondError(w, http.StatusUnauthorized, "invalid_code")
	case errors.Is(err, devauth.ErrRateLimited):
		respondError(w, http.StatusTooManyRequests, "rate_limited")
	case errors.Is(err, identity.ErrUnauthorized):
		respondError(w, http.StatusForbidden, "account_unavailable")
	case err != nil:
		serverError(w, err)
	default:
		noCredentialCache(w)
		respond(w, http.StatusOK, map[string]any{"data": map[string]any{
			"accessToken": token, "tokenType": "Bearer",
			"expiresInSeconds": 28800, "idleTimeoutSeconds": 1800,
		}})
	}
}
