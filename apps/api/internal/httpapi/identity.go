package httpapi

import (
	"context"
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
	if s.logoutPersistentSession(w, r) {
		return
	}
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
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("accountID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_account_id")
		return
	}
	if nilHumanSelfReviewPort(s.access) {
		publicProfileFailure(w, identity.ErrProfileUnavailable)
		return
	}
	actor, digest, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	access := identity.ProfileAccess{Actor: actor, SessionDigest: digest}
	current, hasCurrent := s.access.(identity.CurrentProfileStore)
	if hasCurrent && nilHumanSelfReviewPort(current) {
		publicProfileFailure(w, identity.ErrProfileUnavailable)
		return
	}
	var profile identity.Profile
	if hasCurrent {
		profile, err = current.ReadProfileCurrent(r.Context(), access, id)
	} else {
		profile, err = s.access.ReadProfile(r.Context(), actor.ID, id)
		// Legacy adapters cannot assert a new relationship audience without the
		// current typed source/session fence. Native Store always uses it above.
		if err == nil && profile.Visibility != "public" && profile.Visibility != "private" {
			err = identity.ErrProfileUnavailable
		}
	}
	if errors.Is(err, identity.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		publicProfileFailure(w, err)
		return
	}
	if profile.AccountID != id || !identity.ProfileVisibilityValid(profile.Visibility) {
		publicProfileFailure(w, identity.ErrProfileUnavailable)
		return
	}
	encoded, err := json.Marshal(map[string]any{"data": profile})
	if err != nil || r.Context().Err() != nil {
		publicProfileFailure(w, identity.ErrProfileUnavailable)
		return
	}
	if hasCurrent {
		if err = current.ValidateProfileCurrent(r.Context(), access, id, profile); err != nil {
			switch {
			case errors.Is(err, identity.ErrNotFound):
				respondError(w, http.StatusNotFound, "not_found")
			case errors.Is(err, identity.ErrConflict):
				publicProfileError(w, http.StatusConflict, "profile_changed", "资料或可见范围已变化，请重新查看")
			default:
				publicProfileFailure(w, err)
			}
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(encoded, '\n'))
}

func (s *server) listConsents(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) || !humanControlNoBody(w, r) {
		return
	}
	if s.access == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	items, e := s.access.ListProfileGrants(r.Context(), a.ID)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	original, _ := json.Marshal(items)
	proof := humanControlFunctionProof(func(ctx context.Context) error {
		current, e := s.access.ListProfileGrants(ctx, a.ID)
		if e != nil {
			return identity.ErrControlUnavailable
		}
		raw, e := json.Marshal(current)
		if e != nil || string(raw) != string(original) {
			return identity.ErrControlChanged
		}
		return nil
	})
	s.respondHumanControl(w, r, d, a, map[string]any{"data": items}, proof, true)
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
	s.legacyHumanControlWrite(w, r)
}

func (s *server) readGrantRevokeControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "grantID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.access.(identity.GrantRevokeControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	v, e := port.ReadGrantRevokeControl(r.Context(), d, a, r.PathValue("grantID"))
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) previewGrantRevokeControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) {
		return
	}
	port, ok := s.access.(identity.GrantRevokeControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := identity.DecodeGrantRevokeControl(raw, false)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	v, e := port.PreviewGrantRevokeControl(r.Context(), d, a, in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, identity.GrantRevokeControlDigest(in)) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitGrantRevokeControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	port, ok := s.access.(identity.GrantRevokeControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := identity.DecodeGrantRevokeControl(raw, true)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_human_control")
		return
	}
	v, e := port.CommitGrantRevokeControl(r.Context(), d, a, in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "GRANT_REVOKE", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readGrantRevokeControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyGrantRevokeControl(w, r, false)
}
func (s *server) stopGrantRevokeControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyGrantRevokeControl(w, r, true)
}
func (s *server) historyGrantRevokeControl(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.access.(identity.GrantRevokeControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	var v identity.HumanControlReceipt
	var e error
	if stop {
		v, e = port.StopGrantRevokeOperation(r.Context(), d, a, r.PathValue("operationID"))
	} else {
		v, e = port.ReadGrantRevokeOperation(r.Context(), d, a, r.PathValue("operationID"))
	}
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "GRANT_REVOKE", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) registerGrantRevokeControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/consent-revoke-control/{grantID}", s.readGrantRevokeControl)
	mux.HandleFunc("POST /v1/me/consent-revoke-control/previews", s.previewGrantRevokeControl)
	mux.HandleFunc("PUT /v1/me/consent-revoke-control/operations/{operationID}", s.commitGrantRevokeControl)
	mux.HandleFunc("GET /v1/me/consent-revoke-control/operations/{operationID}", s.readGrantRevokeControlOperation)
	mux.HandleFunc("POST /v1/me/consent-revoke-control/operations/{operationID}/stop", s.stopGrantRevokeControlOperation)
}
