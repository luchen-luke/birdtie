package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/sessionrefresh"
)

// Device endpoints accept small strict bodies and never log credentials or
// expose whether an account, identity, family, or token exists.
func decodeDeviceCredential(w http.ResponseWriter, r *http.Request, target any, allowEmpty bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if allowEmpty && errors.Is(err, io.EOF) {
		return true
	}
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid_refresh_request")
		return false
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		respondError(w, http.StatusBadRequest, "invalid_refresh_request")
		return false
	}
	return true
}

func (s *server) refreshPersistentSession(w http.ResponseWriter, r *http.Request) {
	noCredentialCache(w)
	var in struct {
		RefreshToken string `json:"refreshToken"`
		OperationID  string `json:"operationId"`
	}
	if !decodeDeviceCredential(w, r, &in, false) {
		return
	}
	op, err := sessionrefresh.NormalizeOperationID(in.OperationID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid_refresh_request")
		return
	}
	if _, err = sessionrefresh.ParseToken(in.RefreshToken); err != nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	store, ok := s.access.(sessionrefresh.Store)
	if !ok || !s.devPhoneEnabled {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	result, err := store.RefreshSession(r.Context(), in.RefreshToken, op)
	if errors.Is(err, identity.ErrUnauthorized) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if errors.Is(err, sessionrefresh.ErrInvalidRequest) {
		respondError(w, http.StatusBadRequest, "invalid_refresh_request")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": result})
}

// Returns false only for the original body-less access-token logout route.
func (s *server) logoutPersistentSession(w http.ResponseWriter, r *http.Request) bool {
	noCredentialCache(w)
	var in struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !decodeDeviceCredential(w, r, &in, true) {
		return true
	}
	if in.RefreshToken == "" {
		return false
	}
	if _, err := sessionrefresh.ParseToken(in.RefreshToken); err != nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return true
	}
	store, ok := s.access.(sessionrefresh.Store)
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return true
	}
	err := store.RevokeRefreshFamily(r.Context(), in.RefreshToken)
	if errors.Is(err, identity.ErrUnauthorized) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return true
	}
	if err != nil {
		serverError(w, err)
		return true
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}
