package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) actor(r *http.Request, required bool) (identity.Actor, [32]byte, error) {
	var empty [32]byte
	headers := r.Header.Values("Authorization")
	if len(headers) == 0 && !required {
		return identity.Actor{}, empty, nil
	}
	if len(headers) != 1 {
		return identity.Actor{}, empty, identity.ErrUnauthorized
	}
	digest, err := identity.ParseBearer(headers[0])
	if err != nil {
		return identity.Actor{}, empty, err
	}
	actor, err := s.access.Authenticate(r.Context(), digest)
	return actor, digest, err
}

func authFailed(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, http.StatusUnauthorized, "unauthorized")
	} else {
		serverError(w, err)
	}
	return true
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": actor})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	_, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if err := s.access.RevokeSession(r.Context(), digest); err != nil {
		if authFailed(w, err) {
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) updateOwnProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		publicProfileError(w, http.StatusForbidden, "forbidden", "请在当前账号的本人资料中修改")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		publicProfileError(w, http.StatusBadRequest, "invalid_profile", "本人资料不接受目标选择参数")
		return
	}
	if s.access == nil || s.humanProfiles == nil {
		publicProfileError(w, http.StatusServiceUnavailable, "profile_unavailable", "资料服务暂不可用，请稍后重试")
		return
	}
	actor, digest, err := s.actor(r, true)
	if err != nil {
		publicProfileFailure(w, err)
		return
	}
	input, err := decodePublicProfile(w, r)
	if err != nil {
		publicProfileFailure(w, err)
		return
	}
	profile, err := s.humanProfiles.UpdateHumanProfile(r.Context(), digest, actor, input)
	if err != nil {
		publicProfileFailure(w, err)
	} else {
		returned, validErr := identity.NormalizeHumanProfileInput(identity.ProfileInput{DisplayName: profile.DisplayName, Bio: profile.Bio, Visibility: profile.Visibility})
		if validErr != nil || profile.AccountID != actor.ID || returned != input || profile.DisplayName != returned.DisplayName || profile.Bio != returned.Bio {
			publicProfileFailure(w, identity.ErrProfileUnavailable)
			return
		}
		respond(w, http.StatusOK, map[string]any{"data": profile})
	}
}

func (s *server) getProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("accountID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_account_id")
		return
	}
	actor, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	profile, err := s.access.ReadProfile(r.Context(), actor.ID, id)
	if errors.Is(err, identity.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": profile})
}

func (s *server) listConsents(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	grants, err := s.access.ListProfileGrants(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": grants})
}

func (s *server) grantConsent(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	var request struct {
		RecipientAccountID string    `json:"recipientAccountId"`
		ExpiresAt          time.Time `json:"expiresAt"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if !uuidPath.MatchString(request.RecipientAccountID) {
		respondError(w, http.StatusBadRequest, "invalid_recipient")
		return
	}
	grant, err := s.access.GrantProfileRead(
		r.Context(), actor.ID, request.RecipientAccountID, request.ExpiresAt)
	switch {
	case errors.Is(err, identity.ErrInvalidGrant):
		respondError(w, http.StatusBadRequest, "invalid_grant")
	case errors.Is(err, identity.ErrConflict):
		respondError(w, http.StatusConflict, "active_grant_exists")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, http.StatusCreated, map[string]any{"data": grant})
	}
}

func (s *server) revokeConsent(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	grantID := r.PathValue("grantID")
	if !uuidPath.MatchString(grantID) {
		respondError(w, http.StatusBadRequest, "invalid_grant_id")
		return
	}
	err = s.access.RevokeProfileGrant(r.Context(), actor.ID, grantID)
	if errors.Is(err, identity.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
